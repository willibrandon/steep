# Feature Specification: Dashboard & Activity Monitoring

**Feature Branch**: `002-dashboard-activity`
**Created**: 2025-11-21
**Status**: Draft
**Input**: Implement the Dashboard and Activity Monitoring view for Steep with real-time metrics and connection management

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View Active Connections (Priority: P1)

As a DBA, I want to see all active database connections with query details so I can monitor current database activity and identify potential issues.

**Why this priority**: Core monitoring capability - DBAs need visibility into what's happening in the database before they can take any action. This is the foundation for all other monitoring features.

**Independent Test**: Can be fully tested by connecting to a PostgreSQL database and observing the activity table populate with real connection data, delivering immediate visibility into database activity.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL database with active connections, **When** the DBA opens the Activity view, **Then** they see a table displaying all connections with PID, User, Database, State, Duration, and Query (truncated to fit display)

2. **Given** the Activity view is displayed, **When** a new connection is established to the database, **Then** the table updates to show the new connection within 1 second

3. **Given** the Activity view is displayed, **When** the DBA presses 'd' on a selected row, **Then** the full query text is displayed in a detail view

4. **Given** the Activity view is displayed, **When** the DBA presses '/' to filter, **Then** they can enter a filter term to show only connections matching that state (active, idle, idle-in-transaction)

5. **Given** the Activity view is displayed, **When** the DBA presses 's', **Then** they can select a column to sort by (ascending/descending toggle)

---

### User Story 2 - View Dashboard Metrics (Priority: P1)

As a DBA, I want to see key database metrics (TPS, cache hit ratio, connection count, database size) on a dashboard so I can assess database health at a glance.

**Why this priority**: Essential for rapid health assessment - allows DBAs to quickly determine if the database is operating normally before diving into details.

**Independent Test**: Can be fully tested by observing the dashboard display real-time metrics that update automatically, delivering immediate health visibility.

**Acceptance Scenarios**:

1. **Given** a connected PostgreSQL database, **When** the DBA opens the Dashboard view, **Then** they see a multi-panel layout displaying TPS, cache hit ratio, active connection count, and database size

2. **Given** the Dashboard is displayed, **When** database activity changes, **Then** the metrics update automatically within the configured refresh interval

3. **Given** the Dashboard is displayed with TPS metric, **When** the DBA observes the metric, **Then** they see the transactions per second calculated from the difference between consecutive snapshots

4. **Given** the Dashboard is displayed, **When** the cache hit ratio falls below 90%, **Then** the metric is visually highlighted to indicate potential performance concern

---

### User Story 3 - Kill Problematic Connections (Priority: P2)

As a DBA, I want to kill or terminate problematic connections so I can resolve blocking issues and restore database responsiveness.

**Why this priority**: Administrative action that depends on first being able to see connections (P1). Important but secondary to visibility.

**Independent Test**: Can be fully tested by selecting a connection and using the kill action, delivering ability to resolve blocking issues.

**Acceptance Scenarios**:

1. **Given** the Activity view with a selected connection, **When** the DBA presses 'x' to kill, **Then** a confirmation dialog appears showing connection details and asking for confirmation

2. **Given** a confirmation dialog is displayed, **When** the DBA confirms the action, **Then** the system sends pg_terminate_backend() for the selected PID and displays a success/failure message

3. **Given** a confirmation dialog is displayed, **When** the DBA cancels the action, **Then** no action is taken and the dialog closes

4. **Given** the application is running in read-only mode, **When** the DBA attempts to kill a connection, **Then** the action is blocked with a message explaining read-only mode prevents destructive operations

---

### User Story 4 - Filter Connections by State (Priority: P2)

As a DBA, I want to filter connections by state (active, idle, idle-in-transaction) so I can focus on specific activity types and investigate issues more efficiently.

**Why this priority**: Enhances the core viewing capability (P1) but not essential for basic monitoring.

**Independent Test**: Can be fully tested by applying different state filters and observing the table update, delivering focused investigation capability.

**Acceptance Scenarios**:

1. **Given** the Activity view with multiple connections, **When** the DBA filters by "active", **Then** only connections with state='active' are displayed

2. **Given** a filter is applied, **When** the DBA clears the filter, **Then** all connections are displayed again

3. **Given** the Activity view, **When** the DBA filters by "idle-in-transaction", **Then** only connections in that state are shown, helping identify potential lock holders

---

### Edge Cases

- What happens when the database connection is lost during monitoring?
  - System displays a connection error message and attempts to reconnect automatically
  - Metrics show as "N/A" or stale indicator until connection is restored

