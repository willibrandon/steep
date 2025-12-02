# Research: Service Architecture (steep-agent)

**Date**: 2025-12-01
**Feature**: 013-service-architecture

## Research Topics

### 1. kardianos/service Library

**Decision**: Use kardianos/service v1.2.x for cross-platform service management

**Rationale**:
- Battle-tested library with 3k+ GitHub stars
- Unified API across Windows Services, systemd, launchd, SysV, Upstart, OpenRC
- MIT license compatible with project
- Active maintenance (v1.2.0+ adds Shutdowner interface for graceful shutdown)
- Clean interface model: implement `Start()` and `Stop()` methods

**Key Implementation Details**:

```go
// Interface to implement
type program struct {
    agent *Agent
}

func (p *program) Start(s service.Service) error {
    // Must return quickly - start work in goroutine
    go p.agent.Run()
    return nil
}

func (p *program) Stop(s service.Service) error {
    // Graceful shutdown - signal agent to stop
    p.agent.Shutdown()
    return nil
}
```

**Platform Support**:
| Platform | Service Manager | Status |
|----------|-----------------|--------|
| Windows XP+ | Windows Services (SCM) | Full support |
| macOS | launchd | Full support |
| Linux | systemd | Full support |
| Linux | Upstart | Full support |
| Linux | SysV init | Full support |
| Linux | OpenRC | Full support |

**Known Limitations**:
- Dependencies field not fully implemented on Linux/macOS (not needed for Steep)
- macOS UserService interactive detection unreliable (use `--user` flag explicitly)
- `Service.Run()` must be called early and blocks until stopped

**Alternatives Considered**:
| Alternative | Rejected Because |
|-------------|------------------|
| Manual init scripts | Platform-specific maintenance burden |
| systemd-only | No Windows/macOS support |
| supervisord | External dependency, not embedded |

---

### 2. Agent Detection Mechanism

**Decision**: Use PID file with SQLite write lock as backup

**Rationale**:
- PID file is standard Unix convention, works on Windows
- SQLite WAL mode allows concurrent readers but single writer
- TUI can detect agent by: (1) checking PID file exists and process running, (2) checking SQLite `steep_agent_status` table

**Implementation**:
```go
// PID file location
const PIDFile = "~/.config/steep/steep-agent.pid"

// Agent writes status to SQLite on startup
type AgentStatus struct {
    PID         int       `db:"pid"`
    StartTime   time.Time `db:"start_time"`
    LastCollect time.Time `db:"last_collect"`
    Version     string    `db:"version"`
}
```

**Detection Algorithm (TUI)**:
1. Check if `--standalone` or `--client` flag provided → use explicit mode
2. Read PID file → check if process exists (kill -0 or equivalent)
3. If process exists, query `steep_agent_status` table for freshness
4. If last_collect within 2x collection interval → agent healthy, use client mode
5. Otherwise → fall back to standalone mode

**Alternatives Considered**:
| Alternative | Rejected Because |
|-------------|------------------|
| Unix socket | Platform-specific, adds IPC complexity |
| TCP port | Firewall issues, port conflicts |
| Named pipe | Platform-specific implementation |
| File lock only | Can't detect stale locks from crashed processes |

---

### 3. Multi-Instance Data Model

**Decision**: Add `instance_name` column to all time-series tables

**Rationale**:
- Minimal schema change to existing tables
- Clear data provenance for TUI filtering
- Single SQLite database remains single writer (agent)
- Indexes on (instance_name, timestamp) for efficient queries

**Schema Addition**:
```sql
-- Add to existing tables (activity_snapshots, query_stats, etc.)
ALTER TABLE activity_snapshots ADD COLUMN instance_name TEXT DEFAULT 'default';
CREATE INDEX idx_activity_instance_time ON activity_snapshots(instance_name, timestamp);

-- New table for instance metadata
CREATE TABLE agent_instances (
    name TEXT PRIMARY KEY,
    connection_string TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    last_seen TIMESTAMP,
    error_message TEXT
);
```

