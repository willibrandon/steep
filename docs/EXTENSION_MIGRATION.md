# Extension Migration Design Document

## Overview

This document describes the architectural migration of steep-repl daemon services into the PostgreSQL extension, eliminating the need for a separate daemon process for most operations.

### Problem Statement

The current architecture requires:
1. PostgreSQL running with the steep_repl extension
2. A separate steep-repl daemon process running
3. Configuration synchronization between both components
4. Users wondering "is the daemon up?" when PostgreSQL is already running

This creates operational complexity and failure modes where PostgreSQL is healthy but steep-repl features are unavailable because the daemon is down.

### Solution

Move all business logic into the PostgreSQL extension. When PostgreSQL is up, steep_repl is up. The CLI connects directly to PostgreSQL - no intermediary daemon required.

## Prerequisites

### Privilege Requirements (Graduated, Not Blanket Superuser)

steep_repl operations have graduated privilege requirements. **Superuser is NOT required for most operations.**

| Operation | Minimum Privilege Required |
|-----------|---------------------------|
| Schema fingerprinting | SELECT on information_schema |
| Node registration/heartbeat | INSERT/UPDATE on steep_repl.nodes |
| Snapshot export (COPY TO STDOUT) | SELECT on tables being exported |
| Snapshot import (COPY FROM STDIN) | INSERT on tables being imported |
| Replication slot creation | REPLICATION role attribute |
| Replication origin functions | Superuser (or explicit GRANT) |
| dblink/postgres_fdw operations | USAGE on extension + connection perms |
| Background worker operations | Extension in shared_preload_libraries |

**Key insight**: We use `COPY TO STDOUT` / `COPY FROM STDIN`, not `COPY TO '/file'`. The CLI handles file I/O client-side, so server-side file access permissions (pg_read_server_files, pg_write_server_files) are not needed.

### Recommended Role Setup

For most steep_repl operations, create a dedicated role:

```sql
-- Create steep_repl role with REPLICATION attribute
CREATE ROLE steep_repl WITH LOGIN REPLICATION PASSWORD 'secure_password';

-- Grant permissions on steep_repl schema
GRANT USAGE ON SCHEMA steep_repl TO steep_repl;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA steep_repl TO steep_repl;

-- Grant permissions on user tables (for snapshot export/import)
GRANT SELECT ON ALL TABLES IN SCHEMA public TO steep_repl;  -- for export
GRANT INSERT ON ALL TABLES IN SCHEMA public TO steep_repl;  -- for import

-- Grant extension usage for cross-node operations
GRANT USAGE ON FOREIGN DATA WRAPPER postgres_fdw TO steep_repl;
```

Only replication origin functions require superuser by default, but these can be GRANTed to the steep_repl role if needed.

## Architecture

### Current Architecture (Daemon-Based)

```
┌──────────────┐     gRPC      ┌──────────────────┐     SQL     ┌────────────────┐
│  steep-repl  │──────────────▶│  steep-repl      │────────────▶│  PostgreSQL    │
│  CLI         │               │  daemon          │             │  + extension   │
└──────────────┘               └──────────────────┘             └────────────────┘
                                      │
                                      │ IPC/HTTP
                                      ▼
                               ┌──────────────────┐
                               │  Other clients   │
                               └──────────────────┘
```

**Problems:**
- Two processes to manage
- Daemon can be down while PostgreSQL is up
- Extra configuration (YAML files, TLS certs for gRPC)
- Extra network ports (gRPC, HTTP health)

### New Architecture (Extension-Native)

```
┌──────────────┐                                    ┌────────────────────────────┐
│  steep-repl  │         PostgreSQL Protocol        │  PostgreSQL                │
│  CLI         │───────────────────────────────────▶│  + steep_repl extension   │
└──────────────┘                                    │  + background worker       │
       │                                            └────────────────────────────┘
       │                                                         │
       │◀─────────────── LISTEN/NOTIFY ──────────────────────────┘
       │                 (progress updates)
       │
       ▼
┌──────────────┐
│  psql / any  │  (can also call steep_repl.* functions directly)
│  SQL client  │
└──────────────┘
```

