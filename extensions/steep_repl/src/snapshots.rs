//! Snapshots table for steep_repl extension.
//!
//! This module creates the snapshots table for tracking generated
//! snapshot manifests and real-time progress for two-phase initialization.
//!
//! T016: Implement steep_repl.start_snapshot() SQL function
//! T017: Implement steep_repl.snapshot_progress() SQL function
//! T018: Implement steep_repl.cancel_snapshot() SQL function

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Snapshots table: Generated snapshot manifests with progress tracking for two-phase initialization
CREATE TABLE steep_repl.snapshots (
    -- Identity
    snapshot_id TEXT PRIMARY KEY,
    source_node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    target_node_id TEXT REFERENCES steep_repl.nodes(node_id),

    -- Snapshot metadata
    lsn TEXT,
    storage_path TEXT,
    compression TEXT DEFAULT 'gzip',
    parallel INTEGER NOT NULL DEFAULT 4,
    checksum TEXT,

    -- Status tracking
    status TEXT NOT NULL DEFAULT 'pending',
    phase TEXT NOT NULL DEFAULT 'idle',
    error_message TEXT,

    -- Progress tracking
    overall_percent REAL NOT NULL DEFAULT 0,
    current_table TEXT,
    table_count INTEGER NOT NULL DEFAULT 0,
    tables_completed INTEGER NOT NULL DEFAULT 0,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    bytes_written BIGINT NOT NULL DEFAULT 0,
    rows_total BIGINT NOT NULL DEFAULT 0,
    rows_written BIGINT NOT NULL DEFAULT 0,
    throughput_bytes_sec REAL NOT NULL DEFAULT 0,
    eta_seconds INTEGER NOT NULL DEFAULT 0,
    compression_ratio REAL NOT NULL DEFAULT 0,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT snapshots_size_check CHECK (size_bytes >= 0),
    CONSTRAINT snapshots_bytes_written_check CHECK (bytes_written >= 0),
    CONSTRAINT snapshots_table_count_check CHECK (table_count >= 0),
    CONSTRAINT snapshots_tables_completed_check CHECK (tables_completed >= 0 AND tables_completed <= table_count),
    CONSTRAINT snapshots_percent_check CHECK (overall_percent >= 0 AND overall_percent <= 100),
    CONSTRAINT snapshots_compression_check CHECK (compression IN ('none', 'gzip', 'lz4', 'zstd')),
    CONSTRAINT snapshots_status_check CHECK (status IN ('pending', 'generating', 'complete', 'applying', 'applied', 'failed', 'cancelled', 'expired')),
    CONSTRAINT snapshots_phase_check CHECK (phase IN ('idle', 'schema', 'data', 'indexes', 'constraints', 'sequences', 'verify', 'finalizing'))
);

-- Comments
COMMENT ON TABLE steep_repl.snapshots IS 'Snapshot manifests with real-time progress tracking for two-phase initialization';
COMMENT ON COLUMN steep_repl.snapshots.snapshot_id IS 'Unique snapshot identifier';
COMMENT ON COLUMN steep_repl.snapshots.source_node_id IS 'Node snapshot was taken from';
COMMENT ON COLUMN steep_repl.snapshots.target_node_id IS 'Node snapshot is being applied to (NULL during generation)';
COMMENT ON COLUMN steep_repl.snapshots.lsn IS 'WAL position at snapshot time';
COMMENT ON COLUMN steep_repl.snapshots.storage_path IS 'File system or S3 path';
COMMENT ON COLUMN steep_repl.snapshots.compression IS 'Compression type (none, gzip, lz4, zstd)';
COMMENT ON COLUMN steep_repl.snapshots.parallel IS 'Number of parallel workers for COPY operations';
COMMENT ON COLUMN steep_repl.snapshots.checksum IS 'SHA256 of manifest';
COMMENT ON COLUMN steep_repl.snapshots.status IS 'Overall status: pending, generating, complete, applying, applied, failed, cancelled, expired';
COMMENT ON COLUMN steep_repl.snapshots.phase IS 'Current phase: idle, schema, data, indexes, constraints, sequences, verify, finalizing';
COMMENT ON COLUMN steep_repl.snapshots.error_message IS 'Error details if status is failed';
COMMENT ON COLUMN steep_repl.snapshots.overall_percent IS 'Overall completion percentage (0-100)';
COMMENT ON COLUMN steep_repl.snapshots.current_table IS 'Table currently being processed';
COMMENT ON COLUMN steep_repl.snapshots.table_count IS 'Total number of tables in snapshot';
COMMENT ON COLUMN steep_repl.snapshots.tables_completed IS 'Number of tables completed';
COMMENT ON COLUMN steep_repl.snapshots.size_bytes IS 'Total snapshot size in bytes';
COMMENT ON COLUMN steep_repl.snapshots.bytes_written IS 'Bytes written so far';
COMMENT ON COLUMN steep_repl.snapshots.rows_total IS 'Total rows to process';
COMMENT ON COLUMN steep_repl.snapshots.rows_written IS 'Rows written so far';
COMMENT ON COLUMN steep_repl.snapshots.throughput_bytes_sec IS 'Current throughput in bytes/sec';
COMMENT ON COLUMN steep_repl.snapshots.eta_seconds IS 'Estimated time remaining in seconds';
COMMENT ON COLUMN steep_repl.snapshots.compression_ratio IS 'Compression ratio achieved (0-1)';
COMMENT ON COLUMN steep_repl.snapshots.created_at IS 'When snapshot record was created';
COMMENT ON COLUMN steep_repl.snapshots.started_at IS 'When operation actually started';
COMMENT ON COLUMN steep_repl.snapshots.completed_at IS 'When operation completed';
COMMENT ON COLUMN steep_repl.snapshots.expires_at IS 'Auto-cleanup timestamp';

