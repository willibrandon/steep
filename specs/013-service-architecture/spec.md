# Feature Specification: Service Architecture (steep-agent)

**Feature Branch**: `013-service-architecture`
**Created**: 2025-12-01
**Status**: Draft
**Input**: User description: "Implement Service Architecture (steep-agent) for continuous background data collection independent of TUI runtime. Use kardianos/service library for cross-platform service management."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Continuous Data Collection (Priority: P1)

As a DBA, I want monitoring data to be collected continuously even when the TUI is not running, so I have historical data available when I open Steep.

**Why this priority**: This is the core value proposition of the agent architecture. Without continuous collection, there's no benefit over the current TUI-only approach. Historical data continuity enables better trend analysis, overnight issue detection, and complete metrics baselines.

**Independent Test**: Can be fully tested by running steep-agent in foreground mode, closing it, then verifying SQLite contains timestamped data from the collection period. Delivers immediate value by providing historical context when TUI opens.

**Acceptance Scenarios**:

1. **Given** steep-agent is running and collecting data, **When** I close all TUI instances and wait 10 minutes, **Then** the SQLite database contains new data entries timestamped during that period
2. **Given** steep-agent has been running overnight, **When** I open the TUI in the morning, **Then** I can view replication lag, query stats, and connection history from the overnight period
3. **Given** steep-agent is collecting from PostgreSQL, **When** PostgreSQL becomes temporarily unavailable, **Then** steep-agent logs the error and resumes collection automatically when connectivity is restored

---

### User Story 2 - Service Installation & Management (Priority: P1)

As a DBA, I want to install steep-agent as a system service (systemd/launchd/Windows Service) so it starts automatically on boot and runs without my intervention.

**Why this priority**: Without reliable service management, continuous collection is impractical. DBAs cannot manually start the agent every time the system restarts. This is essential infrastructure for production use.

**Independent Test**: Can be tested by running `steep-agent install`, rebooting the system, and verifying the agent is running via `steep-agent status`. Delivers value by making the agent "set and forget."

**Acceptance Scenarios**:

1. **Given** I run `steep-agent install` on macOS, **When** I reboot the system, **Then** steep-agent starts automatically and is visible via `launchctl list`
2. **Given** I run `steep-agent install` on Linux with systemd, **When** I run `systemctl status steep-agent`, **Then** the service shows as active and running
3. **Given** I run `steep-agent install` on Windows, **When** I open services.msc, **Then** Steep Agent appears as a running service
4. **Given** steep-agent crashes unexpectedly, **When** the service manager detects the crash, **Then** the agent restarts automatically with exponential backoff
5. **Given** I run `steep-agent uninstall`, **When** I check the service manager, **Then** the service is completely removed

---

### User Story 3 - Automatic Agent/TUI Coordination (Priority: P1)

As a DBA, I want the TUI to automatically detect when the agent is running and coordinate data collection to avoid conflicts, so I don't have to manually manage modes.

**Why this priority**: Seamless coordination is essential for good user experience. The TUI should "just work" whether the agent is running or not, without requiring explicit flags or configuration.

**Independent Test**: Start TUI alone (collects via log parsing), start agent, verify TUI switches to agent mode and stops its own collection. Stop agent, verify TUI resumes its own collection.

**Acceptance Scenarios**:

1. **Given** steep-agent is running, **When** I run `steep`, **Then** the TUI auto-detects the agent and shows "Agent: Running" in status bar
2. **Given** steep-agent is not running, **When** I run `steep`, **Then** the TUI collects data directly via log parsing and shows "Agent: Stopped" in status bar
3. **Given** TUI is running with agent healthy, **When** the agent stops, **Then** the TUI detects this within 10 seconds and resumes its own log collection
4. **Given** TUI is collecting data directly, **When** the agent starts, **Then** the TUI detects this within 10 seconds and stops its own collection
5. **Given** TUI is running with agent healthy, **When** I view the queries view header, **Then** it shows [AGENT] to indicate data source

---

### User Story 4 - Automatic Data Retention (Priority: P2)

As a DBA, I want steep-agent to handle data retention and cleanup automatically so the SQLite database doesn't grow unbounded and consume disk space.

**Why this priority**: Essential for production deployments but not required for basic functionality. Without retention management, the database would grow indefinitely, eventually causing disk space issues.

**Independent Test**: Can be tested by configuring short retention periods (e.g., 1 hour), generating data for 2 hours, then verifying older data was pruned. Delivers value by making the agent maintenance-free.

