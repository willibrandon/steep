-- Test steep_repl schema and table existence
-- Verifies all tables are created correctly

-- Schema exists
SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'steep_repl') AS schema_exists;

-- All tables exist
SELECT tablename FROM pg_tables
WHERE schemaname = 'steep_repl'
ORDER BY tablename;

-- Check nodes table columns
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'steep_repl' AND table_name = 'nodes'
ORDER BY ordinal_position;

-- Check coordinator_state table columns
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'steep_repl' AND table_name = 'coordinator_state'
ORDER BY ordinal_position;

-- Check audit_log table columns
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_schema = 'steep_repl' AND table_name = 'audit_log'
ORDER BY ordinal_position;
