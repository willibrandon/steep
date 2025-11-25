# Implementation Plan: Replication Monitoring & Setup

**Branch**: `006-replication-monitoring` | **Date**: 2025-11-24 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/006-replication-monitoring/spec.md`

## Summary

Implement a comprehensive Replication Monitoring & Setup view for Steep that provides real-time monitoring of PostgreSQL streaming replication (physical and logical), visual topology representation, lag trend analysis with historical persistence, and guided setup wizards for configuring replication. The view follows existing patterns from locks/tables views with multi-tab layout, split-panel design, and keyboard-driven navigation.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**:
- bubbletea v1.3.10, bubbles v0.21.0, lipgloss v1.1.0 (TUI framework)
- pgx/v5 v5.7.6 (PostgreSQL driver)
- go-sqlite3 v1.14.32 (local persistence)
- xlab/treeprint v1.2.0 (already in use for topology)
- charmbracelet/huh (NEW - forms library for setup wizards)
- guptarohit/asciigraph (NEW - sparkline charts)
- sethvargo/go-password (NEW - secure password generation)
**Storage**:
- PostgreSQL (source - replication system views)
- SQLite (local persistence for lag history, same pattern as query_stats)
**Testing**: go test with testcontainers for PostgreSQL integration tests
**Target Platform**: Terminal (macOS, Linux) - minimum 80x24
**Project Type**: Single Go module with internal packages
**Performance Goals**:
- Query execution < 500ms
- 2-second refresh intervals
- Support 100+ replicas without degradation
**Constraints**:
- < 500ms query execution
- Read-only mode must block all setup operations
- PostgreSQL 11+ minimum, 15+ for pg_hba_file_rules
**Scale/Scope**: Monitor up to 100+ replicas, 7-day lag history retention

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence/Plan |
|-----------|--------|---------------|
| I. Real-Time First | PASS | 2-second auto-refresh (FR-013), live lag metrics |
| II. Keyboard-Driven Interface | PASS | Vim-style navigation (FR-024), comprehensive keybindings defined in spec |
| III. Query Efficiency (NON-NEGOTIABLE) | PLAN | Will use prepared statements, result limits; queries target system views with existing indexes |
| IV. Incremental Delivery | PASS | 13 user stories prioritized P1→P2→P3, each independently testable |
| V. Comprehensive Coverage | PASS | Covers physical + logical replication, topology, slots, setup wizards |
| VI. Visual Design First (NON-NEGOTIABLE) | PLAN | Must complete visual design phase with reference study (pg_top, htop, k9s), ASCII mockups, and library validation demos before implementation |

**Gate Decision**: PROCEED - All principles addressed. Visual Design First (VI) will be executed in Phase 0 research.

## Project Structure

### Documentation (this feature)

```text
specs/006-replication-monitoring/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (N/A - internal Go, no external API)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/
├── db/
│   ├── models/
│   │   └── replication.go       # Replica, Slot, Publication, Subscription models
│   └── queries/
│       └── replication.go       # PostgreSQL replication queries
├── monitors/
│   └── replication.go           # Replication monitor goroutine
├── storage/sqlite/
│   ├── schema.go                # Add replication_lag_history table
│   └── replication_store.go     # Lag history persistence
├── ui/
│   ├── components/
│   │   └── sparkline.go         # Lag sparkline component (uses asciigraph)
│   └── views/
│       └── replication/
│           ├── view.go          # Main view implementing ViewModel
│           ├── tabs.go          # Tab definitions (Overview, Slots, Logical, Setup)
│           ├── help.go          # Help overlay content
│           ├── repviz/
│           │   ├── topology.go  # ASCII topology tree (uses treeprint)
│           │   └── pipeline.go  # WAL pipeline visualization
│           └── setup/
│               ├── config_check.go     # Configuration checker
│               ├── physical_wizard.go  # Physical replication wizard (uses huh)
│               ├── logical_wizard.go   # Logical replication wizard
│               └── connstring.go       # Connection string builder

tests/
├── integration/
│   └── replication_test.go      # Integration tests with testcontainers
└── unit/
    ├── replication_model_test.go
    └── lag_history_test.go
```

**Structure Decision**: Follows existing pattern from locks/ and tables/ views - single view.go implementing ViewModel interface with separate tabs.go, help.go, and specialized visualization packages (repviz/ similar to deadviz/ in locks).

## Complexity Tracking

> No constitution violations requiring justification.

| Aspect | Complexity Level | Justification |
|--------|-----------------|---------------|
| New dependencies (huh, asciigraph, go-password) | Low | All are well-maintained Charm libraries or established packages |
| SQLite schema extension | Low | Same pattern as existing deadlock_events table |
| Setup wizards with forms | Medium | New pattern for Steep, but huh library handles complexity |
