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

### VI. Visual Design First (NON-NEGOTIABLE)
- ALL UI features MUST complete visual design phase BEFORE implementation
- Study 2-3 reference tools (pg_top, htop, k9s) with screenshots
- Create ASCII mockup showing exact layout
- Build 3 throwaway demos testing different rendering approaches
- Define visual acceptance criteria: "Must look like [tool X]"
- Implement static mockup first, get approval, THEN add real-time data

## Architecture

### Directory Structure

```
steep/
├── cmd/
│   ├── steep/             # TUI application entry point
│   ├── steep-agent/       # Background agent entry point
│   └── steep-repl/        # Replication daemon entry point
├── extensions/
│   └── steep_repl/        # PostgreSQL extension (Rust/pgrx)
├── internal/
│   ├── agent/             # Background agent implementation
│   │   ├── collectors/    # Data collectors (activity, queries, etc.)
│   │   ├── agent.go       # Agent orchestration
│   │   ├── service.go     # kardianos/service integration
│   │   └── retention.go   # Data retention/pruning
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
│   ├── repl/              # Replication daemon
│   │   ├── config/        # Replication config (YAML)
│   │   ├── daemon/        # Daemon lifecycle management
│   │   ├── grpc/          # gRPC server + proto definitions
│   │   ├── ipc/           # Unix socket IPC (JSON-RPC)
│   │   ├── pool/          # PostgreSQL connection pool
│   │   └── store/         # Node store (PostgreSQL-backed)
│   ├── ui/                # Bubbletea UI components
│   │   ├── app.go         # Main app model
│   │   ├── components/    # Reusable components (Table, Chart, StatusBar)
│   │   ├── views/         # View implementations (Dashboard, Activity, Queries)
│   │   └── styles/        # Lipgloss styles (centralized)
│   └── utils/
├── pkg/pgstats/           # Public PostgreSQL statistics parsing
├── tests/integration/repl/ # Replication integration tests
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
- `pg_stat_progress_vacuum` - VACUUM operation progress
- `pg_stat_progress_cluster` - VACUUM FULL/CLUSTER progress
- `pg_roles` - Database role information
- `pg_settings` - Configuration parameters

**Extension Awareness**: Graceful degradation when optional extensions unavailable:
- `pg_stat_statements` (highly recommended)
- `pgstattuple` (bloat detection)
- `pg_buffercache` (buffer cache inspection)

**Version Compatibility**:
- Minimum: PostgreSQL 11 (best effort)
- Target: PostgreSQL 18 (full feature support)
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

## Reference Tools (Available Locally)

These reference implementations are available for studying UI/UX patterns before implementing features:

- **pg_top**: `/Users/brandon/src/pg_top` - PostgreSQL activity monitor (primary reference for database monitoring UIs)
- **htop**: `/Users/brandon/src/htop` - Process viewer with excellent graph rendering (reference for sparklines/bars)
- **k9s**: `/Users/brandon/src/k9s` - Kubernetes TUI (reference for keyboard navigation and view switching)

**Before implementing any UI feature**: Study these tools, run them, screenshot relevant views, and include visual references in spec.md per Constitution Principle VI (Visual Design First).

## Common Gotchas

1. **Extension Availability**: Always check for `pg_stat_statements` before querying; provide installation guidance if missing
2. **Query Timeouts**: Implement timeouts to prevent hanging on slow queries
3. **Terminal Size Changes**: Handle `tea.WindowSizeMsg` to re-render on terminal resize
4. **Connection Failures**: Implement retry logic for transient failures; display clear errors for permanent failures
5. **Bubbletea State**: Never mutate shared state; always return new state from `Update()`
6. **Goroutine Leaks**: Always use context cancellation to clean up monitor goroutines on app exit
7. **VACUUM Blocking**: VACUUM operations can block; show progress and allow cancellation
8. **Read-Only Mode**: Verify `--readonly` flag before executing destructive operations

## Code Mismatches

When you find a mismatch between code and its dependencies (e.g., Go code expects a database column that doesn't exist), fix the dependency to match the code. Do not question whether the mismatch should exist. The existing code is the source of truth.

# Task Completion
Complete every task as specified. Never suggest skipping, cutting, or marking tasks as "not needed." If a task is in the plan, it must be implemented. If you believe a task is truly impossible, explain why and ask for guidance - do not unilaterally decide to skip it.

## Extension Testing Rules
NEVER blame "old extension schema", "stale database", or suggest dropping/recreating databases when pg_regress tests fail. If tests fail after code changes, the code changes themselves are wrong or incomplete. Investigate the actual build output and errors.

## Active Technologies
- Go 1.21+ (001-foundation)
- PostgreSQL 11+ (target 18) for monitoring; YAML file for configuration (~/.config/steep/config.yaml) (001-foundation)
- Go 1.21+ + bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool (002-dashboard-activity)
- PostgreSQL 11+ (pg_stat_activity, pg_stat_database system views) (002-dashboard-activity)
- Go 1.21+ + bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool, pg_query_go/v5, honeytail/parsers/postgresql, go-sqlite3, golang.design/x/clipboard (003-query-performance)
- SQLite (query_stats.db in ~/.config/steep/), PostgreSQL (source database) (003-query-performance)
- Go 1.21+ (using Go 1.25.4 per go.mod) + bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool, xlab/treeprin (004-locks-blocking)
- PostgreSQL (pg_locks, pg_stat_activity system views) (004-locks-blocking)
- Go 1.21+ (Go 1.25.4 per go.mod) + bubbletea, bubbles, lipgloss, pgx/pgxpool, golang.design/x/clipboard (005-tables-statistics)
- PostgreSQL (source database via pg_stat_all_tables, pg_stat_all_indexes, pgstattuple) (005-tables-statistics)
- Go 1.25.4 (per go.mod) + bubbletea, bubbles, lipgloss, pgx/pgxpool, asciigraph (sparklines), go-sqlite3 (lag history) (006-replication-monitoring)
- PostgreSQL (pg_stat_replication, pg_replication_slots, pg_publication, pg_subscription) (006-replication-monitoring)
- Go 1.25.4 + bubbletea, vimtea (vim-style editor), lipgloss, pgx/pgxpool, chroma (syntax highlighting), golang.design/x/clipboard, go-yaml (007-sql-editor)
- SQLite (query history in ~/.config/steep/), YAML (snippets in ~/.config/steep/snippets.yaml) (007-sql-editor)
- Go 1.25.4 (per go.mod) + bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool (008-configuration-viewer)
- PostgreSQL (pg_settings view - read-only) (008-configuration-viewer)
- Go 1.25.4 (per go.mod) + bubbletea, bubbles (viewport), lipgloss, pgx/pgxpool (009-log-viewer)
- PostgreSQL log files (file system read or pg_read_file()), position tracking via SQLite (009-log-viewer)
- Go 1.25.4 (per go.mod) + bubbletea, bubbles, lipgloss, pgx/pgxpool (010-database-operations)
- PostgreSQL (pg_stat_progress_vacuum, pg_stat_progress_cluster, pg_stat_all_tables, pg_roles, pg_catalog, pg_class_aclitem) (010-database-operations)
- Go 1.25.4 + bubbletea, bubbles, lipgloss, asciigraph (v0.7.3), pterm (v0.12.82), pgx/pgxpool, go-sqlite3 (011-visualizations)
- SQLite (existing ~/.config/steep/steep.db for metrics persistence), PostgreSQL (source metrics) (011-visualizations)
- Go 1.25.4 (per go.mod) + bubbletea, bubbles, lipgloss, pgx/pgxpool, go-sqlite3, viper (config) (012-alert-system)
- SQLite (~/.config/steep/steep.db) for alert history and acknowledgment persistence; YAML (~/.config/steep/config.yaml) for alert rule configuration (012-alert-system)
- Go 1.25.4 (per existing go.mod) (013-service-architecture)
- SQLite (~/.config/steep/steep.db) with WAL mode for concurrent access (013-service-architecture)
- Go 1.25.4 + grpc-go, protobuf, kardianos/service, pgx/pgxpool (014-repl-foundation)
- Rust + pgrx 0.16.1 for PostgreSQL extension (014-repl-foundation)
- PostgreSQL 18 (steep_repl schema tables: nodes, coordinator_state, audit_log) (014-repl-foundation)
- Docker (ghcr.io/willibrandon/pg18-steep-repl for integration tests) (014-repl-foundation)
- Go 1.25.4 (per go.mod), Rust + pgrx 0.16.1 (PostgreSQL extension) + pgx/pgxpool (database), bubbletea/bubbles/lipgloss (TUI), grpc-go/protobuf (daemon communication), viper (config) (015-node-init)
- PostgreSQL 18 (steep_repl schema - nodes, coordinator_state, audit_log + new tables), YAML config (015-node-init)

## Recent Changes
- 014-repl-foundation: steep-repl replication daemon with gRPC and IPC interfaces
- 014-repl-foundation: steep_repl PostgreSQL extension (Rust/pgrx) with nodes, coordinator_state, audit_log tables
- 014-repl-foundation: Cross-platform service management via kardianos/service (macOS launchd, Linux systemd, Windows SCM)
- 014-repl-foundation: gRPC service (Coordinator) for node registration, health checks, heartbeats
- 014-repl-foundation: Unix socket IPC with JSON-RPC protocol for local CLI communication
- 014-repl-foundation: Windows named pipe IPC support (\\.\pipe\steep-repl)
- 014-repl-foundation: HTTP health endpoint for load balancers and monitoring systems (/health)
- 014-repl-foundation: PostgreSQL-backed node store with constraints (priority 1-100, port 1-65535, valid status)
- 014-repl-foundation: TLS support for gRPC (optional cert/key/CA configuration)
- 014-repl-foundation: PostgreSQL connection pooling with automatic reconnection and exponential backoff
- 014-repl-foundation: CLI commands: run, install, uninstall, start, stop, restart, status, init-tls, health
- 014-repl-foundation: Integration tests with testcontainers-go and Docker image (ghcr.io/willibrandon/pg18-steep-repl)
- 014-repl-foundation: Make targets: build-repl, build-repl-daemon, build-repl-ext, test-repl, test-repl-integration
- 014-repl-foundation: Multi-platform CI (Linux, macOS, Windows) with PostgreSQL 18
- 013-service-architecture: steep-agent background daemon for continuous data collection independent of TUI runtime
- 013-service-architecture: Cross-platform service management via kardianos/service (macOS launchd, Linux systemd, Windows SCM)
- 013-service-architecture: CLI commands: install, uninstall, start, stop, restart, status, run, logs
- 013-service-architecture: TUI auto-detects agent and switches between [AGENT] and [LOG] collection modes
- 013-service-architecture: Multi-instance PostgreSQL monitoring with configurable instances array
- 013-service-architecture: Data collectors for activity, queries, replication, locks, and metrics
- 013-service-architecture: Configurable collection intervals (agent.intervals) and retention periods (agent.retention)
- 013-service-architecture: Background alerting via webhooks (agent.alerts.webhook_url)
- 013-service-architecture: Automatic data retention with configurable pruning
- 013-service-architecture: Graceful shutdown with WAL checkpoint, schema versioning, disk full detection
- 012-alert-system: Threshold-based alerts with YAML config (alerts.rules in config.yaml)
- 012-alert-system: Expression-based rules with binary operators (+, -, *, /), parentheses, and operator precedence
- 012-alert-system: Available metrics: active_connections, max_connections, cache_hit_ratio, tps, database_size, replication_lag_bytes, longest_transaction_seconds, idle_in_transaction_seconds
- 012-alert-system: Operators: >, <, >=, <=, ==, !=
- 012-alert-system: Alert states: normal, warning, critical with color-coded indicators (yellow/red)
- 012-alert-system: Alert panel in Dashboard showing active alerts with severity icons, current values, and thresholds
- 012-alert-system: Alert counts in status bar (e.g., "2 CRIT 1 WARN")
- 012-alert-system: Alert history overlay (a key) with j/k navigation, g/G jump to top/bottom
- 012-alert-system: Alert acknowledgment (Enter key in history) persisted to SQLite
- 012-alert-system: SQLite persistence for alert events (~/.config/steep/steep.db alert_events table)
- 012-alert-system: Configurable history retention (alerts.history_retention, default 720h/30 days)
- 012-alert-system: Hourly prune goroutine for automatic history cleanup
- 012-alert-system: Graceful degradation when metrics unavailable during connection loss
- 012-alert-system: Message templates with Go text/template syntax (fields: Name, Metric, Warning, Critical, State, PrevState, Value, Threshold, ValueFmt, ThreshFmt)
- 011-visualizations: Time-series graphs on Dashboard (TPS, connections, cache hit ratio) with configurable time windows (1m-24h)
- 011-visualizations: TPS heatmap showing weekly activity patterns (24h × 7d grid) with RGB color gradient
- 011-visualizations: Sparklines in Activity view (query duration trends) and Tables view (size trends)
- 011-visualizations: Bar charts in Queries view (top 10 by execution time) and Tables view (top 10 by size)
- 011-visualizations: Global chart toggle (V key) across Dashboard, Queries, and Tables views
- 011-visualizations: Metrics collection infrastructure (internal/metrics package) with CircularBuffer and MetricsStore
- 011-visualizations: Per-connection duration tracking for Activity view sparklines
- 007-sql-editor: External REPL support via :repl command (pgcli, psql, litecli, sqlite3 with Docker fallback)
- 007-sql-editor: SQLite REPL for steep.db debugging (:repl sqlite, :repl litecli, :repl sqlite3)
- 007-sql-editor: Multi-arch Docker image for litecli (willibrandon/litecli) supporting amd64 and arm64
- 007-sql-editor: Multi-arch Docker image for pgcli (willibrandon/pgcli) supporting amd64 and arm64
- 007-sql-editor: Windows Docker support with automatic host.docker.internal translation
- 010-database-operations: Maintenance operations menu (VACUUM, VACUUM FULL, VACUUM ANALYZE, ANALYZE, REINDEX TABLE, REINDEX CONCURRENTLY)
- 010-database-operations: Operation progress tracking with real-time progress bar for VACUUM/VACUUM FULL
- 010-database-operations: Single-operation enforcement (one maintenance op at a time)
- 010-database-operations: Operation cancellation via pg_cancel_backend
- 010-database-operations: Session-scoped operation history overlay (H key) with FIFO eviction
- 010-database-operations: Actionable error messages for common failure scenarios
- 010-database-operations: Connection loss detection and handling during maintenance
- 010-database-operations: Permissions dialog (p key) for viewing table grants/grantees
- 010-database-operations: Roles view (key 0) for browsing/creating/dropping/altering database roles
- 010-database-operations: Scrollable help overlay with j/k navigation
- 006-replication-monitoring: Overview tab with replica status, lag metrics, and topology visualization
- 006-replication-monitoring: Sparklines for lag history with color coding (green <1MB, yellow 1-10MB, red >10MB)
- 006-replication-monitoring: Time windows: 1m (memory), 5m/15m/1h (SQLite persistence)
- 006-replication-monitoring: Slots tab for replication slots management with drop inactive slots
- 006-replication-monitoring: Logical tab for publications and subscriptions browser
- 006-replication-monitoring: Setup tab with physical/logical replication wizards, connection string builder, config checker
- 006-replication-monitoring: Pipeline visualization in topology view
- 009-log-viewer: Extended :level command with timestamp support (:level error -1h, :level warn+ >14:30)
- 009-log-viewer: Command and search history with SQLite persistence, shell-style deduplication, ↑/↓ navigation
- 009-log-viewer: Real-time PostgreSQL log streaming with follow mode and severity filtering
- 009-log-viewer: Support for stderr, CSV, and JSON log formats with auto-detection from log_destination
- 009-log-viewer: Remote log viewing via pg_read_file() for containerized/remote PostgreSQL instances
- 009-log-viewer: logs.access_method config option (auto, filesystem, pg_read_file)
- 009-log-viewer: Historical log navigation with :goto command (supports relative times like -1h)
- 009-log-viewer: Search with regex pattern matching and n/N navigation between matches
- 009-log-viewer: Multi-line log entry parsing with proper DETAIL/HINT/CONTEXT handling
- 008-configuration-viewer: Browse all PostgreSQL parameters from pg_settings with category filtering and search
- 008-configuration-viewer: Parameter details overlay with type, context, constraints, default values, and descriptions
- 008-configuration-viewer: Color-coded status indicators (yellow=modified, red=pending restart)
- 008-configuration-viewer: :set command to modify parameters via ALTER SYSTEM (writes to postgresql.auto.conf)
- 008-configuration-viewer: :reset command to restore defaults, :reload to apply changes via pg_reload_conf()
- 008-configuration-viewer: :export config command to export parameters to PostgreSQL conf file format
- 008-configuration-viewer: Sort by name or category, copy parameter name/value to clipboard
- 008-configuration-viewer: Responsive layout, read-only mode support, 60-second auto-refresh
- 007-sql-editor: Multi-line SQL editor with vim-style editing (vimtea), Chroma syntax highlighting
- 007-sql-editor: Query execution with F5/Ctrl+Enter, paginated results (100 rows/page), column sorting
- 007-sql-editor: Transaction support (BEGIN/COMMIT/ROLLBACK/SAVEPOINT) with state tracking
- 007-sql-editor: Query history with SQLite persistence, shell-style deduplication, Ctrl+R search
- 007-sql-editor: Named snippets with YAML persistence, snippet browser (Ctrl+O)
- 007-sql-editor: Export to CSV/JSON with :export command, tilde expansion, auto-extension
- 007-sql-editor: Automatic reconnection on connection loss with exponential backoff and query retry
- 007-sql-editor: Configurable themes, resize support, read-only mode blocking DDL/DML
- 005-tables-statistics: Hierarchical schema/table browser with expand/collapse, partition visualization
- 005-tables-statistics: Table statistics (size, rows, cache hit, bloat) with color-coded bloat warnings
- 005-tables-statistics: Index usage panel with unused index highlighting (yellow)
- 005-tables-statistics: Table details panel with columns, constraints, indexes, size breakdown
- 005-tables-statistics: SQL copy menu (SELECT, INSERT, UPDATE, DELETE templates)
- 005-tables-statistics: Maintenance operations (VACUUM, ANALYZE, REINDEX) with confirmation dialogs
- 005-tables-statistics: pgstattuple extension auto-install prompt, readonly mode support
- 004-locks-blocking: Lock monitoring with active locks table, blocking detection (red/yellow color coding), dependency tree visualization
- 004-locks-blocking: Kill blocking queries with confirmation dialog, readonly mode support
- 004-locks-blocking: Deadlock history with PostgreSQL log parsing (CSV/JSON formats), cycle visualization
- 004-locks-blocking: Sort direction toggle (S key) across all views, server-side SQL sorting
- 003-query-performance: Query performance monitoring with fingerprinting, EXPLAIN plans (JSON) and EXPLAIN ANALYZE (tree visualization with pev), search/filter, clipboard, reset stats
- 003-query-performance: Performance benchmarks validating <500ms query, <100ms UI targets
- 003-query-performance: Make targets: bench, test-short, test-integration
- 001-foundation: Added Go 1.21+
