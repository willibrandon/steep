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

Steep will be built through **13 distinct features** (including 006.5), each following the complete spec-kit workflow:

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
        └── 006.5-service-architecture (steep-agent daemon)
            └── 007-sql-editor (Interactive queries)
                ├── 008-configuration-viewer (Config display)
                ├── 009-log-viewer (Log display)
                └── 010-database-operations (Management)
                    ├── 011-advanced-visualizations (Charts)
                    └── 012-alert-system (Alerting)
```

### Priority Classification

- **P1 (Must-Have)**: Features 001-006, 006.5 - Core monitoring MVP + Service Architecture
- **P2 (Should-Have)**: Features 007-010 - Management and operations
- **P3 (Nice-to-Have)**: Features 011-012 - Advanced analytics

---

## Implementation Sequence

### Phase 1: Foundation & Core Monitoring (P1)

**Estimated Timeline**: 5-7 weeks for P1 features

| Feature | Branch | Dependencies | Estimated Effort |
|---------|--------|--------------|------------------|
| 001 - Foundation & Infrastructure | `001-foundation` | None | 1 week |
| 002 - Dashboard & Activity Monitoring | `002-dashboard-activity` | 001 | 1 week |
| 003 - Query Performance Monitoring | `003-query-performance` | 001 | 1 week |
| 004 - Locks & Blocking Detection | `004-locks-blocking` | 001 | 1 week |
| 005 - Tables & Statistics Viewer | `005-tables-statistics` | 001 | 1 week |
| 006 - Replication Monitoring | `006-replication-monitoring` | 001 | 1 week |
| 006.5 - Service Architecture | `006-5-service-architecture` | 001-006 | 1 week |

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

**UI Architecture**:
- **Framework**: Bubbletea TUI with lipgloss styling (consistent with Query Performance and Locks views)
- **View Structure**: `internal/ui/views/tables/view.go` implementing `ViewModel` interface
- **Components**: Reuse existing components from `internal/ui/components/` (StatusBar, HelpText)
- **Styles**: Use centralized styles from `internal/ui/styles/` for consistent color coding
- **Layout**: Status bar (top) → Title → Tree/Table content → Footer with keyboard hints
- **Modes**: ModeNormal, ModeDetails, ModeConfirmInstall, ModeConfirmAction, ModeHelp
- **Message Types**: TablesDataMsg, InstallExtensionMsg, InstallExtensionResultMsg, RefreshTablesMsg

**Styling Guidelines**:
- Match visual style of Query Performance (`internal/ui/views/queries/`) and Locks (`internal/ui/views/locks/`) views
- Use `lipgloss.JoinVertical` for composing view layout
- Dynamic height calculation accounting for fixed components (status bar, title, footer)
- Color coding: Yellow for warnings (unused indexes), Red for critical (high bloat >20%), Green for healthy
- Confirmation dialogs with `renderWithOverlay()` pattern for destructive actions
- Toast messages for operation results (success/failure)
- Spinner animation for async operations (extension install, VACUUM progress)

**Libraries**:
- `github.com/charmbracelet/bubbletea` - TUI framework, message passing, tea.Cmd pattern
- `github.com/charmbracelet/bubbles/spinner` - Loading indicators for async operations
- `github.com/charmbracelet/lipgloss` - Styling, layout composition, color theming
- `github.com/jackc/pgx/v5/pgxpool` - PostgreSQL connection pooling
- `github.com/xlab/treeprint` - ASCII tree rendering for hierarchy (optional)

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
- Sort tables by size, bloat, or cache hit ratio (`s`/`S` keys like other views)
- Auto-refresh every 30 seconds (static data, slower refresh)
- Graceful degradation: Skip bloat if pgstattuple not available
- Prompt to install pgstattuple if not available with confirmation dialog
- Show clear error if extension install fails (insufficient privileges)
- Help overlay accessible via `h` key (consistent with other views)
- Copy table/index name to clipboard with `y` key

**Spec-Kit Command**:
```bash
/speckit.specify Implement Tables and Statistics Viewer with hierarchical database browser (Database → Schema → Table). Display table statistics including size, row count, bloat percentage, and cache hit ratio from pg_stat_all_tables. Show index usage statistics with scan counts and unused index detection from pg_stat_all_indexes. Include table details panel showing columns, constraints, and foreign keys. Support bloat estimation using pgstattuple extension with graceful fallback. Auto-install pgstattuple via CREATE EXTENSION with user confirmation if not available. Build using Bubbletea framework with lipgloss styling matching Query Performance and Locks views. Reuse existing UI components (StatusBar, confirmation dialogs, toast messages). Implement standard keyboard navigation (j/k, s/S sort, h help, y yank). Use color coding for warnings (yellow) and critical states (red). Prioritize all P1 stories (browsing and viewing table stats) and P2 (bloat detection, auto-install extension). Use auto-refresh every 30 seconds. Queries must execute under 500ms.
```

---

### Feature 006: Replication Monitoring

**Branch**: `006-replication-monitoring`

**Purpose**: Monitor PostgreSQL replication status, lag metrics, WAL streaming, and provide rich visual feedback for both physical and logical replication. Additionally, provide guided setup wizards to help developers and DBAs quickly configure and start replication. This feature establishes the foundation for future multi-master replication system integration.

**User Stories** (Priority Order):

*Monitoring Stories:*
1. **P1**: As a DBA, I want to see replication lag (bytes and time) to ensure replicas are synchronized
2. **P1**: As a DBA, I want to see replication slot status to monitor replication health
3. **P1**: As a DBA, I want to visualize the replication topology to understand primary/replica relationships
4. **P2**: As a DBA, I want to see WAL sender/receiver statistics to understand replication throughput
5. **P2**: As a DBA, I want to visualize replication timeline to see lag trends over time
6. **P2**: As a DBA, I want to see WAL pipeline stages (sent → write → flush → replay) per replica
7. **P2**: As a DBA, I want to monitor logical replication subscriptions and publications
8. **P3**: As a DBA, I want to manage replication slots (advance, drop) to prevent WAL buildup
9. **P3**: As a DBA, I want historical lag data for trend analysis and capacity planning

*Setup & Configuration Stories:*
10. **P1**: As a DBA, I want to check if my PostgreSQL is configured for replication to understand what changes are needed
11. **P1**: As a DBA, I want a guided wizard to set up physical streaming replication to quickly add replicas
12. **P2**: As a DBA, I want to create replication users with proper privileges to secure my replication setup
13. **P2**: As a DBA, I want to generate pg_basebackup commands to provision new replicas
14. **P2**: As a DBA, I want a guided wizard to set up logical replication (publications/subscriptions)
15. **P2**: As a DBA, I want to generate connection strings (primary_conninfo) for replica configuration
16. **P3**: As a developer, I want to set up a local replication environment for testing
17. **P3**: As a DBA, I want to validate my replication configuration before going live

**Technical Scope**:

*Monitoring:*
- Replication view querying `pg_stat_replication` for sender statistics
- Replication slot information from `pg_replication_slots` (physical and logical)
- WAL statistics from `pg_stat_wal_receiver` and `pg_stat_archiver`
- Logical replication from `pg_publication`, `pg_subscription`, `pg_stat_subscription`
- Lag calculation (byte lag and time lag) using `pg_wal_lsn_diff()`
- Historical lag data persistence in SQLite for trend analysis
- Slot management actions (advance, drop with confirmation)
- Data model designed for future bi-directional/multi-master replication

*Setup & Configuration:*
- Configuration checker validating replication-readiness (wal_level, max_wal_senders, etc.)
- Replication user creation with REPLICATION privilege
- Physical replication slot creation via `pg_create_physical_replication_slot()`
- Logical replication slot creation via `pg_create_logical_replication_slot()`
- pg_basebackup command generation with progress monitoring
- primary_conninfo connection string builder
- pg_hba.conf entry generation for replication access
- Publication/subscription wizard for logical replication
- Configuration validation and pre-flight checks

**UI Architecture**:
- **Framework**: Bubbletea TUI with lipgloss styling (consistent with other views)
- **View Structure**: `internal/ui/views/replication/view.go` implementing `ViewModel` interface
- **Custom Visualizations**: `internal/ui/views/replication/repviz/` package for specialized components
- **Setup Wizards**: `internal/ui/views/replication/setup/` package using `huh` forms library
- **Layout**: Split view - Topology (left 40%) | Details (right 60%)
- **Modes**: ModeNormal, ModeDetails, ModeTopology, ModeSlotManage, ModeSetup, ModeConfigCheck, ModeHelp

**Visual Components**:

1. **REPLICATION TOPOLOGY PANEL** (using treeprint)
   ```
   ┌─────────────────── REPLICATION TOPOLOGY ───────────────────┐
   │                      ┌──────────┐                          │
   │                      │ PRIMARY  │                          │
   │                      │ pg-main  │                          │
   │                      └────┬─────┘                          │
   │               ┌──────────┼──────────┐                      │
   │               ▼          ▼          ▼                      │
   │         ┌─────────┐ ┌─────────┐ ┌─────────┐               │
   │         │REPLICA 1│ │REPLICA 2│ │REPLICA 3│               │
   │         │ sync ●  │ │ async ● │ │ async ● │               │
   │         │ 0 bytes │ │ 1.2 MB  │ │ 45 MB   │               │
   │         └─────────┘ └────┬────┘ └─────────┘               │
   │                          ▼                                 │
   │                    ┌─────────┐                             │
   │                    │REPLICA 4│  (cascading)                │
   │                    │ async ● │                             │
   │                    └─────────┘                             │
   └────────────────────────────────────────────────────────────┘
   ```
   - ASCII tree showing primary → replica relationships
   - Support for cascading replication (replica → replica chains)
   - Node labels: hostname/app_name, sync state, lag indicator
   - Color coding: Green (healthy), Yellow (lagging), Red (disconnected)

2. **WAL PIPELINE VISUALIZATION** (custom repviz component, inspired by deadviz)
   ```
   ┌─────────────────── WAL PIPELINE: replica-1 ───────────────────┐
   │                                                                │
   │  Sent        Write       Flush       Replay                   │
   │  ┌───┐       ┌───┐       ┌───┐       ┌───┐                   │
   │  │ ● │ ───▶  │ ● │ ───▶  │ ● │ ───▶  │ ○ │                   │
   │  └───┘       └───┘       └───┘       └───┘                   │
   │  0/5A000     0/5A000     0/59F00     0/59000                 │
   │              ════════════════════════▶                        │
   │                     Lag: 4 KB                                 │
   └───────────────────────────────────────────────────────────────┘
   ```
   - Per-replica horizontal pipeline showing LSN positions
   - Visual progress indicators (●/○) for each stage
   - Byte lag displayed between stages
   - Color gradient based on lag severity

3. **LAG SPARKLINES** (using asciigraph)
   ```
   replica-1  [▁▁▂▂▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▂]  0 bytes    ●
   replica-2  [▁▂▃▃▂▂▃▄▅▅▄▃▃▂▂▃▃▄▄▃▃▂▂▂▃▃▄▅▆]  1.2 MB    ●
   replica-3  [▃▅▆▇▇▆▆▇█▇▇▆▆▇▇█▇▇▆▅▅▆▇▇█▇▇▆▅▅]  45 MB     ●
              └──────── 60 seconds ────────┘
   ```
   - Per-replica sparkline showing lag history (60-second window, 1s intervals)
   - Unicode block characters (▁▂▃▄▅▆▇█) for compact display
   - Threshold markers for warning/critical levels
   - Historical data from SQLite for extended time windows (5m, 15m, 1h, 24h)

4. **REPLICATION SLOT STATUS** (using bubbles/progress)
   ```
   ┌─────────────────── REPLICATION SLOTS ───────────────────┐
   │                                                          │
   │  Slot Name          Type      Active   WAL Retained     │
   │  ─────────────────────────────────────────────────────  │
   │  replica_slot_1     physical  ● Yes    ████░░░░  2 GB   │
   │  replica_slot_2     physical  ● Yes    ██░░░░░░  0.5 GB │
   │  logical_slot_1     logical   ○ No     ████████  8 GB   │ ← Warning!
   │                                                          │
   └──────────────────────────────────────────────────────────┘
   ```
   - Progress bar showing WAL retention percentage
   - Physical vs logical slot distinction
   - Color-coded: Green (<50%), Yellow (50-80%), Red (>80% or inactive)
   - Warning indicators for stale/inactive slots

5. **SYNC STATE INDICATORS**
   - Visual icons: `● sync` `◐ async` `◑ potential` `○ quorum` `✗ disconnected`
   - Streaming vs catchup mode indication
   - Connection state (streaming, backup, catchup)

6. **LOGICAL REPLICATION PANEL**
   ```
   ┌─────────────────── LOGICAL REPLICATION ───────────────────┐
   │  Publications:                                             │
   │    pub_orders (3 tables) → 2 subscribers                  │
   │    pub_users (1 table) → 1 subscriber                     │
   │                                                            │
   │  Subscriptions:                                            │
   │    sub_analytics ← upstream:5432  ● enabled  lag: 120 ms  │
   │    sub_reporting ← upstream:5432  ● enabled  lag: 45 ms   │
   └────────────────────────────────────────────────────────────┘
   ```

7. **CONFIGURATION CHECKER PANEL**
   ```
   ┌─────────────────── REPLICATION READINESS CHECK ───────────────────┐
   │                                                                    │
   │  PostgreSQL Configuration:                                        │
   │  ──────────────────────────────────────────────────────────────   │
   │  ● wal_level              replica     ✓ Ready (replica or logical)│
   │  ● max_wal_senders        10          ✓ Ready (> 0)               │
   │  ● max_replication_slots  10          ✓ Ready (> 0)               │
   │  ○ wal_keep_size          0           ⚠ Consider setting > 0      │
   │  ● hot_standby            on          ✓ Ready                     │
   │  ○ archive_mode           off         ⚠ Recommended for DR        │
   │                                                                    │
   │  Authentication (pg_hba.conf):                                    │
   │  ──────────────────────────────────────────────────────────────   │
   │  ○ Replication entry      not found   ✗ Required for replicas     │
   │                                                                    │
   │  Replication User:                                                │
   │  ──────────────────────────────────────────────────────────────   │
   │  ○ repl_user              not found   ✗ Create with [u] key       │
   │                                                                    │
   │  Overall Status: ⚠ PARTIALLY READY - 2 issues to resolve          │
   │                                                                    │
   │  [u] Create User  [s] Setup Wizard  [g] Generate pg_hba entry     │
   └────────────────────────────────────────────────────────────────────┘
   ```

8. **PHYSICAL REPLICATION SETUP WIZARD** (using huh forms)
   ```
   ┌─────────────────── SETUP PHYSICAL REPLICATION ───────────────────┐
   │                                                                   │
   │  Step 1 of 5: Primary Server Configuration                       │
   │  ═══════════════════════════════════════════                     │
   │                                                                   │
   │  This wizard will help you configure streaming replication.      │
   │                                                                   │
   │  ┌─────────────────────────────────────────────────────────────┐ │
   │  │ Replication User                                             │ │
   │  │ ┌─────────────────────────────────────────────────────────┐ │ │
   │  │ │ repl_user                                                │ │ │
   │  │ └─────────────────────────────────────────────────────────┘ │ │
   │  │ Username for replication connections                        │ │
   │  └─────────────────────────────────────────────────────────────┘ │
   │                                                                   │
   │  ┌─────────────────────────────────────────────────────────────┐ │
   │  │ Synchronous Mode                                             │ │
   │  │ ○ Asynchronous (default, best performance)                  │ │
   │  │ ● Synchronous (data safety, higher latency)                 │ │
   │  └─────────────────────────────────────────────────────────────┘ │
   │                                                                   │
   │  ┌─────────────────────────────────────────────────────────────┐ │
   │  │ Number of Replicas                                           │ │
   │  │ ┌───┐                                                        │ │
   │  │ │ 2 │                                                        │ │
   │  │ └───┘                                                        │ │
   │  └─────────────────────────────────────────────────────────────┘ │
   │                                                                   │
   │         [←] Back    [Enter] Continue    [Esc] Cancel             │
   └───────────────────────────────────────────────────────────────────┘
   ```

9. **PG_BASEBACKUP COMMAND GENERATOR**
   ```
   ┌─────────────────── REPLICA PROVISIONING ───────────────────┐
   │                                                             │
   │  Generated pg_basebackup command:                          │
   │  ┌─────────────────────────────────────────────────────────┐
   │  │ pg_basebackup \                                         │
   │  │   --host=primary.example.com \                          │
   │  │   --port=5432 \                                         │
   │  │   --username=repl_user \                                │
   │  │   --pgdata=/var/lib/postgresql/16/main \                │
   │  │   --wal-method=stream \                                 │
   │  │   --write-recovery-conf \                               │
   │  │   --slot=replica_slot_1 \                               │
   │  │   --create-slot \                                       │
   │  │   --checkpoint=fast \                                   │
   │  │   --progress \                                          │
   │  │   --verbose                                             │
   │  └─────────────────────────────────────────────────────────┘
   │                                                             │
   │  Recovery configuration (auto-generated):                  │
   │  ┌─────────────────────────────────────────────────────────┐
   │  │ primary_conninfo = 'host=primary.example.com port=5432  │
   │  │   user=repl_user password=*** application_name=replica1'│
   │  │ primary_slot_name = 'replica_slot_1'                    │
   │  └─────────────────────────────────────────────────────────┘
   │                                                             │
   │  [y] Copy to clipboard  [Enter] Execute  [Esc] Cancel      │
   └─────────────────────────────────────────────────────────────┘
   ```

10. **LOGICAL REPLICATION SETUP WIZARD**
    ```
    ┌─────────────────── SETUP LOGICAL REPLICATION ───────────────────┐
    │                                                                  │
    │  Step 2 of 4: Select Tables for Publication                     │
    │  ═══════════════════════════════════════════                    │
    │                                                                  │
    │  Publication Name: pub_analytics                                │
    │                                                                  │
    │  Select tables to include:                                      │
    │  ┌────────────────────────────────────────────────────────────┐ │
    │  │ Schema: public                                              │ │
    │  │ ────────────────────────────────────────────────────────── │ │
    │  │ [x] orders              1.2 GB    50M rows                 │ │
    │  │ [x] order_items         800 MB    120M rows                │ │
    │  │ [ ] order_archive       5.0 GB    200M rows   (large!)     │ │
    │  │ [x] customers           200 MB    2M rows                  │ │
    │  │ [ ] audit_log           3.0 GB    500M rows   (large!)     │ │
    │  │                                                             │ │
    │  │ Schema: analytics                                           │ │
    │  │ ────────────────────────────────────────────────────────── │ │
    │  │ [x] daily_metrics       50 MB     365 rows                 │ │
    │  │ [x] user_sessions       100 MB    1M rows                  │ │
    │  └────────────────────────────────────────────────────────────┘ │
    │                                                                  │
    │  Selected: 5 tables (2.35 GB, 173M rows)                        │
    │  ⚠ Large tables may take significant time for initial sync     │
    │                                                                  │
    │         [Space] Toggle  [a] All  [n] None  [Enter] Continue     │
    └──────────────────────────────────────────────────────────────────┘
    ```

11. **CONNECTION STRING BUILDER**
    ```
    ┌─────────────────── CONNECTION STRING BUILDER ───────────────────┐
    │                                                                  │
    │  Build primary_conninfo for replica configuration:              │
    │                                                                  │
    │  Host:           ┌────────────────────────────────────────────┐ │
    │                  │ primary.example.com                         │ │
    │                  └────────────────────────────────────────────┘ │
    │  Port:           ┌────────┐                                     │
    │                  │ 5432   │                                     │
    │                  └────────┘                                     │
    │  User:           ┌────────────────────────────────────────────┐ │
    │                  │ repl_user                                   │ │
    │                  └────────────────────────────────────────────┘ │
    │  Application:    ┌────────────────────────────────────────────┐ │
    │                  │ replica_east_1                              │ │
    │                  └────────────────────────────────────────────┘ │
    │  SSL Mode:       ○ disable  ● prefer  ○ require  ○ verify-full │
    │                                                                  │
    │  Generated:                                                     │
    │  ┌────────────────────────────────────────────────────────────┐ │
    │  │ primary_conninfo = 'host=primary.example.com port=5432     │ │
    │  │   user=repl_user sslmode=prefer application_name=replica_e │ │
    │  │   ast_1'                                                    │ │
    │  └────────────────────────────────────────────────────────────┘ │
    │                                                                  │
    │  [y] Copy  [t] Test Connection  [Enter] Apply  [Esc] Cancel    │
    └──────────────────────────────────────────────────────────────────┘
    ```

**Libraries**:
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles/progress` - Progress bars for slot retention
- `github.com/charmbracelet/bubbles/spinner` - Loading indicators
- `github.com/charmbracelet/lipgloss` - Styling, box drawing, layout
- `github.com/charmbracelet/huh` - Form inputs for setup wizards
- `github.com/xlab/treeprint` - Topology tree rendering
- `github.com/guptarohit/asciigraph` - Sparklines and time-series charts
- `github.com/fatih/color` - Color formatting (consistent with deadviz)
- `github.com/mattn/go-sqlite3` - Historical lag data persistence
- `github.com/sethvargo/go-password` - Secure password generation for replication users

