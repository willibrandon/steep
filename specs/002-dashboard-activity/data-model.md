# Data Model: Dashboard & Activity Monitoring

**Feature**: 002-dashboard-activity
**Date**: 2025-11-21

## Entities

### Connection

Represents a PostgreSQL backend process from pg_stat_activity.

| Field | Type | Description | Constraints |
|-------|------|-------------|-------------|
| PID | int | Process ID | Primary identifier, > 0 |
| User | string | Database username | Not empty |
| Database | string | Database name | Not empty |
| State | ConnectionState | Current connection state | Enum value |
| DurationSeconds | int | Seconds since query started | >= 0, null if idle |
| Query | string | Current/last query text | Truncated to 500 chars in table, full in detail |
| ClientAddr | string | Client IP address | May be empty for local connections |
| ApplicationName | string | Application identifier | May be empty |
| WaitEventType | string | Wait event category | May be empty |
| WaitEvent | string | Specific wait event | May be empty |

**State Transitions**:
- idle -> active (query starts)
- active -> idle (query completes)
- active -> idle in transaction (transaction opened)
- idle in transaction -> idle (transaction commits/rolls back)
- idle in transaction -> idle in transaction (aborted) (error occurs)
- Any state -> disconnected (connection terminates)

### ConnectionState (Enum)

| Value | Display | Color |
|-------|---------|-------|
| active | active | Green |
| idle | idle | Gray |
| idle_in_transaction | idle in transaction | Yellow |
| idle_in_transaction_aborted | idle in transaction (aborted) | Orange |
| fastpath | fastpath function call | Blue |
| disabled | disabled | Red |

### Metrics

Represents dashboard metrics from pg_stat_database.

| Field | Type | Description | Constraints |
|-------|------|-------------|-------------|
| TPS | float64 | Transactions per second | >= 0, calculated from delta |
| CacheHitRatio | float64 | Buffer cache hit percentage | 0-100 |
| ConnectionCount | int | Total active connections | >= 0 |
| DatabaseSize | int64 | Database size in bytes | >= 0 |
| Timestamp | time.Time | When metrics were captured | Not null |

**Derived Fields**:
- CacheHitRatio = blks_hit / (blks_hit + blks_read) * 100
- TPS = (xact_commit + xact_rollback) delta / interval

### MetricsSnapshot

Internal type for calculating deltas.

| Field | Type | Description |
|-------|------|-------------|
| TotalXacts | int64 | xact_commit + xact_rollback |
| BlksHit | int64 | Blocks found in cache |
| BlksRead | int64 | Blocks read from disk |
| Timestamp | time.Time | Snapshot time |

### DashboardPanel

UI entity for metric display.

| Field | Type | Description |
|-------|------|-------------|
| Label | string | Panel title (e.g., "TPS") |
| Value | string | Formatted value (e.g., "1,234") |
| Unit | string | Optional unit (e.g., "req/s", "%") |
| Status | PanelStatus | Normal/Warning/Critical |

### PanelStatus (Enum)

| Value | Threshold | Color |
|-------|-----------|-------|
| Normal | Default | Default |
| Warning | CacheHitRatio < 90% | Yellow |
| Critical | CacheHitRatio < 80% | Red |

### ActivityFilter

User-applied filters for the activity table.

| Field | Type | Description |
|-------|------|-------------|
| StateFilter | string | Filter by connection state |
| DatabaseFilter | string | Filter by database name |
| QueryFilter | string | Search in query text |
| ShowAllDatabases | bool | Show all vs current DB only |

### Pagination

State for paginated results.

| Field | Type | Description |
|-------|------|-------------|
| Limit | int | Rows per page (default 500) |
| Offset | int | Current offset |
| TotalCount | int | Total matching rows |
| HasMore | bool | More pages available |

## Relationships

```
Metrics 1--* MetricsSnapshot : calculated from
DashboardPanel *--1 Metrics : displays
Connection *--1 ActivityFilter : filtered by
Connection *--1 Pagination : paginated by
```

## Validation Rules

### Connection
- PID must be positive integer
- State must be valid ConnectionState enum value
- DurationSeconds >= 0 when not null
- Query truncated to 500 chars for table display

### Metrics
- TPS >= 0 (negative indicates counter reset, handle gracefully)
- CacheHitRatio between 0 and 100
- ConnectionCount >= 0
- DatabaseSize >= 0

### ActivityFilter
- StateFilter must be valid state or empty
- Limit must be between 1 and 1000
- Offset must be >= 0

## SQL Mappings

### Connection from pg_stat_activity
```sql
pid -> PID
usename -> User
datname -> Database
state -> State (map string to enum)
EXTRACT(EPOCH FROM (now() - query_start))::int -> DurationSeconds
query -> Query
client_addr::text -> ClientAddr
application_name -> ApplicationName
wait_event_type -> WaitEventType
wait_event -> WaitEvent
```

### Metrics from pg_stat_database
```sql
sum(xact_commit + xact_rollback) -> TotalXacts (for delta)
sum(blks_hit) -> BlksHit
sum(blks_read) -> BlksRead
pg_database_size(current_database()) -> DatabaseSize
```
