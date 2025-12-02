# Quickstart: Service Architecture (steep-agent)

**Date**: 2025-12-01
**Feature**: 013-service-architecture

## Overview

This feature adds a background daemon (`steep-agent`) that collects PostgreSQL monitoring data continuously, independent of whether the TUI is running. The TUI can operate in two modes:

- **Standalone Mode**: Current behavior - TUI connects directly to PostgreSQL
- **Client Mode**: TUI reads from agent-maintained SQLite database

## Prerequisites

- Go 1.21+ installed
- PostgreSQL instance(s) accessible
- Existing Steep configuration at `~/.config/steep/config.yaml`

## Quick Setup

### 1. Build the Agent

```bash
# Build both binaries
make build

# Or build agent separately
go build -o bin/steep-agent cmd/steep-agent/main.go
```

### 2. Configure Agent (Optional)

Add agent section to `~/.config/steep/config.yaml`:

```yaml
agent:
  enabled: true
  intervals:
    activity: 2s
    queries: 5s
    replication: 2s
  retention:
    activity_history: 24h
    query_stats: 168h  # 7 days
```

### 3. Test in Foreground

```bash
# Run agent in foreground to verify setup
./bin/steep-agent run --debug

# In another terminal, verify TUI detects agent
./bin/steep  # Should show "Agent: Connected" in status bar
```

### 4. Install as Service

```bash
# Linux (systemd) - requires sudo
sudo ./bin/steep-agent install
sudo ./bin/steep-agent start

# macOS (launchd) - user service
./bin/steep-agent install --user
./bin/steep-agent start

# Windows (Services) - run as Administrator
steep-agent.exe install
steep-agent.exe start
```

### 5. Verify Service

```bash
# Check status
./bin/steep-agent status

# View logs
journalctl -u steep-agent -f  # Linux
log show --predicate 'subsystem == "steep-agent"' --last 1h  # macOS
```

## Usage Patterns

### Standalone Mode (No Agent)

```bash
# Explicit standalone (ignores running agent)
./bin/steep --standalone

# Auto-detect (uses standalone if no agent running)
./bin/steep
```

### Client Mode (With Agent)

```bash
# Require agent (fails if not running)
./bin/steep --client

# Auto-detect (uses client mode if agent healthy)
./bin/steep
```

### Multi-Instance Monitoring

```yaml
# config.yaml
agent:
  instances:
    - name: primary
      connection: "host=db1.example.com port=5432 dbname=prod"
    - name: replica1
      connection: "host=db2.example.com port=5432 dbname=prod"
    - name: replica2
      connection: "host=db3.example.com port=5432 dbname=prod"
```

## Development Workflow

### Running Tests

```bash
# Unit tests
go test ./internal/agent/...

# Integration tests (requires Docker for testcontainers)
go test ./tests/integration/agent/...
```

### Debugging

```bash
# Run agent with debug logging
./bin/steep-agent run --debug

# Check SQLite database directly
sqlite3 ~/.config/steep/steep.db "SELECT * FROM agent_status"
sqlite3 ~/.config/steep/steep.db "SELECT * FROM agent_instances"
```

### Common Issues

| Issue | Solution |
|-------|----------|
| "Permission denied" on install | Use `sudo` (Linux) or run as Administrator (Windows) |
| "Service already installed" | Run `steep-agent uninstall` first |
| TUI not detecting agent | Check PID file exists: `cat ~/.config/steep/steep-agent.pid` |
| Agent not collecting data | Check PostgreSQL connection: `steep-agent run --debug` |

## Architecture Summary

```
┌─────────────────────────────────────────────────────────────────┐
│                     Deployment Modes                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Standalone (current):                                          │
│  steep ──────▶ PostgreSQL ──────▶ SQLite (embedded writes)     │
│                                                                  │
│  With Agent:                                                     │
│  steep-agent ──▶ PostgreSQL ──▶ SQLite ◀── steep (reads only)  │
│       │              │                                          │
│       └──────▶ PostgreSQL2 (multi-instance)                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Key Files

| File | Purpose |
|------|---------|
| `cmd/steep-agent/main.go` | Agent entry point, Cobra CLI |
| `internal/agent/agent.go` | Agent orchestration |
| `internal/agent/collector.go` | Data collection coordinator |
| `internal/agent/service.go` | kardianos/service integration |
| `internal/agent/retention.go` | Data retention/pruning |
| `internal/config/config.go` | Agent config parsing (modified) |
| `internal/app/app.go` | TUI client mode support (modified) |
