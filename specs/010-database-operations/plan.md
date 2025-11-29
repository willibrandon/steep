# Implementation Plan: Database Management Operations

**Branch**: `010-database-operations` | **Date**: 2025-11-28 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/010-database-operations/spec.md`

## Summary

Implement comprehensive database maintenance operations (VACUUM, ANALYZE, REINDEX) with progress tracking, cancellation support, and vacuum status visibility in the Tables view. Add a new Roles view (key `0`) for user/role management with permission viewing and GRANT/REVOKE operations. All destructive operations respect read-only mode and require confirmation dialogs.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**: bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool
**Storage**: PostgreSQL (source database via pg_stat_progress_vacuum, pg_stat_all_tables, pg_roles, pg_catalog)
**Testing**: go test with testcontainers for integration tests
**Target Platform**: Terminal/CLI (macOS, Linux)
**Project Type**: Single TUI application
**Performance Goals**: <500ms query execution, <100ms UI updates, 1-second progress polling
**Constraints**: Operations must not block UI, single operation at a time, keyboard-driven interface
**Scale/Scope**: Single PostgreSQL connection, all roles and tables in connected database

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | ✅ PASS | Progress tracking updates within 1 second via pg_stat_progress_vacuum polling |
| II. Keyboard-Driven Interface | ✅ PASS | All operations via keyboard: `x` for operations menu, `0` for Roles view, confirmation dialogs use y/n/Esc |
| III. Query Efficiency | ✅ PASS | Use existing optimized queries; progress queries are lightweight SELECT from system views |
| IV. Incremental Delivery | ✅ PASS | P1 stories (VACUUM, ANALYZE) independent of P2/P3; each story testable standalone |
| V. Comprehensive Coverage | ✅ PASS | Covers maintenance operations gap; role management fills security visibility gap |
| VI. Visual Design First | ⚠️ PARTIAL | Existing Tables view patterns apply; Roles view needs ASCII mockup before implementation |

**Gate Result**: PASS with condition - Roles view (P3) requires visual design phase before implementation.

### Post-Design Re-evaluation

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | ✅ PASS | Progress polling at 1-second intervals (data-model.md: OperationProgress) |
| II. Keyboard-Driven Interface | ✅ PASS | All keybindings defined (contracts/ui_operations.go.md) |
| III. Query Efficiency | ✅ PASS | Queries use system views with index access (contracts/maintenance.go.md) |
| IV. Incremental Delivery | ✅ PASS | 7 implementation phases in quickstart.md, each testable |
| V. Comprehensive Coverage | ✅ PASS | All P1/P2/P3 stories covered in contracts |
| VI. Visual Design First | ⚠️ DEFERRED | Roles view mockup to be created before Phase 6 implementation |

**Post-Design Gate Result**: PASS - Proceed to task generation.

## Project Structure

### Documentation (this feature)

```text
specs/010-database-operations/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal Go interfaces)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/
├── db/
│   ├── models/
│   │   ├── table.go           # Extend Table model with vacuum timestamps
│   │   ├── role.go            # NEW: Role model with attributes
│   │   └── operation.go       # NEW: MaintenanceOperation model
│   └── queries/
│       ├── tables.go          # Extend with vacuum status, progress, cancellation
│       ├── roles.go           # NEW: Role and permission queries
│       └── maintenance.go     # NEW: VACUUM variants, progress polling
├── monitors/
│   └── maintenance.go         # NEW: Progress monitor goroutine
└── ui/
    ├── components/
    │   └── progress.go        # NEW: Progress indicator component
    └── views/
        ├── tables/
        │   ├── view.go        # Extend: operations menu, vacuum status columns
        │   └── operations.go  # NEW: Operations menu and confirmation dialogs
        └── roles/
            ├── view.go        # NEW: Roles view with `0` key binding
            ├── help.go        # NEW: Help overlay for Roles view
            └── permissions.go # NEW: GRANT/REVOKE dialogs

tests/
├── integration/
│   ├── maintenance_test.go    # NEW: VACUUM/ANALYZE/REINDEX tests
│   └── roles_test.go          # NEW: Role query tests
└── unit/
    └── progress_test.go       # NEW: Progress calculation tests
```

**Structure Decision**: Extends existing single-project structure. New files follow established patterns in `internal/db/queries/` and `internal/ui/views/`. Roles view mirrors Tables view architecture.

## Complexity Tracking

> **No violations requiring justification** - feature uses existing patterns and adds minimal new complexity.

