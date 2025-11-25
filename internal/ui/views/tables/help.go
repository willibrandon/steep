package tables

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay for the tables view.
func HelpOverlay(width, height int, pgstattupleAvailable bool) string {
	title := styles.HelpTitleStyle.Render("Tables View Help")

	keyBindings := []struct {
		key  string
		desc string
	}{
		{"Navigation", ""},
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"g / Home", "Go to top"},
		{"G / End", "Go to bottom"},
		{"Ctrl+d / PgDn", "Page down"},
		{"Ctrl+u / PgUp", "Page up"},
		{"", ""},
		{"Tree", ""},
		{"Enter", "Expand/collapse schema, open table details"},
		{"→ / l", "Expand schema or partitions"},
		{"←", "Collapse or move to parent"},
		{"P", "Toggle system schemas"},
		{"", ""},
		{"Index Panel", ""},
		{"i", "Toggle focus between tables/indexes"},
		{"y", "Copy table or index name"},
		{"", ""},
		{"Sorting", ""},
		{"s", "Cycle sort column"},
		{"S", "Toggle sort direction"},
		{"", ""},
		{"Details Panel", ""},
		{"d / Enter", "Open table details"},
		{"j / k", "Scroll details"},
		{"y", "Open SQL copy menu"},
		{"Esc / q", "Close details"},
		{"", ""},
		{"Copy Menu (in details)", ""},
		{"n", "Copy table name"},
		{"s", "Copy SELECT query"},
		{"i", "Copy INSERT template"},
		{"u", "Copy UPDATE template"},
		{"d", "Copy DELETE template"},
		{"", ""},
		{"General", ""},
		{"r", "Refresh data"},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay"},
	}

	// Calculate max key width for alignment
	maxKeyWidth := 0
	for _, kb := range keyBindings {
		if len(kb.key) > maxKeyWidth {
			maxKeyWidth = len(kb.key)
		}
	}

	var lines string
	for _, kb := range keyBindings {
		if kb.key == "" && kb.desc == "" {
			lines += "\n"
			continue
		}
		if kb.desc == "" {
			// Section header
			lines += styles.HelpTitleStyle.Render(kb.key) + "\n"
			continue
		}
		key := styles.HelpKeyStyle.Render(padRight(kb.key, maxKeyWidth+2))
		desc := styles.HelpDescStyle.Render(kb.desc)
		lines += key + desc + "\n"
	}

	// Add pgstattuple status
	var pgstatStatus string
	if pgstattupleAvailable {
		pgstatStatus = lipgloss.NewStyle().Foreground(styles.ColorSuccess).Render("✓") + " pgstattuple installed"
	} else {
		pgstatStatus = lipgloss.NewStyle().Foreground(styles.ColorMuted).Render("✗") + " pgstattuple not installed (estimated bloat)"
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		lines,
		pgstatStatus,
	)

	dialog := styles.HelpDialogStyle.Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		dialog,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}
