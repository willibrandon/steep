# Data Model: Foundation Infrastructure

**Feature**: 001-foundation | **Date**: 2025-11-19

## Overview

This document defines the data structures for Steep's foundation infrastructure, including configuration models, application state, and database connection profiles.

---

## 1. Configuration Model

### Config

**Purpose**: Root configuration structure loaded from YAML file

**Fields**:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| connection | ConnectionConfig | Yes | localhost defaults | Database connection settings |
| ui | UIConfig | No | Default theme | User interface preferences |
| debug | bool | No | false | Enable debug logging |

**Validation Rules**:
- connection.host must not be empty
- connection.port must be 1-65535
- connection.database must not be empty

**Example**:
```yaml
connection:
  host: localhost
  port: 5432
  database: postgres
  user: brandon
  password_command: "pass show postgres/local"
  sslmode: prefer
  pool_max_conns: 10

ui:
  theme: dark
  refresh_interval: 1s

debug: false
```

---

### ConnectionConfig

**Purpose**: Database connection parameters

**Fields**:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| host | string | Yes | "localhost" | PostgreSQL host |
| port | int | Yes | 5432 | PostgreSQL port |
| database | string | Yes | "postgres" | Database name |
| user | string | Yes | OS username | Database user |
| password_command | string | No | nil | Command to retrieve password |
| sslmode | string | No | "prefer" | SSL mode: disable, prefer, require |
| pool_max_conns | int | No | 10 | Maximum connections in pool |
| pool_min_conns | int | No | 2 | Minimum connections in pool |

**Validation Rules**:
- sslmode must be one of: disable, prefer, require
- pool_max_conns must be >= pool_min_conns
- pool_max_conns must be >= 1
- pool_min_conns must be >= 0

**Derived Fields**:
- connection_string: Computed from host, port, database, user, sslmode

**State Transitions**: None (immutable after load)

---

### UIConfig

**Purpose**: User interface preferences

**Fields**:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| theme | string | No | "dark" | Color theme: dark, light |
| refresh_interval | duration | No | "1s" | Status bar refresh rate |
| date_format | string | No | "2006-01-02 15:04:05" | Go time format string |

**Validation Rules**:
| theme must be one of: dark, light
- refresh_interval must be >= 100ms and <= 60s
- date_format must be valid Go time format

**State Transitions**: None (immutable after load)

---

## 2. Application State Model

### Model (Bubbletea Root Model)

**Purpose**: Top-level application state for Bubbletea

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| config | *Config | Loaded configuration |
| dbPool | *pgxpool.Pool | PostgreSQL connection pool |
| currentView | ViewType | Currently displayed view |
| views | map[ViewType]ViewModel | View instances |
| width | int | Terminal width in characters |
| height | int | Terminal height in characters |
| connected | bool | Database connection status |
| connectionErr | error | Last connection error (if any) |
| statusBarData | StatusBarData | Status bar metrics |
| helpVisible | bool | Help overlay visibility |
| quitting | bool | Application shutdown flag |

**Relationships**:
- Model → Config (1:1)
- Model → dbPool (1:1 optional)
- Model → ViewModels (1:many)

**State Lifecycle**:
```
Init → Loading Config → Connecting DB → Ready → Quitting
                ↓                ↓
            ConfigError    ConnectionError
```

**State Transitions**:

| From State | Event | To State | Action |
|------------|-------|----------|--------|
| Init | ConfigLoaded | Connecting DB | Create connection pool |
| Connecting DB | DatabaseConnected | Ready | Start status bar ticker |
| Connecting DB | ConnectionFailed | ConnectionError | Display error message |
| Ready | KeyMsg('q') | Quitting | Cleanup and exit |
| Ready | ConnectionLost | ConnectionError | Attempt reconnection |
| ConnectionError | Reconnected | Ready | Resume normal operation |

---

### ViewType

**Purpose**: Enumeration of available views

**Values**:

| Value | Description |
|-------|-------------|
| Dashboard | Main overview (placeholder in foundation) |
| Activity | Connection activity (future feature) |
| Queries | Query performance (future feature) |
| Locks | Lock monitoring (future feature) |
| Tables | Table statistics (future feature) |
| Replication | Replication status (future feature) |

**Foundation Phase**: Only Dashboard is implemented; others are placeholders

---

### ViewModel Interface

**Purpose**: Common interface for all views

**Methods**:

```go
type ViewModel interface {
    Init() tea.Cmd
    Update(tea.Msg) (ViewModel, tea.Cmd)
    View() string
    SetSize(width, height int)
}
```

**Implementations**:
- DashboardView (foundation phase)
- Future: ActivityView, QueriesView, LocksView, TablesView, ReplicationView

