# Feature Specification: Log Viewer

**Feature Branch**: `009-log-viewer`
**Created**: 2025-11-27
**Status**: Draft
**Input**: Implement Log Viewer for monitoring PostgreSQL server logs in real-time with tail capability, severity color-coding, filtering, and timestamp navigation.

## Clarifications

### Session 2025-11-27

- Q: What is the maximum log history buffer size for scrollback? → A: 10,000 entries (balances memory usage ~5-10MB with usability)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Real-Time Log Monitoring (Priority: P1)

As a DBA, I want to tail server logs in real-time to monitor database activity as it happens, so I can quickly identify and respond to issues without switching to external tools.

**Why this priority**: Real-time log visibility is the core value proposition. Without this, the feature provides no utility. DBAs need immediate visibility into server activity for incident response and troubleshooting.

**Independent Test**: Can be fully tested by opening the log viewer and observing that new log entries appear automatically as the database generates them. Delivers immediate value for monitoring database activity.

**Acceptance Scenarios**:

1. **Given** the log viewer is open with follow mode enabled, **When** the PostgreSQL server writes a new log entry, **Then** the entry appears in the viewer within 1 second without user action
2. **Given** the log viewer is displaying logs, **When** the user presses `f` to toggle follow mode off, **Then** auto-scrolling stops and the user can scroll freely through historical logs
3. **Given** follow mode is disabled, **When** the user presses `f` to enable follow mode, **Then** the view jumps to the newest logs and resumes auto-scrolling
4. **Given** the database is idle, **When** no new logs are generated, **Then** the viewer shows existing logs with a timestamp indicating last refresh
5. **Given** the user is on any other view, **When** the user presses `9`, **Then** the log viewer opens showing the most recent logs

---

### User Story 2 - Filter by Severity Level (Priority: P2)

As a DBA, I want to filter logs by severity level (ERROR, WARNING, INFO, DEBUG) so I can focus on important messages without being overwhelmed by routine entries.

**Why this priority**: Filtering transforms raw log data into actionable information. This is essential for production environments where log volume can be high, but depends on P1 for log access.

**Independent Test**: Can be tested by applying severity filters and verifying only matching entries are displayed. Delivers focused troubleshooting capability.

**Acceptance Scenarios**:

1. **Given** the log viewer is displaying mixed severity logs, **When** the user enters `:level error`, **Then** only ERROR-level entries are displayed
2. **Given** a level filter is active, **When** the user enters `:level warning`, **Then** the filter changes to show only WARNING-level entries
3. **Given** a level filter is active, **When** the user enters `:level clear` or `:level all`, **Then** all severity levels are displayed again
4. **Given** ERROR severity filter is active, **When** new ERROR logs arrive in follow mode, **Then** they appear in the filtered view
5. **Given** the log viewer is displaying logs, **When** the user views the display, **Then** entries are color-coded: Red (ERROR), Yellow (WARNING), White (INFO), Gray (DEBUG)

---

### User Story 3 - Search by Text Pattern (Priority: P2)

As a DBA, I want to search logs by text pattern using regex to find specific events, error messages, or database objects across all log entries.

**Why this priority**: Pattern search enables targeted investigation of specific issues. Combined with severity filtering, this provides comprehensive log analysis capability.

**Independent Test**: Can be tested by entering a search pattern and verifying matching entries are highlighted or filtered. Delivers investigation capability for specific issues.

**Acceptance Scenarios**:

1. **Given** the log viewer is displaying logs, **When** the user presses `/` and enters a pattern (e.g., "connection"), **Then** matching entries are highlighted in the current view
2. **Given** a search pattern is active, **When** the user presses `n`, **Then** the view navigates to the next matching entry
3. **Given** a search pattern is active, **When** the user presses `N`, **Then** the view navigates to the previous matching entry
4. **Given** the user enters a regex pattern like `error.*timeout`, **When** searching, **Then** entries matching the regex are found
5. **Given** a search is active, **When** the user presses `Escape`, **Then** the search is cleared and all entries are shown without highlighting

---

### User Story 4 - Navigate by Timestamp (Priority: P3)

As a DBA, I want to navigate logs by timestamp to review historical events around a known incident time, so I can investigate issues that occurred at specific times.

**Why this priority**: Timestamp navigation is valuable for post-incident analysis but is less critical than real-time monitoring and filtering. Most urgent troubleshooting uses the other capabilities first.

**Independent Test**: Can be tested by entering a timestamp and verifying the view jumps to that time period. Delivers historical investigation capability.

**Acceptance Scenarios**:

1. **Given** the log viewer is displaying logs, **When** the user enters `:goto 2025-11-27 14:30`, **Then** the view scrolls to logs around that timestamp
2. **Given** the user enters a timestamp that has no logs, **When** the command executes, **Then** the view shows the nearest available logs with an informative message
3. **Given** the log viewer is displaying logs, **When** the user uses `g` or `G`, **Then** the view jumps to the oldest or newest logs respectively
4. **Given** the user wants to navigate incrementally, **When** using `j/k` or arrow keys, **Then** the view scrolls line by line through the log history

---

### Edge Cases

