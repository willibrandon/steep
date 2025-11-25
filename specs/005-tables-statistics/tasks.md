# Tasks: Tables & Statistics Viewer

**Input**: Design documents from `/specs/005-tables-statistics/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Not explicitly requested - manual testing checklist provided in quickstart.md

**Organization**: Tasks grouped by user story for independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, etc.)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- Models: `internal/db/models/`
- Queries: `internal/db/queries/`
- Views: `internal/ui/views/tables/`
- App integration: `internal/app/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create directory structure and base files for Tables feature

- [X] T001 Create tables view package directory at internal/ui/views/tables/
- [X] T002 [P] Create data models file at internal/db/models/table.go with Schema, Table, Index, TableColumn, Constraint, TableDetails structs
- [X] T003 [P] Create queries file at internal/db/queries/tables.go with package declaration and imports
- [X] T004 [P] Add FormatBytes helper function to internal/db/models/table.go for human-readable size display

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core database queries that ALL user stories depend on

**CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Implement GetSchemas query function in internal/db/queries/tables.go (returns all schemas with OID, name, owner)
- [X] T006 Implement GetTablesWithStats query function in internal/db/queries/tables.go (returns tables with size, row count, cache hit ratio)
- [X] T007 [P] Implement GetIndexesWithStats query function in internal/db/queries/tables.go (returns indexes with scan count, size, cache hit)
- [X] T008 [P] Implement GetPartitionHierarchy query function in internal/db/queries/tables.go (returns parent→child OID mapping)
- [X] T009 Create TablesView struct with all fields in internal/ui/views/tables/view.go per contracts/view.go.md
- [X] T010 Implement NewTablesView factory function in internal/ui/views/tables/view.go
- [X] T011 Implement SetSize method for TablesView in internal/ui/views/tables/view.go
- [X] T012 Register TablesView in internal/app/app.go for key '5' (ViewTables already defined in types.go)

**Checkpoint**: Foundation ready - can load schemas/tables data and switch to Tables view

---

## Phase 3: User Story 1 - Browse Schema Hierarchy (Priority: P1) MVP

**Goal**: Hierarchical navigation of schemas and tables with expand/collapse and system schema toggle

**Independent Test**: Press '5' to open Tables view, navigate with j/k, expand/collapse schemas with Enter, toggle system schemas with P

### Implementation for User Story 1

- [X] T013 [US1] Implement Init() method in internal/ui/views/tables/view.go (fetch schemas, tables, check extension)
- [X] T014 [US1] Implement fetchTablesData command in internal/ui/views/tables/view.go (calls GetSchemas, GetTablesWithStats, GetIndexesWithStats, GetPartitionHierarchy)
- [X] T015 [US1] Implement buildTreeItems helper in internal/ui/views/tables/view.go (builds flat list from schema→table→partition hierarchy)
- [X] T016 [US1] Implement renderMainView method in internal/ui/views/tables/view.go (renders tree with schema/table rows, headers, footer)
- [X] T017 [US1] Implement renderTreeRow helper in internal/ui/views/tables/view.go (renders single row with tree prefixes: ▼▶├└)
- [X] T018 [US1] Implement Update() method for navigation keys (j/k/arrows) in internal/ui/views/tables/view.go
- [X] T019 [US1] Implement Update() for expand/collapse (Enter, left/right arrows) in internal/ui/views/tables/view.go
- [X] T020 [US1] Implement system schema toggle (P key) in internal/ui/views/tables/view.go (filter showSystemSchemas)
- [X] T021 [US1] Implement partition expand/collapse in internal/ui/views/tables/view.go (nested under parent tables)
- [X] T022 [US1] Implement 30-second auto-refresh using scheduleRefresh command in internal/ui/views/tables/view.go

**Checkpoint**: User Story 1 complete - can browse all schemas/tables, expand/collapse, toggle system schemas

---

## Phase 4: User Story 2 - View Table Size and Row Statistics (Priority: P1)

**Goal**: Display table statistics (size, rows, cache hit) with sorting

**Independent Test**: View table list with Size/Rows/Cache columns, sort by pressing s/S

