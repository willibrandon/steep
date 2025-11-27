package replication

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// renderStatusBar renders the boxed status bar (like Tables view).
func (v *ReplicationView) renderStatusBar() string {
	// Connection info (left side)
	connInfo := v.connectionInfo
	if connInfo == "" {
		connInfo = "PostgreSQL"
	}
	title := styles.StatusTitleStyle.Render(connInfo)

	// Server role indicator
	var roleIndicator string
	if v.data.IsPrimary {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			Render(" [PRIMARY]")
	} else {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Render(" [STANDBY]")
	}

	// Additional indicators
	var indicators string
	if v.readOnly {
		indicators += lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(" [read-only]")
	}
	if v.err != nil {
		indicators += styles.ErrorStyle.Render(" [ERROR]")
	}
	if v.refreshing {
		indicators += lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(" [refreshing...]")
	}

	// Stale indicator
	var staleIndicator string
	if !v.lastUpdate.IsZero() && time.Since(v.lastUpdate) > 35*time.Second {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	// Timestamp (right side)
	updateStr := "never"
	if !v.lastUpdate.IsZero() {
		updateStr = v.lastUpdate.Format("15:04:05")
	}
	timestamp := styles.StatusTimeStyle.Render(updateStr)

	// Query timing (debug mode only)
	var queryTiming string
	if v.debug && v.data != nil && v.data.QueryDuration > 0 {
		timingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
		if v.data.QueryDuration > 500*time.Millisecond {
			timingStyle = timingStyle.Foreground(lipgloss.Color("196")) // Red if > 500ms
		} else if v.data.QueryDuration > 100*time.Millisecond {
			timingStyle = timingStyle.Foreground(lipgloss.Color("214")) // Yellow if > 100ms
		}
		queryTiming = timingStyle.Render(fmt.Sprintf(" [query: %s]", v.data.QueryDuration.Round(time.Microsecond)))
	}

	// Calculate gap
	leftContent := title + roleIndicator + indicators + staleIndicator
	rightContent := queryTiming + " " + timestamp
	gap := v.width - lipgloss.Width(leftContent) - lipgloss.Width(rightContent) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(leftContent + spaces + rightContent)
}

// renderOverview renders the Overview tab content.
func (v *ReplicationView) renderOverview() string {
	// Check for permission errors and display guidance
	if v.err != nil {
		return v.renderError()
	}

	if !v.data.IsPrimary {
		// Connected to standby - show WAL receiver status
		return v.renderStandbyOverview()
	}

	if len(v.data.Replicas) == 0 {
		return v.renderNoReplicas()
	}

	return v.renderReplicaTable()
}

// renderError renders an error message with guidance.
func (v *ReplicationView) renderError() string {
	errMsg := v.err.Error()

	// Detect permission-related errors
	isPermissionErr := strings.Contains(strings.ToLower(errMsg), "permission") ||
		strings.Contains(strings.ToLower(errMsg), "denied") ||
		strings.Contains(strings.ToLower(errMsg), "insufficient_privilege")

	var b strings.Builder

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(errorStyle.Render("Error: " + errMsg))
	b.WriteString("\n\n")

	if isPermissionErr {
		b.WriteString(hintStyle.Render("Permission denied. To view replication status:\n\n"))
		b.WriteString(hintStyle.Render("  1. Connect as a superuser, or\n"))
		b.WriteString(hintStyle.Render("  2. Grant the pg_monitor role:\n"))
		b.WriteString(hintStyle.Render("     GRANT pg_monitor TO your_user;\n\n"))
		b.WriteString(hintStyle.Render("This grants read-only access to monitoring views."))
	} else {
		b.WriteString(hintStyle.Render("Check your database connection and permissions.\n"))
		b.WriteString(hintStyle.Render("Press r to retry."))
	}

	return lipgloss.Place(
		v.width, v.height-4,
		lipgloss.Center, lipgloss.Center,
		b.String(),
	)
}

// renderNoReplicas renders the empty state message.
func (v *ReplicationView) renderNoReplicas() string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("No streaming replicas connected.\n\n" +
			"To set up replication:\n" +
			"  1. Press Tab to go to Setup tab\n" +
			"  2. Run configuration checker (c)\n" +
			"  3. Use physical replication wizard (1)")

	return lipgloss.Place(
		v.width, v.height-4,
		lipgloss.Center, lipgloss.Center,
		msg,
	)
}

