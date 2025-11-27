# Tasks: Configuration Viewer

**Input**: Design documents from `/specs/008-configuration-viewer/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md

**Tests**: Test tasks included per standard Steep development workflow.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- Models: `internal/db/models/`
- Queries: `internal/db/queries/`
- Views: `internal/ui/views/config/`
- Tests: `tests/integration/`, `tests/unit/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create package structure and shared types for Configuration Viewer

- [x] T001 Create config view package directory at internal/ui/views/config/
- [x] T002 [P] Add ViewConfig to ViewType enum in internal/ui/views/types.go
- [x] T003 [P] Add ViewConfig String() and ShortName() cases returning "Configuration" and "cfg" in internal/ui/views/types.go

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T004 Create Parameter struct with all 17 fields from pg_settings in internal/db/models/config.go
- [x] T005 Add Parameter helper methods (IsModified, RequiresRestart, RequiresReload, CanUserChange, TopLevelCategory) in internal/db/models/config.go
- [x] T006 Create ConfigData struct with Parameters slice, ModifiedCount, PendingRestartCount, Categories in internal/db/models/config.go
- [x] T007 Add NewConfigData() constructor and FilterByCategory(), FilterBySearch() methods in internal/db/models/config.go
- [x] T008 Create GetAllParameters query function in internal/db/queries/config.go with SQL from data-model.md
- [x] T009 Create ConfigDataMsg and RefreshConfigMsg message types in internal/ui/views/config/view.go

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - View All Server Configuration Parameters (Priority: P1) 🎯 MVP

**Goal**: Display all PostgreSQL configuration parameters in a sortable table with yellow highlighting for modified parameters and auto-refresh every 60 seconds

**Independent Test**: Open Configuration view with `8` key, verify all ~350 parameters display with Name, Value, Unit, Category, Description columns. Modified parameters (setting != boot_val) highlighted yellow. Sort by pressing `s` key.

### Tests for User Story 1

- [x] T010 [P] [US1] Unit test for Parameter.IsModified() in tests/unit/config_model_test.go
- [x] T011 [P] [US1] Unit test for Parameter helper methods (RequiresRestart, TopLevelCategory) in tests/unit/config_model_test.go
- [x] T012 [P] [US1] Integration test for GetAllParameters query in tests/integration/config_test.go

### Implementation for User Story 1

- [x] T013 [US1] Create ConfigView struct with width, height, mode, data, selectedIdx, scrollOffset, sortColumn, sortAsc, lastUpdate, refreshing, err fields in internal/ui/views/config/view.go
- [x] T014 [US1] Implement NewConfigView() constructor in internal/ui/views/config/view.go
- [x] T015 [US1] Implement Init() returning initial refresh command in internal/ui/views/config/view.go
- [x] T016 [US1] Implement SetSize(width, height int) method in internal/ui/views/config/view.go
- [x] T017 [US1] Create SortColumn enum (SortByName, SortByCategory) with String() method in internal/ui/views/config/view.go
- [x] T018 [US1] Implement sortParameters() helper for sorting by name or category in internal/ui/views/config/view.go
- [x] T019 [US1] Implement renderTable() for main parameter table with columns: Name, Value, Unit, Category, Description in internal/ui/views/config/view.go
- [x] T020 [US1] Add yellow highlighting style for modified parameters (where IsModified() returns true) in renderTable()
- [x] T021 [US1] Implement View() composing header, table, and footer in internal/ui/views/config/view.go
- [x] T022 [US1] Implement Update() handling ConfigDataMsg, tea.KeyMsg for navigation (j/k/↑/↓/g/G) in internal/ui/views/config/view.go
- [x] T023 [US1] Add sort key handling (`s` to cycle column, `S` to toggle direction) in Update()
- [x] T024 [US1] Create config monitor with 60-second refresh interval in internal/monitors/config.go
- [x] T025 [US1] Register ConfigView and config monitor in internal/app/app.go
- [x] T026 [US1] Add `8` key binding to switch to Configuration view in internal/app/app.go

