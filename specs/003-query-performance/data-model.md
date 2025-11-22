# Data Model: Query Performance Monitoring

**Feature**: 003-query-performance
**Date**: 2025-11-21

## Entity Overview

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   QueryStats    │     │   DataSource    │     │  ExplainResult  │
│─────────────────│     │─────────────────│     │─────────────────│
│ fingerprint (PK)│     │ type            │     │ fingerprint (FK)│
│ normalized_query│     │ status          │     │ plan_json       │
│ calls           │     │ last_error      │     │ fetched_at      │
│ total_time_ms   │     │ config          │     └─────────────────┘
│ min_time_ms     │
│ max_time_ms     │
│ total_rows      │
│ first_seen      │
│ last_seen       │
└─────────────────┘
```

## Entities

### QueryStats

Primary entity for aggregated query statistics.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| fingerprint | uint64 | PRIMARY KEY | pg_query fingerprint hash |
| normalized_query | string | NOT NULL, max 10KB | Normalized SQL with placeholders |
| calls | int64 | DEFAULT 0 | Total execution count |
| total_time_ms | float64 | DEFAULT 0 | Sum of all execution times |
| min_time_ms | float64 | NULLABLE | Minimum execution time observed |
| max_time_ms | float64 | NULLABLE | Maximum execution time observed |
| total_rows | int64 | DEFAULT 0 | Sum of all rows returned |
| first_seen | timestamp | DEFAULT NOW | When query first appeared |
| last_seen | timestamp | DEFAULT NOW | When query last executed |

**Computed Fields** (not stored):
- `mean_time_ms` = total_time_ms / calls

**Validation Rules**:
- fingerprint must be non-zero
- normalized_query must not be empty
- calls >= 0
- total_time_ms >= 0
- min_time_ms <= max_time_ms (when both non-null)

**Lifecycle**:
1. Created when new fingerprint detected
2. Updated (increment calls, update times/rows) on each query event
3. Deleted when last_seen > 7 days old

---

### DataSource

Runtime state for data collection method (not persisted).

| Field | Type | Values | Description |
|-------|------|--------|-------------|
| type | enum | log_parsing, activity_sampling | Active collection method |
| status | enum | active, degraded, unavailable | Current health state |
| last_error | string | - | Most recent error message |
| config | struct | - | Source-specific configuration |

**LogParsingConfig**:
- log_file_path: string
- log_line_prefix: string
- last_position: int64 (file offset)

**SamplingConfig**:
- poll_interval: duration
- last_poll: timestamp

**State Transitions**:
```
unavailable → active: Log file accessible or sampling started
active → degraded: Errors but still collecting some data
degraded → unavailable: Complete collection failure
active/degraded → active: Recovery from errors
```

---

### ExplainResult

Cached EXPLAIN plan output (optional, in-memory cache).

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| fingerprint | uint64 | FOREIGN KEY | Reference to QueryStats |
| plan_json | string | NOT NULL | EXPLAIN (FORMAT JSON) output |
| fetched_at | timestamp | NOT NULL | When plan was retrieved |

**Cache Policy**:
- TTL: 5 minutes (plans can change with data)
- Max entries: 100 (LRU eviction)
- Not persisted to SQLite

---

## SQLite Schema

```sql
-- Main statistics table
CREATE TABLE IF NOT EXISTS query_stats (
    fingerprint       INTEGER PRIMARY KEY,
    normalized_query  TEXT NOT NULL,
    calls             INTEGER NOT NULL DEFAULT 0,
    total_time_ms     REAL NOT NULL DEFAULT 0,
    min_time_ms       REAL,
    max_time_ms       REAL,
    total_rows        INTEGER NOT NULL DEFAULT 0,
    first_seen        TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen         TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Indexes for top-N queries by different metrics
CREATE INDEX IF NOT EXISTS idx_query_stats_total_time
    ON query_stats(total_time_ms DESC);
CREATE INDEX IF NOT EXISTS idx_query_stats_calls
    ON query_stats(calls DESC);
CREATE INDEX IF NOT EXISTS idx_query_stats_total_rows
    ON query_stats(total_rows DESC);
CREATE INDEX IF NOT EXISTS idx_query_stats_last_seen
    ON query_stats(last_seen);

-- Full-text search for query text (optional, if FTS5 available)
-- CREATE VIRTUAL TABLE IF NOT EXISTS query_stats_fts
--     USING fts5(normalized_query, content='query_stats', content_rowid='fingerprint');
```

## Go Structs

```go
// QueryStats represents aggregated statistics for a normalized query pattern
type QueryStats struct {
    Fingerprint      uint64    `db:"fingerprint"`
    NormalizedQuery  string    `db:"normalized_query"`
    Calls            int64     `db:"calls"`
    TotalTimeMs      float64   `db:"total_time_ms"`
    MinTimeMs        *float64  `db:"min_time_ms"`
    MaxTimeMs        *float64  `db:"max_time_ms"`
    TotalRows        int64     `db:"total_rows"`
    FirstSeen        time.Time `db:"first_seen"`
    LastSeen         time.Time `db:"last_seen"`
}

// MeanTimeMs returns average execution time
func (q *QueryStats) MeanTimeMs() float64 {
    if q.Calls == 0 {
        return 0
    }
    return q.TotalTimeMs / float64(q.Calls)
}

// DataSourceType indicates the collection method
type DataSourceType int

const (
    DataSourceLogParsing DataSourceType = iota
    DataSourceActivitySampling
)

// DataSourceStatus indicates collection health
type DataSourceStatus int

const (
    DataSourceActive DataSourceStatus = iota
    DataSourceDegraded
    DataSourceUnavailable
)

// DataSource represents runtime collection state
type DataSource struct {
    Type      DataSourceType
    Status    DataSourceStatus
    LastError string
    Config    interface{} // LogParsingConfig or SamplingConfig
}

// QueryEvent represents a single query execution from log or sample
type QueryEvent struct {
    Query      string
    DurationMs float64
    Rows       int64
    Timestamp  time.Time
    Database   string
    User       string
}
```

## Query Patterns

### Insert/Update Statistics (Upsert)

```sql
INSERT INTO query_stats (fingerprint, normalized_query, calls, total_time_ms, min_time_ms, max_time_ms, total_rows, last_seen)
VALUES (?, ?, 1, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(fingerprint) DO UPDATE SET
    calls = calls + 1,
    total_time_ms = total_time_ms + excluded.total_time_ms,
    min_time_ms = MIN(COALESCE(min_time_ms, excluded.min_time_ms), excluded.min_time_ms),
    max_time_ms = MAX(COALESCE(max_time_ms, excluded.max_time_ms), excluded.max_time_ms),
    total_rows = total_rows + excluded.total_rows,
    last_seen = datetime('now');
```

### Top Queries by Time

```sql
SELECT fingerprint, normalized_query, calls, total_time_ms,
       total_time_ms/calls as mean_time_ms, total_rows
FROM query_stats
ORDER BY total_time_ms DESC
LIMIT 50;
```

### Top Queries by Calls

```sql
SELECT fingerprint, normalized_query, calls, total_time_ms,
       total_time_ms/calls as mean_time_ms, total_rows
FROM query_stats
ORDER BY calls DESC
LIMIT 50;
```

### Top Queries by Rows

```sql
SELECT fingerprint, normalized_query, calls, total_time_ms,
       total_time_ms/calls as mean_time_ms, total_rows
FROM query_stats
ORDER BY total_rows DESC
LIMIT 50;
```

### Search by Pattern

```sql
SELECT fingerprint, normalized_query, calls, total_time_ms,
       total_time_ms/calls as mean_time_ms, total_rows
FROM query_stats
WHERE normalized_query REGEXP ?
ORDER BY total_time_ms DESC
LIMIT 50;
```

### Cleanup Old Records

```sql
DELETE FROM query_stats
WHERE last_seen < datetime('now', '-7 days');
```

### Reset Statistics

```sql
DELETE FROM query_stats;
```

## Relationships

- **QueryStats ↔ ExplainResult**: One-to-one (optional). EXPLAIN fetched on-demand and cached in memory.
- **DataSource → QueryStats**: One-to-many. DataSource produces QueryEvents that update QueryStats.

## Invariants

1. Sum of all calls must equal total query events processed (minus unparseable)
2. For any QueryStats: min_time_ms <= mean_time_ms <= max_time_ms
3. first_seen <= last_seen
4. No duplicate fingerprints in query_stats table
5. Cleanup runs before any query to ensure 7-day window
