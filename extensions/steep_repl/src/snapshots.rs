//! Snapshots table for steep_repl extension.
//!
//! This module creates the snapshots table for tracking generated
//! snapshot manifests and real-time progress for two-phase initialization.

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
    CONSTRAINT snapshots_phase_check CHECK (phase IN ('idle', 'schema', 'data', 'indexes', 'constraints', 'sequences', 'verify'))
);

-- Comments
COMMENT ON TABLE steep_repl.snapshots IS 'Snapshot manifests with real-time progress tracking for two-phase initialization';
COMMENT ON COLUMN steep_repl.snapshots.snapshot_id IS 'Unique snapshot identifier';
COMMENT ON COLUMN steep_repl.snapshots.source_node_id IS 'Node snapshot was taken from';
COMMENT ON COLUMN steep_repl.snapshots.target_node_id IS 'Node snapshot is being applied to (NULL during generation)';
COMMENT ON COLUMN steep_repl.snapshots.lsn IS 'WAL position at snapshot time';
COMMENT ON COLUMN steep_repl.snapshots.storage_path IS 'File system or S3 path';
COMMENT ON COLUMN steep_repl.snapshots.compression IS 'Compression type (none, gzip, lz4, zstd)';
COMMENT ON COLUMN steep_repl.snapshots.checksum IS 'SHA256 of manifest';
COMMENT ON COLUMN steep_repl.snapshots.status IS 'Overall status: pending, generating, complete, applying, applied, failed, cancelled, expired';
COMMENT ON COLUMN steep_repl.snapshots.phase IS 'Current phase: idle, schema, data, indexes, constraints, sequences, verify';
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
}
