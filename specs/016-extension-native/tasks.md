# Tasks: Extension-Native Architecture

**Input**: Design documents from `/specs/016-extension-native/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Organization**: Tasks grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Extension (Rust)**: `extensions/steep_repl/src/`
- **CLI (Go)**: `cmd/steep-repl/`, `internal/repl/`
- **Tests**: `tests/integration/repl/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and extension schema modifications

- [X] T001 Add work_queue table to extension schema in extensions/steep_repl/src/work_queue.rs
- [X] T002 [P] Add pg_shmem_init! and BackgroundWorkerBuilder to _PG_init() in extensions/steep_repl/src/lib.rs
- [X] T003 [P] Create shared memory progress struct in extensions/steep_repl/src/progress.rs
- [X] T004 [P] Create NOTIFY helper functions in extensions/steep_repl/src/notify.rs
- [X] T005 Extend snapshots table with progress columns in extensions/steep_repl/src/snapshots.rs
- [X] T006 [P] Extend merge_audit_log table with progress columns in extensions/steep_repl/src/merge_audit_log.rs

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core background worker infrastructure that MUST be complete before user stories

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T007 Implement background worker main loop in extensions/steep_repl/src/worker.rs
- [X] T008 Implement work queue claim with FOR UPDATE SKIP LOCKED in extensions/steep_repl/src/work_queue.rs
- [X] T009 Implement shared memory progress read/write with PgLwLock in extensions/steep_repl/src/progress.rs
- [X] T010 Implement pg_notify wrapper for progress updates in extensions/steep_repl/src/notify.rs
- [X] T011 [P] Create health.rs with steep_repl.health() function in extensions/steep_repl/src/health.rs
- [X] T012 [P] Create internal/repl/direct/client.go for PostgreSQL direct connection
- [X] T013 [P] Create internal/repl/direct/progress.go for NOTIFY payload parsing
- [X] T014 Update extension Cargo.toml to ensure pgrx bgworker feature enabled in extensions/steep_repl/Cargo.toml
- [X] T015 Run cargo pgrx test to verify extension compiles with new modules

**Checkpoint**: Foundation ready - user story implementation can begin

---

## Phase 3: User Story 1 - Direct CLI to PostgreSQL Operations (Priority: P1) 🎯 MVP

**Goal**: CLI commands connect directly to PostgreSQL without daemon using `--direct` flag

**Independent Test**: Run `steep-repl snapshot generate --direct` against PostgreSQL with extension, verify completion without daemon

### Implementation for User Story 1

- [X] T016 [US1] Implement steep_repl.start_snapshot() SQL function in extensions/steep_repl/src/snapshots.rs
- [X] T017 [US1] Implement steep_repl.snapshot_progress() SQL function reading from shared memory in extensions/steep_repl/src/snapshots.rs
- [X] T018 [US1] Implement steep_repl.cancel_snapshot() SQL function in extensions/steep_repl/src/snapshots.rs
- [X] T019 [P] [US1] Implement steep_repl.register_node() SQL function in extensions/steep_repl/src/nodes.rs
- [X] T020 [P] [US1] Implement steep_repl.heartbeat() SQL function in extensions/steep_repl/src/nodes.rs
- [X] T021 [P] [US1] Implement steep_repl.node_status() SQL function in extensions/steep_repl/src/nodes.rs
- [X] T022 [US1] Create cmd/steep-repl/direct/executor.go with direct PostgreSQL execution logic
- [ ] T023 [US1] Create cmd/steep-repl/direct/detector.go with auto-detection logic (FR-012)
- [ ] T024 [US1] Add --direct and -c flags to cmd/steep-repl/cmd_snapshot.go
- [ ] T025 [US1] Implement direct mode snapshot generate in cmd/steep-repl/cmd_snapshot.go
- [ ] T026 [P] [US1] Add --direct flag to cmd/steep-repl/cmd_schema.go with direct execution
- [ ] T027 [P] [US1] Add --direct flag to cmd/steep-repl/cmd_node.go with direct execution
- [ ] T028 [P] [US1] Add --direct flag to cmd/steep-repl/cmd_merge.go with direct execution
- [ ] T029 [US1] Ensure existing --remote flag continues to work in all modified commands

**Checkpoint**: Direct CLI mode works for all commands without daemon

---

## Phase 4: User Story 2 - Background Worker for Long Operations (Priority: P1)

**Goal**: Snapshot operations run as PostgreSQL background workers, visible across sessions

**Independent Test**: Start snapshot via SQL, disconnect, reconnect, verify operation continues and progress queryable

### Implementation for User Story 2

