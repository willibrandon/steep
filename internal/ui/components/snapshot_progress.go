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

// SnapshotPhase represents the current phase of snapshot operation.
type SnapshotPhase string

const (
	PhaseGeneration  SnapshotPhase = "generation"
	PhaseApplication SnapshotPhase = "application"
	PhaseIdle        SnapshotPhase = "idle"
)

// TableProgress represents progress for a single table in the snapshot.
type TableProgress struct {
	TableName   string
	Schema      string
	RowsTotal   int64
	RowsCopied  int64
	BytesTotal  int64
	BytesCopied int64
	Percent     float64
	Status      string // pending, in_progress, completed, failed
	Error       string
}

// SnapshotProgressData represents the complete snapshot progress data for TUI display.
type SnapshotProgressData struct {
	// Identification
	SnapshotID string
	SourceNode string
	TargetNode string

	// Phase information
	Phase       SnapshotPhase
	CurrentStep string // schema, tables, sequences, checksums

	// Overall progress
	OverallPercent float64
	StartedAt      time.Time
	ETASeconds     int

	// Generation stats
	GenTablesTotal     int
	GenTablesCompleted int
	GenBytesTotal      int64
	GenBytesWritten    int64
	GenRowsTotal       int64
	GenRowsWritten     int64

	// Application stats
	AppTablesTotal     int
	AppTablesCompleted int
	AppBytesTotal      int64
	AppBytesLoaded     int64
	AppRowsTotal       int64
	AppRowsLoaded      int64

	// Per-table progress
	Tables            []TableProgress
	CurrentTable      string
	CurrentTableIndex int

	// Compression
	CompressionEnabled bool
	CompressionRatio   float64 // 0.35 = 35% of original size

	// Checksum verification
	ChecksumsTotal    int
	ChecksumsVerified int
	ChecksumsFailed   int
	ChecksumStatus    string // verifying, passed, failed

	// Throughput (samples for sparkline - last 60 seconds)
	ThroughputHistory []float64 // bytes/sec samples
	CurrentThroughput float64   // current bytes/sec

	// Error state
	ErrorMessage string
}

// SnapshotProgressOverlay renders a detailed snapshot progress overlay with
// two-section layout, per-table progress, and throughput sparkline (T087j).
type SnapshotProgressOverlay struct {
	width    int
	height   int
	visible  bool
	progress *SnapshotProgressData

	// Animated progress bars (T087h style)
	overallProgress progress.Model
	tableProgress   progress.Model

	// Spinner for active phases (T087i style)
	spinner spinner.Model

	// Scroll state for table list
	tableScrollOffset int
	maxVisibleTables  int
}

// NewSnapshotProgressOverlay creates a new snapshot progress overlay.
func NewSnapshotProgressOverlay() *SnapshotProgressOverlay {
	// Create animated progress bar with orange -> green gradient
	overallProg := progress.New(
		progress.WithScaledGradient("#FF8C00", "#00FF00"),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	tableProg := progress.New(
		progress.WithScaledGradient("#FF8C00", "#00FF00"),
		progress.WithWidth(35),
		progress.WithoutPercentage(),
	)

	// Create spinner with Dot style
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(styles.ColorAccent)),
	)

	return &SnapshotProgressOverlay{
		overallProgress:  overallProg,
		tableProgress:    tableProg,
		spinner:          s,
		maxVisibleTables: 5,
	}
}

// Init initializes the overlay's animated components.
func (p *SnapshotProgressOverlay) Init() tea.Cmd {
	return p.spinner.Tick
}

// Update handles messages for the animated components.
func (p *SnapshotProgressOverlay) Update(msg tea.Msg) (*SnapshotProgressOverlay, tea.Cmd) {
	var cmds []tea.Cmd

	if p.visible && p.progress != nil && p.isActive() {
		switch msg := msg.(type) {
		case spinner.TickMsg:
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			cmds = append(cmds, cmd)

		case progress.FrameMsg:
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

		case tea.KeyMsg:
			// Handle scroll in table list
			switch msg.String() {
			case "j", "down":
				if p.tableScrollOffset < len(p.progress.Tables)-p.maxVisibleTables {
					p.tableScrollOffset++
				}
			case "k", "up":
				if p.tableScrollOffset > 0 {
					p.tableScrollOffset--
				}
			}
		}
	}

	return p, tea.Batch(cmds...)
}

