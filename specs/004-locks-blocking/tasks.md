# Tasks: Locks & Blocking Detection

**Input**: Design documents from `/specs/004-locks-blocking/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: Integration tests included as this is a database-heavy feature requiring testcontainers validation.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Single project**: `internal/`, `tests/` at repository root
- Follows existing Steep structure from CLAUDE.md

---

## Phase 1: Setup

**Purpose**: Project initialization and dependency setup

- [x] T001 Add github.com/xlab/treeprint dependency with `go get github.com/xlab/treeprint`
- [x] T002 [P] Create locks view directory structure at internal/ui/views/locks/

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL - Constitution Principle VI**: Visual Design First must be completed here before any UI implementation.

- [x] T003 Study reference tools (pg_top, htop, k9s) and capture screenshots of lock/tree displays
- [x] T004 Create ASCII mockup of locks view layout in specs/004-locks-blocking/mockups.md
- [x] T005 Build Demo 1: Simple lock table with lipgloss borders in internal/ui/demos/demo_locks_table/main.go
- [x] T006 Build Demo 2: Split view (table top, tree bottom) in internal/ui/demos/demo_locks_split/main.go
- [x] T007 Build Demo 3: Alternative layout for comparison in internal/ui/demos/demo_locks_alt/main.go
- [x] T008 Document visual acceptance criteria and chosen approach in specs/004-locks-blocking/visual-design.md
- [x] T009 [P] Create Lock model struct in internal/db/models/lock.go
- [x] T010 [P] Create BlockingRelationship struct in internal/db/models/lock.go
- [x] T011 [P] Create BlockingChain struct for tree rendering in internal/db/models/lock.go
- [x] T012 Wire `4` key navigation to ViewLocks in internal/app/app.go (was already wired)

**Checkpoint**: Foundation ready - Visual design approved, models defined, navigation wired

---

## Phase 3: User Story 1 - View Active Locks (Priority: P1) MVP

**Goal**: Display all active locks with type, mode, granted status, database, relation, and query

**Independent Test**: Navigate to Locks view with `5` key, verify table displays all columns with accurate lock data

### Tests for User Story 1

- [x] T013 [P] [US1] Integration test for GetLocks query in tests/integration/locks/locks_test.go
- [x] T014 [P] [US1] Unit test for lock table rendering in tests/unit/locks/locks_view_test.go

### Implementation for User Story 1

- [x] T015 [US1] Implement GetLocks() query function in internal/db/queries/locks.go
- [x] T016 [US1] Implement LocksMonitor goroutine with 2s refresh in internal/monitors/locks.go
- [x] T017 [US1] Create LocksUpdateMsg message type in internal/ui/messages.go
- [x] T018 [US1] Implement LocksView with table display in internal/ui/views/locks/view.go
- [x] T019 [US1] Implement SetSize() for terminal resize handling in internal/ui/views/locks/view.go
- [x] T020 [US1] Add column sorting with `s` key in internal/ui/views/locks/view.go
- [x] T021 [US1] Implement detail view for full query with `d` key in internal/ui/views/locks/view.go (MUST follow explain.go pattern exactly: manual scrollOffset, msg.String() for keys, JoinVertical layout, pg_format via Docker, chroma monokai highlighting, footer key hints)
- [x] T022 [US1] Create help text with keyboard shortcuts in internal/ui/views/locks/help.go
- [x] T023 [US1] Add auto-refresh ticker (2 seconds) in internal/ui/views/locks/view.go

**Checkpoint**: User Story 1 complete - Locks view displays all active locks with sorting and detail view

---

## Phase 4: User Story 2 - Identify Blocking Queries (Priority: P1) MVP

**Goal**: Detect and highlight blocking relationships with color coding (red=blocked, yellow=blocking)

**Independent Test**: Create blocking scenario in database, verify blocked queries appear red and blocking queries appear yellow

### Tests for User Story 2

- [x] T024 [P] [US2] Integration test for GetBlockingRelationships query in tests/integration/locks/locks_test.go
- [x] T025 [P] [US2] Unit test for blocking color assignment in tests/unit/locks/locks_view_test.go

### Implementation for User Story 2

- [x] T026 [US2] Implement GetBlockingRelationships() using pg_blocking_pids() in internal/db/queries/locks.go
- [x] T027 [US2] Build blocking/blocked PID maps in LocksMonitor in internal/monitors/locks.go
- [x] T028 [US2] Add blocked (red) and blocking (yellow) styles in internal/ui/styles/colors.go
- [x] T029 [US2] Apply conditional row styling based on blocking status in internal/ui/views/locks/view.go
- [x] T030 [US2] Update LocksUpdateMsg to include BlockingPIDs and BlockedPIDs maps in internal/ui/messages.go

**Checkpoint**: User Stories 1 & 2 complete - Lock contention immediately visible through color coding

---

## Phase 5: User Story 3 - Visualize Lock Dependency Tree (Priority: P2)

**Goal**: Display ASCII tree visualization of blocking chains below the lock table

**Independent Test**: Create multi-level blocking chain (A blocks B, B blocks C), verify tree shows correct hierarchy

### Tests for User Story 3

- [x] T031 [P] [US3] Unit test for RenderLockTree with various chain depths in tests/unit/locks/lock_tree_test.go

### Implementation for User Story 3

- [x] T032 [US3] Implement BuildBlockingChains() to construct tree from relationships in internal/monitors/locks.go
- [x] T033 [US3] Implement RenderLockTree() using treeprint library in internal/ui/components/lock_tree.go
- [x] T034 [US3] Add truncateQuery() helper for tree node display in internal/ui/components/lock_tree.go
- [x] T035 [US3] Integrate tree view below table in locks view in internal/ui/views/locks/view.go
- [x] T036 [US3] Handle empty tree state (no blocking) gracefully in internal/ui/views/locks/view.go

**Checkpoint**: User Story 3 complete - Blocking relationships visualized as hierarchical tree

---

## Phase 6: User Story 4 - Kill Blocking Query (Priority: P2)

**Goal**: Terminate blocking queries with confirmation dialog, respecting readonly mode

**Independent Test**: Select blocking query, press `x`, confirm dialog appears, query terminates on confirm

### Tests for User Story 4

- [x] T037 [P] [US4] Integration test for TerminateBackend query in tests/integration/locks/locks_test.go
- [x] T038 [P] [US4] Unit test for confirmation dialog behavior in tests/unit/locks/locks_view_test.go

### Implementation for User Story 4

- [x] T039 [US4] Implement TerminateBackend() query function in internal/db/queries/locks.go
- [x] T040 [US4] Create KillQueryResultMsg message type in internal/ui/messages.go
- [x] T041 [US4] Implement confirmation dialog for kill action in internal/ui/views/locks/view.go
- [x] T042 [US4] Add `x` key handler with readonly mode check in internal/ui/views/locks/view.go
- [x] T043 [US4] Handle kill success/failure messages in view in internal/ui/views/locks/view.go
- [x] T044 [US4] Auto-refresh after kill action completes in internal/ui/views/locks/view.go

**Checkpoint**: User Story 4 complete - DBAs can resolve lock contention by terminating blockers

---

## Phase 7: User Story 5 - View Deadlock History (Priority: P3)

**Goal**: Display historical deadlock information for pattern analysis

**Independent Test**: Trigger deadlocks, verify historical records captured and displayed

**Note**: This story may require additional storage infrastructure (SQLite similar to query stats) for deadlock history persistence.

### Implementation for User Story 5

- [ ] T045 [US5] Design deadlock history storage schema (evaluate SQLite vs in-memory)
- [ ] T046 [US5] Implement deadlock event capture from PostgreSQL logs or polling
- [ ] T047 [US5] Create deadlock history view or tab in locks view
- [ ] T048 [US5] Display deadlock details with timestamps, PIDs, and queries

**Checkpoint**: User Story 5 complete - Historical deadlock patterns visible for analysis

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T049 [P] Validate performance: GetLocks() < 500ms with 100+ locks
- [ ] T050 [P] Validate performance: GetBlockingRelationships() < 500ms
- [ ] T051 [P] Validate memory usage < 50MB during operation
- [ ] T052 Test minimum terminal size 80x24 rendering
- [ ] T053 Run full quickstart.md manual testing checklist
- [ ] T054 Update help text with all keyboard shortcuts in internal/ui/views/locks/help.go
- [ ] T055 Code cleanup and error message improvements

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories 1-2 (Phases 3-4)**: Both P1 priority, can proceed sequentially (US2 builds on US1)
- **User Stories 3-4 (Phases 5-6)**: P2 priority, can start after US1-2 complete
- **User Story 5 (Phase 7)**: P3 priority, can proceed independently but lower priority
- **Polish (Phase 8)**: Depends on US1-4 completion (US5 optional)

### User Story Dependencies

- **User Story 1 (P1)**: After Foundational - No dependencies on other stories
- **User Story 2 (P1)**: After US1 - Extends lock display with blocking detection
- **User Story 3 (P2)**: After US2 - Uses blocking relationships for tree
- **User Story 4 (P2)**: After US2 - Needs blocking detection for kill targeting
- **User Story 5 (P3)**: After Foundational - Independent but deferred

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Models before queries
- Queries before monitors
- Monitors before views
- Core implementation before polish

### Parallel Opportunities

- T001, T002 can run in parallel (Setup phase)
- T003-T008 are sequential (visual design validation)
- T009, T010, T011 can run in parallel (model definitions)
- T013, T014 can run in parallel (US1 tests)
- T024, T025 can run in parallel (US2 tests)
- T031 can run standalone (US3 tests)
- T037, T038 can run in parallel (US4 tests)
- T049, T050, T051 can run in parallel (performance validation)

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together:
Task: "T013 [P] [US1] Integration test for GetLocks query in tests/integration/locks_test.go"
Task: "T014 [P] [US1] Unit test for lock table rendering in tests/unit/locks_view_test.go"

# Launch all models in Foundational phase together:
Task: "T009 [P] Create Lock model struct in internal/db/models/lock.go"
Task: "T010 [P] Create BlockingRelationship struct in internal/db/models/lock.go"
Task: "T011 [P] Create BlockingChain struct for tree rendering in internal/db/models/lock.go"
```