**Database Queries**:

*Monitoring:*
- `pg_stat_replication` for sender statistics (sent_lsn, write_lsn, flush_lsn, replay_lsn)
- `pg_replication_slots` for slot information (physical and logical)
- `pg_stat_wal_receiver` for receiver statistics on replicas
- `pg_stat_archiver` for WAL archiving status
- `pg_publication` and `pg_publication_tables` for logical publications
- `pg_subscription` and `pg_stat_subscription` for logical subscriptions
- `pg_wal_lsn_diff()` for byte lag calculation
- `pg_last_wal_receive_lsn()`, `pg_last_wal_replay_lsn()` for replica-side stats

*Setup & Configuration:*
- `pg_settings` for configuration parameters (wal_level, max_wal_senders, max_replication_slots, etc.)
- `pg_hba_file_rules` (PG15+) for pg_hba.conf inspection
- `pg_read_file()` for pg_hba.conf on older versions
- `pg_roles` to check/create replication users
- `CREATE ROLE ... WITH REPLICATION LOGIN PASSWORD ...` for user creation
- `pg_create_physical_replication_slot()` for physical slots
- `pg_create_logical_replication_slot()` for logical slots
- `pg_drop_replication_slot()` for slot cleanup
- `pg_replication_slot_advance()` for slot management
- `ALTER SYSTEM SET ...` for configuration changes (requires superuser)
- `pg_reload_conf()` to apply configuration without restart
- `CREATE PUBLICATION ... FOR TABLE ...` for logical replication
- `CREATE SUBSCRIPTION ... CONNECTION ... PUBLICATION ...` for subscriptions
- `pg_replication_origin_create()` for replication origins (future multi-master)

