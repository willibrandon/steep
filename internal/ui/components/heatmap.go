// Package components provides reusable UI components.
package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// HeatmapConfig configures a Heatmap component.
type HeatmapConfig struct {
	Title        string // Chart title
	Width        int    // Total width available
	ShowLegend   bool   // Show color legend
	ShowHourAxis bool   // Show hour axis (0-23)
	ShowDayAxis  bool   // Show day of week axis
	Condensed    bool   // Use condensed mode (4 time periods instead of 24 hours)
}

// DefaultHeatmapConfig returns sensible defaults.
func DefaultHeatmapConfig() HeatmapConfig {
	return HeatmapConfig{
		Title:        "",
		Width:        100,
		ShowLegend:   true,
		ShowHourAxis: true,
		ShowDayAxis:  true,
		Condensed:    false,
	}
}

// Heatmap renders a 7x24 heatmap showing patterns by day of week and hour.
type Heatmap struct {
	config HeatmapConfig
	// Data is a 7x24 matrix where [0]=Sunday, [6]=Saturday
	// Values of -1 indicate no data for that cell
	data    [7][24]float64
	min     float64
	max     float64
	hasData bool
}

// NewHeatmap creates a new heatmap component.
func NewHeatmap(config HeatmapConfig) *Heatmap {
	if config.Width < 40 {
		config.Width = 40
	}

	h := &Heatmap{
		config:  config,
		hasData: false,
	}
	// Initialize data with -1 (no data)
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			h.data[d][hr] = -1
		}
	}
	return h
}

// SetData sets the heatmap data matrix, min, and max values.
func (h *Heatmap) SetData(data [7][24]float64, min, max float64) {
	h.data = data
	h.min = min
	h.max = max

	// Check if we have any valid data
	h.hasData = false
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			if data[d][hr] >= 0 {
				h.hasData = true
				return
			}
		}
	}
}

// SetSize updates the component width.
func (h *Heatmap) SetSize(width int) {
	if width >= 40 {
		h.config.Width = width
	}
	// Auto-enable condensed mode for narrow terminals
	h.config.Condensed = width < 100
}

// SetTitle updates the heatmap title.
func (h *Heatmap) SetTitle(title string) {
	h.config.Title = title
}

// HasData returns whether the heatmap has any data to display.
func (h *Heatmap) HasData() bool {
	return h.hasData
}

// View renders the heatmap.
func (h *Heatmap) View() string {
	if !h.hasData {
		return h.renderEmpty()
	}

	var content string
	if h.config.Condensed {
		content = h.renderCondensed()
	} else {
		content = h.renderFull()
	}

	// Center the heatmap within the available width
	contentWidth := lipgloss.Width(content)
	if contentWidth < h.config.Width {
		return lipgloss.NewStyle().
			Width(h.config.Width).
			Align(lipgloss.Center).
			Render(content)
	}
	return content
}

// renderEmpty shows a placeholder when no data is available.
func (h *Heatmap) renderEmpty() string {
	contentWidth := h.config.Width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	contentStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Foreground(styles.ColorMuted)

	content := contentStyle.Render("Collecting data... (need 24h+ for patterns)")

	if h.config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		header := titleStyle.Render(h.config.Title)
		return lipgloss.JoinVertical(lipgloss.Left, header, "", content)
	}

	return content
}

