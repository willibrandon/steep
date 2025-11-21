# Tasks: Dashboard & Activity Monitoring

**Feature Branch**: `002-dashboard-activity`
**Generated**: 2025-11-21
**Total Tasks**: 81

## Phase 1: Setup

Project initialization and directory structure.

- [x] T001 Create directory structure for internal/db/queries/ and internal/db/models/
- [x] T002 Create directory structure for internal/monitors/
- [x] T003 Create directory structure for internal/ui/components/, internal/ui/views/, and internal/ui/styles/
- [x] T004 Create directory structure for tests/unit/ and tests/integration/
- [x] T005 [P] Add bubbletea dependency in go.mod
- [x] T006 [P] Add bubbles (table, viewport) dependency in go.mod
- [x] T007 [P] Add lipgloss dependency in go.mod
- [x] T008 [P] Add pgx/pgxpool dependency in go.mod
- [x] T009 Build mimo tui-driver from /Users/brandon/src/mimo (cd /Users/brandon/src/mimo && zig build)
- [x] T010 Create symlink or add tui-driver to PATH for development workflow (~/bin/tui-driver)

## Phase 2: Foundational

Blocking prerequisites for all user stories. MUST complete before user story phases.

### Visual Design First (Constitution VI Compliance)

- [x] T011 Study pg_top reference tool, take screenshots of activity view layout in specs/002-dashboard-activity/visual-design/
- [x] T012 Study htop reference tool, take screenshots of metrics panel design in specs/002-dashboard-activity/visual-design/
- [x] T013 Study k9s reference tool, take screenshots of keyboard navigation patterns in specs/002-dashboard-activity/visual-design/
- [x] T014 Create ASCII mockup of combined Dashboard/Activity view layout in specs/002-dashboard-activity/visual-design/mockup.txt
- [x] T015 Build throwaway demo 1: Basic table rendering with bubbles/table in internal/ui/demos/demo1/
- [x] T016 Use tui-driver to screenshot demo1 and save to specs/002-dashboard-activity/visual-design/demo1.txt
- [x] T017 Build throwaway demo 2: Metrics panel with lipgloss in internal/ui/demos/demo2/
- [x] T018 Use tui-driver to screenshot demo2 and save to specs/002-dashboard-activity/visual-design/demo2.txt
- [x] T019 Build throwaway demo 3: Combined layout with borders in internal/ui/demos/demo3/
- [x] T020 Use tui-driver to screenshot demo3 and save to specs/002-dashboard-activity/visual-design/demo3.txt
- [x] T021 Compare demo screenshots with reference tools, document visual acceptance criteria in specs/002-dashboard-activity/visual-design/decision.md

### Core Infrastructure

- [x] T022 [P] Define ConnectionState enum in internal/db/models/connection.go
- [x] T023 [P] Define Connection struct in internal/db/models/connection.go
- [x] T024 [P] Define Metrics and MetricsSnapshot structs in internal/db/models/metrics.go
- [x] T025 [P] Define ActivityFilter and Pagination structs in internal/db/models/filter.go
- [x] T026 [P] Define PanelStatus enum and DashboardPanel struct in internal/db/models/panel.go
- [x] T027 Define Bubbletea message types (ActivityDataMsg, MetricsDataMsg, TickMsg, etc.) in internal/ui/messages.go
- [x] T028 Define color palette for connection states in internal/ui/styles/colors.go
- [x] T029 Define common Lipgloss styles (borders, panels, tables) in internal/ui/styles/styles.go

## Phase 3: User Story 1 - View Active Connections (P1)

**Goal**: Display all active database connections with query details in a sortable, filterable table.

**Independent Test**: Connect to PostgreSQL and verify activity table displays real connection data with auto-refresh.

### Database Layer

- [x] T030 [US1] Implement GetActivityConnections query with LIMIT/OFFSET in internal/db/queries/activity.go
- [x] T031 [US1] Implement GetConnectionCount query for total count in internal/db/queries/activity.go

### Monitor Layer

- [x] T032 [US1] Implement ActivityMonitor goroutine with channel output in internal/monitors/activity.go

### UI Layer

- [x] T033 [US1] Implement activity table component using bubbles/table in internal/ui/components/activity_table.go
- [x] T034 [US1] Implement query detail viewport for 'd' key action in internal/ui/components/detail.go
- [x] T035 [US1] Implement dashboard view model with activity table section in internal/ui/views/dashboard.go
- [x] T036 [US1] Implement keyboard navigation (hjkl, g/G, s for sort, / for filter) in internal/ui/views/dashboard.go
- [x] T037 [US1] Implement connection state color-coding in table rows in internal/ui/views/dashboard.go
- [x] T038 [US1] Implement auto-refresh with tea.Tick and configurable interval in internal/ui/views/dashboard.go
- [x] T039 [US1] Implement static mockup with hardcoded data for visual approval in internal/ui/demos/demo_dashboard/