**Data Model** (SQLite persistence):
```sql
CREATE TABLE replication_lag_history (
    id INTEGER PRIMARY KEY,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    replica_name TEXT NOT NULL,
    sent_lsn TEXT,
    write_lsn TEXT,
    flush_lsn TEXT,
    replay_lsn TEXT,
    byte_lag INTEGER,
    time_lag_ms INTEGER,
    sync_state TEXT,
    -- Future multi-master fields
    direction TEXT DEFAULT 'outbound',  -- 'outbound' | 'inbound' for bi-directional
    conflict_count INTEGER DEFAULT 0
);

CREATE INDEX idx_lag_history_time ON replication_lag_history(timestamp, replica_name);
```

**Acceptance Criteria**:

*Monitoring:*
- Replication view accessible via `6` key
- Topology panel shows primary/replica relationships with cascading support
- Replica table shows: Application Name, State, Sent/Write/Flush/Replay LSN, Byte Lag, Time Lag
- WAL pipeline visualization per selected replica
- Lag sparklines with configurable time windows (1m, 5m, 15m, 1h)
- Replication slot table with WAL retention progress bars
- Logical replication panel showing publications and subscriptions
- Sort replicas by lag, name, or state (`s`/`S` keys)
- Color coding: Green (lag < 1MB), Yellow (1-10MB), Red (>10MB)
- Sync state visual indicators matching PostgreSQL states
- Historical lag data stored in SQLite (retention: 24 hours default)
- Graceful handling when replication not configured
- Auto-detect primary vs replica role and show appropriate stats
- Toggle between overview and detail modes (`Enter`/`d`)
- Help overlay with keybindings (`h`)
- Auto-refresh every 2 seconds
- Performance: Queries execute under 500ms