// renderFull renders the full 7x24 heatmap.
func (h *Heatmap) renderFull() string {
	var sb strings.Builder

	// Title
	if h.config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		sb.WriteString(titleStyle.Render(h.config.Title))
		sb.WriteString("\n\n")
	}

	// Day labels - 5 chars each ("Sun  ", "Mon  ", etc.)
	dayLabels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

	// Hour axis header - align with 5-char day label prefix
	if h.config.ShowHourAxis {
		sb.WriteString("     ") // 5 spaces to match day label width
		for hr := 0; hr < 24; hr++ {
			sb.WriteString(fmt.Sprintf("%02d ", hr))
		}
		sb.WriteString("\n")
	}

	// Render each day row
	for d := 0; d < 7; d++ {
		if h.config.ShowDayAxis {
			sb.WriteString(fmt.Sprintf("%-5s", dayLabels[d])) // 5 chars, left-aligned
		}

		for hr := 0; hr < 24; hr++ {
			val := h.data[d][hr]
			cell := h.renderCell(val)
			sb.WriteString(cell)
		}
		sb.WriteString("\n")
	}

	// Legend
	if h.config.ShowLegend {
		sb.WriteString("\n")
		sb.WriteString(h.renderLegend())
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// renderCondensed renders the condensed 7x4 heatmap (4 time periods).
func (h *Heatmap) renderCondensed() string {
	var sb strings.Builder

	// Title
	if h.config.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(styles.ColorAccent).
			Bold(true)
		sb.WriteString(titleStyle.Render(h.config.Title))
		sb.WriteString("\n\n")
	}

	// Period labels
	periods := []struct {
		name  string
		start int
		end   int
	}{
		{"Night", 0, 5},
		{"Morning", 6, 11},
		{"Afternoon", 12, 17},
		{"Evening", 18, 23},
	}

	// Day labels - 5 chars each
	dayLabels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

	// Period header - align with 5-char day label prefix
	if h.config.ShowHourAxis {
		sb.WriteString("     ") // 5 spaces to match day label width
		for _, p := range periods {
			sb.WriteString(fmt.Sprintf("%-11s", p.name)) // 11 chars to match condensed cell width
		}
		sb.WriteString("\n")
	}

	// Render each day row with aggregated periods
	for d := 0; d < 7; d++ {
		if h.config.ShowDayAxis {
			sb.WriteString(fmt.Sprintf("%-5s", dayLabels[d])) // 5 chars, left-aligned
		}

		for _, p := range periods {
			// Aggregate hours in this period
			avgVal := h.aggregatePeriod(d, p.start, p.end)
			cell := h.renderCondensedCell(avgVal)
			sb.WriteString(cell)
		}
		sb.WriteString("\n")
	}

	// Legend
	if h.config.ShowLegend {
		sb.WriteString("\n")
		sb.WriteString(h.renderCondensedLegend())
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// aggregatePeriod calculates the average value for a period.
func (h *Heatmap) aggregatePeriod(day, startHour, endHour int) float64 {
	var sum float64
	var count int

	for hr := startHour; hr <= endHour; hr++ {
		if h.data[day][hr] >= 0 {
			sum += h.data[day][hr]
			count++
		}
	}

	if count == 0 {
		return -1 // No data
	}
	return sum / float64(count)
}

// renderCell renders a single heatmap cell with color.
func (h *Heatmap) renderCell(value float64) string {
	// No data
	if value < 0 {
		noDataStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		return noDataStyle.Render("·· ")
	}

	// Calculate intensity level (0-3)
	level := h.getIntensityLevel(value)
	char, style := h.getCellStyle(level)
	return style.Render(char + " ")
}

// renderCondensedCell renders a condensed cell (11 chars wide to match period header).
func (h *Heatmap) renderCondensedCell(value float64) string {
	// No data - 11 chars total
	if value < 0 {
		noDataStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		return noDataStyle.Render("··         ") // 2 + 9 = 11 chars
	}

	// Calculate intensity level (0-3)
	level := h.getIntensityLevel(value)
	char, style := h.getCellStyle(level)
	return style.Render(char + "         ") // 2 + 9 = 11 chars
}

// getIntensityLevel returns 0-3 based on value position between min and max.
func (h *Heatmap) getIntensityLevel(value float64) int {
	if h.max == h.min {
		return 2 // All values same, use medium
	}

	// Normalize to 0-1
	normalized := (value - h.min) / (h.max - h.min)

	// Map to 0-3
	switch {
	case normalized < 0.25:
		return 0 // Low
	case normalized < 0.50:
		return 1 // Medium
	case normalized < 0.75:
		return 2 // High
	default:
		return 3 // Peak
	}
}

// getCellStyle returns the character and style for a given intensity level.
// Uses cyan gradient consistent with bar charts.
func (h *Heatmap) getCellStyle(level int) (string, lipgloss.Style) {
	switch level {
	case 0: // Low (0-25%)
		return "░░", lipgloss.NewStyle().Foreground(lipgloss.Color("23"))
	case 1: // Medium (25-50%)
		return "▒▒", lipgloss.NewStyle().Foreground(lipgloss.Color("30"))
	case 2: // High (50-75%)
		return "▓▓", lipgloss.NewStyle().Foreground(lipgloss.Color("37"))
	default: // Peak (75-100%)
		return "██", lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	}
}

// renderLegend renders the color legend.
func (h *Heatmap) renderLegend() string {
	legendStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	low, lowStyle := h.getCellStyle(0)
	med, medStyle := h.getCellStyle(1)
	high, highStyle := h.getCellStyle(2)
	peak, peakStyle := h.getCellStyle(3)

	legend := fmt.Sprintf(" %s Low  %s Medium  %s High  %s Peak",
		lowStyle.Render(low),
		medStyle.Render(med),
		highStyle.Render(high),
		peakStyle.Render(peak),
	)

	return legendStyle.Render("Legend:") + legend
}

// renderCondensedLegend renders a condensed legend.
func (h *Heatmap) renderCondensedLegend() string {
	low, lowStyle := h.getCellStyle(0)
	med, medStyle := h.getCellStyle(1)
	high, highStyle := h.getCellStyle(2)
	peak, peakStyle := h.getCellStyle(3)

	return fmt.Sprintf(" %s Low %s Med %s High %s Peak",
		lowStyle.Render(low),
		medStyle.Render(med),
		highStyle.Render(high),
		peakStyle.Render(peak),
	)
}

// HeatmapPanel wraps a heatmap with a bordered panel.
type HeatmapPanel struct {
	heatmap *Heatmap
	visible bool
}

// NewHeatmapPanel creates a new heatmap panel.
func NewHeatmapPanel(config HeatmapConfig) *HeatmapPanel {
	return &HeatmapPanel{
		heatmap: NewHeatmap(config),
		visible: false, // Hidden by default
	}
}

// SetData sets the heatmap data.
func (p *HeatmapPanel) SetData(data [7][24]float64, min, max float64) {
	p.heatmap.SetData(data, min, max)
}

// SetSize updates the panel dimensions.
func (p *HeatmapPanel) SetSize(width int) {
	// Account for border
	p.heatmap.SetSize(width - 4)
}

// SetTitle updates the panel title.
func (p *HeatmapPanel) SetTitle(title string) {
	p.heatmap.SetTitle(title)
}

// SetVisible sets the panel visibility.
func (p *HeatmapPanel) SetVisible(visible bool) {
	p.visible = visible
}

// IsVisible returns the panel visibility state.
func (p *HeatmapPanel) IsVisible() bool {
	return p.visible
}

// Toggle toggles the panel visibility.
func (p *HeatmapPanel) Toggle() {
	p.visible = !p.visible
}

// HasData returns whether the heatmap has data.
func (p *HeatmapPanel) HasData() bool {
	return p.heatmap.HasData()
}

// Height returns the height of the panel when visible.
func (p *HeatmapPanel) Height() int {
	if !p.visible {
		return 0
	}
	// Title (1) + blank (1) + hours header (1) + 7 days + blank (1) + legend (1) + borders (2) = 14
	if p.heatmap.config.Condensed {
		return 12 // Slightly smaller in condensed mode
	}
	return 14
}

// View renders the panel with border.
func (p *HeatmapPanel) View() string {
	if !p.visible {
		return ""
	}

	content := p.heatmap.View()

	// Apply border
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1)

	return borderStyle.Render(content)
}

// RenderSimpleHeatmap renders a heatmap without creating a component instance.
// Useful for one-off rendering.
func RenderSimpleHeatmap(data [7][24]float64, min, max float64, config HeatmapConfig) string {
	h := NewHeatmap(config)
	h.SetData(data, min, max)
	return h.View()
}
