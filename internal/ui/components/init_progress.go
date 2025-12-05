// Package components provides reusable UI components for Steep TUI.
package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// InitProgressData represents the initialization progress data for TUI display.
type InitProgressData struct {
	NodeID              string
	NodeName            string
	State               string // uninitialized, preparing, copying, catching_up, synchronized, etc.
	Phase               string // generation, application, catching_up
	OverallPercent      float64
	TablesTotal         int
	TablesCompleted     int
	CurrentTable        string
	CurrentTablePercent float64
	RowsCopied          int64
	BytesCopied         int64
	ThroughputRowsSec   float64
	ThroughputBytesSec  float64
	StartedAt           time.Time
	ETASeconds          int
	ParallelWorkers     int
	ErrorMessage        string
	SourceNode          string
}

// InitProgressOverlay renders a detailed progress overlay for node initialization.
type InitProgressOverlay struct {
	width    int
	height   int
	visible  bool
	progress *InitProgressData
}

// NewInitProgressOverlay creates a new initialization progress overlay.
func NewInitProgressOverlay() *InitProgressOverlay {
	return &InitProgressOverlay{}
}

// SetSize sets the overlay dimensions.
func (p *InitProgressOverlay) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// Show displays the overlay with the given progress data.
func (p *InitProgressOverlay) Show(progress *InitProgressData) {
	p.progress = progress
	p.visible = true
}

// Hide hides the overlay.
func (p *InitProgressOverlay) Hide() {
	p.visible = false
}

// IsVisible returns whether the overlay is visible.
func (p *InitProgressOverlay) IsVisible() bool {
	return p.visible
}

// GetNodeID returns the node ID being displayed.
func (p *InitProgressOverlay) GetNodeID() string {
	if p.progress == nil {
		return ""
	}
	return p.progress.NodeID
}

// SetProgress updates the progress data.
func (p *InitProgressOverlay) SetProgress(progress *InitProgressData) {
	p.progress = progress
}

// View renders the progress overlay.
func (p *InitProgressOverlay) View() string {
	if !p.visible || p.progress == nil {
		return ""
	}

	pr := p.progress

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Width(16)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	errorStyle := lipgloss.NewStyle().
		Foreground(styles.ColorError).
		Bold(true)

	// Build content
	var lines []string

	// Title with node name
	nodeDisplay := pr.NodeID
	if pr.NodeName != "" && pr.NodeName != pr.NodeID {
		nodeDisplay = fmt.Sprintf("%s (%s)", pr.NodeName, pr.NodeID)
	}
	lines = append(lines, titleStyle.Render(fmt.Sprintf("Node Initialization: %s", nodeDisplay)))

	// State and Phase
	stateColor := stateToColor(pr.State)
	stateStyle := lipgloss.NewStyle().Foreground(stateColor).Bold(true)
	lines = append(lines, labelStyle.Render("State:")+stateStyle.Render(pr.State))

	if pr.Phase != "" {
		lines = append(lines, labelStyle.Render("Phase:")+valueStyle.Render(phaseDisplayName(pr.Phase)))
	}

	if pr.SourceNode != "" {
		lines = append(lines, labelStyle.Render("Source:")+valueStyle.Render(pr.SourceNode))
	}

	lines = append(lines, "")

	// Overall progress bar
	lines = append(lines, labelStyle.Render("Overall:"))
	lines = append(lines, renderProgressBar(pr.OverallPercent, 40))
	lines = append(lines, "")

	// Tables progress
	if pr.TablesTotal > 0 {
		tablesLine := fmt.Sprintf("%d / %d tables", pr.TablesCompleted, pr.TablesTotal)
		lines = append(lines, labelStyle.Render("Tables:")+valueStyle.Render(tablesLine))
	}

	// Current table with its progress
	if pr.CurrentTable != "" {
		lines = append(lines, labelStyle.Render("Current Table:")+valueStyle.Render(pr.CurrentTable))
		lines = append(lines, renderProgressBar(pr.CurrentTablePercent, 40))
	}

	lines = append(lines, "")

	// Statistics
	lines = append(lines, titleStyle.Render("Statistics"))

	// Rows and bytes copied
	if pr.RowsCopied > 0 {
		lines = append(lines, labelStyle.Render("Rows Copied:")+valueStyle.Render(formatNumber(pr.RowsCopied)))
	}
	if pr.BytesCopied > 0 {
		lines = append(lines, labelStyle.Render("Bytes Copied:")+valueStyle.Render(formatBytes(pr.BytesCopied)))
	}

	// Throughput
	if pr.ThroughputRowsSec > 0 {
		lines = append(lines, labelStyle.Render("Throughput:")+valueStyle.Render(fmt.Sprintf("%.0f rows/sec", pr.ThroughputRowsSec)))
	}
	if pr.ThroughputBytesSec > 0 {
		lines = append(lines, labelStyle.Render("")+valueStyle.Render(formatBytes(int64(pr.ThroughputBytesSec))+"/sec"))
	}

	// Parallel workers
	if pr.ParallelWorkers > 1 {
		lines = append(lines, labelStyle.Render("Workers:")+valueStyle.Render(fmt.Sprintf("%d", pr.ParallelWorkers)))
	}

	lines = append(lines, "")

	// Time information
	lines = append(lines, titleStyle.Render("Time"))

	// Elapsed time
	if !pr.StartedAt.IsZero() {
		elapsed := time.Since(pr.StartedAt)
		lines = append(lines, labelStyle.Render("Elapsed:")+valueStyle.Render(formatDuration(elapsed)))
	}

	// ETA
	if pr.ETASeconds > 0 {
		eta := time.Duration(pr.ETASeconds) * time.Second
		lines = append(lines, labelStyle.Render("ETA:")+valueStyle.Render(formatDuration(eta)))
	}

	// Error message
	if pr.ErrorMessage != "" {
		lines = append(lines, "")
		lines = append(lines, errorStyle.Render("Error: "+pr.ErrorMessage))
	}

	// Help text
	lines = append(lines, "")
	helpStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	lines = append(lines, helpStyle.Render("[C] Cancel  [Esc] Close"))

	content := strings.Join(lines, "\n")

	// Dialog box style
	dialogWidth := 50
	if p.width > 60 {
		dialogWidth = 55
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(dialogWidth)

	return dialogStyle.Render(content)
}

// renderProgressBar renders a text-based progress bar.
func renderProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(float64(width) * percent / 100)
	empty := width - filled

	// Color based on progress
	var barColor lipgloss.Color
	switch {
	case percent >= 100:
		barColor = styles.ColorSuccess
	case percent >= 50:
		barColor = styles.ColorAccent
	default:
		barColor = lipgloss.Color("214") // Orange
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	bar := filledStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))

	percentStyle := lipgloss.NewStyle().Width(6)
	return fmt.Sprintf("%s %s", bar, percentStyle.Render(fmt.Sprintf("%.1f%%", percent)))
}

