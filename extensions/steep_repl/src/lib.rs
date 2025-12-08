//! steep_repl - PostgreSQL extension for bidirectional replication coordination
//!
//! This extension creates the steep_repl schema with tables for:
//! - nodes: Cluster node registration and status
//! - coordinator_state: Key-value store for cluster coordination
//! - audit_log: Immutable audit trail of system activity
//! - init_progress: Real-time initialization progress tracking
//! - schema_fingerprints: Schema fingerprints for drift detection
//! - init_slots: Replication slots for manual initialization
//! - snapshots: Generated snapshot manifests for two-phase initialization
//! - snapshot_progress: Real-time two-phase snapshot progress tracking
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

// Re-export utility functions for SQL access
pub use utils::{steep_repl_version, steep_repl_min_pg_version};

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
        // return any postgresql.conf settings that are required for your tests
        vec![]
    }
}
