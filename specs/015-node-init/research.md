# Research: Node Initialization & Snapshots

**Feature**: 015-node-init
**Date**: 2025-12-04

## Research Topics

### 1. PostgreSQL Logical Replication copy_data Behavior

**Question**: How does PostgreSQL's copy_data=true work with subscriptions, and how can we track progress?

**Decision**: Use `CREATE SUBSCRIPTION ... WITH (copy_data = true)` for automatic initialization. Track progress via `pg_stat_subscription_stats` (PG15+) and `pg_stat_progress_copy` (during COPY phase).

**Rationale**:
- PostgreSQL natively handles table copying when subscription is created
- `pg_stat_subscription_stats` provides sync_state per table (init, data, sync, ready)
- For PG18, parallel COPY is available via `streaming = parallel` subscription option
- Progress granularity is per-table; we poll and aggregate for overall progress

**Alternatives Considered**:
- Manual COPY TO/FROM: More control but reinvents PostgreSQL's built-in sync
- pg_dump/pg_restore: Better for very large databases (manual init path)

**Key Queries**:
```sql
-- Check subscription table sync progress
SELECT srsubid, srrelid::regclass, srsublsn, srsubstate
FROM pg_subscription_rel
WHERE srsubid = (SELECT oid FROM pg_subscription WHERE subname = 'steep_sub');

-- States: 'i' = init, 'd' = data copying, 's' = sync, 'r' = ready
```

### 2. Two-Phase Snapshot Generation

**Question**: How to implement portable snapshot generation separate from application?

**Decision**: Use `pg_dump` with `--snapshot` for consistent export. Store manifest.json with LSN, table list, checksums. Use parallel COPY FROM for import on PG18.

**Rationale**:
- pg_dump --snapshot uses exported snapshot for consistency
- Separating generation from application allows:
  - Network transfer optimization (compress, resume)
  - Multi-target initialization from single snapshot
  - User control over timing
- PG18's parallel COPY FROM speeds up large table imports

**Implementation**:
```bash
# Phase 1: Generate on source
steep-repl snapshot generate node_a --output /snapshots/2025-12-04/

# Creates:
# /snapshots/2025-12-04/manifest.json  (LSN, tables, checksums)
# /snapshots/2025-12-04/schema.sql
# /snapshots/2025-12-04/data/table1.csv.gz
# /snapshots/2025-12-04/data/table2.csv.gz
# /snapshots/2025-12-04/sequences.json

# Phase 2: Apply on target
steep-repl snapshot apply node_b --input /snapshots/2025-12-04/
```

**Alternatives Considered**:
- pg_basebackup: Physical copy, not suitable for logical replication setup
- Streaming only: Network bandwidth constraints for large databases

### 3. Manual Initialization Workflow

**Question**: How to support user-managed pg_dump/pg_basebackup with steep-repl completion?

**Decision**: Prepare phase creates replication slot and records LSN. Complete phase verifies state, installs steep_repl metadata, creates subscription with copy_data=false.

**Rationale**:
- Enterprise users have existing backup tooling
- Multi-TB databases benefit from custom backup pipelines
- Replication slot ensures WAL retention during backup window

**Workflow**:
```bash
# Step 1: Prepare (creates slot, records LSN)
steep-repl node prepare node_a --slot steep_init_slot
# Output: LSN: 0/1234ABCD, Slot: steep_init_slot

# Step 2: User runs their backup (steep-repl not involved)
pg_basebackup -D /backup -S steep_init_slot -X stream -v

# Step 3: User restores to target (steep-repl not involved)
pg_restore ...

# Step 4: Complete (verifies, creates subscription)
steep-repl node complete node_b --source node_a --source-lsn 0/1234ABCD
```

**Key Queries**:
```sql
-- Create replication slot
SELECT pg_create_logical_replication_slot('steep_init_slot', 'pgoutput');

-- Get current LSN
SELECT pg_current_wal_lsn();
```

### 4. Schema Fingerprinting Algorithm

**Question**: How to compute schema fingerprints for drift detection?

**Decision**: SHA256 hash of canonical column definition string, computed in PostgreSQL extension for performance.

**Rationale**:
- In-database computation avoids data transfer overhead
- SHA256 provides collision resistance
- Canonical ordering (ordinal_position) ensures deterministic fingerprints
- Column string includes: name, type, default, nullability

**Implementation**:
```sql
CREATE FUNCTION steep_repl.compute_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' ||
        coalesce(column_default, 'NULL') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;
```

**Fingerprint Comparison**:
```sql
CREATE FUNCTION steep_repl.compare_fingerprints(p_peer_node TEXT)
RETURNS TABLE (
    table_schema TEXT,
    table_name TEXT,
    local_fingerprint TEXT,
    remote_fingerprint TEXT,
    status TEXT  -- MATCH, MISMATCH, LOCAL_ONLY, REMOTE_ONLY
);
```

**Alternatives Considered**:
- Hash entire CREATE TABLE DDL: Formatting differences cause false mismatches
- MD5: Weaker, SHA256 preferred for security

### 5. Initialization State Machine

**Question**: How to track and persist initialization states across daemon restarts?

