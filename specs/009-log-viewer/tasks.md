# Tasks: Log Viewer

**Input**: Design documents from `/specs/009-log-viewer/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md

**Tests**: Not explicitly requested - tests not included. Add if needed.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

- **Project type**: Single Go module (existing Steep application)
- **Base paths**: `internal/ui/views/logs/`, `internal/db/models/`, `internal/monitors/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create directory structure and base files for log viewer

- [X] T001 Create logs view directory structure at `internal/ui/views/logs/`
- [X] T002 [P] Create LogEntry model and LogSeverity enum in `internal/db/models/log_entry.go`
- [X] T003 [P] Create LogSource model with LogFormat and AccessMethod enums in `internal/monitors/log_source.go`
- [X] T004 [P] Define severity color styles in `internal/ui/styles/logs.go` (Red ERROR, Yellow WARNING, White INFO, Gray DEBUG)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Create LogBuffer ring buffer (10,000 capacity) with Add, GetRange, GetAll, Clear, Len in `internal/ui/views/logs/buffer.go`
- [X] T006 Create LogFilter struct with Matches, SetLevel, SetSearch, Clear methods in `internal/ui/views/logs/filter.go`
- [X] T007 Extend existing log parsers to return LogEntry instead of QueryEvent (adapter pattern) in `internal/monitors/log_entry_parser.go`
- [X] T008 Create LogsView base struct implementing ViewModel interface in `internal/ui/views/logs/view.go`
- [X] T009 Register LogsView with `9` key in main app router in `internal/ui/app.go`
- [X] T010 Add help text skeleton in `internal/ui/views/logs/help.go`

**Checkpoint**: Foundation ready - view accessible via `9` key, shows placeholder

---

## Phase 3: User Story 1 - Real-Time Log Monitoring (Priority: P1) 🎯 MVP

**Goal**: Display PostgreSQL server logs in real-time with follow mode (auto-scroll to newest)

**Independent Test**: Open log viewer with `9`, verify logs appear and new entries show within 1 second

### Implementation for User Story 1

- [X] T011 [US1] Implement CheckLoggingStatus call on view Init in `internal/ui/views/logs/view.go`
- [X] T012 [US1] Implement ModeConfirmEnableLogging dialog (reuse pattern from queries view) in `internal/ui/views/logs/view.go`
- [X] T013 [US1] Implement log file discovery using LogSource configuration in `internal/ui/views/logs/view.go`
- [X] T014 [US1] Implement log file reading with position tracking in `internal/ui/views/logs/collector.go`
- [X] T015 [US1] Implement stderr format parsing (regex-based) in `internal/ui/views/logs/collector.go`
- [X] T016 [P] [US1] Implement CSV format parsing (reuse csv_log_parser column indices) in `internal/ui/views/logs/collector.go`
- [X] T017 [P] [US1] Implement JSON format parsing (reuse JSONLogEntry struct) in `internal/ui/views/logs/collector.go`
- [X] T018 [US1] Implement format auto-detection based on file extension and content in `internal/ui/views/logs/collector.go`
- [X] T019 [US1] Implement 1-second refresh ticker for follow mode in `internal/ui/views/logs/view.go`
- [X] T020 [US1] Implement follow mode toggle with `f` key in `internal/ui/views/logs/keys.go`
- [X] T021 [US1] Implement viewport rendering with severity color-coding in `internal/ui/views/logs/render.go`
- [X] T022 [US1] Implement status bar showing follow mode, entry count, last update in `internal/ui/views/logs/render.go`
- [X] T023 [US1] Implement basic navigation (j/k, g/G, Ctrl+d/u) in `internal/ui/views/logs/keys.go`
- [X] T024 [US1] Implement error display with configuration guidance when logs inaccessible in `internal/ui/views/logs/render.go`
- [X] T025 [US1] Handle log rotation detection (file size < last position) in `internal/ui/views/logs/collector.go`
- [X] T026 [US1] Update help text with P1 keybindings in `internal/ui/views/logs/help.go`

**Checkpoint**: User Story 1 complete - real-time log viewing functional with follow mode

---

## Phase 4: User Story 2 - Filter by Severity Level (Priority: P2)

**Goal**: Filter logs by severity level using `:level` command, color-coded display

**Independent Test**: Enter `:level error`, verify only ERROR entries shown; `:level all` shows all

### Implementation for User Story 2

- [ ] T027 [US2] Implement ModeCommand for `:` command input in `internal/ui/views/logs/view.go`
- [ ] T028 [US2] Implement command parsing for `:level <level>` in `internal/ui/views/logs/commands.go`
- [ ] T029 [US2] Implement `:level clear` and `:level all` to reset filter in `internal/ui/views/logs/commands.go`
- [ ] T030 [US2] Apply LogFilter.Severity to viewport content generation in `internal/ui/views/logs/render.go`
- [ ] T031 [US2] Show active filter in status bar `[level:error]` in `internal/ui/views/logs/render.go`
- [ ] T032 [US2] Ensure filtered entries update in follow mode in `internal/ui/views/logs/view.go`
- [ ] T033 [US2] Update help text with `:level` command in `internal/ui/views/logs/help.go`

