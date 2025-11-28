# Research: Log Viewer

**Feature**: 009-log-viewer
**Date**: 2025-11-27

## Research Summary

All technical context items were resolved through codebase analysis. This document captures research findings and best practices for the Log Viewer implementation.

---

## 1. Log Format Parsing

### Decision: Multi-format support with auto-detection

### Rationale
PostgreSQL supports multiple log formats (stderr, csvlog, jsonlog). The existing codebase already has parsers for CSV and JSON formats. Auto-detection based on file extension and content allows seamless handling without user configuration.

### Alternatives Considered
1. **Single format only**: Rejected - limits usability across different PostgreSQL configurations
2. **User-configurable format**: Rejected - adds configuration burden; auto-detection is sufficient
3. **Parse log_destination setting**: Chosen - leverage existing `CheckLoggingStatus()` to detect format

### Implementation Notes
- JSON format (`.json`): Use existing `JSONLogEntry` struct from `json_log_parser.go`
- CSV format (`.csv`): Use column indices from `csv_log_parser.go` (csvErrorSeverity=11, csvMessage=13)
- stderr format (`.log`): Use regex-based parsing from `log_collector.go`

---

## 2. Severity Level Mapping

### Decision: Map PostgreSQL severities to four display categories

### Rationale
PostgreSQL has many severity levels (DEBUG1-5, INFO, NOTICE, WARNING, ERROR, LOG, FATAL, PANIC). For TUI display, consolidate to four color-coded categories.

### Mapping
| PostgreSQL Severity | Display Category | Color |
|---------------------|------------------|-------|
| FATAL, PANIC, ERROR | ERROR | Red (lipgloss.Color("9")) |
| WARNING | WARNING | Yellow (lipgloss.Color("11")) |
| LOG, INFO, NOTICE | INFO | White (default) |
| DEBUG1-5 | DEBUG | Gray (lipgloss.Color("8")) |

### Alternatives Considered
1. **Show all PostgreSQL severities**: Rejected - too many colors, cluttered display
2. **Only ERROR/INFO**: Rejected - loses important WARNING distinction
3. **Configurable mapping**: Rejected over-engineering for this use case

---

## 3. Log Buffer Implementation

### Decision: Ring buffer with 10,000 entry capacity

### Rationale
The clarification session established 10,000 entries as the buffer limit (~5-10MB memory). A ring buffer provides O(1) insertion and automatic eviction of oldest entries.

### Implementation Pattern
```go
type LogBuffer struct {
    entries []LogEntry
    head    int
    size    int
    cap     int  // 10,000
}

func (b *LogBuffer) Add(entry LogEntry) {
    b.entries[b.head] = entry
    b.head = (b.head + 1) % b.cap
    if b.size < b.cap {
        b.size++
    }
}
```

### Alternatives Considered
1. **Unlimited buffer with LRU**: Rejected - memory unbounded
2. **Disk-backed virtual scroll**: Rejected - adds complexity, 10k entries sufficient
3. **Slice with re-allocation**: Rejected - GC pressure, worse performance

---

## 4. File Position Tracking

### Decision: Reuse existing PositionStore interface with SQLite persistence

### Rationale
The existing `log_collector.go` already defines `PositionStore` interface and SQLite implementation. Reuse prevents duplication and ensures consistent behavior across the application.

### Implementation Notes
- `GetLogPosition(ctx, filePath)` - retrieve last read position
- `SaveLogPosition(ctx, filePath, position)` - persist after each read cycle
- Handle file rotation: if file size < last position, reset to 0

---

## 5. Follow Mode Implementation

### Decision: Viewport auto-scroll with toggle state

### Rationale
Follow mode is the primary use case for real-time monitoring. The bubbles `viewport` component supports `GotoBottom()` for auto-scroll.

### Implementation Pattern
```go
type LogsView struct {
    followMode bool  // default: true
    viewport   viewport.Model
}

func (v *LogsView) Update(msg tea.Msg) {
    switch msg := msg.(type) {
    case LogEntriesMsg:
        v.appendEntries(msg.Entries)
        if v.followMode {
            v.viewport.GotoBottom()
        }
    case tea.KeyMsg:
        if msg.String() == "f" {
            v.followMode = !v.followMode
        }
    }
}
```

### Alternatives Considered
1. **Always follow (no toggle)**: Rejected - users need to scroll back to investigate
2. **Auto-disable on scroll**: Considered - may implement as enhancement (scroll up disables follow)

---

## 6. Search Implementation

### Decision: Go regexp with incremental highlighting

### Rationale
Go's `regexp` package is mature and sufficient for log search patterns. Pre-compile pattern once, apply to visible entries for highlighting.

### Implementation Notes
- Use `regexp.MustCompile()` with error handling for invalid patterns
- Highlight matches with lipgloss background color (e.g., dark yellow)
- Track match positions for `n`/`N` navigation
- Clear search on `Escape`

### Performance Consideration
- Only highlight visible entries (viewport window)
- Cache compiled regex until pattern changes

---

## 7. Reference Tool Analysis

### Tools Studied

1. **lnav** (log navigator)
   - Color-coded severity
   - SQL-like filtering
   - Follow mode with `F` key
   - Takeaway: Severity colors, follow mode UX

2. **htop** (process viewer)
   - Scrollable list with real-time updates
   - Color differentiation by state
   - Takeaway: Compact row format, status bar

3. **k9s** (Kubernetes TUI)
   - Log streaming view
   - Follow mode auto-scroll
   - Search with `/`
   - Takeaway: Keyboard patterns, search UX

### Visual Design Direction
- Single-line log entries (truncate long messages)
- Timestamp | Severity | PID | Message format
- Status bar showing: follow mode, filter, entry count
- Severity column with colored background for visibility

---

## 8. Logging Configuration Prompt

### Decision: Reuse ModeConfirmEnableLogging pattern

### Rationale
The queries view already has a working implementation for prompting users to enable logging. Reuse ensures consistent UX and reduces code duplication.

### Implementation Notes
From `internal/ui/views/queries/view.go`:
- Check `CheckLoggingStatus()` on view init
- If disabled and not readonly: show prompt
- If user accepts: call `EnableLogging()`, show toast
- If user declines: show configuration guidance

---

## Summary

| Research Area | Decision | Status |
|---------------|----------|--------|
| Log format parsing | Multi-format with auto-detection | ✅ Resolved |
| Severity mapping | 4 categories (ERROR/WARNING/INFO/DEBUG) | ✅ Resolved |
| Buffer implementation | Ring buffer, 10,000 entries | ✅ Resolved |
| Position tracking | Reuse PositionStore/SQLite | ✅ Resolved |
| Follow mode | Viewport auto-scroll with toggle | ✅ Resolved |
| Search | Go regexp with highlighting | ✅ Resolved |
| Visual design | lnav/htop/k9s inspired | ✅ Resolved |
| Config prompt | Reuse ModeConfirmEnableLogging | ✅ Resolved |

**All NEEDS CLARIFICATION items resolved. Ready for Phase 1.**
