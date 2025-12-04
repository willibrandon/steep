# Bidirectional Replication Implementation Guide

## Complete Spec-Kit Workflow Roadmap

This guide provides the complete logical sequence of `/speckit.specify` commands necessary to build Steep's bidirectional replication system. Each feature is designed to be independently deliverable, testable, and aligned with the constitution's principles of incremental delivery.

**Reference Design Document**: `docs/BIDIRECTIONAL_REPLICATION.md` v0.8

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

Bidirectional replication will be built through **6 distinct features**, each following the complete spec-kit workflow:

1. `/speckit.specify` - Define user stories and requirements
2. `/speckit.clarify` - Resolve ambiguities (recommended)
3. `/speckit.plan` - Create technical implementation plan
4. `/speckit.tasks` - Generate actionable task breakdown
5. `/speckit.analyze` - Validate consistency (optional)
6. `/speckit.implement` - Execute implementation

Each feature delivers standalone value and builds upon previous features. The sequence is optimized for:
- **Early user value**: Core replication infrastructure first
- **Reduced risk**: Extension and daemon before UI
- **Independent testing**: Each feature testable in isolation
- **Iterative refinement**: Learn and adapt between features

---

## Prerequisites

Before starting bidirectional replication development, ensure:

1. **Constitution established**: Run `/speckit.constitution` (already completed)
2. **Steep core features complete**: Features 001-006 implemented (foundation through replication monitoring)
3. **PostgreSQL 18 available**: Required for native conflict logging and enhanced logical replication
4. **Rust/pgrx environment**: For steep_repl extension development
   - Rust toolchain (MSVC on Windows, stable on Linux/macOS)
   - `cargo install cargo-pgrx`
   - PostgreSQL 18 development files
5. **Go 1.25+**: For steep-repl daemon
6. **Git workflow**: Feature branch naming (014-a-foundation, 014-b-ranges, etc.)

---

## Feature Breakdown Strategy

### Dependency Hierarchy

```
014-a-foundation (Extension + Daemon skeleton)
    ├── 014-b-identity-ranges (Range allocation, constraints)
    │   └── 014-c-conflict-resolution (Conflict handling)
    │       └── 014-d-ddl-replication (DDL capture, queue)
    │           └── 014-e-topology (Multi-node coordination)
    │               └── 014-f-production (Networking, security, validation)
```

### Priority Classification

- **P1 (Must-Have)**: Features 014-a through 014-c - Core replication functionality
- **P2 (Should-Have)**: Features 014-d and 014-e - DDL and multi-node
- **P3 (Nice-to-Have)**: Feature 014-f - Production hardening

---

## Implementation Sequence

### Phase 1: Foundation (P1)

| Feature | Branch | Dependencies | Scope |
|---------|--------|--------------|-------|
| 014-a - Foundation & Infrastructure | `014-a-foundation` | 001-006 | Extension skeleton, daemon, IPC |
| 014-b - Identity Range Management | `014-b-identity-ranges` | 014-a | Ranges, constraints, allocation |
| 014-c - Conflict Detection & Resolution | `014-c-conflict-resolution` | 014-b | Detection, strategies, UI |

### Phase 2: Advanced Replication (P2)

| Feature | Branch | Dependencies | Scope |
|---------|--------|--------------|-------|
| 014-d - DDL Replication | `014-d-ddl-replication` | 014-c | ProcessUtility hook, queue, approval |
| 014-e - Topology & Coordination | `014-e-topology` | 014-d | Multi-node, election, mesh |

### Phase 3: Production Readiness (P3)

| Feature | Branch | Dependencies | Scope |
|---------|--------|--------------|-------|
| 014-f - Production Hardening | `014-f-production` | 014-e | Networking, security, validation, runbook |

---

## Feature Details

### Feature 014-a: Foundation & Infrastructure

**Branch**: `014-a-foundation`

**Purpose**: Establish bidirectional replication infrastructure including the steep_repl PostgreSQL extension and steep-repl daemon with cross-platform IPC.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want to install the steep_repl extension on PostgreSQL 18 to enable bidirectional replication metadata tracking
2. **P1**: As a DBA, I want to install the steep-repl daemon as a system service (Windows SCM, Linux systemd, macOS launchd)
3. **P1**: As a DBA, I want steep-repl to communicate with PostgreSQL using pgx connection pooling
4. **P2**: As a DBA, I want steep TUI to communicate with steep-repl daemon via IPC (named pipes on Windows, Unix sockets on Linux/macOS)
5. **P2**: As a DBA, I want steep-repl to expose gRPC endpoints for node-to-node communication
6. **P3**: As a DBA, I want steep-repl to expose HTTP health check endpoints for load balancers

**Technical Scope**:

*steep_repl Extension (Rust/pgrx):*
- Extension skeleton with `CREATE EXTENSION steep_repl`
- Schema creation: `steep_repl` schema with core metadata tables
- Tables: `nodes`, `identity_ranges`, `conflict_log`, `ddl_queue`, `coordinator_state`, `audit_log`
- Basic functions: `steep_repl.version()`, `steep_repl.node_id()`
- Custom GUC: `steep_repl.bypass_range_check`
- ProcessUtility hook skeleton (DDL capture - populated in 014-d)

