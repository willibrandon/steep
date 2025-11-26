# Implementation Plan: SQL Editor & Execution

**Branch**: `007-sql-editor` | **Date**: 2025-11-25 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/007-sql-editor/spec.md`

## Summary

Implement an interactive SQL editor view (accessible via '7' key) that enables DBAs to write multi-line SQL queries, execute them against the connected database, view paginated results, manage transactions, and access query history. The view follows existing Steep patterns using bubbles/textarea for input and bubbles/table+viewport for results display.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**:
- `github.com/charmbracelet/bubbles` v0.21.1+ (textarea, table, viewport)
- `github.com/charmbracelet/bubbletea` v1.3.10 (TUI framework)
- `github.com/charmbracelet/lipgloss` v1.1.0 (styling)
- `github.com/alecthomas/chroma/v2` v2.20.0 (SQL syntax highlighting, already used in explain.go)
- `github.com/jackc/pgx/v5` v5.7.6 (PostgreSQL driver)
- `github.com/mattn/go-sqlite3` v1.14.32 (history/snippets persistence)

**Storage**:
- PostgreSQL (query execution target via existing connection pool)
- SQLite (`~/.config/steep/query_history.db` for history, `~/.config/steep/snippets.yaml` for snippets)

**Testing**:
- Unit tests for business logic (query parsing, history management, export formatting)
- Integration tests with testcontainers for query execution
- Manual UI testing checklist

**Target Platform**: Terminal (macOS, Linux) - minimum 80x24 terminal size

**Project Type**: Single project (Go application)

**Performance Goals**:
- Query execution < 500ms for typical monitoring queries
- Results display < 100ms for result sets under 1000 rows
- History recall < 50ms for 100 stored queries
- UI rendering at 60 FPS without lag

**Constraints**:
- Query timeout default 30 seconds (configurable)
- In-memory history buffer: 100 queries
- Results pagination: 100 rows per page

**Scale/Scope**: Single-user TUI application, single database connection at a time

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence/Justification |
|-----------|--------|------------------------|
| I. Real-Time First | PASS | SQL Editor is interactive (on-demand query execution), not real-time monitoring. Transaction state updates reflect immediately in status bar. |
| II. Keyboard-Driven Interface | PASS | All functionality via keyboard: Ctrl+Enter execute, Tab focus switch, j/k navigation, vim-style shortcuts. Mouse optional. |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | User queries go through existing pgxpool. System queries (history, snippets) use prepared statements with limits. |
| IV. Incremental Delivery | PASS | P1 (editor+results) → P2 (highlighting, transactions, keyboard) → P3 (history, snippets, export). Each increment independently testable. |
| V. Comprehensive Coverage | PASS | SQL Editor fills gap for ad-hoc query execution within Steep, reducing need to switch to psql/pgcli. |
| VI. Visual Design First (NON-NEGOTIABLE) | PASS | Reference study: pgcli, DataGrip, VS Code SQL extensions. ASCII mockup in spec.md. Static mockup implementation before real-time data. |

## Project Structure

### Documentation (this feature)

```text
specs/007-sql-editor/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal interfaces)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/ui/views/sqleditor/
├── view.go              # Main SQLEditorView implementing ViewModel
├── editor.go            # Textarea wrapper with focus management
├── results.go           # Results table with pagination
├── statusbar.go         # Connection info + transaction state
├── history.go           # Query history management (in-memory + SQLite)
├── snippets.go          # Saved query snippet management (YAML)
├── highlight.go         # Chroma-based SQL syntax highlighting
├── export.go            # CSV/JSON export functionality
├── transaction.go       # Transaction state management
├── help.go              # Help overlay content
└── keys.go              # Key bindings definition

internal/ui/views/types.go     # Add ViewSQLEditor constant
internal/ui/styles/sqleditor.go # SQL Editor specific styles

internal/db/queries/
└── sqleditor.go         # Query execution with timeout/cancellation

tests/
├── integration/sqleditor_test.go
└── unit/
    ├── history_test.go
    ├── export_test.go
    └── highlight_test.go
```

**Structure Decision**: Follows existing Steep view patterns (see `internal/ui/views/replication/`, `internal/ui/views/queries/`). Each view has its own subdirectory with focused files for major subsystems. Styles go in `internal/ui/styles/` per constitution.

## Complexity Tracking

> No constitution violations requiring justification.
