# Data Model: Log Viewer

**Feature**: 009-log-viewer
**Date**: 2025-11-27

## Entities

### LogEntry

A single parsed log record from PostgreSQL server logs.

| Field | Type | Description | Validation |
|-------|------|-------------|------------|
| `Timestamp` | `time.Time` | When the log entry was written | Required, parseable from log |
| `Severity` | `LogSeverity` | Log level (ERROR, WARNING, INFO, DEBUG) | Required, enum |
| `PID` | `int` | PostgreSQL backend process ID | Optional, 0 if unknown |
| `Database` | `string` | Database name | Optional, empty if N/A |
| `User` | `string` | PostgreSQL username | Optional, empty if N/A |
| `Application` | `string` | Application name from connection | Optional |
| `Message` | `string` | Log message content | Required |
| `Detail` | `string` | DETAIL line if present | Optional |
| `Hint` | `string` | HINT line if present | Optional |
| `RawLine` | `string` | Original unparsed log line | For debugging/fallback |
| `SourceFile` | `string` | Log file this entry came from | Required for position tracking |
| `SourceLine` | `int64` | Byte offset in source file | For bookmark/resume |

### LogSeverity (Enum)

```go
type LogSeverity int

const (
    SeverityDebug   LogSeverity = iota  // DEBUG1-5
    SeverityInfo                         // LOG, INFO, NOTICE
    SeverityWarning                      // WARNING
    SeverityError                        // ERROR, FATAL, PANIC
)
```

**Mapping from PostgreSQL severities:**

