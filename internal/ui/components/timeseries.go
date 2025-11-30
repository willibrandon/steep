// Package components provides reusable UI components.
package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"

	"github.com/willibrandon/steep/internal/metrics"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// TimeSeriesChart renders a time-series line graph using asciigraph.
type TimeSeriesChart struct {
	title  string
	width  int
	height int
	color  asciigraph.AnsiColor
	data   []float64
	window metrics.TimeWindow

	// Data requirements
	minPoints int
}

// TimeSeriesConfig configures a TimeSeriesChart.
type TimeSeriesConfig struct {
	Title     string
	Width     int
	Height    int
	Color     asciigraph.AnsiColor
	Window    metrics.TimeWindow
	MinPoints int
}

// DefaultTimeSeriesConfig returns sensible defaults.
func DefaultTimeSeriesConfig() TimeSeriesConfig {
	return TimeSeriesConfig{
		Title:     "",
		Width:     80,
		Height:    8,
		Color:     asciigraph.Green,
		Window:    metrics.TimeWindow1h,
		MinPoints: 3,
	}
}

// NewTimeSeriesChart creates a new time-series chart component.
func NewTimeSeriesChart(config TimeSeriesConfig) *TimeSeriesChart {
	if config.MinPoints < 1 {
		config.MinPoints = 3
	}
	if config.Height < 3 {
		config.Height = 3
	}
	if config.Width < 20 {
		config.Width = 20
	}

	return &TimeSeriesChart{
		title:     config.Title,
		width:     config.Width,
		height:    config.Height,
		color:     config.Color,
		window:    config.Window,
		minPoints: config.MinPoints,
	}
}

// SetData updates the chart data.
func (c *TimeSeriesChart) SetData(data []float64) {
	c.data = data
}

// SetWindow updates the time window.
func (c *TimeSeriesChart) SetWindow(window metrics.TimeWindow) {
	c.window = window
}

// SetSize updates the chart dimensions.
func (c *TimeSeriesChart) SetSize(width, height int) {
	if width >= 20 {
		c.width = width
	}
	if height >= 3 {
		c.height = height
	}
}

// SetTitle updates the chart title.
func (c *TimeSeriesChart) SetTitle(title string) {
	c.title = title
}

// HasSufficientData returns true if there's enough data to render a meaningful chart.
func (c *TimeSeriesChart) HasSufficientData() bool {
	return len(c.data) >= c.minPoints
}

// View renders the time-series chart.
func (c *TimeSeriesChart) View() string {
	caption := c.buildCaption()

	if !c.HasSufficientData() {
		return c.renderCollectingData(caption)
	}

	return c.renderGraph(caption)
}

// buildCaption creates the chart caption with title and time window.
func (c *TimeSeriesChart) buildCaption() string {
	if c.title == "" {
		return c.window.String()
	}
	return fmt.Sprintf("%s (%s)", c.title, c.window.String())
}

// renderCollectingData shows the "Collecting data..." placeholder.
func (c *TimeSeriesChart) renderCollectingData(caption string) string {
	expected := c.expectedPoints()
	current := len(c.data)

	// Simple centered message
	msg := fmt.Sprintf("Collecting data... (%d/%d points)", current, expected)

	contentStyle := lipgloss.NewStyle().
		Width(c.width - 4).
		Height(c.height - 2).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(styles.ColorMuted)

	content := contentStyle.Render(msg)

	// Add title header
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Bold(true)
	header := titleStyle.Render(caption)

	return lipgloss.JoinVertical(lipgloss.Left, header, content)
}

// renderGraph renders the actual asciigraph chart.
func (c *TimeSeriesChart) renderGraph(caption string) string {
	data := c.resampleData()

	// Graph dimensions - leave room for Y-axis labels
	graphWidth := c.width - 10
	if graphWidth < 20 {
		graphWidth = 20
	}

	graphHeight := c.height - 2 // Leave room for caption line
	if graphHeight < 2 {
		graphHeight = 2
	}

	// Build asciigraph with caption
	opts := []asciigraph.Option{
		asciigraph.Height(graphHeight),
		asciigraph.Width(graphWidth),
		asciigraph.Caption(caption),
	}

	// Add color
	switch c.color {
	case asciigraph.Green:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Green))
	case asciigraph.Blue:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Blue))
	case asciigraph.Cyan:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Cyan))
	case asciigraph.Yellow:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Yellow))
	case asciigraph.Red:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Red))
	default:
		opts = append(opts, asciigraph.SeriesColors(asciigraph.Default))
	}

	graph := asciigraph.Plot(data, opts...)

	return strings.TrimRight(graph, "\n")
}

