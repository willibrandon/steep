# Feature Specification: Query Performance Monitoring

**Feature Branch**: `003-query-performance`
**Created**: 2025-11-21
**Status**: Draft
**Input**: Implement Query Performance Monitoring view without requiring pg_stat_statements extension. Use PostgreSQL query logging as primary data source with log parsing. Fall back to pg_stat_activity sampling when logging unavailable.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View Top Queries by Execution Time (Priority: P1)

As a DBA, I want to see the top queries ranked by total execution time so I can identify the most impactful slow queries that need optimization.

**Why this priority**: Identifying time-consuming queries is the most critical performance monitoring task. Slow queries directly impact user experience and database resource utilization.

**Independent Test**: Can be fully tested by navigating to the Queries view and observing queries sorted by total execution time. Delivers immediate value by showing which queries consume the most database time.

**Acceptance Scenarios**:

1. **Given** the Queries view is displayed, **When** I view the "By Time" tab (default), **Then** I see queries ranked by total execution time in descending order showing Query, Calls, Total Time, Mean Time, and Rows
2. **Given** the Queries view is displayed, **When** I press `s` to sort, **Then** I can select any column to re-sort the data
3. **Given** the Queries view is displayed, **When** 5 seconds pass, **Then** the data auto-refreshes with updated statistics

---

### User Story 2 - View Top Queries by Call Count (Priority: P1)

As a DBA, I want to see the top queries ranked by call count so I can identify frequently executed queries that may benefit from caching or optimization.

**Why this priority**: High-frequency queries compound even small inefficiencies. This is essential for identifying candidates for prepared statements, connection pooling optimization, or result caching.

**Independent Test**: Can be fully tested by switching to the "By Calls" tab and observing queries sorted by execution count. Delivers value by revealing query patterns and hotspots.

**Acceptance Scenarios**:

1. **Given** the Queries view is displayed, **When** I switch to the "By Calls" tab using arrow keys, **Then** I see queries ranked by call count in descending order
2. **Given** a query has been executed multiple times, **When** viewing the "By Calls" tab, **Then** the call count accurately reflects the number of executions captured

---

### User Story 3 - View Top Queries by Rows Returned (Priority: P1)

As a DBA, I want to see the top queries ranked by rows returned so I can identify queries that may need result limiting or pagination.

**Why this priority**: Queries returning excessive rows indicate missing WHERE clauses or unbounded result sets that can cause memory and network issues.

**Independent Test**: Can be fully tested by switching to the "By Rows" tab and observing queries sorted by row count. Delivers value by identifying potential resource hogs.

**Acceptance Scenarios**:

1. **Given** the Queries view is displayed, **When** I switch to the "By Rows" tab, **Then** I see queries ranked by total rows returned in descending order

---

### User Story 4 - View EXPLAIN Plans (Priority: P2)

As a DBA, I want to view EXPLAIN plans for queries so I can understand query execution strategy and identify optimization opportunities.

**Why this priority**: EXPLAIN plans are essential for query optimization but require the foundational query list from P1 stories. Users must first identify problem queries before analyzing their plans.

**Independent Test**: Can be tested by selecting a query and pressing `e` to view its EXPLAIN plan. Delivers value by revealing index usage, join strategies, and cost estimates.

**Acceptance Scenarios**:

1. **Given** a query is selected in the Queries view, **When** I press `e`, **Then** I see the EXPLAIN plan output in a formatted, readable view
2. **Given** the EXPLAIN plan is displayed, **When** I press `Esc` or `q`, **Then** I return to the query list
3. **Given** a query cannot be explained (syntax error or permissions), **When** I press `e`, **Then** I see a clear error message explaining why

---

### User Story 5 - Search and Filter Queries (Priority: P2)

As a DBA, I want to search and filter queries by text pattern so I can find specific query types or tables quickly.

**Why this priority**: Search is powerful but secondary to viewing the data. Users first need to see what queries exist before filtering them.

**Independent Test**: Can be tested by pressing `/` and entering a search pattern. Delivers value by enabling quick navigation in large query sets.

**Acceptance Scenarios**:

1. **Given** the Queries view is displayed, **When** I press `/` and enter a search pattern, **Then** the list filters to show only matching queries
2. **Given** I enter a regex pattern like `SELECT.*users`, **When** the filter is applied, **Then** I see all queries matching that pattern
3. **Given** a search filter is active, **When** I clear the search, **Then** the full query list is restored

---

### User Story 6 - Copy Query to Clipboard (Priority: P2)

As a DBA, I want to copy query text to clipboard so I can paste it into other tools for analysis or execution.

**Why this priority**: Convenience feature that enhances workflow but not essential for core monitoring functionality.

**Independent Test**: Can be tested by selecting a query and pressing `y`. Delivers value by enabling easy query transfer to other tools.

**Acceptance Scenarios**:

1. **Given** a query is selected, **When** I press `y`, **Then** the full query text is copied to system clipboard
2. **Given** the query was copied, **When** I paste in another application, **Then** I see the complete, un-truncated query text

---

### User Story 7 - Reset Statistics (Priority: P3)

As a DBA, I want to reset query statistics so I can start fresh monitoring after configuration changes or deployments.

**Why this priority**: Occasional maintenance task, not part of daily monitoring workflow.

