-- Test nodes table operations
-- Verifies inserts, constraints, and updates

-- Insert a valid node
INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status, init_state)
VALUES ('node-1', 'Primary', 'pg1.example.com', 5432, 80, 'healthy', 'synchronized');

-- Verify the insert
SELECT node_id, node_name, host, port, priority, status, init_state
FROM steep_repl.nodes WHERE node_id = 'node-1';

-- Test init_state update
UPDATE steep_repl.nodes SET init_state = 'preparing' WHERE node_id = 'node-1';
SELECT init_state FROM steep_repl.nodes WHERE node_id = 'node-1';

-- Insert a second node referencing the first as source
INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status, init_state, init_source_node)
VALUES ('node-2', 'Replica', 'pg2.example.com', 5432, 50, 'healthy', 'copying', 'node-1');

-- Verify the foreign key reference
SELECT n.node_id, n.init_source_node, s.node_name as source_name
FROM steep_repl.nodes n
LEFT JOIN steep_repl.nodes s ON n.init_source_node = s.node_id
WHERE n.node_id = 'node-2';

-- Test coordinator_state insert
INSERT INTO steep_repl.coordinator_state (key, value)
VALUES ('cluster_version', '{"version": 1, "created_at": "2024-01-01"}');

SELECT key, value->>'version' as version FROM steep_repl.coordinator_state WHERE key = 'cluster_version';

-- Test audit_log insert
INSERT INTO steep_repl.audit_log (action, actor, target_type, target_id, success)
VALUES ('node.registered', 'daemon@pg1', 'node', 'node-1', true);

SELECT action, actor, target_type, target_id, success
FROM steep_repl.audit_log WHERE target_id = 'node-1';

-- Cleanup
DELETE FROM steep_repl.audit_log WHERE target_id IN ('node-1', 'node-2');
DELETE FROM steep_repl.coordinator_state WHERE key = 'cluster_version';
DELETE FROM steep_repl.nodes WHERE node_id IN ('node-1', 'node-2');
