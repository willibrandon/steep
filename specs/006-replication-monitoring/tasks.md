# Tasks: Replication Monitoring & Setup

**Input**: Design documents from `/specs/006-replication-monitoring/`
**Prerequisites**: plan.md, spec.md, data-model.md, research.md, quickstart.md

**Tests**: Not explicitly requested in specification - test tasks not included.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- Models: `internal/db/models/`
- Queries: `internal/db/queries/`
- Monitors: `internal/monitors/`
- Storage: `internal/storage/sqlite/`
- UI Views: `internal/ui/views/replication/`
- UI Components: `internal/ui/components/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, new dependencies, and basic structure

- [x] T001 Install new dependencies: `go get github.com/charmbracelet/huh github.com/guptarohit/asciigraph github.com/sethvargo/go-password/password`
- [x] T002 Run `go mod tidy` to update go.mod and go.sum
- [x] T003 [P] Create directory structure: `internal/ui/views/replication/`, `internal/ui/views/replication/repviz/`, `internal/ui/views/replication/setup/`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core data models, queries, and infrastructure that ALL user stories depend on

**WARNING**: No user story work can begin until this phase is complete

### Data Models

- [x] T004 [P] Create Replica model with enums (ReplicationSyncState, LagSeverity) and helper methods (LagSeverity(), FormatByteLag()) in `internal/db/models/replication.go`
- [x] T005 [P] Create ReplicationSlot model with SlotType enum and helper methods (IsOrphaned(), RetentionWarning(), FormatRetainedBytes()) in `internal/db/models/replication.go`
- [x] T006 [P] Create Publication model with OperationFlags() helper in `internal/db/models/replication.go`
- [x] T007 [P] Create Subscription model with LagSeverity(), IsStale() helpers in `internal/db/models/replication.go`
- [x] T008 [P] Create LagHistoryEntry model for SQLite persistence in `internal/db/models/replication.go`
- [x] T009 [P] Create ReplicationData aggregate struct and NewReplicationData() constructor in `internal/db/models/replication.go`
- [x] T010 [P] Create ReplicationConfig and ConfigParam structs with IsReady(), RequiresRestart(), GetIssues() methods in `internal/db/models/replication.go`

### Database Queries

- [x] T011 [P] Implement GetReplicas() query (pg_stat_replication) with version-aware column handling in `internal/db/queries/replication.go`
- [x] T012 [P] Implement GetSlots() query (pg_replication_slots) with PG13+ wal_status handling in `internal/db/queries/replication.go`
- [x] T013 [P] Implement GetPublications() query (pg_publication, pg_publication_tables) in `internal/db/queries/replication.go`
- [x] T014 [P] Implement GetSubscriptions() query (pg_subscription, pg_stat_subscription) with PG12+ handling in `internal/db/queries/replication.go`
- [x] T015 [P] Implement GetReplicationConfig() query (pg_settings) for configuration readiness check in `internal/db/queries/replication.go`
- [x] T016 [P] Implement GetWALReceiverStatus() query (pg_stat_wal_receiver) for standby detection in `internal/db/queries/replication.go`
- [x] T017 Implement IsPrimary() detection query to determine server role in `internal/db/queries/replication.go`

### SQLite Storage

- [x] T018 Add replication_lag_history table schema with indexes to `internal/storage/sqlite/schema.go`
- [x] T019 Create ReplicationStore with SaveLagEntry(), GetLagHistory(), PruneLagHistory() methods in `internal/storage/sqlite/replication_store.go`

### Monitor Infrastructure

- [x] T020 Implement ReplicationMonitor with 2-second refresh, lag history ring buffer, and SQLite persistence in `internal/monitors/replication.go`
- [x] T021 Define ReplicationUpdate message type for Bubbletea message passing in `internal/monitors/replication.go`

### Base View Structure

- [x] T022 Create ReplicationView struct implementing ViewModel interface with Init(), Update(), View(), SetSize() in `internal/ui/views/replication/view.go`
- [x] T023 Implement tab definitions (Overview, Slots, Logical, Setup) and tab switching logic in `internal/ui/views/replication/tabs.go`
- [x] T024 Create help overlay content with all keybindings in `internal/ui/views/replication/help.go`
- [x] T025 Register `6` key for Replication view and add to view map in `internal/app/app.go`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - View Replication Lag (Priority: P1) MVP

**Goal**: Display replication lag (bytes and time) for each replica with color-coded severity

**Independent Test**: Connect to PostgreSQL primary with replicas, navigate to replication view, verify lag metrics display with correct color coding

### Implementation for User Story 1

- [x] T026 [US1] Implement replica table rendering in Overview tab showing Name, State, Sync, Byte Lag, Time Lag columns in `internal/ui/views/replication/view.go`
- [x] T027 [US1] Add color-coded lag severity styling: green (<1MB), yellow (1-10MB), red (>10MB) in `internal/ui/views/replication/view.go`
- [x] T028 [US1] Implement j/k vim-style navigation for replica list in `internal/ui/views/replication/view.go`
- [x] T029 [US1] Add sort functionality (by lag, name, state) with `s` key in `internal/ui/views/replication/view.go`
- [x] T030 [US1] Display "Replication not configured" message when no replicas exist in `internal/ui/views/replication/view.go`
- [x] T031 [US1] Handle permission errors with clear guidance message in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 1 complete - can view replication lag with color coding

---

## Phase 4: User Story 2 - View Replication Slot Status (Priority: P1)

**Goal**: Display replication slot status to monitor WAL retention and prevent disk exhaustion

**Independent Test**: View slots panel on any PostgreSQL with replication slots, verify slot details display correctly

### Implementation for User Story 2

- [x] T032 [US2] Implement Slots tab with table showing Name, Type, Active, Retained WAL columns in `internal/ui/views/replication/view.go`
- [x] T033 [US2] Add warning indicator for inactive slots retaining significant WAL (>80% threshold) in `internal/ui/views/replication/view.go`
- [x] T034 [US2] Highlight orphaned slots (inactive, referencing deleted replicas) as cleanup candidates in `internal/ui/views/replication/view.go`
- [x] T035 [US2] Add Tab key navigation to switch to Slots tab in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 2 complete - can view slot status with warnings

---

## Phase 5: User Story 3 - Visualize Replication Topology (Priority: P1)

**Goal**: ASCII tree diagram showing primary/replica relationships including cascading replication

**Independent Test**: Connect to primary, toggle topology view with `t`, verify tree structure displays correctly

### Implementation for User Story 3

- [x] T036 [US3] Implement topology tree rendering using xlab/treeprint showing primary at root in `internal/ui/views/replication/repviz/topology.go`
- [x] T037 [US3] Add support for cascading replication (replica-to-replica chains) in tree structure in `internal/ui/views/replication/repviz/topology.go`
- [x] T038 [US3] Display sync state indicators (sync, async, potential, quorum) on each node in `internal/ui/views/replication/repviz/topology.go`
- [x] T039 [US3] Show lag bytes next to each replica node in topology in `internal/ui/views/replication/repviz/topology.go`
- [x] T040 [US3] Add `t` key toggle to show/hide topology view in Overview tab in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 3 complete - can visualize replication topology

---

## Phase 6: User Story 4 - Check Replication Configuration Readiness (Priority: P1)

**Goal**: Validate PostgreSQL configuration parameters for replication readiness

**Independent Test**: Open configuration checker on any PostgreSQL, verify validation results display

### Implementation for User Story 4

- [x] T041 [US4] Implement configuration checker panel showing wal_level, max_wal_senders, max_replication_slots, wal_keep_size, hot_standby, archive_mode in `internal/ui/views/replication/setup/config_check.go`
- [x] T042 [US4] Display green checkmark for correctly configured parameters in `internal/ui/views/replication/setup/config_check.go`
- [x] T043 [US4] Display red X with guidance text for misconfigured parameters in `internal/ui/views/replication/setup/config_check.go`
- [x] T044 [US4] Show overall "READY" or "NOT READY" status summary in `internal/ui/views/replication/setup/config_check.go`
- [x] T045 [US4] Integrate configuration checker into Setup tab in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 4 complete - can check configuration readiness

---

## Phase 7: User Story 5 - Physical Replication Setup Wizard (Priority: P1)

**Goal**: Guided multi-step wizard to configure physical streaming replication

**Independent Test**: Walk through wizard on replication-ready PostgreSQL, verify generated commands are correct

### Implementation for User Story 5

- [x] T046 [US5] Create PhysicalWizard struct with multi-step form using charmbracelet/huh in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T047 [US5] Implement Step 1: Replication user configuration (username, password auto-gen/manual) in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T048 [US5] Implement Step 2: Sync mode selection (sync, async) and replica count in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T049 [US5] Implement Step 3: Review panel with generated pg_basebackup command in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T050 [US5] Generate recovery.conf/postgresql.auto.conf configuration for replica in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T051 [US5] Generate pg_hba.conf entries for replication access in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T052 [US5] Add copy-to-clipboard for all generated commands using golang.design/x/clipboard in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T053 [US5] Block wizard in read-only mode with appropriate message in `internal/ui/views/replication/setup/physical_wizard.go`
- [x] T054 [US5] Integrate physical wizard into Setup tab with keyboard shortcut in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 5 complete - can run physical replication setup wizard

---

## Phase 8: User Story 6 - View WAL Pipeline Stages (Priority: P2)

**Goal**: Show WAL pipeline stages (sent -> write -> flush -> replay) per replica for bottleneck diagnosis

**Independent Test**: Select replica, view details, verify pipeline visualization shows all 4 stages

### Implementation for User Story 6

- [x] T055 [US6] Implement WAL pipeline visualization showing Sent, Write, Flush, Replay LSN positions in `internal/ui/views/replication/repviz/pipeline.go`
- [x] T056 [US6] Add visual differentiation for lagging stages with byte difference display in `internal/ui/views/replication/repviz/pipeline.go`
- [x] T057 [US6] Show "caught up" indicator when all stages have minimal lag in `internal/ui/views/replication/repviz/pipeline.go`
- [x] T058 [US6] Add replica detail view showing pipeline when replica is selected in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 6 complete - can view WAL pipeline stages

---

## Phase 9: User Story 7 - View Lag History Trends (Priority: P2)

**Goal**: Sparkline visualization of lag trends over configurable time windows

**Independent Test**: Observe sparklines in replica list, change time window, verify historical data displays

### Implementation for User Story 7

- [x] T059 [US7] Create sparkline component using asciigraph with color support in `internal/ui/components/sparkline.go`
- [x] T060 [US7] Implement Unicode block alternative for compact single-line sparklines in `internal/ui/components/sparkline.go`
- [x] T061 [US7] Add sparkline column to replica table showing lag history in `internal/ui/views/replication/view.go`
- [x] T062 [US7] Implement time window selector (1m, 5m, 15m, 1h) with keyboard shortcut in `internal/ui/views/replication/view.go`
- [x] T063 [US7] Integrate with ReplicationStore to fetch historical data for extended windows in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 7 complete - can view lag trend sparklines

---

## Phase 10: User Story 8 - Monitor Logical Replication (Priority: P2)

**Goal**: Display logical replication publications and subscriptions

**Independent Test**: View Logical tab on instance with logical replication, verify pub/sub status displays

### Implementation for User Story 8

- [x] T064 [US8] Implement Logical tab with publications table showing name, table count, operation flags in `internal/ui/views/replication/logical.go`
- [x] T065 [US8] Add subscriptions table showing name, enabled status, upstream connection, lag in `internal/ui/views/replication/logical.go`
- [x] T066 [US8] Display "No logical replication configured" when no publications/subscriptions exist in `internal/ui/views/replication/logical.go`
- [x] T067 [US8] Handle wal_level != logical gracefully with guidance message in `internal/ui/views/replication/logical.go`

**Checkpoint**: User Story 8 complete - can monitor logical replication

---

## Phase 11: User Story 9 - Logical Replication Setup Wizard (Priority: P2)

**Goal**: Guided wizard to create publications and subscriptions for logical replication

**Independent Test**: Walk through wizard, verify generated CREATE PUBLICATION/SUBSCRIPTION SQL is correct

### Implementation for User Story 9

- [x] T068 [US9] Create LogicalWizard struct with multi-step form using charmbracelet/huh in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T069 [US9] Implement table selection step showing tables with size and row counts in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T070 [US9] Add warning for tables over 1GB about initial sync duration in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T071 [US9] Generate CREATE PUBLICATION SQL with selected tables and operations in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T072 [US9] Generate CREATE SUBSCRIPTION SQL with connection string and publication reference in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T073 [US9] Add copy-to-clipboard for generated SQL in `internal/ui/views/replication/setup/logical_wizard.go`
- [x] T074 [US9] Integrate logical wizard into Setup tab with keyboard shortcut in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 9 complete - can run logical replication setup wizard

---

## Phase 12: User Story 10 - Generate Connection Strings (Priority: P2)

**Goal**: Connection string builder with live preview and validation

**Independent Test**: Open builder, fill in fields, verify connection string preview and test connection

### Implementation for User Story 10

- [ ] T075 [US10] Create connection string builder form with host, port, user, application_name fields in `internal/ui/views/replication/setup/connstring.go`
- [ ] T076 [US10] Implement live preview of generated primary_conninfo string in `internal/ui/views/replication/setup/connstring.go`
- [ ] T077 [US10] Add test connection button to validate connectivity in `internal/ui/views/replication/setup/connstring.go`
- [ ] T078 [US10] Default to sslmode=prefer in generated connection strings in `internal/ui/views/replication/setup/connstring.go`
- [ ] T079 [US10] Add copy-to-clipboard for generated string in `internal/ui/views/replication/setup/connstring.go`
- [ ] T080 [US10] Integrate connection builder into Setup tab in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 10 complete - can generate and test connection strings

---

## Phase 13: User Story 11 - Create Replication Users (Priority: P2)

**Goal**: Create replication users with secure auto-generated or validated passwords

**Independent Test**: Create user through interface, verify user exists in database with correct privileges

### Implementation for User Story 11

- [ ] T081 [US11] Implement secure password generation using sethvargo/go-password in `internal/db/queries/replication.go`
- [ ] T082 [US11] Add password strength validation for user-provided passwords in `internal/db/queries/replication.go`
- [ ] T083 [US11] Implement CreateReplicationUser() query (CREATE USER with REPLICATION LOGIN) in `internal/db/queries/replication.go`
- [ ] T084 [US11] Create user creation form with username input and password option (auto/manual) in `internal/ui/views/replication/setup/physical_wizard.go`
- [ ] T085 [US11] Display generated password once with copy option, then mask in `internal/ui/views/replication/setup/physical_wizard.go`
- [ ] T086 [US11] Require superuser for user creation with clear error if unavailable in `internal/ui/views/replication/setup/physical_wizard.go`

**Checkpoint**: User Story 11 complete - can create replication users

---

## Phase 14: User Story 12 - Manage Replication Slots (Priority: P3)

**Goal**: Drop inactive/orphaned replication slots with confirmation dialog

**Independent Test**: Select inactive slot, attempt to drop, verify confirmation dialog and slot removal

### Implementation for User Story 12

- [ ] T087 [US12] Implement DropSlot() query (pg_drop_replication_slot) in `internal/db/queries/replication.go`
- [ ] T088 [US12] Add confirmation dialog for slot deletion with data loss warning in `internal/ui/views/replication/view.go`
- [ ] T089 [US12] Implement `d` key shortcut to drop selected slot in Slots tab in `internal/ui/views/replication/view.go`
- [ ] T090 [US12] Block slot management in read-only mode with appropriate message in `internal/ui/views/replication/view.go`
- [ ] T091 [US12] Refresh slot list after successful deletion in `internal/ui/views/replication/view.go`

**Checkpoint**: User Story 12 complete - can manage replication slots

---

## Phase 15: User Story 13 - Historical Lag Analysis (Priority: P3)

**Goal**: Configurable retention (24h-7d) for persistent lag history with automatic cleanup

**Independent Test**: Run application for extended period, query historical data, verify retention policy works

### Implementation for User Story 13

- [ ] T092 [US13] Add lag history retention configuration option (default 24h, max 7 days) in `internal/config/config.go`
- [ ] T093 [US13] Implement automatic cleanup of old lag history entries based on retention setting in `internal/monitors/replication.go`
- [ ] T094 [US13] Add extended time window options (6h, 12h, 24h, 7d) to sparkline selector in `internal/ui/views/replication/view.go`
- [ ] T095 [US13] Optimize GetLagHistory() query with proper indexing for long time ranges in `internal/storage/sqlite/replication_store.go`

**Checkpoint**: User Story 13 complete - can analyze historical lag trends

---

## Phase 16: Polish & Cross-Cutting Concerns

**Purpose**: Final improvements affecting multiple user stories

- [ ] T096 [P] Add ALTER SYSTEM command generation for wal_level, max_wal_senders, max_replication_slots in `internal/ui/views/replication/setup/config_check.go`
- [ ] T097 [P] Add restart-required indicator for postmaster-context parameters in `internal/ui/views/replication/setup/config_check.go`
- [ ] T098 [P] Implement auto-detection of primary vs replica role and adjust displayed statistics in `internal/ui/views/replication/view.go`
- [ ] T099 [P] Add WAL receiver status display when connected to standby server in `internal/ui/views/replication/view.go`
- [ ] T100 Ensure all views render correctly at 80x24 minimum terminal size in `internal/ui/views/replication/view.go`
- [ ] T101 Run quickstart.md manual testing checklist
- [ ] T102 Performance validation: verify query execution < 500ms, monitor 100+ replicas

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories (Phases 3-15)**: All depend on Foundational phase completion
  - P1 stories (US1-5) should be completed first
  - P2 stories (US6-11) can follow
  - P3 stories (US12-13) are lowest priority
- **Polish (Phase 16)**: Depends on all user stories being complete

### User Story Dependencies

| Story | Priority | Dependencies | Notes |
|-------|----------|--------------|-------|
| US1 - View Lag | P1 | Foundational only | Core monitoring |
| US2 - View Slots | P1 | Foundational only | Independent of US1 |
| US3 - Topology | P1 | Foundational only | Independent |
| US4 - Config Check | P1 | Foundational only | Independent |
| US5 - Physical Wizard | P1 | US4 (config check) | Uses config checker |
| US6 - WAL Pipeline | P2 | US1 (replica data) | Extends replica view |
| US7 - Lag History | P2 | US1 (replica data) | Extends replica view |
| US8 - Logical Repl | P2 | Foundational only | Independent tab |
| US9 - Logical Wizard | P2 | US8 (logical data) | Extends logical tab |
| US10 - Conn Strings | P2 | Foundational only | Setup utility |
| US11 - Create Users | P2 | US5 (wizard context) | Part of wizard flow |
| US12 - Slot Mgmt | P3 | US2 (slot view) | Extends slot tab |
| US13 - Lag History | P3 | US7 (sparklines) | Extends history feature |

### Parallel Opportunities

**Within Phase 2 (Foundational)**:
- T004-T010 (all models) can run in parallel
- T011-T017 (all queries) can run in parallel
- T018-T019 (SQLite) must be sequential

**User Stories in Parallel** (with multiple developers):
- US1, US2, US3, US4 can all start immediately after Foundational
- US5 can start after US4 config checker
- US6, US7 can start after US1
- US8 can start after Foundational
- US9 can start after US8
- US10, US11 can start after US5

---

## Parallel Example: Foundational Phase

```bash
# Launch all model tasks together:
Task: "Create Replica model in internal/db/models/replication.go"
Task: "Create ReplicationSlot model in internal/db/models/replication.go"
Task: "Create Publication model in internal/db/models/replication.go"
Task: "Create Subscription model in internal/db/models/replication.go"

# Launch all query tasks together:
Task: "Implement GetReplicas() in internal/db/queries/replication.go"
Task: "Implement GetSlots() in internal/db/queries/replication.go"
Task: "Implement GetPublications() in internal/db/queries/replication.go"
```

---

## Implementation Strategy

### MVP First (User Stories 1-5)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phases 3-7: User Stories 1-5 (all P1)
4. **STOP and VALIDATE**: Test all P1 stories independently
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational = Foundation ready
2. Add US1 (lag view) = Can see replication health
3. Add US2 (slots) = Can monitor WAL retention
4. Add US3 (topology) = Can visualize architecture
5. Add US4 + US5 (config + wizard) = Can set up replication
6. Continue with P2 stories as needed

### Suggested MVP Scope

**Minimum Viable Product**: User Stories 1-4
- View replication lag with color coding
- View replication slot status
- Visualize replication topology
- Check configuration readiness

This delivers immediate monitoring value without setup wizards.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- All setup operations must respect read-only mode (FR-027)
- PostgreSQL 11+ minimum, some features require PG13+ or PG15+
