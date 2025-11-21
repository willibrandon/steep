# Implementation Plan: Dashboard & Activity Monitoring

**Branch**: `002-dashboard-activity` | **Date**: 2025-11-21 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/002-dashboard-activity/spec.md`

## Summary

Build a combined Dashboard and Activity Monitoring view for Steep that displays real-time PostgreSQL metrics (TPS, cache hit ratio, connection count, database size) in a top panel and active connections in a sortable/filterable table below. Support auto-refresh at 1-second intervals, connection state color-coding, cancel/terminate actions with confirmation, and pagination for large result sets (500 row default). Uses pg_stat_activity and pg_stat_database as data sources with Bubbletea TUI framework.

## Technical Context

**Language/Version**: Go 1.21+
**Primary Dependencies**: bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool
**Storage**: PostgreSQL 11+ (pg_stat_activity, pg_stat_database system views)
**Testing**: go test with testcontainers for integration tests
**Target Platform**: Terminal (macOS, Linux) with 256-color support
**Project Type**: Single TUI application
**Performance Goals**: <500ms query execution, 1-second refresh intervals, smooth 60 FPS rendering
**Constraints**: <50MB memory, <5% CPU idle, minimum 80x24 terminal
**Scale/Scope**: 100+ concurrent database connections, 500 row display limit with pagination

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | PASS | 1-second configurable refresh, live pg_stat_activity data |
| II. Keyboard-Driven | PASS | vim-like navigation (hjkl, g/G, /), actions (s, d, c, x) |
| III. Query Efficiency | PASS | Prepared statements, 500 row LIMIT, <500ms target |
| IV. Incremental Delivery | PASS | P1 stories (view connections, metrics) before P2 (kill, filter) |
| V. Comprehensive Coverage | PASS | Covers activity monitoring, first of planned features |
| VI. Visual Design First | NEEDS COMPLIANCE | Must study pg_top/htop, create ASCII mockups before implementation |

**Gate Status**: CONDITIONAL PASS - Visual Design First compliance required during Phase 1

## Project Structure

### Documentation (this feature)

```text
specs/002-dashboard-activity/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal message contracts)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/
├── app/                 # Application orchestration
├── config/              # Configuration management (Viper)
├── db/                  # Database connection and queries
│   ├── connection.go
│   ├── queries/         # SQL query definitions
│   │   ├── activity.go
│   │   └── stats.go
│   └── models/          # Data models
│       ├── connection.go
│       └── metrics.go
├── monitors/            # Background data fetching goroutines
│   ├── activity.go
│   └── stats.go
└── ui/                  # Bubbletea UI
    ├── app.go           # Main app model
    ├── components/      # Reusable components
    │   ├── table.go
    │   ├── panel.go
    │   └── dialog.go
    ├── views/           # View implementations
    │   └── dashboard.go # Combined dashboard + activity view
    └── styles/          # Lipgloss styles

tests/
├── integration/         # Database integration tests
│   └── activity_test.go
└── unit/                # Unit tests for business logic
    └── metrics_test.go
```

**Structure Decision**: Single project structure following existing Steep architecture defined in CLAUDE.md. The combined Dashboard/Activity view is a single Bubbletea view model with two visual sections.

## Complexity Tracking

> No violations requiring justification. Design follows constitution principles.

