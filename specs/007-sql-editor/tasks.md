# Tasks: SQL Editor & Execution

**Input**: Design documents from `/specs/007-sql-editor/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Not explicitly requested in specification. Manual UI testing checklist provided in quickstart.md.

**Organization**: Tasks grouped by user story (8 stories: US1-US8) to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1-US8)
- File paths follow existing Steep patterns: `internal/ui/views/sqleditor/`

---

## Phase 1: Setup ✓

**Purpose**: Create project structure and shared infrastructure

- [x] T001 Create sqleditor package directory at internal/ui/views/sqleditor/
- [x] T002 [P] Add ViewSQLEditor constant to internal/ui/views/types.go
- [x] T003 [P] Create SQL Editor styles in internal/ui/styles/sqleditor.go
- [x] T004 [P] Create data models (Query, ResultSet, TransactionState, HistoryEntry, Snippet, EditorState) in internal/ui/views/sqleditor/models.go
- [x] T005 [P] Create Bubbletea message types in internal/ui/views/sqleditor/messages.go
- [x] T006 [P] Define key bindings in internal/ui/views/sqleditor/keys.go

---

## Phase 2: Foundational (Blocking Prerequisites) ✓

**Purpose**: Core infrastructure required by ALL user stories

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T007 Create SessionExecutor with transaction state tracking in internal/ui/views/sqleditor/executor.go
- [x] T008 Implement query execution with timeout and cancellation in internal/db/queries/sqleditor.go
- [x] T009 [P] Implement value formatting (formatValue) for all PostgreSQL types in internal/ui/views/sqleditor/format.go
- [x] T010 Create main SQLEditorView struct with Init/Update/View/SetSize in internal/ui/views/sqleditor/view.go
- [x] T011 Wire SQL Editor view to app navigation ('7' key) in internal/app/app.go

**Checkpoint**: SQL Editor view accessible via '7' key, foundation ready ✓

---

## Phase 3: User Story 1 - Write and Execute SQL Queries (Priority: P1) ✓

**Goal**: Enable DBAs to write multi-line SQL queries and execute them against the database

**Independent Test**: Open SQL Editor, type query, press Ctrl+Enter, verify results appear

### Implementation for User Story 1

- [x] T012 [US1] Create Editor component wrapping bubbles/textarea with line numbers in internal/ui/views/sqleditor/view.go
- [x] T013 [US1] Configure textarea with ShowLineNumbers, FocusedStyle, BlurredStyle in internal/ui/views/sqleditor/view.go
- [x] T014 [US1] Implement focus management (Focus/Blur methods) in internal/ui/views/sqleditor/view.go
- [x] T015 [US1] Add placeholder text "Enter SQL query... (Ctrl+Enter to execute)" in internal/ui/views/sqleditor/view.go
- [x] T016 [US1] Implement Ctrl+Enter key binding to trigger query execution in internal/ui/views/sqleditor/view.go
- [x] T017 [US1] Implement Esc key to cancel running query in internal/ui/views/sqleditor/view.go
- [x] T018 [US1] Display query execution progress indicator in internal/ui/views/sqleditor/view.go
- [x] T019 [US1] Display database error messages with line/position info in internal/ui/views/sqleditor/view.go
- [x] T020 [US1] Add query audit logging with timestamp/status/duration in internal/ui/views/sqleditor/executor.go

**Checkpoint**: Can write and execute queries, see success or error messages ✓

---

## Phase 4: User Story 2 - View Query Results in Paginated Table (Priority: P1) ✓

**Goal**: Display query results in scrollable, paginated table with navigation

**Independent Test**: Execute query returning 500+ rows, verify pagination and row navigation work

### Implementation for User Story 2

- [x] T021 [US2] Create ResultsTable component using bubbles/table in internal/ui/views/sqleditor/view.go
- [x] T022 [US2] Implement column header display with type indicators in internal/ui/views/sqleditor/view.go
- [x] T023 [US2] Implement pagination (100 rows/page) with page indicator in internal/ui/views/sqleditor/view.go
- [x] T024 [US2] Implement n/p keys for page navigation in internal/ui/views/sqleditor/view.go
- [x] T025 [US2] Display execution time and total row count in footer in internal/ui/views/sqleditor/view.go
- [x] T026 [US2] Style NULL values with dimmed text (styles.DimStyle) in internal/ui/views/sqleditor/view.go
- [x] T027 [US2] Implement row highlighting for current selection in internal/ui/views/sqleditor/view.go
- [x] T028 [US2] Handle empty result sets ("0 rows returned" message) in internal/ui/views/sqleditor/view.go

**Checkpoint**: Results display with pagination, NULL styling, row navigation. US1+US2 = functional MVP ✓

---

## Phase 5: User Story 3 - Syntax Highlighting for SQL (Priority: P2)

**Goal**: Apply SQL syntax highlighting to executed queries and history display

**Independent Test**: Execute query, verify keywords/strings/numbers show in different colors

### Implementation for User Story 3

- [ ] T029 [US3] Create HighlightSQL function using Chroma (postgresql, monokai) in internal/ui/views/sqleditor/highlight.go
- [ ] T030 [US3] Apply highlighting to executed query header in results pane in internal/ui/views/sqleditor/results.go
- [ ] T031 [US3] Apply highlighting to history display (when history implemented) in internal/ui/views/sqleditor/history.go

**Checkpoint**: SQL syntax highlighting visible in query header and history

---

## Phase 6: User Story 4 - Transaction Management (Priority: P2)

**Goal**: Support BEGIN/COMMIT/ROLLBACK commands with visual transaction indicator

**Independent Test**: Issue BEGIN, execute INSERT, issue ROLLBACK, verify changes not persisted

### Implementation for User Story 4

- [ ] T032 [US4] Implement DetectTransactionStatement function in internal/ui/views/sqleditor/transaction.go
- [ ] T033 [US4] Handle BEGIN command - start pgx.Tx and update state in internal/ui/views/sqleditor/executor.go
- [ ] T034 [US4] Handle COMMIT command - commit transaction and clear state in internal/ui/views/sqleditor/executor.go
- [ ] T035 [US4] Handle ROLLBACK command - rollback transaction and clear state in internal/ui/views/sqleditor/executor.go
- [ ] T036 [US4] Implement SAVEPOINT and ROLLBACK TO savepoint support in internal/ui/views/sqleditor/executor.go
- [ ] T037 [US4] Create StatusBar component with connection info in internal/ui/views/sqleditor/statusbar.go
- [ ] T038 [US4] Display transaction indicator (green "TX" badge) when active in internal/ui/views/sqleditor/statusbar.go
- [ ] T039 [US4] Display DDL warning when in active transaction in internal/ui/views/sqleditor/view.go
- [ ] T040 [US4] Block DDL/DML in read-only mode with warning in internal/ui/views/sqleditor/executor.go

**Checkpoint**: Transaction control fully functional with visual indicator

---

## Phase 7: User Story 5 - Natural Keyboard Shortcuts (Priority: P2)

**Goal**: Vim-style and VS Code-like keyboard shortcuts for efficient navigation

**Independent Test**: Verify Tab switches focus, j/k navigates rows, y copies cell

### Implementation for User Story 5

- [ ] T041 [US5] Implement Tab/Shift+Tab to switch focus between editor and results in internal/ui/views/sqleditor/view.go
- [ ] T042 [US5] Implement j/k (vim-style) row navigation in results pane in internal/ui/views/sqleditor/results.go
- [ ] T043 [US5] Implement g/G for first/last row navigation in internal/ui/views/sqleditor/results.go
- [ ] T044 [US5] Implement y key to copy current cell value to clipboard in internal/ui/views/sqleditor/results.go
- [ ] T045 [US5] Implement Y key to copy entire row (tab-separated) to clipboard in internal/ui/views/sqleditor/results.go
- [ ] T046 [US5] Implement Ctrl+Up/Down to resize editor/results split in internal/ui/views/sqleditor/view.go
- [ ] T047 [US5] Create help overlay content with all shortcuts in internal/ui/views/sqleditor/help.go
- [ ] T048 [US5] Display keyboard hints in footer area in internal/ui/views/sqleditor/view.go

**Checkpoint**: All keyboard shortcuts working, help overlay available

---

## Phase 8: User Story 6 - Query History with Recall (Priority: P3)

**Goal**: Store and recall previously executed queries

**Independent Test**: Execute 3 queries, press up arrow at editor top, verify history cycles

### Implementation for User Story 6

- [ ] T049 [US6] Create HistoryManager struct with in-memory cache (100 entries) in internal/ui/views/sqleditor/history.go
- [ ] T050 [US6] Initialize SQLite database for persistent history in internal/ui/views/sqleditor/history.go
- [ ] T051 [US6] Implement Add() to store query with deduplication in internal/ui/views/sqleditor/history.go
- [ ] T052 [US6] Implement Previous()/Next() for navigation in internal/ui/views/sqleditor/history.go
- [ ] T053 [US6] Detect cursor at editor boundary (isAtStart) for history trigger in internal/ui/views/sqleditor/editor.go
- [ ] T054 [US6] Implement Up arrow at boundary to recall previous query in internal/ui/views/sqleditor/view.go
- [ ] T055 [US6] Implement Down arrow to navigate forward in history in internal/ui/views/sqleditor/view.go
- [ ] T056 [US6] Implement Ctrl+R reverse search overlay in internal/ui/views/sqleditor/view.go

**Checkpoint**: Query history with recall and reverse search working

---

## Phase 9: User Story 7 - Save Queries as Snippets (Priority: P3)

**Goal**: Save and load named query snippets

**Independent Test**: Save query as "test-snippet", restart app, load "test-snippet"

### Implementation for User Story 7

- [ ] T057 [US7] Create SnippetManager struct with YAML persistence in internal/ui/views/sqleditor/snippets.go
- [ ] T058 [US7] Implement Save(name, sql) with overwrite confirmation in internal/ui/views/sqleditor/snippets.go
- [ ] T059 [US7] Implement Load(name) to retrieve snippet in internal/ui/views/sqleditor/snippets.go
- [ ] T060 [US7] Implement List() to get all saved snippets in internal/ui/views/sqleditor/snippets.go
- [ ] T061 [US7] Add :save NAME command handling in internal/ui/views/sqleditor/view.go
- [ ] T062 [US7] Add :load NAME command handling in internal/ui/views/sqleditor/view.go
- [ ] T063 [US7] Implement Ctrl+O snippet browser overlay in internal/ui/views/sqleditor/view.go

**Checkpoint**: Snippets save/load with YAML persistence working

---

## Phase 10: User Story 8 - Export Results (Priority: P3)

**Goal**: Export query results to CSV or JSON files

**Independent Test**: Execute query, run :export csv output.csv, verify file contents

### Implementation for User Story 8

- [ ] T064 [US8] Implement ExportCSV function with proper escaping in internal/ui/views/sqleditor/export.go
- [ ] T065 [US8] Implement ExportJSON function (array of objects) in internal/ui/views/sqleditor/export.go
- [ ] T066 [US8] Add :export csv FILENAME command handling in internal/ui/views/sqleditor/view.go
- [ ] T067 [US8] Add :export json FILENAME command handling in internal/ui/views/sqleditor/view.go
- [ ] T068 [US8] Display export success/error notification in internal/ui/views/sqleditor/view.go

**Checkpoint**: CSV and JSON export fully functional

---

## Phase 11: Polish & Cross-Cutting Concerns

**Purpose**: Final polish and integration

- [ ] T069 [P] Handle automatic reconnection on connection loss in internal/ui/views/sqleditor/executor.go
- [ ] T070 [P] Display connection status notification (disconnected/reconnecting/reconnected) in internal/ui/views/sqleditor/statusbar.go
- [ ] T071 Implement query retry on successful reconnection in internal/ui/views/sqleditor/executor.go
- [ ] T072 Handle large result sets with streaming/progress in internal/ui/views/sqleditor/results.go
- [ ] T073 [P] Add documentation to README.md for SQL Editor feature
- [ ] T074 Run manual UI testing checklist from quickstart.md

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories (Phase 3-10)**: All depend on Phase 2 completion
  - US1 + US2 (both P1) form the MVP
  - US3-US5 (P2) enhance the MVP
  - US6-US8 (P3) add convenience features
- **Polish (Phase 11)**: Depends on all desired stories complete

### User Story Dependencies

- **US1 (P1)**: Start after Phase 2 - Core query execution
- **US2 (P1)**: Start after Phase 2 - Results display (can parallel with US1)
- **US3 (P2)**: Start after Phase 2 - Syntax highlighting (independent)
- **US4 (P2)**: Start after Phase 2 - Transactions (independent)
- **US5 (P2)**: Requires US2 complete (results navigation)
- **US6 (P3)**: Start after Phase 2 - History (independent)
- **US7 (P3)**: Start after Phase 2 - Snippets (independent)
- **US8 (P3)**: Requires US2 complete (needs results to export)

### Parallel Opportunities

**Setup Phase**:
```bash
# All marked [P] can run in parallel:
T002, T003, T004, T005, T006
```

**User Story Phase (after Foundational)**:
```bash
# US1 and US2 can run in parallel (different files)
# US3, US4, US6, US7 can start in parallel (all independent)
```

---

## Parallel Example: User Story 1 + User Story 2

```bash
# After Phase 2 completes, launch both P1 stories in parallel:

