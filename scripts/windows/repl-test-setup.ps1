<#
.SYNOPSIS
    Create a PostgreSQL replication test environment

.DESCRIPTION
    Sets up Docker containers for PostgreSQL primary and replica(s) with streaming
    and optionally logical replication for testing Steep's replication monitoring.

.PARAMETER Replicas
    Number of streaming replicas (default: 1)

.PARAMETER Logical
    Also set up logical replication (adds subscriber container)

.PARAMETER Cascade
    Set up cascading replication (replica1 -> replica2)

.PARAMETER PgVersion
    PostgreSQL version (default: 18)

.PARAMETER PrimaryPort
    Primary port (default: 15432)

.PARAMETER GenerateLag
    Insert data to create visible lag

.EXAMPLE
    .\repl-test-setup.ps1
    Basic: primary + 1 replica

.EXAMPLE
    .\repl-test-setup.ps1 -Replicas 2
    Primary + 2 replicas

.EXAMPLE
    .\repl-test-setup.ps1 -Logical
    Primary + replica + logical subscriber

.EXAMPLE
    .\repl-test-setup.ps1 -Cascade
    Primary -> replica1 -> replica2
#>

[CmdletBinding()]
param(
    [int]$Replicas = 1,
    [switch]$Logical,
    [switch]$Cascade,
    [string]$PgVersion = "18",
    [int]$PrimaryPort = 15432,
    [switch]$GenerateLag
)

$ErrorActionPreference = "Stop"

# Configuration
$NetworkName = "steep-repl-test"
$PrimaryName = "steep-pg-primary"
$ReplUser = "replicator"
$ReplPass = "repl_password"
$PostgresPass = "postgres"

# Colors
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[OK] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

# Check Docker is available
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Err "Docker is not installed or not in PATH"
    exit 1
}

# Check if containers already exist
$existingContainers = docker ps -a --format "{{.Names}}" 2>$null | Where-Object { $_ -eq $PrimaryName }
if ($existingContainers) {
    Write-Warn "Containers already exist. Run .\repl-test-teardown.ps1 first."
    exit 1
}

# Cascade requires at least 2 replicas
if ($Cascade) {
    $Replicas = [Math]::Max($Replicas, 2)
}

Write-Info "Setting up PostgreSQL replication test environment"
Write-Info "  PostgreSQL version: $PgVersion"
Write-Info "  Streaming replicas: $Replicas"
Write-Info "  Logical replication: $Logical"
Write-Info "  Cascading: $Cascade"
Write-Info "  Primary port: $PrimaryPort"
Write-Host ""

# Create network
Write-Info "Creating Docker network: $NetworkName"
docker network create $NetworkName 2>$null
Write-Success "Network ready"

