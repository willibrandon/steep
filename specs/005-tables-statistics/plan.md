# Implementation Plan: Tables & Statistics Viewer

**Branch**: `005-tables-statistics` | **Date**: 2025-11-24 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/005-tables-statistics/spec.md`

## Summary

Implement a Tables & Statistics Viewer that provides hierarchical browsing of PostgreSQL schemas and tables with comprehensive statistics display. The view will show table sizes (total including indexes/TOAST), row counts, cache hit ratios, and bloat percentages (when pgstattuple is available). Features include system schema toggle, partitioned table hierarchy, index usage statistics with unused index highlighting, table details panel, and optional maintenance operations (VACUUM/ANALYZE/REINDEX). The implementation follows existing Bubbletea patterns established in the Queries and Locks views.

## Technical Context

**Language/Version**: Go 1.21+ (Go 1.25.4 per go.mod)
**Primary Dependencies**: bubbletea, bubbles, lipgloss, pgx/pgxpool, golang.design/x/clipboard
**Storage**: PostgreSQL (source database via pg_stat_all_tables, pg_stat_all_indexes, pgstattuple)
**Testing**: Go testing with testcontainers for integration tests
**Target Platform**: Terminal (macOS, Linux, Windows)
**Project Type**: Single TUI application
**Performance Goals**: <500ms query execution, <100ms UI rendering, 30s auto-refresh
**Constraints**: Minimum terminal 80x24, readonly mode support, graceful extension fallback
**Scale/Scope**: Databases with hundreds of schemas/tables

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | PASS | 30s auto-refresh specified (FR-007), appropriate for static schema data |
| II. Keyboard-Driven Interface | PASS | Full keyboard navigation: j/k, Enter, Tab, s/S, P, h, y, v/a/r (FR-002, FR-006, FR-014, FR-015) |
| III. Query Efficiency | PASS | <500ms query target (FR-016), prepared statements pattern from existing views |
| IV. Incremental Delivery | PASS | Stories prioritized P1→P2→P3, each independently testable |
| V. Comprehensive Coverage | PASS | Covers table statistics, index usage, bloat detection - reduces tool switching |
| VI. Visual Design First | PENDING | Requires reference study, ASCII mockup, library validation before implementation |

**Gate Status**: CONDITIONAL PASS - Phase 0 must include visual design artifacts per Principle VI.

## Project Structure

### Documentation (this feature)

```text
specs/005-tables-statistics/
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
│   │   └── table.go           # NEW: Schema, Table, Index, Column, Constraint models
│   └── queries/
│       └── tables.go          # NEW: Table statistics queries
├── ui/
│   └── views/
│       └── tables/
│           ├── view.go        # NEW: TablesView implementing ViewModel
│           ├── tabs.go        # NEW: Tab definitions (Tables, Indexes, Details)
│           └── help.go        # NEW: Help overlay content
└── app/
    └── app.go                 # UPDATE: Register TablesView for key '5'
```

**Structure Decision**: Follows established pattern from `internal/ui/views/locks/` and `internal/ui/views/queries/`. Each view has its own package under `internal/ui/views/` with view.go, tabs.go, and help.go. Database queries in `internal/db/queries/` with models in `internal/db/models/`.

## Complexity Tracking

No violations requiring justification. Implementation follows established patterns.

## Phase 0: Research Outputs

### Visual Design Research (Constitution Principle VI)

**Reference Tools to Study**:
1. **pg_top** (`/Users/brandon/src/pg_top`) - PostgreSQL activity monitor, table statistics display
2. **htop** (`/Users/brandon/src/htop`) - Process viewer, tree hierarchy rendering
3. **k9s** (`/Users/brandon/src/k9s`) - Kubernetes TUI, resource browser with expand/collapse

**Research Tasks**:
1. Capture screenshots of pg_top table statistics display
2. Study htop tree rendering for schema→table hierarchy
3. Analyze k9s resource navigation patterns (expand/collapse, details panel)
4. Create ASCII mockup of Tables view layout
5. Build throwaway demos testing tree rendering approaches

### PostgreSQL Query Research

**Research Tasks**:
1. Optimal query for table statistics (pg_stat_all_tables + pg_statio_all_tables + size functions)
2. Partition hierarchy detection (pg_inherits)
3. pgstattuple extension detection and bloat calculation
4. Index usage statistics query patterns
5. System schema filtering (pg_catalog, information_schema, pg_toast)

### Bubbletea Pattern Research

**Research Tasks**:
1. Tree/hierarchy rendering in Bubbletea (existing solutions or custom)
2. Multi-panel layout (table list + details panel side-by-side or modal)
3. Existing patterns from locks/queries views for consistency

## Phase 1: Design Outputs

### Data Model (`data-model.md`)

Key entities extracted from spec:
- Schema (name, owner, is_system)
- Table (name, schema, total_size, heap_size, index_size, toast_size, row_count, bloat_pct, cache_hit_ratio, is_partitioned, parent_table_oid)
- Index (name, table_oid, size, scan_count, rows_read, cache_hit_ratio, is_unused)
- TableColumn (name, data_type, is_nullable, default_value, position)
- Constraint (name, type, definition)

### Contracts (`contracts/`)

Internal Go interfaces:
- TableDataProvider interface for database queries
- TablesViewModel interface methods (matching existing ViewModel pattern)

### Quickstart (`quickstart.md`)

Setup instructions for development and testing this feature.
