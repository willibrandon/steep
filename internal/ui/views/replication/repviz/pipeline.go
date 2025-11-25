// Package repviz provides replication visualization components.
package repviz

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/db/models"
)

// MinimalLagThreshold is the byte difference below which a stage is considered "caught up"
const MinimalLagThreshold = 1024 * 1024 // 1MB

// Styles for pipeline visualization
var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	stageStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	lsnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	goodStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	criticalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	arrowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	tagGood       = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("255"))
	tagWarn       = lipgloss.NewStyle().Background(lipgloss.Color("136")).Foreground(lipgloss.Color("16"))
	tagCrit       = lipgloss.NewStyle().Background(lipgloss.Color("124")).Foreground(lipgloss.Color("255"))
)

// PipelineStage represents a single stage in the WAL pipeline.
type PipelineStage struct {
	Name      string
	LSN       string
	BytesLag  int64 // Bytes behind previous stage
	Severity  models.LagSeverity
}

// RenderWALPipeline renders a visual representation of the WAL pipeline for a replica.
func RenderWALPipeline(replica *models.Replica, width int) string {
	var b strings.Builder

	stages := calculatePipelineStages(replica)
	allCaughtUp := isAllCaughtUp(stages)

	// Calculate pipeline width: 4 boxes + 3 arrow gaps
	// Box: ┌ + 12 dashes + ┐ = 14 chars
	// Arrow: " ───▶ " = 6 chars
	pipelineWidth := 4*14 + 3*6 // 74 chars

	// Header
	b.WriteString(headerStyle.Render("WAL Pipeline"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", pipelineWidth)))
	b.WriteString("\n\n")

	// Pipeline flow diagram
	//
	// ┌──────────┐      ┌──────────┐      ┌──────────┐      ┌──────────┐
	// │   Sent   │ ───▶ │  Write   │ ───▶ │  Flush   │ ───▶ │  Replay  │
	// │ 0/300060 │      │ 0/300060 │      │ 0/300060 │      │ 0/2FFFF0 │
	// └──────────┘      └──────────┘      └──────────┘      └──────────┘
	//                      ✓ 0 B             ✓ 0 B            ⚠ 112 B

	// Top border
	b.WriteString(renderPipelineRow(stages, "top"))
	b.WriteString("\n")

	// Stage names
	b.WriteString(renderPipelineRow(stages, "name"))
	b.WriteString("\n")

	// LSN values
	b.WriteString(renderPipelineRow(stages, "lsn"))
	b.WriteString("\n")

	// Bottom border
	b.WriteString(renderPipelineRow(stages, "bottom"))
	b.WriteString("\n")

	// Lag indicators
	b.WriteString(renderPipelineRow(stages, "lag"))
	b.WriteString("\n\n")

	// Status summary
	if allCaughtUp {
		b.WriteString(goodStyle.Render("● Replica fully caught up"))
	} else {
		// Find the bottleneck
		bottleneck := findBottleneck(stages)
		if bottleneck != "" {
			b.WriteString(warnStyle.Render(fmt.Sprintf("○ Bottleneck: %s stage", bottleneck)))
		} else {
			b.WriteString(dimStyle.Render("○ Pipeline has lag"))
		}
	}

	return b.String()
}

// renderPipelineRow renders a single row of the pipeline diagram.
func renderPipelineRow(stages []PipelineStage, rowType string) string {
	var parts []string
	boxWidth := 12

	for i, stage := range stages {
		var segment string

		switch rowType {
		case "top":
			segment = "┌" + strings.Repeat("─", boxWidth) + "┐"

		case "name":
			name := centerText(stage.Name, boxWidth)
			segment = "│" + stageStyle.Render(name) + "│"

		case "lsn":
			lsn := truncateLSN(stage.LSN, boxWidth)
			lsn = centerText(lsn, boxWidth)
			segment = "│" + lsnStyle.Render(lsn) + "│"

		case "bottom":
			segment = "└" + strings.Repeat("─", boxWidth) + "┘"

		case "lag":
			// Center raw text first, then apply styling
			lagWidth := boxWidth + 2
			if i == 0 {
				// First stage (Sent) - reference point
				// "● current" = icon(1) + space(1) + "current"(7) = 9 display chars
				content := "● current"
				displayLen := 9
				leftPad := (lagWidth - displayLen) / 2
				rightPad := lagWidth - displayLen - leftPad
				segment = strings.Repeat(" ", leftPad) + goodStyle.Render(content) + strings.Repeat(" ", rightPad)
			} else {
				segment = formatLagIndicatorCentered(stage, lagWidth)
			}
		}

		parts = append(parts, segment)

		// Add arrow between stages (except after last)
		if i < len(stages)-1 {
			switch rowType {
			case "top", "bottom", "lag":
				parts = append(parts, "      ")
			case "name", "lsn":
				parts = append(parts, arrowStyle.Render(" ───▶ "))
			}
		}
	}

	return strings.Join(parts, "")
}

// formatLagIndicatorCentered returns a centered, colored lag indicator string.
func formatLagIndicatorCentered(stage PipelineStage, width int) string {
	bytes := stage.BytesLag
	if bytes < 0 {
		bytes = -bytes
	}

	lagStr := models.FormatBytes(bytes)

	// Use fixed-width format: icon + space + value, then pad to width
	// Account for unicode icon width (displays as 1 char but may be multi-byte)
	var icon string
	var style lipgloss.Style

	switch stage.Severity {
	case models.LagSeverityHealthy:
		icon = "✓"
		style = goodStyle
	case models.LagSeverityWarning:
		icon = "⚠"
		style = warnStyle
	case models.LagSeverityCritical:
		icon = "✗"
		style = criticalStyle
	default:
		icon = "?"
		style = dimStyle
	}

	// Format: "  ✓ 0 B     " (left-pad 2, icon, space, value, right-pad to width)
	content := icon + " " + lagStr
	displayLen := 1 + 1 + len(lagStr) // icon(1) + space(1) + value
	leftPad := (width - displayLen) / 2
	rightPad := width - displayLen - leftPad

	if leftPad < 0 {
		leftPad = 0
	}
	if rightPad < 0 {
		rightPad = 0
	}

	return strings.Repeat(" ", leftPad) + style.Render(content) + strings.Repeat(" ", rightPad)
}

// findBottleneck returns the name of the stage with the most lag.
func findBottleneck(stages []PipelineStage) string {
	var maxLag int64
	var bottleneck string

	for i, stage := range stages {
		if i == 0 {
			continue
		}
		lag := stage.BytesLag
		if lag < 0 {
			lag = -lag
		}
		if lag > maxLag {
			maxLag = lag
			bottleneck = stage.Name
		}
	}

	if maxLag > MinimalLagThreshold {
		return bottleneck
	}
	return ""
}

// calculatePipelineStages computes the stages and their byte differences.
func calculatePipelineStages(replica *models.Replica) []PipelineStage {
	sentBytes := parseLSNToBytes(replica.SentLSN)
	writeBytes := parseLSNToBytes(replica.WriteLSN)
	flushBytes := parseLSNToBytes(replica.FlushLSN)
	replayBytes := parseLSNToBytes(replica.ReplayLSN)

	writeLag := sentBytes - writeBytes
	flushLag := writeBytes - flushBytes
	replayLag := flushBytes - replayBytes

	return []PipelineStage{
		{
			Name:     "Sent",
			LSN:      replica.SentLSN,
			BytesLag: 0,
			Severity: models.LagSeverityHealthy,
		},
		{
			Name:     "Write",
			LSN:      replica.WriteLSN,
			BytesLag: writeLag,
			Severity: getLagSeverity(writeLag),
		},
		{
			Name:     "Flush",
			LSN:      replica.FlushLSN,
			BytesLag: flushLag,
			Severity: getLagSeverity(flushLag),
		},
		{
			Name:     "Replay",
			LSN:      replica.ReplayLSN,
			BytesLag: replayLag,
			Severity: getLagSeverity(replayLag),
		},
	}
}

// parseLSNToBytes converts an LSN string (e.g., "0/3000060") to a byte position.
// LSN format is "logfile/offset" where both parts are hex numbers.
// The full 64-bit position is (logfile << 32) + offset.
func parseLSNToBytes(lsn string) int64 {
	if lsn == "" || lsn == "0/0" {
		return 0
	}

	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0
	}

	var logfile, offset int64
	fmt.Sscanf(parts[0], "%x", &logfile)
	fmt.Sscanf(parts[1], "%x", &offset)

	return (logfile << 32) + offset
}

// getLagSeverity determines severity based on byte lag.
func getLagSeverity(bytes int64) models.LagSeverity {
	if bytes < 0 {
		bytes = -bytes
	}

	const (
		oneMB = 1024 * 1024
		tenMB = 10 * oneMB
	)
	switch {
	case bytes < oneMB:
		return models.LagSeverityHealthy
	case bytes < tenMB:
		return models.LagSeverityWarning
	default:
		return models.LagSeverityCritical
	}
}

// isAllCaughtUp returns true if all pipeline stages have minimal lag.
func isAllCaughtUp(stages []PipelineStage) bool {
	for i, stage := range stages {
		if i == 0 {
			continue
		}
		lag := stage.BytesLag
		if lag < 0 {
			lag = -lag
		}
		if lag > MinimalLagThreshold {
			return false
		}
	}
	return true
}

// centerText centers text within a given width.
func centerText(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	leftPad := (width - len(s)) / 2
	rightPad := width - len(s) - leftPad
	return strings.Repeat(" ", leftPad) + s + strings.Repeat(" ", rightPad)
}

// truncateLSN truncates an LSN to fit within maxLen, showing the most significant part.
func truncateLSN(lsn string, maxLen int) string {
	if len(lsn) <= maxLen {
		return lsn
	}
	// For LSN like "0/1A2B3C4D", keep the format but truncate offset
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return lsn[:maxLen]
	}
	// Try to fit: "X/..." or truncate offset
	prefix := parts[0] + "/"
	remaining := maxLen - len(prefix)
	if remaining < 4 {
		return lsn[:maxLen]
	}
	return prefix + parts[1][:remaining-2] + ".."
}
