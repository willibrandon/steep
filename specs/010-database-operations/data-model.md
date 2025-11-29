# Data Model: Database Management Operations

**Feature**: 010-database-operations | **Date**: 2025-11-28

## Overview

This document defines the data entities required for database maintenance operations and role management in Steep. These models extend the existing table/index models with vacuum status tracking and add new entities for roles, permissions, and operation progress.

## Entity Definitions

### 1. Table (Extended)

Extends the existing `models.Table` struct with vacuum/analyze status fields.

**New Fields**:

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| `LastVacuum` | `*time.Time` | Last manual VACUUM timestamp | `pg_stat_all_tables.last_vacuum` |
| `LastAutovacuum` | `*time.Time` | Last autovacuum timestamp | `pg_stat_all_tables.last_autovacuum` |
| `LastAnalyze` | `*time.Time` | Last manual ANALYZE timestamp | `pg_stat_all_tables.last_analyze` |
| `LastAutoanalyze` | `*time.Time` | Last autoanalyze timestamp | `pg_stat_all_tables.last_autoanalyze` |
| `VacuumCount` | `int64` | Total manual vacuum count | `pg_stat_all_tables.vacuum_count` |
| `AutovacuumCount` | `int64` | Total autovacuum count | `pg_stat_all_tables.autovacuum_count` |
| `AutovacuumEnabled` | `bool` | Whether autovacuum is enabled for table | `pg_class.reloptions` |

**Derived Fields**:

| Field | Type | Description | Calculation |
|-------|------|-------------|-------------|
| `IsVacuumStale` | `bool` | Whether vacuum is overdue | `time.Since(LastVacuum) > StaleThreshold` |
| `LastMaintenanceTime` | `*time.Time` | Most recent vacuum (manual or auto) | `max(LastVacuum, LastAutovacuum)` |

**Validation Rules**:
- Timestamps can be nil if never vacuumed/analyzed
- StaleThreshold defaults to 7 days (configurable)

---

### 2. Role

Represents a PostgreSQL role (user or group).

**Fields**:

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| `OID` | `uint32` | Role OID | `pg_roles.oid` |
| `Name` | `string` | Role name | `pg_roles.rolname` |
| `IsSuperuser` | `bool` | Has superuser privileges | `pg_roles.rolsuper` |
| `CanLogin` | `bool` | Can establish connections | `pg_roles.rolcanlogin` |
| `CanCreateRole` | `bool` | Can create other roles | `pg_roles.rolcreaterole` |
| `CanCreateDB` | `bool` | Can create databases | `pg_roles.rolcreatedb` |
| `CanBypassRLS` | `bool` | Can bypass row-level security | `pg_roles.rolbypassrls` |
| `ConnectionLimit` | `int` | Max concurrent connections (-1 = unlimited) | `pg_roles.rolconnlimit` |
| `ValidUntil` | `*time.Time` | Password expiration time | `pg_roles.rolvaliduntil` |
| `Memberships` | `[]RoleMembership` | Roles this role is a member of | `pg_auth_members` |
| `MemberOf` | `[]string` | Role names this role belongs to (flattened) | Derived |

**Relationships**:
- Role -> RoleMembership (1:N) via `pg_auth_members`
- Role -> Permission (1:N) via `pg_default_acl`, `relacl`, etc.

**Validation Rules**:
- Name must be non-empty
- ConnectionLimit >= -1 (where -1 means unlimited)

---

### 3. RoleMembership

Represents membership of one role in another.

**Fields**:

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| `RoleOID` | `uint32` | OID of the parent role (group) | `pg_auth_members.roleid` |
| `RoleName` | `string` | Name of the parent role | Joined from `pg_roles` |
| `MemberOID` | `uint32` | OID of the member role | `pg_auth_members.member` |
| `MemberName` | `string` | Name of the member role | Joined from `pg_roles` |
| `GrantorOID` | `uint32` | Who granted the membership | `pg_auth_members.grantor` |
| `GrantorName` | `string` | Name of the grantor | Joined from `pg_roles` |
| `AdminOption` | `bool` | Can grant membership to others | `pg_auth_members.admin_option` |
| `InheritOption` | `bool` | Automatically inherits privileges | `pg_auth_members.inherit_option` |
| `SetOption` | `bool` | Can SET ROLE to this role | `pg_auth_members.set_option` |

---

### 4. Permission

Represents a privilege grant on a database object.