*steep-repl Daemon (Go):*
- Command structure: `install`, `uninstall`, `start`, `stop`, `status`, `run`, `version`
- Service management via `kardianos/service` (cross-platform)
- Configuration loading from `~/.config/steep/config.yaml`
- PostgreSQL connection management via `pgx/pgxpool`
- IPC: `github.com/Microsoft/go-winio` for Windows named pipes
- gRPC server skeleton with TLS support
- Health check HTTP endpoint (`/health`)
- Structured logging with JSON output

*Directory Structure:*
```
steep/
├── cmd/
│   └── steep-repl/          # New daemon entry point
├── internal/
│   └── repl/                # New replication package
│       ├── config/          # Replication config parsing
│       ├── ipc/             # IPC (named pipes/unix sockets)
│       ├── grpc/            # gRPC server/client
│       ├── coordinator/     # Coordinator logic (skeleton)
│       ├── ranges/          # Range management (skeleton)
│       ├── conflict/        # Conflict handling (skeleton)
│       ├── ddl/             # DDL coordination (skeleton)
│       ├── topology/        # Topology management (skeleton)
│       └── metrics/         # Metrics collection
└── extensions/
    └── steep_repl/          # pgrx extension project
        ├── Cargo.toml
        ├── src/
        │   ├── lib.rs
        │   ├── schema.rs
        │   └── hooks.rs
        └── sql/
```

**Libraries**:
- Rust: `pgrx` (PostgreSQL extension framework)
- Go: `kardianos/service`, `Microsoft/go-winio`, `grpc-go`, `pgx/v5`

**Database Schema** (created by extension):
```sql
CREATE SCHEMA steep_repl;

CREATE TABLE steep_repl.nodes (
    node_id         TEXT PRIMARY KEY,
    node_name       TEXT NOT NULL,
    host            TEXT NOT NULL,
    port            INTEGER NOT NULL DEFAULT 5432,
    priority        INTEGER NOT NULL DEFAULT 50,
    is_coordinator  BOOLEAN NOT NULL DEFAULT false,
    last_seen       TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE steep_repl.coordinator_state (
    key             TEXT PRIMARY KEY,
    value           JSONB NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE steep_repl.audit_log (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    action          TEXT NOT NULL,
    actor           TEXT NOT NULL,
    target_type     TEXT,
    target_id       TEXT,
    old_value       JSONB,
    new_value       JSONB,
    client_ip       INET,
    success         BOOLEAN NOT NULL DEFAULT true,
    error_message   TEXT
);
```

**Cross-Platform Considerations**:
- Windows: Named pipe `\\.\pipe\steep-repl`, SCM service, `%APPDATA%\steep\config.yaml`
- Linux: Unix socket `/tmp/steep-repl.sock`, systemd service, `~/.config/steep/config.yaml`
- macOS: Unix socket, launchd service

**Acceptance Criteria**:
- Extension installs on PostgreSQL 16, 17, 18 (Windows, Linux, macOS)
- Daemon installs and runs as service on all platforms
- `steep-repl status` shows connection to PostgreSQL
- `steep-repl health` returns JSON health status
- Steep TUI can connect to daemon via IPC
- gRPC server accepts connections on configurable port (default 5433)
- All tables created with proper indexes
- Audit logging captures extension load events

**Spec-Kit Command**:
```bash
/speckit.specify Build the foundation infrastructure for Steep's bidirectional replication system. Create a PostgreSQL extension (steep_repl) using Rust/pgrx that establishes the replication schema with metadata tables for nodes, identity ranges, conflicts, DDL queue, coordinator state, and audit logging. Implement a Go daemon (steep-repl) using kardianos/service for cross-platform service management (Windows SCM, Linux systemd, macOS launchd). The daemon should connect to PostgreSQL via pgx, expose gRPC endpoints for node-to-node communication, and provide IPC communication with Steep TUI via named pipes (Windows) or Unix sockets (Linux/macOS). Include health check HTTP endpoint. Windows is the primary deployment target. Focus on P1 stories (extension install, daemon service) first. Reference: docs/BIDIRECTIONAL_REPLICATION.md sections 2, 3, 11, 12.
```

---

### Feature 014-b: Identity Range Management

**Branch**: `014-b-identity-ranges`

**Purpose**: Implement identity range allocation and enforcement to prevent primary key collisions in bidirectional replication, following SQL Server merge replication patterns.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want each node to have non-overlapping ID ranges for each replicated table to prevent primary key collisions
2. **P1**: As a DBA, I want CHECK constraints to enforce ID ranges so out-of-range inserts fail immediately
3. **P1**: As a DBA, I want the steep-repl daemon to automatically allocate new ranges when utilization exceeds 80%
4. **P2**: As a DBA, I want to view identity range status in Steep TUI showing utilization per table
5. **P2**: As a DBA, I want to manually reallocate or expand ranges via CLI when needed
6. **P2**: As a DBA, I want a bypass mode to temporarily disable range checking for bulk imports
7. **P3**: As a DBA, I want alerts when ranges approach exhaustion (80%, 95% thresholds)

**Technical Scope**:

