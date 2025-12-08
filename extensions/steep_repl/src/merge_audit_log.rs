//! Merge audit log and operations tables for steep_repl extension.
//!
//! This module creates:
//! - steep_repl.merge_operations: Operation-level tracking for background worker merges
//! - steep_repl.merge_audit_log: Per-row audit log for compliance and debugging
//!
//! T067c: Add steep_repl.merge_audit_log table
//! T006: Extend merge_audit_log table with progress columns

use pgrx::prelude::*;

// =============================================================================
// Merge Operations Table (T006)
// =============================================================================
// Operation-level tracking for background worker merges.

extension_sql!(
    r#"
-- =============================================================================
-- Merge Operations Table (T006)
-- =============================================================================
-- Tracks bidirectional merge operations for background worker processing.
-- Similar to snapshots table but for merge operations.

CREATE TABLE steep_repl.merge_operations (
    -- Primary identifier
    merge_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Operation configuration
    tables          TEXT[] NOT NULL,
    strategy        TEXT NOT NULL DEFAULT 'prefer-local',
    peer_connstr    TEXT NOT NULL,          -- Connection string to peer (redacted in logs)
    dry_run         BOOLEAN NOT NULL DEFAULT false,

    -- Status tracking
    status          TEXT NOT NULL DEFAULT 'pending',
    phase           TEXT NOT NULL DEFAULT 'idle',
    error_message   TEXT,

    -- Progress tracking
    overall_percent REAL NOT NULL DEFAULT 0,
    current_table   TEXT,
    tables_total    INTEGER NOT NULL DEFAULT 0,
    tables_completed INTEGER NOT NULL DEFAULT 0,
    local_only_count BIGINT NOT NULL DEFAULT 0,
    remote_only_count BIGINT NOT NULL DEFAULT 0,
    match_count     BIGINT NOT NULL DEFAULT 0,
    conflict_count  BIGINT NOT NULL DEFAULT 0,
    rows_merged     BIGINT NOT NULL DEFAULT 0,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT merge_operations_strategy_check CHECK (
        strategy IN ('prefer-local', 'prefer-remote', 'last-modified')
    ),
    CONSTRAINT merge_operations_status_check CHECK (
        status IN ('pending', 'running', 'complete', 'failed', 'cancelled')
    ),
    CONSTRAINT merge_operations_phase_check CHECK (
        phase IN ('idle', 'analyzing', 'merging', 'verifying', 'finalizing')
    ),
    CONSTRAINT merge_operations_percent_check CHECK (
        overall_percent >= 0 AND overall_percent <= 100
    )
);

-- Indexes for merge operations
CREATE INDEX merge_operations_status_idx ON steep_repl.merge_operations (status);
CREATE INDEX merge_operations_active_idx ON steep_repl.merge_operations (status)
    WHERE status IN ('pending', 'running');
CREATE INDEX merge_operations_created_idx ON steep_repl.merge_operations (created_at);

-- Comments
COMMENT ON TABLE steep_repl.merge_operations IS
    'Bidirectional merge operations tracked for background worker processing';

COMMENT ON COLUMN steep_repl.merge_operations.merge_id IS 'Unique merge operation identifier';
COMMENT ON COLUMN steep_repl.merge_operations.tables IS 'Array of tables being merged';
COMMENT ON COLUMN steep_repl.merge_operations.strategy IS 'Conflict resolution: prefer-local, prefer-remote, last-modified';
COMMENT ON COLUMN steep_repl.merge_operations.peer_connstr IS 'Connection string to peer database (store redacted version for logging)';
COMMENT ON COLUMN steep_repl.merge_operations.dry_run IS 'If true, analyze only without applying changes';
COMMENT ON COLUMN steep_repl.merge_operations.status IS 'Operation status: pending, running, complete, failed, cancelled';
COMMENT ON COLUMN steep_repl.merge_operations.phase IS 'Current phase: idle, analyzing, merging, verifying, finalizing';
COMMENT ON COLUMN steep_repl.merge_operations.error_message IS 'Error details if status is failed';
COMMENT ON COLUMN steep_repl.merge_operations.overall_percent IS 'Overall completion percentage (0-100)';
COMMENT ON COLUMN steep_repl.merge_operations.current_table IS 'Table currently being processed';
COMMENT ON COLUMN steep_repl.merge_operations.tables_total IS 'Total number of tables to merge';
COMMENT ON COLUMN steep_repl.merge_operations.tables_completed IS 'Number of tables completed';
COMMENT ON COLUMN steep_repl.merge_operations.local_only_count IS 'Rows existing only on local';
COMMENT ON COLUMN steep_repl.merge_operations.remote_only_count IS 'Rows existing only on remote';
COMMENT ON COLUMN steep_repl.merge_operations.match_count IS 'Rows that match on both sides';
COMMENT ON COLUMN steep_repl.merge_operations.conflict_count IS 'Rows with conflicting values';
COMMENT ON COLUMN steep_repl.merge_operations.rows_merged IS 'Total rows actually merged';
COMMENT ON COLUMN steep_repl.merge_operations.created_at IS 'When merge was queued';
COMMENT ON COLUMN steep_repl.merge_operations.started_at IS 'When merge started processing';
COMMENT ON COLUMN steep_repl.merge_operations.completed_at IS 'When merge completed/failed';

-- LISTEN/NOTIFY for real-time updates
CREATE OR REPLACE FUNCTION steep_repl.notify_merge_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('steep_repl_merges', json_build_object(
        'merge_id', NEW.merge_id,
        'status', NEW.status,
        'phase', NEW.phase,
        'overall_percent', NEW.overall_percent
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER merge_notify
AFTER INSERT OR UPDATE ON steep_repl.merge_operations
FOR EACH ROW EXECUTE FUNCTION steep_repl.notify_merge_change();

COMMENT ON FUNCTION steep_repl.notify_merge_change() IS
    'Sends notification on merge operation changes for real-time CLI updates';
"#,
    name = "create_merge_operations_table",
    requires = ["create_schema"],
);

// =============================================================================
// Merge Audit Log Table (T067c)
// =============================================================================
// Per-row audit log for compliance and debugging.

extension_sql!(
    r#"
-- =============================================================================
-- Merge Audit Log Table (T067c)
-- =============================================================================
-- Immutable log of all merge decisions for compliance and debugging.
-- Each row records one row's fate during a bidirectional merge operation.

CREATE TABLE steep_repl.merge_audit_log (
    -- Primary identifier
    id              BIGSERIAL PRIMARY KEY,

    -- Merge operation grouping
    merge_id        UUID NOT NULL REFERENCES steep_repl.merge_operations(merge_id),

    -- Table identification
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,

    -- Row identification
    pk_value        JSONB NOT NULL,          -- The PK of the affected row (e.g., {"id": 1})

    -- Classification
    category        TEXT NOT NULL CHECK (category IN ('match', 'conflict', 'local_only', 'remote_only')),

    -- Resolution (only for conflicts and transfers)
    resolution      TEXT CHECK (resolution IS NULL OR resolution IN ('kept_a', 'kept_b', 'skipped')),

    -- Full row values for debugging
    node_a_value    JSONB,                   -- Full row from Node A (NULL if remote_only)
    node_b_value    JSONB,                   -- Full row from Node B (NULL if local_only)

    -- Metadata
    resolved_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_by     TEXT                     -- e.g., 'strategy:prefer-node-a', 'strategy:last-modified', 'manual'
);

-- Indexes for efficient querying
CREATE INDEX merge_audit_log_merge_id_idx ON steep_repl.merge_audit_log (merge_id);
CREATE INDEX merge_audit_log_table_idx ON steep_repl.merge_audit_log (table_schema, table_name);
CREATE INDEX merge_audit_log_resolved_at_idx ON steep_repl.merge_audit_log (resolved_at);
CREATE INDEX merge_audit_log_category_idx ON steep_repl.merge_audit_log (category);

COMMENT ON TABLE steep_repl.merge_audit_log IS
    'Audit trail of all bidirectional merge decisions. Every row involved in a merge is logged.';

COMMENT ON COLUMN steep_repl.merge_audit_log.merge_id IS
    'UUID grouping all rows from one merge operation';
COMMENT ON COLUMN steep_repl.merge_audit_log.pk_value IS
    'Primary key value(s) as JSONB, e.g., {"id": 1} or {"order_id": 1, "item_id": 2}';
COMMENT ON COLUMN steep_repl.merge_audit_log.category IS
    'Row category: match (identical), conflict (different), local_only (A), remote_only (B)';
COMMENT ON COLUMN steep_repl.merge_audit_log.resolution IS
    'How conflict was resolved: kept_a, kept_b, or skipped';
COMMENT ON COLUMN steep_repl.merge_audit_log.node_a_value IS
    'Full row data from Node A as JSONB (NULL if row only exists on B)';
COMMENT ON COLUMN steep_repl.merge_audit_log.node_b_value IS
    'Full row data from Node B as JSONB (NULL if row only exists on A)';
COMMENT ON COLUMN steep_repl.merge_audit_log.resolved_by IS
    'Resolution method, e.g., strategy:prefer-node-a, strategy:last-modified, manual';

-- =============================================================================
-- Merge Audit Helper Functions
-- =============================================================================

-- Log a merge decision
CREATE FUNCTION steep_repl.log_merge_decision(
    p_merge_id UUID,
    p_table_schema TEXT,
    p_table_name TEXT,
    p_pk_value JSONB,
    p_category TEXT,
    p_resolution TEXT DEFAULT NULL,
    p_node_a_value JSONB DEFAULT NULL,
    p_node_b_value JSONB DEFAULT NULL,
    p_resolved_by TEXT DEFAULT NULL
)
RETURNS BIGINT AS $$
    INSERT INTO steep_repl.merge_audit_log (
        merge_id, table_schema, table_name, pk_value,
        category, resolution, node_a_value, node_b_value, resolved_by
    ) VALUES (
        p_merge_id, p_table_schema, p_table_name, p_pk_value,
        p_category, p_resolution, p_node_a_value, p_node_b_value, p_resolved_by
    )
    RETURNING id;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.log_merge_decision IS
    'Log a single merge decision to the audit log. Returns the audit log entry ID.';

-- Get merge summary
CREATE FUNCTION steep_repl.get_merge_summary(p_merge_id UUID)
RETURNS TABLE (
    category TEXT,
    resolution TEXT,
    count BIGINT
) AS $$
    SELECT
        category,
        resolution,
        count(*)::BIGINT
    FROM steep_repl.merge_audit_log
    WHERE merge_id = p_merge_id
    GROUP BY category, resolution
    ORDER BY category, resolution;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION steep_repl.get_merge_summary IS
    'Get summary statistics for a merge operation by category and resolution.';

-- Get conflicts for a merge
CREATE FUNCTION steep_repl.get_merge_conflicts(p_merge_id UUID)
RETURNS SETOF steep_repl.merge_audit_log AS $$
    SELECT *
    FROM steep_repl.merge_audit_log
    WHERE merge_id = p_merge_id AND category = 'conflict'
    ORDER BY table_schema, table_name, id;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION steep_repl.get_merge_conflicts IS
    'Get all conflict records for a merge operation.';

-- Prune old merge audit logs
CREATE FUNCTION steep_repl.prune_merge_audit_log(p_older_than INTERVAL)
RETURNS BIGINT AS $$
DECLARE
    v_deleted BIGINT;
BEGIN
    DELETE FROM steep_repl.merge_audit_log
    WHERE resolved_at < now() - p_older_than;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.prune_merge_audit_log IS
    'Delete merge audit log entries older than the specified interval. Returns count of deleted rows.';
"#,
    name = "create_merge_audit_log_table",
    requires = ["create_merge_operations_table"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    // =============================================================================
    // Merge Operations Table Tests
    // =============================================================================

    #[pg_test]
    fn test_merge_operations_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'merge_operations'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "merge_operations table should exist");
    }

    #[pg_test]
    fn test_merge_operations_columns() {
        let result = Spi::get_one::<i64>(
            "SELECT count(*) FROM information_schema.columns
             WHERE table_schema = 'steep_repl' AND table_name = 'merge_operations'"
        );
        assert_eq!(result, Ok(Some(20)), "merge_operations should have 20 columns");
    }

    #[pg_test]
    fn test_merge_operations_insert_minimal() {
        // Insert with minimal required fields
        Spi::run(
            "INSERT INTO steep_repl.merge_operations (tables, peer_connstr)
             VALUES (ARRAY['public.users'], 'host=peer port=5432')"
        ).expect("insert should succeed");

        // Verify defaults
        let status = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.merge_operations
             WHERE peer_connstr = 'host=peer port=5432'"
        );
        assert_eq!(status, Ok(Some("pending".to_string())));

        let phase = Spi::get_one::<String>(
            "SELECT phase FROM steep_repl.merge_operations
             WHERE peer_connstr = 'host=peer port=5432'"
        );
        assert_eq!(phase, Ok(Some("idle".to_string())));

        // Cleanup
        Spi::run(
            "DELETE FROM steep_repl.merge_operations
             WHERE peer_connstr = 'host=peer port=5432'"
        ).expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_merge_operations_progress_tracking() {
        // Insert with progress data
        Spi::run(
            "INSERT INTO steep_repl.merge_operations (
                tables, peer_connstr, status, phase,
                overall_percent, tables_total, tables_completed,
                local_only_count, remote_only_count, match_count, conflict_count
             ) VALUES (
                ARRAY['public.users', 'public.orders'],
                'host=peer port=5432 dbname=testdb',
                'running', 'merging',
                50.0, 2, 1,
                100, 50, 1000, 5
             )"
        ).expect("insert should succeed");

        let percent = Spi::get_one::<f32>(
            "SELECT overall_percent FROM steep_repl.merge_operations
             WHERE status = 'running' AND phase = 'merging'"
        );
        assert_eq!(percent, Ok(Some(50.0)));

        let conflicts = Spi::get_one::<i64>(
            "SELECT conflict_count FROM steep_repl.merge_operations
             WHERE status = 'running' AND phase = 'merging'"
        );
        assert_eq!(conflicts, Ok(Some(5)));

        // Cleanup
        Spi::run(
            "DELETE FROM steep_repl.merge_operations
             WHERE peer_connstr = 'host=peer port=5432 dbname=testdb'"
        ).expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_merge_operations_status_constraint() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'merge_operations'
                AND c.conname = 'merge_operations_status_check'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "status check constraint should exist");
    }

    #[pg_test]
    fn test_merge_operations_indexes() {
        let indexes = vec![
            "merge_operations_status_idx",
            "merge_operations_active_idx",
            "merge_operations_created_idx",
        ];

        for idx_name in indexes {
            let result = Spi::get_one::<bool>(&format!(
                "SELECT EXISTS(
                    SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'steep_repl' AND indexname = '{}'
                )",
                idx_name
            ));
            assert_eq!(result, Ok(Some(true)), "index {} should exist", idx_name);
        }
    }

    #[pg_test]
    fn test_merge_operations_notify_trigger() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_trigger t
                JOIN pg_class c ON t.tgrelid = c.oid
                JOIN pg_namespace n ON c.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND c.relname = 'merge_operations'
                AND t.tgname = 'merge_notify'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "merge_notify trigger should exist");
    }

    // =============================================================================
    // Merge Audit Log Table Tests
    // =============================================================================

    #[pg_test]
    fn test_merge_audit_log_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'merge_audit_log'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "merge_audit_log table should exist");
    }

    #[pg_test]
    fn test_merge_audit_log_columns() {
        let result = Spi::get_one::<i64>(
            "SELECT count(*) FROM information_schema.columns
             WHERE table_schema = 'steep_repl' AND table_name = 'merge_audit_log'"
        );
        assert_eq!(result, Ok(Some(11)), "merge_audit_log should have 11 columns");
    }

    #[pg_test]
    fn test_log_merge_decision_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'log_merge_decision'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "log_merge_decision function should exist");
    }

    #[pg_test]
    fn test_get_merge_summary_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'get_merge_summary'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "get_merge_summary function should exist");
    }

    #[pg_test]
    fn test_get_merge_conflicts_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'get_merge_conflicts'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "get_merge_conflicts function should exist");
    }

    #[pg_test]
    fn test_prune_merge_audit_log_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'prune_merge_audit_log'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "prune_merge_audit_log function should exist");
    }

    #[pg_test]
    fn test_log_merge_decision_inserts_row() {
        // First create a merge_operation (required by FK)
        let merge_id = Spi::get_one::<pgrx::Uuid>(
            "INSERT INTO steep_repl.merge_operations (tables, peer_connstr)
             VALUES (ARRAY['public.test_table'], 'host=test port=5432')
             RETURNING merge_id"
        ).expect("create merge_operation").unwrap();

        // Log a decision
        let result = Spi::get_one::<i64>(&format!(
            "SELECT steep_repl.log_merge_decision(
                '{}'::uuid,
                'public',
                'test_table',
                '{{\"id\": 1}}'::jsonb,
                'conflict',
                'kept_a',
                '{{\"id\": 1, \"name\": \"alice\"}}'::jsonb,
                '{{\"id\": 1, \"name\": \"bob\"}}'::jsonb,
                'strategy:prefer-node-a'
            )",
            merge_id
        ));

        match result {
            Ok(Some(id)) => {
                assert!(id > 0, "should return positive ID");
            }
            _ => panic!("log_merge_decision should return an ID"),
        }

        // Verify it's stored
        let count = Spi::get_one::<i64>(&format!(
            "SELECT count(*) FROM steep_repl.merge_audit_log WHERE merge_id = '{}'",
            merge_id
        ));
        assert_eq!(count, Ok(Some(1)), "should have 1 audit log entry");

        // Cleanup (audit log first due to FK)
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_audit_log WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup audit log should succeed");
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_operations WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup merge_operation should succeed");
    }

    #[pg_test]
    fn test_get_merge_summary_returns_correct_counts() {
        // First create a merge_operation (required by FK)
        let merge_id = Spi::get_one::<pgrx::Uuid>(
            "INSERT INTO steep_repl.merge_operations (tables, peer_connstr)
             VALUES (ARRAY['public.t'], 'host=test2 port=5432')
             RETURNING merge_id"
        ).expect("create merge_operation").unwrap();

        // Log multiple decisions
        Spi::run(&format!(
            "SELECT steep_repl.log_merge_decision('{}'::uuid, 'public', 't', '{{\"id\": 1}}'::jsonb, 'match', NULL, NULL, NULL, NULL)",
            merge_id
        )).expect("log match 1");
        Spi::run(&format!(
            "SELECT steep_repl.log_merge_decision('{}'::uuid, 'public', 't', '{{\"id\": 2}}'::jsonb, 'match', NULL, NULL, NULL, NULL)",
            merge_id
        )).expect("log match 2");
        Spi::run(&format!(
            "SELECT steep_repl.log_merge_decision('{}'::uuid, 'public', 't', '{{\"id\": 3}}'::jsonb, 'conflict', 'kept_a', NULL, NULL, NULL)",
            merge_id
        )).expect("log conflict");

        // Get summary
        let match_count = Spi::get_one::<i64>(&format!(
            "SELECT count FROM steep_repl.get_merge_summary('{}') WHERE category = 'match'",
            merge_id
        ));
        assert_eq!(match_count, Ok(Some(2)), "should have 2 matches");

        let conflict_count = Spi::get_one::<i64>(&format!(
            "SELECT count FROM steep_repl.get_merge_summary('{}') WHERE category = 'conflict'",
            merge_id
        ));
        assert_eq!(conflict_count, Ok(Some(1)), "should have 1 conflict");

        // Cleanup (audit log first due to FK)
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_audit_log WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup audit log should succeed");
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_operations WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup merge_operation should succeed");
    }

    #[pg_test]
    fn test_merge_audit_log_indexes() {
        // Check that all expected indexes exist
        let indexes = vec![
            "merge_audit_log_merge_id_idx",
            "merge_audit_log_table_idx",
            "merge_audit_log_resolved_at_idx",
            "merge_audit_log_category_idx",
        ];

        for idx_name in indexes {
            let result = Spi::get_one::<bool>(&format!(
                "SELECT EXISTS(
                    SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'steep_repl' AND indexname = '{}'
                )",
                idx_name
            ));
            assert_eq!(result, Ok(Some(true)), "index {} should exist", idx_name);
        }
    }

    #[pg_test]
    fn test_merge_audit_log_fk_to_operations() {
        // Verify the FK constraint exists
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_constraint c
                JOIN pg_class r ON c.conrelid = r.oid
                JOIN pg_namespace n ON r.relnamespace = n.oid
                WHERE n.nspname = 'steep_repl'
                AND r.relname = 'merge_audit_log'
                AND c.contype = 'f'
                AND c.conname LIKE '%merge_id%'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "FK constraint to merge_operations should exist");
    }
}
