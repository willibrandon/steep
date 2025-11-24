# Feature Specification: Tables & Statistics Viewer

**Feature Branch**: `005-tables-statistics`
**Created**: 2025-11-24
**Status**: Draft
**Input**: User description: "Implement Tables and Statistics Viewer with hierarchical database browser (Database → Schema → Table). Display table statistics including size, row count, bloat percentage, and cache hit ratio. Show index usage statistics with scan counts and unused index detection. Include table details panel showing columns, constraints, and foreign keys. Support bloat estimation using pgstattuple extension with graceful fallback. Auto-install pgstattuple via CREATE EXTENSION with user confirmation if not available."

## Clarifications

### Session 2025-11-24

- Q: Should system schemas (pg_catalog, information_schema, pg_toast) be visible in the hierarchy? → A: Hidden by default with toggle key (`P`) to show/hide system schemas
- Q: Should pgstattuple install prompt reappear if user declines? → A: Remember preference for current session only (prompt once per app launch)
- Q: How should partitioned tables be displayed? → A: Hierarchical - parent table with expandable child partitions nested underneath
- Q: What should the "Size" column show in the table list? → A: Total size (table + indexes + TOAST data)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse Schema Hierarchy (Priority: P1)

As a DBA, I want to browse databases, schemas, and tables in a hierarchical view so that I can explore and understand the database structure without leaving the terminal.

**Why this priority**: Schema navigation is the foundation for all other table statistics features. Without the ability to browse and select tables, users cannot access any statistics or perform maintenance operations.

**Independent Test**: Can be fully tested by navigating through the hierarchy (expand/collapse nodes, select tables) and verifies that the tree view correctly displays all schemas and tables in the connected database.

**Acceptance Scenarios**:

1. **Given** user is connected to a PostgreSQL database, **When** they press `5` to open the Tables view, **Then** they see a hierarchical list of schemas with expandable nodes
2. **Given** the Tables view is open, **When** user presses `Enter` or right arrow on a collapsed schema, **Then** the schema expands to show all tables within it
3. **Given** a schema is expanded, **When** user presses `Enter` or left arrow on it, **Then** the schema collapses to hide its tables
4. **Given** the Tables view is open, **When** user presses `j` or down arrow, **Then** cursor moves to the next item in the list
5. **Given** the Tables view is open, **When** user presses `k` or up arrow, **Then** cursor moves to the previous item in the list
6. **Given** the Tables view is open with system schemas hidden (default), **When** user presses `P`, **Then** system schemas (pg_catalog, information_schema, pg_toast) become visible
7. **Given** the Tables view is open with system schemas visible, **When** user presses `P`, **Then** system schemas are hidden again
8. **Given** a schema contains partitioned tables, **When** user views the table list, **Then** parent partition tables are shown with an expandable indicator
9. **Given** a partitioned parent table is collapsed, **When** user expands it, **Then** child partition tables are shown nested underneath the parent

---

### User Story 2 - View Table Size and Row Statistics (Priority: P1)

As a DBA, I want to see table sizes and row counts so that I can understand storage usage and identify tables that may need attention.

**Why this priority**: Size and row count information is essential for capacity planning and identifying large tables that may impact performance. This is core functionality that DBAs need for daily monitoring.

**Independent Test**: Can be fully tested by selecting any table and verifying that size (in MB) and row count statistics are displayed accurately.

**Acceptance Scenarios**:

1. **Given** a schema is expanded, **When** user views the table list, **Then** each table row displays: Name, Total Size (MB, includes table + indexes + TOAST), Row Count, Cache Hit %
2. **Given** a table is selected, **When** user views table details, **Then** they see size breakdown: table heap size, index size, and TOAST size separately
3. **Given** multiple tables exist, **When** user presses `s` to cycle sort columns, **Then** the table list sorts by the selected column (Name, Size, Row Count, Cache Hit %)
4. **Given** the table list is sorted, **When** user presses `S`, **Then** the sort direction toggles between ascending and descending

---

### User Story 3 - View Index Usage Statistics (Priority: P2)

As a DBA, I want to see index usage statistics so that I can identify unused or inefficient indexes that may be wasting storage and slowing down write operations.

**Why this priority**: Index optimization is important for performance tuning but is less urgent than basic table browsing and size information. DBAs typically analyze index usage periodically rather than continuously.

**Independent Test**: Can be fully tested by selecting a table and viewing its indexes with scan counts, size, and usage indicators.

**Acceptance Scenarios**:

