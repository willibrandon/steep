# Data Model: Alert System

**Feature**: 012-alert-system
**Date**: 2025-11-30

## Entities Overview

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   AlertRule     │────▶│   AlertState    │────▶│   AlertEvent    │
│   (config)      │     │   (runtime)     │     │   (persistence) │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                      │                       │
         ▼                      ▼                       ▼
   YAML config           In-memory map            SQLite table
```

---

## 1. AlertRule

Represents a configured monitoring condition loaded from YAML.

**Source**: `~/.config/steep/config.yaml` under `alerts.rules`

**Attributes**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Unique identifier for the rule (e.g., "high_replication_lag") |
| metric | string | yes | Metric expression to evaluate (e.g., "replication_lag_bytes" or "active_connections / max_connections") |
| operator | string | no | Comparison operator: ">", "<", ">=", "<=", "==", "!=" (default: ">") |
| warning | float64 | yes | Threshold that triggers Warning state |
| critical | float64 | yes | Threshold that triggers Critical state |
| enabled | bool | no | Whether the rule is active (default: true) |
| message | string | no | Custom message template (default: auto-generated) |

**Validation Rules**:
- `name` must be unique across all rules
- `name` must match pattern: `^[a-z][a-z0-9_]*$` (lowercase, underscores)
- `metric` must reference valid metric names or expression
- `warning` and `critical` must be ordered correctly based on operator:
  - For ">" operator: warning < critical
  - For "<" operator: warning > critical

**Example**:
```yaml
- name: high_replication_lag
  metric: replication_lag_bytes
  operator: ">"
  warning: 104857600   # 100MB
  critical: 524288000  # 500MB
  enabled: true
  message: "Replication lag: {value} > {threshold}"
```

---

## 2. AlertState

Runtime state for each alert rule (in-memory).

**Attributes**:

| Field | Type | Description |
|-------|------|-------------|
| rule_name | string | Reference to AlertRule.name |
| current_state | enum | One of: Normal, Warning, Critical |
| previous_state | enum | State before last transition |
| metric_value | float64 | Current evaluated metric value |
| triggered_at | time.Time | When current state was entered |
| last_evaluated | time.Time | When rule was last evaluated |
| acknowledged | bool | Whether current alert is acknowledged |
| acknowledged_at | time.Time | When acknowledged (if applicable) |

**State Transitions**:
```
         ┌──────────────────────────────────────┐
         │                                      │
         ▼                                      │
    ┌─────────┐     value > warning     ┌───────────┐
    │ Normal  │ ───────────────────────▶│  Warning  │
    └─────────┘                         └───────────┘
         ▲                                   │  │
         │         value <= warning          │  │
         │◀──────────────────────────────────┘  │
         │                                      │
         │                              value > critical
         │                                      │
         │                                      ▼
         │                              ┌───────────┐
         │      value <= warning        │ Critical  │
         └◀─────────────────────────────│           │
                                        └───────────┘
                                             │  ▲
                            value <= critical │  │ value > critical
                            (but > warning)   │  │
                                             ▼  │
                                        ┌───────────┐
                                        │  Warning  │
                                        └───────────┘
