# Data Model: Bidirectional Replication Foundation

**Date**: 2025-12-04
**Feature**: 014-repl-foundation

## Overview

This feature creates three PostgreSQL tables in the `steep_repl` schema to support bidirectional replication coordination.

## Entities

### 1. Node

Represents a PostgreSQL database instance participating in bidirectional replication.

**Table**: `steep_repl.nodes`

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| node_id | TEXT | PRIMARY KEY | - | Unique identifier (UUID format recommended) |
| node_name | TEXT | NOT NULL | - | Human-readable name |
| host | TEXT | NOT NULL | - | Hostname or IP address |
| port | INTEGER | NOT NULL | 5432 | PostgreSQL port |
| priority | INTEGER | NOT NULL | 50 | Coordinator election priority (1-100, higher = preferred) |
| is_coordinator | BOOLEAN | NOT NULL | false | Currently elected coordinator |
| last_seen | TIMESTAMPTZ | - | NULL | Last heartbeat timestamp |
| status | TEXT | NOT NULL | 'unknown' | Node status |

**Status Values**:
- `unknown` - Initial state, no heartbeat received
- `healthy` - Responding to health checks
- `degraded` - Responding but with issues
- `unreachable` - Not responding to health checks
- `offline` - Explicitly taken offline

**Indexes**:
- `PRIMARY KEY (node_id)`
- `idx_nodes_status (status)` - For filtering by status
- `idx_nodes_coordinator (is_coordinator) WHERE is_coordinator = true` - Partial index

**Validation Rules**:
- `priority` must be between 1 and 100
- `port` must be between 1 and 65535
- `host` must not be empty

---

### 2. Coordinator State

Key-value store for cluster-wide coordination data used by the elected coordinator.

**Table**: `steep_repl.coordinator_state`

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| key | TEXT | PRIMARY KEY | - | State key |
| value | JSONB | NOT NULL | - | State value (JSON object) |
| updated_at | TIMESTAMPTZ | NOT NULL | now() | Last update timestamp |

**Reserved Keys** (populated by later features):
- `cluster_version` - Schema version for upgrade detection
- `range_allocator` - Next available identity range (014-c)
- `ddl_sequence` - DDL operation sequence number (014-e)

**Indexes**:
- `PRIMARY KEY (key)`

**Validation Rules**:
- `value` must be valid JSONB
- `key` should follow naming convention: `category.subcategory`

---

### 3. Audit Log Entry

Immutable record of system activity for compliance and debugging.

**Table**: `steep_repl.audit_log`

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | BIGSERIAL | PRIMARY KEY | auto | Unique log entry ID |
| occurred_at | TIMESTAMPTZ | NOT NULL | now() | Event timestamp |
| action | TEXT | NOT NULL | - | Action type |
| actor | TEXT | NOT NULL | - | Who performed action (role@host format) |
| target_type | TEXT | - | NULL | Type of target entity |
| target_id | TEXT | - | NULL | ID of target entity |
| old_value | JSONB | - | NULL | Previous state (for updates) |
| new_value | JSONB | - | NULL | New state (for creates/updates) |
| client_ip | INET | - | NULL | Client IP address |
| success | BOOLEAN | NOT NULL | true | Whether action succeeded |
| error_message | TEXT | - | NULL | Error details if failed |

**Action Types** (this feature):
- `node.registered` - New node joined cluster
- `node.updated` - Node configuration changed
- `node.removed` - Node left cluster
- `coordinator.elected` - New coordinator elected
- `state.updated` - Coordinator state changed
- `daemon.started` - Daemon process started
- `daemon.stopped` - Daemon process stopped

**Target Types**:
- `node` - steep_repl.nodes entry
- `state` - steep_repl.coordinator_state entry
- `daemon` - steep-repl daemon

**Indexes**:
- `PRIMARY KEY (id)`
- `idx_audit_log_occurred_at (occurred_at DESC)` - For time-based queries
- `idx_audit_log_actor (actor)` - For actor filtering
- `idx_audit_log_action (action)` - For action type filtering
- `idx_audit_log_target (target_type, target_id) WHERE target_type IS NOT NULL` - Partial index

**Retention**:
- Default: 2 years (per design document)
- Configurable via `repl.audit.retention` config option
- Automatic pruning by steep-repl daemon (hourly check)

---

## Entity Relationships

```
┌─────────────────┐
│     nodes       │
├─────────────────┤
│ node_id (PK)    │◄─────────────────┐
│ node_name       │                  │
│ host            │                  │
│ port            │                  │ target_id (when target_type = 'node')
│ priority        │                  │
│ is_coordinator  │                  │
│ last_seen       │                  │
│ status          │                  │
└─────────────────┘                  │
                                     │
┌─────────────────┐                  │
│coordinator_state│                  │
├─────────────────┤                  │
│ key (PK)        │◄─────────────────┤ target_id (when target_type = 'state')
│ value           │                  │
│ updated_at      │                  │
└─────────────────┘                  │
                                     │
┌─────────────────────────────────┐  │
│          audit_log              │  │
├─────────────────────────────────┤  │
│ id (PK)                         │  │
│ occurred_at                     │  │
│ action                          │  │
│ actor                           │  │
│ target_type  ───────────────────┼──┘
│ target_id    ───────────────────┘
│ old_value                       │
│ new_value                       │
│ client_ip                       │
│ success                         │
│ error_message                   │
└─────────────────────────────────┘
```