### Visual Verification (TUI Driver)

- [x] T040 [US1] Use tui-driver to screenshot activity table with hardcoded data, verify layout matches mockup
- [x] T041 [US1] Use tui-driver to test keyboard navigation (send 'j', 'k', 'g', 'G'), screenshot each state
- [x] T042 [US1] Use tui-driver to test 'd' key detail view, screenshot and verify query text display

## Phase 4: User Story 2 - View Dashboard Metrics (P1)

**Goal**: Display key database metrics (TPS, cache hit ratio, connection count, database size) in top panel.

**Independent Test**: Observe dashboard metrics update automatically at configured refresh interval.

### Database Layer

- [ ] T043 [US2] Implement GetDatabaseStats query for pg_stat_database in internal/db/queries/stats.go
- [ ] T044 [US2] Implement GetDatabaseSize query in internal/db/queries/stats.go

### Monitor Layer

- [ ] T045 [US2] Implement StatsMonitor goroutine with TPS delta calculation in internal/monitors/stats.go

### UI Layer

- [ ] T046 [US2] Implement metrics panel component with 4 panels in internal/ui/components/panel.go
- [ ] T047 [US2] Integrate metrics panel into dashboard view above activity table in internal/ui/views/dashboard.go
- [ ] T048 [US2] Implement cache hit ratio warning highlight (<90%) in internal/ui/components/panel.go
- [ ] T049 [US2] Implement TPS formatting with units (e.g., "1,234 tx/s") in internal/ui/components/panel.go
- [ ] T050 [US2] Implement database size formatting (KB/MB/GB) in internal/ui/components/panel.go

### Visual Verification (TUI Driver)

- [ ] T051 [US2] Use tui-driver to screenshot combined dashboard+activity view, verify metrics panel layout
- [ ] T052 [US2] Use tui-driver to screenshot low cache hit ratio state (<90%), verify warning highlight visible

## Phase 5: User Story 3 - Kill Problematic Connections (P2)

**Goal**: Allow DBAs to cancel queries or terminate connections with confirmation.

**Independent Test**: Select a connection, press 'x' or 'c', confirm dialog appears, action executes.

### Database Layer

- [ ] T053 [US3] Implement CancelQuery (pg_cancel_backend) in internal/db/queries/activity.go
- [ ] T054 [US3] Implement TerminateConnection (pg_terminate_backend) in internal/db/queries/activity.go

### UI Layer

- [ ] T055 [US3] Implement confirmation dialog component in internal/ui/components/dialog.go
- [ ] T056 [US3] Implement 'c' key handler for cancel query action in internal/ui/views/dashboard.go
- [ ] T057 [US3] Implement 'x' key handler for terminate connection action in internal/ui/views/dashboard.go
- [ ] T058 [US3] Implement read-only mode check blocking kill actions in internal/ui/views/dashboard.go
- [ ] T059 [US3] Implement self-connection warning when killing own PID in internal/ui/views/dashboard.go
- [ ] T060 [US3] Implement success/failure toast message after action in internal/ui/views/dashboard.go

### Visual Verification (TUI Driver)

- [ ] T061 [US3] Use tui-driver to send 'x' key, screenshot confirmation dialog, verify layout and PID display
- [ ] T062 [US3] Use tui-driver to send 'y' to confirm, screenshot success toast message
- [ ] T063 [US3] Use tui-driver to test read-only mode block, screenshot error message

## Phase 6: User Story 4 - Filter Connections by State (P2)

**Goal**: Allow DBAs to filter connections by state and database for focused investigation.

**Independent Test**: Apply state filter, verify only matching connections display.

- [ ] T064 [US4] Implement state filter logic in ActivityMonitor in internal/monitors/activity.go
- [ ] T065 [US4] Implement database filter toggle (all DBs vs current DB) in internal/ui/views/dashboard.go
- [ ] T066 [US4] Implement filter input mode with '/' key in internal/ui/views/dashboard.go
- [ ] T067 [US4] Implement filter clear action in internal/ui/views/dashboard.go
- [ ] T068 [US4] Display active filter indicator in status bar in internal/ui/views/dashboard.go

### Visual Verification (TUI Driver)

- [ ] T069 [US4] Use tui-driver to send '/' then 'active', screenshot filtered table showing only active connections
- [ ] T070 [US4] Use tui-driver to diff before/after filter screenshots, verify correct rows filtered

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T071 Implement connection loss handling with exponential backoff in internal/db/connection.go
- [ ] T072 Implement manual retry option ('r' key) for reconnection in internal/ui/views/dashboard.go
- [ ] T073 Implement stale data indicator when queries exceed 500ms in internal/ui/views/dashboard.go
- [ ] T074 Implement empty state message when no connections in internal/ui/views/dashboard.go
- [ ] T075 Implement pagination controls (PgUp/PgDn) for large result sets in internal/ui/views/dashboard.go
- [ ] T076 Implement help overlay ('?' key) showing all keyboard shortcuts in internal/ui/components/help.go
- [ ] T077 Validate minimum terminal size (80x24) with graceful message in internal/ui/views/dashboard.go
- [ ] T078 Implement query text truncation with "..." in table view in internal/ui/components/table.go