*Setup & Configuration:*
- Configuration checker panel shows replication readiness (`c` key)
- Check validates: wal_level, max_wal_senders, max_replication_slots, wal_keep_size, hot_standby, archive_mode
- Check validates pg_hba.conf for replication entries (PG15+ uses pg_hba_file_rules)
- Check validates replication user exists with proper privileges
- Visual status indicators: ✓ Ready, ⚠ Warning, ✗ Required
- Overall status summary with issue count
- Physical replication setup wizard (`w` key) with multi-step form
- Wizard Step 1: Primary configuration (replication user, sync mode, replica count)
- Wizard Step 2: Replication slot creation (name, type)
- Wizard Step 3: pg_hba.conf entry generation
- Wizard Step 4: pg_basebackup command generation
- Wizard Step 5: Review and execute/copy commands
- Logical replication setup wizard (`W` key) with multi-step form
- Logical Step 1: Publication name and type (FOR TABLE vs FOR ALL TABLES)
- Logical Step 2: Table selection with size/row counts
- Logical Step 3: Subscriber connection details
- Logical Step 4: Subscription creation with options
- Replication user creation with secure password generation (`u` key)
- Connection string builder for primary_conninfo (`b` key)
- Connection string builder validates with test connection option
- Copy generated commands to clipboard (`y` key in setup panels)
- Read-only mode disables all setup/modification operations
- Confirmation dialogs for destructive operations (slot drop, user drop)
- Setup operations require superuser privileges (graceful error if not)

