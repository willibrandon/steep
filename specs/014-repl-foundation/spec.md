# Feature Specification: Bidirectional Replication Foundation

**Feature Branch**: `014-repl-foundation`
**Created**: 2025-12-04
**Status**: Draft
**Input**: Build foundation infrastructure for Steep bidirectional replication. Create PostgreSQL extension (steep_repl) using Rust/pgrx with schema tables for nodes, coordinator_state, and audit_log. Implement Go daemon (steep-repl) using kardianos/service for cross-platform service management. Windows is primary target.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Install PostgreSQL Extension (Priority: P1)

As a DBA, I want to install the steep_repl extension on my PostgreSQL 18 database so that the replication system has a place to store its coordination data, node registry, and audit trail.

**Why this priority**: The extension is the foundational data layer that all other replication components depend on. Without the schema tables, no coordination can occur.

**Independent Test**: Can be fully tested by running `CREATE EXTENSION steep_repl;` on PostgreSQL 18 and verifying all tables and indexes are created correctly.

**Acceptance Scenarios**:

1. **Given** PostgreSQL 18 is running, **When** I run `CREATE EXTENSION steep_repl;`, **Then** the steep_repl schema is created with nodes, coordinator_state, and audit_log tables.

2. **Given** PostgreSQL 17 or earlier, **When** I attempt to install the extension, **Then** I receive a clear error message stating PostgreSQL 18 is required.

3. **Given** the extension is installed, **When** I query `\dt steep_repl.*`, **Then** I see nodes, coordinator_state, and audit_log tables with appropriate columns and indexes.

4. **Given** the extension is installed, **When** a DBA action occurs (node registration, state update), **Then** an entry is recorded in steep_repl.audit_log with actor, action, timestamp, and outcome.

---

### User Story 2 - Install and Run Daemon as System Service (Priority: P1)

As a DBA, I want to install the steep-repl daemon as a system service that starts automatically and survives reboots, so that replication coordination is always available.

**Why this priority**: The daemon is the brain of the replication system. Without it running reliably, no coordination, health monitoring, or inter-node communication can occur.

**Independent Test**: Can be fully tested by running `steep-repl install`, `steep-repl start`, rebooting, and verifying the service auto-starts.

**Acceptance Scenarios**:

1. **Given** steep-repl binary is available, **When** I run `steep-repl install` on Windows, **Then** the service is registered with Windows Service Control Manager.

2. **Given** steep-repl binary is available, **When** I run `steep-repl install` on Linux, **Then** a systemd unit file is created and enabled.

3. **Given** steep-repl binary is available, **When** I run `steep-repl install` on macOS, **Then** a launchd plist is created and loaded.

4. **Given** the service is installed, **When** I run `steep-repl start`, **Then** the daemon begins running and logs startup.

5. **Given** the service is running, **When** I run `steep-repl stop`, **Then** the daemon shuts down gracefully.

6. **Given** the service is running, **When** I run `steep-repl status`, **Then** I see the daemon's running state and uptime.

7. **Given** the service is installed and running, **When** I reboot the machine, **Then** the daemon auto-starts after reboot.

8. **Given** the service is running, **When** I run `steep-repl uninstall`, **Then** the service is stopped and removed from the service manager.

---

### User Story 3 - Daemon Connects to PostgreSQL (Priority: P1)

As a DBA, I want steep-repl to connect to my PostgreSQL database using connection pooling, so that the daemon can read and write coordination data efficiently.

**Why this priority**: Database connectivity is essential for the daemon to function. Without it, the daemon cannot register nodes, track state, or log audit events.

**Independent Test**: Can be fully tested by configuring connection settings and running `steep-repl status` to verify database connectivity.

**Acceptance Scenarios**:

1. **Given** valid PostgreSQL credentials in config, **When** the daemon starts, **Then** it establishes a connection pool to PostgreSQL.

2. **Given** the daemon is connected, **When** I run `steep-repl status`, **Then** the output shows "PostgreSQL: connected" with version information.

3. **Given** invalid PostgreSQL credentials, **When** the daemon starts, **Then** it logs a connection error and retries with exponential backoff.

4. **Given** PostgreSQL version is below 18, **When** the daemon connects, **Then** it logs a clear error message and refuses to proceed with replication features.

5. **Given** the daemon is running and PostgreSQL restarts, **When** PostgreSQL becomes available again, **Then** the daemon automatically reconnects.

---

### User Story 4 - TUI Communicates with Daemon via IPC (Priority: P2)

As a DBA, I want the Steep TUI to communicate with the steep-repl daemon via local IPC (named pipes on Windows, Unix sockets on Linux/macOS), so that I can view replication status and manage the system from the TUI.