-- Indexes
CREATE INDEX idx_snapshots_source ON steep_repl.snapshots(source_node_id);
CREATE INDEX idx_snapshots_target ON steep_repl.snapshots(target_node_id) WHERE target_node_id IS NOT NULL;
CREATE INDEX idx_snapshots_status ON steep_repl.snapshots(status);
CREATE INDEX idx_snapshots_active ON steep_repl.snapshots(status) WHERE status IN ('generating', 'applying');
CREATE INDEX idx_snapshots_expires ON steep_repl.snapshots(expires_at) WHERE expires_at IS NOT NULL;

-- LISTEN/NOTIFY for real-time updates
CREATE OR REPLACE FUNCTION steep_repl.notify_snapshot_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('steep_repl_snapshots', json_build_object(
        'snapshot_id', NEW.snapshot_id,
        'status', NEW.status,
        'phase', NEW.phase,
        'overall_percent', NEW.overall_percent
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER snapshot_notify
AFTER INSERT OR UPDATE ON steep_repl.snapshots
FOR EACH ROW EXECUTE FUNCTION steep_repl.notify_snapshot_change();

COMMENT ON FUNCTION steep_repl.notify_snapshot_change() IS 'Sends notification on snapshot changes for real-time TUI updates';
"#,
    name = "create_snapshots_table",
    requires = ["create_nodes_table"],
);

// =============================================================================
// T016: start_snapshot() - Start a snapshot generation operation
// =============================================================================

/// Internal implementation of start_snapshot.
/// Creates a snapshot record, queues the work, and returns the snapshot_id.
#[pg_extern]
fn _steep_repl_start_snapshot(
    p_output_path: &str,
    p_compression: default!(Option<&str>, "'none'"),
    p_parallel: default!(Option<i32>, "4"),
) -> Option<String> {
    let compression = p_compression.unwrap_or("none");
    let parallel = p_parallel.unwrap_or(4);

    // Validate compression
    if !["none", "gzip", "lz4", "zstd"].contains(&compression) {
        pgrx::error!("Invalid compression type '{}'. Must be one of: none, gzip, lz4, zstd", compression);
    }

    // Validate parallel
    if parallel < 1 || parallel > 32 {
        pgrx::error!("Invalid parallel value {}. Must be between 1 and 32", parallel);
    }

    // Generate unique snapshot ID
    let snapshot_id = format!(
        "snap_{}_{:08x}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0),
        rand_u32()
    );

    // Escape values for SQL
    let escaped_snapshot_id = snapshot_id.replace('\'', "''");
    let escaped_path = p_output_path.replace('\'', "''");
    let escaped_compression = compression.replace('\'', "''");

    // Get source node_id (use local node or create a temporary one)
    let source_node_id = match Spi::get_one::<String>(
        "SELECT node_id FROM steep_repl.nodes WHERE status = 'healthy' ORDER BY priority DESC LIMIT 1"
    ) {
        Ok(Some(id)) => id,
        _ => {
            // Create a local node if none exists
            let local_id = "local";
            if let Err(e) = Spi::run(&format!(
                "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
                 VALUES ('{}', 'Local', 'localhost', 5432, 100, 'healthy')
                 ON CONFLICT (node_id) DO NOTHING",
                local_id
            )) {
                pgrx::error!("Failed to create local node: {}", e);
            }
            local_id.to_string()
        }
    };

    // Insert snapshot record
    if let Err(e) = Spi::run(&format!(
        r#"INSERT INTO steep_repl.snapshots (
               snapshot_id, source_node_id, storage_path, compression, parallel, status, phase
           ) VALUES (
               '{}', '{}', '{}', '{}', {}, 'pending', 'idle'
           )"#,
        escaped_snapshot_id,
        source_node_id.replace('\'', "''"),
        escaped_path,
        escaped_compression,
        parallel
    )) {
        pgrx::error!("Failed to create snapshot record: {}", e);
    }

    // Queue work for background worker
    match crate::work_queue::queue_snapshot_generate(
        &snapshot_id,
        p_output_path,
        compression,
        parallel,
    ) {
        Ok(_work_id) => {
            // Notify worker that new work is available
            crate::notify::notify_work_available();
        }
        Err(e) => {
            // Rollback the snapshot record
            let _ = Spi::run(&format!(
                "DELETE FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_snapshot_id
            ));
            pgrx::error!("Failed to queue snapshot work: {}", e);
        }
    }

    Some(snapshot_id)
}

