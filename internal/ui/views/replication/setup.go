package replication

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views/replication/setup"
)

// renderSetup renders the Setup tab content.
func (v *ReplicationView) renderSetup() string {
	// If in config check mode, render the config checker
	if v.mode == ModeConfigCheck {
		return v.renderConfigCheck()
	}

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
		{"p", "Physical Replication", "Set up streaming replication with pg_basebackup"},
		{"o", "Logical Replication", "Create publications and subscriptions"},
		{"n", "Connection Builder", "Generate primary_conninfo connection strings"},
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

// renderConfigCheck renders the configuration checker panel.
// T045: Integrate configuration checker into Setup tab
func (v *ReplicationView) renderConfigCheck() string {
	cfg := setup.ConfigCheckConfig{
		Width:  v.width,
		Height: v.height,
	}
	return setup.RenderConfigCheck(v.data.Config, cfg)
}

// renderPhysicalWizard renders the physical replication setup wizard.
// T054: Integrate physical wizard into Setup tab
func (v *ReplicationView) renderPhysicalWizard() string {
	if v.physicalWizard == nil {
		return ""
	}
	cfg := setup.PhysicalWizardRenderConfig{
		Width:    v.width,
		Height:   v.height,
		ReadOnly: v.readOnly,
	}
	return setup.RenderPhysicalWizard(v.physicalWizard, cfg)
}

// initPhysicalWizard initializes the physical wizard state.
func (v *ReplicationView) initPhysicalWizard() {
	// Extract host and port from connection info
	host := "localhost"
	port := "5432"
	// Parse from connectionInfo if available (format like "postgres@localhost:5432/dbname")
	if v.connectionInfo != "" {
		// Simple extraction - assumes format user@host:port/db
		parts := strings.Split(v.connectionInfo, "@")
		if len(parts) > 1 {
			hostPart := strings.Split(parts[1], "/")[0]
			hostPort := strings.Split(hostPart, ":")
			host = hostPort[0]
			if len(hostPort) > 1 {
				port = hostPort[1]
			}
		}
	}
	v.physicalWizard = setup.NewPhysicalWizardState(host, port)
}
