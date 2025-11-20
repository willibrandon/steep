# Steep - Postgres Monitoring Terminal Application

## Design Document v1.0

---

## 1. Executive Summary

Steep is a comprehensive terminal-based monitoring and management application for PostgreSQL databases, built with Go and the Bubbletea TUI framework. It provides real-time monitoring, performance analysis, and administrative capabilities that DBAs and developers need in a responsive, keyboard-driven interface.

**Key Design Principles:**
- **Comprehensive**: Cover all critical monitoring and management tasks
- **Real-time**: Live updates and metrics with configurable refresh rates
- **Efficient**: Minimal resource overhead, optimized queries
- **Intuitive**: Keyboard-driven navigation with discoverable shortcuts
- **Extensible**: Plugin architecture for custom monitors and actions

---

## 2. Core Features

### 2.1 Real-Time Monitoring

#### Connection & Activity Monitoring
- Active connections with query details
- Connection pool statistics
- Idle/active/idle-in-transaction states
- Client IP, database, user, application name
- Connection duration and wait events
- Kill/terminate connections capability

#### Query Performance
- Currently executing queries with duration
- Top N queries by execution time
- Top N queries by total time
- Top N queries by calls
- Query execution plans (EXPLAIN/EXPLAIN ANALYZE)
- Query history and statistics from pg_stat_statements
- Slow query log viewer
- Query text search and filtering

#### Database Statistics
- Database size and growth trends
- Table sizes (disk, toast, indexes)
- Index usage statistics and unused indexes
- Cache hit ratios (tables, indexes, overall)
- Sequential vs index scan ratios
- Tuple statistics (inserts, updates, deletes)
- Dead tuple counts and bloat estimation

#### Replication & High Availability
- Replication lag monitoring (bytes and time)
- Replication slot status
- WAL sender/receiver statistics
- Streaming replication status
- Logical replication monitoring
- Archive status and WAL generation rate

#### Lock Monitoring
- Active locks by type and mode
- Lock wait queue visualization
- Blocking query identification
- Lock dependency tree
- Deadlock detection and history

#### Performance Metrics
- Transactions per second (TPS)
- Tuple operations (read/written/returned)
- Cache hit rates with historical trends
- Checkpoint statistics and timing
- Background writer statistics
- WAL write rates and timing

#### System Health
- Table bloat estimates
- Index bloat estimates
- Vacuum and autovacuum status
- Long-running transactions
- Transaction ID wraparound monitoring
- Temp file usage
- Disk I/O statistics

### 2.2 Management & Operations

#### Query Execution
- Interactive SQL editor with syntax highlighting
- Result set pagination and export
- Transaction management (BEGIN, COMMIT, ROLLBACK)
- Query history with recall
- Saved queries/snippets library

#### Database Browsing
- Database/schema/table hierarchy navigation
- Table structure viewer (columns, types, constraints)
- Index definitions and properties
- View definitions
- Trigger and function listings
- Foreign key relationships visualization

#### Administrative Tasks
- VACUUM operations (manual, full, analyze)
- REINDEX operations
- ANALYZE table statistics
- User and role management
- Database/schema creation
- Grant/revoke permissions
- Configuration parameter viewing
- Server log viewer with filtering

#### Backup & Recovery
- pg_dump/pg_dumpall execution
- Backup scheduling status
- Point-in-time recovery (PITR) information
- WAL archiving status

### 2.3 Visualization & Analysis

#### Charts & Graphs
- Time-series graphs for key metrics
- Sparklines for inline trending
- Bar charts for comparative data
- Stacked area charts for composition metrics
- Heatmaps for time-based patterns

#### Alerting & Thresholds
- Configurable alerts for metrics
- Visual indicators (colors, symbols)
- Alert history and acknowledgment
- Custom alert rules

---

## 3. Architecture

### 3.1 Application Structure

