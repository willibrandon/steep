# Feature Specification: SQL Editor & Execution

**Feature Branch**: `007-sql-editor`
**Created**: 2025-11-25
**Status**: Draft
**Input**: User description: "Implement SQL Editor & Execution view accessible via '7' key as a professional-grade SQL IDE experience within the TUI"

## Clarifications

### Session 2025-11-25

- Q: Should executed queries be logged for audit/troubleshooting purposes? → A: Log to application log file with timestamp and execution status
- Q: How should reconnection be handled when database connection is lost? → A: Automatic reconnection with notification - retry silently, notify user of status

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Write and Execute SQL Queries (Priority: P1)

As a DBA, I want to write multi-line SQL queries and execute them against my connected database so I can interact with the database interactively without leaving the Steep monitoring interface.

**Why this priority**: This is the core functionality of the SQL Editor. Without the ability to write and execute queries, no other features have value. DBAs need this capability to investigate issues discovered through monitoring views.

**Independent Test**: Can be fully tested by opening the SQL Editor view, typing a query, executing it, and verifying the query runs against the database. Delivers immediate value for ad-hoc database interaction.

**Acceptance Scenarios**:

1. **Given** the user is connected to a database, **When** they press '7' from any view, **Then** they see the SQL Editor view with an empty query input area
2. **Given** the SQL Editor is open with a query entered, **When** the user presses the execute shortcut, **Then** the query is sent to the database and results appear in the results pane
3. **Given** a query is executing, **When** the user presses the cancel key, **Then** the query execution is cancelled and the editor returns to ready state
4. **Given** a query execution fails, **When** the error is returned from the database, **Then** the error message is displayed clearly with line/position information if available

---

### User Story 2 - View Query Results in Paginated Table (Priority: P1)

As a DBA, I want to see query results displayed in a scrollable, paginated table so I can review large result sets efficiently without overwhelming the terminal display.

**Why this priority**: Results display is essential for the SQL Editor to be useful. Users must be able to see and navigate through query output to understand their data.

**Independent Test**: Can be tested by executing a query that returns 500+ rows and verifying pagination works, row counts display correctly, and navigation is smooth. Delivers value immediately after query execution works.

**Acceptance Scenarios**:

1. **Given** a query returns 250 rows, **When** results are displayed, **Then** the first 100 rows show on page 1 with "Page 1/3" indicator
2. **Given** results are paginated, **When** the user presses the next page key, **Then** the display advances to show the next 100 rows
3. **Given** results contain NULL values, **When** results are displayed, **Then** NULL values appear with distinct visual styling (dimmed text)
4. **Given** results are displayed, **When** the user navigates to a row, **Then** that row is highlighted as the current selection

---

### User Story 3 - Syntax Highlighting for SQL (Priority: P2)

As a DBA, I want SQL syntax highlighting on executed queries and in the history display so I can read and understand queries more easily.

**Why this priority**: Syntax highlighting improves readability and user experience but is not essential for basic functionality. The editor works without it, just less pleasantly.

**Independent Test**: Can be tested by executing a query and verifying the executed query header shows colored keywords, strings, and comments. Delivers improved readability.

**Acceptance Scenarios**:

1. **Given** a query has been executed, **When** viewing the query in results header, **Then** SQL keywords appear in distinct color, strings in another, numbers in another
2. **Given** the history panel is open, **When** viewing previous queries, **Then** each query displays with syntax highlighting applied

---

### User Story 4 - Transaction Management (Priority: P2)

As a DBA, I want to manage database transactions (BEGIN, COMMIT, ROLLBACK) within the SQL Editor so I can test changes safely before making them permanent.

**Why this priority**: Transaction control is important for safe database interaction but not required for read-only queries. Many DBAs use transactions for testing changes.

**Independent Test**: Can be tested by beginning a transaction, executing DDL/DML, then rolling back to verify changes are not persisted. Delivers safe testing capability.

**Acceptance Scenarios**:

1. **Given** no transaction is active, **When** the user issues a begin transaction command, **Then** a transaction indicator appears in the status bar
2. **Given** a transaction is active, **When** the user issues a commit command, **Then** the transaction is committed and the indicator disappears
3. **Given** a transaction is active, **When** the user issues a rollback command, **Then** the transaction is rolled back and the indicator disappears
4. **Given** a transaction is active, **When** the user attempts to execute DDL, **Then** a warning is displayed before execution proceeds

