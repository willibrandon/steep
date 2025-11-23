# Database Query Contracts: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## Query Contracts

### GetLocks

**Purpose**: Retrieve all active locks with associated process information

**Input Parameters**:
- `ctx context.Context` - Context with timeout
- `pool *pgxpool.Pool` - Database connection pool
- `limit int` - Maximum number of locks to return (default: 200)

**Output**: `([]Lock, error)`

**SQL**:
```sql
SELECT
    l.pid,
    COALESCE(a.usename, '') AS usename,
    COALESCE(a.datname, '') AS datname,
    l.locktype,
    l.mode,
    l.granted,
    COALESCE(c.relname, l.relation::text, '') AS relation,
    COALESCE(TRIM(regexp_replace(a.query, '\s+', ' ', 'g')), '') AS query,
    COALESCE(a.state, '') AS state,
    COALESCE(EXTRACT(EPOCH FROM age(clock_timestamp(), a.query_start))::int, 0) AS duration_seconds,
    COALESCE(a.wait_event_type, '') AS wait_event_type,
    COALESCE(a.wait_event, '') AS wait_event
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
LEFT JOIN pg_class c ON c.oid = l.relation
WHERE a.pid != pg_backend_pid()
ORDER BY l.granted ASC, a.query_start ASC NULLS LAST
LIMIT $1;
```

**Performance Requirements**:
- Execution time < 500ms with 100+ locks
- Result set bounded by limit parameter

---

### GetBlockingRelationships

**Purpose**: Identify all blocking relationships (which PIDs are blocking which)

**Input Parameters**:
- `ctx context.Context` - Context with timeout
- `pool *pgxpool.Pool` - Database connection pool

**Output**: `([]BlockingRelationship, error)`

**SQL**:
```sql
SELECT
    blocked.pid AS blocked_pid,
    COALESCE(blocked.usename, '') AS blocked_user,
    COALESCE(TRIM(regexp_replace(blocked.query, '\s+', ' ', 'g')), '') AS blocked_query,
    COALESCE(EXTRACT(EPOCH FROM age(clock_timestamp(), blocked.query_start))::int, 0) AS blocked_duration_seconds,
    blocking.pid AS blocking_pid,
    COALESCE(blocking.usename, '') AS blocking_user,
    COALESCE(TRIM(regexp_replace(blocking.query, '\s+', ' ', 'g')), '') AS blocking_query
FROM pg_stat_activity AS blocked
JOIN pg_stat_activity AS blocking
    ON blocking.pid = ANY(pg_blocking_pids(blocked.pid))
WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0;
```

**Performance Requirements**:
- Execution time < 500ms
- Uses pg_blocking_pids() for efficiency (requires PostgreSQL 9.6+)

---

### TerminateBackend

**Purpose**: Kill a blocking query by terminating its backend process

**Input Parameters**:
- `ctx context.Context` - Context with timeout
- `pool *pgxpool.Pool` - Database connection pool
- `pid int` - Process ID to terminate

**Output**: `(bool, error)` - True if terminated successfully

**SQL**:
```sql
SELECT pg_terminate_backend($1);
```

**Prerequisites**:
- User must have pg_signal_backend role or superuser privileges
- Application must NOT be in readonly mode

**Error Handling**:
- Returns false if PID doesn't exist (already terminated)
- Returns error if permission denied

---

### GetLockCount

**Purpose**: Get aggregate lock count per PID for dashboard integration

**Input Parameters**:
- `ctx context.Context` - Context with timeout
- `pool *pgxpool.Pool` - Database connection pool

**Output**: `(map[int]int, error)` - Map of PID to lock count

**SQL**:
```sql
SELECT pid, count(*) AS lock_count
FROM pg_locks
WHERE relation IS NOT NULL
GROUP BY pid;
```

## Message Types

### LocksUpdateMsg

**Purpose**: Deliver lock data from monitor goroutine to UI

```go
type LocksUpdateMsg struct {
    Locks         []Lock
    Blocking      []BlockingRelationship
    BlockingPIDs  map[int]bool  // Set of PIDs that are blocking others
    BlockedPIDs   map[int]bool  // Set of PIDs that are blocked
    Error         error
}
```

### KillQueryResultMsg

**Purpose**: Deliver result of kill query action

```go
type KillQueryResultMsg struct {
    PID       int
    Success   bool
    Error     error
}
```

## Monitor Interface

### LocksMonitor

**Initialization**:
```go
func NewLocksMonitor(pool *pgxpool.Pool, interval time.Duration) *LocksMonitor
```

**Start**:
```go
func (m *LocksMonitor) Start(ctx context.Context) <-chan LocksUpdateMsg
```

**Behavior**:
- Queries database every `interval` (default: 2 seconds)
- Sends LocksUpdateMsg through returned channel
- Builds blocking chain hierarchy from BlockingRelationship data
- Stops when context is cancelled
