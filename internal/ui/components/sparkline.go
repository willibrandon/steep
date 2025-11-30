// Package components provides reusable UI components.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
)

// SparklineConfig holds configuration for sparkline rendering.
type SparklineConfig struct {
	// Width is the number of characters for the sparkline
	Width int
	// Height is the number of lines (1 for compact, 2+ for multi-line)
	Height int
	// Color is the lipgloss color for the sparkline
	Color lipgloss.Color
	// Caption is an optional label shown below the sparkline
	Caption string
	// Min is the minimum value for scaling (0 = auto)
	Min float64
	// Max is the maximum value for scaling (0 = auto)
	Max float64
}

// DefaultSparklineConfig returns sensible defaults for inline sparklines.
func DefaultSparklineConfig() SparklineConfig {
	return SparklineConfig{
		Width:  12,
		Height: 1,
		Color:  lipgloss.Color("117"), // Light blue
	}
}

// RenderSparkline renders a sparkline chart from data values.
// Uses asciigraph for multi-line charts and Unicode blocks for compact single-line.
func RenderSparkline(data []float64, config SparklineConfig) string {
	if len(data) == 0 {
		return strings.Repeat("─", config.Width)
	}

	if config.Height == 1 {
		// Use compact Unicode block sparkline for single line
		return RenderUnicodeSparkline(data, config)
	}

	// Use asciigraph for multi-line charts
	return RenderAsciigraphSparkline(data, config)
}

// RenderUnicodeSparkline renders a compact single-line sparkline using Unicode block characters.
// Uses characters: ▁▂▃▄▅▆▇█ to represent data values.
func RenderUnicodeSparkline(data []float64, config SparklineConfig) string {
	if len(data) == 0 {
		return strings.Repeat("─", config.Width)
	}

	// Unicode block characters from lowest to highest
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Find min and max for scaling
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// Use config overrides if set
	if config.Min != 0 || config.Max != 0 {
		if config.Min != 0 {
			minVal = config.Min
		}
		if config.Max != 0 {
			maxVal = config.Max
		}
	}

	// Avoid division by zero
	valueRange := maxVal - minVal
	if valueRange == 0 {
		valueRange = 1
	}

	// Resample data to fit width
	resampled := resampleData(data, config.Width)

	// Build sparkline
	var sb strings.Builder
	for _, v := range resampled {
		// Normalize to 0-7 range (8 block characters)
		normalized := (v - minVal) / valueRange
		idx := int(normalized * 7)
		if idx > 7 {
			idx = 7
		}
		if idx < 0 {
			idx = 0
		}
		sb.WriteRune(blocks[idx])
	}

	rawSparkline := sb.String()

	// Only apply color if one is set (skip for selected rows to preserve highlight)
	if config.Color != "" {
		style := lipgloss.NewStyle().Foreground(config.Color)
		return style.Render(rawSparkline)
	}
	return rawSparkline
}

// RenderAsciigraphSparkline renders a multi-line chart using asciigraph.
func RenderAsciigraphSparkline(data []float64, config SparklineConfig) string {
	if len(data) == 0 {
		return ""
	}

	// Resample if needed
	resampled := data
	if len(data) > config.Width {
		resampled = resampleData(data, config.Width)
	}

	// Configure asciigraph
	opts := []asciigraph.Option{
		asciigraph.Height(config.Height),
		asciigraph.Width(config.Width),
	}

	if config.Min != 0 {
		opts = append(opts, asciigraph.LowerBound(config.Min))
	}
	if config.Max != 0 {
		opts = append(opts, asciigraph.UpperBound(config.Max))
	}
	if config.Caption != "" {
		opts = append(opts, asciigraph.Caption(config.Caption))
	}

	// Render the graph
	graph := asciigraph.Plot(resampled, opts...)

	// Apply color styling
	style := lipgloss.NewStyle().Foreground(config.Color)
	lines := strings.Split(graph, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}

	return strings.Join(lines, "\n")
}

// RenderSparklineWithSeverity renders a sparkline with color based on current value severity.
// Uses green/yellow/red based on the latest value relative to thresholds.
func RenderSparklineWithSeverity(data []float64, config SparklineConfig, warningThreshold, criticalThreshold float64) string {
	if len(data) == 0 {
		return strings.Repeat("─", config.Width)
	}

	// Determine color based on latest value
	latestValue := data[len(data)-1]
	switch {
	case latestValue >= criticalThreshold:
		config.Color = lipgloss.Color("196") // Red
	case latestValue >= warningThreshold:
		config.Color = lipgloss.Color("214") // Yellow
	default:
		config.Color = lipgloss.Color("42") // Green
	}

	return RenderSparkline(data, config)
}

// resampleData resamples data to fit within the target width.
// Uses simple averaging for downsampling.
func resampleData(data []float64, targetWidth int) []float64 {
	if len(data) <= targetWidth {
		// Pad with zeros if needed, or return as-is
		return data
	}

	result := make([]float64, targetWidth)
	bucketSize := float64(len(data)) / float64(targetWidth)

	for i := 0; i < targetWidth; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > len(data) {
			end = len(data)
		}
		if start >= end {
			start = end - 1
		}
		if start < 0 {
			start = 0
		}

		// Average the values in this bucket
		sum := 0.0
		count := 0
		for j := start; j < end; j++ {
			sum += data[j]
			count++
		}
		if count > 0 {
			result[i] = sum / float64(count)
		}
	}

	return result
}

// SparklineTrend indicates the overall trend direction.
type SparklineTrend int

const (
	TrendStable SparklineTrend = iota
	TrendUp
	TrendDown
)

// GetTrend analyzes the data to determine overall trend.
func GetTrend(data []float64) SparklineTrend {
	if len(data) < 2 {
		return TrendStable
	}

	// Compare first third average with last third average
	third := len(data) / 3
	if third < 1 {
		third = 1
	}

	var firstSum, lastSum float64
	for i := 0; i < third; i++ {
		firstSum += data[i]
	}
	for i := len(data) - third; i < len(data); i++ {
		lastSum += data[i]
	}

	firstAvg := firstSum / float64(third)
	lastAvg := lastSum / float64(third)

	// Use 10% threshold for significance
	diff := lastAvg - firstAvg
	threshold := firstAvg * 0.1
	if threshold < 1 {
		threshold = 1
	}

	if diff > threshold {
		return TrendUp
	}
	if diff < -threshold {
		return TrendDown
	}
	return TrendStable
}

// TrendIndicator returns a Unicode arrow for the trend direction.
func TrendIndicator(trend SparklineTrend) string {
	switch trend {
	case TrendUp:
		return "↑"
	case TrendDown:
		return "↓"
	default:
		return "→"
	}
}
