# Tasks: Foundation Infrastructure

**Input**: Design documents from `/specs/001-foundation/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/config-schema.yaml

**Tests**: Tests are NOT explicitly requested in the specification, so test tasks are omitted. Manual testing will be performed per quickstart.md validation checklist.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

Project follows Go standard layout:
- `cmd/steep/` - Application entry point
- `internal/` - Private application code
- `configs/` - Configuration files
- `tests/unit/` and `tests/integration/` - Test files

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Initialize Go module with go mod init github.com/willibrandon/steep
- [X] T002 [P] Install Bubbletea dependencies: bubbletea v0.25+, bubbles v0.18+, lipgloss v0.9+
- [X] T003 [P] Install database dependencies: pgx/v5, pgxpool/v5
- [X] T004 [P] Install configuration dependency: viper v1.18+
- [X] T005 [P] Create directory structure: cmd/steep, internal/{app,config,db/models,ui/{components,views,styles}}, configs, tests/{integration,unit}
- [X] T006 Create .gitignore for Go project (binaries, test artifacts, IDE files)
- [X] T007 Create Makefile with targets: build, test, test-coverage, clean, run

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T008 [P] Define Config struct in internal/config/config.go with ConnectionConfig and UIConfig fields
- [X] T009 [P] Define default configuration values in internal/config/defaults.go
- [X] T010 [P] Implement Viper configuration loading with YAML and environment variable support in internal/config/config.go
- [X] T011 [P] Define centralized Lipgloss theme (colors, spacing, borders) in internal/ui/styles/theme.go
- [X] T012 [P] Define keyboard binding constants in internal/ui/keys.go
- [X] T013 Create example configuration file in configs/config.yaml.example with all fields documented
- [X] T014 Implement configuration validation logic (port range, pool settings, SSL mode) in internal/config/config.go

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Launch Application and Connect to Database (Priority: P1) 🎯 MVP

**Goal**: Enable DBA to launch Steep from command line and establish PostgreSQL connection

**Independent Test**: Launch application with valid credentials and verify connection is displayed in status bar

### Implementation for User Story 1

- [X] T015 [P] [US1] Create ConnectionProfile model in internal/db/models/profile.go with host, port, database, user fields
- [X] T016 [P] [US1] Implement password command execution with 5-second timeout in internal/db/password.go
- [X] T017 [US1] Implement pgxpool connection setup with pool configuration in internal/db/connection.go
- [X] T018 [US1] Implement connection validation query (SELECT version()) in internal/db/connection.go
- [X] T019 [US1] Implement PGPASSWORD and password_command fallback logic in internal/db/password.go
- [X] T020 [US1] Implement interactive password prompt with hidden input in internal/db/password.go
- [X] T021 [US1] Create DatabaseConnectedMsg and ConnectionFailedMsg message types in internal/app/messages.go
- [X] T022 [US1] Implement main Bubbletea Model struct with config, dbPool, connection state in internal/app/app.go
- [X] T023 [US1] Implement Model.Init() to trigger config loading and database connection in internal/app/app.go
- [X] T024 [US1] Implement Model.Update() to handle DatabaseConnectedMsg and ConnectionFailedMsg in internal/app/app.go
- [X] T025 [US1] Implement actionable error message formatting for connection failures in internal/app/errors.go
- [X] T026 [US1] Create main.go entry point that initializes Bubbletea program in cmd/steep/main.go
- [X] T027 [US1] Implement graceful shutdown with pool cleanup on quit in internal/app/app.go

**Checkpoint**: At this point, User Story 1 should be fully functional - application launches and connects to database

---

## Phase 4: User Story 2 - Navigate Using Keyboard (Priority: P1)

**Goal**: Enable DBA to navigate application using only keyboard shortcuts (q, h, Esc, Tab)

**Independent Test**: Launch application and use keyboard shortcuts without mouse interaction

### Implementation for User Story 2

- [X] T028 [P] [US2] Create HelpText component with keyboard shortcuts display in internal/ui/components/help.go
- [X] T029 [P] [US2] Define help screen content with all available shortcuts in internal/ui/components/help.go
- [X] T030 [US2] Add helpVisible bool field to Model struct in internal/app/app.go
- [X] T031 [US2] Implement keyboard handler for 'q' (quit) in Model.Update() in internal/app/app.go
- [X] T032 [US2] Implement keyboard handler for 'h' and '?' (toggle help) in Model.Update() in internal/app/app.go
- [X] T033 [US2] Implement keyboard handler for 'Esc' (close help dialog) in Model.Update() in internal/app/app.go
- [X] T034 [US2] Implement help overlay rendering in Model.View() in internal/app/app.go
- [X] T035 [US2] Style help dialog with Lipgloss (borders, padding, colors) in internal/ui/components/help.go

**Checkpoint**: At this point, User Stories 1 AND 2 should both work - keyboard navigation is fully functional

---

## Phase 5: User Story 3 - View Connection Status and Basic Metrics (Priority: P1)

**Goal**: Enable DBA to see real-time connection status and basic database information in status bar

**Independent Test**: Connect to database and verify status bar displays connection state, database name, timestamp with auto-refresh

### Implementation for User Story 3

- [ ] T036 [P] [US3] Define StatusBarData struct with connected, database, timestamp, activeConnections fields in internal/app/app.go
- [ ] T037 [P] [US3] Create DatabaseMetrics struct in internal/db/metrics.go
- [ ] T038 [P] [US3] Create StatusBar component with connection state indicator in internal/ui/components/statusbar.go
- [ ] T039 [US3] Implement query for active connections (SELECT count(*) FROM pg_stat_activity WHERE state = 'active') in internal/db/metrics.go
- [ ] T040 [US3] Create StatusBarTickMsg message type with data field in internal/app/messages.go
- [ ] T041 [US3] Implement ticker Cmd that fires every 1 second (configurable via ui.refresh_interval) in internal/app/app.go
- [ ] T042 [US3] Implement Model.Update() handler for StatusBarTickMsg that queries metrics in internal/app/app.go
- [ ] T043 [US3] Implement StatusBar.View() with Lipgloss styling (colors for connected/disconnected) in internal/ui/components/statusbar.go
- [ ] T044 [US3] Integrate StatusBar into Model.View() layout in internal/app/app.go
- [ ] T045 [US3] Implement connection loss detection and status bar update in internal/app/app.go

**Checkpoint**: At this point, User Stories 1, 2, AND 3 should all work - real-time status monitoring is functional

---

## Phase 6: User Story 4 - Switch Between Views (Priority: P2)

**Goal**: Enable DBA to switch between different monitoring views using keyboard shortcuts (1-9, Tab)

**Independent Test**: Use number keys or Tab to switch between available views (placeholders)

### Implementation for User Story 4

- [ ] T046 [P] [US4] Define ViewType enum (Dashboard, Activity, Queries, Locks, Tables, Replication) in internal/ui/views/types.go
- [ ] T047 [P] [US4] Define ViewModel interface with Init(), Update(), View(), SetSize() methods in internal/ui/views/types.go
- [ ] T048 [P] [US4] Create Table component for displaying tabular data with column headers in internal/ui/components/table.go
- [ ] T049 [US4] Implement placeholder DashboardView with ViewModel interface in internal/ui/views/dashboard.go
- [ ] T050 [US4] Add currentView ViewType and views map[ViewType]ViewModel to Model struct in internal/app/app.go
- [ ] T051 [US4] Initialize views map with DashboardView in Model.Init() in internal/app/app.go
- [ ] T052 [US4] Implement keyboard handler for number keys '1'-'9' (jump to view) in Model.Update() in internal/app/app.go
- [ ] T053 [US4] Implement keyboard handler for 'Tab' (cycle next view) in Model.Update() in internal/app/app.go
- [ ] T054 [US4] Implement keyboard handler for 'Shift+Tab' (cycle previous view) in Model.Update() in internal/app/app.go
- [ ] T055 [US4] Implement view rendering dispatch in Model.View() in internal/app/app.go
- [ ] T056 [US4] Handle terminal resize events (tea.WindowSizeMsg) and propagate to views in internal/app/app.go

**Checkpoint**: All P1 and P2 user stories are complete - view switching framework is established

---

## Phase 7: Error Handling & Reconnection (Cross-Cutting)

**Purpose**: Implement automatic reconnection with exponential backoff (addresses edge case from spec.md)

- [ ] T057 [P] Define ReconnectionState struct with attempt, lastAttempt, nextDelay, maxAttempts in internal/db/reconnection.go
- [ ] T058 [P] Create ReconnectAttemptMsg message type in internal/app/messages.go
- [ ] T059 Implement exponential backoff calculation (1s, 2s, 4s, 8s, 16s capped at 30s) in internal/db/reconnection.go
- [ ] T060 Implement reconnection logic with max 5 attempts in internal/db/reconnection.go
- [ ] T061 Add reconnectionState field to Model struct in internal/app/app.go
- [ ] T062 Implement Model.Update() handler for ReconnectAttemptMsg in internal/app/app.go
- [ ] T063 Implement connection loss detection and reconnection trigger in internal/app/app.go
- [ ] T064 Display reconnection status in status bar or error overlay in internal/ui/components/statusbar.go
- [ ] T065 Implement permanent failure message after max attempts exhausted in internal/app/errors.go

---

## Phase 8: Logging & Terminal Handling (Cross-Cutting)

**Purpose**: Implement logging and terminal resize handling (addresses FR-021 through FR-024, FR-013)

- [ ] T066 [P] Configure slog with Error and Warning to stderr by default in cmd/steep/main.go
- [ ] T067 [P] Implement debug flag (--debug or STEEP_DEBUG=1) to enable Info level logging in cmd/steep/main.go
- [ ] T068 Add logging for critical operations (connection, reconnection, errors) in internal/db/connection.go
- [ ] T069 Implement minimum terminal size validation (80x24) on startup in internal/app/app.go
- [ ] T070 Display warning if terminal is too small in internal/app/app.go

---

## Phase 9: SSL/TLS Support & Environment Variables (Cross-Cutting)

**Purpose**: Complete SSL support and environment variable precedence (FR-015, FR-018)

- [ ] T071 [P] Implement SSL mode configuration (disable, prefer, require) in internal/db/connection.go
- [ ] T072 [P] Implement environment variable precedence: STEEP_* > PG* > Config > Defaults in internal/config/config.go
- [ ] T073 Add SSL certificate validation and error handling in internal/db/connection.go
- [ ] T074 Test connection with SSL required and SSL disabled modes in tests/integration/connection_test.go

---

## Phase 10: Polish & Documentation

**Purpose**: Finalize foundation for production use

- [ ] T075 [P] Create README.md with project description, installation, and usage instructions
- [ ] T076 [P] Document all keyboard shortcuts in README.md
- [ ] T077 [P] Add configuration examples for common use cases (local, remote, SSL) in README.md
- [ ] T078 Update configs/steep.yaml.example with comprehensive comments
- [ ] T079 Verify all FR requirements from spec.md are implemented
- [ ] T080 Run quickstart.md testing checklist and validate all criteria pass
- [ ] T081 Verify success criteria SC-001 through SC-010 from spec.md
- [ ] T082 [P] Add code comments for exported functions and types
- [ ] T083 Create CONTRIBUTING.md with development setup instructions

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phases 3-6)**: All depend on Foundational phase completion
  - User Story 1 (P1): Can start after Foundational - No dependencies on other stories
  - User Story 2 (P1): Can start after Foundational - No dependencies on other stories
  - User Story 3 (P1): Depends on User Story 1 (needs database connection)
  - User Story 4 (P2): Can start after Foundational - No dependencies on other stories (uses placeholders)
- **Error Handling (Phase 7)**: Depends on User Story 1 and 3 (connection and status bar)
- **Logging (Phase 8)**: Can start after Foundational
- **SSL/TLS (Phase 9)**: Depends on User Story 1 (connection setup)
- **Polish (Phase 10)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Foundation only - independently testable ✅
- **User Story 2 (P1)**: Foundation only - independently testable ✅
- **User Story 3 (P1)**: Requires US1 (database connection) - testable after US1 complete
- **User Story 4 (P2)**: Foundation only - independently testable ✅ (uses placeholder views)

### Within Each User Story

- Models before services
- Services before main application integration
- Message types before handlers
- UI components before integration into views
- Core implementation before error handling

### Parallel Opportunities

**Phase 1 (Setup)**: T002, T003, T004 can run in parallel (different dependency installs), T005 can run in parallel with installs

**Phase 2 (Foundational)**: T008, T009, T010 can run in parallel (different files), T011, T012, T013 can run in parallel

**Phase 3 (User Story 1)**: T015, T016 can run in parallel (different concerns)

**Phase 4 (User Story 2)**: T028, T029 can run in parallel (help component)

**Phase 5 (User Story 3)**: T036, T037, T038 can run in parallel (data structures and component)

**Phase 6 (User Story 4)**: T046, T047, T048 can run in parallel (type definitions and components)

**Phase 7 (Reconnection)**: T057, T058 can run in parallel (data structure and message type)

**Phase 8 (Logging)**: T066, T067 can run in parallel (different setup tasks)

**Phase 9 (SSL)**: T071, T072 can run in parallel (SSL vs env vars)

**Phase 10 (Polish)**: T075, T076, T077, T082 can run in parallel (different documentation files)

---

## Parallel Example: User Story 1

```bash
# Launch model and password handling in parallel:
Task T015: "Create ConnectionProfile model in internal/db/models/profile.go"
Task T016: "Implement password command execution with 5-second timeout in internal/db/password.go"

