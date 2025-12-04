# Bidirectional Replication Implementation Guide

## Complete Spec-Kit Workflow Roadmap

This guide provides the complete sequence of `/speckit.specify` commands to build Steep's bidirectional replication system. Each feature is independently deliverable and testable.

**Reference Design Document**: `docs/BIDIRECTIONAL_REPLICATION.md` v0.8

---

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Design Document Section Mapping](#design-document-section-mapping)
- [Feature Breakdown](#feature-breakdown)
- [Implementation Sequence](#implementation-sequence)
- [Feature Details](#feature-details)
  - [014-a: Foundation & Infrastructure](#feature-014-a-foundation--infrastructure)
  - [014-b: Node Initialization & Snapshots](#feature-014-b-node-initialization--snapshots)
  - [014-c: Identity Range Management](#feature-014-c-identity-range-management)
  - [014-d: Conflict Detection & Resolution](#feature-014-d-conflict-detection--resolution)
  - [014-e: DDL Replication](#feature-014-e-ddl-replication)
  - [014-f: Filtering & Selective Replication](#feature-014-f-filtering--selective-replication)
  - [014-g: Topology & Coordination](#feature-014-g-topology--coordination)
  - [014-h: Monitoring & TUI Integration](#feature-014-h-monitoring--tui-integration)
  - [014-i: Production Hardening](#feature-014-i-production-hardening)
- [Verification Checklist](#verification-checklist)
- [Testing Requirements](#testing-requirements)

---

## Overview

Bidirectional replication is implemented through **9 features** covering all 22 sections of the design document:

| Feature | Branch | Design Doc Sections | Priority |
|---------|--------|---------------------|----------|
| 014-a | `014-a-foundation` | 2, 4, 13, 14 | P1 |
| 014-b | `014-b-node-init` | 6, 17.4 | P1 |
| 014-c | `014-c-identity-ranges` | 5 | P1 |
| 014-d | `014-d-conflict-resolution` | 10, 17.1, 17.2, 17.3, 17.5 | P1 |
| 014-e | `014-e-ddl-replication` | 11 | P2 |
| 014-f | `014-f-filtering` | 7 | P2 |
| 014-g | `014-g-topology` | 12 | P2 |
| 014-h | `014-h-monitoring` | 8, 15 | P2 |
| 014-i | `014-i-production` | 19, 20, 21, 22 | P3 |

**Sections 1, 3, 16, 18**: Context/reference only (Executive Summary, PostgreSQL Version Requirements, Implementation Phases, References)

---

## Prerequisites

1. **Steep core features complete**: Features 001-013 implemented
2. **PostgreSQL 17 or 18**: Required (18 recommended for native conflict logging)
3. **PostgreSQL 17+**: Required for row/column filtering (Section 7)
4. **Rust/pgrx environment**: For steep_repl extension
   - Rust toolchain (MSVC on Windows)
   - `cargo install cargo-pgrx`
   - PostgreSQL development files
5. **Go 1.25+**: For steep-repl daemon

---

## Design Document Section Mapping

Every section from `BIDIRECTIONAL_REPLICATION.md` must be covered:

| Section | Title | Covered In |
|---------|-------|------------|
| 1 | Executive Summary | Context (all features) |
| 2 | Architecture Overview | 014-a |
| 3 | PostgreSQL Version Requirements | Reference only |
| 4 | Cross-Platform Compatibility | 014-a |
| **5** | **Identity Range Management** | **014-c** |
| 5.8 | Composite Primary Keys | 014-c |
| **6** | **Node Initialization and Snapshots** | **014-b** |
| 6.8 | Initial Sync with Existing Data | 014-b |
| 7 | Filtering and Selective Replication | 014-f |
| 8 | Monitoring and Health Checks | 014-h |
| 9 | Conflict Detection and Resolution | 014-d |
| 10 | DDL Replication | 014-e |
| 10.6 | Application Trigger Behavior | 014-e |
| 11 | Topology Management | 014-g |
| 12 | steep_repl Extension Schema | 014-a |
| 13 | steep-repl Daemon | 014-a |
| 14 | Steep TUI Integration | 014-h |
| 15 | Implementation Phases | Reference only |
| 16.1 | Coordinator Availability | 014-d |
| 16.2 | Clock Synchronization | 014-d |
| 16.3 | Large Transactions | 014-d |
| 16.4 | Schema Versioning | 014-b |
| 16.5 | Conflict Resolution Rollback | 014-d |
| 17 | References | Reference only |
| 18 | Production Readiness | 014-i |
| 19 | Networking | 014-i |
| 19.7 | Extension Upgrade Strategy | 014-i |
| 20 | Security | 014-i |
| 21 | Operations Runbook | 014-i |
| 21.4 | Slot Cleanup on Node Removal | 014-i |
| **22** | **Testing Requirements** | **All features** |

---

## Feature Breakdown

### Dependency Hierarchy

```
014-a-foundation
    │
    └── 014-b-node-init (initialize nodes via snapshot/backup)
            │
            ├── 014-c-identity-ranges
            │       │
            │       └── 014-d-conflict-resolution
            │               │
            │               └── 014-e-ddl-replication
            │                       │
            │                       └── 014-f-filtering
            │
            └── 014-g-topology
                    │
                    └── 014-h-monitoring
                            │
                            └── 014-i-production
```

### Priority Classification

- **P1 (Must-Have)**: 014-a, 014-b, 014-c, 014-d - Core replication
- **P2 (Should-Have)**: 014-e, 014-f, 014-g, 014-h - Advanced features
- **P3 (Nice-to-Have)**: 014-i - Production hardening

---

## Implementation Sequence

### Phase 1: Core Infrastructure (P1)

| Order | Feature | Branch | Sections | Scope |
|-------|---------|--------|----------|-------|
| 1 | 014-a | `014-a-foundation` | 2, 4, 13, 14 | Extension, daemon, IPC |
| 2 | 014-b | `014-b-node-init` | 6, 17.4 | Snapshots, manual init, schema sync |
| 3 | 014-c | `014-c-identity-ranges` | 5 | Range allocation, constraints |
| 4 | 014-d | `014-d-conflict-resolution` | 10, 17.1-3, 17.5 | Detection, strategies, resolution |

### Phase 2: Advanced Features (P2)

| Order | Feature | Branch | Sections | Scope |
|-------|---------|--------|----------|-------|
| 5 | 014-e | `014-e-ddl-replication` | 11 | ProcessUtility hook, queue |
| 6 | 014-f | `014-f-filtering` | 7 | Table/row/column filters |
| 7 | 014-g | `014-g-topology` | 12 | Multi-node, election |
| 8 | 014-h | `014-h-monitoring` | 8, 15 | Health, alerts, TUI |

### Phase 3: Production (P3)

| Order | Feature | Branch | Sections | Scope |
|-------|---------|--------|----------|-------|
| 9 | 014-i | `014-i-production` | 19, 20, 21, 22 | Validation, networking, security, runbook |

---

## Feature Details

---

### Feature 014-a: Foundation & Infrastructure

**Branch**: `014-a-foundation`

**Design Document Sections**:
- Section 2: Architecture Overview
- Section 4: Cross-Platform Compatibility (4.1-4.9)
- Section 13: steep_repl Extension Schema (13.1-13.2)
- Section 14: steep-repl Daemon (14.1-14.4)

**Purpose**: Establish core infrastructure including the steep_repl PostgreSQL extension and steep-repl daemon with cross-platform support (Windows first).

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want to install the steep_repl extension on PostgreSQL 17/18 |
| P1 | As a DBA, I want to install steep-repl daemon as a system service (Windows SCM, Linux systemd, macOS launchd) |
| P1 | As a DBA, I want steep-repl to connect to PostgreSQL via pgx connection pooling |
| P2 | As a DBA, I want Steep TUI to communicate with steep-repl via IPC (named pipes on Windows, Unix sockets on Linux/macOS) |
| P2 | As a DBA, I want steep-repl to expose gRPC endpoints for node-to-node communication |
| P3 | As a DBA, I want HTTP health check endpoints for load balancers |

#### Technical Scope

**steep_repl Extension (Rust/pgrx)** - Section 13:
```sql
-- Schema and core tables
CREATE SCHEMA steep_repl;

CREATE TABLE steep_repl.nodes (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    priority INTEGER NOT NULL DEFAULT 50,
    is_coordinator BOOLEAN NOT NULL DEFAULT false,
    last_seen TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE steep_repl.coordinator_state (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

-- Additional tables created but populated by later features:
-- steep_repl.identity_ranges (014-c)
-- steep_repl.conflict_log (014-d)
-- steep_repl.ddl_queue (014-e)
-- steep_repl.schema_fingerprints (014-b)
```

**Cross-Platform IPC** - Section 4.2:
```go
// internal/repl/ipc/listener.go
func NewListener(name string) (net.Listener, error) {
    if runtime.GOOS == "windows" {
        return winio.ListenPipe(`\\.\pipe\steep-repl`, nil)
    }
    return net.Listen("unix", filepath.Join(os.TempDir(), "steep-repl.sock"))
}
```

**Service Management** - Section 4.3:
```go
// kardianos/service for cross-platform
svcConfig := &service.Config{
    Name:        "steep-repl",
    DisplayName: "Steep Replication Coordinator",
    Description: "Coordinates bidirectional PostgreSQL replication",
}
```

**Directory Structure**:
```
steep/
├── cmd/
│   └── steep-repl/           # Daemon entry point
├── internal/
│   └── repl/                 # Replication package
│       ├── config/
│       ├── ipc/              # Named pipes/Unix sockets
│       ├── grpc/             # Node-to-node
│       └── ...
└── extensions/
    └── steep_repl/           # pgrx extension
        ├── Cargo.toml
        └── src/
```

**Platform Specifics** - Sections 4.4, 4.8:
| Platform | Config Path | IPC | Service |
|----------|-------------|-----|---------|
| Windows | `%APPDATA%\steep` | Named pipe `\\.\pipe\steep-repl` | SCM |
| Linux | `~/.config/steep` | Unix socket `/tmp/steep-repl.sock` | systemd |
| macOS | `~/Library/Application Support/steep` | Unix socket | launchd |

#### Libraries

- **Rust**: `pgrx` (PostgreSQL extension)
- **Go**: `kardianos/service`, `Microsoft/go-winio`, `grpc-go`, `pgx/v5`

#### Acceptance Criteria

- [ ] Extension installs on PostgreSQL 17, 18 (Windows, Linux, macOS)
- [ ] Daemon installs and runs as service on all platforms
- [ ] `steep-repl status` shows connection to PostgreSQL
- [ ] `steep-repl health` returns JSON health status
- [ ] Steep TUI connects to daemon via IPC
- [ ] gRPC server accepts connections (port 5433)
- [ ] All schema tables created with indexes
- [ ] Audit logging captures events

#### Spec-Kit Command

```bash
/speckit.specify Build foundation infrastructure for Steep bidirectional replication. Create PostgreSQL extension (steep_repl) using Rust/pgrx with schema tables for nodes, coordinator_state, and audit_log. Implement Go daemon (steep-repl) using kardianos/service for cross-platform service management. The daemon connects to PostgreSQL via pgx, exposes gRPC for node-to-node communication, and provides IPC via named pipes (Windows) or Unix sockets (Linux/macOS). Include HTTP health endpoint. Windows is primary target. Reference: BIDIRECTIONAL_REPLICATION.md sections 2, 4, 13, 14.
```

---

### Feature 014-b: Node Initialization & Snapshots

**Branch**: `014-b-node-init`

**Design Document Sections**:
- Section 6: Node Initialization and Snapshots (6.1-6.8)
- Section 17.4: Schema Versioning

**Purpose**: Initialize nodes for replication via automatic snapshots or manual backups. This is typically the first operation after installing the extension and daemon.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want to initialize a new node from an existing node using automatic snapshot (copy_data=true) |
| P1 | As a DBA, I want to initialize a node from my own pg_dump/pg_basebackup for large databases |
| P1 | As a DBA, I want to see initialization progress (% complete, rows/sec, ETA) in Steep TUI |
| P2 | As a DBA, I want to reinitialize specific tables when they diverge without full reinit |
| P2 | As a DBA, I want schema fingerprinting to detect drift before initialization |
| P2 | As a DBA, I want schema sync to validate/fix schema mismatches |
| P3 | As a DBA, I want configurable parallel workers for faster snapshot copy |

#### Technical Scope

**Initialization Methods** - Section 6.1:
| Method | Use Case | How |
|--------|----------|-----|
| Snapshot (automatic) | Small/medium DBs | `CREATE SUBSCRIPTION ... WITH (copy_data = true)` |
| Manual (backup) | Large DBs (multi-TB) | User provides pg_dump/pg_basebackup, steep-repl completes |
| Reinitialization | Recovery | Partial or full table reinit |

**Snapshot Initialization** - Section 6.2:
```yaml
replication:
  initialization:
    method: snapshot              # snapshot | manual
    parallel_workers: 4           # Parallel table copy
    snapshot_timeout: 24h
    large_table_threshold: 10GB
    large_table_method: pg_dump   # pg_dump | copy | basebackup
```

**Manual Initialization** - Section 6.3:
```bash
# Step 1: Prepare on source
steep-repl init prepare --node node_a --slot steep_init_slot

# Step 2: User performs backup/restore (their tooling)
pg_basebackup -D /backup -S steep_init_slot ...
# ... restore on target ...

# Step 3: Complete initialization
steep-repl init complete --node node_b \
    --source node_a \
    --source-lsn 0/1234ABCD
```

**Reinitialization** - Section 6.4:
```bash
# Reinitialize specific tables
steep-repl reinit --node node_b --tables orders,line_items

# Reinitialize entire schema
steep-repl reinit --node node_b --schema sales

# Full reinitialization
steep-repl reinit --node node_b --full
```

**Schema Fingerprinting** - Section 17.4:
```sql
CREATE TABLE steep_repl.schema_fingerprints (
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,      -- SHA256 of column definitions
    column_count INTEGER NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (table_schema, table_name)
);

CREATE FUNCTION steep_repl.compute_fingerprint(
    p_schema TEXT, p_table TEXT
) RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' || coalesce(column_default, '') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;

CREATE FUNCTION steep_repl.compare_schemas(
    p_source_node TEXT, p_target_node TEXT
) RETURNS TABLE (table_name TEXT, status TEXT, difference TEXT);
```

**Schema Sync Modes** - Section 6.5:
```yaml
replication:
  initialization:
    schema_sync:
      mode: strict    # strict | auto | manual
      # strict: Fail if schemas don't match
      # auto: Apply DDL to make schemas match
      # manual: Warn but allow user to fix
```

**Initialization States** - Section 6.6:
```
UNINITIALIZED → PREPARING → COPYING → CATCHING_UP → SYNCHRONIZED
      │             │           │            │
      │             │           │            ▼
      │             │           │        SYNCHRONIZED
      │             │           │            │
      ▼             ▼           ▼            ▼
   FAILED ◄──────────────────────────── DIVERGED
      │                                     │
      └────────► REINITIALIZING ◄───────────┘
```

**Progress Tracking UI** - Section 6.7:
```
┌─ Node Initialization ─────────────────────────────────────────────┐
│                                                                   │
│  Initializing node_b from node_a                                 │
│                                                                   │
│  Overall: ████████████░░░░░░░░ 62%  (14 of 23 tables)           │
│                                                                   │
│  Current: orders (1.2GB)                                         │
│           ██████████████░░░░░░ 71%  42,000 rows/sec              │
│           ETA: 3m 24s                                            │
│                                                                   │
│  Completed:                                                       │
│    ✓ customers (245MB) - 2m 14s                                  │
│    ✓ products (89MB) - 45s                                       │
│                                                                   │
│  Pending: line_items, inventory, audit_log, ...                  │
│                                                                   │
│  [C]ancel initialization                                         │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] `steep-repl init node_b --from node_a --method snapshot` works
- [ ] `steep-repl init prepare/complete` workflow for manual init
- [ ] Progress tracking with % complete, rows/sec, ETA
- [ ] `steep-repl reinit --tables` for partial reinitialization
- [ ] Schema fingerprint comparison across nodes
- [ ] Schema sync validates schemas match before data copy
- [ ] Initialization state machine tracked in steep_repl.nodes
- [ ] TUI shows node states (COPYING, CATCHING_UP, SYNCHRONIZED, etc.)
- [ ] Large table handling (>10GB) with configurable method

#### Spec-Kit Command

```bash
/speckit.specify Implement node initialization and snapshots for Steep bidirectional replication. Support automatic snapshot initialization using PostgreSQL's copy_data=true and manual initialization from user-provided pg_dump/pg_basebackup. Track initialization progress (% complete, rows/sec, ETA) in TUI. Implement reinitialization for diverged/corrupted nodes (partial by table or full). Create schema fingerprinting (SHA256 of column definitions) to detect drift before initialization. Support schema sync modes (strict/auto/manual). Track initialization states (UNINITIALIZED, PREPARING, COPYING, CATCHING_UP, SYNCHRONIZED, DIVERGED, FAILED, REINITIALIZING). Handle initial sync with existing data on both nodes (Section 6.8). Reference: BIDIRECTIONAL_REPLICATION.md sections 6, 17.4.
```

---

### Feature 014-c: Identity Range Management

**Branch**: `014-c-identity-ranges`

**Design Document Sections**:
- Section 5: Identity Range Management (5.1-5.8)

**Purpose**: Prevent primary key collisions by allocating non-overlapping ID ranges to each node, following SQL Server merge replication patterns.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want each node to have non-overlapping ID ranges per table |
| P1 | As a DBA, I want CHECK constraints to enforce ranges (fail-fast on violation) |
| P1 | As a DBA, I want automatic range expansion when utilization exceeds 80% |
| P2 | As a DBA, I want to view range status in Steep TUI (utilization %) |
| P2 | As a DBA, I want to manually reallocate/expand ranges via CLI |
| P2 | As a DBA, I want bypass mode for bulk imports (with audit logging) |
| P3 | As a DBA, I want alerts when ranges approach exhaustion (80%, 95%) |

#### Technical Scope

**Range Mechanism** - Section 5.2:
```sql
-- Node A: Allocated range 1-10000
ALTER TABLE orders ADD CONSTRAINT steep_range_orders
    CHECK (order_id >= 1 AND order_id <= 10000);
ALTER SEQUENCE orders_order_id_seq RESTART WITH 1;

-- Node B: Allocated range 10001-20000
ALTER TABLE orders ADD CONSTRAINT steep_range_orders
    CHECK (order_id >= 10001 AND order_id <= 20000);
ALTER SEQUENCE orders_order_id_seq RESTART WITH 10001;
```

**Range Tracking** - Section 13.1:
```sql
CREATE TABLE steep_repl.identity_ranges (
    id BIGSERIAL PRIMARY KEY,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    range_start BIGINT NOT NULL,
    range_end BIGINT NOT NULL,
    allocated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    status TEXT NOT NULL DEFAULT 'active',  -- active, exhausted, released
    UNIQUE (table_schema, table_name, node_id, range_start)
);
```

**Range Functions** - Section 13.2:
```sql
CREATE FUNCTION steep_repl.allocate_range(
    p_table_schema TEXT, p_table_name TEXT, p_range_size BIGINT DEFAULT 10000
) RETURNS TABLE (range_start BIGINT, range_end BIGINT);

CREATE FUNCTION steep_repl.check_range_threshold(
    p_table_schema TEXT, p_table_name TEXT, p_threshold_percent INTEGER DEFAULT 80
) RETURNS TABLE (current_value BIGINT, range_end BIGINT, percent_used NUMERIC, needs_expansion BOOLEAN);

CREATE FUNCTION steep_repl.apply_range_constraint(
    p_table_schema TEXT, p_table_name TEXT, p_range_start BIGINT, p_range_end BIGINT
) RETURNS BOOLEAN;
```

**Bypass Mode** - Section 5.6:
```sql
-- Custom GUC for session bypass
SELECT pg_catalog.set_config('steep_repl.bypass_range_check', 'off', false);

-- Range check function (supports bypass)
CREATE FUNCTION steep_repl.check_id_range(
    p_table_schema TEXT, p_table_name TEXT, p_id BIGINT
) RETURNS BOOLEAN AS $$
DECLARE
    v_bypass TEXT;
BEGIN
    v_bypass := current_setting('steep_repl.bypass_range_check', true);
    IF v_bypass = 'on' THEN
        RETURN true;
    END IF;
    -- Normal range check
    ...
END;
$$ LANGUAGE plpgsql STABLE;

-- Usage
SET steep_repl.bypass_range_check = 'on';
COPY orders FROM '/path/to/data.csv';
SET steep_repl.bypass_range_check = 'off';
```

**Configuration** - Section 5.4:
```yaml
replication:
  identity_ranges:
    enabled: true
    default_range_size: 10000
    threshold_percent: 80
    tables:
      orders:
        range_size: 100000    # High-volume
      audit_log:
        enabled: false        # Uses UUIDs
```

**Range Monitoring UI** - Section 5.7:
```
┌─ Identity Ranges ─────────────────────────────────────────────────┐
│                                                                   │
│  Table           This Node           Peer Nodes       Next Avail  │
│  ──────────────────────────────────────────────────────────────── │
│  orders          1-10000 (87%)       B: 10001-20000   20001       │
│  customers       5001-10000 (34%)    B: 1-5000        10001       │
│  line_items      1-50000 (92%) ⚠     B: 50001-100000  100001      │
│                                                                   │
│  ⚠ line_items approaching threshold - next range pre-allocated   │
│                                                                   │
│  [R]eallocate  [V]iew constraints  [B]ypass mode  [H]istory      │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] Range allocation creates non-overlapping ranges
- [ ] CHECK constraints enforce ranges
- [ ] Sequence reseeding to range start
- [ ] Automatic expansion at 80% threshold
- [ ] TUI displays utilization with color coding
- [ ] CLI: `steep-repl range status`, `steep-repl range allocate`
- [ ] Bypass mode with audit logging
- [ ] Clear error messages on constraint violations

#### Spec-Kit Command

```bash
/speckit.specify Implement identity range management for Steep bidirectional replication to prevent primary key collisions. Allocate non-overlapping ID ranges per table per node using CHECK constraints. Implement range allocation functions, threshold monitoring (80%), and automatic expansion. Support bypass mode for bulk imports with session GUC (steep_repl.bypass_range_check) and audit logging. Create TUI view showing range utilization (%) with color coding. Handle composite primary keys (Section 5.8) where child tables with FK to parent tables inherit range partitioning. Follows SQL Server merge replication patterns. Reference: BIDIRECTIONAL_REPLICATION.md section 5.
```

---

### Feature 014-d: Conflict Detection & Resolution

**Branch**: `014-d-conflict-resolution`

**Design Document Sections**:
- Section 10: Conflict Detection and Resolution (10.1-10.5)
- Section 17.1: Coordinator Availability
- Section 17.2: Clock Synchronization
- Section 17.3: Large Transactions
- Section 17.5: Conflict Resolution Rollback

**Purpose**: Detect and resolve conflicts using PostgreSQL 18's native conflict logging with configurable resolution strategies.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want INSERT-INSERT, UPDATE-UPDATE, UPDATE-DELETE conflicts detected automatically |
| P1 | As a DBA, I want default resolution strategy (last-write-wins) applied automatically |
| P1 | As a DBA, I want to view pending manual conflicts in Steep TUI |
| P2 | As a DBA, I want to resolve conflicts manually (local/remote/merge) |
| P2 | As a DBA, I want per-table resolution strategy configuration |
| P2 | As a DBA, I want bulk conflict resolution (by transaction ID) |
| P3 | As a DBA, I want to revert a conflict resolution if it was wrong |
| P3 | As a DBA, I want NTP clock sync validation on daemon start |

#### Technical Scope

**Conflict Types** - Section 10.1:
| Type | Description |
|------|-------------|
| INSERT-INSERT | Same PK inserted on both nodes |
| UPDATE-UPDATE | Same row updated on both nodes |
| UPDATE-DELETE | Row updated on one, deleted on other |

**Resolution Strategies** - Section 10.3:
| Strategy | Description |
|----------|-------------|
| `last_write_wins` | Higher timestamp wins (default) |
| `first_write_wins` | Lower timestamp wins |
| `node_priority` | Designated node always wins |
| `keep_local` | Local always wins |
| `apply_remote` | Remote always wins |
| `manual` | Queue for human resolution |

**Conflict Log** - Section 10.2:
```sql
CREATE TABLE steep_repl.conflict_log (
    id BIGSERIAL PRIMARY KEY,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    subscription TEXT NOT NULL,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    conflict_type TEXT NOT NULL,
    local_tuple JSONB,
    remote_tuple JSONB,
    local_origin TEXT,
    remote_origin TEXT,
    local_xact_ts TIMESTAMPTZ,
    remote_xact_ts TIMESTAMPTZ,
    origin_xid BIGINT,              -- For bulk resolution (Section 15.3)
    resolution TEXT,                 -- PENDING, APPLIED_REMOTE, KEPT_LOCAL, MERGED, REVERTED
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT
);
```

**Clock Synchronization** - Section 17.2:
```yaml
replication:
  clock_sync:
    require_ntp: true
    max_drift_ms: 1000
    use_commit_timestamp: true    # Fallback: track_commit_timestamp
    tie_breaker: node_priority    # When timestamps equal
```

**Bulk Resolution** - Section 17.3:
```sql
CREATE FUNCTION steep_repl.resolve_conflicts_bulk(
    p_resolution TEXT,
    p_filter_xid BIGINT DEFAULT NULL,
    p_filter_table TEXT DEFAULT NULL,
    p_filter_time_start TIMESTAMPTZ DEFAULT NULL,
    p_filter_time_end TIMESTAMPTZ DEFAULT NULL
) RETURNS INTEGER;
```

**Revert Resolution** - Section 17.5:
```sql
CREATE FUNCTION steep_repl.revert_resolution(
    p_conflict_id BIGINT,
    p_reason TEXT DEFAULT 'Manual revert'
) RETURNS BIGINT;  -- Returns new conflict_id for the revert
```

**Coordinator Failover** - Section 17.1:
```
2-Node: Simple failover (no Raft)
- One node is coordinator (highest priority)
- If unreachable, other node self-promotes
- State in steep_repl.coordinator_state (PostgreSQL)
```

**Manual Resolution UI** - Section 10.5:
```
┌─ Pending Conflicts ───────────────────────────────────────────────┐
│                                                                   │
│  #1 UPDATE-UPDATE on orders.order_id = 50432                     │
│  ────────────────────────────────────────────────────────────────│
│  Detected: 2025-12-03 14:32:15                                   │
│                                                                   │
│  LOCAL (node_a, 14:32:10)        REMOTE (node_b, 14:32:12)      │
│  ┌─────────────────────────┐    ┌─────────────────────────┐     │
│  │ status: processing      │    │ status: shipped         │     │
│  │ updated_by: alice       │    │ updated_by: bob         │     │
│  └─────────────────────────┘    └─────────────────────────┘     │
│                                                                   │
│  [L]ocal wins  [R]emote wins  [M]erge  [S]kip                   │
└───────────────────────────────────────────────────────────────────┘
```

**Bulk Resolution UI** - Section 17.3:
```
┌─ Pending Conflicts (grouped by transaction) ──────────────────────┐
│                                                                   │
│  ▶ Transaction 1234567 (2025-12-03 14:32:00) - 23 conflicts      │
│    └─ orders: 15 UPDATE-UPDATE                                   │
│    └─ line_items: 8 UPDATE-UPDATE                                │
│                                                                   │
│  [A]ll local  [Z]All remote  [E]xpand  [C]ollapse               │
└───────────────────────────────────────────────────────────────────┘
```

**Configuration**:
```yaml
replication:
  conflicts:
    default_strategy: last_write_wins
    tables:
      orders:
        strategy: manual
      inventory:
        strategy: last_write_wins
      customer_preferences:
        strategy: node_priority
        priority: [node_a, node_b]
```

#### Acceptance Criteria

- [ ] Conflicts logged to steep_repl.conflict_log
- [ ] Automatic resolution for configured strategies
- [ ] Manual resolution UI with side-by-side comparison
- [ ] Bulk resolution by transaction ID
- [ ] Revert capability with audit trail
- [ ] Clock sync validation on daemon start
- [ ] Per-table strategy configuration
- [ ] Coordinator failover works (2-node)

#### Spec-Kit Command

```bash
/speckit.specify Implement conflict detection and resolution for Steep bidirectional replication. Integrate with PostgreSQL 17/18 conflict logging (native in 18). Create conflict_log table storing local/remote tuples and timestamps. Implement resolution strategies: last_write_wins (default), first_write_wins, node_priority, keep_local, apply_remote, manual. Build TUI for manual resolution with side-by-side comparison. Support bulk resolution by transaction ID for large transactions. Include revert capability. Require NTP clock sync with startup validation. Implement simple coordinator failover (no Raft) with state in PostgreSQL. Reference: BIDIRECTIONAL_REPLICATION.md sections 10, 17.1, 17.2, 17.3, 17.5.
```

---

### Feature 014-e: DDL Replication

**Branch**: `014-e-ddl-replication`

**Design Document Sections**:
- Section 11: DDL Replication (11.1-11.6)

**Purpose**: Capture and replicate DDL changes via PostgreSQL ProcessUtility hook to prevent schema drift.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want CREATE TABLE, ALTER TABLE ADD COLUMN, CREATE INDEX automatically replicated |
| P1 | As a DBA, I want a DDL queue showing pending/applied/rejected operations |
| P2 | As a DBA, I want DROP TABLE, ALTER TABLE DROP COLUMN to require approval |
| P2 | As a DBA, I want to approve/reject queued DDL in Steep TUI |
| P2 | As a DBA, I want schema fingerprints validated before DDL apply |
| P3 | As a DBA, I want CREATE FUNCTION, CREATE TRIGGER excluded by default |

#### Technical Scope

**Captured DDL** - Section 11.2:
| DDL Type | Captured | Notes |
|----------|----------|-------|
| CREATE TABLE | Yes | Including constraints |
| DROP TABLE | Yes | Requires approval |
| ALTER TABLE ADD COLUMN | Yes | Auto-apply |
| ALTER TABLE DROP COLUMN | Yes | Requires approval |
| CREATE INDEX | Yes | CONCURRENTLY supported |
| DROP INDEX | Yes | Auto-apply |
| CREATE/DROP FUNCTION | Configurable | Excluded by default |
| CREATE/DROP TRIGGER | Configurable | Excluded by default |
| TRUNCATE | Yes | Requires approval |

**DDL Queue** - Section 11.3:
```sql
CREATE TABLE steep_repl.ddl_queue (
    id BIGSERIAL PRIMARY KEY,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    origin_node TEXT NOT NULL,
    ddl_command TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_schema TEXT,
    object_name TEXT,
    status TEXT NOT NULL,        -- PENDING, APPROVED, APPLIED, REJECTED, FAILED
    pre_fingerprint TEXT,        -- Schema before DDL
    post_fingerprint TEXT,       -- Expected schema after
    applied_at TIMESTAMPTZ,
    applied_by TEXT,
    error_message TEXT
);
```

**ProcessUtility Hook** - Section 11.4:
```rust
// extensions/steep_repl/src/hooks.rs
#[pg_guard]
pub unsafe extern "C" fn steep_process_utility_hook(...) {
    // Skip if replicated DDL (prevent loops)
    if is_replicated_ddl_context() {
        call_prev_hook(...);
        return;
    }

    // Capture DDL before execution
    let ddl_info = capture_ddl_info(pstmt, query_string);

    // Execute original DDL
    call_prev_hook(...);

    // Queue for replication
    if let Some(info) = ddl_info {
        queue_ddl_for_replication(info);
        notify_daemon();
    }
}
```

**Configuration** - Section 11.5:
```yaml
replication:
  ddl:
    auto_apply:
      - CREATE TABLE
      - ALTER TABLE ADD COLUMN
      - CREATE INDEX
      - DROP INDEX
    require_approval:
      - DROP TABLE
      - ALTER TABLE DROP COLUMN
      - TRUNCATE
    exclude:
      - CREATE FUNCTION
      - CREATE TRIGGER
```

**DDL Queue UI** - Section 11.6:
```
┌─ DDL Queue ───────────────────────────────────────────────────────┐
│                                                                   │
│  Status: 2 pending, 15 applied today, 0 rejected                 │
│                                                                   │
│  PENDING                                                          │
│  ──────────────────────────────────────────────────────────────── │
│  #42 DROP TABLE legacy_audit (from node_b, 14:20:05)             │
│      ⚠ Destructive operation - requires approval                 │
│                                                                   │
│  RECENTLY APPLIED                                                 │
│  ──────────────────────────────────────────────────────────────── │
│  #40 CREATE INDEX idx_orders_date ON orders(created_at) ✓        │
│                                                                   │
│  [A]pprove  [R]eject  [V]iew full DDL  [D]iff schemas            │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] ProcessUtility hook captures DDL
- [ ] DDL queued with fingerprints
- [ ] Auto-apply for non-destructive DDL
- [ ] Approval workflow for destructive DDL
- [ ] Schema fingerprint validation before apply
- [ ] Loop prevention (don't re-capture replicated DDL)
- [ ] CREATE INDEX CONCURRENTLY supported
- [ ] TUI shows DDL queue with approve/reject

#### Spec-Kit Command

```bash
/speckit.specify Implement DDL replication for Steep using PostgreSQL ProcessUtility hook in steep_repl extension (Rust/pgrx). Capture CREATE TABLE, ALTER TABLE, CREATE INDEX, DROP operations. Store DDL in ddl_queue table with schema fingerprints. Auto-apply non-destructive DDL, require approval for destructive (DROP TABLE, DROP COLUMN, TRUNCATE). Include loop prevention to avoid re-capturing replicated DDL. Handle application trigger behavior (Section 11.6 - ENABLE ALWAYS TRIGGER). Build TUI view showing DDL queue with approve/reject actions. Reference: BIDIRECTIONAL_REPLICATION.md section 11.
```

---

### Feature 014-f: Filtering & Selective Replication

**Branch**: `014-f-filtering`

**Design Document Sections**:
- Section 7: Filtering and Selective Replication (7.1-7.4)

**Purpose**: Configure what data replicates using PostgreSQL's native publication filtering (tables, rows, columns).

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want to include/exclude specific tables from replication |
| P2 | As a DBA, I want row-level filters (e.g., `WHERE region IN ('US', 'EU')`) |
| P2 | As a DBA, I want column-level filters to exclude sensitive columns |
| P2 | As a DBA, I want to view active filters in Steep TUI |
| P3 | As a DBA, I want schema-level exclusions |

**Note**: Row and column filtering require PostgreSQL 17+.

#### Technical Scope

**Table-Level Filtering** - Section 7.1:
```sql
-- Include specific tables
CREATE PUBLICATION steep_pub FOR TABLE orders, customers, products;

-- Or exclude via schema
CREATE PUBLICATION steep_pub FOR ALL TABLES
    WHERE (schemaname NOT IN ('audit', 'temp', 'staging'));
```

**Row-Level Filtering** - Section 7.2 (PG17+):
```sql
CREATE PUBLICATION steep_pub FOR TABLE orders
    WHERE (region IN ('US', 'EU'));

CREATE PUBLICATION steep_pub FOR TABLE customers
    WHERE (status = 'active');
```

**Row Filter Limitations**:
| Limitation | Details |
|------------|---------|
| Filter on replica key | Row may "disappear" from subscriber |
| No partitioned parent | Apply to individual partitions |
| UPDATE changes match | Row effectively deleted if no longer matches |
| Bidirectional complexity | Can cause INSERT-INSERT conflicts |

**Column Filtering** - Section 7.3 (PG17+):
```sql
CREATE PUBLICATION steep_pub FOR TABLE customers (id, name, email, created_at);
-- Excludes: ssn, credit_card, password_hash
```

**Column Filter Limitations**:
| Limitation | Details |
|------------|---------|
| Must include replica identity | PK columns cannot be excluded |
| TOAST columns | May behave unexpectedly |
| Schema changes | Adding columns requires publication update |
| Generated columns | Cannot be included |

**Configuration**:
```yaml
replication:
  filtering:
    include_tables:
      - public.orders
      - public.customers
      - sales.*
    exclude_tables:
      - public.audit_log
      - staging.*
    exclude_schemas:
      - pg_temp
    row_filters:
      orders:
        where: "region IN ('US', 'EU')"
      customers:
        where: "status = 'active'"
    column_filters:
      customers:
        include: [id, name, email, created_at]
        # OR exclude: [ssn, credit_card, password_hash]
```

**Filtering UI** - Section 7.4:
```
┌─ Replication Filters ─────────────────────────────────────────────┐
│                                                                   │
│  Tables: 23 included, 5 excluded                                 │
│                                                                   │
│  Table              Filter              Columns                   │
│  ──────────────────────────────────────────────────────────────── │
│  orders             region IN (US,EU)   all                      │
│  customers          status = 'active'   4 of 12 (excl: ssn,...)  │
│  products           -                   all                      │
│  audit_log          EXCLUDED            -                        │
│                                                                   │
│  [E]dit filters  [V]alidate  [A]pply changes                    │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] Table include/exclude via publication
- [ ] Row filters applied (PG17+)
- [ ] Column filters applied (PG17+)
- [ ] Schema-level exclusions
- [ ] TUI displays active filters
- [ ] Warnings for filter limitations
- [ ] Filter changes propagate to publications

#### Spec-Kit Command

```bash
/speckit.specify Implement filtering for Steep bidirectional replication using PostgreSQL native publication features. Support table-level filtering (include/exclude), row-level filtering with WHERE clauses (PG17+), and column-level filtering (PG17+). Document limitations (replica identity columns required, UPDATE changing filter match, bidirectional conflicts). Create TUI view showing active filters per table. Reference: BIDIRECTIONAL_REPLICATION.md section 7.
```

---

### Feature 014-g: Topology & Coordination

**Branch**: `014-g-topology`

**Design Document Sections**:
- Section 12: Topology Management (12.1-12.3)

**Purpose**: Manage multi-node topologies (star or mesh) with coordinator election and health monitoring.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want to register nodes in a replication topology |
| P1 | As a DBA, I want health checks between nodes |
| P1 | As a DBA, I want coordinator election based on priority |
| P2 | As a DBA, I want to view topology status in Steep TUI |
| P2 | As a DBA, I want automatic coordinator failover |
| P2 | As a DBA, I want to add/remove nodes via CLI |
| P3 | As a DBA, I want mesh topology (any node can coordinate) |

#### Technical Scope

**Topologies** - Section 12.1:
```
STAR (Hub-Spoke)              MESH (Peer-to-Peer)

      ┌───┐                   ┌───┐───────┌───┐
      │ A │ (Hub)             │ A │       │ B │
      └─┬─┘                   └─┬─┘───────└─┬─┘
   ┌────┼────┐                  │     ╳     │
 ┌─┴─┐┌─┴─┐┌─┴─┐              ┌─┴─┐       ┌─┴─┐
 │ B ││ C ││ D │              │ C │───────│ D │
 └───┘└───┘└───┘              └───┘       └───┘
```

**Configuration** - Section 12.2:
```yaml
replication:
  topology:
    mode: mesh                 # star | mesh
    this_node:
      name: node_a
      priority: 100
    nodes:
      - name: node_b
        host: node-b.example.com
        port: 5432
        priority: 90
    health_check:
      interval: 5s
      timeout: 10s
      unhealthy_threshold: 3
```

**Coordinator Election** - Section 12.3:
```
1. All nodes start as followers
2. Highest priority node with quorum becomes coordinator
3. If coordinator fails, next highest priority takes over
4. Coordinator state stored in steep_repl.coordinator_state
```

**Topology UI**:
```
┌─ Replication Topology ────────────────────────────────────────────┐
│                                                                   │
│  Mode: MESH (2 nodes)                                            │
│  Coordinator: hq (this node)                                     │
│                                                                   │
│        ┌──────────┐                   ┌──────────┐               │
│        │    HQ    │◄─────────────────►│  CLOUD   │               │
│        │ ● coord  │                   │ ● online │               │
│        └──────────┘                   └──────────┘               │
│                                                                   │
│  Node        Priority  Status      Lag         Last Seen         │
│  ──────────────────────────────────────────────────────────────── │
│  hq          100       COORD       -           -                  │
│  cloud       90        ONLINE      1.2s        2s ago             │
│                                                                   │
│  [A]dd node  [R]emove node  [P]romote  [D]emote                 │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] Node registration on daemon start
- [ ] Health checks at configurable interval
- [ ] Priority-based coordinator election
- [ ] Automatic failover on coordinator loss
- [ ] TUI displays topology with status
- [ ] CLI: `steep-repl node add/remove/status`
- [ ] Star and mesh topologies supported
- [ ] Quorum for N>2 nodes

#### Spec-Kit Command

```bash
/speckit.specify Implement topology management for Steep multi-node bidirectional replication. Support star (hub-spoke) and mesh topologies. Create topology manager for node registration, discovery, and health monitoring. Implement priority-based coordinator election with automatic failover. Store state in PostgreSQL. Build TUI topology view showing node status and coordinator role. Provide CLI for adding/removing nodes. Implement quorum for N>2 nodes. Reference: BIDIRECTIONAL_REPLICATION.md section 12.
```

---

### Feature 014-h: Monitoring & TUI Integration

**Branch**: `014-h-monitoring`

**Design Document Sections**:
- Section 8: Monitoring and Health Checks (8.1-8.6)
- Section 15: Steep TUI Integration (15.1-15.2)

**Purpose**: Integrate bidirectional replication monitoring into Steep's dashboard and provide health endpoints.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want replication health metrics in Steep dashboard |
| P1 | As a DBA, I want `steep-repl health` CLI and HTTP endpoint |
| P2 | As a DBA, I want alerts for lag, conflicts, range exhaustion |
| P2 | As a DBA, I want a "Bidirectional" tab in Replication view |
| P2 | As a DBA, I want conflict count in status bar |
| P3 | As a DBA, I want structured JSON logging for troubleshooting |

#### Technical Scope

**Health Metrics** - Section 8.1:
| Metric | Source | Alert Threshold |
|--------|--------|-----------------|
| Replication lag (bytes) | pg_stat_replication | 100MB |
| Replication lag (time) | pg_stat_replication | 60s |
| Conflict rate | conflict_log | > 10/min |
| Pending conflicts | conflict_log | > 0 (manual) |
| DDL queue depth | ddl_queue | > 5 |
| Range utilization | identity_ranges | > 80% |
| Node health | steep-repl | Any not SYNCHRONIZED |

**Health Endpoints** - Section 8.2:
```bash
steep-repl health --json
```
```json
{
  "status": "healthy",
  "node": "node_a",
  "checks": {
    "postgresql": "ok",
    "extension": "ok",
    "peer_connectivity": "ok",
    "replication_lag": {"status": "ok", "lag_bytes": 1024},
    "conflict_rate": {"status": "ok", "per_minute": 0.5},
    "range_utilization": {"status": "warning", "tables_above_80pct": ["orders"]}
  }
}
```

**Alerts** - Section 8.3:
```yaml
alerts:
  rules:
    - name: replication_lag_critical
      metric: replication_lag_bytes
      warning: 52428800     # 50MB
      critical: 104857600   # 100MB
    - name: high_conflict_rate
      metric: steep_repl_conflicts_per_minute
      warning: 5
      critical: 20
    - name: range_exhaustion_warning
      metric: steep_repl_range_utilization_pct
      warning: 80
      critical: 95
```

**Dashboard Panel** - Section 8.4:
```
┌─ Bidirectional Replication ───────────────────────────────────────┐
│                                                                   │
│  Topology: node_a ◄──────► node_b                                │
│  Status: SYNCHRONIZED                                            │
│                                                                   │
│  Lag (A→B): 1.2s  ████████░░ 1.2MB                              │
│  Lag (B→A): 0.8s  ██████░░░░ 0.8MB                              │
│                                                                   │
│  Conflicts (24h):  3 resolved, 0 pending                        │
│  DDL Queue:        0 pending                                     │
│  Ranges:           2 tables > 80%  ⚠                            │
└───────────────────────────────────────────────────────────────────┘
```

**TUI Views** - Section 15.1:
| Location | Addition |
|----------|----------|
| Replication View | New "Bidirectional" tab |
| Replication View | "Conflicts" subtab |
| Replication View | "DDL Queue" subtab |
| Replication View | "Ranges" subtab |
| Dashboard | Conflict count in alert panel |
| Status Bar | Replication health indicator |

**Key Bindings** - Section 15.2:
| Key | Action |
|-----|--------|
| Tab | Cycle subtabs |
| c | View conflicts |
| d | View DDL queue |
| r | View identity ranges |
| L/R | Local/Remote wins (conflicts) |
| A/X | Approve/Reject (DDL) |

**Structured Logging** - Section 8.6:
```json
{"level":"info","ts":"2025-12-03T14:32:00Z","msg":"conflict detected","table":"orders","pk":"50432","type":"UPDATE_UPDATE","resolution":"last_write_wins"}
{"level":"warn","ts":"2025-12-03T14:32:01Z","msg":"range threshold exceeded","table":"line_items","utilization":87.5}
```

#### Acceptance Criteria

- [ ] Health endpoint returns JSON status
- [ ] Dashboard shows bidirectional replication panel
- [ ] Alerts for lag, conflicts, ranges
- [ ] Bidirectional tab with subtabs in TUI
- [ ] Conflict count in status bar
- [ ] Key bindings work per spec
- [ ] Structured logging enabled

#### Spec-Kit Command

```bash
/speckit.specify Implement monitoring and TUI integration for Steep bidirectional replication. Add health endpoint (CLI and HTTP) returning JSON status for PostgreSQL, extension, peers, lag, conflicts, and ranges. Integrate alerts for replication_lag, conflict_rate, range_utilization using existing alert system. Add "Bidirectional" tab to Replication view with subtabs for Overview, Conflicts, DDL Queue, Ranges. Show conflict count in status bar. Add key bindings per spec. Enable structured JSON logging. Reference: BIDIRECTIONAL_REPLICATION.md sections 8, 15.
```

---

### Feature 014-i: Production Hardening

**Branch**: `014-i-production`

**Design Document Sections**:
- Section 19: Production Readiness (19.1-19.6)
- Section 20: Networking (20.1-20.7)
- Section 21: Security (21.1-21.6)
- Section 22: Operations Runbook (22.1-22.4)

**Purpose**: Production-ready features including data validation, Tailscale networking, security hardening, and operational procedures.

#### User Stories

| Priority | Story |
|----------|-------|
| P1 | As a DBA, I want data validation (row counts, checksums) to detect divergence |
| P1 | As a DBA, I want Tailscale integration for secure cross-site networking |
| P1 | As a DBA, I want failover to surviving node when one becomes unreachable |
| P2 | As a DBA, I want failback procedure when failed node returns |
| P2 | As a DBA, I want RBAC for conflict resolution and DDL approval |
| P2 | As a DBA, I want credentials via env vars or password commands (no plaintext) |
| P3 | As a DBA, I want business notifications (Slack, email, PagerDuty) |
| P3 | As a DBA, I want coordinated backup support |
| P3 | As a DBA, I want operations runbook automation |

#### Technical Scope

**Data Validation** - Section 19.1:
```sql
-- Level 1: Row count (fast, frequent)
CREATE FUNCTION steep_repl.validate_row_counts(p_peer_node TEXT)
RETURNS TABLE (table_name TEXT, local_count BIGINT, remote_count BIGINT, status TEXT);

-- Level 2: Checksum (slower, periodic)
CREATE FUNCTION steep_repl.validate_checksums(
    p_table TEXT, p_peer_node TEXT, p_sample_pct NUMERIC DEFAULT 100
) RETURNS TABLE (pk_value TEXT, local_hash TEXT, remote_hash TEXT, divergence_type TEXT);

-- Level 3: Full compare with repair
CREATE FUNCTION steep_repl.compare_and_repair(
    p_table TEXT, p_peer_node TEXT, p_dry_run BOOLEAN DEFAULT true
) RETURNS TABLE (pk_value TEXT, divergence_type TEXT, repair_sql TEXT);
```

**Validation Configuration**:
```yaml
replication:
  validation:
    enabled: true
    row_count:
      interval: 5m
      alert_threshold_percent: 0.1
    checksum:
      schedule: "0 3 * * *"
      sample_percent: 10
    on_divergence:
      action: alert           # alert | auto_repair | pause
```

**Clock Synchronization** - Section 17.2:
| Platform | NTP | Check Command |
|----------|-----|---------------|
| Windows | w32time | `w32tm /query /status` |
| Linux | chrony | `chronyc tracking` |

**Clock Synchronization (Production)** - Section 19.2:
See Section 17.2 for clock sync requirements. Additional production considerations:
- Recommended NTP sync interval: 60 seconds
- Maximum allowed drift before warning: 100ms
- Maximum allowed drift before error: 1000ms

**Failover/Failback** - Section 19.3:
```bash
# Failover (HQ down, promote Cloud)
steep-repl failover --promote cloud

# Failback (HQ returns)
steep-repl failback --prepare
steep-repl failback --sync
steep-repl failback --validate
steep-repl failback --complete
```

**Backup Coordination** - Section 19.4:
```bash
steep-repl backup prepare --all-nodes
steep-repl backup snapshot    # Returns LSN
# ... pg_basebackup on each node ...
steep-repl backup complete --backup-id bk_...
```

**Notifications** - Section 19.5:
```yaml
replication:
  notifications:
    channels:
      slack:
        webhook_url: "https://hooks.slack.com/..."
      email:
        to: ["dba@company.com"]
      pagerduty:
        routing_key: "..."
    routing:
      failover_started: [slack, email, pagerduty]
      conflict_pending_manual: [slack, email]
```

**WAL Retention** - Section 19.6:
```yaml
replication:
  wal_retention:
    expected_max_outage: 48h
    alert_threshold_percent: 50
    recommended_settings:
      wal_keep_size: "10GB"
      max_slot_wal_keep_size: "20GB"
```

**Tailscale Integration** - Section 20.3:
```yaml
replication:
  networking:
    provider: tailscale
    tailscale:
      expect_connected: true
    nodes:
      - name: hq
        host: hq.mynet.ts.net      # MagicDNS
        port: 5432
```

**Tailscale ACLs** - Section 20.3:
```json
{
  "acls": [{
    "action": "accept",
    "src": ["tag:steep-repl"],
    "dst": ["tag:steep-repl:5432,5433,5434"]
  }]
}
```

**Manual Networking** - Section 20.4:
```yaml
replication:
  networking:
    provider: manual
    tls:
      enabled: true
      cert_file: /etc/steep/certs/server.crt
      key_file: /etc/steep/certs/server.key
```

**Security Model** - Section 21.1:
```
Layer 1: NETWORK - Tailscale/WireGuard encryption
Layer 2: TRANSPORT - TLS 1.3 for gRPC
Layer 3: AUTHENTICATION - PostgreSQL roles, no password storage
Layer 4: AUTHORIZATION - RBAC for operations
Layer 5: AUDIT - All actions logged
```

**Credential Management** - Section 21.2:
```yaml
replication:
  credentials:
    hq:
      user_env: STEEP_HQ_USER
      password_env: STEEP_HQ_PASSWORD
      # OR
      password_command: "pass show postgres/hq"
```

**RBAC** - Section 21.4:
```yaml
replication:
  rbac:
    roles:
      viewer: [view_status, view_conflicts]
      operator: [viewer, resolve_conflicts, approve_ddl]
      admin: [operator, enable_bypass, failover]
    role_mapping:
      steep_admin: admin
      steep_ops: operator
```

**Audit Logging** - Section 21.5:
| Action | Logged |
|--------|--------|
| conflict_resolved | Old/new resolution, who |
| ddl_approved | DDL command, approver |
| bypass_enabled | Duration, reason, who |
| failover_initiated | Nodes, trigger |

**Operations Runbook** - Section 22:
| Scenario | Section |
|----------|---------|
| High conflict rate | 22.1 |
| Node unreachable | 22.1 |
| Range exhaustion | 22.1 |
| Replication lag growing | 22.1 |
| DDL stuck in queue | 22.1 |
| Data validation failed | 22.1 |
| Weekly maintenance | 22.2 |
| Adding/removing nodes | 22.2 |
| Upgrading steep-repl | 22.2 |
| Slot cleanup on node removal | 22.4 |
| Emergency stop | 22.3 |
| Force failover | 22.3 |
| Bypass range constraints | 22.3 |

**Validation UI**:
```
┌─ Data Validation ─────────────────────────────────────────────────┐
│                                                                   │
│  Last Validation: 2025-12-03 03:15:00                            │
│  Status: ● OK                                                    │
│                                                                   │
│  Row Counts:                                                      │
│  Table           Local       Remote      Status                   │
│  orders          1,245,892   1,245,892   ● OK                    │
│  line_items      4,892,103   4,892,101   ⚠ WARN (-2)             │
│                                                                   │
│  [V]alidate now  [R]epair  [H]istory                            │
└───────────────────────────────────────────────────────────────────┘
```

**Failover UI**:
```
┌─ Failover Status ─────────────────────────────────────────────────┐
│                                                                   │
│  ⚠ FAILOVER ACTIVE                                               │
│                                                                   │
│  HQ: UNREACHABLE since 2025-12-03 14:30:00                       │
│  Cloud: PROMOTED (coordinator)                                    │
│                                                                   │
│  [P]repare failback  [V]alidate  [C]omplete                     │
└───────────────────────────────────────────────────────────────────┘
```

#### Acceptance Criteria

- [ ] Row count validation on schedule
- [ ] Checksum validation with sampling
- [ ] Repair function generates SQL
- [ ] Tailscale status integration
- [ ] Manual failover works
- [ ] Automatic failover (configurable)
- [ ] Failback procedure complete
- [ ] RBAC enforced
- [ ] Credentials from env/command
- [ ] Notifications sent
- [ ] Audit logging active
- [ ] Clock sync validated
- [ ] Backup coordination works
- [ ] Runbook scenarios documented

#### Spec-Kit Command

```bash
/speckit.specify Implement production hardening for Steep bidirectional replication. Create data validation: row counts (frequent), checksums (periodic with sampling), full compare with repair SQL. Integrate Tailscale for zero-config networking (Windows + Linux). Implement failover (manual/automatic) with identity range expansion and failback procedure. Add RBAC for operations. Support credentials via env vars and password commands. Integrate notifications (Slack, email, PagerDuty). Add backup coordination. Validate clock sync on startup. Include extension upgrade strategy (Section 20.7) and slot cleanup on node removal (Section 22.4). Create operational runbook procedures. Reference: BIDIRECTIONAL_REPLICATION.md sections 19, 20, 21, 22.
```

---

## Verification Checklist

After implementation, verify ALL design document sections are covered:

| Section | Title | Feature | Verified |
|---------|-------|---------|----------|
| 1 | Executive Summary | Context | [ ] |
| 2 | Architecture Overview | 014-a | [ ] |
| 3 | PostgreSQL Version Requirements | Reference | [ ] |
| 4 | Cross-Platform Compatibility | 014-a | [ ] |
| 5 | Identity Range Management | 014-c | [ ] |
| 5.8 | Composite Primary Keys | 014-c | [ ] |
| 6 | Node Initialization and Snapshots | 014-b | [ ] |
| 6.8 | Initial Sync with Existing Data | 014-b | [ ] |
| 7 | Filtering and Selective Replication | 014-f | [ ] |
| 8 | Monitoring and Health Checks | 014-h | [ ] |
| 9 | Conflict Detection and Resolution | 014-d | [ ] |
| 10 | DDL Replication | 014-e | [ ] |
| 10.6 | Application Trigger Behavior | 014-e | [ ] |
| 11 | Topology Management | 014-g | [ ] |
| 12 | steep_repl Extension Schema | 014-a | [ ] |
| 13 | steep-repl Daemon | 014-a | [ ] |
| 14 | Steep TUI Integration | 014-h | [ ] |
| 15 | Implementation Phases | Reference | [ ] |
| 16.1 | Coordinator Availability | 014-d | [ ] |
| 16.2 | Clock Synchronization | 014-d | [ ] |
| 16.3 | Large Transactions | 014-d | [ ] |
| 16.4 | Schema Versioning | 014-b | [ ] |
| 16.5 | Conflict Resolution Rollback | 014-d | [ ] |
| 17 | References | Reference | [ ] |
| 18 | Production Readiness | 014-i | [ ] |
| 19 | Networking | 014-i | [ ] |
| 19.7 | Extension Upgrade Strategy | 014-i | [ ] |
| 20 | Security | 014-i | [ ] |
| 21 | Operations Runbook | 014-i | [ ] |
| 21.4 | Slot Cleanup on Node Removal | 014-i | [ ] |
| 22 | Testing Requirements | All | [ ] |

---

## Testing Requirements

**Reference**: `docs/BIDIRECTIONAL_REPLICATION.md` Section 22

### Core Principles

```
┌─────────────────────────────────────────────────────────────────┐
│                    Testing Philosophy                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ❌ PROHIBITED:                                                 │
│  • Mocks (gomock, mockery, etc.)                               │
│  • Fakes (in-memory implementations)                           │
│  • Test doubles (stubs, spies)                                 │
│  • Interface-based dependency injection for testing            │
│                                                                 │
│  ✓ REQUIRED:                                                   │
│  • Real PostgreSQL via testcontainers                          │
│  • Real steep_repl extension installed                         │
│  • Real steep-repl daemon processes                            │
│  • Real network connections (localhost/Docker network)         │
│  • Full replication topologies for integration tests           │
│                                                                 │
│  TARGET: 70% code coverage                                      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Test Types by Feature

| Feature | Unit Tests | Integration Tests | Topology Tests |
|---------|------------|-------------------|----------------|
| 014-a | Config parsing, path handling | Extension install, daemon start, IPC | - |
| 014-b | Progress calculation | Snapshot copy, schema fingerprint | Two-node init |
| 014-c | Range utilization calc | Constraint creation, allocation | Range enforcement |
| 014-d | Conflict type parsing | Conflict logging, resolution | UPDATE-UPDATE, INSERT-INSERT |
| 014-e | DDL parsing | ProcessUtility hook, queue | DDL replication |
| 014-f | Filter parsing | Publication creation | Filtered replication |
| 014-g | Priority sorting | Node registration, health | Coordinator election, failover |
| 014-h | Metric calculation | Health endpoint | Dashboard integration |
| 014-i | Config validation | Tailscale status, RBAC | Failover/failback |

### Per-Feature Testing Criteria

Each feature MUST include:

1. **Integration tests** with real PostgreSQL (testcontainers)
2. **Topology tests** for replication features (two-node minimum)
3. **Cross-platform tests** for IPC (named pipes on Windows, Unix sockets on Linux)
4. **Extension tests** via pgrx `#[pg_test]`

### Test Infrastructure

```go
// Required test setup for every replication test
func SetupTwoNodeTopology(t *testing.T) *TwoNodeTopology {
    // 1. Create Docker network
    // 2. Start two PostgreSQL 17 or 18 containers with wal_level=logical
    // 3. Install steep_repl extension on both
    // 4. Create test schema on both
    // 5. Start steep-repl daemons
    // 6. Setup bidirectional replication
    // Returns topology with connection strings and cleanup function
}
```

### Coverage Requirements

| Package | Target | Enforcement |
|---------|--------|-------------|
| `internal/repl/ranges` | 75% | CI fails below |
| `internal/repl/conflicts` | 75% | CI fails below |
| `internal/repl/ddl` | 70% | CI fails below |
| `internal/repl/topology` | 70% | CI fails below |
| `extensions/steep_repl` | 70% | cargo pgrx test |
| **Overall** | **70%** | CI fails below |

### Makefile Targets

```makefile
test:              # Run all tests
test-short:        # Unit tests only (no testcontainers)
test-integration:  # Integration tests with real PostgreSQL
test-topology:     # Full replication topology tests
test-extension:    # Rust extension tests via pgrx
test-coverage:     # Coverage report with 70% threshold check
```

### Acceptance Criteria for Each Feature

Every feature's acceptance criteria includes:

- [ ] Integration tests pass with real PostgreSQL 17, 18
- [ ] Topology tests pass with two-node replication
- [ ] Extension tests pass via `cargo pgrx test`
- [ ] Coverage meets package-specific threshold
- [ ] No mocks, fakes, or test doubles in test code

---

## References

- **Design Document**: `docs/BIDIRECTIONAL_REPLICATION.md` v1.0
- **Constitution**: `.specify/memory/constitution.md`
- **pgrx**: https://github.com/pgcentralfoundation/pgrx
- **kardianos/service**: https://github.com/kardianos/service
- **PostgreSQL 18 Logical Replication**: https://www.postgresql.org/docs/18/logical-replication.html
- **testcontainers-go**: https://github.com/testcontainers/testcontainers-go
