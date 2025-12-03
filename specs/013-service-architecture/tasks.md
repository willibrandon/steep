# Tasks: Service Architecture (steep-agent)

**Input**: Design documents from `/specs/013-service-architecture/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli-interface.md, quickstart.md

**Tests**: Not explicitly requested in specification. Test tasks omitted.

**Organization**: Tasks grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Project type**: Single Go project with new entry point
- **Agent code**: `cmd/steep-agent/`, `internal/agent/`
- **Modified files**: `cmd/steep/main.go`, `internal/app/app.go`, `internal/config/config.go`

---

## Phase 1: Setup

**Purpose**: Project initialization and dependency management

- [x] T001 Add kardianos/service v1.2.x dependency via `go get github.com/kardianos/service`
- [x] T002 [P] Create cmd/steep-agent/ directory structure
- [x] T003 [P] Create internal/agent/ directory structure
- [x] T004 Update Makefile with `steep-agent` build target

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Add agent configuration section parsing in internal/config/config.go (intervals, retention, instances, alerts)
- [x] T006 [P] Create AgentStatus entity and SQLite table in internal/agent/status.go per data-model.md
- [x] T007 [P] Create AgentInstance entity and SQLite table in internal/agent/instance.go per data-model.md
- [x] T008 Add instance_name column migration to existing tables (activity_snapshots, query_stats, replication_lag_history, lock_snapshots, deadlock_events, metrics_history, alert_events)
- [x] T009 Create base agent struct and lifecycle methods in internal/agent/agent.go (New, Start, Stop, Shutdown)
- [x] T010 Implement PID file management (create, read, remove) in internal/agent/pidfile.go

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Continuous Data Collection (Priority: P1)

**Goal**: Monitoring data collected continuously even when TUI not running

**Independent Test**: Run steep-agent in foreground mode, close it, verify SQLite contains timestamped data from collection period

### Implementation for User Story 1

- [x] T011 [US1] Implement collector coordinator in internal/agent/collector.go with goroutine management per research.md
- [x] T012 [P] [US1] Create activity collector goroutine in internal/agent/collectors/activity.go (reuse existing monitor)
- [x] T013 [P] [US1] Create queries collector goroutine in internal/agent/collectors/queries.go (reuse existing monitor)
- [x] T014 [P] [US1] Create replication collector goroutine in internal/agent/collectors/replication.go (reuse existing monitor)
- [x] T015 [P] [US1] Create locks collector goroutine in internal/agent/collectors/locks.go (reuse existing monitor)
- [x] T016 [P] [US1] Create metrics collector goroutine in internal/agent/collectors/metrics.go (reuse existing monitor)
- [x] T017 [US1] Implement PostgreSQL connection pooling with automatic reconnection in internal/agent/pool.go
- [x] T018 [US1] Implement configurable collection intervals per data type per cli-interface.md
- [x] T019 [US1] Update AgentStatus.last_collect timestamp on each successful collection cycle
- [x] T020 [US1] Implement `steep-agent run` command for foreground debugging in cmd/steep-agent/main.go
- [x] T021 [US1] Add --debug flag for verbose logging per cli-interface.md
- [x] T022 [US1] Add --config flag for custom config path per cli-interface.md
- [x] T023 [US1] Handle SIGINT/SIGTERM for clean foreground shutdown

**Checkpoint**: Agent can run in foreground and collect data continuously. Verify with `sqlite3 ~/.config/steep/steep.db "SELECT * FROM agent_status"`

---

## Phase 4: User Story 2 - Service Installation & Management (Priority: P1)

**Goal**: Install steep-agent as system service that starts on boot

**Independent Test**: Run `steep-agent install`, reboot, verify agent running via `steep-agent status`

### Implementation for User Story 2

- [x] T024 [US2] Implement kardianos/service wrapper in internal/agent/service.go per research.md
- [x] T025 [US2] Implement service.Program interface (Start/Stop methods) in internal/agent/service.go
- [x] T026 [US2] Implement `steep-agent install` command with --user flag per cli-interface.md
- [x] T027 [US2] Implement `steep-agent uninstall` command per cli-interface.md
- [x] T028 [US2] Implement `steep-agent start` command per cli-interface.md
- [x] T029 [US2] Implement `steep-agent stop` command per cli-interface.md
- [x] T030 [US2] Implement `steep-agent restart` command per cli-interface.md
- [x] T031 [US2] Implement `steep-agent status` command with human-readable and --json output per cli-interface.md
- [x] T032 [US2] Configure automatic restart on crash via service manager (exponential backoff in service config)
- [x] T033 [US2] Implement proper exit codes per cli-interface.md (0=success, 1=permission denied, 2=service exists, etc.)

**Checkpoint**: Service can be installed, started, stopped, and survives system reboot

---

## Phase 5: User Story 3 - Automatic Agent/TUI Coordination (Priority: P1)

**Goal**: TUI automatically detects agent presence and coordinates data collection to avoid conflicts

**Independent Test**: Start TUI alone (collects via log parsing), start agent, verify TUI switches to agent mode and stops its own collection

### Implementation for User Story 3

- [x] T034 [US3] Implement agent detection logic in internal/app/detection.go (PID file + process check + last_collect freshness)
- [x] T035 [US3] Add IsAgentHealthy() and GetAgentStatus() functions for health checking
- [x] T036 [US3] Modify internal/app/app.go to auto-switch between agent mode and direct log collection
- [x] T037 [US3] TUI stops its own log collection when agent is healthy, resumes when agent stops
- [x] T038 [US3] Add "Agent: Running/Stopped" indicator to bottom status bar
- [x] T039 [US3] Add [AGENT]/[LOG]/[SAMPLE] data source indicator to queries view header
- [x] T040 [US3] Fix log position persistence on context cancellation to prevent double-counting on mode switch
- [x] T041 [US3] Strip LIMIT/OFFSET from query fingerprints for SQL editor pagination compatibility
- [x] T042 [US3] Enable query logging for SQL editor queries via SET log_min_duration_statement

**Checkpoint**: TUI correctly auto-detects agent presence and seamlessly switches collection modes

---

## Phase 6: User Story 4 - Automatic Data Retention (Priority: P2)

**Goal**: Agent handles data retention and cleanup automatically

**Independent Test**: Configure 1-hour retention, generate 2 hours of data, verify older data pruned

### Implementation for User Story 4

- [x] T043 [US4] Implement retention manager in internal/agent/retention.go with hourly ticker per research.md
- [x] T044 [US4] Implement per-data-type pruning with DELETE LIMIT to avoid long transactions
- [x] T045 [US4] Add default retention periods (activity: 24h, queries: 7d, replication: 24h, locks: 24h, metrics: 24h)
- [x] T046 [US4] Parse retention configuration from config.yaml per cli-interface.md
- [x] T047 [US4] Run initial prune on agent startup for data exceeding retention
- [x] T048 [US4] Ensure pruning does not block concurrent TUI reads (WAL mode handles this)

**Checkpoint**: Database size remains stable over time, old data automatically removed

---

## Phase 7: User Story 5 - Multi-Instance Monitoring (Priority: P2)

**Goal**: Monitor multiple PostgreSQL instances from single agent

**Independent Test**: Configure two PG instances, run agent, verify TUI shows data from both

### Implementation for User Story 5

- [x] T049 [US5] Parse instances configuration array from config.yaml per cli-interface.md
- [x] T050 [US5] Create connection pool per configured instance in internal/agent/pool.go
- [x] T051 [US5] Update collectors to iterate over all instances and tag data with instance_name
- [x] T052 [US5] Update AgentInstance table on connection state changes per data-model.md
- [x] T053 [US5] Handle partial failures (continue collecting from available instances)
- [x] T054 [US5] Add instance selector/filter to TUI views (identify which instance data came from)

**Checkpoint**: Agent monitors multiple instances, TUI distinguishes data by instance

---

## Phase 8: User Story 6 - Shared Configuration (Priority: P2)

**Goal**: Agent and TUI use same YAML config file with drift detection (non-enforced)

**Design Decision**: Shared configuration is recommended but not enforced. Both components independently load their configured YAML file. If configs differ, a warning appears in the TUI debug panel. This follows the Docker daemon/CLI model where mismatched configs are user error rather than a system failure.

**Independent Test**: Modify ~/.config/steep/config.yaml, verify both agent and TUI respect changes. Run agent with one config and TUI with another, verify warning appears in debug panel.

### Implementation for User Story 6

- [ ] T055 [US6] Ensure agent uses existing config loader from internal/config/config.go
- [ ] T056 [US6] Validate agent configuration on startup with clear error messages
- [ ] T057 [US6] Implement interval validation (>= 1s, <= 60s) per cli-interface.md
- [ ] T058 [US6] Implement retention validation (>= 1h, <= 720h) per cli-interface.md
- [ ] T059 [US6] Implement instance name validation (alphanumeric, hyphens, underscores) per cli-interface.md
- [ ] T060 [US6] Agent writes config_hash (SHA256 of agent section) to agent_status table on startup
- [ ] T061 [US6] TUI computes its own config_hash and compares with agent_status.config_hash on agent health check
- [ ] T062 [US6] Log warning to debug panel when TUI and agent config_hash differ ("Config mismatch detected: TUI and agent using different configurations")

**Checkpoint**: Agent and TUI can use different configs (user's choice) but mismatch is surfaced via debug panel warning. For best results, use the same config file for both components.

---

## Phase 9: User Story 7 - Background Alerting (Priority: P3)

**Goal**: Agent sends webhook notifications when thresholds breached

**Independent Test**: Configure webhook URL, trigger alert condition, verify webhook receives POST

### Implementation for User Story 7

- [ ] T063 [US7] Integrate existing alert engine (Feature 012) into agent in internal/agent/alerter.go
- [ ] T064 [US7] Implement webhook delivery with HTTP POST in internal/agent/webhook.go
- [ ] T065 [US7] Implement exponential backoff retry for failed webhook delivery
- [ ] T066 [US7] Send resolution notification when alert condition clears
- [ ] T067 [US7] Log webhook delivery success/failure without crashing agent
- [ ] T068 [US7] Parse alerts.enabled and alerts.webhook_url from config.yaml

**Checkpoint**: Agent sends webhook notifications for alert state changes

---

## Phase 10: User Story 8 - Agent Health Monitoring (Priority: P3)

**Goal**: Query agent status and health from TUI and CLI

**Independent Test**: View agent health in TUI status bar or via `steep-agent status`

### Implementation for User Story 8

- [ ] T069 [US8] Enhance `steep-agent status` to show connected instances, error counts, last errors
- [ ] T070 [US8] Add agent uptime to status bar in TUI when agent is healthy
- [ ] T071 [US8] Track and expose collection error counts in agent_status table
- [ ] T072 [US8] Display most recent error message in status output
- [ ] T073 [US8] Add health check endpoint in agent_status table (healthy if last_collect within 2x interval)

**Checkpoint**: Agent health visible via CLI and TUI

---

## Phase 11: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T074 [P] Implement graceful shutdown with context cancellation and 5-second timeout per research.md
- [ ] T075 [P] Add WAL checkpoint before exit (PRAGMA wal_checkpoint(TRUNCATE)) per research.md
- [ ] T076 [P] Implement schema version check with migration path for upgrades
- [ ] T077 [P] Add disk full detection and warning without crashing
- [ ] T078 [P] Add SQLite corruption detection on startup with recreate option
- [ ] T079 Update Makefile with full build, install, and test targets for steep-agent
- [ ] T080 Run quickstart.md validation scenarios end-to-end

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories (Phase 3-10)**: All depend on Foundational completion
  - US1-US3 (P1) are core - recommended sequential completion
  - US4-US6 (P2) can proceed after P1 stories
  - US7-US8 (P3) can proceed after P2 stories
- **Polish (Phase 11)**: Depends on all user stories being complete

### User Story Dependencies

- **US1 (Continuous Collection)**: Foundation only - delivers core value
- **US2 (Service Management)**: Foundation only - independent of US1 but typically done after
- **US3 (Dual-Mode TUI)**: Foundation + requires agent to exist (US1 or US2 complete)
- **US4 (Retention)**: Foundation only - can parallelize with US1-US3
- **US5 (Multi-Instance)**: Foundation + US1 (needs collector infrastructure)
- **US6 (Shared Config)**: Foundation only - can parallelize early
- **US7 (Background Alerting)**: US1 + US4 (needs collector + basic agent running)
- **US8 (Health Monitoring)**: US1 + US2 (needs agent status infrastructure)

### Within Each User Story

- Core agent code before CLI commands
- CLI commands before TUI integration
- Core functionality before polish features

### Parallel Opportunities

**Setup Phase:**
```
T002 (create cmd/steep-agent/) || T003 (create internal/agent/)
```

**Foundational Phase:**
```
T006 (AgentStatus entity) || T007 (AgentInstance entity)
```

**User Story 1 (Collectors):**
```
T012 (activity) || T013 (queries) || T014 (replication) || T015 (locks) || T016 (metrics)
```

**Polish Phase:**
```
T074 (graceful shutdown) || T075 (WAL checkpoint) || T076 (schema version) || T077 (disk full) || T078 (corruption detection)
```

---

## Implementation Strategy

### MVP First (P1 Stories Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: US1 - Continuous Data Collection
4. Complete Phase 4: US2 - Service Installation
5. Complete Phase 5: US3 - Dual-Mode TUI
6. **STOP and VALIDATE**: All P1 stories functional
7. Deploy MVP - agent collects data, installs as service, TUI auto-detects

### Incremental Delivery

1. Setup + Foundational - Foundation ready
2. US1 (Collection) - Agent runs in foreground, collects data
3. US2 (Service) - Agent installs as system service
4. US3 (Dual-Mode) - TUI switches modes automatically
5. US4 (Retention) - Database stays bounded
6. US5 (Multi-Instance) - Monitor multiple PostgreSQL servers
7. US6 (Config) - Single config for both components
8. US7 (Alerting) - Proactive notifications
9. US8 (Health) - Operational visibility
10. Polish - Production hardening

### Recommended Execution Order

Single developer (13 hrs/day):
1. Day 1: Setup + Foundational (T001-T010)
2. Day 2: US1 Continuous Collection (T011-T023)
3. Day 3: US2 Service Management (T024-T033)
4. Day 4: US3 Dual-Mode TUI (T034-T042)
5. Day 5: US4-US6 P2 Stories (T043-T062)
6. Day 6: US7-US8 P3 Stories + Polish (T063-T080)

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story
- Each user story independently completable and testable
- Existing monitors (activity, queries, replication, locks, tables) reused unchanged
- WAL mode already enabled - no changes needed for concurrent access
- kardianos/service handles platform-specific service management
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