**Benefits:**
- Single process (PostgreSQL)
- PostgreSQL up = steep_repl up
- Standard PostgreSQL authentication/authorization
- No extra ports or protocols
- Progress via LISTEN/NOTIFY (built-in pub/sub)

## Component Migration

### Services Moving to Extension

| Daemon Service | Extension Implementation | API |
|---------------|-------------------------|-----|
| Node Registration | SQL function | `steep_repl.register_node(node_id, name, host, port, priority)` |
| Heartbeat | SQL function | `steep_repl.heartbeat(node_id)` |
| Schema Capture | SQL function (exists) | `steep_repl.capture_all_fingerprints(node_id)` |
| Schema Compare | SQL function (exists) | `steep_repl.compare_fingerprints(local_node, peer_node)` |
| Snapshot Generate | Background worker + SQL | `steep_repl.start_snapshot(output_path, compression, parallel)` |
| Snapshot Apply | Background worker + SQL | `steep_repl.start_apply(input_path, parallel, verify)` |
| Snapshot Progress | Shared memory + SQL | `steep_repl.snapshot_progress()` |
| Init Start | SQL function | `steep_repl.start_init(target_node, source_node, method)` |
| Init Progress | SQL function | `steep_repl.init_progress(node_id)` |
| Bidirectional Merge | SQL function + FDW | `steep_repl.start_merge(peer_conn, tables, strategy)` |
| Overlap Analysis | SQL function + FDW | `steep_repl.analyze_overlap(peer_conn, tables)` |

### Services That Cannot Move

| Service | Reason | Mitigation |
|---------|--------|------------|
| gRPC Server | PostgreSQL cannot host arbitrary protocols | CLI connects directly via PostgreSQL protocol |
| HTTP Health | Same as above | Use `pg_isready` or query `steep_repl.health()` |
| IPC Socket | Not exposed by PostgreSQL | Not needed - use PostgreSQL connection |

### Services That Become Unnecessary

| Service | Why Not Needed |
|---------|---------------|
| Service install/uninstall | PostgreSQL handles its own service management |
| TLS certificate management | Use PostgreSQL's native SSL configuration |
| Connection pooling | PostgreSQL handles this (or use pgbouncer) |

## Implementation Details

### 1. Background Worker for Long Operations

Snapshot generation and apply operations can take hours for large databases. These must run as background workers to avoid blocking the calling session.

```rust
// In _PG_init(), register the background worker
#[pg_guard]
pub extern "C-unwind" fn _PG_init() {
    // Version check...

    // Register background worker (only if shared_preload_libraries)
    if unsafe { pgrx::pg_sys::process_shared_preload_libraries_in_progress } {
        BackgroundWorkerBuilder::new("steep_repl_worker")
            .set_function("steep_repl_worker_main")
            .set_library("steep_repl")
            .enable_spi_access()
            .load();
    }
}
```

The background worker:
- Polls a work queue table (`steep_repl.work_queue`)
- Executes snapshot/apply/merge operations
- Updates progress in shared memory
- Sends NOTIFY messages for real-time progress

### 2. Shared Memory for Progress Tracking

Use pgrx shared memory for cross-session progress visibility:

```rust
use pgrx::atomics::*;
use pgrx::lwlock::PgLwLock;
use pgrx::shmem::*;

#[derive(Copy, Clone, Default)]
pub struct SnapshotProgress {
    pub active: bool,
    pub phase: i32,           // 0=idle, 1=schema, 2=data, 3=sequences, 4=finalizing
    pub overall_percent: f32,
    pub tables_completed: i32,
    pub tables_total: i32,
    pub bytes_processed: i64,
    pub current_table: [u8; 128],  // Fixed-size for shared memory
    pub error: [u8; 256],
}

unsafe impl PGRXSharedMemory for SnapshotProgress {}

static SNAPSHOT_PROGRESS: PgLwLock<SnapshotProgress> =
    unsafe { PgLwLock::new(c"steep_repl_snapshot_progress") };
```

### 3. LISTEN/NOTIFY for Real-Time Progress

The background worker sends progress notifications:

