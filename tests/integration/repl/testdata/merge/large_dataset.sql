-- Large dataset for performance tests
-- Generates 10,000 rows with known overlap patterns:
-- - 8,000 matches (80%)
-- - 1,000 conflicts (10%)
-- - 500 local_only (5%)
-- - 500 remote_only (5%)
--
-- Usage: This file generates data using SQL functions.
-- Run on each node with appropriate node identifier.

-- =============================================================================
-- Node A Data
-- =============================================================================
-- Generate 9,500 rows: 8,000 matches + 1,000 conflicts + 500 local_only
-- IDs 1-8000: matches (same data on both)
-- IDs 8001-9000: conflicts (same ID, different data)
-- IDs 9001-9500: local_only (only on A)

INSERT INTO public.users (id, name, email, version, created_at, updated_at)
SELECT
    n as id,
    'user_' || n as name,
    'user_' || n || '@example.com' as email,
    CASE
        WHEN n <= 8000 THEN 'v1'           -- matches: same version
        WHEN n <= 9000 THEN 'v1_a'         -- conflicts: A has v1_a
        ELSE 'v1'                           -- local_only: normal version
    END as version,
    '2024-01-01 00:00:00+00'::timestamptz as created_at,
    '2024-01-01 00:00:00+00'::timestamptz as updated_at
FROM generate_series(1, 9500) n;

-- =============================================================================
-- Node B Data
-- =============================================================================
-- Generate 9,500 rows: 8,000 matches + 1,000 conflicts + 500 remote_only
-- IDs 1-8000: matches (same data on both)
-- IDs 8001-9000: conflicts (same ID, different data)
-- IDs 9501-10000: remote_only (only on B)

INSERT INTO public.users (id, name, email, version, created_at, updated_at)
SELECT
    CASE
        WHEN n <= 9000 THEN n              -- rows 1-9000 exist on both
        ELSE n + 500                        -- remote_only: IDs 9501-10000
    END as id,
    'user_' || CASE WHEN n <= 9000 THEN n ELSE n + 500 END as name,
    'user_' || CASE WHEN n <= 9000 THEN n ELSE n + 500 END || '@example.com' as email,
    CASE
        WHEN n <= 8000 THEN 'v1'           -- matches: same version
        WHEN n <= 9000 THEN 'v1_b'         -- conflicts: B has v1_b
        ELSE 'v1'                           -- remote_only: normal version
    END as version,
    '2024-01-01 00:00:00+00'::timestamptz as created_at,
    '2024-01-01 00:00:00+00'::timestamptz as updated_at
FROM generate_series(1, 9500) n;

-- =============================================================================
-- Verification Queries
-- =============================================================================
-- After running on both nodes, verify counts:
--
-- Node A should have: 9,500 rows (IDs 1-9500)
-- Node B should have: 9,500 rows (IDs 1-9000 + 9501-10000)
--
-- Overlap analysis should show:
-- - Matches: 8,000 (IDs 1-8000, identical)
-- - Conflicts: 1,000 (IDs 8001-9000, different version)
-- - Local only: 500 (IDs 9001-9500, only on A)
-- - Remote only: 500 (IDs 9501-10000, only on B)
