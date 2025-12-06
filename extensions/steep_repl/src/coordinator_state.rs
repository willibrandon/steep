//! Coordinator state table for steep_repl extension.
//!
//! This module creates the coordinator_state table for cluster-wide
//! coordination data storage using key-value pairs with JSONB values.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Coordinator state table: Key-value store for cluster coordination
CREATE TABLE steep_repl.coordinator_state (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE steep_repl.coordinator_state IS 'Key-value store for cluster-wide coordination data';
COMMENT ON COLUMN steep_repl.coordinator_state.key IS 'State key (e.g., cluster_version, range_allocator)';
COMMENT ON COLUMN steep_repl.coordinator_state.value IS 'State value as JSONB';
COMMENT ON COLUMN steep_repl.coordinator_state.updated_at IS 'Last update timestamp';
"#,
    name = "create_coordinator_state_table",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_coordinator_state_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'coordinator_state'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "coordinator_state table should exist");
    }

    #[pg_test]
    fn test_coordinator_state_jsonb() {
        // Test JSONB storage
        Spi::run(
            r#"INSERT INTO steep_repl.coordinator_state (key, value)
               VALUES ('test_key', '{"version": 1, "data": "test"}')"#
        ).expect("jsonb insert should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT (value->>'version')::int FROM steep_repl.coordinator_state WHERE key = 'test_key'"
        );
        assert_eq!(result, Ok(Some(1)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.coordinator_state WHERE key = 'test_key'")
            .expect("cleanup should succeed");
    }
}