```sql
-- Worker sends:
NOTIFY steep_repl_progress, '{"op":"snapshot","percent":45.2,"table":"public.orders"}';

-- CLI listens:
LISTEN steep_repl_progress;
```

The CLI uses asynchronous notification handling in pgx:

```go
// In steep-repl CLI (Go)
conn.Exec(ctx, "LISTEN steep_repl_progress")
for {
    notification, err := conn.WaitForNotification(ctx)
    if err != nil { ... }
    // Parse JSON, update progress display
}
```

### 4. SQL Function API

#### Snapshot Generation

```sql
-- Start a snapshot (returns immediately, work happens in background)
CREATE FUNCTION steep_repl.start_snapshot(
    p_output_path TEXT,
    p_compression TEXT DEFAULT 'none',  -- none, gzip, lz4, zstd
    p_parallel INT DEFAULT 4
) RETURNS steep_repl.snapshots AS $$
DECLARE
    v_snapshot_id TEXT;
    v_result steep_repl.snapshots;
BEGIN
    -- Validate superuser
    IF NOT pg_is_superuser() THEN
        RAISE EXCEPTION 'steep_repl.start_snapshot requires superuser';
    END IF;

    -- Generate snapshot ID
    v_snapshot_id := 'snap_' || to_char(now(), 'YYYYMMDD_HH24MISS') || '_' || substr(md5(random()::text), 1, 8);

    -- Insert work queue entry
    INSERT INTO steep_repl.work_queue (operation, snapshot_id, params)
    VALUES ('snapshot_generate', v_snapshot_id, jsonb_build_object(
        'output_path', p_output_path,
        'compression', p_compression,
        'parallel', p_parallel
    ));

    -- Insert initial snapshot record
    INSERT INTO steep_repl.snapshots (snapshot_id, status, phase, storage_path)
    VALUES (v_snapshot_id, 'pending', 'queued', p_output_path)
    RETURNING * INTO v_result;

    -- Signal background worker
    PERFORM pg_notify('steep_repl_work', v_snapshot_id);

    RETURN v_result;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Check snapshot progress (non-blocking)
CREATE FUNCTION steep_repl.snapshot_progress(p_snapshot_id TEXT DEFAULT NULL)
RETURNS TABLE (
    snapshot_id TEXT,
    phase TEXT,
    overall_percent FLOAT,
    tables_completed INT,
    tables_total INT,
    current_table TEXT,
    bytes_processed BIGINT,
    eta_seconds INT,
    error TEXT
) AS $$
    SELECT
        s.snapshot_id,
        s.phase,
        s.overall_percent,
        s.tables_completed,
        s.table_count,
        s.current_table,
        s.bytes_processed,
        s.eta_seconds,
        s.error_message
    FROM steep_repl.snapshots s
    WHERE (p_snapshot_id IS NULL AND s.status IN ('pending', 'running'))
       OR s.snapshot_id = p_snapshot_id
    ORDER BY s.created_at DESC
    LIMIT 1;
$$ LANGUAGE sql STABLE;

-- Wait for snapshot completion (blocking with timeout)
CREATE FUNCTION steep_repl.wait_snapshot(
    p_snapshot_id TEXT,
    p_timeout_seconds INT DEFAULT 86400  -- 24 hours
) RETURNS steep_repl.snapshots AS $$
DECLARE
    v_start TIMESTAMPTZ := clock_timestamp();
    v_result steep_repl.snapshots;
BEGIN
    LOOP
        SELECT * INTO v_result
        FROM steep_repl.snapshots
        WHERE snapshot_id = p_snapshot_id;

        IF v_result.status IN ('complete', 'failed') THEN
            RETURN v_result;
        END IF;

        IF clock_timestamp() - v_start > (p_timeout_seconds || ' seconds')::interval THEN
            RAISE EXCEPTION 'Timeout waiting for snapshot %', p_snapshot_id;
        END IF;

        -- Sleep 1 second between checks
        PERFORM pg_sleep(1);
    END LOOP;
END;
$$ LANGUAGE plpgsql;
```

#### Node Management

