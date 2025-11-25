// Package setup provides setup wizard and configuration check components.
package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ConfigCheckConfig holds configuration for the config checker rendering.
type ConfigCheckConfig struct {
	Width  int
	Height int
}

// RenderConfigCheck renders the configuration checker panel.
// T041: Implement configuration checker panel showing wal_level, max_wal_senders,
//
//	max_replication_slots, wal_keep_size, hot_standby, archive_mode
//
// T042: Display green checkmark for correctly configured parameters
// T043: Display red X with guidance text for misconfigured parameters
// T044: Show overall "READY" or "NOT READY" status summary
func RenderConfigCheck(config *models.ReplicationConfig, cfg ConfigCheckConfig) string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Configuration Readiness Check"))
	b.WriteString("\n\n")

	if config == nil {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Configuration data not available."))
		b.WriteString("\n\n")
		b.WriteString(renderConfigFooter())
		return b.String()
	}

	// Overall status summary (T044)
	b.WriteString(renderOverallStatus(config))
	b.WriteString("\n\n")

	// Parameter table
	b.WriteString(renderParameterTable(config))
	b.WriteString("\n")

	// Guidance section for misconfigured parameters
	issues := config.GetIssues()
	if len(issues) > 0 {
		b.WriteString(renderGuidance(issues))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(renderConfigFooter())
	return b.String()
}

// renderOverallStatus renders the READY or NOT READY status.
func renderOverallStatus(config *models.ReplicationConfig) string {
	var statusText string
	var statusStyle lipgloss.Style

	if config.IsReady() {
		statusText = "✓ READY"
		statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")). // Green
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("42"))
	} else {
		statusText = "✗ NOT READY"
		statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")). // Red
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196"))

		if config.RequiresRestart() {
			statusText += " (restart required)"
		}
	}

	return statusStyle.Render(statusText)
}

// renderParameterTable renders the configuration parameters as a table.
func renderParameterTable(config *models.ReplicationConfig) string {
	var b strings.Builder

	// Column headers matching row widths: name(22) + value(12) + required(30) + status
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	header := padToWidth("Parameter", 22) + padToWidth("Current", 12) + padToWidth("Required", 30) + "Status"
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Separator
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	b.WriteString(sepStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n")

	// Render each parameter
	params := config.AllParams()
	for _, p := range params {
		b.WriteString(renderParamRow(p))
		b.WriteString("\n")
	}

	return b.String()
}

// renderParamRow renders a single parameter row with status indicator.
// T042: Display green checkmark for correctly configured parameters
// T043: Display red X for misconfigured parameters
func renderParamRow(p models.ConfigParam) string {
	// Status indicator (T042 & T043)
	var statusIndicator string
	var statusColor lipgloss.Color

	if p.IsValid {
		statusIndicator = "✓"
		statusColor = lipgloss.Color("42") // Green
	} else {
		statusIndicator = "✗"
		statusColor = lipgloss.Color("196") // Red
	}

	// Add unit to current value if present
	currentValue := p.CurrentValue
	if p.Unit != "" && currentValue != "" {
		currentValue = fmt.Sprintf("%s %s", currentValue, p.Unit)
	}

	// Truncate and pad values to fixed widths
	name := padToWidth(p.Name, 22)
	value := padToWidth(truncate(currentValue, 11), 12)
	required := padToWidth(truncate(p.RequiredValue, 24), 32) // Wider to push status right

	// Apply colors
	valueStyle := lipgloss.NewStyle()
	if !p.IsValid {
		valueStyle = valueStyle.Foreground(lipgloss.Color("196"))
	}
	requiredStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle := lipgloss.NewStyle().Foreground(statusColor)

	// Add restart indicator
	restartIndicator := ""
	if p.NeedsRestart && !p.IsValid {
		restartIndicator = " *"
	}

	return name + valueStyle.Render(value) + requiredStyle.Render(required) + statusStyle.Render(statusIndicator) + restartIndicator
}

// padToWidth pads a string to exact width with spaces.
func padToWidth(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// renderGuidance renders guidance text for misconfigured parameters.
func renderGuidance(issues []models.ConfigParam) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	b.WriteString(headerStyle.Render("Guidance"))
	b.WriteString("\n\n")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	for _, p := range issues {
		guidance := getParamGuidance(p)
		b.WriteString(fmt.Sprintf("  • %s: %s\n", p.Name, guidance))
	}

	b.WriteString("\n")
	if hasRestartRequired(issues) {
		b.WriteString(hintStyle.Render("  * Parameters marked with * require PostgreSQL restart"))
		b.WriteString("\n")
	}

	return b.String()
}

// getParamGuidance returns specific guidance for a misconfigured parameter.
func getParamGuidance(p models.ConfigParam) string {
	switch p.Name {
	case "wal_level":
		return fmt.Sprintf("Set to 'replica' or 'logical' in postgresql.conf (current: %s)", p.CurrentValue)
	case "max_wal_senders":
		return fmt.Sprintf("Set to at least 2 (one per replica + buffer) in postgresql.conf (current: %s)", p.CurrentValue)
	case "max_replication_slots":
		return fmt.Sprintf("Set to at least 1 per replica/subscriber in postgresql.conf (current: %s)", p.CurrentValue)
	case "wal_keep_size":
		return fmt.Sprintf("Recommended: Set to retain WAL for replica reconnection (current: %s)", p.CurrentValue)
	case "hot_standby":
		return fmt.Sprintf("Set to 'on' to allow queries on standby servers (current: %s)", p.CurrentValue)
	case "archive_mode":
		return fmt.Sprintf("Set to 'on' for point-in-time recovery (current: %s)", p.CurrentValue)
	default:
		return fmt.Sprintf("Current value: %s, required: %s", p.CurrentValue, p.RequiredValue)
	}
}

// hasRestartRequired checks if any issues require a restart.
func hasRestartRequired(issues []models.ConfigParam) bool {
	for _, p := range issues {
		if p.NeedsRestart {
			return true
		}
	}
	return false
}

// renderConfigFooter renders the navigation footer.
func renderConfigFooter() string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	return footerStyle.Render("[esc/q]back")
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
