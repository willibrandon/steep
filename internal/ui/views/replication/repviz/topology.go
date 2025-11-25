// Package repviz provides visualization components for replication monitoring.
package repviz

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/xlab/treeprint"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// TopologyConfig holds configuration for topology rendering.
type TopologyConfig struct {
	Width    int
	Height   int
	IsPrimary bool
}

// RenderTopology creates an ASCII tree diagram showing primary/replica relationships
// including cascading replication support.
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
		b.WriteString(renderFooter())
		return b.String()
	}

	// Create tree with PRIMARY as root
	tree := treeprint.NewWithRoot(formatPrimaryNode())

	// Build cascading topology
	// First, find all direct replicas (no upstream or upstream is this primary)
	directReplicas := make([]models.Replica, 0)
	cascadeReplicas := make(map[string][]models.Replica) // upstream -> replicas

	for _, r := range data.Replicas {
		if r.Upstream == "" {
			directReplicas = append(directReplicas, r)
		} else {
			cascadeReplicas[r.Upstream] = append(cascadeReplicas[r.Upstream], r)
		}
	}

	// Add direct replicas and their cascading children
	for _, r := range directReplicas {
		addReplicaToTree(tree, r, cascadeReplicas, 0)
	}

	// Render the tree
	b.WriteString(tree.String())
	b.WriteString("\n")

	// Legend
	b.WriteString(renderLegend())
	b.WriteString("\n\n")

	// Footer
	b.WriteString(renderFooter())
	return b.String()
}

// renderStandbyTopology renders topology view when connected to a standby.
func renderStandbyTopology(data *models.ReplicationData, config TopologyConfig) string {
	var b strings.Builder

	// Header (with leading newline for spacing from tab bar)
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Replication Topology (Standby View)"))
	b.WriteString("\n\n")

	tree := treeprint.New()

	if data.WALReceiverStatus != nil {
		wal := data.WALReceiverStatus
		primaryLabel := fmt.Sprintf("PRIMARY %s:%d", wal.SenderHost, wal.SenderPort)
		primaryBranch := tree.AddBranch(primaryLabel)

		// Show this standby as child
		lagStr := formatLagBytes(wal.LagBytes)
		standbyLabel := fmt.Sprintf("THIS STANDBY (%s lag)", lagStr)
		primaryBranch.AddNode(standbyLabel)
	} else {
		tree.AddNode("Unable to determine upstream primary")
	}

	b.WriteString(tree.String())
	b.WriteString("\n\n")
	b.WriteString(renderFooter())
	return b.String()
}

// addReplicaToTree recursively adds a replica and its cascading children to the tree.
func addReplicaToTree(parent treeprint.Tree, replica models.Replica, cascadeMap map[string][]models.Replica, depth int) {
	// Prevent infinite recursion
	if depth > 10 {
		return
	}

	nodeLabel := formatReplicaNode(replica)
	branch := parent.AddBranch(nodeLabel)

	// Check for cascading replicas (replicas that have this replica as upstream)
	if children, ok := cascadeMap[replica.ApplicationName]; ok {
		for _, child := range children {
			addReplicaToTree(branch, child, cascadeMap, depth+1)
		}
	}
}

// formatPrimaryNode creates the label for the primary server root node.
func formatPrimaryNode() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("42")) // Green
	return style.Render("● PRIMARY")
}

// formatReplicaNode creates a formatted label for a replica node.
// T038: Display sync state indicators (sync, async, potential, quorum) on each node
// T039: Show lag bytes next to each replica node
func formatReplicaNode(r models.Replica) string {
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

	// Build the node label
	// Format: ● replica-name (sync) [1.2 MB]
	syncStyle := lipgloss.NewStyle().Foreground(syncColor)
	lagStyle := lipgloss.NewStyle().Foreground(lagColor)

	label := fmt.Sprintf("%s %s %s [%s]",
		stateIcon,
		r.ApplicationName,
		syncStyle.Render(fmt.Sprintf("(%s)", syncIndicator)),
		lagStyle.Render(lagStr),
	)

	return label
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
		"  ● streaming  ○ other state",
		"  (sync) synchronous  (async) asynchronous",
		"  (potential) potential sync  (quorum) quorum",
		"  [lag] replication lag in bytes",
	}

	return legendStyle.Render(strings.Join(legend, "\n"))
}

// renderFooter renders the navigation hint footer.
func renderFooter() string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	return footerStyle.Render("[esc/q]back")
}