```sql
-- Register or update a node
CREATE FUNCTION steep_repl.register_node(
    p_node_id TEXT,
    p_node_name TEXT,
    p_host TEXT DEFAULT NULL,
    p_port INT DEFAULT 5432,
    p_priority INT DEFAULT 50
) RETURNS steep_repl.nodes AS $$
    INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
    VALUES (p_node_id, p_node_name, p_host, p_port, p_priority, 'active')
    ON CONFLICT (node_id) DO UPDATE SET
        node_name = EXCLUDED.node_name,
        host = COALESCE(EXCLUDED.host, steep_repl.nodes.host),
        port = EXCLUDED.port,
        priority = EXCLUDED.priority,
        last_seen = now()
    RETURNING *;
$$ LANGUAGE sql;

-- Heartbeat (updates last_seen)
CREATE FUNCTION steep_repl.heartbeat(p_node_id TEXT)
RETURNS VOID AS $$
    UPDATE steep_repl.nodes
    SET last_seen = now()
    WHERE node_id = p_node_id;
$$ LANGUAGE sql;

-- Get node status
CREATE FUNCTION steep_repl.node_status(p_node_id TEXT DEFAULT NULL)
RETURNS TABLE (
    node_id TEXT,
    node_name TEXT,
    status TEXT,
    last_seen TIMESTAMPTZ,
    is_healthy BOOLEAN
) AS $$
    SELECT
        n.node_id,
        n.node_name,
        n.status,
        n.last_seen,
        (n.last_seen > now() - interval '30 seconds') as is_healthy
    FROM steep_repl.nodes n
    WHERE p_node_id IS NULL OR n.node_id = p_node_id
    ORDER BY n.priority DESC, n.node_name;
$$ LANGUAGE sql STABLE;
```

#### Bidirectional Merge

```sql
-- Analyze overlap between nodes (dry run)
CREATE FUNCTION steep_repl.analyze_overlap(
    p_peer_connstr TEXT,
    p_tables TEXT[],
    p_primary_keys JSONB DEFAULT NULL  -- {"schema.table": ["pk_col1", "pk_col2"]}
) RETURNS TABLE (
    table_name TEXT,
    local_only_count BIGINT,
    remote_only_count BIGINT,
    match_count BIGINT,
    conflict_count BIGINT
) AS $$
DECLARE
    v_table TEXT;
    v_pk_cols TEXT[];
    v_fdw_server TEXT := 'steep_repl_peer_' || md5(p_peer_connstr);
BEGIN
    -- Create temporary FDW server
    PERFORM steep_repl._create_temp_fdw(v_fdw_server, p_peer_connstr);

    -- Analyze each table
    FOREACH v_table IN ARRAY p_tables LOOP
        -- Get primary key columns
        v_pk_cols := COALESCE(
            (p_primary_keys->>v_table)::TEXT[],
            steep_repl._get_pk_columns(v_table)
        );

        RETURN QUERY
        SELECT
            v_table,
            steep_repl._count_local_only(v_table, v_fdw_server, v_pk_cols),
            steep_repl._count_remote_only(v_table, v_fdw_server, v_pk_cols),
            steep_repl._count_matches(v_table, v_fdw_server, v_pk_cols),
            steep_repl._count_conflicts(v_table, v_fdw_server, v_pk_cols);
    END LOOP;

    -- Cleanup FDW
    PERFORM steep_repl._drop_temp_fdw(v_fdw_server);
END;
$$ LANGUAGE plpgsql;

-- Start merge operation
CREATE FUNCTION steep_repl.start_merge(
    p_peer_connstr TEXT,
    p_tables TEXT[],
    p_strategy TEXT DEFAULT 'prefer-local',  -- prefer-local, prefer-remote, last-modified, manual
    p_dry_run BOOLEAN DEFAULT false
) RETURNS steep_repl.merge_audit_log AS $$
DECLARE
    v_merge_id TEXT;
    v_result steep_repl.merge_audit_log;
BEGIN
    IF NOT pg_is_superuser() THEN
        RAISE EXCEPTION 'steep_repl.start_merge requires superuser';
    END IF;

    v_merge_id := 'merge_' || to_char(now(), 'YYYYMMDD_HH24MISS');

    -- Queue merge operation
    INSERT INTO steep_repl.work_queue (operation, merge_id, params)
    VALUES ('bidirectional_merge', v_merge_id, jsonb_build_object(
        'peer_connstr', p_peer_connstr,
        'tables', p_tables,
        'strategy', p_strategy,
        'dry_run', p_dry_run
    ));

    -- Insert audit log entry
    INSERT INTO steep_repl.merge_audit_log (merge_id, status, tables, strategy)
    VALUES (v_merge_id, 'pending', p_tables, p_strategy)
    RETURNING * INTO v_result;

    PERFORM pg_notify('steep_repl_work', v_merge_id);

    RETURN v_result;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;
```

