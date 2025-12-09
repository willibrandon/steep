# Database Registration Architecture

**Status**: Proposed
**Date**: 2025-12-09
**Related**: tasks.md (Phase 1-2), worker.rs

## Problem Statement

The current static background worker architecture spawns workers for ALL non-template databases discovered in `pg_database`, regardless of whether:

1. The `steep_repl` extension is installed in that database
2. The database actually needs replication monitoring
3. The database even exists (race condition during init scripts)

This leads to:
- **Worker exhaustion**: Hitting `max_worker_processes` limit
- **Wasted resources**: Workers spawned for chinook, chinook_serial, etc. that don't need them
- **Log noise**: `FATAL: database "X" does not exist` when databases are created/dropped
- **Untracked workers**: `wait_for_startup()` returning `Untracked { notify_pid: 0 }` treated as failure, causing repeated spawn attempts

## Decision

Implement **explicit database registration** with a central catalog in the `postgres` database.

### Design Principles

1. **Explicit over implicit**: DBAs must explicitly register databases that participate in replication
2. **Central catalog**: Registration data lives in `postgres.steep_repl.databases`
3. **Two registration methods**:
   - Manual registration from `postgres` (simplest)
   - Auto-registration from any DB via libpq (convenient)
4. **Static worker reads catalog**: Only spawn workers for registered databases

## Implementation

### 1. Central Catalog Table

Located in `postgres` database, created when extension is installed there:

```sql
-- In postgres database
CREATE TABLE steep_repl.databases (
    datname     TEXT PRIMARY KEY,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    enabled     BOOLEAN NOT NULL DEFAULT true,
    options     JSONB
);

-- Index for the static worker's query
CREATE INDEX ON steep_repl.databases (enabled) WHERE enabled = true;
```

### 2. Registration Functions

#### Manual Registration (postgres-only, no dependencies)

```sql
-- Run in postgres database
-- Simple: no cross-database communication needed
SELECT steep_repl.register_db('mydb');
SELECT steep_repl.unregister_db('mydb');
SELECT steep_repl.list_databases();
```

Implementation in Rust:

```rust
/// Register a database in the central catalog.
/// Must be called from the postgres database.
#[pg_extern]
fn register_db(dbname: &str) -> Result<(), String> {
    // Verify we're in postgres database
    let current = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| e.to_string())?
        .ok_or("Could not get current database")?;

    if current != "postgres" {
        return Err("register_db() must be called from postgres database".to_string());
    }

    Spi::run(&format!(
        "INSERT INTO steep_repl.databases (datname) VALUES ('{}') ON CONFLICT DO NOTHING",
        dbname.replace("'", "''")
    )).map_err(|e| e.to_string())?;

    Ok(())
}
```

#### Auto-Registration (from any database, uses libpq)

```sql
-- Run in any database where steep_repl is installed
SELECT steep_repl.register_current_db();
```

Implementation using `libpq.rs` crate:

```rust
/// Register the current database in the central catalog.
/// Uses libpq to connect to postgres and insert the registration.
#[pg_extern]
fn register_current_db() -> Result<(), String> {
    let current_db = Spi::get_one::<String>("SELECT current_database()::text")
        .map_err(|e| e.to_string())?
        .ok_or("Could not get current database")?;

    // Connect to postgres via Unix socket (same cluster)
    let conn = libpq::Connection::new("dbname=postgres")
        .map_err(|e| format!("Failed to connect to postgres: {}", e))?;

    // Check connection status
    if conn.status() != libpq::connection::Status::Ok {
        return Err(format!("Connection failed: {:?}", conn.error_message()));
    }

    // Insert registration (null-terminated for libpq text format)
    let db_param = format!("{}\0", current_db);
    let result = conn.exec_params(
        "INSERT INTO steep_repl.databases (datname) VALUES ($1) ON CONFLICT DO NOTHING",
        &[libpq::types::TEXT.oid],
        &[Some(db_param.as_bytes())],
        &[],
        libpq::Format::Text,
    );

    match result.status() {
        libpq::Status::CommandOk => Ok(()),
        _ => Err(format!("Registration failed: {:?}", conn.error_message())),
    }
}
```

### 3. Static Worker Changes

