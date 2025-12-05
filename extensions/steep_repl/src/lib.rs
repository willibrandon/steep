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
    -- Initialization state tracking (015-node-init)
    init_state TEXT NOT NULL DEFAULT 'uninitialized',
    init_source_node TEXT REFERENCES steep_repl.nodes(node_id),
    init_started_at TIMESTAMPTZ,
    init_completed_at TIMESTAMPTZ,
    CONSTRAINT nodes_priority_check CHECK (priority >= 1 AND priority <= 100),
    CONSTRAINT nodes_port_check CHECK (port >= 1 AND port <= 65535),
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
// Init Progress Table (T006)
// =============================================================================

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
    CONSTRAINT progress_phase_check CHECK (phase IN ('generation', 'application', 'catching_up')),
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
COMMENT ON COLUMN steep_repl.init_progress.phase IS 'Current phase: generation, application, catching_up';
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

// =============================================================================
// Schema Fingerprints Table (T007)
// =============================================================================

extension_sql!(
    r#"
-- Schema fingerprints table: Schema fingerprints for drift detection
CREATE TABLE steep_repl.schema_fingerprints (
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    column_count INTEGER NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    column_definitions JSONB,
    PRIMARY KEY (table_schema, table_name),
    CONSTRAINT fingerprints_column_count_check CHECK (column_count >= 0)
);

COMMENT ON TABLE steep_repl.schema_fingerprints IS 'Schema fingerprints for drift detection';
COMMENT ON COLUMN steep_repl.schema_fingerprints.table_schema IS 'PostgreSQL schema name';
COMMENT ON COLUMN steep_repl.schema_fingerprints.table_name IS 'Table name';
COMMENT ON COLUMN steep_repl.schema_fingerprints.fingerprint IS 'SHA256 hash of column definitions';
COMMENT ON COLUMN steep_repl.schema_fingerprints.column_count IS 'Number of columns';
COMMENT ON COLUMN steep_repl.schema_fingerprints.captured_at IS 'When fingerprint was computed';
COMMENT ON COLUMN steep_repl.schema_fingerprints.column_definitions IS 'Detailed column info for diff';

-- Index for fingerprint queries
CREATE INDEX idx_fingerprints_captured ON steep_repl.schema_fingerprints(captured_at);
"#,
    name = "create_schema_fingerprints_table",
    requires = ["create_schema"],
);

// =============================================================================
// Init Slots Table (T008)
// =============================================================================

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

// =============================================================================
// Snapshots Table (T009)
// =============================================================================

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

// =============================================================================
// Schema Fingerprint Functions (T010)
// =============================================================================

extension_sql!(
    r#"
-- Compute fingerprint for a single table
-- Returns SHA256 hash of column definitions (name, type, default, nullable) in ordinal order
CREATE FUNCTION steep_repl.compute_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' ||
        coalesce(column_default, 'NULL') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION steep_repl.compute_fingerprint(TEXT, TEXT) IS 'Compute SHA256 fingerprint of table column definitions';

-- Capture fingerprint for a table (insert or update)
CREATE FUNCTION steep_repl.capture_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS steep_repl.schema_fingerprints AS $$
    INSERT INTO steep_repl.schema_fingerprints (table_schema, table_name, fingerprint, column_count, column_definitions)
    SELECT
        p_schema,
        p_table,
        steep_repl.compute_fingerprint(p_schema, p_table),
        count(*)::integer,
        jsonb_agg(jsonb_build_object(
            'name', column_name,
            'type', data_type,
            'default', column_default,
            'nullable', is_nullable,
            'position', ordinal_position
        ) ORDER BY ordinal_position)
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table
    GROUP BY 1, 2
    ON CONFLICT (table_schema, table_name) DO UPDATE SET
        fingerprint = EXCLUDED.fingerprint,
        column_count = EXCLUDED.column_count,
        column_definitions = EXCLUDED.column_definitions,
        captured_at = now()
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.capture_fingerprint(TEXT, TEXT) IS 'Capture and store schema fingerprint for a table';

-- Capture all user tables
CREATE FUNCTION steep_repl.capture_all_fingerprints()
RETURNS INTEGER AS $$
DECLARE
    v_count INTEGER := 0;
    rec RECORD;
BEGIN
    FOR rec IN
        SELECT schemaname, tablename
        FROM pg_tables
        WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
    LOOP
        PERFORM steep_repl.capture_fingerprint(rec.schemaname, rec.tablename);
        v_count := v_count + 1;
    END LOOP;
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.capture_all_fingerprints() IS 'Capture fingerprints for all user tables';
"#,
    name = "create_fingerprint_functions",
    requires = ["create_schema_fingerprints_table"],
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
            ("init_state", "text"),
            ("init_source_node", "text"),
            ("init_started_at", "timestamp with time zone"),
            ("init_completed_at", "timestamp with time zone"),
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

    // =========================================================================
    // Init Progress Table Tests (T006)
    // =========================================================================

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
             VALUES ('test-node-progress', 'application', 50.0, 10)"
        ).expect("progress insert should succeed");

        let result = Spi::get_one::<f32>(
            "SELECT overall_percent FROM steep_repl.init_progress WHERE node_id = 'test-node-progress'"
        );
        assert_eq!(result, Ok(Some(50.0)));

        // Cleanup (cascade will delete progress)
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-progress'")
            .expect("cleanup should succeed");
    }

    // =========================================================================
    // Schema Fingerprints Table Tests (T007)
    // =========================================================================

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
            "INSERT INTO steep_repl.schema_fingerprints (table_schema, table_name, fingerprint, column_count)
             VALUES ('public', 'test_table', 'abc123def456', 5)"
        ).expect("fingerprint insert should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT column_count FROM steep_repl.schema_fingerprints
             WHERE table_schema = 'public' AND table_name = 'test_table'"
        );
        assert_eq!(result, Ok(Some(5)));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE table_schema = 'public' AND table_name = 'test_table'")
            .expect("cleanup should succeed");
    }

    // =========================================================================
    // Init Slots Table Tests (T008)
    // =========================================================================

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

    // =========================================================================
    // Snapshots Table Tests (T009)
    // =========================================================================

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

    // =========================================================================
    // Fingerprint Functions Tests (T010)
    // =========================================================================

    #[pg_test]
    fn test_compute_fingerprint_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'compute_fingerprint'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compute_fingerprint function should exist");
    }

    #[pg_test]
    fn test_capture_fingerprint_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'capture_fingerprint'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "capture_fingerprint function should exist");
    }

    #[pg_test]
    fn test_capture_all_fingerprints_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'capture_all_fingerprints'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "capture_all_fingerprints function should exist");
    }

    #[pg_test]
    fn test_compute_fingerprint_returns_hex() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_fp_table (id INT, name TEXT)").expect("create test table");

        // Compute fingerprint
        let result = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_fp_table')"
        );

        // Should return a hex string (SHA256 = 64 hex chars)
        match result {
            Ok(Some(fp)) => {
                assert_eq!(fp.len(), 64, "fingerprint should be 64 hex characters");
                assert!(fp.chars().all(|c| c.is_ascii_hexdigit()), "fingerprint should be hex");
            }
            _ => panic!("compute_fingerprint should return a string"),
        }

        // Cleanup
        Spi::run("DROP TABLE IF EXISTS public.test_fp_table").expect("cleanup test table");
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