### 5. Work Queue Table

```sql
CREATE TABLE steep_repl.work_queue (
    id BIGSERIAL PRIMARY KEY,
    operation TEXT NOT NULL,  -- snapshot_generate, snapshot_apply, bidirectional_merge, etc.
    snapshot_id TEXT,
    merge_id TEXT,
    params JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, running, complete, failed
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_message TEXT,

    CONSTRAINT work_queue_operation_check CHECK (
        operation IN ('snapshot_generate', 'snapshot_apply', 'bidirectional_merge', 'init_start', 'reinit')
    )
);

CREATE INDEX work_queue_pending_idx ON steep_repl.work_queue (created_at)
    WHERE status = 'pending';
```

### 6. CLI Changes

The CLI will connect directly to PostgreSQL instead of gRPC:

```go
// Before (gRPC)
client, _ := replgrpc.NewClient(ctx, clientCfg)
stream, _ := client.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{...})

// After (PostgreSQL direct)
conn, _ := pgx.Connect(ctx, connString)

// Start snapshot
var snapshotID string
conn.QueryRow(ctx,
    "SELECT snapshot_id FROM steep_repl.start_snapshot($1, $2, $3)",
    outputPath, compression, parallel,
).Scan(&snapshotID)

// Listen for progress
conn.Exec(ctx, "LISTEN steep_repl_progress")
for {
    notification, _ := conn.WaitForNotification(ctx)
    progress := parseProgress(notification.Payload)
    displayProgress(progress)
    if progress.Complete {
        break
    }
}
```

## Configuration Changes

### Before (Daemon Required)

```yaml
# ~/.config/steep/config.yaml
repl:
  enabled: true
  node_id: "my-node"
  node_name: "My Node"

  postgresql:
    host: localhost
    port: 5432
    database: mydb
    user: postgres

  grpc:
    port: 15460
    tls:
      cert_file: /path/to/cert.pem
      key_file: /path/to/key.pem

  ipc:
    enabled: true
    path: /tmp/steep-repl.sock
```

### After (Extension-Native)

```yaml
# ~/.config/steep/config.yaml (simplified)
connections:
  default:
    host: localhost
    port: 5432
    database: mydb
    user: postgres
    # Uses standard PostgreSQL SSL settings
    sslmode: prefer

  production:
    host: prod-db.example.com
    port: 5432
    database: proddb
    user: steep_admin
    sslmode: require
```

### PostgreSQL Configuration

```ini
# postgresql.conf
shared_preload_libraries = 'steep_repl'  # Required for background worker

# Optional: tune for large snapshots
max_worker_processes = 16
```

## Migration Path

### Phase 1: Parallel Operation (v0.x)

Both daemon and extension work simultaneously. Users can choose either:
- Daemon mode (existing): `steep-repl snapshot generate --remote localhost:15460`
- Extension mode (new): `steep-repl snapshot generate --direct` (connects to PostgreSQL)

### Phase 2: Extension Default (v1.0)

- Extension mode becomes default
- Daemon mode deprecated but still functional
- CLI auto-detects: if `--remote` specified, use gRPC; otherwise, use PostgreSQL

### Phase 3: Daemon Removal (v2.0)

- Daemon code removed
- CLI only supports direct PostgreSQL connection
- gRPC/IPC code removed from codebase

## Compatibility Matrix

