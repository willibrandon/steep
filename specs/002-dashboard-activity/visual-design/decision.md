# Visual Design Decision: Dashboard & Activity Monitoring

**Feature**: 002-dashboard-activity
**Date**: 2025-11-21

## Summary

After studying reference tools (pg_top, htop, k9s) and building three throwaway demos, the following visual design decisions have been made for Constitution VI compliance.

## Reference Tool Analysis

### pg_top
- **Studied**: `layout.h` (screen positions), `display.c` (render routines)
- **Key insight**: Fixed row positions for each metric type, compact single-line stats
- **Applicable to Steep**: Status bar at top, metrics in fixed position, table starts at consistent row

### htop
- **Studied**: `MeterMode.h`, `HeaderLayout.h`, `Meter.h`
- **Key insight**: Multiple meter display modes (bar, text, graph), flexible column layouts
- **Applicable to Steep**: 4-panel text mode layout, warning/critical color states

### k9s
- **Studied**: `internal/ui/key.go`
- **Key insight**: Full vim-style key definitions with shift variants
- **Applicable to Steep**: Vim navigation (hjkl, g/G), mnemonic action keys (d/c/x)

## Demo Screenshots Analysis

### Demo 1: Basic Table (`demo1.txt`)
- ✅ Column widths appropriate for content
- ✅ Selected row highlighting works
- ✅ State values display correctly
- ⚠️ Border rendering has some artifacts on left edge

**Decision**: Use bubbles/table as base component, adjust border styling

### Demo 2: Metrics Panel (`demo2.txt`)
- ✅ 4-panel layout fits 80-column terminal
- ✅ Labels and values centered correctly
- ✅ Rounded borders look clean
- ⚠️ Warning background color needs testing

**Decision**: Use rounded borders for metrics panels, normal borders for table

### Demo 3: Combined Layout (`demo3.txt`)
- ✅ All elements fit in 80x24 minimum
- ✅ Status bar displays connection info and timestamp
- ✅ Metrics panels show all 4 values
- ✅ Table shows 8 rows of activity data
- ✅ Footer shows hints and pagination
- ⚠️ Some border alignment issues at edges

**Decision**: This layout is approved as the base design. Minor border tweaks needed.

## Visual Acceptance Criteria

### MUST Have
1. **Minimum terminal**: 80x24 must display complete UI without clipping
2. **Status bar**: Connection string (host:port/db) left, timestamp right
3. **Metrics panels**: 4 equal-width panels in single row showing TPS, Cache Hit, Connections, DB Size
4. **Activity table**: PID, User, Database, State, Duration, Query columns
5. **Footer**: Keyboard hints left, pagination count right
6. **Row selection**: Visible highlight on selected row
7. **State colors**: Green=active, Yellow=idle in txn, Gray=idle

### SHOULD Have
1. **Warning highlight**: Cache hit < 90% shows yellow background
2. **Critical highlight**: Cache hit < 80% shows red background
3. **Query truncation**: Long queries end with "..." in table view
4. **Empty state**: Centered message when no connections

### Reference Comparison
- **Layout**: "Must feel like htop with pg_top data"
- **Navigation**: "Must navigate like k9s (vim-style)"
- **Information density**: "Must show same info as pg_top row 4 (TPS, hit%, etc.)"

## Color Palette

```go
var colors = struct {
    Active   lipgloss.Color // Green - active connections
    Idle     lipgloss.Color // Gray - idle connections
    IdleTxn  lipgloss.Color // Yellow - idle in transaction
    Aborted  lipgloss.Color // Orange - idle in txn (aborted)
    Warning  lipgloss.Color // Yellow bg - cache hit < 90%
    Critical lipgloss.Color // Red bg - cache hit < 80%
    Border   lipgloss.Color // Gray - all borders
    Accent   lipgloss.Color // Cyan - titles, highlights
    Muted    lipgloss.Color // Dark gray - secondary text
}{
    Active:   lipgloss.Color("10"),
    Idle:     lipgloss.Color("8"),
    IdleTxn:  lipgloss.Color("11"),
    Aborted:  lipgloss.Color("208"),
    Warning:  lipgloss.Color("11"),
    Critical: lipgloss.Color("9"),
    Border:   lipgloss.Color("240"),
    Accent:   lipgloss.Color("6"),
    Muted:    lipgloss.Color("8"),
}
```

## Keyboard Mapping

| Key | Action | Category |
|-----|--------|----------|
| j/↓ | Move down | Navigation |
| k/↑ | Move up | Navigation |
| g | Go to top | Navigation |
| G | Go to bottom | Navigation |
| PgUp/PgDn | Page up/down | Navigation |
| Enter/d | Show detail | Action |
| c | Cancel query | Action |
| x | Terminate connection | Action |
| r | Refresh | Action |
| / | Enter filter mode | Filter |
| s | Change sort | Filter |
| Esc | Clear/cancel | General |
| ? | Show help | General |
| q | Quit | General |

## Layout Specification

```
Row 0-2:   Status bar (border + content + border)
Row 3-6:   Metrics panel (border + label + value + border)
Row 7-N-3: Activity table (header + data rows)
Row N-2:   Footer (hints + pagination)
```

For 24-row terminal:
- Status bar: 3 rows
- Metrics: 4 rows
- Table: 14 rows (header + 13 data rows visible)
- Footer: 3 rows

## Next Steps

1. ✅ Visual design phase complete
2. Start T022-T029: Core Infrastructure (models, messages, styles)
3. Implement static mockup first with hardcoded data
4. Add real-time data after visual approval

## Files Created

- `mockup.txt` - ASCII mockup of final layout
- `reference-study.md` - Notes from studying pg_top, htop, k9s
- `demo1.txt` - Screenshot of basic table demo
- `demo2.txt` - Screenshot of metrics panel demo
- `demo3.txt` - Screenshot of combined layout demo
- `decision.md` - This document
