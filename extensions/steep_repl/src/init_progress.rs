//! Init progress table for steep_repl extension.
//!
//! This module creates the init_progress table for real-time
//! initialization progress tracking with throughput metrics.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Init progress table: Real-time initialization progress tracking
CREATE TABLE steep_repl.init_progress (
    node_id TEXT PRIMARY KEY REFERENCES steep_repl.nodes(node_id) ON DELETE CASCADE,
    phase TEXT NOT NULL,
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    parallel_workers INTEGER DEFAULT 1,
    error_message TEXT,
    CONSTRAINT progress_phase_check CHECK (phase IN ('preparing', 'copying', 'catching_up', 'complete', 'failed')),
    CONSTRAINT progress_overall_percent_check CHECK (overall_percent BETWEEN 0 AND 100),
    CONSTRAINT progress_tables_total_check CHECK (tables_total >= 0),
    CONSTRAINT progress_tables_completed_check CHECK (tables_completed >= 0),
    CONSTRAINT progress_current_table_percent_check CHECK (current_table_percent BETWEEN 0 AND 100),
    CONSTRAINT progress_rows_check CHECK (rows_copied >= 0),
    CONSTRAINT progress_bytes_check CHECK (bytes_copied >= 0),
    CONSTRAINT progress_eta_check CHECK (eta_seconds >= 0),
    CONSTRAINT progress_parallel_check CHECK (parallel_workers BETWEEN 1 AND 16),
    CONSTRAINT progress_tables_check CHECK (tables_completed <= tables_total)
);

COMMENT ON TABLE steep_repl.init_progress IS 'Real-time initialization progress tracking';
COMMENT ON COLUMN steep_repl.init_progress.node_id IS 'Node being initialized';
COMMENT ON COLUMN steep_repl.init_progress.phase IS 'Current phase: preparing, copying, catching_up, complete, failed';
COMMENT ON COLUMN steep_repl.init_progress.overall_percent IS 'Overall progress 0-100';
COMMENT ON COLUMN steep_repl.init_progress.tables_total IS 'Total tables to process';
COMMENT ON COLUMN steep_repl.init_progress.tables_completed IS 'Tables finished';
COMMENT ON COLUMN steep_repl.init_progress.current_table IS 'Table currently processing';
COMMENT ON COLUMN steep_repl.init_progress.current_table_percent IS 'Progress within current table';
COMMENT ON COLUMN steep_repl.init_progress.rows_copied IS 'Total rows copied so far';
COMMENT ON COLUMN steep_repl.init_progress.bytes_copied IS 'Total bytes copied';
COMMENT ON COLUMN steep_repl.init_progress.throughput_rows_sec IS 'Current throughput in rows/sec';
COMMENT ON COLUMN steep_repl.init_progress.eta_seconds IS 'Estimated seconds remaining';
COMMENT ON COLUMN steep_repl.init_progress.parallel_workers IS 'Active parallel workers';
COMMENT ON COLUMN steep_repl.init_progress.error_message IS 'Last error if any';
"#,
    name = "create_init_progress_table",
    requires = ["create_nodes_table"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_init_progress_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'init_progress'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "init_progress table should exist");
    }

    #[pg_test]
    fn test_init_progress_insert() {
        // First create a node to reference
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test-node-progress', 'Test Node', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Insert progress record
        Spi::run(
            "INSERT INTO steep_repl.init_progress (node_id, phase, overall_percent, tables_total)
             VALUES ('test-node-progress', 'copying', 50.0, 10)"
        ).expect("progress insert should succeed");

        let result = Spi::get_one::<f32>(
            "SELECT overall_percent FROM steep_repl.init_progress WHERE node_id = 'test-node-progress'"
        );
        assert_eq!(result, Ok(Some(50.0)));

        // Cleanup (cascade will delete progress)
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-progress'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_init_progress_constraints() {
        // Verify phase constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'init_progress'
                AND c.conname = 'progress_phase_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "phase check constraint should exist");

        // Verify percent constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'init_progress'
                AND c.conname = 'progress_overall_percent_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "percent check constraint should exist");
    }
}
