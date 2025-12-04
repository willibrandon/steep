# Tasks: Bidirectional Replication Foundation

**Input**: Design documents from `/specs/014-repl-foundation/`
**Prerequisites**: plan.md, spec.md, data-model.md, contracts/, research.md, quickstart.md

**Tests**: Tests are included for integration validation only. No TDD approach specified.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing. This feature has two components: Rust extension and Go daemon.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Rust Extension**: `extensions/steep_repl/`
- **Go Daemon**: `cmd/steep-repl/`, `internal/repl/`
- **Tests**: `tests/integration/repl/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Initialize both Rust extension and Go daemon projects

- [x] T001 Create extensions directory structure: `extensions/steep_repl/`
- [x] T002 Initialize Rust/pgrx project with `cargo pgrx init` in `extensions/steep_repl/`
- [x] T003 [P] Configure Cargo.toml with pg18-only feature in `extensions/steep_repl/Cargo.toml`
- [x] T004 [P] Create Go daemon directory structure: `cmd/steep-repl/`, `internal/repl/`
- [x] T005 Add Go dependencies to go.mod: kardianos/service, Microsoft/go-winio, grpc-go
- [x] T006 [P] Copy gRPC proto from contracts to `internal/repl/grpc/proto/repl.proto`
- [x] T007 Generate gRPC Go code from proto in `internal/repl/grpc/proto/`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T008 Create Go models matching data-model.md in `internal/repl/models/node.go`
- [x] T009 [P] Create Go models for coordinator state in `internal/repl/models/state.go`
- [x] T010 [P] Create Go models for audit log in `internal/repl/models/audit.go`
- [x] T011 Create configuration struct for repl section in `internal/repl/config/config.go`
- [x] T012 Implement config loading with Viper (platform paths) in `internal/repl/config/config.go`
- [x] T013 Create daemon orchestrator skeleton in `internal/repl/daemon/daemon.go`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Install PostgreSQL Extension (Priority: P1)

**Goal**: DBAs can install steep_repl extension on PostgreSQL 18 to create schema tables

**Independent Test**: Run `CREATE EXTENSION steep_repl;` on PG18 and verify tables exist

### Implementation for User Story 1

- [x] T014 [US1] Create extension entry point in `extensions/steep_repl/src/lib.rs`
- [x] T015 [US1] Add PostgreSQL 18 version check in `extensions/steep_repl/src/lib.rs`
- [x] T016 [US1] Define steep_repl schema via extension_sql! in `extensions/steep_repl/src/lib.rs`
- [x] T017 [US1] Create nodes table via extension_sql! in `extensions/steep_repl/src/lib.rs`
- [x] T018 [US1] Create coordinator_state table via extension_sql! in `extensions/steep_repl/src/lib.rs`
- [x] T019 [US1] Create audit_log table via extension_sql! in `extensions/steep_repl/src/lib.rs`
- [x] T020 [US1] Create indexes on audit_log (occurred_at, actor, action) in `extensions/steep_repl/src/lib.rs`
- [x] T021 [US1] Create extension control file in `extensions/steep_repl/steep_repl.control`
- [x] T022 [US1] Add pgrx test for schema creation in `extensions/steep_repl/src/lib.rs`
- [x] T023 [US1] Add pgrx test for table structure verification in `extensions/steep_repl/src/lib.rs`
- [x] T024 [US1] Build and test extension with `cargo pgrx test --pg18`

**Checkpoint**: Extension installs on PG18 and creates all tables/indexes

---

## Phase 4: User Story 2 - Install and Run Daemon as System Service (Priority: P1)

**Goal**: DBAs can install steep-repl as a system service that survives reboots

**Independent Test**: Run `steep-repl install`, `steep-repl start`, verify with `steep-repl status`

### Implementation for User Story 2

- [x] T025 [US2] Create main.go with Cobra CLI commands in `cmd/steep-repl/main.go`
- [x] T026 [US2] Implement service.Program interface in `internal/repl/daemon/service.go`
- [x] T027 [US2] Implement Start() method (non-blocking) in `internal/repl/daemon/service.go`
- [x] T028 [US2] Implement Stop() method (graceful shutdown) in `internal/repl/daemon/service.go`
- [x] T029 [US2] Add platform-specific service options (launchd/systemd/SCM) in `internal/repl/daemon/service.go`
- [x] T030 [US2] Implement install command in `cmd/steep-repl/main.go`
- [x] T031 [US2] Implement uninstall command in `cmd/steep-repl/main.go`
- [x] T032 [US2] Implement start command in `cmd/steep-repl/main.go`
- [x] T033 [US2] Implement stop command in `cmd/steep-repl/main.go`
- [x] T034 [US2] Implement restart command in `cmd/steep-repl/main.go`
- [x] T035 [US2] Implement status command in `cmd/steep-repl/main.go`
- [x] T036 [US2] Implement run command (foreground) in `cmd/steep-repl/main.go`
- [x] T037 [US2] Add platform system logging integration in `internal/repl/daemon/service.go`
- [x] T038 [US2] Implement PID file management in `internal/repl/daemon/pidfile.go`
- [x] T039 [P] [US2] Add build target to Makefile: `make build-repl`

**Checkpoint**: Daemon installs as service and responds to all CLI commands

---

## Phase 5: User Story 3 - Daemon Connects to PostgreSQL (Priority: P1)

**Goal**: Daemon establishes connection pool to PostgreSQL with retry logic

**Independent Test**: Configure connection, run `steep-repl status`, verify "PostgreSQL: connected"

### Implementation for User Story 3

- [x] T040 [US3] Create pgx connection pool wrapper in `internal/repl/db/pool.go`
- [x] T041 [US3] Implement connection string builder from config in `internal/repl/db/pool.go`
- [x] T042 [US3] Add environment variable support (PGHOST, etc.) in `internal/repl/db/pool.go`
- [x] T043 [US3] Implement password_command execution in `internal/repl/db/pool.go`
- [x] T044 [US3] Add PostgreSQL 18 version validation on connect in `internal/repl/db/pool.go`
- [x] T045 [US3] Implement exponential backoff retry logic in `internal/repl/db/pool.go`
- [x] T046 [US3] Add connection health check method in `internal/repl/db/pool.go`
- [x] T047 [US3] Integrate pool with daemon Start() in `internal/repl/daemon/daemon.go`
- [x] T048 [US3] Add PostgreSQL status to status command output in `cmd/steep-repl/main.go`
- [x] T049 [US3] Implement audit log writer in `internal/repl/db/audit.go`
- [x] T050 [US3] Log daemon.started event on startup in `internal/repl/daemon/daemon.go`

**Checkpoint**: Daemon connects to PostgreSQL, shows status, logs audit events

---

## Phase 6: User Story 4 - TUI Communicates with Daemon via IPC (Priority: P2)

**Goal**: TUI can connect to daemon via named pipes (Windows) or Unix sockets

**Independent Test**: Start daemon, use test client to call status.get via IPC

### Implementation for User Story 4

- [x] T051 [US4] Create cross-platform IPC listener in `internal/repl/ipc/listener.go`
- [x] T052 [US4] Implement Windows named pipe listener with go-winio in `internal/repl/ipc/listener_windows.go`
- [x] T053 [P] [US4] Implement Unix socket listener in `internal/repl/ipc/listener_unix.go`
- [x] T054 [US4] Add stale endpoint cleanup on startup in `internal/repl/ipc/listener.go`
- [x] T055 [US4] Define IPC message types per contracts in `internal/repl/ipc/messages.go`
- [x] T056 [US4] Implement JSON-over-IPC protocol handler in `internal/repl/ipc/server.go`
- [x] T057 [US4] Implement status.get method handler in `internal/repl/ipc/handlers.go`
- [x] T058 [US4] Implement health.check method handler in `internal/repl/ipc/handlers.go`
- [x] T059 [US4] Implement nodes.list method handler in `internal/repl/ipc/handlers.go`
- [x] T060 [US4] Implement nodes.get method handler in `internal/repl/ipc/handlers.go`
- [x] T061 [US4] Implement audit.query method handler in `internal/repl/ipc/handlers.go`
- [x] T062 [US4] Integrate IPC server with daemon in `internal/repl/daemon/daemon.go`
- [x] T063 [US4] Create IPC client for testing in `internal/repl/ipc/client.go`

**Checkpoint**: IPC server accepts connections and responds to all methods

---

## Phase 7: User Story 5 - Node-to-Node Communication via gRPC (Priority: P2)

**Goal**: Daemons on different nodes can communicate via gRPC with mTLS

**Independent Test**: Configure two nodes, run `steep-repl health --remote node-b:5433`

### Implementation for User Story 5

- [x] T064 [US5] Implement mTLS server credentials loader in `internal/repl/grpc/tls.go`
- [x] T065 [P] [US5] Implement mTLS client credentials loader in `internal/repl/grpc/tls.go`
- [x] T066 [US5] Create gRPC server with mTLS in `internal/repl/grpc/server.go`
- [x] T067 [US5] Implement Coordinator.HealthCheck RPC in `internal/repl/grpc/server.go`
- [x] T068 [US5] Implement Coordinator.RegisterNode RPC in `internal/repl/grpc/server.go`
- [x] T069 [US5] Implement Coordinator.GetNodes RPC in `internal/repl/grpc/server.go`
- [x] T070 [US5] Implement Coordinator.Heartbeat RPC in `internal/repl/grpc/server.go`
- [x] T071 [US5] Create gRPC client for node communication in `internal/repl/grpc/client.go`
- [x] T072 [US5] Implement health --remote CLI command in `cmd/steep-repl/main.go`
- [x] T073 [US5] Integrate gRPC server with daemon in `internal/repl/daemon/daemon.go`
- [x] T074 [US5] Add failed connection logging in `internal/repl/grpc/server.go`

**Checkpoint**: Two nodes can communicate via gRPC with mTLS

---

## Phase 8: User Story 6 - HTTP Health Endpoint (Priority: P3)

**Goal**: Daemon exposes HTTP /health endpoint for load balancers

**Independent Test**: Run `curl http://localhost:8080/health` and verify JSON response

