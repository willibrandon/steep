# CLI Commands: Node Initialization & Snapshots

**Feature**: 015-node-init
**Date**: 2025-12-04

## Command Overview

All commands are subcommands of `steep-repl`:

```
steep-repl node       # Node initialization and management
steep-repl snapshot   # Two-phase snapshot operations
steep-repl schema     # Schema comparison and fingerprinting
```

Node subcommands:
```
steep-repl node start <target>     # Start automatic snapshot initialization
steep-repl node prepare <node>     # Prepare for manual initialization
steep-repl node complete <target>  # Complete manual initialization
steep-repl node cancel <node>      # Cancel in-progress initialization
steep-repl node progress <node>    # Check initialization progress
steep-repl node reinit <node>      # Reinitialize a diverged node
steep-repl node merge <a> <b>      # Merge two nodes with existing data
```

---

## steep-repl node

Initialize a node for bidirectional replication.

### Automatic Snapshot Initialization

```bash
steep-repl node start <target-node> --from <source-node> [options]
```

**Arguments:**
- `<target-node>`: Node ID to initialize
- `--from <source-node>`: Source node to copy data from

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--method` | snapshot | Init method: snapshot, direct, two-phase |
| `--parallel` | 4 | Number of parallel workers (1-16) |
| `--schema-sync` | strict | Schema sync mode: strict, auto, manual |
| `--force` | false | Truncate existing data on target |
| `--large-table-threshold` | 10GB | Threshold for special handling |
| `--large-table-method` | pg_dump | Method for large tables |
| `--timeout` | 24h | Maximum initialization time |

**Examples:**
```bash
# Basic automatic initialization
steep-repl node start node_b --from node_a

# With parallel workers and auto schema sync
steep-repl node start node_b --from node_a --parallel 8 --schema-sync auto

# Force reinit with truncation
steep-repl node start node_b --from node_a --force
```

**Output:**
```
Initializing node_b from node_a...
  Method: snapshot
  Parallel workers: 4
  Schema sync: strict

Checking schemas...
  Tables: 23 tables
  Fingerprints: All match ✓

Starting data copy...
  Progress: 45% (10 of 23 tables)
  Current: orders (1.2GB) - 71% @ 42,000 rows/sec
  ETA: 8m 34s

Press Ctrl+C to cancel
```

### Manual Initialization (Prepare)

```bash
steep-repl node prepare <node> --slot <slot-name>
```

**Arguments:**
- `<node>`: Node to prepare for initialization

**Options:**
- `--slot <slot-name>`: Replication slot name to create

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--expires` | 168h | Slot expiration time |

**Example:**
```bash
steep-repl node prepare node_a --slot steep_init_20251204

# Output:
# Replication slot created
#   Slot: steep_init_20251204
#   LSN: 0/1A234B00
#   Expires: 2025-12-11T14:30:00Z
#
# Next steps:
#   1. Run your backup: pg_basebackup -D /backup -S steep_init_20251204
#   2. Restore to target node
#   3. Run: steep-repl node complete <target> --source node_a --source-lsn 0/1A234B00
```

### Manual Initialization (Complete)

```bash
steep-repl node complete <target> --source <source> --source-lsn <lsn>
```

**Arguments:**
- `<target>`: Target node that received the backup

**Required Flags:**
- `--source <source>`: Source node the backup came from
- `--source-lsn <lsn>`: LSN from the prepare step

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--schema-sync` | strict | Schema sync mode |
| `--skip-schema-check` | false | Skip schema verification |

**Example:**
```bash
steep-repl node complete node_b --source node_a --source-lsn 0/1A234B00

# Output:
# Completing initialization of node_b...
#   Source: node_a
#   LSN: 0/1A234B00
#
# Verifying schema...
#   23 tables checked
#   All schemas match ✓
#
# Installing steep_repl metadata...
#   Extension installed ✓
#   Ranges allocated ✓
#
# Creating subscription...
#   Subscription created with copy_data=false
#   Catching up from 0/1A234B00...
#   WAL applied: 1.2MB
#
# Initialization complete!
#   State: SYNCHRONIZED
```

### Cancel Initialization

```bash
steep-repl node cancel <node> [--cleanup]
```

**Arguments:**
- `<node>`: Node to cancel initialization for

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--cleanup` | true | Cleanup partial data |

---

## steep-repl node reinit

Reinitialize a diverged or corrupted node.

### Syntax

```bash
steep-repl node reinit <node> [scope] [options]
```

**Scope (mutually exclusive):**
| Option | Description |
|--------|-------------|
| `--full` | Full node reinitialization |
| `--tables <list>` | Specific tables (comma-separated) |
| `--schema <name>` | All tables in schema |

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--source` | auto | Source node for data |
| `--parallel` | 4 | Parallel workers |
| `--confirm` | false | Skip confirmation prompt |

**Examples:**
```bash
# Reinitialize specific tables
steep-repl node reinit node_b --tables public.orders,public.line_items

