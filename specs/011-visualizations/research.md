# Research: Advanced Visualizations

**Feature**: 011-visualizations
**Date**: 2025-11-29
**Updated**: 2025-11-30

## Summary

Research findings for implementing time-series graphs, sparklines, bar charts, and heatmaps in Steep. Key finding: **pterm** library provides production-ready bar charts and heatmaps, significantly reducing implementation effort.

---

## 1. Library Selection

### Decision Matrix

| Chart Type | Library | Why |
|------------|---------|-----|
| Time-series line graphs | **asciigraph** (existing) | Best-in-class for streaming line charts, already integrated |
| Sparklines | **existing sparkline.go** | Compact Unicode blocks (▁▂▃▄▅▆▇█), already built |
| Bar charts | **pterm** (NEW) | Production-ready BarChartPrinter with labels, values, colors |
| Heatmaps | **pterm** (NEW) | Production-ready HeatmapPrinter with RGB gradients, axes, legends |

### Libraries Added

**pterm** (github.com/pterm/pterm)
- Full-featured terminal output library
- `BarChartPrinter`: Horizontal/vertical bars, value labels, custom characters
- `HeatmapPrinter`: RGB color gradients, axis labels, legend, grid, boxed
- Already uses `mattn/go-runewidth` (we have this dependency)

### Libraries Kept

**asciigraph** (v0.7.3 - already installed)
- Multi-series line plots with 256 ANSI colors
- Height/width control, captions, bounds
- Used for Dashboard time-series graphs

**Existing sparkline.go**
- Unicode block sparklines (▁▂▃▄▅▆▇█)
- Severity coloring (green/yellow/red)
- Trend indicators (↑/→/↓)

### Libraries Considered But Not Used

| Library | Reason Skipped |
|---------|----------------|
| gammazero/deque | Simple ring buffer sufficient for our use case |
| VictoriaMetrics/fastcache | Wrong eviction model (LRU vs FIFO) |
| influxdata/tdigest | Nice-to-have; can add later for percentile stats |
| dgryski/go-tsz | Overkill for 7-day retention |
| aybabtme/uniplot | pterm covers our histogram/bar needs |
| jedib0t/go-pretty | Progress tracking separate from visualization |
| montanaflynn/stats | Simple mean/min/max inline is sufficient |

---

## 2. pterm Bar Charts

### Capabilities

```go
pterm.DefaultBarChart.
    WithBars(pterm.Bars{
        {Label: "SELECT * FROM...", Value: 42, Style: pterm.NewStyle(pterm.FgGreen)},
        {Label: "INSERT INTO...", Value: 21, Style: pterm.NewStyle(pterm.FgYellow)},
    }).
    WithHorizontal(true).
    WithShowValue(true).
    WithWidth(40).
    Srender()
```

### Features
- Horizontal and vertical orientation
- Value labels (optional)
- Per-bar colors/styles
- Automatic scaling to terminal width
- Negative value support

### Integration with Bubbletea
- `Srender()` returns string - perfect for View() methods
- No direct terminal I/O conflicts
- Thread-safe rendering

---

## 3. pterm Heatmaps

### Capabilities

```go
data := [][]float32{
    {0.9, 0.2, 0.7, 0.4},  // Hour 0-3
    {0.2, 0.7, 0.5, 0.3},  // Hour 4-7
    // ... 24 hours × 7 days
}

headerData := pterm.HeatmapAxis{
    XAxis: []string{"0", "3", "6", "9", "12", "15", "18", "21"},
    YAxis: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
}

pterm.DefaultHeatmap.
    WithAxisData(headerData).
    WithData(data).
    WithEnableRGB().
    WithRGBRange(
        pterm.RGB{R: 0, G: 100, B: 255, Background: true},    // Blue (low)
        pterm.RGB{R: 255, G: 200, B: 0, Background: true},    // Yellow (mid)
        pterm.RGB{R: 255, G: 0, B: 0, Background: true},      // Red (high)
    ).
    WithLegend(true).
    WithBoxed(true).
    Srender()
```