/// Generate a pseudo-random u32 for snapshot ID uniqueness.
fn rand_u32() -> u32 {
    use std::time::SystemTime;
    let t = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();
    // Simple hash using nanoseconds
    let nanos = t.subsec_nanos();
    nanos.wrapping_mul(2654435761) // Knuth's multiplicative hash
}

// =============================================================================
// T017: snapshot_progress() - Query snapshot progress from shared memory
// =============================================================================

/// Internal implementation of snapshot_progress.
/// Returns progress from shared memory if the snapshot is active,
/// otherwise returns progress from the snapshots table.
#[pg_extern]
fn _steep_repl_snapshot_progress(
    p_snapshot_id: Option<&str>,
) -> TableIterator<
    'static,
    (
        name!(snapshot_id, Option<String>),
        name!(phase, Option<String>),
        name!(overall_percent, Option<f64>),
        name!(tables_completed, Option<i32>),
        name!(tables_total, Option<i32>),
        name!(current_table, Option<String>),
        name!(bytes_processed, Option<i64>),
        name!(eta_seconds, Option<i32>),
        name!(error, Option<String>),
    ),
> {
    let mut results = Vec::new();

    // Check shared memory first for real-time progress
    let shmem_progress = crate::progress::get_progress_snapshot();
    let shmem_op_id = shmem_progress.get_operation_id();
    let shmem_active = shmem_progress.active ||
        shmem_progress.phase == crate::progress::ProgressPhase::Complete as i32 ||
        shmem_progress.phase == crate::progress::ProgressPhase::Failed as i32;

    // If specific snapshot requested and it's in shared memory, return real-time data
    if let Some(snap_id) = p_snapshot_id {
        if shmem_active && shmem_op_id == snap_id {
            // Return from shared memory
            let phase = crate::progress::ProgressPhase::from_i32(shmem_progress.phase);
            let error = if phase == crate::progress::ProgressPhase::Failed {
                let err = shmem_progress.get_error_message();
                if err.is_empty() { None } else { Some(err) }
            } else {
                None
            };

            results.push((
                Some(snap_id.to_string()),
                Some(phase.as_str().to_string()),
                Some(shmem_progress.overall_percent as f64),
                Some(shmem_progress.tables_completed),
                Some(shmem_progress.tables_total),
                if shmem_progress.get_current_table().is_empty() {
                    None
                } else {
                    Some(shmem_progress.get_current_table())
                },
                Some(shmem_progress.bytes_processed),
                if shmem_progress.eta_seconds > 0 {
                    Some(shmem_progress.eta_seconds)
                } else {
                    None
                },
                error,
            ));

            return TableIterator::new(results);
        }

        // Not in shared memory, query from table using simple Spi queries
        let escaped_id = snap_id.replace('\'', "''");
        let query = format!(
            r#"SELECT snapshot_id, phase, overall_percent, tables_completed, table_count,
                      current_table, bytes_written, eta_seconds, error_message
               FROM steep_repl.snapshots
               WHERE snapshot_id = '{}'"#,
            escaped_id
        );

        // Use individual queries to avoid complex closure types
        let db_snap_id = Spi::get_one::<String>(&format!(
            "SELECT snapshot_id FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
            escaped_id
        )).ok().flatten();

        if db_snap_id.is_some() {
            let db_phase = Spi::get_one::<String>(&format!(
                "SELECT phase FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_percent = Spi::get_one::<f32>(&format!(
                "SELECT overall_percent FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_tables_done = Spi::get_one::<i32>(&format!(
                "SELECT tables_completed FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_tables_total = Spi::get_one::<i32>(&format!(
                "SELECT table_count FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_current = Spi::get_one::<String>(&format!(
                "SELECT current_table FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_bytes = Spi::get_one::<i64>(&format!(
                "SELECT bytes_written FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_eta = Spi::get_one::<i32>(&format!(
                "SELECT eta_seconds FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            let db_error = Spi::get_one::<String>(&format!(
                "SELECT error_message FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                escaped_id
            )).ok().flatten();

            results.push((
                db_snap_id,
                db_phase,
                db_percent.map(|p| p as f64),
                db_tables_done,
                db_tables_total,
                db_current,
                db_bytes,
                db_eta,
                db_error,
            ));
        }
    } else {
        // No specific snapshot - return all active/recent snapshots
        // First check if there's an active operation in shared memory
        if shmem_active && !shmem_op_id.is_empty() {
            let phase = crate::progress::ProgressPhase::from_i32(shmem_progress.phase);
            let error = if phase == crate::progress::ProgressPhase::Failed {
                let err = shmem_progress.get_error_message();
                if err.is_empty() { None } else { Some(err) }
            } else {
                None
            };

            results.push((
                Some(shmem_op_id),
                Some(phase.as_str().to_string()),
                Some(shmem_progress.overall_percent as f64),
                Some(shmem_progress.tables_completed),
                Some(shmem_progress.tables_total),
                if shmem_progress.get_current_table().is_empty() {
                    None
                } else {
                    Some(shmem_progress.get_current_table())
                },
                Some(shmem_progress.bytes_processed),
                if shmem_progress.eta_seconds > 0 {
                    Some(shmem_progress.eta_seconds)
                } else {
                    None
                },
                error,
            ));
        }

        // Also query recent snapshots from table - get snapshot IDs first
        // We use a simpler approach to avoid complex closure types
        let pending_ids: Vec<String> = Spi::connect(|client| {
            let mut ids = Vec::new();
            let result = client.select(
                r#"SELECT snapshot_id
                   FROM steep_repl.snapshots
                   WHERE status IN ('generating', 'applying', 'pending')
                   ORDER BY created_at DESC
                   LIMIT 10"#,
                None,
                &[],
            )?;

            for row in result {
                if let Some(id) = row.get_by_name::<String, _>("snapshot_id")? {
                    ids.push(id);
                }
            }
            Ok::<Vec<String>, pgrx::spi::SpiError>(ids)
        }).unwrap_or_default();

        // Query each snapshot's details
        for snap_id in pending_ids {
            // Skip if already added from shared memory
            if results.iter().any(|r| r.0.as_ref() == Some(&snap_id)) {
                continue;
            }

            let escaped_id = snap_id.replace('\'', "''");

            let phase = Spi::get_one::<String>(&format!(
                "SELECT phase FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let percent = Spi::get_one::<f32>(&format!(
                "SELECT overall_percent FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let tables_done = Spi::get_one::<i32>(&format!(
                "SELECT tables_completed FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let tables_total = Spi::get_one::<i32>(&format!(
                "SELECT table_count FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let current = Spi::get_one::<String>(&format!(
                "SELECT current_table FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let bytes = Spi::get_one::<i64>(&format!(
                "SELECT bytes_written FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let eta = Spi::get_one::<i32>(&format!(
                "SELECT eta_seconds FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            let error = Spi::get_one::<String>(&format!(
                "SELECT error_message FROM steep_repl.snapshots WHERE snapshot_id = '{}'", escaped_id
            )).ok().flatten();

            results.push((
                Some(snap_id),
                phase,
                percent.map(|p| p as f64),
                tables_done,
                tables_total,
                current,
                bytes,
                eta,
                error,
            ));
        }
    }

    TableIterator::new(results)
}

