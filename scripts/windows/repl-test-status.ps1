<#
.SYNOPSIS
    Show PostgreSQL replication test environment status

.DESCRIPTION
    Displays the current status of all replication test containers, including
    streaming replication lag, slot status, and logical replication info.

.PARAMETER Watch
    Continuously monitor (refresh every 2s)

.PARAMETER Json
    Output in JSON format

.EXAMPLE
    .\repl-test-status.ps1
    Show current status

.EXAMPLE
    .\repl-test-status.ps1 -Watch
    Continuously monitor status
#>

[CmdletBinding()]
param(
    [switch]$Watch,
    [switch]$Json
)

$PrimaryName = "steep-pg-primary"
$ContainerPrefix = "steep-pg-"

# Colors
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[OK] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

function Show-Status {
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

    if ($Json) {
        Show-StatusJson
        return
    }

    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "Replication Test Environment Status" -ForegroundColor Cyan
    Write-Host "Time: $timestamp"
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host ""

    # Check if primary is running
    $running = docker ps --format "{{.Names}}" 2>$null | Where-Object { $_ -eq $PrimaryName }
    if (-not $running) {
        Write-Err "Primary container is not running"
        Write-Host ""
        Write-Host "Run .\repl-test-setup.ps1 to create the environment"
        return $false
    }

    # Container status
    Write-Host "Containers:" -ForegroundColor Blue
    docker ps -a --filter "name=$ContainerPrefix" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>$null | Out-Host
    Write-Host ""

    # Primary info
    Write-Host "Primary Server:" -ForegroundColor Blue
    $version = docker exec $PrimaryName psql -U postgres -t -c "SELECT 'Version: ' || version();" 2>$null
    if ($version) {
        Write-Host ($version | Select-Object -First 1).Trim()
    } else {
        Write-Err "Cannot connect to primary"
    }

    $isPrimary = docker exec $PrimaryName psql -U postgres -t -c "SELECT NOT pg_is_in_recovery();" 2>$null
    if ($isPrimary -and $isPrimary.Trim() -eq "t") {
        Write-Host "  Role: " -NoNewline
        Write-Host "PRIMARY" -ForegroundColor Green
    } else {
        Write-Host "  Role: " -NoNewline
        Write-Host "STANDBY" -ForegroundColor Yellow
    }
    Write-Host ""

    # Replication status
    Write-Host "Streaming Replication:" -ForegroundColor Blue
    $replCount = (docker exec $PrimaryName psql -U postgres -t -c "SELECT count(*) FROM pg_stat_replication;" 2>$null) -join ""
    $replCount = if ($replCount) { $replCount.Trim() } else { "0" }

    if ([int]$replCount -gt 0) {
        $sql = "SELECT application_name AS replica, client_addr AS address, state, sync_state AS sync, pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) AS lag, CASE WHEN pg_wal_lsn_diff(sent_lsn, replay_lsn) < 1048576 THEN 'healthy' WHEN pg_wal_lsn_diff(sent_lsn, replay_lsn) < 10485760 THEN 'warning' ELSE 'critical' END AS status FROM pg_stat_replication ORDER BY application_name;"
        docker exec $PrimaryName psql -U postgres -c $sql 2>$null | Out-Host
    } else {
        Write-Host "  No streaming replicas connected"
    }
    Write-Host ""

    # Replication slots
    Write-Host "Replication Slots:" -ForegroundColor Blue
    $slotCount = (docker exec $PrimaryName psql -U postgres -t -c "SELECT count(*) FROM pg_replication_slots;" 2>$null) -join ""
    $slotCount = if ($slotCount) { $slotCount.Trim() } else { "0" }

    if ([int]$slotCount -gt 0) {
        $sql = "SELECT slot_name, slot_type AS type, CASE WHEN active THEN 'yes' ELSE 'no' END AS active, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained, COALESCE(wal_status, '-') AS wal_status FROM pg_replication_slots ORDER BY slot_name;"
        docker exec $PrimaryName psql -U postgres -c $sql 2>$null | Out-Host
    } else {
        Write-Host "  No replication slots"
    }
    Write-Host ""

    # Check for logical replication
    $pubCount = (docker exec $PrimaryName psql -U postgres -d steep_test -t -c "SELECT count(*) FROM pg_publication;" 2>$null) -join ""
    $pubCount = if ($pubCount) { $pubCount.Trim() } else { "0" }

    if ([int]$pubCount -gt 0) {
        Write-Host "Publications (Primary):" -ForegroundColor Blue
        $sql = "SELECT pubname AS name, CASE WHEN puballtables THEN 'all' ELSE 'partial' END AS tables, CONCAT_WS(' ', CASE WHEN pubinsert THEN 'I' END, CASE WHEN pubupdate THEN 'U' END, CASE WHEN pubdelete THEN 'D' END, CASE WHEN pubtruncate THEN 'T' END) AS operations FROM pg_publication;"
        docker exec $PrimaryName psql -U postgres -d steep_test -c $sql 2>$null | Out-Host
        Write-Host ""
    }

    # Check subscriber if it exists
    $subscriber = docker ps --format "{{.Names}}" 2>$null | Where-Object { $_ -eq "steep-pg-subscriber" }
    if ($subscriber) {
        Write-Host "Subscriptions (Subscriber):" -ForegroundColor Blue
        $sql = "SELECT subname AS name, CASE WHEN subenabled THEN 'yes' ELSE 'no' END AS enabled, substring(subconninfo from 'host=([^ ]+)') AS provider FROM pg_subscription;"
        docker exec steep-pg-subscriber psql -U postgres -d steep_test -c $sql 2>$null | Out-Host
        Write-Host ""
    }

    # Configuration status
    Write-Host "Configuration:" -ForegroundColor Blue
    $sql = "SELECT '  wal_level: ' || current_setting('wal_level') || CASE WHEN current_setting('wal_level') IN ('replica', 'logical') THEN ' (OK)' ELSE ' (needs config)' END;"
    $walLevel = (docker exec $PrimaryName psql -U postgres -t -c $sql 2>$null) -join ""
    if ($walLevel) { Write-Host $walLevel.Trim() }

    $sql = "SELECT '  max_wal_senders: ' || current_setting('max_wal_senders') || CASE WHEN current_setting('max_wal_senders')::int > 0 THEN ' (OK)' ELSE ' (needs config)' END;"
    $maxSenders = (docker exec $PrimaryName psql -U postgres -t -c $sql 2>$null) -join ""
    if ($maxSenders) { Write-Host $maxSenders.Trim() }

    $sql = "SELECT '  max_replication_slots: ' || current_setting('max_replication_slots') || CASE WHEN current_setting('max_replication_slots')::int > 0 THEN ' (OK)' ELSE ' (needs config)' END;"
    $maxSlots = (docker exec $PrimaryName psql -U postgres -t -c $sql 2>$null) -join ""
    if ($maxSlots) { Write-Host $maxSlots.Trim() }
    Write-Host ""

    # Connection info
    Write-Host "Connection Info:" -ForegroundColor Blue
    $containerInfo = docker ps --filter "name=$ContainerPrefix" --format "{{.Names}}|{{.Ports}}" 2>$null
    foreach ($line in $containerInfo) {
        $parts = $line -split '\|'
        $name = $parts[0]
        $ports = $parts[1]

        if ($ports -match '0\.0\.0\.0:(\d+)') {
            $port = $matches[1]
            $role = "replica"
            if ($name -eq $PrimaryName) { $role = "primary" }
            elseif ($name -match "subscriber") { $role = "subscriber" }

            Write-Host "  $name ($role): postgres://postgres:postgres@localhost:$port/postgres"
        }
    }
    Write-Host ""

    return $true
}

