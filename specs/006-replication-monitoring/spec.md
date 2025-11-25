# Feature Specification: Replication Monitoring & Setup

**Feature Branch**: `006-replication-monitoring`
**Created**: 2025-11-24
**Status**: Draft
**Input**: User description: "Implement Replication Monitoring and Setup view for tracking PostgreSQL streaming replication with rich visual feedback for both physical and logical replication, plus guided setup wizards for configuring and starting replication."

## Clarifications

### Session 2025-11-24

- Q: Should historical lag data retention be configurable or fixed at 24 hours? → A: Configurable retention period (default 24h, max 7 days)
- Q: How should replication user passwords be handled? → A: Default to auto-generate, but allow user-provided password with strength validation

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View Replication Lag (Priority: P1)

As a DBA, I want to see replication lag (bytes and time) to ensure replicas are synchronized.

**Why this priority**: Replication lag is the single most critical metric for database reliability. If replicas fall behind, failover becomes risky and read replicas serve stale data.

**Independent Test**: Can be fully tested by connecting to a PostgreSQL instance with replication configured and verifying that lag metrics display correctly. Delivers immediate visibility into replication health.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL primary with connected replicas, **When** I navigate to the replication view, **Then** I see a table showing each replica's byte lag and time lag
2. **Given** a replica that is lagging, **When** lag exceeds 10MB, **Then** the lag indicator displays in red with a warning
3. **Given** a replica that is healthy, **When** lag is below 1MB, **Then** the lag indicator displays in green

---

### User Story 2 - View Replication Slot Status (Priority: P1)

As a DBA, I want to see replication slot status to monitor replication health and prevent WAL buildup.

**Why this priority**: Inactive or abandoned replication slots can cause disk space exhaustion by retaining WAL indefinitely. This is a common cause of production incidents.

**Independent Test**: Can be fully tested by viewing slot information on any PostgreSQL instance with replication slots. Delivers visibility into WAL retention and slot health.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL instance with replication slots, **When** I view the slots panel, **Then** I see each slot's name, type (physical/logical), active status, and WAL retained
2. **Given** an inactive slot retaining significant WAL, **When** retention exceeds 80% of available space, **Then** the slot displays a warning indicator
3. **Given** a slot that is actively streaming, **When** I view its status, **Then** I see it marked as active with current lag

---

### User Story 3 - Visualize Replication Topology (Priority: P1)

As a DBA, I want to visualize the replication topology to understand primary/replica relationships at a glance.

**Why this priority**: Understanding the replication structure is essential for capacity planning, failover procedures, and debugging replication issues. A visual topology makes complex setups immediately comprehensible.

**Independent Test**: Can be tested by connecting to a primary server and viewing the topology diagram. Delivers immediate understanding of replication architecture.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL primary with multiple replicas, **When** I toggle the topology view, **Then** I see an ASCII tree diagram showing the primary at the top and replicas below
2. **Given** cascading replication (replica feeding another replica), **When** viewing topology, **Then** the cascade chain is visible in the tree structure
3. **Given** replicas with different sync states, **When** viewing topology, **Then** each node shows its sync state (sync, async, potential) with appropriate visual indicators

---

### User Story 4 - Check Replication Configuration Readiness (Priority: P1)

As a DBA, I want to check if my PostgreSQL is configured for replication to understand what changes are needed before adding replicas.

**Why this priority**: Setting up replication requires several configuration parameters to be correct. A readiness checker prevents failed setup attempts and guides users through prerequisites.

**Independent Test**: Can be tested on any PostgreSQL instance by opening the configuration checker panel. Delivers clear guidance on what's ready and what needs configuration.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL instance, **When** I open the configuration checker, **Then** I see validation results for: wal_level, max_wal_senders, max_replication_slots, wal_keep_size, hot_standby, archive_mode
2. **Given** a required parameter is misconfigured, **When** viewing the checker, **Then** I see a red indicator with guidance on the required value
3. **Given** all required parameters are configured, **When** viewing the checker, **Then** I see a green "READY" status with an overall summary

---

### User Story 5 - Physical Replication Setup Wizard (Priority: P1)

As a DBA, I want a guided wizard to set up physical streaming replication to quickly add replicas without manual configuration errors.

