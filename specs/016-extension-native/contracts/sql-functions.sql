-- SQL Function API Contracts: Extension-Native Architecture
-- Feature: 016-extension-native
-- Date: 2025-12-08
--
-- This file defines the SQL function signatures for the steep_repl extension.
-- These functions provide the API for direct CLI access without a daemon.

-- =============================================================================
-- SNAPSHOT OPERATIONS (FR-002, FR-003, FR-004, FR-006)
-- =============================================================================

-- Start a snapshot generation operation (returns immediately)
-- Queues work for background worker, returns snapshot record
-- Required privilege: SELECT on tables being exported
CREATE OR REPLACE FUNCTION steep_repl.start_snapshot(
    p_output_path TEXT,
    p_compression TEXT DEFAULT 'none',  -- none, gzip, lz4, zstd
    p_parallel INTEGER DEFAULT 4
) RETURNS steep_repl.snapshots
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
-- Implementation in extension (Rust)
-- Validates parameters, creates work_queue entry, returns snapshot record
$$;

-- Query snapshot progress (non-blocking)
-- Returns current progress from shared memory or snapshots table
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.snapshot_progress(
    p_snapshot_id TEXT DEFAULT NULL
) RETURNS TABLE (
    snapshot_id TEXT,
    phase TEXT,
    overall_percent FLOAT,
    tables_completed INTEGER,
    tables_total INTEGER,
    current_table TEXT,
    bytes_processed BIGINT,
    eta_seconds INTEGER,
    error TEXT
)
LANGUAGE sql
STABLE
AS $$
-- Implementation reads from shared memory (if active) or snapshots table
$$;

-- Cancel a running snapshot operation
-- Signals background worker to terminate gracefully
-- Required privilege: Owner of snapshot or superuser
CREATE OR REPLACE FUNCTION steep_repl.cancel_snapshot(
    p_snapshot_id TEXT
) RETURNS BOOLEAN
LANGUAGE plpgsql
AS $$
-- Implementation updates work_queue status to 'cancelled'
-- Background worker checks this and terminates
$$;

-- Wait for snapshot completion (blocking with timeout)
-- Polls progress until complete or timeout
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.wait_snapshot(
    p_snapshot_id TEXT,
    p_timeout_seconds INTEGER DEFAULT 86400  -- 24 hours
) RETURNS steep_repl.snapshots
LANGUAGE plpgsql
AS $$
-- Implementation polls snapshot_progress() with pg_sleep
$$;

-- Start a snapshot apply operation (returns immediately)
-- Required privilege: INSERT on tables being imported
CREATE OR REPLACE FUNCTION steep_repl.start_apply(
    p_input_path TEXT,
    p_parallel INTEGER DEFAULT 4,
    p_verify BOOLEAN DEFAULT true
) RETURNS steep_repl.snapshots
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
-- Implementation similar to start_snapshot
$$;

-- =============================================================================
-- NODE MANAGEMENT (FR-006)
-- =============================================================================

-- Register or update a node
-- Required privilege: INSERT/UPDATE on steep_repl.nodes
CREATE OR REPLACE FUNCTION steep_repl.register_node(
    p_node_id TEXT,
    p_node_name TEXT,
    p_host TEXT DEFAULT NULL,
    p_port INTEGER DEFAULT 5432,
    p_priority INTEGER DEFAULT 50
) RETURNS steep_repl.nodes
LANGUAGE sql
AS $$
    INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
    VALUES (p_node_id, p_node_name, p_host, p_port, p_priority, 'active')
    ON CONFLICT (node_id) DO UPDATE SET
        node_name = EXCLUDED.node_name,
        host = COALESCE(EXCLUDED.host, steep_repl.nodes.host),
        port = EXCLUDED.port,
        priority = EXCLUDED.priority,
        last_seen = now()
    RETURNING *;
$$;

-- Update heartbeat timestamp
-- Required privilege: UPDATE on steep_repl.nodes
CREATE OR REPLACE FUNCTION steep_repl.heartbeat(
    p_node_id TEXT
) RETURNS VOID
LANGUAGE sql
AS $$
    UPDATE steep_repl.nodes
    SET last_seen = now()
    WHERE node_id = p_node_id;
$$;

-- Get node status
-- Required privilege: SELECT on steep_repl.nodes
CREATE OR REPLACE FUNCTION steep_repl.node_status(
    p_node_id TEXT DEFAULT NULL
) RETURNS TABLE (
    node_id TEXT,
    node_name TEXT,
    status TEXT,
    last_seen TIMESTAMPTZ,
    is_healthy BOOLEAN
)
LANGUAGE sql
STABLE
AS $$
    SELECT
        n.node_id,
        n.node_name,
        n.status,
        n.last_seen,
        (n.last_seen > now() - interval '30 seconds') as is_healthy
    FROM steep_repl.nodes n
    WHERE p_node_id IS NULL OR n.node_id = p_node_id
    ORDER BY n.priority DESC, n.node_name;
$$;

-- =============================================================================
-- SCHEMA OPERATIONS (FR-006)
-- =============================================================================

