//! Nodes table and SQL functions for steep_repl extension.
//!
//! This module creates the nodes table for tracking PostgreSQL instances
//! participating in bidirectional replication, and provides SQL functions
//! for node management: register_node, heartbeat, node_status.
//!
//! ## Task References
//!
//! - T019: Implement steep_repl.register_node() SQL function
//! - T020: Implement steep_repl.heartbeat() SQL function
//! - T021: Implement steep_repl.node_status() SQL function

use pgrx::datum::TimestampWithTimeZone;
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

// =============================================================================
// SQL Functions (T019, T020, T021)
// =============================================================================

/// Escape a string for safe use in SQL queries.
/// This provides basic SQL injection protection for string values.
fn escape_sql_string(s: &str) -> String {
    s.replace('\'', "''")
}

/// T019: Register or update a node.
///
/// This function registers a new node or updates an existing one. It uses
/// INSERT ... ON CONFLICT DO UPDATE (UPSERT) semantics. When updating an
/// existing node, the host is only updated if a new value is provided
/// (NULL preserves the existing value).
///
/// # Arguments
///
/// * `p_node_id` - Unique identifier for the node (UUID format recommended)
/// * `p_node_name` - Human-readable name for the node
/// * `p_host` - Hostname or IP address (optional, defaults to 'localhost')
/// * `p_port` - PostgreSQL port (default: 5432)
/// * `p_priority` - Coordinator election priority 1-100, higher = preferred (default: 50)
///
/// # Returns
///
/// Returns a table with the registered/updated node's details.
#[pg_extern]
fn _steep_repl_register_node(
    p_node_id: &str,
    p_node_name: &str,
    p_host: default!(Option<&str>, "NULL"),
    p_port: default!(Option<i32>, "5432"),
    p_priority: default!(Option<i32>, "50"),
) -> TableIterator<
    'static,
    (
        name!(node_id, Option<String>),
        name!(node_name, Option<String>),
        name!(host, Option<String>),
        name!(port, Option<i32>),
        name!(priority, Option<i32>),
        name!(is_coordinator, Option<bool>),
        name!(last_seen, Option<TimestampWithTimeZone>),
        name!(status, Option<String>),
    ),
> {
    let port = p_port.unwrap_or(5432);
    let priority = p_priority.unwrap_or(50);

    // Validate priority range
    if !(1..=100).contains(&priority) {
        pgrx::error!("priority must be between 1 and 100, got {}", priority);
    }

    // Validate port range
    if !(1..=65535).contains(&port) {
        pgrx::error!("port must be between 1 and 65535, got {}", port);
    }

    // If host is not provided, use 'localhost' as default
    let host = p_host.unwrap_or("localhost");
    if host.is_empty() {
        pgrx::error!("host cannot be empty");
    }

    // Escape strings for SQL
    let safe_node_id = escape_sql_string(p_node_id);
    let safe_node_name = escape_sql_string(p_node_name);
    let safe_host = escape_sql_string(host);

    // Execute UPSERT query
    let query = format!(
        "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status, last_seen)
         VALUES ('{}', '{}', '{}', {}, {}, 'healthy', now())
         ON CONFLICT (node_id) DO UPDATE SET
             node_name = EXCLUDED.node_name,
             host = COALESCE(EXCLUDED.host, steep_repl.nodes.host),
             port = EXCLUDED.port,
             priority = EXCLUDED.priority,
             last_seen = now()",
        safe_node_id, safe_node_name, safe_host, port, priority
    );

    if let Err(e) = Spi::run(&query) {
        pgrx::error!("Failed to register node: {}", e);
    }

    // Query the result
    let mut rows = Vec::new();

    let node_id = Spi::get_one::<String>(&format!(
        "SELECT node_id FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let node_name = Spi::get_one::<String>(&format!(
        "SELECT node_name FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let host_result = Spi::get_one::<String>(&format!(
        "SELECT host FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let port_result = Spi::get_one::<i32>(&format!(
        "SELECT port FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let priority_result = Spi::get_one::<i32>(&format!(
        "SELECT priority FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let is_coordinator = Spi::get_one::<bool>(&format!(
        "SELECT is_coordinator FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let last_seen = Spi::get_one::<TimestampWithTimeZone>(&format!(
        "SELECT last_seen FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    let status = Spi::get_one::<String>(&format!(
        "SELECT status FROM steep_repl.nodes WHERE node_id = '{}'",
        safe_node_id
    )).ok().flatten();

    rows.push((
        node_id,
        node_name,
        host_result,
        port_result,
        priority_result,
        is_coordinator,
        last_seen,
        status,
    ));

    TableIterator::new(rows)
}