**Why this priority**: This enables the user interface to display real-time replication information and send commands. It's P2 because the daemon can run headless without the TUI.

**Independent Test**: Can be tested by starting the daemon, launching Steep TUI, and verifying the TUI shows "steep-repl: connected" in the status bar.

**Acceptance Scenarios**:

1. **Given** the daemon is running on Windows, **When** the TUI starts, **Then** it connects via named pipe `\\.\pipe\steep-repl`.

2. **Given** the daemon is running on Linux/macOS, **When** the TUI starts, **Then** it connects via Unix socket at `/tmp/steep-repl.sock`.

3. **Given** the TUI is connected, **When** I navigate to the replication view, **Then** I see status information fetched from the daemon.

4. **Given** the daemon is not running, **When** the TUI attempts to connect, **Then** it shows "steep-repl: disconnected" and gracefully degrades.

5. **Given** the daemon stops while TUI is connected, **When** the connection is lost, **Then** the TUI detects disconnection and attempts to reconnect periodically.

---

### User Story 5 - Node-to-Node Communication via gRPC (Priority: P2)

As a DBA, I want steep-repl daemons on different nodes to communicate via gRPC, so that they can coordinate replication activities across the cluster.

**Why this priority**: Inter-node communication is required for multi-node coordination. It's P2 because single-node setup works without it.

**Independent Test**: Can be tested by configuring two nodes and verifying they can ping each other via `steep-repl health --remote node-b:5433`.

**Acceptance Scenarios**:

1. **Given** the daemon is configured with gRPC port 5433, **When** it starts, **Then** it listens for gRPC connections on that port.

2. **Given** two daemons on separate nodes, **When** node A's daemon sends a health check to node B, **Then** node B responds with its health status.

3. **Given** mTLS certificates are configured, **When** nodes communicate, **Then** all gRPC traffic is encrypted and both nodes verify each other's certificates.

4. **Given** a node becomes unreachable, **When** another node tries to communicate, **Then** the failure is logged and the node is marked as unavailable.

---

### User Story 6 - HTTP Health Endpoint (Priority: P3)

As a DBA, I want an HTTP health endpoint on the steep-repl daemon, so that load balancers and monitoring systems can check the daemon's health status.

**Why this priority**: Health endpoints are useful for infrastructure integration but not required for basic operation.

**Independent Test**: Can be tested by running `curl http://localhost:8080/health` and verifying JSON response.

**Acceptance Scenarios**:

1. **Given** the daemon is running with HTTP enabled, **When** I request `/health`, **Then** I receive a JSON response with status and component health.

2. **Given** the daemon can connect to PostgreSQL, **When** I request `/health`, **Then** the PostgreSQL health check returns "healthy".

3. **Given** the daemon cannot connect to PostgreSQL, **When** I request `/health`, **Then** the response indicates "unhealthy" with reason.

4. **Given** the daemon is configured without HTTP, **When** I request the health endpoint, **Then** no HTTP listener is started.

---

### Edge Cases

- What happens when the PostgreSQL extension is installed on an unsupported version? Clear error with required version stated.
- What happens when the config file is missing or malformed? Daemon refuses to start with actionable error message.
- What happens when multiple daemons try to run on the same machine? Second instance detects conflict via lock file and exits.
- What happens when IPC socket/pipe already exists from crashed process? Daemon removes stale socket and creates new one.
- What happens when gRPC port is already in use? Daemon logs clear error and exits.
- What happens when audit_log table grows very large? Configurable retention with automatic pruning.

## Requirements *(mandatory)*

### Functional Requirements

**PostgreSQL Extension (steep_repl)**:

- **FR-001**: Extension MUST create `steep_repl` schema on installation.
- **FR-002**: Extension MUST create `steep_repl.nodes` table with columns: node_id (PK), node_name, host, port (default 5432), priority (default 50), is_coordinator (default false), last_seen, status (default 'unknown').
- **FR-003**: Extension MUST create `steep_repl.coordinator_state` table with columns: key (PK), value (JSONB), updated_at (default now()).
- **FR-004**: Extension MUST create `steep_repl.audit_log` table with columns: id (serial PK), occurred_at (default now()), action, actor, target_type, target_id, old_value (JSONB), new_value (JSONB), client_ip (INET), success (default true), error_message.
- **FR-005**: Extension MUST create indexes on audit_log for occurred_at, actor, and action columns.
- **FR-006**: Extension MUST validate PostgreSQL version is 18+ during installation; fail with clear error if not.
- **FR-007**: Extension MUST be installable via standard `CREATE EXTENSION steep_repl;` command.

