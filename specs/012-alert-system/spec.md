# Feature Specification: Alert System

**Feature Branch**: `012-alert-system`
**Created**: 2025-11-30
**Status**: Draft
**Input**: User description: "Implement Alert System for threshold-based monitoring with visual indicators and history tracking"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure Threshold Alerts (Priority: P1)

As a DBA, I want to configure alerts for critical metrics (replication lag, connection limits, cache hit ratio, long transactions) so that I am automatically notified when database health degrades beyond acceptable thresholds.

**Why this priority**: This is the foundation of the alert system. Without configurable alert rules, no alerts can be triggered. DBAs need to define what constitutes a problem for their specific environment before any monitoring value is delivered.

**Independent Test**: Can be fully tested by adding alert rules to the configuration file and verifying the application loads and validates them on startup. Delivers immediate value by enabling users to define their monitoring thresholds.

**Acceptance Scenarios**:

1. **Given** an empty alerts section in config, **When** the application starts, **Then** no alerts are active and the system operates normally without errors
2. **Given** valid alert rules in config (e.g., replication_lag > 100MB), **When** the application starts, **Then** the rules are loaded and validated successfully
3. **Given** an invalid alert rule (e.g., unknown metric name), **When** the application starts, **Then** a warning is logged but the application continues with valid rules
4. **Given** a configured alert rule with Warning and Critical thresholds, **When** the metric crosses the Warning threshold, **Then** the alert state changes to Warning
5. **Given** a configured alert rule in Warning state, **When** the metric crosses the Critical threshold, **Then** the alert state changes to Critical
6. **Given** a configured alert rule in Critical state, **When** the metric returns below the Warning threshold, **Then** the alert state changes to Normal

---

### User Story 2 - Visual Alert Indicators (Priority: P1)

As a DBA, I want visual indicators when alerts trigger so that I can quickly identify problems at a glance without reading detailed metrics.

**Why this priority**: Visual indicators transform raw data into actionable information. Even with alerts configured, users need immediate visual feedback to act quickly. This is essential for the monitoring value proposition.

**Independent Test**: Can be tested by triggering alert conditions and verifying color-coded indicators appear in the Dashboard and status bar. Delivers immediate value by making alert states visible.

**Acceptance Scenarios**:

1. **Given** an active Warning alert, **When** viewing the Dashboard, **Then** a yellow/orange warning indicator is displayed
2. **Given** an active Critical alert, **When** viewing the Dashboard, **Then** a red critical indicator is displayed
3. **Given** multiple active alerts, **When** viewing the status bar, **Then** alert counts are displayed (e.g., "2 Warnings, 1 Critical")
4. **Given** no active alerts, **When** viewing the Dashboard, **Then** no alert indicators are displayed (clean state)
5. **Given** an alert transitions from Critical to Normal, **When** viewing the Dashboard, **Then** the alert indicator is removed

---

### User Story 3 - Alert Panel Display (Priority: P1)

As a DBA, I want an alert panel showing all active alerts with their severity and message so that I can understand the current state of all monitored conditions.

**Why this priority**: The alert panel provides the detailed context needed to understand and respond to alerts. It bridges the gap between visual indicators and actionable information.

**Independent Test**: Can be tested by triggering multiple alerts and verifying they appear in the Dashboard alert panel with correct severity, metric name, threshold, and current value.

**Acceptance Scenarios**:

1. **Given** active alerts exist, **When** viewing the Dashboard, **Then** the alert panel displays each alert with severity icon, metric name, condition, and current value
2. **Given** multiple alerts with different severities, **When** viewing the alert panel, **Then** alerts are sorted by severity (Critical first, then Warning)
3. **Given** an alert panel with multiple alerts, **When** an alert transitions to Normal, **Then** the alert is removed from the panel
4. **Given** a long alert message, **When** viewing the alert panel, **Then** the message is truncated appropriately for the display width

---

### User Story 4 - Alert History (Priority: P2)

As a DBA, I want to view alert history so that I can review past incidents, identify patterns, and understand the timeline of database health issues.

**Why this priority**: History provides retrospective analysis capability. While not essential for real-time monitoring, it enables learning from past incidents and demonstrating compliance.

**Independent Test**: Can be tested by triggering alerts, letting them resolve, and then viewing the alert history to verify all state transitions are recorded with timestamps.

**Acceptance Scenarios**:

1. **Given** alerts have triggered and resolved, **When** pressing the 'a' key to view alert history, **Then** a history overlay displays with timestamp, metric, condition, state transitions, and acknowledgment status
2. **Given** the alert history view is open, **When** scrolling through history, **Then** older alerts are accessible via standard navigation (j/k or arrow keys)
3. **Given** alert history with many entries, **When** viewing history, **Then** entries are sorted by timestamp (most recent first)
4. **Given** the application restarts, **When** viewing alert history, **Then** previously recorded alerts are still visible (persisted to storage)

---

### User Story 5 - Alert Acknowledgment (Priority: P2)

As a DBA, I want to acknowledge alerts to track resolution status so that I can communicate to my team that an issue is being addressed and track what has been handled.

**Why this priority**: Acknowledgment enables team workflow coordination. It's valuable for multi-person teams but not essential for individual DBA monitoring.

**Independent Test**: Can be tested by selecting an alert in the history view, pressing Enter to acknowledge, and verifying the acknowledgment status is updated and persisted.

**Acceptance Scenarios**:

