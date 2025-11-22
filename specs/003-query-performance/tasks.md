# Tasks: Query Performance Monitoring

**Input**: Design documents from `/specs/003-query-performance/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Test tasks are included per project testing requirements (unit tests for business logic, integration tests for database queries).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- **Source**: `internal/` (monitors, storage, ui, config)
- **Tests**: `tests/` (unit, integration)
- **Commands**: `cmd/steep/`

---

## Phase 1: Setup

**Purpose**: Project initialization, dependencies, and directory structure

- [x] T001 Add Go dependencies: pg_query_go/v6, go-sqlite3, testcontainers-go
- [x] T002 [P] Create directory structure: internal/monitors/queries/, internal/storage/sqlite/, internal/ui/views/queries/
- [x] T003 [P] Create test directory structure: tests/unit/monitors/queries/, tests/integration/queries/

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T004 Implement SQLite database connection manager in internal/storage/sqlite/db.go
- [x] T005 Create SQLite schema with query_stats table and indexes in internal/storage/sqlite/schema.go
- [x] T006 [P] Implement QueryFingerprinter using pg_query_go in internal/monitors/queries/fingerprint.go
- [x] T007 [P] Implement QueryStatsStore interface in internal/storage/sqlite/queries.go
- [x] T008 Implement LogCollector for PostgreSQL log parsing in internal/monitors/queries/log_collector.go
- [x] T009 Implement SamplingCollector for pg_stat_activity polling in internal/monitors/queries/sampling_collector.go
- [x] T010 Implement QueryMonitor goroutine orchestrating collection pipeline in internal/monitors/queries/monitor.go
- [x] T011 [P] Add queries-specific configuration to internal/config/queries.go
- [x] T012 Unit tests for fingerprinting in tests/unit/monitors/queries/fingerprint_test.go
- [x] T013 Unit tests for statistics aggregation in tests/unit/monitors/queries/stats_test.go
- [x] T014 Integration tests for SQLite storage in tests/integration/queries/storage_test.go

**Checkpoint**: Foundation ready - data collection pipeline operational, user story implementation can begin ✅

---

## Phase 3: User Story 1 - View Top Queries by Time (Priority: P1) MVP

**Goal**: Display queries ranked by total execution time with auto-refresh

**Independent Test**: Navigate to Queries view, observe queries sorted by total time, verify 5s auto-refresh updates data

### Visual Design (Constitution VI)

- [x] T015 [US1] Study pg_top and htop query displays, document visual patterns in specs/003-query-performance/visual-research.md
- [x] T016 [US1] Create ASCII mockup of Queries view in specs/003-query-performance/mockup.md
- [x] T017 [US1] Build throwaway demo testing table rendering approaches in internal/ui/views/queries/demo/

### Implementation for User Story 1

- [x] T018 [US1] Create base QueriesView model implementing ViewModel interface in internal/ui/views/queries/view.go
- [x] T019 [US1] Implement query table component with columns (Query, Calls, Total Time, Mean Time, Rows) in internal/ui/views/queries/view.go
- [x] T020 [US1] Add vim-style navigation (j/k/g/G) to table in internal/ui/views/queries/view.go
- [x] T021 [US1] Implement column sorting with `s` key in internal/ui/views/queries/view.go
- [x] T022 [US1] Add auto-refresh timer (configurable) in internal/app/app.go
- [x] T023 [US1] Register Queries view with main app and `3` key navigation in internal/app/app.go
- [x] T024 [US1] Wire monitor data to view via tea.Msg in internal/app/app.go

**Checkpoint**: User Story 1 complete - can view top queries by time with sorting and auto-refresh ✅

---

## Phase 4: User Story 2 - View Top Queries by Call Count (Priority: P1)

**Goal**: Switch to "By Calls" tab to see queries ranked by execution frequency

**Independent Test**: Switch to "By Calls" tab using arrow keys, verify queries sorted by call count

### Implementation for User Story 2

- [x] T025 [US2] Implement tab component with "By Time", "By Calls", "By Rows" in internal/ui/views/queries/tabs.go
- [x] T026 [US2] Add left/right arrow key navigation between tabs in internal/ui/views/queries/tabs.go
- [x] T027 [US2] Update table query to sort by calls when "By Calls" tab active in internal/ui/views/queries/view.go
- [x] T028 [US2] Style active tab indicator in internal/ui/views/queries/tabs.go

**Checkpoint**: User Story 2 complete - can switch tabs to view by calls ✅

---

## Phase 5: User Story 3 - View Top Queries by Rows (Priority: P1)

**Goal**: Switch to "By Rows" tab to see queries ranked by total rows returned

**Independent Test**: Switch to "By Rows" tab, verify queries sorted by row count

### Implementation for User Story 3

- [x] T029 [US3] Update table query to sort by rows when "By Rows" tab active in internal/ui/views/queries/view.go
- [x] T030 [US3] Ensure all three sort modes work correctly with column re-sorting in internal/ui/views/queries/view.go

**Checkpoint**: User Story 3 complete - all three tabs functional ✅

---

## Phase 6: User Story 4 - View EXPLAIN Plans (Priority: P2)

**Goal**: View execution plan for selected query to understand performance characteristics

**Independent Test**: Select query, press `e`, see formatted EXPLAIN JSON output, press Esc to return

### Implementation for User Story 4

- [x] T031 [US4] Implement ExplainCache with LRU eviction in internal/monitors/queries/explain_cache.go
- [x] T032 [US4] Add EXPLAIN (FORMAT JSON) execution in internal/monitors/queries/monitor.go
- [x] T033 [US4] Create EXPLAIN plan display component with viewport in internal/ui/views/queries/explain.go
- [x] T034 [US4] Add `e` key handler to execute EXPLAIN for selected query in internal/ui/views/queries/view.go
- [x] T035 [US4] Format JSON output for readability in internal/ui/views/queries/explain.go
- [x] T036 [US4] Add Esc/q key to return from EXPLAIN view in internal/ui/views/queries/explain.go
- [x] T037 [US4] Handle EXPLAIN errors (permissions, syntax) with user-friendly messages in internal/ui/views/queries/explain.go

**Checkpoint**: User Story 4 complete - can view EXPLAIN plans for any query ✅

---

## Phase 7: User Story 5 - Search and Filter Queries (Priority: P2)

**Goal**: Filter query list by text pattern using regex

**Independent Test**: Press `/`, enter pattern, see filtered results, clear to restore full list

### Implementation for User Story 5

- [x] T038 [US5] Create search input component in internal/ui/views/queries/view.go
- [x] T039 [US5] Add `/` key handler to activate search mode in internal/ui/views/queries/view.go
- [x] T040 [US5] Implement regex pattern matching in SQLite query in internal/storage/sqlite/queries.go
- [x] T041 [US5] Display active filter indicator in status bar in internal/ui/views/queries/view.go
- [x] T042 [US5] Add Esc key to clear filter and restore full list in internal/ui/views/queries/view.go

**Checkpoint**: User Story 5 complete - can search and filter queries by pattern ✅

---

## Phase 8: User Story 6 - Copy Query to Clipboard (Priority: P2)

**Goal**: Copy selected query text to system clipboard for use in other tools

**Independent Test**: Select query, press `y`, paste in another app to verify full query text

### Implementation for User Story 6

- [ ] T043 [US6] Implement ClipboardWriter wrapper with graceful degradation in internal/ui/clipboard.go
- [ ] T044 [US6] Add `y` key handler to copy full query text in internal/ui/views/queries/view.go
- [ ] T045 [US6] Display confirmation message in status bar after copy in internal/ui/views/queries/view.go
- [ ] T046 [US6] Handle clipboard unavailability (headless) with error message in internal/ui/clipboard.go

**Checkpoint**: User Story 6 complete - can copy queries to clipboard

---

## Phase 9: User Story 7 - Reset Statistics (Priority: P3)

**Goal**: Clear all query statistics to start fresh monitoring

**Independent Test**: Press `R`, confirm in dialog, verify stats cleared and empty view displayed

### Implementation for User Story 7

- [ ] T047 [US7] Create confirmation dialog component in internal/ui/components/confirm.go
- [ ] T048 [US7] Add `R` key handler to show reset confirmation in internal/ui/views/queries/view.go
- [ ] T049 [US7] Implement Reset() method in QueryStatsStore in internal/storage/sqlite/queries.go
- [ ] T050 [US7] Refresh view after reset to show empty state in internal/ui/views/queries/view.go

**Checkpoint**: User Story 7 complete - can reset statistics with confirmation

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T051 [P] Add data source status indicator (log_parsing/sampling) in internal/ui/views/queries/view.go
- [ ] T052 [P] Implement auto-enable logging prompt with ALTER SYSTEM in internal/monitors/queries/monitor.go
- [ ] T053 [P] Add 7-day cleanup on each refresh cycle in internal/monitors/queries/monitor.go
- [ ] T054 [P] Add help text overlay with keybindings in internal/ui/views/queries/help.go
- [ ] T055 [P] Ensure 80x24 minimum terminal size rendering in internal/ui/views/queries/view.go
- [ ] T056 [P] Add error handling for log file permissions with guidance in internal/monitors/queries/log_collector.go
- [ ] T057 Run quickstart.md validation scenarios
- [ ] T058 Performance validation: verify <500ms query execution, <100ms UI response

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phases 3-9)**: All depend on Foundational phase completion
  - US1 (Phase 3): Must complete first - establishes base view
  - US2 (Phase 4): Depends on US1 (adds tabs to existing view)
  - US3 (Phase 5): Depends on US2 (adds third tab)
  - US4-7 (Phases 6-9): Can proceed in parallel after US1-3
- **Polish (Phase 10)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational - Base view infrastructure
- **User Story 2 (P1)**: Depends on US1 - Adds tab navigation
- **User Story 3 (P1)**: Depends on US2 - Adds third tab
- **User Story 4 (P2)**: Depends on US1 - Independent EXPLAIN feature
- **User Story 5 (P2)**: Depends on US1 - Independent search feature
- **User Story 6 (P2)**: Depends on US1 - Independent clipboard feature
- **User Story 7 (P3)**: Depends on US1 - Independent reset feature

### Within Each User Story

- Visual design before implementation (US1 only)
- Models/services before UI
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- T002, T003 can run in parallel (directory creation)
- T006, T007, T011 can run in parallel (independent modules)
- T012, T013, T014 can run in parallel (independent test files)
- T015, T016, T017 can run sequentially (visual research flow)
- US4, US5, US6, US7 can all run in parallel after US3 completes
- All Polish tasks (T051-T058) can run in parallel

---

## Parallel Example: Foundational Phase

```bash
# After T004-T005 (database setup), these can run in parallel:
Task: "T006 Implement QueryFingerprinter in internal/monitors/queries/fingerprint.go"
Task: "T007 Implement QueryStatsStore in internal/storage/sqlite/queries.go"
Task: "T011 Add queries-specific configuration in internal/config/queries.go"

