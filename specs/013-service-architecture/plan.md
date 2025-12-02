# Implementation Plan: Service Architecture (steep-agent)

**Branch**: `013-service-architecture` | **Date**: 2025-12-01 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/013-service-architecture/spec.md`

## Summary

Implement a background daemon (steep-agent) that continuously collects PostgreSQL monitoring data and persists to SQLite, enabling 24/7 data collection independent of TUI runtime. The TUI automatically detects the agent and coordinates data collection, switching seamlessly between direct log collection and agent-provided data. Uses kardianos/service for cross-platform service management (Windows Services, systemd, launchd).

## Technical Context

**Language/Version**: Go 1.25.4 (per existing go.mod)
**Primary Dependencies**:
- kardianos/service (cross-platform service management)
- spf13/cobra (CLI framework, already in project)
- mattn/go-sqlite3 (SQLite with WAL mode, already in project)
- jackc/pgx/v5/pgxpool (PostgreSQL connection pooling, already in project)
- spf13/viper (configuration, already in project)

**Storage**: SQLite (~/.config/steep/steep.db) with WAL mode for concurrent access
**Testing**: go test with testcontainers for integration tests (existing pattern)
**Target Platform**: Windows, macOS, Linux (cross-platform service support)
**Project Type**: Single project with new entry point (cmd/steep-agent/)
**Performance Goals**:
- Agent 99.9% uptime over 7-day period
- TUI startup with agent detection < 500ms
- Graceful shutdown < 5 seconds
- PostgreSQL reconnection < 30 seconds after outage

**Constraints**:
- SQLite database size must remain stable with retention pruning
- Agent handles 5+ simultaneous TUI readers without degradation
- Multi-instance monitoring < 10% overhead per additional instance
- Existing monitors reused without modification

**Scale/Scope**:
- Single agent monitoring 1-N PostgreSQL instances
- Multiple TUI clients reading from shared SQLite
- Data retention configurable per data type (hours to days)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | ✅ PASS | Agent collects at configurable intervals (1-30s); TUI auto-detects agent and reads fresh SQLite data |
| II. Keyboard-Driven Interface | ✅ PASS | No UI changes to keyboard navigation; status bar addition follows existing patterns |
| III. Query Efficiency (NON-NEGOTIABLE) | ✅ PASS | Reuses existing optimized monitors; no new PostgreSQL queries |
| IV. Incremental Delivery | ✅ PASS | 8 user stories with P1/P2/P3 priorities; P1 delivers core agent value independently |
| V. Comprehensive Coverage | ✅ PASS | Extends existing monitoring with 24/7 collection; no feature gaps introduced |
| VI. Visual Design First (NON-NEGOTIABLE) | ✅ N/A | No new UI views; minimal status bar changes only |

**Gate Result**: PASS - All applicable principles satisfied. Visual Design First not applicable as this is primarily backend/daemon work with minimal UI changes (status bar indicator only).

## Project Structure

### Documentation (this feature)

```text
specs/013-service-architecture/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (CLI interface spec)
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
cmd/
├── steep/               # Existing TUI entry point (unchanged - agent detection in app.go)
└── steep-agent/         # NEW: Agent daemon entry point
    └── main.go          # Cobra CLI: install, uninstall, start, stop, restart, status, run

internal/
├── agent/               # NEW: Agent-specific code
│   ├── agent.go         # Main agent orchestration, lifecycle management
│   ├── collector.go     # Data collection coordinator, goroutine management
│   ├── service.go       # kardianos/service integration
│   ├── config.go        # Agent-specific configuration parsing
│   └── retention.go     # Data retention/pruning logic
├── app/
│   └── app.go           # Modified: add agent detection and collection coordination
├── monitors/            # REUSE: Existing monitors unchanged
├── storage/sqlite/      # REUSE: Existing SQLite stores unchanged
├── config/
│   └── config.go        # Modified: add agent section parsing
└── ui/components/
    └── statusbar.go     # Modified: add "Agent: Connected" indicator

tests/
├── integration/
│   └── agent/           # NEW: Agent integration tests
│       ├── lifecycle_test.go
│       ├── collection_test.go
│       └── multiinstance_test.go
└── unit/
    └── agent/           # NEW: Agent unit tests
        ├── retention_test.go
        └── config_test.go
```

**Structure Decision**: Single project with new entry point. Agent code isolated in `internal/agent/` package. Existing monitors and SQLite stores reused without modification. Minimal changes to existing TUI code (flag parsing, agent detection, status bar).

## Complexity Tracking

> No violations requiring justification. Architecture reuses existing components and follows established patterns.

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| Service library | kardianos/service | Battle-tested, supports all target platforms, MIT license |
| IPC mechanism | SQLite as shared state | No additional IPC complexity; WAL mode already enabled |
| Multi-instance | Single agent, N connections | Simpler than N agents; single SQLite writer |
