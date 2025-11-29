# Feature Specification: Database Management Operations

**Feature Branch**: `010-database-operations`
**Created**: 2025-11-28
**Status**: Draft
**Input**: User description: "Implement Database Management Operations for executing maintenance tasks (VACUUM, ANALYZE, REINDEX) and managing users/roles."

## Clarifications

### Session 2025-11-28

- Q: What is the default threshold for stale vacuum status indicators? → A: 7 days (standard maintenance cycle awareness)
- Q: Can users cancel long-running maintenance operations? → A: Yes, allow cancellation with confirmation dialog (uses pg_cancel_backend)
- Q: Can users run multiple maintenance operations concurrently? → A: No, one operation at a time per session; block new until current completes
- Q: How do users access the Roles view? → A: Top-level view accessible via `0` key (the only remaining number key)
- Q: Should operation history be persisted? → A: Session-only history; cleared when application exits

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Execute VACUUM on Tables (Priority: P1)

As a DBA, I want to execute VACUUM on tables to reclaim storage space and remove dead tuples, ensuring the database maintains optimal storage efficiency.

**Why this priority**: VACUUM is the most fundamental maintenance operation. Dead tuples accumulate constantly during normal database operations, and without regular vacuuming, tables grow indefinitely, queries slow down, and transaction ID wraparound becomes a risk. This is the core maintenance capability DBAs need.

**Independent Test**: Can be fully tested by selecting a table with dead tuples, initiating VACUUM, and verifying that dead tuple count decreases and storage is reclaimed.

**Acceptance Scenarios**:

1. **Given** the user is in the Tables view with a table selected, **When** they press the operation key (`x`), **Then** an operations menu appears showing VACUUM options (VACUUM, VACUUM FULL, VACUUM ANALYZE)
2. **Given** the operations menu is displayed, **When** the user selects a VACUUM variant, **Then** a confirmation dialog appears showing the table name and operation details
3. **Given** the user confirms VACUUM execution, **When** the operation is running, **Then** a progress indicator shows completion percentage from pg_stat_progress_vacuum
4. **Given** VACUUM completes successfully, **When** the operation finishes, **Then** the completion status, duration, and dead tuples removed are displayed
5. **Given** the application is in read-only mode, **When** the user attempts to access VACUUM operations, **Then** the operations are disabled with a clear message explaining read-only mode restrictions

---

### User Story 2 - Execute ANALYZE on Tables (Priority: P1)

As a DBA, I want to execute ANALYZE on tables to update query planner statistics, ensuring the optimizer makes efficient execution plans.

**Why this priority**: ANALYZE is equally critical to VACUUM for database performance. Stale statistics cause the query planner to make poor decisions, leading to slow queries and full table scans. Every DBA needs this capability for routine maintenance.

**Independent Test**: Can be fully tested by selecting a table, initiating ANALYZE, and verifying that pg_stat_user_tables shows updated last_analyze timestamp.

**Acceptance Scenarios**:

1. **Given** the user is in the Tables view with a table selected, **When** they open the operations menu, **Then** ANALYZE appears as an available operation
2. **Given** ANALYZE is selected, **When** the user confirms execution, **Then** the operation runs and displays completion status with duration
3. **Given** ANALYZE completes, **When** viewing the table details, **Then** the last_analyze timestamp reflects the operation time

---

### User Story 3 - Execute REINDEX on Indexes (Priority: P2)

As a DBA, I want to execute REINDEX on indexes to rebuild corrupted or bloated indexes, restoring query performance.

**Why this priority**: Index maintenance is less frequent than VACUUM/ANALYZE but critical when index bloat or corruption occurs. This is a P2 because indexes can degrade silently, but the impact is usually less immediate than dead tuple accumulation.

**Independent Test**: Can be fully tested by selecting an index or table, initiating REINDEX, and verifying that index size changes and queries using the index perform correctly.

**Acceptance Scenarios**:

1. **Given** the user is viewing a table's indexes, **When** they select REINDEX from the operations menu, **Then** options appear for REINDEX INDEX (single) or REINDEX TABLE (all indexes)
2. **Given** REINDEX is selected, **When** the user confirms execution, **Then** a warning displays about table locks during the operation
3. **Given** REINDEX completes, **When** viewing index statistics, **Then** the index shows updated size and usage stats

---

### User Story 4 - View VACUUM and Autovacuum Status (Priority: P2)

As a DBA, I want to view VACUUM and autovacuum status for all tables to monitor maintenance activity and identify tables needing attention.

**Why this priority**: Visibility into vacuum status helps DBAs understand maintenance health without executing operations. This informs decisions about manual intervention and helps diagnose performance issues.

**Independent Test**: Can be fully tested by viewing the Tables view and verifying that vacuum status columns (last_vacuum, last_autovacuum) display accurate timestamps.

**Acceptance Scenarios**:

1. **Given** the user is in the Tables view, **When** viewing the table list, **Then** columns display last_vacuum and last_autovacuum timestamps for each table
2. **Given** a table hasn't been vacuumed recently, **When** the timestamp exceeds a threshold, **Then** visual indicators highlight tables needing attention (color-coded warning)
3. **Given** autovacuum is disabled on a table, **When** viewing that table's status, **Then** a clear indicator shows autovacuum is off

---

### User Story 5 - Manage Database Users and Roles (Priority: P3)

As a DBA, I want to view and manage database users and roles to control access and understand permission structures.

**Why this priority**: User management is important but less frequently needed than maintenance operations. Most environments have stable user configurations, making this a lower-priority convenience feature.