# Start primary
Write-Info "Starting primary server..."
docker run -d `
    --name $PrimaryName `
    --network $NetworkName `
    --hostname pg-primary `
    --shm-size=256m `
    -e POSTGRES_PASSWORD=$PostgresPass `
    -e POSTGRES_HOST_AUTH_METHOD=scram-sha-256 `
    -e "POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256" `
    -p "${PrimaryPort}:5432" `
    "postgres:${PgVersion}" `
    -c wal_level=logical `
    -c max_wal_senders=100 `
    -c max_replication_slots=100 `
    -c hot_standby=on `
    -c hot_standby_feedback=on `
    -c wal_keep_size=256MB `
    -c max_connections=100 `
    "-c" "listen_addresses=*" | Out-Null

# Wait for primary to be ready (pg_isready + data directory initialized)
Write-Info "Waiting for primary to be ready..."
$primaryReady = $false
$pgData = ""
for ($i = 1; $i -le 60; $i++) {
    $ready = docker exec $PrimaryName pg_isready -U postgres 2>$null
    if ($LASTEXITCODE -eq 0) {
        # Get the actual PGDATA path (changed in PG18 to /var/lib/postgresql/18/docker)
        $pgData = (docker exec $PrimaryName printenv PGDATA 2>$null).Trim()
        if ($pgData) {
            # Check that pg_hba.conf exists (initdb complete)
            $null = docker exec $PrimaryName ls "$pgData/pg_hba.conf" 2>$null
            if ($LASTEXITCODE -eq 0) {
                $primaryReady = $true
                break
            }
        }
    }
    Start-Sleep -Seconds 1
}

if (-not $primaryReady) {
    Write-Err "Primary failed to start or initialize"
    exit 1
}
Write-Success "Primary is ready (PGDATA: $pgData)"

# Configure pg_hba.conf for replication
Write-Info "Configuring replication access..."
$hbaLines = @(
    "# Replication connections",
    "host    replication     $ReplUser      0.0.0.0/0       scram-sha-256",
    "host    all             all             0.0.0.0/0       scram-sha-256"
)
foreach ($line in $hbaLines) {
    $result = docker exec $PrimaryName sh -c "echo '$line' >> $pgData/pg_hba.conf" 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Failed to configure pg_hba.conf: $result"
        exit 1
    }
}

# Reload configuration
docker exec -u postgres $PrimaryName pg_ctl reload -D $pgData 2>$null | Out-Null
Write-Success "Replication access configured"

# Create replication user
Write-Info "Creating replication user..."
docker exec $PrimaryName psql -U postgres -c "CREATE USER $ReplUser WITH REPLICATION LOGIN PASSWORD '$ReplPass';" 2>$null | Out-Null
Write-Success "Replication user created: $ReplUser"

# Create test database and table for lag generation
Write-Info "Creating test database..."
docker exec $PrimaryName psql -U postgres -c "CREATE DATABASE steep_test;" 2>$null | Out-Null
docker exec $PrimaryName psql -U postgres -d steep_test -c @"
    CREATE TABLE lag_test (
        id SERIAL PRIMARY KEY,
        data TEXT,
        padding TEXT,
        created_at TIMESTAMP DEFAULT NOW()
    );
    CREATE INDEX idx_lag_test_created ON lag_test(created_at);
"@ 2>$null | Out-Null
Write-Success "Test database created"

# Function to create a streaming replica
function New-Replica {
    param(
        [int]$ReplicaNum,
        [string]$UpstreamName,
        [string]$UpstreamHost
    )

    $replicaName = "steep-pg-replica$ReplicaNum"
    $replicaPort = $PrimaryPort + $ReplicaNum
    $slotName = "replica${ReplicaNum}_slot"

    Write-Info "Creating replication slot: $slotName on $UpstreamName..."
    docker exec $UpstreamName psql -U postgres -c "SELECT pg_create_physical_replication_slot('$slotName');" 2>$null | Out-Null

    Write-Info "Taking base backup for replica$ReplicaNum..."

    # Create a volume for the replica data
    docker volume create "steep-replica${ReplicaNum}-data" 2>$null | Out-Null

    # Run pg_basebackup in a temporary container
    # Mount to PGDATA path and backup to same location
    $basebackupResult = docker run --rm `
        --network $NetworkName `
        -v "steep-replica${ReplicaNum}-data:$pgData" `
        -e PGPASSWORD=$ReplPass `
        "postgres:${PgVersion}" `
        pg_basebackup -h $UpstreamHost -U $ReplUser -D $pgData -Fp -Xs -P -R -S $slotName 2>&1

    if ($LASTEXITCODE -ne 0) {
        Write-Err "Base backup failed: $basebackupResult"
        return $false
    }

    # Configure replication settings
    $conninfo = "primary_conninfo = 'host=$UpstreamHost port=5432 user=$ReplUser password=$ReplPass application_name=replica$ReplicaNum'"
    $slotconf = "primary_slot_name = '$slotName'"

    docker run --rm `
        -v "steep-replica${ReplicaNum}-data:$pgData" `
        "postgres:${PgVersion}" `
        bash -c "echo `"$conninfo`" >> $pgData/postgresql.auto.conf && echo `"$slotconf`" >> $pgData/postgresql.auto.conf && chmod 700 $pgData" 2>$null

    Write-Info "Starting replica$ReplicaNum..."
    docker run -d `
        --name $replicaName `
        --network $NetworkName `
        --hostname "pg-replica$ReplicaNum" `
        --shm-size=256m `
        -e POSTGRES_PASSWORD=$PostgresPass `
        -v "steep-replica${ReplicaNum}-data:$pgData" `
        -p "${replicaPort}:5432" `
        "postgres:${PgVersion}" `
        -c hot_standby=on `
        -c max_wal_senders=100 `
        -c max_replication_slots=100 `
        -c wal_level=logical | Out-Null

    # Wait for replica to be ready
    Write-Info "Waiting for replica$ReplicaNum to be ready..."
    for ($i = 1; $i -le 60; $i++) {
        $ready = docker exec $replicaName pg_isready -U postgres 2>$null
        if ($LASTEXITCODE -eq 0) { break }
        Start-Sleep -Seconds 1
    }

    $ready = docker exec $replicaName pg_isready -U postgres 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Success "Replica$ReplicaNum is ready (port: $replicaPort)"
    } else {
        Write-Err "Replica$ReplicaNum failed to start"
        # Show logs for debugging
        docker logs --tail 20 $replicaName 2>&1
        return $false
    }
    return $true
}

