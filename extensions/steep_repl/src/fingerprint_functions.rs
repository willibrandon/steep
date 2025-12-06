//! Schema fingerprint SQL functions for steep_repl extension.
//!
//! This module provides SQL functions for computing, capturing, and comparing
//! schema fingerprints across nodes for drift detection.

use pgrx::prelude::*;

extension_sql!(
    r#"
-- Compute fingerprint for a single table
-- Returns SHA256 hash of column definitions (name, type, default, nullable) in ordinal order
CREATE FUNCTION steep_repl.compute_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' ||
        coalesce(column_default, 'NULL') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION steep_repl.compute_fingerprint(TEXT, TEXT) IS 'Compute SHA256 fingerprint of table column definitions';

-- Capture fingerprint for a table (insert or update)
CREATE FUNCTION steep_repl.capture_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS steep_repl.schema_fingerprints AS $$
    INSERT INTO steep_repl.schema_fingerprints (table_schema, table_name, fingerprint, column_count, column_definitions)
    SELECT
        p_schema,
        p_table,
        steep_repl.compute_fingerprint(p_schema, p_table),
        count(*)::integer,
        jsonb_agg(jsonb_build_object(
            'name', column_name,
            'type', data_type,
            'default', column_default,
            'nullable', is_nullable,
            'position', ordinal_position
        ) ORDER BY ordinal_position)
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table
    GROUP BY 1, 2
    ON CONFLICT (table_schema, table_name) DO UPDATE SET
        fingerprint = EXCLUDED.fingerprint,
        column_count = EXCLUDED.column_count,
        column_definitions = EXCLUDED.column_definitions,
        captured_at = now()
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.capture_fingerprint(TEXT, TEXT) IS 'Capture and store schema fingerprint for a table';

-- Capture all user tables
CREATE FUNCTION steep_repl.capture_all_fingerprints()
RETURNS INTEGER AS $$
DECLARE
    v_count INTEGER := 0;
    rec RECORD;
BEGIN
    FOR rec IN
        SELECT schemaname, tablename
        FROM pg_tables
        WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
    LOOP
        PERFORM steep_repl.capture_fingerprint(rec.schemaname, rec.tablename);
        v_count := v_count + 1;
    END LOOP;
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.capture_all_fingerprints() IS 'Capture fingerprints for all user tables';

-- Compare fingerprints with a peer node via dblink
-- Returns a table of comparison results
-- Requires dblink extension and peer node connection info in steep_repl.nodes
CREATE FUNCTION steep_repl.compare_fingerprints(p_peer_node TEXT)
RETURNS TABLE (
    table_schema TEXT,
    table_name TEXT,
    local_fingerprint TEXT,
    remote_fingerprint TEXT,
    status TEXT,  -- MATCH, MISMATCH, LOCAL_ONLY, REMOTE_ONLY
    local_column_count INTEGER,
    remote_column_count INTEGER
) AS $function$
DECLARE
    v_peer_host TEXT;
    v_peer_port INTEGER;
    v_conn_str TEXT;
BEGIN
    -- Ensure dblink extension is available
    CREATE EXTENSION IF NOT EXISTS dblink;

    -- Get peer connection info from nodes table
    SELECT host, port INTO v_peer_host, v_peer_port
    FROM steep_repl.nodes
    WHERE node_id = p_peer_node;

    IF v_peer_host IS NULL THEN
        RAISE EXCEPTION 'Peer node % not found in steep_repl.nodes', p_peer_node;
    END IF;

    -- Build connection string (uses current database name and user)
    v_conn_str := format(
        'host=%s port=%s dbname=%s user=%s sslmode=disable',
        v_peer_host,
        COALESCE(v_peer_port, 5432),
        current_database(),
        current_user
    );

    -- Capture local fingerprints first
    PERFORM steep_repl.capture_all_fingerprints();

    -- Create temp table for remote fingerprints
    CREATE TEMP TABLE IF NOT EXISTS _remote_fps (
        table_schema TEXT,
        table_name TEXT,
        fingerprint TEXT,
        column_count INTEGER
    ) ON COMMIT DROP;
    TRUNCATE _remote_fps;

    -- Fetch remote fingerprints via dblink
    INSERT INTO _remote_fps
    SELECT * FROM dblink(
        v_conn_str,
        $$
            SELECT table_schema, table_name,
                   steep_repl.compute_fingerprint(table_schema, table_name) as fingerprint,
                   (SELECT count(*)::integer FROM information_schema.columns c
                    WHERE c.table_schema = t.table_schema AND c.table_name = t.table_name) as column_count
            FROM information_schema.tables t
            WHERE table_type = 'BASE TABLE'
            AND table_schema NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
        $$
    ) AS t(table_schema TEXT, table_name TEXT, fingerprint TEXT, column_count INTEGER);

    -- Return comparison results
    RETURN QUERY
    SELECT
        COALESCE(l.table_schema, r.table_schema) as table_schema,
        COALESCE(l.table_name, r.table_name) as table_name,
        COALESCE(l.fingerprint, '') as local_fingerprint,
        COALESCE(r.fingerprint, '') as remote_fingerprint,
        CASE
            WHEN l.fingerprint IS NULL THEN 'REMOTE_ONLY'
            WHEN r.fingerprint IS NULL THEN 'LOCAL_ONLY'
            WHEN l.fingerprint = r.fingerprint THEN 'MATCH'
            ELSE 'MISMATCH'
        END as status,
        COALESCE(l.column_count, 0) as local_column_count,
        COALESCE(r.column_count, 0) as remote_column_count
    FROM steep_repl.schema_fingerprints l
    FULL OUTER JOIN _remote_fps r
        ON l.table_schema = r.table_schema AND l.table_name = r.table_name
    ORDER BY table_schema, table_name;

END;
$function$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.compare_fingerprints(TEXT) IS 'Compare schema fingerprints with a peer node via dblink';

-- Get detailed column differences between local and remote table
-- Used when compare_fingerprints returns MISMATCH status
CREATE FUNCTION steep_repl.get_column_diff(
    p_peer_node TEXT,
    p_table_schema TEXT,
    p_table_name TEXT
)
RETURNS TABLE (
    column_name TEXT,
    difference_type TEXT,  -- missing_local, missing_remote, type_change, default_change, nullable_change
    local_definition TEXT,
    remote_definition TEXT
) AS $function$
DECLARE
    v_peer_host TEXT;
    v_peer_port INTEGER;
    v_conn_str TEXT;
BEGIN
    -- Ensure dblink extension is available
    CREATE EXTENSION IF NOT EXISTS dblink;

    -- Get peer connection info
    SELECT host, port INTO v_peer_host, v_peer_port
    FROM steep_repl.nodes
    WHERE node_id = p_peer_node;

    IF v_peer_host IS NULL THEN
        RAISE EXCEPTION 'Peer node % not found in steep_repl.nodes', p_peer_node;
    END IF;

    -- Build connection string
    v_conn_str := format(
        'host=%s port=%s dbname=%s user=%s sslmode=disable',
        v_peer_host,
        COALESCE(v_peer_port, 5432),
        current_database(),
        current_user
    );

    -- Create temp table for remote columns
    CREATE TEMP TABLE IF NOT EXISTS _remote_cols (
        column_name TEXT,
        data_type TEXT,
        column_default TEXT,
        is_nullable TEXT,
        ordinal_position INTEGER
    ) ON COMMIT DROP;
    TRUNCATE _remote_cols;

    -- Fetch remote column info via dblink
    INSERT INTO _remote_cols
    SELECT * FROM dblink(
        v_conn_str,
        format($$
            SELECT column_name, data_type, column_default, is_nullable, ordinal_position
            FROM information_schema.columns
            WHERE table_schema = %L AND table_name = %L
            ORDER BY ordinal_position
        $$, p_table_schema, p_table_name)
    ) AS t(column_name TEXT, data_type TEXT, column_default TEXT, is_nullable TEXT, ordinal_position INTEGER);

    -- Return column differences
    RETURN QUERY
    WITH local_cols AS (
        SELECT c.column_name, c.data_type, c.column_default, c.is_nullable, c.ordinal_position
        FROM information_schema.columns c
        WHERE c.table_schema = p_table_schema AND c.table_name = p_table_name
    )
    SELECT
        COALESCE(l.column_name, r.column_name) as column_name,
        CASE
            WHEN l.column_name IS NULL THEN 'missing_local'
            WHEN r.column_name IS NULL THEN 'missing_remote'
            WHEN l.data_type <> r.data_type THEN 'type_change'
            WHEN COALESCE(l.column_default, '') <> COALESCE(r.column_default, '') THEN 'default_change'
            WHEN l.is_nullable <> r.is_nullable THEN 'nullable_change'
            ELSE 'match'
        END as difference_type,
        CASE
            WHEN l.column_name IS NULL THEN ''
            ELSE format('%s %s %s %s', l.column_name, l.data_type,
                CASE WHEN l.column_default IS NOT NULL THEN 'DEFAULT ' || l.column_default ELSE '' END,
                CASE WHEN l.is_nullable = 'NO' THEN 'NOT NULL' ELSE '' END)
        END as local_definition,
        CASE
            WHEN r.column_name IS NULL THEN ''
            ELSE format('%s %s %s %s', r.column_name, r.data_type,
                CASE WHEN r.column_default IS NOT NULL THEN 'DEFAULT ' || r.column_default ELSE '' END,
                CASE WHEN r.is_nullable = 'NO' THEN 'NOT NULL' ELSE '' END)
        END as remote_definition
    FROM local_cols l
    FULL OUTER JOIN _remote_cols r ON l.column_name = r.column_name
    WHERE l.column_name IS NULL OR r.column_name IS NULL
       OR l.data_type <> r.data_type
       OR COALESCE(l.column_default, '') <> COALESCE(r.column_default, '')
       OR l.is_nullable <> r.is_nullable
    ORDER BY COALESCE(l.ordinal_position, r.ordinal_position);

END;
$function$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.get_column_diff(TEXT, TEXT, TEXT) IS 'Get detailed column differences between local and remote table';
"#,
    name = "create_fingerprint_functions",
    requires = ["create_schema_fingerprints_table"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_compute_fingerprint_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'compute_fingerprint'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compute_fingerprint function should exist");
    }

    #[pg_test]
    fn test_capture_fingerprint_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'capture_fingerprint'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "capture_fingerprint function should exist");
    }

    #[pg_test]
    fn test_capture_all_fingerprints_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'capture_all_fingerprints'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "capture_all_fingerprints function should exist");
    }

    #[pg_test]
    fn test_compare_fingerprints_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'compare_fingerprints'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compare_fingerprints function should exist");
    }

    #[pg_test]
    fn test_get_column_diff_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'get_column_diff'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "get_column_diff function should exist");
    }

    #[pg_test]
    fn test_compute_fingerprint_returns_hex() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_fp_table (id INT, name TEXT)").expect("create test table");

        // Compute fingerprint
        let result = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_fp_table')"
        );

        // Should return a hex string (SHA256 = 64 hex chars)
        match result {
            Ok(Some(fp)) => {
                assert_eq!(fp.len(), 64, "fingerprint should be 64 hex characters");
                assert!(fp.chars().all(|c| c.is_ascii_hexdigit()), "fingerprint should be hex");
            }
            _ => panic!("compute_fingerprint should return a string"),
        }

        // Cleanup
        Spi::run("DROP TABLE IF EXISTS public.test_fp_table").expect("cleanup test table");
    }

    #[pg_test]
    fn test_capture_fingerprint_stores_result() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_capture_table (id INT, name TEXT, created_at TIMESTAMP)")
            .expect("create test table");

        // Capture fingerprint
        Spi::run("SELECT steep_repl.capture_fingerprint('public', 'test_capture_table')")
            .expect("capture fingerprint should succeed");

        // Verify it's stored
        let result = Spi::get_one::<i32>(
            "SELECT column_count FROM steep_repl.schema_fingerprints
             WHERE table_schema = 'public' AND table_name = 'test_capture_table'"
        );
        assert_eq!(result, Ok(Some(3)), "should have 3 columns");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE table_schema = 'public' AND table_name = 'test_capture_table'")
            .expect("cleanup fingerprint should succeed");
        Spi::run("DROP TABLE IF EXISTS public.test_capture_table").expect("cleanup test table");
    }

    #[pg_test]
    fn test_capture_fingerprint_updates_on_conflict() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_update_table (id INT)")
            .expect("create test table");

        // Capture fingerprint
        Spi::run("SELECT steep_repl.capture_fingerprint('public', 'test_update_table')")
            .expect("first capture should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT column_count FROM steep_repl.schema_fingerprints
             WHERE table_schema = 'public' AND table_name = 'test_update_table'"
        );
        assert_eq!(result, Ok(Some(1)), "should have 1 column initially");

        // Add a column
        Spi::run("ALTER TABLE public.test_update_table ADD COLUMN name TEXT")
            .expect("alter table should succeed");

        // Re-capture fingerprint
        Spi::run("SELECT steep_repl.capture_fingerprint('public', 'test_update_table')")
            .expect("second capture should succeed");

        let result = Spi::get_one::<i32>(
            "SELECT column_count FROM steep_repl.schema_fingerprints
             WHERE table_schema = 'public' AND table_name = 'test_update_table'"
        );
        assert_eq!(result, Ok(Some(2)), "should have 2 columns after alter");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE table_schema = 'public' AND table_name = 'test_update_table'")
            .expect("cleanup fingerprint should succeed");
        Spi::run("DROP TABLE IF EXISTS public.test_update_table").expect("cleanup test table");
    }

    #[pg_test]
    fn test_capture_all_fingerprints_returns_count() {
        // Create a few test tables
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_all_1 (id INT)").expect("create test table 1");
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_all_2 (id INT)").expect("create test table 2");

        // Capture all fingerprints
        let result = Spi::get_one::<i32>("SELECT steep_repl.capture_all_fingerprints()");

        match result {
            Ok(Some(count)) => {
                assert!(count >= 2, "should capture at least 2 tables");
            }
            _ => panic!("capture_all_fingerprints should return a count"),
        }

        // Cleanup
        Spi::run("DELETE FROM steep_repl.schema_fingerprints WHERE table_schema = 'public' AND table_name LIKE 'test_all_%'")
            .expect("cleanup fingerprints should succeed");
        Spi::run("DROP TABLE IF EXISTS public.test_all_1").expect("cleanup test table 1");
        Spi::run("DROP TABLE IF EXISTS public.test_all_2").expect("cleanup test table 2");
    }

    #[pg_test]
    fn test_fingerprint_deterministic() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_deterministic (id INT, name TEXT)")
            .expect("create test table");

        // Compute fingerprint twice
        let fp1 = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_deterministic')"
        ).expect("first fingerprint");

        let fp2 = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_deterministic')"
        ).expect("second fingerprint");

        assert_eq!(fp1, fp2, "fingerprints should be deterministic");

        // Cleanup
        Spi::run("DROP TABLE IF EXISTS public.test_deterministic").expect("cleanup test table");
    }

    #[pg_test]
    fn test_fingerprint_changes_with_schema() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_changes (id INT)")
            .expect("create test table");

        // Compute fingerprint
        let fp1 = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_changes')"
        ).expect("first fingerprint");

        // Add a column
        Spi::run("ALTER TABLE public.test_changes ADD COLUMN name TEXT")
            .expect("alter table");

        // Compute fingerprint again
        let fp2 = Spi::get_one::<String>(
            "SELECT steep_repl.compute_fingerprint('public', 'test_changes')"
        ).expect("second fingerprint");

        assert_ne!(fp1, fp2, "fingerprints should change when schema changes");

        // Cleanup
        Spi::run("DROP TABLE IF EXISTS public.test_changes").expect("cleanup test table");
    }
}
