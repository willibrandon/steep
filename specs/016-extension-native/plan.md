# Implementation Plan: Extension-Native Architecture

**Branch**: `016-extension-native` | **Date**: 2025-12-08 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/016-extension-native/spec.md`

## Summary

Migrate steep-repl daemon services into the PostgreSQL extension to eliminate the daemon dependency. When PostgreSQL is up, steep_repl is up. The CLI connects directly via PostgreSQL protocol, using background workers for long-running operations and LISTEN/NOTIFY for real-time progress.

## Technical Context

**Language/Version**: Rust (pgrx 0.16.1) for extension, Go 1.25.4 for CLI
**Primary Dependencies**: pgrx (background workers, shared memory, SPI), pgx/v5 (CLI database access)
**Storage**: PostgreSQL 18+ (steep_repl schema tables, shared memory for progress)
**Testing**: cargo pgrx test for extension, go test + testcontainers for CLI
**Target Platform**: PostgreSQL 18+ on Linux/macOS
**Project Type**: Multi-component (Rust extension + Go CLI)
**Performance Goals**: Progress updates visible within 1 second (SC-003), operations on 100GB+ databases (SC-002)
**Constraints**: Background workers require `shared_preload_libraries` configuration and PostgreSQL restart
**Scale/Scope**: Snapshot operations for databases up to terabytes; single background worker per operation type

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | PASS | Progress via shared memory + LISTEN/NOTIFY within 1 second |
| II. Keyboard-Driven Interface | N/A | Backend feature, no UI changes |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | Uses background workers to avoid blocking; work queue with index on pending status |
| IV. Incremental Delivery | PASS | P1 stories (US1, US2) deliver core value; P2/P3 are enhancements |
| V. Comprehensive Coverage | PASS | Covers all existing daemon operations |
| VI. Visual Design First (NON-NEGOTIABLE) | N/A | Backend feature, no UI changes |

**Gate Result**: PASS - All applicable principles satisfied.

## Project Structure

### Documentation (this feature)

```text
specs/016-extension-native/
├── plan.md              # This file
├── research.md          # Phase 0: pgrx background workers, shared memory patterns
├── data-model.md        # Phase 1: work_queue, progress tables
├── quickstart.md        # Phase 1: How to use direct mode
├── contracts/           # Phase 1: SQL function signatures
└── tasks.md             # Phase 2: Task breakdown (/speckit.tasks)
```

### Source Code (repository root)

```text
extensions/steep_repl/src/
├── lib.rs                    # Extension entry point (add _PG_init bgworker registration)
├── worker.rs                 # NEW: Background worker main loop
├── work_queue.rs             # NEW: Work queue table and functions
├── progress.rs               # NEW: Shared memory progress tracking
├── snapshot_worker.rs        # NEW: Snapshot generate/apply implementation
├── notify.rs                 # NEW: LISTEN/NOTIFY helpers
├── snapshots.rs              # MODIFY: Add start_snapshot, snapshot_progress functions
├── merge.rs                  # MODIFY: Add start_merge, analyze_overlap functions
├── nodes.rs                  # MODIFY: Add register_node, heartbeat functions
└── health.rs                 # NEW: steep_repl.health() function

cmd/steep-repl/
├── cmd_snapshot.go           # MODIFY: Add --direct flag, direct PostgreSQL execution
├── cmd_schema.go             # MODIFY: Add --direct flag
├── cmd_node.go               # MODIFY: Add --direct flag
├── cmd_merge.go              # MODIFY: Add --direct flag
└── direct/                   # NEW: Direct mode execution package
    ├── executor.go           # Direct PostgreSQL execution
    ├── progress.go           # LISTEN/NOTIFY progress handling
    └── detector.go           # Auto-detection logic (FR-012)

internal/repl/
├── direct/                   # NEW: Direct mode shared logic
│   ├── client.go             # PostgreSQL direct client
│   └── progress.go           # Progress notification parser
└── init/
    └── snapshot_generate.go  # MODIFY: Extract core logic for extension reuse

tests/integration/repl/
├── direct_test.go            # NEW: Direct mode integration tests
└── background_worker_test.go # NEW: Background worker tests
```

**Structure Decision**: Extends existing multi-component architecture. Extension gains background worker, shared memory, and SQL function API. CLI gains direct mode alongside existing gRPC mode.

## Complexity Tracking

No constitution violations to justify.