// stateToColor returns the appropriate color for an init state.
func stateToColor(state string) lipgloss.Color {
	switch state {
	case "synchronized":
		return styles.ColorSuccess
	case "preparing", "copying", "catching_up", "reinitializing":
		return styles.ColorAccent
	case "uninitialized":
		return styles.ColorMuted
	case "failed":
		return styles.ColorError
	case "diverged":
		return styles.ColorAlertWarning
	default:
		return styles.ColorMuted
	}
}

// phaseDisplayName returns a human-readable name for the phase.
func phaseDisplayName(phase string) string {
	switch phase {
	case "generation":
		return "Snapshot Generation"
	case "application":
		return "Snapshot Application"
	case "catching_up":
		return "WAL Catch-up"
	default:
		return phase
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// formatNumber formats a large number with commas.
func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	s := fmt.Sprintf("%d", n)
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// formatBytes formats bytes in a human-readable way.
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// RenderCompactProgress renders a compact progress string for display in a table row.
// Format: "45.2% (5/12 tables) ETA: 2m 30s"
func RenderCompactProgress(progress *InitProgressData) string {
	if progress == nil {
		return "-"
	}

	var parts []string

	// Percent
	parts = append(parts, fmt.Sprintf("%.1f%%", progress.OverallPercent))

	// Tables if available
	if progress.TablesTotal > 0 {
		parts = append(parts, fmt.Sprintf("(%d/%d)", progress.TablesCompleted, progress.TablesTotal))
	}

	// ETA if available
	if progress.ETASeconds > 0 {
		eta := time.Duration(progress.ETASeconds) * time.Second
		parts = append(parts, fmt.Sprintf("ETA: %s", formatDuration(eta)))
	}

	return strings.Join(parts, " ")
}

// RenderStateWithProgress renders a state string with optional progress indicator.
// For active states, includes progress percent. For terminal states, just the state.
func RenderStateWithProgress(state string, progress *InitProgressData) string {
	// Color the state
	stateColor := stateToColor(state)
	stateStyle := lipgloss.NewStyle().Foreground(stateColor)

	if progress == nil || !isActiveState(state) {
		return stateStyle.Render(state)
	}

	// Active state with progress
	return fmt.Sprintf("%s %.0f%%", stateStyle.Render(state), progress.OverallPercent)
}

// isActiveState returns true if the state indicates active initialization.
func isActiveState(state string) bool {
	switch state {
	case "preparing", "copying", "catching_up", "reinitializing":
		return true
	default:
		return false
	}
}
