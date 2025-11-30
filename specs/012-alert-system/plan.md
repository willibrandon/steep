# Implementation Plan: Alert System

**Branch**: `012-alert-system` | **Date**: 2025-11-30 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/012-alert-system/spec.md`

## Summary

Implement a threshold-based alert system for Steep that monitors PostgreSQL metrics (replication lag, connections, cache hit ratio, transaction duration) and provides visual indicators, history tracking, and acknowledgment capabilities. The system will evaluate configured alert rules on each refresh cycle, display active alerts in the Dashboard with color-coded severity, persist alert history to SQLite, and support alert acknowledgment for team coordination.

## Technical Context

**Language/Version**: Go 1.25.4 (per go.mod)
**Primary Dependencies**: bubbletea, bubbles, lipgloss, pgx/pgxpool, go-sqlite3, viper (config)
**Storage**: SQLite (~/.config/steep/steep.db) for alert history and acknowledgment persistence; YAML (~/.config/steep/config.yaml) for alert rule configuration
**Testing**: Go testing, testcontainers for integration tests
**Target Platform**: macOS, Linux terminals (80x24 minimum)
**Project Type**: Single TUI application
**Performance Goals**: <100ms alert evaluation per cycle, <500ms total refresh cycle
**Constraints**: Alert evaluation must not block UI rendering, history retention 30 days default
**Scale/Scope**: 10-50 configured alert rules, 100k+ historical alert events over 30 days

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Real-Time First | PASS | Alerts evaluate every refresh cycle (1-5s), state changes reflected immediately |
| II. Keyboard-Driven Interface | PASS | 'a' key for history, Enter for acknowledge, j/k navigation |
| III. Query Efficiency (NON-NEGOTIABLE) | PASS | Alert evaluation uses existing metrics (no additional DB queries), SQLite writes batched |
| IV. Incremental Delivery | PASS | P1 (config, visual, panel) -> P2 (history, ack) -> P3 (custom rules) |
| V. Comprehensive Coverage | PASS | Fills gap: no current alerting capability in Steep |
| VI. Visual Design First (NON-NEGOTIABLE) | REQUIRES ATTENTION | Need ASCII mockups and reference study before P1 implementation |

**Gate Status**: PASS with attention required for Principle VI during Phase 1 design.

## Project Structure

### Documentation (this feature)

```text
specs/012-alert-system/
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
├── alerts/                      # NEW: Alert system core
│   ├── config.go               # Alert configuration types
│   ├── engine.go               # Alert evaluation engine
│   ├── metrics.go              # Metric value extraction
│   ├── rule.go                 # Alert rule parsing and validation
│   └── state.go                # Alert state management
├── config/
│   ├── config.go               # MODIFY: Add AlertsConfig struct
│   └── alerts.go               # NEW: Alert config loading/validation
├── storage/sqlite/
│   ├── schema.go               # MODIFY: Add alert_events table
│   └── alert_store.go          # NEW: Alert history persistence
└── ui/
    ├── components/
    │   └── alert_panel.go      # NEW: Alert panel component
    ├── views/
    │   └── dashboard.go        # MODIFY: Integrate alert panel
    └── messages.go             # MODIFY: Add alert messages
```

**Structure Decision**: Follows existing Steep architecture with new `internal/alerts/` package for alert domain logic. Reuses existing SQLite storage pattern and Bubbletea component patterns.

## Complexity Tracking

No constitution violations requiring justification.