# Create streaming replicas
if ($Cascade) {
    # Cascading: primary -> replica1 -> replica2
    $null = New-Replica -ReplicaNum 1 -UpstreamName $PrimaryName -UpstreamHost "pg-primary"
    Start-Sleep -Seconds 2
    Write-Info "Setting up cascading replication (replica1 -> replica2)..."
    $null = New-Replica -ReplicaNum 2 -UpstreamName "steep-pg-replica1" -UpstreamHost "pg-replica1"
} else {
    # Standard: all replicas connect to primary
    for ($i = 1; $i -le $Replicas; $i++) {
        $null = New-Replica -ReplicaNum $i -UpstreamName $PrimaryName -UpstreamHost "pg-primary"
    }
}

# Set up logical replication if requested
if ($Logical) {
    Write-Info "Setting up logical replication..."

    # Create publication on primary
    docker exec $PrimaryName psql -U postgres -d steep_test -c "CREATE PUBLICATION test_publication FOR TABLE lag_test;" 2>$null | Out-Null
    Write-Success "Publication created: test_publication"

    # Create a separate subscriber container
    $subscriberName = "steep-pg-subscriber"
    $subscriberPort = $PrimaryPort + $Replicas + 1

    Write-Info "Starting logical subscriber..."
    docker run -d `
        --name $subscriberName `
        --network $NetworkName `
        --hostname pg-subscriber `
        --shm-size=256m `
        -e POSTGRES_PASSWORD=$PostgresPass `
        -p "${subscriberPort}:5432" `
        "postgres:${PgVersion}" `
        -c wal_level=logical | Out-Null

    # Wait for subscriber to be ready
    for ($i = 1; $i -le 30; $i++) {
        $ready = docker exec $subscriberName pg_isready -U postgres 2>$null
        if ($LASTEXITCODE -eq 0) { break }
        Start-Sleep -Seconds 1
    }

    # Create matching database and table structure on subscriber
    docker exec $subscriberName psql -U postgres -c "CREATE DATABASE steep_test;" 2>$null | Out-Null
    docker exec $subscriberName psql -U postgres -d steep_test -c @"
        CREATE TABLE lag_test (
            id SERIAL PRIMARY KEY,
            data TEXT,
            padding TEXT,
            created_at TIMESTAMP DEFAULT NOW()
        );
        CREATE INDEX idx_lag_test_created ON lag_test(created_at);
"@ 2>$null | Out-Null

    # Create subscription
    docker exec $subscriberName psql -U postgres -d steep_test -c @"
        CREATE SUBSCRIPTION test_subscription
        CONNECTION 'host=pg-primary port=5432 dbname=steep_test user=postgres password=$PostgresPass'
        PUBLICATION test_publication;
"@ 2>$null | Out-Null

    Write-Success "Subscriber ready (port: $subscriberPort)"
    Write-Success "Subscription created: test_subscription"
}