**Daemon Service Management**:

- **FR-008**: Daemon MUST support `install`, `uninstall`, `start`, `stop`, `restart`, `status`, `run` commands.
- **FR-009**: Daemon MUST integrate with Windows Service Control Manager on Windows.
- **FR-010**: Daemon MUST integrate with systemd on Linux.
- **FR-011**: Daemon MUST integrate with launchd on macOS.
- **FR-012**: Daemon MUST use platform-appropriate config paths: `%APPDATA%\steep` (Windows), `~/.config/steep` (Linux), `~/Library/Application Support/steep` (macOS).
- **FR-013**: Daemon MUST auto-start after installation when system boots.
- **FR-014**: Daemon MUST shut down gracefully on stop signal, closing all connections.
- **FR-014a**: Daemon MUST write operational logs to the platform's native system log (Windows Event Log, syslog on Linux, os_log on macOS).

**PostgreSQL Connectivity**:

- **FR-015**: Daemon MUST connect to PostgreSQL using pgx connection pooling.
- **FR-016**: Daemon MUST read connection settings from config file.
- **FR-017**: Daemon MUST support environment variables for credentials (PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD).
- **FR-018**: Daemon MUST support password_command for external credential managers.
- **FR-019**: Daemon MUST implement connection retry with exponential backoff on failure.
- **FR-020**: Daemon MUST validate PostgreSQL version is 18+ on connection.

**IPC Communication**:

- **FR-021**: Daemon MUST listen on named pipe `\\.\pipe\steep-repl` on Windows.
- **FR-022**: Daemon MUST listen on Unix socket `/tmp/steep-repl.sock` on Linux/macOS.
- **FR-023**: Daemon MUST clean up stale IPC endpoints on startup.
- **FR-024**: Daemon MUST expose status, health, and basic control operations via IPC.

**gRPC Communication**:

- **FR-025**: Daemon MUST expose gRPC server on configurable port (default 5433).
- **FR-026**: Daemon MUST support mutual TLS (mTLS) for gRPC connections, requiring valid node certificates for authentication.
- **FR-027**: Daemon MUST implement health check RPC for node-to-node verification.
- **FR-028**: Daemon MUST log failed connection attempts from other nodes.

**HTTP Health Endpoint**:

- **FR-029**: Daemon MUST optionally expose HTTP health endpoint on configurable port.
- **FR-030**: Health endpoint MUST return JSON with overall status and per-component health.
- **FR-031**: Health endpoint MUST include PostgreSQL connectivity status.

**Audit Logging**:

- **FR-032**: Daemon MUST log significant events to steep_repl.audit_log (node registration, config changes, errors).
- **FR-033**: Audit entries MUST include actor (role@host), action, timestamp, and success/failure.

### Key Entities

- **Node**: A PostgreSQL database instance participating in bidirectional replication. Has identity (node_id), location (host:port), priority for coordinator election, and health status.
- **Coordinator State**: Key-value store for cluster-wide coordination data. Used by the elected coordinator to track global state.
- **Audit Log Entry**: Immutable record of system activity for compliance and debugging. Captures who did what, when, to what, with what outcome.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can install the extension on PostgreSQL 18 in under 1 minute.
- **SC-002**: DBAs can install and start the daemon as a service in under 2 minutes per platform.
- **SC-003**: Daemon successfully connects to PostgreSQL within 5 seconds of startup under normal conditions.
- **SC-004**: Daemon survives system reboot and auto-restarts within 30 seconds.
- **SC-005**: TUI connects to daemon via IPC within 1 second when both are running.
- **SC-006**: Two daemons can establish gRPC communication within 5 seconds.
- **SC-007**: HTTP health endpoint responds within 100 milliseconds.
- **SC-008**: All schema tables created with proper indexes as verified by system catalog queries.
- **SC-009**: Audit log captures 100% of specified events with required fields populated.
- **SC-010**: Daemon handles PostgreSQL connection loss and recovers within 30 seconds of database availability.

## Clarifications

### Session 2025-12-04

- Q: How should nodes authenticate each other for gRPC communication? → A: Mutual TLS (mTLS) with node certificates
- Q: Where should the daemon write operational logs? → A: Platform system log (Windows Event Log, syslog, os_log)

## Assumptions

- PostgreSQL 18 is installed and accessible on target systems.
- Rust toolchain with pgrx is available for building the extension.
- Go 1.21+ is available for building the daemon.
- Windows users have Administrator privileges for service installation.
- Linux users have sudo access for systemd unit installation.
- Firewall rules allow gRPC port (5433) for inter-node communication.
- TLS certificates are managed externally (self-signed or CA-issued).