# US1: Editor and execution
Task: "T012 [US1] Create Editor component wrapping bubbles/textarea in internal/ui/views/sqleditor/editor.go"

# US2: Results display (different file)
Task: "T021 [US2] Create ResultsTable component using bubbles/table in internal/ui/views/sqleditor/results.go"
```

---

## Implementation Strategy

### MVP First (US1 + US2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL)
3. Complete Phase 3: User Story 1 (write/execute)
4. Complete Phase 4: User Story 2 (results display)
5. **STOP and VALIDATE**: Test basic SQL editing independently
6. Deploy/demo MVP - users can write and execute queries

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 + US2 → MVP ready (basic SQL editor)
3. US3 + US4 + US5 → Enhanced (highlighting, transactions, keyboard)
4. US6 + US7 + US8 → Full feature (history, snippets, export)
5. Polish → Production ready

### Suggested Checkpoints

- **After T011**: SQL Editor accessible via '7' key
- **After T020**: Can execute queries and see success/error
- **After T028**: Results display with pagination - **MVP COMPLETE**
- **After T048**: All P2 features working
- **After T068**: All P3 features working
- **After T074**: Feature complete and tested

---

## Notes

- [P] tasks can run in parallel (different files)
- [US#] label maps task to user story for traceability
- US1 + US2 = MVP (minimum viable SQL Editor)
- Each user story independently completable and testable
- Commit after each task or logical group
- Test manually using quickstart.md checklist