# Both work on different files with no dependencies
```

---

## Implementation Strategy

### MVP First (User Stories 1, 2, 3 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (Connection)
4. Complete Phase 4: User Story 2 (Keyboard Navigation)
5. Complete Phase 5: User Story 3 (Status Bar)
6. **STOP and VALIDATE**: Test all P1 user stories independently
7. This delivers core value: connect, navigate, monitor status

### Full Foundation (All User Stories + Cross-Cutting)

1. Complete MVP (Phases 1-5)
2. Add Phase 6: User Story 4 (View Switching)
3. Add Phase 7: Error Handling & Reconnection
4. Add Phase 8: Logging & Terminal Handling
5. Add Phase 9: SSL/TLS Support
6. Complete Phase 10: Polish & Documentation
7. **VALIDATE**: Run full quickstart.md checklist

### Parallel Team Strategy

With multiple developers after Foundational phase complete:

1. **Developer A**: User Story 1 (Phase 3) - Connection
2. **Developer B**: User Story 2 (Phase 4) - Keyboard Navigation
3. **Developer C**: User Story 4 (Phase 6) - View Switching (can work in parallel, uses placeholders)

After P1 stories (1, 2, 3) integrate:

4. **Developer A**: User Story 3 (Phase 5) - Status Bar (needs connection from US1)
5. **Developer B**: Phase 7 - Reconnection Logic
6. **Developer C**: Phase 8 - Logging

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each P1 user story should be independently completable and testable
- User Story 3 has soft dependency on User Story 1 (needs connection)
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Manual testing per quickstart.md checklist (no automated tests in this phase)
- All file paths follow Go standard layout per plan.md project structure