1. **Given** an active or historical alert, **When** selecting the alert and pressing Enter, **Then** the alert is marked as acknowledged with a timestamp
2. **Given** an acknowledged alert, **When** viewing the alert panel or history, **Then** the acknowledgment status is visible (e.g., checkmark or "Ack" label)
3. **Given** an acknowledged alert, **When** the application restarts, **Then** the acknowledgment status is preserved
4. **Given** an alert that has been acknowledged, **When** the same condition triggers again (new incident), **Then** a new unacknowledged alert is created

---

### User Story 6 - Custom Alert Rules (Priority: P3)

As a DBA, I want custom alert rules with complex conditions so that I can monitor specific scenarios unique to my environment (e.g., ratio-based thresholds, compound conditions).

**Why this priority**: Custom rules provide advanced flexibility. Most common monitoring needs are covered by simple threshold comparisons; complex rules are an enhancement for power users.

**Independent Test**: Can be tested by configuring complex alert rules (e.g., active_connections / max_connections > 0.8) and verifying they evaluate correctly.

**Acceptance Scenarios**:

1. **Given** a ratio-based alert rule (e.g., active_connections / max_connections > 0.8), **When** the ratio exceeds the threshold, **Then** the alert triggers
2. **Given** a time-based alert rule (e.g., transaction_duration > 5m), **When** a transaction exceeds the duration, **Then** the alert triggers
3. **Given** an invalid custom rule syntax, **When** the application starts, **Then** a warning is logged and the rule is skipped

---

### Edge Cases

- What happens when the database connection is lost during alert evaluation?
  - Alert evaluation is skipped for that cycle; existing alerts remain in their current state; a connection warning is displayed
- What happens when a metric is unavailable (e.g., pg_stat_statements not installed)?
  - Alert rules for unavailable metrics are disabled with a warning; other alerts continue to function
- What happens when the SQLite storage for history is unavailable or corrupted?
  - Alert system continues to function for real-time monitoring; history features are disabled with a warning
- How does the system handle rapid metric fluctuations (flapping)?
  - Alerts evaluate on each refresh cycle; if metrics oscillate rapidly, alerts will change state accordingly (no built-in hysteresis in initial implementation)
- What happens when the alert configuration file has syntax errors?
  - The application logs the error and continues with default/empty alert configuration

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST load alert configuration from the alerts section of the configuration file on startup
- **FR-002**: System MUST validate alert rules on startup and log warnings for invalid rules without preventing startup
- **FR-003**: System MUST support alert states: Normal, Warning, and Critical
- **FR-004**: System MUST evaluate alert conditions on each metric refresh cycle (1-5 seconds configurable)
- **FR-005**: System MUST display active alerts in the Dashboard with color-coded severity indicators (yellow for Warning, red for Critical)
- **FR-006**: System MUST display alert counts in the status bar showing Warning and Critical counts
- **FR-007**: System MUST provide an alert panel in the Dashboard showing active alerts with severity, metric name, condition, and current value
- **FR-008**: System MUST persist alert history to SQLite storage
- **FR-009**: System MUST provide an alert history view accessible via the 'a' key showing timestamp, metric, condition, state, and acknowledgment status
- **FR-010**: System MUST support alert acknowledgment via Enter key in the history view
- **FR-011**: System MUST persist acknowledgment status to storage
- **FR-012**: System MUST support threshold comparison operators: >, <, >=, <=, ==, !=
- **FR-013**: System MUST support ratio/expression-based conditions (e.g., metric1 / metric2 > threshold)
- **FR-014**: System MUST support duration-based conditions with time units (e.g., > 5m, > 1h)
- **FR-015**: System MUST support these built-in metrics for alerts: replication_lag_bytes, active_connections, max_connections, cache_hit_ratio, longest_transaction_seconds, idle_in_transaction_seconds
- **FR-016**: System MUST gracefully degrade when monitored metrics are unavailable
- **FR-017**: System MUST remove alerts from the active panel when they return to Normal state
- **FR-018**: System MUST sort active alerts by severity (Critical first, then Warning)

### Key Entities

- **Alert Rule**: Represents a configured monitoring condition with metric name, operator, threshold values (warning/critical), and enabled state
- **Alert State**: Represents the current condition of an alert rule (Normal, Warning, Critical) with timestamp of last state change
- **Alert Event**: Represents a historical state transition with timestamp, metric, old state, new state, triggering value, and acknowledgment status
- **Metric Value**: Represents a current metric reading with name, value, and timestamp used for alert evaluation

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can configure at least 5 different alert types (replication lag, connections, cache hit, transaction duration, idle transactions) through the configuration file
- **SC-002**: Alert state changes are reflected in the UI within one refresh cycle (1-5 seconds) of the metric crossing a threshold
- **SC-003**: Active alerts are visible in the Dashboard without requiring navigation to other views
- **SC-004**: Status bar alert counts are always visible regardless of current view
- **SC-005**: Alert history retains at least 30 days of alert events (configurable retention period)
- **SC-006**: 100% of alert state transitions are recorded in history
- **SC-007**: Alert evaluation completes within 100ms per cycle to avoid impacting refresh performance
- **SC-008**: Invalid alert configurations are logged as warnings without preventing application startup
- **SC-009**: Acknowledged alerts remain acknowledged across application restarts
- **SC-010**: DBAs can identify the severity and affected metric of any alert within 2 seconds of viewing the Dashboard

## Assumptions

- Alert sound notifications are an optional enhancement and will not be included in the initial implementation
- The existing SQLite database (~/.config/steep/steep.db) will be used for alert history persistence
- Alert evaluation will use the same database connection pool as other monitoring features
- The 'a' key is available for alert history view (not conflicting with other bindings)
- Hysteresis/debouncing for flapping alerts will be considered a future enhancement if needed
- The initial implementation focuses on single-database monitoring; multi-database alert aggregation is out of scope
