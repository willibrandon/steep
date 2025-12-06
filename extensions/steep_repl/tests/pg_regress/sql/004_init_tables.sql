-- Test initialization-related tables
-- Verifies init_progress, init_slots, and snapshots

-- Create a node first (required for foreign keys)
INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
VALUES ('init-test-node', 'Init Test', 'localhost', 5432, 50, 'healthy');

-- Test init_progress table
INSERT INTO steep_repl.init_progress (node_id, phase, overall_percent, tables_total, tables_completed)
VALUES ('init-test-node', 'copying', 45.5, 100, 45);

SELECT node_id, phase, overall_percent, tables_total, tables_completed
FROM steep_repl.init_progress WHERE node_id = 'init-test-node';

-- Update progress
UPDATE steep_repl.init_progress
SET overall_percent = 50.0, tables_completed = 50, current_table = 'public.users'
WHERE node_id = 'init-test-node';

SELECT overall_percent, tables_completed, current_table
FROM steep_repl.init_progress WHERE node_id = 'init-test-node';

-- Test init_slots table
INSERT INTO steep_repl.init_slots (slot_name, node_id, lsn, expires_at)
VALUES ('steep_init_slot_1', 'init-test-node', '0/1ABC000', now() + interval '1 hour');

SELECT slot_name, node_id, lsn, expires_at IS NOT NULL AS has_expiry
FROM steep_repl.init_slots WHERE slot_name = 'steep_init_slot_1';

-- Test snapshots table
INSERT INTO steep_repl.snapshots
(snapshot_id, source_node_id, lsn, storage_path, size_bytes, table_count, checksum, status)
VALUES ('snap-001', 'init-test-node', '0/1ABC000', '/backups/snap-001', 1073741824, 50, 'sha256:abc123', 'complete');

SELECT snapshot_id, source_node_id, size_bytes, table_count, compression, status
FROM steep_repl.snapshots WHERE snapshot_id = 'snap-001';

-- Verify default compression
SELECT compression FROM steep_repl.snapshots WHERE snapshot_id = 'snap-001';

-- Cleanup (cascades to init_progress)
DELETE FROM steep_repl.init_slots WHERE slot_name = 'steep_init_slot_1';
DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap-001';
DELETE FROM steep_repl.nodes WHERE node_id = 'init-test-node';
