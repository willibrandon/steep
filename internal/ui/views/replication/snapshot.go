// Package replication provides the Replication view for monitoring PostgreSQL replication.
package replication

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// SnapshotEntry represents a snapshot in the list.
type SnapshotEntry struct {
	SnapshotID   string
	SourceNode   string
	TargetNode   string
	Phase        components.SnapshotPhase
	Status       string // generating, applying, completed, failed, cancelled
	Progress     float64
	StartedAt    time.Time
	CompletedAt  *time.Time
	BytesTotal   int64
	BytesDone    int64
	TablesTotal  int
	TablesDone   int
	ErrorMessage string

	// Full progress data for overlay
	ProgressData *components.SnapshotProgressData
}

// renderSnapshots renders the Snapshots tab content.
func (v *ReplicationView) renderSnapshots() string {
	if len(v.snapshots) == 0 {
		return v.renderNoSnapshots()
	}

	return v.renderSnapshotTable()
}

// renderNoSnapshots renders the empty state for the Snapshots tab.
func (v *ReplicationView) renderNoSnapshots() string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("No snapshots found.\n\n" +
			"Snapshots appear here when:\n" +
			"  - Running 'steep-repl snapshot generate'\n" +
			"  - Running 'steep-repl snapshot apply'\n" +
			"  - Starting node initialization with --snapshot\n\n" +
			"Press 'S' to start a new snapshot (requires steep-repl daemon)")

	return lipgloss.Place(
		v.width, v.height-8,
		lipgloss.Center, lipgloss.Center,
		msg,
	)
}

// renderSnapshotTable renders the snapshot list table.
func (v *ReplicationView) renderSnapshotTable() string {
	var b strings.Builder

	// Calculate available height
	tableHeight := v.height - 8
	if tableHeight < 3 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
			Render("Terminal too small. Resize to at least 80x24.")
	}

	// Column headers
	headers := v.getSnapshotTableHeaders()

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for _, h := range headers {
		text := truncateWithEllipsis(h.Name, h.Width)
		headerRow.WriteString(headerStyle.Render(padRight(text, h.Width)))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Data rows
	visibleRows := min(tableHeight, len(v.snapshots)-v.snapshotScrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.snapshotScrollOffset + i
		if idx >= len(v.snapshots) {
			break
		}
		snap := v.snapshots[idx]
		b.WriteString(v.renderSnapshotRow(snap, idx == v.snapshotSelectedIdx, headers))
		b.WriteString("\n")
	}

	// Wrap content
	contentHeight := v.height - 8
	content := lipgloss.NewStyle().
		Height(contentHeight).
		Render(b.String())

	return content + "\n" + v.renderSnapshotFooter()
}

// renderSnapshotRow renders a single snapshot row.
func (v *ReplicationView) renderSnapshotRow(snap SnapshotEntry, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Status color
	statusStyle := baseStyle.Foreground(snapshotStatusColor(snap.Status))

	// Build row
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "id":
			idDisplay := snap.SnapshotID
			if len(idDisplay) > h.Width {
				idDisplay = idDisplay[:h.Width-3] + "..."
			}
			row.WriteString(baseStyle.Render(padRight(idDisplay, h.Width)))
		case "source":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(snap.SourceNode, h.Width), h.Width)))
		case "target":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(snap.TargetNode, h.Width), h.Width)))
		case "phase":
			phaseDisplay := formatSnapshotPhase(snap.Phase)
			row.WriteString(baseStyle.Render(padRight(phaseDisplay, h.Width)))
		case "status":
			row.WriteString(statusStyle.Render(padRight(snap.Status, h.Width)))
		case "progress":
			progressStr := formatSnapshotProgress(snap)
			row.WriteString(baseStyle.Render(padRight(progressStr, h.Width)))
		case "tables":
			tablesStr := "-"
			if snap.TablesTotal > 0 {
				tablesStr = fmt.Sprintf("%d/%d", snap.TablesDone, snap.TablesTotal)
			}
			row.WriteString(baseStyle.Render(padRight(tablesStr, h.Width)))
		case "size":
			sizeStr := "-"
			if snap.BytesTotal > 0 {
				sizeStr = fmt.Sprintf("%s/%s",
					formatBytesCompact(snap.BytesDone),
					formatBytesCompact(snap.BytesTotal))
			}
			row.WriteString(baseStyle.Render(padRight(sizeStr, h.Width)))
		case "started":
			startedStr := "-"
			if !snap.StartedAt.IsZero() {
				startedStr = formatTimeAgo(snap.StartedAt)
			}
			row.WriteString(baseStyle.Render(padRight(startedStr, h.Width)))
		}
	}

	return row.String()
}

