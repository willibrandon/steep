# Node Command Redesign Proposal

**Created**: 2025-12-09
**Status**: Draft
**Context**: T027 revealed fundamental design issues with `cmd_node.go`

---

## Executive Summary

The current `node` command conflates four distinct concerns into one monolithic 937-line file:
1. Node registration/health (CRUD on nodes table)
2. Replication infrastructure (publications, subscriptions, slots)
3. Initialization workflows (orchestrated multi-step operations)
4. Data operations (merge - duplicated from cmd_merge.go)

This proposal separates these concerns into focused commands with clear extension function mappings.

---

## Current State Analysis

### cmd_node.go Subcommands (7 total)

| Subcommand | Purpose | gRPC Method | Extension Functions Used |
|------------|---------|-------------|-------------------------|
| `start` | Auto-init target from source | StartInit | None - daemon orchestrates |
| `prepare` | Create slot, record LSN | PrepareInit | None - daemon orchestrates |
| `complete` | Create subscription from LSN | CompleteInit | None - daemon orchestrates |
| `cancel` | Drop subscription, reset state | CancelInit | None - daemon orchestrates |
| `progress` | Show init progress | GetProgress | None - daemon orchestrates |
| `reinit` | Reset node for re-init | StartReinit | None - daemon orchestrates |
| `merge` | Bidirectional data merge | StartBidirectionalMerge | None - daemon orchestrates |

### Problems Identified

1. **No basic node operations**: Cannot `register`, `list`, `remove` nodes
2. **All operations require daemon**: No direct mode support
3. **Naming mismatch**: Functions named `Init*` but commands lack `init` prefix
4. **Duplication**: `node merge` duplicates `cmd_merge.go` functionality
5. **Missing replication primitives**: No way to manage pub/sub/slots directly
6. **Extension functions underutilized**: `register_node()`, `heartbeat()`, `node_status()` exist but unused

### Extension Functions That Exist

**Node Management (nodes.rs):**
- `steep_repl.register_node(id, name, host, port, priority)` - EXISTS
- `steep_repl.heartbeat(id)` - EXISTS
- `steep_repl.node_status(id)` - EXISTS

**Progress (progress.rs):**
- `steep_repl.get_progress()` - EXISTS
- `steep_repl.is_operation_active()` - EXISTS

**Work Queue (work_queue.rs):**
- `steep_repl.list_operations(status)` - EXISTS
- `steep_repl.cancel_work(id)` - EXISTS

**Merge (merge.rs):**
- `steep_repl.compare_table_rows()` - EXISTS
- `steep_repl.compare_table_summary()` - EXISTS
- `steep_repl.quiesce_writes()` - EXISTS
- `steep_repl.release_quiesce()` - EXISTS

**Merge Audit (merge_audit_log.rs):**
- `steep_repl.log_merge_decision()` - EXISTS
- `steep_repl.get_merge_summary()` - EXISTS
- `steep_repl.get_merge_conflicts()` - EXISTS

### Extension Functions That Are MISSING

**Publication Management:**
- `steep_repl.create_publication(name, tables[])` - MISSING
- `steep_repl.drop_publication(name)` - MISSING
- `steep_repl.list_publications()` - MISSING
- `steep_repl.add_publication_tables(name, tables[])` - MISSING
- `steep_repl.remove_publication_tables(name, tables[])` - MISSING

**Subscription Management:**
- `steep_repl.create_subscription(name, connstr, publication, slot, copy_data)` - MISSING
- `steep_repl.drop_subscription(name)` - MISSING
- `steep_repl.enable_subscription(name)` - MISSING
- `steep_repl.disable_subscription(name)` - MISSING
- `steep_repl.list_subscriptions()` - MISSING
- `steep_repl.subscription_status(name)` - MISSING
- `steep_repl.refresh_subscription(name)` - MISSING

**Replication Slot Management:**
- `steep_repl.create_replication_slot(name, plugin)` - MISSING
- `steep_repl.drop_replication_slot(name)` - MISSING
- `steep_repl.list_replication_slots()` - MISSING
- `steep_repl.slot_lag(name)` - MISSING

**Node State Management:**
- `steep_repl.remove_node(id)` - MISSING
- `steep_repl.update_node_state(id, state)` - MISSING
- `steep_repl.list_nodes()` - MISSING (node_status with NULL works but not named clearly)