```
steep/
├── cmd/
│   └── steep/           # Main application entry point
├── internal/
│   ├── app/            # Application orchestration
│   ├── config/         # Configuration management
│   ├── db/             # Database connection and query management
│   │   ├── connection.go
│   │   ├── queries/    # SQL query definitions
│   │   └── models/     # Data models
│   ├── monitors/       # Monitoring modules
│   │   ├── activity.go
│   │   ├── locks.go
│   │   ├── replication.go
│   │   ├── performance.go
│   │   └── ...
│   ├── ui/             # Bubbletea UI components
│   │   ├── app.go      # Main app model
│   │   ├── components/ # Reusable components
│   │   │   ├── table.go
│   │   │   ├── chart.go
│   │   │   ├── statusbar.go
│   │   │   └── ...
│   │   ├── views/      # View implementations
│   │   │   ├── dashboard.go
│   │   │   ├── queries.go
│   │   │   ├── connections.go
│   │   │   └── ...
│   │   └── styles/     # Lipgloss styles
│   └── utils/          # Utility functions
├── pkg/                # Public packages
│   └── pgstats/        # Postgres statistics parsing
├── configs/            # Default configurations
├── docs/               # Documentation
└── scripts/            # Build and deployment scripts
```

### 3.2 Technology Stack

#### Core Libraries
- **bubbletea**: TUI application framework
- **bubbles**: Reusable TUI components (table, viewport, list, etc.)
- **lipgloss**: Styling and layout
- **harmonica**: Spring physics-based animations

#### Postgres Connectivity
- **pgx**: PostgreSQL driver and toolkit
- **pgxpool**: Connection pooling

#### Data Visualization
- **asciigraph**: ASCII-based graphs and charts
- **sparklines**: Inline trend visualization

#### Additional Libraries
- **viper**: Configuration management
- **cobra**: CLI argument parsing (if needed)
- **chroma**: Syntax highlighting for SQL
- **glamour**: Markdown rendering for help text

### 3.3 Data Flow

```
┌─────────────┐
│  PostgreSQL │
└──────┬──────┘
       │
       │ pgx queries
       │
┌──────▼──────┐
│  Monitors   │ ← Periodic polling (configurable intervals)
│  (Goroutines)│
└──────┬──────┘
       │
       │ Channel messages
       │
┌──────▼──────┐
│  App Model  │ ← Bubbletea Update()
│             │
└──────┬──────┘
       │
       │ View rendering
       │
┌──────▼──────┐
│  Terminal   │
└─────────────┘
```

### 3.4 Bubbletea Model Structure

```go
type Model struct {
    // Core state
    currentView   ViewType
    views         map[ViewType]ViewModel

    // Database connection
    dbPool        *pgxpool.Pool
    dbConfig      *DBConfig

    // Monitors
    monitors      map[string]*Monitor
    monitorData   chan MonitorUpdate

    // UI components
    statusBar     statusbar.Model
    help          help.Model

    // Application state
    width, height int
    ready         bool
    quitting      bool
    err           error
}

type ViewModel interface {
    Init() tea.Cmd
    Update(tea.Msg) (ViewModel, tea.Cmd)
    View() string
}
```

---

## 4. User Interface Design

### 4.1 Layout Structure

```
┌────────────────────────────────────────────────────────────────┐
│ Steep - postgres@localhost:5432/mydb          [Connected] 14:32│ ← Header
├────────────────────────────────────────────────────────────────┤
│                                                                │
│                        Main Content Area                       │
│                     (View-specific content)                    │
│                                                                │
│                                                                │
├────────────────────────────────────────────────────────────────┤
│ [1]Dashboard [2]Activity [3]Queries [4]Tables [5]Locks [6]Rep │ ← Tab bar
├────────────────────────────────────────────────────────────────┤
│ TPS: 125  |  Cache Hit: 99.2%  |  Connections: 15/100         │ ← Status bar
├────────────────────────────────────────────────────────────────┤
│ q:quit  h:help  r:refresh  /:search  tab:next view            │ ← Help bar
└────────────────────────────────────────────────────────────────┘
```

