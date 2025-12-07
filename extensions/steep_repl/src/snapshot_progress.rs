//! Snapshot progress table for steep_repl extension.
//!
//! This module creates the snapshot_progress table for real-time
//! two-phase snapshot progress tracking (generation and application).
//! Implements T087b: Progress tracking infrastructure.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Snapshot progress table: Real-time two-phase snapshot progress tracking
CREATE TABLE steep_repl.snapshot_progress (
    snapshot_id TEXT PRIMARY KEY,
    phase TEXT NOT NULL,
    overall_percent REAL NOT NULL DEFAULT 0,
    current_step TEXT NOT NULL DEFAULT 'schema',
    -- Table tracking
    tables_total INTEGER NOT NULL DEFAULT 0,
    tables_completed INTEGER NOT NULL DEFAULT 0,
    current_table TEXT,
    current_table_bytes BIGINT DEFAULT 0,
    current_table_total_bytes BIGINT DEFAULT 0,
    -- Byte/row tracking
    bytes_written BIGINT DEFAULT 0,
    bytes_total BIGINT DEFAULT 0,
    rows_written BIGINT DEFAULT 0,
    rows_total BIGINT DEFAULT 0,
    -- Throughput (rolling 10-second average)
    throughput_bytes_sec REAL DEFAULT 0,
    throughput_rows_sec REAL DEFAULT 0,
    -- Timing
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    eta_seconds INTEGER DEFAULT 0,
    -- Compression
    compression_enabled BOOLEAN DEFAULT false,
    compression_ratio REAL DEFAULT 0,
    -- Checksum verification (application phase)
    checksum_verifications INTEGER DEFAULT 0,
    checksums_verified INTEGER DEFAULT 0,
    checksums_failed INTEGER DEFAULT 0,
    -- Metadata
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    error_message TEXT,
    -- Constraints
    CONSTRAINT snapshot_progress_phase_check CHECK (phase IN ('generation', 'application')),
    CONSTRAINT snapshot_progress_step_check CHECK (current_step IN ('schema', 'tables', 'sequences', 'checksums', 'finalizing')),
    CONSTRAINT snapshot_progress_overall_percent_check CHECK (overall_percent BETWEEN 0 AND 100),
    CONSTRAINT snapshot_progress_tables_total_check CHECK (tables_total >= 0),
    CONSTRAINT snapshot_progress_tables_completed_check CHECK (tables_completed >= 0),
    CONSTRAINT snapshot_progress_tables_check CHECK (tables_completed <= tables_total),
    CONSTRAINT snapshot_progress_bytes_written_check CHECK (bytes_written >= 0),
    CONSTRAINT snapshot_progress_bytes_total_check CHECK (bytes_total >= 0),
    CONSTRAINT snapshot_progress_rows_written_check CHECK (rows_written >= 0),
    CONSTRAINT snapshot_progress_rows_total_check CHECK (rows_total >= 0),
    CONSTRAINT snapshot_progress_eta_check CHECK (eta_seconds >= 0),
    CONSTRAINT snapshot_progress_checksums_verified_check CHECK (checksums_verified >= 0),
    CONSTRAINT snapshot_progress_checksums_failed_check CHECK (checksums_failed >= 0)
);

COMMENT ON TABLE steep_repl.snapshot_progress IS 'Real-time two-phase snapshot progress tracking';
COMMENT ON COLUMN steep_repl.snapshot_progress.snapshot_id IS 'Unique snapshot identifier';
COMMENT ON COLUMN steep_repl.snapshot_progress.phase IS 'Current phase: generation or application';
COMMENT ON COLUMN steep_repl.snapshot_progress.overall_percent IS 'Overall progress 0-100';
COMMENT ON COLUMN steep_repl.snapshot_progress.current_step IS 'Current step: schema, tables, sequences, checksums, finalizing';
COMMENT ON COLUMN steep_repl.snapshot_progress.tables_total IS 'Total tables to process';
COMMENT ON COLUMN steep_repl.snapshot_progress.tables_completed IS 'Tables finished';
COMMENT ON COLUMN steep_repl.snapshot_progress.current_table IS 'Table currently processing';
COMMENT ON COLUMN steep_repl.snapshot_progress.current_table_bytes IS 'Bytes processed for current table';
COMMENT ON COLUMN steep_repl.snapshot_progress.current_table_total_bytes IS 'Total bytes for current table';
COMMENT ON COLUMN steep_repl.snapshot_progress.bytes_written IS 'Total bytes written/read so far';
COMMENT ON COLUMN steep_repl.snapshot_progress.bytes_total IS 'Total bytes expected';
COMMENT ON COLUMN steep_repl.snapshot_progress.rows_written IS 'Total rows written/read so far';
COMMENT ON COLUMN steep_repl.snapshot_progress.rows_total IS 'Total rows expected';
COMMENT ON COLUMN steep_repl.snapshot_progress.throughput_bytes_sec IS 'Rolling average throughput in bytes/sec';
COMMENT ON COLUMN steep_repl.snapshot_progress.throughput_rows_sec IS 'Rolling average throughput in rows/sec';
COMMENT ON COLUMN steep_repl.snapshot_progress.started_at IS 'When the operation started';
COMMENT ON COLUMN steep_repl.snapshot_progress.eta_seconds IS 'Estimated seconds remaining';
COMMENT ON COLUMN steep_repl.snapshot_progress.compression_enabled IS 'Whether compression is enabled';
COMMENT ON COLUMN steep_repl.snapshot_progress.compression_ratio IS 'Compression ratio if enabled';
COMMENT ON COLUMN steep_repl.snapshot_progress.checksum_verifications IS 'Total checksum verifications to perform';
COMMENT ON COLUMN steep_repl.snapshot_progress.checksums_verified IS 'Checksums that passed verification';
COMMENT ON COLUMN steep_repl.snapshot_progress.checksums_failed IS 'Checksums that failed verification';
COMMENT ON COLUMN steep_repl.snapshot_progress.updated_at IS 'Last update timestamp';
COMMENT ON COLUMN steep_repl.snapshot_progress.error_message IS 'Error message if operation failed';

