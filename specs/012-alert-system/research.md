# Research: Alert System

**Feature**: 012-alert-system
**Date**: 2025-11-30

## Research Summary

All technical decisions resolved. No NEEDS CLARIFICATION items remain.

---

## 1. Alert Configuration Format

**Decision**: Use YAML configuration with Viper integration, consistent with existing Steep config patterns.

**Rationale**:
- Steep already uses Viper for configuration loading from `~/.config/steep/config.yaml`
- Users familiar with existing config format (connection, ui, queries sections)
- YAML provides readable, hierarchical configuration
- Viper handles environment variable overrides automatically

**Alternatives Considered**:
- Separate alert config file: Rejected - adds complexity, users expect single config file
- JSON: Rejected - YAML is more readable for complex nested structures
- TOML: Rejected - not consistent with existing Steep patterns

**Configuration Structure**:
```yaml
alerts:
  enabled: true
  refresh_interval: 5s  # How often to evaluate (defaults to UI refresh)
  history_retention: 30d  # How long to keep alert history

  rules:
    - name: high_replication_lag
      metric: replication_lag_bytes
      warning: 104857600   # 100MB
      critical: 524288000  # 500MB
      enabled: true

    - name: connection_saturation
      metric: active_connections / max_connections
      warning: 0.8
      critical: 0.95
      enabled: true

    - name: low_cache_hit
      metric: cache_hit_ratio
      operator: "<"  # Default is ">"
      warning: 0.95
      critical: 0.90
      enabled: true

    - name: long_transaction
      metric: longest_transaction_seconds
      warning: 300   # 5 minutes
      critical: 900  # 15 minutes
      enabled: true
```

---

## 2. Alert Evaluation Engine

**Decision**: Implement a goroutine-based evaluator that receives metric updates via Bubbletea messages.

**Rationale**:
- Matches existing Steep architecture (monitors run in goroutines, send messages to UI)
- Non-blocking evaluation allows UI to remain responsive
- Can reuse existing metrics from `MetricsDataMsg` without additional DB queries

**Design**:
```go
// Engine evaluates alert rules against current metrics
type Engine struct {
    rules    []Rule
    states   map[string]*State  // keyed by rule name
    store    AlertStore         // for persistence
    mu       sync.RWMutex
}

// Evaluate checks all rules against current metric values
// Returns list of state changes (for UI notification)
func (e *Engine) Evaluate(metrics MetricValues) []StateChange

// StateChange represents a transition (e.g., Normal -> Warning)
type StateChange struct {
    RuleName    string
    PrevState   AlertState
    NewState    AlertState
    MetricValue float64
    Timestamp   time.Time
}
```

**Alternatives Considered**:
- Separate evaluation goroutine: Rejected - adds complexity, metrics already arrive via messages
- Polling-based: Rejected - Steep already pushes metrics via messages

---

## 3. Metric Value Extraction

**Decision**: Extract metrics from existing `models.Metrics` struct plus pg_stat_activity data.

**Rationale**:
- Steep already collects all required metrics in the refresh cycle
- No additional database queries needed
- Performance requirement (<100ms) easily met

**Available Metrics** (from existing code):
```go
// From models.Metrics (internal/db/models/metrics.go)
type Metrics struct {
    ActiveConnections      int
    MaxConnections        int
    CacheHitRatio         float64
    TransactionsPerSecond float64
    // ... more fields
}

// From pg_stat_activity (already queried)
// - longest_transaction_seconds (calculated from xact_start)
// - idle_in_transaction_seconds (calculated from state_change)

// From pg_stat_replication (existing replication monitor)
// - replication_lag_bytes (byte_lag field)
```

**Metric Name Mapping**:
| Config Name | Source | Expression |
|-------------|--------|------------|
| `active_connections` | Metrics.ActiveConnections | direct |
| `max_connections` | Metrics.MaxConnections | direct |
| `cache_hit_ratio` | Metrics.CacheHitRatio | direct |
| `replication_lag_bytes` | ReplicationData.ByteLag | max across replicas |
| `longest_transaction_seconds` | pg_stat_activity | calculated |
| `idle_in_transaction_seconds` | pg_stat_activity | calculated |
| `tps` | Metrics.TransactionsPerSecond | direct |

---

## 4. Alert History Persistence

**Decision**: Add `alert_events` table to existing SQLite database (`~/.config/steep/steep.db`).

**Rationale**:
- Reuses existing SQLite infrastructure
- Consistent with other history tables (query_history, replication_lag_history)
- Allows 30-day retention with automatic pruning

**Schema**:
```sql
CREATE TABLE IF NOT EXISTS alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_name TEXT NOT NULL,
    prev_state TEXT NOT NULL,  -- 'normal', 'warning', 'critical'
    new_state TEXT NOT NULL,
    metric_value REAL NOT NULL,
    threshold_value REAL,
    triggered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    acknowledged_at DATETIME,
    acknowledged_by TEXT  -- Optional: user/hostname
);

CREATE INDEX IF NOT EXISTS idx_alert_events_triggered ON alert_events(triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_rule ON alert_events(rule_name, triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_state ON alert_events(new_state, triggered_at DESC);
```

**Alternatives Considered**:
- Separate SQLite database: Rejected - unnecessary fragmentation
- In-memory only: Rejected - spec requires persistence across restarts