// getSnapshotTableHeaders returns headers adapted to terminal width.
func (v *ReplicationView) getSnapshotTableHeaders() []ColumnConfig {
	const (
		idWidth       = 12
		sourceWidth   = 14
		targetWidth   = 14
		phaseWidth    = 12
		statusWidth   = 10
		progressWidth = 10
		tablesWidth   = 8
		sizeWidth     = 14
		startedWidth  = 10
	)

	// Wide mode (>140): Show all columns
	if v.width >= 140 {
		return []ColumnConfig{
			{"ID", idWidth, "id"},
			{"Source", sourceWidth, "source"},
			{"Target", targetWidth, "target"},
			{"Phase", phaseWidth, "phase"},
			{"Status", statusWidth, "status"},
			{"Progress", progressWidth, "progress"},
			{"Tables", tablesWidth, "tables"},
			{"Size", sizeWidth, "size"},
			{"Started", startedWidth, "started"},
		}
	}

	// Normal mode (100-140): Drop Size column
	if v.width >= 100 {
		return []ColumnConfig{
			{"ID", idWidth, "id"},
			{"Source", sourceWidth, "source"},
			{"Target", targetWidth, "target"},
			{"Phase", phaseWidth, "phase"},
			{"Status", statusWidth, "status"},
			{"Progress", progressWidth, "progress"},
			{"Tables", tablesWidth, "tables"},
			{"Started", startedWidth, "started"},
		}
	}

	// Narrow mode (80-100): Essential columns
	if v.width >= 80 {
		return []ColumnConfig{
			{"ID", idWidth, "id"},
			{"Source", sourceWidth, "source"},
			{"Target", targetWidth, "target"},
			{"Status", statusWidth, "status"},
			{"Progress", progressWidth, "progress"},
		}
	}

	// Minimum (<80): Minimal columns
	return []ColumnConfig{
		{"ID", idWidth, "id"},
		{"Status", statusWidth, "status"},
		{"Progress", progressWidth, "progress"},
	}
}

// renderSnapshotFooter renders the footer for the Snapshots tab.
func (v *ReplicationView) renderSnapshotFooter() string {
	helpStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	var hints []string

	hints = append(hints, "Enter: Details")
	if !v.readOnly {
		hints = append(hints, "S: New Snapshot")
		// Only show Cancel if there's an active snapshot
		for _, s := range v.snapshots {
			if s.Status == "generating" || s.Status == "applying" {
				hints = append(hints, "C: Cancel")
				break
			}
		}
	}
	hints = append(hints, "?: Help")

	return helpStyle.Render(strings.Join(hints, "  "))
}