**Acceptance Scenarios**:

1. **Given** retention is configured as 24 hours for activity_history, **When** 36 hours have passed, **Then** activity data older than 24 hours is automatically deleted
2. **Given** steep-agent starts with existing SQLite data, **When** some data exceeds configured retention, **Then** the agent prunes old data during startup
3. **Given** retention pruning is running, **When** TUI is reading data, **Then** the pruning does not cause read errors or visible disruption

---

### User Story 5 - Multi-Instance Monitoring (Priority: P2)

As a DBA, I want steep-agent to monitor multiple PostgreSQL instances and aggregate data into a single SQLite database so I can view all my databases from one TUI.

**Why this priority**: Valuable for production environments with primary/replica topologies or multiple database clusters, but single-instance monitoring is sufficient for MVP.

**Independent Test**: Can be tested by configuring two PostgreSQL instances in config, running the agent, and verifying TUI shows data from both instances. Delivers value by providing unified monitoring.

**Acceptance Scenarios**:

1. **Given** config specifies two PostgreSQL instances (primary and replica1), **When** steep-agent starts, **Then** it establishes connections to both and collects data from each
2. **Given** one instance becomes unavailable, **When** steep-agent detects the failure, **Then** it continues collecting from available instances and logs the connection error
3. **Given** TUI is displaying data, **When** I view activity or replication data, **Then** I can distinguish which instance each data point came from

---

### User Story 6 - Shared Configuration (Priority: P2)

As a DBA, I want to configure steep-agent via the same YAML config file used by the TUI so I don't have to maintain separate configurations.

**Why this priority**: Reduces operational complexity and prevents configuration drift between agent and TUI. Important for usability but not blocking for core functionality.

**Design Decision**: Shared configuration is **recommended but not enforced**. Both components independently load their configured YAML file (defaulting to `~/.config/steep/config.yaml`). If configs differ, the TUI displays a warning in the debug panel. This follows the Docker daemon/CLI model where using different configs is considered user error rather than a system failure. For best results, users should run both components with the same config file.

**Independent Test**: Can be tested by modifying ~/.config/steep/config.yaml and verifying both steep-agent and steep TUI respect the changes. Additionally, run agent with one config and TUI with another via `--config` flags, verify warning appears in TUI debug panel.

**Acceptance Scenarios**:

1. **Given** config.yaml contains agent.intervals.activity = 5s, **When** steep-agent starts, **Then** it collects activity data every 5 seconds
2. **Given** config.yaml is modified while steep-agent is running, **When** I restart the agent, **Then** it uses the updated configuration
3. **Given** TUI and agent use the same config file, **When** I change connection settings, **Then** both components use the new connection
4. **Given** agent is running with config A and TUI is started with config B, **When** TUI detects the agent, **Then** a warning appears in the debug panel: "Config mismatch detected: TUI and agent using different configurations"

---

### User Story 7 - Background Alerting (Priority: P3)

As a DBA, I want steep-agent to support alerting/notifications when thresholds are breached so I'm notified of issues even when not watching the TUI.

**Why this priority**: Enhances the value of continuous collection but requires additional infrastructure (webhook endpoints, notification services). The current TUI alert system (Feature 012) fires only when TUI is open.

**Independent Test**: Can be tested by configuring a webhook URL, triggering an alert condition (e.g., high replication lag), and verifying the webhook receives a notification. Delivers value by enabling proactive incident response.

**Acceptance Scenarios**:

1. **Given** agent alerts are enabled with a webhook URL, **When** replication lag exceeds the critical threshold, **Then** the agent sends a POST request to the webhook with alert details
2. **Given** webhook delivery fails, **When** the agent retries, **Then** it uses exponential backoff and logs delivery failures
3. **Given** an alert condition clears, **When** the metric returns to normal, **Then** the agent sends a resolution notification to the webhook

---

### User Story 8 - Agent Health Monitoring (Priority: P3)

As a DBA, I want to query steep-agent status and health from the TUI so I can verify the agent is functioning correctly without leaving the monitoring interface.

**Why this priority**: Nice-to-have visibility into agent internals. Most users will rely on `steep-agent status` CLI command rather than TUI integration.

**Independent Test**: Can be tested by viewing agent status in TUI status bar or a dedicated panel showing uptime, last collection time, and error counts. Delivers value by providing operational visibility.

**Acceptance Scenarios**:

