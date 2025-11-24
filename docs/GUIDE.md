# Steep Implementation Guide

## Complete Spec-Kit Workflow Roadmap

This guide provides the complete logical sequence of `/speckit.specify` commands necessary to build Steep as a production-ready PostgreSQL monitoring terminal application. Each feature is designed to be independently deliverable, testable, and aligned with the constitution's principles of incremental delivery.

---

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Feature Breakdown Strategy](#feature-breakdown-strategy)
- [Implementation Sequence](#implementation-sequence)
- [Feature Details](#feature-details)
- [Best Practices](#best-practices)
- [Success Criteria](#success-criteria)

---

## Overview

Steep will be built through **12 distinct features**, each following the complete spec-kit workflow:

1. `/speckit.specify` - Define user stories and requirements
2. `/speckit.clarify` - Resolve ambiguities (optional but recommended)
3. `/speckit.plan` - Create technical implementation plan
4. `/speckit.tasks` - Generate actionable task breakdown
5. `/speckit.analyze` - Validate consistency (optional)
6. `/speckit.implement` - Execute implementation

Each feature delivers standalone value and builds upon previous features. The sequence is optimized for:
- **Early user value**: Core monitoring available first
- **Reduced risk**: Infrastructure before complex features
- **Independent testing**: Each feature testable in isolation
- **Iterative refinement**: Learn and adapt between features

---

## Prerequisites

Before starting feature development, ensure:

1. **Constitution established**: Run `/speckit.constitution` (already completed)
2. **Go environment**: Go 1.21+ installed and configured
3. **PostgreSQL access**: Test database available for development
4. **Bubbletea familiarity**: Review local Bubbletea examples at `/Users/brandon/src/bubbletea`
5. **Git workflow**: Understand feature branch naming (001-feature-name)

---

## Feature Breakdown Strategy

### Dependency Hierarchy

```
001-foundation (Base infrastructure)
    ├── 002-dashboard-activity (First monitoring view)
    ├── 003-query-performance (Query stats)
    ├── 004-locks-blocking (Lock monitoring)
    ├── 005-tables-statistics (Table stats)
    └── 006-replication-monitoring (Replication)
        └── 007-sql-editor (Interactive queries)
            ├── 008-configuration-viewer (Config display)
            ├── 009-log-viewer (Log display)
            └── 010-database-operations (Management)
                ├── 011-advanced-visualizations (Charts)
                └── 012-alert-system (Alerting)
```

### Priority Classification

- **P1 (Must-Have)**: Features 001-006 - Core monitoring MVP
- **P2 (Should-Have)**: Features 007-010 - Management and operations
- **P3 (Nice-to-Have)**: Features 011-012 - Advanced analytics

---

## Implementation Sequence

### Phase 1: Foundation & Core Monitoring (P1)

**Estimated Timeline**: 4-6 weeks for P1 features

| Feature | Branch | Dependencies | Estimated Effort |
|---------|--------|--------------|------------------|
| 001 - Foundation & Infrastructure | `001-foundation` | None | 1 week |
| 002 - Dashboard & Activity Monitoring | `002-dashboard-activity` | 001 | 1 week |
| 003 - Query Performance Monitoring | `003-query-performance` | 001 | 1 week |
| 004 - Locks & Blocking Detection | `004-locks-blocking` | 001 | 1 week |
| 005 - Tables & Statistics Viewer | `005-tables-statistics` | 001 | 1 week |
| 006 - Replication Monitoring | `006-replication-monitoring` | 001 | 1 week |

### Phase 2: Interactive Operations (P2)

**Estimated Timeline**: 3-4 weeks for P2 features

| Feature | Branch | Dependencies | Estimated Effort |
|---------|--------|--------------|------------------|
| 007 - SQL Editor & Execution | `007-sql-editor` | 001 | 1 week |
| 008 - Configuration Viewer | `008-configuration-viewer` | 001 | 3-5 days |
| 009 - Log Viewer | `009-log-viewer` | 001 | 3-5 days |
| 010 - Database Operations | `010-database-operations` | 001, 007 | 1 week |

### Phase 3: Advanced Features (P3)

**Estimated Timeline**: 2-3 weeks for P3 features

| Feature | Branch | Dependencies | Estimated Effort |
|---------|--------|--------------|------------------|
| 011 - Advanced Visualizations | `011-visualizations` | 001-006 | 1-2 weeks |
| 012 - Alert System | `012-alert-system` | 001-006 | 1 week |

---

## Feature Details

### Feature 001: Foundation & Infrastructure

**Branch**: `001-foundation`

**Purpose**: Establish core application architecture, database connectivity, and reusable UI components.

**User Stories**:
1. As a DBA, I want to launch Steep and connect to a PostgreSQL database using connection credentials
2. As a DBA, I want basic keyboard navigation (quit, help) to interact with the application
3. As a DBA, I want to see a status bar showing connection state and basic metrics

**Technical Scope**:
- Go module initialization with dependencies (bubbletea, bubbles, lipgloss, pgx, viper)
- Bubbletea application scaffold with main model
- Database connection management with pgxpool
- Configuration file loading (YAML support)
- Reusable UI components: Table, StatusBar, HelpText
- Basic styling system with Lipgloss
- Error handling and logging infrastructure
- View switching framework (ViewType enum, ViewModel interface)

**Acceptance Criteria**:
- Application launches and reads config from `~/.config/steep/config.yaml`
- Establishes connection to PostgreSQL and displays connection status
- Status bar shows: connection state, database name, timestamp
- Keyboard shortcuts: `q` quits, `h` shows help, `Esc` closes dialogs
- Application gracefully handles connection failures with error display
- Minimum terminal size (80x24) supported

**Spec-Kit Command**:
```bash
/speckit.specify Build the foundation infrastructure for Steep, a PostgreSQL monitoring TUI application. The application should initialize using Go with Bubbletea framework, establish database connections using pgx connection pooling, load configuration from YAML files, and provide a basic keyboard-driven interface with view switching capabilities. Include reusable UI components (table, status bar, help text) using Lipgloss for consistent styling. Focus on P1 user story: launching the app, connecting to PostgreSQL, and basic navigation. This is the foundation upon which all monitoring features will be built.
```

---

### Feature 002: Dashboard & Activity Monitoring

**Branch**: `002-dashboard-activity`

**Purpose**: Provide real-time visibility into active database connections and current activity.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to see all active connections with query details to monitor current database activity
2. **P1**: As a DBA, I want to see key metrics (TPS, cache hit ratio, connection count) on a dashboard to assess database health at a glance
3. **P2**: As a DBA, I want to kill or terminate problematic connections to resolve blocking issues
4. **P2**: As a DBA, I want to filter connections by state (active, idle, idle-in-transaction) to focus on specific activity types

**Technical Scope**:
- Dashboard view with multi-panel layout
- Activity monitor querying `pg_stat_activity`
- Real-time metrics from `pg_stat_database`
- Table component with sorting and filtering
- Auto-refresh mechanism with configurable intervals
- Connection state indicators (colors for active/idle/blocked)
- Action handler for killing connections (with confirmation)

**Database Queries**:
- `pg_stat_activity` for active connections
- `pg_stat_database` for database-wide statistics
- Transaction count queries for TPS calculation

**Acceptance Criteria**:
- Dashboard displays: TPS, cache hit %, active connections, database size
- Activity view shows table with: PID, User, Database, State, Duration, Query (truncated)
- Table supports sorting by any column (click column header or keyboard shortcut `s`)
- Filter connections by state using `/` search command
- Auto-refresh every 1 second (configurable)
- Kill connection action (`x` key) with confirmation dialog
- View full query text with `d` (details) key
- Performance: Queries execute in < 500ms on database with 100+ connections

**Spec-Kit Command**:
```bash
/speckit.specify Implement the Dashboard and Activity Monitoring view for Steep. Create a multi-panel dashboard showing real-time key metrics (TPS, cache hit ratio, connection count, database size). Build an Activity view displaying all active PostgreSQL connections in a sortable, filterable table showing PID, user, database, state, duration, and query text. Support auto-refresh with 1-second intervals. Enable DBA actions: kill/terminate connections, view full query text, filter by connection state. Prioritize P1 story (viewing connections and metrics) over P2 story (killing connections). Query pg_stat_activity and pg_stat_database with optimized queries that execute in under 500ms.
```

---

### Feature 003: Query Performance Monitoring

**Branch**: `003-query-performance`

**Purpose**: Implement query performance monitoring to identify slow or frequent queries without pg_stat_statements or extensions. Uses built-in PostgreSQL features: query logging (reloadable) or pg_stat_activity sampling. Aggregates data client-side, fingerprints for deduplication, persists in SQLite.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to see top queries by total execution time to identify the most impactful slow queries
2. **P1**: As a DBA, I want to see top queries by call count to identify frequently executed queries
3. **P2**: As a DBA, I want to view EXPLAIN plans for queries to understand query execution strategy
4. **P2**: As a DBA, I want to search and filter queries by text pattern to find specific query types
5. **P3**: As a DBA, I want to reset statistics to start fresh monitoring

**Technical Scope**:
- Queries view with tabbed interface (By Time, By Calls, By Rows)
- Log-based primary approach: Parse PostgreSQL logs for query history (enable `log_min_duration_statement = 0` via reload)
- Sampling fallback: Poll pg_stat_activity for real-time estimates when logging disabled
- Query fingerprinting using `github.com/pganalyze/pg_query_go/v5` for deduplication
- Log parsing using `github.com/honeycombio/honeytail/parsers/postgresql`
- Client-side aggregation and persistence in SQLite (`query_stats.db`)
- EXPLAIN plan execution via `EXPLAIN (FORMAT JSON) <query>`
- Search and filter by query text (regex support via SQLite)
- Query statistics table: Query fingerprint, Calls, Total Time, Mean Time, Min/Max, Rows
- Copy query text to clipboard functionality

**Architecture**:
- **Sources**: Logs (primary) or pg_stat_activity polling (fallback)
- **Pipeline**: Fetch data → Parse/extract → Fingerprint/normalize → Aggregate → Persist in SQLite → Query for views
- **Persistence**: SQLite schema with fingerprint, query_text, calls, total_time, min_time, max_time, rows
- **Libraries**: pg_query_go (fingerprinting), honeytail (log parsing), go-sqlite3 (persistence)

**Database Queries**:
- `SHOW log_min_duration_statement` to check logging config
- `SELECT pg_current_logfile()` for log file location
- `SELECT * FROM pg_stat_activity` for sampling fallback
- `EXPLAIN (FORMAT JSON) <query>` for plan visualization

**Acceptance Criteria**:
- Queries view accessible via `3` key or tab navigation
- Three tabs: "By Time", "By Calls", "By Rows" switchable with arrow keys
- Table shows: Query (normalized, 100 chars), Calls, Total Time, Mean Time, Rows
- Sort by any column with `s` key
- Search queries with `/` key (regex pattern matching)
- View EXPLAIN plan with `e` key (displays formatted JSON output)
- Copy query text to clipboard with `y` key
- Auto-enable logging if disabled (via `ALTER SYSTEM` + `pg_reload_conf()`) with user confirmation
- Fallback to pg_stat_activity sampling with guidance when logging unavailable
- Reset statistics with `R` key (requires confirmation, truncates SQLite table)
- Auto-refresh every 5 seconds (configurable)
- Top 50 queries per view for performance
- Queries execute in under 500ms

**Spec-Kit Command**:
```bash
/speckit.specify Implement Query Performance Monitoring view without requiring pg_stat_statements extension. Use PostgreSQL query logging (log_min_duration_statement) as primary data source with log parsing via honeytail library. Fall back to pg_stat_activity sampling when logging unavailable. Fingerprint queries using pg_query_go for deduplication and normalization. Aggregate statistics client-side and persist in SQLite database. Create tabbed interface showing top queries by execution time, call count, and rows. Support EXPLAIN plan viewing, query text search with regex patterns, and copy-to-clipboard functionality. Auto-enable logging via ALTER SYSTEM with user confirmation if disabled. Enable statistics reset with confirmation. Prioritize P1 stories (viewing top queries) over P2 (EXPLAIN, search) and P3 (reset). Queries must execute in under 500ms and support auto-refresh every 5 seconds.
```

---

### Feature 004: Locks & Blocking Detection

**Branch**: `004-locks-blocking`

**Purpose**: Monitor database locks, identify blocking queries, and visualize lock dependency trees.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to see all active locks with their type and mode to understand lock contention
2. **P1**: As a DBA, I want to identify which queries are blocking others to resolve deadlock situations
3. **P2**: As a DBA, I want to visualize the lock dependency tree to understand blocking relationships
4. **P2**: As a DBA, I want to kill blocking queries to quickly resolve lock contention
5. **P3**: As a DBA, I want to see deadlock history to analyze recurring deadlock patterns

**Technical Scope**:
- Locks view querying `pg_locks` system catalog
- Blocking query detection via joins on pg_locks and pg_stat_activity
- Lock dependency tree visualization (ASCII tree structure)
- Lock type and mode display (ACCESS SHARE, ROW EXCLUSIVE, etc.)
- Highlight blocked vs blocking queries with color coding
- Action to kill blocking query with confirmation
- Lock wait queue visualization

**Database Queries**:
- Join `pg_locks` with `pg_stat_activity` to get lock details
- Self-join on `pg_locks` to detect blocking relationships
- Query for granted vs waiting locks

**Acceptance Criteria**:
- Locks view accessible via `5` key
- Table shows: PID, Lock Type, Mode, Granted (yes/no), Database, Relation, Query (truncated)
- Blocked queries highlighted in red, blocking queries in yellow
- Lock dependency tree displayed below table (ASCII art visualization)
- Sort by any column with `s` key
- View full blocking query with `d` key
- Kill blocking query with `x` key (requires confirmation)
- Auto-refresh every 2 seconds
- Performance: Query executes in < 500ms even with 100+ locks

**Spec-Kit Command**:
```bash
/speckit.specify Implement Locks and Blocking Detection view for monitoring database lock contention. Query pg_locks and pg_stat_activity to display active locks with type, mode, granted status, and associated queries. Detect and highlight blocking relationships with color coding (red for blocked, yellow for blockers). Visualize lock dependency trees using ASCII art to show blocking chains. Support killing blocking queries with confirmation dialog. Prioritize P1 stories (viewing locks and identifying blockers) over P2 (dependency tree visualization and kill action). Auto-refresh every 2 seconds with queries executing under 500ms.
```

---

### Feature 005: Tables & Statistics Viewer

**Branch**: `005-tables-statistics`

**Purpose**: Browse database schema hierarchy and view table/index statistics including bloat detection.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to browse databases, schemas, and tables in a hierarchical view to explore database structure
2. **P1**: As a DBA, I want to see table sizes and row counts to understand storage usage
3. **P2**: As a DBA, I want to see index usage statistics to identify unused or inefficient indexes
4. **P2**: As a DBA, I want to see bloat estimates for tables and indexes to plan VACUUM operations
5. **P2**: As a DBA, I want Steep to offer to install pgstattuple extension if not available to enable bloat detection
6. **P3**: As a DBA, I want to execute VACUUM, ANALYZE, or REINDEX on selected tables to maintain database health

**Technical Scope**:
- Hierarchical tree view: Database → Schema → Table
- Table statistics from `pg_stat_all_tables` and `pg_statio_all_tables`
- Index usage from `pg_stat_all_indexes`
- Table and index size queries using `pg_relation_size()`, `pg_total_relation_size()`
- Bloat estimation using pgstattuple extension (with graceful fallback)
- Auto-install pgstattuple extension via `CREATE EXTENSION pgstattuple` with user confirmation
- Table details panel: columns, types, constraints, indexes
- Cache hit ratio per table
- Sequential vs index scan ratio

**Database Queries**:
- `pg_stat_all_tables` for table access statistics
- `pg_stat_all_indexes` for index usage
- `pg_relation_size()` for size calculations
- `pgstattuple()` for bloat detection (if available)
- `SELECT * FROM pg_extension WHERE extname = 'pgstattuple'` to check extension status
- `CREATE EXTENSION pgstattuple` for auto-install (requires superuser or create privilege)
- System catalogs for schema/table metadata

**Acceptance Criteria**:
- Tables view accessible via `5` key
- Hierarchical browser with expand/collapse (arrow keys or `Enter`)
- Table list shows: Name, Size (MB), Row Count, Bloat %, Cache Hit %
- Index list shows: Name, Size (MB), Scans, Rows Read, Cache Hit %
- Details panel shows: Column definitions, constraints, foreign keys
- Highlight unused indexes (0 scans) in yellow
- Highlight high bloat (>20%) in red
- Sort tables by size, bloat, or cache hit ratio
- Auto-refresh every 30 seconds (static data, slower refresh)
- Graceful degradation: Skip bloat if pgstattuple not available
- Prompt to install pgstattuple if not available with confirmation dialog
- Show clear error if extension install fails (insufficient privileges)

**Spec-Kit Command**:
```bash
/speckit.specify Implement Tables and Statistics Viewer with hierarchical database browser (Database → Schema → Table). Display table statistics including size, row count, bloat percentage, and cache hit ratio from pg_stat_all_tables. Show index usage statistics with scan counts and unused index detection from pg_stat_all_indexes. Include table details panel showing columns, constraints, and foreign keys. Support bloat estimation using pgstattuple extension with graceful fallback. Auto-install pgstattuple via CREATE EXTENSION with user confirmation if not available. Prioritize all P1 stories (browsing and viewing table stats) and P2 (bloat detection, auto-install extension). Use auto-refresh every 30 seconds. Queries must execute under 500ms.
```

---

### Feature 006: Replication Monitoring

**Branch**: `006-replication-monitoring`

**Purpose**: Monitor PostgreSQL replication status, lag metrics, and WAL streaming.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to see replication lag (bytes and time) to ensure replicas are synchronized
2. **P1**: As a DBA, I want to see replication slot status to monitor replication health
3. **P2**: As a DBA, I want to see WAL sender/receiver statistics to understand replication throughput
4. **P2**: As a DBA, I want to visualize replication timeline to see lag trends over time
5. **P3**: As a DBA, I want to manage replication slots (advance, drop) to prevent WAL buildup

**Technical Scope**:
- Replication view querying `pg_stat_replication`
- Replication slot information from `pg_replication_slots`
- WAL statistics from `pg_stat_wal_receiver` and `pg_stat_archiver`
- Lag calculation (byte lag and time lag)
- Replication timeline visualization (sparkline showing lag over time)
- Slot management actions (advance, drop with confirmation)

**Database Queries**:
- `pg_stat_replication` for sender statistics
- `pg_replication_slots` for slot information
- `pg_stat_wal_receiver` for receiver statistics on replicas
- `pg_wal_lsn_diff()` for lag calculation

**Acceptance Criteria**:
- Replication view accessible via `6` key
- Table shows: Application Name, State, Sent LSN, Write LSN, Flush LSN, Replay LSN
- Calculated columns: Byte Lag, Time Lag (estimated)
- Replication slot table shows: Slot Name, Plugin, Database, Active, Restart LSN
- Lag timeline sparkline (last 60 data points, 1-second intervals)
- Color coding: Green (lag < 1MB), Yellow (1-10MB), Red (>10MB)
- Graceful handling when replication not configured (show "N/A" message)
- Auto-refresh every 2 seconds
- Performance: Queries execute under 500ms

**Spec-Kit Command**:
```bash
/speckit.specify Implement Replication Monitoring view for tracking PostgreSQL streaming replication. Display replication statistics from pg_stat_replication including sender/receiver LSN positions, byte lag, and time lag estimates. Show replication slot status from pg_replication_slots with active/inactive indicators. Visualize lag trends over time using sparklines (60-second window). Support graceful degradation when replication is not configured. Prioritize P1 stories (viewing lag and slot status) over P2 (timeline visualization). Auto-refresh every 2 seconds with sub-500ms query execution.
```

---

### Feature 007: SQL Editor & Execution

**Branch**: `007-sql-editor`

**Purpose**: Provide interactive SQL editor with query execution, syntax highlighting, and transaction management.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to write and execute SQL queries to interact with the database interactively
2. **P1**: As a DBA, I want to see query results in a paginated table to review query output
3. **P2**: As a DBA, I want syntax highlighting for SQL to write queries more easily
4. **P2**: As a DBA, I want to manage transactions (BEGIN, COMMIT, ROLLBACK) to test changes safely
5. **P3**: As a DBA, I want query history with recall to re-execute previous queries
6. **P3**: As a DBA, I want to save queries as snippets to reuse common queries

**Technical Scope**:
- Full-screen SQL editor view
- Multi-line text input component
- SQL syntax highlighting using chroma library
- Query execution with result display in table format
- Result pagination (for large result sets)
- Transaction state management
- Query history storage (in-memory and persistent)
- Saved queries/snippets library (YAML file storage)
- Export results (CSV, JSON formats)

**Acceptance Criteria**:
- SQL Editor accessible via `e` key or view navigation
- Multi-line editor with syntax highlighting for SQL keywords, strings, comments
- Execute query with `Ctrl+Enter`
- Results displayed in paginated table (100 rows per page)
- Navigation: `n` for next page, `p` for previous page
- Transaction controls: `:begin`, `:commit`, `:rollback` commands
- Transaction indicator in status bar (shows "TX" when in transaction)
- Query history: Up/Down arrow to recall previous queries
- Save query: `:save <name>` command
- Load query: `:load <name>` command
- Export results: `:export csv <file>` or `:export json <file>`
- Error display with helpful messages
- Query timeout (configurable, default 30 seconds)

**Spec-Kit Command**:
```bash
/speckit.specify Implement interactive SQL Editor with full-screen multi-line editor, syntax highlighting using chroma library, and query execution capabilities. Display results in paginated tables supporting large result sets. Provide transaction management (BEGIN, COMMIT, ROLLBACK) with transaction state indicator. Include query history with up/down arrow recall and saved queries/snippets library. Support result export to CSV and JSON formats. Prioritize P1 stories (basic query execution) over P2 (syntax highlighting, transactions) and P3 (history, snippets). Implement query timeout with configurable limit (default 30s).
```

---

### Feature 008: Configuration Viewer

**Branch**: `008-configuration-viewer`

**Purpose**: Browse and search PostgreSQL server configuration parameters.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to view all server configuration parameters to understand current settings
2. **P2**: As a DBA, I want to search configuration parameters by name or description to find specific settings
3. **P2**: As a DBA, I want to see which parameters differ from defaults to identify customizations
4. **P3**: As a DBA, I want context-sensitive help for parameters to understand their purpose

**Technical Scope**:
- Configuration view querying `pg_settings`
- Parameter table with columns: Name, Setting, Unit, Category, Description
- Search/filter by parameter name or category
- Highlight modified parameters (where setting != boot_val)
- Display parameter context (postmaster, sighup, user, etc.)
- Show parameter constraints (min, max for numeric settings)
- Read-only view (no editing to prevent accidental changes)

**Database Queries**:
- `SELECT * FROM pg_settings ORDER BY name`
- Filter queries for search functionality

**Acceptance Criteria**:
- Configuration view accessible via `8` key
- Table shows: Name, Current Value, Unit, Category, Short Description
- Search by parameter name with `/` key
- Filter by category (memory, connections, logging, etc.)
- Modified parameters highlighted in yellow
- Parameter details view (`d` key): Full description, context, min/max, default value
- Sort by name or category
- Auto-refresh every 60 seconds (config rarely changes)
- Export configuration to file: `:export config <file>`

**Spec-Kit Command**:
```bash
/speckit.specify Implement Configuration Viewer for browsing PostgreSQL server settings from pg_settings. Display parameters in sortable table with name, current value, unit, category, and description. Support search by parameter name and filter by category. Highlight modified parameters that differ from defaults. Show detailed parameter information including context, constraints (min/max), and default values. Provide read-only view to prevent accidental changes. Prioritize P1 story (viewing parameters) over P2 (search/filter) and P3 (context help). Auto-refresh every 60 seconds.
```

---

### Feature 009: Log Viewer

**Branch**: `009-log-viewer`

**Purpose**: View and filter PostgreSQL server logs in real-time.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to tail server logs in real-time to monitor database activity
2. **P2**: As a DBA, I want to filter logs by severity level (ERROR, WARNING, INFO) to focus on important messages
3. **P2**: As a DBA, I want to search logs by text pattern to find specific events
4. **P3**: As a DBA, I want to navigate logs by timestamp to review historical events

**Technical Scope**:
- Log viewer with tail capability (follow mode)
- Log parsing for PostgreSQL log format (csvlog or stderr format)
- Color coding by severity level
- Filter by log level, message pattern, timestamp range
- Scroll through historical logs
- Auto-scroll toggle (follow mode on/off)
- Log source: File system or pg_read_file() function (if permissions allow)

**Configuration Requirements**:
- PostgreSQL must have logging enabled (`log_destination`, `logging_collector`)
- Application needs read access to log directory or pg_read_file() permission

**Acceptance Criteria**:
- Log viewer accessible via `9` key
- Displays logs in reverse chronological order (newest first)
- Color coding: Red (ERROR), Yellow (WARNING), White (INFO), Gray (DEBUG)
- Follow mode: Auto-scroll to newest logs (toggle with `f` key)
- Filter by level: `:level error`, `:level warning`
- Search pattern: `/` key for regex search
- Scroll with arrow keys or `j/k`, `g/G` for top/bottom
- Show log metadata: timestamp, PID, database, user, message
- Graceful error if log access not available (show configuration instructions)
- Refresh every 1 second in follow mode

**Spec-Kit Command**:
```bash
/speckit.specify Implement Log Viewer for monitoring PostgreSQL server logs in real-time. Support tailing logs with auto-scroll (follow mode) and display in reverse chronological order. Parse PostgreSQL log format (csvlog or stderr) and color-code by severity level (ERROR, WARNING, INFO, DEBUG). Enable filtering by log level, text pattern search with regex, and timestamp-based navigation. Handle log access via file system reads or pg_read_file() function. Prioritize P1 story (basic log viewing) over P2 (filtering) and P3 (timestamp navigation). Provide clear error messages if log access unavailable with configuration guidance.
```

---

### Feature 010: Database Management Operations

**Branch**: `010-database-operations`

**Purpose**: Execute database maintenance operations (VACUUM, ANALYZE, REINDEX) and manage users/roles.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to execute VACUUM on tables to reclaim storage space
2. **P1**: As a DBA, I want to execute ANALYZE on tables to update query planner statistics
3. **P2**: As a DBA, I want to execute REINDEX on indexes to rebuild corrupted or bloated indexes
4. **P2**: As a DBA, I want to view VACUUM and autovacuum status to monitor maintenance activity
5. **P3**: As a DBA, I want to manage database users and roles to control access
6. **P3**: As a DBA, I want to grant/revoke permissions to manage security

**Technical Scope**:
- Operations menu/view for maintenance commands
- VACUUM execution (VACUUM, VACUUM FULL, VACUUM ANALYZE)
- ANALYZE execution for statistics updates
- REINDEX execution for index maintenance
- Progress tracking for long-running operations using `pg_stat_progress_vacuum`
- Autovacuum status from `pg_stat_all_tables` (last_vacuum, last_autovacuum)
- User/role management queries on `pg_roles`, `pg_user`
- Permission management (GRANT/REVOKE) with confirmation
- Read-only mode enforcement (check `--readonly` flag)

**Database Queries**:
- `VACUUM (VERBOSE, ANALYZE) <table>`
- `ANALYZE <table>`
- `REINDEX INDEX <index>` or `REINDEX TABLE <table>`
- `pg_stat_progress_vacuum` for progress tracking
- `pg_roles` for role information

**Acceptance Criteria**:
- Operations view accessible from Tables view (`x` key on selected table)
- Operation menu shows: VACUUM, VACUUM FULL, VACUUM ANALYZE, ANALYZE, REINDEX
- Confirmation dialog before executing (show estimated time/impact)
- Progress indicator for long-running operations (percentage complete)
- Operation log showing: Started, Completed, Duration, Rows affected
- Read-only mode: Disable destructive operations, show warning
- Autovacuum status in Tables view: Last Vacuum time, Last Autovacuum time
- User management: List users with roles, connection limits, permissions
- Grant/Revoke permission with role selection UI
- Error handling with actionable messages (e.g., insufficient permissions)

**Spec-Kit Command**:
```bash
/speckit.specify Implement Database Management Operations for executing maintenance tasks (VACUUM, ANALYZE, REINDEX) and managing users/roles. Provide operation menu accessible from Tables view with confirmation dialogs before execution. Track operation progress using pg_stat_progress_vacuum and display completion percentage. Show autovacuum status (last vacuum time) in Tables view. Support user/role management with viewing roles, connection limits, and permissions. Enable GRANT/REVOKE operations with confirmation. Enforce read-only mode to prevent destructive operations when --readonly flag set. Prioritize P1 stories (VACUUM, ANALYZE) over P2 (REINDEX, vacuum status) and P3 (user management).
```

---

### Feature 011: Advanced Visualizations

**Branch**: `011-visualizations`

**Purpose**: Add time-series charts, sparklines, and visual analytics for metrics trending.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want time-series graphs for key metrics (TPS, connections, cache hit) to see trends over time
2. **P2**: As a DBA, I want sparklines inline with tables to quickly see metric trends
3. **P2**: As a DBA, I want bar charts for comparative data (table sizes, query times) to identify outliers
4. **P3**: As a DBA, I want heatmaps for time-based patterns (query load by hour) to plan maintenance windows

**Technical Scope**:
- Integration with asciigraph library for ASCII charts
- Time-series data collection and storage (in-memory circular buffer)
- Sparkline generation for inline trending
- Bar chart visualization for comparative metrics
- Heatmap generation for time-based patterns
- Chart configuration (time window, aggregation, scale)
- Historical data persistence (optional, file-based storage)

**Chart Types**:
- Line charts: TPS over time, connections over time, cache hit ratio over time
- Sparklines: Replication lag trend, table growth trend
- Bar charts: Top 10 tables by size, top 10 queries by time
- Heatmaps: Query volume by hour/day, connection count by hour

**Acceptance Criteria**:
- Enhanced Dashboard with time-series graphs (1-hour window, 1-second granularity)
- Sparklines in Activity view (query duration trend per connection)
- Sparklines in Tables view (table size growth, 24-hour window)
- Bar chart in Queries view (top 10 queries by total time)
- Configurable time windows: 1m, 5m, 15m, 1h, 24h
- Chart refresh with view refresh (real-time updates)
- Memory limit for historical data (max 10,000 data points per metric)
- Optional persistence to disk for historical analysis
- Chart toggle: `v` key to show/hide visualizations

**Spec-Kit Command**:
```bash
/speckit.specify Implement Advanced Visualizations using asciigraph library for ASCII-based charts. Add time-series line graphs for key metrics (TPS, connections, cache hit ratio) with configurable time windows (1m, 5m, 1h, 24h). Generate sparklines for inline trending in Activity and Tables views showing query duration and table growth. Create bar charts for comparative analysis (top tables by size, top queries by time). Support in-memory circular buffer for historical data with memory limits and optional disk persistence. Prioritize P1 story (time-series graphs) over P2 (sparklines, bar charts) and P3 (heatmaps). Provide chart toggle to show/hide visualizations.
```

---

### Feature 012: Alert System

**Branch**: `012-alert-system`

**Purpose**: Configure threshold-based alerts for critical metrics with visual indicators and history.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want to configure alerts for critical metrics (replication lag, connection limits) to be notified of issues
2. **P1**: As a DBA, I want visual indicators when alerts trigger to quickly identify problems
3. **P2**: As a DBA, I want alert history to review past incidents
4. **P2**: As a DBA, I want to acknowledge alerts to track resolution status
5. **P3**: As a DBA, I want custom alert rules with complex conditions to monitor specific scenarios

**Technical Scope**:
- Alert configuration in YAML config file
- Alert rule engine evaluating conditions on metric updates
- Alert states: Normal, Warning, Critical
- Visual indicators: Color changes, icons, status bar notifications
- Alert panel showing active alerts with severity and message
- Alert history log (timestamped events)
- Alert acknowledgment tracking
- Alert conditions: threshold comparisons, rate of change, time-based triggers

**Alert Examples**:
- Replication lag > 100MB (Warning)
- Active connections > 80% of max_connections (Critical)
- Cache hit ratio < 90% (Warning)
- Long-running transaction > 5 minutes (Warning)
- Disk space < 10% free (Critical)

**Acceptance Criteria**:
- Alert configuration in `~/.config/steep/config.yaml` under `alerts:` section
- Alert panel in Dashboard showing active alerts (color-coded by severity)
- Status bar shows alert count: "⚠ 2 Warnings, ❌ 1 Critical"
- Alert evaluation every refresh cycle (1-5 seconds depending on metric)
- Alert history view (`a` key): Timestamp, Metric, Condition, State, Acknowledged
- Acknowledge alert with `Enter` key (marks as acknowledged, stays in history)
- Alert notification sound (optional, configurable)
- Custom alert rules: Support conditions like `replication_lag_bytes > 100MB`, `active_connections / max_connections > 0.8`
- Alert rule validation on startup (invalid rules logged as warnings)

**Spec-Kit Command**:
```bash
/speckit.specify Implement Alert System for threshold-based monitoring with visual indicators and history tracking. Support alert configuration in YAML with conditions for critical metrics (replication lag, connection limits, cache hit ratio, long transactions). Evaluate alert rules on each metric refresh cycle and update alert states (Normal, Warning, Critical). Display active alerts in Dashboard with color-coded severity indicators and status bar alert counts. Provide alert history view with timestamp, metric, condition, state, and acknowledgment status. Enable alert acknowledgment to track resolution. Prioritize P1 stories (basic alerts, visual indicators) over P2 (history, acknowledgment) and P3 (custom complex rules). Validate alert rules on startup.
```

---

## Best Practices

### Spec-Kit Workflow Discipline

1. **Always run `/speckit.clarify` before `/speckit.plan`**
   - Identify underspecified areas early
   - Ask targeted questions to fill gaps
   - Update spec.md with clarifications before planning

2. **Validate plan.md before generating tasks**
   - Review Technical Context section for accuracy
   - Check Constitution compliance
   - Verify project structure matches Go conventions

3. **Use `/speckit.analyze` after `/speckit.tasks`**
   - Verify task breakdown aligns with user stories
   - Check for missing dependencies or circular references
   - Ensure task ordering respects constitution principles

4. **Commit after each major milestone**
   - After spec.md is finalized (post-clarification)
   - After plan.md is validated
   - After tasks.md is generated
   - After implementation is complete and tested

### Constitution Compliance

Every feature must adhere to the constitution principles:

- **Real-Time First**: Implement auto-refresh with configurable intervals
- **Keyboard-Driven**: All actions accessible via keyboard shortcuts
- **Query Efficiency**: Review SQL queries for performance before implementation
- **Incremental Delivery**: Each feature independently testable and valuable
- **Comprehensive Coverage**: Avoid feature gaps that require context switching

### Testing Strategy

For each feature:

1. **Unit Tests**: Business logic, data parsing, calculations
2. **Integration Tests**: Database queries against real PostgreSQL (use testcontainers)
3. **Manual UI Tests**: Keyboard navigation, view rendering, error handling
4. **Performance Tests**: Query execution time, memory usage, render latency

### Performance Validation

Before completing each feature, validate:

- Query execution time: < 500ms for standard operations
- Memory footprint: < 50MB for typical usage
- View switching: < 100ms latency
- Auto-refresh impact: < 5% CPU when idle

### Error Handling

Implement robust error handling:

- Graceful degradation when extensions unavailable
- Clear error messages with actionable guidance
- Retry logic for transient connection failures
- Fallback displays when data unavailable

---

## Success Criteria

### MVP (P1 Features Complete)

- [ ] Application launches and connects to PostgreSQL
- [ ] Dashboard shows real-time key metrics (TPS, connections, cache hit)
- [ ] Activity view displays active connections with sorting/filtering
- [ ] Queries view shows top queries from pg_stat_statements
- [ ] Locks view identifies blocking queries
- [ ] Tables view browses schema hierarchy with statistics
- [ ] Replication view monitors lag and slot status
- [ ] All views support keyboard navigation
- [ ] Auto-refresh with configurable intervals
- [ ] Performance goals met: < 500ms queries, < 50MB memory

### Production Ready (All Features Complete)

- [ ] All P1, P2, and P3 features implemented
- [ ] SQL Editor with syntax highlighting and transaction management
- [ ] Configuration and Log viewers operational
- [ ] Database operations (VACUUM, ANALYZE, REINDEX) functional
- [ ] Advanced visualizations (charts, sparklines, heatmaps)
- [ ] Alert system with configurable rules and history
- [ ] Comprehensive test coverage (unit, integration, UI)
- [ ] Documentation complete (README, user guide, developer guide)
- [ ] Performance benchmarks validated
- [ ] Security audit passed (no credential leakage, safe SQL execution)

---

## Appendix: Feature Dependencies Matrix

| Feature | Depends On | Enables |
|---------|------------|---------|
| 001 - Foundation | None | All features |
| 002 - Dashboard & Activity | 001 | - |
| 003 - Query Performance | 001 | - |
| 004 - Locks & Blocking | 001 | - |
| 005 - Tables & Statistics | 001 | 010 (Operations) |
| 006 - Replication | 001 | - |
| 007 - SQL Editor | 001 | 010 (User management queries) |
| 008 - Configuration Viewer | 001 | - |
| 009 - Log Viewer | 001 | - |
| 010 - Database Operations | 001, 005, 007 | - |
| 011 - Advanced Visualizations | 001-006 | - |
| 012 - Alert System | 001-006 | - |

---

**Document Version**: 1.0
**Last Updated**: 2025-11-19
**Status**: Implementation Roadmap
**Maintained By**: Steep Development Team