// SetSize sets the overlay dimensions.
func (p *SnapshotProgressOverlay) SetSize(width, height int) {
	p.width = width
	p.height = height
	// Adjust visible tables based on height
	if height > 40 {
		p.maxVisibleTables = 8
	} else if height > 30 {
		p.maxVisibleTables = 6
	} else {
		p.maxVisibleTables = 4
	}
}

// Show displays the overlay with the given progress data.
func (p *SnapshotProgressOverlay) Show(progress *SnapshotProgressData) tea.Cmd {
	p.progress = progress
	p.visible = true
	p.tableScrollOffset = 0

	// Auto-scroll to current table
	if progress != nil && progress.CurrentTableIndex >= 0 {
		if progress.CurrentTableIndex >= p.maxVisibleTables {
			p.tableScrollOffset = progress.CurrentTableIndex - p.maxVisibleTables/2
		}
	}

	if p.isActive() {
		overallCmd := p.overallProgress.SetPercent(progress.OverallPercent / 100.0)
		tableCmd := p.tableProgress.SetPercent(p.getCurrentTablePercent() / 100.0)
		return tea.Batch(p.spinner.Tick, overallCmd, tableCmd)
	}

	return nil
}

// Hide hides the overlay.
func (p *SnapshotProgressOverlay) Hide() {
	p.visible = false
}

// IsVisible returns whether the overlay is visible.
func (p *SnapshotProgressOverlay) IsVisible() bool {
	return p.visible
}

// SetProgress updates the progress data and triggers animation.
func (p *SnapshotProgressOverlay) SetProgress(progress *SnapshotProgressData) tea.Cmd {
	p.progress = progress

	if progress == nil {
		return nil
	}

	// Auto-scroll to keep current table visible
	if progress.CurrentTableIndex >= 0 {
		if progress.CurrentTableIndex >= p.tableScrollOffset+p.maxVisibleTables {
			p.tableScrollOffset = progress.CurrentTableIndex - p.maxVisibleTables + 1
		} else if progress.CurrentTableIndex < p.tableScrollOffset {
			p.tableScrollOffset = progress.CurrentTableIndex
		}
	}

	overallCmd := p.overallProgress.SetPercent(progress.OverallPercent / 100.0)
	tableCmd := p.tableProgress.SetPercent(p.getCurrentTablePercent() / 100.0)

	return tea.Batch(overallCmd, tableCmd)
}

// View renders the snapshot progress overlay.
func (p *SnapshotProgressOverlay) View() string {
	if !p.visible || p.progress == nil {
		return ""
	}

	pr := p.progress

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)

	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(styles.ColorMuted).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Width(16)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	errorStyle := lipgloss.NewStyle().
		Foreground(styles.ColorError).
		Bold(true)

	// Calculate dialog dimensions
	dialogWidth := 70
	if p.width > 80 {
		dialogWidth = 75
	}
	sectionWidth := (dialogWidth - 8) / 2 // Two columns with padding

	// Build left section (Generation Stats)
	leftSection := p.renderGenerationSection(sectionWidth, sectionTitleStyle, labelStyle, valueStyle)

	// Build right section (Application Stats)
	rightSection := p.renderApplicationSection(sectionWidth, sectionTitleStyle, labelStyle, valueStyle)

	// Combine sections side by side
	var lines []string

	// Header
	phaseDisplay := string(pr.Phase)
	switch pr.Phase {
	case PhaseGeneration:
		phaseDisplay = "Generating Snapshot"
	case PhaseApplication:
		phaseDisplay = "Applying Snapshot"
	}

	headerText := fmt.Sprintf("Snapshot Progress: %s", phaseDisplay)
	if p.isActive() {
		headerText = p.spinner.View() + " " + headerText
	}
	lines = append(lines, titleStyle.Render(headerText))

	// Source/Target info
	if pr.SourceNode != "" || pr.TargetNode != "" {
		infoLine := ""
		if pr.SourceNode != "" {
			infoLine += fmt.Sprintf("Source: %s", pr.SourceNode)
		}
		if pr.TargetNode != "" {
			if infoLine != "" {
				infoLine += "  →  "
			}
			infoLine += fmt.Sprintf("Target: %s", pr.TargetNode)
		}
		lines = append(lines, valueStyle.Render(infoLine))
	}

	lines = append(lines, "")

	// Overall progress bar
	lines = append(lines, labelStyle.Render("Overall Progress:"))
	lines = append(lines, p.renderProgressBarLine(p.overallProgress, pr.OverallPercent))
	lines = append(lines, "")

	// Two-section layout
	combinedSections := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftSection,
		"  │  ", // Separator
		rightSection,
	)
	lines = append(lines, combinedSections)
	lines = append(lines, "")

	// Per-table progress list
	lines = append(lines, p.renderTableList(dialogWidth-6)...)
	lines = append(lines, "")

	// Throughput sparkline
	if len(pr.ThroughputHistory) > 0 {
		lines = append(lines, p.renderThroughputSparkline(dialogWidth-6)...)
		lines = append(lines, "")
	}

	// Compression ratio (if enabled)
	if pr.CompressionEnabled {
		compressionLine := p.renderCompressionInfo(labelStyle, valueStyle)
		lines = append(lines, compressionLine)
	}

	// Checksum verification status
	if pr.ChecksumsTotal > 0 || pr.ChecksumStatus != "" {
		checksumLine := p.renderChecksumInfo(labelStyle)
		lines = append(lines, checksumLine)
	}

	// Time info
	lines = append(lines, "")
	lines = append(lines, p.renderTimeInfo(labelStyle, valueStyle)...)

	// Error message
	if pr.ErrorMessage != "" {
		lines = append(lines, "")
		lines = append(lines, errorStyle.Render("Error: "+pr.ErrorMessage))
	}

	// Help text
	lines = append(lines, "")
	helpStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	if p.isActive() {
		lines = append(lines, helpStyle.Render("[C] Cancel  [j/k] Scroll tables  [Esc] Close"))
	} else {
		lines = append(lines, helpStyle.Render("[Esc] Close"))
	}

	content := strings.Join(lines, "\n")

	// Dialog box style
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(dialogWidth)

	return dialogStyle.Render(content)
}

