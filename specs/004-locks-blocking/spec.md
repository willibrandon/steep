# Feature Specification: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Created**: 2025-11-22
**Status**: Draft
**Input**: User description: "Implement Locks and Blocking Detection view for monitoring database lock contention. Query pg_locks and pg_stat_activity to display active locks with type, mode, granted status, and associated queries. Detect and highlight blocking relationships with color coding (red for blocked, yellow for blockers). Visualize lock dependency trees using ASCII art to show blocking chains. Support killing blocking queries with confirmation dialog."

## User Scenarios & Testing

### User Story 1 - View Active Locks (Priority: P1)

As a DBA, I want to see all active locks with their type and mode so that I can understand current lock contention in the database.

**Why this priority**: This is the foundational capability - without seeing locks, no other lock-related functionality is useful. DBAs need immediate visibility into lock state to begin any troubleshooting.

**Independent Test**: Can be fully tested by navigating to the Locks view and verifying all active locks are displayed with complete information. Delivers immediate value by providing lock visibility.

**Acceptance Scenarios**:

1. **Given** a database with active transactions holding locks, **When** I press the `5` key, **Then** I see a table displaying all active locks with columns: PID, Lock Type, Mode, Granted, Database, Relation, Query (truncated)
2. **Given** the Locks view is displayed, **When** I press `s` and select a column, **Then** the table is sorted by that column
3. **Given** a lock row is selected, **When** I press `d`, **Then** I see the full query text in a detail view
4. **Given** the Locks view is active, **When** 2 seconds elapse, **Then** the view automatically refreshes with current lock data

---

### User Story 2 - Identify Blocking Queries (Priority: P1)

As a DBA, I want to identify which queries are blocking others so that I can quickly resolve deadlock situations and reduce wait times.

**Why this priority**: Identifying blockers is the primary diagnostic need. A DBA seeing locks needs to immediately know which are causing problems for other queries.

**Independent Test**: Can be fully tested by creating blocking scenarios and verifying blocked/blocker highlighting. Delivers immediate diagnostic value.

**Acceptance Scenarios**:

1. **Given** a database with blocking locks (query A holding lock, query B waiting), **When** I view the Locks view, **Then** blocked queries are highlighted in red and blocking queries are highlighted in yellow
2. **Given** blocking relationships exist, **When** viewing the lock table, **Then** I can clearly distinguish blocked queries from blocking queries and from non-blocking queries
3. **Given** multiple blocking chains exist, **When** viewing the Locks view, **Then** all blocking relationships are correctly identified and color-coded

---

### User Story 3 - Visualize Lock Dependency Tree (Priority: P2)

As a DBA, I want to visualize the lock dependency tree so that I can understand the blocking relationships and chains in a hierarchical view.

**Why this priority**: While P1 stories show blocking exists, this story helps understand complex blocking chains. Valuable for diagnosing multi-level blocking scenarios but not essential for basic lock monitoring.

**Independent Test**: Can be fully tested by creating multi-level blocking chains and verifying ASCII tree renders correctly below the table.

**Acceptance Scenarios**:

1. **Given** blocking relationships exist, **When** I view the Locks view, **Then** I see an ASCII tree visualization below the lock table showing the dependency hierarchy
2. **Given** a multi-level blocking chain (A blocks B, B blocks C), **When** viewing the tree, **Then** the tree shows the correct parent-child relationships with appropriate indentation
3. **Given** multiple independent blocking chains, **When** viewing the tree, **Then** each chain is displayed as a separate tree root

---

### User Story 4 - Kill Blocking Query (Priority: P2)

As a DBA, I want to kill blocking queries so that I can quickly resolve lock contention and restore normal database operation.

**Why this priority**: This is an action capability that depends on first identifying blockers (P1). Provides resolution mechanism but requires confirmation to prevent accidental termination.

**Independent Test**: Can be fully tested by selecting a blocking query and verifying kill action with confirmation dialog.

**Acceptance Scenarios**:

1. **Given** a blocking query is selected, **When** I press `x`, **Then** a confirmation dialog appears asking to confirm termination
2. **Given** the confirmation dialog is displayed, **When** I confirm the action, **Then** the blocking query is terminated and the view refreshes
3. **Given** the confirmation dialog is displayed, **When** I cancel the action, **Then** no query is terminated and I return to the normal view
4. **Given** the application is in read-only mode, **When** I press `x` on a blocking query, **Then** the action is disabled and I see a message indicating read-only mode

