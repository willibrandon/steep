# Research: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## 1. PostgreSQL Lock Query Patterns

### Decision: Use `pg_blocking_pids()` function as primary approach

**Rationale**: Available in PostgreSQL 9.6+ (well within our PG 11+ target), more efficient than complex self-joins on pg_locks, and directly returns blocking PIDs.

**Alternatives considered**:
- Full self-join on pg_locks: More complex, slower, but provides detailed lock type information
- Will use hybrid approach: `pg_blocking_pids()` for blocking detection + separate lock details query

### Primary Query: Blocking Relationships

```sql
SELECT
    blocked.pid AS blocked_pid,
    blocked.usename AS blocked_user,
    blocked.datname AS database,
    blocked.query AS blocked_query,
    blocked.state AS blocked_state,
    blocked.wait_event_type,
    blocked.wait_event,
    age(clock_timestamp(), blocked.query_start) AS blocked_duration,
    blocking.pid AS blocking_pid,
    blocking.usename AS blocking_user,
    blocking.query AS blocking_query,
    blocking.state AS blocking_state
FROM pg_stat_activity AS blocked
JOIN pg_stat_activity AS blocking
    ON blocking.pid = ANY(pg_blocking_pids(blocked.pid))
WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0;
```

### Secondary Query: All Locks with Details

```sql
SELECT
    l.pid,
    a.usename,
    a.datname AS database,
    l.locktype,
    l.mode,
    l.granted,
    COALESCE(c.relname, l.relation::text, '') AS relation,
    a.query,
    a.state,
    age(clock_timestamp(), a.query_start) AS duration
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
LEFT JOIN pg_class c ON c.oid = l.relation
WHERE a.pid != pg_backend_pid()
ORDER BY
    l.granted ASC,  -- Waiting locks first
    a.query_start ASC
LIMIT 200;
```

### Lock Types in PostgreSQL

| Lock Type | Description | Common Scenario |
|-----------|-------------|-----------------|
| relation | Tables, indexes, sequences | Most common - table-level locks |
| transactionid | Transaction ID | Row-level conflict detection |
| virtualxid | Virtual transaction ID | Session-level |
| tuple | Specific row | Direct tuple lock |
| advisory | Application-level locks | User-defined synchronization |

### Lock Modes (Least to Most Restrictive)

1. **AccessShareLock** - SELECT
2. **RowShareLock** - SELECT FOR UPDATE/SHARE
3. **RowExclusiveLock** - INSERT/UPDATE/DELETE
4. **ShareUpdateExclusiveLock** - VACUUM, ANALYZE
5. **ShareLock** - CREATE INDEX
6. **ShareRowExclusiveLock** - Rarely used
7. **ExclusiveLock** - REFRESH MATERIALIZED VIEW CONCURRENTLY
8. **AccessExclusiveLock** - DROP, TRUNCATE, ALTER TABLE

### Performance Considerations

- **Statement timeout**: Set to 2s to match refresh interval
- **Result limits**: Cap at 200 locks max to prevent runaway queries
- **Refresh interval**: 2 seconds (matches FR-007)
- **Note**: `query` field shows last executed query, not necessarily the lock-holding query

## 2. ASCII Tree Rendering with treeprint

### Decision: Use github.com/xlab/treeprint

**Rationale**: User explicitly specified this library in the feature description. Simple API, produces clean ASCII output.

**Usage Pattern**:

```go
import "github.com/xlab/treeprint"

func RenderLockTree(blockers []BlockingChain) string {
    tree := treeprint.NewWithRoot("Lock Dependencies")

    for _, blocker := range blockers {
        meta := fmt.Sprintf("PID:%d %s", blocker.BlockerPID, blocker.LockMode)
        branch := tree.AddMetaBranch(meta, truncateQuery(blocker.Query, 50))

        for _, blocked := range blocker.Blocked {
            addChainToTree(branch, blocked)
        }
    }

    return tree.String()
}

func addChainToTree(parent treeprint.Tree, chain BlockingChain) {
    meta := fmt.Sprintf("PID:%d waiting", chain.BlockerPID)
    branch := parent.AddMetaBranch(meta, truncateQuery(chain.Query, 50))

    for _, blocked := range chain.Blocked {
        addChainToTree(branch, blocked)
    }
}
```

