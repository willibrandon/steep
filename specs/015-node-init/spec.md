# Feature Specification: Node Initialization & Snapshots

**Feature Branch**: `015-node-init`
**Created**: 2025-12-04
**Status**: Draft
**Input**: Implement node initialization and snapshots for Steep bidirectional replication. Support automatic snapshot initialization using PostgreSQL's copy_data=true and manual initialization from user-provided pg_dump/pg_basebackup. Track initialization progress (% complete, rows/sec, ETA) in TUI. Implement reinitialization for diverged/corrupted nodes (partial by table or full). Create schema fingerprinting (SHA256 of column definitions) to detect drift before initialization. Support schema sync modes (strict/auto/manual). Track initialization states (UNINITIALIZED, PREPARING, COPYING, CATCHING_UP, SYNCHRONIZED, DIVERGED, FAILED, REINITIALIZING). Handle initial sync with existing data on both nodes.

**Reference**: BIDIRECTIONAL_REPLICATION.md sections 6, 15.4

## Clarifications

### Session 2025-12-04

- Q: What should be the default schema sync mode for initialization? → A: `strict` (fail on mismatch by default for safety)
- Q: Should initialization operations emit structured logs/events for external monitoring? → A: Yes, emit structured JSON logs for all state transitions and milestones

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Automatic Snapshot Initialization (Priority: P1)

As a DBA, I want to initialize a new node from an existing node using automatic snapshot (copy_data=true) so that I can quickly set up replication for small to medium databases without manual intervention.

**Why this priority**: This is the primary, most common initialization method. Most users will use this for databases under 100GB. It provides the fastest path to a working replication setup.

**Independent Test**: Can be fully tested by running `steep-repl init node_b --from node_a --method snapshot` and verifying data appears on the target node with subscription active.

**Acceptance Scenarios**:

1. **Given** a registered source node (node_a) with data, **When** I run `steep-repl init node_b --from node_a --method snapshot`, **Then** the target node receives a copy of all data and a subscription is created starting from the snapshot LSN.

2. **Given** an initialization in progress, **When** I view the Steep TUI, **Then** I see real-time progress including overall percentage, current table, rows/sec throughput, and ETA.

3. **Given** a snapshot initialization in progress, **When** I press C to cancel, **Then** the initialization is aborted, partial data is cleaned up, and the node returns to UNINITIALIZED state.

4. **Given** tables larger than the configured threshold (default 10GB), **When** automatic snapshot runs, **Then** the system uses the configured large table method (pg_dump, COPY, or basebackup) for those tables.

---

### User Story 2 - Manual Initialization from Backup (Priority: P1)

As a DBA, I want to initialize a node from my own pg_dump/pg_basebackup for large databases so that I can use my existing backup infrastructure and avoid extended snapshot operations on production.

**Why this priority**: Essential for enterprise deployments with multi-TB databases where automatic snapshot is impractical. DBAs need control over their backup/restore process.

**Independent Test**: Can be fully tested by running prepare on source, performing a pg_basebackup, restoring to target, then running complete to finish setup.

**Acceptance Scenarios**:

1. **Given** a source node (node_a), **When** I run `steep-repl init prepare --node node_a --slot steep_init_slot`, **Then** a replication slot is created and the current LSN is recorded for later use.

2. **Given** I have restored my backup to the target node, **When** I run `steep-repl init complete --node node_b --source node_a --source-lsn 0/1234ABCD`, **Then** the system verifies schema, installs steep_repl metadata, creates the subscription, and applies WAL changes since the backup.

3. **Given** a manual initialization in progress, **When** the schema on target doesn't match source, **Then** the system reports the mismatch and follows the configured schema_sync mode (strict: fail, auto: apply DDL, manual: warn).

---

### User Story 3 - Progress Tracking in TUI (Priority: P1)

As a DBA, I want to see initialization progress (% complete, rows/sec, ETA) in Steep TUI so that I can monitor long-running operations and estimate completion time.

**Why this priority**: Visibility into progress is essential for operational confidence. DBAs need to know how long operations will take and whether they're proceeding normally.

**Independent Test**: Can be tested by starting any initialization and viewing the Nodes or dedicated initialization overlay in Steep TUI.

**Acceptance Scenarios**:

1. **Given** an initialization in progress, **When** I open the Steep TUI, **Then** I see a progress panel showing: overall percentage, tables completed/total, current table with progress bar, throughput (rows/sec), and estimated time remaining.

2. **Given** a two-phase snapshot initialization, **When** viewing progress, **Then** I see distinct phases (Generation, Application) with individual progress for each.

3. **Given** multiple tables being copied in parallel, **When** viewing progress, **Then** I see the number of parallel workers and aggregate throughput.

4. **Given** the Nodes view in TUI, **When** a node is being initialized, **Then** the node row shows state (COPYING, CATCHING_UP, etc.) and progress percentage with ETA.

---

### User Story 4 - Partial Reinitialization (Priority: P2)