// resampleData resamples data to fit within the graph width.
func (c *TimeSeriesChart) resampleData() []float64 {
	graphWidth := c.width - 10
	if graphWidth < 20 {
		graphWidth = 20
	}

	if len(c.data) <= graphWidth {
		return c.data
	}

	// Downsample using averaging
	result := make([]float64, graphWidth)
	bucketSize := float64(len(c.data)) / float64(graphWidth)

	for i := 0; i < graphWidth; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > len(c.data) {
			end = len(c.data)
		}
		if start >= end {
			start = end - 1
		}
		if start < 0 {
			start = 0
		}

		sum := 0.0
		count := 0
		for j := start; j < end; j++ {
			sum += c.data[j]
			count++
		}
		if count > 0 {
			result[i] = sum / float64(count)
		}
	}

	return result
}

// expectedPoints returns the expected number of data points for the current time window.
func (c *TimeSeriesChart) expectedPoints() int {
	switch c.window {
	case metrics.TimeWindow1m:
		return 60
	case metrics.TimeWindow5m:
		return 300
	case metrics.TimeWindow15m:
		return 90
	case metrics.TimeWindow1h:
		return 360
	case metrics.TimeWindow24h:
		return 1440
	default:
		return 60
	}
}

// TimeSeriesPanel manages multiple time-series charts for the dashboard.
type TimeSeriesPanel struct {
	width  int
	height int

	tpsChart         *TimeSeriesChart
	connectionsChart *TimeSeriesChart
	cacheHitChart    *TimeSeriesChart

	window metrics.TimeWindow
}

// NewTimeSeriesPanel creates a panel containing TPS, Connections, and Cache Hit charts.
func NewTimeSeriesPanel() *TimeSeriesPanel {
	tpsConfig := DefaultTimeSeriesConfig()
	tpsConfig.Title = "TPS"
	tpsConfig.Color = asciigraph.Green

	connConfig := DefaultTimeSeriesConfig()
	connConfig.Title = "Connections"
	connConfig.Color = asciigraph.Blue

	cacheConfig := DefaultTimeSeriesConfig()
	cacheConfig.Title = "Cache Hit %"
	cacheConfig.Color = asciigraph.Cyan

	return &TimeSeriesPanel{
		tpsChart:         NewTimeSeriesChart(tpsConfig),
		connectionsChart: NewTimeSeriesChart(connConfig),
		cacheHitChart:    NewTimeSeriesChart(cacheConfig),
		window:           metrics.TimeWindow1h,
	}
}

// SetSize sets the dimensions of the panel.
func (p *TimeSeriesPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.updateChartSizes()
}

// updateChartSizes recalculates individual chart sizes based on panel dimensions.
func (p *TimeSeriesPanel) updateChartSizes() {
	// Each chart gets 1/3 of the height, last chart gets remainder
	chartHeight := p.height / 3
	if chartHeight < 4 {
		chartHeight = 4
	}
	lastChartHeight := p.height - (chartHeight * 2) // Give remainder to last chart

	chartWidth := p.width
	if chartWidth < 40 {
		chartWidth = 40
	}

	p.tpsChart.SetSize(chartWidth, chartHeight)
	p.connectionsChart.SetSize(chartWidth, chartHeight)
	p.cacheHitChart.SetSize(chartWidth, lastChartHeight)
}

// SetWindow sets the time window for all charts.
func (p *TimeSeriesPanel) SetWindow(window metrics.TimeWindow) {
	p.window = window
	p.tpsChart.SetWindow(window)
	p.connectionsChart.SetWindow(window)
	p.cacheHitChart.SetWindow(window)
}

// GetWindow returns the current time window.
func (p *TimeSeriesPanel) GetWindow() metrics.TimeWindow {
	return p.window
}

// SetTPSData sets the TPS chart data.
func (p *TimeSeriesPanel) SetTPSData(data []float64) {
	p.tpsChart.SetData(data)
}

// SetConnectionsData sets the Connections chart data.
func (p *TimeSeriesPanel) SetConnectionsData(data []float64) {
	p.connectionsChart.SetData(data)
}

// SetCacheHitData sets the Cache Hit Ratio chart data.
func (p *TimeSeriesPanel) SetCacheHitData(data []float64) {
	p.cacheHitChart.SetData(data)
}

// View renders all three charts stacked vertically.
func (p *TimeSeriesPanel) View() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		p.tpsChart.View(),
		p.connectionsChart.View(),
		p.cacheHitChart.View(),
	)
}
