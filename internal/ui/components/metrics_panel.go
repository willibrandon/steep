package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// MetricsPanel displays the four main database metrics.
type MetricsPanel struct {
	width   int
	metrics models.Metrics
	maxConn int
}

// NewMetricsPanel creates a new metrics panel component.
func NewMetricsPanel() *MetricsPanel {
	return &MetricsPanel{
		maxConn: 100, // Default, should be updated from SHOW max_connections
	}
}

// SetMetrics updates the displayed metrics.
func (p *MetricsPanel) SetMetrics(metrics models.Metrics) {
	p.metrics = metrics
}

// SetMaxConnections sets the maximum connection limit for display.
func (p *MetricsPanel) SetMaxConnections(max int) {
	p.maxConn = max
}

// SetWidth sets the width of the panel.
func (p *MetricsPanel) SetWidth(width int) {
	p.width = width
}

// View renders the metrics panel.
func (p *MetricsPanel) View() string {
	// Each panel has border (2 chars) + padding (2 chars) = 4 chars overhead
	// With 4 panels: 4 * 4 = 16 chars total overhead
	panelWidth := (p.width - 16) / 4
	if panelWidth < 12 {
		panelWidth = 12
	}

	// Create the four panels
	tpsPanel := p.renderPanel("TPS", p.formatTPS(), "", models.PanelNormal, panelWidth)
	cachePanel := p.renderPanel("Cache Hit", p.formatCacheRatio(), "", p.getCacheStatus(), panelWidth)
	connPanel := p.renderPanel("Connections", p.formatConnections(), "", models.PanelNormal, panelWidth)
	sizePanel := p.renderPanel("DB Size", p.formatSize(), "", models.PanelNormal, panelWidth)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		tpsPanel,
		cachePanel,
		connPanel,
		sizePanel,
	)
}

// renderPanel renders a single metric panel.
func (p *MetricsPanel) renderPanel(label, value, unit string, status models.PanelStatus, width int) string {
	labelStyle := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(styles.ColorMuted)

	valueStyle := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Bold(true)

	// Apply status colors
	switch status {
	case models.PanelWarning:
		valueStyle = valueStyle.
			Foreground(styles.ColorWarningFg).
			Background(styles.ColorWarningBg)
	case models.PanelCritical:
		valueStyle = valueStyle.
			Foreground(styles.ColorCriticalFg).
			Background(styles.ColorCriticalBg)
	default:
		valueStyle = valueStyle.Foreground(styles.ColorActive)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		labelStyle.Render(label),
		valueStyle.Render(value+unit),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorder).
		Padding(0, 1).
		Render(content)
}

// formatTPS formats the TPS value with appropriate units.
func (p *MetricsPanel) formatTPS() string {
	tps := p.metrics.TPS
	if tps >= 10000 {
		return fmt.Sprintf("%.1fk/s", tps/1000)
	} else if tps >= 1000 {
		return fmt.Sprintf("%.2fk/s", tps/1000)
	}
	return fmt.Sprintf("%.0f/s", tps)
}

// formatCacheRatio formats the cache hit ratio as a percentage.
func (p *MetricsPanel) formatCacheRatio() string {
	return fmt.Sprintf("%.1f%%", p.metrics.CacheHitRatio)
}

// formatConnections formats the connection count with max.
func (p *MetricsPanel) formatConnections() string {
	return fmt.Sprintf("%d/%d", p.metrics.ConnectionCount, p.maxConn)
}

// formatSize formats the database size with appropriate units.
func (p *MetricsPanel) formatSize() string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	size := p.metrics.DatabaseSize
	switch {
	case size >= TB:
		return fmt.Sprintf("%.1f TB", float64(size)/TB)
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// getCacheStatus returns the panel status based on cache hit ratio.
func (p *MetricsPanel) getCacheStatus() models.PanelStatus {
	if p.metrics.CacheHitRatio < 80 {
		return models.PanelCritical
	} else if p.metrics.CacheHitRatio < 90 {
		return models.PanelWarning
	}
	return models.PanelNormal
}
