# Feature Specification: Extension-Native Architecture

**Feature Branch**: `016-extension-native`
**Created**: 2025-12-08
**Status**: Draft
**Input**: User description: "Migrate steep-repl daemon services into the PostgreSQL extension to eliminate the need for a separate daemon process"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Direct CLI to PostgreSQL Operations (Priority: P1)

As a DBA, I want to run steep-repl CLI commands that connect directly to PostgreSQL without requiring a separate daemon process, so that steep_repl functionality is always available when PostgreSQL is running.

**Why this priority**: This is the core architectural change - eliminating the daemon dependency. Without this, users face the operational complexity of managing two processes and the failure mode where PostgreSQL is healthy but steep-repl features are unavailable.

**Independent Test**: Can be fully tested by running `steep-repl snapshot generate --direct` against a PostgreSQL instance with the steep_repl extension loaded, verifying the snapshot completes without any daemon running.

**Acceptance Scenarios**:

1. **Given** PostgreSQL is running with steep_repl extension loaded, **When** I run `steep-repl snapshot generate --direct -c "postgresql://..."`, **Then** the snapshot generates successfully without any daemon process running.
2. **Given** PostgreSQL is running with steep_repl extension loaded, **When** I run `steep-repl schema compare --direct node-a node-b`, **Then** the schema comparison executes successfully via direct PostgreSQL connection.
3. **Given** PostgreSQL is running with steep_repl extension loaded, **When** the steep-repl daemon is not running, **Then** all CLI operations using `--direct` flag work normally.

---

### User Story 2 - Background Worker for Long Operations (Priority: P1)

As a DBA, I want snapshot generation and apply operations to run as PostgreSQL background workers, so that long-running operations don't block my session and progress is visible across all connections.

**Why this priority**: Snapshot operations on large databases can take hours. Without background workers, the calling session would be blocked and disconnection would abort the operation.

**Independent Test**: Can be tested by starting a snapshot via SQL function call, disconnecting the session, reconnecting, and verifying the operation continues and progress is queryable.

**Acceptance Scenarios**:

1. **Given** a database with tables to export, **When** I call `steep_repl.start_snapshot('/path', 'gzip', 4)`, **Then** the function returns immediately with a snapshot ID and the operation proceeds in the background.
2. **Given** a snapshot operation is in progress, **When** I query `steep_repl.snapshot_progress()` from any session, **Then** I see current progress including phase, percentage, and current table.
3. **Given** a snapshot operation is in progress, **When** I disconnect my session and reconnect, **Then** the snapshot operation continues and I can still query progress.
4. **Given** a snapshot operation is in progress, **When** I call `steep_repl.cancel_snapshot(snapshot_id)`, **Then** the background worker terminates the operation gracefully.

---

### User Story 3 - Real-Time Progress via LISTEN/NOTIFY (Priority: P2)

As a DBA running operations from the CLI, I want to see real-time progress updates using PostgreSQL's native LISTEN/NOTIFY mechanism, so that I get immediate feedback without polling.

**Why this priority**: Real-time progress improves user experience but operations can complete without it. The background worker and queryable progress (US2) provide the minimum viable experience.

**Independent Test**: Can be tested by running `LISTEN steep_repl_progress` in one session, starting a snapshot in another, and verifying notifications arrive with progress updates.

**Acceptance Scenarios**:

1. **Given** I am listening on `steep_repl_progress` channel, **When** a snapshot operation makes progress, **Then** I receive NOTIFY messages with JSON payloads containing operation ID, phase, percentage, and current table.
2. **Given** the CLI is running with `--direct` mode, **When** I start a snapshot, **Then** the CLI automatically subscribes to LISTEN/NOTIFY and displays a live progress bar.
3. **Given** multiple operations are running, **When** I listen for progress, **Then** each notification includes the operation ID so I can filter to my operation.

---

### User Story 4 - SQL Function API for All Operations (Priority: P2)

As a DBA or application developer, I want all steep_repl operations exposed as SQL functions, so that I can invoke them from any SQL client (psql, pgAdmin, application code) without needing the CLI.

