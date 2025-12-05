# Tasks: Node Initialization & Snapshots

**Input**: Design documents from `/specs/015-node-init/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Integration tests are included for critical paths as this is a daemon feature with complex state management.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Daemon**: `internal/repl/` (Go)
- **Extension**: `extensions/steep_repl/src/` (Rust/pgrx)
- **TUI**: `internal/ui/` (Go)
- **CLI**: `cmd/steep-repl/` (Go)
- **Tests**: `tests/integration/repl/` (Go)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and core infrastructure for initialization feature

- [x] T001 Add initialization config section to internal/repl/config/config.go (InitConfig struct with method, parallel_workers, schema_sync mode, thresholds)
- [x] T002 [P] Create internal/repl/init/ package directory structure (manager.go, snapshot.go, manual.go, reinit.go, progress.go, schema.go)
- [x] T003 [P] Add InitState enum (8 states) to internal/repl/models/node.go with state transition validation
- [x] T004 Generate gRPC code from specs/015-node-init/contracts/init.proto into internal/repl/grpc/proto/

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Add init_state, init_source_node, init_started_at, init_completed_at columns to nodes table in extensions/steep_repl/src/lib.rs
- [x] T006 Create steep_repl.init_progress table in extensions/steep_repl/src/lib.rs (phase, percent, tables, current_table, throughput, ETA)
- [x] T007 [P] Create steep_repl.schema_fingerprints table in extensions/steep_repl/src/lib.rs (schema, table, fingerprint, column_count, captured_at)
- [x] T008 [P] Create steep_repl.init_slots table in extensions/steep_repl/src/lib.rs (slot_name, node_id, lsn, created_at, expires_at)
- [x] T009 [P] Create steep_repl.snapshots table in extensions/steep_repl/src/lib.rs (snapshot_id, source_node, lsn, storage_path, manifest_json, status)
- [x] T010 Implement compute_fingerprint(schema, table) SQL function in extensions/steep_repl/src/lib.rs (SHA256 of column definitions)
- [x] T011 [P] Create InitProgress Go model in internal/repl/models/progress.go matching init_progress table
- [x] T012 [P] Create SchemaFingerprint Go model in internal/repl/models/fingerprint.go matching schema_fingerprints table
- [x] T013 [P] Create Snapshot Go model in internal/repl/models/snapshot.go matching snapshots table
- [x] T014 Implement InitManager struct skeleton in internal/repl/init/manager.go (holds pool, config, channels for progress updates)
- [x] T015 Add InitService RPC handlers skeleton in internal/repl/grpc/init_handlers.go (StartInit, PrepareInit, CompleteInit, CancelInit, GetProgress, StreamProgress)
- [x] T016 Add init subcommand group to cmd/steep-repl/main.go (init, init prepare, init complete, init cancel)
- [x] T017 Implement structured JSON logging for init events in internal/repl/init/logging.go (init.started, init.state_change, init.table_complete, etc.)

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Automatic Snapshot Initialization (Priority: P1) MVP

**Goal**: Initialize a new node from an existing node using automatic snapshot (copy_data=true) for databases under 100GB

**Independent Test**: Run `steep-repl init node_b --from node_a --method snapshot` and verify data appears on target with subscription active

### Tests for User Story 1

- [x] T018 [P] [US1] Integration test for automatic init workflow in tests/integration/repl/init_test.go (start init, verify state transitions, verify data copied)
- [x] T019 [P] [US1] Integration test for init cancellation in tests/integration/repl/init_test.go (cancel mid-copy, verify cleanup, verify UNINITIALIZED state)

### Implementation for User Story 1

- [x] T020 [US1] Implement StartInit RPC handler in internal/repl/grpc/init_handlers.go (validate nodes, check schemas, create subscription)
- [x] T021 [US1] Implement snapshot initialization logic in internal/repl/init/snapshot.go (CREATE SUBSCRIPTION with copy_data=true)
- [x] T022 [US1] Implement state transition persistence in internal/repl/init/manager.go (UNINITIALIZED -> PREPARING -> COPYING -> CATCHING_UP -> SYNCHRONIZED)
- [x] T023 [US1] Implement CancelInit RPC handler in internal/repl/grpc/init_handlers.go (drop subscription, cleanup partial data, reset state)
- [x] T024 [US1] Add progress polling for pg_subscription_rel sync state in internal/repl/init/snapshot.go (track per-table i/d/s/r states)
- [x] T025 [US1] Add large table detection and handling in internal/repl/init/snapshot.go (threshold check, alternate method for >10GB tables)
- [x] T026 [US1] Add init command CLI implementation in cmd/steep-repl/main.go (`steep-repl init <target> --from <source> [--method snapshot] [--parallel N]`)
- [x] T027 [US1] Emit structured JSON logs for all state transitions in internal/repl/init/snapshot.go (init.started, init.state_change, init.completed)

**Checkpoint**: Automatic snapshot initialization works end-to-end with state tracking and cancellation

---

## Phase 4: User Story 2 - Manual Initialization from Backup (Priority: P1)

**Goal**: Initialize a node from user-managed pg_dump/pg_basebackup for multi-TB databases

**Independent Test**: Run prepare on source, perform pg_basebackup, restore, run complete, verify subscription active from recorded LSN

### Tests for User Story 2

- [x] T028 [P] [US2] Integration test for prepare/complete workflow in tests/integration/repl/init_test.go (create slot, verify LSN, complete init)
- [x] T029 [P] [US2] Integration test for schema verification during complete in tests/integration/repl/init_test.go (mismatch detection in strict mode)

### Implementation for User Story 2

- [x] T030 [US2] Implement PrepareInit RPC handler in internal/repl/grpc/init_handlers.go (create replication slot, record LSN, set expiry)
- [x] T031 [US2] Implement prepare logic in internal/repl/init/manual.go (pg_create_logical_replication_slot, pg_current_wal_lsn)
- [x] T032 [US2] Implement CompleteInit RPC handler in internal/repl/grpc/init_handlers.go (verify schema, install metadata, create subscription)
- [x] T033 [US2] Implement complete logic in internal/repl/init/manual.go (schema check, CREATE SUBSCRIPTION with copy_data=false, origin advance)
- [x] T034 [US2] Add init prepare CLI command in cmd/steep-repl/main.go (`steep-repl init prepare --node <node> --slot <name>`)
- [x] T035 [US2] Add init complete CLI command in cmd/steep-repl/main.go (`steep-repl init complete --node <target> --source <source> --source-lsn <lsn>`)
- [x] T036 [US2] Handle WAL catch-up phase after complete in internal/repl/init/manual.go (track lag until caught up, transition to SYNCHRONIZED)

**Checkpoint**: Manual initialization workflow works with user-managed backup/restore

---

## Phase 5: User Story 3 - Progress Tracking in TUI (Priority: P1)

**Goal**: Display initialization progress (% complete, rows/sec, ETA) in Steep TUI

**Independent Test**: Start initialization, open TUI, verify progress panel shows real-time updates within 2 seconds

### Implementation for User Story 3

- [x] T037 [US3] Implement GetProgress RPC handler in internal/repl/grpc/handlers.go (fetch from init_progress table)
- [x] T038 [US3] Implement StreamProgress RPC handler in internal/repl/grpc/handlers.go (poll and stream updates at configurable interval)
- [x] T039 [US3] Add progress tracking goroutine in internal/repl/init/progress.go (poll pg_subscription_rel, pg_stat_progress_copy, update init_progress table)
- [x] T040 [US3] Calculate ETA and throughput in internal/repl/init/progress.go (rows/sec, bytes/sec, time remaining estimate)
- [x] T041 [US3] Create progress overlay component in internal/ui/components/progress.go (phases, progress bars, current table, ETA)
- [x] T042 [US3] Add progress column to Nodes view in internal/ui/views/replication.go (state + percent + ETA for initializing nodes)
- [x] T043 [US3] Implement D key binding for detailed progress overlay in internal/ui/views/replication.go
- [x] T044 [US3] Implement C key binding to cancel initialization from TUI in internal/ui/views/replication.go

**Checkpoint**: TUI shows real-time initialization progress with cancel capability

---

## Phase 6: User Story 4 - Partial Reinitialization (Priority: P2)

**Goal**: Reinitialize specific tables when they diverge without full node reinit

**Independent Test**: Corrupt data in one table, run `steep-repl reinit --node node_b --tables orders`, verify only that table resynchronized

### Tests for User Story 4

- [ ] T045 [P] [US4] Integration test for partial reinit by table list in tests/integration/repl/reinit_test.go

### Implementation for User Story 4

- [ ] T046 [US4] Implement StartReinit RPC handler in internal/repl/grpc/handlers.go (validate scope, start reinit)
- [ ] T047 [US4] Implement reinit logic in internal/repl/init/reinit.go (pause replication, truncate, recopy, resume)
- [ ] T048 [US4] Add table-scope reinit in internal/repl/init/reinit.go (--tables flag processing, selective TRUNCATE/COPY)
- [ ] T049 [US4] Add schema-scope reinit in internal/repl/init/reinit.go (--schema flag processing, iterate tables)
- [ ] T050 [US4] Add full reinit in internal/repl/init/reinit.go (--full flag processing, complete node reinit)
- [ ] T051 [US4] Add reinit CLI command in cmd/steep-repl/main.go (`steep-repl reinit --node <node> [--tables X,Y] [--schema S] [--full]`)
- [ ] T052 [US4] Add REINITIALIZING state handling and UI indication in internal/ui/views/replication.go

**Checkpoint**: Partial reinitialization works for tables, schemas, or full node

---

## Phase 7: User Story 5 - Schema Fingerprinting and Drift Detection (Priority: P2)

**Goal**: Compute and compare schema fingerprints to detect drift before initialization

**Independent Test**: Create table mismatch between nodes, run schema comparison, verify diff is reported

### Tests for User Story 5

- [ ] T053 [P] [US5] Integration test for fingerprint computation in tests/integration/repl/schema_test.go
- [ ] T054 [P] [US5] Integration test for schema comparison in tests/integration/repl/schema_test.go (match, mismatch, local_only, remote_only)

### Implementation for User Story 5

- [ ] T055 [US5] Implement compare_fingerprints(peer_node) SQL function in extensions/steep_repl/src/lib.rs (uses postgres_fdw or dblink)
- [ ] T056 [US5] Create schema.go in internal/repl/init/ for fingerprint operations (capture, compare, diff)
- [ ] T057 [US5] Implement CompareSchemas RPC handler in internal/repl/grpc/handlers.go
- [ ] T058 [US5] Add schema compare CLI command in cmd/steep-repl/main.go (`steep-repl schema compare <node-a> <node-b>`)
- [ ] T059 [US5] Add schema diff CLI command in cmd/steep-repl/main.go (`steep-repl schema diff <node-a> <node-b> <table>`)
- [ ] T060 [US5] Add schema capture CLI command in cmd/steep-repl/main.go (`steep-repl schema capture --node <node>`)
- [ ] T061 [US5] Integrate schema check before init in internal/repl/init/manager.go (fail fast on mismatch in strict mode)

**Checkpoint**: Schema fingerprinting detects drift with detailed diff output

---

## Phase 8: User Story 6 - Schema Sync Mode Configuration (Priority: P2)

**Goal**: Configure schema sync behavior (strict/auto/manual) for initialization

**Independent Test**: Configure each mode, attempt initialization with mismatch, verify behavior

### Implementation for User Story 6

- [ ] T062 [US6] Add schema_sync config section to internal/repl/config/config.go (mode: strict|auto|manual)
- [ ] T063 [US6] Implement strict mode behavior in internal/repl/init/schema.go (fail with error listing differences)
- [ ] T064 [US6] Implement auto mode behavior in internal/repl/init/schema.go (generate and apply DDL to fix mismatches)
- [ ] T065 [US6] Implement manual mode behavior in internal/repl/init/schema.go (warn but proceed with confirmation)
- [ ] T066 [US6] Add --schema-sync flag to init CLI commands in cmd/steep-repl/main.go (override config)

**Checkpoint**: Schema sync modes work as configured (strict/auto/manual)

---

## Phase 9: User Story 7 - Initial Sync with Existing Data on Both Nodes (Priority: P2)

**Goal**: Set up bidirectional replication between nodes that both already have data

**Independent Test**: Set up two nodes with overlapping data, run bidirectional merge, verify reconciliation

### Tests for User Story 7

- [ ] T067 [P] [US7] Integration test for overlap analysis in tests/integration/repl/merge_test.go

### Implementation for User Story 7

- [ ] T068 [US7] Implement overlap analysis in internal/repl/init/merge.go (compare PKs, count matches/conflicts/unique)
- [ ] T069 [US7] Add analyze-overlap CLI command in cmd/steep-repl/main.go (`steep-repl analyze-overlap --tables X,Y`)
- [ ] T070 [US7] Implement conflict resolution strategies in internal/repl/init/merge.go (prefer-node-a, prefer-node-b, last-modified, manual)
- [ ] T071 [US7] Add bidirectional-merge init mode in internal/repl/init/manager.go (quiesce, analyze, resolve, enable replication)
- [ ] T072 [US7] Add --mode=bidirectional-merge to init CLI in cmd/steep-repl/main.go
- [ ] T073 [US7] Add --strategy flag for conflict resolution in cmd/steep-repl/main.go

**Checkpoint**: Bidirectional merge reconciles existing data with conflict resolution

---

## Phase 10: User Story 8 - Configurable Parallel Workers (Priority: P3)

**Goal**: Configure parallel workers for faster snapshot copy

**Independent Test**: Configure different parallel_workers values, measure throughput

### Implementation for User Story 8

- [ ] T074 [US8] Add parallel_workers config in internal/repl/config/config.go (default 4, range 1-16)
- [ ] T075 [US8] Implement parallel table copying in internal/repl/init/snapshot.go (worker pool pattern)
- [ ] T076 [US8] Add PG18 parallel COPY support detection in internal/repl/init/snapshot.go (use streaming=parallel)
- [ ] T077 [US8] Add --parallel flag to snapshot and init CLI commands in cmd/steep-repl/main.go
- [ ] T078 [US8] Show parallel worker count in progress display in internal/ui/components/progress.go

**Checkpoint**: Parallel workers accelerate snapshot operations

---

## Phase 11: Two-Phase Snapshot (Cross-cutting P2/P3)

**Goal**: Generate snapshot separately from application for network transfer and multi-target init

### Tests for Two-Phase Snapshot

- [ ] T079 [P] Integration test for snapshot generate/apply in tests/integration/repl/snapshot_test.go

### Implementation for Two-Phase Snapshot

- [ ] T080 Implement GenerateSnapshot RPC handler in internal/repl/grpc/handlers.go
- [ ] T081 Implement snapshot generation in internal/repl/init/snapshot.go (create slot, export schema, COPY tables to files, capture sequences)
- [ ] T082 Create manifest.json generator in internal/repl/init/snapshot.go (LSN, table list, checksums, sizes)
- [ ] T083 Implement ApplySnapshot RPC handler in internal/repl/grpc/handlers.go
- [ ] T084 Implement snapshot application in internal/repl/init/snapshot.go (verify checksums, COPY FROM files, restore sequences, create subscription)
- [ ] T085 Add snapshot generate CLI command in cmd/steep-repl/main.go (`steep-repl snapshot generate --source <node> --output <path>`)
- [ ] T086 Add snapshot apply CLI command in cmd/steep-repl/main.go (`steep-repl snapshot apply --target <node> --input <path>`)
- [ ] T087 Add compression support to snapshot operations in internal/repl/init/snapshot.go (gzip, lz4, zstd options)

**Checkpoint**: Two-phase snapshot workflow works for offline transfer scenarios

---

## Phase 12: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T088 [P] Add snapshot list CLI command in cmd/steep-repl/main.go (`steep-repl snapshot list`)
- [ ] T089 [P] Add snapshot delete CLI command in cmd/steep-repl/main.go (`steep-repl snapshot delete <id>`)
- [ ] T090 [P] Add schema export CLI command in cmd/steep-repl/main.go (`steep-repl schema export --node <node> --output <file>`)
- [ ] T091 Add resume support for interrupted snapshot operations in internal/repl/init/snapshot.go
- [ ] T092 Add retry logic with exponential backoff for transient failures in internal/repl/init/manager.go
- [ ] T093 Validate all quickstart.md scenarios work end-to-end
- [ ] T094 Update internal/repl/README.md with initialization documentation
- [ ] T095 Performance validation: 10GB database init < 30 minutes, fingerprint < 1s for 1000 tables

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-10)**: All depend on Foundational phase completion
  - US1 (Auto Init): Can start after Foundational - MVP
  - US2 (Manual Init): Can start after Foundational
  - US3 (Progress TUI): Depends on US1 or US2 having progress to display
  - US4 (Partial Reinit): Can start after Foundational
  - US5 (Fingerprinting): Can start after Foundational
  - US6 (Schema Sync): Depends on US5 (uses fingerprinting)
  - US7 (Bidirectional Merge): Can start after Foundational
  - US8 (Parallel Workers): Can start after US1 (enhances snapshot)
- **Two-Phase Snapshot (Phase 11)**: Can start after Foundational, shares code with US1
- **Polish (Phase 12)**: Depends on core user stories being complete

### User Story Dependencies

- **US1 (Auto Init)**: Foundation only - PRIMARY MVP
- **US2 (Manual Init)**: Foundation only - can parallelize with US1
- **US3 (Progress TUI)**: Needs US1 or US2 for init operations to track
- **US4 (Partial Reinit)**: Foundation only - can parallelize
- **US5 (Fingerprinting)**: Foundation only - can parallelize
- **US6 (Schema Sync)**: Depends on US5 (fingerprinting)
- **US7 (Bidirectional Merge)**: Foundation only - can parallelize
- **US8 (Parallel Workers)**: Enhances US1, can add after US1 works

### Parallel Opportunities

Setup Phase:
- T002, T003 can run in parallel (directory structure, enum)

Foundational Phase:
- T007, T008, T009 can run in parallel (independent tables)
- T011, T012, T013 can run in parallel (independent Go models)

User Story Phases:
- Tests within each story can run in parallel with each other
- Independent user stories can be developed in parallel (US1+US2+US4+US5+US7)

---

## Parallel Example: Foundational Phase

```bash
# Launch table creation tasks in parallel:
Task: "Create steep_repl.schema_fingerprints table in extensions/steep_repl/src/lib.rs"
Task: "Create steep_repl.init_slots table in extensions/steep_repl/src/lib.rs"
Task: "Create steep_repl.snapshots table in extensions/steep_repl/src/lib.rs"