| PostgreSQL | LogSeverity | Display Color |
|------------|-------------|---------------|
| DEBUG1, DEBUG2, DEBUG3, DEBUG4, DEBUG5 | SeverityDebug | Gray (#808080) |
| LOG, INFO, NOTICE | SeverityInfo | White (default) |
| WARNING | SeverityWarning | Yellow (#FFFF00) |
| ERROR, FATAL, PANIC | SeverityError | Red (#FF0000) |

---

### LogBuffer

Ring buffer holding parsed log entries in memory.

| Field | Type | Description |
|-------|------|-------------|
| `entries` | `[]LogEntry` | Fixed-size slice (capacity 10,000) |
| `head` | `int` | Next write index |
| `size` | `int` | Current number of entries (0 to cap) |
| `cap` | `int` | Maximum capacity (10,000) |

**Operations:**
- `Add(entry LogEntry)` - Insert new entry, evict oldest if full
- `GetRange(start, count int) []LogEntry` - Get entries for viewport
- `GetAll() []LogEntry` - Get all entries (for search)
- `Clear()` - Reset buffer
- `Len() int` - Current entry count

---

### LogFilter

Active filter criteria for log display.

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `Severity` | `*LogSeverity` | Filter to single severity level | nil (show all) |
| `MinSeverity` | `LogSeverity` | Minimum severity to show | SeverityDebug |
| `SearchPattern` | `*regexp.Regexp` | Compiled regex for text search | nil |
| `SearchText` | `string` | Original search text (for display) | "" |
| `TimestampFrom` | `*time.Time` | Start of timestamp range | nil |
| `TimestampTo` | `*time.Time` | End of timestamp range | nil |

**Operations:**
- `Matches(entry LogEntry) bool` - Check if entry passes filter
- `SetLevel(level string) error` - Parse and set severity filter
- `SetSearch(pattern string) error` - Compile and set search pattern
- `Clear()` - Reset all filters

---

### LogSource

Configuration for log data source.

| Field | Type | Description |
|-------|------|-------------|
| `LogDir` | `string` | Directory containing log files |
| `LogPattern` | `string` | Glob pattern for log files (e.g., "postgresql-*.log") |
| `LogFormat` | `LogFormat` | Detected format (CSV, JSON, Stderr) |
| `AccessMethod` | `AccessMethod` | How to read logs (FileSystem, PgReadFile) |
| `Enabled` | `bool` | Whether logging is enabled on server |

### LogFormat (Enum)

```go
type LogFormat int

const (
    LogFormatUnknown LogFormat = iota
    LogFormatStderr           // Plain text with log_line_prefix
    LogFormatCSV              // PostgreSQL csvlog format
    LogFormatJSON             // PostgreSQL jsonlog format
)
```

### AccessMethod (Enum)

```go
type AccessMethod int

const (
    AccessFileSystem AccessMethod = iota  // Direct file read
    AccessPgReadFile                       // pg_read_file() function
)
```

---

### LogViewerState

Complete state for the log viewer view.

| Field | Type | Description |
|-------|------|-------------|
| `width` | `int` | Terminal width |
| `height` | `int` | Terminal height |
| `viewport` | `viewport.Model` | Bubbles viewport for scrolling |
| `buffer` | `*LogBuffer` | In-memory log entries |
| `filter` | `LogFilter` | Active filter criteria |
| `source` | `LogSource` | Log source configuration |
| `followMode` | `bool` | Auto-scroll to newest |
| `mode` | `ViewMode` | Current interaction mode |
| `lastUpdate` | `time.Time` | Last log refresh timestamp |
| `err` | `error` | Current error state |
| `toastMsg` | `string` | Toast notification message |
| `readOnly` | `bool` | Read-only mode flag |

### ViewMode (Enum)

```go
type ViewMode int

const (
    ModeNormal ViewMode = iota
    ModeSearch                    // Typing search pattern
    ModeCommand                   // Typing :command
    ModeConfirmEnableLogging      // Prompt to enable logging
    ModeHelp                      // Showing help overlay
)
```

---

## State Transitions

```
┌─────────────────────────────────────────────────────────────────────┐
│                            View Lifecycle                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────┐    CheckLoggingStatus()    ┌────────────────────┐   │
│  │   Init   │ ──────────────────────────▶│ Logging Disabled?  │   │
│  └──────────┘                            └─────────┬──────────┘   │
│       │                                       yes  │  no          │
│       │                              ┌─────────────┴──────────┐   │
│       │                              ▼                        ▼   │
│       │                    ┌─────────────────┐    ┌───────────┐   │
│       │                    │ ConfirmEnable   │    │  Loading  │   │
│       │                    │    Dialog       │    │   Logs    │   │
│       │                    └────────┬────────┘    └─────┬─────┘   │
│       │                        y/n  │                   │         │
│       │               ┌─────────────┼─────────────┐     │         │
│       │               ▼             ▼             ▼     │         │
│       │    ┌──────────────┐  ┌──────────┐  ┌──────────┐ │         │
│       │    │EnableLogging │  │  Error   │  │  Error   │ │         │
│       │    │   +Reload    │  │  (show   │  │  (show   │ │         │
│       │    └──────┬───────┘  │  config) │  │ message) │ │         │
│       │           │          └──────────┘  └──────────┘ │         │
│       │           ▼                                     │         │
│       └──────────▶┌─────────────────────────────────────┘         │
│                   ▼                                               │
│           ┌───────────────┐                                       │
│           │  ModeNormal   │◀────────────────────────────┐        │
│           │ (follow=true) │                              │        │
│           └───────┬───────┘                              │        │
│                   │                                      │        │
│    ┌──────────────┼──────────────────────┐              │        │
│    │ Press '/'    │ Press ':'   Press 'f'│              │        │
│    ▼              ▼              ▼        │              │        │
│ ┌──────────┐ ┌──────────┐  ┌──────────┐  │              │        │
│ │ModeSearch│ │ModeCmd   │  │ Toggle   │  │              │        │
│ │ (input)  │ │ (input)  │  │ Follow   │  │              │        │
│ └────┬─────┘ └────┬─────┘  └──────────┘  │              │        │
│      │            │                      │              │        │
│ Enter│       Enter│                      │              │        │
│      ▼            ▼                      │              │        │
│ ┌──────────┐ ┌──────────┐               │              │        │
│ │ Apply    │ │ Parse &  │               │              │        │
│ │ Filter   │ │ Execute  │               │              │        │
│ └────┬─────┘ └────┬─────┘               │              │        │
│      │            │                      │              │        │
│      └────────────┴──────────────────────┴──────────────┘        │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

---

## Relationships

```
┌─────────────────────────────────────────────────────────────────┐
│                        LogViewerState                           │
│                                                                 │
│  ┌───────────────┐    ┌───────────────┐    ┌───────────────┐  │
│  │   LogBuffer   │    │   LogFilter   │    │   LogSource   │  │
│  │               │    │               │    │               │  │
│  │ entries[]:    │    │ Severity      │    │ LogDir        │  │
│  │  LogEntry     │──▶ │ SearchPattern │    │ LogPattern    │  │
│  │               │    │ TimestampFrom │    │ LogFormat     │  │
│  └───────────────┘    └───────────────┘    └───────────────┘  │
│         │                     │                    │           │
│         │                     │                    │           │
│         ▼                     │                    │           │
│  ┌───────────────┐            │                    │           │
│  │   LogEntry    │◀───────────┘ (filter.Matches)   │           │
│  │               │                                 │           │
│  │ Timestamp     │                                 │           │
│  │ Severity      │◀────────────────────────────────┘           │
│  │ PID           │        (parsed from source)                 │
│  │ Message       │                                             │
│  │ ...           │                                             │
│  └───────────────┘                                             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## File Locations

| Entity | File Path |
|--------|-----------|
| `LogEntry` | `internal/db/models/log_entry.go` |
| `LogSeverity` | `internal/db/models/log_entry.go` |
| `LogBuffer` | `internal/ui/views/logs/buffer.go` |
| `LogFilter` | `internal/ui/views/logs/filter.go` |
| `LogSource` | `internal/monitors/log_source.go` |
| `LogViewerState` | `internal/ui/views/logs/view.go` |