// renderSnapshotProgressOverlay renders the detailed snapshot progress overlay.
func (v *ReplicationView) renderSnapshotProgressOverlay() string {
	if v.snapshotOverlay == nil || !v.snapshotOverlay.IsVisible() {
		return v.renderSnapshots()
	}

	// Render the overlay
	v.snapshotOverlay.SetSize(v.width, v.height)
	overlay := v.snapshotOverlay.View()

	// Center the overlay over the background
	return lipgloss.Place(
		v.width, v.height-8,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// snapshotStatusColor returns the appropriate color for a snapshot status.
func snapshotStatusColor(status string) lipgloss.Color {
	switch status {
	case "completed":
		return styles.ColorSuccess
	case "generating", "applying":
		return styles.ColorAccent
	case "failed":
		return styles.ColorError
	case "cancelled":
		return styles.ColorAlertWarning
	default:
		return styles.ColorMuted
	}
}

// formatSnapshotPhase formats the snapshot phase for display.
func formatSnapshotPhase(phase components.SnapshotPhase) string {
	switch phase {
	case components.PhaseGeneration:
		return "Generation"
	case components.PhaseApplication:
		return "Application"
	case components.PhaseIdle:
		return "-"
	default:
		return string(phase)
	}
}

// formatSnapshotProgress formats progress for display.
func formatSnapshotProgress(snap SnapshotEntry) string {
	if snap.Status == "completed" {
		return "100%"
	}
	if snap.Status == "failed" || snap.Status == "cancelled" {
		return fmt.Sprintf("%.0f%%", snap.Progress)
	}
	return fmt.Sprintf("%.1f%%", snap.Progress)
}

// formatBytesCompact formats bytes in a compact human-readable way.
func formatBytesCompact(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case b >= TB:
		return fmt.Sprintf("%.1fT", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.1fG", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.1fM", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.0fK", float64(b)/KB)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// formatTimeAgo formats a time as relative to now.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// moveSnapshotSelection moves the snapshot selection by delta.
func (v *ReplicationView) moveSnapshotSelection(delta int) {
	if len(v.snapshots) == 0 {
		return
	}
	v.snapshotSelectedIdx += delta
	if v.snapshotSelectedIdx < 0 {
		v.snapshotSelectedIdx = 0
	}
	if v.snapshotSelectedIdx >= len(v.snapshots) {
		v.snapshotSelectedIdx = len(v.snapshots) - 1
	}
	v.ensureSnapshotVisible()
}

// ensureSnapshotVisible ensures the selected snapshot is visible in the viewport.
func (v *ReplicationView) ensureSnapshotVisible() {
	tableHeight := v.height - 10 // Reserve space for header and footer
	if tableHeight < 1 {
		tableHeight = 1
	}
	if v.snapshotSelectedIdx < v.snapshotScrollOffset {
		v.snapshotScrollOffset = v.snapshotSelectedIdx
	}
	if v.snapshotSelectedIdx >= v.snapshotScrollOffset+tableHeight {
		v.snapshotScrollOffset = v.snapshotSelectedIdx - tableHeight + 1
	}
}

// buildSnapshotProgressData creates SnapshotProgressData from a SnapshotEntry.
func (v *ReplicationView) buildSnapshotProgressData(snap SnapshotEntry) *components.SnapshotProgressData {
	// If we have full progress data, use it
	if snap.ProgressData != nil {
		return snap.ProgressData
	}

	// Build basic progress data from snapshot entry
	data := &components.SnapshotProgressData{
		SnapshotID:     snap.SnapshotID,
		SourceNode:     snap.SourceNode,
		TargetNode:     snap.TargetNode,
		Phase:          snap.Phase,
		OverallPercent: snap.Progress,
		StartedAt:      snap.StartedAt,
	}

	// Set tables/bytes based on phase
	if snap.Phase == components.PhaseGeneration {
		data.GenTablesTotal = snap.TablesTotal
		data.GenTablesCompleted = snap.TablesDone
		data.GenBytesTotal = snap.BytesTotal
		data.GenBytesWritten = snap.BytesDone
	} else if snap.Phase == components.PhaseApplication {
		data.AppTablesTotal = snap.TablesTotal
		data.AppTablesCompleted = snap.TablesDone
		data.AppBytesTotal = snap.BytesTotal
		data.AppBytesLoaded = snap.BytesDone
	}

	if snap.ErrorMessage != "" {
		data.ErrorMessage = snap.ErrorMessage
	}

	return data
}