**Fields**:

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| `ObjectType` | `PermissionObjectType` | Type of object (table, schema, etc.) | Derived |
| `ObjectOID` | `uint32` | OID of the object | Various catalogs |
| `ObjectName` | `string` | Qualified object name | Derived |
| `Grantee` | `string` | Role name or PUBLIC | `aclexplode()` |
| `Grantor` | `string` | Who granted the privilege | `aclexplode()` |
| `PrivilegeType` | `PrivilegeType` | Privilege type (SELECT, INSERT, etc.) | `aclexplode()` |
| `IsGrantable` | `bool` | Can re-grant to others | `aclexplode()` |

**Enums**:

```go
type PermissionObjectType string
const (
    ObjectTypeTable    PermissionObjectType = "table"
    ObjectTypeSchema   PermissionObjectType = "schema"
    ObjectTypeDatabase PermissionObjectType = "database"
    ObjectTypeSequence PermissionObjectType = "sequence"
    ObjectTypeFunction PermissionObjectType = "function"
)

type PrivilegeType string
const (
    PrivilegeSelect     PrivilegeType = "SELECT"
    PrivilegeInsert     PrivilegeType = "INSERT"
    PrivilegeUpdate     PrivilegeType = "UPDATE"
    PrivilegeDelete     PrivilegeType = "DELETE"
    PrivilegeTruncate   PrivilegeType = "TRUNCATE"
    PrivilegeReferences PrivilegeType = "REFERENCES"
    PrivilegeTrigger    PrivilegeType = "TRIGGER"
    PrivilegeCreate     PrivilegeType = "CREATE"
    PrivilegeConnect    PrivilegeType = "CONNECT"
    PrivilegeUsage      PrivilegeType = "USAGE"
    PrivilegeExecute    PrivilegeType = "EXECUTE"
    PrivilegeAll        PrivilegeType = "ALL"
)
```

---

### 5. MaintenanceOperation

Represents an in-progress or completed maintenance operation.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique operation ID (UUID) |
| `Type` | `OperationType` | Operation type |
| `TargetSchema` | `string` | Schema name |
| `TargetTable` | `string` | Table name |
| `TargetIndex` | `string` | Index name (for REINDEX INDEX) |
| `Status` | `OperationStatus` | Current status |
| `Progress` | `*OperationProgress` | Progress data (nil if not trackable) |
| `BackendPID` | `int` | PostgreSQL backend PID |
| `StartedAt` | `time.Time` | Operation start time |
| `CompletedAt` | `*time.Time` | Operation completion time |
| `Duration` | `time.Duration` | Total duration (calculated) |
| `Error` | `error` | Error if failed |
| `Result` | `*OperationResult` | Result data if completed |

**Enums**:

```go
type OperationType string
const (
    OpVacuum        OperationType = "VACUUM"
    OpVacuumFull    OperationType = "VACUUM FULL"
    OpVacuumAnalyze OperationType = "VACUUM ANALYZE"
    OpAnalyze       OperationType = "ANALYZE"
    OpReindexTable  OperationType = "REINDEX TABLE"
    OpReindexIndex  OperationType = "REINDEX INDEX"
)

type OperationStatus string
const (
    StatusPending    OperationStatus = "pending"
    StatusRunning    OperationStatus = "running"
    StatusCompleted  OperationStatus = "completed"
    StatusCancelled  OperationStatus = "cancelled"
    StatusFailed     OperationStatus = "failed"
)
```

**State Transitions**:
```
pending -> running -> completed
                   -> cancelled
                   -> failed
```

---

### 6. OperationProgress

Real-time progress data for trackable operations.

**Fields**:

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| `Phase` | `string` | Current VACUUM phase | `pg_stat_progress_vacuum.phase` |
| `HeapBlksTotal` | `int64` | Total heap blocks | `pg_stat_progress_vacuum.heap_blks_total` |
| `HeapBlksScanned` | `int64` | Heap blocks scanned | `pg_stat_progress_vacuum.heap_blks_scanned` |
| `HeapBlksVacuumed` | `int64` | Heap blocks vacuumed | `pg_stat_progress_vacuum.heap_blks_vacuumed` |
| `IndexVacuumCount` | `int64` | Index vacuum cycles completed | `pg_stat_progress_vacuum.index_vacuum_count` |
| `IndexesTotal` | `int64` | Total indexes to process | `pg_stat_progress_vacuum.indexes_total` |
| `IndexesProcessed` | `int64` | Indexes already processed | `pg_stat_progress_vacuum.indexes_processed` |
| `PercentComplete` | `float64` | Overall progress percentage | Calculated |
| `LastUpdated` | `time.Time` | Last progress update time | Client-side |

