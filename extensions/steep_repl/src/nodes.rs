//! Nodes table for steep_repl extension.
//!
//! This module creates the nodes table for tracking PostgreSQL instances
//! participating in bidirectional replication.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Nodes table: PostgreSQL instances participating in replication
CREATE TABLE steep_repl.nodes (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    -- Daemon gRPC address for cross-node health checks
    grpc_host TEXT,
    grpc_port INTEGER,
    priority INTEGER NOT NULL DEFAULT 50,
    is_coordinator BOOLEAN NOT NULL DEFAULT false,
    last_seen TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'unknown',
    -- Initialization state tracking (015-node-init)
    init_state TEXT NOT NULL DEFAULT 'uninitialized',
    init_source_node TEXT REFERENCES steep_repl.nodes(node_id),
    init_started_at TIMESTAMPTZ,
    init_completed_at TIMESTAMPTZ,
    -- Throughput metrics for ETA calculation (015-node-init)
    last_sync_throughput_bytes_sec REAL,
    last_sync_at TIMESTAMPTZ,
    CONSTRAINT nodes_priority_check CHECK (priority >= 1 AND priority <= 100),
    CONSTRAINT nodes_throughput_check CHECK (last_sync_throughput_bytes_sec IS NULL OR last_sync_throughput_bytes_sec >= 0),
    CONSTRAINT nodes_port_check CHECK (port >= 1 AND port <= 65535),
    CONSTRAINT nodes_grpc_port_check CHECK (grpc_port IS NULL OR (grpc_port >= 1 AND grpc_port <= 65535)),
    CONSTRAINT nodes_host_check CHECK (host <> ''),
    CONSTRAINT nodes_status_check CHECK (status IN ('unknown', 'healthy', 'degraded', 'unreachable', 'offline')),
    CONSTRAINT nodes_init_state_check CHECK (init_state IN (
        'uninitialized', 'preparing', 'copying', 'catching_up',
        'synchronized', 'diverged', 'failed', 'reinitializing'
    ))
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
COMMENT ON COLUMN steep_repl.nodes.init_state IS 'Initialization state (uninitialized, preparing, copying, catching_up, synchronized, diverged, failed, reinitializing)';
COMMENT ON COLUMN steep_repl.nodes.init_source_node IS 'Source node for initialization data copy';
COMMENT ON COLUMN steep_repl.nodes.init_started_at IS 'When initialization began';
COMMENT ON COLUMN steep_repl.nodes.init_completed_at IS 'When initialization completed successfully';
COMMENT ON COLUMN steep_repl.nodes.last_sync_throughput_bytes_sec IS 'EWMA throughput from last successful sync (bytes/sec)';
COMMENT ON COLUMN steep_repl.nodes.last_sync_at IS 'When last sync operation completed';

-- Indexes for nodes table
CREATE INDEX idx_nodes_status ON steep_repl.nodes(status);
CREATE INDEX idx_nodes_coordinator ON steep_repl.nodes(is_coordinator)
    WHERE is_coordinator = true;
CREATE INDEX idx_nodes_init_state ON steep_repl.nodes(init_state);
"#,
    name = "create_nodes_table",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

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
            ("init_state", "text"),
            ("init_source_node", "text"),
            ("init_started_at", "timestamp with time zone"),
            ("init_completed_at", "timestamp with time zone"),
            ("last_sync_throughput_bytes_sec", "real"),
            ("last_sync_at", "timestamp with time zone"),
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
    fn test_nodes_constraints() {
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

        // Check that init_state constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_init_state_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "init_state check constraint should exist");

        // Check that throughput constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'nodes'
                AND c.conname = 'nodes_throughput_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "throughput check constraint should exist");
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
}
