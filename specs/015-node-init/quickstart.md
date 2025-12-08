# Quickstart: Node Initialization & Snapshots

**Feature**: 015-node-init
**Date**: 2025-12-04

## Overview

This guide walks through the most common initialization scenarios for Steep bidirectional replication.

## Prerequisites

1. PostgreSQL 18 installed on all nodes
2. `steep_repl` extension installed: `CREATE EXTENSION steep_repl;`
3. `steep-repl` daemon running on all nodes
4. Nodes registered in the cluster

## Scenario 1: Automatic Initialization (Small/Medium DBs)

Best for databases under 100GB.

```bash
# Initialize node_b from node_a
steep-repl node start node_b --from node_a

# Monitor progress in TUI
steep --view replication
```

**What happens:**
1. Schema fingerprints compared (fails in strict mode if mismatch)
2. Subscription created with `copy_data=true`
3. PostgreSQL copies all table data
4. WAL changes applied during catch-up
5. Node transitions to SYNCHRONIZED

**Expected time:** ~30 minutes for 10GB

## Scenario 2: Manual Initialization (Large DBs)

Best for databases over 100GB where you control backup timing.

### Step 1: Prepare on Source

```bash
steep-repl node prepare node_a --slot steep_init_slot

# Output:
#   Slot: steep_init_slot
#   LSN: 0/1A234B00
```

### Step 2: Run Your Backup (outside steep-repl)

```bash
# Option A: pg_basebackup (recommended for large DBs)
pg_basebackup -h node_a_host -D /backup -S steep_init_slot -X stream -P

# Option B: pg_dump (for selective restore)
pg_dump -h node_a_host -Fd -f /backup -j 4 mydb
```

### Step 3: Restore to Target (outside steep-repl)

```bash
# For pg_basebackup: configure new instance from backup
# For pg_dump:
pg_restore -h node_b_host -d mydb -j 4 /backup
```

### Step 4: Complete Initialization

```bash
steep-repl node complete node_b \
    --source node_a \
    --source-lsn 0/1A234B00
```

**What happens:**
1. Schema verified against source
2. `steep_repl` extension and metadata installed
3. Subscription created with `copy_data=false`
4. WAL changes since LSN applied
5. Node transitions to SYNCHRONIZED

## Scenario 3: Two-Phase Snapshot

Best when you want to generate once and apply to multiple nodes, or transfer across networks.

### Phase 1: Generate Snapshot

```bash
steep-repl snapshot generate \
    --source node_a \
    --output /snapshots/2025-12-04 \
    --parallel 8 \
    --compress gzip

# Creates:
# /snapshots/2025-12-04/
# ├── manifest.json
# ├── schema.sql
# ├── data/
# │   ├── public.orders.csv.gz
# │   └── ...
# └── sequences.json
```

### Transfer (if needed)

```bash
rsync -avz /snapshots/2025-12-04 node_b_host:/snapshots/
# Or: aws s3 sync /snapshots/2025-12-04 s3://bucket/snapshots/
```

### Phase 2: Apply Snapshot

```bash
steep-repl snapshot apply \
    --target node_b \
    --input /snapshots/2025-12-04 \
    --parallel 8
```

## Scenario 4: Partial Reinitialization

When specific tables have diverged but the rest of the node is healthy.

```bash
# Reinitialize specific tables
steep-repl node reinit node_b --tables public.orders,public.line_items

# Or reinitialize entire schema
steep-repl node reinit node_b --schema sales
```

**What happens:**
1. Replication paused for affected tables
2. Tables TRUNCATED on target
3. Data copied from source
4. Replication resumed
5. Other tables continue replicating normally

## Scenario 5: Bidirectional Merge (Both Nodes Have Data)

When setting up replication between existing databases with overlapping data.

```bash
# Analyze overlap first
steep-repl analyze-overlap --tables orders,customers

# Output shows conflicts that need resolution

# Resolve and enable replication
steep-repl node merge --mode=bidirectional-merge \
    --strategy prefer-node-a  # or: prefer-node-b, last-modified, manual
```

## Monitoring Progress

### CLI Progress

```bash
# Watch progress
steep-repl node start node_b --from node_a
# Shows real-time progress bar

# Or check status separately
steep-repl status --node node_b
```

### TUI Progress

Press `R` for Replication view in Steep TUI:

```
┌─ Nodes ───────────────────────────────────────────────────────────┐
│                                                                   │
│  Node        State          Lag         Init Progress   Health   │
│  ──────────────────────────────────────────────────────────────── │
│  node_a      SYNCHRONIZED   -           -               ● OK     │
│  node_b      COPYING        -           67% (ETA 14m)   ◐ INIT   │
│                                                                   │
│  [I]nitialize  [R]einitialize  [P]ause  [D]etails               │
└───────────────────────────────────────────────────────────────────┘
```

Press `D` on a node to see detailed progress overlay.

## Schema Validation

Before any initialization, check for schema mismatches:

```bash
steep-repl schema compare node_a node_b

# If mismatches found:
steep-repl schema diff node_a node_b public.customers
```

### Schema Sync Modes

| Mode | Behavior |
|------|----------|
| `strict` (default) | Fail if schemas don't match |
| `auto` | Apply DDL to fix mismatches |
| `manual` | Warn but allow user to proceed |

```bash
# Use auto mode to fix mismatches
steep-repl node start node_b --from node_a --schema-sync auto
```

## Troubleshooting

### Initialization Stuck

```bash
# Check current state
steep-repl status --node node_b

# Check for errors
steep-repl logs --node node_b --tail 100

# Cancel and retry
steep-repl node cancel node_b
steep-repl node start node_b --from node_a
```

### Schema Mismatch in Strict Mode

```bash
# See what's different
steep-repl schema diff node_a node_b public.orders

# Fix manually or use auto mode
steep-repl node start node_b --from node_a --schema-sync auto
```

### Partial Failure

If initialization fails partway:

```bash
# Node is in FAILED state
steep-repl status --node node_b

# View error details
steep-repl logs --node node_b --level error

# Retry from where it left off (if resumable)
steep-repl node start node_b --from node_a --resume

# Or restart completely
steep-repl node start node_b --from node_a --force
```

## Configuration

Key settings in `~/.config/steep/config.yaml`:

```yaml
repl:
  initialization:
    method: snapshot
    parallel_workers: 4
    snapshot_timeout: 24h
    large_table_threshold: 10GB

    schema_sync:
      mode: strict

    storage:
      type: local
      path: /var/steep/snapshots
```

## Next Steps

After successful initialization:
- Verify replication is active: `steep-repl status --node node_b`
- Monitor lag in TUI: Press `R` for Replication view
- Configure alerts for replication issues