**Origin Tracking (for conflict detection):**
- `steep_repl.create_replication_origin(name)` - MISSING
- `steep_repl.drop_replication_origin(name)` - MISSING
- `steep_repl.list_replication_origins()` - MISSING
- `steep_repl.advance_origin(name, lsn)` - MISSING

---

## Proposed Architecture

### Command Structure

```
steep-repl
├── node                    # Node CRUD (register, list, status, remove)
├── replication             # Replication primitives (pub, sub, slot)
│   ├── pub                 # Publication management
│   ├── sub                 # Subscription management
│   └── slot                # Replication slot management
├── init                    # Initialization workflows (auto, prepare, complete, reset)
├── schema                  # Schema operations (existing - capture, compare, diff)
├── snapshot                # Snapshot operations (existing - generate, apply)
├── merge                   # Merge operations (existing - analyze, start)
└── ops                     # Work queue operations (list, status, cancel)
```

### New Files

| File | Purpose | Subcommands |
|------|---------|-------------|
| `cmd_node.go` | Node CRUD | register, list, status, heartbeat, remove |
| `cmd_replication.go` | Replication primitives | pub/sub/slot subcommands |
| `cmd_init.go` | Initialization workflows | auto, prepare, complete, cancel, status, reset |
| `cmd_ops.go` | Work queue management | list, status, cancel |

### cmd_node.go (Simplified)

```go
// 5 subcommands, all map directly to extension functions

steep-repl node register <id> --name <name> --host <host> --port <port> --priority <n>
    → steep_repl.register_node(id, name, host, port, priority)
    → Direct mode: Yes

steep-repl node list
    → steep_repl.node_status(NULL)  // Returns all nodes
    → Direct mode: Yes

steep-repl node status [id]
    → steep_repl.node_status(id)
    → Direct mode: Yes

steep-repl node heartbeat <id>
    → steep_repl.heartbeat(id)
    → Direct mode: Yes

steep-repl node remove <id>
    → steep_repl.remove_node(id)  // NEW function needed
    → Direct mode: Yes
```

### cmd_replication.go (New)

```go
// Publication Management
steep-repl replication pub create <name> --tables <t1,t2,...>
    → steep_repl.create_publication(name, tables)
    → Direct mode: Yes

steep-repl replication pub drop <name>
    → steep_repl.drop_publication(name)
    → Direct mode: Yes

steep-repl replication pub list
    → steep_repl.list_publications()
    → Direct mode: Yes

steep-repl replication pub add-tables <name> --tables <t1,t2,...>
    → steep_repl.add_publication_tables(name, tables)
    → Direct mode: Yes

steep-repl replication pub remove-tables <name> --tables <t1,t2,...>
    → steep_repl.remove_publication_tables(name, tables)
    → Direct mode: Yes

// Subscription Management
steep-repl replication sub create <name> --connection <connstr> --publication <pub> [--slot <slot>] [--copy-data]
    → steep_repl.create_subscription(name, connstr, pub, slot, copy_data)
    → Direct mode: Yes

steep-repl replication sub drop <name>
    → steep_repl.drop_subscription(name)
    → Direct mode: Yes

steep-repl replication sub enable <name>
    → steep_repl.enable_subscription(name)
    → Direct mode: Yes

steep-repl replication sub disable <name>
    → steep_repl.disable_subscription(name)
    → Direct mode: Yes

steep-repl replication sub list
    → steep_repl.list_subscriptions()
    → Direct mode: Yes

steep-repl replication sub status [name]
    → steep_repl.subscription_status(name)
    → Direct mode: Yes

steep-repl replication sub refresh <name>
    → steep_repl.refresh_subscription(name)
    → Direct mode: Yes

// Replication Slot Management
steep-repl replication slot create <name> [--plugin pgoutput]
    → steep_repl.create_replication_slot(name, plugin)
    → Direct mode: Yes

steep-repl replication slot drop <name>
    → steep_repl.drop_replication_slot(name)
    → Direct mode: Yes

steep-repl replication slot list
    → steep_repl.list_replication_slots()
    → Direct mode: Yes

steep-repl replication slot lag [name]
    → steep_repl.slot_lag(name)
    → Direct mode: Yes
```

### cmd_init.go (New - Extracted from cmd_node.go)