### Implementation for User Story 6

- [ ] T075 [US6] Create HTTP server with configurable port in `internal/repl/health/http.go`
- [ ] T076 [US6] Implement /health endpoint handler in `internal/repl/health/http.go`
- [ ] T077 [US6] Implement /ready endpoint handler in `internal/repl/health/http.go`
- [ ] T078 [US6] Implement /live endpoint handler in `internal/repl/health/http.go`
- [ ] T079 [US6] Add component health aggregation in `internal/repl/health/http.go`
- [ ] T080 [US6] Integrate HTTP server with daemon (optional enable) in `internal/repl/daemon/daemon.go`
- [ ] T081 [US6] Add HTTP status to status command output in `cmd/steep-repl/main.go`

**Checkpoint**: HTTP health endpoint returns JSON with component health

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Integration testing, documentation, and final validation

- [ ] T082 [P] Create integration test for extension installation in `tests/integration/repl/extension_test.go`
- [ ] T083 [P] Create integration test for daemon lifecycle in `tests/integration/repl/daemon_test.go`
- [ ] T084 [P] Create integration test for IPC communication in `tests/integration/repl/ipc_test.go`
- [ ] T085 [P] Create integration test for gRPC communication in `tests/integration/repl/grpc_test.go`
- [ ] T086 Validate quickstart.md steps work end-to-end
- [ ] T087 Add steep-repl to make targets in Makefile
- [ ] T088 Update CLAUDE.md with 014-repl-foundation recent changes
- [ ] T089 Run cross-platform build verification (Windows, Linux, macOS)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **US1 Extension (Phase 3)**: Depends on Phase 1 (Rust setup only) - Can run in parallel with Phase 2
- **US2 Service (Phase 4)**: Depends on Phase 2 - Requires Go foundation
- **US3 PostgreSQL (Phase 5)**: Depends on Phase 4 - Requires daemon structure
- **US4 IPC (Phase 6)**: Depends on Phase 5 - Requires PostgreSQL connectivity for status
- **US5 gRPC (Phase 7)**: Depends on Phase 5 - Requires PostgreSQL connectivity
- **US6 HTTP (Phase 8)**: Depends on Phase 5 - Requires PostgreSQL connectivity
- **Polish (Phase 9)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Independent - Rust extension has no Go dependencies
- **User Story 2 (P1)**: Depends on Phase 2 Go foundation
- **User Story 3 (P1)**: Depends on US2 (daemon must exist to add PostgreSQL)
- **User Story 4 (P2)**: Depends on US3 (needs PostgreSQL status for IPC responses)
- **User Story 5 (P2)**: Depends on US3 (needs PostgreSQL status for gRPC responses)
- **User Story 6 (P3)**: Depends on US3 (needs PostgreSQL status for health responses)