// renderGenerationSection renders the generation statistics section.
func (p *SnapshotProgressOverlay) renderGenerationSection(width int, titleStyle, labelStyle, valueStyle lipgloss.Style) string {
	pr := p.progress
	var lines []string

	// Section title
	lines = append(lines, titleStyle.Width(width).Render("Generation Stats"))

	// Active indicator for generation phase
	if pr.Phase == PhaseGeneration {
		labelStyle = labelStyle.Width(14)
	} else {
		labelStyle = labelStyle.Width(14).Foreground(styles.ColorMuted)
		valueStyle = valueStyle.Foreground(styles.ColorMuted)
	}

	// Tables progress
	tablesLine := fmt.Sprintf("%d / %d", pr.GenTablesCompleted, pr.GenTablesTotal)
	lines = append(lines, labelStyle.Render("Tables:")+valueStyle.Render(tablesLine))

	// Rows written
	if pr.GenRowsTotal > 0 {
		rowsLine := fmt.Sprintf("%s / %s", formatNumber(pr.GenRowsWritten), formatNumber(pr.GenRowsTotal))
		lines = append(lines, labelStyle.Render("Rows:")+valueStyle.Render(rowsLine))
	}

	// Bytes written
	if pr.GenBytesTotal > 0 {
		bytesLine := fmt.Sprintf("%s / %s", formatBytes(pr.GenBytesWritten), formatBytes(pr.GenBytesTotal))
		lines = append(lines, labelStyle.Render("Size:")+valueStyle.Render(bytesLine))
	}

	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// renderApplicationSection renders the application statistics section.
func (p *SnapshotProgressOverlay) renderApplicationSection(width int, titleStyle, labelStyle, valueStyle lipgloss.Style) string {
	pr := p.progress
	var lines []string

	// Section title
	lines = append(lines, titleStyle.Width(width).Render("Application Stats"))

	// Active indicator for application phase
	if pr.Phase == PhaseApplication {
		labelStyle = labelStyle.Width(14)
	} else {
		labelStyle = labelStyle.Width(14).Foreground(styles.ColorMuted)
		valueStyle = valueStyle.Foreground(styles.ColorMuted)
	}

	// Tables progress
	tablesLine := fmt.Sprintf("%d / %d", pr.AppTablesCompleted, pr.AppTablesTotal)
	lines = append(lines, labelStyle.Render("Tables:")+valueStyle.Render(tablesLine))

	// Rows loaded
	if pr.AppRowsTotal > 0 {
		rowsLine := fmt.Sprintf("%s / %s", formatNumber(pr.AppRowsLoaded), formatNumber(pr.AppRowsTotal))
		lines = append(lines, labelStyle.Render("Rows:")+valueStyle.Render(rowsLine))
	}

	// Bytes loaded
	if pr.AppBytesTotal > 0 {
		bytesLine := fmt.Sprintf("%s / %s", formatBytes(pr.AppBytesLoaded), formatBytes(pr.AppBytesTotal))
		lines = append(lines, labelStyle.Render("Size:")+valueStyle.Render(bytesLine))
	}

	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// renderTableList renders the per-table progress list with current table highlighted.
func (p *SnapshotProgressOverlay) renderTableList(width int) []string {
	pr := p.progress
	var lines []string

	if len(pr.Tables) == 0 {
		return lines
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15"))

	lines = append(lines, titleStyle.Render("Tables"))

	// Column headers
	headerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	header := fmt.Sprintf("  %-30s %8s %12s %s", "Table", "Rows", "Size", "Status")
	lines = append(lines, headerStyle.Render(header))

	// Visible tables with scroll
	startIdx := p.tableScrollOffset
	endIdx := startIdx + p.maxVisibleTables
	if endIdx > len(pr.Tables) {
		endIdx = len(pr.Tables)
	}

	for i := startIdx; i < endIdx; i++ {
		t := pr.Tables[i]
		lines = append(lines, p.renderTableRow(t, i == pr.CurrentTableIndex, width))
	}

	// Scroll indicators
	if len(pr.Tables) > p.maxVisibleTables {
		scrollInfo := fmt.Sprintf("  (%d-%d of %d)", startIdx+1, endIdx, len(pr.Tables))
		if startIdx > 0 {
			scrollInfo = "  ↑ " + scrollInfo
		}
		if endIdx < len(pr.Tables) {
			scrollInfo = scrollInfo + " ↓"
		}
		scrollStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
		lines = append(lines, scrollStyle.Render(scrollInfo))
	}

	return lines
}

// renderTableRow renders a single table row in the progress list.
func (p *SnapshotProgressOverlay) renderTableRow(t TableProgress, isCurrent bool, _ int) string {
	// Status indicator
	var statusIcon string
	var statusColor lipgloss.Color
	switch t.Status {
	case "completed":
		statusIcon = "✓"
		statusColor = styles.ColorSuccess
	case "in_progress":
		statusIcon = "●"
		statusColor = styles.ColorAccent
	case "failed":
		statusIcon = "✗"
		statusColor = styles.ColorError
	default:
		statusIcon = "○"
		statusColor = styles.ColorMuted
	}

	// Format table name
	tableName := t.TableName
	if t.Schema != "" && t.Schema != "public" {
		tableName = t.Schema + "." + t.TableName
	}
	if len(tableName) > 30 {
		tableName = tableName[:27] + "..."
	}

	// Format rows and size
	rowsStr := "-"
	if t.RowsTotal > 0 {
		rowsStr = formatNumber(t.RowsCopied)
	}

	sizeStr := "-"
	if t.BytesTotal > 0 {
		sizeStr = formatBytes(t.BytesCopied)
	}

	// Build row with highlighting for current table
	rowStyle := lipgloss.NewStyle()
	if isCurrent {
		rowStyle = rowStyle.Bold(true).Foreground(styles.ColorAccent)
	}

	statusStyle := lipgloss.NewStyle().Foreground(statusColor)

	row := fmt.Sprintf("%s %-30s %8s %12s",
		statusStyle.Render(statusIcon),
		tableName,
		rowsStr,
		sizeStr,
	)

	if t.Status == "in_progress" && t.Percent > 0 {
		row += fmt.Sprintf(" %.0f%%", t.Percent)
	}

	return rowStyle.Render(row)
}

// renderThroughputSparkline renders the throughput sparkline with current value.
func (p *SnapshotProgressOverlay) renderThroughputSparkline(_ int) []string {
	pr := p.progress
	var lines []string

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15"))

	lines = append(lines, titleStyle.Render("Throughput (last 60s)"))

	// Render sparkline using existing infrastructure
	sparkConfig := SparklineConfig{
		Width:  50,
		Height: 1,
		Color:  lipgloss.Color("117"), // Light blue
	}

	sparkline := RenderUnicodeSparkline(pr.ThroughputHistory, sparkConfig)

	// Current throughput value
	currentStr := formatBytes(int64(pr.CurrentThroughput)) + "/s"
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	lines = append(lines, fmt.Sprintf("  %s  %s", sparkline, valueStyle.Render(currentStr)))

	return lines
}

// renderCompressionInfo renders compression ratio information.
func (p *SnapshotProgressOverlay) renderCompressionInfo(labelStyle, valueStyle lipgloss.Style) string {
	pr := p.progress

	compressionPct := pr.CompressionRatio * 100
	var compressionText string
	if compressionPct > 0 {
		savings := 100 - compressionPct
		compressionText = fmt.Sprintf("%.1f%% of original (%.1f%% savings)", compressionPct, savings)
	} else {
		compressionText = "Enabled (calculating...)"
	}

	return labelStyle.Render("Compression:") + valueStyle.Render(compressionText)
}

// renderChecksumInfo renders checksum verification status with pass/fail counts.
func (p *SnapshotProgressOverlay) renderChecksumInfo(labelStyle lipgloss.Style) string {
	pr := p.progress

	var statusText string
	var statusColor lipgloss.Color

	switch pr.ChecksumStatus {
	case "verifying":
		statusText = "Verifying..."
		statusColor = styles.ColorAccent
	case "passed":
		statusText = "Passed"
		statusColor = styles.ColorSuccess
	case "failed":
		statusText = "Failed"
		statusColor = styles.ColorError
	default:
		statusText = "Pending"
		statusColor = styles.ColorMuted
	}

	statusStyle := lipgloss.NewStyle().Foreground(statusColor)

	// Add counts
	if pr.ChecksumsTotal > 0 {
		passedStyle := lipgloss.NewStyle().Foreground(styles.ColorSuccess)
		failedStyle := lipgloss.NewStyle().Foreground(styles.ColorError)

		countsText := fmt.Sprintf(" (%s passed",
			passedStyle.Render(fmt.Sprintf("%d/%d", pr.ChecksumsVerified, pr.ChecksumsTotal)))

		if pr.ChecksumsFailed > 0 {
			countsText += fmt.Sprintf(", %s failed", failedStyle.Render(fmt.Sprintf("%d", pr.ChecksumsFailed)))
		}
		countsText += ")"

		return labelStyle.Render("Checksums:") + statusStyle.Render(statusText) + countsText
	}

	return labelStyle.Render("Checksums:") + statusStyle.Render(statusText)
}

// renderTimeInfo renders elapsed time and ETA.
func (p *SnapshotProgressOverlay) renderTimeInfo(labelStyle, valueStyle lipgloss.Style) []string {
	pr := p.progress
	var lines []string

	// Elapsed time
	if !pr.StartedAt.IsZero() {
		elapsed := time.Since(pr.StartedAt)
		lines = append(lines, labelStyle.Render("Elapsed:")+valueStyle.Render(formatDuration(elapsed)))
	}

	// ETA
	if pr.ETASeconds > 0 {
		eta := time.Duration(pr.ETASeconds) * time.Second
		lines = append(lines, labelStyle.Render("ETA:")+valueStyle.Render(formatDuration(eta)))
	} else if pr.ETASeconds == 0 && p.isActive() {
		lines = append(lines, labelStyle.Render("ETA:")+valueStyle.Render("Calculating..."))
	}

	return lines
}

// renderProgressBarLine renders an animated progress bar with percentage.
func (p *SnapshotProgressOverlay) renderProgressBarLine(prog progress.Model, percent float64) string {
	percentStyle := lipgloss.NewStyle().Width(8)
	return fmt.Sprintf("  %s %s", prog.View(), percentStyle.Render(fmt.Sprintf("%.1f%%", percent)))
}

// getCurrentTablePercent returns the current table's progress percentage.
func (p *SnapshotProgressOverlay) getCurrentTablePercent() float64 {
	if p.progress == nil || len(p.progress.Tables) == 0 {
		return 0
	}

	for _, t := range p.progress.Tables {
		if t.Status == "in_progress" {
			return t.Percent
		}
	}
	return 0
}

// isActive returns true if the snapshot operation is active.
func (p *SnapshotProgressOverlay) isActive() bool {
	if p.progress == nil {
		return false
	}
	return p.progress.Phase == PhaseGeneration || p.progress.Phase == PhaseApplication
}

// GetPhase returns the current phase being displayed.
func (p *SnapshotProgressOverlay) GetPhase() SnapshotPhase {
	if p.progress == nil {
		return PhaseIdle
	}
	return p.progress.Phase
}

// GetSnapshotID returns the snapshot ID being displayed.
func (p *SnapshotProgressOverlay) GetSnapshotID() string {
	if p.progress == nil {
		return ""
	}
	return p.progress.SnapshotID
}