**Notes**:
- `audit_log.target_id` references either `nodes.node_id` or `coordinator_state.key` depending on `target_type`
- No foreign key constraints on audit_log to preserve history if nodes are removed
- Later features will add additional tables (identity_ranges, conflict_log, ddl_queue)

---

## State Transitions

### Node Status

```
          ┌──────────────────────────────────────────┐
          │                                          │
          ▼                                          │
    ┌─────────┐    heartbeat    ┌─────────┐         │
    │ unknown │───────────────► │ healthy │──┐      │
    └─────────┘                 └─────────┘  │      │
                                     │       │      │
                          slow       │       │ fast │
                          response   ▼       │      │
                                ┌──────────┐ │      │
                                │ degraded │─┘      │
                                └──────────┘        │
                                     │              │
                          no         │              │
                          heartbeat  ▼              │
                                ┌─────────────┐     │
                                │ unreachable │─────┘
                                └─────────────┘
                                     │
                          admin      │
                          action     ▼
                                ┌─────────┐
                                │ offline │
                                └─────────┘
```

### Coordinator Election

- Only one node can have `is_coordinator = true` at any time
- Election triggered when:
  - Current coordinator becomes `unreachable`
  - Current coordinator set to `offline`
  - No coordinator exists (initial cluster)
- Highest `priority` node among `healthy` nodes wins
- Ties broken by lexicographically smallest `node_id`

---

## SQL Schema

```sql
-- Extension installation creates this schema
CREATE SCHEMA IF NOT EXISTS steep_repl;

-- Nodes table
CREATE TABLE steep_repl.nodes (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    priority INTEGER NOT NULL DEFAULT 50
        CHECK (priority >= 1 AND priority <= 100),
    is_coordinator BOOLEAN NOT NULL DEFAULT false,
    last_seen TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('unknown', 'healthy', 'degraded', 'unreachable', 'offline')),
    CHECK (port >= 1 AND port <= 65535),
    CHECK (host <> '')
);

CREATE INDEX idx_nodes_status ON steep_repl.nodes(status);
CREATE INDEX idx_nodes_coordinator ON steep_repl.nodes(is_coordinator)
    WHERE is_coordinator = true;

-- Coordinator state table
CREATE TABLE steep_repl.coordinator_state (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Audit log table
CREATE TABLE steep_repl.audit_log (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    target_type TEXT,
    target_id TEXT,
    old_value JSONB,
    new_value JSONB,
    client_ip INET,
    success BOOLEAN NOT NULL DEFAULT true,
    error_message TEXT
);

CREATE INDEX idx_audit_log_occurred_at ON steep_repl.audit_log(occurred_at DESC);
CREATE INDEX idx_audit_log_actor ON steep_repl.audit_log(actor);
CREATE INDEX idx_audit_log_action ON steep_repl.audit_log(action);
CREATE INDEX idx_audit_log_target ON steep_repl.audit_log(target_type, target_id)
    WHERE target_type IS NOT NULL;
```

---

## Go Types

```go
// internal/repl/models/node.go
type Node struct {
    NodeID        string    `db:"node_id"`
    NodeName      string    `db:"node_name"`
    Host          string    `db:"host"`
    Port          int       `db:"port"`
    Priority      int       `db:"priority"`
    IsCoordinator bool      `db:"is_coordinator"`
    LastSeen      *time.Time `db:"last_seen"`
    Status        string    `db:"status"`
}

type NodeStatus string

const (
    NodeStatusUnknown     NodeStatus = "unknown"
    NodeStatusHealthy     NodeStatus = "healthy"
    NodeStatusDegraded    NodeStatus = "degraded"
    NodeStatusUnreachable NodeStatus = "unreachable"
    NodeStatusOffline     NodeStatus = "offline"
)

// internal/repl/models/state.go
type CoordinatorState struct {
    Key       string    `db:"key"`
    Value     []byte    `db:"value"` // JSONB
    UpdatedAt time.Time `db:"updated_at"`
}

// internal/repl/models/audit.go
type AuditLogEntry struct {
    ID           int64      `db:"id"`
    OccurredAt   time.Time  `db:"occurred_at"`
    Action       string     `db:"action"`
    Actor        string     `db:"actor"`
    TargetType   *string    `db:"target_type"`
    TargetID     *string    `db:"target_id"`
    OldValue     []byte     `db:"old_value"` // JSONB
    NewValue     []byte     `db:"new_value"` // JSONB
    ClientIP     *string    `db:"client_ip"`
    Success      bool       `db:"success"`
    ErrorMessage *string    `db:"error_message"`
}
```
