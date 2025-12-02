# Data Model: Service Architecture (steep-agent)

**Date**: 2025-12-01
**Feature**: 013-service-architecture

## Entities

### 1. AgentStatus

Tracks the running agent's state for TUI detection and health monitoring.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| id | INTEGER | PRIMARY KEY, always 1 | Single row table |
| pid | INTEGER | NOT NULL | Agent process ID |
| start_time | TIMESTAMP | NOT NULL | Agent start time |
| last_collect | TIMESTAMP | NOT NULL | Last successful collection |
| version | TEXT | NOT NULL | Agent version string |
| config_hash | TEXT | | Hash of config for drift detection |

**Table**: `agent_status`
**Lifecycle**: Created on agent start, updated each collection cycle, deleted on clean shutdown

---

### 2. AgentInstance

Metadata for each monitored PostgreSQL instance (multi-instance support).

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| name | TEXT | PRIMARY KEY | Instance identifier (e.g., "primary", "replica1") |
| connection_string | TEXT | NOT NULL | PostgreSQL connection DSN (password excluded) |
| status | TEXT | NOT NULL, DEFAULT 'unknown' | connected, disconnected, error, unknown |
| last_seen | TIMESTAMP | | Last successful connection |
| error_message | TEXT | | Most recent error if status=error |

**Table**: `agent_instances`
**Lifecycle**: Created from config on agent start, updated on connection state changes

**Status Transitions**:
```
unknown → connected (first successful connection)
connected → disconnected (connection lost)
disconnected → connected (reconnection successful)
connected → error (query failure)
error → connected (recovery)
```

---

### 3. Schema Changes to Existing Tables

Add `instance_name` column to enable multi-instance data separation.

**Affected Tables**:
- `activity_snapshots`
- `query_stats`
- `replication_lag_history`
- `lock_snapshots`
- `deadlock_events`
- `metrics_history`
- `alert_events`

**Column Addition**:
```sql
ALTER TABLE {table_name} ADD COLUMN instance_name TEXT DEFAULT 'default';
CREATE INDEX idx_{table_name}_instance_time ON {table_name}(instance_name, timestamp);
```

**Migration Strategy**:
- Existing data gets `instance_name = 'default'`
- New data from single-instance config also uses 'default'
- Multi-instance data uses configured instance names

---

### 4. AgentConfig (Runtime)

In-memory representation of agent configuration (not persisted, loaded from YAML).

| Field | Type | Description |
|-------|------|-------------|
| Enabled | bool | Agent enabled in config |
| Intervals | map[string]Duration | Collection intervals per data type |
| Retention | map[string]Duration | Retention periods per data type |
| Instances | []InstanceConfig | PostgreSQL instances to monitor |
| Alerts.Enabled | bool | Background alerting enabled |
| Alerts.WebhookURL | string | Webhook endpoint for notifications |

**InstanceConfig**:
| Field | Type | Description |
|-------|------|-------------|
| Name | string | Instance identifier |
| Connection | string | PostgreSQL connection string |

---

## Relationships

```
┌─────────────────┐
│  AgentStatus    │ (singleton)
│  - agent state  │
└────────┬────────┘
         │ manages
         ▼
┌─────────────────┐     collects into     ┌──────────────────────┐
│ AgentInstance   │─────────────────────▶│ Existing Tables      │
│ - primary       │                       │ + instance_name col  │
│ - replica1      │                       │ - activity_snapshots │
│ - ...           │                       │ - query_stats        │
└─────────────────┘                       │ - replication_lag    │
                                          │ - lock_snapshots     │
                                          │ - metrics_history    │
                                          └──────────────────────┘
```

---

## Validation Rules

### AgentStatus
- `pid` must be > 0
- `start_time` must be <= `last_collect`
- `version` must match semantic version pattern

### AgentInstance
- `name` must be non-empty, alphanumeric with hyphens/underscores
- `connection_string` must be valid PostgreSQL DSN
- `status` must be one of: connected, disconnected, error, unknown

### Instance Name (in data tables)
- Must exist in `agent_instances` table or be 'default'
- Foreign key relationship optional (performance consideration)

---

## SQLite Schema DDL

```sql
-- Agent status (singleton)
CREATE TABLE IF NOT EXISTS agent_status (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    pid INTEGER NOT NULL,
    start_time TIMESTAMP NOT NULL,
    last_collect TIMESTAMP NOT NULL,
    version TEXT NOT NULL,
    config_hash TEXT
);

-- Agent instances (multi-instance support)
CREATE TABLE IF NOT EXISTS agent_instances (
    name TEXT PRIMARY KEY,
    connection_string TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('connected', 'disconnected', 'error', 'unknown')),
    last_seen TIMESTAMP,
    error_message TEXT
);

-- Migration: Add instance_name to existing tables
-- (Applied via schema version check on startup)
```

---

## Data Volume Estimates

| Table | Rows/Day (single instance) | Retention | Max Rows |
|-------|---------------------------|-----------|----------|
| agent_status | 1 | Forever | 1 |
| agent_instances | N | Forever | N (typically 1-5) |
| activity_snapshots | 43,200 (2s interval) | 24h | 43,200 |
| query_stats | 17,280 (5s interval) | 7d | 120,960 |
| replication_lag | 43,200 (2s interval) | 24h | 43,200 |
| metrics_history | 86,400 (1s interval) | 24h | 86,400 |

**Estimated Database Size**: ~50-100 MB with default retention (single instance)