1. **Given** TUI is running with agent healthy, **When** I check the status bar, **Then** I see agent uptime and last collection timestamp
2. **Given** agent is experiencing collection errors, **When** I view agent health in TUI, **Then** I see error counts and most recent error messages
3. **Given** agent is healthy, **When** I run `steep-agent status`, **Then** I see running state, PID, uptime, connected instances, and last successful collection time

---

### Edge Cases

- What happens when SQLite database becomes corrupted or locked?
  - Agent should detect corruption on startup and offer to recreate the database
  - Lock conflicts should be handled gracefully with retry logic
- How does the system handle disk full conditions?
  - Agent should detect write failures and emit warnings without crashing
  - Retention pruning should prioritize freeing space
- What happens when agent and TUI use incompatible schema versions?
  - Schema version check on startup with clear error message
  - Migration path for schema upgrades
- How does agent handle graceful shutdown during active writes?
  - Context cancellation with timeout for in-flight operations
  - WAL checkpoint before exit to ensure data durability

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide a steep-agent daemon that runs independently of the TUI
- **FR-002**: System MUST support service installation on Windows (SCM), macOS (launchd), and Linux (systemd)
- **FR-003**: System MUST support user-level service installation (`--user` flag) in addition to system-wide
- **FR-004**: Agent MUST collect data continuously at configurable intervals per data type
- **FR-005**: Agent MUST persist collected data to SQLite database at ~/.config/steep/steep.db
- **FR-006**: Agent MUST use SQLite WAL mode to allow concurrent TUI read access
- **FR-007**: Agent MUST handle PostgreSQL connection failures with automatic reconnection
- **FR-008**: Agent MUST implement graceful shutdown completing in-flight writes
- **FR-009**: Agent MUST support monitoring multiple PostgreSQL instances from single daemon
- **FR-010**: Agent MUST implement configurable data retention with automatic pruning
- **FR-011**: TUI MUST auto-detect agent presence on startup and periodically during runtime
- **FR-012**: TUI MUST switch between agent mode and direct log collection automatically based on agent health
- **FR-013**: TUI MUST display agent status (Running/Stopped) in status bar
- **FR-014**: TUI MUST display data source indicator ([AGENT]/[LOG]/[SAMPLE]) in queries view header
- **FR-015**: Agent MUST support foreground run mode for debugging (`steep-agent run`)
- **FR-016**: Agent MUST restart automatically on crash with exponential backoff
- **FR-017**: Agent MUST log collection errors without crashing
- **FR-018**: Agent MUST support webhook notifications for alert conditions (P3)

### Key Entities

- **Agent Instance**: Running steep-agent process with PID, start time, configuration, and connection pools
- **Collection Interval**: Per-data-type timing configuration (activity: 2s, queries: 5s, etc.)
- **Retention Policy**: Per-data-type retention duration (activity: 24h, queries: 7d, etc.)
- **Monitored Instance**: PostgreSQL connection with name, connection string, and health status
- **Agent Status**: Current state including running/stopped, uptime, last collection time, error counts

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Agent maintains 99.9% uptime over a 7-day period under normal operating conditions
- **SC-002**: TUI startup completes in under 500ms with agent health check
- **SC-003**: Historical data is available for the configured retention period with no gaps longer than 2x collection interval
- **SC-004**: Service installation completes successfully on all three platforms (Windows, macOS, Linux)
- **SC-005**: Agent recovers from PostgreSQL outage within 30 seconds of connectivity restoration
- **SC-006**: SQLite database size remains stable when retention pruning is active (no unbounded growth)
- **SC-007**: Agent handles 5 simultaneous TUI readers without performance degradation
- **SC-008**: Graceful shutdown completes within 5 seconds with all data persisted
- **SC-009**: Multi-instance monitoring adds less than 10% overhead per additional instance

## Assumptions

- SQLite WAL mode is already enabled in current implementation (verified in db.go)
- Existing monitors (activity, queries, replication, locks, tables) can be reused without modification
- kardianos/service library provides sufficient abstraction for cross-platform service management
- Users have appropriate permissions to install system services (or can use --user for user-level)
- PostgreSQL connection credentials are already configured in config.yaml
- The existing alert engine (Feature 012) can be adapted for background evaluation with minimal changes

## Dependencies

- **Feature 001-012**: All existing features must be complete as agent reuses their monitors and SQLite stores
- **kardianos/service**: External library for cross-platform service management (MIT license)
- **spf13/cobra**: CLI framework for steep-agent commands (already used in project)