---

### User Story 5 - Natural Keyboard Shortcuts (Priority: P2)

As a DBA, I want keyboard shortcuts that feel natural (similar to VS Code or vim) so I can work efficiently without learning a new navigation paradigm.

**Why this priority**: Efficient keyboard navigation is important for power users but the basic functionality works with minimal shortcuts. This enhances productivity.

**Independent Test**: Can be tested by verifying all documented shortcuts work correctly and feel consistent with established patterns. Delivers productivity improvements.

**Acceptance Scenarios**:

1. **Given** the editor has focus, **When** the user presses Tab, **Then** focus moves to the results pane
2. **Given** the results pane has focus, **When** the user presses j/k, **Then** the row selection moves down/up
3. **Given** results are displayed, **When** the user presses the copy cell key, **Then** the current cell value is copied to clipboard

---

### User Story 6 - Query History with Recall (Priority: P3)

As a DBA, I want to recall previously executed queries so I can re-run queries without retyping them and review what queries I've run during my session.

**Why this priority**: History is a convenience feature that enhances productivity but doesn't block core functionality. DBAs can manually retype queries if needed.

**Independent Test**: Can be tested by executing several queries, then using arrow keys at editor boundary to cycle through history. Delivers productivity gains.

**Acceptance Scenarios**:

1. **Given** multiple queries have been executed, **When** the cursor is at the top of the editor and user presses up arrow, **Then** the previous query replaces the current content
2. **Given** history navigation is active, **When** the user presses down arrow, **Then** the next (more recent) query is shown
3. **Given** history contains queries, **When** the user opens reverse search, **Then** they can type to filter history by content

---

### User Story 7 - Save Queries as Snippets (Priority: P3)

As a DBA, I want to save frequently used queries as named snippets so I can reuse common queries without retyping them across sessions.

**Why this priority**: Snippet management is a power-user feature that enhances long-term productivity but is not essential for basic SQL editing.

**Independent Test**: Can be tested by saving a query with a name, closing and reopening the application, then loading the saved snippet. Delivers reusability across sessions.

**Acceptance Scenarios**:

1. **Given** a query is in the editor, **When** the user issues a save command with a name, **Then** the query is persisted as a named snippet
2. **Given** snippets exist, **When** the user issues a load command with a name, **Then** the snippet content replaces the editor content
3. **Given** snippets exist, **When** the user opens the snippet browser, **Then** they see a list of saved snippets they can select from

---

### User Story 8 - Export Results (Priority: P3)

As a DBA, I want to export query results to CSV or JSON files so I can share data with colleagues or import into other tools.

**Why this priority**: Export is a convenience feature for data portability but doesn't affect core editing/execution functionality.

**Independent Test**: Can be tested by executing a query, then using export command to save results to a file and verifying file contents. Delivers data portability.

**Acceptance Scenarios**:

1. **Given** query results are displayed, **When** the user issues an export CSV command with filename, **Then** results are written to a CSV file
2. **Given** query results are displayed, **When** the user issues an export JSON command with filename, **Then** results are written as a JSON array of objects

---

### Edge Cases

- What happens when a query returns no rows? Display "0 rows returned" message in results pane
- What happens when a query returns millions of rows? Stream results with pagination, showing progress and allowing early cancellation
- What happens when the database connection is lost during execution? Automatically attempt reconnection silently, display notification of connection status change, and retry the failed operation if reconnection succeeds
- What happens when query execution exceeds timeout? Cancel query, display timeout message with elapsed time
- What happens when the user executes multiple statements? Execute sequentially, show results for each (or error on first failure)
- What happens when a snippet name already exists during save? Prompt for overwrite confirmation
- What happens when read-only mode is active and user tries DDL/DML? Block execution, display read-only mode warning

## Requirements *(mandatory)*

### Functional Requirements

**Core Editor (P1)**
- **FR-001**: System MUST provide a multi-line text input area for SQL queries with line numbers displayed
- **FR-002**: System MUST execute SQL queries against the connected database when user triggers execution
- **FR-003**: System MUST display a visual indicator showing which pane (editor or results) currently has focus
- **FR-004**: System MUST allow users to cancel a running query before completion
- **FR-005**: System MUST enforce a configurable query timeout (default 30 seconds)