### Implementation for User Story 2

- [X] T023 [US2] Add table statistics columns to renderMainView in internal/ui/views/tables/view.go (Name, Size, Rows, Cache Hit %)
- [X] T024 [US2] Implement sortTables helper in internal/ui/views/tables/view.go (sort by name, size, rows, cache hit)
- [X] T025 [US2] Implement Update() for sort keys (s cycles column, S toggles direction) in internal/ui/views/tables/view.go
- [X] T026 [US2] Add sort indicator to column headers in renderMainView in internal/ui/views/tables/view.go
- [X] T027 [US2] Implement dynamic column widths based on terminal width in renderMainView in internal/ui/views/tables/view.go

**Checkpoint**: User Story 2 complete - can view table statistics and sort by any column

---

## Phase 5: User Story 3 - View Index Usage Statistics (Priority: P2)

**Goal**: Display index statistics with unused index highlighting and Tab focus switching

**Independent Test**: Select table, view index list with scan counts, see yellow highlighting for unused indexes

### Implementation for User Story 3

- [X] T028 [US3] Add index list section to renderMainView in internal/ui/views/tables/view.go (below table list or in split view)
- [X] T029 [US3] Implement renderIndexRow helper in internal/ui/views/tables/view.go (Name, Size, Scans, Rows Read, Cache Hit %)
- [X] T030 [US3] Add yellow highlighting for unused indexes (ScanCount == 0) using styles.ColorWarning in internal/ui/views/tables/view.go
- [X] T031 [US3] Implement Tab key for focus switching between table list and index list in internal/ui/views/tables/view.go
- [X] T032 [US3] Implement clipboard copy (y key) for selected index name in internal/ui/views/tables/view.go

**Checkpoint**: User Story 3 complete - can view index statistics, identify unused indexes, copy names

---

## Phase 6: User Story 4 - View Bloat Estimates (Priority: P2)

**Goal**: Display bloat percentage with color coding, graceful fallback when pgstattuple unavailable

**Independent Test**: View bloat column, see red/yellow color coding, see "N/A" when extension missing

### Implementation for User Story 4

- [X] T033 [US4] Implement CheckPgstattupleExtension query in internal/db/queries/tables.go
- [X] T034 [US4] Implement checkExtension command in internal/ui/views/tables/view.go (sets pgstattupleAvailable flag)
- [X] T035 [US4] Add Bloat % column to renderMainView in internal/ui/views/tables/view.go (shows "N/A" when unavailable)
- [X] T036 [US4] Implement estimated bloat calculation from dead_rows in internal/db/queries/tables.go (fallback when no pgstattuple)
- [X] T037 [US4] Add red highlighting for high bloat (>20%) using styles.ColorError in internal/ui/views/tables/view.go
- [X] T038 [US4] Add yellow highlighting for moderate bloat (10-20%) using styles.ColorWarning in internal/ui/views/tables/view.go
- [X] T039 [US4] Add Bloat to sortTables options in internal/ui/views/tables/view.go

**Checkpoint**: User Story 4 complete - can view bloat estimates with color coding

---

## Phase 7: User Story 5 - Auto-Install pgstattuple Extension (Priority: P2)

**Goal**: Prompt to install pgstattuple extension with confirmation dialog, session-scoped preference

**Independent Test**: Connect without pgstattuple, see install prompt, confirm/decline, verify preference remembered

### Implementation for User Story 5

- [X] T040 [US5] Implement InstallPgstattupleExtension query in internal/db/queries/tables.go (CREATE EXTENSION pgstattuple)
- [X] T041 [US5] Implement ModeConfirmInstall rendering in internal/ui/views/tables/view.go (confirmation dialog overlay)
- [X] T042 [US5] Implement installExtension command in internal/ui/views/tables/view.go (executes CREATE EXTENSION)
- [X] T043 [US5] Add install prompt logic to Init() in internal/ui/views/tables/view.go (show dialog if not installed and not readonly and not previously declined)
- [X] T044 [US5] Implement Update() for confirm dialog keys (y/Enter to confirm, n/Esc to decline) in internal/ui/views/tables/view.go
- [X] T045 [US5] Implement session-scoped installPromptShown flag to prevent re-prompting after decline in internal/ui/views/tables/view.go
- [X] T046 [US5] Add success/failure toast messages for extension installation in internal/ui/views/tables/view.go
- [X] T047 [US5] Implement readonly mode check to skip install prompt in internal/ui/views/tables/view.go

