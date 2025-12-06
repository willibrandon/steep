//! Audit log table for steep_repl extension.
//!
//! This module creates the audit_log table for an immutable record
//! of system activity with full before/after state capture.

use pgrx::prelude::*;

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

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

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
}
