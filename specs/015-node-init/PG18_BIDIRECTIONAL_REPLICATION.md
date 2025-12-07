# PostgreSQL 18 Bidirectional Replication Features

This document captures PostgreSQL 18's native support for bidirectional logical replication and how Steep leverages these capabilities.

## Overview

PostgreSQL 18 introduces comprehensive bidirectional replication support with:
- 8 conflict types with detailed logging
- Origin-based conflict detection and prevention
- Dead tuple retention for update-deleted conflict detection
- Conflict statistics monitoring
- Sequence synchronization
- Configurable data retention duration

## Conflict Types (8 Total)

PostgreSQL 18 defines these conflict types in `src/include/replication/conflict.h`:

| Conflict Type | Description | When It Occurs |
|--------------|-------------|----------------|
| `CT_INSERT_EXISTS` | Insert violates unique constraint | Row with same PK already exists |
| `CT_UPDATE_ORIGIN_DIFFERS` | Update on row modified by different origin | Both nodes modified same row |
| `CT_UPDATE_EXISTS` | Update violates unique constraint | Updated row would conflict with existing |
| `CT_UPDATE_DELETED` | Target row deleted by different origin | Node A updated, Node B deleted same row |
| `CT_UPDATE_MISSING` | Target row not found | Row doesn't exist on subscriber |
| `CT_DELETE_ORIGIN_DIFFERS` | Delete on row modified by different origin | Both nodes touched same row |
| `CT_DELETE_MISSING` | Target row not found for delete | Already deleted or never existed |
| `CT_MULTIPLE_UNIQUE_CONFLICTS` | Single operation violates multiple constraints | Complex multi-column constraints |

**New in PG18**: `CT_UPDATE_DELETED` and `CT_UPDATE_ORIGIN_DIFFERS` require `retain_dead_tuples=true` and `track_commit_timestamp=on`.

## Conflict Logging Detail

Each logged conflict includes:
- Conflict type
- Affected table (schema.name)
- Origin ID and name (requires `track_commit_timestamp`)
- Transaction ID and commit timestamp of conflicting local row
- Key values from unique indexes
- Existing local row values
- Remote incoming row values
- Replica identity columns

Implementation: `src/backend/replication/logical/conflict.c` (488+ lines, new in PG18)

## Key Subscription Parameters

### origin = none (Ping-Pong Prevention)

```sql
-- Modern approach to prevent replication loops
CREATE SUBSCRIPTION sub_from_node_b
    CONNECTION 'host=node-b dbname=mydb'
    PUBLICATION pub_b
    WITH (origin = none);  -- Only replicate locally-originated changes
```

- `origin = any` (default): Replicate all changes regardless of origin
- `origin = none`: Only replicate changes without an origin marker

This is the **recommended approach** for bidirectional setups.

### retain_dead_tuples (Conflict Detection Enhancement)

```sql
CREATE SUBSCRIPTION sub_from_node_b
    CONNECTION 'host=node-b dbname=mydb'
    PUBLICATION pub_b
    WITH (
        origin = none,
        retain_dead_tuples = true  -- Keep deleted tuples for conflict detection
    );
```

When enabled:
- Creates dedicated replication slot named `pg_conflict_detection`
- Prevents VACUUM from removing tuples deleted by other origins
- Enables detection of `CT_UPDATE_DELETED` conflicts
- One slot per node (shared across subscriptions)

### max_retention_duration

```sql
ALTER SUBSCRIPTION sub_from_node_b
    SET (max_retention_duration = '1h');  -- Limit dead tuple retention
```

- Default: 0 (retain indefinitely)
- Prevents excessive dead tuple accumulation when apply worker lags
- Automatically resumes retention when conditions allow

## Conflict Statistics Monitoring

PostgreSQL 18 adds per-subscription conflict counts to `pg_stat_subscription_stats`:

```sql
SELECT
    subname,
    confl_insert_exists,
    confl_update_origin_differs,
    confl_update_exists,
    confl_update_deleted,        -- NEW in PG18
    confl_update_missing,
    confl_delete_origin_differs,
    confl_delete_missing,
    confl_multiple_unique_conflicts  -- NEW in PG18
FROM pg_stat_subscription_stats;
```

This enables:
- Real-time monitoring of conflict rates
- Alerting on conflict spikes
- Diagnosing replication topology issues