### Features
- RGB color gradients (continuous)
- Basic color palette (discrete)
- Axis labels (X and Y)
- Legend with value scale
- Grid lines and box borders
- Cell-only mode (no numbers, just colors)

### Output Example
```
┌──────┬───┬───┬───┬───┬───┬───┬───┬───┐
│      │ 0 │ 3 │ 6 │ 9 │12 │15 │18 │21 │
├──────┼───┼───┼───┼───┼───┼───┼───┼───┤
│ Mon  │░░░│░░░│▒▒▒│▓▓▓│███│▓▓▓│▒▒▒│░░░│
│ Tue  │░░░│░░░│▒▒▒│███│███│▓▓▓│▒▒▒│░░░│
│ ...  │   │   │   │   │   │   │   │   │
└──────┴───┴───┴───┴───┴───┴───┴───┴───┘
Legend│ 0.0 │ 0.5 │ 1.0 │
```

---

## 4. asciigraph (Unchanged)

### Decision
Continue using asciigraph for time-series line graphs on Dashboard.

### Rationale
- Already installed (v0.7.3)
- Best-in-class for streaming line charts
- Multi-series support with colors
- pterm doesn't have line graphs

### Usage Pattern
```go
data := []float64{10, 15, 12, 18, 22, 20, 25}

graph := asciigraph.Plot(data,
    asciigraph.Height(8),
    asciigraph.Width(60),
    asciigraph.Caption("TPS"),
    asciigraph.SeriesColors(asciigraph.Green),
)
```

---

## 5. Existing Sparkline Component (Unchanged)

### Decision
Continue using `internal/ui/components/sparkline.go` for inline table sparklines.

### Rationale
- Compact single-line format (8-15 chars)
- Unicode blocks (▁▂▃▄▅▆▇█) perfect for table columns
- Already has severity coloring and trend detection
- pterm's charts are full-panel, not inline

---

## 6. Circular Buffer Implementation

### Decision
Implement simple ring buffer - no external dependency needed.

### Rationale
- Use case is straightforward: append, evict oldest, get recent N
- gammazero/deque adds dependency for minimal benefit
- Go standard library sufficient

### Design
```go
type CircularBuffer struct {
    data     []DataPoint
    capacity int
    head     int
    size     int
    mu       sync.RWMutex
}
```

---

## 7. SQLite Persistence Schema (Unchanged)

### Schema
```sql
CREATE TABLE IF NOT EXISTS metrics_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    value REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_history_name_time
    ON metrics_history(metric_name, timestamp);
```

### Retention
7 days (per clarification session)

---

## 8. Time Window Aggregation (Unchanged)

| Window | Granularity | Data Points | Source |
|--------|-------------|-------------|--------|
| 1m | 1s | 60 | Memory buffer |
| 5m | 1s | 300 | Memory buffer |
| 15m | 10s | 90 | Memory + SQLite |
| 1h | 10s | 360 | SQLite |
| 24h | 1m | 1440 | SQLite (aggregated) |

---

## 9. Implementation Impact

### Reduced Scope (Thanks to pterm)

| Original Plan | New Plan |
|---------------|----------|
| Build custom `barchart.go` | Use `pterm.BarChartPrinter` |
| Build custom `heatmap.go` | Use `pterm.HeatmapPrinter` |
| Design bar chart data structures | Use `pterm.Bars` |
| Design heatmap rendering | Use `pterm.HeatmapData` |

### Estimated Effort Savings
- ~2-3 days saved on bar chart implementation
- ~3-4 days saved on heatmap implementation (P3)
- More robust output with pterm's battle-tested renderers

---

## 10. Reference Tool Analysis

### htop (sparklines reference)
- Uses Unicode blocks for CPU/memory bars
- Updates at 1s intervals
- Color-coded by usage level

### k9s (layout reference)
- Toggle panels with single key
- Consistent keyboard navigation
- Charts in header area, tables below

### pg_top (metrics reference)
- TPS and cache hit ratio in header
- Connection list dominates view
- No graphical trends

---

## Unresolved Questions

None. All technical decisions finalized.

---

## Next Steps

1. Add pterm dependency: `go get github.com/pterm/pterm`
2. Update data-model.md with pterm types
3. Proceed to task generation