```

**Transition Rules**:
1. Normal → Warning: metric crosses warning threshold
2. Warning → Critical: metric crosses critical threshold
3. Critical → Warning: metric drops below critical but above warning
4. Warning → Normal: metric drops below warning
5. Critical → Normal: metric drops below warning (skip Warning)

---

## 3. AlertEvent

Persisted record of a state transition.

**Storage**: SQLite table `alert_events`

**Attributes**:

| Field | Type | SQLite Type | Description |
|-------|------|-------------|-------------|
| id | int64 | INTEGER PRIMARY KEY | Auto-increment ID |
| rule_name | string | TEXT NOT NULL | Rule that triggered |
| prev_state | string | TEXT NOT NULL | State before transition |
| new_state | string | TEXT NOT NULL | State after transition |
| metric_value | float64 | REAL NOT NULL | Value that triggered transition |
| threshold_value | float64 | REAL | Threshold that was crossed |
| triggered_at | time.Time | DATETIME | When transition occurred |
| acknowledged_at | time.Time | DATETIME | When acknowledged (nullable) |
| acknowledged_by | string | TEXT | Who acknowledged (nullable) |

**Indexes**:
- `idx_alert_events_triggered`: (triggered_at DESC) - for history view
- `idx_alert_events_rule`: (rule_name, triggered_at DESC) - for rule-specific queries
- `idx_alert_events_state`: (new_state, triggered_at DESC) - for filtering by severity

**Retention**:
- Default: 30 days
- Configurable via `alerts.history_retention`
- Pruned by background goroutine (hourly)

---

## 4. MetricValue

Runtime metric snapshot for evaluation.

**Attributes**:

| Field | Type | Description |
|-------|------|-------------|
| name | string | Metric name (e.g., "active_connections") |
| value | float64 | Current value |
| timestamp | time.Time | When value was collected |
| available | bool | Whether metric is available |

**Available Metrics**:

| Metric Name | Source | Type | Unit |
|-------------|--------|------|------|
| active_connections | models.Metrics | int→float | count |
| max_connections | models.Metrics | int→float | count |
| cache_hit_ratio | models.Metrics | float | percentage (0-1) |
| tps | models.Metrics | float | transactions/second |
| replication_lag_bytes | ReplicationData | int→float | bytes |
| longest_transaction_seconds | pg_stat_activity | float | seconds |
| idle_in_transaction_seconds | pg_stat_activity | float | seconds |

**Derived Metrics** (expressions):
- `active_connections / max_connections` → connection utilization ratio
- `replication_lag_bytes / 1048576` → lag in megabytes

---

## 5. ActiveAlert

UI-facing representation of a currently firing alert.

**Attributes**:

| Field | Type | Description |
|-------|------|-------------|
| rule_name | string | Rule identifier |
| state | enum | Warning or Critical |
| metric_value | float64 | Current value |
| threshold | float64 | Threshold that was crossed |
| triggered_at | time.Time | When alert started |
| duration | time.Duration | How long active (calculated) |
| acknowledged | bool | Whether acknowledged |
| message | string | Formatted alert message |

---

## 6. AlertsConfig

Configuration structure loaded from YAML.

**Location**: Root config `alerts:` section

**Attributes**:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | bool | true | Master switch for alert system |
| refresh_interval | duration | (UI refresh) | How often to evaluate rules |
| history_retention | duration | 720h (30d) | How long to keep history |
| rules | []AlertRule | [] | List of alert rules |

**Example YAML**:
```yaml
alerts:
  enabled: true
  refresh_interval: 5s
  history_retention: 720h  # 30 days

  rules:
    - name: high_replication_lag
      metric: replication_lag_bytes
      warning: 104857600
      critical: 524288000
      enabled: true

    - name: connection_saturation
      metric: active_connections / max_connections
      warning: 0.8
      critical: 0.95

    - name: low_cache_hit
      metric: cache_hit_ratio
      operator: "<"
      warning: 0.95
      critical: 0.90

    - name: long_transaction
      metric: longest_transaction_seconds
      warning: 300
      critical: 900
```

---

## Relationships Summary

```
AlertsConfig (YAML)
    │
    └── rules: []AlertRule
            │
            ▼
    AlertEngine (runtime)
        │
        ├── states: map[rule_name]*AlertState
        │       │
        │       └── On state change ──▶ AlertEvent (SQLite)
        │
        └── Evaluate(metrics) ──▶ []ActiveAlert ──▶ UI
```

---

## SQLite Schema

```sql
-- Alert events history table
CREATE TABLE IF NOT EXISTS alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_name TEXT NOT NULL,
    prev_state TEXT NOT NULL CHECK(prev_state IN ('normal', 'warning', 'critical')),
    new_state TEXT NOT NULL CHECK(new_state IN ('normal', 'warning', 'critical')),
    metric_value REAL NOT NULL,
    threshold_value REAL,
    triggered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    acknowledged_at DATETIME,
    acknowledged_by TEXT
);

-- Index for history view (most recent first)
CREATE INDEX IF NOT EXISTS idx_alert_events_triggered
    ON alert_events(triggered_at DESC);

-- Index for rule-specific queries
CREATE INDEX IF NOT EXISTS idx_alert_events_rule
    ON alert_events(rule_name, triggered_at DESC);

-- Index for filtering by severity
CREATE INDEX IF NOT EXISTS idx_alert_events_state
    ON alert_events(new_state, triggered_at DESC);
```