## Sequence Synchronization (New in PG18)

PostgreSQL 18 adds native sequence replication:

```sql
-- Synchronize all published sequences
ALTER SUBSCRIPTION sub_from_node_b REFRESH PUBLICATION;

-- Explicitly refresh sequences
ALTER SUBSCRIPTION sub_from_node_b REFRESH SEQUENCES;
```

Implementation: `src/backend/replication/logical/sequencesync.c` (745 lines)

Features:
- Dedicated `sequencesync` worker
- Batch operations within transactions
- State tracking: INIT → READY
- Fetches current values and page LSNs from publisher

## Required Configuration

For full bidirectional replication with conflict detection:

```sql
-- postgresql.conf (both nodes)
wal_level = logical
track_commit_timestamp = on  -- Required for origin-based conflict detection
```

```sql
-- Node A setup
CREATE PUBLICATION pub_a FOR ALL TABLES;
CREATE SUBSCRIPTION sub_from_b
    CONNECTION 'host=node-b dbname=mydb user=repl_user'
    PUBLICATION pub_b
    WITH (
        origin = none,
        retain_dead_tuples = true,
        copy_data = false  -- Data already synchronized
    );

-- Node B setup (mirror)
CREATE PUBLICATION pub_b FOR ALL TABLES;
CREATE SUBSCRIPTION sub_from_a
    CONNECTION 'host=node-a dbname=mydb user=repl_user'
    PUBLICATION pub_a
    WITH (
        origin = none,
        retain_dead_tuples = true,
        copy_data = false
    );
```

## What PostgreSQL 18 Does NOT Provide

PostgreSQL 18 **detects and logs** conflicts but does **not automatically resolve** them. By default:
- Conflicts cause the apply worker to error and stop
- Manual intervention required via `ALTER SUBSCRIPTION ... SKIP <lsn>`

**This is where Steep adds value**:

1. **Initial Data Synchronization** - Getting two databases with existing data into sync BEFORE enabling replication
2. **Overlap Analysis** - Identifying conflicts before they cause replication errors
3. **Conflict Resolution Strategies** - prefer-node-a, prefer-node-b, last-modified, manual
4. **Orchestration UX** - Single command to set up bidirectional replication
5. **Monitoring** - Surfacing conflict stats in TUI with alerting

## Steep's Role in Bidirectional Replication

### Pre-Replication Phase (Steep handles)

```
Node A (existing data)          Node B (existing data)
        │                               │
        └───────────┬───────────────────┘
                    │
            ┌───────▼───────┐
            │ steep-repl    │
            │ analyze-overlap│
            └───────┬───────┘
                    │
        ┌───────────▼───────────┐
        │ Overlap Analysis       │
        │ (hash-based via FDW)   │
        │ - Matches: 10,000      │
        │ - Conflicts: 50        │
        │ - A-only: 200          │
        │ - B-only: 150          │
        └───────────┬───────────┘
                    │
        ┌───────────▼───────────┐
        │ Conflict Resolution    │
        │ (prefer-a/b/last-mod)  │
        └───────────┬───────────┘
                    │
        ┌───────────▼───────────┐
        │ Data Reconciliation    │
        │ - Apply A-only to B    │
        │ - Apply B-only to A    │
        │ - Resolve conflicts    │
        └───────────┬───────────┘
                    │
            ┌───────▼───────┐
            │ PostgreSQL 18  │
            │ Bidirectional  │
            │ Replication    │
            │ (origin=none)  │
            └───────────────┘
```

### Post-Replication Phase (PostgreSQL handles)

Once bidirectional replication is enabled:
- PostgreSQL handles all change propagation
- Origin tracking prevents ping-pong
- Conflicts logged to PostgreSQL log
- Statistics available in `pg_stat_subscription_stats`
- Steep monitors and surfaces these stats in TUI

## Steep Extension Enhancements for Bidirectional Merge

The `steep_repl` extension provides high-performance overlap analysis and merge auditing.

### Architecture: Hash-Based Comparison via postgres_fdw

For maximum performance, we use a two-phase approach:

1. **Phase 1: Hash Comparison** - Minimal data transfer
   - Extension computes row hashes on each node (Rust/pgrx for speed)
   - postgres_fdw transfers only PKs and 8-byte hashes
   - PostgreSQL compares hashes using indexes and hash joins

