//! Snapshots table for steep_repl extension.
//!
//! This module creates the snapshots table for tracking generated
//! snapshot manifests used in two-phase initialization.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Snapshots table: Generated snapshot manifests for two-phase initialization
CREATE TABLE steep_repl.snapshots (
    snapshot_id TEXT PRIMARY KEY,
    source_node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    lsn TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    size_bytes BIGINT NOT NULL,
    table_count INTEGER NOT NULL,
    compression TEXT DEFAULT 'gzip',
    checksum TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending',
    CONSTRAINT snapshots_size_check CHECK (size_bytes >= 0),
    CONSTRAINT snapshots_table_count_check CHECK (table_count >= 0),
    CONSTRAINT snapshots_compression_check CHECK (compression IN ('none', 'gzip', 'lz4', 'zstd')),
    CONSTRAINT snapshots_status_check CHECK (status IN ('pending', 'complete', 'applied', 'expired'))
);

COMMENT ON TABLE steep_repl.snapshots IS 'Generated snapshot manifests for two-phase initialization';
COMMENT ON COLUMN steep_repl.snapshots.snapshot_id IS 'Unique snapshot identifier';
COMMENT ON COLUMN steep_repl.snapshots.source_node_id IS 'Node snapshot was taken from';
COMMENT ON COLUMN steep_repl.snapshots.lsn IS 'WAL position at snapshot time';
COMMENT ON COLUMN steep_repl.snapshots.storage_path IS 'File system or S3 path';
COMMENT ON COLUMN steep_repl.snapshots.size_bytes IS 'Total snapshot size';
COMMENT ON COLUMN steep_repl.snapshots.table_count IS 'Number of tables included';
COMMENT ON COLUMN steep_repl.snapshots.compression IS 'Compression type (none, gzip, lz4, zstd)';
COMMENT ON COLUMN steep_repl.snapshots.checksum IS 'SHA256 of manifest';
COMMENT ON COLUMN steep_repl.snapshots.expires_at IS 'Auto-cleanup timestamp';
COMMENT ON COLUMN steep_repl.snapshots.status IS 'Status: pending, complete, applied, expired';

-- Indexes for snapshots
CREATE INDEX idx_snapshots_source ON steep_repl.snapshots(source_node_id);
CREATE INDEX idx_snapshots_status ON steep_repl.snapshots(status);
CREATE INDEX idx_snapshots_expires ON steep_repl.snapshots(expires_at) WHERE expires_at IS NOT NULL;
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
    fn test_snapshots_insert() {
        // First create a node to reference
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test-node-snapshots', 'Test Node', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Insert snapshot record
        Spi::run(
            "INSERT INTO steep_repl.snapshots
             (snapshot_id, source_node_id, lsn, storage_path, size_bytes, table_count, checksum)
             VALUES ('snap_test_01', 'test-node-snapshots', '0/1A234B00', '/tmp/snapshots/snap_test_01', 1024000, 5, 'sha256:abc123')"
        ).expect("snapshot insert should succeed");

        let result = Spi::get_one::<i64>(
            "SELECT size_bytes FROM steep_repl.snapshots WHERE snapshot_id = 'snap_test_01'"
        );
        assert_eq!(result, Ok(Some(1024000)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_test_01'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-snapshots'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_snapshots_compression_constraint() {
        // Verify compression constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshots'
                AND c.conname = 'snapshots_compression_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compression check constraint should exist");
    }

    #[pg_test]
    fn test_snapshots_status_constraint() {
        // Verify status constraint exists
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
    fn test_snapshots_indexes() {
        let indexes = vec![
            "idx_snapshots_source",
            "idx_snapshots_status",
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
}