- [ ] T030 [US2] Implement snapshot_generate operation handler in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T031 [US2] Implement snapshot_apply operation handler in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T032 [US2] Implement steep_repl.start_apply() SQL function in extensions/steep_repl/src/snapshots.rs
- [ ] T033 [US2] Wire snapshot operations to work queue in worker.rs main loop in extensions/steep_repl/src/worker.rs
- [ ] T034 [US2] Update shared memory progress from snapshot worker during execution in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T035 [US2] Persist progress to snapshots table every 5 seconds in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T036 [US2] Handle cancellation check in snapshot worker loop in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T037 [US2] Mark work_queue as failed on PostgreSQL restart (in-progress → failed) in extensions/steep_repl/src/worker.rs
- [ ] T038 [US2] Implement steep_repl.wait_snapshot() blocking poll function in extensions/steep_repl/src/snapshots.rs

**Checkpoint**: Background worker executes snapshots, survives session disconnect

---

## Phase 5: User Story 3 - Real-Time Progress via LISTEN/NOTIFY (Priority: P2)

**Goal**: Real-time progress updates via PostgreSQL LISTEN/NOTIFY mechanism

**Independent Test**: Run `LISTEN steep_repl_progress`, start snapshot in another session, verify notifications arrive

### Implementation for User Story 3

- [ ] T039 [US3] Send pg_notify from snapshot worker on progress updates in extensions/steep_repl/src/snapshot_worker.rs
- [ ] T040 [US3] Create cmd/steep-repl/direct/progress.go with LISTEN/NOTIFY subscription
- [ ] T041 [US3] Implement live progress bar display in CLI using NOTIFY in cmd/steep-repl/direct/progress.go
- [ ] T042 [US3] Wire CLI snapshot generate to subscribe and display progress in cmd/steep-repl/cmd_snapshot.go
- [ ] T043 [US3] Filter notifications by operation ID to handle multiple concurrent operations in cmd/steep-repl/direct/progress.go
- [ ] T044 [US3] Send completion/failure notification from worker in extensions/steep_repl/src/snapshot_worker.rs

**Checkpoint**: CLI shows live progress bar via LISTEN/NOTIFY

---

## Phase 6: User Story 4 - SQL Function API for All Operations (Priority: P2)

**Goal**: All steep_repl operations exposed as SQL functions callable from any SQL client

**Independent Test**: Execute all steep_repl functions from psql without CLI

### Implementation for User Story 4

- [ ] T045 [P] [US4] Implement steep_repl.analyze_overlap() SQL function in extensions/steep_repl/src/merge.rs
- [ ] T046 [P] [US4] Implement steep_repl.start_merge() SQL function queuing to background worker in extensions/steep_repl/src/merge.rs
- [ ] T047 [P] [US4] Implement steep_repl.merge_progress() SQL function in extensions/steep_repl/src/merge.rs
- [ ] T048 [P] [US4] Implement steep_repl.capture_fingerprints() wrapper if not exists in extensions/steep_repl/src/fingerprint_functions.rs
- [ ] T049 [P] [US4] Implement steep_repl.compare_fingerprints() wrapper if not exists in extensions/steep_repl/src/fingerprint_functions.rs
- [ ] T050 [US4] Implement bidirectional_merge operation handler in background worker in extensions/steep_repl/src/worker.rs
- [ ] T051 [P] [US4] Implement steep_repl.list_operations() for work queue inspection in extensions/steep_repl/src/work_queue.rs
- [ ] T052 [P] [US4] Implement steep_repl.cancel_operation() for any work queue entry in extensions/steep_repl/src/work_queue.rs
- [ ] T053 [P] [US4] Implement steep_repl.bgworker_available() utility function in extensions/steep_repl/src/utils.rs

**Checkpoint**: All operations callable from psql/pgAdmin

---

## Phase 7: User Story 5 - Simplified Configuration (Priority: P3)

**Goal**: Only PostgreSQL connection details required, no daemon config

**Independent Test**: Setup steep on new server with only connection string, verify all features work

### Implementation for User Story 5

- [ ] T054 [US5] Support connection string via -c flag in all direct mode commands in cmd/steep-repl/direct/executor.go
- [ ] T055 [US5] Support PGHOST/PGPORT/PGDATABASE/PGUSER environment variables in direct mode in internal/repl/direct/client.go
- [ ] T056 [US5] Support sslmode from connection string in direct mode in internal/repl/direct/client.go
- [ ] T057 [US5] Update config parsing to make gRPC/IPC/HTTP sections optional in internal/repl/config/config.go
- [ ] T058 [US5] Document minimal configuration in quickstart.md verification in specs/016-extension-native/quickstart.md

**Checkpoint**: New deployments need only PostgreSQL connection details

---

## Phase 8: User Story 6 - Backward Compatibility During Migration (Priority: P3)

