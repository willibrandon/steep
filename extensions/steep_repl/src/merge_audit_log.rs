//! Merge audit log table for steep_repl extension.
//!
//! This module creates the steep_repl.merge_audit_log table that tracks
//! all decisions made during bidirectional merge operations. Every row
//! involved in a merge is logged with its category (match, conflict,
//! local_only, remote_only) and resolution (kept_a, kept_b, skipped).
//!
//! T067c: Add steep_repl.merge_audit_log table

use pgrx::prelude::*;

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
    merge_id        UUID NOT NULL,           -- Groups all rows from one merge operation

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
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

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
        // Generate a test UUID
        let merge_id = Spi::get_one::<pgrx::Uuid>(
            "SELECT gen_random_uuid()"
        ).expect("generate uuid").unwrap();

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

        // Cleanup
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_audit_log WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_get_merge_summary_returns_correct_counts() {
        // Generate a test UUID
        let merge_id = Spi::get_one::<pgrx::Uuid>(
            "SELECT gen_random_uuid()"
        ).expect("generate uuid").unwrap();

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

        // Cleanup
        Spi::run(&format!(
            "DELETE FROM steep_repl.merge_audit_log WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup should succeed");
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
}