- What happens when there are no active connections?
  - Activity table displays an empty state message: "No active connections"
  - Dashboard connection count shows 0

- What happens when attempting to kill your own connection?
  - System warns that this will disconnect the monitoring session
  - Requires explicit confirmation with warning message

- How does the system handle very long query text (>10KB)?
  - Query text is truncated in the table view with "..." indicator
  - Full text available in detail view with scrolling
  - Very long queries (>100KB) are capped with notice

- What happens when queries take longer than 500ms?
  - System completes the query but logs a warning
  - Stale data indicator shown until fresh data arrives

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST display a combined view with Dashboard metrics panel at the top showing TPS, cache hit ratio, active connection count, and database size
- **FR-002**: System MUST display an Activity table below the Dashboard metrics showing PID, User, Database, State, Duration, and Query columns with default limit of 500 rows and pagination for additional results
- **FR-003**: System MUST auto-refresh data at configurable intervals (default 1 second, configurable 1-5 seconds)
- **FR-004**: System MUST support sorting the activity table by any column via keyboard shortcut 's'
- **FR-005**: System MUST support filtering connections by state using '/' search command
- **FR-005a**: System MUST support filtering connections by database with default showing all databases and a toggle to show only current database
- **FR-006**: System MUST display full query text in a detail view when user presses 'd' on selected row
- **FR-007**: System MUST provide cancel query action via 'c' key (pg_cancel_backend) and terminate connection action via 'x' key (pg_terminate_backend), both with confirmation dialog
- **FR-008**: System MUST respect read-only mode and block kill actions when enabled
- **FR-009**: System MUST execute monitoring queries in under 500ms on databases with 100+ connections
- **FR-010**: System MUST color-code connection states (active=green, idle=gray, idle-in-transaction=yellow, blocked=red)
- **FR-011**: System MUST calculate TPS by comparing transaction counts between consecutive snapshots
- **FR-012**: System MUST display cache hit ratio as a percentage (blks_hit / (blks_hit + blks_read) * 100)
- **FR-013**: System MUST truncate query text in table view to fit available width with "..." indicator
- **FR-014**: System MUST support keyboard navigation (hjkl, g/G for top/bottom, / for search)
- **FR-015**: System MUST handle connection loss gracefully with error display and exponential backoff reconnection (1s, 2s, 4s... up to 30s max) plus manual retry option

### Key Entities

- **Connection**: A PostgreSQL backend process identified by PID, associated with a user, database, client address, and current query state
- **Metric**: A measured database value (TPS, cache ratio, count, size) with timestamp and optional trend data
- **Dashboard Panel**: A UI region displaying a single metric with label, value, and optional visual indicator
- **Activity Row**: A table row representing one connection with all its attributes

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can view all active connections and their current queries within 1 second of opening the Activity view
- **SC-002**: Dashboard metrics refresh and display within the configured interval (1 second default) with no user-perceptible lag
- **SC-003**: DBAs can identify and terminate a problematic connection in under 10 seconds from initial observation
- **SC-004**: System performs monitoring queries in under 500ms even with 100+ active database connections
- **SC-005**: 95% of users successfully locate and terminate a blocking connection on first attempt
- **SC-006**: All keyboard shortcuts work consistently with vim-like patterns (hjkl navigation, / search, g/G bounds)
- **SC-007**: Cache hit ratio below 90% is immediately visually distinguishable from normal values
- **SC-008**: Connection state is immediately recognizable through color coding without reading text

## Clarifications

### Session 2025-11-21

- Q: Are Dashboard and Activity separate views or combined? → A: Combined view with Dashboard metrics at top, Activity table below
- Q: Should Activity table show connections for all databases or only current? → A: Configurable filter with default showing all databases
- Q: What reconnection behavior on connection loss? → A: Exponential backoff (1s, 2s, 4s... up to 30s max) with manual retry option
- Q: Should users have both cancel query and terminate connection options? → A: Both options: 'c' to cancel query, 'x' to terminate connection
- Q: Should there be a display limit for connections? → A: Default limit of 500 rows with pagination/scrolling to load more

## Assumptions

- PostgreSQL version 11+ is used (for pg_stat_activity columns compatibility)
- The monitoring user has appropriate privileges to query pg_stat_activity and pg_stat_database
- The monitoring user has permission to call pg_terminate_backend() for kill functionality
- Terminal supports 256 colors for state color-coding
- Minimum terminal size of 80x24 is available
- Network latency to database is under 100ms for sub-500ms query performance
