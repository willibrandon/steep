package replication

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// renderTopology renders the topology view.
func (v *ReplicationView) renderTopology() string {
	// Basic topology rendering - will be enhanced in US3
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Replication Topology"))
	b.WriteString("\n\n")

	if len(v.data.Replicas) == 0 {
		b.WriteString("No replicas connected.\n")
		b.WriteString("\nPress t or Esc to return")
		return b.String()
	}

	// Simple tree representation
	b.WriteString("PRIMARY\n")
	for i, r := range v.data.Replicas {
		prefix := "├── "
		if i == len(v.data.Replicas)-1 {
			prefix = "└── "
		}
		lagStr := r.FormatByteLag()
		syncStr := r.SyncState.String()
		b.WriteString(fmt.Sprintf("%s%s (%s, %s lag)\n", prefix, r.ApplicationName, syncStr, lagStr))
	}

	b.WriteString("\nPress t or Esc to return")
	return b.String()
}
