# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Steep** is a terminal-based PostgreSQL monitoring and management application built with Go and the Bubbletea TUI framework. It provides real-time monitoring, performance analysis, and administrative capabilities for DBAs and developers through a keyboard-driven interface.

## Development Workflow

This project uses **Spec-Kit** for spec-driven development. All features follow this workflow:

1. `/speckit.specify` - Define user stories and requirements
2. `/speckit.clarify` - Resolve ambiguities (recommended before planning)
3. `/speckit.plan` - Create technical implementation plan
4. `/speckit.tasks` - Generate actionable task breakdown
5. `/speckit.analyze` - Validate consistency (optional)
6. `/speckit.implement` - Execute implementation

### Feature Branch Naming

Feature branches use numeric prefixes: `001-feature-name`, `002-dashboard-activity`, etc.

The implementation roadmap is in `docs/GUIDE.md` with 12 planned features organized by priority (P1/P2/P3).

## Constitution Compliance

**Critical**: All development MUST comply with `.specify/memory/constitution.md`. Key principles:

### I. Real-Time First
- All monitoring features require real-time updates with configurable refresh intervals (1-5s)
- Query execution must be < 500ms for standard monitors

### II. Keyboard-Driven Interface
- All functionality accessible via keyboard (vim-like: hjkl, g/G, /, etc.)
- Mouse interaction is optional

### III. Query Efficiency (NON-NEGOTIABLE)
- Use prepared statements, appropriate indexes, result limits
- Monitor queries MUST NOT impact production database performance
- Query plans MUST be reviewed before implementation
- No unbounded result sets

### IV. Incremental Delivery
- Features implemented in independently testable increments aligned with user stories
- Prioritize P1 stories (critical monitoring) before P2/P3
- Each increment must provide standalone value

### V. Comprehensive Coverage
- Cover all critical PostgreSQL monitoring scenarios: activity, query performance, locks, replication, bloat, vacuum
- Avoid feature gaps requiring context switching to other tools

## Architecture

### Directory Structure (Planned)

```
steep/
├── cmd/steep/              # Main application entry point
├── internal/
│   ├── app/               # Application orchestration
│   ├── config/            # Configuration management (Viper)
│   ├── db/                # Database connection (pgx/pgxpool)
│   │   ├── connection.go
│   │   ├── queries/       # SQL query definitions
│   │   └── models/        # Data models
│   ├── monitors/          # Monitoring modules (goroutines)
│   │   ├── activity.go
│   │   ├── locks.go
│   │   ├── replication.go
│   │   └── performance.go
│   ├── ui/                # Bubbletea UI components
│   │   ├── app.go         # Main app model
│   │   ├── components/    # Reusable components (Table, Chart, StatusBar)
│   │   ├── views/         # View implementations (Dashboard, Activity, Queries)
│   │   └── styles/        # Lipgloss styles (centralized)
│   └── utils/
├── pkg/pgstats/           # Public PostgreSQL statistics parsing
└── configs/               # Default configurations
```

### Bubbletea Architecture Patterns

**ViewModel Interface**: Each view implements:
```go
type ViewModel interface {
    Init() tea.Cmd
    Update(tea.Msg) (ViewModel, tea.Cmd)
    View() string
}
```

**Message Passing**: Use Bubbletea's `tea.Msg` pattern for:
- Monitor updates (from goroutines via channels)
- User input events
- Timer events for auto-refresh
- NO shared mutable state between views

**Component Reusability**: Extract common UI patterns to `internal/ui/components/`:
- Table (sortable, filterable)
- Chart (time-series, sparklines)
- StatusBar
- HelpText

**Styling Consistency**: All styles in `internal/ui/styles/` using Lipgloss. Colors, spacing, borders must be consistent across views.

### Monitor Goroutines

- One goroutine per monitor type (Activity, Queries, Locks, etc.)
- Communicate with main Bubbletea loop via channels
- Use context cancellation for cleanup
- Configurable refresh intervals per monitor type

### Database Query Guidelines

**PostgreSQL System Views** (primary data sources):
- `pg_stat_activity` - Current connections and activity
- `pg_stat_statements` - Query statistics (requires extension)
- `pg_locks` - Lock information
- `pg_stat_replication` - Replication status
- `pg_stat_all_tables` - Table access statistics
- `pg_stat_all_indexes` - Index usage statistics

**Extension Awareness**: Graceful degradation when optional extensions unavailable:
- `pg_stat_statements` (highly recommended)
- `pgstattuple` (bloat detection)
- `pg_buffercache` (buffer cache inspection)

**Version Compatibility**:
- Minimum: PostgreSQL 11 (best effort)
- Target: PostgreSQL 13+ (full feature support)
- Detect version with `SHOW server_version` and adjust features

## Technology Stack

### Core Libraries
- **bubbletea**: TUI application framework
- **bubbles**: Reusable TUI components (table, viewport, list)
- **lipgloss**: Styling and layout
- **pgx/pgxpool**: PostgreSQL driver and connection pooling
- **viper**: Configuration management

