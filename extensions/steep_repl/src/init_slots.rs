//! Init slots table for steep_repl extension.
//!
//! This module creates the init_slots table for tracking replication slots
//! used during manual initialization workflow.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Init slots table: Replication slots for manual initialization workflow
CREATE TABLE steep_repl.init_slots (
    slot_name TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    lsn TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    used_by_node TEXT REFERENCES steep_repl.nodes(node_id),
    used_at TIMESTAMPTZ
);

COMMENT ON TABLE steep_repl.init_slots IS 'Replication slots for manual initialization workflow';
COMMENT ON COLUMN steep_repl.init_slots.slot_name IS 'Replication slot name';
COMMENT ON COLUMN steep_repl.init_slots.node_id IS 'Node that owns the slot';
COMMENT ON COLUMN steep_repl.init_slots.lsn IS 'LSN at slot creation';
COMMENT ON COLUMN steep_repl.init_slots.created_at IS 'When slot was created';
COMMENT ON COLUMN steep_repl.init_slots.expires_at IS 'Auto-cleanup timestamp';
COMMENT ON COLUMN steep_repl.init_slots.used_by_node IS 'Node that used this slot for init';
COMMENT ON COLUMN steep_repl.init_slots.used_at IS 'When slot was consumed';

-- Indexes for init slots
CREATE INDEX idx_init_slots_node ON steep_repl.init_slots(node_id);
CREATE INDEX idx_init_slots_expires ON steep_repl.init_slots(expires_at) WHERE expires_at IS NOT NULL;
"#,
    name = "create_init_slots_table",
    requires = ["create_nodes_table"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_init_slots_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'init_slots'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "init_slots table should exist");
    }

    #[pg_test]
    fn test_init_slots_insert() {
        // First create a node to reference
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test-node-slots', 'Test Node', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Insert slot record
        Spi::run(
            "INSERT INTO steep_repl.init_slots (slot_name, node_id, lsn)
             VALUES ('test_slot_01', 'test-node-slots', '0/1A234B00')"
        ).expect("slot insert should succeed");

        let result = Spi::get_one::<String>(
            "SELECT lsn FROM steep_repl.init_slots WHERE slot_name = 'test_slot_01'"
        );
        assert_eq!(result, Ok(Some("0/1A234B00".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.init_slots WHERE slot_name = 'test_slot_01'")
            .expect("cleanup init_slots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-slots'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_init_slots_indexes() {
        let indexes = vec![
            "idx_init_slots_node",
            "idx_init_slots_expires",
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
