// Package repviz provides visualization components for replication monitoring.
package repviz

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// TopologyConfig holds configuration for topology rendering.
type TopologyConfig struct {
	Width       int
	Height      int
	IsPrimary   bool
	SelectedIdx int             // Currently selected replica index (for highlighting)
	Expanded    map[string]bool // Which replicas have expanded pipeline (by app name)
}

// treeNode represents a node in our custom tree structure.
type treeNode struct {
	replica  *models.Replica
	children []*treeNode
	index    int // Global index for selection tracking
}

// RenderTopology creates an ASCII tree diagram showing primary/replica relationships
// including cascading replication support with expandable WAL pipeline per node.
// T036: Implement topology tree rendering using xlab/treeprint showing primary at root
// T037: Add support for cascading replication (replica-to-replica chains)
// T038: Display sync state indicators (sync, async, potential, quorum) on each node
// T039: Show lag bytes next to each replica node in topology
func RenderTopology(data *models.ReplicationData, config TopologyConfig) string {
	var b strings.Builder

	// Header (with leading newline for spacing from tab bar)
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Replication Topology"))
	b.WriteString("\n\n")

	if !data.IsPrimary {
		// Connected to standby - show upstream info
		return renderStandbyTopology(data, config)
	}

	if len(data.Replicas) == 0 {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("No replicas connected."))
		b.WriteString("\n\n")
		b.WriteString(renderTopologyFooter(config))
		return b.String()
	}

	// Build tree structure
	root := buildTopologyTree(data.Replicas)

	// Render PRIMARY node
	b.WriteString(formatPrimaryNode())
	b.WriteString("\n")

	// Render children with custom tree drawing
	renderTreeNodes(&b, root, config, "", true)

	b.WriteString("\n")

	// Legend
	b.WriteString(renderLegend())
	b.WriteString("\n\n")

	// Footer
	b.WriteString(renderTopologyFooter(config))
	return b.String()
}

// buildTopologyTree constructs the tree structure from flat replica list.
func buildTopologyTree(replicas []models.Replica) []*treeNode {
	// Build cascading topology
	directReplicas := make([]*treeNode, 0)
	cascadeMap := make(map[string][]*treeNode) // upstream -> children

	idx := 0
	replicaNodes := make(map[string]*treeNode)

	// First pass: create all nodes
	for i := range replicas {
		node := &treeNode{
			replica: &replicas[i],
			index:   idx,
		}
		replicaNodes[replicas[i].ApplicationName] = node
		idx++
	}

	// Second pass: build hierarchy
	for i := range replicas {
		r := &replicas[i]
		node := replicaNodes[r.ApplicationName]

		if r.Upstream == "" {
			directReplicas = append(directReplicas, node)
		} else {
			cascadeMap[r.Upstream] = append(cascadeMap[r.Upstream], node)
		}
	}

	// Third pass: attach children
	for _, node := range replicaNodes {
		if children, ok := cascadeMap[node.replica.ApplicationName]; ok {
			node.children = children
		}
	}

	return directReplicas
}

// renderTreeNodes recursively renders tree nodes with expandable pipelines.
func renderTreeNodes(b *strings.Builder, nodes []*treeNode, config TopologyConfig, prefix string, isRoot bool) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Draw tree branch
		var branch, childPrefix string
		if isLast {
			branch = "└── "
			childPrefix = prefix + "    "
		} else {
			branch = "├── "
			childPrefix = prefix + "│   "
		}

		// Format node with selection highlight
		nodeLabel := formatReplicaNodeWithState(node.replica, node.index, config)

		b.WriteString(prefix)
		b.WriteString(branch)
		b.WriteString(nodeLabel)
		b.WriteString("\n")

		// Check if this node is expanded - show inline pipeline
		if config.Expanded != nil && config.Expanded[node.replica.ApplicationName] {
			renderInlinePipeline(b, node.replica, childPrefix)
		}

		// Render children
		if len(node.children) > 0 {
			renderTreeNodes(b, node.children, config, childPrefix, false)
		}
	}
}

// formatReplicaNodeWithState creates a formatted label with selection/expansion indicators.
func formatReplicaNodeWithState(r *models.Replica, idx int, config TopologyConfig) string {
	// Determine sync state indicator and color
	syncIndicator, syncColor := getSyncStateIndicator(r.SyncState)

	// Format lag with severity color
	lagStr := formatLagBytes(r.ByteLag)
	lagColor := getLagColor(r.GetLagSeverity())

	// State indicator
	stateIcon := "○"
	if r.State == "streaming" {
		stateIcon = "●"
	}

	// Expansion indicator
	expandIcon := "▶" // collapsed
	if config.Expanded != nil && config.Expanded[r.ApplicationName] {
		expandIcon = "▼" // expanded
	}

	// Selection highlight
	isSelected := idx == config.SelectedIdx

	// Build the node label
	syncStyle := lipgloss.NewStyle().Foreground(syncColor)
	lagStyle := lipgloss.NewStyle().Foreground(lagColor)

	label := fmt.Sprintf("%s %s %s %s [%s]",
		expandIcon,
		stateIcon,
		r.ApplicationName,
		syncStyle.Render(fmt.Sprintf("(%s)", syncIndicator)),
		lagStyle.Render(lagStr),
	)

	if isSelected {
		selectStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Bold(true)
		return selectStyle.Render(label)
	}

	return label
}

