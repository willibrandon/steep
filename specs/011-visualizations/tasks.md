# Tasks: Advanced Visualizations

**Input**: Design documents from `/specs/011-visualizations/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Unit tests included for core components per project standards.

**Organization**: Tasks grouped by user story (US1-US6) to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1-US6)
- Exact file paths included in descriptions

---

## Phase 1: Setup

**Purpose**: Add dependencies and create package structure

- [x] T001 Add pterm dependency with `go get github.com/pterm/pterm`
- [x] T002 Run `go mod tidy` to update go.sum
- [x] T003 Create metrics package directory at internal/metrics/
- [x] T004 Create mockups directory at specs/011-visualizations/mockups/

**Checkpoint**: Dependencies installed, directories created ✅

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure required by ALL user stories

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 [P] Implement TimeWindow type with Duration/Granularity/String methods in internal/metrics/timewindow.go
- [x] T006 [P] Implement DataPoint struct in internal/metrics/datapoint.go
- [x] T007 Implement CircularBuffer with Push/GetRecent/GetSince/GetValues/Len/Clear in internal/metrics/buffer.go
- [x] T008 Add unit tests for CircularBuffer eviction behavior in internal/metrics/buffer_test.go
- [x] T009 Add metrics_history table schema to internal/storage/sqlite/schema.go
- [x] T010 Implement MetricsStore with SaveDataPoint/SaveBatch/GetHistory/GetAggregated/Prune in internal/storage/sqlite/metrics_store.go
- [x] T011 Add unit tests for MetricsStore in internal/storage/sqlite/metrics_store_test.go
- [x] T012 Implement MetricsCollector with Record/GetValues/GetDataPoints/GetLatest/Start/Stop in internal/metrics/collector.go
- [x] T013 Add unit tests for MetricsCollector in internal/metrics/collector_test.go
- [x] T014 Integrate MetricsCollector initialization in internal/app/app.go
- [x] T015 Record TPS/connections/cache_hit_ratio metrics in database monitor goroutine in internal/monitors/stats.go

**Checkpoint**: Foundation ready - metrics collection operational, user story implementation can begin ✅

---

## Phase 3: User Story 1 - Time-Series Graphs (Priority: P1) 🎯 MVP

**Goal**: Display time-series line graphs for TPS, connections, cache hit ratio on Dashboard with configurable time windows

**Independent Test**: Navigate to Dashboard (key `1`), verify three graphs appear and update with refresh interval. Press `1`-`5` to change time windows.

### Implementation for User Story 1

- [x] T016 [P] [US1] Create ASCII mockup of Dashboard with graphs in specs/011-visualizations/mockups/dashboard-graphs.txt
- [x] T017 [P] [US1] Implement TimeSeriesChart component wrapping asciigraph in internal/ui/components/timeseries.go
- [x] T018 [P] [US1] Add unit tests for TimeSeriesChart rendering in internal/ui/components/timeseries_test.go
- [x] T019 [US1] Add time-series graph panel to Dashboard view in internal/ui/views/dashboard.go
- [x] T020 [US1] Implement time window state and number key handlers (1-5) in internal/ui/views/dashboard.go
- [x] T021 [US1] Add "Collecting data..." message for insufficient data in internal/ui/components/timeseries.go
- [x] T022 [US1] Handle terminal resize for graph dimensions in internal/ui/views/dashboard.go

**Checkpoint**: Dashboard shows TPS, connections, cache hit ratio graphs with time window selection ✅

---

## Phase 4: User Story 5 - Chart Toggle (Priority: P2)

**Goal**: Toggle chart visibility with 'v' key, preserving state across view switches

**Independent Test**: Press `v` on Dashboard - charts disappear. Press `v` again - charts reappear. Switch views and return - toggle state preserved.

### Implementation for User Story 5

- [x] T023 [P] [US5] Add ChartToggleState to app model in internal/app/app.go
- [x] T024 [US5] Implement 'v' key handler for chart toggle in internal/app/app.go (global handler)
- [x] T025 [US5] Update status bar to show chart visibility state in internal/ui/components/statusbar.go
- [x] T026 [US5] Propagate toggle state to all views that display charts in internal/app/app.go

**Checkpoint**: Chart toggle works globally across all views ✅

---

## Phase 5: User Story 2 - Activity View Sparklines (Priority: P2)

**Goal**: Show sparklines for query duration trend per connection in Activity view

**Independent Test**: Navigate to Activity view (key `2`), verify sparkline column shows duration trends. Connections with spikes show visible peaks.

### Implementation for User Story 2

- [x] T027 [P] [US2] Create ASCII mockup of Activity view with sparklines in specs/011-visualizations/mockups/activity-sparklines.txt
- [x] T028 [P] [US2] Implement per-connection duration tracking in internal/metrics/connection_metrics.go
- [x] T029 [P] [US2] Add unit tests for connection metrics in internal/metrics/connection_metrics_test.go
- [x] T030 [US2] Add sparkline column to Activity view table in internal/ui/views/activity.go
- [x] T031 [US2] Handle narrow terminal (hide sparklines, show "-") in internal/ui/views/activity.go

**Checkpoint**: Activity view shows query duration sparklines per connection ✅

---

## Phase 6: User Story 3 - Tables View Sparklines (Priority: P2)

**Goal**: Show sparklines for table size growth trend in Tables view

**Independent Test**: Navigate to Tables view (key `5`), verify sparkline column shows size trends over 24h. Growing tables show upward trend.

### Implementation for User Story 3

- [x] T032 [P] [US3] Create ASCII mockup of Tables view with sparklines in specs/011-visualizations/mockups/tables-sparklines.txt
- [x] T033 [US3] Record table sizes to MetricsStore in internal/ui/views/tables/view.go (recordTableSizes, refreshTableSizeCache)
- [x] T034 [US3] Add sparkline column (Trend) to Tables view in internal/ui/views/tables/render.go (renderTableSparkline)
- [x] T035 [US3] Handle narrow terminal (hide sparklines when width < 80) in internal/ui/views/tables/render.go (showSparklines)

**Checkpoint**: Tables view shows size trend sparklines per table

---

## Phase 7: User Story 4 - Bar Charts (Priority: P2)

**Goal**: Show horizontal bar charts for top 10 queries by execution time and top 10 tables by size

**Independent Test**: Navigate to Queries view (key `3`), verify bar chart panel shows top 10 queries. Navigate to Tables view, verify bar chart shows top 10 tables.

### Implementation for User Story 4

- [x] T036 [P] [US4] Create ASCII mockup of Queries view bar chart in specs/011-visualizations/mockups/queries-barchart.txt
- [x] T037 [P] [US4] Implement BarChart component wrapping pterm in internal/ui/components/barchart.go
- [x] T038 [P] [US4] Add unit tests for BarChart rendering in internal/ui/components/barchart_test.go
- [x] T039 [US4] Add bar chart panel to Queries view in internal/ui/views/queries/view.go
- [x] T040 [US4] Add bar chart panel to Tables view in internal/ui/views/tables/render.go
- [x] T041 [US4] Implement color coding by rank (cyan gradient: brightest=top, dimmer=lower) in internal/ui/components/barchart.go

**Checkpoint**: Bar charts visible in Queries and Tables views ✅

---

## Phase 8: User Story 6 - Heatmaps (Priority: P3)

**Goal**: Show heatmap of query volume by hour (24) and day (7) with color gradient

**Independent Test**: After collecting data for several hours, access heatmap panel. Verify 24x7 grid shows query patterns with blue (low) to red (high) gradient.

### Implementation for User Story 6

- [x] T042 [P] [US6] Create ASCII mockup of heatmap panel in specs/011-visualizations/mockups/heatmap.txt
- [x] T043 [P] [US6] Implement hourly volume aggregation query in internal/storage/sqlite/metrics_store.go
- [x] T044 [P] [US6] Implement Heatmap component wrapping pterm in internal/ui/components/heatmap.go
- [x] T045 [P] [US6] Add unit tests for Heatmap rendering in internal/ui/components/heatmap_test.go
- [x] T046 [US6] Add heatmap panel to Dashboard or create dedicated view in internal/ui/views/dashboard.go
- [x] T047 [US6] Add keybinding to toggle heatmap visibility in internal/ui/views/dashboard.go

**Checkpoint**: Heatmap shows query volume patterns by hour and day ✅

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, help text, performance validation

- [ ] T048 Update help overlay with chart keybindings (v, 1-5) in internal/ui/components/help.go
- [ ] T049 Add benchmark tests for chart rendering (<50ms) in internal/ui/components/timeseries_test.go
- [ ] T050 Add benchmark tests for CircularBuffer operations (<1ms) in internal/metrics/buffer_test.go
- [ ] T051 Add integration tests for MetricsStore persistence in tests/integration/metrics_store_test.go
- [ ] T052 Verify memory usage stays under 10MB with profiling
- [ ] T053 Run quickstart.md validation scenarios
- [ ] T054 Update CLAUDE.md via .specify/scripts/bash/update-agent-context.sh

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **US1 Time-Series (Phase 3)**: Depends on Foundational - MVP target
- **US5 Chart Toggle (Phase 4)**: Depends on US1 (needs charts to toggle)
- **US2 Activity Sparklines (Phase 5)**: Depends on Foundational
- **US3 Tables Sparklines (Phase 6)**: Depends on Foundational
- **US4 Bar Charts (Phase 7)**: Depends on Foundational
- **US6 Heatmaps (Phase 8)**: Depends on Foundational + sufficient historical data
- **Polish (Phase 9)**: Depends on desired user stories complete

### User Story Dependencies

```
Foundational (Phase 2)
        │
        ├──────────────────┬──────────────────┬──────────────────┐
        ▼                  ▼                  ▼                  ▼
   US1 Time-Series    US2 Activity      US3 Tables        US4 Bar Charts
   (Phase 3) 🎯       (Phase 5)         (Phase 6)         (Phase 7)
        │
        ▼
   US5 Chart Toggle                                       US6 Heatmaps
   (Phase 4)                                              (Phase 8)
