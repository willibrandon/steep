# Maintenance Operations Contract

**Package**: `internal/db/queries`

This contract defines the interface for database maintenance operations.

## Interface Definition

```go
// MaintenanceExecutor provides methods for executing and monitoring
// database maintenance operations.
type MaintenanceExecutor interface {
    // ExecuteVacuum runs VACUUM on a table.
    // Supports options: full, analyze, verbose.
    ExecuteVacuum(ctx context.Context, schema, table string, opts VacuumOptions) error

    // ExecuteAnalyze runs ANALYZE on a table.
    ExecuteAnalyze(ctx context.Context, schema, table string) error

    // ExecuteReindex runs REINDEX on a table or index.
    ExecuteReindex(ctx context.Context, schema, name string, isIndex bool) error

    // CancelOperation cancels a running maintenance operation by PID.
    // Returns true if cancellation signal was sent successfully.
    CancelOperation(ctx context.Context, pid int) (bool, error)

    // GetVacuumProgress returns current progress for a VACUUM operation.
    // Returns nil if no VACUUM is in progress for the given table.
    GetVacuumProgress(ctx context.Context, schema, table string) (*OperationProgress, error)

    // GetVacuumFullProgress returns current progress for a VACUUM FULL operation.
    // Uses pg_stat_progress_cluster since VACUUM FULL rewrites the table.
    GetVacuumFullProgress(ctx context.Context, schema, table string) (*OperationProgress, error)

    // GetRunningOperations returns all maintenance operations currently running
    // in the connected database.
    GetRunningOperations(ctx context.Context) ([]RunningOperation, error)
}

// VacuumOptions configures VACUUM behavior.
type VacuumOptions struct {
    Full    bool // Use VACUUM FULL (exclusive lock, returns space to OS)
    Analyze bool // Also run ANALYZE after VACUUM
    Verbose bool // Emit detailed logging
}

// RunningOperation represents a maintenance operation currently executing.
type RunningOperation struct {
    PID         int
    Database    string
    Schema      string
    Table       string
    Operation   string // "VACUUM", "VACUUM FULL", "ANALYZE", etc.
    Phase       string
    ProgressPct float64
    StartedAt   time.Time
}
```

## SQL Queries

### Execute VACUUM

```sql
-- Plain VACUUM
VACUUM "schema_name"."table_name";

-- VACUUM FULL
VACUUM FULL "schema_name"."table_name";

-- VACUUM ANALYZE
VACUUM ANALYZE "schema_name"."table_name";

-- VACUUM FULL ANALYZE
VACUUM (FULL, ANALYZE) "schema_name"."table_name";
```

### Execute ANALYZE

```sql
ANALYZE "schema_name"."table_name";
```

### Execute REINDEX

```sql
-- Reindex single index
REINDEX INDEX "schema_name"."index_name";

-- Reindex all indexes on table
REINDEX TABLE "schema_name"."table_name";
```

### Cancel Operation

```sql
SELECT pg_cancel_backend($1);  -- $1 = backend PID
```

### Get VACUUM Progress

```sql
SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    indexes_total,
    indexes_processed,
    ROUND(100.0 * heap_blks_scanned / NULLIF(heap_blks_total, 0), 2) AS progress_pct
FROM pg_stat_progress_vacuum
WHERE relid = $1::regclass;  -- $1 = 'schema.table'
```

### Get VACUUM FULL Progress

```sql
SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    command,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    ROUND(100.0 * heap_blks_scanned / NULLIF(heap_blks_total, 0), 2) AS progress_pct
FROM pg_stat_progress_cluster
WHERE relid = $1::regclass;
```

### Get Running Operations

```sql
SELECT
    a.pid,
    a.datname,
    n.nspname AS schema_name,
    c.relname AS table_name,
    CASE
        WHEN pv.pid IS NOT NULL THEN 'VACUUM'
        WHEN pc.pid IS NOT NULL THEN
            CASE pc.command
                WHEN 'VACUUM FULL' THEN 'VACUUM FULL'
                ELSE pc.command
            END
        ELSE 'ANALYZE'
    END AS operation,
    COALESCE(pv.phase, pc.phase, 'running') AS phase,
    COALESCE(
        ROUND(100.0 * pv.heap_blks_scanned / NULLIF(pv.heap_blks_total, 0), 2),
        ROUND(100.0 * pc.heap_blks_scanned / NULLIF(pc.heap_blks_total, 0), 2),
        0
    ) AS progress_pct,
    a.backend_start
FROM pg_stat_activity a
LEFT JOIN pg_stat_progress_vacuum pv ON pv.pid = a.pid
LEFT JOIN pg_stat_progress_cluster pc ON pc.pid = a.pid
LEFT JOIN pg_class c ON c.oid = COALESCE(pv.relid, pc.relid)
LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE a.query ~* '^(VACUUM|ANALYZE|REINDEX)'
  AND a.state = 'active';
```

## Error Handling

| Error | Condition | User Message |
|-------|-----------|--------------|
| `ErrReadOnlyMode` | Operation attempted in read-only mode | "Operation blocked: application is in read-only mode" |
| `ErrOperationInProgress` | Another operation is already running | "Another maintenance operation is in progress. Wait for it to complete or cancel it." |
| `ErrInsufficientPrivileges` | User lacks required privileges | "Insufficient privileges. Required: {privilege} on {schema}.{table}" |
| `ErrTableNotFound` | Target table doesn't exist | "Table {schema}.{table} not found" |
| `ErrCancellationFailed` | pg_cancel_backend returned false | "Failed to cancel operation (PID {pid}). Process may have already completed." |
| `ErrConnectionLost` | Database connection lost during operation | "Database connection lost. Operation may still be running server-side." |
