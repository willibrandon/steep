//! Database registration for steep_repl.
//!
//! This module provides the central catalog for tracking which databases
//! participate in replication. The catalog lives in the `postgres` database
//! and is read by the static background worker to determine which databases
//! need workers spawned.
//!
//! Two registration methods are provided:
//! - `register_db(dbname)`: Manual registration, must be called from postgres
//! - `register_current_db()`: Auto-registration from any database via libpq

use pgrx::prelude::*;
use pgrx::datum::TimestampWithTimeZone;

// =============================================================================
// Schema: steep_repl.databases table
// =============================================================================

extension_sql!(
    r#"
-- Central catalog of databases participating in steep_repl replication.
-- This table MUST be created in the postgres database.
-- The static background worker reads this to determine which databases need workers.
CREATE TABLE IF NOT EXISTS steep_repl.databases (
    datname         TEXT PRIMARY KEY,
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    options         JSONB
);

-- Index for the static worker's query (only enabled databases)
CREATE INDEX IF NOT EXISTS databases_enabled_idx
    ON steep_repl.databases (enabled)
    WHERE enabled = true;

-- Comment for documentation
COMMENT ON TABLE steep_repl.databases IS
    'Central catalog of databases participating in steep_repl replication. '
    'Only databases registered here will have background workers spawned.';
"#,
    name = "databases_table",
    requires = ["create_schema"]
);

// =============================================================================
// Manual Registration Functions (postgres-only)
// =============================================================================

/// Register a database in the central catalog.
///
/// This function MUST be called from the postgres database.
/// It adds the specified database to the list of databases that will
/// have background workers spawned for replication monitoring.
///
/// # Arguments
/// * `dbname` - Name of the database to register
///
/// # Example
/// ```sql
/// \c postgres
/// SELECT steep_repl.register_db('mydb');
/// ```
#[pg_extern]
fn register_db(dbname: &str) -> Result<String, String> {
    // Verify we're in postgres database
    let current = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| format!("Failed to get current database: {}", e))?
        .ok_or("Could not determine current database")?;

    if current != "postgres" {
        return Err(format!(
            "register_db() must be called from postgres database (current: {}). \
             Use register_current_db() to register from within '{}'.",
            current, dbname
        ));
    }

    // Validate database exists
    let exists = Spi::get_one::<bool>(&format!(
        "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = '{}')",
        dbname.replace('\'', "''")
    ))
    .map_err(|e| format!("Failed to check database existence: {}", e))?
    .unwrap_or(false);

    if !exists {
        return Err(format!("Database '{}' does not exist", dbname));
    }

    // Insert or update registration
    Spi::run(&format!(
        "INSERT INTO steep_repl.databases (datname) VALUES ('{}') \
         ON CONFLICT (datname) DO UPDATE SET enabled = true, registered_at = now()",
        dbname.replace('\'', "''")
    ))
    .map_err(|e| format!("Failed to register database: {}", e))?;

    Ok(format!("Database '{}' registered successfully", dbname))
}

/// Unregister a database from the central catalog.
///
/// This function MUST be called from the postgres database.
/// It removes the database from the list, so no new workers will be spawned.
/// Existing workers will exit on their next iteration.
///
/// # Arguments
/// * `dbname` - Name of the database to unregister
///
/// # Example
/// ```sql
/// \c postgres
/// SELECT steep_repl.unregister_db('mydb');
/// ```
#[pg_extern]
fn unregister_db(dbname: &str) -> Result<String, String> {
    // Verify we're in postgres database
    let current = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| format!("Failed to get current database: {}", e))?
        .ok_or("Could not determine current database")?;

    if current != "postgres" {
        return Err(format!(
            "unregister_db() must be called from postgres database (current: {})",
            current
        ));
    }

    // Delete registration
    let deleted = Spi::get_one::<i64>(&format!(
        "WITH deleted AS (
            DELETE FROM steep_repl.databases WHERE datname = '{}' RETURNING 1
        ) SELECT count(*) FROM deleted",
        dbname.replace('\'', "''")
    ))
    .map_err(|e| format!("Failed to unregister database: {}", e))?
    .unwrap_or(0);

    if deleted > 0 {
        Ok(format!("Database '{}' unregistered successfully", dbname))
    } else {
        Ok(format!("Database '{}' was not registered", dbname))
    }
}

/// Disable a database without removing it from the catalog.
///
/// This function MUST be called from the postgres database.
/// Disabled databases will not have workers spawned, but their
/// registration is preserved for easy re-enabling.
///
/// # Arguments
/// * `dbname` - Name of the database to disable
///
/// # Example
/// ```sql
/// \c postgres
/// SELECT steep_repl.disable_db('mydb');
/// ```
#[pg_extern]
fn disable_db(dbname: &str) -> Result<String, String> {
    // Verify we're in postgres database
    let current = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| format!("Failed to get current database: {}", e))?
        .ok_or("Could not determine current database")?;

    if current != "postgres" {
        return Err(format!(
            "disable_db() must be called from postgres database (current: {})",
            current
        ));
    }

    let updated = Spi::get_one::<i64>(&format!(
        "WITH updated AS (
            UPDATE steep_repl.databases SET enabled = false WHERE datname = '{}' RETURNING 1
        ) SELECT count(*) FROM updated",
        dbname.replace('\'', "''")
    ))
    .map_err(|e| format!("Failed to disable database: {}", e))?
    .unwrap_or(0);

    if updated > 0 {
        Ok(format!("Database '{}' disabled", dbname))
    } else {
        Ok(format!("Database '{}' is not registered", dbname))
    }
}