- What happens when log files are not accessible (permissions, file not found)?
  - Display clear error message with configuration instructions for enabling logging or granting access
- What happens when PostgreSQL logging is not configured?
  - Prompt user to enable logging configuration (using existing `EnableLogging()` pattern from queries view)
  - If user accepts, configure logging via `ALTER SYSTEM SET` and `pg_reload_conf()`
  - If user declines, show guidance on required PostgreSQL settings
  - Respect readonly mode - show configuration guidance only, no prompt to enable
- How does the system handle log rotation while viewing?
  - Seamlessly continue displaying logs, detecting new log files as they appear
- What happens when log entries span multiple lines (stack traces, DETAIL messages)?
  - Parse and display multi-line entries as single logical units with proper indentation
- How does the system handle extremely large log files?
  - Load logs incrementally from the end of file, supporting scrollback with pagination
- What happens when csvlog format is configured vs stderr format?
  - Auto-detect format based on PostgreSQL configuration and parse accordingly
- What happens in read-only mode?
  - Log viewing remains fully functional (it is inherently read-only)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST display PostgreSQL server logs in reverse chronological order (newest first)
- **FR-002**: System MUST support follow mode that auto-scrolls to show new log entries as they arrive
- **FR-003**: System MUST refresh logs every 1 second while in follow mode
- **FR-004**: System MUST toggle follow mode on/off with the `f` key
- **FR-005**: System MUST color-code log entries by severity: Red (ERROR), Yellow (WARNING), White (INFO), Gray (DEBUG)
- **FR-006**: System MUST parse log metadata: timestamp, process ID (PID), database name, user name, and message content
- **FR-007**: System MUST filter logs by severity level using `:level <level>` command
- **FR-008**: System MUST support text pattern search with regex using `/` key
- **FR-009**: System MUST support navigate to next/previous search match with `n`/`N` keys
- **FR-010**: System MUST support timestamp-based navigation using `:goto <timestamp>` command
- **FR-011**: System MUST support keyboard navigation: `j/k` for line scroll, `g/G` for top/bottom
- **FR-012**: System MUST be accessible via the `9` key from any view
- **FR-013**: System MUST access logs via file system reads OR pg_read_file() function based on available permissions
- **FR-014**: System MUST auto-detect log format (csvlog or stderr) and parse accordingly
- **FR-015**: System MUST handle multi-line log entries (stack traces, DETAIL/HINT messages) as single logical units
- **FR-016**: System MUST display clear error message with configuration guidance when logs are inaccessible
- **FR-017**: System MUST handle log file rotation gracefully without losing context
- **FR-018**: System MUST support incremental loading for large log files (read from end, paginate backwards) with a maximum buffer of 10,000 entries in memory
- **FR-019**: System MUST prompt user to enable logging configuration when logging is disabled (unless in readonly mode)
- **FR-020**: System MUST detect logging status on view initialization using existing `CheckLoggingStatus()` infrastructure

### Key Entities

- **LogEntry**: A single parsed log record containing timestamp, severity level, PID, database, user, and message
- **LogSource**: The source of log data - either file system path or pg_read_file() query
- **LogFilter**: Active filter criteria including severity level, search pattern, and timestamp range
- **LogViewer**: The view state including current position, follow mode status, and active filters

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: New log entries appear in the viewer within 1 second of being written to the PostgreSQL log
- **SC-002**: Users can scroll through 10,000 historical log entries without perceptible lag
- **SC-003**: Severity filtering takes effect within 100ms of entering the filter command
- **SC-004**: Search pattern matching highlights results within 500ms for typical log volumes
- **SC-005**: Log viewer accessible via single keypress (`9`) from any screen
- **SC-006**: 100% of log entries display correct severity color-coding
- **SC-007**: Users can identify ERROR-level issues within 3 seconds of viewing logs (via color coding and optional filtering)
- **SC-008**: Clear error messages displayed within 2 seconds when log access fails, including specific remediation steps

## Assumptions

- PostgreSQL server has logging enabled with `logging_collector = on` (or user accepts prompt to enable it)
- Log files are in a standard location discoverable via `pg_settings` or configuration
- User has either file system read access to log directory OR superuser/pg_read_server_files role for pg_read_file()
- Log format is either csvlog, jsonlog, or stderr (line-based with prefix) - not syslog
- Terminal supports 256-color or true-color for optimal severity color display
- Log entries include timestamps in a parseable format (ISO 8601 or PostgreSQL default)

## Existing Infrastructure to Leverage

The following existing components should be reused or extended:

- **`internal/monitors/queries/monitor.go`**: `CheckLoggingStatus()` returns LoggingStatus with Enabled, LogDir, LogPattern, LogLinePrefix; `EnableLogging()` configures logging via ALTER SYSTEM SET
- **`internal/monitors/queries/log_collector.go`**: LogCollector with CSV/JSON log parsing, file position tracking, QueryEvent model
- **`internal/ui/views/queries/view.go`**: `ModeConfirmEnableLogging` prompt pattern with dialog rendering and Y/N handling
- **`internal/db/queries/config.go`**: `AlterSystemSet()`, `ReloadConfig()` for configuration changes