**Why this priority**: The SQL API enables integration with existing tools and workflows. The CLI (US1) provides a good UX, but the SQL API enables automation and programmatic access.

**Independent Test**: Can be tested by executing all steep_repl functions from psql without the CLI, verifying each operation works correctly.

**Acceptance Scenarios**:

1. **Given** I am connected via psql, **When** I call `SELECT * FROM steep_repl.start_snapshot('/snapshots/db1', 'zstd')`, **Then** a snapshot operation begins and returns the snapshot record.
2. **Given** I am connected via psql, **When** I call `SELECT * FROM steep_repl.analyze_overlap('host=peer dbname=db', ARRAY['public.users'])`, **Then** I receive a result set showing local-only, remote-only, matches, and conflicts counts.
3. **Given** I am connected via psql, **When** I call `SELECT * FROM steep_repl.start_merge('host=peer dbname=db', ARRAY['public.users'], 'prefer-local')`, **Then** a merge operation begins and returns the merge audit log record.

---

### User Story 5 - Simplified Configuration (Priority: P3)

As a DBA setting up steep, I want configuration to be simpler with only PostgreSQL connection details required, so that I don't need to manage separate daemon configuration files, ports, and TLS certificates.

**Why this priority**: Simplified configuration improves setup experience but users can work with complex configuration. This is a nice-to-have improvement.

**Independent Test**: Can be tested by setting up steep on a new server with only a PostgreSQL connection string and verifying all features work.

**Acceptance Scenarios**:

1. **Given** I have a PostgreSQL connection string, **When** I run steep-repl commands with `--direct`, **Then** no additional configuration files are required.
2. **Given** PostgreSQL is configured with SSL, **When** I use `sslmode=require` in my connection string, **Then** all steep-repl communications use the encrypted connection.
3. **Given** I previously used daemon mode configuration, **When** I switch to direct mode, **Then** I can remove the gRPC, IPC, and HTTP health configuration sections.

---

### User Story 6 - Backward Compatibility During Migration (Priority: P3)

As a DBA with existing steep-repl deployments, I want both daemon mode and direct mode to work during the migration period, so that I can transition gradually without disruption.

**Why this priority**: Backward compatibility prevents breaking existing deployments but is only needed during the transition period. New deployments can use direct mode immediately.

**Independent Test**: Can be tested by running the same operation twice - once with `--remote` (daemon mode) and once with `--direct` (extension mode) - and verifying identical results.

**Acceptance Scenarios**:

1. **Given** I have an existing daemon running, **When** I run `steep-repl snapshot generate --remote localhost:15460`, **Then** the operation works as before via gRPC.
2. **Given** no daemon is running, **When** I run `steep-repl snapshot generate --direct`, **Then** the operation works via direct PostgreSQL connection.
3. **Given** both modes are available, **When** I don't specify `--remote` or `--direct`, **Then** the CLI auto-detects and uses direct mode if the extension supports it.

---

### Edge Cases

- What happens when a background worker operation fails mid-execution?
  - The work_queue entry is marked 'failed' with error_message, and the operation can be retried.
- How does the system handle concurrent snapshot requests?
  - Multiple snapshots can be queued; the background worker processes them sequentially (one at a time per worker).
- What happens if PostgreSQL restarts during a long operation?
  - In-progress operations are marked as failed; the work_queue allows detection and manual restart if needed.
- How does the system handle superuser requirement validation?
  - All functions that require superuser check `pg_is_superuser()` and raise an exception if the caller lacks privileges.
- What happens if `shared_preload_libraries` doesn't include steep_repl?
  - Background worker features are unavailable; functions that require it raise a clear error message.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow CLI commands to connect directly to PostgreSQL without a daemon using `--direct` flag
