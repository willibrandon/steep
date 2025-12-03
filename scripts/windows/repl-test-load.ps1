<#
.SYNOPSIS
    Generate load on the replication test environment

.DESCRIPTION
    Inserts, updates, and deletes data to create replication traffic for testing
    Steep's replication monitoring capabilities.

.PARAMETER Port
    Primary port (default: 15432)

.PARAMETER Database
    Database name (default: steep_test)

.PARAMETER BatchSize
    Batch size per operation (default: 100)

.PARAMETER Iterations
    Number of iterations, 0=infinite (default: 0)

.PARAMETER Delay
    Delay between iterations in seconds (default: 1)

.EXAMPLE
    .\repl-test-load.ps1
    Run with defaults

.EXAMPLE
    .\repl-test-load.ps1 -BatchSize 500
    500 rows per batch

.EXAMPLE
    .\repl-test-load.ps1 -Iterations 10
    Run 10 iterations then stop

.EXAMPLE
    .\repl-test-load.ps1 -BatchSize 200 -Iterations 50 -Delay 0.5
    200 rows, 50 iterations, 0.5s delay
#>

[CmdletBinding()]
param(
    [Alias("p")]
    [int]$Port = 15432,

    [Alias("d")]
    [string]$Database = "steep_test",

    [Alias("b")]
    [int]$BatchSize = 100,

    [Alias("n")]
    [int]$Iterations = 0,

    [Alias("s")]
    [double]$Delay = 1
)

$PrimaryName = "steep-pg-primary"

if (-not $env:PGPASSWORD) {
    $env:PGPASSWORD = "postgres"
}

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Replication Load Generator" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Primary: localhost:$Port/$Database"
Write-Host "Batch size: $BatchSize rows"
$iterText = if ($Iterations -eq 0) { "infinite" } else { $Iterations }
Write-Host "Iterations: $iterText"
Write-Host "Delay: ${Delay}s"
Write-Host ""
Write-Host "Press Ctrl+C to stop" -ForegroundColor Yellow
Write-Host ""

# Check connection
$testConn = docker exec $PrimaryName psql -U postgres -d $Database -c "SELECT 1" 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Cannot connect to primary at localhost:$Port" -ForegroundColor Red
    Write-Host "Make sure the replication test environment is running:"
    Write-Host "  .\repl-test-setup.ps1"
    exit 1
}

# Create load test table if it doesn't exist
Write-Host "Creating load test table..." -ForegroundColor Cyan
docker exec $PrimaryName psql -U postgres -d $Database -c @"
CREATE TABLE IF NOT EXISTS load_test (
    id SERIAL PRIMARY KEY,
    data TEXT,
    counter INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_load_test_created ON load_test(created_at);
CREATE INDEX IF NOT EXISTS idx_load_test_counter ON load_test(counter);
"@ 2>$null | Out-Null

$iteration = 0
$totalInserts = 0
$totalUpdates = 0
$totalDeletes = 0

# Cleanup handler
$cleanup = {
    Write-Host ""
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "Load Generation Summary" -ForegroundColor Cyan
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "Iterations completed: $iteration"
    Write-Host "Total inserts: $totalInserts"
    Write-Host "Total updates: $totalUpdates"
    Write-Host "Total deletes: $totalDeletes"
    Write-Host ""
}

try {
    while ($true) {
        $iteration++

        Write-Host "--- Iteration $iteration ---" -ForegroundColor Green

        # INSERT batch
        Write-Host "  Inserting $BatchSize rows... " -NoNewline
        docker exec $PrimaryName psql -U postgres -d $Database -q -c @"
INSERT INTO load_test (data, counter)
SELECT
    md5(random()::text) || md5(random()::text),
    floor(random() * 1000)::int
FROM generate_series(1, $BatchSize);
"@ 2>$null | Out-Null
        $totalInserts += $BatchSize
        Write-Host "done" -ForegroundColor Green

        Start-Sleep -Milliseconds 500

        # UPDATE random rows
        Write-Host "  Updating random rows... " -NoNewline
        $updated = docker exec $PrimaryName psql -U postgres -d $Database -t -q -c @"
UPDATE load_test
SET counter = counter + 1,
    updated_at = NOW(),
    data = md5(random()::text)
WHERE id IN (
    SELECT id FROM load_test
    ORDER BY random()
    LIMIT $BatchSize
);
SELECT count(*) FROM load_test WHERE updated_at > NOW() - interval '2 seconds';
"@ 2>$null
        $updated = if ($updated) { $updated.Trim() } else { "0" }
        $totalUpdates += [int]$updated
        Write-Host "$updated rows" -ForegroundColor Green

        Start-Sleep -Milliseconds 500

        # DELETE oldest rows (keep table size manageable)
        Write-Host "  Deleting old rows... " -NoNewline
        $deleted = docker exec $PrimaryName psql -U postgres -d $Database -t -q -c @"
WITH deleted AS (
    DELETE FROM load_test
    WHERE id IN (
        SELECT id FROM load_test
        ORDER BY created_at ASC
        LIMIT $BatchSize
    )
    RETURNING id
)
SELECT count(*) FROM deleted;
"@ 2>$null
        $deleted = if ($deleted) { $deleted.Trim() } else { "0" }
        $totalDeletes += [int]$deleted
        Write-Host "$deleted rows" -ForegroundColor Green

        # Show current table size
        $rowCount = docker exec $PrimaryName psql -U postgres -d $Database -t -q -c "SELECT count(*) FROM load_test" 2>$null
        $rowCount = if ($rowCount) { $rowCount.Trim() } else { "0" }
        Write-Host "  Table size: " -NoNewline
        Write-Host "$rowCount rows" -ForegroundColor Yellow

        # Check if we've reached iteration limit
        if ($Iterations -gt 0 -and $iteration -ge $Iterations) {
            Write-Host ""
            Write-Host "Reached iteration limit ($Iterations)" -ForegroundColor Cyan
            & $cleanup
            exit 0
        }

        # Delay between iterations
        Start-Sleep -Seconds $Delay
    }
} finally {
    & $cleanup
}