**Independent Test**: Can be tested by pressing `R` and confirming the reset. Delivers value by enabling clean-slate analysis.

**Acceptance Scenarios**:

1. **Given** I am in the Queries view, **When** I press `R`, **Then** I see a confirmation dialog asking to confirm statistics reset
2. **Given** the confirmation dialog is displayed, **When** I confirm, **Then** all query statistics are cleared and the view shows empty results
3. **Given** the confirmation dialog is displayed, **When** I cancel, **Then** statistics remain unchanged

---

### Edge Cases

- What happens when PostgreSQL logging is disabled and cannot be enabled (no superuser privileges)?
  - System falls back to pg_stat_activity sampling with a status message indicating limited data accuracy

- What happens when log files are not accessible on a local database (permissions issue)?
  - System displays clear error message with guidance on configuring log access, then falls back to sampling

- What happens when the SQLite database is corrupted or inaccessible?
  - System attempts to recreate the database, with warning to user about lost historical data

- How does system handle very long queries (>100 characters)?
  - Query text is truncated in table view with `...`, full text available via clipboard copy or detail view

- What happens when EXPLAIN fails on a query?
  - System displays error message with reason (e.g., "Query contains bind parameters" or "Permission denied")

## Requirements *(mandatory)*

### Functional Requirements

#### Data Collection

- **FR-001**: System MUST detect PostgreSQL logging configuration by querying `log_min_duration_statement`
- **FR-002**: System MUST parse PostgreSQL log files using the honeytail library when logging is enabled
- **FR-003**: System MUST fall back to pg_stat_activity sampling when log parsing is unavailable
- **FR-004**: System MUST fingerprint queries using pg_query_go to normalize and deduplicate query patterns
- **FR-005**: System MUST aggregate query statistics client-side (calls, total time, mean time, min/max, rows)
- **FR-006**: System MUST persist aggregated statistics in a local SQLite database
- **FR-006a**: System MUST automatically delete query statistics older than 7 days on each refresh cycle

#### User Interface

- **FR-007**: System MUST provide a tabbed interface with three tabs: "By Time", "By Calls", "By Rows"
- **FR-008**: System MUST display query table showing: Query (normalized, truncated to 100 chars), Calls, Total Time, Mean Time, Rows
- **FR-009**: System MUST allow switching tabs using left/right arrow keys
- **FR-010**: System MUST allow sorting by any column with `s` key
- **FR-011**: System MUST support vim-style navigation (j/k for up/down, g/G for top/bottom)
- **FR-012**: System MUST provide search functionality via `/` key with regex pattern support

#### Actions

- **FR-013**: System MUST execute EXPLAIN (FORMAT JSON) and display formatted output when user presses `e`
- **FR-014**: System MUST copy full query text to system clipboard when user presses `y`
- **FR-015**: System MUST reset statistics (truncate SQLite table) when user presses `R` and confirms

#### Configuration & Auto-Setup

- **FR-016**: System MUST offer to auto-enable logging via `ALTER SYSTEM SET log_min_duration_statement = 0` with user confirmation when logging is disabled
- **FR-017**: System MUST call `pg_reload_conf()` after modifying logging settings
- **FR-018**: System MUST provide guidance when auto-enable fails (insufficient permissions)

#### Performance

- **FR-019**: System MUST auto-refresh data every 5 seconds (configurable via settings)
- **FR-020**: System MUST limit displayed results to top 50 queries per view
- **FR-021**: All queries and operations MUST complete within 500ms

### Key Entities

- **QueryStats**: Represents aggregated statistics for a normalized query pattern
  - Fingerprint (unique identifier from normalization)
  - Normalized query text
  - Call count
  - Total execution time
  - Mean execution time
  - Min/Max execution time
  - Total rows returned
  - Last seen timestamp

- **DataSource**: Represents the active data collection method
  - Type (log_parsing, activity_sampling)
  - Status (active, degraded, unavailable)
  - Configuration details

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can identify the top 10 slowest queries within 10 seconds of opening the Queries view
- **SC-002**: Data refreshes automatically every 5 seconds without user interaction
- **SC-003**: Users can switch between tabs and sort columns in under 100ms response time
- **SC-004**: Query operations (EXPLAIN, copy, search) complete within 500ms
- **SC-005**: System maintains accurate query statistics across application restarts (SQLite persistence)
- **SC-006**: Users without superuser privileges can still use the feature with pg_stat_activity sampling fallback
- **SC-007**: Query fingerprinting correctly groups 95% of similar queries (with different parameter values)

## Clarifications

### Session 2025-11-21

- Q: How long should the SQLite statistics database retain query data before automatic cleanup? → A: 7 days rolling window
- Q: When connecting to a remote PostgreSQL server, how should the system access log files for parsing? → A: Only support log parsing for local databases; use sampling fallback for remote

## Assumptions

- PostgreSQL log format is standard and parseable by honeytail library
- Log parsing only supported for local PostgreSQL instances; remote connections automatically use pg_stat_activity sampling
- SQLite database stored in user's config directory (~/.config/steep/)
- Query fingerprinting via pg_query_go handles PostgreSQL syntax correctly
- System clipboard access is available on the user's platform
- Auto-enabling logging requires superuser or appropriate ALTER SYSTEM permissions
