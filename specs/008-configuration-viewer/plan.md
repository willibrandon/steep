# Implementation Plan: Configuration Viewer

**Branch**: `008-configuration-viewer` | **Date**: 2025-11-27 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/008-configuration-viewer/spec.md`

## Summary

Implement a read-only Configuration Viewer for browsing PostgreSQL server settings from `pg_settings`. The view displays parameters in a sortable table with search/filter capabilities, highlights modified parameters in yellow, and provides detailed parameter information in a detail view. Auto-refreshes every 60 seconds with export functionality.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**: bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool
**Storage**: PostgreSQL (pg_settings view - read-only)
**Testing**: go test, testcontainers for integration tests
**Target Platform**: Terminal (macOS, Linux)
**Project Type**: Single Go application with TUI
**Performance Goals**: < 2 seconds initial load, < 500ms query execution, < 5 seconds search response
**Constraints**: < 100MB memory, read-only (no configuration changes), minimum 80x24 terminal
**Scale/Scope**: ~350 PostgreSQL configuration parameters

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | PASS | Auto-refresh every 60 seconds (appropriate for config which rarely changes) |
| II. Keyboard-Driven Interface | PASS | `8` key access, `/` search, `d` details, `Esc`/`q` navigation, sort keys |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | Single query to pg_settings with no joins; ~350 rows bounded result set |
| IV. Incremental Delivery | PASS | P1 (view), P2 (search/filter), P3 (details, export) clearly prioritized |
| V. Comprehensive Coverage | PASS | Fills gap in configuration visibility for PostgreSQL monitoring |
| VI. Visual Design First (NON-NEGOTIABLE) | PASS | ASCII mockups in research.md; follows existing locks/tables view patterns |

**Gate Status**: PASS

### Post-Design Re-evaluation (Phase 1 Complete)

All constitution principles verified:

1. **Real-Time First**: 60-second refresh cycle documented in research.md
2. **Keyboard-Driven**: Full keybinding table in research.md section 9
3. **Query Efficiency**: Single pg_settings query, ~10ms execution, no joins
4. **Incremental Delivery**: Data model supports P1→P2→P3 progression
5. **Comprehensive Coverage**: Fills configuration monitoring gap
6. **Visual Design First**: ASCII mockups for main view and detail view in research.md section 6

## Project Structure

### Documentation (this feature)

```text
specs/008-configuration-viewer/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (N/A - internal TUI feature)
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
internal/
├── db/
│   ├── models/
│   │   └── config.go           # Parameter model (NEW)
│   └── queries/
│       └── config.go           # pg_settings queries (NEW)
├── ui/
│   ├── views/
│   │   ├── types.go            # Add ViewConfig enum (MODIFY)
│   │   └── config/             # Configuration view package (NEW)
│   │       ├── view.go         # Main view implementation
│   │       ├── help.go         # Help panel content
│   │       └── export.go       # Export functionality
│   └── styles/                 # Existing styles (reuse)
└── app/
    └── app.go                  # Add config view registration (MODIFY)

tests/
├── integration/
│   └── config_test.go          # pg_settings query tests (NEW)
└── unit/
    └── config_model_test.go    # Model tests (NEW)
```

**Structure Decision**: Follows existing pattern from locks, tables, queries views. Configuration view is a new package under `internal/ui/views/config/` with corresponding model and query modules.

## Complexity Tracking

No violations requiring justification. Feature follows established patterns.
