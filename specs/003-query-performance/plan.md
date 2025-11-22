# Implementation Plan: Query Performance Monitoring

**Branch**: `003-query-performance` | **Date**: 2025-11-21 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/003-query-performance/spec.md`

## Summary

Implement a Query Performance Monitoring view that identifies slow and frequent queries without requiring pg_stat_statements extension. Primary data source is PostgreSQL query logging (log_min_duration_statement) parsed via honeytail library, with pg_stat_activity sampling as fallback for remote databases. Queries are fingerprinted using pg_query_go for deduplication, aggregated client-side, and persisted in SQLite with 7-day retention. UI provides tabbed views (By Time/Calls/Rows), EXPLAIN plan viewing, search/filter, and clipboard copy.

## Technical Context

**Language/Version**: Go 1.21+
**Primary Dependencies**: bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool, pg_query_go/v5, honeytail/parsers/postgresql, go-sqlite3, golang.design/x/clipboard
**Storage**: SQLite (query_stats.db in ~/.config/steep/), PostgreSQL (source database)
**Testing**: go test, testcontainers-go
**Target Platform**: macOS, Linux (terminal)
**Project Type**: Single TUI application
**Performance Goals**: <500ms query execution, <100ms UI response, 5s auto-refresh
**Constraints**: <50MB memory, 80x24 minimum terminal, 7-day data retention
**Scale/Scope**: Top 50 queries per view, ~1000 unique fingerprints typical

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | PASS | 5s configurable auto-refresh (FR-019), live data from logs/sampling |
| II. Keyboard-Driven Interface | PASS | All actions have keybindings: tabs (arrows), sort (s), search (/), explain (e), copy (y), reset (R), vim navigation (j/k/g/G) |
| III. Query Efficiency | PASS | SQLite queries local, PostgreSQL queries bounded (top 50), prepared statements, <500ms target |
| IV. Incremental Delivery | PASS | P1 (view queries) → P2 (EXPLAIN, search, copy) → P3 (reset); each independently testable |
| V. Comprehensive Coverage | PASS | Fills critical gap in monitoring suite for query performance analysis |
| VI. Visual Design First | DEFERRED | Visual mockups and reference study required before implementation; will be addressed in tasks |

**Gate Result**: PASS (VI deferred to task planning phase)

## Project Structure

### Documentation (this feature)

```text
specs/003-query-performance/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
internal/
├── monitors/
│   └── queries/           # Query performance monitor
│       ├── monitor.go     # Main monitor goroutine
│       ├── collector.go   # Data collection (logs/sampling)
│       ├── fingerprint.go # Query normalization via pg_query_go
│       └── stats.go       # Statistics aggregation
├── storage/
│   └── sqlite/            # SQLite persistence layer
│       ├── db.go          # Connection management
│       └── queries.go     # Query stats CRUD
├── ui/
│   └── views/
│       └── queries/       # Queries view
│           ├── view.go    # Main view model
│           ├── table.go   # Query table component
│           ├── tabs.go    # Tab navigation
│           ├── explain.go # EXPLAIN plan display
│           └── search.go  # Search/filter input
└── config/
    └── queries.go         # Queries-specific config

cmd/steep/
└── main.go                # View registration

tests/
├── integration/
│   └── queries/           # Integration tests with testcontainers
└── unit/
    └── monitors/
        └── queries/       # Unit tests for fingerprinting, aggregation
```

**Structure Decision**: Single project structure following existing Steep patterns. New monitor under `internal/monitors/queries/`, new view under `internal/ui/views/queries/`, SQLite storage layer under `internal/storage/sqlite/`.

## Complexity Tracking

No constitution violations requiring justification.