```go
// Initialization Workflows (orchestration over primitives)
// These may use daemon OR direct mode depending on complexity

steep-repl init auto <target> --from <source> [--parallel <n>]
    → Complex workflow:
       1. steep_repl.create_publication() on source
       2. steep_repl.create_replication_slot() on source
       3. steep_repl.create_subscription() on target with copy_data=true
       4. Monitor progress via steep_repl.get_progress()
    → Direct mode: Possible with --conn-source and --conn-target

steep-repl init prepare <node> [--slot <name>]
    → steep_repl.create_replication_slot(name)
    → steep_repl.update_node_state(node, 'preparing')
    → Record LSN
    → Direct mode: Yes

steep-repl init complete <target> --from <source> --lsn <lsn>
    → steep_repl.create_subscription() starting from LSN
    → steep_repl.update_node_state(node, 'catching_up')
    → Direct mode: Yes

steep-repl init cancel <node>
    → steep_repl.drop_subscription() if exists
    → steep_repl.update_node_state(node, 'uninitialized')
    → Direct mode: Yes

steep-repl init status <node>
    → steep_repl.node_status(node)  // includes init_state
    → steep_repl.get_progress() if operation active
    → Direct mode: Yes

steep-repl init reset <node> [--full | --tables <t1,t2> | --schema <s>]
    → steep_repl.update_node_state(node, 'reinitializing')
    → Optionally truncate specified tables
    → Direct mode: Yes
```

### cmd_ops.go (New)

```go
// Work Queue Operations

steep-repl ops list [--status pending|running|completed|failed]
    → steep_repl.list_operations(status)
    → Direct mode: Yes

steep-repl ops status <id>
    → steep_repl.get_progress() filtered by operation
    → Direct mode: Yes

steep-repl ops cancel <id>
    → steep_repl.cancel_work(id)
    → Direct mode: Yes
```

---

## Extension Implementation Plan

### Phase 1: Node Management Completion

**File: nodes.rs**

Add:
```sql
-- Remove a node from the cluster
CREATE FUNCTION steep_repl.remove_node(p_node_id TEXT) RETURNS BOOLEAN

-- List all nodes (explicit name, clearer than node_status(NULL))
CREATE FUNCTION steep_repl.list_nodes() RETURNS TABLE(...)

-- Update node initialization state
CREATE FUNCTION steep_repl.update_node_state(p_node_id TEXT, p_state TEXT) RETURNS BOOLEAN
```

### Phase 2: Replication Slot Management

**New File: replication_slots.rs**

```sql
-- Create logical replication slot
CREATE FUNCTION steep_repl.create_replication_slot(
    p_slot_name TEXT,
    p_plugin TEXT DEFAULT 'pgoutput'
) RETURNS TABLE(slot_name TEXT, lsn PG_LSN)

-- Drop replication slot
CREATE FUNCTION steep_repl.drop_replication_slot(p_slot_name TEXT) RETURNS BOOLEAN

-- List replication slots
CREATE FUNCTION steep_repl.list_replication_slots() RETURNS TABLE(
    slot_name TEXT,
    plugin TEXT,
    slot_type TEXT,
    active BOOLEAN,
    restart_lsn PG_LSN,
    confirmed_flush_lsn PG_LSN,
    wal_status TEXT
)

-- Get slot lag in bytes
CREATE FUNCTION steep_repl.slot_lag(p_slot_name TEXT DEFAULT NULL) RETURNS TABLE(
    slot_name TEXT,
    lag_bytes BIGINT,
    lag_pretty TEXT
)
```

### Phase 3: Publication Management

**New File: publications.rs**

```sql
-- Create publication
CREATE FUNCTION steep_repl.create_publication(
    p_name TEXT,
    p_tables TEXT[]
) RETURNS TEXT

-- Drop publication
CREATE FUNCTION steep_repl.drop_publication(p_name TEXT) RETURNS BOOLEAN

-- List publications
CREATE FUNCTION steep_repl.list_publications() RETURNS TABLE(
    pubname TEXT,
    pubowner TEXT,
    puballtables BOOLEAN,
    tables TEXT[]
)

-- Add tables to publication
CREATE FUNCTION steep_repl.add_publication_tables(
    p_name TEXT,
    p_tables TEXT[]
) RETURNS INTEGER

-- Remove tables from publication
CREATE FUNCTION steep_repl.remove_publication_tables(
    p_name TEXT,
    p_tables TEXT[]
) RETURNS INTEGER
```

