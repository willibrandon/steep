<#
.SYNOPSIS
    Generate heavy load to create visible replication lag

.DESCRIPTION
    Creates bursts of writes to intentionally cause lag for testing Steep's
    lag monitoring and visualization features.

.PARAMETER Port
    Primary port (default: 15432)

.PARAMETER Database
    Database name (default: steep_test)

.PARAMETER BurstSize
    Rows per burst (default: 10000)

.PARAMETER BurstCount
    Number of bursts (default: 5)

.PARAMETER BurstDelay
    Delay between bursts in seconds (default: 0.1)

.EXAMPLE
    .\repl-test-lag.ps1
    Run with defaults (50k rows total)

.EXAMPLE
    .\repl-test-lag.ps1 -BurstSize 50000 -BurstCount 3
    3 bursts of 50k rows each

.EXAMPLE
    .\repl-test-lag.ps1 -BurstSize 100000 -BurstCount 1
    Single burst of 100k rows
#>

[CmdletBinding()]
param(
    [Alias("p")]
    [int]$Port = 15432,

    [Alias("d")]
    [string]$Database = "steep_test",

    [Alias("b")]
    [int]$BurstSize = 10000,

    [Alias("n")]
    [int]$BurstCount = 5,

    [Alias("s")]
    [double]$BurstDelay = 0.1
)

$PrimaryName = "steep-pg-primary"

if (-not $env:PGPASSWORD) {
    $env:PGPASSWORD = "postgres"
}

$totalRows = $BurstSize * $BurstCount

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Replication Lag Generator" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Primary: localhost:$Port/$Database"
Write-Host "Burst size: $BurstSize rows"
Write-Host "Burst count: $BurstCount"
Write-Host "Total rows: $totalRows"
Write-Host ""

# Check connection
$testConn = docker exec $PrimaryName psql -U postgres -d $Database -c "SELECT 1" 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Cannot connect to primary at localhost:$Port" -ForegroundColor Red
    Write-Host "Make sure the replication test environment is running:"
    Write-Host "  .\repl-test-setup.ps1"
    exit 1
}

# Ensure lag_test table exists
Write-Host "Verifying lag test table..." -ForegroundColor Cyan
$tableExists = docker exec $PrimaryName psql -U postgres -d $Database -tAc "SELECT 1 FROM pg_tables WHERE tablename='lag_test'" 2>$null
if (-not $tableExists -or $tableExists.Trim() -ne "1") {
    Write-Host "Error: lag_test table not found. Run .\repl-test-setup.ps1 first" -ForegroundColor Red
    exit 1
}

# Show initial replication status
Write-Host "Initial replication status:" -ForegroundColor Cyan
docker exec $PrimaryName psql -U postgres -d $Database -t -c @"
SELECT
    application_name,
    pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes,
    pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) as lag_pretty
FROM pg_stat_replication;
"@ 2>$null

Write-Host ""
Write-Host "Starting burst writes - watch Steep for lag!" -ForegroundColor Yellow
Write-Host ""

Write-Host "Inserting $totalRows rows in a single transaction..." -ForegroundColor Green
Write-Host "(This keeps the transaction open longer to create visible lag)" -ForegroundColor Yellow
Write-Host ""

# Build the insert statements
$insertStatements = ""
for ($i = 1; $i -le $BurstCount; $i++) {
    $insertStatements += "INSERT INTO lag_test (data, padding) SELECT md5(random()::text) || md5(random()::text), repeat(md5(random()::text), 20) FROM generate_series(1, $BurstSize);`n"
}

# Start the insert as a background job
$insertJob = Start-Job -ScriptBlock {
    param($PrimaryName, $Database, $insertStatements)
    docker exec $PrimaryName psql -U postgres -d $Database -c @"
BEGIN;
$insertStatements
COMMIT;
"@
} -ArgumentList $PrimaryName, $Database, $insertStatements

# Monitor lag while insert is running
Write-Host "Monitoring lag during insert..." -ForegroundColor Cyan
for ($i = 1; $i -le 20; $i++) {
    $jobState = Get-Job -Id $insertJob.Id
    if ($jobState.State -ne "Running") {
        Write-Host "Insert completed" -ForegroundColor Green
        break
    }

    $lagInfo = docker exec $PrimaryName psql -U postgres -d $Database -t -c @"
    SELECT
        application_name,
        pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) as byte_lag,
        replay_lag::text as time_lag
    FROM pg_stat_replication
    LIMIT 1;
"@ 2>$null

    $lagInfo = if ($lagInfo) { $lagInfo.Trim() -replace '\s+', ' ' } else { "checking..." }
    Write-Host "  $lagInfo" -ForegroundColor Yellow

    Start-Sleep -Milliseconds 300
}

# Wait for insert to finish
Wait-Job -Job $insertJob | Out-Null
Remove-Job -Job $insertJob

Write-Host ""
Write-Host "Burst complete! Monitoring lag recovery..." -ForegroundColor Cyan
Write-Host ""

# Monitor lag recovery
for ($i = 1; $i -le 10; $i++) {
    $lagResult = docker exec $PrimaryName psql -U postgres -d $Database -t -c @"
    SELECT COALESCE(pg_wal_lsn_diff(sent_lsn, replay_lsn), 0)
    FROM pg_stat_replication
    LIMIT 1;
"@ 2>$null
    # Handle array output - take first non-empty line
    $lagBytes = if ($lagResult) {
        ($lagResult | Where-Object { $_.Trim() -ne "" } | Select-Object -First 1).Trim()
    } else { "0" }
    if (-not $lagBytes) { $lagBytes = "0" }

    $lagPrettyResult = docker exec $PrimaryName psql -U postgres -d $Database -t -c @"
    SELECT COALESCE(pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)), '0 bytes')
    FROM pg_stat_replication
    LIMIT 1;
"@ 2>$null
    # Handle array output - take first non-empty line
    $lagPretty = if ($lagPrettyResult) {
        ($lagPrettyResult | Where-Object { $_.Trim() -ne "" } | Select-Object -First 1).Trim()
    } else { "no replicas" }
    if (-not $lagPretty) { $lagPretty = "no replicas" }

    Write-Host "  Lag: " -NoNewline
    Write-Host $lagPretty -ForegroundColor Yellow

    if ([int64]$lagBytes -eq 0) {
        Write-Host "Replica caught up!" -ForegroundColor Green
        break
    }

    Start-Sleep -Milliseconds 500
}

Write-Host ""
Write-Host "Final table size:" -ForegroundColor Cyan
docker exec $PrimaryName psql -U postgres -d $Database -c @"
SELECT
    pg_size_pretty(pg_total_relation_size('lag_test')) as table_size,
    count(*) as row_count
FROM lag_test;
"@ 2>$null

Write-Host ""
Write-Host "Cleanup: TRUNCATE lag_test; to remove test data" -ForegroundColor Cyan
