# Data Model: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## Entities

### Lock

Represents an active database lock retrieved from pg_locks.

| Field | Type | Description | Source |
|-------|------|-------------|--------|
| PID | int | Process ID holding/waiting for lock | pg_locks.pid |
| User | string | Database username | pg_stat_activity.usename |
| Database | string | Database name | pg_stat_activity.datname |
| LockType | string | Type of lock (relation, transactionid, tuple, etc.) | pg_locks.locktype |
| Mode | string | Lock mode (AccessShareLock, RowExclusiveLock, etc.) | pg_locks.mode |
| Granted | bool | True if lock held, false if waiting | pg_locks.granted |
| Relation | string | Name of locked relation (table/index) or empty | pg_class.relname via pg_locks.relation |
| Query | string | Associated query text (may be truncated) | pg_stat_activity.query |
| State | string | Connection state (active, idle, etc.) | pg_stat_activity.state |
| Duration | time.Duration | Time since query started | age(clock_timestamp(), query_start) |
| WaitEventType | string | Type of wait event if waiting | pg_stat_activity.wait_event_type |
| WaitEvent | string | Specific wait event | pg_stat_activity.wait_event |

**Validation Rules**:
- PID must be > 0
- LockType must be one of: relation, transactionid, virtualxid, tuple, object, userlock, advisory
- Mode must be a valid PostgreSQL lock mode

### BlockingRelationship

Represents a blocker-blocked relationship between two processes.

| Field | Type | Description |
|-------|------|-------------|
| BlockedPID | int | PID of the process waiting for lock |
| BlockedUser | string | Username of blocked process |
| BlockedQuery | string | Query text of blocked process |
| BlockedDuration | time.Duration | How long the process has been blocked |
| BlockingPID | int | PID of the process holding the lock |
| BlockingUser | string | Username of blocking process |
| BlockingQuery | string | Query text of blocking process |

### BlockingChain

Hierarchical structure for lock dependency tree visualization.

| Field | Type | Description |
|-------|------|-------------|
| BlockerPID | int | PID of the blocking process |
| Query | string | Query text (truncated for display) |
| LockMode | string | Lock mode held |
| User | string | Username |
| Blocked | []BlockingChain | Recursively blocked processes |

## State Transitions

### Lock View States

```
┌─────────────┐     [5 key]      ┌─────────────┐
│  Any View   │ ───────────────► │ Locks View  │
└─────────────┘                  └─────────────┘
                                       │
                    ┌──────────────────┼──────────────────┐
                    │                  │                  │
                    ▼                  ▼                  ▼
              ┌─────────┐      ┌──────────────┐   ┌────────────┐
              │ Sorting │      │ Detail Modal │   │ Kill Dialog│
              │  [s]    │      │    [d]       │   │    [x]     │
              └─────────┘      └──────────────┘   └────────────┘
                    │                  │                  │
                    └──────────────────┼──────────────────┘
                                       │
                                       ▼
                               ┌─────────────┐
                               │ Locks View  │
                               └─────────────┘
```

### Kill Confirmation Flow

```
[x pressed on blocking query]
        │
        ▼
┌───────────────────┐     [Cancel/Esc]     ┌─────────────┐
│ Confirmation      │ ─────────────────────► │ Locks View  │
│ Dialog            │                        └─────────────┘
└───────────────────┘
        │
        │ [Confirm/Enter]
        ▼
┌───────────────────┐
│ pg_terminate_     │
│ backend()         │
└───────────────────┘
        │
        ├── Success ──► "Query terminated" message ──► Refresh
        │
        └── Failure ──► "Permission denied" or "Already terminated" ──► Refresh
```

## Relationships

```
┌─────────────────┐         ┌─────────────────────┐
│      Lock       │         │ BlockingRelationship │
│                 │         │                      │
│ PID             │◄────────│ BlockedPID           │
│ User            │         │ BlockingPID          │
│ Query           │         │                      │
│ ...             │         └─────────────────────┘
└─────────────────┘
        │
        │ builds
        ▼
┌─────────────────┐
│ BlockingChain   │
│                 │
│ BlockerPID      │
│ Blocked[] ──────┼──► [Recursive BlockingChain]
└─────────────────┘
```

## Display Formatting

### Color Coding

| State | Foreground Color | Background | Description |
|-------|------------------|------------|-------------|
| Blocked | Red (#FF0000) | Default | Query waiting for lock |
| Blocking | Yellow (#FFFF00) | Default | Query holding lock that blocks others |
| Normal | Default | Default | Query with lock, not involved in blocking |

### Table Columns

| Column | Width | Alignment | Truncation |
|--------|-------|-----------|------------|
| PID | 8 | Right | None |
| Type | 15 | Left | None |
| Mode | 20 | Left | Truncate |
| Granted | 7 | Center | None |
| DB | 12 | Left | Truncate |
| Relation | 20 | Left | Truncate |
| Query | Remaining | Left | Truncate with "..." |

### Tree Node Format

```
[PID:1234 RowExclusiveLock] UPDATE users SET status...
```

- Metadata in brackets: PID and lock mode
- Query truncated to ~50 characters
- Newlines replaced with spaces