*Extension Functions (Rust/pgrx):*
```sql
-- Allocate a new range for a table on this node
CREATE FUNCTION steep_repl.allocate_range(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_range_size BIGINT DEFAULT 10000
) RETURNS TABLE (range_start BIGINT, range_end BIGINT);

-- Check range consumption percentage
CREATE FUNCTION steep_repl.check_range_threshold(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_threshold_percent INTEGER DEFAULT 80
) RETURNS TABLE (current_value BIGINT, range_end BIGINT, percent_used NUMERIC, needs_expansion BOOLEAN);

-- Apply range constraint to a table
CREATE FUNCTION steep_repl.apply_range_constraint(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_range_start BIGINT,
    p_range_end BIGINT
) RETURNS BOOLEAN;

-- Check ID range (called by constraint, supports bypass)
CREATE FUNCTION steep_repl.check_id_range(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_id BIGINT
) RETURNS BOOLEAN;
```

*Daemon Components (Go):*
```go
// internal/repl/ranges/coordinator.go
type RangeCoordinator struct {
    db          *pgxpool.Pool
    config      *config.RangeConfig
    checkTicker *time.Ticker
}

func (r *RangeCoordinator) MonitorRanges(ctx context.Context)
func (r *RangeCoordinator) AllocateRange(table string, size int64) (*Range, error)
func (r *RangeCoordinator) ExpandRange(table string) error
func (r *RangeCoordinator) EnableBypass(table string, duration time.Duration) error
func (r *RangeCoordinator) DisableBypass(table string) error
```

*Database Schema Additions:*
```sql
CREATE TABLE steep_repl.identity_ranges (
    id              BIGSERIAL PRIMARY KEY,
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    node_id         TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    range_start     BIGINT NOT NULL,
    range_end       BIGINT NOT NULL,
    allocated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    status          TEXT NOT NULL DEFAULT 'active',  -- active, exhausted, released
    UNIQUE (table_schema, table_name, node_id, range_start)
);

CREATE INDEX idx_identity_ranges_table ON steep_repl.identity_ranges(table_schema, table_name);
```

*UI Components:*
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

**Configuration**:
```yaml
replication:
  identity_ranges:
    enabled: true
    default_range_size: 10000
    threshold_percent: 80
    tables:
      orders:
        range_size: 100000
      audit_log:
        enabled: false  # Uses UUIDs
```

**Libraries**:
- Existing: `pgrx`, `pgx/v5`, `bubbletea`, `lipgloss`

**Acceptance Criteria**:
- Range allocation function creates non-overlapping ranges across nodes
- CHECK constraint `steep_range_<table>` enforces ranges
- Sequence reseeding to range start on allocation
- Automatic pre-allocation at 80% threshold
- TUI displays range utilization with color coding (green <80%, yellow 80-95%, red >95%)
- CLI commands: `steep-repl range status`, `steep-repl range allocate`
- Bypass mode with audit logging
- Constraint violations produce clear error messages
- Works on PostgreSQL 16, 17, 18

**Spec-Kit Command**:
```bash
/speckit.specify Implement identity range management for Steep bidirectional replication to prevent primary key collisions. Create PostgreSQL functions (via steep_repl extension) to allocate non-overlapping ranges per table per node, apply CHECK constraints enforcing ranges, and check range utilization. Implement range coordinator in steep-repl daemon for automatic range expansion at 80% threshold. Add TUI view showing range status with utilization percentages and color coding. Support bypass mode for bulk imports with audit logging. Include CLI commands for manual range management. Follows SQL Server merge replication identity range patterns. Reference: docs/BIDIRECTIONAL_REPLICATION.md section 4.
```

---

### Feature 014-c: Conflict Detection & Resolution

**Branch**: `014-c-conflict-resolution`

**Purpose**: Implement conflict detection using PostgreSQL 18's native conflict logging and resolution strategies including automatic (last-write-wins, node-priority) and manual resolution with UI support.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want INSERT-INSERT, UPDATE-UPDATE, and UPDATE-DELETE conflicts detected and logged automatically
2. **P1**: As a DBA, I want a default conflict resolution strategy (last-write-wins) applied automatically
3. **P1**: As a DBA, I want to view pending conflicts requiring manual resolution in Steep TUI
4. **P2**: As a DBA, I want to resolve conflicts manually by choosing local, remote, or merged values
5. **P2**: As a DBA, I want per-table resolution strategy configuration (last-write-wins, node-priority, manual)
6. **P2**: As a DBA, I want bulk conflict resolution for large transactions (resolve all by transaction ID)
7. **P3**: As a DBA, I want to revert a conflict resolution if it was wrong
8. **P3**: As a DBA, I want conflict history with audit trail for compliance

**Technical Scope**:

*Conflict Types*:
| Type | Description |
|------|-------------|
| INSERT-INSERT | Same PK inserted on both nodes |
| UPDATE-UPDATE | Same row updated on both nodes |
| UPDATE-DELETE | Row updated on one node, deleted on other |

*Resolution Strategies*:
| Strategy | Description |
|----------|-------------|
| `last_write_wins` | Higher timestamp wins (default) |
| `first_write_wins` | Lower timestamp wins |
| `node_priority` | Designated node always wins |
| `keep_local` | Local value always wins |
| `apply_remote` | Remote value always wins |
| `manual` | Queue for human resolution |