-- Index for efficient queries by phase and status
CREATE INDEX idx_snapshot_progress_phase ON steep_repl.snapshot_progress(phase);
CREATE INDEX idx_snapshot_progress_updated_at ON steep_repl.snapshot_progress(updated_at DESC);
"#,
    name = "create_snapshot_progress_table",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_snapshot_progress_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'snapshot_progress'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "snapshot_progress table should exist");
    }

    #[pg_test]
    fn test_snapshot_progress_insert_generation() {
        // Insert generation progress record
        Spi::run(
            "INSERT INTO steep_repl.snapshot_progress (
                snapshot_id, phase, overall_percent, current_step,
                tables_total, bytes_total, compression_enabled
            ) VALUES (
                'snap_test_gen', 'generation', 25.5, 'tables',
                10, 1073741824, true
            )"
        ).expect("generation progress insert should succeed");

        let result = Spi::get_one::<f32>(
            "SELECT overall_percent FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_gen'"
        );
        assert_eq!(result, Ok(Some(25.5)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_gen'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_snapshot_progress_insert_application() {
        // Insert application progress record with checksum tracking
        Spi::run(
            "INSERT INTO steep_repl.snapshot_progress (
                snapshot_id, phase, overall_percent, current_step,
                tables_total, checksum_verifications, checksums_verified
            ) VALUES (
                'snap_test_app', 'application', 50.0, 'checksums',
                15, 15, 10
            )"
        ).expect("application progress insert should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT checksums_verified FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_app'"
        );
        assert_eq!(result, Ok(Some(10)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_app'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_snapshot_progress_phase_constraint() {
        // Verify phase constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshot_progress'
                AND c.conname = 'snapshot_progress_phase_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "phase check constraint should exist");
    }

    #[pg_test]
    fn test_snapshot_progress_step_constraint() {
        // Verify step constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'snapshot_progress'
                AND c.conname = 'snapshot_progress_step_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "step check constraint should exist");
    }

    #[pg_test]
    fn test_snapshot_progress_update() {
        // Insert initial record
        Spi::run(
            "INSERT INTO steep_repl.snapshot_progress (
                snapshot_id, phase, overall_percent, current_step,
                tables_total, tables_completed, throughput_bytes_sec
            ) VALUES (
                'snap_test_update', 'generation', 0.0, 'schema',
                5, 0, 0.0
            )"
        ).expect("insert should succeed");

        // Update progress
        Spi::run(
            "UPDATE steep_repl.snapshot_progress
             SET overall_percent = 40.0,
                 current_step = 'tables',
                 tables_completed = 2,
                 throughput_bytes_sec = 52428800.0,
                 updated_at = now()
             WHERE snapshot_id = 'snap_test_update'"
        ).expect("update should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT tables_completed FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_update'"
        );
        assert_eq!(result, Ok(Some(2)));

        let throughput = Spi::get_one::<f32>(
            "SELECT throughput_bytes_sec FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_update'"
        );
        assert_eq!(throughput, Ok(Some(52428800.0)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_test_update'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_snapshot_progress_indexes_exist() {
        // Check phase index
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_indexes
                WHERE schemaname = 'steep_repl'
                AND tablename = 'snapshot_progress'
                AND indexname = 'idx_snapshot_progress_phase'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "phase index should exist");

        // Check updated_at index
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_indexes
                WHERE schemaname = 'steep_repl'
                AND tablename = 'snapshot_progress'
                AND indexname = 'idx_snapshot_progress_updated_at'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "updated_at index should exist");
    }
}
