# CLI Interface Contract: steep-agent

**Date**: 2025-12-01
**Feature**: 013-service-architecture

## steep-agent Commands

### Service Management

#### `steep-agent install [flags]`

Install steep-agent as a system service.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--user` | bool | false | Install as user service instead of system |
| `--config` | string | ~/.config/steep/config.yaml | Path to config file |

**Exit Codes**:
- 0: Success
- 1: Permission denied (need elevated privileges)
- 2: Service already installed
- 3: Configuration error

**Example**:
```bash
# System service (requires sudo on Unix)
sudo steep-agent install

# User service (no sudo required)
steep-agent install --user
```

---

#### `steep-agent uninstall`

Remove steep-agent service.

**Exit Codes**:
- 0: Success
- 1: Permission denied
- 2: Service not installed

---

#### `steep-agent start`

Start the installed service.

**Exit Codes**:
- 0: Success
- 1: Service not installed
- 2: Service already running
- 3: Start failed (check logs)

---

#### `steep-agent stop`

Stop the running service.

**Exit Codes**:
- 0: Success
- 1: Service not running
- 2: Stop failed (timeout)

---

#### `steep-agent restart`

Restart the service (stop + start).

**Exit Codes**:
- 0: Success
- 1: Service not installed
- 2: Restart failed

---

#### `steep-agent status`

Show service status and health information.

**Output Format** (JSON when `--json` flag):
```json
{
  "state": "running",
  "pid": 12345,
  "uptime": "2h 15m 30s",
  "last_collect": "2025-12-01T10:30:45Z",
  "instances": [
    {
      "name": "primary",
      "status": "connected",
      "last_seen": "2025-12-01T10:30:45Z"
    }
  ],
  "errors": []
}
```

**Human-Readable Output**:
```
steep-agent status: running
  PID:          12345
  Uptime:       2h 15m 30s
  Last Collect: 2025-12-01 10:30:45

Instances:
  primary     connected    (last seen: 5s ago)
  replica1    connected    (last seen: 5s ago)

Errors: none
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | Output in JSON format |

**Exit Codes**:
- 0: Running and healthy
- 1: Service not installed
- 2: Service stopped
- 3: Service running but unhealthy

---

### Direct Run (Debugging)

#### `steep-agent run [flags]`

Run agent in foreground (not as service). Useful for debugging.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--config` | string | ~/.config/steep/config.yaml | Path to config file |
| `--debug` | bool | false | Enable debug logging |

**Behavior**:
- Runs until SIGINT/SIGTERM received
- Logs to stdout/stderr
- Does not daemonize
- PID file still created for TUI detection

**Exit Codes**:
- 0: Clean shutdown
- 1: Configuration error
- 2: Database error
- 3: All PostgreSQL connections failed

---

## steep TUI Flags (Additions)

### `steep [flags]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--standalone` | bool | false | Force standalone mode (ignore agent) |
| `--client` | bool | false | Force client mode (require agent) |

**Mode Detection Logic** (when neither flag provided):
1. Check for PID file at `~/.config/steep/steep-agent.pid`
2. Verify process is running (kill -0 equivalent)
3. Query `agent_status` table for `last_collect` freshness
4. If agent healthy → client mode
5. Otherwise → standalone mode

**Client Mode Behavior**:
- No PostgreSQL connection pool created initially
- Monitoring data read from SQLite only
- SQL Editor and maintenance ops still connect to PostgreSQL on demand
- Status bar shows "Agent: Connected" with last collect timestamp

**Error Messages**:

```
# --client flag but agent not running
Error: steep-agent is not running.

To start the agent:
  steep-agent start

Or run in standalone mode:
  steep --standalone
```

---

## Configuration Schema (YAML)

```yaml
# ~/.config/steep/config.yaml

# Existing connection section (unchanged)
connection:
  host: localhost
  port: 5432
  database: postgres
  user: postgres
  # ...

# NEW: Agent-specific settings
agent:
  enabled: true  # Enable agent features

  # Collection intervals per data type
  intervals:
    activity: 2s          # pg_stat_activity
    queries: 5s           # Query stats
    replication: 2s       # Replication lag
    locks: 2s             # Lock monitoring
    tables: 30s           # Table statistics
    metrics: 1s           # Dashboard metrics

  # Data retention periods
  retention:
    activity_history: 24h
    query_stats: 168h     # 7 days
    replication_lag: 24h
    lock_history: 24h
    metrics: 24h

  # Multi-instance support (optional)
  instances:
    - name: primary
      connection: "host=localhost port=5432 dbname=postgres"
    - name: replica1
      connection: "host=replica1 port=5432 dbname=postgres"

  # Background alerting (P3)
  alerts:
    enabled: false
    webhook_url: ""
    # Uses existing alerts.rules from Feature 012
```

**Validation Rules**:
- `intervals.*` must be >= 1s and <= 60s
- `retention.*` must be >= 1h and <= 720h (30 days)
- `instances[].name` must be unique, alphanumeric with hyphens/underscores
- `instances[].connection` must be valid PostgreSQL DSN
