# Quickstart: Extension-Native Architecture

**Feature**: 016-extension-native
**Date**: 2025-12-08

## Overview

The extension-native architecture eliminates the need for a separate steep-repl daemon. When PostgreSQL is running with the steep_repl extension, all replication features are available.

## Prerequisites

1. PostgreSQL 18+ installed
2. steep_repl extension built and installed
3. Extension added to `shared_preload_libraries` (for background worker support)

## Setup

### 1. Configure PostgreSQL

Add to `postgresql.conf`:

```ini
shared_preload_libraries = 'steep_repl'
```

Restart PostgreSQL:

```bash
pg_ctl restart -D /path/to/data
```

### 2. Create Extension

```sql
CREATE EXTENSION IF NOT EXISTS steep_repl;
```

### 3. Create Dedicated Role (Optional but Recommended)

```sql
-- Create role with REPLICATION attribute
CREATE ROLE steep_repl WITH LOGIN REPLICATION PASSWORD 'secure_password';

-- Grant permissions on steep_repl schema
GRANT USAGE ON SCHEMA steep_repl TO steep_repl;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA steep_repl TO steep_repl;

-- Grant permissions on user tables
GRANT SELECT ON ALL TABLES IN SCHEMA public TO steep_repl;  -- for export
GRANT INSERT ON ALL TABLES IN SCHEMA public TO steep_repl;  -- for import
```

## Usage

### CLI Direct Mode

Use the `--direct` flag to connect directly to PostgreSQL:

```bash
# Generate a snapshot
steep-repl snapshot generate my-node \
    --output /backups/snapshot \
    --direct \
    -c "postgresql://steep_repl:password@localhost:5432/mydb"

# Compare schemas between nodes
steep-repl schema compare node-a node-b --direct

# Check node status
steep-repl node status --direct
```

### SQL Function API

All operations are also available as SQL functions:

```sql
-- Start a snapshot (returns immediately)
SELECT * FROM steep_repl.start_snapshot('/backups/snapshot', 'zstd', 4);

-- Check progress
SELECT * FROM steep_repl.snapshot_progress();

-- Register a node
SELECT * FROM steep_repl.register_node('my-node', 'My Node', 'localhost', 5432);

-- Analyze overlap with peer
SELECT * FROM steep_repl.analyze_overlap(
    'host=peer.example.com dbname=mydb',
    ARRAY['public.users', 'public.orders']
);

-- Start a merge
SELECT * FROM steep_repl.start_merge(
    'host=peer.example.com dbname=mydb',
    ARRAY['public.users'],
    'prefer-local'
);

-- Check health
SELECT * FROM steep_repl.health();
```

### Real-Time Progress Monitoring

Subscribe to progress notifications:

```sql
-- In psql session 1: Listen for progress
LISTEN steep_repl_progress;

-- In psql session 2: Start a snapshot
SELECT * FROM steep_repl.start_snapshot('/backups/snapshot', 'zstd');

-- Session 1 receives notifications like:
-- {"op":"snapshot","snapshot_id":"snap_20251208_123456","phase":"data","percent":45.2,"table":"public.orders"}
```

## Mode Detection

When neither `--direct` nor `--remote` is specified, the CLI auto-detects:

1. **Try direct mode first** - connects to PostgreSQL and checks for extension
2. **Fall back to daemon** - if extension not available, try gRPC connection

```bash
# Auto-detect mode (prefers direct)
steep-repl snapshot generate my-node --output /backups/snapshot

# Force direct mode (fails if extension not available)
steep-repl snapshot generate my-node --output /backups/snapshot --direct

# Force daemon mode (uses gRPC)
steep-repl snapshot generate my-node --output /backups/snapshot --remote localhost:15460
```

## Comparison: Daemon vs Direct Mode

| Aspect | Daemon Mode (`--remote`) | Direct Mode (`--direct`) |
|--------|--------------------------|-------------------------|
| Process Required | steep-repl daemon | PostgreSQL only |
| Configuration | YAML + gRPC config | Connection string only |
| Progress | gRPC streaming | LISTEN/NOTIFY |
| Long Operations | Daemon handles | Background worker |
| Network Ports | gRPC port (15460) | PostgreSQL port (5432) |
| Authentication | TLS certificates | PostgreSQL auth |

## Troubleshooting

### Background Worker Not Running

If `steep_repl.bgworker_available()` returns `false`:

1. Check `shared_preload_libraries` includes `steep_repl`
2. Restart PostgreSQL after configuration change
3. Check PostgreSQL logs for extension load errors

### Extension Not Available

If `--direct` mode fails:

1. Verify extension is installed: `\dx steep_repl`
2. Check PostgreSQL version: requires 18+
3. Ensure you have connection permissions

### Progress Not Updating

If progress stays at 0%:

1. Check background worker is running: `SELECT * FROM pg_stat_activity WHERE backend_type = 'background worker'`
2. Verify work queue has entry: `SELECT * FROM steep_repl.work_queue WHERE status = 'running'`
3. Check for errors: `SELECT error_message FROM steep_repl.snapshots WHERE status = 'failed'`

## Migration from Daemon Mode

To migrate existing deployments:

1. Update extension to version with background worker support
2. Add `shared_preload_libraries = 'steep_repl'` to postgresql.conf
3. Restart PostgreSQL
4. Test with `--direct` flag
5. Optionally stop daemon service once verified
6. Remove daemon configuration sections from config.yaml