// renderInlinePipeline renders a compact WAL pipeline below the node.
func renderInlinePipeline(b *strings.Builder, replica *models.Replica, prefix string) {
	stages := calculatePipelineStages(replica)

	// Compact pipeline visualization with consistent column widths
	// Column width: 8 chars for content, 3 chars for arrow separator
	const colWidth = 8

	pipelineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Stage names line: Sent ───▶ Write ───▶ Flush ───▶ Replay
	b.WriteString(prefix)
	b.WriteString(pipelineStyle.Render("│ "))

	for i, s := range stages {
		name := fmt.Sprintf("%-*s", colWidth, s.Name)
		b.WriteString(stageStyle.Render(name))
		if i < len(stages)-1 {
			b.WriteString(arrowStyle.Render("─▶ "))
		}
	}
	b.WriteString("\n")

	// LSN values line
	b.WriteString(prefix)
	b.WriteString(pipelineStyle.Render("│ "))

	for i, s := range stages {
		lsn := truncateLSNCompact(s.LSN, colWidth)
		b.WriteString(lsnStyle.Render(fmt.Sprintf("%-*s", colWidth, lsn)))
		if i < len(stages)-1 {
			b.WriteString("   ") // Same width as arrow
		}
	}
	b.WriteString("\n")

	// Lag indicators line
	b.WriteString(prefix)
	b.WriteString(pipelineStyle.Render("│ "))

	for i, s := range stages {
		var indicator string
		if i == 0 {
			indicator = goodStyle.Render(fmt.Sprintf("%-*s", colWidth, "● cur"))
		} else {
			indicator = formatCompactLag(s, colWidth)
		}
		b.WriteString(indicator)
		if i < len(stages)-1 {
			b.WriteString("   ")
		}
	}
	b.WriteString("\n")

	// Bottom border
	b.WriteString(prefix)
	b.WriteString(pipelineStyle.Render("│"))
	b.WriteString("\n")
}

// truncateLSNCompact truncates LSN to fit in width.
func truncateLSNCompact(lsn string, width int) string {
	if len(lsn) <= width {
		return lsn
	}
	// Show "X/...end" format
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return lsn[:width]
	}
	// Keep prefix and last few chars of offset
	prefix := parts[0] + "/"
	remaining := width - len(prefix)
	if remaining < 3 {
		return lsn[:width]
	}
	return prefix + parts[1][len(parts[1])-(remaining):]
}

// formatCompactLag returns a compact lag indicator string with proper width.
func formatCompactLag(stage PipelineStage, width int) string {
	bytes := stage.BytesLag
	if bytes < 0 {
		bytes = -bytes
	}

	lagStr := formatCompactBytes(bytes)

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

	// Format: "✓ 0B    " padded to width
	content := fmt.Sprintf("%s %s", icon, lagStr)
	padded := fmt.Sprintf("%-*s", width, content)
	return style.Render(padded)
}

// formatCompactBytes formats bytes compactly.
func formatCompactBytes(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.0fG", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.0fM", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0fK", float64(bytes)/float64(kb))
	case bytes == 0:
		return "0B"
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// renderStandbyTopology renders topology view when connected to a standby.
func renderStandbyTopology(data *models.ReplicationData, config TopologyConfig) string {
	var b strings.Builder

	// Header (with leading newline for spacing from tab bar)
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Replication Topology (Standby View)"))
	b.WriteString("\n\n")

	if data.WALReceiverStatus != nil {
		wal := data.WALReceiverStatus
		primaryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
		b.WriteString(primaryStyle.Render(fmt.Sprintf("● PRIMARY %s:%d", wal.SenderHost, wal.SenderPort)))
		b.WriteString("\n")

		// Show this standby as child
		lagStr := formatLagBytes(wal.LagBytes)
		standbyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
		b.WriteString("└── ")
		b.WriteString(standbyStyle.Render(fmt.Sprintf("THIS STANDBY (%s lag)", lagStr)))
		b.WriteString("\n")
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
			Render("Unable to determine upstream primary"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(renderTopologyFooter(config))
	return b.String()
}

// formatPrimaryNode creates the label for the primary server root node.
func formatPrimaryNode() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("42")) // Green
	return style.Render("● PRIMARY")
}

// getSyncStateIndicator returns the display text and color for a sync state.
func getSyncStateIndicator(state models.ReplicationSyncState) (string, lipgloss.Color) {
	switch state {
	case models.SyncStateSync:
		return "sync", lipgloss.Color("42") // Green
	case models.SyncStateAsync:
		return "async", lipgloss.Color("246") // Gray
	case models.SyncStatePotential:
		return "potential", lipgloss.Color("214") // Yellow/Orange
	case models.SyncStateQuorum:
		return "quorum", lipgloss.Color("39") // Cyan
	default:
		return "unknown", lipgloss.Color("241")
	}
}

// getLagColor returns the appropriate color for a lag severity level.
func getLagColor(severity models.LagSeverity) lipgloss.Color {
	switch severity {
	case models.LagSeverityHealthy:
		return lipgloss.Color("42") // Green
	case models.LagSeverityWarning:
		return lipgloss.Color("214") // Yellow/Orange
	case models.LagSeverityCritical:
		return lipgloss.Color("196") // Red
	default:
		return lipgloss.Color("246")
	}
}

// formatLagBytes formats byte lag as human-readable string.
func formatLagBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// renderLegend creates a legend explaining the topology symbols.
func renderLegend() string {
	legendStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	legend := []string{
		"Legend:",
		"  ▶ collapsed  ▼ expanded  ● streaming  ○ other",
		"  (sync) synchronous  (async) asynchronous",
		"  [lag] replication lag in bytes",
	}

	return legendStyle.Render(strings.Join(legend, "\n"))
}

// renderTopologyFooter renders the navigation hint footer.
func renderTopologyFooter(config TopologyConfig) string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	return footerStyle.Render("[j/k/scroll]navigate [enter/space/click]toggle [a]expand all [esc/q]back")
}