### Phase 4: Subscription Management

**New File: subscriptions.rs**

```sql
-- Create subscription
CREATE FUNCTION steep_repl.create_subscription(
    p_name TEXT,
    p_conninfo TEXT,
    p_publication TEXT,
    p_slot_name TEXT DEFAULT NULL,
    p_copy_data BOOLEAN DEFAULT true,
    p_enabled BOOLEAN DEFAULT true
) RETURNS TEXT

-- Drop subscription
CREATE FUNCTION steep_repl.drop_subscription(p_name TEXT) RETURNS BOOLEAN

-- Enable subscription
CREATE FUNCTION steep_repl.enable_subscription(p_name TEXT) RETURNS BOOLEAN

-- Disable subscription
CREATE FUNCTION steep_repl.disable_subscription(p_name TEXT) RETURNS BOOLEAN

-- List subscriptions
CREATE FUNCTION steep_repl.list_subscriptions() RETURNS TABLE(
    subname TEXT,
    subowner TEXT,
    subenabled BOOLEAN,
    subconninfo TEXT,
    subslotname TEXT,
    subpublications TEXT[]
)

-- Subscription status (replication state)
CREATE FUNCTION steep_repl.subscription_status(p_name TEXT DEFAULT NULL) RETURNS TABLE(
    subname TEXT,
    pid INTEGER,
    relid REGCLASS,
    received_lsn PG_LSN,
    last_msg_send_time TIMESTAMPTZ,
    last_msg_receipt_time TIMESTAMPTZ,
    latest_end_lsn PG_LSN,
    latest_end_time TIMESTAMPTZ
)

-- Refresh subscription (sync new tables)
CREATE FUNCTION steep_repl.refresh_subscription(
    p_name TEXT,
    p_copy_data BOOLEAN DEFAULT false
) RETURNS BOOLEAN
```

### Phase 5: Origin Tracking (for conflict detection)

**New File: origins.rs**

```sql
-- Create replication origin
CREATE FUNCTION steep_repl.create_replication_origin(p_name TEXT) RETURNS OID

-- Drop replication origin
CREATE FUNCTION steep_repl.drop_replication_origin(p_name TEXT) RETURNS BOOLEAN

-- List replication origins
CREATE FUNCTION steep_repl.list_replication_origins() RETURNS TABLE(
    roident OID,
    roname TEXT,
    remote_lsn PG_LSN,
    local_lsn PG_LSN
)

-- Advance origin (for conflict resolution)
CREATE FUNCTION steep_repl.advance_origin(
    p_name TEXT,
    p_lsn PG_LSN
) RETURNS BOOLEAN
```

---

## Task Additions for spec 016

Add to tasks.md (after existing tasks):

