# Tasks: Database Management Operations

**Input**: Design documents from `/specs/010-database-operations/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests are included as they are standard practice in this codebase (see existing `tests/` directory).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Single project**: `internal/` for Go packages, `tests/` for tests
- Paths follow established Steep architecture per CLAUDE.md

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create new files and extend existing models with vacuum status

- [x] T001 Create branch `010-database-operations` if not exists and checkout
- [x] T002 [P] Create `internal/db/models/operation.go` with MaintenanceOperation, OperationProgress, OperationResult, OperationHistory types per data-model.md
- [x] T003 [P] Create `internal/db/models/role.go` with Role, RoleMembership, Permission types per data-model.md
- [x] T004 [P] Create `internal/db/models/enums.go` with OperationType, OperationStatus, PermissionObjectType, PrivilegeType enums per data-model.md

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Extend `internal/db/models/table.go` with vacuum status fields: LastVacuum, LastAutovacuum, LastAnalyze, LastAutoanalyze, VacuumCount, AutovacuumCount, AutovacuumEnabled per data-model.md
- [x] T006 Extend `internal/db/queries/tables.go` GetTablesWithStats query to include vacuum status columns from pg_stat_all_tables per contracts/vacuum_status.go.md
- [x] T007 Create `internal/db/queries/maintenance.go` with MaintenanceExecutor interface stub and VacuumOptions, RunningOperation types per contracts/maintenance.go.md
- [x] T008 [P] Create `internal/ui/views/tables/operations.go` with OperationsMenu, OperationMenuItem types and DefaultOperationsMenu constructor per contracts/ui_operations.go.md
- [x] T009 [P] Add TablesMode constants for ModeOperationsMenu, ModeOperationProgress, ModeConfirmCancel, ModeConfirmVacuum, ModeConfirmAnalyze, ModeConfirmReindex in `internal/ui/views/tables/view.go`
- [x] T010 Add operationsMenu, currentOperation, operationHistory fields to TablesView struct in `internal/ui/views/tables/view.go`
- [x] T011 Implement read-only mode check helper function in `internal/ui/views/tables/operations.go` that disables destructive operations when --readonly flag is set

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Execute VACUUM on Tables (Priority: P1) 🎯 MVP

**Goal**: Enable DBAs to execute VACUUM variants (VACUUM, VACUUM FULL, VACUUM ANALYZE) on tables with progress tracking

**Independent Test**: Select a table, press `x`, select VACUUM, confirm, verify progress displays and operation completes with result message

### Tests for User Story 1

- [x] T012 [P] [US1] Create `tests/integration/maintenance_test.go` with TestExecuteVacuum, TestVacuumProgress tests using testcontainers
- [x] T013 [P] [US1] Create `tests/unit/progress_test.go` with TestCalculatePercent, TestFormatDuration unit tests

### Implementation for User Story 1

- [x] T014 [US1] Implement ExecuteVacuum function in `internal/db/queries/maintenance.go` with VACUUM, VACUUM FULL, VACUUM ANALYZE SQL generation per contracts/maintenance.go.md
- [x] T015 [US1] Implement GetVacuumProgress function in `internal/db/queries/maintenance.go` querying pg_stat_progress_vacuum per contracts/maintenance.go.md
- [x] T016 [US1] Implement GetVacuumFullProgress function in `internal/db/queries/maintenance.go` querying pg_stat_progress_cluster per contracts/maintenance.go.md
- [x] T017 [US1] Add `x` key binding in `internal/ui/views/tables/view.go` handleKeyPress to open operations menu when table selected
- [x] T018 [US1] Implement OperationsMenu.View() rendering with VACUUM, VACUUM FULL, VACUUM ANALYZE options in `internal/ui/views/tables/operations.go`
- [x] T019 [US1] Implement OperationsMenu key handling (j/k navigation, Enter select, Esc close) in `internal/ui/views/tables/operations.go`
- [x] T020 [US1] Create ConfirmOperationDialog component in `internal/ui/views/tables/operations.go` with y/Enter confirm, n/Esc cancel per contracts/ui_operations.go.md
- [x] T021 [US1] Implement GetOperationWarning function for VACUUM FULL lock warning in `internal/ui/views/tables/operations.go`
- [x] T022 [US1] Create ProgressIndicator component in `internal/ui/views/tables/operations.go` with progress bar, phase, elapsed time per contracts/ui_operations.go.md
- [x] T023 [US1] Implement StartOperation method in TablesView that executes vacuum and starts progress polling in `internal/ui/views/tables/view.go`
- [x] T024 [US1] Implement pollProgress using tea.Tick for 1-second polling interval in `internal/ui/views/tables/view.go`
- [x] T025 [US1] Add OperationStartedMsg, OperationProgressMsg, OperationCompletedMsg message handlers to TablesView.Update() in `internal/ui/views/tables/view.go`
- [x] T026 [US1] Display operation result toast with duration and completion status after VACUUM completes in `internal/ui/views/tables/view.go`
- [x] T027 [US1] Add ModeOperationsMenu and ModeOperationProgress rendering to TablesView.View() in `internal/ui/views/tables/view.go`
- [x] T028 [US1] Update help overlay in `internal/ui/views/tables/help.go` with `x` key for operations menu

**Checkpoint**: At this point, User Story 1 should be fully functional - DBAs can execute VACUUM with progress tracking

---

## Phase 4: User Story 2 - Execute ANALYZE on Tables (Priority: P1)

**Goal**: Enable DBAs to execute ANALYZE on tables to update query planner statistics

**Independent Test**: Select a table, press `x`, select ANALYZE, confirm, verify operation completes and last_analyze timestamp updates

### Tests for User Story 2

- [x] T029 [P] [US2] Add TestExecuteAnalyze test to `tests/integration/maintenance_test.go`

### Implementation for User Story 2

- [x] T030 [US2] Implement ExecuteAnalyze function in `internal/db/queries/tables.go` per contracts/maintenance.go.md
- [x] T031 [US2] Add ANALYZE option to OperationsMenu in `internal/ui/views/tables/operations.go` (should already be in DefaultOperationsMenu)
- [x] T032 [US2] Handle ANALYZE execution in StartOperation with spinner (no progress tracking available) in `internal/ui/views/tables/view.go`

**Checkpoint**: User Stories 1 AND 2 should both work independently - VACUUM and ANALYZE operations functional

---

## Phase 5: User Story 3 - Execute REINDEX on Indexes (Priority: P2)

**Goal**: Enable DBAs to execute REINDEX on table indexes to rebuild corrupted or bloated indexes

**Independent Test**: Select a table, press `x`, select REINDEX TABLE, confirm warning about locks, verify operation completes

### Tests for User Story 3

- [x] T033 [P] [US3] Add TestExecuteReindex test to `tests/integration/maintenance_test.go`

### Implementation for User Story 3

- [x] T034 [US3] Implement ExecuteReindex function in `internal/db/queries/maintenance.go` with REINDEX TABLE and REINDEX INDEX SQL per contracts/maintenance.go.md
- [x] T035 [US3] Add REINDEX TABLE option to OperationsMenu in `internal/ui/views/tables/operations.go`
- [x] T036 [US3] Add GetOperationWarning case for REINDEX lock warning in `internal/ui/views/tables/operations.go`
- [x] T037 [US3] Handle REINDEX execution in StartOperation with spinner (no progress tracking) in `internal/ui/views/tables/view.go`

**Checkpoint**: User Stories 1, 2, AND 3 should work - all maintenance operations functional

---

## Phase 6: User Story 4 - View VACUUM and Autovacuum Status (Priority: P2)

**Goal**: Display vacuum/autovacuum timestamps in Tables view with stale indicators

**Independent Test**: Navigate to Tables view, verify Last Vacuum and Last Autovacuum columns display timestamps with color-coded staleness

### Tests for User Story 4

- [ ] T038 [P] [US4] Add TestGetTablesWithVacuumStatus test to `tests/integration/maintenance_test.go`
- [ ] T039 [P] [US4] Create `tests/unit/vacuum_status_test.go` with TestGetVacuumStatusIndicator, TestFormatVacuumTimestamp tests

### Implementation for User Story 4

- [ ] T040 [US4] Implement FormatVacuumTimestamp function in `internal/db/queries/maintenance.go` per contracts/vacuum_status.go.md (relative time: "5m ago", "2h ago", "3d ago")
- [ ] T041 [US4] Implement GetVacuumStatusIndicator function with 7-day stale threshold in `internal/db/queries/maintenance.go` per contracts/vacuum_status.go.md
- [ ] T042 [US4] Add "Last Vacuum" column to Tables view header in `internal/ui/views/tables/view.go` renderHeader()
- [ ] T043 [US4] Add "Last Autovacuum" column to Tables view header in `internal/ui/views/tables/view.go` renderHeader()
- [ ] T044 [US4] Render vacuum timestamps with color-coded staleness (green=OK, yellow=warning, red=critical) in `internal/ui/views/tables/view.go` renderTreeRow()
- [ ] T045 [US4] Add autovacuum enabled/disabled indicator to table details panel in `internal/ui/views/tables/view.go`

**Checkpoint**: Tables view now shows maintenance health status for all tables

---

## Phase 7: User Story 5 - Manage Database Users and Roles (Priority: P3)

**Goal**: Create new Roles view accessible via `0` key showing all database roles with attributes and memberships

**Independent Test**: Press `0` from any view, verify Roles view displays all roles with name, superuser status, login capability, connection limit, memberships

### Tests for User Story 5

- [ ] T046 [P] [US5] Create `tests/integration/roles_test.go` with TestGetRoles, TestGetRoleMemberships tests

### Implementation for User Story 5

- [ ] T047 [US5] Create `internal/db/queries/roles.go` with GetRoles function per contracts/roles.go.md SQL query
- [ ] T048 [US5] Implement GetRoleMemberships function in `internal/db/queries/roles.go` per contracts/roles.go.md
- [ ] T049 [US5] Implement GetRoleDetails function in `internal/db/queries/roles.go` per contracts/roles.go.md
- [ ] T050 [US5] Implement FormatRoleAttributes helper (S=superuser, L=login, R=createrole, D=createdb, B=bypassrls) in `internal/db/queries/roles.go`
- [ ] T051 [US5] Implement FormatConnectionLimit helper (-1 = ∞) in `internal/db/queries/roles.go`
- [ ] T052 [US5] Create `internal/ui/views/roles/view.go` with RolesView struct following Tables view pattern
- [ ] T053 [US5] Implement RolesView.Init() to fetch roles on load in `internal/ui/views/roles/view.go`
- [ ] T054 [US5] Implement RolesView.View() with role table (Name, Attributes, Conn Limit, Valid Until, Member Of) in `internal/ui/views/roles/view.go`
- [ ] T055 [US5] Implement RolesView.Update() with keyboard navigation (j/k, g/G, Enter for details) in `internal/ui/views/roles/view.go`
- [ ] T056 [US5] Add role details panel showing memberships, owned objects on Enter in `internal/ui/views/roles/view.go`
- [ ] T057 [US5] Create `internal/ui/views/roles/help.go` with help overlay for Roles view
- [ ] T058 [US5] Add `0` key binding in `internal/ui/app.go` to switch to Roles view
- [ ] T059 [US5] Register RolesView in view switching logic in `internal/ui/app.go`

**Checkpoint**: Roles view fully functional - users can browse all database roles and view details

---

## Phase 8: User Story 6 - Grant/Revoke Permissions (Priority: P3)

**Goal**: Enable viewing and modifying permissions on database objects with GRANT/REVOKE operations

**Independent Test**: Select a table, view permissions, grant SELECT to a role, verify permission appears in list

### Tests for User Story 6

- [ ] T060 [P] [US6] Add TestGetTablePermissions, TestGrantTablePrivilege, TestRevokeTablePrivilege tests to `tests/integration/roles_test.go`

### Implementation for User Story 6

- [ ] T061 [US6] Implement GetTablePermissions function in `internal/db/queries/roles.go` using aclexplode() per contracts/roles.go.md
- [ ] T062 [US6] Implement GrantTablePrivilege function in `internal/db/queries/roles.go` with SQL generation per contracts/roles.go.md
- [ ] T063 [US6] Implement RevokeTablePrivilege function in `internal/db/queries/roles.go` with optional CASCADE per contracts/roles.go.md
- [ ] T064 [US6] Create `internal/ui/views/tables/permissions.go` with PermissionsDialog component
- [ ] T065 [US6] Implement permissions list view showing current grants in `internal/ui/views/tables/permissions.go`
- [ ] T066 [US6] Add GRANT dialog with role selector and privilege type picker in `internal/ui/views/tables/permissions.go`
- [ ] T067 [US6] Add REVOKE confirmation dialog with optional CASCADE warning in `internal/ui/views/tables/permissions.go`
- [ ] T068 [US6] Wire permissions dialog to table details panel or operations menu in `internal/ui/views/tables/view.go`
- [ ] T069 [US6] Enforce read-only mode blocking for GRANT/REVOKE operations in `internal/ui/views/tables/permissions.go`

**Checkpoint**: Full permission management - view, grant, revoke permissions with confirmation

---

## Phase 9: Cancellation Support (Cross-Cutting P1)

**Goal**: Enable cancellation of running maintenance operations via `c` key

**Independent Test**: Start VACUUM on large table, press `c`, confirm cancel, verify operation stops

### Implementation for Cancellation

- [ ] T070 Implement CancelOperation function calling pg_cancel_backend in `internal/db/queries/maintenance.go` per contracts/maintenance.go.md
- [ ] T071 Add `c` key binding in ModeOperationProgress to show cancel confirmation in `internal/ui/views/tables/view.go`
- [ ] T072 Create cancel confirmation dialog (ModeConfirmCancel) in `internal/ui/views/tables/operations.go`
- [ ] T073 Handle CancelOperationMsg and OperationCancelledMsg in TablesView.Update() in `internal/ui/views/tables/view.go`
- [ ] T074 Add cancel hint "[c] Cancel operation" to progress indicator view in `internal/ui/views/tables/operations.go`

**Checkpoint**: Users can cancel long-running operations with confirmation

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T075 Implement single-operation enforcement in TablesView - block new operations while one is in progress in `internal/ui/views/tables/view.go`
- [ ] T076 Add session-only OperationHistory tracking in TablesView (cleared on exit) in `internal/ui/views/tables/view.go`
- [ ] T077 Add GetRunningOperations query to detect existing maintenance operations in `internal/db/queries/maintenance.go`
- [ ] T078 Implement error handling for ErrReadOnlyMode, ErrOperationInProgress, ErrInsufficientPrivileges in `internal/db/queries/maintenance.go`
- [ ] T079 Add actionable error messages per contracts/maintenance.go.md error table in `internal/ui/views/tables/operations.go`
- [ ] T080 [P] Add connection loss handling during operations - show reconnection status in `internal/ui/views/tables/view.go`
- [ ] T081 Run integration tests with `make test` to validate all operations
- [ ] T082 Run quickstart.md validation checklist

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-8)**: All depend on Foundational phase completion
  - US1 (VACUUM) and US2 (ANALYZE) can proceed in parallel
  - US3 (REINDEX) can proceed in parallel with US1/US2
  - US4 (Vacuum Status) depends on T005-T006 from Foundational
  - US5 (Roles View) is fully independent
  - US6 (Permissions) depends on US5 completion (needs roles queries)
- **Cancellation (Phase 9)**: Depends on US1 completion (needs progress mode)
- **Polish (Phase 10)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational - Shares operations menu with US1
- **User Story 3 (P2)**: Can start after Foundational - Shares operations menu with US1/US2
- **User Story 4 (P2)**: Can start after Foundational - Extends Tables view independently
- **User Story 5 (P3)**: Can start after Foundational - Fully independent (new view)
- **User Story 6 (P3)**: Depends on US5 for role queries and models

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Models before services/queries
- Queries before UI components
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel (T002, T003, T004)
- All Foundational tasks marked [P] can run in parallel (T008, T009)
- Test tasks marked [P] can run in parallel within each story
- US1, US2, US3 can start in parallel after Foundational
- US4 and US5 can start in parallel after Foundational
- US6 must wait for US5

---

## Parallel Example: User Story 1 (VACUUM)

```bash
# Launch all tests for User Story 1 together:
Task: "Create tests/integration/maintenance_test.go with TestExecuteVacuum"
Task: "Create tests/unit/progress_test.go with TestCalculatePercent"