function Show-StatusJson {
    # Build JSON output for programmatic use
    $containers = docker ps -a --filter "name=$ContainerPrefix" --format '{"name":"{{.Names}}","status":"{{.Status}}","ports":"{{.Ports}}"}' 2>$null

    $running = docker ps --format "{{.Names}}" 2>$null | Where-Object { $_ -eq $PrimaryName }
    if ($running) {
        $sql = "SELECT json_agg(row_to_json(r)) FROM (SELECT application_name, state, sync_state, pg_wal_lsn_diff(sent_lsn, replay_lsn) AS lag_bytes FROM pg_stat_replication) r;"
        $replication = (docker exec $PrimaryName psql -U postgres -t -c $sql 2>$null) -join ""
        $replication = if ($replication) { $replication.Trim() } else { "null" }

        $sql = "SELECT json_agg(row_to_json(s)) FROM (SELECT slot_name, slot_type, active, pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS retained_bytes FROM pg_replication_slots) s;"
        $slots = (docker exec $PrimaryName psql -U postgres -t -c $sql 2>$null) -join ""
        $slots = if ($slots) { $slots.Trim() } else { "null" }
    } else {
        $replication = "null"
        $slots = "null"
    }

    $containersJson = if ($containers) { "[$($containers -join ',')]" } else { "[]" }

    @"
{
  "timestamp": "$(Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ')",
  "containers": $containersJson,
  "replication": $replication,
  "slots": $slots
}
"@
}

# Main execution
if ($Watch) {
    while ($true) {
        Clear-Host
        $null = Show-Status
        Write-Host "Refreshing every 2 seconds... Press Ctrl+C to stop" -ForegroundColor Cyan
        Start-Sleep -Seconds 2
    }
} else {
    $null = Show-Status
}