*Extension Schema:*
```sql
CREATE TABLE steep_repl.conflict_log (
    id              BIGSERIAL PRIMARY KEY,
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    subscription    TEXT NOT NULL,
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    conflict_type   TEXT NOT NULL,
    local_tuple     JSONB,
    remote_tuple    JSONB,
    local_origin    TEXT,
    remote_origin   TEXT,
    local_xact_ts   TIMESTAMPTZ,
    remote_xact_ts  TIMESTAMPTZ,
    origin_xid      BIGINT,  -- For grouping by transaction
    resolution      TEXT,    -- PENDING, APPLIED_REMOTE, KEPT_LOCAL, MERGED, REVERTED
    resolved_at     TIMESTAMPTZ,
    resolved_by     TEXT
);

CREATE INDEX idx_conflict_log_pending ON steep_repl.conflict_log(resolution) WHERE resolution = 'PENDING';
CREATE INDEX idx_conflict_log_table ON steep_repl.conflict_log(table_schema, table_name);
```

*Extension Functions:*
```sql
-- Resolve a single conflict
CREATE FUNCTION steep_repl.resolve_conflict(
    p_conflict_id BIGINT,
    p_resolution TEXT,
    p_merged_tuple JSONB DEFAULT NULL
) RETURNS BOOLEAN;

-- Bulk resolve by transaction
CREATE FUNCTION steep_repl.resolve_conflicts_bulk(
    p_resolution TEXT,
    p_filter_xid BIGINT DEFAULT NULL,
    p_filter_table TEXT DEFAULT NULL,
    p_filter_time_start TIMESTAMPTZ DEFAULT NULL,
    p_filter_time_end TIMESTAMPTZ DEFAULT NULL
) RETURNS INTEGER;

-- Revert a resolution
CREATE FUNCTION steep_repl.revert_resolution(
    p_conflict_id BIGINT,
    p_reason TEXT DEFAULT 'Manual revert'
) RETURNS BIGINT;

-- Apply a tuple to a table
CREATE FUNCTION steep_repl.apply_tuple(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_tuple JSONB
) RETURNS BOOLEAN;
```

*Daemon Components:*
```go
// internal/repl/conflict/arbitrator.go
type ConflictArbitrator struct {
    db       *pgxpool.Pool
    config   *config.ConflictConfig
    policies map[string]ResolutionStrategy
}

func (a *ConflictArbitrator) ProcessConflict(ctx context.Context, conflict *Conflict) error
func (a *ConflictArbitrator) ResolveManual(conflictID int64, resolution string, mergedTuple *json.RawMessage) error
func (a *ConflictArbitrator) ResolveBulk(filter BulkFilter, resolution string) (int, error)
func (a *ConflictArbitrator) Revert(conflictID int64, reason string) error
```

*UI - Conflict Resolution:*
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
│  │ notes: "checking..."    │    │ notes: "out for deliv"  │     │
│  └─────────────────────────┘    └─────────────────────────┘     │
│                                                                   │
│  [L]ocal wins  [R]emote wins  [M]erge  [S]kip                   │
│                                                                   │
│  ▼ 3 more conflicts pending                                      │
└───────────────────────────────────────────────────────────────────┘
```

*UI - Bulk Resolution:*
```
┌─ Pending Conflicts ───────────────────────────────────────────────┐
│                                                                   │
│  Showing: 47 conflicts (grouped by transaction)                  │
│                                                                   │
│  ▶ Transaction 1234567 (2025-12-03 14:32:00) - 23 conflicts      │
│    └─ orders: 15 UPDATE-UPDATE                                   │
│    └─ line_items: 8 UPDATE-UPDATE                                │
│                                                                   │
│  ▶ Transaction 1234590 (2025-12-03 14:32:05) - 12 conflicts      │
│    └─ inventory: 12 UPDATE-UPDATE                                │
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

**Clock Synchronization Requirements**:
- NTP required for last-write-wins
- `steep-repl` validates clock sync on startup
- Falls back to `pg_xact_commit_timestamp()` when enabled
- Node priority tie-breaker when timestamps equal

**Acceptance Criteria**:
- Conflicts logged to `steep_repl.conflict_log` with full tuple data
- Automatic resolution for configured strategies
- Manual resolution UI with side-by-side comparison
- Bulk resolution by transaction, table, or time range
- Revert capability with audit trail
- Clock sync validation on daemon start
- Per-table strategy configuration
- Conflict rate metrics exposed
- Alert integration for pending conflicts
- Works with PostgreSQL 18 native conflict logging

**Spec-Kit Command**:
```bash
/speckit.specify Implement conflict detection and resolution for Steep bidirectional replication. Integrate with PostgreSQL 18's native conflict logging (pg_stat_subscription_stats). Create conflict_log table storing local/remote tuples, timestamps, and resolution status. Implement resolution strategies: last_write_wins (default), first_write_wins, node_priority, keep_local, apply_remote, and manual. Build conflict arbitrator in steep-repl daemon for automatic resolution. Create TUI views for pending conflicts with side-by-side tuple comparison and manual resolution (local/remote/merge). Support bulk resolution by transaction ID for large transactions. Include revert capability for wrong resolutions. Require NTP clock synchronization with startup validation. Reference: docs/BIDIRECTIONAL_REPLICATION.md sections 8, 15.2, 15.3, 15.5.
```

---

### Feature 014-d: DDL Replication

**Branch**: `014-d-ddl-replication`