### Visual Verification (TUI Driver)

- [ ] T079 Use tui-driver to resize terminal to 79x23, screenshot size warning message
- [ ] T080 Use tui-driver to send '?' key, screenshot help overlay with all shortcuts
- [ ] T081 Use tui-driver to screenshot empty state (no connections) message

## Dependencies

```
Phase 1 (Setup) ─┬─> Phase 2 (Foundational)
                 │
                 ├─> Phase 3 (US1: View Connections) ─┬─> Phase 5 (US3: Kill Connections)
                 │                                    │
                 ├─> Phase 4 (US2: View Metrics)      └─> Phase 6 (US4: Filter Connections)
                 │
                 └─> Phase 7 (Polish) [after all user stories]
```

**User Story Dependencies**:
- US1 and US2 are independent (can develop in parallel after Phase 2)
- US3 depends on US1 (need activity table to select connections)
- US4 depends on US1 (need activity table to filter)

## Parallel Execution Examples

### Phase 2 Parallelization
Tasks T017-T021 can run in parallel (different model files):
```
T017 ─┐
T018 ─┤
T019 ─┼─> T022 (depends on models)
T020 ─┤
T021 ─┘
```

### US1 and US2 Parallelization
After Phase 2, both P1 stories can develop in parallel:
```
Phase 2 ─┬─> US1: T025-T034 (Activity table)
         │
         └─> US2: T035-T042 (Metrics panel)
```

### Within US1 Parallelization
```
T025 ─┬─> T027 (monitor needs query)
T026 ─┘
      │
      └─> T028-T034 (UI needs monitor)
```

## Implementation Strategy

### MVP Scope
**User Story 1 (View Active Connections)** alone provides a functional, independently testable MVP that delivers immediate value to DBAs.

### Incremental Delivery
1. **Week 1**: Phase 1-2 (Setup + Foundational + Visual Design)
2. **Week 2**: Phase 3 (US1 - Activity table with auto-refresh)
3. **Week 3**: Phase 4 (US2 - Metrics panel integration)
4. **Week 4**: Phase 5-6 (US3-4 - Kill actions and filtering)
5. **Week 5**: Phase 7 (Polish and edge cases)

### Testing Strategy
- Unit tests for TPS calculation, cache ratio, query truncation
- Integration tests for pg_stat_activity queries with testcontainers
- Manual UI testing checklist for each user story completion

### Visual Design Gate
**CRITICAL**: Tasks T009-T016 MUST be completed and approved before starting UI implementation tasks (T028+). This ensures Constitution VI compliance.

## Task Summary

| Phase | Tasks | Parallelizable |
|-------|-------|----------------|
| Phase 1: Setup | 10 | 4 |
| Phase 2: Foundational | 19 | 5 |
| Phase 3: US1 | 13 | 2 |
| Phase 4: US2 | 10 | 0 |
| Phase 5: US3 | 11 | 0 |
| Phase 6: US4 | 7 | 0 |
| Phase 7: Polish | 11 | 0 |
| **Total** | **81** | **11** |

## TUI Driver Integration

The mimo tui-driver enables visual verification during development:

### Setup
```bash
# Build tui-driver (one-time)
cd /Users/brandon/src/mimo && zig build

# Add to PATH or create symlink
ln -s /Users/brandon/src/mimo/zig-out/bin/tui-driver /usr/local/bin/tui-driver
```

### Development Workflow
```bash
# Start Steep in the driver
tui-driver start ./bin/steep --cols 120 --rows 40

# Take screenshot for verification
tui-driver screenshot /tmp/dashboard.txt

# Test keyboard interactions
tui-driver send 'j'              # Navigate down
tui-driver send '\x1b[A'         # Up arrow
tui-driver send '/'              # Enter filter mode
tui-driver send 'active\n'       # Type filter

# Compare states
tui-driver screenshot /tmp/before.txt
tui-driver send 'x'              # Kill action
tui-driver screenshot /tmp/after.txt
tui-driver diff /tmp/before.txt /tmp/after.txt

# Stop session
tui-driver stop
```

### Benefits
- **Constitution VI compliance**: Screenshot throwaway demos for visual approval
- **Regression detection**: Diff screenshots between versions
- **Keyboard testing**: Verify all shortcuts work correctly
- **CI integration potential**: Automated visual testing in future