```markdown
## Phase 10: Node Command Redesign

### Extension: Node Management Completion
- [ ] T071 [P] Implement steep_repl.remove_node() in nodes.rs
- [ ] T072 [P] Implement steep_repl.list_nodes() in nodes.rs
- [ ] T073 [P] Implement steep_repl.update_node_state() in nodes.rs

### Extension: Replication Slot Management
- [ ] T074 Create replication_slots.rs with table and schema
- [ ] T075 [P] Implement steep_repl.create_replication_slot()
- [ ] T076 [P] Implement steep_repl.drop_replication_slot()
- [ ] T077 [P] Implement steep_repl.list_replication_slots()
- [ ] T078 [P] Implement steep_repl.slot_lag()

### Extension: Publication Management
- [ ] T079 Create publications.rs with helper functions
- [ ] T080 [P] Implement steep_repl.create_publication()
- [ ] T081 [P] Implement steep_repl.drop_publication()
- [ ] T082 [P] Implement steep_repl.list_publications()
- [ ] T083 [P] Implement steep_repl.add_publication_tables()
- [ ] T084 [P] Implement steep_repl.remove_publication_tables()

### Extension: Subscription Management
- [ ] T085 Create subscriptions.rs with table and schema
- [ ] T086 [P] Implement steep_repl.create_subscription()
- [ ] T087 [P] Implement steep_repl.drop_subscription()
- [ ] T088 [P] Implement steep_repl.enable_subscription()
- [ ] T089 [P] Implement steep_repl.disable_subscription()
- [ ] T090 [P] Implement steep_repl.list_subscriptions()
- [ ] T091 [P] Implement steep_repl.subscription_status()
- [ ] T092 [P] Implement steep_repl.refresh_subscription()

### Extension: Origin Tracking
- [ ] T093 Create origins.rs with helper functions
- [ ] T094 [P] Implement steep_repl.create_replication_origin()
- [ ] T095 [P] Implement steep_repl.drop_replication_origin()
- [ ] T096 [P] Implement steep_repl.list_replication_origins()
- [ ] T097 [P] Implement steep_repl.advance_origin()

### CLI: Command Restructuring
- [ ] T098 Rewrite cmd_node.go with 5 subcommands (register, list, status, heartbeat, remove)
- [ ] T099 Add --direct and -c flags to all cmd_node.go subcommands
- [ ] T100 Create cmd_replication.go with pub/sub/slot subcommands
- [ ] T101 Add --direct and -c flags to all cmd_replication.go subcommands
- [ ] T102 Create cmd_init.go extracting init workflows from cmd_node.go
- [ ] T103 Add --direct and -c flags to applicable cmd_init.go subcommands
- [ ] T104 Create cmd_ops.go for work queue management
- [ ] T105 Add --direct and -c flags to all cmd_ops.go subcommands
- [ ] T106 Remove duplicate `node merge` (use cmd_merge.go instead)
- [ ] T107 Update main.go to register new commands

### Executor: Direct Mode Support
- [ ] T108 Add RemoveNode() to executor.go
- [ ] T109 Add ListNodes() to executor.go
- [ ] T110 Add UpdateNodeState() to executor.go
- [ ] T111 Add CreateReplicationSlot() to executor.go
- [ ] T112 Add DropReplicationSlot() to executor.go
- [ ] T113 Add ListReplicationSlots() to executor.go
- [ ] T114 Add SlotLag() to executor.go
- [ ] T115 Add CreatePublication() to executor.go
- [ ] T116 Add DropPublication() to executor.go
- [ ] T117 Add ListPublications() to executor.go
- [ ] T118 Add AddPublicationTables() to executor.go
- [ ] T119 Add RemovePublicationTables() to executor.go
- [ ] T120 Add CreateSubscription() to executor.go
- [ ] T121 Add DropSubscription() to executor.go
- [ ] T122 Add EnableSubscription() to executor.go
- [ ] T123 Add DisableSubscription() to executor.go
- [ ] T124 Add ListSubscriptions() to executor.go
- [ ] T125 Add SubscriptionStatus() to executor.go
- [ ] T126 Add RefreshSubscription() to executor.go
- [ ] T127 Add CreateReplicationOrigin() to executor.go
- [ ] T128 Add DropReplicationOrigin() to executor.go
- [ ] T129 Add ListReplicationOrigins() to executor.go
- [ ] T130 Add AdvanceOrigin() to executor.go
```

---

## Migration Strategy

1. **Keep existing cmd_node.go working** during transition (--remote mode)
2. **Build new commands in parallel** with --direct mode first
3. **Deprecate old commands** with warnings pointing to new equivalents
4. **Remove old commands** after migration period

### Deprecation Mapping

| Old Command | New Command |
|-------------|-------------|
| `node start` | `init auto` |
| `node prepare` | `init prepare` |
| `node complete` | `init complete` |
| `node cancel` | `init cancel` |
| `node progress` | `init status` or `ops status` |
| `node reinit` | `init reset` |
| `node merge` | `merge start` (already exists) |

---

## Benefits

1. **Clear separation of concerns**: Node CRUD vs Replication primitives vs Workflows
2. **Direct mode everywhere**: All operations can bypass daemon
3. **Composable primitives**: Build custom workflows from pub/sub/slot functions
4. **Testable in isolation**: Each command maps to one SQL function
5. **No duplication**: Remove `node merge` in favor of existing `merge` command
6. **Self-documenting**: Command structure matches PostgreSQL concepts

---

## Open Questions

1. Should `init auto` support --direct mode with two connection strings, or is daemon orchestration acceptable for this complex workflow?
2. Should we version the extension schema to support migrations?
3. How do we handle privilege escalation for DDL operations (CREATE PUBLICATION requires specific grants)?

---

## Next Steps

1. Review and approve this proposal
2. Add tasks T071-T130 to tasks.md
3. Prioritize extension functions (Phase 2-4) before CLI restructuring
4. Mark T027 as superseded by this redesign
