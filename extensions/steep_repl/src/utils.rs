//! Utility functions for steep_repl extension.
//!
//! This module provides helper functions for version information
//! and PostgreSQL version requirements.

use pgrx::prelude::*;

/// Returns the steep_repl extension version.
#[pg_extern]
pub fn steep_repl_version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

/// Returns the minimum required PostgreSQL version.
#[pg_extern]
pub fn steep_repl_min_pg_version() -> i32 {
    180000
}

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_steep_repl_version() {
        let version = crate::utils::steep_repl_version();
        assert!(!version.is_empty(), "version should not be empty");
    }

    #[pg_test]
    fn test_steep_repl_min_pg_version() {
        let min_version = crate::utils::steep_repl_min_pg_version();
        assert_eq!(min_version, 180000, "minimum version should be 180000 (PG18)");
    }

    #[pg_test]
    fn test_version_callable_from_sql() {
        let result = Spi::get_one::<String>("SELECT steep_repl_version()");
        assert!(result.is_ok(), "steep_repl_version should be callable from SQL");
        match result {
            Ok(Some(v)) => assert!(!v.is_empty(), "version should not be empty"),
            _ => panic!("steep_repl_version should return a string"),
        }
    }

    #[pg_test]
    fn test_min_version_callable_from_sql() {
        let result = Spi::get_one::<i32>("SELECT steep_repl_min_pg_version()");
        assert_eq!(result, Ok(Some(180000)), "min version should be 180000");
    }
}
