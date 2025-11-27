# Feature Specification: Configuration Viewer

**Feature Branch**: `008-configuration-viewer`
**Created**: 2025-11-27
**Status**: Draft
**Input**: User description: "Implement Configuration Viewer for browsing PostgreSQL server settings from pg_settings. Display parameters in sortable table with name, current value, unit, category, and description. Support search by parameter name and filter by category. Highlight modified parameters that differ from defaults. Show detailed parameter information including context, constraints (min/max), and default values. Provide read-only view to prevent accidental changes. Prioritize P1 story (viewing parameters) and P2 (search/filter) and P3 (context help). Auto-refresh every 60 seconds."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View All Server Configuration Parameters (Priority: P1)

As a DBA, I want to view all PostgreSQL server configuration parameters in a sortable table so that I can understand the current server settings at a glance.

**Why this priority**: This is the core functionality of the Configuration Viewer. Without the ability to browse parameters, none of the other features (search, filter, details) would have value. DBAs need visibility into server configuration as a fundamental monitoring capability.

**Independent Test**: Can be fully tested by opening the Configuration view and verifying that all PostgreSQL parameters are displayed in a readable table format. Delivers immediate value by providing a single location to review all server settings.

**Acceptance Scenarios**:

1. **Given** I am connected to a PostgreSQL server, **When** I press the `8` key, **Then** I see the Configuration Viewer with a table of all server parameters
2. **Given** I am viewing the Configuration Viewer, **When** I scroll through the table, **Then** I can see all parameters with their Name, Current Value, Unit, Category, and Short Description
3. **Given** I am viewing the Configuration Viewer, **When** I press a sort key, **Then** the table is sorted by the selected column (name or category)
4. **Given** I am viewing the Configuration Viewer, **When** 60 seconds pass, **Then** the parameter values are automatically refreshed from the server
5. **Given** I am viewing the Configuration Viewer, **When** a parameter has been modified from its default, **Then** that row is highlighted in yellow to indicate customization

---

### User Story 2 - Search and Filter Configuration Parameters (Priority: P2)

As a DBA, I want to search configuration parameters by name and filter by category so that I can quickly find specific settings without scrolling through hundreds of parameters.

**Why this priority**: PostgreSQL has over 300 configuration parameters. Search and filter capabilities transform the viewer from a browsing tool into a productivity tool. This depends on P1 (must have parameters displayed first) but significantly enhances the user experience.

**Independent Test**: Can be tested by searching for a known parameter (e.g., "shared_buffers") and verifying it appears in filtered results. Also test category filtering by selecting a category (e.g., "Memory") and verifying only memory-related parameters are shown.

**Acceptance Scenarios**:

1. **Given** I am viewing the Configuration Viewer, **When** I press `/` and type a search term, **Then** the table filters to show only parameters whose names or descriptions contain the search term
2. **Given** I am searching with a term that matches multiple parameters, **When** I view the results, **Then** all matching parameters are displayed
3. **Given** I am viewing the Configuration Viewer, **When** I select a category filter, **Then** only parameters in that category are displayed
4. **Given** I have an active search filter, **When** I clear the search, **Then** all parameters are displayed again
5. **Given** I am searching for a parameter, **When** no parameters match my search term, **Then** I see a message indicating no results found

---

### User Story 3 - View Parameter Details and Context Help (Priority: P3)

As a DBA, I want to see detailed information about a parameter including its context, constraints, and default value so that I can understand when and how the parameter can be changed.

**Why this priority**: While viewing parameters is essential, understanding the full context (whether a restart is required, valid value ranges, etc.) is advanced usage. This enhances the P1 capability but requires P1 to be complete first.

**Independent Test**: Can be tested by selecting a parameter (e.g., "max_connections") and pressing `d` to view details. Verify that the detail view shows: full description, context (postmaster/sighup/user), min/max constraints, default value, and current source.

**Acceptance Scenarios**:

1. **Given** I am viewing the Configuration Viewer with a parameter selected, **When** I press `d`, **Then** I see a detailed view of that parameter
2. **Given** I am viewing parameter details, **When** I view a numeric parameter, **Then** I see its minimum and maximum allowed values
3. **Given** I am viewing parameter details, **When** I look at the context field, **Then** I understand when the change takes effect (postmaster = restart required, sighup = reload required, user = session-level)
4. **Given** I am viewing parameter details, **When** I press `Escape` or `q`, **Then** I return to the main parameter list
5. **Given** I am viewing parameter details for a modified parameter, **When** I compare the current value to the default, **Then** I can see both values and understand the customization

