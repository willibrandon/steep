//! NOTIFY helper functions for steep_repl extension.
//!
//! This module provides helper functions for sending PostgreSQL NOTIFY
//! messages for real-time progress updates. CLI clients can LISTEN to
//! these channels to receive live progress without polling.
//!
//! T004: Create NOTIFY helper functions

use pgrx::prelude::*;

/// Channel name for progress notifications
pub const PROGRESS_CHANNEL: &str = "steep_repl_progress";

/// Channel name for work queue notifications (wake up worker)
pub const WORK_CHANNEL: &str = "steep_repl_work";

/// Send a NOTIFY with JSON payload.
/// This is a low-level function used by other modules.
pub fn send_notify(channel: &str, payload: &str) {
    // Escape single quotes in payload
    let escaped = payload.replace('\'', "''");
    let sql = format!("SELECT pg_notify('{}', '{}')", channel, escaped);

    if let Err(e) = Spi::run(&sql) {
        pgrx::warning!("Failed to send notification: {}", e);
    }
}

/// Send a progress notification with structured JSON payload.
pub fn notify_progress(
    operation_type: &str,
    operation_id: &str,
    phase: &str,
    percent: f32,
    tables_completed: i32,
    tables_total: i32,
    current_table: Option<&str>,
    bytes_processed: i64,
    eta_seconds: Option<i32>,
    error: Option<&str>,
) {
    let payload = format!(
        r#"{{"op":"{}","id":"{}","phase":"{}","percent":{:.1},"tables_completed":{},"tables_total":{},{}{}{}}}"#,
        operation_type,
        operation_id,
        phase,
        percent,
        tables_completed,
        tables_total,
        current_table.map(|t| format!(r#""table":"{}","#, t.replace('"', "\\\""))).unwrap_or_default(),
        format!(r#""bytes":{}"#, bytes_processed),
        match (eta_seconds, error) {
            (Some(eta), None) => format!(r#","eta":{}"#, eta),
            (None, Some(err)) => format!(r#","error":"{}""#, err.replace('"', "\\\"")),
            (Some(eta), Some(err)) => format!(r#","eta":{},"error":"{}""#, eta, err.replace('"', "\\\"")),
            (None, None) => String::new(),
        }
    );

    send_notify(PROGRESS_CHANNEL, &payload);
}

/// Send a simple status notification (e.g., "complete", "failed").
pub fn notify_status(operation_type: &str, operation_id: &str, status: &str, error: Option<&str>) {
    let payload = match error {
        Some(err) => format!(
            r#"{{"op":"{}","id":"{}","status":"{}","error":"{}"}}"#,
            operation_type,
            operation_id,
            status,
            err.replace('"', "\\\"")
        ),
        None => format!(
            r#"{{"op":"{}","id":"{}","status":"{}"}}"#,
            operation_type,
            operation_id,
            status
        ),
    };

    send_notify(PROGRESS_CHANNEL, &payload);
}

/// Notify that new work is available in the queue.
/// This wakes up the background worker if it's waiting on a latch.
pub fn notify_work_available() {
    send_notify(WORK_CHANNEL, "new_work");
}

// =============================================================================
// SQL Functions for Manual NOTIFY
// =============================================================================

/// Send a custom notification on the progress channel (internal implementation).
/// Useful for testing or manual progress updates.
#[pg_extern]
fn _steep_repl_send_progress_notify(payload: &str) {
    send_notify(PROGRESS_CHANNEL, payload);
}

/// Send a notification to wake up the background worker (internal implementation).
#[pg_extern]
fn _steep_repl_wake_worker() {
    notify_work_available();
}

// Schema-qualified wrapper functions for notify
extension_sql!(
    r#"
-- Wrapper functions in steep_repl schema
CREATE FUNCTION steep_repl.send_progress_notify(payload TEXT) RETURNS void
    LANGUAGE sql AS $$ SELECT _steep_repl_send_progress_notify(payload) $$;

CREATE FUNCTION steep_repl.wake_worker() RETURNS void
    LANGUAGE sql AS $$ SELECT _steep_repl_wake_worker() $$;

COMMENT ON FUNCTION steep_repl.send_progress_notify(TEXT) IS 'Send a custom notification on the progress channel';
COMMENT ON FUNCTION steep_repl.wake_worker() IS 'Send a notification to wake up the background worker';
"#,
    name = "create_notify_wrapper_functions",
    requires = ["create_schema", _steep_repl_send_progress_notify, _steep_repl_wake_worker],
);

// =============================================================================
// SQL Helper Functions
// =============================================================================

extension_sql!(
    r#"
-- Helper function to build and send progress notification
CREATE FUNCTION steep_repl.notify_operation_progress(
    p_operation_type TEXT,
    p_operation_id TEXT,
    p_phase TEXT,
    p_percent REAL,
    p_tables_completed INTEGER DEFAULT 0,
    p_tables_total INTEGER DEFAULT 0,
    p_current_table TEXT DEFAULT NULL,
    p_bytes_processed BIGINT DEFAULT 0,
    p_eta_seconds INTEGER DEFAULT NULL,
    p_error TEXT DEFAULT NULL
)
RETURNS VOID AS $$
DECLARE
    v_payload JSONB;
BEGIN
    v_payload := jsonb_build_object(
        'op', p_operation_type,
        'id', p_operation_id,
        'phase', p_phase,
        'percent', p_percent,
        'tables_completed', p_tables_completed,
        'tables_total', p_tables_total,
        'bytes', p_bytes_processed
    );

    IF p_current_table IS NOT NULL THEN
        v_payload := v_payload || jsonb_build_object('table', p_current_table);
    END IF;

    IF p_eta_seconds IS NOT NULL THEN
        v_payload := v_payload || jsonb_build_object('eta', p_eta_seconds);
    END IF;

    IF p_error IS NOT NULL THEN
        v_payload := v_payload || jsonb_build_object('error', p_error);
    END IF;

    PERFORM pg_notify('steep_repl_progress', v_payload::text);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.notify_operation_progress IS
    'Send a progress notification with structured JSON payload';

-- Helper function to send completion notification
CREATE FUNCTION steep_repl.notify_operation_complete(
    p_operation_type TEXT,
    p_operation_id TEXT
)
RETURNS VOID AS $$
BEGIN
    PERFORM pg_notify('steep_repl_progress', jsonb_build_object(
        'op', p_operation_type,
        'id', p_operation_id,
        'status', 'complete',
        'percent', 100
    )::text);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.notify_operation_complete IS
    'Send a completion notification for an operation';

-- Helper function to send failure notification
CREATE FUNCTION steep_repl.notify_operation_failed(
    p_operation_type TEXT,
    p_operation_id TEXT,
    p_error TEXT
)
RETURNS VOID AS $$
BEGIN
    PERFORM pg_notify('steep_repl_progress', jsonb_build_object(
        'op', p_operation_type,
        'id', p_operation_id,
        'status', 'failed',
        'error', p_error
    )::text);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.notify_operation_failed IS
    'Send a failure notification for an operation';
"#,
    name = "create_notify_functions",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_notify_operation_progress_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'notify_operation_progress'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "notify_operation_progress function should exist");
    }

    #[pg_test]
    fn test_notify_operation_complete_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'notify_operation_complete'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "notify_operation_complete function should exist");
    }

    #[pg_test]
    fn test_notify_operation_failed_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'notify_operation_failed'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "notify_operation_failed function should exist");
    }

    #[pg_test]
    fn test_send_progress_notify_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'send_progress_notify'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "send_progress_notify function should exist");
    }

    #[pg_test]
    fn test_wake_worker_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'wake_worker'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "wake_worker function should exist");
    }

    #[pg_test]
    fn test_send_progress_notify_works() {
        // Just test that it doesn't error - actual notification would need a listener
        let result = Spi::run(
            "SELECT steep_repl.send_progress_notify('{\"test\": true}')"
        );
        assert!(result.is_ok(), "send_progress_notify should not error");
    }

    #[pg_test]
    fn test_notify_operation_progress_works() {
        // Test the SQL function
        let result = Spi::run(
            "SELECT steep_repl.notify_operation_progress(
                'snapshot_generate',
                'test_snap_001',
                'data',
                45.5,
                5,
                10,
                'public.users',
                1048576,
                60,
                NULL
            )"
        );
        assert!(result.is_ok(), "notify_operation_progress should not error");
    }

    #[pg_test]
    fn test_notify_operation_complete_works() {
        let result = Spi::run(
            "SELECT steep_repl.notify_operation_complete('snapshot_generate', 'test_snap_001')"
        );
        assert!(result.is_ok(), "notify_operation_complete should not error");
    }

    #[pg_test]
    fn test_notify_operation_failed_works() {
        let result = Spi::run(
            "SELECT steep_repl.notify_operation_failed('snapshot_generate', 'test_snap_001', 'Connection failed')"
        );
        assert!(result.is_ok(), "notify_operation_failed should not error");
    }

    #[pg_test]
    fn test_wake_worker_works() {
        let result = Spi::run(
            "SELECT steep_repl.wake_worker()"
        );
        assert!(result.is_ok(), "wake_worker should not error");
    }
}