### 4.2 Views/Screens

#### Dashboard View (Default)
- Multi-panel overview showing key metrics
- Real-time graphs for TPS, connections, cache hits
- Quick stats: database size, active queries, replication lag
- Alert panel for warnings/errors
- System health indicators

#### Activity View
- Table of active connections
- Columns: PID, User, Database, State, Duration, Query (truncated)
- Actions: Kill connection, View full query, Filter
- Sort by various columns
- Auto-refresh with configurable interval

#### Queries View
- Top queries from pg_stat_statements
- Tabs: "By Time", "By Calls", "By Rows"
- Columns: Query (fingerprint), Calls, Total Time, Mean Time, Rows
- Actions: EXPLAIN, Copy query, Reset statistics
- Search/filter capability

#### Tables View
- Hierarchical browser: Database → Schema → Table
- Table details: Size, Row count, Bloat estimate
- Index information
- Table I/O statistics
- Actions: VACUUM, ANALYZE, REINDEX

#### Locks View
- Active locks with type, mode, granted status
- Blocking tree visualization
- Highlight blocked vs blocking queries
- Actions: View blocking query, Kill blocker

#### Replication View
- Replication slot status
- WAL sender/receiver stats
- Lag metrics (bytes and time)
- Replication timeline
- Actions: Advance slot, Drop slot

#### Configuration View
- Server configuration parameters
- Search and filter settings
- Show modified values vs defaults
- Context-sensitive help for parameters

#### Logs View
- Server log viewer with tail capability
- Filtering by log level, message pattern
- Color-coded by severity
- Timestamp-based navigation

#### SQL Editor View
- Full-screen SQL editor
- Syntax highlighting
- Query execution with result display
- Transaction controls
- Query history

### 4.3 Navigation & Keyboard Shortcuts

#### Global Shortcuts
- `q`: Quit application
- `h` or `?`: Help screen
- `r`: Manual refresh current view
- `/`: Search/filter
- `Esc`: Cancel/close dialogs
- `Tab`: Next view
- `Shift+Tab`: Previous view
- `1-9`: Jump to specific view
- `:`: Command mode (vim-like)

#### View-Specific Shortcuts
- Arrow keys / `hjkl`: Navigation
- `Space` / `Enter`: Select/action
- `d`: Details/drill-down
- `e`: Edit/execute
- `x`: Execute action (kill, vacuum, etc.)
- `s`: Sort
- `f`: Filter
- `c`: Copy
- `g/G`: Go to top/bottom
- `Ctrl+r`: Toggle auto-refresh

### 4.4 Color Scheme & Theming

#### Default Theme
- **Background**: Terminal default
- **Primary**: Cyan/Blue for headers and highlights
- **Success**: Green for healthy metrics
- **Warning**: Yellow for warnings
- **Error**: Red for errors and critical states
- **Muted**: Gray for secondary information

#### Configurable Themes
- Support for light/dark variants
- Custom color definitions in config file
- Respect terminal color capabilities

### 4.5 Responsive Design

- Adapt to terminal size changes
- Minimum size requirements with graceful degradation
- Collapsible panels on small screens
- Text truncation with full-text on-demand

---

## 5. Database Integration

### 5.1 Connection Management

```go
type ConnectionConfig struct {
    Host            string
    Port            int
    Database        string
    User            string
    Password        string
    SSLMode         string
    ApplicationName string
    MaxConns        int
    MinConns        int
}
```

- Support for multiple connection profiles
- Connection pooling with pgxpool
- Automatic reconnection on failures
- SSL/TLS support
- SSH tunnel support for remote connections

### 5.2 Key Postgres System Views

#### Activity & Sessions
- `pg_stat_activity`: Current activity and connections
- `pg_stat_database`: Database-wide statistics
- `pg_stat_all_tables`: Table access statistics
- `pg_stat_all_indexes`: Index usage statistics

