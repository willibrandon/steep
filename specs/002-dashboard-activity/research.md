# Research: Dashboard & Activity Monitoring

**Feature**: 002-dashboard-activity
**Date**: 2025-11-21

## Research Tasks

### 1. Bubbletea Table Component Best Practices

**Decision**: Use `bubbles/table` component with custom styling for the Activity table

**Rationale**:
- Built-in support for sorting, selection, and keyboard navigation
- Integrates well with Lipgloss styling system
- Handles virtualized rendering for large datasets efficiently
- Already used in reference tools like k9s

**Alternatives Considered**:
- Custom table implementation: More control but significant effort, reinventing the wheel
- Third-party table libraries: Additional dependencies, may not integrate well with Bubbletea message pattern

**Implementation Notes**:
- Use `table.WithColumns()` for fixed column definitions
- Implement custom `table.Styles` for state color-coding
- Handle `table.KeyMap` customization for vim-like bindings

### 2. Real-Time Refresh Pattern in Bubbletea

**Decision**: Use `tea.Tick` with configurable interval and channel-based monitor updates

**Rationale**:
- `tea.Tick` is the idiomatic Bubbletea pattern for periodic updates
- Separating data fetching (goroutine) from rendering (main loop) prevents UI blocking
- Channel-based updates allow graceful handling of slow queries

**Alternatives Considered**:
- Direct queries in Update(): Blocks UI during query execution
- Single goroutine for all monitors: Less isolation, harder to manage different refresh rates

**Implementation Pattern**:
```go
type tickMsg time.Time
type activityDataMsg []Connection
type metricsDataMsg Metrics

func (m model) Init() tea.Cmd {
    return tea.Batch(
        m.tickCmd(),
        m.fetchActivityCmd(),
        m.fetchMetricsCmd(),
    )
}

func (m model) tickCmd() tea.Cmd {
    return tea.Tick(m.refreshInterval, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}
```

### 3. PostgreSQL pg_stat_activity Query Optimization

**Decision**: Use prepared statement with LIMIT and indexed ORDER BY

**Rationale**:
- LIMIT 500 prevents unbounded result sets (Constitution III)
- ORDER BY backend_start DESC shows newest connections first (most relevant)
- Prepared statements reduce parsing overhead on repeated queries

**Query Design**:
```sql
SELECT
    pid,
    usename,
    datname,
    state,
    EXTRACT(EPOCH FROM (now() - query_start))::int AS duration_seconds,
    LEFT(query, 500) AS query_truncated,
    client_addr,
    application_name,
    wait_event_type,
    wait_event
FROM pg_stat_activity
WHERE pid != pg_backend_pid()
  AND ($1 = '' OR datname = $1)  -- Optional database filter
ORDER BY backend_start DESC
LIMIT $2 OFFSET $3
```

**Alternatives Considered**:
- No LIMIT: Violates Constitution III, could return thousands of rows
- No query truncation: Full query text could be megabytes

### 4. TPS Calculation from pg_stat_database

**Decision**: Calculate TPS as delta of xact_commit + xact_rollback between snapshots

**Rationale**:
- xact_commit and xact_rollback are cumulative counters
- Delta over interval gives accurate TPS
- Simple calculation, widely used in PostgreSQL monitoring tools

**Query Design**:
```sql
SELECT
    sum(xact_commit + xact_rollback) AS total_xacts,
    sum(blks_hit) AS blks_hit,
    sum(blks_read) AS blks_read,
    pg_database_size(current_database()) AS db_size
FROM pg_stat_database
WHERE datname = current_database()
```

**TPS Calculation**:
```go
tps := float64(currentXacts - previousXacts) / intervalSeconds
```

### 5. Connection State Color Mapping

**Decision**: Use ANSI 256-color palette for maximum terminal compatibility

**Rationale**:
- 256-color is supported by most modern terminals
- Provides sufficient palette for distinct state colors
- Falls back gracefully to basic colors if needed

**Color Mapping**:
| State | Color | ANSI Code | Rationale |
|-------|-------|-----------|-----------|
| active | Green | 82 | Actively executing, positive |
| idle | Gray | 243 | Dormant, low priority |
| idle in transaction | Yellow | 220 | Potential lock holder, warning |
| idle in transaction (aborted) | Orange | 208 | Error state, needs attention |
| fastpath function call | Blue | 75 | System operation |
| disabled | Red | 196 | Error condition |

### 6. Confirmation Dialog Pattern

**Decision**: Modal overlay dialog with y/n confirmation

**Rationale**:
- Destructive actions (cancel/terminate) need explicit confirmation
- Modal prevents accidental additional keypresses
- Follows convention of htop and similar tools

**Implementation Pattern**:
```go
type confirmDialog struct {
    visible     bool
    action      string // "cancel" or "terminate"
    targetPID   int
    targetQuery string
}

func (m model) View() string {
    if m.dialog.visible {
        return m.renderDialog()
    }
    return m.renderMainView()
}
```

### 7. Exponential Backoff for Reconnection

**Decision**: Use exponential backoff with jitter, capped at 30 seconds

**Rationale**:
- Prevents thundering herd on database recovery
- Jitter prevents synchronized retry storms
- 30-second cap balances responsiveness with resource efficiency

**Implementation**:
```go
func backoffDuration(attempt int) time.Duration {
    base := time.Second * time.Duration(math.Pow(2, float64(attempt)))
    if base > 30*time.Second {
        base = 30 * time.Second
    }
    jitter := time.Duration(rand.Int63n(int64(base / 4)))
    return base + jitter
}
```

### 8. Visual Design Reference Tools

**Decision**: Study pg_top (primary), htop (secondary), k9s (tertiary)

**Rationale**:
- pg_top: Direct competitor, PostgreSQL-specific activity monitoring
- htop: Excellent metrics panel design, color-coding patterns
- k9s: Modern TUI patterns, keyboard navigation, filtering

**Visual Requirements** (from Constitution VI):
1. Take screenshots of each tool's activity/dashboard views
2. Create ASCII mockup showing exact layout
3. Build throwaway demos testing rendering approaches
4. Define acceptance criteria: "Must look like pg_top with htop-style metrics panel"

**pg_top Reference Layout**:
```
┌─ Metrics Panel ────────────────────────────────┐
│ TPS: 1,234  Cache: 99.2%  Conn: 47  Size: 2.1G │
└────────────────────────────────────────────────┘
┌─ Activity Table ───────────────────────────────┐
│ PID    User     DB      State   Duration Query │
│ 12345  app      mydb    active  0:05     SELEC │
│ 12346  admin    mydb    idle    -        -     │
└────────────────────────────────────────────────┘
```

## Unknowns Resolved

All technical context items are clear. No NEEDS CLARIFICATION markers remain.

## Next Steps

1. Phase 1: Generate data-model.md with Connection and Metrics entities
2. Phase 1: Generate contracts for internal messages
3. Phase 1: Create quickstart.md for development setup
4. Comply with Constitution VI by including visual design artifacts in tasks
