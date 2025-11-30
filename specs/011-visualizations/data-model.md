# Data Model: Advanced Visualizations

**Feature**: 011-visualizations
**Date**: 2025-11-29
**Updated**: 2025-11-30

## Overview

This document defines the data structures for time-series metrics collection, storage, and visualization rendering. Bar charts and heatmaps use pterm's native types.

---

## Core Entities (Custom)

### 1. MetricSeries

A named collection of time-series data points for a single metric.

```go
// MetricSeries represents a time-series of measurements for a single metric.
type MetricSeries struct {
    Name       string          // Metric identifier (e.g., "tps", "connections", "cache_hit_ratio")
    Buffer     *CircularBuffer // In-memory ring buffer of data points
    LastUpdate time.Time       // Timestamp of most recent data point
}
```

**Attributes**:
| Field | Type | Description |
|-------|------|-------------|
| Name | string | Unique metric identifier |
| Buffer | *CircularBuffer | In-memory ring buffer of data points |
| LastUpdate | time.Time | Timestamp of most recent data point |

**Validation Rules**:
- Name must be non-empty and match `^[a-z_]+$`
- Buffer capacity defaults to 10,000
- LastUpdate updated on every Push()

---

### 2. DataPoint

A single measurement with timestamp.

```go
// DataPoint represents a single metric measurement at a point in time.
type DataPoint struct {
    Timestamp time.Time
    Value     float64
}
```

**Attributes**:
| Field | Type | Description |
|-------|------|-------------|
| Timestamp | time.Time | When measurement was taken |
| Value | float64 | Measured value |

**Validation Rules**:
- Value must not be Inf or NaN (filter before storage)
- Timestamp must be valid (not zero)

---

### 3. CircularBuffer

Ring buffer for efficient time-series storage with automatic eviction.

```go
// CircularBuffer is a fixed-size ring buffer for DataPoints.
type CircularBuffer struct {
    data       []DataPoint
    capacity   int
    head       int  // Next write position
    size       int  // Current element count
    mu         sync.RWMutex
}
```

**Attributes**:
| Field | Type | Description |
|-------|------|-------------|
| data | []DataPoint | Underlying storage |
| capacity | int | Maximum elements (10,000 default) |
| head | int | Next write index |
| size | int | Current count (0 to capacity) |

**Operations**:
| Method | Description |
|--------|-------------|
| Push(dp DataPoint) | Add new point, evict oldest if full |
| GetRecent(n int) []DataPoint | Get n most recent points |
| GetSince(t time.Time) []DataPoint | Get points since timestamp |
| GetValues() []float64 | Get all values as float64 slice (for asciigraph) |
| Len() int | Current element count |
| Clear() | Reset buffer |

**State Transitions**:
```
Empty → Partial → Full → Full (overwrite oldest)
```

---

### 4. TimeWindow

Configurable time range for chart data.

```go
// TimeWindow represents a configurable time range for chart data.
type TimeWindow int

const (
    TimeWindow1m TimeWindow = iota
    TimeWindow5m
    TimeWindow15m
    TimeWindow1h
    TimeWindow24h
)

// Duration returns the time.Duration for the window.
func (tw TimeWindow) Duration() time.Duration

// Granularity returns the data point interval for this window.
func (tw TimeWindow) Granularity() time.Duration

// String returns a display label like "1m", "5m", etc.
func (tw TimeWindow) String() string
```

| Window | Duration | Granularity | Source |
|--------|----------|-------------|--------|
| 1m | 1 minute | 1s | Memory |
| 5m | 5 minutes | 1s | Memory |
| 15m | 15 minutes | 10s | Memory + SQLite |
| 1h | 1 hour | 10s | SQLite |
| 24h | 24 hours | 1m | SQLite |

---

## pterm Types (External)

### Bar Charts

pterm provides `pterm.Bars` and `pterm.Bar` for bar chart data:

```go
// pterm.Bar - single bar in a chart
type Bar struct {
    Label      string
    Value      int
    Style      *Style
    LabelStyle *Style
}

// pterm.Bars - collection of bars
type Bars []Bar
```

**Usage in Steep**:
```go
// Convert query stats to pterm bars
func QueryStatsToBars(stats []QueryStat, limit int) pterm.Bars {
    bars := make(pterm.Bars, 0, limit)
    for i, s := range stats {
        if i >= limit {
            break
        }
        bars = append(bars, pterm.Bar{
            Label: truncateQuery(s.Query, 30),
            Value: int(s.TotalTimeMs),
            Style: colorByRank(i),
        })
    }
    return bars
}
```

