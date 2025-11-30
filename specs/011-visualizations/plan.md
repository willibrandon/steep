# Implementation Plan: Advanced Visualizations

**Branch**: `011-visualizations` | **Date**: 2025-11-29 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/011-visualizations/spec.md`
**Updated**: 2025-11-30

## Summary

Add time-series graphs, sparklines, and bar charts for database metrics visualization. The P1 story adds time-series graphs (TPS, connections, cache hit ratio) to the Dashboard view with configurable time windows. P2 adds sparklines to Activity and Tables views, plus bar charts for comparative analysis. P3 adds heatmaps for temporal patterns.

**Key Library Decisions:**
- **asciigraph** (existing) for time-series line graphs
- **pterm** (NEW) for bar charts and heatmaps - battle-tested implementations
- **existing sparkline.go** for inline table sparklines

## Technical Context

**Language/Version**: Go 1.25.4
**Primary Dependencies**: bubbletea, bubbles, lipgloss, asciigraph (v0.7.3), pterm (NEW), pgx/pgxpool, go-sqlite3
**Storage**: SQLite (existing ~/.config/steep/steep.db), PostgreSQL (source metrics)
**Testing**: go test, testcontainers for integration tests
**Target Platform**: macOS, Linux terminals (256 color support)
**Project Type**: Single CLI application
**Performance Goals**: <50ms chart rendering, <500ms query execution, 60 FPS UI
**Constraints**: <10MB memory for visualization data, async persistence writes
**Scale/Scope**: 10,000 data points per metric in memory, 7-day SQLite retention

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Real-Time First | ✅ PASS | Graphs update with view refresh interval (1-5s); historical data supplementary |
| II. Keyboard-Driven | ✅ PASS | 'v' toggle, number keys for time windows, existing navigation preserved |
| III. Query Efficiency | ✅ PASS | Circular buffer limits (10K points), prepared statements, async persistence |
| IV. Incremental Delivery | ✅ PASS | P1 (time-series) → P2 (sparklines, bar charts) → P3 (heatmaps) |
| V. Comprehensive Coverage | ✅ PASS | Adds visual analytics to existing monitoring capabilities |
| VI. Visual Design First | ⚠️ PENDING | Visual mockups required before implementation |

**Gate Status**: PASS with VI pending (mockups required in Phase 1)

## Project Structure

### Documentation (this feature)

```text
specs/011-visualizations/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal interfaces)
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
internal/
├── storage/
│   └── sqlite/
│       ├── metrics_store.go     # NEW: Metrics time-series persistence
│       └── db.go                # MODIFY: Add metrics schema
├── ui/
│   ├── components/
│   │   ├── sparkline.go         # EXISTS: Extend for Activity/Tables
│   │   └── timeseries.go        # NEW: Time-series graph wrapper (asciigraph)
│   └── views/
│       ├── dashboard.go         # MODIFY: Add time-series graphs panel
│       ├── activity.go          # MODIFY: Add sparklines column
│       ├── tables/tabs.go       # MODIFY: Add sparklines column
│       ├── queries/tabs.go      # MODIFY: Add bar chart panel (pterm)
│       └── heatmap/             # NEW: Heatmap view (P3, uses pterm)
│           └── view.go
├── metrics/
│   ├── collector.go             # NEW: Metrics collection with circular buffer
│   └── buffer.go                # NEW: CircularBuffer implementation
└── db/
    └── models/
        └── metrics.go           # MODIFY: Add time-series data structures

tests/
├── integration/
│   └── metrics_store_test.go    # NEW: SQLite persistence tests
└── unit/
    ├── circular_buffer_test.go  # NEW: Buffer eviction tests
    └── timeseries_test.go       # NEW: Chart rendering tests
```

**Structure Decision**: Single project structure following existing patterns. Using pterm for bar charts (P2) and heatmaps (P3) instead of custom implementations - significantly reduces implementation effort while providing battle-tested renderers.

## Library Usage

### asciigraph (Time-Series Line Graphs)
```go
// Dashboard TPS graph
graph := asciigraph.Plot(tpsData,
    asciigraph.Height(8),
    asciigraph.Width(termWidth - 10),
    asciigraph.Caption("TPS (1h)"),
    asciigraph.SeriesColors(asciigraph.Green),
)
```

### pterm (Bar Charts - P2)
```go
// Top queries bar chart
chart, _ := pterm.DefaultBarChart.
    WithBars(pterm.Bars{
        {Label: truncateQuery(q1), Value: int(q1.TotalTime), Style: pterm.NewStyle(pterm.FgRed)},
        {Label: truncateQuery(q2), Value: int(q2.TotalTime), Style: pterm.NewStyle(pterm.FgYellow)},
        // ... top 10
    }).
    WithHorizontal(true).
    WithShowValue(true).
    WithWidth(40).
    Srender()
```

### pterm (Heatmaps - P3)
```go
// Query volume heatmap
heatmap, _ := pterm.DefaultHeatmap.
    WithAxisData(pterm.HeatmapAxis{
        XAxis: []string{"0", "3", "6", "9", "12", "15", "18", "21"},
        YAxis: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
    }).
    WithData(queryVolumeData).
    WithEnableRGB().
    WithLegend(true).
    WithBoxed(true).
    Srender()
```

### Existing Sparkline (Inline Trends)
```go
// Activity view connection sparkline
sparkline := components.RenderSparklineWithSeverity(
    durationHistory,
    components.SparklineConfig{Width: 12, Height: 1},
    warningMs, criticalMs,
)
```

## Complexity Tracking

No violations requiring justification. Feature uses existing patterns and leverages external libraries:
- Sparkline component already exists (extend, don't replace)
- SQLite storage pattern from replication_store.go
- asciigraph library already in go.mod
- **pterm** provides bar charts and heatmaps - eliminates custom component development

## Implementation Notes

### What We Build
- `internal/metrics/collector.go` - Metrics collection goroutine
- `internal/metrics/buffer.go` - CircularBuffer with 10K limit
- `internal/storage/sqlite/metrics_store.go` - SQLite persistence
- `internal/ui/components/timeseries.go` - asciigraph wrapper with config
- Dashboard time-series panel integration
- Activity/Tables sparkline column integration
- Queries bar chart panel (wraps pterm)
- Heatmap view (wraps pterm)

### What pterm Provides
- `pterm.BarChartPrinter` - Horizontal/vertical bars, values, colors
- `pterm.HeatmapPrinter` - RGB gradients, axes, legend, grid
- Thread-safe `Srender()` for Bubbletea integration
- Terminal width detection (via `pterm.GetTerminalWidth()`)

### Integration Pattern
All pterm renderers return strings via `Srender()`, making them perfect for Bubbletea's `View()` method:

```go
func (m Model) View() string {
    // pterm renders to string, not stdout
    chart, _ := pterm.DefaultBarChart.WithBars(m.bars).Srender()
    return lipgloss.JoinVertical(lipgloss.Left, m.header, chart, m.footer)
}
```
