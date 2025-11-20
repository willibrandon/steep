# Feature Specification: Foundation Infrastructure

**Feature Branch**: `001-foundation`
**Created**: 2025-11-19
**Status**: Draft
**Input**: User description: "Build the foundation infrastructure for Steep, a PostgreSQL monitoring TUI application. The application should initialize using Go with Bubbletea framework, establish database connections using pgx connection pooling, load configuration from YAML files, and provide a basic keyboard-driven interface with view switching capabilities. Include reusable UI components (table, status bar, help text) using Lipgloss for consistent styling. Focus on P1 user story: launching the app, connecting to PostgreSQL, and basic navigation. This is the foundation upon which all monitoring features will be built."

## Clarifications

### Session 2025-11-19

- Q: When the database connection drops, the application should attempt automatic reconnection with exponential backoff. What parameters should govern this retry behavior? → A: Initial delay: 1s, Max delay: 30s, Max attempts: 5 (balanced approach)
- Q: For operational debugging and troubleshooting, what logging output should the application provide? → A: Error and Warning to stderr, Info messages suppressed unless debug flag enabled (standard CLI practice)
- Q: The spec mentions supporting password/password_command for credentials. How should password storage be handled in the configuration file? → A: Support password_command only in config, no plaintext password field (secure, uses external password managers)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Launch Application and Connect to Database (Priority: P1)

As a DBA, I want to launch Steep from the command line and connect to my PostgreSQL database so that I can start monitoring my database immediately.

**Why this priority**: This is the fundamental capability - without being able to launch and connect, no other features are accessible. This establishes the core application lifecycle.

**Independent Test**: Can be fully tested by launching the application with valid connection credentials and verifying that a connection is established and displayed in the status bar.

**Acceptance Scenarios**:

1. **Given** I have PostgreSQL running locally on port 5432, **When** I launch Steep with default configuration, **Then** the application connects successfully and displays the database name in the status bar
2. **Given** I have a config file at ~/.config/steep/config.yaml with connection details, **When** I launch Steep, **Then** the application loads the configuration and connects to the specified database
3. **Given** invalid database credentials in my config, **When** I launch Steep, **Then** the application displays a clear error message explaining the connection failure and how to fix it
4. **Given** PostgreSQL is not running, **When** I launch Steep, **Then** the application displays an error message indicating the database is unavailable and suggests troubleshooting steps

---

### User Story 2 - Navigate Using Keyboard (Priority: P1)

As a DBA, I want to navigate the application using only my keyboard so that I can work efficiently without switching to a mouse.

**Why this priority**: Keyboard-driven navigation is a core principle defined in the constitution. Terminal users expect and require full keyboard control for productivity.

**Independent Test**: Can be fully tested by launching the application and using keyboard shortcuts (q for quit, h for help, Esc to close dialogs) without any mouse interaction.

**Acceptance Scenarios**:

1. **Given** the application is running, **When** I press 'q', **Then** the application exits gracefully
2. **Given** the application is running, **When** I press 'h' or '?', **Then** a help screen displays showing all available keyboard shortcuts
3. **Given** a help dialog is open, **When** I press 'Esc', **Then** the dialog closes and I return to the main view
4. **Given** the application is running, **When** I press Tab, **Then** I can cycle through available views (even if most views are placeholder in this foundation phase)

---

### User Story 3 - View Connection Status and Basic Metrics (Priority: P1)

As a DBA, I want to see real-time connection status and basic database information in a status bar so that I know the monitoring connection is active.

**Why this priority**: Provides immediate visual feedback that the monitoring tool is functioning correctly and connected. Essential for user confidence in the tool.

**Independent Test**: Can be fully tested by connecting to a database and verifying the status bar displays connection state, database name, and current timestamp with auto-refresh.

**Acceptance Scenarios**:

1. **Given** I am connected to a database, **When** viewing the main screen, **Then** the status bar displays "Connected", database name, and current time
2. **Given** the connection is lost, **When** the database becomes unavailable, **Then** the status bar updates to show "Disconnected" status with a visual indicator (red color)
3. **Given** I am connected, **When** time passes, **Then** the timestamp in the status bar updates automatically every second
4. **Given** I am on any view, **When** viewing the status bar, **Then** I see basic metrics like connection count (retrieved from pg_stat_activity)