---

### User Story 5 - View Deadlock History (Priority: P3)

As a DBA, I want to see deadlock history so that I can analyze recurring deadlock patterns over time.

**Why this priority**: Historical analysis is valuable for pattern detection but not critical for real-time monitoring. Requires additional data collection/storage beyond basic lock monitoring.

**Independent Test**: Can be tested by triggering deadlocks and verifying historical records are captured and displayed.

**Acceptance Scenarios**:

1. **Given** deadlocks have occurred in the database, **When** I access the deadlock history view, **Then** I see a list of past deadlocks with timestamps, involved PIDs, and queries
2. **Given** deadlock history is displayed, **When** I select a historical deadlock, **Then** I see detailed information about the queries and locks involved

---

### Edge Cases

- What happens when no locks exist in the database? Display an empty table with a message "No active locks"
- What happens when a blocking query terminates before the kill action completes? Display "Query already terminated" message
- What happens when the user lacks permission to terminate a query? Display "Permission denied" with guidance to check database privileges
- How does the system handle locks on system tables? Display them with appropriate labeling
- What happens when the lock count exceeds display capacity? Implement pagination or scrolling with clear indication of total count

## Requirements

### Functional Requirements

- **FR-001**: System MUST display all active locks from the database with columns: PID, Lock Type, Mode, Granted status, Database, Relation, and Query (truncated to fit display)
- **FR-002**: System MUST make the Locks view accessible via the `5` key from any view
- **FR-003**: System MUST detect blocking relationships by identifying locks that are waiting (not granted) and the locks that are blocking them
- **FR-004**: System MUST highlight blocked queries in red and blocking queries in yellow to visually distinguish their status
- **FR-005**: System MUST support sorting the lock table by any column using the `s` key
- **FR-006**: System MUST display the full query text when the `d` key is pressed on a selected lock row
- **FR-007**: System MUST auto-refresh the Locks view every 2 seconds
- **FR-008**: System MUST render a lock dependency tree below the lock table using ASCII art to show blocking chains hierarchically
- **FR-009**: System MUST support terminating blocking queries via the `x` key with a confirmation dialog
- **FR-010**: System MUST respect read-only mode by disabling the kill action when `--readonly` flag is active
- **FR-011**: System MUST execute lock queries in under 500ms even with 100+ active locks

### Key Entities

- **Lock**: Represents an active database lock with attributes: PID (process ID), lock type (relation, transactionid, tuple, etc.), lock mode (ACCESS SHARE, ROW EXCLUSIVE, etc.), granted status (boolean), database name, relation name, and associated query text
- **Blocking Relationship**: Represents a blocker-blocked relationship between two locks where one granted lock prevents another from being granted on the same resource
- **Lock Dependency Tree**: Hierarchical structure showing blocking chains where each node represents a lock/query and children represent queries blocked by it

## Success Criteria

### Measurable Outcomes

- **SC-001**: Users can access the Locks view and see all active locks within 1 second of pressing the `5` key
- **SC-002**: Users can identify blocked and blocking queries at a glance through color coding without reading detailed information
- **SC-003**: Lock query execution completes in under 500ms even when 100+ locks are active in the database
- **SC-004**: Users can understand multi-level blocking chains through the dependency tree visualization without manual correlation
- **SC-005**: Users can terminate blocking queries in under 5 seconds (including confirmation dialog)
- **SC-006**: The view auto-refreshes every 2 seconds without user intervention, keeping data current
- **SC-007**: Minimum terminal size of 80x24 displays all essential lock information without critical data truncation

## Assumptions

- PostgreSQL version 11 or higher is required (for consistent pg_locks behavior)
- Users have at least SELECT permission on pg_locks and pg_stat_activity system views
- Users with kill capability have appropriate database privileges (pg_signal_backend or superuser)
- The treeprint library (github.com/xlab/treeprint) will be used for ASCII tree rendering as specified in the user input
- Lock counts in typical monitoring scenarios are under 100; performance optimization for higher counts is best-effort