**Keyboard Navigation**:

*Monitoring:*
- `j/k` or `↑/↓`: Navigate between replicas
- `Enter` or `d`: Show replica details (WAL pipeline, extended stats)
- `t`: Toggle topology view
- `l`: Toggle logical replication panel
- `s`: Cycle sort column (lag, name, state)
- `S`: Toggle sort direction
- `1-4`: Switch sparkline time window (1m, 5m, 15m, 1h)
- `x`: Manage slot (drop with confirmation, requires non-readonly mode)
- `y`: Copy replica info to clipboard
- `R`: Force refresh
- `h` or `?`: Show help overlay
- `Esc`: Close overlay/details

*Setup & Configuration:*
- `c`: Open configuration checker panel
- `w`: Open physical replication setup wizard
- `W`: Open logical replication setup wizard
- `u`: Create replication user (in config checker or wizard)
- `b`: Open connection string builder
- `g`: Generate pg_hba.conf entry (in config checker)
- `Tab`: Next field (in wizards/forms)
- `Shift+Tab`: Previous field (in wizards/forms)
- `Space`: Toggle selection (in table picker)
- `a`: Select all tables (in logical replication wizard)
- `n`: Select none (in logical replication wizard)
- `Enter`: Continue to next step (in wizards)
- `←`: Back to previous step (in wizards)
- `y`: Copy generated command to clipboard (in setup panels)
- `t`: Test connection (in connection string builder)
- `Esc`: Cancel wizard/close setup panel

