package replication

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// renderSetup renders the Setup tab content.
func (v *ReplicationView) renderSetup() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(headerStyle.Render("Setup Wizards"))
	b.WriteString("\n\n")

	items := []struct {
		key  string
		name string
		desc string
	}{
		{"1", "Physical Replication", "Set up streaming replication with pg_basebackup"},
		{"2", "Logical Replication", "Create publications and subscriptions"},
		{"3", "Connection Builder", "Generate primary_conninfo connection strings"},
		{"c", "Configuration Checker", "Verify PostgreSQL settings for replication"},
	}

	for _, item := range items {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			hintStyle.Render(fmt.Sprintf("[%s]", item.key)),
			itemStyle.Render(item.name)))
		b.WriteString(fmt.Sprintf("      %s\n\n",
			hintStyle.Render(item.desc)))
	}

	if v.readOnly {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render("Note: Setup wizards are disabled in read-only mode"))
	}

	return b.String()
}