As a DBA, I want to reinitialize specific tables when they diverge without full reinit so that I can recover from data issues while minimizing disruption to other tables.

**Why this priority**: Full reinitialization can take hours for large databases. Partial reinit allows surgical recovery, keeping most replication active.

**Independent Test**: Can be tested by corrupting data in a single table, running `steep-repl reinit --node node_b --tables orders`, and verifying only that table is resynchronized.

**Acceptance Scenarios**:

1. **Given** a diverged table (orders), **When** I run `steep-repl reinit --node node_b --tables orders,line_items`, **Then** only those tables are truncated and recopied while other tables continue normal replication.

2. **Given** all tables in a schema need reinit, **When** I run `steep-repl reinit --node node_b --schema sales`, **Then** all tables in that schema are reinitialized together.

3. **Given** a full reinit is needed, **When** I run `steep-repl reinit --node node_b --full`, **Then** the node is completely reinitialized from scratch.

4. **Given** a partial reinit in progress, **When** viewing the TUI, **Then** I see which tables are being reinitialized and which continue normal operation.

---

### User Story 5 - Schema Fingerprinting and Drift Detection (Priority: P2)

As a DBA, I want schema fingerprinting to detect drift before initialization so that I can identify schema mismatches early and avoid data corruption.

**Why this priority**: Schema mismatches cause silent data corruption or replication failures. Detecting drift before initialization prevents expensive recovery operations.

**Independent Test**: Can be tested by creating a table mismatch between nodes, running schema comparison, and verifying the diff is reported.

**Acceptance Scenarios**:

1. **Given** two nodes, **When** I run schema comparison (via CLI or TUI), **Then** I see a table showing each table's fingerprint match status (MATCH, MISMATCH, LOCAL_ONLY, REMOTE_ONLY).

2. **Given** a schema mismatch exists, **When** I view the diff, **Then** I see exactly which columns differ (missing, extra, type change, default change).

3. **Given** fingerprints are stored in steep_repl.schema_fingerprints, **When** I query the table, **Then** I see SHA256 hashes of column definitions with capture timestamps.

4. **Given** initialization is attempted with mismatched schemas in strict mode, **When** the process starts, **Then** it fails immediately with a clear error listing the differences.

---

### User Story 6 - Schema Sync Mode Configuration (Priority: P2)

As a DBA, I want schema sync to validate/fix schema mismatches with configurable behavior so that I can choose between strict safety, automatic fixing, or manual intervention.

**Why this priority**: Different environments need different behaviors. Production may require strict mode while development benefits from auto-sync.

**Independent Test**: Can be tested by configuring each mode and attempting initialization with a schema mismatch.

**Acceptance Scenarios**:

1. **Given** schema_sync.mode is set to strict, **When** schemas don't match, **Then** initialization fails with an error.

2. **Given** schema_sync.mode is set to auto, **When** schemas don't match, **Then** DDL is applied to the target to make schemas match before data copy.

3. **Given** schema_sync.mode is set to manual, **When** schemas don't match, **Then** a warning is displayed but initialization proceeds (with user confirmation).

---

### User Story 7 - Initial Sync with Existing Data on Both Nodes (Priority: P2)

As a DBA, I want to set up bidirectional replication between nodes that both already have data so that I can consolidate or merge existing databases.

**Why this priority**: Common in mergers, migrations, or consolidating regional databases. Requires careful conflict handling.

**Independent Test**: Can be tested by setting up two nodes with overlapping data, running the bidirectional merge workflow, and verifying data is reconciled.

**Acceptance Scenarios**:

1. **Given** two nodes with existing data, **When** I run `steep-repl init --mode=bidirectional-merge`, **Then** writes are quiesced on both nodes.

2. **Given** quiesced nodes, **When** overlap analysis runs, **Then** I see counts of matching rows, conflicting rows, and unique rows per table.

3. **Given** conflicting rows exist, **When** I choose a resolution strategy (prefer-node-a, prefer-node-b, last-modified, manual), **Then** conflicts are resolved accordingly.

4. **Given** conflicts are resolved, **When** bidirectional replication is enabled, **Then** subscriptions are created with copy_data=false (data already reconciled).

---

### User Story 8 - Configurable Parallel Workers (Priority: P3)

As a DBA, I want to configure parallel workers for faster snapshot copy so that I can optimize initialization speed based on my hardware.

**Why this priority**: Performance optimization that benefits large databases. Not required for basic functionality.

**Independent Test**: Can be tested by configuring different parallel_workers values and measuring throughput.

**Acceptance Scenarios**:

1. **Given** parallel_workers is set to 4 in configuration, **When** snapshot runs, **Then** up to 4 tables are copied concurrently using PostgreSQL 18 parallel COPY.

2. **Given** a large table (>10GB), **When** snapshot runs with parallel workers, **Then** that single table benefits from parallel data export/import.

---

### Edge Cases

