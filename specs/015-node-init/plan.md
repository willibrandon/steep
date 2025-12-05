# Implementation Plan: Node Initialization & Snapshots

**Branch**: `015-node-init` | **Date**: 2025-12-04 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/015-node-init/spec.md`

## Summary

Implement node initialization and snapshot management for Steep bidirectional replication. This includes automatic snapshot initialization (copy_data=true), manual initialization from user backups (pg_dump/pg_basebackup), two-phase snapshot workflow (generate/apply), reinitialization for diverged nodes, schema fingerprinting for drift detection, and progress tracking in TUI. The feature extends the existing steep-repl daemon with initialization orchestration and adds PostgreSQL extension functions for schema fingerprinting.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod), Rust + pgrx 0.16.1 (PostgreSQL extension)
**Primary Dependencies**: pgx/pgxpool (database), bubbletea/bubbles/lipgloss (TUI), grpc-go/protobuf (daemon communication), viper (config)
**Storage**: PostgreSQL 18 (steep_repl schema - nodes, coordinator_state, audit_log + new tables), YAML config
**Testing**: go test, testcontainers-go (integration), pgrx pg_test (extension)
**Target Platform**: Linux, macOS, Windows (cross-platform daemon and TUI)
**Project Type**: CLI/TUI application with background daemon and PostgreSQL extension
**Performance Goals**: 10GB database initialization < 30 minutes, progress updates < 2s latency, schema fingerprinting < 1s for 1000 tables
**Constraints**: Query execution < 500ms, no unbounded result sets, PG18 required for parallel COPY features
**Scale/Scope**: Multi-TB databases supported via manual initialization, parallel workers configurable 1-16

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | PASS | Progress updates within 2s refresh interval |
| II. Keyboard-Driven Interface | PASS | TUI progress overlay with C to cancel, navigation in Nodes view |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | Schema fingerprinting uses indexed system catalog queries, bounded result sets |
| IV. Incremental Delivery | PASS | 8 user stories prioritized P1-P3, each independently testable |
| V. Comprehensive Coverage | PASS | Covers full initialization lifecycle: snapshot, manual, reinit, merge |
| VI. Visual Design First (UI Features) | PENDING | Progress overlay requires mockup before implementation |

**Gate Result**: PASS (pending visual design for TUI overlay in Phase 1)

## Project Structure

### Documentation (this feature)

```text
specs/015-node-init/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (gRPC proto, CLI commands)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
# Daemon (steep-repl) - extend existing structure
internal/repl/
├── config/
│   └── config.go           # Add initialization config section
├── init/                   # NEW: Initialization orchestration
│   ├── manager.go          # InitManager coordinates snapshots
│   ├── snapshot.go         # Snapshot generation and application
│   ├── manual.go           # Manual init prepare/complete workflow
│   ├── reinit.go           # Reinitialization (partial/full)
│   ├── progress.go         # Progress tracking and events
│   └── schema.go           # Schema comparison (calls extension)
├── models/
│   ├── node.go             # Add init_state field (8 states)
│   ├── snapshot.go         # NEW: Snapshot manifest model
│   ├── progress.go         # NEW: InitProgress model
│   └── fingerprint.go      # NEW: SchemaFingerprint model
├── grpc/
│   └── proto/
│       └── repl.proto      # Add Init service RPCs
└── ipc/
    └── handlers.go         # Add init command handlers

# PostgreSQL Extension - extend existing
extensions/steep_repl/src/
└── lib.rs                  # Add schema_fingerprints table, compute_fingerprint(), compare_fingerprints()

# CLI commands
cmd/steep-repl/
└── main.go                 # Add init, reinit subcommands

# TUI - extend existing views
internal/ui/
├── views/
│   └── replication.go      # Add Nodes view with init state column
└── components/
    └── progress.go         # NEW: Progress overlay component

# Tests
tests/integration/repl/
├── init_test.go            # NEW: Integration tests for initialization
├── snapshot_test.go        # NEW: Snapshot generation/application tests
└── schema_test.go          # NEW: Schema fingerprinting tests
```

**Structure Decision**: Extends existing steep-repl daemon and TUI architecture. New `internal/repl/init/` package encapsulates initialization logic. PostgreSQL extension gets new tables and functions for schema fingerprinting. TUI gets progress overlay component for initialization monitoring.

## Complexity Tracking

> No violations requiring justification. Design follows existing patterns.

| Aspect | Approach | Rationale |
|--------|----------|-----------|
| State machine | Enum in nodes table | Matches existing NodeStatus pattern |
| Progress tracking | PostgreSQL table + gRPC streaming | Daemon-owned state, TUI subscribes |
| Schema fingerprinting | Extension SQL function | Runs in-database for performance |

## Constitution Re-Check (Post Phase 1 Design)

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | PASS | gRPC streaming for progress, 1-2s poll interval |
| II. Keyboard-Driven Interface | PASS | C=cancel, I=init, R=reinit, D=details in TUI |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | Fingerprint queries use information_schema indexes, bounded by table count |
| IV. Incremental Delivery | PASS | P1 stories (auto/manual init, progress) can ship independently |
| V. Comprehensive Coverage | PASS | All init scenarios covered: snapshot, manual, reinit, merge |
| VI. Visual Design First (UI Features) | ACTION | Progress overlay mockup in spec.md (from BIDIRECTIONAL_REPLICATION.md Section 6.7) |

**Post-Design Gate Result**: PASS

Visual design requirement satisfied by existing ASCII mockups in BIDIRECTIONAL_REPLICATION.md Section 6.7:
- Node Initialization progress overlay with phases, progress bars, ETA
- Nodes view with state column
- Snapshot generation/application progress views

## Phase 1 Artifacts Generated

| Artifact | Path | Description |
|----------|------|-------------|
| Research | [research.md](./research.md) | Technical decisions for 8 topics |
| Data Model | [data-model.md](./data-model.md) | 5 entities, migrations, Go models |
| gRPC Proto | [contracts/init.proto](./contracts/init.proto) | InitService with 10 RPCs |
| CLI Commands | [contracts/cli-commands.md](./contracts/cli-commands.md) | init, reinit, snapshot, schema |
| Quickstart | [quickstart.md](./quickstart.md) | 5 initialization scenarios |

## Next Steps

Run `/speckit.tasks` to generate the implementation task breakdown.