**Alternatives Considered**:
| Alternative | Rejected Because |
|-------------|------------------|
| Separate SQLite per instance | Complicates TUI, multiple file handles |
| Instance ID (integer) | Less readable, requires join for instance name |
| Namespace prefix in table names | Major schema change, breaks existing queries |

---

### 4. Retention Pruning Strategy

**Decision**: Hourly prune goroutine with configurable retention per data type

**Rationale**:
- Existing alert history prune pattern already in codebase
- Hourly frequency balances disk space vs. write overhead
- Per-data-type retention allows different policies (activity: 24h, queries: 7d)
- SQLite DELETE with LIMIT to avoid long-running transactions

**Implementation**:
```go
func (a *Agent) startRetentionPruner() {
    ticker := time.NewTicker(1 * time.Hour)
    for range ticker.C {
        for dataType, retention := range a.config.Retention {
            cutoff := time.Now().Add(-retention)
            // DELETE with LIMIT to avoid blocking
            a.pruneOlderThan(dataType, cutoff, 10000)
        }
    }
}
```

**Default Retention Periods**:
| Data Type | Default | Rationale |
|-----------|---------|-----------|
| activity_history | 24h | High volume, recent data most relevant |
| query_stats | 7d | Lower volume, trend analysis valuable |
| replication_lag | 24h | Time-series for sparklines |
| lock_history | 24h | Deadlock investigation window |
| alert_events | 30d | Existing default from Feature 012 |
| metrics | 24h | Dashboard sparklines/graphs |

---

### 5. Graceful Shutdown

**Decision**: Context cancellation with 5-second timeout, WAL checkpoint before exit

**Rationale**:
- Context cancellation is idiomatic Go pattern
- 5-second timeout balances responsiveness vs. data safety
- WAL checkpoint ensures durability without waiting for auto-checkpoint
- Matches kardianos/service expectation that Stop() returns quickly

**Implementation**:
```go
func (a *Agent) Shutdown() {
    // Signal all collectors to stop
    a.cancel()

    // Wait for in-flight operations with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Wait for collector goroutines
    done := make(chan struct{})
    go func() {
        a.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        // Clean shutdown
    case <-ctx.Done():
        // Force shutdown after timeout
        log.Warn("Shutdown timeout, forcing exit")
    }

    // WAL checkpoint for durability
    a.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
    a.db.Close()
}
```

---

### 6. Client Mode TUI Behavior

**Decision**: Read-only SQLite access, no PostgreSQL connection required

**Rationale**:
- Faster startup (no connection establishment)
- Lower resource usage (no connection pool)
- Agent handles all data collection
- TUI becomes pure viewer of SQLite data

**Behavior Changes in Client Mode**:
| Feature | Standalone Mode | Client Mode |
|---------|-----------------|-------------|
| PostgreSQL connection | Required | Not required |
| Data collection | TUI-driven | Agent-driven |
| SQLite access | Read-write | Read-only |
| Kill connection (x key) | Direct pg_cancel_backend | Via agent (future) or disabled |
| SQL Editor | Direct execution | Direct execution (still needs PG) |
| Maintenance ops | Direct execution | Direct execution (still needs PG) |

**Note**: SQL Editor and maintenance operations still require PostgreSQL connection even in client mode. Only monitoring data comes from SQLite.

---

## Summary

All research topics resolved. No NEEDS CLARIFICATION items remain.

| Topic | Decision |
|-------|----------|
| Service library | kardianos/service v1.2.x |
| Agent detection | PID file + SQLite status table |
| Multi-instance | instance_name column in tables |
| Retention | Hourly prune with per-type config |
| Graceful shutdown | Context cancellation + 5s timeout + WAL checkpoint |
| Client mode | Read-only SQLite, optional PG for interactive ops |