// renderStandbyOverview renders the standby server overview.
func (v *ReplicationView) renderStandbyOverview() string {
	if v.data.WALReceiverStatus == nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Connected to standby server.\nWAL receiver status not available.")
	}

	wal := v.data.WALReceiverStatus
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(20)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	b.WriteString(headerStyle.Render("WAL Receiver Status"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Status:"))
	b.WriteString(valueStyle.Render(wal.Status))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Primary Host:"))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%s:%d", wal.SenderHost, wal.SenderPort)))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Received LSN:"))
	b.WriteString(valueStyle.Render(wal.ReceivedLSN))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Lag:"))
	lagStr := models.FormatBytes(wal.LagBytes)
	lagStyle := valueStyle
	if wal.LagBytes > 10*1024*1024 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("196"))
	} else if wal.LagBytes > 1024*1024 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("214"))
	} else {
		lagStyle = lagStyle.Foreground(lipgloss.Color("42"))
	}
	b.WriteString(lagStyle.Render(lagStr))
	b.WriteString("\n")

	if wal.SlotName != "" {
		b.WriteString(labelStyle.Render("Slot:"))
		b.WriteString(valueStyle.Render(wal.SlotName))
		b.WriteString("\n")
	}

	return b.String()
}

// renderReplicaTable renders the replica list table.
func (v *ReplicationView) renderReplicaTable() string {
	var b strings.Builder

	// Calculate available height for table
	tableHeight := v.height - 6 // status + title + tabs + header + footer
	if tableHeight < 3 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
			Render("Terminal too small. Resize to at least 80x24.")
	}

	// Column headers - adaptive for terminal width
	// Full width: 18+14+10+8+10+9+14 = 83
	// Minimum (80): hide Trend column: 18+14+10+8+10+9 = 69
	// Very narrow (<70): hide Client too: 18+10+8+10+9 = 55
	headers := v.getAdaptiveHeaders()

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for i, h := range headers {
		text := truncateWithEllipsis(h.Name, h.Width)
		// Add sort indicator
		if SortColumn(i) == v.sortColumn {
			if v.sortAsc {
				text += " ↑"
			} else {
				text += " ↓"
			}
		}
		headerRow.WriteString(headerStyle.Render(padRight(text, h.Width)))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Data rows
	visibleRows := min(tableHeight, len(v.data.Replicas)-v.scrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.scrollOffset + i
		if idx >= len(v.data.Replicas) {
			break
		}
		replica := v.data.Replicas[idx]
		b.WriteString(v.renderReplicaRow(replica, idx == v.selectedIdx, headers))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(v.renderFooter())

	return b.String()
}

// renderReplicaRow renders a single replica row.
func (v *ReplicationView) renderReplicaRow(r models.Replica, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Lag severity colors
	lagSeverity := r.GetLagSeverity()
	var lagStyle lipgloss.Style
	switch lagSeverity {
	case models.LagSeverityHealthy:
		lagStyle = baseStyle.Foreground(lipgloss.Color("42"))
	case models.LagSeverityWarning:
		lagStyle = baseStyle.Foreground(lipgloss.Color("214"))
	case models.LagSeverityCritical:
		lagStyle = baseStyle.Foreground(lipgloss.Color("196"))
	}

	// Sync state color
	var syncStyle lipgloss.Style
	switch r.SyncState {
	case models.SyncStateSync:
		syncStyle = baseStyle.Foreground(lipgloss.Color("42"))
	case models.SyncStatePotential:
		syncStyle = baseStyle.Foreground(lipgloss.Color("214"))
	case models.SyncStateQuorum:
		syncStyle = baseStyle.Foreground(lipgloss.Color("39"))
	default:
		syncStyle = baseStyle.Foreground(lipgloss.Color("245"))
	}

	// Build row dynamically based on available columns
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "name":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.ApplicationName, h.Width), h.Width)))
		case "client":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.ClientAddr, h.Width), h.Width)))
		case "state":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.State, h.Width), h.Width)))
		case "sync":
			row.WriteString(syncStyle.Render(padRight(r.SyncState.String(), h.Width)))
		case "byte_lag":
			row.WriteString(lagStyle.Render(padRight(r.FormatByteLag(), h.Width)))
		case "time_lag":
			row.WriteString(lagStyle.Render(padRight(r.FormatReplayLag(), h.Width)))
		case "trend":
			// Sparkline column - get lag history for this replica
			sparkline := v.renderLagSparkline(r.ApplicationName, h.Width-2)
			sparklineWidth := lipgloss.Width(sparkline)
			if sparklineWidth < h.Width {
				sparkline += strings.Repeat(" ", h.Width-sparklineWidth)
			}
			// Add ANSI reset at the very end to prevent color bleeding
			row.WriteString(sparkline + "\033[0m")
		}
	}

	return row.String()
}

