# PostgreSQL Database Maintenance Operations Research

This document provides comprehensive technical information about PostgreSQL maintenance operations, focusing on VACUUM, role management, and privilege systems. This research supports the implementation of database maintenance features in the Steep PostgreSQL monitoring TUI.

## Table of Contents

1. [pg_stat_progress_vacuum](#1-pg_stat_progress_vacuum)
2. [VACUUM Variants](#2-vacuum-variants)
3. [pg_cancel_backend and VACUUM Cancellation](#3-pg_cancel_backend-and-vacuum-cancellation)
4. [pg_stat_all_tables Vacuum Columns](#4-pg_stat_all_tables-vacuum-columns)
5. [pg_roles View](#5-pg_roles-view)
6. [GRANT/REVOKE Patterns](#6-grantrevoke-patterns)

---

## 1. pg_stat_progress_vacuum

### Overview

The `pg_stat_progress_vacuum` view provides real-time monitoring of VACUUM operations (excluding VACUUM FULL). Introduced in PostgreSQL 9.6, it contains one row for each backend currently running VACUUM, including autovacuum worker processes.

**Important**: VACUUM FULL operations report progress via `pg_stat_progress_cluster` instead, since VACUUM FULL rewrites the entire table similar to CLUSTER.

### View Structure

```sql
\d pg_stat_progress_vacuum
```

| Column | Type | Description |
|--------|------|-------------|
| `pid` | integer | Process ID of the backend running VACUUM |
| `datid` | oid | OID of the database |
| `datname` | name | Name of the database |
| `relid` | oid | OID of the table being vacuumed |
| `phase` | text | Current processing phase (see below) |
| `heap_blks_total` | bigint | Total number of heap blocks in the table |
| `heap_blks_scanned` | bigint | Number of heap blocks scanned (including visibility map skips) |
| `heap_blks_vacuumed` | bigint | Number of heap blocks actually vacuumed |
| `index_vacuum_count` | bigint | Number of completed index vacuum cycles |
| `max_dead_tuple_bytes` | bigint | Storage capacity before index vacuum is needed |
| `dead_tuple_bytes` | bigint | Dead tuple data collected since last index vacuum |
| `num_dead_item_ids` | bigint | Number of dead item identifiers collected |
| `indexes_total` | bigint | Total number of indexes to process |
| `indexes_processed` | bigint | Number of indexes already processed |
| `delay_time` | double precision | Milliseconds spent in cost-based delay (PostgreSQL 17+) |

### VACUUM Phases

The `phase` column shows the current stage of the VACUUM operation:

| Phase | Description |
|-------|-------------|
| `initializing` | Preparation before heap scanning begins |
| `scanning heap` | Actively scanning the heap, pruning dead tuples and defragmenting pages |
| `vacuuming indexes` | Processing indexes after heap scan completion |
| `vacuuming heap` | Cleaning up heap after index vacuum operations |
| `cleaning up indexes` | Final index maintenance after complete heap scan |
| `truncating heap` | Returning empty pages at the end of the table to the OS |
| `performing final cleanup` | Updating statistics and finishing the operation |

### Progress Calculation

Calculate VACUUM progress percentage during the "scanning heap" phase:

```sql
SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    ROUND(100.0 * heap_blks_scanned / NULLIF(heap_blks_total, 0), 2) AS progress_pct
FROM pg_stat_progress_vacuum
WHERE phase = 'scanning heap';
```

**Formula**: `Progress % = (heap_blks_scanned / heap_blks_total) × 100`

**Note**: The visibility map optimization allows PostgreSQL to skip clean blocks, but they're still counted in `heap_blks_scanned`, ensuring accurate progress tracking.

### Example Query

Monitor all active VACUUM operations:

```sql
SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    indexes_total,
    indexes_processed,
    ROUND(100.0 * heap_blks_scanned / NULLIF(heap_blks_total, 0), 2) AS scan_progress_pct,
    ROUND(100.0 * indexes_processed / NULLIF(indexes_total, 0), 2) AS index_progress_pct
FROM pg_stat_progress_vacuum;
```

---

## 2. VACUUM Variants

PostgreSQL provides several VACUUM variants with different behaviors, performance characteristics, and locking requirements.

### VACUUM (Plain)

**Purpose**: Reclaim space from dead tuples and make it available for reuse within the same table.

**Characteristics**:
- Does NOT return space to the operating system (in most cases)
- Can operate in parallel with normal reading and writing (no exclusive lock)
- Supports parallel index processing via the PARALLEL option
- Progress tracked in `pg_stat_progress_vacuum`
- Faster than VACUUM FULL

**Locking**: Acquires ShareUpdateExclusiveLock (allows SELECT, INSERT, UPDATE, DELETE)

**Syntax**:
```sql
VACUUM [PARALLEL n] table_name;
```

**Use Case**: Routine maintenance for tables with regular updates/deletes

### VACUUM FULL

**Purpose**: Rewrite the entire table into a new disk file with no wasted space, returning unused space to the operating system.

**Characteristics**:
- Rewrites the entire table to reclaim all dead space
- Returns disk space to the operating system
- Much slower than plain VACUUM
- Requires exclusive lock on the table (blocks all operations)
- Does NOT support parallel processing
- Progress tracked in `pg_stat_progress_cluster` (not `pg_stat_progress_vacuum`)

**Locking**: Acquires AccessExclusiveLock (blocks all operations)

**Syntax**:
```sql
VACUUM FULL table_name;
```

**Use Case**: One-time reclamation of significant disk space; rarely needed with proper autovacuum tuning

**Warning**: VACUUM FULL can take hours on large tables and blocks all access. Use with caution in production.

### VACUUM ANALYZE

**Purpose**: Combine vacuum with statistics updates for the query planner.

**Characteristics**:
- Performs regular VACUUM
- Updates table statistics via ANALYZE
- Helps query planner make better execution decisions
- Handy combination for routine maintenance scripts
- Progress tracked in `pg_stat_progress_vacuum` (for VACUUM portion)

**Syntax**:
```sql
VACUUM ANALYZE table_name;
VACUUM (ANALYZE) table_name;  -- Alternative syntax
```

**Use Case**: Routine maintenance when both space reclamation and statistics updates are needed

### VACUUM VERBOSE

**Purpose**: Provide detailed progress information during VACUUM operations.

**Characteristics**:
- Prints detailed activity reports for each table at INFO log level
- Shows which tables are being processed
- Displays statistics about dead tuples, pages, and space reclaimed
- Can be combined with other options

**Syntax**:
```sql
VACUUM VERBOSE table_name;
VACUUM (VERBOSE, ANALYZE) table_name;
```

**Output Example**:
```
INFO:  vacuuming "public.large_table"
INFO:  index "large_table_pkey" now contains 1000000 row versions in 2745 pages
DETAIL:  0 index row versions were removed.
0 index pages have been deleted, 0 are currently reusable.
INFO:  "large_table": found 0 removable, 1000000 nonremovable row versions in 8850 out of 8850 pages
DETAIL:  0 dead row versions cannot be removed yet, oldest xmin: 1234
```

### Comparison Table

| Feature | VACUUM | VACUUM FULL | VACUUM ANALYZE | VACUUM VERBOSE |
|---------|--------|-------------|----------------|----------------|
| Returns space to OS | No | Yes | No | No |
| Locking | ShareUpdateExclusive | AccessExclusive | ShareUpdateExclusive | ShareUpdateExclusive |
| Parallel support | Yes | No | Yes | Yes |
| Progress view | pg_stat_progress_vacuum | pg_stat_progress_cluster | pg_stat_progress_vacuum | pg_stat_progress_vacuum |
| Speed | Fast | Very slow | Fast | Fast |
| Updates statistics | No | No | Yes | No |
| Verbose output | No | No | No | Yes |
| Blocks concurrent operations | No | Yes | No | No |

### Combined Options

VACUUM options can be combined:

```sql
VACUUM (FULL, VERBOSE, ANALYZE) table_name;
VACUUM (VERBOSE, ANALYZE, PARALLEL 4) table_name;
```

---

## 3. pg_cancel_backend and VACUUM Cancellation

### Overview

PostgreSQL provides two functions to terminate backend processes:
- `pg_cancel_backend(pid)`: Send SIGINT (graceful cancellation)
- `pg_terminate_backend(pid)`: Send SIGTERM (forceful termination)

For VACUUM operations, `pg_cancel_backend()` is preferred as it's less severe and allows safe cleanup.

### Safety and Side Effects

**Key Points**:
1. Canceling VACUUM is **safe** and will not corrupt data
2. All work done by the interrupted VACUUM is **lost**
3. The next VACUUM will start from the beginning
4. No database corruption or data loss occurs

**Behavior**:
```sql
SELECT pg_cancel_backend(pid) FROM pg_stat_progress_vacuum WHERE relid = 'my_table'::regclass;
```

When canceled:
- The database safely rolls back to the state before VACUUM started
- Dead tuples remain in the table (bloat persists)
- The next VACUUM (manual or autovacuum) will start fresh
- No partial progress is saved

### VACUUM vs VACUUM FULL Cancellation

**Regular VACUUM**:
- Work lost: Modest impact (dead tuples not reclaimed)
- Restart cost: Low to moderate
- Safe to cancel during normal operations

**VACUUM FULL**:
- Work lost: **Significant** (entire table rewrite discarded)
- Restart cost: **Very high** (must rewrite entire table again)
- Cancellation is more costly but still safe
- PostgreSQL keeps the old table version and discards the in-progress new version
- Old files remain in use; new files are removed

**Example**: Canceling a 6-hour VACUUM FULL at 5 hours 59 minutes means all 6 hours of work is lost.

### Autovacuum Considerations

**Behavior**:
- If you cancel an autovacuum, it will likely restart soon after
- Autovacuum is launched to prevent issues (e.g., transaction ID wraparound)
- Frequent cancellation can lead to serious problems:
  - Transaction ID wraparound
  - Excessive table bloat
  - Performance degradation

**Best Practice**: Instead of frequently canceling autovacuum, tune it:
```sql
ALTER TABLE problematic_table SET (
    autovacuum_vacuum_scale_factor = 0.1,
    autovacuum_vacuum_threshold = 100,
    autovacuum_vacuum_cost_delay = 5
);
```

### Detection and Cancellation Pattern

**Identify long-running VACUUM**:
```sql
SELECT
    pid,
    now() - backend_start AS backend_runtime,
    datname,
    usename,
    query,
    state
FROM pg_stat_activity
WHERE query LIKE 'VACUUM%' OR query LIKE 'autovacuum:%'
ORDER BY backend_start;
```

**Cancel specific VACUUM**:
```sql
SELECT pg_cancel_backend(12345);  -- Replace with actual PID
```

**Confirm cancellation**:
```sql
SELECT pid, query FROM pg_stat_activity WHERE pid = 12345;
```

### Recommendations

1. **Use pg_cancel_backend** (not pg_terminate_backend) for graceful shutdown
2. **Avoid canceling VACUUM FULL** unless absolutely necessary
3. **Don't cancel autovacuum frequently** - tune it instead
4. **Monitor for wraparound warnings** if canceling autovacuum
5. **Let VACUUM complete** when possible, especially VACUUM FULL
6. **Use VACUUM VERBOSE** to monitor progress and estimate completion time

---

## 4. pg_stat_all_tables Vacuum Columns

### Overview

The `pg_stat_all_tables` view provides comprehensive statistics about table access and maintenance operations, including vacuum and analyze activity.

### Vacuum-Related Columns

```sql
SELECT
    relname,
    last_vacuum,
    last_autovacuum,
    last_analyze,
    last_autoanalyze,
    vacuum_count,
    autovacuum_count,
    analyze_count,
    autoanalyze_count,
    n_ins_since_vacuum,
    n_mod_since_analyze
FROM pg_stat_all_tables
WHERE schemaname = 'public'
ORDER BY relname;
```

| Column | Type | Description |
|--------|------|-------------|
| `last_vacuum` | timestamp with time zone | Last time this table was manually vacuumed (not autovacuum) |
| `last_autovacuum` | timestamp with time zone | Last time this table was vacuumed by autovacuum daemon |
| `last_analyze` | timestamp with time zone | Last time this table was manually analyzed |
| `last_autoanalyze` | timestamp with time zone | Last time this table was analyzed by autovacuum daemon |
| `vacuum_count` | bigint | Number of times this table has been manually vacuumed |
| `autovacuum_count` | bigint | Number of times this table has been vacuumed by autovacuum |
| `analyze_count` | bigint | Number of times this table has been manually analyzed |
| `autoanalyze_count` | bigint | Number of times this table has been analyzed by autovacuum |
| `n_ins_since_vacuum` | bigint | Estimated number of rows inserted since last vacuum |
| `n_mod_since_analyze` | bigint | Estimated number of rows modified since last analyze |
| `total_vacuum_time` | double precision | Total time spent vacuuming this table (ms, PostgreSQL 16+) |
| `total_autovacuum_time` | double precision | Total time spent in autovacuum for this table (ms, PostgreSQL 16+) |
| `total_analyze_time` | double precision | Total time spent analyzing this table (ms, PostgreSQL 16+) |
| `total_autoanalyze_time` | double precision | Total time spent in autoanalyze for this table (ms, PostgreSQL 16+) |

### Column Semantics

**Timestamp Columns** (`last_*`):
- Set to current timestamp when the operation completes
- NULL if the operation has never run on this table
- Timezone-aware (timestamptz)
- Reset on database statistics reset

**Count Columns** (`*_count`):
- Increment each time the operation runs
- Reset on database statistics reset
- 64-bit integers (bigint)

**Delta Columns** (`n_ins_since_vacuum`, `n_mod_since_analyze`):
- Estimates, not exact counts
- Used by autovacuum to determine when to run
- Reset when corresponding operation runs

### Monitoring Queries

**Identify tables needing vacuum**:
```sql
SELECT
    schemaname,
    relname,
    COALESCE(last_vacuum, last_autovacuum) AS last_vacuum_any,
    now() - COALESCE(last_vacuum, last_autovacuum) AS time_since_vacuum,
    n_dead_tup AS dead_tuples,
    n_live_tup AS live_tuples,
    ROUND(100.0 * n_dead_tup / NULLIF(n_live_tup, 0), 2) AS dead_ratio_pct
FROM pg_stat_all_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
    AND n_live_tup > 0
ORDER BY n_dead_tup DESC
LIMIT 20;
```

**Tables never vacuumed**:
```sql
SELECT
    schemaname,
    relname,
    n_live_tup,
    n_dead_tup,
    last_vacuum,
    last_autovacuum
FROM pg_stat_all_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
    AND last_vacuum IS NULL
    AND last_autovacuum IS NULL
    AND n_live_tup > 0
ORDER BY n_dead_tup DESC;
```

**Vacuum frequency analysis**:
```sql
SELECT
    schemaname,
    relname,
    vacuum_count + autovacuum_count AS total_vacuums,
    vacuum_count AS manual_vacuums,
    autovacuum_count AS auto_vacuums,
    GREATEST(last_vacuum, last_autovacuum) AS most_recent_vacuum,
    now() - GREATEST(last_vacuum, last_autovacuum) AS time_since_vacuum
FROM pg_stat_all_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY total_vacuums DESC
LIMIT 20;
```

**Analyze staleness**:
```sql
SELECT
    schemaname,
    relname,
    n_mod_since_analyze,
    COALESCE(last_analyze, last_autoanalyze) AS last_analyze_any,
    now() - COALESCE(last_analyze, last_autoanalyze) AS time_since_analyze,
    ROUND(100.0 * n_mod_since_analyze / NULLIF(n_live_tup, 0), 2) AS mod_ratio_pct
FROM pg_stat_all_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
    AND n_live_tup > 0
    AND n_mod_since_analyze > 1000
ORDER BY n_mod_since_analyze DESC
LIMIT 20;
```

### Integration with Autovacuum

Autovacuum uses these thresholds to determine when to run:

**Vacuum threshold**:
```
threshold = autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor * n_live_tup
```

**Analyze threshold**:
```
threshold = autovacuum_analyze_threshold + autovacuum_analyze_scale_factor * n_live_tup
```

Autovacuum runs when `n_dead_tup` (for vacuum) or `n_mod_since_analyze` (for analyze) exceeds the threshold.

---

## 5. pg_roles View

### Overview

The `pg_roles` view provides access to information about database roles. It's a publicly readable view of the `pg_authid` system catalog with the password field masked for security.

**Important**: PostgreSQL uses "roles" for both users (can login) and groups (cannot login). The distinction is the `rolcanlogin` flag.

### View Structure

```sql
\d pg_roles
```

| Column | Type | Description |
|--------|------|-------------|
| `rolname` | name | Role name (identifier) |
| `rolsuper` | boolean | Role has superuser privileges |
| `rolinherit` | boolean | Role automatically inherits privileges of roles it's a member of |
| `rolcreaterole` | boolean | Role can create other roles |
| `rolcreatedb` | boolean | Role can create databases |
| `rolcanlogin` | boolean | Role can be used as initial session authorization (i.e., can login) |
| `rolreplication` | boolean | Role can initiate replication and create/drop replication slots |
| `rolconnlimit` | integer | Maximum concurrent connections for this role (-1 = unlimited) |
| `rolpassword` | text | Always displays as `********` for security (masked) |
| `rolvaliduntil` | timestamp with time zone | Password expiration time (NULL = never expires) |
| `rolbypassrls` | boolean | Role can bypass row-level security policies |
| `rolconfig` | text[] | Role-specific runtime configuration variable defaults |
| `oid` | oid | Role object identifier (references pg_authid) |

### Key Columns Explained

**rolsuper**:
- If true, role has unrestricted access to the database
- Can override all access restrictions
- Can modify system catalogs directly
- Exercise extreme caution with superuser privileges

**rolcanlogin**:
- Distinguishes "users" (true) from "groups" (false)
- Only roles with `rolcanlogin = true` can be used in connection strings
- Groups are used purely for privilege management

**rolcreaterole**:
- Allows creating, altering, and dropping other roles
- Can grant/revoke role memberships
- Cannot create superuser roles unless the role itself is a superuser

**rolcreatedb**:
- Allows creating new databases
- Can drop databases they own
- Commonly granted to application owners

**rolconnlimit**:
- -1 means unlimited connections
- 0 means the role cannot connect (even if rolcanlogin is true)
- Positive values set hard limits on concurrent connections

**rolvaliduntil**:
- Enforces password expiration
- NULL means password never expires
- Only affects password authentication (not trust, peer, etc.)

**rolbypassrls** (Row-Level Security):
- Allows role to bypass RLS policies
- Critical for data privacy compliance
- Superusers automatically bypass RLS

**rolconfig**:
- Array of `setting = value` strings
- Applied when role starts a session
- Example: `{work_mem=16MB,maintenance_work_mem=64MB}`

### Common Queries

**List all users (can login)**:
```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolconnlimit,
    rolvaliduntil
FROM pg_roles
WHERE rolcanlogin = true
ORDER BY rolname;
```

**List all groups (cannot login)**:
```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb
FROM pg_roles
WHERE rolcanlogin = false
ORDER BY rolname;
```

**Find roles with specific privileges**:
```sql
-- Superusers
SELECT rolname FROM pg_roles WHERE rolsuper = true;

-- Roles that can create databases
SELECT rolname FROM pg_roles WHERE rolcreatedb = true;

-- Roles with connection limits
SELECT rolname, rolconnlimit FROM pg_roles WHERE rolconnlimit >= 0;

-- Roles with expiring passwords
SELECT rolname, rolvaliduntil
FROM pg_roles
WHERE rolvaliduntil IS NOT NULL
ORDER BY rolvaliduntil;
```

**Audit role configurations**:
```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolcanlogin,
    rolreplication,
    rolbypassrls,
    rolconfig
FROM pg_roles
WHERE rolsuper = true OR rolcreaterole = true OR rolbypassrls = true
ORDER BY rolsuper DESC, rolcreaterole DESC;
```

### Role Memberships

To see which roles are members of other roles, query `pg_auth_members`:

```sql
SELECT
    member.rolname AS member_role,
    granted.rolname AS granted_role,
    grantor.rolname AS grantor,
    am.admin_option,
    am.inherit_option,
    am.set_option
FROM pg_auth_members am
JOIN pg_roles member ON member.oid = am.member
JOIN pg_roles granted ON granted.oid = am.roleid
JOIN pg_roles grantor ON grantor.oid = am.grantor
ORDER BY granted_role, member_role;
```

### pg_auth_members Structure

| Column | Type | Description |
|--------|------|-------------|
| `oid` | oid | Row identifier |
| `roleid` | oid | The role that has members (the "group") |
| `member` | oid | The role that is a member of roleid |
| `grantor` | oid | Who granted this membership |
| `admin_option` | boolean | Member can grant this membership to others |
| `inherit_option` | boolean | Member automatically inherits privileges from roleid |
| `set_option` | boolean | Member can SET ROLE to roleid |

**Recursive Role Membership Query**:
```sql
WITH RECURSIVE role_tree AS (
    -- Base case: direct memberships
    SELECT
        am.roleid,
        am.member,
        r.rolname AS role_name,
        m.rolname AS member_name,
        1 AS depth
    FROM pg_auth_members am
    JOIN pg_roles r ON r.oid = am.roleid
    JOIN pg_roles m ON m.oid = am.member
    WHERE m.rolname = 'target_user'  -- Replace with specific user

    UNION ALL

    -- Recursive case: indirect memberships
    SELECT
        am.roleid,
        am.member,
        r.rolname,
        m.rolname,
        rt.depth + 1
    FROM pg_auth_members am
    JOIN pg_roles r ON r.oid = am.roleid
    JOIN pg_roles m ON m.oid = am.member
    JOIN role_tree rt ON rt.roleid = am.member
    WHERE rt.depth < 10  -- Prevent infinite loops
)
SELECT DISTINCT role_name, depth
FROM role_tree
ORDER BY depth, role_name;
```

---

## 6. GRANT/REVOKE Patterns

### Overview

PostgreSQL's privilege system controls access to database objects (tables, schemas, functions, etc.) and operations. Privileges are granted to roles using the `GRANT` command and removed using `REVOKE`.

### Privilege Types

**Table Privileges**:
- `SELECT` - Read data from table
- `INSERT` - Insert new rows
- `UPDATE` - Modify existing rows
- `DELETE` - Delete rows
- `TRUNCATE` - Remove all rows quickly
- `REFERENCES` - Create foreign keys referencing this table
- `TRIGGER` - Create triggers on the table
- `MAINTAIN` - Run VACUUM, ANALYZE, CLUSTER, REINDEX

**Schema Privileges**:
- `USAGE` - Access objects in the schema
- `CREATE` - Create new objects in the schema

**Database Privileges**:
- `CONNECT` - Connect to the database
- `CREATE` - Create new schemas
- `TEMPORARY` (or `TEMP`) - Create temporary tables

**Function/Procedure Privileges**:
- `EXECUTE` - Execute the function or procedure

**Sequence Privileges**:
- `USAGE` - Use the sequence (nextval, currval, setval)
- `SELECT` - Query the sequence
- `UPDATE` - Modify the sequence state

**ALL Privileges**:
- Shorthand for all privileges applicable to the object type
- Equivalent to granting each privilege individually

### GRANT Syntax

**Basic GRANT**:
```sql
GRANT privilege_type ON object_type object_name TO role_name;
```

**Examples**:
```sql
-- Grant SELECT on a table
GRANT SELECT ON TABLE users TO readonly_user;

-- Grant multiple privileges
GRANT SELECT, INSERT, UPDATE ON TABLE orders TO app_user;

-- Grant all privileges
GRANT ALL PRIVILEGES ON TABLE products TO admin_user;

-- Grant schema access
GRANT USAGE ON SCHEMA public TO app_user;

-- Grant database connection
GRANT CONNECT ON DATABASE mydb TO app_user;

-- Grant execute on function
GRANT EXECUTE ON FUNCTION calculate_total(int, int) TO app_user;
```

**WITH GRANT OPTION**:
```sql
-- Allow the recipient to grant the privilege to others
GRANT SELECT ON TABLE users TO manager_user WITH GRANT OPTION;
```

**PUBLIC Role**:
```sql
-- Grant to all roles (including future ones)
GRANT SELECT ON TABLE public_data TO PUBLIC;
```

**Multiple Objects**:
```sql
-- Grant on all tables in schema
GRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_user;

-- Grant on all sequences in schema
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO app_user;
```

### REVOKE Syntax

**Basic REVOKE**:
```sql
REVOKE privilege_type ON object_type object_name FROM role_name;
```

**Examples**:
```sql
-- Revoke SELECT privilege
REVOKE SELECT ON TABLE users FROM readonly_user;

-- Revoke multiple privileges
REVOKE INSERT, UPDATE, DELETE ON TABLE orders FROM app_user;

-- Revoke all privileges
REVOKE ALL PRIVILEGES ON TABLE products FROM app_user;

-- Revoke grant option only
REVOKE GRANT OPTION FOR SELECT ON TABLE users FROM manager_user;
```

**CASCADE vs RESTRICT**:
```sql
-- RESTRICT: Fail if dependent privileges exist (default)
REVOKE SELECT ON TABLE users FROM manager_user RESTRICT;

-- CASCADE: Also revoke from roles that received via grant option
REVOKE SELECT ON TABLE users FROM manager_user CASCADE;
```

### Default Privileges

PostgreSQL assigns default privileges when objects are created. Owners have all privileges; other roles may have default PUBLIC privileges.

**View Current Default Privileges**:
```sql
SELECT
    defaclrole::regrole AS owner,
    defaclnamespace::regnamespace AS schema,
    CASE defaclobjtype
        WHEN 'r' THEN 'table'
        WHEN 'S' THEN 'sequence'
        WHEN 'f' THEN 'function'
        WHEN 'T' THEN 'type'
        WHEN 'n' THEN 'schema'
    END AS object_type,
    defaclacl AS default_acl
FROM pg_default_acl;
```

**ALTER DEFAULT PRIVILEGES**:
```sql
-- Grant SELECT on future tables in schema
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT ON TABLES TO readonly_user;

-- Grant EXECUTE on future functions
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO app_user;

-- For specific role's objects
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner IN SCHEMA public
    GRANT SELECT ON TABLES TO readonly_user;
```

### Querying Existing Privileges

**Using aclexplode**:

The `aclexplode` function decomposes ACL arrays into individual privilege grants:

```sql
-- Schema privileges
SELECT
    n.nspname AS schema_name,
    grantor.rolname AS grantor,
    CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE grantee.rolname END AS grantee,
    a.privilege_type,
    a.is_grantable
FROM pg_namespace n
CROSS JOIN LATERAL aclexplode(n.nspacl) AS a
LEFT JOIN pg_roles grantor ON grantor.oid = a.grantor
LEFT JOIN pg_roles grantee ON grantee.oid = a.grantee
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schema_name, grantee, privilege_type;
```

**Table privileges**:
```sql
SELECT
    schemaname,
    tablename,
    grantor.rolname AS grantor,
    CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE grantee.rolname END AS grantee,
    a.privilege_type,
    a.is_grantable
FROM pg_tables t
JOIN pg_class c ON c.relname = t.tablename AND c.relnamespace = t.schemaname::regnamespace
CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault('r', c.relowner))) AS a
LEFT JOIN pg_roles grantor ON grantor.oid = a.grantor
LEFT JOIN pg_roles grantee ON grantee.oid = a.grantee
WHERE schemaname = 'public'
ORDER BY tablename, grantee, privilege_type;
```

**Database privileges**:
```sql
SELECT
    d.datname AS database,
    grantor.rolname AS grantor,
    CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE grantee.rolname END AS grantee,
    a.privilege_type,
    a.is_grantable
FROM pg_database d
CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) AS a
LEFT JOIN pg_roles grantor ON grantor.oid = a.grantor
LEFT JOIN pg_roles grantee ON grantee.oid = a.grantee
WHERE d.datname = current_database()
ORDER BY grantee, privilege_type;
```

### Using has_*_privilege Functions

**Check specific privileges**:
```sql
-- Check if user can SELECT from table
SELECT has_table_privilege('app_user', 'users', 'SELECT');

-- Check if user can use schema
SELECT has_schema_privilege('app_user', 'public', 'USAGE');

-- Check if user can connect to database
SELECT has_database_privilege('app_user', 'mydb', 'CONNECT');

-- Check current user's privileges
SELECT has_table_privilege('users', 'SELECT');
```

**Audit user privileges**:
```sql
SELECT
    schemaname,
    tablename,
    has_table_privilege('app_user', schemaname || '.' || tablename, 'SELECT') AS can_select,
    has_table_privilege('app_user', schemaname || '.' || tablename, 'INSERT') AS can_insert,
    has_table_privilege('app_user', schemaname || '.' || tablename, 'UPDATE') AS can_update,
    has_table_privilege('app_user', schemaname || '.' || tablename, 'DELETE') AS can_delete
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY tablename;
```

### ACL Format

ACLs are stored as `aclitem[]` arrays with the format:
```
grantee=privilege_abbreviations[*]/grantor
```

**Privilege Abbreviations**:
- `r` = SELECT
- `w` = UPDATE
- `a` = INSERT
- `d` = DELETE
- `D` = TRUNCATE
- `x` = REFERENCES
- `t` = TRIGGER
- `X` = EXECUTE
- `U` = USAGE
- `C` = CREATE
- `c` = CONNECT
- `T` = TEMPORARY
- `m` = MAINTAIN

**Asterisk (*)** indicates the privilege includes GRANT OPTION

**Empty grantee** represents PUBLIC

**Examples**:
- `postgres=CTc/postgres` - postgres has CREATE, TEMPORARY, and CONNECT
- `app_user=r*/postgres` - app_user has SELECT with GRANT OPTION, granted by postgres
- `=U/postgres` - PUBLIC has USAGE, granted by postgres

### Common Privilege Patterns

**Read-only user**:
```sql
CREATE ROLE readonly_user WITH LOGIN PASSWORD 'secret';
GRANT CONNECT ON DATABASE mydb TO readonly_user;
GRANT USAGE ON SCHEMA public TO readonly_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO readonly_user;
```

**Application user (read/write)**:
```sql
CREATE ROLE app_user WITH LOGIN PASSWORD 'secret';
GRANT CONNECT ON DATABASE mydb TO app_user;
GRANT USAGE ON SCHEMA public TO app_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE ON SEQUENCES TO app_user;
```

**Admin user (full access)**:
```sql
CREATE ROLE admin_user WITH LOGIN PASSWORD 'secret' CREATEDB CREATEROLE;
GRANT ALL PRIVILEGES ON DATABASE mydb TO admin_user;
GRANT ALL PRIVILEGES ON SCHEMA public TO admin_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO admin_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO admin_user;
```

**Maintenance user (VACUUM, ANALYZE)**:
```sql
CREATE ROLE maintenance_user WITH LOGIN PASSWORD 'secret';
GRANT CONNECT ON DATABASE mydb TO maintenance_user;
GRANT USAGE ON SCHEMA public TO maintenance_user;
GRANT MAINTAIN ON ALL TABLES IN SCHEMA public TO maintenance_user;
-- MAINTAIN privilege added in PostgreSQL 13
```

**Group-based privileges**:
```sql
-- Create groups
CREATE ROLE readonly_group NOLOGIN;
CREATE ROLE readwrite_group NOLOGIN;

-- Grant privileges to groups
GRANT CONNECT ON DATABASE mydb TO readonly_group;
GRANT USAGE ON SCHEMA public TO readonly_group;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_group;

GRANT CONNECT ON DATABASE mydb TO readwrite_group;
GRANT USAGE ON SCHEMA public TO readwrite_group;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO readwrite_group;

-- Grant group membership to users
GRANT readonly_group TO user1;
GRANT readwrite_group TO user2;
```

### Special Considerations

**Owner Privileges**:
- Object owners always have all privileges on their objects
- Owner privileges cannot be revoked
- The right to drop or alter an object is inherent to ownership

**PUBLIC Role**:
- Represents all roles, including future ones
- Many system objects grant privileges to PUBLIC by default
- Revoke PUBLIC privileges for security:
  ```sql
  REVOKE ALL ON SCHEMA public FROM PUBLIC;
  ```

**pg_catalog Schema**:
- System catalog schema
- SELECT granted to PUBLIC by default
- Cannot fully revoke without breaking functionality
- Do not grant write access

**Privilege Inheritance**:
- Schema USAGE is required to access objects within the schema
- Both schema and object privileges are needed:
  ```sql
  GRANT USAGE ON SCHEMA myschema TO app_user;  -- Required first
  GRANT SELECT ON TABLE myschema.mytable TO app_user;  -- Then table access
  ```

---

## References

### Official PostgreSQL Documentation

- [pg_stat_progress_vacuum](https://www.postgresql.org/docs/current/progress-reporting.html)
- [VACUUM Command](https://www.postgresql.org/docs/current/sql-vacuum.html)
- [pg_roles View](https://www.postgresql.org/docs/current/view-pg-roles.html)
- [pg_auth_members Catalog](https://www.postgresql.org/docs/current/catalog-pg-auth-members.html)
- [GRANT Command](https://www.postgresql.org/docs/current/sql-grant.html)
- [Privileges](https://www.postgresql.org/docs/current/ddl-priv.html)
- [System Information Functions](https://www.postgresql.org/docs/current/functions-info.html)

### Third-Party Resources

- [Deep dive into Postgres stats: pg_stat_progress_vacuum - Data Egret](https://dataegret.com/2017/10/deep-dive-into-postgres-stats-pg_stat_progress_vacuum/)
- [Monitoring PostgreSQL VACUUM processes | Datadog](https://www.datadoghq.com/blog/postgresql-vacuum-monitoring/)
- [PostgreSQL Privilege Helper Queries | Geeky Tidbits](https://www.geekytidbits.com/postgres-privilege-helper-queries/)
- [PostgreSQL: Get member roles and permissions | CYBERTEC](https://www.cybertec-postgresql.com/en/postgresql-get-member-roles-and-permissions/)
- [Managing privileges in PostgreSQL with grant and revoke | Prisma](https://www.prisma.io/dataguide/postgresql/authentication-and-authorization/managing-privileges)

### Stack Overflow / Stack Exchange

- [Does cancelling an (AUTO)VACUUM process make all the work done useless?](https://dba.stackexchange.com/questions/159647/does-cancelling-an-autovacuum-process-in-postgresql-make-all-the-work-done-use)
- [PostgreSQL: Show all the privileges for a concrete user](https://stackoverflow.com/questions/40759177/postgresql-show-all-the-privileges-for-a-concrete-user)
- [How to get all roles that a user is a member of (including inherited roles)?](https://dba.stackexchange.com/questions/56096/how-to-get-all-roles-that-a-user-is-a-member-of-including-inherited-roles)

---

## Version Compatibility

This research is based on PostgreSQL 18.1 (latest as of November 2025) but includes notes for compatibility with PostgreSQL 11+.

**Version-Specific Features**:
- `pg_stat_progress_vacuum`: PostgreSQL 9.6+
- `pg_stat_progress_cluster`: PostgreSQL 12+
- `delay_time` column in `pg_stat_progress_vacuum`: PostgreSQL 17+
- `inherit_option` and `set_option` in `pg_auth_members`: PostgreSQL 16+
- `MAINTAIN` privilege: PostgreSQL 13+
- `total_*_time` columns in `pg_stat_all_tables`: PostgreSQL 16+

**Minimum Target**: PostgreSQL 11 (with graceful degradation)
**Full Feature Support**: PostgreSQL 18

---

## Implementation Notes for Steep

### Progress Monitoring

1. Query `pg_stat_progress_vacuum` every 1-2 seconds for real-time updates
2. Calculate progress percentage: `(heap_blks_scanned / heap_blks_total) * 100`
3. Display current phase and index processing status
4. Handle VACUUM FULL by checking `pg_stat_progress_cluster` instead

### VACUUM Operations

1. Provide confirmation dialog before running VACUUM FULL
2. Show estimated time based on table size
3. Allow cancellation via `pg_cancel_backend()`
4. Warn users about work loss when canceling
5. Support read-only mode (block all VACUUM operations)

### Role Management

1. Display roles with indicators: [U] for users, [G] for groups, [S] for superusers
2. Show role memberships with inheritance tree
3. Color-code based on privileges (red=superuser, yellow=elevated)
4. Filter by: all roles, users only, groups only

### Privilege Queries

1. Use `aclexplode()` to decompose ACLs into readable format
2. Provide `has_*_privilege()` checks for current user
3. Cache privilege queries for performance
4. Display ACLs in human-readable format (not raw aclitem)

### UI Considerations

1. Show progress bar for VACUUM operations (scanning heap phase)
2. Display vacuum history from `pg_stat_all_tables`
3. Highlight tables never vacuumed (NULL timestamps)
4. Color-code dead tuple ratio (green < 5%, yellow < 10%, red >= 10%)

---

*Document generated on 2025-11-28 for Steep PostgreSQL Monitoring TUI*