/// Enable a previously disabled database.
///
/// This function MUST be called from the postgres database.
///
/// # Arguments
/// * `dbname` - Name of the database to enable
///
/// # Example
/// ```sql
/// \c postgres
/// SELECT steep_repl.enable_db('mydb');
/// ```
#[pg_extern]
fn enable_db(dbname: &str) -> Result<String, String> {
    // Verify we're in postgres database
    let current = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| format!("Failed to get current database: {}", e))?
        .ok_or("Could not determine current database")?;

    if current != "postgres" {
        return Err(format!(
            "enable_db() must be called from postgres database (current: {})",
            current
        ));
    }

    let updated = Spi::get_one::<i64>(&format!(
        "WITH updated AS (
            UPDATE steep_repl.databases SET enabled = true WHERE datname = '{}' RETURNING 1
        ) SELECT count(*) FROM updated",
        dbname.replace('\'', "''")
    ))
    .map_err(|e| format!("Failed to enable database: {}", e))?
    .unwrap_or(0);

    if updated > 0 {
        Ok(format!("Database '{}' enabled", dbname))
    } else {
        Ok(format!("Database '{}' is not registered", dbname))
    }
}

// =============================================================================
// Auto-Registration Function (any database, uses libpq)
// =============================================================================

/// Register the current database in the central catalog.
///
/// This function can be called from ANY database where steep_repl is installed.
/// It uses libpq to connect to the postgres database and insert the registration.
///
/// # Example
/// ```sql
/// \c mydb
/// CREATE EXTENSION steep_repl;
/// SELECT steep_repl.register_current_db();
/// ```
#[pg_extern]
fn register_current_db() -> Result<String, String> {
    // Get current database name
    let current_db = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| format!("Failed to get current database: {}", e))?
        .ok_or("Could not determine current database")?;

    // If we're already in postgres, just call register_db directly
    if current_db == "postgres" {
        return Err(
            "Already in postgres database. Use register_db('dbname') to register other databases."
                .to_string(),
        );
    }

    // Get the current user to use for the connection
    let current_user = Spi::get_one::<String>("SELECT current_user::text")
        .map_err(|e| format!("Failed to get current user: {}", e))?
        .ok_or("Could not determine current user")?;

    // Connect to postgres via Unix socket (same cluster)
    // Use the same user as the current connection for authentication
    let conn_string = format!("dbname=postgres user={}", current_user);
    let conn = libpq::Connection::new(&conn_string)
        .map_err(|e| format!("Failed to connect to postgres database: {}", e))?;

    // Check connection status
    if conn.status() != libpq::connection::Status::Ok {
        return Err(format!(
            "Connection to postgres failed: {:?}",
            conn.error_message()
        ));
    }

    // Check if steep_repl.databases table exists in postgres
    let check_result = conn.exec(
        "SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = 'databases'"
    );

    if check_result.ntuples() == 0 {
        return Err(
            "steep_repl extension is not installed in postgres database. \
             Please run: \\c postgres && CREATE EXTENSION steep_repl;"
                .to_string(),
        );
    }

    // Insert registration using parameterized query
    // Note: libpq text format requires null-terminated strings
    let db_param = format!("{}\0", current_db);
    let result = conn.exec_params(
        "INSERT INTO steep_repl.databases (datname) VALUES ($1) \
         ON CONFLICT (datname) DO UPDATE SET enabled = true, registered_at = now()",
        &[libpq::types::TEXT.oid],
        &[Some(db_param.as_bytes())],
        &[],
        libpq::Format::Text,
    );

    match result.status() {
        libpq::Status::CommandOk => {
            Ok(format!("Database '{}' registered successfully in central catalog", current_db))
        }
        _ => Err(format!(
            "Failed to register database: {:?}",
            conn.error_message()
        )),
    }
}

// =============================================================================
// Query Functions
// =============================================================================

/// List all registered databases.
///
/// Returns a table of all databases in the central catalog with their status.
/// This function can be called from any database, but the data lives in postgres.
///
/// # Example
/// ```sql
/// SELECT * FROM steep_repl.list_databases();
/// ```
#[pg_extern]
fn list_databases(
) -> TableIterator<'static, (name!(datname, String), name!(enabled, bool), name!(registered_at, TimestampWithTimeZone))>
{
    let query = "SELECT datname, enabled, registered_at FROM steep_repl.databases ORDER BY datname";

    let mut results = Vec::new();

    let _ = Spi::connect(|client| {
        let table = client.select(query, None, &[]);
        if let Ok(table) = table {
            for row in table {
                let datname: Option<String> = row.get(1).ok().flatten();
                let enabled: Option<bool> = row.get(2).ok().flatten();
                let registered_at: Option<TimestampWithTimeZone> = row.get(3).ok().flatten();

                if let (Some(d), Some(e), Some(r)) = (datname, enabled, registered_at) {
                    results.push((d, e, r));
                }
            }
        }
        Ok::<(), pgrx::spi::Error>(())
    });

    TableIterator::new(results)
}

