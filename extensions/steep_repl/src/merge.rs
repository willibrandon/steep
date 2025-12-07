//! Merge functions for bidirectional replication data synchronization.
//!
//! This module provides SQL functions for:
//! - row_hash: Fast row hashing for comparison (T067a)
//! - compare_tables: Hash-based table comparison via postgres_fdw (T067b)
//! - quiesce_writes: Block writes during merge operations (T067d)

use pgrx::prelude::*;

extension_sql!(
    r#"
-- =============================================================================
-- T067a: Row Hash Function
-- =============================================================================
-- Computes a fast 8-byte hash of a row for comparison.
-- Uses PostgreSQL's native hashtextextended() for maximum performance.
-- This is NOT cryptographically secure, but sufficient for data comparison.

CREATE FUNCTION steep_repl.row_hash(p_row ANYELEMENT)
RETURNS BIGINT AS $$
    -- Use PostgreSQL's built-in hashtextextended for fast 64-bit hashing
    -- Much faster than MD5: native C implementation, no string manipulation
    -- The second parameter (0) is a seed value for consistent hashing
    SELECT hashtextextended(p_row::text, 0)
$$ LANGUAGE SQL IMMUTABLE STRICT PARALLEL SAFE;

COMMENT ON FUNCTION steep_repl.row_hash(ANYELEMENT) IS
    'Compute 8-byte hash of a row for fast comparison. Uses hashtextextended internally.';

-- =============================================================================
-- T067b: Compare Tables Function
-- =============================================================================
-- Hash-based table comparison via postgres_fdw.
-- Returns overlap analysis: matches, conflicts, local_only, remote_only.
-- Minimal network transfer: only PKs and 8-byte hashes cross the wire.

CREATE TYPE steep_repl.overlap_category AS ENUM (
    'match',      -- Same PK, same data
    'conflict',   -- Same PK, different data
    'local_only', -- Only exists locally (a_only)
    'remote_only' -- Only exists on remote (b_only)
);

CREATE TYPE steep_repl.overlap_result AS (
    pk_value JSONB,
    category steep_repl.overlap_category,
    local_hash BIGINT,
    remote_hash BIGINT
);

CREATE TYPE steep_repl.overlap_summary AS (
    table_schema TEXT,
    table_name TEXT,
    total_rows BIGINT,
    matches BIGINT,
    conflicts BIGINT,
    local_only BIGINT,
    remote_only BIGINT
);

-- Compare a single table with a remote table via postgres_fdw
-- Returns detailed row-by-row comparison results
CREATE FUNCTION steep_repl.compare_table_rows(
    p_local_schema TEXT,
    p_local_table TEXT,
    p_remote_server TEXT,
    p_remote_schema TEXT,
    p_remote_table TEXT,
    p_pk_columns TEXT[]
)
RETURNS SETOF steep_repl.overlap_result AS $function$
DECLARE
    v_pk_select TEXT;
    v_pk_json TEXT;
    v_pk_join TEXT;
    v_pk_coalesce TEXT;
    v_remote_query TEXT;
    v_compare_query TEXT;
    v_col TEXT;
    v_idx INT;
BEGIN
    -- Ensure postgres_fdw extension is available
    CREATE EXTENSION IF NOT EXISTS postgres_fdw;

    -- Build PK column expressions
    v_pk_select := '';
    v_pk_json := '';
    v_pk_join := '';
    v_pk_coalesce := '';

    FOR v_idx IN 1..array_length(p_pk_columns, 1) LOOP
        v_col := p_pk_columns[v_idx];

        IF v_idx > 1 THEN
            v_pk_select := v_pk_select || ', ';
            v_pk_json := v_pk_json || ', ';
            v_pk_join := v_pk_join || ' AND ';
            v_pk_coalesce := v_pk_coalesce || ', ';
        END IF;

        v_pk_select := v_pk_select || format('l.%I', v_col);
        v_pk_json := v_pk_json || format('''%s'', COALESCE(l.%I, r.%I)', v_col, v_col, v_col);
        v_pk_join := v_pk_join || format('l.%I = r.%I', v_col, v_col);
        v_pk_coalesce := v_pk_coalesce || format('COALESCE(l.%I, r.%I)', v_col, v_col);
    END LOOP;

    -- Create temporary foreign table for remote hashes
    EXECUTE format(
        'CREATE TEMP TABLE IF NOT EXISTS _remote_hashes_%s_%s (
            pk_json JSONB,
            row_hash BIGINT
        ) ON COMMIT DROP',
        p_remote_schema, p_remote_table
    );

    EXECUTE format('TRUNCATE _remote_hashes_%s_%s', p_remote_schema, p_remote_table);

    -- Query remote server for hashes via dblink (simpler than FDW for dynamic queries)
    CREATE EXTENSION IF NOT EXISTS dblink;

    -- Get connection string from foreign server and user mapping
    DECLARE
        v_conn_str TEXT;
        v_password TEXT;
    BEGIN
        -- Get server options
        SELECT format('host=%s port=%s dbname=%s',
            (SELECT option_value FROM pg_options_to_table(fs.srvoptions) WHERE option_name = 'host'),
            COALESCE((SELECT option_value FROM pg_options_to_table(fs.srvoptions) WHERE option_name = 'port'), '5432'),
            (SELECT option_value FROM pg_options_to_table(fs.srvoptions) WHERE option_name = 'dbname')
        )
        INTO v_conn_str
        FROM pg_foreign_server fs
        WHERE fs.srvname = p_remote_server;

        -- Get user and password from user mapping
        SELECT
            format(' user=%s', COALESCE(
                (SELECT option_value FROM pg_options_to_table(um.umoptions) WHERE option_name = 'user'),
                current_user
            )) ||
            COALESCE(
                format(' password=%s', (SELECT option_value FROM pg_options_to_table(um.umoptions) WHERE option_name = 'password')),
                ''
            )
        INTO v_password
        FROM pg_user_mapping um
        JOIN pg_foreign_server fs ON um.umserver = fs.oid
        WHERE fs.srvname = p_remote_server
          AND um.umuser IN (0, (SELECT oid FROM pg_roles WHERE rolname = current_user));

        v_conn_str := v_conn_str || COALESCE(v_password, format(' user=%s', current_user));

        IF v_conn_str IS NULL THEN
            RAISE EXCEPTION 'Foreign server % not found', p_remote_server;
        END IF;

        -- Build remote query to get PK + hash
        -- Note: v_pk_json has 'l.' and 'r.' prefixes for local comparison, but for remote
        -- we need plain column names since the remote table alias is 't'
        v_remote_query := format(
            'SELECT jsonb_build_object(%s) as pk_json, steep_repl.row_hash(t.*) as row_hash FROM %I.%I t',
            replace(replace(v_pk_json, 'l.', 't.'), 'r.', 't.'), -- Replace prefixes for remote alias
            p_remote_schema, p_remote_table
        );

        -- Fetch remote hashes
        EXECUTE format(
            'INSERT INTO _remote_hashes_%s_%s SELECT * FROM dblink($conn$%s$conn$, $q$%s$q$) AS t(pk_json JSONB, row_hash BIGINT)',
            p_remote_schema, p_remote_table,
            v_conn_str, v_remote_query
        );
    END;

    -- Build and execute comparison query
    -- v_pk_json has 'l.' and 'r.' prefixes for the outer join, but for the CTE we need 't.'
    v_compare_query := format($q$
        WITH local_hashes AS (
            SELECT jsonb_build_object(%s) as pk_json, steep_repl.row_hash(t.*) as row_hash
            FROM %I.%I t
        )
        SELECT
            COALESCE(l.pk_json, r.pk_json)::JSONB as pk_value,
            CASE
                WHEN l.pk_json IS NULL THEN 'remote_only'::steep_repl.overlap_category
                WHEN r.pk_json IS NULL THEN 'local_only'::steep_repl.overlap_category
                WHEN l.row_hash = r.row_hash THEN 'match'::steep_repl.overlap_category
                ELSE 'conflict'::steep_repl.overlap_category
            END as category,
            l.row_hash as local_hash,
            r.row_hash as remote_hash
        FROM local_hashes l
        FULL OUTER JOIN _remote_hashes_%s_%s r ON l.pk_json = r.pk_json
    $q$,
        replace(replace(v_pk_json, 'l.', 't.'), 'r.', 't.'),  -- Replace both l. and r. with t. for CTE
        p_local_schema, p_local_table,
        p_remote_schema, p_remote_table
    );

    RETURN QUERY EXECUTE v_compare_query;
END;
$function$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.compare_table_rows(TEXT, TEXT, TEXT, TEXT, TEXT, TEXT[]) IS
    'Compare table rows with remote table via postgres_fdw/dblink. Returns per-row overlap analysis.';

-- Get summary statistics for table comparison
CREATE FUNCTION steep_repl.compare_table_summary(
    p_local_schema TEXT,
    p_local_table TEXT,
    p_remote_server TEXT,
    p_remote_schema TEXT,
    p_remote_table TEXT,
    p_pk_columns TEXT[]
)
RETURNS steep_repl.overlap_summary AS $function$
    SELECT
        p_local_schema::TEXT as table_schema,
        p_local_table::TEXT as table_name,
        count(*)::BIGINT as total_rows,
        count(*) FILTER (WHERE category = 'match')::BIGINT as matches,
        count(*) FILTER (WHERE category = 'conflict')::BIGINT as conflicts,
        count(*) FILTER (WHERE category = 'local_only')::BIGINT as local_only,
        count(*) FILTER (WHERE category = 'remote_only')::BIGINT as remote_only
    FROM steep_repl.compare_table_rows(
        p_local_schema, p_local_table,
        p_remote_server, p_remote_schema, p_remote_table,
        p_pk_columns
    );
$function$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.compare_table_summary(TEXT, TEXT, TEXT, TEXT, TEXT, TEXT[]) IS
    'Get overlap analysis summary for table comparison. Returns counts of matches, conflicts, local_only, remote_only.';

-- =============================================================================
-- T067d: Quiesce Writes Function
-- =============================================================================
-- Block writes to a table during merge operations using advisory locks.
-- Returns true if quiesce succeeded within timeout.

CREATE FUNCTION steep_repl.quiesce_writes(
    p_schema TEXT,
    p_table TEXT,
    p_timeout_ms INTEGER DEFAULT 30000
)
RETURNS BOOLEAN AS $function$
DECLARE
    v_lock_id BIGINT;
    v_start_time TIMESTAMPTZ;
    v_active_count INTEGER;
BEGIN
    -- Generate a deterministic lock ID from schema.table
    v_lock_id := hashtext(p_schema || '.' || p_table);

    -- Try to acquire exclusive advisory lock
    IF NOT pg_try_advisory_lock(v_lock_id) THEN
        RAISE NOTICE 'Could not acquire advisory lock for %.%', p_schema, p_table;
        RETURN FALSE;
    END IF;

    v_start_time := clock_timestamp();

    -- Wait for active transactions on this table to complete
    LOOP
        SELECT count(*) INTO v_active_count
        FROM pg_stat_activity psa
        JOIN pg_locks pl ON psa.pid = pl.pid
        JOIN pg_class pc ON pl.relation = pc.oid
        JOIN pg_namespace pn ON pc.relnamespace = pn.oid
        WHERE pn.nspname = p_schema
          AND pc.relname = p_table
          AND pl.mode IN ('RowExclusiveLock', 'ExclusiveLock', 'AccessExclusiveLock')
          AND psa.pid <> pg_backend_pid();

        EXIT WHEN v_active_count = 0;

        -- Check timeout
        IF extract(epoch from (clock_timestamp() - v_start_time)) * 1000 > p_timeout_ms THEN
            -- Release lock and fail
            PERFORM pg_advisory_unlock(v_lock_id);
            RAISE NOTICE 'Timeout waiting for active transactions on %.%', p_schema, p_table;
            RETURN FALSE;
        END IF;

        -- Brief sleep before retry
        PERFORM pg_sleep(0.1);
    END LOOP;

    -- Lock acquired and no active transactions
    RETURN TRUE;
END;
$function$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.quiesce_writes(TEXT, TEXT, INTEGER) IS
    'Block writes to a table during merge. Returns true if quiesce succeeded within timeout.';

-- Release quiesce lock
CREATE FUNCTION steep_repl.release_quiesce(
    p_schema TEXT,
    p_table TEXT
)
RETURNS BOOLEAN AS $function$
DECLARE
    v_lock_id BIGINT;
BEGIN
    v_lock_id := hashtext(p_schema || '.' || p_table);
    RETURN pg_advisory_unlock(v_lock_id);
END;
$function$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.release_quiesce(TEXT, TEXT) IS
    'Release quiesce lock on a table after merge completion.';
"#,
    name = "create_merge_functions",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_row_hash_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'row_hash'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "row_hash function should exist");
    }

    #[pg_test]
    fn test_row_hash_returns_bigint() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_row_hash (id INT, name TEXT)").expect("create test table");
        Spi::run("INSERT INTO public.test_row_hash VALUES (1, 'alice')").expect("insert test row");

        let result = Spi::get_one::<i64>(
            "SELECT steep_repl.row_hash(t.*) FROM public.test_row_hash t WHERE id = 1"
        );

        match result {
            Ok(Some(hash)) => {
                assert_ne!(hash, 0, "row_hash should return non-zero value");
            }
            _ => panic!("row_hash should return a bigint"),
        }

        // Cleanup
        Spi::run("DROP TABLE IF EXISTS public.test_row_hash").expect("cleanup test table");
    }

    #[pg_test]
    fn test_row_hash_deterministic() {
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_hash_det (id INT, name TEXT)").expect("create test table");
        Spi::run("INSERT INTO public.test_hash_det VALUES (1, 'test')").expect("insert test row");

        let hash1 = Spi::get_one::<i64>(
            "SELECT steep_repl.row_hash(t.*) FROM public.test_hash_det t WHERE id = 1"
        ).expect("first hash");

        let hash2 = Spi::get_one::<i64>(
            "SELECT steep_repl.row_hash(t.*) FROM public.test_hash_det t WHERE id = 1"
        ).expect("second hash");

        assert_eq!(hash1, hash2, "row_hash should be deterministic");

        Spi::run("DROP TABLE IF EXISTS public.test_hash_det").expect("cleanup test table");
    }

    #[pg_test]
    fn test_row_hash_different_for_different_data() {
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_hash_diff (id INT, name TEXT)").expect("create test table");
        Spi::run("INSERT INTO public.test_hash_diff VALUES (1, 'alice'), (2, 'bob')").expect("insert test rows");

        let hash1 = Spi::get_one::<i64>(
            "SELECT steep_repl.row_hash(t.*) FROM public.test_hash_diff t WHERE id = 1"
        ).expect("first hash");

        let hash2 = Spi::get_one::<i64>(
            "SELECT steep_repl.row_hash(t.*) FROM public.test_hash_diff t WHERE id = 2"
        ).expect("second hash");

        assert_ne!(hash1, hash2, "different rows should have different hashes");

        Spi::run("DROP TABLE IF EXISTS public.test_hash_diff").expect("cleanup test table");
    }

    #[pg_test]
    fn test_compare_table_rows_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'compare_table_rows'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compare_table_rows function should exist");
    }

    #[pg_test]
    fn test_compare_table_summary_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'compare_table_summary'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "compare_table_summary function should exist");
    }

    #[pg_test]
    fn test_quiesce_writes_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'quiesce_writes'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "quiesce_writes function should exist");
    }

    #[pg_test]
    fn test_release_quiesce_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'release_quiesce'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "release_quiesce function should exist");
    }

    #[pg_test]
    fn test_quiesce_and_release() {
        // Create a test table
        Spi::run("CREATE TABLE IF NOT EXISTS public.test_quiesce (id INT)").expect("create test table");

        // Quiesce should succeed on empty table
        let result = Spi::get_one::<bool>(
            "SELECT steep_repl.quiesce_writes('public', 'test_quiesce', 1000)"
        );
        assert_eq!(result, Ok(Some(true)), "quiesce should succeed");

        // Release should succeed
        let release_result = Spi::get_one::<bool>(
            "SELECT steep_repl.release_quiesce('public', 'test_quiesce')"
        );
        assert_eq!(release_result, Ok(Some(true)), "release should succeed");

        Spi::run("DROP TABLE IF EXISTS public.test_quiesce").expect("cleanup test table");
    }

    #[pg_test]
    fn test_overlap_category_enum_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_type t
                JOIN pg_namespace n ON t.typnamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_category'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "overlap_category enum should exist");
    }

    #[pg_test]
    fn test_overlap_result_type_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_type t
                JOIN pg_namespace n ON t.typnamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_result'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "overlap_result type should exist");
    }

    #[pg_test]
    fn test_overlap_summary_type_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_type t
                JOIN pg_namespace n ON t.typnamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_summary'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "overlap_summary type should exist");
    }
}
