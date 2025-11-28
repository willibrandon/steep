# Implementation Plan: Log Viewer

**Branch**: `009-log-viewer` | **Date**: 2025-11-27 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/009-log-viewer/spec.md`

## Summary

Implement a real-time PostgreSQL log viewer TUI that displays server logs with severity color-coding, supports follow mode (auto-scroll), filtering by severity level, regex text search, and timestamp navigation. Leverages existing log parsing infrastructure (`LogCollector`, CSV/JSON parsers) and logging configuration detection (`CheckLoggingStatus`, `EnableLogging`).

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**: bubbletea, bubbles (viewport), lipgloss, pgx/pgxpool
**Storage**: PostgreSQL log files (file system read or pg_read_file()), position tracking via SQLite
**Testing**: go test, testcontainers for integration tests
**Target Platform**: macOS, Linux (terminal)
**Project Type**: Single Go module (existing Steep application)
**Performance Goals**: 1-second log refresh, <100ms filter response, smooth scrolling through 10,000 entries
**Constraints**: <10MB memory for log buffer (10,000 entries), read-only operation (no database writes)
**Scale/Scope**: Single view addition to existing 9-view application

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | ✅ PASS | 1-second refresh in follow mode (FR-003), configurable intervals |
| II. Keyboard-Driven Interface | ✅ PASS | vim-like navigation (j/k, g/G, /), `f` toggle, `:level` command |
| III. Query Efficiency (NON-NEGOTIABLE) | ✅ PASS | No database queries - reads log files directly; incremental file reads |
| IV. Incremental Delivery | ✅ PASS | P1 (real-time viewing) → P2 (filtering, search) → P3 (timestamp nav) |
| V. Comprehensive Coverage | ✅ PASS | Fills log monitoring gap; complements existing activity/query views |
| VI. Visual Design First (NON-NEGOTIABLE) | ⚠️ REQUIRED | Must complete reference study, ASCII mockup, and static mockup before implementation |

### Visual Design Checklist (Constitution VI)

- [ ] Reference study: Study `htop` (log-like output), `lnav` (log viewer), `k9s` (log streaming)
- [ ] ASCII mockup: Character-by-character layout in spec.md
- [ ] Library validation: Test viewport scrolling with color-coded content
- [ ] Visual acceptance criteria: Define in spec.md
- [ ] Static mockup: Implement with hardcoded log data before real-time integration

## Project Structure

### Documentation (this feature)

```text
specs/009-log-viewer/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (N/A - no API contracts)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/
├── ui/
│   └── views/
│       └── logs/                    # NEW: Log viewer view
│           ├── view.go              # Main view implementation
│           ├── keys.go              # Keyboard handling
│           ├── help.go              # Help text
│           └── render.go            # Rendering logic
├── monitors/
│   ├── log_parser.go                # EXTEND: Generic log entry parsing
│   ├── csv_log_parser.go            # REUSE: CSV format parsing
│   ├── json_log_parser.go           # REUSE: JSON format parsing
│   └── queries/
│       ├── monitor.go               # REUSE: CheckLoggingStatus(), EnableLogging()
│       └── log_collector.go         # EXTEND: File reading, position tracking
├── db/
│   └── models/
│       └── log_entry.go             # NEW: LogEntry model
└── storage/
    └── sqlite/
        └── log_positions.go         # EXTEND: Position persistence
```

**Structure Decision**: Follows existing Steep view architecture pattern. New view in `internal/ui/views/logs/` following the same structure as `activity/`, `queries/`, `tables/` views. Extends existing log parsing infrastructure rather than duplicating.

## Complexity Tracking

> No violations requiring justification - design follows existing patterns.

## Existing Infrastructure Analysis

### Components to Reuse

| Component | Location | Purpose |
|-----------|----------|---------|
| `LogCollector` | `internal/monitors/queries/log_collector.go` | File reading, position tracking, multi-line handling |
| `CSVLogParser` | `internal/monitors/csv_log_parser.go` | CSV format log parsing (column indices defined) |
| `JSONLogParser` | `internal/monitors/json_log_parser.go` | JSON format log parsing (JSONLogEntry struct) |
| `CheckLoggingStatus()` | `internal/monitors/queries/monitor.go` | Detect log_dir, log_pattern, logging_collector status |
| `EnableLogging()` | `internal/monitors/queries/monitor.go` | Enable logging via ALTER SYSTEM SET |
| `ModeConfirmEnableLogging` | `internal/ui/views/queries/view.go` | Dialog pattern for prompting logging enablement |
| `PositionStore` | `internal/monitors/queries/log_collector.go` | Interface for persisting file positions |
| `viewport` | `bubbles` library | Scrollable content rendering |

### Components to Create

| Component | Purpose |
|-----------|---------|
| `LogEntry` model | Unified log entry with severity, timestamp, PID, database, user, message |
| `LogsView` | Bubbletea view implementing ViewModel interface |
| `LogBuffer` | Ring buffer holding up to 10,000 parsed log entries |
| `LogFilter` | Filter state (severity level, search pattern, timestamp range) |
| Severity color styles | Red (ERROR), Yellow (WARNING), White (INFO), Gray (DEBUG) |

## Key Design Decisions

1. **Log Source Priority**: File system read preferred over pg_read_file() for performance
2. **Buffer Strategy**: Ring buffer with 10,000 entry limit, oldest entries evicted
3. **Format Detection**: Auto-detect CSV vs JSON vs stderr based on file extension and content
4. **Follow Mode**: Default ON, auto-scroll to newest, toggle with `f` key
5. **Filter Application**: Client-side filtering on buffered entries (fast, no re-read)
6. **Search Implementation**: Go regexp package for pattern matching, highlight matches
