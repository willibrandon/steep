//! steep_repl - PostgreSQL extension for bidirectional replication coordination
//!
//! This extension creates the steep_repl schema with tables for:
//! - nodes: Cluster node registration and status
//! - coordinator_state: Key-value store for cluster coordination
//! - audit_log: Immutable audit trail of system activity
//! - init_progress: Real-time initialization progress tracking
//! - schema_fingerprints: Schema fingerprints for drift detection
//! - init_slots: Replication slots for manual initialization
//! - snapshots: Snapshot manifests with real-time progress tracking (unified table)
//!
//! Requires PostgreSQL 18 or later.

use pgrx::prelude::*;

// Extension metadata
::pgrx::pg_module_magic!(name, version);

// =============================================================================
// Module declarations
// =============================================================================

mod schema;
mod nodes;
mod coordinator_state;
mod audit_log;
mod init_progress;
mod schema_fingerprints;
mod init_slots;
mod snapshots;
mod fingerprint_functions;
mod merge;
mod merge_audit_log;
mod utils;
mod work_queue;
mod progress;
mod notify;

// Re-export utility functions for SQL access
pub use utils::{steep_repl_version, steep_repl_min_pg_version};

// =============================================================================
// PostgreSQL 18 Version Check and Extension Initialization
// =============================================================================

/// Initialize the steep_repl extension.
///
/// This function:
/// 1. Checks PostgreSQL version (18+ required)
/// 2. Initializes shared memory for progress tracking
/// 3. Registers background worker (if loaded via shared_preload_libraries)
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

    // Initialize shared memory for operation progress tracking.
    // This allocates a PgLwLock-protected struct in shared memory
    // that can be read by SQL functions and written by background workers.
    progress::init_shared_memory();

    // Note: Background worker registration (BackgroundWorkerBuilder) requires
    // the extension to be loaded via shared_preload_libraries. This will be
    // implemented in T007 (Phase 2: Foundational).
    //
    // For now, direct mode operations will work without background workers.
    // Operations will be executed synchronously in the client connection
    // instead of being queued to the work_queue table.
}

// =============================================================================
// Tests
// =============================================================================

/// This module is required by `cargo pgrx test` invocations.
/// It must be visible at the root of your extension crate.
#[cfg(test)]
pub mod pg_test {
    pub fn setup(_options: Vec<&str>) {
        // perform one-off initialization when the pg_test framework starts
    }

    #[must_use]
    pub fn postgresql_conf_options() -> Vec<&'static str> {
        // Load steep_repl via shared_preload_libraries so that shared memory
        // for progress tracking is initialized before tests run.
        vec!["shared_preload_libraries='steep_repl'"]
    }
}
