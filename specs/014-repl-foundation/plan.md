# Implementation Plan: Bidirectional Replication Foundation

**Branch**: `014-repl-foundation` | **Date**: 2025-12-04 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/014-repl-foundation/spec.md`

## Summary

Build the foundational infrastructure for Steep's bidirectional replication system:
1. **steep_repl PostgreSQL extension** (Rust/pgrx) - Creates schema tables for nodes, coordinator_state, and audit_log
2. **steep-repl daemon** (Go) - Cross-platform service with PostgreSQL connectivity, IPC, gRPC, and HTTP health endpoints

This is infrastructure-only; no replication logic is implemented in this feature.

## Technical Context

**Language/Version**:
- Rust (latest stable) for PostgreSQL extension via pgrx
- Go 1.21+ for daemon (matches existing steep codebase)

**Primary Dependencies**:
- pgrx (Rust PostgreSQL extension framework)
- kardianos/service (cross-platform service management)
- pgx/v5 (PostgreSQL driver with connection pooling)
- Microsoft/go-winio (Windows named pipes)
- grpc-go (node-to-node communication)
- crypto/tls (mTLS implementation)

**Storage**: PostgreSQL 18 (steep_repl schema tables)

**Testing**:
- cargo test (Rust extension unit tests)
- go test (Go daemon unit tests)
- testcontainers (integration tests with real PostgreSQL 18)

**Target Platform**: Windows (primary), Linux, macOS (cross-platform)

**Project Type**: Multi-component (Rust extension + Go daemon)

**Performance Goals**:
- Daemon startup: < 5 seconds to PostgreSQL connection
- IPC response: < 1 second
- gRPC health check: < 5 seconds
- HTTP health endpoint: < 100 milliseconds

**Constraints**:
- PostgreSQL 18 required (DDL replication, sequence sync features)
- mTLS required for node-to-node gRPC
- Platform-native logging (Event Log, syslog, os_log)

**Scale/Scope**:
- 2-10 nodes in typical cluster
- Audit log retention: configurable (default 2 years per design doc)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Applicability | Status | Notes |
|-----------|---------------|--------|-------|
| I. Real-Time First | Partial | PASS | Daemon monitors PostgreSQL connectivity in real-time; TUI integration provides live status |
| II. Keyboard-Driven Interface | N/A | PASS | This feature is infrastructure (daemon + extension); no direct UI. TUI integration is P2. |
| III. Query Efficiency | Applicable | PASS | Extension tables have defined indexes (FR-005). Daemon uses prepared statements via pgx. |
| IV. Incremental Delivery | Applicable | PASS | 6 user stories with P1/P2/P3 prioritization. Extension and daemon can be deployed independently. |
| V. Comprehensive Coverage | Partial | PASS | Foundation for bidirectional replication coverage; actual replication features in later features. |
| VI. Visual Design First | N/A | PASS | No UI components in this feature. TUI status bar integration (P2) uses existing patterns. |

**Gate Status**: PASSED - No violations. This is an infrastructure feature with no UI components requiring visual design.

## Project Structure

### Documentation (this feature)

```text
specs/014-repl-foundation/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (gRPC proto, IPC messages)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
# PostgreSQL Extension (Rust/pgrx)
extensions/
└── steep_repl/
    ├── Cargo.toml
    ├── src/
    │   └── lib.rs           # Extension entry point, schema creation
    └── sql/
        └── steep_repl--0.1.0.sql  # Extension SQL definitions

# Go Daemon
cmd/
└── steep-repl/
    └── main.go              # Daemon entry point

internal/
└── repl/
    ├── config/
    │   └── config.go        # Configuration loading
    ├── daemon/
    │   └── daemon.go        # Main daemon orchestration
    ├── db/
    │   └── pool.go          # PostgreSQL connection pooling
    ├── ipc/
    │   ├── listener.go      # Cross-platform IPC (named pipes/Unix sockets)
    │   └── messages.go      # IPC message types
    ├── grpc/
    │   ├── server.go        # gRPC server with mTLS
    │   ├── client.go        # gRPC client for node communication
    │   └── proto/
    │       └── repl.proto   # gRPC service definitions
    └── health/
        └── http.go          # HTTP health endpoint

# Tests
tests/
├── integration/
│   └── repl/
│       ├── extension_test.go    # Extension installation tests
│       └── daemon_test.go       # Daemon integration tests
└── unit/
    └── repl/
        └── ...                  # Unit tests mirroring internal/repl/
```

**Structure Decision**: Multi-component structure with separate `extensions/` directory for Rust code and `internal/repl/` for Go daemon code. Follows existing Steep patterns (`cmd/steep-agent/`, `internal/agent/`).

## Complexity Tracking

> No violations detected - section intentionally empty.

## Post-Design Constitution Re-Check

*Re-evaluated after Phase 1 design artifacts completed.*

| Principle | Post-Design Status | Verification |
|-----------|-------------------|--------------|
| I. Real-Time First | PASS | Daemon provides real-time status via IPC; TUI can poll for live updates |
| II. Keyboard-Driven | N/A | No UI in this feature |
| III. Query Efficiency | PASS | data-model.md defines indexes for all query patterns; pgx uses prepared statements |
| IV. Incremental Delivery | PASS | Extension and daemon are independently deployable; P1→P2→P3 story ordering preserved |
| V. Comprehensive Coverage | PASS | Foundation for full replication feature set |
| VI. Visual Design First | N/A | No UI components |

**Post-Design Gate Status**: PASSED

## Generated Artifacts

| Artifact | Path | Description |
|----------|------|-------------|
| research.md | specs/014-repl-foundation/research.md | Technology decisions and patterns |
| data-model.md | specs/014-repl-foundation/data-model.md | PostgreSQL schema and Go types |
| repl.proto | specs/014-repl-foundation/contracts/repl.proto | gRPC service definitions |
| ipc-messages.md | specs/014-repl-foundation/contracts/ipc-messages.md | IPC JSON protocol |
| http-health.md | specs/014-repl-foundation/contracts/http-health.md | HTTP health endpoint spec |
| quickstart.md | specs/014-repl-foundation/quickstart.md | Build and installation guide |
