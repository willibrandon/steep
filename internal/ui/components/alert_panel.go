package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/alerts"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// AlertPanel displays active alerts with severity icons and details.
type AlertPanel struct {
	width  int
	alerts []alerts.ActiveAlert
}

// NewAlertPanel creates a new alert panel component.
func NewAlertPanel() *AlertPanel {
	return &AlertPanel{}
}

// SetAlerts updates the displayed alerts and sorts by severity (Critical first).
func (p *AlertPanel) SetAlerts(activeAlerts []alerts.ActiveAlert) {
	// Copy to avoid mutating original
	p.alerts = make([]alerts.ActiveAlert, len(activeAlerts))
	copy(p.alerts, activeAlerts)

	// Sort by severity: Critical first, then Warning
	sort.Slice(p.alerts, func(i, j int) bool {
		if p.alerts[i].State == alerts.StateCritical && p.alerts[j].State != alerts.StateCritical {
			return true
		}
		if p.alerts[i].State != alerts.StateCritical && p.alerts[j].State == alerts.StateCritical {
			return false
		}
		// Same severity: sort by duration (longest first)
		return p.alerts[i].Duration > p.alerts[j].Duration
	})
}

// SetWidth sets the width of the panel.
func (p *AlertPanel) SetWidth(width int) {
	p.width = width
}

// HasAlerts returns true if there are active alerts.
func (p *AlertPanel) HasAlerts() bool {
	return len(p.alerts) > 0
}

// Height returns the height needed for the panel.
func (p *AlertPanel) Height() int {
	if !p.HasAlerts() {
		return 0
	}
	// Title + each alert line + border (2)
	return len(p.alerts) + 3
}

// View renders the alert panel.
func (p *AlertPanel) View() string {
	if !p.HasAlerts() {
		return ""
	}

	// Available width for content (minus border and padding)
	contentWidth := p.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorAlertCritical).
		Bold(true)
	title := titleStyle.Render("Active Alerts")

	// Render each alert
	var alertLines []string
	for _, alert := range p.alerts {
		alertLines = append(alertLines, p.renderAlert(alert, contentWidth))
	}

	// Join all content
	content := lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(alertLines, "\n"))

	// Panel border with red color for critical, orange for warning-only
	borderColor := styles.ColorAlertWarning
	for _, alert := range p.alerts {
		if alert.IsCritical() {
			borderColor = styles.ColorAlertCritical
			break
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(p.width - 2).
		Render(content)
}

// renderAlert renders a single alert line with severity icon.
func (p *AlertPanel) renderAlert(alert alerts.ActiveAlert, maxWidth int) string {
	// Severity icon
	var icon string
	var iconStyle lipgloss.Style
	if alert.Acknowledged {
		// Dim icon when acknowledged
		icon = "●"
		iconStyle = lipgloss.NewStyle().Foreground(styles.ColorAlertAck)
	} else if alert.IsCritical() {
		icon = "●"
		iconStyle = lipgloss.NewStyle().Foreground(styles.ColorAlertCritical).Bold(true)
	} else {
		icon = "●"
		iconStyle = lipgloss.NewStyle().Foreground(styles.ColorAlertWarning).Bold(true)
	}

	// Acknowledged indicator - ASCII checkbox style
	ackIndicator := " [ ]"
	if alert.Acknowledged {
		ackIndicator = " [x]"
	}

	// Duration
	duration := alert.DurationString()
	durationStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	// Use the formatted message (supports templates), fallback to default format
	message := alert.Message
	if message == "" {
		// Default format if no message template configured
		valueStr := fmt.Sprintf("%.2f", alert.MetricValue)
		thresholdStr := fmt.Sprintf("%.2f", alert.Threshold)
		message = fmt.Sprintf("%s: %s (threshold: %s)", alert.RuleName, valueStr, thresholdStr)
	}

	// Calculate available width for message
	// icon(1) + space(1) + space(1) + duration + ack(" [x]" = 4 chars)
	fixedWidth := 3 + 1 + len(duration) + 4
	availableForMessage := maxWidth - fixedWidth

	if len(message) > availableForMessage && availableForMessage > 3 {
		message = message[:availableForMessage-3] + "..."
	}

	// When acknowledged, dim the entire line
	if alert.Acknowledged {
		dimStyle := lipgloss.NewStyle().Foreground(styles.ColorAlertAck)
		return fmt.Sprintf("%s %s",
			iconStyle.Render(icon),
			dimStyle.Render(fmt.Sprintf("%s %s%s", message, duration, ackIndicator)),
		)
	}

	return fmt.Sprintf("%s %s %s%s",
		iconStyle.Render(icon),
		message,
		durationStyle.Render(duration),
		ackIndicator,
	)
}