**Purpose**: Implement automatic DDL capture via PostgreSQL ProcessUtility hook and coordinated DDL application across nodes with optional approval workflow for destructive operations.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want CREATE TABLE, ALTER TABLE ADD COLUMN, and CREATE INDEX DDL automatically captured and replicated
2. **P1**: As a DBA, I want a DDL queue showing pending, applied, and rejected DDL operations
3. **P2**: As a DBA, I want DROP TABLE, ALTER TABLE DROP COLUMN, and TRUNCATE to require approval before replication
4. **P2**: As a DBA, I want to approve or reject queued DDL operations in Steep TUI
5. **P2**: As a DBA, I want schema fingerprinting to detect drift before DDL application
6. **P3**: As a DBA, I want to configure which DDL types are auto-applied vs require approval
7. **P3**: As a DBA, I want CREATE FUNCTION and CREATE TRIGGER excluded from replication by default

**Technical Scope**:

*ProcessUtility Hook (Rust/pgrx):*
```rust
// extensions/steep_repl/src/hooks.rs
use pgrx::prelude::*;

static mut PREV_PROCESS_UTILITY_HOOK: pg_sys::ProcessUtility_hook_type = None;

#[pg_guard]
pub unsafe extern "C" fn steep_process_utility_hook(
    pstmt: *mut pg_sys::PlannedStmt,
    query_string: *const std::os::raw::c_char,
    // ... other params
) {
    // Skip if replicated DDL context (prevent loops)
    if is_replicated_ddl_context() {
        call_prev_hook(...);
        return;
    }

    // Capture DDL before execution
    let ddl_info = capture_ddl_info(pstmt, query_string);

    // Execute original DDL
    call_prev_hook(...);

    // If successful, queue for replication
    if let Some(info) = ddl_info {
        queue_ddl_for_replication(info);
        notify_daemon();
    }
}
```

*DDL Queue Schema:*
```sql
CREATE TABLE steep_repl.ddl_queue (
    id              BIGSERIAL PRIMARY KEY,
    captured_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    origin_node     TEXT NOT NULL,
    ddl_command     TEXT NOT NULL,
    object_type     TEXT NOT NULL,
    object_schema   TEXT,
    object_name     TEXT,
    status          TEXT NOT NULL,  -- PENDING, APPROVED, APPLIED, REJECTED, FAILED
    pre_fingerprint TEXT,
    post_fingerprint TEXT,
    applied_at      TIMESTAMPTZ,
    applied_by      TEXT,
    error_message   TEXT
);

CREATE TABLE steep_repl.schema_fingerprints (
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    column_count    INTEGER NOT NULL,
    captured_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (table_schema, table_name)
);
```

*Extension Functions:*
```sql
-- Compute schema fingerprint (SHA256 of column definitions)
CREATE FUNCTION steep_repl.compute_fingerprint(
    p_schema TEXT,
    p_table TEXT
) RETURNS TEXT;

-- Compare fingerprints across nodes
CREATE FUNCTION steep_repl.compare_fingerprints(
    p_peer_node TEXT
) RETURNS TABLE (table_schema TEXT, table_name TEXT, local_fp TEXT, remote_fp TEXT, status TEXT);
```

*Daemon Components:*
```go
// internal/repl/ddl/coordinator.go
type DDLCoordinator struct {
    db     *pgxpool.Pool
    config *config.DDLConfig
}

func (d *DDLCoordinator) ProcessQueue(ctx context.Context) error
func (d *DDLCoordinator) ApplyDDL(ddlID int64) error
func (d *DDLCoordinator) ApproveDDL(ddlID int64, user string) error
func (d *DDLCoordinator) RejectDDL(ddlID int64, reason string, user string) error
func (d *DDLCoordinator) ValidateSchemaMatch(peer string) ([]SchemaDiff, error)
```