# Launch Go model creation tasks in parallel:
Task: "Create InitProgress Go model in internal/repl/models/progress.go"
Task: "Create SchemaFingerprint Go model in internal/repl/models/fingerprint.go"
Task: "Create Snapshot Go model in internal/repl/models/snapshot.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 + 3 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (Auto Init)
4. Complete Phase 5: User Story 3 (Progress TUI)
5. **STOP and VALIDATE**: Test automatic initialization with progress display
6. Deploy/demo - basic initialization works

### P1 Complete (Stories 1, 2, 3)

1. Complete MVP above
2. Add Phase 4: User Story 2 (Manual Init)
3. **VALIDATE**: Both init methods work with progress tracking
4. All P1 functionality complete

### Incremental Delivery

1. Setup + Foundational -> Foundation ready
2. Add US1 + US3 -> Test independently -> Deploy (MVP)
3. Add US2 -> Test manual init -> Deploy
4. Add US5 + US6 -> Test schema fingerprinting -> Deploy
5. Add US4 -> Test partial reinit -> Deploy
6. Add US7 -> Test bidirectional merge -> Deploy
7. Add US8 + Two-Phase -> Performance optimization -> Deploy

---

## Summary

| Phase | Story | Task Count | Parallel Tasks |
|-------|-------|------------|----------------|
| 1. Setup | - | 4 | 2 |
| 2. Foundational | - | 13 | 6 |
| 3. US1 Auto Init | P1 | 10 | 2 |
| 4. US2 Manual Init | P1 | 9 | 2 |
| 5. US3 Progress TUI | P1 | 8 | 0 |
| 6. US4 Partial Reinit | P2 | 8 | 1 |
| 7. US5 Fingerprinting | P2 | 9 | 2 |
| 8. US6 Schema Sync | P2 | 5 | 0 |
| 9. US7 Bidirectional | P2 | 7 | 1 |
| 10. US8 Parallel | P3 | 5 | 0 |
| 11. Two-Phase | - | 9 | 1 |
| 12. Polish | - | 8 | 3 |
| **Total** | | **95** | **20** |

**MVP Scope**: Phases 1-3, 5 (Setup + Foundational + US1 + US3) = 35 tasks
**P1 Complete**: Add Phase 4 (US2) = 44 tasks
**Full Feature**: All 95 tasks

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Integration tests use testcontainers-go with PostgreSQL 18 Docker image
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
