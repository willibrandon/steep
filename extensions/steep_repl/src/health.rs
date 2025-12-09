//! Health check functions for steep_repl extension.
//!
//! This module provides health check functionality for monitoring the
//! extension state. It's an alternative to HTTP health endpoints,
//! allowing clients to check health via SQL.
//!
//! T011: Create health.rs with steep_repl.health() function

use pgrx::prelude::*;

/// Health status enum
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HealthStatus {
    /// All systems operational
    Healthy,
    /// Some issues but operational
    Degraded,
    /// Critical issues, not fully operational
    Unhealthy,
}

impl HealthStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            HealthStatus::Healthy => "healthy",
            HealthStatus::Degraded => "degraded",
            HealthStatus::Unhealthy => "unhealthy",
        }
    }
}

/// Check if the background worker is running.
///
/// This queries pg_stat_activity for our background worker.
/// Note: backend_type shows bgw_type (not literal "background worker"),
/// and for our worker bgw_type = bgw_name = "steep_repl_worker".
fn is_bgworker_running() -> bool {
    // Check for our worker by backend_type (which equals bgw_type = "steep_repl_worker")
    let result = Spi::get_one::<bool>(
        "SELECT EXISTS(
            SELECT 1 FROM pg_stat_activity
            WHERE backend_type LIKE 'steep_repl%'
        )"
    );
    result.ok().flatten().unwrap_or(false)
}

/// Check if shared memory is available (extension loaded via shared_preload_libraries).
fn is_shmem_available() -> bool {
    // Try to access shared memory - if it works, it's available
    // We read from progress which uses PgLwLock
    let _snapshot = crate::progress::get_progress_snapshot();
    true // If we get here without panic, shared memory is available
}

/// Count active operations in the work queue.
fn count_active_operations() -> i32 {
    let result = Spi::get_one::<i64>(
        "SELECT COUNT(*) FROM steep_repl.work_queue WHERE status = 'processing'"
    );
    result.ok().flatten().unwrap_or(0) as i32
}

/// Get the last error from shared memory progress (if any).
fn get_last_error() -> Option<String> {
    let progress = crate::progress::get_progress_snapshot();
    if progress.phase == crate::progress::ProgressPhase::Failed as i32 {
        let error = progress.get_error_message();
        if error.is_empty() {
            None
        } else {
            Some(error)
        }
    } else {
        None
    }
}

/// Determine overall health status based on component states.
fn determine_health_status(
    bgworker_running: bool,
    shmem_available: bool,
    _active_operations: i32,
    last_error: &Option<String>,
) -> HealthStatus {
    // Critical: shared memory must be available
    if !shmem_available {
        return HealthStatus::Unhealthy;
    }

    // Degraded if background worker not running (extension not in shared_preload_libraries)
    if !bgworker_running {
        return HealthStatus::Degraded;
    }

    // Degraded if there's a recent error
    if last_error.is_some() {
        return HealthStatus::Degraded;
    }

    HealthStatus::Healthy
}

// =============================================================================
// SQL Functions
// =============================================================================

/// Internal implementation of health check.
/// Returns a set of health status fields.
#[pg_extern]
fn _steep_repl_health() -> TableIterator<
    'static,
    (
        name!(status, Option<String>),
        name!(extension_version, Option<String>),
        name!(pg_version, Option<String>),
        name!(background_worker_running, Option<bool>),
        name!(shared_memory_available, Option<bool>),
        name!(active_operations, Option<i32>),
        name!(last_error, Option<String>),
    ),
> {
    let bgworker_running = is_bgworker_running();
    let shmem_available = is_shmem_available();
    let active_operations = count_active_operations();
    let last_error = get_last_error();

    let status = determine_health_status(
        bgworker_running,
        shmem_available,
        active_operations,
        &last_error,
    );

    // Get PostgreSQL version
    let pg_version: Option<String> = Spi::get_one::<String>("SELECT version()")
        .ok()
        .flatten();

    // Get extension version
    let ext_version: Option<String> = Some(crate::utils::steep_repl_version().to_string());

    let row = (
        Some(status.as_str().to_string()),
        ext_version,
        pg_version,
        Some(bgworker_running),
        Some(shmem_available),
        Some(active_operations),
        last_error,
    );

    TableIterator::new(vec![row])
}

// Schema-qualified wrapper function
extension_sql!(
    r#"
-- Health check function in steep_repl schema
CREATE FUNCTION steep_repl.health()
RETURNS TABLE (
    status TEXT,
    extension_version TEXT,
    pg_version TEXT,
    background_worker_running BOOLEAN,
    shared_memory_available BOOLEAN,
    active_operations INTEGER,
    last_error TEXT
)
LANGUAGE sql STABLE
AS $$ SELECT * FROM _steep_repl_health() $$;

COMMENT ON FUNCTION steep_repl.health() IS
    'Health check function returning extension status (healthy, degraded, unhealthy)';
"#,
    name = "create_health_function",
    requires = ["create_schema", _steep_repl_health],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_health_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'health'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "health function should exist");
    }

    #[pg_test]
    fn test_health_returns_row() {
        let result = Spi::get_one::<i64>(
            "SELECT COUNT(*) FROM steep_repl.health()"
        );
        assert_eq!(result, Ok(Some(1)), "health should return exactly 1 row");
    }

    #[pg_test]
    fn test_health_status_valid() {
        let result = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.health()"
        );
        let status = result.expect("query should succeed").expect("status should not be null");
        assert!(
            status == "healthy" || status == "degraded" || status == "unhealthy",
            "status should be healthy, degraded, or unhealthy, got: {}",
            status
        );
    }

    #[pg_test]
    fn test_health_extension_version() {
        let result = Spi::get_one::<String>(
            "SELECT extension_version FROM steep_repl.health()"
        );
        let version = result.expect("query should succeed").expect("version should not be null");
        assert!(!version.is_empty(), "extension version should not be empty");
    }

    #[pg_test]
    fn test_health_pg_version() {
        let result = Spi::get_one::<String>(
            "SELECT pg_version FROM steep_repl.health()"
        );
        let version = result.expect("query should succeed").expect("pg_version should not be null");
        assert!(version.contains("PostgreSQL"), "pg_version should contain PostgreSQL");
    }

    #[pg_test]
    fn test_health_bgworker_field() {
        let result = Spi::get_one::<bool>(
            "SELECT background_worker_running FROM steep_repl.health()"
        );
        // Should return a boolean (true or false)
        assert!(result.is_ok(), "background_worker_running should be queryable");
    }

    #[pg_test]
    fn test_health_shmem_field() {
        let result = Spi::get_one::<bool>(
            "SELECT shared_memory_available FROM steep_repl.health()"
        );
        // In test environment, shared memory should be available
        assert_eq!(result, Ok(Some(true)), "shared_memory_available should be true in tests");
    }

    #[pg_test]
    fn test_health_active_operations_field() {
        let result = Spi::get_one::<i32>(
            "SELECT active_operations FROM steep_repl.health()"
        );
        let count = result.expect("query should succeed").expect("active_operations should not be null");
        assert!(count >= 0, "active_operations should be non-negative");
    }

    #[pg_test]
    fn test_health_status_enum() {
        use super::HealthStatus;
        assert_eq!(HealthStatus::Healthy.as_str(), "healthy");
        assert_eq!(HealthStatus::Degraded.as_str(), "degraded");
        assert_eq!(HealthStatus::Unhealthy.as_str(), "unhealthy");
    }
}