**Checkpoint**: User Story 2 complete - severity filtering works independently

---

## Phase 5: User Story 3 - Search by Text Pattern (Priority: P2)

**Goal**: Search logs with regex pattern using `/`, navigate matches with `n`/`N`

**Independent Test**: Press `/`, enter pattern, verify matches highlighted; `n`/`N` navigates

### Implementation for User Story 3

- [ ] T034 [US3] Implement ModeSearch for `/` search input in `internal/ui/views/logs/view.go`
- [ ] T035 [US3] Implement regex compilation with error handling in `internal/ui/views/logs/filter.go`
- [ ] T036 [US3] Implement match highlighting in viewport content in `internal/ui/views/logs/render.go`
- [ ] T037 [US3] Implement `n` key to navigate to next match in `internal/ui/views/logs/keys.go`
- [ ] T038 [US3] Implement `N` key to navigate to previous match in `internal/ui/views/logs/keys.go`
- [ ] T039 [US3] Implement `Escape` to clear search in `internal/ui/views/logs/keys.go`
- [ ] T040 [US3] Show active search in status bar `[search:/pattern/]` in `internal/ui/views/logs/render.go`
- [ ] T041 [US3] Update help text with search keybindings in `internal/ui/views/logs/help.go`

**Checkpoint**: User Story 3 complete - search with regex works independently

---

## Phase 6: User Story 4 - Navigate by Timestamp (Priority: P3)

**Goal**: Jump to specific timestamp with `:goto`, use `g`/`G` for oldest/newest

**Independent Test**: Enter `:goto 2025-11-27 14:30`, verify view scrolls to that time

### Implementation for User Story 4

- [ ] T042 [US4] Implement command parsing for `:goto <timestamp>` in `internal/ui/views/logs/commands.go`
- [ ] T043 [US4] Implement timestamp parsing (support multiple formats) in `internal/ui/views/logs/commands.go`
- [ ] T044 [US4] Implement binary search to find nearest log entry by timestamp in `internal/ui/views/logs/buffer.go`
- [ ] T045 [US4] Implement scroll to timestamp position in viewport in `internal/ui/views/logs/view.go`
- [ ] T046 [US4] Show message when no logs at requested timestamp in `internal/ui/views/logs/view.go`
- [ ] T047 [US4] Ensure `g` goes to oldest and `G` goes to newest in `internal/ui/views/logs/keys.go`
- [ ] T048 [US4] Update help text with `:goto` command in `internal/ui/views/logs/help.go`

**Checkpoint**: User Story 4 complete - timestamp navigation works independently

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T049 [P] Handle readonly mode - disable enable logging prompt in `internal/ui/views/logs/view.go`
- [ ] T050 [P] Implement multi-line log entry display (stack traces, DETAIL) in `internal/ui/views/logs/render.go`
- [ ] T051 [P] Add mouse scroll support (wheel up/down) in `internal/ui/views/logs/view.go`
- [ ] T052 Complete help overlay with all keybindings and commands in `internal/ui/views/logs/help.go`
- [ ] T053 Run quickstart.md validation - verify all documented commands work
- [ ] T054 Performance validation - verify 10,000 entry scrolling is smooth

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion
  - User stories can proceed in parallel if desired
  - Recommended: sequential by priority (P1 → P2 → P3)
- **Polish (Phase 7)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational - Independent of US1 (uses shared filter infrastructure)
- **User Story 3 (P2)**: Can start after Foundational - Independent of US1/US2 (uses shared filter infrastructure)
- **User Story 4 (P3)**: Can start after Foundational - Depends on buffer having entries (US1)

### Within Each User Story

- Models before services
- Core implementation before UI
- Story complete before moving to next priority

### Parallel Opportunities

**Phase 1 (Setup)**:
- T002, T003, T004 can run in parallel (different files)

**Phase 3 (US1)**:
- T016, T017 can run in parallel (CSV and JSON parsers)

**Phase 7 (Polish)**:
- T049, T050, T051 can run in parallel (different concerns)

---

## Parallel Example: Phase 1 Setup

```bash
# Launch all setup tasks together:
Task: "Create LogEntry model and LogSeverity enum in internal/db/models/log_entry.go"
Task: "Create LogSource model with LogFormat and AccessMethod enums in internal/monitors/log_source.go"
Task: "Define severity color styles in internal/ui/styles/logs.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001-T004)
2. Complete Phase 2: Foundational (T005-T010)
3. Complete Phase 3: User Story 1 (T011-T026)
4. **STOP and VALIDATE**: Test real-time log viewing independently
5. Deploy/demo if ready - basic log monitoring functional

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy (MVP!)
3. Add User Story 2 → Test independently → Severity filtering added
4. Add User Story 3 → Test independently → Search capability added
5. Add User Story 4 → Test independently → Timestamp navigation added
6. Each story adds value without breaking previous stories

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Constitution VI (Visual Design First): Consider adding ASCII mockup task before T021 if not done