*UI - DDL Queue:*
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
│  #41 ALTER TABLE orders DROP COLUMN deprecated_field (node_a)    │
│      ⚠ Destructive operation - requires approval                 │
│                                                                   │
│  RECENTLY APPLIED                                                 │
│  ──────────────────────────────────────────────────────────────── │
│  #40 CREATE INDEX idx_orders_date ON orders(created_at) ✓        │
│  #39 ALTER TABLE customers ADD COLUMN loyalty_tier TEXT ✓        │
│                                                                   │
│  [A]pprove  [R]eject  [V]iew full DDL  [D]iff schemas            │
└───────────────────────────────────────────────────────────────────┘
```

**Configuration**:
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

**Acceptance Criteria**:
- ProcessUtility hook captures DDL on originating node
- DDL queued with object metadata and schema fingerprints
- Auto-apply for non-destructive DDL
- Approval workflow for destructive DDL
- Schema fingerprint validation before apply
- Clear error messages on apply failure
- TUI shows DDL queue with approval actions
- Audit logging for approvals/rejections
- Replication-loop prevention (don't re-capture replicated DDL)
- CREATE INDEX CONCURRENTLY supported

**Spec-Kit Command**:
```bash
/speckit.specify Implement DDL replication for Steep bidirectional replication using PostgreSQL ProcessUtility hook in the steep_repl extension (Rust/pgrx). Capture CREATE TABLE, ALTER TABLE, CREATE INDEX, DROP operations and queue them for replication. Store DDL in ddl_queue table with origin node, command, object metadata, and schema fingerprints. Implement approval workflow for destructive operations (DROP TABLE, ALTER TABLE DROP COLUMN, TRUNCATE). Auto-apply non-destructive DDL. Create schema fingerprinting (SHA256 of column definitions) for drift detection before DDL apply. Build TUI view showing DDL queue with approve/reject actions. Exclude CREATE FUNCTION and CREATE TRIGGER by default. Reference: docs/BIDIRECTIONAL_REPLICATION.md sections 9, 15.4.
```

---

### Feature 014-e: Topology & Coordination

**Branch**: `014-e-topology`

**Purpose**: Implement multi-node topology management including node discovery, coordinator election, health monitoring, and support for star and mesh topologies.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want to register nodes in a replication topology (star or mesh)
2. **P1**: As a DBA, I want health checks between nodes with automatic unhealthy detection
3. **P1**: As a DBA, I want coordinator election based on node priority for range/DDL coordination
4. **P2**: As a DBA, I want to view topology status in Steep TUI showing node health and lag
5. **P2**: As a DBA, I want automatic coordinator failover when the current coordinator becomes unreachable
6. **P2**: As a DBA, I want to add or remove nodes from the topology via CLI
7. **P3**: As a DBA, I want mesh topology where any node can coordinate with any other

**Technical Scope**:

*Topology Modes:*
```
STAR (Hub-Spoke)              MESH (Peer-to-Peer)

      ┌───┐                   ┌───┐───────┌───┐
      │ A │ (Hub)             │ A │       │ B │
      └─┬─┘                   └─┬─┘───────└─┬─┘
   ┌────┼────┐                  │    ╲ ╱    │
   │    │    │                  │     ╳     │
 ┌─┴─┐┌─┴─┐┌─┴─┐              ┌─┴─┐  ╱ ╲  ┌─┴─┐
 │ B ││ C ││ D │ (Spokes)     │ C │───────│ D │
 └───┘└───┘└───┘              └───┘       └───┘
```

*Daemon Components:*
```go
// internal/repl/topology/manager.go
type TopologyManager struct {
    db       *pgxpool.Pool
    grpc     *grpc.Server
    config   *config.TopologyConfig
    nodes    map[string]*NodeInfo
    isCoord  bool
}

func (t *TopologyManager) DiscoverPeers(ctx context.Context) error
func (t *TopologyManager) HealthCheck(ctx context.Context) error
func (t *TopologyManager) ElectCoordinator(ctx context.Context) error
func (t *TopologyManager) AddNode(node *NodeInfo) error
func (t *TopologyManager) RemoveNode(nodeID string) error
func (t *TopologyManager) GetTopology() *Topology
```

*Coordinator Election:*
```
2-Node Topology (Simple failover):
┌─────────────────────────────────────────────────────────┐
│ • One node is coordinator (highest priority)            │
│ • If coordinator unreachable, other node self-promotes  │
│ • State read from local steep_repl tables               │
│ • No split-brain possible (only 2 nodes)               │
└─────────────────────────────────────────────────────────┘

N-Node Topology:
┌─────────────────────────────────────────────────────────┐
│ • Priority-based election (highest available wins)      │
│ • Quorum required: (N/2)+1 nodes must agree             │
│ • Coordinator state in steep_repl.coordinator_state    │
└─────────────────────────────────────────────────────────┘
```

*gRPC Protocol:*
```protobuf
// proto/replication.proto
service SteepReplication {
    rpc HealthCheck(HealthRequest) returns (HealthResponse);
    rpc ProposeCoordinator(ProposeRequest) returns (ProposeResponse);
    rpc AllocateRange(RangeRequest) returns (RangeResponse);
    rpc PropagateConflict(ConflictNotification) returns (ConflictAck);
    rpc PropagateDDL(DDLNotification) returns (DDLAck);
}
```

*UI - Topology View:*
```
┌─ Replication Topology ────────────────────────────────────────────┐
│                                                                   │
│  Mode: MESH (2 nodes)                                            │
│  Coordinator: hq (this node)                                     │
│                                                                   │
│        ┌──────────┐                   ┌──────────┐               │
│        │    HQ    │                   │  CLOUD   │               │
│        │ ● coord  │◄─────────────────►│          │               │
│        │ 0 lag    │   Lag: 1.2s       │ ● online │               │
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

**Configuration**:
```yaml
replication:
  topology:
    mode: mesh
    this_node:
      name: hq
      priority: 100
    nodes:
      - name: cloud
        host: cloud.mynet.ts.net
        port: 5432
        grpc_port: 5433
        priority: 90
    health_check:
      interval: 5s
      timeout: 10s
      unhealthy_threshold: 3
```

**Acceptance Criteria**:
- Nodes register with topology on daemon start
- Health checks run at configurable interval
- Coordinator elected based on priority
- Automatic failover on coordinator failure
- TUI displays topology with node status
- CLI: `steep-repl node add`, `steep-repl node remove`, `steep-repl node status`
- gRPC communication secured with TLS
- Quorum enforcement for N>2 nodes
- State persistence in PostgreSQL tables

