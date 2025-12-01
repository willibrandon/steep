# Tasks: Alert System

**Input**: Design documents from `/specs/012-alert-system/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Tests NOT explicitly requested - implementation tasks only.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- **Domain logic**: `internal/alerts/`
- **Configuration**: `internal/config/`
- **Storage**: `internal/storage/sqlite/`
- **UI Components**: `internal/ui/components/`
- **UI Views**: `internal/ui/views/`
- **Messages**: `internal/ui/messages.go`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create alert system package structure and types

- [x] T001 Create internal/alerts/ package directory structure
- [x] T002 [P] Define AlertState enum and constants in internal/alerts/types.go
- [x] T003 [P] Define Operator enum and constants in internal/alerts/types.go
- [x] T004 [P] Define Rule struct in internal/alerts/rule.go
- [x] T005 [P] Define State struct in internal/alerts/state.go
- [x] T006 [P] Define Event struct in internal/alerts/event.go
- [x] T007 [P] Define ActiveAlert struct in internal/alerts/active.go

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T008 Add AlertsConfig struct to internal/config/alerts.go with Enabled, HistoryRetention, Rules fields
- [x] T009 Add Alerts field to Config struct in internal/config/config.go
- [x] T010 Add Viper defaults for alerts section in internal/config/config.go applyDefaults()
- [x] T011 Add alert config validation in internal/config/config.go ValidateConfig()
- [x] T012 Add alert_events table schema to internal/storage/sqlite/schema.go initSchema()
- [x] T013 Implement AlertStore interface in internal/storage/sqlite/alert_store.go (SaveEvent, GetHistory, Acknowledge, Prune)
- [x] T014 Define AlertStateMsg and AlertHistoryMsg in internal/ui/messages.go
- [x] T015 [P] Add alert-related styles (warning yellow, critical red) to internal/ui/styles/colors.go

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Configure Threshold Alerts (Priority: P1)

**Goal**: DBAs can configure alerts for critical metrics via YAML config file and rules are validated on startup

**Independent Test**: Add alert rules to config file, start application, verify rules load and validate successfully

### Implementation for User Story 1

- [x] T016 [US1] Implement rule validation logic in internal/alerts/rule.go (ValidateRule function)
- [x] T017 [US1] Implement simple expression parser in internal/alerts/expression.go (metric references only for US1)
- [x] T018 [US1] Implement MetricValues interface adapter in internal/alerts/metrics.go to extract values from models.Metrics
- [x] T019 [US1] Implement Engine struct in internal/alerts/engine.go with LoadRules, state map, and mutex
- [x] T020 [US1] Implement Evaluate method in internal/alerts/engine.go with threshold comparison logic
- [x] T021 [US1] Implement state transition logic in internal/alerts/state.go (Normal/Warning/Critical transitions)
- [x] T022 [US1] Initialize AlertEngine in internal/app/model.go New() function with config rules
- [x] T023 [US1] Integrate alert evaluation into MetricsDataMsg handler in internal/app/model.go Update()
- [x] T024 [US1] Add logging for rule validation warnings in internal/alerts/engine.go

**Checkpoint**: Alert rules load from config, evaluate on metrics refresh, state transitions work

---

## Phase 4: User Story 2 - Visual Alert Indicators (Priority: P1)

**Goal**: DBAs see color-coded alert indicators in Dashboard and status bar counts

**Independent Test**: Trigger alert conditions, verify yellow/red indicators appear in Dashboard and status bar shows counts

### Implementation for User Story 2

- [x] T025 [US2] Implement GetActiveAlerts method in internal/alerts/engine.go
- [x] T026 [US2] Implement WarningCount and CriticalCount methods in internal/alerts/engine.go
- [x] T027 [US2] Add activeAlerts, warningCount, criticalCount fields to DashboardView in internal/ui/views/dashboard.go
- [x] T028 [US2] Handle AlertStateMsg in DashboardView Update() in internal/ui/views/dashboard.go
- [x] T029 [US2] Add renderAlertCounts method to render status bar counts in internal/ui/views/dashboard.go
- [x] T030 [US2] Integrate alert counts into renderStatusBar in internal/ui/views/dashboard.go
- [x] T031 [US2] Send AlertStateMsg from app model after each evaluation in internal/app/model.go

**Checkpoint**: Alert counts visible in status bar, colors update with severity

---

## Phase 5: User Story 3 - Alert Panel Display (Priority: P1)

**Goal**: Dashboard shows alert panel with all active alerts, severity icons, and details

**Independent Test**: Trigger multiple alerts, verify panel appears with severity sorting, correct formatting

### Implementation for User Story 3

- [x] T032 [US3] Create AlertPanel component struct in internal/ui/components/alert_panel.go
- [x] T033 [US3] Implement AlertPanel.SetAlerts method in internal/ui/components/alert_panel.go
- [x] T034 [US3] Implement AlertPanel.View method with severity icons and formatting in internal/ui/components/alert_panel.go
- [x] T035 [US3] Implement severity sorting (Critical first) in AlertPanel in internal/ui/components/alert_panel.go
- [x] T036 [US3] Add alertPanel field to DashboardView in internal/ui/views/dashboard.go
- [x] T037 [US3] Initialize AlertPanel in NewDashboard in internal/ui/views/dashboard.go
- [x] T038 [US3] Update alertPanel.SetAlerts when AlertStateMsg received in internal/ui/views/dashboard.go
- [x] T039 [US3] Render alertPanel in renderMain only when alerts active in internal/ui/views/dashboard.go
- [x] T040 [US3] Handle message truncation for narrow terminals in internal/ui/components/alert_panel.go

**Checkpoint**: User Stories 1, 2, and 3 complete - Core P1 alerting MVP functional

---

## Phase 6: User Story 4 - Alert History (Priority: P2)

**Goal**: DBAs can view alert history with 'a' key, see past incidents with timestamps

**Independent Test**: Trigger and resolve alerts, press 'a' key, verify history overlay shows all transitions

### Implementation for User Story 4

- [x] T041 [US4] Add history overlay state to DashboardView (showHistory, historyEvents, historyIndex) in internal/ui/views/dashboard.go
- [x] T042 [US4] Handle 'a' key to toggle history overlay in DashboardView handleKeyPress in internal/ui/views/dashboard.go
- [x] T043 [US4] Implement loadAlertHistory command that fetches from AlertStore in internal/ui/views/dashboard.go
- [x] T044 [US4] Handle AlertHistoryMsg in DashboardView Update in internal/ui/views/dashboard.go
- [x] T045 [US4] Implement renderHistoryOverlay method in internal/ui/views/dashboard.go
- [x] T046 [US4] Implement j/k navigation in history overlay in internal/ui/views/dashboard.go
- [x] T047 [US4] Implement Esc to close history overlay in internal/ui/views/dashboard.go
- [x] T048 [US4] Persist state changes to AlertStore on each transition in internal/alerts/engine.go
- [x] T049 [US4] Add AlertStore reference to Engine struct in internal/alerts/engine.go
- [x] T050 [US4] Initialize AlertStore in app model and pass to Engine in internal/app/model.go

**Checkpoint**: Alert history persists and displays correctly

---

## Phase 7: User Story 5 - Alert Acknowledgment (Priority: P2)

**Goal**: DBAs can acknowledge alerts with Enter key, acknowledgment persists across restarts

**Independent Test**: Select alert in history, press Enter, verify checkmark appears and persists after restart

### Implementation for User Story 5

- [ ] T051 [US5] Handle Enter key in history overlay to acknowledge selected alert in internal/ui/views/dashboard.go
- [ ] T052 [US5] Implement acknowledgeAlert command in internal/ui/views/dashboard.go
- [ ] T053 [US5] Implement Engine.Acknowledge method in internal/alerts/engine.go
- [ ] T054 [US5] Call AlertStore.Acknowledge to persist acknowledgment in internal/alerts/engine.go
- [ ] T055 [US5] Handle AlertAcknowledgedMsg in DashboardView Update in internal/ui/views/dashboard.go
- [ ] T056 [US5] Update history view to show acknowledgment status (checkmark) in internal/ui/views/dashboard.go
- [ ] T057 [US5] Show acknowledgment status in active alert panel in internal/ui/components/alert_panel.go

**Checkpoint**: User Stories 4 and 5 complete - P2 history and acknowledgment functional

---

## Phase 8: User Story 6 - Custom Alert Rules (Priority: P3)

**Goal**: DBAs can use expression-based rules (ratios, arithmetic) for complex conditions

**Independent Test**: Configure ratio rule (active_connections/max_connections > 0.8), verify it evaluates correctly

### Implementation for User Story 6

- [ ] T058 [US6] Extend expression parser to handle binary operators (+, -, *, /) in internal/alerts/expression.go
- [ ] T059 [US6] Implement Expression interface with Evaluate method in internal/alerts/expression.go
- [ ] T060 [US6] Implement MetricRef expression type in internal/alerts/expression.go
- [ ] T061 [US6] Implement BinaryOp expression type in internal/alerts/expression.go
- [ ] T062 [US6] Implement Constant expression type in internal/alerts/expression.go
- [ ] T063 [US6] Update Engine.evaluateRule to use parsed expressions in internal/alerts/engine.go
- [ ] T064 [US6] Add expression validation in rule loading in internal/alerts/engine.go
- [ ] T065 [US6] Handle expression parse errors gracefully (log warning, skip rule) in internal/alerts/engine.go

**Checkpoint**: All user stories complete - Full alert system functional

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T066 [P] Add help text for 'a' key in dashboard footer in internal/ui/views/dashboard.go
- [ ] T067 [P] Add help text for history overlay in internal/ui/views/dashboard.go
- [ ] T068 Implement hourly prune goroutine for alert history in internal/app/model.go
- [ ] T069 [P] Ensure alert panel renders correctly at 80x24 minimum terminal in internal/ui/components/alert_panel.go
- [ ] T070 Handle edge case: database connection lost during evaluation in internal/alerts/engine.go
- [ ] T071 Handle edge case: unavailable metrics (graceful degradation) in internal/alerts/metrics.go
- [ ] T072 Add example alert rules to default config template
- [ ] T073 Run quickstart.md validation scenarios manually

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-8)**: All depend on Foundational phase completion
  - US1, US2, US3 (P1): Should be done together for MVP
  - US4, US5 (P2): Can proceed after P1 complete
  - US6 (P3): Can proceed after P2 complete
- **Polish (Phase 9)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational - No dependencies on other stories
- **User Story 2 (P1)**: Depends on US1 (needs Engine with Evaluate)
- **User Story 3 (P1)**: Depends on US2 (needs GetActiveAlerts)
- **User Story 4 (P2)**: Depends on US1 (needs Engine with state transitions)
- **User Story 5 (P2)**: Depends on US4 (needs history overlay)
- **User Story 6 (P3)**: Depends on US1 (extends expression parser)

### Within Each User Story

- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks T002-T007 marked [P] can run in parallel
- T015 can run in parallel with other Foundational tasks
- Tasks in Polish phase marked [P] can run in parallel

---

## Parallel Example: Phase 1 Setup

```bash
# Launch all type definitions in parallel:
Task: "Define AlertState enum and constants in internal/alerts/types.go"
Task: "Define Operator enum and constants in internal/alerts/types.go"
Task: "Define Rule struct in internal/alerts/rule.go"
Task: "Define State struct in internal/alerts/state.go"
Task: "Define Event struct in internal/alerts/event.go"
Task: "Define ActiveAlert struct in internal/alerts/active.go"
```

---

## Implementation Strategy

### MVP First (P1 Stories Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (Configure Alerts)
4. Complete Phase 4: User Story 2 (Visual Indicators)
5. Complete Phase 5: User Story 3 (Alert Panel)
6. **STOP and VALIDATE**: Test P1 stories - alerts configure, evaluate, display
7. Deploy/demo MVP

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add US1 + US2 + US3 → Test independently → Deploy MVP (P1 complete!)
3. Add US4 + US5 → Test independently → Deploy P2 features
4. Add US6 → Test independently → Deploy P3 features
5. Each story adds value without breaking previous stories

### Task Summary

| Phase | Story | Task Count |
|-------|-------|------------|
| Phase 1: Setup | - | 7 |
| Phase 2: Foundational | - | 8 |
| Phase 3: US1 | P1 | 9 |
| Phase 4: US2 | P1 | 7 |
| Phase 5: US3 | P1 | 9 |
| Phase 6: US4 | P2 | 10 |
| Phase 7: US5 | P2 | 7 |
| Phase 8: US6 | P3 | 8 |
| Phase 9: Polish | - | 8 |
| **Total** | | **73** |

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- P1 stories (US1-US3) form the MVP - complete these first
- Constitution Principle VI: Complete visual design mockups before Phase 5 (US3) implementation
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
