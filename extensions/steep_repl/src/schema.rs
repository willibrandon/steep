//! Schema creation for steep_repl extension.
//!
//! This module creates the steep_repl schema as the bootstrap step.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Create the steep_repl schema
CREATE SCHEMA IF NOT EXISTS steep_repl;

COMMENT ON SCHEMA steep_repl IS 'Steep bidirectional replication coordination schema';
"#,
    name = "create_schema",
    bootstrap,
);

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
}
