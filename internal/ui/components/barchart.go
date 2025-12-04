// Package components provides reusable UI components.
package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pterm/pterm"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// BarChartItem represents a single bar in the chart.
type BarChartItem struct {
	Label string  // Display label (will be truncated if needed)
	Value float64 // Numeric value for the bar
	Rank  int     // 1-based rank for color coding (1=top, 10=bottom)
}

// BarChartConfig configures a BarChart.
type BarChartConfig struct {
	Title          string // Chart title
	Width          int    // Total width available
	Height         int    // Total height available (number of bars)
	MaxLabelWidth  int    // Maximum label width before truncation
	ShowValues     bool   // Show values at end of bars
	ValueFormatter func(float64) string
	Horizontal     bool // Horizontal bars (default true)
}

// DefaultBarChartConfig returns sensible defaults.
func DefaultBarChartConfig() BarChartConfig {
	return BarChartConfig{
		Title:         "",
		Width:         80,
		Height:        10,
		MaxLabelWidth: 25,
		ShowValues:    true,
		Horizontal:    true,
		ValueFormatter: func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		},
	}
}

// BarChart renders horizontal bar charts with rank-based coloring.
type BarChart struct {
	config BarChartConfig
	items  []BarChartItem
}

// NewBarChart creates a new bar chart component.
func NewBarChart(config BarChartConfig) *BarChart {
	if config.Width < 40 {
		config.Width = 40
	}
	if config.Height < 1 {
		config.Height = 10
	}
	if config.MaxLabelWidth < 10 {
		config.MaxLabelWidth = 10
	}
	if config.ValueFormatter == nil {
		config.ValueFormatter = func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		}
	}

	return &BarChart{
		config: config,
		items:  nil,
	}
}

// SetItems updates the bar chart data.
func (c *BarChart) SetItems(items []BarChartItem) {
	c.items = items
}

// SetSize updates the chart dimensions.
func (c *BarChart) SetSize(width, height int) {
	if width >= 40 {
		c.config.Width = width
	}
	if height >= 1 {
		c.config.Height = height
	}
}

// SetTitle updates the chart title.
func (c *BarChart) SetTitle(title string) {
	c.config.Title = title
}

// View renders the bar chart.
func (c *BarChart) View() string {
	if len(c.items) == 0 {
		return c.renderEmpty()
	}

	return c.renderChart()
}

// renderEmpty shows a placeholder when no data is available.
func (c *BarChart) renderEmpty() string {
	contentStyle := lipgloss.NewStyle().
		Width(c.config.Width-4).
		Height(c.config.Height).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(styles.ColorMuted)

	content := contentStyle.Render("No data available")

	if c.config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		header := titleStyle.Render(c.config.Title)
		return lipgloss.JoinVertical(lipgloss.Left, header, content)
	}

	return content
}

// renderChart renders the actual bar chart using pterm.
func (c *BarChart) renderChart() string {
	// Disable pterm's color for now - we'll apply lipgloss colors instead
	pterm.DisableColor()
	defer pterm.EnableColor()

	// Build pterm bars
	bars := make(pterm.Bars, 0, len(c.items))
	maxItems := c.config.Height
	if maxItems > len(c.items) {
		maxItems = len(c.items)
	}

	for i := 0; i < maxItems; i++ {
		item := c.items[i]
		label := truncateLabel(item.Label, c.config.MaxLabelWidth)
		bars = append(bars, pterm.Bar{
			Label: label,
			Value: int(item.Value), // pterm uses int values
		})
	}

	// Calculate bar width - leave room for label and value
	barAreaWidth := c.config.Width - c.config.MaxLabelWidth - 15
	if barAreaWidth < 10 {
		barAreaWidth = 10
	}

	// Render using pterm
	chart, err := pterm.DefaultBarChart.
		WithBars(bars).
		WithHorizontal(c.config.Horizontal).
		WithShowValue(c.config.ShowValues).
		WithWidth(barAreaWidth).
		Srender()

	if err != nil {
		return c.renderEmpty()
	}

	// Apply color coding to the rendered output
	chart = c.applyRankColors(chart)

	// Add title
	if c.config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		header := titleStyle.Render(c.config.Title)
		return lipgloss.JoinVertical(lipgloss.Left, header, "", chart)
	}

	return chart
}

// applyRankColors applies rank-based coloring to the chart output.
// Rank 1-3: Red, Rank 4-6: Yellow, Rank 7+: Green
func (c *BarChart) applyRankColors(chart string) string {
	lines := strings.Split(chart, "\n")
	coloredLines := make([]string, 0, len(lines))

	// Build a lookup of labels to ranks
	labelRanks := make(map[string]int)
	for _, item := range c.items {
		label := truncateLabel(item.Label, c.config.MaxLabelWidth)
		labelRanks[label] = item.Rank
	}

	for _, line := range lines {
		colored := line
		// Find which item this line belongs to by checking for labels
		for label, rank := range labelRanks {
			if strings.Contains(line, label) {
				// Apply color based on rank
				barStyle := c.getRankStyle(rank)
				// Color the bar characters (█)
				colored = colorBarInLine(line, barStyle)
				break
			}
		}
		coloredLines = append(coloredLines, colored)
	}

	return strings.Join(coloredLines, "\n")
}