**Independent Test**: Can be fully tested by opening the Roles view and verifying that all database roles appear with their attributes, connection limits, and role memberships.

**Acceptance Scenarios**:

1. **Given** the user accesses the Roles view, **When** the view loads, **Then** all database roles display with: name, superuser status, login capability, connection limit, and role memberships
2. **Given** a role is selected, **When** viewing role details, **Then** permissions, owned objects, and granted privileges are displayed
3. **Given** the application is in read-only mode, **When** viewing roles, **Then** all role information is visible but modification operations are disabled

---

### User Story 6 - Grant/Revoke Permissions (Priority: P3)

As a DBA, I want to grant and revoke permissions on database objects to manage security and access control.

**Why this priority**: Permission management is the least frequent operation in this feature set. Security changes typically happen during setup or infrequent policy updates, making this suitable for P3.

**Independent Test**: Can be fully tested by selecting an object, granting/revoking a permission, and verifying the change via pg_catalog queries.

**Acceptance Scenarios**:

1. **Given** the user has a table selected, **When** they access the permissions option, **Then** current grants for that object display with grantee roles
2. **Given** the user selects GRANT, **When** they choose a role and permission type, **Then** a confirmation dialog shows the exact GRANT statement
3. **Given** GRANT is confirmed, **When** the operation completes, **Then** the permission list updates to reflect the change
4. **Given** the application is in read-only mode, **When** attempting GRANT/REVOKE, **Then** operations are disabled with explanation

---

### Edge Cases

- What happens when VACUUM is executed on a table with active transactions? The operation should proceed but may not be able to clean all dead tuples; a message should explain this limitation.
- How does the system handle VACUUM FULL blocking concurrent access? A warning should appear before execution explaining that VACUUM FULL takes an exclusive lock.
- What happens when the user lacks privileges for the requested operation? An actionable error message appears explaining required privileges.
- How does progress tracking handle operations with no progress data? For operations without pg_stat_progress support (ANALYZE, REINDEX), display "In Progress" without percentage.
- What happens if the database connection is lost during a long-running operation? The operation continues server-side; upon reconnection, the view should show current state.
- How are system catalogs and internal tables handled? Operations on system catalogs should be restricted with appropriate warnings.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide an operations menu accessible from the Tables view via the `x` key on any selected table
- **FR-002**: System MUST offer VACUUM variants: VACUUM, VACUUM FULL, and VACUUM ANALYZE in the operations menu
- **FR-003**: System MUST offer ANALYZE as a standalone operation in the operations menu
- **FR-004**: System MUST offer REINDEX options for individual indexes and all table indexes
- **FR-005**: System MUST display a confirmation dialog before executing any maintenance operation, showing the operation type and target object
- **FR-006**: System MUST track and display operation progress using pg_stat_progress_vacuum for VACUUM operations
- **FR-007**: System MUST display operation results including: completion status, duration, and rows/tuples affected
- **FR-008**: System MUST maintain session-only operation history; history is cleared when the application exits
- **FR-009**: System MUST allow cancellation of in-progress maintenance operations with a confirmation dialog, using pg_cancel_backend to terminate the server-side process
- **FR-010**: System MUST enforce single-operation execution; new operations are blocked while one is in progress, with clear feedback indicating the active operation
- **FR-011**: System MUST display last_vacuum and last_autovacuum timestamps in the Tables view for each table
- **FR-012**: System MUST provide visual indicators for tables with stale vacuum status (default threshold: 7 days, configurable)
- **FR-013**: System MUST provide a Roles view as a top-level view accessible via `0` key, showing all database roles with attributes (superuser, login, connection_limit, role memberships)
- **FR-014**: System MUST allow viewing permissions (grants) on database objects
- **FR-015**: System MUST support GRANT and REVOKE operations with confirmation dialogs
- **FR-016**: System MUST enforce read-only mode by disabling all destructive operations (VACUUM, ANALYZE, REINDEX, GRANT, REVOKE) when --readonly flag is set
- **FR-017**: System MUST display clear, actionable error messages for permission failures and operation errors
- **FR-018**: System MUST warn users about operations with significant impact (VACUUM FULL lock, REINDEX lock)

### Key Entities

- **Table**: Database table with maintenance status (last_vacuum, last_autovacuum, dead tuples, size)
- **Index**: Table index with bloat status and usage statistics
- **Role**: Database role with attributes (superuser, login, connection_limit, memberships)
- **Permission**: Grant relationship between roles and objects with privilege types
- **MaintenanceOperation**: Represents an in-progress or completed operation with type, target, status, progress, duration, and result
- **OperationProgress**: Real-time progress data from pg_stat_progress_vacuum (phase, heap_blks_total, heap_blks_scanned, etc.)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can complete any maintenance operation (VACUUM, ANALYZE, REINDEX) in 3 or fewer interactions from table selection
- **SC-002**: Operation progress updates display within 1 second of server-side progress changes
- **SC-003**: All maintenance operations display completion status and duration upon finishing
- **SC-004**: Tables view displays vacuum status (timestamps) for 100% of user tables in the current database
- **SC-005**: Read-only mode prevents 100% of destructive operations with clear feedback
- **SC-006**: Error messages include actionable guidance (e.g., "Insufficient privileges. Required: VACUUM permission on table X")
- **SC-007**: Roles view displays all roles with their complete attribute set within the configured refresh interval
- **SC-008**: Operations complete with keyboard-only navigation (no mouse required)