**Spec-Kit Command**:
```bash
/speckit.specify Implement topology and coordination for Steep multi-node bidirectional replication. Create topology manager in steep-repl daemon for node registration, discovery, and health monitoring. Implement priority-based coordinator election with automatic failover (no Raft for MVP; state stored in PostgreSQL). Support star (hub-spoke) and mesh topologies. Create gRPC protocol for node-to-node communication including health checks, range allocation requests, and conflict/DDL propagation. Build TUI topology view showing node status, coordinator role, and replication lag. Provide CLI commands for adding/removing nodes. Implement quorum requirements for N>2 node topologies. Reference: docs/BIDIRECTIONAL_REPLICATION.md sections 10, 15.1.
```

---

### Feature 014-f: Production Hardening

**Branch**: `014-f-production`

**Purpose**: Implement production readiness features including Tailscale networking integration, security hardening, data validation, failover/failback procedures, and operations runbook automation.

**User Stories** (Priority Order):

1. **P1**: As a DBA, I want data validation (row counts, checksums) to detect replication divergence
2. **P1**: As a DBA, I want Tailscale integration for secure cross-site networking without firewall configuration
3. **P1**: As a DBA, I want failover to the surviving node when one node becomes unreachable
4. **P2**: As a DBA, I want failback procedure to restore normal operation when the failed node returns
5. **P2**: As a DBA, I want RBAC for conflict resolution and DDL approval permissions
6. **P2**: As a DBA, I want credential management via environment variables or password commands (no plaintext)
7. **P3**: As a DBA, I want business notifications (Slack, email, PagerDuty) for replication events
8. **P3**: As a DBA, I want coordinated backup support for consistent cross-node recovery

**Technical Scope**:

*Data Validation:*
```sql
-- Level 1: Row count validation (fast, frequent)
CREATE FUNCTION steep_repl.validate_row_counts(
    p_peer_node TEXT DEFAULT NULL
) RETURNS TABLE (
    table_schema TEXT, table_name TEXT,
    local_count BIGINT, remote_count BIGINT,
    difference BIGINT, diff_percent NUMERIC, status TEXT
);

-- Level 2: Checksum validation (slower, periodic)
CREATE FUNCTION steep_repl.validate_checksums(
    p_table_schema TEXT, p_table_name TEXT,
    p_peer_node TEXT, p_sample_pct NUMERIC DEFAULT 100
) RETURNS TABLE (pk_value TEXT, local_hash TEXT, remote_hash TEXT, divergence_type TEXT);

-- Level 3: Full compare with repair (on-demand)
CREATE FUNCTION steep_repl.compare_and_repair(
    p_table_schema TEXT, p_table_name TEXT,
    p_peer_node TEXT, p_dry_run BOOLEAN DEFAULT true
) RETURNS TABLE (pk_value TEXT, divergence_type TEXT, repair_sql TEXT, applied BOOLEAN);
```

*Tailscale Integration:*
```go
// internal/repl/network/tailscale.go
type TailscaleStatus struct {
    Self struct {
        Online   bool
        HostName string
        TailAddr string
    }
    Peer map[string]struct {
        HostName      string
        Online        bool
        LastHandshake string
    }
}

func GetTailscaleStatus() (*TailscaleStatus, error)
func (d *Daemon) checkTailscalePeers() error
```

*Failover/Failback:*
```go
// internal/repl/failover/manager.go
type FailoverManager struct {
    db      *pgxpool.Pool
    config  *config.FailoverConfig
    topo    *topology.Manager
}

func (f *FailoverManager) InitiateFailover(promotedNode string) error
func (f *FailoverManager) PrepareFailback(failedNode string) error
func (f *FailoverManager) SyncFailback(failedNode string) error
func (f *FailoverManager) CompleteFailback(failedNode string) error
```

*Security - RBAC:*
```yaml
replication:
  rbac:
    enabled: true
    roles:
      viewer:
        permissions: [view_status, view_conflicts, view_ranges, view_ddl_queue]
      operator:
        inherits: viewer
        permissions: [resolve_conflicts, approve_ddl, reject_ddl]
      admin:
        inherits: operator
        permissions: [enable_bypass, manage_ranges, failover, failback]
    role_mapping:
      steep_admin: admin
      steep_ops: operator
```

*Notifications:*
```yaml
replication:
  notifications:
    channels:
      slack:
        webhook_url: "https://hooks.slack.com/..."
      email:
        smtp_host: "smtp.company.com"
        to: ["dba@company.com"]
      pagerduty:
        routing_key: "..."
    routing:
      conflict_detected:
        channels: [slack]
        tables: [orders, payments, inventory]
      failover_started:
        channels: [slack, email, pagerduty]
```

*UI - Data Validation:*
```
┌─ Data Validation ─────────────────────────────────────────────────┐
│                                                                   │
│  Last Validation: 2025-12-03 03:15:00 (8 hours ago)              │
│  Status: ● OK (all tables match)                                 │
│                                                                   │
│  Row Counts (5m ago):                                            │
│  ──────────────────────────────────────────────────────────────── │
│  Table           Local       Remote      Diff      Status        │
│  orders          1,245,892   1,245,892   0         ● OK          │
│  customers       89,234      89,234      0         ● OK          │
│  line_items      4,892,103   4,892,101   2         ⚠ WARN        │
│                                                                   │
│  [V]alidate now  [R]epair divergence  [H]istory                 │
└───────────────────────────────────────────────────────────────────┘
```