-- Capture schema fingerprints for a node
-- Required privilege: SELECT on information_schema
CREATE OR REPLACE FUNCTION steep_repl.capture_fingerprints(
    p_node_id TEXT
) RETURNS INTEGER  -- Returns count of fingerprints captured
LANGUAGE plpgsql
AS $$
-- Existing implementation in extension
$$;

-- Compare fingerprints between nodes
-- Required privilege: SELECT on steep_repl.schema_fingerprints
CREATE OR REPLACE FUNCTION steep_repl.compare_fingerprints(
    p_local_node TEXT,
    p_peer_node TEXT
) RETURNS TABLE (
    schema_name TEXT,
    table_name TEXT,
    local_fingerprint TEXT,
    peer_fingerprint TEXT,
    status TEXT  -- match, local_only, peer_only, mismatch
)
LANGUAGE sql
STABLE
AS $$
-- Existing implementation in extension
$$;

-- =============================================================================
-- BIDIRECTIONAL MERGE (FR-006)
-- =============================================================================

-- Analyze overlap between local and peer databases
-- Required privilege: USAGE on dblink/postgres_fdw + SELECT on tables
CREATE OR REPLACE FUNCTION steep_repl.analyze_overlap(
    p_peer_connstr TEXT,
    p_tables TEXT[],
    p_primary_keys JSONB DEFAULT NULL  -- {"schema.table": ["pk_col1", "pk_col2"]}
) RETURNS TABLE (
    table_name TEXT,
    local_only_count BIGINT,
    remote_only_count BIGINT,
    match_count BIGINT,
    conflict_count BIGINT
)
LANGUAGE plpgsql
AS $$
-- Implementation creates temp FDW, analyzes each table
$$;

-- Start a bidirectional merge operation
-- Required privilege: USAGE on dblink/postgres_fdw + SELECT/INSERT on tables
CREATE OR REPLACE FUNCTION steep_repl.start_merge(
    p_peer_connstr TEXT,
    p_tables TEXT[],
    p_strategy TEXT DEFAULT 'prefer-local',  -- prefer-local, prefer-remote, last-modified
    p_dry_run BOOLEAN DEFAULT false
) RETURNS steep_repl.merge_audit_log
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
-- Implementation queues work for background worker
$$;

-- Query merge progress
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.merge_progress(
    p_merge_id TEXT DEFAULT NULL
) RETURNS TABLE (
    merge_id TEXT,
    status TEXT,
    current_table TEXT,
    tables_completed INTEGER,
    tables_total INTEGER,
    rows_merged BIGINT,
    conflict_count BIGINT,
    error TEXT
)
LANGUAGE sql
STABLE
AS $$
-- Implementation reads from merge_audit_log
$$;

-- =============================================================================
-- HEALTH CHECK (FR-013)
-- =============================================================================

-- Health check function (alternative to HTTP health endpoint)
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.health()
RETURNS TABLE (
    status TEXT,           -- healthy, degraded, unhealthy
    extension_version TEXT,
    pg_version TEXT,
    background_worker_running BOOLEAN,
    shared_memory_available BOOLEAN,
    active_operations INTEGER,
    last_error TEXT
)
LANGUAGE plpgsql
STABLE
AS $$
-- Implementation checks extension state
$$;

-- =============================================================================
-- WORK QUEUE MANAGEMENT (FR-008, FR-009)
-- =============================================================================

-- List pending and running operations
-- Required privilege: SELECT on steep_repl.work_queue
CREATE OR REPLACE FUNCTION steep_repl.list_operations(
    p_status TEXT DEFAULT NULL  -- NULL = all statuses
) RETURNS SETOF steep_repl.work_queue
LANGUAGE sql
STABLE
AS $$
    SELECT *
    FROM steep_repl.work_queue
    WHERE p_status IS NULL OR status = p_status
    ORDER BY created_at DESC
    LIMIT 100;
$$;

-- Cancel a pending or running operation
-- Required privilege: Owner or superuser
CREATE OR REPLACE FUNCTION steep_repl.cancel_operation(
    p_id BIGINT
) RETURNS BOOLEAN
LANGUAGE plpgsql
AS $$
-- Implementation updates status to 'cancelled'
$$;

-- =============================================================================
-- UTILITY FUNCTIONS
-- =============================================================================

-- Get extension version
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.version()
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT '0.1.0'::TEXT;
$$;

-- Check minimum PostgreSQL version requirement
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.min_pg_version()
RETURNS INTEGER
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT 180000;  -- PostgreSQL 18
$$;

-- Check if background worker features are available
-- (extension must be in shared_preload_libraries)
-- Required privilege: None (public)
CREATE OR REPLACE FUNCTION steep_repl.bgworker_available()
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
AS $$
BEGIN
    -- Check if our background worker is registered
    RETURN EXISTS (
        SELECT 1 FROM pg_stat_activity
        WHERE backend_type = 'background worker'
        AND application_name LIKE 'steep_repl%'
    );
END;
$$;