# Generate some lag if requested
if ($GenerateLag) {
    Write-Info "Generating test data to create lag..."
    docker exec $PrimaryName psql -U postgres -d steep_test -c @"
        INSERT INTO lag_test (data)
        SELECT md5(random()::text)
        FROM generate_series(1, 10000);
"@ 2>$null | Out-Null
    Write-Success "Test data inserted"
}

# Verify replication is working
Write-Host ""
Write-Info "Verifying replication status..."
Start-Sleep -Seconds 2

$replStatus = docker exec $PrimaryName psql -U postgres -t -c @"
    SELECT application_name, state, sync_state,
           pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes
    FROM pg_stat_replication;
"@ 2>$null

if ($replStatus -and $replStatus.Trim()) {
    Write-Success "Streaming replication is active:"
    docker exec $PrimaryName psql -U postgres -c @"
        SELECT application_name AS replica,
               state,
               sync_state AS sync,
               pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) AS lag
        FROM pg_stat_replication;
"@ 2>$null
} else {
    Write-Warn "No streaming replicas connected yet (may need a moment to sync)"
}

# Show slot status
Write-Host ""
Write-Info "Replication slots:"
docker exec $PrimaryName psql -U postgres -c @"
    SELECT slot_name, slot_type, active,
           pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained
    FROM pg_replication_slots;
"@ 2>$null

# Show logical replication status if enabled
if ($Logical) {
    Write-Host ""
    Write-Info "Publications:"
    docker exec $PrimaryName psql -U postgres -d steep_test -c @"
        SELECT pubname, puballtables, pubinsert, pubupdate, pubdelete
        FROM pg_publication;
"@ 2>$null

    Write-Host ""
    Write-Info "Subscriptions:"
    docker exec $subscriberName psql -U postgres -d steep_test -c @"
        SELECT subname, subenabled, subconninfo
        FROM pg_subscription;
"@ 2>$null
}

# Print connection info
Write-Host ""
Write-Host "==========================================" -ForegroundColor Cyan
Write-Success "Replication test environment is ready!"
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "PostgreSQL connection details:"
Write-Host "  Primary:    localhost:$PrimaryPort (user: postgres, password: $PostgresPass)"
for ($i = 1; $i -le $Replicas; $i++) {
    $rport = $PrimaryPort + $i
    Write-Host "  Replica${i}:   localhost:$rport (user: postgres, password: $PostgresPass)"
}
if ($Logical) {
    $sPort = $PrimaryPort + $Replicas + 1
    Write-Host "  Subscriber: localhost:$sPort (database: steep_test)"
}
Write-Host ""
Write-Host "Quick start with Steep (use environment variables):"
Write-Host "  `$env:STEEP_CONNECTION_HOST='localhost'"
Write-Host "  `$env:STEEP_CONNECTION_PORT='$PrimaryPort'"
Write-Host "  `$env:STEEP_CONNECTION_USER='postgres'"
Write-Host "  `$env:STEEP_CONNECTION_DATABASE='postgres'"
Write-Host "  `$env:PGPASSWORD='$PostgresPass'"
Write-Host "  .\bin\steep.exe"
Write-Host ""
Write-Host "Or edit config.yaml to set port: $PrimaryPort"
Write-Host ""
Write-Host "To generate lag:"
Write-Host "  docker exec $PrimaryName psql -U postgres -d steep_test -c \"
Write-Host '    "INSERT INTO lag_test (data) SELECT md5(random()::text) FROM generate_series(1, 100000);"'
Write-Host ""
Write-Host "To tear down:"
Write-Host '  .\scripts\windows\repl-test-teardown.ps1'
Write-Host ""