*UI - Failover Status:*
```
┌─ Failover Status ─────────────────────────────────────────────────┐
│                                                                   │
│  ⚠ FAILOVER ACTIVE                                               │
│                                                                   │
│  HQ (node_a):      UNREACHABLE since 2025-12-03 14:30:00        │
│  Cloud (node_b): PROMOTED (coordinator)                        │
│                                                                   │
│  Failover initiated: 2025-12-03 14:35:00 (automatic)            │
│  Duration: 2h 15m                                                │
│                                                                   │
│  When HQ returns:                                                │
│  [P]repare failback  [V]alidate  [C]omplete failback            │
└───────────────────────────────────────────────────────────────────┘
```

**Configuration**:
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

  failover:
    enabled: true
    mode: manual  # or automatic
    automatic:
      timeout: 5m
      grace_period: 30s

  networking:
    provider: tailscale
    tailscale:
      expect_connected: true

  credentials:
    hq:
      user_env: STEEP_HQ_USER
      password_env: STEEP_HQ_PASSWORD
```

**CLI Commands**:
```bash
# Validation
steep-repl validate row-counts
steep-repl validate checksums --table orders --sample 10
steep-repl validate repair --table orders --dry-run

# Failover
steep-repl failover --promote cloud
steep-repl failback --prepare
steep-repl failback --sync
steep-repl failback --complete

# Backup coordination
steep-repl backup prepare --all-nodes
steep-repl backup complete --backup-id bk_...
```

**Acceptance Criteria**:
- Row count validation runs on schedule, alerts on divergence
- Checksum validation with sampling for large tables
- Repair function generates corrective SQL
- Tailscale status integration for peer health
- Manual and automatic failover modes
- Failback procedure with sync and validation
- RBAC enforcement on sensitive operations
- Credential loading from env vars or commands
- Notification channels for key events
- Clock sync validation on startup
- Comprehensive audit logging
- All CLI commands documented

**Spec-Kit Command**:
```bash
/speckit.specify Implement production hardening for Steep bidirectional replication. Create data validation system with row count checks (frequent), checksum validation (periodic with sampling), and full compare with repair script generation. Integrate Tailscale for zero-config mesh networking between Windows and Linux nodes. Implement failover (manual/automatic) with identity range expansion and coordinator promotion. Create failback procedure with sync, validation, and completion steps. Add RBAC for conflict resolution and DDL approval permissions. Support credentials via environment variables and password commands. Integrate notification channels (Slack, email, PagerDuty) for replication events. Include clock synchronization validation. Reference: docs/BIDIRECTIONAL_REPLICATION.md sections 17-20.
```

---

## Best Practices

### Extension Development (pgrx)

1. **Test on all target PostgreSQL versions** (16, 17, 18)
2. **Use `#[pg_guard]` for all C-callable functions**
3. **Handle panics gracefully** - PostgreSQL doesn't unwind Rust panics
4. **Use prepared statements** for repeated queries
5. **Avoid long-running transactions** in hooks

### Daemon Development

1. **Graceful shutdown** - Handle SIGTERM, complete in-flight operations
2. **Structured logging** - Use JSON format for observability
3. **Health checks** - Expose /health for load balancers
4. **Retry with backoff** - For transient failures
5. **Metrics** - Expose Prometheus metrics

### Cross-Platform

1. **Use `filepath.Join()`** - Never hardcode path separators
2. **Use `runtime.GOOS`** - For platform-specific code paths
3. **Test on Windows first** - It's the primary deployment target
4. **Named pipes on Windows** - Not Unix sockets

### Security

1. **No plaintext passwords** - Use env vars or password commands
2. **TLS for gRPC** - Always encrypt node-to-node communication
3. **Audit everything** - Log all sensitive operations
4. **RBAC enforcement** - Check permissions before actions

---

## Success Criteria

### Feature 014-a (Foundation)
- [ ] Extension installs on PG 16, 17, 18 (Windows, Linux, macOS)
- [ ] Daemon runs as service on all platforms
- [ ] IPC between TUI and daemon works
- [ ] gRPC server accepts connections

### Feature 014-b (Identity Ranges)
- [ ] Ranges allocated without overlap
- [ ] CHECK constraints prevent violations
- [ ] Automatic expansion at threshold
- [ ] Bypass mode with audit trail

### Feature 014-c (Conflicts)
- [ ] Conflicts detected and logged
- [ ] Automatic resolution strategies work
- [ ] Manual resolution UI functional
- [ ] Bulk resolution by transaction

### Feature 014-d (DDL)
- [ ] DDL captured via hook
- [ ] Auto-apply and approval workflows
- [ ] Schema fingerprint validation
- [ ] Loop prevention works

### Feature 014-e (Topology)
- [ ] Nodes register and discover
- [ ] Health checks functional
- [ ] Coordinator election works
- [ ] Failover on coordinator loss

### Feature 014-f (Production)
- [ ] Validation detects divergence
- [ ] Tailscale integration works
- [ ] Failover/failback procedures complete
- [ ] RBAC enforced
- [ ] Notifications sent

---

## References

- **Design Document**: `docs/BIDIRECTIONAL_REPLICATION.md` v0.8
- **Constitution**: `.specify/memory/constitution.md`
- **pgrx Documentation**: https://github.com/pgcentralfoundation/pgrx
- **kardianos/service**: https://github.com/kardianos/service
- **PostgreSQL 18 Logical Replication**: https://www.postgresql.org/docs/18/logical-replication.html