**Results Display (P1)**
- **FR-006**: System MUST display query results in a tabular format with column headers
- **FR-007**: System MUST paginate large result sets (100 rows per page default)
- **FR-008**: System MUST display execution time and total row count for completed queries
- **FR-009**: System MUST display NULL values with distinct visual styling
- **FR-010**: System MUST allow row-by-row navigation through results
- **FR-011**: System MUST display database error messages clearly when queries fail

**Syntax Highlighting (P2)**
- **FR-012**: System MUST apply syntax highlighting to SQL in the executed query header display
- **FR-013**: System MUST apply syntax highlighting to queries shown in history

**Transaction Management (P2)**
- **FR-014**: System MUST support BEGIN, COMMIT, and ROLLBACK transaction commands
- **FR-015**: System MUST display transaction state indicator in the status bar when a transaction is active
- **FR-016**: System MUST display a warning before executing DDL statements within an active transaction
- **FR-017**: System MUST support SAVEPOINT creation and ROLLBACK TO savepoint

**Keyboard Navigation (P2)**
- **FR-018**: System MUST allow switching focus between editor and results pane via keyboard
- **FR-019**: System MUST support vim-style navigation (j/k) in results pane
- **FR-020**: System MUST support copying cell values to clipboard
- **FR-021**: System MUST support copying entire rows to clipboard
- **FR-022**: System MUST allow resizing the split between editor and results via keyboard

**Query History (P3)**
- **FR-023**: System MUST maintain in-memory history of executed queries (100 queries)
- **FR-024**: System MUST persist query history to storage for cross-session recall
- **FR-025**: System MUST allow recall of previous queries via arrow keys at editor boundary
- **FR-026**: System MUST support reverse search through history
- **FR-027**: System MUST detect and avoid storing consecutive duplicate queries

**Snippets (P3)**
- **FR-028**: System MUST allow saving current query content as a named snippet
- **FR-029**: System MUST persist snippets to storage for cross-session access
- **FR-030**: System MUST allow loading a saved snippet by name
- **FR-031**: System MUST provide a browsable list of saved snippets

**Export (P3)**
- **FR-032**: System MUST export results to CSV format
- **FR-033**: System MUST export results to JSON format (array of objects)

**Status & Information**
- **FR-034**: System MUST display connection information (host, database, user) in status bar
- **FR-035**: System MUST display keyboard shortcuts in footer area
- **FR-036**: System MUST respect read-only mode by blocking DDL/DML execution with warning

**Observability (P2)**
- **FR-037**: System MUST log executed queries to application log file with timestamp, execution status, duration, and row count for audit and troubleshooting

**Reliability (P2)**
- **FR-038**: System MUST automatically attempt reconnection when database connection is lost, without requiring user intervention
- **FR-039**: System MUST display a notification when connection status changes (disconnected, reconnecting, reconnected)
- **FR-040**: System MUST retry the failed query operation automatically if reconnection succeeds within a reasonable timeout

### Key Entities

- **Query**: SQL text entered by user, associated execution metadata (duration, row count, error status)
- **Result Set**: Collection of rows and columns returned by a query, with column names and data types
- **Transaction**: Database transaction state (none, active), associated savepoints
- **History Entry**: Executed query text with timestamp, stored for recall
- **Snippet**: Named query text saved for reuse, with creation/modification timestamps

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can write and execute a query within 5 seconds of opening the SQL Editor view
- **SC-002**: Query execution responds within the configured timeout, with queries under 500ms for typical monitoring queries
- **SC-003**: Results display within 100ms of query completion for result sets under 1000 rows
- **SC-004**: Users can navigate through 1000 rows of results using pagination without perceivable lag
- **SC-005**: 90% of standard SQL editing operations are achievable without reaching for mouse
- **SC-006**: Transaction state changes are reflected in UI within 100ms of command execution
- **SC-007**: Query history recall works instantly (under 50ms response time) for 100 stored queries
- **SC-008**: Snippet save and load operations complete within 200ms
- **SC-009**: Export of 10,000 rows completes within 5 seconds for CSV format
- **SC-010**: All keyboard shortcuts are documented in help overlay and footer hints