---

### User Story 4 - Export Configuration (Priority: P3)

As a DBA, I want to export the current server configuration to a file so that I can document settings, compare configurations across servers, or keep records for compliance purposes.

**Why this priority**: Export is a convenience feature that enhances documentation workflows but is not essential for monitoring. It depends on P1 (parameters must be viewable before they can be exported).

**Independent Test**: Can be tested by running the export command and verifying a file is created with all configuration parameters in a readable format.

**Acceptance Scenarios**:

1. **Given** I am viewing the Configuration Viewer, **When** I type `:export config <filename>`, **Then** the current configuration is exported to the specified file
2. **Given** I am exporting configuration, **When** the export completes successfully, **Then** I see a confirmation message with the file path
3. **Given** I have filtered the configuration view, **When** I export, **Then** only the filtered parameters are exported
4. **Given** I specify a filename without a path, **When** I export, **Then** the file is created in the current working directory

---

### Edge Cases

- What happens when the database connection is lost during auto-refresh?
  - Display an error message and attempt to reconnect; preserve the last known configuration display
- What happens when pg_settings returns no results?
  - Display a message indicating the configuration could not be loaded; this should not occur under normal circumstances but may indicate permission issues
- How does the system handle parameters with very long values or descriptions?
  - Truncate display in the table view; show full values in the detail view
- What happens when the user searches for a term with special regex characters?
  - Treat search as literal string match, not regex pattern
- How does the system handle a parameter with NULL values for unit or min/max?
  - Display empty or "N/A" in those fields; this is normal for many parameters

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST display all PostgreSQL configuration parameters from pg_settings in a sortable table
- **FR-002**: System MUST show the following columns for each parameter: Name, Current Value, Unit, Category, Short Description
- **FR-003**: System MUST allow users to sort the parameter table by name or category
- **FR-004**: System MUST highlight parameters where the current setting differs from the boot_val (default) in yellow
- **FR-005**: System MUST allow users to search parameters by name or description using the `/` key
- **FR-006**: System MUST allow users to filter parameters by category
- **FR-007**: System MUST display a detailed parameter view when `d` is pressed, showing: full description, context, min/max constraints, default value, and source
- **FR-008**: System MUST auto-refresh configuration data every 60 seconds
- **FR-009**: System MUST provide read-only access to configuration (no editing capability)
- **FR-010**: System MUST allow export of configuration to a file via `:export config <filename>` command
- **FR-011**: System MUST be accessible via the `8` key from the main navigation
- **FR-012**: System MUST display the parameter context (postmaster, sighup, superuser, user, internal) to indicate when changes take effect
- **FR-013**: System MUST handle connection errors gracefully during refresh, displaying an error message without crashing

### Key Entities

- **Parameter**: A PostgreSQL configuration setting with attributes: name, setting (current value), unit, category, short_desc, extra_desc, context, vartype, source, min_val, max_val, boot_val, reset_val, pending_restart
- **Category**: A grouping of related parameters (e.g., "Resource Usage / Memory", "Connections and Authentication", "Query Tuning")
- **Context**: Indicates when a parameter change takes effect: internal (read-only), postmaster (server restart), sighup (configuration reload), superuser (superuser session), user (any session)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can view all server configuration parameters within 2 seconds of opening the Configuration Viewer
- **SC-002**: Users can find a specific parameter by name in under 5 seconds using search
- **SC-003**: Users can identify all modified parameters (differing from defaults) at a glance via yellow highlighting
- **SC-004**: Parameter details provide sufficient information for users to understand change requirements without consulting external documentation
- **SC-005**: Configuration data remains current with automatic refresh every 60 seconds
- **SC-006**: System maintains read-only access, preventing any accidental configuration changes
- **SC-007**: Export functionality creates a complete, accurate record of current configuration settings

## Assumptions

- The connected PostgreSQL user has permission to query pg_settings (typically available to all users)
- The terminal supports color display for yellow highlighting of modified parameters
- PostgreSQL version 11 or higher is in use (pg_settings structure has been stable since earlier versions)
- Parameters are primarily ASCII/UTF-8 text and do not require special encoding handling
- The export format is plain text (one parameter per line with key-value pairs); future versions may add JSON/YAML formats