**Checkpoint**: User Story 5 complete - extension install flow works with confirmation and session preference

---

## Phase 8: User Story 6 - View Table Details Panel (Priority: P2)

**Goal**: Modal details panel showing columns, constraints, foreign keys

**Independent Test**: Select table, press Enter/d, see columns and constraints, press Esc to close

### Implementation for User Story 6

- [X] T048 [US6] Implement GetTableDetails query in internal/db/queries/tables.go (columns, constraints)
- [X] T049 [US6] Implement fetchTableDetails command in internal/ui/views/tables/view.go
- [X] T050 [US6] Implement ModeDetails handling in Update() in internal/ui/views/tables/view.go (Enter/d opens, Esc/q closes)
- [X] T051 [US6] Implement renderDetails method in internal/ui/views/tables/view.go (columns table, constraints list)
- [X] T052 [US6] Implement renderWithOverlay helper in internal/ui/views/tables/view.go (modal overlay pattern from locks view)
- [X] T053 [US6] Add size breakdown display (heap, indexes, TOAST) to details panel in internal/ui/views/tables/view.go
- [X] T054 [US6] Implement clipboard copy (y key) for table name in schema.table format in details mode in internal/ui/views/tables/view.go

**Checkpoint**: User Story 6 complete - can view full table details in modal panel

---

## Phase 9: User Story 7 - Execute Maintenance Operations (Priority: P3)

**Goal**: VACUUM, ANALYZE, REINDEX operations with confirmation dialogs

**Independent Test**: Select table, press v/a/r, see confirmation, execute and see toast result

### Implementation for User Story 7

- [X] T055 [US7] Implement ExecuteVacuum query in internal/db/queries/tables.go (properly quoted schema.table)
- [X] T056 [US7] Implement ExecuteAnalyze query in internal/db/queries/tables.go
- [X] T057 [US7] Implement ExecuteReindex query in internal/db/queries/tables.go
- [X] T058 [US7] Implement ModeConfirmVacuum/Analyze/Reindex handling in Update() in internal/ui/views/tables/view.go
- [X] T059 [US7] Implement renderMaintenanceConfirm method in internal/ui/views/tables/view.go (confirmation dialog)
- [X] T060 [US7] Implement executeVacuum/executeAnalyze/executeReindex commands in internal/ui/views/tables/view.go
- [X] T061 [US7] Add readonly mode check for maintenance operations in internal/ui/views/tables/view.go (show toast if blocked)
- [X] T062 [US7] Add success/failure toast messages for maintenance operations in internal/ui/views/tables/view.go

**Checkpoint**: User Story 7 complete - can execute maintenance operations with confirmation

---

## Phase 10: Help & Polish

**Purpose**: Help overlay, code cleanup, final integration

- [ ] T063 Create help.go file at internal/ui/views/tables/help.go with keyboard shortcuts content
- [ ] T064 Implement ModeHelp handling in Update() (h/? opens, Esc/q closes) in internal/ui/views/tables/view.go
- [ ] T065 Implement renderHelp method in internal/ui/views/tables/view.go
- [ ] T066 [P] Add empty state handling when no user tables exist in internal/ui/views/tables/view.go
- [ ] T067 [P] Add error handling for query failures with user-friendly messages in internal/ui/views/tables/view.go
- [ ] T068 [P] Add minimum terminal width check (80 columns) with graceful degradation in internal/ui/views/tables/view.go
- [ ] T069 Run go build and fix any compilation errors
- [ ] T070 Run manual testing checklist from quickstart.md

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Story 1-2 (Phase 3-4)**: P1 priority, core functionality
- **User Story 3-6 (Phase 5-8)**: P2 priority, can proceed after US1-2 or in parallel
- **User Story 7 (Phase 9)**: P3 priority, can proceed after foundational
- **Polish (Phase 10)**: Depends on all user stories being complete