**Checkpoint**: User Story 1 complete - can browse all parameters, sort, see modified highlighted yellow

---

## Phase 4: User Story 2 - Search and Filter Configuration Parameters (Priority: P2)

**Goal**: Enable search by name/description with `/` key and category filtering with `c` key

**Independent Test**: Press `/` and type "shared", verify only parameters containing "shared" display. Press `c`, select "Resource Usage", verify only Resource Usage parameters show. Press Escape to clear filters.

### Tests for User Story 2

- [x] T027 [P] [US2] Unit test for ConfigData.FilterBySearch() in tests/unit/config_model_test.go
- [x] T028 [P] [US2] Unit test for ConfigData.FilterByCategory() in tests/unit/config_model_test.go

### Implementation for User Story 2

- [x] T029 [US2] Add ConfigMode enum (ModeNormal, ModeSearch, ModeCategoryFilter) to ConfigView in internal/ui/views/config/view.go
- [x] T030 [US2] Add searchInput, filterActive, categoryFilter fields to ConfigView struct in internal/ui/views/config/view.go
- [x] T031 [US2] Implement search mode: enter on `/`, capture input, filter on Enter, exit on Escape in internal/ui/views/config/view.go
- [x] T032 [US2] Implement renderSearchInput() showing search prompt and current input in internal/ui/views/config/view.go
- [x] T033 [US2] Implement category filter mode: show dropdown on `c` key with unique top-level categories in internal/ui/views/config/view.go
- [x] T034 [US2] Implement renderCategoryFilter() showing selectable category list in internal/ui/views/config/view.go
- [x] T035 [US2] Apply filters to displayed parameters using ConfigData.FilterBySearch() and FilterByCategory() in renderTable()
- [x] T036 [US2] Show "No results found" message when filters match zero parameters in renderTable()
- [x] T037 [US2] Add filter status indicator in header (showing active search/category filter) in View()

**Checkpoint**: User Story 2 complete - can search and filter parameters

---

## Phase 5: User Story 3 - View Parameter Details and Context Help (Priority: P3)

**Goal**: Show detailed parameter information in modal when `d` is pressed, including full description, context, constraints, and source

**Independent Test**: Select "max_connections", press `d`, verify detail view shows: name, value, unit, type, context with explanation ("Restart Required"), range (1-262143), default value, source file location. Press Escape to return.

### Implementation for User Story 3

- [x] T038 [US3] Add ModeDetail to ConfigMode enum in internal/ui/views/config/view.go
- [x] T039 [US3] Add detailScrollOffset field for scrolling long descriptions in ConfigView struct
- [x] T040 [US3] Implement renderDetailView() showing all parameter fields with formatted layout per research.md mockup in internal/ui/views/config/view.go
- [x] T041 [US3] Add context explanation mapping (postmaster="Restart Required", sighup="Reload Required", etc.) in renderDetailView()
- [x] T042 [US3] Format constraints display: show min/max for numeric, enumvals for enum, "N/A" for string types in renderDetailView()
- [x] T043 [US3] Show source file and line number when sourcefile is non-empty in renderDetailView()
- [x] T044 [US3] Handle `d` key to enter detail mode, Escape/q to return to normal mode in Update()
- [x] T045 [US3] Add scroll handling (j/k) within detail view for long descriptions in Update()

**Checkpoint**: User Story 3 complete - can view full parameter details

---

## Phase 6: User Story 4 - Export Configuration (Priority: P3)

**Goal**: Export current configuration to file via `:export config <filename>` command in PostgreSQL conf format

**Independent Test**: Type `:export config /tmp/pg_config.txt`, verify file created with all parameters in `name = 'value' # description` format. Filter to modified only, export again, verify only modified parameters in file.

### Implementation for User Story 4