---

## Implementation Strategy

### MVP First (User Stories 1-2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - includes visual design validation)
3. Complete Phase 3: User Story 1 (view locks)
4. Complete Phase 4: User Story 2 (blocking detection)
5. **STOP and VALIDATE**: Test lock viewing and blocking identification
6. Deploy/demo - This is a functional MVP for lock monitoring

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Basic lock viewing
3. Add User Story 2 → Test independently → Blocking detection (MVP complete!)
4. Add User Story 3 → Test independently → Tree visualization
5. Add User Story 4 → Test independently → Kill capability
6. Add User Story 5 → Test independently → Historical analysis

### Key Milestones

- **After Phase 2**: Visual design approved, ready for implementation
- **After Phase 4**: MVP complete - Lock viewing with blocking detection
- **After Phase 6**: Full P1+P2 features - Complete lock management
- **After Phase 8**: Production-ready with performance validation

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Visual Design First (Phase 2) is NON-NEGOTIABLE per constitution
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently

## UI Consistency Requirements (NON-NEGOTIABLE)

**Before implementing any UI component:**
1. **READ THE ENTIRE `internal/ui/views/queries/view.go` FILE (all ~990 lines)** - Do not skim. Do not stop early.
2. Copy-paste working code as starting template, then modify. Do not write from scratch.

**Reference files:**
- `internal/ui/views/queries/view.go` - **MUST READ COMPLETELY** before writing view.go
- `internal/ui/views/queries/explain.go` - PRIMARY REFERENCE for detail view

**Detail view (T021) MUST follow explain.go exactly:**
1. Manual `scrollOffset` for scrolling (NOT viewport - it causes Esc delay)
2. Key handling: `msg.String() == "esc"` (NOT `msg.Type == tea.KeyEsc`)
3. Layout: `lipgloss.JoinVertical` with title, content, footer
4. SQL formatting: Docker pgFormatter
5. Syntax highlighting: chroma with monokai theme
6. Footer: `[↑/↓]scroll [Esc]close [c]copy` format
7. Table focus: `table.Blur()` on enter, `table.Focus()` on exit

**Failure to follow these patterns will result in Esc key delay bugs.**
