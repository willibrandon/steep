# Steep - PostgreSQL Monitoring TUI

A terminal-based PostgreSQL monitoring tool built with Go and [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- **Real-time Dashboard** - Monitor database metrics with time-series graphs, TPS heatmap, and server status
- **Query Performance Monitoring** - Track slow queries, view EXPLAIN plans with tree visualization, search/filter by pattern
- **Lock Monitoring** - Active locks, blocking query detection, lock dependency tree, deadlock history
- **Replication Monitoring** - Lag metrics, topology visualization, slot management, setup wizards for physical/logical replication
- **Table Statistics** - Schema browser, bloat detection, index usage, maintenance operations
- **SQL Editor** - Interactive SQL editor with vim-style editing, syntax highlighting, transaction support, history, and snippets
- **Log Viewer** - Real-time PostgreSQL log streaming with severity filtering, search, :level/:goto commands, and remote viewing
- **Configuration Viewer** - Browse and modify PostgreSQL parameters with :set/:reset/:reload commands
- **Alert System** - Threshold-based alerts with expression rules, history, and acknowledgment
- **Database Operations** - Table maintenance (VACUUM, ANALYZE, REINDEX), permission management, role administration
- **Background Agent** - `steep-agent` daemon for continuous data collection independent of TUI runtime
- **Keyboard Navigation** - Vim-style and intuitive keyboard shortcuts
- **Automatic Reconnection** - Resilient connection handling with exponential backoff
- **Password Management** - Secure password handling via environment variables or commands
- **SSL/TLS Support** - Full SSL/TLS configuration including certificate verification

## Installation

### Prerequisites

- Go 1.21 or later
- PostgreSQL 12 or later
- Terminal with 80x24 minimum dimensions

### From Source

```bash
# Clone the repository
git clone https://github.com/willibrandon/steep.git
cd steep

# Build the application
make build

# Run
./bin/steep
```

### Build Options

```bash
make build            # Build the steep TUI binary
make build-agent      # Build the steep-agent daemon
make build-repl-daemon # Build the steep-repl daemon
make build-repl-ext   # Build the PostgreSQL extension (requires Rust + pgrx)
make test             # Run all tests
make test-short       # Run tests (skip integration)
make test-integration # Run integration tests only
make test-repl        # Run replication extension tests
make test-repl-integration # Run replication integration tests
make bench            # Run performance benchmarks
make clean            # Remove build artifacts
make help             # Show available targets
```

## Quick Start

### Basic Usage

1. Create a configuration file:

```bash
# Copy the example configuration
cp config.yaml.example config.yaml

# Edit with your database connection details
nano config.yaml
```

2. Set up password authentication:

```bash
# Option 1: Environment variable (for empty password)
export PGPASSWORD=""

# Option 2: Environment variable (with password)
export PGPASSWORD="your-password"

# Option 3: Use password command in config.yaml
# password_command: "security find-generic-password -s 'postgres' -w"
```

3. Run Steep:

```bash
./bin/steep
```

## Configuration

### Configuration File

Steep reads configuration from `config.yaml` in the current directory or `~/.config/steep/config.yaml`.

```yaml
connection:
  host: localhost
  port: 5432
  database: postgres
  user: postgres

  # SSL/TLS Configuration
  sslmode: prefer  # disable, allow, prefer, require, verify-ca, verify-full
  # sslrootcert: /path/to/ca.crt
  # sslcert: /path/to/client.crt
  # sslkey: /path/to/client.key

  # Connection pool settings
  pool_max_conns: 10
  pool_min_conns: 2

ui:
  theme: dark              # dark or light
  refresh_interval: 1s     # Status bar refresh rate
  date_format: "2006-01-02 15:04:05"

debug: false
```

### Environment Variables

All configuration options can be overridden with environment variables:

```bash
export STEEP_CONNECTION_HOST=localhost
export STEEP_CONNECTION_PORT=5432
export STEEP_CONNECTION_DATABASE=mydb
export STEEP_CONNECTION_USER=myuser
export STEEP_CONNECTION_SSLMODE=verify-full
export STEEP_UI_THEME=dark
export STEEP_DEBUG=true
```

### Password Configuration

Steep supports multiple password authentication methods in order of precedence:

1. **Password Command** (most secure for production)
   ```yaml
   connection:
     password_command: "security find-generic-password -s 'postgres' -w"
   ```

2. **Environment Variable**
   ```bash
   export PGPASSWORD="your-password"
   ```

3. **Interactive Prompt** (if no other method is configured)

See [Password Authentication](docs/PASSWORD_AUTH.md) for detailed setup instructions.

### SSL/TLS Configuration

Steep supports all PostgreSQL SSL modes. See [SSL Configuration Guide](docs/SSL_CONFIGURATION.md) for detailed setup.

**Quick SSL Examples:**

```yaml
# Local development (no SSL)
connection:
  sslmode: disable

# Production (SSL with CA verification)
connection:
  sslmode: verify-full
  sslrootcert: /path/to/ca-certificate.crt

# Production with client certificates
connection:
  sslmode: verify-full
  sslrootcert: /path/to/ca-certificate.crt
  sslcert: /path/to/client.crt
  sslkey: /path/to/client.key
```

## Usage

### Keyboard Shortcuts

#### Global

- `q` or `Ctrl+C` - Quit application
- `h` or `?` - Toggle help screen
- `Esc` - Close help screen

#### View Navigation

- `1` - Dashboard view
- `2` - Activity view
- `3` - Queries view
- `4` - Locks view
- `5` - Tables view
- `6` - Replication view
- `7` - SQL Editor view
- `8` - Configuration view
- `9` - Log Viewer
- `0` - Roles view
- `Tab` - Next view
- `Shift+Tab` - Previous view

### Views

#### Dashboard
- Database overview statistics (TPS, active connections, cache hit ratio)
- Time-series graphs for TPS, connections, and cache hit ratio
- TPS heatmap showing weekly activity patterns (24h × 7d grid)
- **Alert panel** showing active alerts with severity icons, values, and thresholds
- **Alert counts** in status bar (e.g., "2 WARN 1 CRIT")
- Time window selection: `w`/`W` to cycle through 1m, 5m, 15m, 1h, 24h
- Toggle charts on/off with `V` key
- Toggle heatmap with `H` key
- Alert history overlay with `a` key

**Dashboard Key Bindings:**

| Key | Action |
|-----|--------|
| `w/W` | Cycle time window forward/backward |
| `H` | Toggle TPS heatmap |
| `V` | Toggle charts (global) |
| `a` | Open alert history overlay |
| `?` | Toggle help |

**Alert History Overlay:**

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate up/down |
| `g/G` | Jump to top/bottom |
| `Enter` | Toggle acknowledgment |
| `Esc` or `a` | Close overlay |

#### Activity
- Real-time connection monitoring with query duration sparklines
- Session statistics and wait events
- Color-coded connection states

#### Queries
- Query performance statistics with fingerprinting
- Bar chart showing top 10 queries by execution time
- Sort by total time, calls, mean time, or rows
- EXPLAIN plan viewer with JSON output (`e` key)
- EXPLAIN ANALYZE with tree visualization (`E` key) - shows timing, cost percentages, and highlights slowest/costliest nodes
- Search/filter queries by regex pattern
- Copy queries to clipboard
- Reset statistics
- Data source indicator (sampling or log parsing)
- Toggle charts on/off with `V` key
- Press `h` for keybinding help

#### Locks
- Active locks monitoring with type, mode, granted status
- Blocking query detection with color coding (red=blocked, yellow=blocking)
- Lock dependency tree visualization
- Kill blocking queries with confirmation dialog
- Deadlock history with log parsing
- Deadlock cycle visualization (2-node horizontal, 3+ node vertical)
- Sort by PID, type, mode, duration, granted status
- Press `h` for keybinding help

#### Tables
- Hierarchical schema/table browser with expand/collapse
- Table statistics: size, rows, cache hit ratio, bloat percentage
- Sparkline trend column showing table size history
- Bar chart showing top 10 tables by size
- Index usage statistics with unused index highlighting (yellow)
- Bloat detection with color coding (red >20%, yellow 10-20%)
- Table details panel: columns, constraints, indexes, size breakdown
- Partition hierarchy visualization
- SQL copy menu: SELECT, INSERT, UPDATE, DELETE templates
- Maintenance operations menu: VACUUM, VACUUM FULL, VACUUM ANALYZE, ANALYZE, REINDEX TABLE, REINDEX CONCURRENTLY
- Operation progress tracking with real-time progress bar (VACUUM/VACUUM FULL)
- Operation cancellation support (sends pg_cancel_backend)
- Session operation history overlay (H key)
- Permissions dialog for viewing/managing table grants (p key)
- pgstattuple extension auto-install prompt
- System schema toggle (P key)
- Toggle charts on/off with `V` key
- Read-only mode support (blocks all maintenance operations)
- Press `h` for keybinding help

**Tables Key Bindings:**

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate up/down |
| `g/G` | First/last item |
| `Enter` | Expand/collapse or open details |
| `→/l` | Expand schema or partitions |
| `←` | Collapse or move to parent |
| `i` | Toggle focus tables/indexes |
| `d` | Open table details |
| `x` | Open operations menu |
| `v` | VACUUM table (quick) |
| `a` | ANALYZE table (quick) |
| `r` | REINDEX table (quick) |
| `p` | View/manage permissions |
| `H` | View operation history |
| `P` | Toggle system schemas |
| `s/S` | Cycle sort column/toggle direction |
| `y` | Copy table or index name |
| `R` | Refresh data |

#### Replication
- **Overview Tab**: Replica status with lag metrics, topology visualization, sparklines for lag trends
- **Slots Tab**: Replication slots management, drop inactive slots with confirmation
- **Logical Tab**: Publications and subscriptions browser
- **Setup Tab**: Physical/logical replication wizards, connection string builder, configuration checker
- Topology view with pipeline visualization
- Lag history sparklines with color coding (green <1MB, yellow 1-10MB, red >10MB)
- Time windows: 1m (memory), 5m/15m/1h (SQLite persistence)
- Press `h` for keybinding help

**Replication Key Bindings:**

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate up/down |
| `g/G` | First/last item |
| `Tab` or `→/←` | Switch tabs |
| `d` or `Enter` | View details |
| `t` | Toggle topology view |
| `w` | Cycle time window (sparklines) |
| `D` | Drop inactive slot (Slots tab) |
| `p/P` | Focus publications/subscriptions (Logical tab) |
| `s/S` | Cycle sort column/toggle direction |
| `y` | Copy selected value |
| `r` | Refresh data |

#### SQL Editor
- Multi-line SQL query editor with vim-style editing
- Live SQL syntax highlighting (Chroma-based)
- Query execution with F5 or Ctrl+Enter
- Paginated results table with sorting (100 rows/page)
- Transaction management (BEGIN, COMMIT, ROLLBACK, SAVEPOINT)
- Query history with Up/Down navigation and Ctrl+R search
- Named query snippets with YAML persistence
- Export results to CSV or JSON
- Keyboard shortcuts: `\` focus toggle, j/k navigation, y/Y copy
- Read-only mode support (blocks DDL/DML)
- Press `h` for keybinding help

**SQL Editor Key Bindings:**

| Key | Action |
|-----|--------|
| `F5` or `Ctrl+Enter` | Execute query |
| `\` | Toggle focus (editor ↔ results) |
| `Tab` | Switch to results |
| `Enter` (results) | Switch to editor, enter insert mode |
| `j/k` | Navigate results rows |
| `n/p` | Next/previous page |
| `g/G` | First/last row |
| `y/Y` | Copy cell/row |
| `s/S` | Cycle sort column/toggle direction |
| `+/-` | Resize editor/results split |
| `Up` (at line 1) | Previous history entry |
| `Ctrl+R` | Search history |
| `Ctrl+O` | Open snippet browser |

**SQL Editor Commands (type `:` in normal mode):**

| Command | Description |
|---------|-------------|
| `:exec` or `:run` | Execute query |
| `:save NAME` | Save query as snippet |
| `:save! NAME` | Save (overwrite existing) |
| `:load NAME` | Load snippet into editor |
| `:delete NAME` | Delete snippet |
| `:snippets` | Open snippet browser |
| `:export csv FILE` | Export results to CSV |
| `:export json FILE` | Export results to JSON |
| `:repl` | Launch pgcli or psql (auto-detect) |
| `:repl pgcli` | Launch pgcli specifically |
| `:repl psql` | Launch psql specifically |
| `:repl sqlite` | Launch SQLite REPL for steep.db |
| `:repl litecli` | Launch litecli specifically |
| `:repl sqlite3` | Launch sqlite3 specifically |
| `:repl docker` | Force Docker (pgcli or psql) |
| `:repl docker pgcli` | Force Docker pgcli |
| `:repl docker psql` | Force Docker psql |
| `:repl docker sqlite` | Force Docker (litecli or sqlite3) |
| `:repl docker litecli` | Force Docker litecli |
| `:repl docker sqlite3` | Force Docker sqlite3 |
| `:clear` | Clear editor and results |

#### Configuration
- Browse all PostgreSQL configuration parameters from pg_settings
- View parameter details: current value, default, type, context, description
- Filter by category or search by name/description
- Sort by name or category (ascending/descending)
- Color-coded status: yellow for modified parameters, red for pending restart
- Modify parameters with `:set` command (writes to postgresql.auto.conf)
- Reset parameters to defaults with `:reset` command
- Reload configuration with `:reload` (calls pg_reload_conf())
- Export current configuration to file with `:export config`
- Copy parameter name or value to clipboard
- Responsive layout adapts to terminal width
- Read-only mode support (blocks :set, :reset, :reload)
- Press `h` for keybinding help

**Configuration Key Bindings:**

| Key | Action |
|-----|--------|
| `j/k` | Navigate up/down |
| `g/G` | First/last parameter |
| `d` or `Enter` | View parameter details |
| `s/S` | Cycle sort column/toggle direction |
| `/` | Search by name or description |
| `c` | Filter by category |
| `y/Y` | Copy parameter name/value |
| `r` | Refresh configuration |
| `Esc` | Clear filters |

**Configuration Commands (type `:` in normal mode):**

| Command | Description |
|---------|-------------|
| `:set PARAM VALUE` | Set parameter value (ALTER SYSTEM) |
| `:reset PARAM` | Reset parameter to default |
| `:reload` | Reload configuration (pg_reload_conf) |
| `:export config FILE` | Export parameters to conf file |

#### Log Viewer
- Real-time PostgreSQL log streaming with follow mode
- Support for stderr, CSV, and JSON log formats
- Remote log viewing via pg_read_file() for containerized/remote PostgreSQL
- Severity filtering and color-coded log levels
- Search with regex pattern matching and n/N navigation
- Historical log navigation with `:goto` command
- Command and search history with SQLite persistence (↑/↓ to navigate)
- Multi-line log entry support with proper message parsing
- Copy log entries to clipboard
- Press `h` for keybinding help

**Log Viewer Key Bindings:**

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate up/down |
| `g/G` | Select oldest/newest entry |
| `Ctrl+d/Ctrl+u` | Half page down/up |
| `f` | Toggle follow mode (auto-scroll) |
| `/` | Start search |
| `n/N` | Next/previous search match |
| `y` | Copy selected entry |
| `Y` | Copy all (filtered) entries |
| `h` | Toggle help |
| `Esc` | Clear search/filters, close help |
| `↑` (in `:` or `/`) | Previous history entry |
| `↓` (in `:` or `/`) | Next history entry |

**Log Viewer Commands:**

| Command | Description |
|---------|-------------|
| `:level LEVEL` | Filter by severity (e.g., error, warning) |
| `:level LEVEL+` | Filter by level and above (e.g., error+) |
| `:level LEVEL TIME` | Filter + jump to time (e.g., `:level error -1h`) |
| `:level LEVEL >TIME` | Filter + first at/after time |
| `:level LEVEL <TIME` | Filter + last at/before time |
| `:level clear` | Clear severity filter |
| `:goto TIME` | Jump to closest entry at time |
| `:goto >TIME` | First entry at or after time |
| `:goto <TIME` | Last entry at or before time |

**Time Formats (for :level and :goto):**

| Format | Example |
|--------|---------|
| Time only | `14:30` (today) |
| Date + time | `2025-11-27 14:30` |
| Relative | `-1h`, `-30m`, `-2d` |

**Log Access Methods:**

Configure in `config.yaml`:
```yaml
logs:
  access_method: auto  # auto, filesystem, or pg_read_file
```

- `auto` (default): Try filesystem first, fall back to pg_read_file
- `filesystem`: Direct disk access (requires local log directory)
- `pg_read_file`: Read via SQL (works with remote/containerized PostgreSQL, requires superuser or pg_read_server_files role)

### Alerts Configuration

Configure threshold-based alerts in `config.yaml`:

```yaml
alerts:
  enabled: true                 # Master switch (default: true)
  history_retention: 720h       # Keep history for 30 days (default)

  rules:
    # Simple threshold alert with custom message
    - name: high_connections
      metric: active_connections
      operator: ">"
      warning: 80               # Warn when > 80 connections
      critical: 95              # Critical when > 95 connections
      enabled: true
      message: "{{.Name}}: {{.ValueFmt}} connections (threshold: {{.ThreshFmt}})"

    # Alert on low cache hit ratio
    - name: low_cache_hit
      metric: cache_hit_ratio
      operator: "<"
      warning: 0.95             # Warn when < 95%
      critical: 0.90            # Critical when < 90%
      enabled: true
      message: "Cache hit ratio dropped to {{.ValueFmt}}%"

    # Expression-based rule (connection utilization ratio)
    - name: connection_saturation
      metric: active_connections / max_connections
      operator: ">"
      warning: 0.8              # Warn at 80% utilization
      critical: 0.95            # Critical at 95% utilization
      enabled: true
```

**Available Metrics:**

| Metric | Description |
|--------|-------------|
| `active_connections` | Number of active database connections |
| `max_connections` | Maximum allowed connections (from pg_settings) |
| `cache_hit_ratio` | Buffer cache hit ratio (0-1, e.g., 0.99 = 99%) |
| `tps` | Transactions per second |
| `database_size` | Database size in bytes |
| `replication_lag_bytes` | Replication lag in bytes |
| `longest_transaction_seconds` | Duration of longest running transaction |
| `idle_in_transaction_seconds` | Duration of longest idle-in-transaction |

**Operators:** `>`, `<`, `>=`, `<=`, `==`, `!=`

**Expressions:** Metrics can be combined with `+`, `-`, `*`, `/` operators and parentheses for complex conditions like `(active_connections / max_connections) * 100`.

**Message Templates:**

Custom alert messages support Go `text/template` syntax with these fields:

| Field | Description | Example |
|-------|-------------|---------|
| `{{.Name}}` | Rule name | `high_connections` |
| `{{.Metric}}` | Metric expression | `active_connections` |
| `{{.Warning}}` | Warning threshold | `80` |
| `{{.Critical}}` | Critical threshold | `95` |
| `{{.State}}` | Current state | `warning`, `critical` |
| `{{.PrevState}}` | Previous state | `normal` |
| `{{.Value}}` | Current metric value | `85.5` |
| `{{.Threshold}}` | Crossed threshold | `80` |
| `{{.ValueFmt}}` | Value (2 decimals) | `85.50` |
| `{{.ThreshFmt}}` | Threshold (2 decimals) | `80.00` |

**Alert States:**
- **Normal** - Metric is within acceptable range
- **Warning** - Metric crossed warning threshold (yellow indicator)
- **Critical** - Metric crossed critical threshold (red indicator)

#### Roles
- Browse database roles with attributes (superuser, login, create role, etc.)
- Role details overlay with membership information
- Create, drop, and alter roles
- Attribute display codes: S=Superuser, L=Login, R=CreateRole, D=CreateDB, B=BypassRLS
- Sort by name, attributes, or connection limit
- Copy role name to clipboard
- Read-only mode support (blocks create/drop/alter)
- Press `h` for keybinding help

**Roles Key Bindings:**

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate up/down |
| `g/G` | First/last role |
| `Enter` | View role details |
| `c` | Create new role |
| `x` | Drop selected role |
| `a` | Alter selected role |
| `s/S` | Cycle sort column/toggle direction |
| `y` | Copy role name |
| `r` | Refresh data |

### Status Bar

The status bar displays:
- Connection status indicator (●)
- Database name
- Alert counts when alerts are active (e.g., "2 CRIT 1 WARN")
- Current timestamp
- Active connections count
- Reconnection status (when applicable)

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
./bin/steep --debug
```

Logs are written to `/tmp/steep.log` (or system temp directory).

View logs in real-time:
```bash
tail -f /tmp/steep.log | jq
```

## Background Agent (steep-agent)

The `steep-agent` is an optional background daemon that collects PostgreSQL monitoring data continuously, independent of whether the TUI is running. This enables historical data visibility when the TUI opens.

### Deployment Modes

**Without Agent (default):**
- TUI collects data directly via log parsing
- Shows `[LOG]` indicator in queries view header
- Data collected only while TUI is running

**With Agent:**
- Agent collects data continuously in the background
- TUI auto-detects agent and uses agent-collected data
- Shows `[AGENT]` indicator in queries view header
- Historical data available when TUI opens

### Quick Setup

```bash
# Build the agent
make build-agent

# Test in foreground (for debugging)
./bin/steep-agent run --debug

# Install as service (macOS)
./bin/steep-agent install --user
./bin/steep-agent start

# Install as service (Linux with systemd)
sudo ./bin/steep-agent install
sudo ./bin/steep-agent start

# Check status
./bin/steep-agent status
```

### Agent Commands

| Command | Description |
|---------|-------------|
| `steep-agent install [--user]` | Install as system or user service |
| `steep-agent uninstall` | Remove the service |
| `steep-agent start` | Start the installed service |
| `steep-agent stop` | Stop the running service |
| `steep-agent restart` | Restart the service |
| `steep-agent status [--json]` | Show service status and health |
| `steep-agent run [--debug]` | Run in foreground (for debugging) |
| `steep-agent logs [-f] [-e] [--clear]` | View aggregated logs |

### Agent Configuration

Add agent section to `~/.config/steep/config.yaml`:

```yaml
agent:
  enabled: true

  # Collection intervals per data type
  intervals:
    activity: 2s          # pg_stat_activity
    queries: 5s           # Query stats
    replication: 2s       # Replication lag
    locks: 2s             # Lock monitoring
    metrics: 1s           # Dashboard metrics

  # Data retention periods
  retention:
    activity_history: 24h
    query_stats: 168h     # 7 days
    replication_lag: 24h
    lock_history: 24h
    metrics: 24h

  # Multi-instance monitoring (optional)
  instances:
    - name: primary
      connection: "host=localhost port=5432 dbname=postgres"
    - name: replica1
      connection: "host=replica1 port=5432 dbname=postgres"

  # Background alerting (optional)
  alerts:
    enabled: false
    webhook_url: ""       # Webhook URL for notifications
```

### TUI Auto-Detection

The TUI automatically detects agent presence and coordinates data collection:

- Start TUI alone → TUI collects data via log parsing (`[LOG]` mode)
- Start agent → TUI switches to agent mode (`[AGENT]` mode)
- Stop agent → TUI resumes log parsing automatically

No manual intervention or TUI restarts required.

### Platform Support

| Platform | Service Manager | Installation |
|----------|----------------|--------------|
| macOS | launchd | `./bin/steep-agent install --user` |
| Linux | systemd | `sudo ./bin/steep-agent install` |
| Windows | SCM | `steep-agent.exe install` (as Administrator) |

## Replication Daemon (steep-repl)

The `steep-repl` daemon is the foundation for bidirectional PostgreSQL replication coordination. It provides a PostgreSQL extension for coordination data storage and a cross-platform daemon for cluster management.

### Components

**PostgreSQL Extension (steep_repl)**:
- Creates `steep_repl` schema with nodes, coordinator_state, and audit_log tables
- Requires PostgreSQL 18+
- Built with Rust and pgrx

**Go Daemon (steep-repl)**:
- Cross-platform service management (launchd/systemd/Windows SCM)
- gRPC server for node-to-node communication (port 5433)
- Unix socket/named pipe IPC for TUI communication
- HTTP health endpoint for load balancers and monitoring
- PostgreSQL connection pooling with automatic reconnection

### Quick Setup

```bash
# Build the extension and daemon
make build-repl-ext      # Requires Rust + cargo-pgrx
make build-repl-daemon

# Install the PostgreSQL extension
psql -c "CREATE EXTENSION steep_repl;"

# Run daemon in foreground (for testing)
./bin/steep-repl run --debug

# Install as service
./bin/steep-repl install
./bin/steep-repl start

# Check status
./bin/steep-repl status
```

### Daemon Commands

| Command | Description |
|---------|-------------|
| `steep-repl install [--user]` | Install as system or user service |
| `steep-repl uninstall` | Remove the service |
| `steep-repl start` | Start the installed service |
| `steep-repl stop` | Stop the running service |
| `steep-repl restart` | Restart the service |
| `steep-repl status [--json]` | Show service status and health |
| `steep-repl run [--debug]` | Run in foreground (for debugging) |
| `steep-repl init-tls` | Generate mTLS certificates for secure node communication |
| `steep-repl health HOST:PORT` | Check health of a remote node via gRPC |

### Configuration

Create `~/.config/steep/repl.config.yaml` (separate from main config.yaml):

```yaml
repl:
  enabled: true
  node_id: "node-1"
  node_name: "Primary Node"

  # PostgreSQL connection (where steep_repl extension is installed)
  postgresql:
    host: localhost
    port: 5432
    database: postgres
    user: postgres
    sslmode: prefer
    # password_command: "pass show postgres/repl"

  # gRPC server for node-to-node communication
  grpc:
    port: 5433
    # Optional TLS configuration (use init-tls to generate certs)
    # tls:
    #   cert_file: /path/to/server.crt
    #   key_file: /path/to/server.key
    #   ca_file: /path/to/ca.crt

  # HTTP health endpoint (optional)
  http:
    enabled: false
    port: 8080

  # IPC socket for TUI communication (optional)
  ipc:
    enabled: false

debug: false
```

See `configs/repl.config.yaml.example` for full documentation.

### Platform Support

| Platform | Service Manager | Installation |
|----------|----------------|--------------|
| macOS | launchd | `./bin/steep-repl install` |
| Linux | systemd | `sudo ./bin/steep-repl install` |
| Windows | SCM | `steep-repl.exe install` (as Administrator) |

## Error Handling

Steep provides helpful error messages for common issues:

### Connection Errors
- PostgreSQL not running
- Authentication failures
- Database doesn't exist
- Network issues
- SSL/TLS errors

### Automatic Reconnection

If the database connection is lost, Steep automatically attempts to reconnect with exponential backoff:

- Attempt 1: 1 second delay
- Attempt 2: 2 seconds delay
- Attempt 3: 4 seconds delay
- Attempt 4: 8 seconds delay
- Attempt 5: 16 seconds delay
- Maximum: 5 attempts, 30 second cap

## Development

### Project Structure

```
steep/
├── cmd/
│   ├── steep/          # TUI application entry point
│   ├── steep-agent/    # Background agent entry point
│   └── steep-repl/     # Replication daemon entry point
├── extensions/
│   └── steep_repl/     # PostgreSQL extension (Rust/pgrx)
├── internal/
│   ├── agent/          # Background agent implementation
│   │   ├── collectors/ # Data collectors (activity, queries, etc.)
│   │   ├── agent.go    # Agent orchestration
│   │   ├── service.go  # kardianos/service integration
│   │   └── retention.go # Data retention/pruning
│   ├── app/            # Application model and message handlers
│   ├── config/         # Configuration management
│   ├── db/             # Database connection and operations
│   ├── logger/         # Structured logging
│   ├── repl/           # Replication daemon
│   │   ├── config/     # Replication config (YAML)
│   │   ├── daemon/     # Daemon lifecycle management
│   │   ├── grpc/       # gRPC server + proto definitions
│   │   ├── ipc/        # Unix socket/named pipe IPC
│   │   ├── pool/       # PostgreSQL connection pool
│   │   └── store/      # Node store (PostgreSQL-backed)
│   └── ui/             # User interface components
│       ├── components/ # Reusable UI components
│       ├── views/      # View implementations
│       └── styles/     # Color schemes and styling
├── tests/integration/repl/ # Replication integration tests
├── docs/               # Documentation
└── specs/              # Feature specifications
```

### Building from Source

```bash
# Install dependencies
go mod download

# Build
go build -o bin/steep cmd/steep/main.go

# Run tests
go test ./...

# Run with race detector
go run -race cmd/steep/main.go
```

## Troubleshooting

### Terminal Too Small

```
Terminal too small: 70x20 (minimum required: 80x24)
Please resize your terminal and try again.
```

**Solution:** Resize your terminal to at least 80 columns by 24 rows.

### Connection Refused

```
Connection refused: PostgreSQL is not accepting connections.
```

**Solutions:**
1. Verify PostgreSQL is running: `brew services list | grep postgresql`
2. Check port configuration in config.yaml
3. Verify firewall settings

### Authentication Failed

```
Authentication failed: Invalid username or password.
```

**Solutions:**
1. Verify credentials in config.yaml
2. Check PGPASSWORD environment variable
3. Test password command manually
4. Try interactive password prompt

### SSL Errors

```
SSL/TLS error: Secure connection failed.
```

**Solutions:**
1. Verify sslmode is compatible with server configuration
2. Check certificate paths and permissions
3. See [SSL Configuration Guide](docs/SSL_CONFIGURATION.md)

### Permission Denied

```
Permission denied: User does not have required privileges.
```

**Solutions:**
1. Verify database user has CONNECT privilege
2. Check pg_hba.conf allows connections from your host
3. Grant required permissions

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Roadmap

- [x] Query performance view
- [x] Lock monitoring view
- [x] Table statistics view
- [x] SQL Editor with history and snippets
- [x] Configuration viewer
- [x] Replication monitoring
- [x] Log viewer
- [x] Database operations (maintenance, permissions, roles)
- [x] Advanced visualizations (time-series graphs, sparklines, bar charts, heatmaps)
- [x] Alert system (threshold-based alerts, expression rules, history, acknowledgment)
- [x] Service architecture (steep-agent background daemon, multi-instance monitoring, TUI auto-detection)
- [x] Replication foundation (steep-repl daemon, PostgreSQL extension, gRPC/IPC, cross-platform service)
- [ ] Export metrics to Prometheus
- [ ] Light theme
- [ ] Custom color schemes
