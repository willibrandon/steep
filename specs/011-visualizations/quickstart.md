# Quickstart: Advanced Visualizations

**Feature**: 011-visualizations
**Date**: 2025-11-29
**Updated**: 2025-11-30

## Prerequisites

1. Go 1.21+ installed
2. PostgreSQL database running and accessible
3. Steep built and functional (`make build`)

## Verification Steps

### Step 1: Verify Existing Dependencies

```bash
# Check asciigraph is in go.mod
grep asciigraph go.mod
# Expected: github.com/guptarohit/asciigraph v0.7.3

# Check mattn/go-runewidth (shared with pterm)
grep go-runewidth go.mod
# Expected: github.com/mattn/go-runewidth
```

### Step 2: Add pterm Dependency

```bash
# Add pterm for bar charts and heatmaps
go get github.com/pterm/pterm

# Verify it was added
grep pterm go.mod
# Expected: github.com/pterm/pterm vX.X.X

# Tidy dependencies
go mod tidy
```

### Step 3: Verify Existing Sparkline Component

```bash
# Confirm sparkline.go exists with asciigraph import
head -20 internal/ui/components/sparkline.go
# Expected: import containing "github.com/guptarohit/asciigraph"
```

### Step 4: Verify SQLite Database

```bash
# Check SQLite database exists
ls -la ~/.config/steep/steep.db
# Expected: file exists (created by previous features)

# Verify schema access
sqlite3 ~/.config/steep/steep.db ".schema" | head -20
```

### Step 5: Run Existing Tests

```bash
# Run all tests to ensure baseline functionality
make test

# Run just UI component tests
go test ./internal/ui/components/... -v
```

### Step 6: Build Application

```bash
make build
./bin/steep --help
```

## Development Setup

### 1. Create Feature Branch (already done by /speckit.specify)

```bash
git checkout 011-visualizations
git status
```

### 2. Create Required Directories

```bash
# Metrics collector package
mkdir -p internal/metrics

# Heatmap view (P3)
mkdir -p internal/ui/views/heatmap
```

### 3: Test pterm Integration

```bash
# Quick pterm smoke test
cat > /tmp/pterm_test.go << 'EOF'
package main

import "github.com/pterm/pterm"

func main() {
    // Bar chart test
    bars := pterm.Bars{
        {Label: "Query 1", Value: 100},
        {Label: "Query 2", Value: 50},
    }
    chart, _ := pterm.DefaultBarChart.
        WithBars(bars).
        WithHorizontal(true).
        WithShowValue(true).
        Srender()
    println("Bar Chart:")
    println(chart)

    // Heatmap test
    data := [][]float32{{0.1, 0.5, 0.9}, {0.3, 0.7, 0.2}}
    axis := pterm.HeatmapAxis{XAxis: []string{"A", "B", "C"}, YAxis: []string{"1", "2"}}
    heatmap, _ := pterm.DefaultHeatmap.
        WithAxisData(axis).
        WithData(data).
        WithEnableRGB().
        Srender()
    println("\nHeatmap:")
    println(heatmap)
}
EOF
go run /tmp/pterm_test.go
rm /tmp/pterm_test.go
```

### 4. Verify Development Environment

```bash
# Check Go version
go version
# Expected: go1.21 or higher

# Verify module dependencies
go mod tidy
go mod verify
```

## Smoke Test After Implementation

### Time-Series Graphs (P1)

1. Start Steep: `./bin/steep`
2. Navigate to Dashboard (key: `1`)
3. Verify TPS, Connections, and Cache Hit Ratio graphs appear
4. Wait 60 seconds and verify graphs update with new data
5. Press `1`-`5` to change time windows
6. Press `v` to toggle charts off/on

### Sparklines (P2)

1. Navigate to Activity view (key: `2`)
2. Verify sparkline column appears for active connections
3. Navigate to Tables view (key: `5`)
4. Verify sparkline column shows table size trends

### Bar Charts (P2)

1. Navigate to Queries view (key: `3`)
2. Verify bar chart shows top 10 queries by execution time
3. Use `j`/`k` to navigate bars (if interactive)

### Heatmap (P3)

1. Run Steep for several hours to collect data
2. Access heatmap panel (implementation-specific keybinding)
3. Verify 24×7 grid shows query volume patterns
4. Verify RGB color gradient from blue (low) to red (high)

## Performance Verification

```bash
# Run benchmarks after implementation
go test -bench=. ./internal/ui/components/... -benchmem
go test -bench=. ./internal/metrics/... -benchmem

# Expected results:
# BenchmarkSparkline-8     50000     25000 ns/op    < 50ms
# BenchmarkTimeSeries-8    20000     45000 ns/op    < 50ms
# BenchmarkBarChart-8      30000     35000 ns/op    < 50ms
# BenchmarkCircularBuffer  100000    10000 ns/op    < 1ms
```

## Troubleshooting

### Charts Not Appearing
- Verify terminal supports 256 colors: `echo $TERM`
- Check terminal size: minimum 80×24
- Verify charts enabled: press `v` to toggle

### pterm Colors Wrong
- Ensure terminal supports RGB: `echo -e "\033[38;2;255;0;0mRed\033[0m"`
- Try `TERM=xterm-256color` if colors are off

### Data Not Persisting
- Check SQLite write permissions: `ls -la ~/.config/steep/`
- Verify disk space available
- Check logs for persistence errors

### Performance Issues
- Profile render time: add timing around chart rendering
- Check buffer sizes: should not exceed 10,000 points
- Verify async persistence: writes should not block UI

## Library Reference

| Library | Usage | Documentation |
|---------|-------|---------------|
| asciigraph | Time-series line graphs | https://github.com/guptarohit/asciigraph |
| pterm | Bar charts, heatmaps | https://github.com/pterm/pterm |
| existing sparkline.go | Inline sparklines | internal/ui/components/sparkline.go |

## Related Files

- Spec: `specs/011-visualizations/spec.md`
- Plan: `specs/011-visualizations/plan.md`
- Research: `specs/011-visualizations/research.md`
- Data Model: `specs/011-visualizations/data-model.md`
- Contracts: `specs/011-visualizations/contracts/`
