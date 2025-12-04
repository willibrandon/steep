//! steep_repl - PostgreSQL extension for bidirectional replication coordination
//!
//! This extension creates the steep_repl schema with tables for:
//! - nodes: Cluster node registration and status
//! - coordinator_state: Key-value store for cluster coordination
//! - audit_log: Immutable audit trail of system activity
//!
//! Requires PostgreSQL 18 or later.

use pgrx::prelude::*;

// Extension metadata
::pgrx::pg_module_magic!(name, version);

// =============================================================================
// PostgreSQL 18 Version Check
// =============================================================================

/// Check that we're running on PostgreSQL 18 or later.
/// This is enforced at extension load time.
#[pg_guard]
pub extern "C-unwind" fn _PG_init() {
    // PostgreSQL version is checked at compile time via pgrx features.
    // Runtime check for additional safety:
    let version = pgrx::pg_sys::PG_VERSION_NUM;
    if version < 180000 {
        pgrx::error!(
            "steep_repl requires PostgreSQL 18 or later (found version {})",
            version
        );
    }
}

// =============================================================================
// Schema Creation (bootstrap)
// =============================================================================

extension_sql!(
    r#"
-- Create the steep_repl schema
CREATE SCHEMA IF NOT EXISTS steep_repl;

COMMENT ON SCHEMA steep_repl IS 'Steep bidirectional replication coordination schema';
"#,
    name = "create_schema",
    bootstrap,
);

// =============================================================================
// Nodes Table
// =============================================================================

extension_sql!(
    r#"
-- Nodes table: PostgreSQL instances participating in replication
CREATE TABLE steep_repl.nodes (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    priority INTEGER NOT NULL DEFAULT 50,
    is_coordinator BOOLEAN NOT NULL DEFAULT false,
    last_seen TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'unknown',
    CONSTRAINT nodes_priority_check CHECK (priority >= 1 AND priority <= 100),
    CONSTRAINT nodes_port_check CHECK (port >= 1 AND port <= 65535),
    CONSTRAINT nodes_host_check CHECK (host <> ''),
    CONSTRAINT nodes_status_check CHECK (status IN ('unknown', 'healthy', 'degraded', 'unreachable', 'offline'))
);

COMMENT ON TABLE steep_repl.nodes IS 'Cluster nodes participating in bidirectional replication';
COMMENT ON COLUMN steep_repl.nodes.node_id IS 'Unique identifier (UUID format recommended)';
COMMENT ON COLUMN steep_repl.nodes.node_name IS 'Human-readable name';
COMMENT ON COLUMN steep_repl.nodes.host IS 'Hostname or IP address';
COMMENT ON COLUMN steep_repl.nodes.port IS 'PostgreSQL port (1-65535)';
COMMENT ON COLUMN steep_repl.nodes.priority IS 'Coordinator election priority (1-100, higher = preferred)';
COMMENT ON COLUMN steep_repl.nodes.is_coordinator IS 'Currently elected coordinator';
COMMENT ON COLUMN steep_repl.nodes.last_seen IS 'Last heartbeat timestamp';
COMMENT ON COLUMN steep_repl.nodes.status IS 'Node health status';

-- Indexes for nodes table
CREATE INDEX idx_nodes_status ON steep_repl.nodes(status);
CREATE INDEX idx_nodes_coordinator ON steep_repl.nodes(is_coordinator)
    WHERE is_coordinator = true;
"#,
    name = "create_nodes_table",
    requires = ["create_schema"],
);

// =============================================================================
// Coordinator State Table
// =============================================================================

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

// =============================================================================
// Audit Log Table
// =============================================================================

extension_sql!(
    r#"
-- Audit log table: Immutable record of system activity
CREATE TABLE steep_repl.audit_log (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    target_type TEXT,
    target_id TEXT,
    old_value JSONB,
    new_value JSONB,
    client_ip INET,
    success BOOLEAN NOT NULL DEFAULT true,
    error_message TEXT
);

COMMENT ON TABLE steep_repl.audit_log IS 'Immutable audit trail of system activity';
COMMENT ON COLUMN steep_repl.audit_log.id IS 'Unique log entry ID';
COMMENT ON COLUMN steep_repl.audit_log.occurred_at IS 'Event timestamp';
COMMENT ON COLUMN steep_repl.audit_log.action IS 'Action type (e.g., node.registered, coordinator.elected)';
COMMENT ON COLUMN steep_repl.audit_log.actor IS 'Who performed action (role@host format)';
COMMENT ON COLUMN steep_repl.audit_log.target_type IS 'Type of target entity (node, state, daemon)';
COMMENT ON COLUMN steep_repl.audit_log.target_id IS 'ID of target entity';
COMMENT ON COLUMN steep_repl.audit_log.old_value IS 'Previous state (for updates)';
COMMENT ON COLUMN steep_repl.audit_log.new_value IS 'New state (for creates/updates)';
COMMENT ON COLUMN steep_repl.audit_log.client_ip IS 'Client IP address';
COMMENT ON COLUMN steep_repl.audit_log.success IS 'Whether action succeeded';
COMMENT ON COLUMN steep_repl.audit_log.error_message IS 'Error details if failed';

-- Indexes for audit log queries
CREATE INDEX idx_audit_log_occurred_at ON steep_repl.audit_log(occurred_at DESC);
CREATE INDEX idx_audit_log_actor ON steep_repl.audit_log(actor);
CREATE INDEX idx_audit_log_action ON steep_repl.audit_log(action);
CREATE INDEX idx_audit_log_target ON steep_repl.audit_log(target_type, target_id)
    WHERE target_type IS NOT NULL;
"#,
    name = "create_audit_log_table",
    requires = ["create_schema"],
);