**Goal**: Both daemon mode (--remote) and direct mode (--direct) work during migration

**Independent Test**: Run same operation via --remote and --direct, verify identical results

### Implementation for User Story 6

- [ ] T059 [US6] Ensure --remote flag continues to use gRPC client in cmd/steep-repl/cmd_snapshot.go
- [ ] T060 [US6] Implement auto-detection: try direct first, fall back to daemon in cmd/steep-repl/direct/detector.go
- [ ] T061 [US6] Add deprecation warning when using --remote in cmd/steep-repl/cmd_snapshot.go
- [ ] T062 [US6] Test daemon mode still works with existing configuration in tests/integration/repl/
- [ ] T063 [US6] Document migration path from daemon to direct mode in docs/EXTENSION_MIGRATION.md

**Checkpoint**: Both modes work, migration path documented

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, testing, and cross-cutting improvements

- [ ] T064 [P] Create tests/integration/repl/direct_test.go with direct mode integration tests
- [ ] T065 [P] Create tests/integration/repl/background_worker_test.go with worker tests
- [ ] T066 [P] Add pg_regress tests for all SQL functions in extensions/steep_repl/sql/
- [ ] T067 Update extension version to reflect new capabilities in extensions/steep_repl/Cargo.toml
- [ ] T068 [P] Add privilege validation tests (graduated privileges per FR-007)
- [ ] T069 Run quickstart.md validation end-to-end
- [ ] T070 Update CLAUDE.md Active Technologies section for 016-extension-native

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-8)**: All depend on Foundational phase completion
  - US1 & US2 (both P1): Can proceed in parallel after Foundational
  - US3 & US4 (both P2): Can proceed after US1/US2 or in parallel
  - US5 & US6 (both P3): Can proceed after core functionality
- **Polish (Phase 9)**: Depends on all user stories being complete

### User Story Dependencies

- **US1 (P1)**: After Foundational - No dependencies on other stories
- **US2 (P1)**: After Foundational - Builds on US1's SQL functions but independently testable
- **US3 (P2)**: After US2 - Requires background worker for progress notifications
- **US4 (P2)**: After Foundational - Independent, can parallel with US1-US3
- **US5 (P3)**: After US1 - Configuration depends on direct mode working
- **US6 (P3)**: After US1 - Backward compat requires direct mode to compare against

### Parallel Opportunities

**Phase 1 (Setup)**:
```
Parallel: T002, T003, T004, T006
Sequential: T001 → T005 (snapshots depends on work_queue)
```

**Phase 2 (Foundational)**:
```
Parallel: T011, T012, T013
Sequential: T007 → T008 → T009 → T010 (worker chain)
```

**Phase 3-8 (User Stories)**:
```
US1 Models: T019, T020, T021 can run in parallel
US4 Functions: T045, T046, T047, T048, T049, T051, T052, T053 can run in parallel
```

---

## Parallel Example: Phase 1 Setup

```bash
# Launch in parallel (different files):
Task: "Add pg_shmem_init! and BackgroundWorkerBuilder to _PG_init() in extensions/steep_repl/src/lib.rs"
Task: "Create shared memory progress struct in extensions/steep_repl/src/progress.rs"
Task: "Create NOTIFY helper functions in extensions/steep_repl/src/notify.rs"
Task: "Extend merge_audit_log table with progress columns in extensions/steep_repl/src/merge_audit_log.rs"
```

---

## Implementation Strategy

### MVP First (User Stories 1 + 2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (Direct CLI)
4. Complete Phase 4: User Story 2 (Background Worker)
5. **STOP and VALIDATE**: Test `--direct` mode end-to-end
6. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 (Direct CLI) → Test independently → **MVP!**
3. Add US2 (Background Worker) → Test session disconnect → **Core complete**
4. Add US3 (LISTEN/NOTIFY) → Test live progress bar
5. Add US4 (SQL API) → Test from psql
6. Add US5 (Config) → Test minimal setup
7. Add US6 (Backward Compat) → Test migration path

### Suggested Task Groupings

**Day 1**: T001-T006 (Setup)
**Day 2**: T007-T015 (Foundational)
**Day 3-4**: T016-T029 (US1 - Direct CLI MVP)
**Day 5-6**: T030-T038 (US2 - Background Worker)
**Day 7**: T039-T044 (US3 - LISTEN/NOTIFY)
**Day 8**: T045-T053 (US4 - SQL API)
**Day 9**: T054-T063 (US5 + US6 - Config & Compat)
**Day 10**: T064-T070 (Polish)

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Extension changes require `cargo pgrx test` validation
- CLI changes require `go build && go test` validation
- Integration tests require Docker PostgreSQL 18 container