---

### Heatmaps

pterm provides `pterm.HeatmapData` and `pterm.HeatmapAxis`:

```go
// pterm.HeatmapData - 2D grid of float32 values
type HeatmapData [][]float32

// pterm.HeatmapAxis - axis labels
type HeatmapAxis struct {
    XAxis []string
    YAxis []string
}
```

**Usage in Steep**:
```go
// Convert query volume to heatmap data
func QueryVolumeToHeatmap(history []HourlyVolume) (pterm.HeatmapData, pterm.HeatmapAxis) {
    // 7 days × 24 hours grid
    data := make([][]float32, 7)
    for i := range data {
        data[i] = make([]float32, 24)
    }

    for _, h := range history {
        day := int(h.Timestamp.Weekday())
        hour := h.Timestamp.Hour()
        data[day][hour] = float32(h.QueryCount)
    }

    axis := pterm.HeatmapAxis{
        XAxis: []string{"0", "3", "6", "9", "12", "15", "18", "21"},
        YAxis: []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
    }

    return data, axis
}
```

---

## SQLite Schema

### metrics_history Table

```sql
CREATE TABLE IF NOT EXISTS metrics_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,        -- ISO 8601 format
    metric_name TEXT NOT NULL,      -- e.g., "tps", "connections"
    value REAL NOT NULL
);

-- Index for efficient time-range queries per metric
CREATE INDEX IF NOT EXISTS idx_metrics_history_name_time
    ON metrics_history(metric_name, timestamp);

-- Index for time-based cleanup
CREATE INDEX IF NOT EXISTS idx_metrics_history_timestamp
    ON metrics_history(timestamp);
```

**Columns**:
| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| id | INTEGER | NO | Auto-increment primary key |
| timestamp | TEXT | NO | ISO 8601 timestamp |
| metric_name | TEXT | NO | Metric identifier |
| value | REAL | NO | Measured value |

**Retention**: 7 days (pruned by cleanup job)

---

## Entity Relationships

```
┌─────────────────┐
│ MetricsCollector│
│                 │
│ - series: map   │───────────┬────────────────────┐
│ - store: *Store │           │                    │
└─────────────────┘           ▼                    ▼
                    ┌─────────────────┐   ┌─────────────────┐
                    │  MetricSeries   │   │  MetricsStore   │
                    │                 │   │                 │
                    │ - Buffer        │   │ - SQLite DB     │
                    └────────┬────────┘   └─────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │ CircularBuffer  │
                    │                 │
                    │ - DataPoints[]  │
                    └─────────────────┘
```

---

## Data Flow

### Collection Flow
```
PostgreSQL → Metrics Query → MetricsCollector
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
              CircularBuffer  CircularBuffer  CircularBuffer
                 (TPS)       (Connections)   (CacheHitRatio)
                    │               │               │
                    └───────────────┼───────────────┘
                                    ▼
                             MetricsStore
                              (SQLite)
```

### Rendering Flow
```
TimeWindow Selection → MetricsCollector.GetData()
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
    Short Window         Long Window          Merge Data
    (Buffer only)      (SQLite query)      (if needed)
         │                    │                    │
         └────────────────────┼────────────────────┘
                              │
            ┌─────────────────┼─────────────────┐
            │                 │                 │
            ▼                 ▼                 ▼
      asciigraph.Plot   pterm.BarChart   pterm.Heatmap
      (time-series)     (comparisons)    (patterns)
```

---

## Memory Estimates

| Component | Size | Notes |
|-----------|------|-------|
| DataPoint | 24 bytes | time.Time (24) + float64 (8) with padding |
| CircularBuffer (10K) | ~240 KB | 10,000 × 24 bytes |
| 3 MetricSeries | ~720 KB | TPS, Connections, CacheHitRatio |
| Connection sparklines (100) | ~2.4 MB | 100 connections × 100 points each |
| Total estimate | <5 MB | Well under 10MB limit |

---

## Type Conversions

### CircularBuffer → asciigraph
```go
// GetValues returns float64 slice for asciigraph.Plot()
func (b *CircularBuffer) GetValues() []float64 {
    b.mu.RLock()
    defer b.mu.RUnlock()

    result := make([]float64, b.size)
    for i := 0; i < b.size; i++ {
        idx := (b.head - b.size + i + b.capacity) % b.capacity
        result[i] = b.data[idx].Value
    }
    return result
}
```

### QueryStats → pterm.Bars
```go
// See pterm Types section above
```

### HourlyVolume → pterm.HeatmapData
```go
// See pterm Types section above
```
