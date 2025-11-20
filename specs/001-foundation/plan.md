# Implementation Plan: Foundation Infrastructure

**Branch**: `001-foundation` | **Date**: 2025-11-19 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-foundation/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Build the foundation infrastructure for Steep, a PostgreSQL monitoring TUI application. This feature establishes the core application architecture including Bubbletea-based terminal UI initialization, PostgreSQL connection management with pgxpool, YAML configuration loading with Viper, keyboard-driven navigation framework, and reusable UI components (Table, StatusBar, HelpText) styled with Lipgloss. The foundation enables launching the application, connecting to PostgreSQL databases, navigating with keyboard shortcuts, and viewing connection status in real-time. This is the base upon which all future monitoring features will be built.

## Technical Context

**Language/Version**: Go 1.21+
**Primary Dependencies**:
- bubbletea v0.25+ (TUI framework)
- bubbles v0.18+ (reusable TUI components)
- lipgloss v0.9+ (styling and layout)
- pgx/v5 (PostgreSQL driver)
- pgxpool/v5 (connection pooling)
- viper v1.18+ (configuration management)

**Storage**: PostgreSQL 11+ (target 18) for monitoring; YAML file for configuration (~/.config/steep/config.yaml)
**Testing**: Go testing with testcontainers for integration tests against real PostgreSQL instances
**Target Platform**: Cross-platform (Linux, macOS, Windows with WSL), terminal emulators with 256-color support (xterm-256color+)
**Project Type**: Single binary CLI application
**Performance Goals**:
- Application launch < 1 second
- Keyboard response < 100ms
- Query execution < 500ms
- View switching < 100ms

**Constraints**:
- Memory footprint < 10MB idle
- Minimum terminal size 80x24
- Single database connection per instance
- No file-based logging (stderr only)

**Scale/Scope**:
- Single-user desktop application
- Connection pool: 5-10 connections typical
- Support 100+ active PostgreSQL connections being monitored
- Minimal resource overhead for production monitoring

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### I. Real-Time First
✅ **PASS**: Feature spec defines status bar auto-refresh every 1 second (FR-010, SC-009) and real-time connection status display (User Story 3). Foundation establishes refresh mechanism for future monitoring views.

### II. Keyboard-Driven Interface
✅ **PASS**: All functionality accessible via keyboard (FR-007): q (quit), h/? (help), Esc (close dialog), Tab/Shift+Tab (switch views), 1-9 (jump to view). No mouse interaction required (User Story 2).

### III. Query Efficiency (NON-NEGOTIABLE)
✅ **PASS**: Uses connection pooling with pgxpool (FR-004), simple validation query on startup (FR-005), single basic metric query for status bar (FR-020). No complex queries in foundation layer. Query performance validated < 500ms (SC success criteria).

### IV. Incremental Delivery
✅ **PASS**: Feature has 4 prioritized user stories (3 P1, 1 P2). Each story independently testable and deliverable. Foundation provides standalone value (connection + navigation) before advanced monitoring features.

### V. Comprehensive Coverage
✅ **PASS**: Foundation establishes infrastructure for future comprehensive coverage. Implements view switching framework (FR-016) and placeholder dashboard (FR-017) to enable future monitoring views.

### Technical Standards - PostgreSQL Compatibility
✅ **PASS**: Supports PostgreSQL 11+ with target 18 (Assumptions section). No extension dependencies in foundation layer. Version detection planned via SELECT version() query (FR-005).

### Technical Standards - Bubbletea Architecture
✅ **PASS**: Component reusability enforced (FR-009, FR-010, FR-011 define reusable Table, StatusBar, HelpText components). View isolation via ViewModel interface (FR-016). Centralized styles module (FR-012). Message passing via Bubbletea tea.Msg pattern.

### Technical Standards - Go Conventions
✅ **PASS**: Error handling specified (FR-014 actionable error messages). Concurrency via connection pool goroutines. Testing with testcontainers for integration tests (Technical Context).

### Technical Standards - Security & Credentials
✅ **PASS**: No password storage in config files (FR-003, password_command only). SSL/TLS support (FR-018). Environment variable support (FR-015). Interactive password prompt fallback (FR-025).

**Gate Status**: ✅ ALL PASSED - Proceed to Phase 0

## Project Structure

### Documentation (this feature)

```text
specs/001-foundation/
├── plan.md              # This file (/speckit.plan command output)
├── spec.md              # Feature specification
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── config-schema.yaml  # YAML configuration schema
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
steep/                      # Repository root
├── cmd/
│   └── steep/             # Main application entry point
│       └── main.go        # Application bootstrap
├── internal/              # Private application code
│   ├── app/              # Application orchestration
│   │   └── app.go        # Main Bubbletea model and lifecycle
│   ├── config/           # Configuration management
│   │   ├── config.go     # Config loading with Viper
│   │   └── defaults.go   # Default configuration values
│   ├── db/               # Database connection management
│   │   ├── connection.go # pgxpool connection setup
│   │   ├── password.go   # Password command execution
│   │   └── models/       # Data models for connection profiles
│   │       └── profile.go
│   └── ui/               # Bubbletea UI components
│       ├── components/   # Reusable UI components
│       │   ├── table.go     # Table component with sorting
│       │   ├── statusbar.go # Status bar component
│       │   └── help.go      # Help text component
│       ├── views/        # View implementations
│       │   └── dashboard.go # Placeholder dashboard view
│       ├── styles/       # Lipgloss styles
│       │   └── theme.go     # Color scheme and spacing
│       └── keys.go       # Keyboard bindings
├── configs/              # Default configuration files
│   └── steep.yaml.example # Example configuration
├── tests/                # Test files
│   ├── integration/      # Integration tests with testcontainers
│   │   └── connection_test.go
│   └── unit/            # Unit tests
│       └── config_test.go
├── go.mod               # Go module definition
├── go.sum               # Go dependencies checksum
├── Makefile             # Build and test automation
└── README.md            # Project documentation (to be created)
```

**Structure Decision**: Selected single project structure (Option 1) as this is a standalone CLI application. The `internal/` directory follows Go conventions for private application code, with clear separation between application orchestration (`app/`), configuration management (`config/`), database connectivity (`db/`), and UI components (`ui/`). Tests are organized by type (unit vs integration) in the `tests/` directory. The `cmd/steep/` directory contains only the main entry point, delegating to `internal/app/` for application logic.

## Complexity Tracking

**Not applicable** - All constitution gates passed without violations requiring justification.

