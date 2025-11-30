// Package contracts defines interfaces for the visualization feature.
// This file is a design artifact - reference for pterm integration patterns.
package contracts

import (
	"github.com/guptarohit/asciigraph"
	"github.com/pterm/pterm"
)

// =============================================================================
// Time-Series Graphs (asciigraph)
// =============================================================================

// TimeSeriesConfig holds configuration for time-series line graphs.
type TimeSeriesConfig struct {
	Width   int    // Character width (0 = auto)
	Height  int    // Line height (default 8)
	Caption string // Label below graph
	Color   asciigraph.AnsiColor
}

// DefaultTimeSeriesConfig returns sensible defaults for Dashboard graphs.
func DefaultTimeSeriesConfig() TimeSeriesConfig {
	return TimeSeriesConfig{
		Width:  60,
		Height: 8,
		Color:  asciigraph.Green,
	}
}

// RenderTimeSeries renders a time-series line graph using asciigraph.
// Returns string suitable for Bubbletea View().
func RenderTimeSeries(data []float64, config TimeSeriesConfig) string {
	opts := []asciigraph.Option{
		asciigraph.Height(config.Height),
		asciigraph.SeriesColors(config.Color),
	}
	if config.Width > 0 {
		opts = append(opts, asciigraph.Width(config.Width))
	}
	if config.Caption != "" {
		opts = append(opts, asciigraph.Caption(config.Caption))
	}
	return asciigraph.Plot(data, opts...)
}

// =============================================================================
// Sparklines (existing component)
// =============================================================================

// SparklineConfig is defined in internal/ui/components/sparkline.go
// Re-exported here for reference.
//
// type SparklineConfig struct {
//     Width   int
//     Height  int
//     Color   lipgloss.Color
//     Caption string
//     Min     float64
//     Max     float64
// }

// =============================================================================
// Bar Charts (pterm)
// =============================================================================

// BarChartConfig holds configuration for horizontal bar charts.
type BarChartConfig struct {
	Width      int  // Character width (0 = terminal width * 2/3)
	ShowValue  bool // Show numeric value at end of bar
	Horizontal bool // Horizontal orientation (default true for Steep)
}

// DefaultBarChartConfig returns sensible defaults for Queries view.
func DefaultBarChartConfig() BarChartConfig {
	return BarChartConfig{
		Width:      40,
		ShowValue:  true,
		Horizontal: true,
	}
}

// RenderBarChart renders a horizontal bar chart using pterm.
// Returns string suitable for Bubbletea View().
func RenderBarChart(bars pterm.Bars, config BarChartConfig) (string, error) {
	printer := pterm.DefaultBarChart.
		WithBars(bars).
		WithHorizontal(config.Horizontal).
		WithShowValue(config.ShowValue)

	if config.Width > 0 {
		printer = printer.WithWidth(config.Width)
	}

	return printer.Srender()
}

// QueryStatToBar converts a query statistic to a pterm Bar.
// Helper for building bar chart data from query performance stats.
func QueryStatToBar(label string, value int, rank int) pterm.Bar {
	// Color by rank: top 1-3 red, 4-6 yellow, 7+ green
	var style *pterm.Style
	switch {
	case rank < 3:
		style = pterm.NewStyle(pterm.FgRed)
	case rank < 6:
		style = pterm.NewStyle(pterm.FgYellow)
	default:
		style = pterm.NewStyle(pterm.FgGreen)
	}

	return pterm.Bar{
		Label: label,
		Value: value,
		Style: style,
	}
}

// =============================================================================
// Heatmaps (pterm)
// =============================================================================

// HeatmapConfig holds configuration for hour/day heatmaps.
type HeatmapConfig struct {
	EnableRGB bool // Use RGB gradient vs discrete colors
	ShowGrid  bool // Show grid lines
	ShowBox   bool // Show box border
	Legend    bool // Show legend
}

// DefaultHeatmapConfig returns sensible defaults for query volume heatmaps.
func DefaultHeatmapConfig() HeatmapConfig {
	return HeatmapConfig{
		EnableRGB: true,
		ShowGrid:  true,
		ShowBox:   true,
		Legend:    true,
	}
}

// RenderHeatmap renders a heatmap using pterm.
// Returns string suitable for Bubbletea View().
func RenderHeatmap(data pterm.HeatmapData, axis pterm.HeatmapAxis, config HeatmapConfig) (string, error) {
	printer := pterm.DefaultHeatmap.
		WithAxisData(axis).
		WithData(data).
		WithEnableRGB(config.EnableRGB).
		WithGrid(config.ShowGrid).
		WithBoxed(config.ShowBox).
		WithLegend(config.Legend)

	if config.EnableRGB {
		// Blue (low) -> Yellow (mid) -> Red (high) gradient
		printer = printer.WithRGBRange(
			pterm.RGB{R: 0, G: 100, B: 255, Background: true},
			pterm.RGB{R: 255, G: 200, B: 0, Background: true},
			pterm.RGB{R: 255, G: 0, B: 0, Background: true},
		)
	}

	return printer.Srender()
}

// BuildHeatmapAxis returns standard hour/day axis labels.
func BuildHeatmapAxis() pterm.HeatmapAxis {
	return pterm.HeatmapAxis{
		XAxis: []string{"0", "3", "6", "9", "12", "15", "18", "21"},
		YAxis: []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	}
}

// =============================================================================
// Chart Toggle State
// =============================================================================

// ChartToggleState tracks visibility of charts across views.
type ChartToggleState struct {
	Enabled bool // Global chart visibility toggle
}

// Toggle flips the chart visibility state.
func (s *ChartToggleState) Toggle() {
	s.Enabled = !s.Enabled
}
