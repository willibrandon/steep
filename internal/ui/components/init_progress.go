// Package components provides reusable UI components for Steep TUI.
package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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

	// Two-phase snapshot fields (T087g)
	CurrentStep       string  // Current step: schema, tables, sequences, checksums
	CompressionRatio  float64 // Compression ratio (e.g., 0.35 means 35% of original size)
	ChecksumsVerified int     // Number of checksums verified (application phase)
	ChecksumsFailed   int     // Number of checksum failures (application phase)
	ChecksumStatus    string  // Checksum verification status: verifying, passed, failed
}

// InitProgressOverlay renders a detailed progress overlay for node initialization.
// It integrates bubbles/progress for animated progress bars and bubbles/spinner
// for activity indication during active phases (T087h, T087i).
type InitProgressOverlay struct {
	width    int
	height   int
	visible  bool
	progress *InitProgressData

	// Animated progress bar (T087h) - gradient from orange → green as progress increases
	overallProgress progress.Model
	tableProgress   progress.Model

	// Spinner (T087i) - Dot style for active phases, hidden when idle
	spinner spinner.Model
}

// NewInitProgressOverlay creates a new initialization progress overlay.
func NewInitProgressOverlay() *InitProgressOverlay {
	// Create animated progress bar with orange → green gradient (T087h)
	overallProg := progress.New(
		progress.WithScaledGradient("#FF8C00", "#00FF00"), // Orange to Green gradient
		progress.WithWidth(40),
		progress.WithoutPercentage(), // We render percentage separately
	)

	tableProg := progress.New(
		progress.WithScaledGradient("#FF8C00", "#00FF00"), // Orange to Green gradient
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	// Create spinner with Dot style (T087i)
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(styles.ColorAccent)),
	)

	return &InitProgressOverlay{
		overallProgress: overallProg,
		tableProgress:   tableProg,
		spinner:         s,
	}
}

// Init initializes the overlay's animated components.
// Call this to get the initial command for the spinner animation.
func (p *InitProgressOverlay) Init() tea.Cmd {
	return p.spinner.Tick
}