#### Performance
- `pg_stat_statements`: Query statistics (requires extension)
- `pg_stat_bgwriter`: Background writer statistics
- `pg_stat_archiver`: WAL archiving statistics
- `pg_statio_*`: I/O statistics

#### Replication
- `pg_stat_replication`: Replication status
- `pg_replication_slots`: Replication slot information
- `pg_stat_wal_receiver`: WAL receiver statistics

#### Locks & Blocking
- `pg_locks`: Lock information
- Custom queries to identify blocking relationships

#### Configuration & System
- `pg_settings`: Configuration parameters
- `pg_database`: Database information
- `pg_tablespace`: Tablespace information
- `pg_stat_progress_*`: Progress tracking for long operations

### 5.3 Query Optimization

- Prepared statements for frequently executed queries
- Batch queries where possible
- Configurable refresh intervals per monitor
- Smart polling: faster refresh for critical metrics, slower for static data
- Query result caching with TTL

### 5.4 Required Extensions

- **pg_stat_statements**: Query statistics (highly recommended)
- **pg_buffercache**: Buffer cache inspection (optional)
- **pgstattuple**: Tuple-level statistics and bloat detection (optional)

---

## 6. Configuration

### 6.1 Configuration File Structure

```yaml
# ~/.config/steep/config.yaml

# Connection profiles
connections:
  default:
    host: localhost
    port: 5432
    database: postgres
    user: postgres
    password_command: "pass show postgres/local"  # External password manager
    sslmode: prefer

  production:
    host: prod.example.com
    port: 5432
    database: myapp
    user: monitoring_user
    sslmode: require
    ssh_tunnel:
      host: bastion.example.com
      port: 22
      user: deploy

# Display preferences
ui:
  theme: dark
  refresh_interval: 2s
  date_format: "2006-01-02 15:04:05"
  table_max_rows: 100

# Monitor-specific settings
monitors:
  activity:
    refresh_interval: 1s
    show_idle: false

  queries:
    refresh_interval: 5s
    min_duration: 100ms  # Only show queries slower than this

  locks:
    refresh_interval: 2s

# Alerting
alerts:
  - name: high_replication_lag
    condition: replication_lag_bytes > 100MB
    severity: warning

  - name: connection_limit
    condition: active_connections > max_connections * 0.8
    severity: critical

# Key bindings (override defaults)
keybindings:
  quit: "q"
  help: "?"
  refresh: "r"
```

### 6.2 Environment Variables

- `STEEP_CONFIG`: Path to config file
- `STEEP_PROFILE`: Default connection profile to use
- `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`: Standard libpq variables

---

## 7. Implementation Phases

### Phase 1: Foundation (MVP)
- [ ] Basic Bubbletea application scaffold
- [ ] Database connection management
- [ ] Dashboard view with key metrics
- [ ] Activity view (pg_stat_activity)
- [ ] Basic table component with sorting
- [ ] Configuration file support

### Phase 2: Core Monitoring
- [ ] Queries view (pg_stat_statements)
- [ ] Locks view with blocking detection
- [ ] Tables view with size and statistics
- [ ] Replication monitoring
- [ ] Status bar with key metrics
- [ ] Auto-refresh implementation

### Phase 3: Advanced Features
- [ ] SQL Editor with syntax highlighting
- [ ] EXPLAIN visualization
- [ ] Database browser (schema/table hierarchy)
- [ ] Configuration viewer
- [ ] Log viewer
- [ ] Chart visualizations (time series)

### Phase 4: Management Operations
- [ ] VACUUM/ANALYZE operations
- [ ] Connection management (kill/terminate)
- [ ] REINDEX operations
- [ ] User/role management
- [ ] Backup status monitoring

### Phase 5: Polish & Extension
- [ ] Alert system with notifications
- [ ] Custom themes
- [ ] Plugin system
- [ ] Export capabilities (CSV, JSON)
- [ ] Historical data tracking
- [ ] Performance optimizations
- [ ] Comprehensive documentation

