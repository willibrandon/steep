-- Test schema fingerprint functions
-- Verifies fingerprint computation and storage

-- Create a test table
CREATE TABLE public.fp_test_table (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT now()
);

-- Compute fingerprint - should return 64 hex characters
SELECT length(steep_repl.compute_fingerprint('public', 'fp_test_table')) AS fingerprint_length;

-- Verify fingerprint is hex
SELECT steep_repl.compute_fingerprint('public', 'fp_test_table') ~ '^[0-9a-f]{64}$' AS is_valid_hex;

-- Capture fingerprint (now requires node_id)
SELECT node_id, table_schema, table_name, column_count
FROM steep_repl.capture_fingerprint('test-node', 'public', 'fp_test_table');

-- Verify it's stored
SELECT node_id, table_schema, table_name, fingerprint IS NOT NULL AS has_fingerprint, column_count
FROM steep_repl.schema_fingerprints
WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'fp_test_table';

-- Verify column definitions are stored as JSONB
SELECT jsonb_array_length(column_definitions) AS column_definitions_count
FROM steep_repl.schema_fingerprints
WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'fp_test_table';

-- Add a column and re-capture
ALTER TABLE public.fp_test_table ADD COLUMN updated_at TIMESTAMP;

SELECT node_id, table_schema, table_name, column_count
FROM steep_repl.capture_fingerprint('test-node', 'public', 'fp_test_table');

-- Verify column count updated
SELECT column_count FROM steep_repl.schema_fingerprints
WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'fp_test_table';

-- Test capture_all_fingerprints returns a count (now requires node_id)
SELECT steep_repl.capture_all_fingerprints('test-node') > 0 AS captured_tables;

-- Cleanup
DELETE FROM steep_repl.schema_fingerprints WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'fp_test_table';
DROP TABLE public.fp_test_table;