### Parallel Opportunities

**Parallel Track A (Rust Extension)**:
- T001, T002, T003 → T014-T024 (US1 complete independently)

**Parallel Track B (Go Daemon)**:
- T004, T005, T006, T007 → T008-T013 → T025-T050 (US2, US3 sequentially)

**After US3 Complete**:
- US4 (IPC), US5 (gRPC), US6 (HTTP) can run in parallel

---

## Parallel Example: Setup Phase

```bash
# After T001-T002 complete, these can run in parallel:
Task: "T003 [P] Configure Cargo.toml with pg18-only feature"
Task: "T004 [P] Create Go daemon directory structure"
Task: "T006 [P] Copy gRPC proto from contracts"
```

## Parallel Example: User Story 4 (IPC)

```bash
# After T051 complete, these can run in parallel:
Task: "T052 [US4] Implement Windows named pipe listener"
Task: "T053 [P] [US4] Implement Unix socket listener"
```

---

## Implementation Strategy

### MVP First (User Stories 1-3 Only)

1. Complete Phase 1: Setup (both Rust and Go)
2. Complete Phase 2: Foundational (Go models and config)
3. Complete Phase 3: User Story 1 (Extension) - can run in parallel with Phase 2
4. Complete Phase 4: User Story 2 (Service management)
5. Complete Phase 5: User Story 3 (PostgreSQL connectivity)
6. **STOP and VALIDATE**: Extension installs, daemon runs as service, connects to PostgreSQL
7. Deploy/demo if ready - this is a functional MVP

### Incremental Delivery

1. MVP (US1-3) → Extension + Daemon + PostgreSQL connectivity
2. Add US4 (IPC) → TUI can communicate with daemon
3. Add US5 (gRPC) → Multi-node coordination possible
4. Add US6 (HTTP) → Load balancer integration
5. Each addition is independently testable

### Parallel Team Strategy

With two developers:

**Developer A (Rust)**:
- Phase 1 (Rust setup)
- User Story 1 (Extension)

**Developer B (Go)**:
- Phase 1 (Go setup)
- Phase 2 (Foundation)
- User Stories 2-6 (Daemon)

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story
- US1 (Extension) is fully independent from Go daemon
- US2-US6 form a dependency chain in the Go daemon
- Verify each checkpoint before proceeding
- Commit after each task or logical group
- Windows is primary target - test on Windows first
