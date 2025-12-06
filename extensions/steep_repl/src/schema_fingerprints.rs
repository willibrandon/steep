//! Schema fingerprints table for steep_repl extension.
//!
//! This module creates the schema_fingerprints table for storing
//! SHA256 hashes of table column definitions for drift detection.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Schema fingerprints table: Schema fingerprints for drift detection
-- node_id allows storing fingerprints for multiple nodes (local + cached remote)
CREATE TABLE steep_repl.schema_fingerprints (
    node_id TEXT NOT NULL,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    column_count INTEGER NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    column_definitions JSONB,
    PRIMARY KEY (node_id, table_schema, table_name),
    CONSTRAINT fingerprints_column_count_check CHECK (column_count >= 0)
);

COMMENT ON TABLE steep_repl.schema_fingerprints IS 'Schema fingerprints for drift detection';
COMMENT ON COLUMN steep_repl.schema_fingerprints.node_id IS 'Node ID that owns these fingerprints';
COMMENT ON COLUMN steep_repl.schema_fingerprints.table_schema IS 'PostgreSQL schema name';
COMMENT ON COLUMN steep_repl.schema_fingerprints.table_name IS 'Table name';
COMMENT ON COLUMN steep_repl.schema_fingerprints.fingerprint IS 'SHA256 hash of column definitions';
COMMENT ON COLUMN steep_repl.schema_fingerprints.column_count IS 'Number of columns';
COMMENT ON COLUMN steep_repl.schema_fingerprints.captured_at IS 'When fingerprint was computed';
COMMENT ON COLUMN steep_repl.schema_fingerprints.column_definitions IS 'Detailed column info for diff';

-- Index for fingerprint queries
CREATE INDEX idx_fingerprints_captured ON steep_repl.schema_fingerprints(captured_at);
CREATE INDEX idx_fingerprints_node ON steep_repl.schema_fingerprints(node_id);
"#,
    name = "create_schema_fingerprints_table",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_schema_fingerprints_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'schema_fingerprints'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "schema_fingerprints table should exist");
    }

    #[pg_test]
    fn test_schema_fingerprints_insert() {
        Spi::run(
            "INSERT INTO steep_repl.schema_fingerprints (node_id, table_schema, table_name, fingerprint, column_count)
             VALUES ('test-node', 'public', 'test_table', 'abc123def456', 5)"
        ).expect("fingerprint insert should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT column_count FROM steep_repl.schema_fingerprints
             WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_table'"
        );
        assert_eq!(result, Ok(Some(5)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_table'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_schema_fingerprints_column_definitions_jsonb() {
        Spi::run(
            r#"INSERT INTO steep_repl.schema_fingerprints
               (node_id, table_schema, table_name, fingerprint, column_count, column_definitions)
               VALUES ('test-node', 'public', 'test_jsonb', 'def456abc123', 2,
                       '[{"name": "id", "type": "integer"}, {"name": "name", "type": "text"}]')"#
        ).expect("fingerprint with jsonb insert should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT jsonb_array_length(column_definitions) FROM steep_repl.schema_fingerprints
             WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_jsonb'"
        );
        assert_eq!(result, Ok(Some(2)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_jsonb'")
            .expect("cleanup should succeed");
    }
}