- **FR-002**: System MUST execute snapshot generation as a PostgreSQL background worker that doesn't block the calling session
- **FR-003**: System MUST execute snapshot apply as a PostgreSQL background worker that doesn't block the calling session
- **FR-004**: System MUST expose progress information via queryable SQL function `steep_repl.snapshot_progress()`
- **FR-005**: System MUST send real-time progress updates via PostgreSQL LISTEN/NOTIFY on `steep_repl_progress` channel
- **FR-006**: System MUST provide SQL function API for all operations: snapshot generate, snapshot apply, schema compare, node registration, bidirectional merge, overlap analysis
- **FR-007**: System MUST validate appropriate privileges before executing operations:
  - Replication slot operations require REPLICATION privilege or superuser
  - Snapshot export/import via COPY TO/FROM STDOUT requires SELECT/INSERT on tables (no superuser needed)
  - Replication origin functions require superuser (or explicit GRANT)
  - Schema comparison and fingerprinting require SELECT on information_schema (any user)
- **FR-008**: System MUST maintain a work queue table (`steep_repl.work_queue`) for background worker job management
- **FR-009**: System MUST support cancellation of in-progress background operations
- **FR-010**: System MUST use shared memory for cross-session progress visibility
- **FR-011**: System MUST continue to support daemon mode via `--remote` flag during migration period
- **FR-012**: System MUST auto-detect available mode when neither flag is specified: try direct mode first, fall back to daemon only if extension doesn't support the operation
- **FR-013**: System MUST provide `steep_repl.health()` function as alternative to HTTP health endpoint

### Key Entities

- **Work Queue Entry**: Represents a queued, running, or completed operation. Key attributes: operation type, status (pending/running/complete/failed), parameters, timestamps, error message.
- **Snapshot Progress**: Real-time progress state for snapshot operations. Key attributes: phase, percentage, tables completed/total, current table, bytes processed, ETA.
- **Background Worker**: PostgreSQL process that polls work queue and executes operations. Relationship: processes Work Queue Entries, updates Snapshot Progress.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All steep-repl CLI commands work with `--direct` flag when no daemon is running
- **SC-002**: Snapshot operations on databases over 100GB complete successfully without session blocking
- **SC-003**: Progress updates are visible within 1 second of operation state changes
- **SC-004**: Configuration for new deployments requires only PostgreSQL connection details (no gRPC/IPC/HTTP config)
- **SC-005**: Operations started via SQL function API produce identical results to CLI-initiated operations
- **SC-006**: Existing daemon-mode deployments continue to work without modification during migration period
- **SC-007**: Background worker operations survive session disconnection and can be monitored from any new session

## Assumptions

- PostgreSQL 18+ is required (same as existing steep_repl requirement)
- `shared_preload_libraries` can be configured to include steep_repl for background worker support
- Users accept that background worker features require PostgreSQL restart after initial extension setup
- Remote CLI access (without direct PostgreSQL connectivity) will use SSH tunneling rather than gRPC proxy

## Design Decisions

### DD-001: No Daemon/Extension Coordination

**Decision**: The CLI decides which mode to use based on flags. There is no coordination protocol between daemon and extension.

**Rationale**: The goal is "when PostgreSQL is up, steep_repl is up." The daemon becomes optional. Adding coordination would:
- Increase complexity
- Add coupling between components
- Create race conditions
- Add polling overhead

**Implementation**: CLI tries direct mode first (extension), falls back to daemon only when auto-detecting and extension doesn't support the operation. Explicit `--direct` or `--remote` flags override auto-detection.

## Privilege Requirements

Operations have graduated privilege requirements - superuser is NOT a blanket requirement:

| Operation | Minimum Privilege Required |
|-----------|---------------------------|
| Schema fingerprinting | SELECT on information_schema |
| Node registration/heartbeat | INSERT/UPDATE on steep_repl.nodes |
| Snapshot export (COPY TO STDOUT) | SELECT on tables being exported |
| Snapshot import (COPY FROM STDIN) | INSERT on tables being imported |
| Replication slot creation | REPLICATION role attribute |
| Replication origin functions | Superuser (or explicit GRANT) |
| dblink/postgres_fdw operations | USAGE on extension + connection perms |
| Background worker operations | Extension in shared_preload_libraries |

Most operations work with a non-superuser role that has:
- `REPLICATION` attribute (for slot management)
- Appropriate table permissions (SELECT/INSERT)
- USAGE on dblink/postgres_fdw extensions