**Spec-Kit Command**:
```bash
/speckit.specify Implement Replication Monitoring and Setup view for tracking PostgreSQL streaming replication with rich visual feedback for both physical and logical replication, plus guided setup wizards for configuring and starting replication.

**Data Sources:**

*Monitoring:*
Query pg_stat_replication for sender statistics (sent_lsn, write_lsn, flush_lsn, replay_lsn), pg_replication_slots for slot status (physical and logical), pg_stat_wal_receiver for receiver statistics, pg_publication/pg_subscription for logical replication, and pg_wal_lsn_diff() for byte lag calculation.

*Setup & Configuration:*
Query pg_settings for configuration parameters (wal_level, max_wal_senders, max_replication_slots, wal_keep_size, hot_standby, archive_mode). Use pg_hba_file_rules (PG15+) or pg_read_file() for pg_hba.conf inspection. Query pg_roles for replication user validation. Use pg_create_physical_replication_slot(), pg_create_logical_replication_slot(), ALTER SYSTEM SET, pg_reload_conf(), CREATE PUBLICATION, CREATE SUBSCRIPTION for setup operations.

**Visual Components:**

*Monitoring (6 components):*

1. REPLICATION TOPOLOGY PANEL using treeprint library:
   - ASCII tree diagram showing primary → replica relationships
   - Support cascading replication (replica → replica chains)
   - Node labels with hostname/app_name, sync state (sync/async/potential), lag indicator
   - Color coding: Green (healthy <1MB), Yellow (lagging 1-10MB), Red (>10MB or disconnected)

2. WAL PIPELINE VISUALIZATION (custom repviz package inspired by deadviz):
   - Per-replica horizontal pipeline showing: Sent → Write → Flush → Replay LSN positions
   - Visual progress indicators (●/○) for each stage completion
   - Byte lag displayed between stages with color gradient

3. LAG SPARKLINES using asciigraph library:
   - Per-replica sparkline showing lag history with Unicode blocks (▁▂▃▄▅▆▇█)
   - Configurable time windows: 60s (default), 5m, 15m, 1h
   - Historical data persisted in SQLite for extended analysis

4. REPLICATION SLOT STATUS using bubbles/progress:
   - Progress bars showing WAL retention percentage of max_slot_wal_keep_size
   - Physical vs logical slot distinction
   - Color-coded warnings for inactive or high-retention slots

5. LOGICAL REPLICATION PANEL:
   - Publications with table counts and subscriber counts
   - Subscriptions with upstream connection, enabled status, and lag

6. SYNC STATE INDICATORS:
   - Visual icons: ● sync, ◐ async, ◑ potential, ○ quorum, ✗ disconnected
   - Streaming vs catchup mode indication

*Setup & Configuration (5 components):*

7. CONFIGURATION CHECKER PANEL:
   - Validates PostgreSQL replication-readiness parameters
   - Checks: wal_level (replica/logical), max_wal_senders (>0), max_replication_slots (>0), wal_keep_size, hot_standby, archive_mode
   - Checks pg_hba.conf for replication entries
   - Validates replication user exists with REPLICATION privilege
   - Visual indicators: ✓ Ready (green), ⚠ Warning (yellow), ✗ Required (red)
   - Overall status summary with actionable issue list

8. PHYSICAL REPLICATION SETUP WIZARD using huh forms library:
   - Step 1: Primary configuration (replication user, synchronous/asynchronous mode, replica count)
   - Step 2: Replication slot creation (slot name, type)
   - Step 3: pg_hba.conf entry generation (host/hostssl, IP range, auth method)
   - Step 4: pg_basebackup command generation with all options
   - Step 5: Review panel with copy-to-clipboard and optional execute

9. PG_BASEBACKUP COMMAND GENERATOR:
   - Generates complete pg_basebackup command with: --host, --port, --username, --pgdata, --wal-method=stream, --write-recovery-conf, --slot, --create-slot, --checkpoint, --progress, --verbose
   - Generates corresponding primary_conninfo and primary_slot_name for recovery
   - Copy command to clipboard or execute directly (with progress monitoring)

10. LOGICAL REPLICATION SETUP WIZARD using huh forms library:
    - Step 1: Publication name and scope (FOR TABLE vs FOR ALL TABLES)
    - Step 2: Table selection with schema browser, size/row counts, large table warnings
    - Step 3: Subscriber connection details (host, port, database, user)
    - Step 4: Subscription creation with options (copy_data, enabled, synchronous_commit)
    - Generated SQL for CREATE PUBLICATION and CREATE SUBSCRIPTION

11. CONNECTION STRING BUILDER:
    - Interactive form for primary_conninfo parameters
    - Fields: host, port, user, password (masked), application_name, sslmode
    - Live preview of generated connection string
    - Test connection button to validate before use
    - Copy to clipboard

**Layout:**
Split view with Topology (left 40%) and Details (right 60%). Details panel shows selected replica stats, WAL pipeline visualization, and lag sparkline. Toggle between overview (all replicas table) and detail (single replica deep-dive) modes. Setup wizards render as centered modal overlays.

**Data Persistence:**
Store historical lag data in SQLite (replication_lag_history table) with 24-hour retention for trend analysis. Data model includes future fields for bi-directional replication support (direction, conflict_count) to prepare for multi-master system integration.

**Keyboard Navigation:**

*Monitoring:*
j/k navigate replicas, Enter/d show details, t toggle topology, l toggle logical panel, s/S sort, 1-4 sparkline windows, x manage slots, y copy, R refresh, h help.

*Setup:*
c config checker, w physical wizard, W logical wizard, u create user, b connection builder, g generate pg_hba entry, Tab/Shift+Tab navigate fields, Space toggle selection, a/n select all/none, Enter continue, ← back, y copy, t test connection, Esc cancel.

**Graceful Degradation:**
Show "Replication not configured" when pg_stat_replication empty. Auto-detect primary vs replica role and display appropriate statistics. Handle permission errors gracefully with guidance. Setup operations require superuser - show clear error with privilege requirements if not. Read-only mode disables all setup/modification operations.

**Security:**
Replication user creation uses secure password generation (go-password library). Passwords displayed once with copy option, then masked. Generated pg_hba.conf entries use scram-sha-256 by default. SSL mode defaults to 'prefer' in connection strings.

Build using Bubbletea framework with lipgloss styling matching Query Performance and Locks views. Create repviz package for custom visualizations (topology.go, pipeline.go, sparkline.go). Create setup package using huh forms for wizards (config_check.go, physical_wizard.go, logical_wizard.go, connstring.go). Prioritize P1 stories (lag viewing, slot status, topology, config checker, physical wizard) then P2 (WAL pipeline, sparklines, logical replication, logical wizard, connection builder) then P3 (slot management, historical trends, advanced setup options). Auto-refresh every 2 seconds with sub-500ms query execution.
```