---

### User Story 4 - Switch Between Views (Priority: P2)

As a DBA, I want to switch between different monitoring views using keyboard shortcuts so that I can navigate to the information I need quickly.

**Why this priority**: Establishes the view-switching framework that all future monitoring features will use. Lower priority than connection and navigation basics because it's primarily scaffolding for future features.

**Independent Test**: Can be fully tested by using number keys (1-9) or Tab to switch between available views (even if views are mostly placeholders at this stage).

**Acceptance Scenarios**:

1. **Given** the application is running, **When** I press '1', **Then** I switch to the Dashboard view (placeholder)
2. **Given** I am on view 1, **When** I press Tab, **Then** I switch to the next available view
3. **Given** I am on the last view, **When** I press Tab, **Then** I cycle back to the first view
4. **Given** I am on any view, **When** I press a view number key (2-9), **Then** I switch directly to that view if it exists

---

### Edge Cases

- What happens when the config file doesn't exist? System should create a default config with localhost connection details and display a message guiding the user to edit it.
- What happens when the terminal is resized? Application should handle terminal resize events and re-render the UI to fit the new dimensions gracefully (minimum size 80x24).
- What happens if the connection drops during monitoring? Application should detect the disconnection, display it in the status bar, and attempt automatic reconnection with exponential backoff (initial delay 1s, max delay 30s, max 5 attempts). After exhausting retry attempts, display error message with manual reconnect option.
- What happens when multiple instances connect to the same database? Each instance operates independently with its own connection pool.
- What happens when the user provides connection credentials via environment variables (PGHOST, PGPORT, etc.)? Environment variables should override config file values following standard PostgreSQL precedence.
- What happens when SSL/TLS is required by the database? Configuration should support SSL modes (disable, require, prefer) with reasonable defaults (prefer).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Application MUST initialize a terminal UI on launch
- **FR-002**: Application MUST load configuration from YAML file at ~/.config/steep/config.yaml (or path specified by STEEP_CONFIG environment variable)
- **FR-003**: Application MUST create a default configuration file if none exists, with commented examples for all connection parameters including password_command usage (no plaintext password field in config)
- **FR-004**: Application MUST establish a connection pool to PostgreSQL using provided credentials (host, port, database, user) with authentication via password_command (external password manager) or PGPASSWORD environment variable
- **FR-005**: Application MUST validate the database connection on startup by executing a simple query (e.g., SELECT version())
- **FR-006**: Application MUST display connection status (connected/disconnected) in the status bar with visual indicators
- **FR-007**: Application MUST support keyboard shortcuts for: quit (q), help (h/?), close dialog (Esc), switch views (Tab/Shift+Tab), jump to view (1-9)
- **FR-008**: Application MUST display a help screen showing all available keyboard shortcuts when user presses 'h' or '?'
- **FR-009**: Application MUST provide a reusable Table component for displaying tabular data with column headers
- **FR-010**: Application MUST provide a reusable StatusBar component showing connection state, database name, and timestamp
- **FR-011**: Application MUST provide a reusable HelpText component for displaying contextual help
- **FR-012**: Application MUST define consistent color scheme and spacing using a centralized styles module
- **FR-013**: Application MUST handle terminal resize events and re-render UI to fit new dimensions (minimum supported size: 80x24)
- **FR-014**: Application MUST gracefully handle connection failures with actionable error messages (e.g., "Connection refused: ensure PostgreSQL is running on localhost:5432")
- **FR-015**: Application MUST support connection configuration via environment variables (PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD) overriding config file
- **FR-016**: Application MUST implement view switching framework with ViewType enum and ViewModel interface
- **FR-017**: Application MUST implement at least a placeholder Dashboard view to demonstrate view switching
- **FR-018**: Application MUST support SSL/TLS connections with configurable sslmode (disable, prefer, require)
- **FR-019**: Application MUST implement graceful shutdown, closing database connections cleanly when user quits
- **FR-020**: Application MUST display a simple metric in the status bar (e.g., current connection count from pg_stat_activity) to validate query execution works
- **FR-021**: Application MUST implement automatic reconnection on connection loss with exponential backoff: initial delay 1 second, maximum delay 30 seconds, maximum 5 retry attempts before displaying permanent failure message
- **FR-022**: Application MUST output Error and Warning level messages to stderr for operational visibility
- **FR-023**: Application MUST suppress Info level logging by default (errors/warnings displayed in UI and stderr only)
- **FR-024**: Application MUST support optional debug flag (e.g., --debug or STEEP_DEBUG=1) to enable verbose logging including Info level messages
- **FR-025**: Application MUST support interactive password prompt on startup if neither password_command nor PGPASSWORD environment variable is available