// Update handles messages for the animated components (spinner and progress bar).
// This should be called from the parent model's Update function.
func (p *InitProgressOverlay) Update(msg tea.Msg) (*InitProgressOverlay, tea.Cmd) {
	var cmds []tea.Cmd

	// Only process spinner ticks when visible and actively initializing
	if p.visible && p.progress != nil && isActiveState(p.progress.State) {
		switch msg := msg.(type) {
		case spinner.TickMsg:
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			cmds = append(cmds, cmd)

		case progress.FrameMsg:
			// Handle progress bar animation frames
			var cmd tea.Cmd
			overallModel, cmd := p.overallProgress.Update(msg)
			p.overallProgress = overallModel.(progress.Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

			tableModel, cmd := p.tableProgress.Update(msg)
			p.tableProgress = tableModel.(progress.Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return p, tea.Batch(cmds...)
}

// SetSize sets the overlay dimensions.
func (p *InitProgressOverlay) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// Show displays the overlay with the given progress data.
// Returns a command to start animations if the state is active.
func (p *InitProgressOverlay) Show(progress *InitProgressData) tea.Cmd {
	p.progress = progress
	p.visible = true

	// Start animations for active states
	if isActiveState(progress.State) {
		// Set progress bar values and trigger animation
		overallCmd := p.overallProgress.SetPercent(progress.OverallPercent / 100.0)
		tableCmd := p.tableProgress.SetPercent(progress.CurrentTablePercent / 100.0)
		return tea.Batch(p.spinner.Tick, overallCmd, tableCmd)
	}

	// For non-active states, just update progress bars without animation
	p.overallProgress.SetPercent(progress.OverallPercent / 100.0)
	p.tableProgress.SetPercent(progress.CurrentTablePercent / 100.0)
	return nil
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

// GetState returns the current state being displayed.
func (p *InitProgressOverlay) GetState() string {
	if p.progress == nil {
		return ""
	}
	return p.progress.State
}

// IsInitializing returns true if the displayed node is actively initializing.
func (p *InitProgressOverlay) IsInitializing() bool {
	if p.progress == nil {
		return false
	}
	return isActiveState(p.progress.State)
}

// SetProgress updates the progress data and triggers progress bar animation.
// Returns a command to animate the progress bars to the new values.
func (p *InitProgressOverlay) SetProgress(progress *InitProgressData) tea.Cmd {
	p.progress = progress

	if progress == nil {
		return nil
	}

	// Animate progress bars to new values
	overallCmd := p.overallProgress.SetPercent(progress.OverallPercent / 100.0)
	tableCmd := p.tableProgress.SetPercent(progress.CurrentTablePercent / 100.0)

	return tea.Batch(overallCmd, tableCmd)
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

	// State and Phase with spinner for active states (T087i)
	stateColor := stateToColor(pr.State)
	stateStyle := lipgloss.NewStyle().Foreground(stateColor).Bold(true)
	stateText := stateStyle.Render(pr.State)
	if isActiveState(pr.State) {
		// Show spinner before state for active phases
		stateText = p.spinner.View() + " " + stateText
	}
	lines = append(lines, labelStyle.Render("State:")+stateText)

	if pr.Phase != "" {
		lines = append(lines, labelStyle.Render("Phase:")+valueStyle.Render(phaseDisplayName(pr.Phase)))
	}

	// Current step within phase (two-phase snapshot)
	if pr.CurrentStep != "" {
		lines = append(lines, labelStyle.Render("Step:")+valueStyle.Render(stepDisplayName(pr.CurrentStep)))
	}

	if pr.SourceNode != "" {
		lines = append(lines, labelStyle.Render("Source:")+valueStyle.Render(pr.SourceNode))
	}

	lines = append(lines, "")

	// Overall progress bar - use animated bubbles/progress bar (T087h)
	lines = append(lines, labelStyle.Render("Overall:"))
	lines = append(lines, p.renderAnimatedProgressBar(p.overallProgress, pr.OverallPercent))
	lines = append(lines, "")

	// Tables progress
	if pr.TablesTotal > 0 {
		tablesLine := fmt.Sprintf("%d / %d tables", pr.TablesCompleted, pr.TablesTotal)
		lines = append(lines, labelStyle.Render("Tables:")+valueStyle.Render(tablesLine))
	}

	// Current table with its progress - use animated bubbles/progress bar (T087h)
	if pr.CurrentTable != "" {
		lines = append(lines, labelStyle.Render("Current Table:")+valueStyle.Render(pr.CurrentTable))
		lines = append(lines, p.renderAnimatedProgressBar(p.tableProgress, pr.CurrentTablePercent))
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

	// Compression ratio (two-phase snapshot)
	if pr.CompressionRatio > 0 {
		compressionPct := pr.CompressionRatio * 100
		lines = append(lines, labelStyle.Render("Compression:")+valueStyle.Render(fmt.Sprintf("%.1f%% of original", compressionPct)))
	}

	// Checksum verification status (application phase)
	if pr.ChecksumsVerified > 0 || pr.ChecksumsFailed > 0 || pr.ChecksumStatus != "" {
		lines = append(lines, "")
		lines = append(lines, titleStyle.Render("Checksum Verification"))

		var checksumStatusDisplay string
		switch pr.ChecksumStatus {
		case "verifying":
			checksumStatusDisplay = "⏳ Verifying..."
		case "passed":
			checksumStatusDisplay = "✓ Passed"
		case "failed":
			checksumStatusDisplay = "✗ Failed"
		default:
			checksumStatusDisplay = pr.ChecksumStatus
		}
		if checksumStatusDisplay != "" {
			statusColor := checksumStatusToColor(pr.ChecksumStatus)
			statusStyle := lipgloss.NewStyle().Foreground(statusColor)
			lines = append(lines, labelStyle.Render("Status:")+statusStyle.Render(checksumStatusDisplay))
		}

		if pr.ChecksumsVerified > 0 || pr.ChecksumsFailed > 0 {
			verifiedStyle := lipgloss.NewStyle().Foreground(styles.ColorSuccess)
			failedStyle := lipgloss.NewStyle().Foreground(styles.ColorError)

			verifiedText := verifiedStyle.Render(fmt.Sprintf("%d passed", pr.ChecksumsVerified))
			if pr.ChecksumsFailed > 0 {
				failedText := failedStyle.Render(fmt.Sprintf("%d failed", pr.ChecksumsFailed))
				lines = append(lines, labelStyle.Render("Checksums:")+verifiedText+", "+failedText)
			} else {
				lines = append(lines, labelStyle.Render("Checksums:")+verifiedText)
			}
		}
	}

	lines = append(lines, "")

	// Time information
	lines = append(lines, titleStyle.Render("Time"))

	// Elapsed time
	if !pr.StartedAt.IsZero() {
		elapsed := time.Since(pr.StartedAt)
		lines = append(lines, labelStyle.Render("Elapsed:")+valueStyle.Render(formatDuration(elapsed)))
	}

	// ETA - show "0s" during active states instead of hiding when ETA reaches 0
	if pr.ETASeconds > 0 {
		eta := time.Duration(pr.ETASeconds) * time.Second
		lines = append(lines, labelStyle.Render("ETA:")+valueStyle.Render(formatDuration(eta)))
	} else if pr.ETASeconds == 0 && isActiveState(pr.State) {
		lines = append(lines, labelStyle.Render("ETA:")+valueStyle.Render("0s"))
	}

	// Error message
	if pr.ErrorMessage != "" {
		lines = append(lines, "")
		lines = append(lines, errorStyle.Render("Error: "+pr.ErrorMessage))
	}

	// Help text
	lines = append(lines, "")
	helpStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	// Only show cancel option if initialization is active
	if isActiveState(pr.State) {
		lines = append(lines, helpStyle.Render("[C] Cancel  [Esc] Close"))
	} else {
		lines = append(lines, helpStyle.Render("[Esc] Close"))
	}

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

// renderAnimatedProgressBar renders a bubbles/progress bar with percentage (T087h).
// The progress bar uses a gradient from orange → green as progress increases.
func (p *InitProgressOverlay) renderAnimatedProgressBar(prog progress.Model, percent float64) string {
	percentStyle := lipgloss.NewStyle().Width(6)
	return fmt.Sprintf("%s %s", prog.View(), percentStyle.Render(fmt.Sprintf("%.1f%%", percent)))
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

// stepDisplayName returns a human-readable name for the current step.
func stepDisplayName(step string) string {
	switch step {
	case "schema":
		return "Exporting Schema"
	case "tables":
		return "Copying Tables"
	case "sequences":
		return "Syncing Sequences"
	case "checksums":
		return "Verifying Checksums"
	default:
		return step
	}
}

// checksumStatusToColor returns the appropriate color for a checksum status.
func checksumStatusToColor(status string) lipgloss.Color {
	switch status {
	case "passed":
		return styles.ColorSuccess
	case "failed":
		return styles.ColorError
	case "verifying":
		return styles.ColorAccent
	default:
		return styles.ColorMuted
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

	// ETA if available - show "0s" during active states instead of hiding
	if progress.ETASeconds > 0 {
		eta := time.Duration(progress.ETASeconds) * time.Second
		parts = append(parts, fmt.Sprintf("ETA: %s", formatDuration(eta)))
	} else if progress.ETASeconds == 0 && isActiveState(progress.State) {
		parts = append(parts, "ETA: 0s")
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