- [x] T046 [US4] Add ModeCommand to ConfigMode enum for command input mode in internal/ui/views/config/view.go
- [x] T047 [US4] Add commandInput field to ConfigView struct in internal/ui/views/config/view.go
- [x] T048 [US4] Create ExportConfigResultMsg message type with Filename, Count, Success, Error fields in internal/ui/views/config/view.go
- [x] T049 [US4] Implement parseExportCommand() to parse `:export config <filename>` syntax in internal/ui/views/config/export.go
- [x] T050 [US4] Implement exportConfig(filename string, params []Parameter) writing PostgreSQL conf format in internal/ui/views/config/export.go
- [x] T051 [US4] Add header comment with timestamp, server info, parameter count in export output
- [x] T052 [US4] Handle `:` key to enter command mode, capture input, execute on Enter in Update()
- [x] T053 [US4] Add toast message for export success/failure in Update() handling ExportConfigResultMsg
- [x] T054 [US4] Export only currently filtered parameters (respect active search/category filter) in exportConfig()

**Checkpoint**: User Story 4 complete - can export configuration to file

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Help panel, error handling, and final polish

- [x] T055 [P] Create help panel content with all keybindings in internal/ui/views/config/help.go (implemented inline in view.go)
- [x] T056 [P] Add ModeHelp to ConfigMode enum and `h` key handler in internal/ui/views/config/view.go
- [x] T057 [P] Implement renderHelp() showing keyboard shortcuts in internal/ui/views/config/view.go
- [x] T058 Add connection error handling in config monitor with error message display in internal/monitors/config.go
- [x] T059 Add pending_restart warning indicator (red highlight with "!" prefix) for parameters with pending_restart=true
- [x] T060 Add clipboard copy for parameter name (`y`) and value (`Y`) using existing clipboard utility in internal/ui/views/config/view.go
- [x] T061 Run quickstart.md validation checklist and fix any issues
- [x] T062 Verify 80x24 minimum terminal size rendering (responsive column layout)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion
  - US1 must complete first (US2-4 depend on base view)
  - US2, US3, US4 can proceed in parallel after US1
- **Polish (Phase 7)**: Depends on US1 being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Depends on US1 base view being complete
- **User Story 3 (P3)**: Depends on US1 base view being complete
- **User Story 4 (P3)**: Depends on US1 base view being complete, benefits from US2 filters

### Within Each User Story

- Tests written first (where applicable)
- Models and queries before view logic
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

**Phase 1 (Setup):**
- T002 and T003 can run in parallel

**Phase 2 (Foundational):**
- T004-T007 (models) can run in parallel with T008 (queries)

**Phase 3 (US1 - Tests):**
- T010, T011, T012 can all run in parallel

**Phase 4 (US2 - Tests):**
- T027 and T028 can run in parallel

**Phase 7 (Polish):**
- T055, T056, T057 can run in parallel

---

## Parallel Example: User Story 1 Tests

```bash
# Launch all US1 tests together:
Task: "Unit test for Parameter.IsModified() in tests/unit/config_model_test.go"
Task: "Unit test for Parameter helper methods in tests/unit/config_model_test.go"
Task: "Integration test for GetAllParameters in tests/integration/config_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
   - Press `8` to open Configuration view
   - Verify all ~350 parameters display
   - Verify modified parameters highlighted yellow
   - Verify sorting works
   - Verify 60-second auto-refresh works
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo (Search & Filter)
4. Add User Story 3 → Test independently → Deploy/Demo (Details)
5. Add User Story 4 → Test independently → Deploy/Demo (Export)
6. Each story adds value without breaking previous stories

### Suggested MVP Scope

**MVP = User Story 1 only:**
- Browse all parameters
- Sort by name/category
- Yellow highlighting for modified
- Auto-refresh every 60 seconds

This delivers immediate value for DBAs who need configuration visibility.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing (TDD)
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Follow existing Steep view patterns (locks, tables, queries)