---

## 5. Visual Design Approach

**Decision**: Follow Constitution Principle VI - complete visual design before implementation.

**Required Before Implementation**:
1. **Reference Study**: Study alert displays in pg_top, htop, k9s for indicator patterns
2. **ASCII Mockup**: Create character-level mockup for Dashboard alert panel
3. **Static Demo**: Build throwaway demo testing alert panel rendering

**Preliminary Design Concepts** (to be validated):

Status Bar Alert Counts:
```
┌─ steep: postgres@localhost:5432/postgres ──────────────────────────────────────────────────────────── 2025-11-30 14:32:05 ─┐
│                                                                                          ⚠ 2 Warnings  ❌ 1 Critical        │
```

Dashboard Alert Panel (when alerts active):
```
┌─ Active Alerts ─────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ ❌ CRITICAL  connection_saturation    active/max: 96% > 95%                                          [Enter] Acknowledge   │
│ ⚠  WARNING   high_replication_lag     lag: 150MB > 100MB                                             15m ago               │
│ ⚠  WARNING   low_cache_hit            ratio: 94.2% < 95%                                             3m ago                │
└─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

Alert History View ('a' key overlay):
```
┌─ Alert History ──────────────────────────────────────────────────────────────────────────────────────────── [Esc] Close ────┐
│                                                                                                                             │
│ Timestamp            Rule                    State Change           Value        Ack                                       │
│ ─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────── │
│ 2025-11-30 14:30:05  connection_saturation   warning → critical     96%          ○                                         │
│ 2025-11-30 14:28:12  high_replication_lag    normal → warning       150MB        ●                                         │
│ 2025-11-30 14:25:00  low_cache_hit           normal → warning       94.2%        ○                                         │
│ 2025-11-30 14:20:33  connection_saturation   normal → warning       82%          ●                                         │
│                                                                                                                             │
│ [j/k] Navigate  [Enter] Acknowledge  [Esc] Close                                                                            │
└─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Expression Parsing for Custom Rules

**Decision**: Support simple arithmetic expressions with basic operators.

**Rationale**:
- Spec requires ratio-based conditions (e.g., `active_connections / max_connections`)
- Keep parser simple - no need for full expression language
- Can use Go's `go/ast` or simple tokenizer

**Supported Expressions**:
```
metric_name                    # Simple threshold
metric1 / metric2              # Ratio
metric1 - metric2              # Difference
metric1 * constant             # Scaled value
```

**Implementation Approach**:
```go
// Rule represents a parsed alert rule
type Rule struct {
    Name       string
    Expression Expression  // Parsed expression tree
    Operator   Operator    // >, <, >=, <=, ==, !=
    Warning    float64
    Critical   float64
    Enabled    bool
}

// Expression can be:
// - MetricRef{name: "active_connections"}
// - BinaryOp{left: MetricRef, op: "/", right: MetricRef}
// - Constant{value: 100}
```

**Alternatives Considered**:
- Full expression parser (govaluate, expr): Rejected - overkill for simple rules
- String templates: Rejected - less type-safe, harder to validate

---

## 7. Key Binding Conflicts

**Decision**: Use 'a' for alert history (available) and 'A' for acknowledge from history view.

**Rationale**:
- Checked existing keybindings: 'a' is not used globally
- Consistent with other overlay patterns (H for heatmap, ? for help)
- Enter for acknowledge matches common TUI patterns

**Key Assignment**:
| Key | Context | Action |
|-----|---------|--------|
| `a` | Any view | Open alert history overlay |
| `A` | Dashboard | Quick-acknowledge selected active alert |
| `Enter` | History overlay | Acknowledge selected alert |
| `Esc` | History overlay | Close overlay |
| `j/k` | History overlay | Navigate alerts |

---

## 8. Integration Points

**Decision**: Integrate via existing Bubbletea message flow.

**Integration Architecture**:
```
┌─────────────────┐     ┌──────────────────┐     ┌───────────────┐
│  MetricsMonitor │────▶│   AlertEngine    │────▶│  AlertPanel   │
│  (existing)     │     │   (new)          │     │  (new)        │
└─────────────────┘     └──────────────────┘     └───────────────┘
        │                       │                       │
        ▼                       ▼                       ▼
   MetricsDataMsg          AlertStateMsg           View()
        │                       │                       │
        └───────────────────────┴───────────────────────┘
                                │
                        ┌───────────────┐
                        │   Dashboard   │
                        │   (modified)  │
                        └───────────────┘
```

**Message Types**:
```go
// AlertStateMsg carries current alert states to UI
type AlertStateMsg struct {
    ActiveAlerts []ActiveAlert
    Changes      []StateChange  // For notifications
}

// ActiveAlert represents a currently firing alert
type ActiveAlert struct {
    RuleName     string
    State        AlertState
    MetricValue  float64
    Threshold    float64
    TriggeredAt  time.Time
    Acknowledged bool
}
```

---

## Research Conclusion

All technical decisions are resolved. The implementation follows existing Steep patterns:
- Configuration via Viper/YAML
- Storage via existing SQLite database
- UI via Bubbletea components and messages
- Metrics from existing monitor infrastructure

**Next Step**: Proceed to Phase 1 (data-model.md, contracts/, quickstart.md)