# Reinitialize entire schema
steep-repl node reinit node_b --schema sales

# Full reinitialization
steep-repl node reinit node_b --full --confirm
```

**Output:**
```
Reinitialization of node_b
  Scope: tables (2 tables)
  Tables: public.orders, public.line_items
  Source: node_a (auto-selected)

WARNING: This will TRUNCATE the following tables on node_b:
  - public.orders (1.5M rows, 524MB)
  - public.line_items (4.2M rows, 1.8GB)

Continue? [y/N]: y

Pausing replication for affected tables...
Truncating tables...
Copying data...
  public.orders: 100% (1.5M rows) ✓
  public.line_items: 67% @ 85,000 rows/sec

Reinitialization complete!
  Duration: 4m 23s
  Rows copied: 5.7M
  Bytes: 2.3GB
```

---

## steep-repl snapshot

Two-phase snapshot operations.

### Generate Snapshot

```bash
steep-repl snapshot generate <source-node> --output <path> [options]
```

**Arguments:**
- `<source-node>`: Source node to snapshot

**Required Flags:**
- `--output <path>`: Output directory or S3 path

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--parallel` | 4 | Parallel workers |
| `--compress` | gzip | Compression: none, gzip, lz4, zstd |
| `--checksum` | sha256 | Checksum algorithm |

**Example:**
```bash
steep-repl snapshot generate node_a --output /snapshots/2025-12-04

# Output:
# Generating snapshot from node_a...
#   Output: /snapshots/2025-12-04
#   Parallel workers: 4
#   Compression: gzip
#
# Creating replication slot...
#   Slot: steep_snapshot_20251204_143022
#   LSN: 0/1A234B00
#
# Exporting schema...
#   23 tables
#   45 indexes
#   Schema saved ✓
#
# Exporting data...
#   Progress: 62% (14 of 23 tables)
#   Current: orders - 71% @ 168,000 rows/sec
#   Output size: 3.2GB
#
# Capturing sequences...
#   15 sequences saved ✓
#
# Snapshot complete!
#   Location: /snapshots/2025-12-04
#   Size: 4.2GB (compressed)
#   LSN: 0/1A234B00
#   Checksum: sha256:abc123...
```

### Apply Snapshot

```bash
steep-repl snapshot apply <target-node> --input <path> [options]
```

**Arguments:**
- `<target-node>`: Target node to apply snapshot to

**Required Flags:**
- `--input <path>`: Snapshot directory or S3 path

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--source` | auto | Source node for subscription |
| `--parallel` | 4 | Parallel workers |
| `--verify` | true | Verify checksums |

### List Snapshots

```bash
steep-repl snapshot list [--source <node>]
```

### Delete Snapshot

```bash
steep-repl snapshot delete <snapshot-id>
```

---

## steep-repl schema

Schema comparison and fingerprinting.

### Compare Schemas

```bash
steep-repl schema compare <node-a> <node-b> [options]
```

**Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `--schemas` | all | Specific schemas to compare |
| `--format` | table | Output: table, json |

**Example:**
```bash
steep-repl schema compare node_a node_b

# Output:
# Schema Comparison: node_a ↔ node_b
#
# Table                  Status     Difference
# ───────────────────────────────────────────────
# public.orders          MATCH      -
# public.customers       MISMATCH   Column 'loyalty_tier' missing on node_b
# public.products        MATCH      -
# public.inventory       LOCAL_ONLY -
#
# Summary: 2 match, 1 mismatch, 1 local only, 0 remote only
```

### Show Diff

```bash
steep-repl schema diff <node-a> <node-b> <table>
```

**Example:**
```bash
steep-repl schema diff node_a node_b public.customers

# Output:
# Schema Diff: public.customers
#
# node_a                              node_b
# ─────────────────────────────────────────────────
# id INTEGER NOT NULL                 id INTEGER NOT NULL
# name TEXT NOT NULL                  name TEXT NOT NULL
# email TEXT NOT NULL                 email TEXT NOT NULL
# + loyalty_tier TEXT                 (missing)
# created_at TIMESTAMPTZ              created_at TIMESTAMPTZ
```

### Capture Fingerprints

```bash
steep-repl schema capture <node> [--schemas <list>]
```

### Export Fingerprints

```bash
steep-repl schema export <node> --output <file>
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Schema mismatch (strict mode) |
| 4 | Initialization cancelled |
| 5 | Timeout |
| 6 | Connection error |

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `STEEP_REPL_CONFIG` | Path to config file |
| `STEEP_REPL_NODE_ID` | Default node ID |
| `STEEP_REPL_PARALLEL` | Default parallel workers |