**Decision**: Add `init_state` column to `steep_repl.nodes` table with CHECK constraint for valid states.

**Rationale**:
- PostgreSQL-backed state survives daemon restarts
- Matches existing node health status pattern
- State transitions logged to audit_log for debugging

**States**:
```
UNINITIALIZED → PREPARING → COPYING → CATCHING_UP → SYNCHRONIZED
      ↓             ↓           ↓            ↓
   FAILED ◄─────────────────────────────── DIVERGED
      ↓                                      ↓
      └──────────► REINITIALIZING ◄──────────┘
```

**Schema Change**:
```sql
ALTER TABLE steep_repl.nodes ADD COLUMN init_state TEXT NOT NULL DEFAULT 'uninitialized';
ALTER TABLE steep_repl.nodes ADD CONSTRAINT nodes_init_state_check
    CHECK (init_state IN ('uninitialized', 'preparing', 'copying',
           'catching_up', 'synchronized', 'diverged', 'failed', 'reinitializing'));
```

### 6. Progress Tracking Implementation

**Question**: How to track and report initialization progress in real-time?

**Decision**: Progress stored in `steep_repl.init_progress` table, updated by daemon, polled by TUI via gRPC streaming.

**Rationale**:
- Persistent progress survives daemon restart
- gRPC streaming provides real-time updates to TUI
- Table structure supports both overall and per-table progress

**Schema**:
```sql
CREATE TABLE steep_repl.init_progress (
    node_id TEXT PRIMARY KEY REFERENCES steep_repl.nodes(node_id),
    phase TEXT NOT NULL,  -- 'generation', 'application', 'catching_up'
    overall_percent REAL NOT NULL DEFAULT 0,
    tables_total INTEGER NOT NULL DEFAULT 0,
    tables_completed INTEGER NOT NULL DEFAULT 0,
    current_table TEXT,
    current_table_percent REAL DEFAULT 0,
    rows_copied BIGINT DEFAULT 0,
    bytes_copied BIGINT DEFAULT 0,
    throughput_rows_sec REAL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    eta_seconds INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 7. Bidirectional Merge with Existing Data

**Question**: How to handle initialization when both nodes have existing data?

**Decision**: Quiesce writes, analyze overlap, resolve conflicts with user-selected strategy, enable replication with copy_data=false.

**Rationale**:
- Common scenario in migrations/mergers
- User must choose conflict resolution strategy
- copy_data=false prevents re-syncing already-reconciled data

**Conflict Analysis**:
```sql
-- Compare primary keys to find:
-- 1. Matching rows (same PK, same data hash)
-- 2. Conflicting rows (same PK, different data hash)
-- 3. Unique rows (PK exists on one node only)

SELECT
    CASE
        WHEN b.pk IS NULL THEN 'A_ONLY'
        WHEN a.pk IS NULL THEN 'B_ONLY'
        WHEN a.data_hash = b.data_hash THEN 'MATCH'
        ELSE 'CONFLICT'
    END as status,
    count(*)
FROM node_a_rows a
FULL OUTER JOIN node_b_rows b USING (pk)
GROUP BY 1;
```

**Resolution Strategies**:
- `prefer-node-a`: Keep node A's version for conflicts
- `prefer-node-b`: Keep node B's version for conflicts
- `last-modified`: Use most recently modified row (requires timestamp column)
- `manual`: Present conflicts for user decision

### 8. Structured JSON Logging

**Question**: What events should be logged for external monitoring integration?

**Decision**: Emit JSON logs for all state transitions, milestones, and errors with consistent schema.

**Rationale**:
- Integration with existing log aggregators (ELK, Splunk, CloudWatch)
- Structured format enables alerting on specific events
- Audit trail for troubleshooting

**Log Schema**:
```json
{
    "timestamp": "2025-12-04T14:30:00Z",
    "level": "info",
    "event": "init.table_complete",
    "node_id": "node_b",
    "source_node": "node_a",
    "table": "orders",
    "rows_copied": 1500000,
    "duration_ms": 45000,
    "phase": "copying",
    "overall_progress": 0.62
}
```

**Event Types**:
- `init.started`, `init.completed`, `init.failed`, `init.cancelled`
- `init.phase_started`, `init.phase_completed` (generation, application, catching_up)
- `init.table_started`, `init.table_complete`, `init.table_failed`
- `init.state_change` (any state transition)
- `schema.mismatch_detected`, `schema.sync_applied`

## Summary

All research topics resolved. No NEEDS CLARIFICATION items remain.

| Topic | Decision | Confidence |
|-------|----------|------------|
| copy_data initialization | Use native PostgreSQL subscription sync | High |
| Two-phase snapshot | pg_dump + parallel COPY, manifest.json | High |
| Manual initialization | Prepare/complete workflow with slot | High |
| Schema fingerprinting | SHA256 of column definitions in extension | High |
| State machine | 8 states in nodes table with CHECK constraint | High |
| Progress tracking | PostgreSQL table + gRPC streaming | High |
| Bidirectional merge | Overlap analysis + conflict resolution strategies | High |
| Structured logging | JSON events for state transitions/milestones | High |