// =============================================================================
// Utility Functions
// =============================================================================

/// Returns the steep_repl extension version.
#[pg_extern]
fn steep_repl_version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

/// Returns the minimum required PostgreSQL version.
#[pg_extern]
fn steep_repl_min_pg_version() -> i32 {
    180000
}

// =============================================================================
// Tests
// =============================================================================

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_schema_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'steep_repl')",
        );
        assert_eq!(result, Ok(Some(true)), "steep_repl schema should exist");
    }

    #[pg_test]
    fn test_nodes_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'nodes'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "nodes table should exist");
    }

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
    fn test_audit_log_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'audit_log'
            )",
        );
        assert_eq!(result, Ok(Some(true)), "audit_log table should exist");
    }

    #[pg_test]
    fn test_nodes_table_columns() {
        // Verify all expected columns exist with correct types
        let columns = vec![
            ("node_id", "text"),
            ("node_name", "text"),
            ("host", "text"),
            ("port", "integer"),
            ("priority", "integer"),
            ("is_coordinator", "boolean"),
            ("last_seen", "timestamp with time zone"),
            ("status", "text"),
        ];

        for (col_name, col_type) in columns {
            let query = format!(
                "SELECT data_type FROM information_schema.columns
                 WHERE table_schema = 'steep_repl'
                 AND table_name = 'nodes'
                 AND column_name = '{}'",
                col_name
            );
            let result = Spi::get_one::<String>(&query);
            assert_eq!(
                result,
                Ok(Some(col_type.to_string())),
                "nodes.{} should be type {}",
                col_name,
                col_type
            );
        }
    }

    #[pg_test]
    fn test_audit_log_indexes() {
        // Check that all required indexes exist
        let indexes = vec![
            "idx_audit_log_occurred_at",
            "idx_audit_log_actor",
            "idx_audit_log_action",
            "idx_audit_log_target",
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
    fn test_nodes_constraints() {
        // In pgrx tests, we verify constraints exist via pg_constraint
        // because SPI errors abort the transaction

        // Check that priority constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_priority_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "priority check constraint should exist");

        // Check that port constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_port_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "port check constraint should exist");

        // Check that host not empty constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_host_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "host check constraint should exist");

        // Check that status constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_status_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "status check constraint should exist");
    }

    #[pg_test]
    fn test_valid_node_insert() {
        // Test valid insert
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('node-a', 'Primary', '192.168.1.10', 5432, 80, 'healthy')"
        ).expect("valid node insert should succeed");

        let result = Spi::get_one::<String>(
            "SELECT node_name FROM steep_repl.nodes WHERE node_id = 'node-a'"
        );
        assert_eq!(result, Ok(Some("Primary".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'node-a'")
            .expect("cleanup should succeed");
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

    #[pg_test]
    fn test_audit_log_insert() {
        Spi::run(
            "INSERT INTO steep_repl.audit_log (action, actor, target_type, target_id, success)
             VALUES ('daemon.started', 'steep_repl@localhost', 'daemon', 'node-a', true)"
        ).expect("audit log insert should succeed");

        let result = Spi::get_one::<String>(
            "SELECT action FROM steep_repl.audit_log WHERE actor = 'steep_repl@localhost' LIMIT 1"
        );
        assert_eq!(result, Ok(Some("daemon.started".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.audit_log WHERE actor = 'steep_repl@localhost'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_steep_repl_version() {
        let version = crate::steep_repl_version();
        assert!(!version.is_empty(), "version should not be empty");
    }

    #[pg_test]
    fn test_steep_repl_min_pg_version() {
        let min_version = crate::steep_repl_min_pg_version();
        assert_eq!(min_version, 180000, "minimum version should be 180000 (PG18)");
    }
}

/// This module is required by `cargo pgrx test` invocations.
/// It must be visible at the root of your extension crate.
#[cfg(test)]
pub mod pg_test {
    pub fn setup(_options: Vec<&str>) {
        // perform one-off initialization when the pg_test framework starts
    }

    #[must_use]
    pub fn postgresql_conf_options() -> Vec<&'static str> {
        // return any postgresql.conf settings that are required for your tests
        vec![]
    }
}