// renderLagSparkline renders a sparkline for the given replica's lag history.
func (v *ReplicationView) renderLagSparkline(replicaName string, width int) string {
	// Use SQLite data for windows > 1 minute, otherwise use in-memory ring buffer
	// Fall back to in-memory if SQLite has no data for this replica
	var history []float64
	var ok bool
	if v.timeWindow > time.Minute && v.sqliteLagHistory != nil {
		history, ok = v.sqliteLagHistory[replicaName]
	}
	// Fall back to in-memory ring buffer if no SQLite data
	if !ok || len(history) == 0 {
		history, ok = v.data.LagHistory[replicaName]
	}
	if !ok || len(history) == 0 {
		return strings.Repeat("─", width)
	}

	// Configure sparkline with severity-based coloring
	config := components.SparklineConfig{
		Width:  width,
		Height: 1,
	}

	// Thresholds: 1MB warning, 10MB critical (in bytes)
	const (
		warningThreshold  = 1024 * 1024      // 1 MB
		criticalThreshold = 10 * 1024 * 1024 // 10 MB
	)

	return components.RenderSparklineWithSeverity(history, config, float64(warningThreshold), float64(criticalThreshold))
}

// ColumnConfig represents a table column configuration.
type ColumnConfig struct {
	Name  string
	Width int
	Key   string // Key for identifying column type
}

// getAdaptiveHeaders returns headers adapted to terminal width.
// T100: Ensure views render correctly at 80x24 minimum terminal size
func (v *ReplicationView) getAdaptiveHeaders() []ColumnConfig {
	// Full set of columns
	allHeaders := []ColumnConfig{
		{"Name", 18, "name"},
		{"Client", 14, "client"},
		{"State", 10, "state"},
		{"Sync", 8, "sync"},
		{"Byte Lag", 10, "byte_lag"},
		{"Time Lag", 9, "time_lag"},
		{"Trend", 14, "trend"},
	}

	// Calculate total width needed
	totalWidth := 0
	for _, h := range allHeaders {
		totalWidth += h.Width
	}

	// If terminal is wide enough, return all columns
	if v.width >= totalWidth+2 {
		return allHeaders
	}

	// Drop Trend column for narrower terminals (80 chars)
	if v.width >= 70 {
		return allHeaders[:6] // All except Trend
	}

	// Drop Client and Trend for very narrow terminals (<70)
	return []ColumnConfig{
		{"Name", 18, "name"},
		{"State", 10, "state"},
		{"Sync", 8, "sync"},
		{"Byte Lag", 10, "byte_lag"},
		{"Time Lag", 9, "time_lag"},
	}
}