1. **Given** a table is selected, **When** user views table details, **Then** they see a list of indexes with: Name, Size (MB), Scans, Rows Read, Cache Hit %
2. **Given** an index has zero scans since last statistics reset, **When** the index list is displayed, **Then** that index row is highlighted in yellow as "unused"
3. **Given** the index list is displayed, **When** user presses `y` with an index selected, **Then** the index name is copied to the clipboard
4. **Given** the index list is displayed, **When** user presses `Tab`, **Then** focus switches between table list and index list

---

### User Story 4 - View Bloat Estimates (Priority: P2)

As a DBA, I want to see bloat estimates for tables and indexes so that I can plan VACUUM operations and maintain database health.

**Why this priority**: Bloat detection requires the optional pgstattuple extension and represents advanced monitoring. Basic table statistics should work without this extension.

**Independent Test**: Can be fully tested by viewing bloat percentage on tables when pgstattuple is available, and verifying graceful fallback when it is not.

**Acceptance Scenarios**:

1. **Given** pgstattuple extension is installed, **When** user views table statistics, **Then** a Bloat % column is displayed with accurate estimates
2. **Given** a table has bloat exceeding 20%, **When** the table list is displayed, **Then** that table row is highlighted in red as critical
3. **Given** a table has bloat between 10-20%, **When** the table list is displayed, **Then** that table row is highlighted in yellow as warning
4. **Given** pgstattuple extension is NOT installed, **When** user views table statistics, **Then** the Bloat % column shows "N/A" and a message indicates the extension is not available

---

### User Story 5 - Auto-Install pgstattuple Extension (Priority: P2)

As a DBA, I want Steep to offer to install the pgstattuple extension if it's not available so that I can enable bloat detection without leaving the application.

**Why this priority**: This is a convenience feature that enhances bloat detection. It depends on Story 4 and requires database write permissions, making it less universally applicable.

**Independent Test**: Can be fully tested by connecting to a database without pgstattuple, receiving the install prompt, and confirming or declining the installation.

**Acceptance Scenarios**:

1. **Given** pgstattuple is not installed and user has CREATE EXTENSION privilege, **When** user opens the Tables view, **Then** a confirmation dialog appears offering to install the extension
2. **Given** the install confirmation dialog is shown, **When** user confirms with `y` or `Enter`, **Then** the system attempts to install pgstattuple and shows a success toast message
3. **Given** the install confirmation dialog is shown, **When** user declines with `n` or `Esc`, **Then** the dialog closes, the view continues without bloat data, and the prompt is suppressed for the remainder of the session
4. **Given** user previously declined the install prompt in this session, **When** user navigates away and returns to the Tables view, **Then** the install prompt does NOT appear again
5. **Given** installation is attempted but fails due to insufficient privileges, **When** the error occurs, **Then** a clear error message is displayed explaining the required permissions
6. **Given** the application is running in readonly mode, **When** user opens the Tables view without pgstattuple, **Then** no install prompt is shown (readonly mode prevents CREATE EXTENSION)

---

### User Story 6 - View Table Details Panel (Priority: P2)

As a DBA, I want to see detailed table information including columns, constraints, and foreign keys so that I can understand table structure without querying system catalogs manually.

**Why this priority**: While useful for exploration, detailed schema information is available through other tools and is less critical than monitoring statistics.

**Independent Test**: Can be fully tested by selecting a table and pressing Enter or `d` to open the details panel, verifying column definitions, constraints, and foreign keys are displayed.

**Acceptance Scenarios**:

1. **Given** a table is selected, **When** user presses `Enter` or `d`, **Then** a details panel opens showing column definitions (name, type, nullable, default)
2. **Given** the details panel is open, **When** user views constraints section, **Then** they see primary keys, unique constraints, check constraints, and foreign keys
3. **Given** the details panel is open, **When** user presses `Esc` or `q`, **Then** the panel closes and focus returns to the table list
4. **Given** the details panel is open with a table name visible, **When** user presses `y`, **Then** the table name (schema.table format) is copied to clipboard

---

### User Story 7 - Execute Maintenance Operations (Priority: P3)

As a DBA, I want to execute VACUUM, ANALYZE, or REINDEX on selected tables so that I can maintain database health directly from the monitoring interface.

**Why this priority**: Maintenance operations are powerful but potentially disruptive. They require careful confirmation and are typically performed infrequently. Core monitoring features should be completed first.

**Independent Test**: Can be fully tested by selecting a table, initiating a VACUUM operation, confirming via dialog, and observing the operation result.

**Acceptance Scenarios**:

1. **Given** a table is selected and application is NOT in readonly mode, **When** user presses `v`, **Then** a confirmation dialog appears for VACUUM operation
2. **Given** a table is selected and application is NOT in readonly mode, **When** user presses `a`, **Then** a confirmation dialog appears for ANALYZE operation
3. **Given** a table is selected and application is NOT in readonly mode, **When** user presses `r`, **Then** a confirmation dialog appears for REINDEX operation
4. **Given** a confirmation dialog is shown, **When** user confirms the operation, **Then** the operation executes and a toast message shows success or failure
5. **Given** application is in readonly mode, **When** user presses `v`, `a`, or `r`, **Then** a toast message indicates the operation is disabled in readonly mode