/// T020: Update heartbeat timestamp for a node.
///
/// This function updates the last_seen timestamp and status of a node,
/// indicating that it is still alive and responding.
///
/// # Arguments
///
/// * `p_node_id` - The node ID to update
///
/// # Returns
///
/// Returns true if the node was found and updated, false otherwise.
#[pg_extern]
fn _steep_repl_heartbeat(p_node_id: &str) -> bool {
    let safe_node_id = escape_sql_string(p_node_id);

    // Check if node exists first
    let exists = Spi::get_one::<bool>(&format!(
        "SELECT EXISTS(SELECT 1 FROM steep_repl.nodes WHERE node_id = '{}')",
        safe_node_id
    )).ok().flatten().unwrap_or(false);

    if !exists {
        return false;
    }

    // Update the heartbeat
    let query = format!(
        "UPDATE steep_repl.nodes SET last_seen = now(), status = 'healthy' WHERE node_id = '{}'",
        safe_node_id
    );

    Spi::run(&query).is_ok()
}

/// T021: Get node status.
///
/// Returns the status of one or all nodes. A node is considered healthy
/// if its last_seen timestamp is within the last 30 seconds.
///
/// # Arguments
///
/// * `p_node_id` - Optional node ID to filter. If NULL, returns all nodes.
///
/// # Returns
///
/// Returns a table with node status information.
#[pg_extern]
fn _steep_repl_node_status(
    p_node_id: default!(Option<&str>, "NULL"),
) -> TableIterator<
    'static,
    (
        name!(node_id, Option<String>),
        name!(node_name, Option<String>),
        name!(status, Option<String>),
        name!(last_seen, Option<TimestampWithTimeZone>),
        name!(is_healthy, Option<bool>),
    ),
> {
    let query = if let Some(node_id) = p_node_id {
        let safe_node_id = escape_sql_string(node_id);
        format!(
            "SELECT
                n.node_id,
                n.node_name,
                n.status,
                n.last_seen,
                (n.last_seen > now() - interval '30 seconds') as is_healthy
            FROM steep_repl.nodes n
            WHERE n.node_id = '{}'
            ORDER BY n.priority DESC, n.node_name",
            safe_node_id
        )
    } else {
        "SELECT
            n.node_id,
            n.node_name,
            n.status,
            n.last_seen,
            (n.last_seen > now() - interval '30 seconds') as is_healthy
        FROM steep_repl.nodes n
        ORDER BY n.priority DESC, n.node_name".to_string()
    };

    Spi::connect(|client| {
        let result = client.select(&query, None, &[]);

        let mut rows = Vec::new();

        if let Ok(tup_table) = result {
            for row in tup_table {
                let node_id: Option<String> = row.get_by_name("node_id").ok().flatten();
                let node_name: Option<String> = row.get_by_name("node_name").ok().flatten();
                let status: Option<String> = row.get_by_name("status").ok().flatten();
                let last_seen: Option<TimestampWithTimeZone> = row.get_by_name("last_seen").ok().flatten();
                let is_healthy: Option<bool> = row.get_by_name("is_healthy").ok().flatten();

                rows.push((node_id, node_name, status, last_seen, is_healthy));
            }
        }

        TableIterator::new(rows)
    })
}