### User Story Dependencies

- **US1 (Browse Schema Hierarchy)**: Foundation only - no other story dependencies
- **US2 (Table Statistics)**: Foundation only - can run parallel with US1
- **US3 (Index Statistics)**: Requires US1 table list rendering
- **US4 (Bloat Estimates)**: Foundation only - adds column to existing table view
- **US5 (Auto-Install Extension)**: Requires US4 bloat display
- **US6 (Table Details)**: Requires US1 table selection
- **US7 (Maintenance Operations)**: Requires US1 table selection

### Parallel Opportunities

**Within Setup (Phase 1)**:
```
T002 [P] Create data models (internal/db/models/table.go)
T003 [P] Create queries file (internal/db/queries/tables.go)
T004 [P] Add FormatBytes helper
```

**Within Foundational (Phase 2)**:
```
T007 [P] GetIndexesWithStats query
T008 [P] GetPartitionHierarchy query
```

**Across User Stories (after Phase 2)**:
- US1 and US2 can proceed in parallel (both are P1, no interdependencies)
- US3, US4, US5, US6 can proceed in parallel after US1/US2 foundations
- US7 can proceed independently after foundational

---

## Parallel Example: Foundational Phase

```bash
# After T005-T006 complete, launch these in parallel:
Task: T007 "GetIndexesWithStats query in internal/db/queries/tables.go"
Task: T008 "GetPartitionHierarchy query in internal/db/queries/tables.go"
```

## Parallel Example: Setup Phase

```bash
# Launch all setup tasks together:
Task: T002 "Create data models in internal/db/models/table.go"
Task: T003 "Create queries file in internal/db/queries/tables.go"
Task: T004 "Add FormatBytes helper to internal/db/models/table.go"
```

---

## Implementation Strategy

### MVP First (User Stories 1-2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (Browse Schema Hierarchy)
4. Complete Phase 4: User Story 2 (Table Statistics)
5. **STOP and VALIDATE**: Test basic browsing and statistics
6. Deploy/demo if ready - this is a functional MVP

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 + US2 → Basic browsing and stats (MVP!)
3. Add US3 → Index usage statistics
4. Add US4 + US5 → Bloat detection with auto-install
5. Add US6 → Table details panel
6. Add US7 → Maintenance operations
7. Each story adds value without breaking previous stories

### Recommended Execution Order

1. T001-T004 (Setup) - ~30 min
2. T005-T012 (Foundational) - ~2 hrs
3. T013-T022 (US1: Browse) - ~2 hrs
4. T023-T027 (US2: Stats) - ~1 hr
5. **MVP CHECKPOINT** - Test and validate
6. T028-T032 (US3: Indexes) - ~1 hr
7. T033-T039 (US4: Bloat) - ~1 hr
8. T040-T047 (US5: Install) - ~1 hr
9. T048-T054 (US6: Details) - ~1.5 hrs
10. T055-T062 (US7: Maintenance) - ~1.5 hrs
11. T063-T070 (Polish) - ~1 hr

---

## Summary

| Phase | Story | Task Count | Parallel Tasks |
|-------|-------|------------|----------------|
| Setup | - | 4 | 3 |
| Foundational | - | 8 | 2 |
| US1 (P1) | Browse Schema | 10 | 0 |
| US2 (P1) | Table Stats | 5 | 0 |
| US3 (P2) | Index Stats | 5 | 0 |
| US4 (P2) | Bloat Estimates | 7 | 0 |
| US5 (P2) | Auto-Install | 8 | 0 |
| US6 (P2) | Table Details | 7 | 0 |
| US7 (P3) | Maintenance | 8 | 0 |
| Polish | - | 8 | 3 |
| **Total** | | **70** | **8** |

**MVP Scope**: Setup + Foundational + US1 + US2 = 27 tasks
**Full Feature**: 70 tasks