- What happens when network fails during snapshot transfer? The operation should be resumable from the last completed table, with progress tracked in the manifest.
- How does system handle source node going offline during initialization? Target node transitions to FAILED state with actionable error message. Replication slot on source is preserved for retry.
- What if target node has existing data when snapshot tries to apply? System should fail with error unless --force is specified, which truncates existing data.
- What happens if parallel worker process crashes? Remaining workers continue; failed table is retried. Overall operation fails only if retries exhausted.
- How are sequences handled during snapshot? Sequence values are captured at snapshot time and restored on target, then refreshed using PG18 REFRESH SEQUENCES.
- What if schema changes on source during snapshot? Snapshot uses consistent snapshot isolation; DDL after snapshot start is captured in WAL for catch-up phase.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST support automatic snapshot initialization using PostgreSQL subscription with copy_data=true for databases under the configurable size threshold.
- **FR-002**: System MUST support two-phase snapshot initialization (generate separately, apply later) for larger databases.
- **FR-003**: System MUST support manual initialization workflow with prepare/complete commands for user-managed backups.
- **FR-004**: System MUST track initialization progress with overall percentage, per-table progress, throughput (rows/sec), and ETA.
- **FR-005**: System MUST persist initialization state in steep_repl.nodes table, surviving daemon restarts.
- **FR-006**: System MUST support partial reinitialization by table list or schema.
- **FR-007**: System MUST compute and store SHA256 fingerprints of table schemas for drift detection.
- **FR-008**: System MUST compare schema fingerprints across nodes before initialization.
- **FR-009**: System MUST support three schema sync modes: strict (fail on mismatch, **default**), auto (apply DDL), manual (warn).
- **FR-010**: System MUST track node states: UNINITIALIZED, PREPARING, COPYING, CATCHING_UP, SYNCHRONIZED, DIVERGED, FAILED, REINITIALIZING.
- **FR-011**: System MUST allow cancellation of in-progress initialization with cleanup of partial data.
- **FR-012**: System MUST handle initialization between nodes that both have existing data with overlap analysis and conflict resolution.
- **FR-013**: System MUST support configurable parallel workers for snapshot generation and application.
- **FR-014**: System MUST create replication slots during prepare phase to capture WAL for catch-up.
- **FR-015**: System MUST apply accumulated WAL changes during CATCHING_UP phase before transitioning to SYNCHRONIZED.
- **FR-016**: System MUST handle large tables (above threshold) with configurable method (pg_dump, COPY, or basebackup).
- **FR-017**: System MUST provide TUI overlay showing initialization progress with cancel option.
- **FR-018**: System MUST update Nodes view to show initialization state and progress for each node.
- **FR-019**: System MUST emit structured JSON logs for all initialization state transitions and milestones (init start, table complete, phase change, failure, completion) for external monitoring integration.

### Key Entities

- **Node**: Represents a PostgreSQL instance participating in replication. Key attributes: name, connection info, state (the 8 initialization states), health status.
- **Snapshot**: A point-in-time capture of database state. Key attributes: source node, LSN, manifest (tables, checksums), storage location, generation timestamp.
- **Schema Fingerprint**: SHA256 hash of table column definitions. Key attributes: schema, table, fingerprint, column_count, captured_at.
- **Initialization Progress**: Real-time tracking of copy operation. Key attributes: phase, overall percentage, current table, table progress, rows copied, throughput, start time.
- **Conflict**: Row-level difference between nodes during bidirectional merge. Key attributes: table, primary key, local tuple, remote tuple, resolution strategy.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can initialize a 10GB database in under 30 minutes with automatic snapshot method.
- **SC-002**: Progress updates display in TUI within 2 seconds of actual progress changes.
- **SC-003**: Schema fingerprint computation completes in under 1 second for databases with up to 1000 tables.
- **SC-004**: Partial reinitialization of a single 100MB table completes in under 5 minutes.
- **SC-005**: Two-phase snapshot generation can saturate available disk I/O (measured by comparing to raw pg_dump performance).
- **SC-006**: Cancelled initializations clean up partial state within 30 seconds.
- **SC-007**: Schema drift detection identifies mismatches with 100% accuracy for column additions, deletions, type changes, and default changes.
- **SC-008**: Manual initialization workflow (prepare + user backup + complete) works end-to-end without steep-repl involvement in the backup step.
- **SC-009**: Bidirectional merge workflow handles 100,000 conflicting rows with user-selected resolution strategy.
- **SC-010**: All 8 initialization states are accurately tracked and visible in both CLI and TUI.

## Assumptions

- PostgreSQL 18 is available for parallel COPY features; earlier versions fall back to sequential copy.
- The steep_repl extension is already installed on both nodes before initialization begins.
- Network connectivity between nodes is stable during initialization; transient failures are handled with retries.
- Users have sufficient disk space for snapshot storage (2x database size for safety margin).
- Configuration file (replication.initialization section) is properly structured before initialization commands.