// Schema-qualified wrapper functions
extension_sql!(
    r#"
-- T019: Register or update a node (UPSERT semantics)
-- Required privilege: INSERT/UPDATE on steep_repl.nodes
CREATE FUNCTION steep_repl.register_node(
    p_node_id TEXT,
    p_node_name TEXT,
    p_host TEXT DEFAULT NULL,
    p_port INTEGER DEFAULT 5432,
    p_priority INTEGER DEFAULT 50
) RETURNS TABLE (
    node_id TEXT,
    node_name TEXT,
    host TEXT,
    port INTEGER,
    priority INTEGER,
    is_coordinator BOOLEAN,
    last_seen TIMESTAMPTZ,
    status TEXT
)
LANGUAGE sql
AS $$ SELECT * FROM _steep_repl_register_node(p_node_id, p_node_name, p_host, p_port, p_priority) $$;

COMMENT ON FUNCTION steep_repl.register_node(TEXT, TEXT, TEXT, INTEGER, INTEGER) IS
    'Register or update a node with UPSERT semantics. Status is set to healthy.';

-- T020: Update heartbeat timestamp
-- Required privilege: UPDATE on steep_repl.nodes
CREATE FUNCTION steep_repl.heartbeat(
    p_node_id TEXT
) RETURNS BOOLEAN
LANGUAGE sql
AS $$ SELECT _steep_repl_heartbeat(p_node_id) $$;

COMMENT ON FUNCTION steep_repl.heartbeat(TEXT) IS
    'Update last_seen timestamp and set status to healthy. Returns true if node exists.';

-- T021: Get node status
-- Required privilege: SELECT on steep_repl.nodes
CREATE FUNCTION steep_repl.node_status(
    p_node_id TEXT DEFAULT NULL
) RETURNS TABLE (
    node_id TEXT,
    node_name TEXT,
    status TEXT,
    last_seen TIMESTAMPTZ,
    is_healthy BOOLEAN
)
LANGUAGE sql STABLE
AS $$ SELECT * FROM _steep_repl_node_status(p_node_id) $$;

COMMENT ON FUNCTION steep_repl.node_status(TEXT) IS
    'Get node status. is_healthy is true if last_seen is within 30 seconds.';
"#,
    name = "create_node_functions",
    requires = ["create_nodes_table", _steep_repl_register_node, _steep_repl_heartbeat, _steep_repl_node_status],
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

    // =========================================================================
    // T019: register_node() function tests
    // =========================================================================

    #[pg_test]
    fn test_register_node_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'register_node'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "register_node function should exist");
    }

    #[pg_test]
    fn test_register_node_new_node() {
        // Register a new node
        let result = Spi::get_one::<String>(
            "SELECT node_id FROM steep_repl.register_node('test-node-1', 'Test Node 1', 'localhost', 5432, 50)"
        );
        assert_eq!(result, Ok(Some("test-node-1".to_string())), "register_node should return node_id");

        // Verify node was created
        let name = Spi::get_one::<String>(
            "SELECT node_name FROM steep_repl.nodes WHERE node_id = 'test-node-1'"
        );
        assert_eq!(name, Ok(Some("Test Node 1".to_string())));

        // Verify status is healthy
        let status = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.nodes WHERE node_id = 'test-node-1'"
        );
        assert_eq!(status, Ok(Some("healthy".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-1'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_register_node_update_existing() {
        // Register a new node
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-2', 'Original Name', 'host1', 5432, 50)"
        ).expect("first register should succeed");

        // Update the same node
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-2', 'Updated Name', 'host2', 5433, 80)"
        ).expect("second register should succeed");

        // Verify update applied
        let name = Spi::get_one::<String>(
            "SELECT node_name FROM steep_repl.nodes WHERE node_id = 'test-node-2'"
        );
        assert_eq!(name, Ok(Some("Updated Name".to_string())));

        let port = Spi::get_one::<i32>(
            "SELECT port FROM steep_repl.nodes WHERE node_id = 'test-node-2'"
        );
        assert_eq!(port, Ok(Some(5433)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-2'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_register_node_defaults() {
        // Register with defaults
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-3', 'Test Node 3')"
        ).expect("register with defaults should succeed");

        // Verify defaults applied
        let host = Spi::get_one::<String>(
            "SELECT host FROM steep_repl.nodes WHERE node_id = 'test-node-3'"
        );
        assert_eq!(host, Ok(Some("localhost".to_string())));

        let port = Spi::get_one::<i32>(
            "SELECT port FROM steep_repl.nodes WHERE node_id = 'test-node-3'"
        );
        assert_eq!(port, Ok(Some(5432)));

        let priority = Spi::get_one::<i32>(
            "SELECT priority FROM steep_repl.nodes WHERE node_id = 'test-node-3'"
        );
        assert_eq!(priority, Ok(Some(50)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-3'")
            .expect("cleanup should succeed");
    }

    // =========================================================================
    // T020: heartbeat() function tests
    // =========================================================================

    #[pg_test]
    fn test_heartbeat_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'heartbeat'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "heartbeat function should exist");
    }

    #[pg_test]
    fn test_heartbeat_updates_last_seen() {
        use pgrx::datum::TimestampWithTimeZone;

        // First create a node with an old last_seen timestamp
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, status, last_seen)
             VALUES ('test-node-4', 'Test Node 4', 'localhost', 'unknown', now() - interval '1 hour')"
        ).expect("insert should succeed");

        // Get initial last_seen
        let initial_time = Spi::get_one::<TimestampWithTimeZone>(
            "SELECT last_seen FROM steep_repl.nodes WHERE node_id = 'test-node-4'"
        ).expect("should get last_seen");

        // Heartbeat should update last_seen to now
        let result = Spi::get_one::<bool>("SELECT steep_repl.heartbeat('test-node-4')");
        assert_eq!(result, Ok(Some(true)), "heartbeat should return true for existing node");

        // Verify last_seen was updated (now it should be much more recent)
        let new_time = Spi::get_one::<TimestampWithTimeZone>(
            "SELECT last_seen FROM steep_repl.nodes WHERE node_id = 'test-node-4'"
        ).expect("should get last_seen");

        assert!(new_time != initial_time, "last_seen should be updated");

        // Also verify status was set to healthy
        let status = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.nodes WHERE node_id = 'test-node-4'"
        ).expect("should get status").unwrap_or_default();
        assert_eq!(status, "healthy", "status should be healthy after heartbeat");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-4'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_heartbeat_nonexistent_node() {
        let result = Spi::get_one::<bool>("SELECT steep_repl.heartbeat('nonexistent-node')");
        assert_eq!(result, Ok(Some(false)), "heartbeat should return false for nonexistent node");
    }

    // =========================================================================
    // T021: node_status() function tests
    // =========================================================================

    #[pg_test]
    fn test_node_status_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'node_status'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "node_status function should exist");
    }

    #[pg_test]
    fn test_node_status_single_node() {
        // Register a node
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-5', 'Test Node 5')"
        ).expect("register should succeed");

        // Get status
        let result = Spi::get_one::<String>(
            "SELECT node_name FROM steep_repl.node_status('test-node-5')"
        );
        assert_eq!(result, Ok(Some("Test Node 5".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-5'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_node_status_all_nodes() {
        // Register two nodes
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-6a', 'Node A', 'localhost', 5432, 80)"
        ).expect("register A should succeed");
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-6b', 'Node B', 'localhost', 5433, 60)"
        ).expect("register B should succeed");

        // Get all statuses (NULL for all)
        let count = Spi::get_one::<i64>(
            "SELECT COUNT(*) FROM steep_repl.node_status() WHERE node_id LIKE 'test-node-6%'"
        );
        assert_eq!(count, Ok(Some(2)), "should return both nodes");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id LIKE 'test-node-6%'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_node_status_is_healthy() {
        // Register a node (just registered, should be healthy)
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-7', 'Test Node 7')"
        ).expect("register should succeed");

        // Check is_healthy (should be true since last_seen is now)
        let is_healthy = Spi::get_one::<bool>(
            "SELECT is_healthy FROM steep_repl.node_status('test-node-7')"
        );
        assert_eq!(is_healthy, Ok(Some(true)), "recently registered node should be healthy");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-7'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_node_status_is_unhealthy_after_timeout() {
        // Register a node with old last_seen
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, status, last_seen)
             VALUES ('test-node-8', 'Test Node 8', 'localhost', 'healthy', now() - interval '1 minute')"
        ).expect("insert should succeed");

        // Check is_healthy (should be false since last_seen is > 30 seconds ago)
        let is_healthy = Spi::get_one::<bool>(
            "SELECT is_healthy FROM steep_repl.node_status('test-node-8')"
        );
        assert_eq!(is_healthy, Ok(Some(false)), "stale node should not be healthy");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-8'")
            .expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_node_status_order_by_priority() {
        // Register nodes with different priorities
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-9a', 'Low Priority', 'localhost', 5432, 20)"
        ).expect("register low should succeed");
        Spi::run(
            "SELECT * FROM steep_repl.register_node('test-node-9b', 'High Priority', 'localhost', 5433, 90)"
        ).expect("register high should succeed");

        // Get first node (should be high priority due to ORDER BY priority DESC)
        let first_name = Spi::get_one::<String>(
            "SELECT node_name FROM steep_repl.node_status() WHERE node_id LIKE 'test-node-9%' LIMIT 1"
        );
        assert_eq!(first_name, Ok(Some("High Priority".to_string())), "high priority node should be first");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id LIKE 'test-node-9%'")
            .expect("cleanup should succeed");
    }
}