### Key Entities

- **Connection Profile**: Represents database connection configuration including host, port, database, user, authentication method (password_command for external password manager or PGPASSWORD environment variable or interactive prompt), SSL mode, connection pool settings (max/min connections)
- **View**: Represents a distinct monitoring screen (Dashboard, Activity, Queries, etc.) with its own rendering logic and keyboard handlers
- **Application State**: Represents overall application state including current view, database connection, window dimensions, error state

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Application launches in under 1 second on typical hardware (measured from command execution to first UI render)
- **SC-002**: User can connect to a local PostgreSQL database and see connection confirmation within 2 seconds of providing credentials
- **SC-003**: All keyboard shortcuts (quit, help, navigation) respond within 100 milliseconds of keypress
- **SC-004**: Application renders correctly on terminals from 80x24 up to 200x100 without visual artifacts
- **SC-005**: Error messages for connection failures provide specific troubleshooting guidance (e.g., check if PostgreSQL is running, verify credentials)
- **SC-006**: Application consumes less than 10MB of memory when idle with active database connection
- **SC-007**: View switching between available views completes within 100 milliseconds
- **SC-008**: Configuration file can be created and edited by users without referring to documentation (due to inline comments and examples)
- **SC-009**: Application handles 100 rapid terminal resize events without crashing or visual corruption
- **SC-010**: Connection pool maintains stable connections for at least 1 hour of continuous monitoring without reconnection

## Assumptions

- Users have PostgreSQL 11 or higher available for testing (target version 18)
- Users are familiar with basic terminal usage and keyboard navigation concepts
- Configuration file format will be YAML due to readability and widespread use
- Default connection assumes localhost:5432 with database name "postgres" and username matching OS user
- Password authentication uses external password manager via password_command (e.g., "pass show postgres/local") or PGPASSWORD environment variable; no plaintext passwords stored in configuration files
- Terminal color support: assume 256-color terminals (xterm-256color) or better
- Connection pooling will use default settings as baseline (can be overridden in config)
- Status bar will refresh automatically every 1 second to update timestamp and metrics
- Help screen will be modal (overlay), not a separate view, to maintain context

## Dependencies

- External dependencies on UI framework, database driver, and configuration libraries (all available via package managers)
- Requires PostgreSQL database for testing and validation
- No dependencies on other Steep features (this is the foundation)

## Scope Boundaries

### In Scope
- Basic application initialization and lifecycle
- Database connection establishment and validation
- Configuration loading from YAML files and environment variables
- Keyboard-driven navigation framework
- Reusable UI components (Table, StatusBar, HelpText)
- View switching infrastructure
- Basic error handling and display
- Terminal resize handling
- Connection status display
- Simple metric retrieval (connection count)

### Out of Scope
- Actual monitoring views with real data (Dashboard, Activity, Queries, etc.) - these are future features
- Advanced query execution and result display - delegated to SQL Editor feature
- Multi-database connection management - single connection for MVP
- Configuration UI - users edit YAML directly in this phase
- Connection persistence/favorites - single profile for MVP
- Advanced error recovery (automatic reconnection will be basic exponential backoff)
- Performance benchmarking tools - will validate manually
- Logging to file - stderr output only for MVP (errors/warnings), file-based logging deferred to future features
- Advanced authentication methods (Kerberos, LDAP, certificate-based) - basic password authentication only in this phase