---

## 8. Technical Considerations

### 8.1 Performance

- Monitor goroutines: One per monitor type, communicating via channels
- Efficient SQL queries: Use indexes, limit result sets
- Connection pooling: Reuse connections, limit concurrent queries
- Render optimization: Only redraw changed portions
- Memory management: Limit data retention, circular buffers for time series

### 8.2 Error Handling

- Graceful degradation when extensions are missing
- Clear error messages with actionable suggestions
- Retry logic for transient connection failures
- Fallback displays when data is unavailable
- Error logging to file for debugging

### 8.3 Security

- No password storage in config (use password_command)
- Support for SSL/TLS connections
- Read-only mode option (prevent destructive operations)
- Audit logging for administrative actions
- Support for connection via Unix sockets

### 8.4 Compatibility

- PostgreSQL versions: 11+ (primary target 18)
- Terminal emulators: xterm-256color and above
- Operating systems: Linux, macOS, Windows (with WSL)
- Graceful handling of version-specific features

### 8.5 Testing

- Unit tests for business logic and data parsing
- Integration tests with real PostgreSQL instances
- UI snapshot testing for regression detection
- Performance benchmarks for query execution
- Manual testing checklist for UI interactions

---

## 9. Future Enhancements

### 9.1 Advanced Analytics

- Query plan regression detection
- Automatic index recommendations
- Bloat trending and forecasting
- Capacity planning metrics
- Anomaly detection with ML

### 9.2 Multi-Database Support

- Monitor multiple databases simultaneously
- Aggregate metrics across cluster
- Cross-database comparison views
- Cluster topology visualization

### 9.3 Integration & Export

- Prometheus metrics exporter
- JSON API for scripting
- Alert integration (PagerDuty, Slack, etc.)
- Export reports (HTML, PDF)
- Session recording/replay

### 9.4 Collaboration Features

- Shared queries and dashboards
- Team annotations on queries
- Incident timeline tracking

---

## 10. Success Metrics

### 10.1 Performance Goals

- Application startup: < 1 second
- View switching: < 100ms
- Query execution: < 500ms for most monitors
- Memory footprint: < 50MB typical usage
- CPU usage: < 5% idle, < 20% active

### 10.2 User Experience Goals

- Intuitive navigation: < 5 minutes to proficiency
- Comprehensive coverage: 90% of common DBA tasks
- Reliability: 99.9% uptime during monitoring
- Responsiveness: Smooth 60 FPS rendering

---

## 11. References & Resources

### 11.1 PostgreSQL Documentation

- Official PostgreSQL Monitoring Documentation
- pg_stat_statements extension
- System catalog tables
- Monitoring best practices

### 11.2 Bubbletea Ecosystem

- Bubbletea tutorial and examples
- Bubbles component library
- Lipgloss styling guide
- Community examples and patterns

### 11.3 Similar Projects (Inspiration)

- pgcli: Enhanced psql with auto-completion
- pgAdmin: Web-based administration
- Postico: macOS GUI client
- pgCenter: Terminal-based monitoring (C implementation)
- pg_top: Top-like interface for PostgreSQL

---

## 12. Glossary

- **Bloat**: Wasted space in tables/indexes due to dead tuples
- **Cache Hit Ratio**: Percentage of reads served from memory vs disk
- **MVCC**: Multi-Version Concurrency Control
- **TPS**: Transactions Per Second
- **WAL**: Write-Ahead Log
- **Replication Lag**: Delay between primary and replica
- **Dead Tuples**: Deleted or obsolete row versions
- **VACUUM**: PostgreSQL maintenance operation to reclaim space
- **pg_stat_statements**: Extension tracking query execution statistics

---

**Document Version**: 1.0
**Last Updated**: 2025-11-19
**Status**: Initial Design
**Next Review**: After Phase 1 implementation
