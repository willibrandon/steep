-- Test snapshot_progress table
-- Verifies two-phase snapshot progress tracking (T087b)

-- =============================================================================
-- Test table exists and structure
-- =============================================================================

-- Verify table exists
SELECT EXISTS(
    SELECT 1 FROM pg_tables
    WHERE schemaname = 'steep_repl' AND tablename = 'snapshot_progress'
) AS table_exists;

-- Verify columns exist with correct types
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'steep_repl' AND table_name = 'snapshot_progress'
ORDER BY ordinal_position;

-- =============================================================================
-- Test generation phase progress
-- =============================================================================

INSERT INTO steep_repl.snapshot_progress (
    snapshot_id, phase, overall_percent, current_step,
    tables_total, tables_completed, bytes_total, compression_enabled
) VALUES (
    'snap_gen_test', 'generation', 25.5, 'tables',
    10, 2, 1073741824, true
);

SELECT snapshot_id, phase, overall_percent, current_step, tables_total, tables_completed
FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_gen_test';

-- Update generation progress
UPDATE steep_repl.snapshot_progress
SET overall_percent = 50.0,
    current_step = 'tables',
    tables_completed = 5,
    current_table = 'public.orders',
    bytes_written = 536870912,
    throughput_bytes_sec = 52428800.0,
    eta_seconds = 10
WHERE snapshot_id = 'snap_gen_test';

SELECT overall_percent, tables_completed, current_table, bytes_written, throughput_bytes_sec, eta_seconds
FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_gen_test';

-- =============================================================================
-- Test application phase progress with checksums
-- =============================================================================

INSERT INTO steep_repl.snapshot_progress (
    snapshot_id, phase, overall_percent, current_step,
    tables_total, checksum_verifications, checksums_verified, checksums_failed
) VALUES (
    'snap_app_test', 'application', 75.0, 'checksums',
    15, 15, 12, 0
);

SELECT snapshot_id, phase, current_step, checksum_verifications, checksums_verified, checksums_failed
FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_app_test';

-- Simulate checksum failure
UPDATE steep_repl.snapshot_progress
SET checksums_verified = 14, checksums_failed = 1
WHERE snapshot_id = 'snap_app_test';

SELECT checksums_verified, checksums_failed
FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_app_test';

-- =============================================================================
-- Test constraints
-- =============================================================================

-- Verify phase constraint exists
SELECT conname FROM pg_constraint c
JOIN pg_class r ON c.conrelid = r.oid
JOIN pg_namespace n ON r.relnamespace = n.oid
WHERE n.nspname = 'steep_repl'
AND r.relname = 'snapshot_progress'
AND c.conname = 'snapshot_progress_phase_check';

-- Verify step constraint exists
SELECT conname FROM pg_constraint c
JOIN pg_class r ON c.conrelid = r.oid
JOIN pg_namespace n ON r.relnamespace = n.oid
WHERE n.nspname = 'steep_repl'
AND r.relname = 'snapshot_progress'
AND c.conname = 'snapshot_progress_step_check';

-- Verify percent constraint exists
SELECT conname FROM pg_constraint c
JOIN pg_class r ON c.conrelid = r.oid
JOIN pg_namespace n ON r.relnamespace = n.oid
WHERE n.nspname = 'steep_repl'
AND r.relname = 'snapshot_progress'
AND c.conname = 'snapshot_progress_overall_percent_check';

-- =============================================================================
-- Test indexes exist
-- =============================================================================

SELECT indexname FROM pg_indexes
WHERE schemaname = 'steep_repl' AND tablename = 'snapshot_progress'
ORDER BY indexname;

-- =============================================================================
-- Test error handling
-- =============================================================================

INSERT INTO steep_repl.snapshot_progress (
    snapshot_id, phase, overall_percent, current_step, error_message
) VALUES (
    'snap_error_test', 'generation', 30.0, 'tables', 'Connection lost during export'
);

SELECT snapshot_id, phase, overall_percent, error_message
FROM steep_repl.snapshot_progress WHERE snapshot_id = 'snap_error_test';

-- =============================================================================
-- Cleanup
-- =============================================================================

DELETE FROM steep_repl.snapshot_progress WHERE snapshot_id IN ('snap_gen_test', 'snap_app_test', 'snap_error_test');