# After tests written and failing, implement in parallel where possible:
Task: "Implement ExecuteVacuum function"
Task: "Implement GetVacuumProgress function"
Task: "Implement GetVacuumFullProgress function"

# Then UI components (some parallel):
Task: "Implement OperationsMenu.View()"
Task: "Implement OperationsMenu key handling"
Task: "Create ConfirmOperationDialog component"
Task: "Create ProgressIndicator component"
```

---

## Parallel Example: User Story 5 (Roles View)

```bash
# Independent from other stories - can start immediately after Foundational:
Task: "Create internal/db/queries/roles.go with GetRoles"
Task: "Implement GetRoleMemberships function"

# UI components:
Task: "Create internal/ui/views/roles/view.go"
Task: "Create internal/ui/views/roles/help.go"

# Integration:
Task: "Add 0 key binding in internal/ui/app.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (4 tasks)
2. Complete Phase 2: Foundational (7 tasks)
3. Complete Phase 3: User Story 1 - VACUUM (17 tasks)
4. **STOP and VALIDATE**: Test VACUUM execution with progress tracking
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 (VACUUM) → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 (ANALYZE) → Test independently → Deploy/Demo
4. Add User Story 3 (REINDEX) → Test independently → Deploy/Demo
5. Add User Story 4 (Vacuum Status) → Test independently → Deploy/Demo
6. Add User Story 5 (Roles View) → Test independently → Deploy/Demo
7. Add User Story 6 (Permissions) → Test independently → Deploy/Demo
8. Add Cancellation Support → Test → Deploy/Demo
9. Polish phase → Final validation

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 (VACUUM) + User Story 2 (ANALYZE)
   - Developer B: User Story 5 (Roles View) + User Story 6 (Permissions)
   - Developer C: User Story 4 (Vacuum Status)
3. Then:
   - Developer A: User Story 3 (REINDEX) + Cancellation
   - Developer B: Polish
4. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Read-only mode (--readonly flag) must block all destructive operations (VACUUM, ANALYZE, REINDEX, GRANT, REVOKE)
- Progress polling uses 1-second interval per spec
- Stale vacuum threshold defaults to 7 days per clarification session
- Roles view uses `0` key (only remaining number key per clarification)
- Session-only operation history (not persisted per clarification)