### Additional Libraries
- **asciigraph**: ASCII charts and graphs
- **chroma**: SQL syntax highlighting
- **glamour**: Markdown rendering for help text

### Testing
- **testcontainers**: Integration tests with real PostgreSQL instances
- Unit tests for business logic
- Integration tests for database query modules
- Manual UI testing checklist for view implementations

## Security Requirements

### Credentials
- NEVER store passwords in config files
- Support `password_command` for external password managers (e.g., `pass show postgres/local`)
- Support environment variables (`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`)
- Support interactive prompts

### SSL/TLS
- Default to `sslmode=prefer`
- Support `sslmode=require` for production

### Read-Only Mode
- Provide `--readonly` flag disabling destructive operations (VACUUM, KILL, REINDEX)
- Consider defaulting to read-only for safety

## Configuration

Configuration file: `~/.config/steep/config.yaml`

Key sections:
- `connections`: Named connection profiles with credentials
- `ui`: Theme, refresh intervals, display preferences
- `monitors`: Per-monitor refresh intervals and filters
- `alerts`: Threshold-based alert rules
- `keybindings`: Custom keyboard shortcut overrides

## Performance Requirements

- **Application startup**: < 1 second
- **View switching**: < 100ms
- **Query execution**: < 500ms for most monitors
- **Memory footprint**: < 50MB typical usage
- **CPU usage**: < 5% idle, < 20% active
- **UI rendering**: Smooth 60 FPS
- **Minimum terminal size**: 80x24 (with graceful degradation)

## Testing Requirements

### Unit Tests
Required for:
- Business logic (data parsing, calculations, metric aggregation)
- Query result parsing
- Configuration loading

### Integration Tests
Required for:
- Database query modules (use testcontainers with real PostgreSQL)
- Connection management and pooling
- Extension detection and fallback logic

### Manual UI Testing
Required for:
- Keyboard navigation completeness
- View rendering at various terminal sizes
- Error message clarity and actionability
- Help text accuracy

### Performance Validation
Before merging:
- Query execution benchmarks on production-scale databases
- Memory profiling under typical usage
- Render latency measurements

## Code Review Checklist

- [ ] SQL queries reviewed for index usage and result limiting
- [ ] Prepared statements used for repeated queries
- [ ] Bubbletea message flow reviewed for race conditions
- [ ] No shared mutable state between views/components
- [ ] Keyboard navigation complete (all actions have shortcuts)
- [ ] Help text updated for new features
- [ ] Error messages are actionable (tell user what to do)
- [ ] Graceful degradation when extensions unavailable
- [ ] UI renders correctly at 80x24 minimum terminal size
- [ ] Auto-refresh intervals configurable
- [ ] Performance validated: < 500ms queries, < 50MB memory
- [ ] Constitution compliance verified
- [ ] PR references user story and priority level (P1/P2/P3)

## Key PostgreSQL Queries by Feature

### Activity Monitoring
```sql
SELECT pid, usename, datname, state,
       now() - query_start as duration,
       query
FROM pg_stat_activity
WHERE state != 'idle'
ORDER BY query_start;
```

### Lock Detection
```sql
SELECT blocked_locks.pid AS blocked_pid,
       blocking_locks.pid AS blocking_pid,
       blocked_activity.query AS blocked_query,
       blocking_activity.query AS blocking_query
FROM pg_locks blocked_locks
JOIN pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid
JOIN pg_locks blocking_locks ON blocking_locks.locktype = blocked_locks.locktype
JOIN pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid
WHERE NOT blocked_locks.granted AND blocking_locks.granted;
```

### Replication Lag
```sql
SELECT application_name, state,
       pg_wal_lsn_diff(sent_lsn, write_lsn) AS write_lag_bytes,
       pg_wal_lsn_diff(sent_lsn, flush_lsn) AS flush_lag_bytes,
       pg_wal_lsn_diff(sent_lsn, replay_lsn) AS replay_lag_bytes
FROM pg_stat_replication;
```

## Documentation References

- **Design Document**: `docs/DESIGN.md` - Complete architecture and feature specifications
- **Implementation Guide**: `docs/GUIDE.md` - 12-feature roadmap with spec-kit commands
- **Constitution**: `.specify/memory/constitution.md` - Non-negotiable development principles
- **Bubbletea Examples**: `/Users/brandon/src/bubbletea` - Local reference for TUI patterns

## Common Gotchas

1. **Extension Availability**: Always check for `pg_stat_statements` before querying; provide installation guidance if missing
2. **Query Timeouts**: Implement timeouts to prevent hanging on slow queries
3. **Terminal Size Changes**: Handle `tea.WindowSizeMsg` to re-render on terminal resize
4. **Connection Failures**: Implement retry logic for transient failures; display clear errors for permanent failures
5. **Bubbletea State**: Never mutate shared state; always return new state from `Update()`
6. **Goroutine Leaks**: Always use context cancellation to clean up monitor goroutines on app exit
7. **VACUUM Blocking**: VACUUM operations can block; show progress and allow cancellation
8. **Read-Only Mode**: Verify `--readonly` flag before executing destructive operations
