# Internal Message Contracts: Dashboard & Activity Monitoring

**Feature**: 002-dashboard-activity
**Date**: 2025-11-21

This feature is a TUI application using Bubbletea, so contracts are internal tea.Msg types rather than HTTP APIs.

## Bubbletea Messages

### Data Messages (from monitors to UI)

#### ActivityDataMsg
Data from activity monitor goroutine.

```go
type ActivityDataMsg struct {
    Connections []Connection
    TotalCount  int
    FetchedAt   time.Time
    Error       error
}
```

#### MetricsDataMsg
Data from metrics monitor goroutine.

```go
type MetricsDataMsg struct {
    Metrics   Metrics
    FetchedAt time.Time
    Error     error
}
```

### Command Messages (UI to monitors)

#### RefreshActivityCmd
Request fresh activity data with filters.

```go
type RefreshActivityCmd struct {
    Filter ActivityFilter
    Limit  int
    Offset int
}
```

#### RefreshMetricsCmd
Request fresh metrics data.

```go
type RefreshMetricsCmd struct{}
```

### Action Messages

#### CancelQueryMsg
Cancel a running query.

```go
type CancelQueryMsg struct {
    PID int
}
```

#### CancelQueryResultMsg
Result of cancel attempt.

```go
type CancelQueryResultMsg struct {
    PID     int
    Success bool
    Error   error
}
```

#### TerminateConnectionMsg
Terminate a connection.

```go
type TerminateConnectionMsg struct {
    PID int
}
```

#### TerminateConnectionResultMsg
Result of terminate attempt.

```go
type TerminateConnectionResultMsg struct {
    PID     int
    Success bool
    Error   error
}
```

### UI State Messages

#### TickMsg
Periodic refresh trigger.

```go
type TickMsg time.Time
```

#### ShowDialogMsg
Show confirmation dialog.

```go
type ShowDialogMsg struct {
    Action    string // "cancel" or "terminate"
    TargetPID int
    Query     string // Truncated query for display
}
```

#### DialogResponseMsg
User response to dialog.

```go
type DialogResponseMsg struct {
    Confirmed bool
}
```

#### FilterChangedMsg
User changed filter settings.

```go
type FilterChangedMsg struct {
    Filter ActivityFilter
}
```

#### SortChangedMsg
User changed sort column.

```go
type SortChangedMsg struct {
    Column    string
    Ascending bool
}
```

#### ConnectionErrorMsg
Database connection lost.

```go
type ConnectionErrorMsg struct {
    Error   error
    Attempt int
}
```

#### ConnectionRestoredMsg
Database connection restored.

```go
type ConnectionRestoredMsg struct{}
```

## Message Flow Diagrams

### Normal Refresh Cycle
```
Init -> TickMsg -> RefreshActivityCmd -> ActivityDataMsg -> Update table
                -> RefreshMetricsCmd  -> MetricsDataMsg  -> Update panels
     -> TickMsg -> (repeat)
```

### Cancel Query Flow
```
User presses 'c' -> ShowDialogMsg -> Render dialog
User presses 'y' -> DialogResponseMsg(confirmed=true) -> CancelQueryMsg
                 -> CancelQueryResultMsg -> Show success/error toast
                                         -> RefreshActivityCmd (immediate)
```

### Connection Loss Flow
```
Query fails -> ConnectionErrorMsg(attempt=1) -> Show error, start backoff
            -> Retry after 1s -> ConnectionErrorMsg(attempt=2)
            -> Retry after 2s -> ConnectionErrorMsg(attempt=3)
            -> ... (up to 30s max)
            -> Success -> ConnectionRestoredMsg -> Resume normal refresh
```

### Filter Change Flow
```
User presses '/' -> Enter filter mode -> Input text
User presses Enter -> FilterChangedMsg -> RefreshActivityCmd with filter
                                       -> ActivityDataMsg -> Update table
```

## Channel Contracts

### ActivityMonitor Channel
```go
activityChan chan ActivityDataMsg
```
- Monitor sends data every refresh interval
- UI reads and updates table
- Buffer size: 1 (latest data wins)

### MetricsMonitor Channel
```go
metricsChan chan MetricsDataMsg
```
- Monitor sends data every refresh interval
- UI reads and updates panels
- Buffer size: 1 (latest data wins)

### Command Channel
```go
commandChan chan interface{} // RefreshActivityCmd, CancelQueryMsg, etc.
```
- UI sends commands to monitors
- Monitors read and execute
- Buffer size: 10 (allow queuing)
