# Vacuum Status Contract

**Package**: `internal/db/queries`

This contract defines the interface for querying vacuum and analyze status.

## Interface Definition

```go
// VacuumStatusProvider provides methods for querying vacuum/analyze status.
type VacuumStatusProvider interface {
    // GetTablesWithVacuumStatus returns tables with vacuum/analyze timestamps.
    // This extends the existing GetTablesWithStats query.
    GetTablesWithVacuumStatus(ctx context.Context) ([]TableVacuumStatus, error)

    // GetTableVacuumDetails returns detailed vacuum info for a specific table.
    GetTableVacuumDetails(ctx context.Context, tableOID uint32) (*TableVacuumDetails, error)

    // IsAutovacuumEnabled checks if autovacuum is enabled for a table.
    IsAutovacuumEnabled(ctx context.Context, tableOID uint32) (bool, error)
}

// TableVacuumStatus contains vacuum/analyze timestamps for a table.
type TableVacuumStatus struct {
    TableOID         uint32
    SchemaName       string
    TableName        string
    LastVacuum       *time.Time
    LastAutovacuum   *time.Time
    LastAnalyze      *time.Time
    LastAutoanalyze  *time.Time
    VacuumCount      int64
    AutovacuumCount  int64
    AnalyzeCount     int64
    AutoanalyzeCount int64
    DeadTuples       int64
    ModsSinceAnalyze int64
}

// TableVacuumDetails contains extended vacuum information.
type TableVacuumDetails struct {
    TableVacuumStatus
    AutovacuumEnabled  bool
    VacuumThreshold    int64  // autovacuum_vacuum_threshold + scale_factor * n_live_tup
    AnalyzeThreshold   int64  // autovacuum_analyze_threshold + scale_factor * n_live_tup
    NeedsVacuum        bool   // Dead tuples exceed threshold
    NeedsAnalyze       bool   // Modifications exceed threshold
}
```

## SQL Queries

### Get Tables with Vacuum Status

This extends the existing `GetTablesWithStats` query:

```sql
SELECT
    t.oid as table_oid,
    nsp.nspname as schema_name,
    t.relname as table_name,
    -- Existing columns...
    s.last_vacuum,
    s.last_autovacuum,
    s.last_analyze,
    s.last_autoanalyze,
    COALESCE(s.vacuum_count, 0) as vacuum_count,
    COALESCE(s.autovacuum_count, 0) as autovacuum_count,
    COALESCE(s.analyze_count, 0) as analyze_count,
    COALESCE(s.autoanalyze_count, 0) as autoanalyze_count,
    COALESCE(s.n_dead_tup, 0) as dead_tuples,
    COALESCE(s.n_mod_since_analyze, 0) as mods_since_analyze
FROM pg_class t
JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
LEFT JOIN pg_stat_all_tables s ON s.relid = t.oid
WHERE t.relkind IN ('r', 'p')
ORDER BY nsp.nspname, t.relname;
```

### Check Autovacuum Status for Table

```sql
SELECT
    COALESCE(
        (SELECT (string_to_array(unnest, '='))[2]::boolean
         FROM unnest(c.reloptions)
         WHERE unnest LIKE 'autovacuum_enabled=%'),
        true  -- Default: enabled
    ) AS autovacuum_enabled
FROM pg_class c
WHERE c.oid = $1;
```

### Calculate Vacuum/Analyze Thresholds

Per PostgreSQL autovacuum documentation:

```sql
-- Get autovacuum settings and calculate thresholds
WITH settings AS (
    SELECT
        current_setting('autovacuum_vacuum_threshold')::int AS vacuum_threshold,
        current_setting('autovacuum_vacuum_scale_factor')::float AS vacuum_scale_factor,
        current_setting('autovacuum_analyze_threshold')::int AS analyze_threshold,
        current_setting('autovacuum_analyze_scale_factor')::float AS analyze_scale_factor
)
SELECT
    t.oid,
    s.n_live_tup,
    s.n_dead_tup,
    s.n_mod_since_analyze,
    -- Vacuum threshold = base + scale_factor * live_tuples
    (settings.vacuum_threshold + settings.vacuum_scale_factor * s.n_live_tup)::bigint AS vacuum_threshold,
    -- Analyze threshold = base + scale_factor * live_tuples
    (settings.analyze_threshold + settings.analyze_scale_factor * s.n_live_tup)::bigint AS analyze_threshold,
    -- Needs vacuum?
    s.n_dead_tup > (settings.vacuum_threshold + settings.vacuum_scale_factor * s.n_live_tup) AS needs_vacuum,
    -- Needs analyze?
    s.n_mod_since_analyze > (settings.analyze_threshold + settings.analyze_scale_factor * s.n_live_tup) AS needs_analyze
FROM pg_class t
JOIN pg_stat_all_tables s ON s.relid = t.oid
CROSS JOIN settings
WHERE t.oid = $1;
```

## Stale Vacuum Detection

Configuration-driven detection of tables needing attention:

```go
// StaleVacuumConfig defines thresholds for vacuum status indicators.
type StaleVacuumConfig struct {
    // StaleThreshold is the duration after which vacuum is considered stale.
    // Default: 7 days (per clarification session).
    StaleThreshold time.Duration

    // WarningThreshold is the duration for showing warning (yellow) indicator.
    // Default: 3 days.
    WarningThreshold time.Duration
}

// GetVacuumStatusIndicator returns the visual indicator status for a table.
func GetVacuumStatusIndicator(status TableVacuumStatus, config StaleVacuumConfig) VacuumIndicator {
    lastMaintenance := maxTime(status.LastVacuum, status.LastAutovacuum)
    if lastMaintenance == nil {
        return VacuumIndicatorCritical // Never vacuumed
    }

    age := time.Since(*lastMaintenance)
    switch {
    case age > config.StaleThreshold:
        return VacuumIndicatorCritical // Red: overdue
    case age > config.WarningThreshold:
        return VacuumIndicatorWarning // Yellow: approaching threshold
    default:
        return VacuumIndicatorOK // Green/normal: recent
    }
}

type VacuumIndicator int
const (
    VacuumIndicatorOK VacuumIndicator = iota
    VacuumIndicatorWarning
    VacuumIndicatorCritical
)
```

## Display Format

Timestamp display in Tables view:

```go
// FormatVacuumTimestamp formats a vacuum timestamp for display.
// Returns "never" for nil, relative time for recent, date for old.
func FormatVacuumTimestamp(t *time.Time) string {
    if t == nil {
        return "never"
    }

    age := time.Since(*t)
    switch {
    case age < time.Hour:
        return fmt.Sprintf("%dm ago", int(age.Minutes()))
    case age < 24*time.Hour:
        return fmt.Sprintf("%dh ago", int(age.Hours()))
    case age < 7*24*time.Hour:
        return fmt.Sprintf("%dd ago", int(age.Hours()/24))
    default:
        return t.Format("Jan 02")
    }
}
```