# After collectors complete, tests can run in parallel:
Task: "T012 Unit tests for fingerprinting"
Task: "T013 Unit tests for statistics aggregation"
Task: "T014 Integration tests for SQLite storage"
```

## Parallel Example: P2 Features (after US1-3 complete)

```bash
# US4, US5, US6, US7 are independent - all can run in parallel:
Task: "T031-T037 EXPLAIN Plans (US4)"
Task: "T038-T042 Search/Filter (US5)"
Task: "T043-T046 Clipboard (US6)"
Task: "T047-T050 Reset (US7)"
```

---

## Implementation Strategy

### MVP First (User Stories 1-3)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (base view)
4. Complete Phase 4: User Story 2 (tabs)
5. Complete Phase 5: User Story 3 (third tab)
6. **STOP and VALIDATE**: Test all three P1 stories
7. Deploy/demo MVP with core query viewing

### Incremental Delivery

1. Setup + Foundational → Data pipeline operational
2. Add US1 → View queries by time → Deploy (MVP!)
3. Add US2 + US3 → Full tabbed interface → Deploy
4. Add US4 → EXPLAIN plans → Deploy
5. Add US5 → Search/filter → Deploy
6. Add US6 → Clipboard → Deploy
7. Add US7 → Reset → Deploy
8. Polish → Production ready

### Parallel Team Strategy

With multiple developers after Foundational:
- Developer A: US1 → US2 → US3 (sequential, base UI)
- Developer B: US4 (EXPLAIN) once US1 baseline ready
- Developer C: US5 + US6 (Search + Clipboard) once US1 baseline ready
- Developer D: US7 (Reset) + Polish

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- US1-3 are sequential (building on same view), US4-7 are parallel
- Visual design phase (US1) follows Constitution VI requirements
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