---

### Edge Cases

- What happens when a database has hundreds of schemas/tables? The view must handle large lists with smooth scrolling and should limit initial data fetch to visible items.
- What happens when table statistics are unavailable due to permissions? Display "N/A" for restricted statistics and show a warning message.
- What happens when a table is dropped while viewing? Handle gracefully by removing the table from the list on next refresh and showing a toast if the selected table was dropped.
- What happens when bloat calculation takes longer than expected? Show a spinner for in-progress bloat calculations and timeout after 5 seconds.
- What happens when the user's terminal is too small? Gracefully degrade by hiding less critical columns and showing a minimum width warning if below 80 columns.
- What happens when the connected database has no user tables? Display an empty state message: "No user tables found in this database."
- What happens when CREATE EXTENSION fails due to insufficient disk space or other system errors? Display the specific error message from PostgreSQL in the toast.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST display a hierarchical view of schemas and tables accessible via the `5` key
- **FR-002**: System MUST allow navigation using keyboard shortcuts (`j`/`k` or arrows for movement, `Enter` for expand/collapse)
- **FR-002a**: System MUST hide system schemas (pg_catalog, information_schema, pg_toast) by default and provide a toggle key (`P`) to show/hide them
- **FR-002b**: System MUST display partitioned tables hierarchically with parent tables expandable to show child partitions
- **FR-003**: System MUST display table statistics: Name, Total Size (MB, includes table + indexes + TOAST), Row Count, and Cache Hit Ratio
- **FR-004**: System MUST display index statistics: Name, Size (MB), Index Scans, Rows Read, and Cache Hit Ratio
- **FR-005**: System MUST highlight unused indexes (0 scans) in yellow
- **FR-006**: System MUST support sorting tables by any displayed column using `s`/`S` keys
- **FR-007**: System MUST auto-refresh table statistics every 30 seconds
- **FR-008**: System MUST display bloat percentage when pgstattuple extension is available
- **FR-009**: System MUST highlight high bloat (>20%) in red and moderate bloat (10-20%) in yellow
- **FR-010**: System MUST gracefully skip bloat statistics when pgstattuple is unavailable, showing "N/A"
- **FR-011**: System MUST offer to install pgstattuple extension with user confirmation when not available (prompt once per session; if declined, do not prompt again until app restart)
- **FR-012**: System MUST respect readonly mode by disabling extension installation and maintenance operations
- **FR-013**: System MUST display table details panel with columns, constraints, and foreign keys
- **FR-014**: System MUST provide copy-to-clipboard functionality for table/index names via `y` key
- **FR-015**: System MUST display help overlay via `h` key with all available keyboard shortcuts
- **FR-016**: System MUST execute all statistics queries within 500ms
- **FR-017**: System MUST support VACUUM, ANALYZE, and REINDEX operations with confirmation dialogs (when not in readonly mode)
- **FR-018**: System MUST display toast messages for operation results (success/failure)

### Key Entities

- **Schema**: Represents a PostgreSQL schema containing tables. Attributes: name, owner.
- **Table**: Represents a database table with statistics. Attributes: name, schema, size, row count, bloat percentage, cache hit ratio, last vacuum time, last analyze time, is_partitioned (boolean), parent_table (reference to parent if this is a partition).
- **Index**: Represents a table index with usage statistics. Attributes: name, table reference, size, scan count, rows read, cache hit ratio.
- **TableColumn**: Represents a column within a table. Attributes: name, data type, nullable, default value, position.
- **Constraint**: Represents a table constraint. Attributes: name, type (PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK), definition.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can navigate to any table in the database hierarchy within 5 keyboard actions (view open → schema expand → table select)
- **SC-002**: Table statistics display refreshes and renders within 1 second, including after auto-refresh
- **SC-003**: Users can identify unused indexes at a glance through color coding without reading individual statistics
- **SC-004**: Users can identify high-bloat tables requiring VACUUM at a glance through color coding
- **SC-005**: 100% of displayed statistics match values from equivalent direct SQL queries
- **SC-006**: Users can complete a table maintenance operation (VACUUM/ANALYZE/REINDEX) within 3 interactions (select table → press shortcut → confirm)
- **SC-007**: Extension installation prompt appears within 2 seconds of opening Tables view when pgstattuple is unavailable
- **SC-008**: Users report finding the information they need 90% of the time without switching to another tool