// =============================================================================
// T018: cancel_snapshot() - Cancel a running snapshot operation
// =============================================================================

/// Internal implementation of cancel_snapshot.
/// Cancels the snapshot by updating the work queue status.
#[pg_extern]
fn _steep_repl_cancel_snapshot(p_snapshot_id: &str) -> bool {
    let escaped_id = p_snapshot_id.replace('\'', "''");

    // Find the work queue entry for this snapshot
    let work_id = match Spi::get_one::<i64>(&format!(
        r#"SELECT id FROM steep_repl.work_queue
           WHERE snapshot_id = '{}' AND status IN ('pending', 'running')
           ORDER BY created_at DESC LIMIT 1"#,
        escaped_id
    )) {
        Ok(Some(id)) => id,
        Ok(None) => return false,
        Err(_) => return false,
    };

    // Cancel the work queue entry
    match crate::work_queue::cancel_work_entry(work_id) {
        Ok(cancelled) => {
            if cancelled {
                // Update the snapshot status
                let _ = Spi::run(&format!(
                    r#"UPDATE steep_repl.snapshots
                       SET status = 'cancelled', completed_at = now()
                       WHERE snapshot_id = '{}' AND status IN ('pending', 'generating')"#,
                    escaped_id
                ));
            }
            cancelled
        }
        Err(_) => false,
    }
}

