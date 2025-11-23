# Implementation Plan: Locks & Blocking Detection

**Branch**: `004-locks-blocking` | **Date**: 2025-11-22 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/004-locks-blocking/spec.md`

## Summary

Implement a Locks & Blocking Detection view for Steep that monitors database lock contention in real-time. The view queries `pg_locks` and `pg_stat_activity` to display active locks with type, mode, granted status, and associated queries. Key features include color-coded blocking relationship detection (red=blocked, yellow=blocking), ASCII tree visualization of lock dependency chains using the treeprint library, and the ability to terminate blocking queries with confirmation.

## Technical Context

**Language/Version**: Go 1.21+ (using Go 1.25.4 per go.mod)
**Primary Dependencies**: bubbletea, bubbles (table, viewport), lipgloss, pgx/pgxpool, xlab/treeprint
**Storage**: PostgreSQL (pg_locks, pg_stat_activity system views)
**Testing**: testcontainers-go for integration tests, standard Go testing for unit tests
**Target Platform**: Terminal (macOS, Linux)
**Project Type**: Single TUI application
**Performance Goals**: Query execution < 500ms with 100+ locks, 2-second auto-refresh
**Constraints**: Minimum terminal 80x24, < 50MB memory
**Scale/Scope**: Up to 100+ concurrent locks in typical scenarios

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | PASS | 2-second auto-refresh specified (FR-007), refresh interval configurable |
| II. Keyboard-Driven | PASS | All actions have keyboard shortcuts: `5` nav, `s` sort, `d` detail, `x` kill |
| III. Query Efficiency | PASS | < 500ms requirement (FR-011), bounded result sets via pg_locks, prepared statements |
| IV. Incremental Delivery | PASS | Stories prioritized P1-P3, independently testable increments |
| V. Comprehensive Coverage | PASS | Adds lock monitoring capability to existing dashboard/activity/queries |
| VI. Visual Design First | PENDING | Must complete before implementation: reference study, ASCII mockups, demos |

**Gate Status**: CONDITIONAL PASS - Principle VI (Visual Design First) must be completed during Phase 0 research before any implementation begins.

## Project Structure

### Documentation (this feature)

```text
specs/004-locks-blocking/
├── spec.md              # Feature specification
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output
```

### Source Code (repository root)

```text
internal/
├── db/
│   ├── models/
│   │   └── lock.go           # Lock and BlockingRelationship models
│   └── queries/
│       └── locks.go          # pg_locks queries with blocking detection
├── monitors/
│   └── locks.go              # Locks monitor goroutine (2s refresh)
└── ui/
    ├── components/
    │   └── lock_tree.go      # ASCII tree rendering with treeprint
    └── views/
        └── locks/
            ├── view.go       # Main locks view implementing ViewModel
            ├── help.go       # Help text for locks view
            └── detail.go     # Full query detail modal

tests/
├── integration/
│   └── locks_test.go         # Integration tests with testcontainers
└── unit/
    └── lock_tree_test.go     # Unit tests for tree rendering logic
```

**Structure Decision**: Following existing patterns from dashboard.go and queries/ subdirectory. The locks view mirrors the queries view structure with its own subdirectory for related components (view, help, detail).

## UI Consistency Requirements (NON-NEGOTIABLE)

The locks view MUST exactly follow the queries view implementation patterns. This is critical for:
- Consistent user experience across all views
- Avoiding known bugs (e.g., viewport component causes Esc key delay)
- Maintainability

### Required Patterns from Queries View

1. **Detail View**: Copy `internal/ui/views/queries/explain.go` pattern exactly
   - Manual `scrollOffset` for scrolling (NOT viewport component)
   - `msg.String() == "esc"` key handling (NOT `msg.Type == tea.KeyEsc`)
   - `lipgloss.JoinVertical` layout

2. **SQL Formatting**: Docker pgFormatter
   ```go
   exec.Command("docker", "run", "--rm", "-i", "ghcr.io/darold/pgformatter:latest", "pg_format")
   ```

3. **Syntax Highlighting**: Chroma monokai
   ```go
   import "github.com/alecthomas/chroma/v2/quick"
   quick.Highlight(&buf, sql, "sql", "terminal16m", "monokai")
   ```

4. **Footer**: Key hints in queries view format: `[key]action [key]action`

5. **Table Focus**: `table.Blur()` on modal enter, `table.Focus()` on modal exit

### Reference Files (MUST study before implementation)

- `internal/ui/views/queries/view.go` - **READ ENTIRE FILE (all ~990 lines)** before writing any view code
- `internal/ui/views/queries/explain.go` - PRIMARY REFERENCE for detail view

## Complexity Tracking

No constitution violations requiring justification. Architecture follows established patterns.