**Why this priority**: Setting up streaming replication involves multiple steps that are error-prone. A guided wizard reduces time-to-value and prevents common mistakes.

**Independent Test**: Can be tested by walking through the wizard on a replication-ready PostgreSQL instance. Delivers step-by-step guidance and generates required configuration.

**Acceptance Scenarios**:

1. **Given** I initiate the physical replication wizard, **When** I complete step 1, **Then** I can configure replication user, sync mode, and replica count
2. **Given** I complete wizard steps, **When** I reach the review panel, **Then** I see generated commands for pg_basebackup and recovery configuration
3. **Given** generated commands are displayed, **When** I select copy, **Then** commands are copied to my clipboard for execution on replica servers

---

### User Story 6 - View WAL Pipeline Stages (Priority: P2)

As a DBA, I want to see WAL pipeline stages (sent -> write -> flush -> replay) per replica to understand exactly where replication bottlenecks occur.

**Why this priority**: When lag occurs, knowing which stage is behind (network, disk write, disk flush, or replay) helps diagnose the root cause quickly.

**Independent Test**: Can be tested by selecting a replica and viewing its detailed pipeline. Delivers granular visibility into replication stages.

**Acceptance Scenarios**:

1. **Given** I select a replica, **When** I view details, **Then** I see a pipeline visualization showing Sent, Write, Flush, and Replay LSN positions
2. **Given** a stage is behind, **When** viewing the pipeline, **Then** the lagging stage shows visual differentiation and byte difference
3. **Given** all stages are caught up, **When** viewing the pipeline, **Then** all stages show as complete with minimal lag

---

### User Story 7 - View Lag History Trends (Priority: P2)

As a DBA, I want to visualize replication lag trends over time to identify patterns and plan capacity.

**Why this priority**: Point-in-time lag values don't tell the full story. Trends reveal whether lag is stable, growing, or correlated with specific events.

**Independent Test**: Can be tested by observing lag sparklines over time. Delivers trend visibility for each replica.

**Acceptance Scenarios**:

1. **Given** I'm viewing replicas, **When** I look at a replica row, **Then** I see a sparkline showing lag history
2. **Given** I want to see longer history, **When** I change the time window (1m, 5m, 15m, 1h), **Then** the sparkline updates to show the selected period
3. **Given** historical data exists, **When** I select extended time windows, **Then** I see data from persistent storage

---

### User Story 8 - Monitor Logical Replication (Priority: P2)

As a DBA, I want to monitor logical replication subscriptions and publications to understand selective replication health.

**Why this priority**: Logical replication is increasingly common for use cases like data distribution and live migrations. Monitoring it is essential for complex environments.

**Independent Test**: Can be tested on instances with logical replication configured. Delivers visibility into publication/subscription status.

**Acceptance Scenarios**:

1. **Given** publications exist, **When** I view the logical replication panel, **Then** I see each publication with table count and subscriber count
2. **Given** subscriptions exist, **When** I view the panel, **Then** I see subscription name, upstream connection, enabled status, and lag
3. **Given** no logical replication is configured, **When** I view the panel, **Then** I see an appropriate message indicating no logical replication

---

### User Story 9 - Logical Replication Setup Wizard (Priority: P2)

As a DBA, I want a guided wizard to set up logical replication (publications/subscriptions) to selectively replicate tables.

**Why this priority**: Logical replication setup is more complex than physical, requiring publication and subscription configuration across databases. A wizard reduces errors.

**Independent Test**: Can be tested by walking through the wizard. Delivers generated SQL for CREATE PUBLICATION and CREATE SUBSCRIPTION.

**Acceptance Scenarios**:

1. **Given** I start the logical replication wizard, **When** I reach table selection, **Then** I see tables with size and row counts, with warnings for large tables
2. **Given** I complete the wizard, **When** I reach the review panel, **Then** I see generated SQL for publication and subscription creation
3. **Given** tables over 1GB are selected, **When** viewing summary, **Then** I see a warning about initial sync duration

---

### User Story 10 - Generate Connection Strings (Priority: P2)

As a DBA, I want to generate connection strings (primary_conninfo) for replica configuration to avoid manual string construction errors.

**Why this priority**: Connection string syntax is error-prone. A builder with validation prevents configuration mistakes.