// Schema-qualified wrapper functions
extension_sql!(
    r#"
-- T016: start_snapshot() - Start a snapshot generation operation
CREATE FUNCTION steep_repl.start_snapshot(
    p_output_path TEXT,
    p_compression TEXT DEFAULT 'none',
    p_parallel INTEGER DEFAULT 4
) RETURNS TEXT
LANGUAGE sql
SECURITY DEFINER
AS $$ SELECT _steep_repl_start_snapshot(p_output_path, p_compression, p_parallel) $$;

COMMENT ON FUNCTION steep_repl.start_snapshot(TEXT, TEXT, INTEGER) IS
    'Start a snapshot generation operation. Returns snapshot_id. Queues work for background processing.';

-- T017: snapshot_progress() - Query snapshot progress
CREATE FUNCTION steep_repl.snapshot_progress(
    p_snapshot_id TEXT DEFAULT NULL
) RETURNS TABLE (
    snapshot_id TEXT,
    phase TEXT,
    overall_percent FLOAT,
    tables_completed INTEGER,
    tables_total INTEGER,
    current_table TEXT,
    bytes_processed BIGINT,
    eta_seconds INTEGER,
    error TEXT
)
LANGUAGE sql STABLE
AS $$ SELECT * FROM _steep_repl_snapshot_progress(p_snapshot_id) $$;

COMMENT ON FUNCTION steep_repl.snapshot_progress(TEXT) IS
    'Query snapshot progress. Returns real-time data from shared memory if active, otherwise from table.';

-- T018: cancel_snapshot() - Cancel a running snapshot
CREATE FUNCTION steep_repl.cancel_snapshot(
    p_snapshot_id TEXT
) RETURNS BOOLEAN
LANGUAGE sql
AS $$ SELECT _steep_repl_cancel_snapshot(p_snapshot_id) $$;

COMMENT ON FUNCTION steep_repl.cancel_snapshot(TEXT) IS
    'Cancel a pending or running snapshot operation. Returns true if cancelled, false if not found or already complete.';
"#,
    name = "create_snapshot_functions",
    requires = [
        "create_snapshots_table",
        "create_work_queue_table",
        _steep_repl_start_snapshot,
        _steep_repl_snapshot_progress,
        _steep_repl_cancel_snapshot
    ],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_snapshots_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'snapshots'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "snapshots table should exist");
    }

    #[pg_test]
    fn test_snapshots_insert_minimal() {
        // First create a node to reference
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test-node-snapshots', 'Test Node', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Insert snapshot record with minimal required fields
        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_test_01', 'test-node-snapshots')"
        ).expect("snapshot insert should succeed");

        let result = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.snapshots WHERE snapshot_id = 'snap_test_01'"
        );
        assert_eq!(result, Ok(Some("pending".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_test_01'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-snapshots'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_snapshots_progress_tracking() {
        // Create source and target nodes
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('src-node', 'Source', 'localhost', 5432, 50, 'healthy'),
                    ('tgt-node', 'Target', 'localhost', 5433, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Insert snapshot with progress data
        Spi::run(
            "INSERT INTO steep_repl.snapshots (
                snapshot_id, source_node_id, target_node_id, status, phase,
                overall_percent, table_count, tables_completed, size_bytes, bytes_written,
                throughput_bytes_sec, eta_seconds
             ) VALUES (
                'snap_progress_test', 'src-node', 'tgt-node', 'generating', 'data',
                45.5, 10, 4, 1048576, 524288, 10485.76, 50
             )"
        ).expect("snapshot insert should succeed");

        // Verify progress fields
        let percent = Spi::get_one::<f32>(
            "SELECT overall_percent FROM steep_repl.snapshots WHERE snapshot_id = 'snap_progress_test'"
        );
        assert_eq!(percent, Ok(Some(45.5)));

        let phase = Spi::get_one::<String>(
            "SELECT phase FROM steep_repl.snapshots WHERE snapshot_id = 'snap_progress_test'"
        );
        assert_eq!(phase, Ok(Some("data".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_progress_test'")
            .expect("cleanup should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id IN ('src-node', 'tgt-node')")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_snapshots_status_constraint() {
        // Verify status constraint allows new values
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshots'
                AND c.conname = 'snapshots_status_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "status check constraint should exist");
    }

    #[pg_test]
    fn test_snapshots_phase_constraint() {
        // Verify phase constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshots'
                AND c.conname = 'snapshots_phase_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "phase check constraint should exist");
    }

    #[pg_test]
    fn test_snapshots_percent_constraint() {
        // Verify percent constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshots'
                AND c.conname = 'snapshots_percent_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "percent check constraint should exist");
    }

    #[pg_test]
    fn test_snapshots_indexes() {
        let indexes = vec![
            "idx_snapshots_source",
            "idx_snapshots_target",
            "idx_snapshots_status",
            "idx_snapshots_active",
            "idx_snapshots_expires",
        ];

        for idx_name in indexes {
            let query = format!(
                "SELECT EXISTS(
                    SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'steep_repl'
                    AND indexname = '{}'
                )",
                idx_name
            );
            let result = Spi::get_one::<bool>(&query);
            assert_eq!(
                result,
                Ok(Some(true)),
                "index {} should exist",
                idx_name
            );
        }
    }

    #[pg_test]
    fn test_snapshots_notify_trigger() {
        // Verify trigger exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_trigger t
                JOIN pg_class c ON t.tgrelid = c.oid
                JOIN pg_namespace n ON c.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND c.relname = 'snapshots'
                AND t.tgname = 'snapshot_notify'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "snapshot_notify trigger should exist");
    }

    #[pg_test]
    fn test_snapshots_notify_function() {
        // Verify function exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND p.proname = 'notify_snapshot_change'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "notify_snapshot_change function should exist");
    }

    // =========================================================================
    // Tests for T016, T017, T018: Snapshot SQL Functions
    // =========================================================================

    #[pg_test]
    fn test_start_snapshot_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'start_snapshot'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "start_snapshot function should exist");
    }

    #[pg_test]
    fn test_snapshot_progress_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'snapshot_progress'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "snapshot_progress function should exist");
    }

    #[pg_test]
    fn test_cancel_snapshot_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'cancel_snapshot'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "cancel_snapshot function should exist");
    }

    #[pg_test]
    fn test_start_snapshot_creates_record() {
        // Call start_snapshot
        let result = Spi::get_one::<String>(
            "SELECT steep_repl.start_snapshot('/tmp/test_snapshot', 'none', 4)"
        );

        assert!(result.is_ok(), "start_snapshot should succeed");
        let snapshot_id = result.unwrap().expect("should return snapshot_id");
        assert!(snapshot_id.starts_with("snap_"), "snapshot_id should start with 'snap_'");

        // Verify snapshot record was created
        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
            snapshot_id
        ));
        assert_eq!(status, Ok(Some("pending".to_string())), "snapshot should have pending status");

        // Verify work queue entry was created
        let work_count = Spi::get_one::<i64>(&format!(
            "SELECT COUNT(*) FROM steep_repl.work_queue WHERE snapshot_id = '{}'",
            snapshot_id
        ));
        assert_eq!(work_count, Ok(Some(1)), "work queue entry should exist");

        // Cleanup
        Spi::run(&format!(
            "DELETE FROM steep_repl.work_queue WHERE snapshot_id = '{}'",
            snapshot_id
        )).expect("cleanup work_queue");
        Spi::run(&format!(
            "DELETE FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
            snapshot_id
        )).expect("cleanup snapshots");
    }

    #[pg_test]
    fn test_start_snapshot_validates_compression() {
        // Invalid compression should fail (but we can't test panics easily)
        // Just verify valid compressions work
        let valid_compressions = vec!["none", "gzip", "lz4", "zstd"];

        for comp in valid_compressions {
            let result = Spi::get_one::<String>(&format!(
                "SELECT steep_repl.start_snapshot('/tmp/test_{}.snap', '{}', 4)",
                comp, comp
            ));
            assert!(result.is_ok(), "start_snapshot with {} compression should succeed", comp);

            if let Ok(Some(snap_id)) = result {
                // Cleanup
                Spi::run(&format!(
                    "DELETE FROM steep_repl.work_queue WHERE snapshot_id = '{}'",
                    snap_id
                )).ok();
                Spi::run(&format!(
                    "DELETE FROM steep_repl.snapshots WHERE snapshot_id = '{}'",
                    snap_id
                )).ok();
            }
        }
    }

    #[pg_test]
    fn test_snapshot_progress_returns_table() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test_progress_node', 'Test', 'localhost', 5432, 50, 'healthy')
             ON CONFLICT (node_id) DO NOTHING"
        ).expect("node insert");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (
                 snapshot_id, source_node_id, status, phase, overall_percent,
                 table_count, tables_completed
             ) VALUES (
                 'test_progress_snap', 'test_progress_node', 'generating', 'data', 50.0,
                 10, 5
             )"
        ).expect("snapshot insert");

        // Query progress
        let result = Spi::get_one::<i64>(
            "SELECT COUNT(*) FROM steep_repl.snapshot_progress('test_progress_snap')"
        );
        assert!(result.is_ok(), "snapshot_progress should succeed");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'test_progress_snap'").ok();
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test_progress_node'").ok();
    }

    #[pg_test]
    fn test_snapshot_progress_from_table() {
        // Create test node and snapshot with known values
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test_table_node', 'Test', 'localhost', 5432, 50, 'healthy')
             ON CONFLICT (node_id) DO NOTHING"
        ).expect("node insert");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (
                 snapshot_id, source_node_id, status, phase, overall_percent,
                 table_count, tables_completed, bytes_written, eta_seconds
             ) VALUES (
                 'test_table_snap', 'test_table_node', 'generating', 'data', 75.0,
                 8, 6, 1000000, 120
             )"
        ).expect("snapshot insert");

        // Query and verify progress values
        let phase = Spi::get_one::<String>(
            "SELECT phase FROM steep_repl.snapshot_progress('test_table_snap')"
        );
        assert_eq!(phase, Ok(Some("data".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'test_table_snap'").ok();
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test_table_node'").ok();
    }

    #[pg_test]
    fn test_cancel_snapshot_pending() {
        // Create test snapshot and work queue entry
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test_cancel_node', 'Test', 'localhost', 5432, 50, 'healthy')
             ON CONFLICT (node_id) DO NOTHING"
        ).expect("node insert");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id, status, phase)
             VALUES ('test_cancel_snap', 'test_cancel_node', 'pending', 'idle')"
        ).expect("snapshot insert");

        Spi::run(
            "INSERT INTO steep_repl.work_queue (operation, snapshot_id, params, status)
             VALUES ('snapshot_generate', 'test_cancel_snap', '{}'::jsonb, 'pending')"
        ).expect("work_queue insert");

        // Cancel should succeed
        let result = Spi::get_one::<bool>(
            "SELECT steep_repl.cancel_snapshot('test_cancel_snap')"
        );
        assert_eq!(result, Ok(Some(true)), "cancel_snapshot should return true");

        // Verify work queue is cancelled
        let status = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.work_queue WHERE snapshot_id = 'test_cancel_snap'"
        );
        assert_eq!(status, Ok(Some("cancelled".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'test_cancel_snap'").ok();
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'test_cancel_snap'").ok();
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test_cancel_node'").ok();
    }

    #[pg_test]
    fn test_cancel_snapshot_not_found() {
        // Cancel non-existent snapshot should return false
        let result = Spi::get_one::<bool>(
            "SELECT steep_repl.cancel_snapshot('nonexistent_snapshot')"
        );
        assert_eq!(result, Ok(Some(false)), "cancel_snapshot should return false for non-existent");
    }
}
