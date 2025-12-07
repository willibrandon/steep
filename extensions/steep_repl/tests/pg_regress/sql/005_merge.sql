-- Test merge functions
-- Verifies row_hash, overlap types, quiesce_writes, release_quiesce

-- =============================================================================
-- Test row_hash function
-- =============================================================================

-- Create test table
CREATE TABLE public.merge_test (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    value NUMERIC(10,2)
);

INSERT INTO public.merge_test VALUES
    (1, 'alice', 100.00),
    (2, 'bob', 200.50),
    (3, 'charlie', 300.75);

-- row_hash should return BIGINT
SELECT pg_typeof(steep_repl.row_hash(t.*)) AS hash_type
FROM public.merge_test t WHERE id = 1;

-- row_hash should be deterministic (same row = same hash)
SELECT steep_repl.row_hash(t.*) = steep_repl.row_hash(t.*) AS is_deterministic
FROM public.merge_test t WHERE id = 1;

-- Different rows should have different hashes
SELECT COUNT(DISTINCT steep_repl.row_hash(t.*)) AS unique_hashes
FROM public.merge_test t;

-- row_hash should change when data changes
DO $$
DECLARE
    hash_before BIGINT;
    hash_after BIGINT;
BEGIN
    SELECT steep_repl.row_hash(t.*) INTO hash_before FROM public.merge_test t WHERE id = 1;
    UPDATE public.merge_test SET value = 999.99 WHERE id = 1;
    SELECT steep_repl.row_hash(t.*) INTO hash_after FROM public.merge_test t WHERE id = 1;

    IF hash_before = hash_after THEN
        RAISE EXCEPTION 'row_hash should change when data changes';
    END IF;

    -- Restore original value
    UPDATE public.merge_test SET value = 100.00 WHERE id = 1;
END $$;

SELECT 'row_hash changes with data' AS test_result;

-- =============================================================================
-- Test overlap types exist
-- =============================================================================

-- overlap_category enum should exist with correct values
SELECT enumlabel FROM pg_enum e
JOIN pg_type t ON e.enumtypid = t.oid
JOIN pg_namespace n ON t.typnamespace = n.oid
WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_category'
ORDER BY enumsortorder;

-- overlap_result type should exist
SELECT a.attname, format_type(a.atttypid, a.atttypmod) AS type
FROM pg_attribute a
JOIN pg_type t ON a.attrelid = t.typrelid
JOIN pg_namespace n ON t.typnamespace = n.oid
WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_result'
AND a.attnum > 0
ORDER BY a.attnum;

-- overlap_summary type should exist
SELECT a.attname, format_type(a.atttypid, a.atttypmod) AS type
FROM pg_attribute a
JOIN pg_type t ON a.attrelid = t.typrelid
JOIN pg_namespace n ON t.typnamespace = n.oid
WHERE n.nspname = 'steep_repl' AND t.typname = 'overlap_summary'
AND a.attnum > 0
ORDER BY a.attnum;

-- =============================================================================
-- Test quiesce_writes and release_quiesce
-- =============================================================================

-- quiesce_writes should succeed on idle table
SELECT steep_repl.quiesce_writes('public', 'merge_test', 1000) AS quiesce_result;

-- release_quiesce should succeed after quiesce
SELECT steep_repl.release_quiesce('public', 'merge_test') AS release_result;

-- Double release returns false (no lock held)
SELECT steep_repl.release_quiesce('public', 'merge_test') AS double_release_result;

-- =============================================================================
-- Test compare_table_rows and compare_table_summary functions exist
-- =============================================================================

-- Verify function signatures exist
SELECT p.proname, pg_get_function_arguments(p.oid) AS args
FROM pg_proc p
JOIN pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = 'steep_repl'
AND p.proname IN ('compare_table_rows', 'compare_table_summary', 'row_hash', 'quiesce_writes', 'release_quiesce')
ORDER BY p.proname;

-- =============================================================================
-- Cleanup
-- =============================================================================
DROP TABLE public.merge_test;
