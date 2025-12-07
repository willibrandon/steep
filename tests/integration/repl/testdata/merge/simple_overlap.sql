-- Simple overlap test data
-- Tests all four overlap categories: match, conflict, local_only, remote_only
--
-- Node A: rows 1, 2, 3
-- Node B: rows 2, 3, 4
--
-- Expected overlap analysis:
-- - Row 1: local_only (exists only on A)
-- - Row 2: match (identical on both)
-- - Row 3: conflict (same PK, different data)
-- - Row 4: remote_only (exists only on B)

-- =============================================================================
-- Node A Data
-- =============================================================================
-- Insert into local node (Node A)

INSERT INTO public.users (id, name, email, version) VALUES
    (1, 'alice', 'alice@example.com', 'v1'),  -- local_only
    (2, 'bob', 'bob@example.com', 'v1'),      -- match
    (3, 'charlie', 'charlie@a.com', 'v1');    -- conflict (different email)

-- =============================================================================
-- Node B Data
-- =============================================================================
-- Insert into remote node (Node B)

INSERT INTO public.users (id, name, email, version) VALUES
    (2, 'bob', 'bob@example.com', 'v1'),      -- match
    (3, 'charlie', 'charlie@b.com', 'v2'),    -- conflict (different email and version)
    (4, 'diana', 'diana@example.com', 'v1');  -- remote_only