**Independent Test**: Can be tested by opening the connection builder and generating a string. Delivers validated, copy-ready connection strings.

**Acceptance Scenarios**:

1. **Given** I open the connection string builder, **When** I fill in host, port, user, and application name, **Then** I see a live preview of the generated string
2. **Given** I've built a connection string, **When** I select test connection, **Then** the system validates connectivity to the primary
3. **Given** a valid connection string, **When** I select copy, **Then** the string is copied to my clipboard

---

### User Story 11 - Create Replication Users (Priority: P2)

As a DBA, I want to create replication users with proper privileges to secure my replication setup.

**Why this priority**: Replication requires a user with REPLICATION privilege. Proper user creation with secure passwords is a common need.

**Independent Test**: Can be tested by creating a replication user through the interface. Delivers a user with correct privileges and secure password.

**Acceptance Scenarios**:

1. **Given** I initiate user creation, **When** I provide a username, **Then** the system generates a secure password
2. **Given** a password is generated, **When** viewing the result, **Then** the password is displayed once with a copy option, then masked
3. **Given** user creation completes, **When** checking the database, **Then** the user exists with REPLICATION and LOGIN privileges

---

### User Story 12 - Manage Replication Slots (Priority: P3)

As a DBA, I want to manage replication slots (advance, drop) to prevent WAL buildup from abandoned slots.

**Why this priority**: Slot management is an operational task needed less frequently but critical when disk space is at risk.

**Independent Test**: Can be tested by selecting a slot and performing management actions. Delivers slot cleanup capability.

**Acceptance Scenarios**:

1. **Given** I select an inactive slot, **When** I choose to drop it, **Then** I see a confirmation dialog warning about data loss implications
2. **Given** I confirm slot deletion, **When** the operation completes, **Then** the slot is removed and WAL is released
3. **Given** read-only mode is enabled, **When** I attempt slot management, **Then** the action is blocked with an appropriate message

---

### User Story 13 - Historical Lag Analysis (Priority: P3)

As a DBA, I want historical lag data for trend analysis and capacity planning over extended periods.

**Why this priority**: Long-term trends require persistent storage. This enables capacity planning and post-incident analysis.

**Independent Test**: Can be tested by querying historical data after running the application for an extended period. Delivers historical analysis capability.

**Acceptance Scenarios**:

1. **Given** the application has been running, **When** I select extended time windows, **Then** I see historical lag data from persistent storage
2. **Given** data older than 24 hours, **When** checking storage, **Then** older data is automatically cleaned up
3. **Given** I need trend analysis, **When** viewing historical data, **Then** I can identify patterns and correlations

---

### Edge Cases

- What happens when PostgreSQL is not configured for replication (wal_level=minimal)?
  - Display "Replication not configured" message with guidance
- What happens when connected user lacks permissions to view replication stats?
  - Display permission error with guidance on required privileges
- What happens when a replica disconnects during monitoring?
  - Show disconnected state immediately with last-known lag
- What happens when setup operations are attempted without superuser privileges?
  - Display clear error explaining required privileges
- What happens when read-only mode is enabled during setup operations?
  - Block all modification operations with appropriate messaging
- What happens when replication slots reference deleted replicas?
  - Highlight orphaned slots as potential cleanup candidates

## Requirements *(mandatory)*

### Functional Requirements

**Monitoring:**

- **FR-001**: System MUST display replication lag in both bytes and time for each replica
- **FR-002**: System MUST show replication slot status including name, type, active state, and WAL retained
- **FR-003**: System MUST visualize replication topology showing primary/replica relationships
- **FR-004**: System MUST support cascading replication visualization (replica-to-replica chains)
- **FR-005**: System MUST color-code lag severity: green (<1MB), yellow (1-10MB), red (>10MB)
- **FR-006**: System MUST display sync state indicators matching PostgreSQL states (sync, async, potential, quorum)
- **FR-007**: System MUST show WAL pipeline stages (sent, write, flush, replay) per replica
- **FR-008**: System MUST display lag history sparklines with configurable time windows (1m, 5m, 15m, 1h)
- **FR-009**: System MUST persist historical lag data for trend analysis with configurable retention period (default 24 hours, maximum 7 days)
- **FR-010**: System MUST show logical replication publications with table counts and subscriber counts
- **FR-011**: System MUST show logical replication subscriptions with upstream connection and lag
- **FR-012**: System MUST auto-detect primary vs replica role and display appropriate statistics
- **FR-013**: System MUST auto-refresh monitoring data at regular intervals (2 seconds default)