---

### Feature 006.5: Service Architecture (steep-agent)

**Branch**: `006-5-service-architecture`

**Purpose**: Introduce a background service/daemon (steep-agent) that continuously collects monitoring data and maintains the SQLite database, enabling data persistence and freshness regardless of whether the TUI is running. This establishes a client/server architecture where the TUI becomes a lightweight client reading from a shared SQLite database maintained by the service.

**User Stories** (Priority Order):
1. **P1**: As a DBA, I want monitoring data to be collected continuously even when the TUI is not running, so I have historical data available when I open Steep
2. **P1**: As a DBA, I want to install steep-agent as a system service (systemd/launchd/Windows Service) so it starts automatically on boot
3. **P1**: As a DBA, I want the TUI to work in both standalone mode (current behavior) and client mode (reading from agent-maintained SQLite)
4. **P2**: As a DBA, I want steep-agent to handle data retention and cleanup automatically so the SQLite database doesn't grow unbounded
5. **P2**: As a DBA, I want steep-agent to monitor multiple PostgreSQL instances and aggregate data into a single SQLite database
6. **P2**: As a DBA, I want to configure steep-agent via the same YAML config file used by the TUI
7. **P3**: As a DBA, I want steep-agent to support alerting/notifications when thresholds are breached
8. **P3**: As a DBA, I want to query steep-agent status and health from the TUI

**Architecture**:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         DEPLOYMENT MODES                                 │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Mode 1: Standalone TUI (current, no changes)                           │
│  ┌─────────┐         ┌────────────┐         ┌──────────┐               │
│  │  steep  │ ──────▶ │ PostgreSQL │ ──────▶ │  SQLite  │               │
│  │  (TUI)  │         │  (source)  │         │ (embedded)│               │
│  └─────────┘         └────────────┘         └──────────┘               │
│                                                                          │
│  Mode 2: With Service (new)                                             │
│  ┌─────────────┐     ┌────────────┐     ┌──────────┐                   │
│  │ steep-agent │ ──▶ │ PostgreSQL │ ──▶ │  SQLite  │ ◀── steep (TUI)  │
│  │  (daemon)   │     │  (source)  │     │ (shared) │     (read-only)   │
│  └─────────────┘     └────────────┘     └──────────┘                   │
│        │                   │                                            │
│        │              ┌────────────┐                                    │
│        └────────────▶ │ PostgreSQL │  (multi-instance support)         │
│                       │  (source2) │                                    │
│                       └────────────┘                                    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**Technical Scope**:

*Service Infrastructure (using kardianos/service)*:
- Cross-platform service management: Windows Services, systemd, launchd, SysV init, Upstart, OpenRC
- Service lifecycle: Install, Uninstall, Start, Stop, Restart, Status
- Run as system service or user service
- Graceful shutdown with context cancellation
- PID file management on Unix systems
- Service recovery/restart on failure

*Data Collection*:
- Goroutine-per-monitor pattern (reuse existing monitors)
- Configurable collection intervals per data type
- Connection pooling with automatic reconnection
- Multi-instance PostgreSQL support (one connection pool per instance)

*SQLite as Shared State*:
- Single writer (steep-agent), multiple readers (TUI instances)
- WAL mode for concurrent read access
- Schema versioning for upgrades
- Automatic data retention/pruning (configurable per table)
- Database location: `~/.config/steep/steep.db` (same as current)

*Communication Strategy*:
- **Primary**: SQLite as shared state (no IPC needed)
- **Optional Future**: Unix socket/named pipe for real-time streaming
- **Optional Future**: gRPC for remote monitoring scenarios

*CLI Commands*:
```bash
# Service management
steep-agent install [--user]     # Install as system/user service
steep-agent uninstall            # Remove service
steep-agent start                # Start service
steep-agent stop                 # Stop service
steep-agent restart              # Restart service
steep-agent status               # Show service status

# Direct run (foreground, for debugging)
steep-agent run                  # Run in foreground
steep-agent run --config /path   # Run with custom config

# TUI mode selection
steep                            # Auto-detect: use agent if running, else standalone
steep --standalone               # Force standalone mode (current behavior)
steep --client                   # Force client mode (requires agent running)
```

**Configuration**:
```yaml
# ~/.config/steep/config.yaml

# Agent-specific settings
agent:
  enabled: true

  # Data collection intervals
  intervals:
    activity: 2s          # pg_stat_activity
    queries: 5s           # Query stats from logs
    replication: 2s       # Replication lag
    locks: 2s             # Lock monitoring
    tables: 30s           # Table statistics

  # Data retention
  retention:
    activity_history: 24h
    query_stats: 7d
    replication_lag: 24h
    lock_history: 24h

  # Multi-instance support
  instances:
    - name: primary
      connection: "host=localhost port=5432 dbname=postgres"
    - name: replica1
      connection: "host=replica1 port=5432 dbname=postgres"

  # Alerting (P3)
  alerts:
    enabled: false
    webhook_url: ""
```

