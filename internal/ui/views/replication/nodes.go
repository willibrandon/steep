// Package replication provides the Replication view for monitoring PostgreSQL replication.
package replication

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ClusterNode represents a node in the bidirectional replication cluster.
// This mirrors the steep_repl.nodes table structure.
type ClusterNode struct {
	NodeID          string
	NodeName        string
	Host            string
	Port            int
	Priority        int
	IsCoordinator   bool
	LastSeen        *time.Time
	Status          string // unknown, healthy, degraded, unreachable, offline
	InitState       string // uninitialized, preparing, copying, catching_up, synchronized, diverged, failed, reinitializing
	InitSourceNode  string
	InitStartedAt   *time.Time
	InitCompletedAt *time.Time

	// Progress data (from init_progress table)
	Progress *components.InitProgressData
}

// renderNodes renders the Nodes tab content showing cluster nodes and their initialization states.
func (v *ReplicationView) renderNodes() string {
	if len(v.clusterNodes) == 0 {
		return v.renderNoNodes()
	}

	return v.renderNodeTable()
}

// renderNoNodes renders the empty state for the Nodes tab.
func (v *ReplicationView) renderNoNodes() string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("No cluster nodes registered.\n\n" +
			"Cluster nodes appear here when steep-repl daemon is running\n" +
			"and nodes have been registered via the coordinator.")

	return lipgloss.Place(
		v.width, v.height-8,
		lipgloss.Center, lipgloss.Center,
		msg,
	)
}

// renderNodeTable renders the node list table with initialization states.
func (v *ReplicationView) renderNodeTable() string {
	var b strings.Builder

	// Calculate available height
	tableHeight := v.height - 8
	if tableHeight < 3 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
			Render("Terminal too small. Resize to at least 80x24.")
	}

	// Column headers
	headers := v.getNodeTableHeaders()

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
	visibleRows := min(tableHeight, len(v.clusterNodes)-v.nodeScrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.nodeScrollOffset + i
		if idx >= len(v.clusterNodes) {
			break
		}
		node := v.clusterNodes[idx]
		b.WriteString(v.renderNodeRow(node, idx == v.nodeSelectedIdx, headers))
		b.WriteString("\n")
	}

	// Wrap content
	contentHeight := v.height - 8
	content := lipgloss.NewStyle().
		Height(contentHeight).
		Render(b.String())

	return content + "\n" + v.renderFooter()
}

// renderNodeRow renders a single node row.
func (v *ReplicationView) renderNodeRow(node ClusterNode, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Status color
	statusStyle := baseStyle.Foreground(nodeStatusColor(node.Status))

	// Init state color
	initStateStyle := baseStyle.Foreground(initStateColor(node.InitState))

	// Build row
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "name":
			name := node.NodeName
			if name == "" {
				name = node.NodeID
			}
			if node.IsCoordinator {
				name = "[C] " + name
			}
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(name, h.Width), h.Width)))
		case "host":
			hostPort := fmt.Sprintf("%s:%d", node.Host, node.Port)
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(hostPort, h.Width), h.Width)))
		case "status":
			row.WriteString(statusStyle.Render(padRight(node.Status, h.Width)))
		case "priority":
			row.WriteString(baseStyle.Render(padRight(fmt.Sprintf("%d", node.Priority), h.Width)))
		case "init_state":
			stateDisplay := formatInitState(node.InitState, node.Progress)
			row.WriteString(initStateStyle.Render(padRight(truncateWithEllipsis(stateDisplay, h.Width), h.Width)))
		case "source":
			source := node.InitSourceNode
			if source == "" {
				source = "-"
			}
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(source, h.Width), h.Width)))
		case "progress":
			progressStr := formatNodeProgress(node.Progress)
			row.WriteString(baseStyle.Render(padRight(progressStr, h.Width)))
		case "eta":
			etaStr := formatNodeETA(node.InitState, node.Progress)
			row.WriteString(baseStyle.Render(padRight(etaStr, h.Width)))
		case "last_seen":
			lastSeen := "-"
			if node.LastSeen != nil {
				lastSeen = formatLastSeen(*node.LastSeen)
			}
			row.WriteString(baseStyle.Render(padRight(lastSeen, h.Width)))
		}
	}

	return row.String()
}

