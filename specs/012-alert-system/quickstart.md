# Quickstart: Alert System Implementation

**Feature**: 012-alert-system
**Date**: 2025-11-30

## Prerequisites

Before implementing, ensure:
1. Feature branch `012-alert-system` is checked out
2. All tests pass: `go test ./...`
3. Visual design phase complete (per Constitution Principle VI)

## Implementation Order

Follow P1 → P2 → P3 priority from spec:

### Phase 1: P1 Stories (Core Alerting)

**1.1 Alert Configuration** (Story 1)
```bash
# Create config package additions
touch internal/config/alerts.go

# Create alert domain package
mkdir -p internal/alerts
touch internal/alerts/config.go
touch internal/alerts/rule.go
```

Key files to modify:
- `internal/config/config.go`: Add `Alerts AlertsConfig` field
- `internal/config/alerts.go`: AlertsConfig struct, validation

Test with:
```yaml
# ~/.config/steep/config.yaml
alerts:
  enabled: true
  rules:
    - name: test_alert
      metric: cache_hit_ratio
      operator: "<"
      warning: 0.95
      critical: 0.90
```

**1.2 Alert Engine** (Story 1 continued)
```bash
touch internal/alerts/engine.go
touch internal/alerts/state.go
touch internal/alerts/metrics.go
```

Integration point:
- Modify `internal/app/model.go`: Initialize engine, subscribe to MetricsDataMsg

**1.3 Visual Indicators** (Story 2)
```bash
touch internal/ui/components/alert_panel.go
```

Modify:
- `internal/ui/views/dashboard.go`: Render alert panel
- `internal/ui/styles/styles.go`: Add alert colors

**1.4 Alert Panel** (Story 3)

Already created in 1.3. Ensure:
- Severity sorting (Critical first)
- Color coding (yellow/red)
- Message truncation

### Phase 2: P2 Stories (History & Acknowledgment)

**2.1 SQLite Schema** (Story 4)
```bash
touch internal/storage/sqlite/alert_store.go
```

Modify:
- `internal/storage/sqlite/schema.go`: Add alert_events table

**2.2 Alert History View** (Story 4)
```bash
# History is an overlay, not a separate view
# Add to dashboard or create shared overlay component
```

Modify:
- `internal/ui/views/dashboard.go`: Add history overlay state
- `internal/ui/keys.go`: Add 'a' key binding

**2.3 Acknowledgment** (Story 5)

Modify:
- `internal/alerts/engine.go`: Add Acknowledge method
- `internal/storage/sqlite/alert_store.go`: Add Acknowledge method
- `internal/ui/views/dashboard.go`: Handle Enter key in history

### Phase 3: P3 Stories (Custom Rules)

**3.1 Expression Parser** (Story 6)
```bash
touch internal/alerts/expression.go
touch internal/alerts/expression_test.go
```

Support:
- Simple metric reference: `cache_hit_ratio`
- Division: `active_connections / max_connections`
- Constants: `replication_lag_bytes / 1048576`

---

## Key Integration Points

### 1. Metrics Data Flow

```go
// In internal/app/model.go Update()

case ui.MetricsDataMsg:
    // Existing metrics handling...

    // NEW: Evaluate alerts
    if m.alertEngine != nil {
        metricValues := alerts.NewMetricValues(msg.Metrics)
        changes := m.alertEngine.Evaluate(metricValues)

        // Send alert state to dashboard
        return m, func() tea.Msg {
            return alerts.AlertStateMsg{
                ActiveAlerts:  m.alertEngine.GetActiveAlerts(),
                Changes:       changes,
                WarningCount:  m.alertEngine.WarningCount(),
                CriticalCount: m.alertEngine.CriticalCount(),
            }
        }
    }
```

### 2. Dashboard Alert Panel

```go
// In internal/ui/views/dashboard.go

func (d *DashboardView) renderAlertPanel() string {
    if len(d.activeAlerts) == 0 {
        return "" // Don't render if no alerts
    }

    // Render bordered panel with alert list
    // Critical alerts first, then Warning
    // Show: icon, rule name, value vs threshold, time ago
}
```

### 3. Status Bar Alert Counts

```go
// In status bar rendering (dashboard or app-level)

func renderStatusBar() string {
    // ... existing status bar content ...

    if d.criticalCount > 0 || d.warningCount > 0 {
        alerts := ""
        if d.warningCount > 0 {
            alerts += styles.WarningStyle.Render(fmt.Sprintf("⚠ %d", d.warningCount))
        }
        if d.criticalCount > 0 {
            alerts += " " + styles.CriticalStyle.Render(fmt.Sprintf("❌ %d", d.criticalCount))
        }
        // Add to status bar
    }
}
```

---

## Testing Checklist

### Unit Tests
- [ ] Rule validation (valid/invalid rules)
- [ ] Expression parsing (simple, division, errors)
- [ ] State transitions (all transitions)
- [ ] Threshold comparisons (>, <, edge cases)

### Integration Tests
- [ ] Config loading with alerts section
- [ ] SQLite alert_events CRUD
- [ ] Engine evaluation with real metrics

### Manual UI Tests
- [ ] Alert panel renders at 80x24 terminal
- [ ] Colors visible on dark/light themes
- [ ] History overlay opens/closes with a/Esc
- [ ] Acknowledgment persists across restart
- [ ] Status bar counts update in real-time

---

## Configuration Example

```yaml
# ~/.config/steep/config.yaml

connection:
  host: localhost
  port: 5432
  database: postgres
  user: postgres

alerts:
  enabled: true
  history_retention: 720h  # 30 days

  rules:
    # Replication lag monitoring
    - name: high_replication_lag
      metric: replication_lag_bytes
      warning: 104857600    # 100MB
      critical: 524288000   # 500MB

    # Connection pool saturation
    - name: connection_saturation
      metric: active_connections / max_connections
      warning: 0.8
      critical: 0.95

    # Cache efficiency
    - name: low_cache_hit
      metric: cache_hit_ratio
      operator: "<"
      warning: 0.95
      critical: 0.90

    # Long-running transactions
    - name: long_transaction
      metric: longest_transaction_seconds
      warning: 300   # 5 minutes
      critical: 900  # 15 minutes

    # Idle in transaction
    - name: idle_in_transaction
      metric: idle_in_transaction_seconds
      warning: 60    # 1 minute
      critical: 300  # 5 minutes
```

---

## Common Issues

### Alert not triggering
1. Check `alerts.enabled: true` in config
2. Verify rule `enabled: true` (default)
3. Check metric name spelling
4. Verify operator direction (> vs <)

### History not persisting
1. Check SQLite database permissions
2. Verify `~/.config/steep/steep.db` exists
3. Check logs for schema migration errors

### Visual issues
1. Ensure terminal supports Unicode (for icons)
2. Check terminal color support (256 colors)
3. Verify minimum terminal size (80x24)