**Setup & Configuration:**

- **FR-014**: System MUST validate replication-readiness configuration parameters
- **FR-015**: System MUST provide a multi-step wizard for physical streaming replication setup
- **FR-016**: System MUST generate pg_basebackup commands with appropriate options
- **FR-017**: System MUST generate primary_conninfo connection strings for replica configuration
- **FR-018**: System MUST provide a multi-step wizard for logical replication setup
- **FR-019**: System MUST allow creation of replication users with secure password (auto-generated by default, or user-provided with strength validation)
- **FR-020**: System MUST generate pg_hba.conf entries for replication access
- **FR-021**: System MUST validate configuration with test connection capability
- **FR-022**: System MUST support copy-to-clipboard for all generated commands and strings
- **FR-023**: System MUST generate ALTER SYSTEM commands for replication parameters (wal_level, max_wal_senders, max_replication_slots) and indicate when restart is required

**Navigation & Interaction:**

- **FR-024**: System MUST support keyboard navigation for all operations (vim-style j/k navigation)
- **FR-025**: System MUST provide sort functionality for replica lists (by lag, name, state)
- **FR-026**: System MUST allow slot management (advance, drop) with confirmation dialogs
- **FR-027**: System MUST respect read-only mode by blocking all modification operations
- **FR-028**: System MUST provide help overlay with keybinding reference

**Graceful Degradation:**

- **FR-029**: System MUST display "Replication not configured" when no replication exists
- **FR-030**: System MUST handle permission errors with clear guidance
- **FR-031**: System MUST require superuser for setup operations with clear error messaging if unavailable

### Key Entities

- **Replica**: Represents a streaming replication standby server with lag metrics, sync state, and connection info
- **Replication Slot**: Represents a physical or logical slot with retention metrics and active status
- **Publication**: Represents a logical replication publication with associated tables and subscribers
- **Subscription**: Represents a logical replication subscription with upstream connection and sync state
- **Lag History Entry**: Time-series record of lag measurements for trend analysis

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can assess replication health within 5 seconds of opening the view
- **SC-002**: All replication lag data refreshes within 2-second intervals
- **SC-003**: Data retrieval completes within 500ms (query execution time)
- **SC-004**: Physical replication wizard generates complete, executable commands in under 2 minutes
- **SC-005**: Connection string builder validates connectivity within 3 seconds
- **SC-006**: 95% of users can identify a lagging replica within 10 seconds of viewing the interface
- **SC-007**: Configuration checker identifies all required settings within 1 second
- **SC-008**: Logical replication wizard generates correct SQL for publication and subscription creation
- **SC-009**: System supports monitoring of 100+ replicas without performance degradation
- **SC-010**: Historical lag data enables trend analysis over configurable periods up to 7 days

## Assumptions

- PostgreSQL 11+ is the minimum supported version; PostgreSQL 15+ required for pg_hba_file_rules access
- Users have appropriate database permissions to view replication statistics
- Setup operations require superuser or equivalent privileges
- Network connectivity exists between monitoring client and PostgreSQL servers
- Physical replication setup generates commands for execution on replica servers (not executed directly by the tool)
- Logical replication requires wal_level=logical on the primary

## Scope Boundaries

**In Scope:**
- Physical streaming replication monitoring
- Logical replication monitoring (publications/subscriptions)
- Cascading replication topology visualization
- Historical lag persistence and trend analysis
- Setup wizards for both physical and logical replication
- Connection string and command generation
- Replication slot management
- Configuration changes for replication parameters (wal_level, max_wal_senders, max_replication_slots, etc.) via ALTER SYSTEM with restart guidance

**Out of Scope:**
- Automatic failover orchestration
- Remote server restart (tool advises user when restart is needed, but does not execute it)
- BDR (Bi-Directional Replication) or multi-master replication (future consideration noted in data model)
- Remote execution of pg_basebackup on replica servers
- Continuous archiving configuration (archive_command setup)
- pgBouncer or connection pooler configuration