// getNodeTableHeaders returns headers adapted to terminal width.
// Wide mode (>140): Shows Priority, Source, Last Seen columns with flex widths
// Normal mode (85-140): Standard columns with flex Name
// Narrow mode (<85): Essential columns only
func (v *ReplicationView) getNodeTableHeaders() []ColumnConfig {
	// Fixed width columns (minimum sizes)
	const (
		hostMinWidth   = 20
		statusWidth    = 10
		initStateWidth = 15 // "Init State" + padding
		progressWidth  = 12
		etaWidth       = 10
		priorityWidth  = 10 // "Priority" (8) + 2 padding
		sourceMinWidth = 16
		lastSeenWidth  = 12
	)

	// Wide mode (>140): Add Priority, Source, Last Seen columns
	// Distribute extra space to Name, Host:Port, and Source columns
	if v.width >= 140 {
		fixedWidth := statusWidth + initStateWidth + progressWidth + etaWidth + priorityWidth + lastSeenWidth
		flexWidth := v.width - fixedWidth - 2

		// Distribute flex space: 40% Name, 30% Host:Port, 30% Source
		nameWidth := max(20, flexWidth*40/100)
		hostWidth := max(hostMinWidth, flexWidth*30/100)
		sourceWidth := max(sourceMinWidth, flexWidth*30/100)

		// Cap maximums for readability
		if nameWidth > 50 {
			nameWidth = 50
		}
		if hostWidth > 30 {
			hostWidth = 30
		}
		if sourceWidth > 30 {
			sourceWidth = 30
		}

		return []ColumnConfig{
			{"Node", nameWidth, "name"},
			{"Host:Port", hostWidth, "host"},
			{"Status", statusWidth, "status"},
			{"Priority", priorityWidth, "priority"},
			{"Init State", initStateWidth, "init_state"},
			{"Source", sourceWidth, "source"},
			{"Progress", progressWidth, "progress"},
			{"ETA", etaWidth, "eta"},
			{"Last Seen", lastSeenWidth, "last_seen"},
		}
	}

	// Normal mode (85-140): Standard columns with flex Name
	if v.width >= 85 {
		fixedWidth := hostMinWidth + statusWidth + initStateWidth + progressWidth + etaWidth
		nameWidth := max(16, v.width-fixedWidth-2)
		if nameWidth > 40 {
			nameWidth = 40
		}
		return []ColumnConfig{
			{"Node", nameWidth, "name"},
			{"Host:Port", hostMinWidth, "host"},
			{"Status", statusWidth, "status"},
			{"Init State", initStateWidth, "init_state"},
			{"Progress", progressWidth, "progress"},
			{"ETA", etaWidth, "eta"},
		}
	}

	// Narrow mode (72-85): No ETA column
	if v.width >= 72 {
		return []ColumnConfig{
			{"Node", 16, "name"},
			{"Host:Port", hostMinWidth, "host"},
			{"Status", statusWidth, "status"},
			{"Init State", initStateWidth, "init_state"},
			{"Progress", progressWidth, "progress"},
		}
	}

	// Minimum (<72): Essential columns only
	return []ColumnConfig{
		{"Node", 16, "name"},
		{"Host:Port", hostMinWidth, "host"},
		{"Status", statusWidth, "status"},
		{"Init State", initStateWidth, "init_state"},
	}
}

// nodeStatusColor returns the appropriate color for a node status.
func nodeStatusColor(status string) lipgloss.Color {
	switch status {
	case "healthy":
		return styles.ColorSuccess
	case "degraded":
		return styles.ColorAlertWarning
	case "unreachable", "offline":
		return styles.ColorError
	default:
		return styles.ColorMuted
	}
}