/// Get list of enabled databases as comma-separated string.
///
/// This is used internally by the static background worker to determine
/// which databases need workers spawned.
///
/// # Returns
/// Comma-separated list of enabled database names, or NULL if none.
#[pg_extern]
fn get_enabled_databases() -> Option<String> {
    Spi::get_one::<String>(
        "SELECT string_agg(datname, ',') FROM steep_repl.databases WHERE enabled = true"
    )
    .ok()
    .flatten()
}

// =============================================================================
// Schema-qualified Wrapper Functions
// =============================================================================

extension_sql!(
    r#"
-- Wrapper functions in steep_repl schema that call the internal implementations
CREATE FUNCTION steep_repl.register_db(dbname TEXT) RETURNS TEXT
    LANGUAGE sql AS $$ SELECT register_db(dbname) $$;

CREATE FUNCTION steep_repl.unregister_db(dbname TEXT) RETURNS TEXT
    LANGUAGE sql AS $$ SELECT unregister_db(dbname) $$;

CREATE FUNCTION steep_repl.disable_db(dbname TEXT) RETURNS TEXT
    LANGUAGE sql AS $$ SELECT disable_db(dbname) $$;

CREATE FUNCTION steep_repl.enable_db(dbname TEXT) RETURNS TEXT
    LANGUAGE sql AS $$ SELECT enable_db(dbname) $$;

CREATE FUNCTION steep_repl.register_current_db() RETURNS TEXT
    LANGUAGE sql AS $$ SELECT register_current_db() $$;

CREATE FUNCTION steep_repl.list_databases()
    RETURNS TABLE(datname TEXT, enabled BOOLEAN, registered_at TIMESTAMPTZ)
    LANGUAGE sql AS $$ SELECT * FROM list_databases() $$;

CREATE FUNCTION steep_repl.get_enabled_databases() RETURNS TEXT
    LANGUAGE sql AS $$ SELECT get_enabled_databases() $$;

COMMENT ON FUNCTION steep_repl.register_db(TEXT) IS 'Register a database in the central catalog (must be called from postgres)';
COMMENT ON FUNCTION steep_repl.unregister_db(TEXT) IS 'Unregister a database from the central catalog';
COMMENT ON FUNCTION steep_repl.disable_db(TEXT) IS 'Disable a database without removing it from the catalog';
COMMENT ON FUNCTION steep_repl.enable_db(TEXT) IS 'Enable a previously disabled database';
COMMENT ON FUNCTION steep_repl.register_current_db() IS 'Register the current database in the central catalog (uses libpq)';
COMMENT ON FUNCTION steep_repl.list_databases() IS 'List all registered databases with their status';
COMMENT ON FUNCTION steep_repl.get_enabled_databases() IS 'Get comma-separated list of enabled databases (used by static worker)';
"#,
    name = "create_databases_wrapper_functions",
    requires = [
        "create_schema",
        "databases_table",
        register_db,
        unregister_db,
        disable_db,
        enable_db,
        register_current_db,
        list_databases,
        get_enabled_databases
    ]
);

// =============================================================================
// Tests
// =============================================================================

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use super::*;

    #[pg_test]
    fn test_databases_table_exists() {
        // Verify the table was created
        let exists = Spi::get_one::<bool>(
            "SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = 'databases')"
        );
        assert_eq!(exists, Ok(Some(true)));
    }

    #[pg_test]
    fn test_register_db_requires_postgres() {
        // In pgrx tests, we're in pgrx_tests database, not postgres
        // So register_db should fail
        let result = register_db("some_db");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("must be called from postgres"));
    }

    #[pg_test]
    fn test_get_enabled_databases_empty() {
        // Clear any existing registrations
        let _ = Spi::run("DELETE FROM steep_repl.databases");

        // Should return None when no databases registered
        let result = get_enabled_databases();
        assert!(result.is_none());
    }

    #[pg_test]
    fn test_databases_direct_insert() {
        // Test direct SQL insert (simulating what register_db does)
        let _ = Spi::run("DELETE FROM steep_repl.databases WHERE datname = 'test_direct'");

        let result = Spi::run(
            "INSERT INTO steep_repl.databases (datname) VALUES ('test_direct') ON CONFLICT DO NOTHING"
        );
        assert!(result.is_ok());

        // Verify it was inserted
        let exists = Spi::get_one::<bool>(
            "SELECT EXISTS(SELECT 1 FROM steep_repl.databases WHERE datname = 'test_direct')"
        );
        assert_eq!(exists, Ok(Some(true)));

        // Cleanup
        let _ = Spi::run("DELETE FROM steep_repl.databases WHERE datname = 'test_direct'");
    }
}