---

### StatusBarData

**Purpose**: Metrics displayed in status bar

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| connected | bool | Connection status |
| database | string | Database name |
| timestamp | time.Time | Current time |
| activeConnections | int | Active connection count from pg_stat_activity |

**Update Frequency**: Every 1 second (configurable via ui.refresh_interval)

**Query Source**: `SELECT count(*) FROM pg_stat_activity WHERE state = 'active'`

---

## 3. Runtime Models

### DatabaseMetrics

**Purpose**: Metrics retrieved from PostgreSQL

**Fields**:

| Field | Type | Source Query |
|-------|------|--------------|
| ActiveConnections | int | `SELECT count(*) FROM pg_stat_activity WHERE state = 'active'` |
| TotalConnections | int | `SELECT count(*) FROM pg_stat_activity` |
| ServerVersion | string | `SELECT version()` |

**Update Frequency**: ActiveConnections updates every 1s for status bar; others on-demand

---

### ReconnectionState

**Purpose**: Tracks automatic reconnection attempts

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| attempt | int | Current attempt number (1-5) |
| lastAttempt | time.Time | Timestamp of last attempt |
| nextDelay | duration | Delay until next attempt (exponential backoff) |
| maxAttempts | int | Maximum attempts before giving up (5) |

**Exponential Backoff**:
- Attempt 1: 1s delay
- Attempt 2: 2s delay
- Attempt 3: 4s delay
- Attempt 4: 8s delay
- Attempt 5: 16s delay (capped at 30s max)

**State Transitions**:
```
Idle → AttemptReconnect → Success → Idle
                        → Failure → Wait → AttemptReconnect
                        → MaxAttemptsExceeded → Failed
```

---

## 4. Message Types (Bubbletea)

### Custom Messages

**Purpose**: Domain events for Bubbletea Update() function

| Message Type | Fields | Trigger |
|--------------|--------|---------|
| DatabaseConnectedMsg | pool *pgxpool.Pool | Successful connection |
| ConnectionFailedMsg | err error | Connection failure |
| ReconnectAttemptMsg | attempt int | Reconnection try |
| StatusBarTickMsg | data StatusBarData | Periodic status update |
| MetricsUpdateMsg | metrics DatabaseMetrics | Query results |

**Message Flow**:
```
User Presses 'q' → tea.KeyMsg → Model.Update() → tea.Quit
Database Query Complete → MetricsUpdateMsg → Model.Update() → Update StatusBarData
```

---

## 5. Environment Variable Overrides

**Purpose**: Allow runtime configuration without editing YAML

| Environment Variable | Overrides Config Field | Priority |
|---------------------|------------------------|----------|
| STEEP_CONNECTION_HOST | connection.host | Highest |
| STEEP_CONNECTION_PORT | connection.port | Highest |
| STEEP_CONNECTION_DATABASE | connection.database | Highest |
| STEEP_CONNECTION_USER | connection.user | Highest |
| STEEP_DEBUG | debug | Highest |
| PGHOST | connection.host | Medium |
| PGPORT | connection.port | Medium |
| PGDATABASE | connection.database | Medium |
| PGUSER | connection.user | Medium |
| PGPASSWORD | password (runtime only) | Medium |

**Precedence Order**:
1. STEEP_* environment variables (highest)
2. PG* environment variables (PostgreSQL standard)
3. Config file values
4. Default values (lowest)

---

## 6. Password Retrieval Flow

**Purpose**: Secure password acquisition without plaintext storage

**Flow**:
```
1. Check password_command in config → Execute if present
2. Check PGPASSWORD environment variable → Use if set
3. Check ~/.pgpass file → Parse for matching credentials
4. Prompt interactively → Read from stdin with hidden input
```

**password_command Execution**:
- Timeout: 5 seconds
- Output: Trim whitespace from stdout
- Error Handling: Display actionable message if command fails

**Interactive Prompt**:
- Hide input characters (terminal raw mode)
- Display: "Enter password for user@host:port/database: "
- Timeout: None (wait for user input)

---

## Summary

All data models defined with validation rules, state transitions, and relationships. Key entities:

1. **Config**: YAML-backed configuration with environment overrides
2. **Model**: Bubbletea root state with view management
3. **StatusBarData**: Real-time metrics from PostgreSQL
4. **ReconnectionState**: Exponential backoff reconnection logic
5. **Message Types**: Custom events for Bubbletea message passing

No complex state machines required in foundation phase. Most state is immutable configuration or simple boolean flags (connected, quitting). Reconnection logic is the only stateful component with defined transitions.

Ready for contract generation (Phase 1 continuation).