**Libraries**:
- `github.com/kardianos/service` - Cross-platform service management (Windows Services, systemd, launchd, SysV, Upstart, OpenRC)
- `github.com/mattn/go-sqlite3` - SQLite with WAL mode for concurrent access
- `github.com/jackc/pgx/v5/pgxpool` - PostgreSQL connection pooling
- `github.com/spf13/viper` - Configuration management (existing)
- `github.com/spf13/cobra` - CLI commands for service management

**Directory Structure**:
```
steep/
├── cmd/
│   ├── steep/           # TUI application (existing)
│   │   └── main.go
│   └── steep-agent/     # Service/daemon (new)
│       └── main.go
├── internal/
│   ├── agent/           # Agent-specific code (new)
│   │   ├── agent.go     # Main agent orchestration
│   │   ├── collector.go # Data collection coordination
│   │   ├── service.go   # kardianos/service integration
│   │   └── config.go    # Agent configuration
│   ├── monitors/        # Reuse existing monitors
│   └── storage/sqlite/  # Reuse existing SQLite code
```

**Acceptance Criteria**:

*Service Management*:
- `steep-agent install` installs service on Windows (SCM), macOS (launchd), Linux (systemd/sysv)
- `steep-agent uninstall` cleanly removes service
- `steep-agent start/stop/restart` control service lifecycle
- `steep-agent status` shows: running/stopped, PID, uptime, last collection time
- Service auto-starts on system boot when installed
- Service restarts automatically on crash (with backoff)
- Graceful shutdown completes in-flight writes to SQLite

*Data Collection*:
- Collects data continuously at configured intervals
- Handles PostgreSQL connection failures with automatic reconnection
- Supports monitoring multiple PostgreSQL instances
- Logs collection errors without crashing
- Respects configured retention periods

*SQLite Integration*:
- Uses WAL mode for concurrent TUI read access
- Prevents database corruption on unclean shutdown
- Automatic schema migration on version upgrades
- Prunes old data based on retention config
- TUI detects agent mode and switches to read-only SQLite access

*TUI Integration*:
- `steep` auto-detects if agent is running (checks PID file or SQLite lock)
- `steep --standalone` forces current embedded behavior
- `steep --client` requires agent, fails with helpful message if not running
- Client mode TUI shows "Agent: Connected" in status bar
- Client mode has fresher historical data (collected even when TUI closed)

*Cross-Platform*:
- Windows: Installs as Windows Service, visible in services.msc
- macOS: Installs as launchd daemon, manageable via launchctl
- Linux: Installs as systemd unit (or sysv/upstart on older systems)
- All platforms: Same CLI interface, same config file format

**Spec-Kit Command**:
```bash
/speckit.specify Implement Service Architecture (steep-agent) for continuous background data collection independent of TUI runtime. Use kardianos/service library for cross-platform service management supporting Windows Services, systemd, launchd, SysV init, Upstart, and OpenRC.

**Architecture:**
Create steep-agent daemon that continuously collects PostgreSQL monitoring data and writes to SQLite database. TUI becomes optional client reading from agent-maintained SQLite. Support two deployment modes: (1) Standalone TUI (current behavior, no changes) and (2) Agent + TUI client mode where agent handles all data collection.

**Service Management:**
Implement CLI commands: install, uninstall, start, stop, restart, status. Support both system-wide and user-level service installation. Handle graceful shutdown with context cancellation. Implement automatic restart on crash with exponential backoff.

**Data Collection:**
Reuse existing monitor goroutines (activity, queries, replication, locks, tables) running in agent process. Support configurable collection intervals per data type. Handle connection failures with automatic reconnection. Support monitoring multiple PostgreSQL instances with separate connection pools.

**SQLite Integration:**
Use SQLite WAL mode for concurrent read access from TUI. Implement automatic data retention/pruning based on configuration. Prevent corruption with proper shutdown handling. Use same database location (~/.config/steep/steep.db) for seamless TUI integration.

**TUI Integration:**
Add --standalone and --client flags to steep command. Auto-detect agent presence via PID file or SQLite write lock. Show agent connection status in TUI status bar. Client mode uses read-only SQLite access with fresher historical data.

**Configuration:**
Extend existing YAML config with agent section: collection intervals, retention periods, multi-instance connections, optional alerting webhook. Single config file shared between agent and TUI.

**Directory Structure:**
Create cmd/steep-agent/ for daemon entry point. Create internal/agent/ package for agent orchestration, service integration, and collector coordination. Reuse internal/monitors/ and internal/storage/sqlite/ packages.

Prioritize P1 stories (continuous collection, service install/management, dual-mode TUI) then P2 (retention, multi-instance, shared config) then P3 (alerting, agent health monitoring). Target cross-platform support from day one using kardianos/service abstractions.
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
| 006.5 - Service Architecture | 001-006 | Enhanced data persistence, multi-instance |
| 007 - SQL Editor | 001 | 010 (User management queries) |
| 008 - Configuration Viewer | 001 | - |
| 009 - Log Viewer | 001 | - |
| 010 - Database Operations | 001, 005, 007 | - |
| 011 - Advanced Visualizations | 001-006 | - |
| 012 - Alert System | 001-006, 006.5 | - |

---

**Document Version**: 1.0
**Last Updated**: 2025-11-19
**Status**: Implementation Roadmap
**Maintained By**: Steep Development Team