2. **Phase 2: Conflict Resolution** - Only for mismatches
   - Full row data fetched only for conflicts
   - Resolution strategy applied
   - Results logged to audit table

```sql
-- Fast row hashing (Rust/pgrx implementation)
CREATE FUNCTION steep_repl.row_hash(record RECORD) RETURNS BIGINT;

-- Hash-based overlap analysis via postgres_fdw
-- Only PKs and hashes cross the network, not full rows
SELECT
    COALESCE(l.pk, r.pk) as pk_value,
    CASE
        WHEN l.pk IS NULL THEN 'b_only'
        WHEN r.pk IS NULL THEN 'a_only'
        WHEN l.hash != r.hash THEN 'conflict'
        ELSE 'match'
    END as category
FROM (
    SELECT id as pk, steep_repl.row_hash(u.*) as hash
    FROM local_users u
) l
FULL OUTER JOIN (
    SELECT id as pk, steep_repl.row_hash(u.*) as hash
    FROM remote_users u  -- via postgres_fdw
) r USING (pk)
WHERE l.hash IS DISTINCT FROM r.hash;  -- Only return non-matches for efficiency
```

### Extension Functions

```sql
-- High-level overlap analysis (uses hash comparison internally)
SELECT * FROM steep_repl.compare_tables(
    local_table  := 'public.users',
    remote_server := 'node_b_fdw',
    remote_table := 'public.users',
    pk_columns   := ARRAY['id']
);
-- Returns: pk_value JSONB, category TEXT, local_hash BIGINT, remote_hash BIGINT

-- Row hashing for comparison (Rust implementation for speed)
SELECT steep_repl.row_hash(users.*) FROM users;
-- Returns: 8-byte hash of all columns

-- Quiesce writes during merge (advisory locks + connection blocking)
SELECT steep_repl.quiesce_writes(
    table_name := 'users',
    timeout_ms := 30000
);
-- Blocks new writes, waits for active transactions to complete
```

### Merge Audit Log

All merge decisions are logged to `steep_repl.merge_audit_log` (same schema as `steep_repl.audit_log`):

```sql
CREATE TABLE steep_repl.merge_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    merge_id        UUID NOT NULL,           -- Groups all rows from one merge operation
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    pk_value        JSONB NOT NULL,          -- The PK of the affected row
    category        TEXT NOT NULL,           -- 'conflict', 'a_only', 'b_only', 'match'
    resolution      TEXT,                    -- 'kept_a', 'kept_b', 'skipped', NULL for non-conflicts
    node_a_value    JSONB,                   -- Full row from Node A (NULL if b_only)
    node_b_value    JSONB,                   -- Full row from Node B (NULL if a_only)
    resolved_at     TIMESTAMPTZ DEFAULT now(),
    resolved_by     TEXT                     -- 'strategy:prefer-node-a', 'strategy:last-modified', 'manual'
);

CREATE INDEX ON steep_repl.merge_audit_log (merge_id);
CREATE INDEX ON steep_repl.merge_audit_log (table_schema, table_name);
CREATE INDEX ON steep_repl.merge_audit_log (resolved_at);
```

This enables:
- **Compliance auditing** - Every row decision is traceable
- **Post-merge verification** - Can re-query to confirm results
- **Debugging** - Full before/after values for conflicts
- **Rollback planning** - Original values preserved for manual recovery

## Feature Comparison: PG17 vs PG18

| Feature | PG17 | PG18 |
|---------|------|------|
| Origin-based conflict detection | No | Yes |
| `CT_UPDATE_DELETED` detection | No | Yes |
| Detailed conflict logging | No | Yes |
| Conflict statistics monitoring | No | Yes |
| Dead tuple retention for conflicts | No | Yes |
| Sequence synchronization | No | Yes |
| Data retention duration limits | No | Yes |
| Origin filtering (origin=none) | Partial | Full |
| Multiple unique constraint detection | No | Yes |
| Dedicated conflict detection slot | No | Yes |

## References

- PostgreSQL 18 Source: `src/backend/replication/logical/conflict.c`
- PostgreSQL 18 Headers: `src/include/replication/conflict.h`
- PostgreSQL 18 Docs: `doc/src/sgml/logical-replication.sgml`