// getRankStyle returns the lipgloss style for a given rank.
// Uses a cyan gradient: brightest for top ranks, dimmer for lower ranks.
func (c *BarChart) getRankStyle(rank int) lipgloss.Style {
	// Cyan gradient from bright to dim
	switch {
	case rank <= 2:
		// Top 2: Brightest cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	case rank <= 4:
		// Rank 3-4: Bright cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("44"))
	case rank <= 6:
		// Rank 5-6: Medium cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("37"))
	case rank <= 8:
		// Rank 7-8: Dim cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("30"))
	default:
		// Rank 9+: Dimmest cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("23"))
	}
}

// colorBarInLine applies color styling to bar characters in a line.
func colorBarInLine(line string, style lipgloss.Style) string {
	// Find bar characters and apply color
	barChars := []rune{'█', '▓', '▒', '░', '▄', '▀', '■'}
	var result strings.Builder
	var barSection strings.Builder
	inBar := false

	for _, ch := range line {
		isBarChar := false
		for _, bc := range barChars {
			if ch == bc {
				isBarChar = true
				break
			}
		}

		if isBarChar {
			if !inBar {
				inBar = true
			}
			barSection.WriteRune(ch)
		} else {
			if inBar {
				// End of bar section - apply style
				result.WriteString(style.Render(barSection.String()))
				barSection.Reset()
				inBar = false
			}
			result.WriteRune(ch)
		}
	}

	// Handle trailing bar section
	if inBar {
		result.WriteString(style.Render(barSection.String()))
	}

	return result.String()
}

// truncateLabel truncates a label to maxLen characters with ellipsis.
func truncateLabel(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// BarChartPanel wraps a bar chart with a bordered panel.
type BarChartPanel struct {
	chart   *BarChart
	visible bool
}

// NewBarChartPanel creates a new bar chart panel.
func NewBarChartPanel(config BarChartConfig) *BarChartPanel {
	return &BarChartPanel{
		chart:   NewBarChart(config),
		visible: true,
	}
}

// SetItems updates the bar chart data.
func (p *BarChartPanel) SetItems(items []BarChartItem) {
	p.chart.SetItems(items)
}

// SetSize updates the panel dimensions.
func (p *BarChartPanel) SetSize(width, height int) {
	// Account for border
	p.chart.SetSize(width-4, height-2)
}

// SetTitle updates the panel title.
func (p *BarChartPanel) SetTitle(title string) {
	p.chart.SetTitle(title)
}

// SetVisible sets the panel visibility.
func (p *BarChartPanel) SetVisible(visible bool) {
	p.visible = visible
}

// IsVisible returns the panel visibility state.
func (p *BarChartPanel) IsVisible() bool {
	return p.visible
}

// View renders the panel with border.
func (p *BarChartPanel) View() string {
	if !p.visible {
		return ""
	}

	content := p.chart.View()

	// Apply border
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1)

	return borderStyle.Render(content)
}

// RenderSimpleBarChart renders a simple horizontal bar chart without pterm.
// This is a fallback for environments where pterm may have issues.
func RenderSimpleBarChart(items []BarChartItem, config BarChartConfig) string {
	if len(items) == 0 {
		return ""
	}

	// Find max value for scaling
	maxVal := items[0].Value
	for _, item := range items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Calculate bar area width
	barWidth := config.Width - config.MaxLabelWidth - 15
	if barWidth < 10 {
		barWidth = 10
	}

	var sb strings.Builder

	// Title
	if config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		sb.WriteString(titleStyle.Render(config.Title))
		sb.WriteString("\n\n")
	}

	maxItems := config.Height
	if maxItems > len(items) {
		maxItems = len(items)
	}

	for i := 0; i < maxItems; i++ {
		item := items[i]

		// Truncate label
		label := truncateLabel(item.Label, config.MaxLabelWidth)
		paddedLabel := label + strings.Repeat(" ", config.MaxLabelWidth-len(label))

		// Calculate bar length
		barLen := int(float64(barWidth) * (item.Value / maxVal))
		if barLen < 1 && item.Value > 0 {
			barLen = 1
		}

		// Build bar
		bar := strings.Repeat("█", barLen)

		// Apply rank-based color
		barStyle := getRankStyleStatic(item.Rank)
		coloredBar := barStyle.Render(bar)

		// Format value
		valueStr := ""
		if config.ShowValues {
			valueStr = " " + config.ValueFormatter(item.Value)
		}

		sb.WriteString(paddedLabel)
		sb.WriteString(" ")
		sb.WriteString(coloredBar)
		sb.WriteString(valueStr)
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// getRankStyleStatic returns the lipgloss style for a given rank (standalone function).
// Uses a cyan gradient: brightest for top ranks, dimmer for lower ranks.
func getRankStyleStatic(rank int) lipgloss.Style {
	// Cyan gradient from bright to dim
	switch {
	case rank <= 2:
		// Top 2: Brightest cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	case rank <= 4:
		// Rank 3-4: Bright cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("44"))
	case rank <= 6:
		// Rank 5-6: Medium cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("37"))
	case rank <= 8:
		// Rank 7-8: Dim cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("30"))
	default:
		// Rank 9+: Dimmest cyan
		return lipgloss.NewStyle().Foreground(lipgloss.Color("23"))
	}
}