| Feature | Daemon Mode | Extension Mode | Notes |
|---------|-------------|----------------|-------|
| Snapshot generate | Yes | Yes | Extension uses background worker |
| Snapshot apply | Yes | Yes | Extension uses background worker |
| Schema compare | Yes | Yes | Both use dblink |
| Bidirectional merge | Yes | Yes | Both use postgres_fdw |
| Progress tracking | gRPC stream | LISTEN/NOTIFY | Different protocols, same info |
| Remote CLI access | Yes (gRPC) | No* | *Can use SSH tunnel + psql |
| HTTP health check | Yes | No* | *Use pg_isready |
| Service management | Yes | No | Use systemd/launchd for PostgreSQL |

## Security Considerations

1. **Superuser Requirement**: All steep_repl operations require superuser. This is enforced in SQL functions with `SECURITY DEFINER` where appropriate.

2. **Connection Strings**: Peer connection strings for merge operations may contain credentials. These are:
   - Not logged (use `SET log_statement = 'none'` in functions)
   - Not stored in tables (only in work_queue.params, which should have restricted access)

3. **File System Access**: Snapshot paths are validated to prevent directory traversal attacks.

4. **FDW Servers**: Temporary FDW servers are created with restricted permissions and dropped after use.

## Testing Strategy

1. **Unit Tests**: pgrx `#[pg_test]` for all SQL functions
2. **Integration Tests**: testcontainers with two PostgreSQL instances
3. **Migration Tests**: Verify daemon and extension modes produce identical results
4. **Performance Tests**: Compare throughput between daemon and extension modes

## Appendix: pgrx Background Worker Implementation

```rust
// src/worker.rs
use pgrx::bgworkers::*;
use pgrx::prelude::*;
use std::time::Duration;

#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn steep_repl_worker_main(_arg: pg_sys::Datum) {
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("postgres"), None);

    log!("steep_repl background worker started");

    while BackgroundWorker::wait_latch(Some(Duration::from_secs(1))) {
        if BackgroundWorker::sighup_received() {
            // Reload configuration if needed
        }

        // Check for pending work
        let result: Result<(), pgrx::spi::Error> = BackgroundWorker::transaction(|| {
            Spi::connect(|client| {
                // Fetch and lock pending work item
                let work = client.select(
                    "SELECT id, operation, params FROM steep_repl.work_queue
                     WHERE status = 'pending'
                     ORDER BY created_at
                     LIMIT 1
                     FOR UPDATE SKIP LOCKED",
                    None,
                    &[],
                )?;

                if let Some(row) = work.first() {
                    let id: i64 = row.get(1)?.unwrap();
                    let operation: &str = row.get(2)?.unwrap();
                    let params: pgrx::JsonB = row.get(3)?.unwrap();

                    // Mark as running
                    client.update(
                        "UPDATE steep_repl.work_queue SET status = 'running', started_at = now() WHERE id = $1",
                        None,
                        &[id.into()],
                    )?;

                    // Execute operation
                    match operation {
                        "snapshot_generate" => execute_snapshot_generate(client, params),
                        "snapshot_apply" => execute_snapshot_apply(client, params),
                        "bidirectional_merge" => execute_merge(client, params),
                        _ => Err(spi::Error::new(format!("Unknown operation: {}", operation))),
                    }?;

                    // Mark complete
                    client.update(
                        "UPDATE steep_repl.work_queue SET status = 'complete', completed_at = now() WHERE id = $1",
                        None,
                        &[id.into()],
                    )?;
                }

                Ok(())
            })
        });

        if let Err(e) = result {
            log!("steep_repl worker error: {}", e);
        }
    }

    log!("steep_repl background worker shutting down");
}
```

## Conclusion

This migration simplifies the steep architecture by leveraging PostgreSQL's native capabilities:
- Background workers replace the daemon's long-running operations
- LISTEN/NOTIFY replaces gRPC streaming for progress
- SQL functions replace gRPC RPCs
- PostgreSQL authentication replaces custom TLS configuration

The result is a simpler, more reliable system where "PostgreSQL up = steep_repl up".