**Progress Calculation**:

```go
func (p *OperationProgress) CalculatePercent() float64 {
    if p.HeapBlksTotal == 0 {
        return 0
    }
    return float64(p.HeapBlksScanned) / float64(p.HeapBlksTotal) * 100
}
```

**Supported Operations**:
- VACUUM: Uses `pg_stat_progress_vacuum`
- VACUUM FULL: Uses `pg_stat_progress_cluster`
- ANALYZE, REINDEX: No progress tracking (show "In Progress" only)

---

### 7. OperationResult

Result data for completed maintenance operations.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `DeadTuplesRemoved` | `int64` | Dead tuples removed (VACUUM) |
| `PagesReclaimed` | `int64` | Pages returned to OS (VACUUM FULL) |
| `IndexesRebuilt` | `int` | Number of indexes rebuilt (REINDEX) |
| `StatisticsUpdated` | `bool` | Whether stats were updated (ANALYZE) |
| `Message` | `string` | Human-readable result summary |

---

### 8. OperationHistory

Session-scoped history of executed operations (not persisted).

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `Operations` | `[]MaintenanceOperation` | List of operations in session |
| `MaxEntries` | `int` | Maximum history entries (default: 100) |

**Behavior**:
- Cleared on application exit (session-only per spec)
- FIFO eviction when MaxEntries exceeded
- Most recent operations first

---

## Entity Relationship Diagram

```
┌─────────────────┐
│     Schema      │
├─────────────────┤
│ - OID           │
│ - Name          │
│ - Tables[]      │◄────────┐
└─────────────────┘         │
                            │ contains
┌─────────────────┐         │
│      Table      │─────────┘
├─────────────────┤
│ - OID           │
│ - LastVacuum    │
│ - LastAutovacuum│
│ - Indexes[]     │
└─────────────────┘
        │
        │ target of
        ▼
┌─────────────────┐       ┌─────────────────┐
│ Maintenance     │       │ Operation       │
│ Operation       │──────►│ Progress        │
├─────────────────┤       ├─────────────────┤
│ - Type          │       │ - Phase         │
│ - Status        │       │ - PercentComplete│
│ - BackendPID    │       └─────────────────┘
│ - Progress*     │
│ - Result*       │
└─────────────────┘
        │
        │ result
        ▼
┌─────────────────┐
│ Operation       │
│ Result          │
├─────────────────┤
│ - DeadTuples    │
│ - PagesReclaimed│
└─────────────────┘

┌─────────────────┐
│      Role       │
├─────────────────┤       ┌─────────────────┐
│ - OID           │       │ Role            │
│ - Name          │◄─────►│ Membership      │
│ - IsSuperuser   │       ├─────────────────┤
│ - CanLogin      │       │ - RoleOID       │
│ - Memberships[] │       │ - MemberOID     │
└─────────────────┘       │ - AdminOption   │
        │                 └─────────────────┘
        │ grants
        ▼
┌─────────────────┐
│   Permission    │
├─────────────────┤
│ - ObjectType    │
│ - ObjectOID     │
│ - Grantee       │
│ - PrivilegeType │
└─────────────────┘
```

## Database Schema References

### Key System Views Used

| View | Purpose |
|------|---------|
| `pg_stat_all_tables` | Vacuum/analyze timestamps and counts |
| `pg_stat_progress_vacuum` | Real-time VACUUM progress |
| `pg_stat_progress_cluster` | Real-time VACUUM FULL progress |
| `pg_roles` | Role attributes |
| `pg_auth_members` | Role membership relationships |
| `pg_class.relacl` | Table-level ACLs |
| `pg_namespace.nspacl` | Schema-level ACLs |
| `pg_database.datacl` | Database-level ACLs |

### Key Functions Used

| Function | Purpose |
|----------|---------|
| `pg_cancel_backend(pid)` | Cancel running operation |
| `aclexplode(acl)` | Parse ACL arrays into rows |
| `has_table_privilege(role, table, priv)` | Check specific privilege |
| `pg_get_userbyid(oid)` | Get role name from OID |