```

### Parallel Opportunities

**Phase 2 (Foundational)**:
- T005, T006 can run in parallel (independent types)
- T009, T010, T011 (MetricsStore) can run parallel to T007, T008 (CircularBuffer)

**Within User Stories**:
- Mockup tasks [P] can run parallel to component implementation
- Unit test tasks [P] can run parallel to other unit tests

---

## Parallel Example: Foundational Phase

```bash
# Launch in parallel:
Task T005: "Implement TimeWindow type in internal/metrics/timewindow.go"
Task T006: "Implement DataPoint struct in internal/metrics/datapoint.go"

# Then:
Task T007: "Implement CircularBuffer in internal/metrics/buffer.go"

# In parallel with CircularBuffer:
Task T009: "Add metrics_history table schema in internal/storage/sqlite/db.go"
Task T010: "Implement MetricsStore in internal/storage/sqlite/metrics_store.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL)
3. Complete Phase 3: User Story 1 (Time-Series Graphs)
4. **STOP and VALIDATE**: Test Dashboard graphs independently
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 (Time-Series) → Test → Deploy (MVP!)
3. Add US5 (Toggle) → Test → Deploy
4. Add US2 (Activity Sparklines) → Test → Deploy
5. Add US3 (Tables Sparklines) → Test → Deploy
6. Add US4 (Bar Charts) → Test → Deploy
7. Add US6 (Heatmaps) → Test → Deploy (P3, can defer)

---

## Notes

- [P] tasks = different files, no dependencies
- [US#] label maps task to specific user story
- Each user story independently completable and testable
- Commit after each task or logical group
- pterm uses Srender() for Bubbletea integration
- asciigraph already in go.mod (v0.7.3)
- Existing sparkline.go in internal/ui/components/ can be referenced