Update `steep_repl_static_worker_main` to read from the catalog instead of `pg_database`:

```rust
// Before (problematic):
Spi::get_one::<String>(
    r#"SELECT string_agg(datname, ',')
       FROM pg_database
       WHERE datallowconn AND NOT datistemplate AND datname != 'postgres'"#
)

// After (explicit registration):
Spi::get_one::<String>(
    r#"SELECT string_agg(datname, ',')
       FROM steep_repl.databases
       WHERE enabled = true"#
)
```

### 4. Dependency: libpq.rs

Add to `extensions/steep_repl/Cargo.toml`:

```toml
[dependencies]
libpq = { version = "6.0", features = ["v18"] }
```

The `libpq` crate provides:
- Safe Rust bindings to PostgreSQL's libpq
- Connection management with proper cleanup (`Drop` impl)
- Parameterized queries to prevent SQL injection
- PostgreSQL 18 support via feature flag

Repository: https://github.com/sanpii/libpq.rs

## DBA Workflow

### Initial Setup (once per cluster)

```sql
-- 1. Install extension in postgres (creates central catalog)
\c postgres
CREATE EXTENSION steep_repl;

-- 2. Optionally create global catalog explicitly
SELECT steep_repl.create_global_catalog();
```

### Per-Database Setup

```sql
-- Option A: Register from postgres (manual)
\c postgres
SELECT steep_repl.register_db('mydb');

-- Option B: Register from the database itself (auto)
\c mydb
CREATE EXTENSION steep_repl;
SELECT steep_repl.register_current_db();
```

### View Registered Databases

```sql
\c postgres
SELECT * FROM steep_repl.databases;

-- Or use helper function
SELECT steep_repl.list_databases();
```

### Disable/Enable Database

```sql
\c postgres
UPDATE steep_repl.databases SET enabled = false WHERE datname = 'mydb';
```

## Benefits

1. **No wasted workers**: Only registered databases get background workers
2. **Clear intent**: DBAs explicitly opt-in databases for replication
3. **Queryable state**: `SELECT * FROM steep_repl.databases` shows what's managed
4. **Graceful handling**: No races with database creation/deletion
5. **No log spam**: No more `FATAL: database "X" does not exist`
6. **Tooling friendly**: Easy to automate with Ansible/Terraform/migrations

## Alternatives Considered

### 1. Scan pg_extension in each database
- **Rejected**: Can't query another database's pg_extension from postgres without dblink

### 2. Use dblink extension
- **Rejected**: Adds external dependency that may not be installed

### 3. Shared memory registration
- **Rejected**: Not queryable via SQL, harder to debug/manage

### 4. Configuration file in PGDATA
- **Rejected**: Not SQL-visible, requires file system access

## Migration Path

For existing deployments:

```sql
-- Run once to migrate from implicit to explicit registration
\c postgres
INSERT INTO steep_repl.databases (datname)
SELECT datname FROM pg_database
WHERE datname IN (
    SELECT DISTINCT datname FROM pg_stat_activity
    WHERE backend_type LIKE 'steep_repl%'
)
ON CONFLICT DO NOTHING;
```

## Tasks

New tasks to add to `tasks.md`:

- [ ] T0XX Create steep_repl.databases table in extension schema
- [ ] T0XX Implement register_db() SQL function (postgres-only)
- [ ] T0XX Implement unregister_db() SQL function
- [ ] T0XX Implement list_databases() SQL function
- [ ] T0XX Add libpq dependency to Cargo.toml
- [ ] T0XX Implement register_current_db() SQL function (libpq-based)
- [ ] T0XX Update static worker to read from steep_repl.databases
- [ ] T0XX Update Docker init scripts to register testdb
- [ ] T0XX Add integration tests for registration workflow

## References

- [libpq.rs GitHub](https://github.com/sanpii/libpq.rs) - Safe Rust bindings for libpq
- [pq-sys](https://github.com/sgrif/pq-sys) - Raw FFI bindings (used by diesel)
- [PostgreSQL Background Workers](https://www.postgresql.org/docs/current/bgworker.html)
- [ChatGPT Discussion](./chat-gpt-discussion.md) - Original analysis of the worker spawning issue