// initStateColor returns the appropriate color for an init state.
func initStateColor(state string) lipgloss.Color {
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

// formatInitState formats the init state with optional progress.
func formatInitState(state string, progress *components.InitProgressData) string {
	if progress == nil || !isInitializing(state) {
		return state
	}
	return fmt.Sprintf("%s %.0f%%", state, progress.OverallPercent)
}

// formatNodeProgress formats progress for display.
func formatNodeProgress(progress *components.InitProgressData) string {
	if progress == nil {
		return "-"
	}
	if progress.TablesTotal > 0 {
		return fmt.Sprintf("%d/%d", progress.TablesCompleted, progress.TablesTotal)
	}
	return fmt.Sprintf("%.1f%%", progress.OverallPercent)
}

// formatNodeETA formats ETA for display.
// During active initialization states (reinitializing, etc.), shows "0s" instead of "-"
// when ETA reaches 0 to provide smoother visual transition before completion.
func formatNodeETA(initState string, progress *components.InitProgressData) string {
	if progress == nil {
		return "-"
	}
	// During active states, show "0s" when ETA is 0 instead of "-"
	// This prevents the brief "-" display just before completion
	if progress.ETASeconds == 0 && isInitializing(initState) {
		return "0s"
	}
	if progress.ETASeconds <= 0 {
		return "-"
	}
	eta := time.Duration(progress.ETASeconds) * time.Second
	if eta < time.Minute {
		return fmt.Sprintf("%ds", int(eta.Seconds()))
	}
	if eta < time.Hour {
		return fmt.Sprintf("%dm", int(eta.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(eta.Hours()), int(eta.Minutes())%60)
}

// isInitializing returns true if the state indicates active initialization.
func isInitializing(state string) bool {
	switch state {
	case "preparing", "copying", "catching_up", "reinitializing":
		return true
	default:
		return false
	}
}

// renderNodeProgressOverlay renders the detailed progress overlay for a node.
func (v *ReplicationView) renderNodeProgressOverlay() string {
	if v.progressOverlay == nil || !v.progressOverlay.IsVisible() {
		return v.renderNodes()
	}

	// Render the overlay
	v.progressOverlay.SetSize(v.width, v.height)
	overlay := v.progressOverlay.View()

	// Center the overlay over the background
	return lipgloss.Place(
		v.width, v.height-8,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// updateClusterNodes converts models.ClusterNode slice to the view's ClusterNode slice.
func (v *ReplicationView) updateClusterNodes(nodes []models.ClusterNode) {
	// Only log when there are interesting states (not synchronized)
	for _, n := range nodes {
		if n.InitState != "synchronized" {
			logger.Debug("updateClusterNodes: node state",
				"node_id", n.NodeID,
				"node_name", n.NodeName,
				"init_state", n.InitState,
			)
		}
	}

	v.clusterNodes = make([]ClusterNode, len(nodes))
	for i, n := range nodes {
		var progress *components.InitProgressData
		if n.InitProgress != nil {
			progress = &components.InitProgressData{
				NodeID:              n.NodeID,
				NodeName:            n.NodeName,
				State:               n.InitState,
				Phase:               n.InitProgress.Phase,
				OverallPercent:      n.InitProgress.OverallPercent,
				TablesTotal:         n.InitProgress.TablesTotal,
				TablesCompleted:     n.InitProgress.TablesCompleted,
				CurrentTable:        n.InitProgress.CurrentTable,
				CurrentTablePercent: n.InitProgress.CurrentTablePercent,
				RowsCopied:          n.InitProgress.RowsCopied,
				BytesCopied:         n.InitProgress.BytesCopied,
				ThroughputRowsSec:   n.InitProgress.ThroughputRowsSec,
				ETASeconds:          n.InitProgress.ETASeconds,
				ParallelWorkers:     n.InitProgress.ParallelWorkers,
				ErrorMessage:        n.InitProgress.ErrorMessage,
				SourceNode:          n.InitSourceNode,
			}
		}
		v.clusterNodes[i] = ClusterNode{
			NodeID:          n.NodeID,
			NodeName:        n.NodeName,
			Host:            n.Host,
			Port:            n.Port,
			Priority:        n.Priority,
			IsCoordinator:   n.IsCoordinator,
			LastSeen:        n.LastSeen,
			Status:          n.Status,
			InitState:       n.InitState,
			InitSourceNode:  n.InitSourceNode,
			InitStartedAt:   n.InitStartedAt,
			InitCompletedAt: n.InitCompletedAt,
			Progress:        progress,
		}
	}
	// Ensure selection is valid
	if v.nodeSelectedIdx >= len(v.clusterNodes) {
		v.nodeSelectedIdx = max(0, len(v.clusterNodes)-1)
	}
	v.ensureNodeVisible()

	// T043: Update progress overlay if visible with fresh data
	if v.progressOverlay != nil && v.progressOverlay.IsVisible() {
		nodeID := v.progressOverlay.GetNodeID()
		for _, node := range v.clusterNodes {
			if node.NodeID == nodeID {
				progressData := v.buildNodeProgressData(node)
				v.progressOverlay.SetProgress(progressData)
				break
			}
		}
	}
}

// buildNodeProgressData creates InitProgressData from a ClusterNode.
// Works for any node state, not just actively initializing nodes.
func (v *ReplicationView) buildNodeProgressData(node ClusterNode) *components.InitProgressData {
	// If node has active progress data, use it
	if node.Progress != nil {
		return node.Progress
	}

	// Build basic progress data from node info
	data := &components.InitProgressData{
		NodeID:     node.NodeID,
		NodeName:   node.NodeName,
		State:      node.InitState,
		SourceNode: node.InitSourceNode,
	}

	// Set phase and percent based on state
	switch node.InitState {
	case "synchronized":
		data.Phase = "complete"
		data.OverallPercent = 100
	case "uninitialized":
		data.Phase = ""
		data.OverallPercent = 0
	case "failed":
		data.Phase = "failed"
		data.OverallPercent = 0
	case "diverged":
		data.Phase = "diverged"
		data.OverallPercent = 0
	default:
		data.Phase = node.InitState
	}

	// Set started time if available
	if node.InitStartedAt != nil {
		data.StartedAt = *node.InitStartedAt
	}

	return data
}

// formatLastSeen formats the last seen time relative to now.
func formatLastSeen(t time.Time) string {
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