**Output Example**:
```
Lock Dependencies
├── [PID:1234 AccessExclusiveLock] ALTER TABLE users ADD COLUMN...
│   ├── [PID:5678 waiting] SELECT * FROM users WHERE id = ...
│   └── [PID:9012 waiting] UPDATE users SET name = 'foo' W...
```

## 3. Visual Design Research

### Reference Tools Studied

Per Constitution Principle VI, the following tools should be studied before implementation:

1. **pg_top** (`/Users/brandon/src/pg_top`) - PostgreSQL activity monitor
   - Displays connections in tabular format with state indicators
   - Has lock count column in activity display
   - Simple color coding for different states

2. **htop** (`/Users/brandon/src/htop`) - Process viewer
   - Excellent use of color to distinguish states
   - Compact table layout with aligned columns
   - Tree view for process hierarchy (similar to our lock tree)

3. **k9s** (`/Users/brandon/src/k9s`) - Kubernetes TUI
   - Color coding: red for errors/blocked, yellow for warnings
   - Help overlay accessible via `?`
   - Confirmation dialogs for destructive actions

### Visual Design Deliverables (Required Before Implementation)

1. **ASCII Mockup**: Character-by-character layout showing:
   - Lock table with columns: PID | Type | Mode | Granted | DB | Relation | Query
   - Color indicators: Red rows for blocked, Yellow for blocking
   - Lock dependency tree below table

2. **Three Demo Prototypes** testing:
   - Demo 1: Simple table with lipgloss borders
   - Demo 2: Split view (table top, tree bottom)
   - Demo 3: Tabbed interface (table tab, tree tab)

3. **Visual Acceptance Criteria**:
   - Blocked queries MUST be rendered in red foreground
   - Blocking queries MUST be rendered in yellow foreground
   - Tree MUST be readable at 80 columns width
   - Table columns MUST align without overflow

## 4. Kill Query Implementation

### Decision: Use pg_terminate_backend()

```sql
SELECT pg_terminate_backend($1);
```

**Safety measures**:
- Confirmation dialog required (FR-009)
- Respect `--readonly` mode (FR-010)
- Check for required privileges before enabling action
- Handle "query already terminated" gracefully

### Required Privileges

- `pg_signal_backend` role (PostgreSQL 9.6+), OR
- Superuser privileges

## 5. View Navigation Integration

### Decision: Follow existing ViewType pattern

ViewLocks is already defined in `internal/ui/views/types.go:12`. The `5` key binding needs to be added to the main app to switch to this view.

### Keyboard Bindings

| Key | Action | Notes |
|-----|--------|-------|
| 5 | Switch to Locks view | Navigate from any view |
| s | Sort by column | Cycle through sort options |
| d | View detail | Show full query in modal |
| x | Kill query | Requires confirmation, respects readonly |
| j/k | Navigate rows | vim-style |
| ? | Show help | Help overlay |

## 6. Technology Stack Summary

### New Dependencies

- `github.com/xlab/treeprint` - ASCII tree rendering

### Existing Dependencies Used

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - Table, viewport components
- `github.com/charmbracelet/lipgloss` - Styling
- `github.com/jackc/pgx/v5` - PostgreSQL driver

### Files to Create

```
internal/db/models/lock.go           - Lock, BlockingRelationship types
internal/db/queries/locks.go         - GetLocks(), GetBlockingRelationships()
internal/monitors/locks.go           - LocksMonitor goroutine
internal/ui/components/lock_tree.go  - RenderLockTree()
internal/ui/views/locks/view.go      - LocksView implementing ViewModel
internal/ui/views/locks/help.go      - Help text
internal/ui/views/locks/detail.go    - Query detail modal
```

## Sources

- [PostgreSQL Wiki - Lock Monitoring](https://wiki.postgresql.org/wiki/Lock_Monitoring)
- [Crunchy Data - Lock Source Detection](https://www.crunchydata.com/blog/one-pid-to-lock-them-all-finding-the-source-of-the-lock-in-postgres)
- [pganalyze - Postgres Lock Monitoring](https://pganalyze.com/blog/postgres-lock-monitoring)
- [xlab/treeprint GitHub](https://github.com/xlab/treeprint)
