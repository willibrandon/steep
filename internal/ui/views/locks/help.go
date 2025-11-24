package locks

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay for the locks view.
func HelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Locks View Help")

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
		{"Actions", ""},
		{"d / Enter", "View lock details"},
		{"s", "Cycle sort column"},
		{"y", "Copy query to clipboard"},
		{"x", "Kill blocking process"},
		{"r", "Refresh data"},
		{"", ""},
		{"General", ""},
		{"h", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
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

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		lines,
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

// DeadlockHelpOverlay renders the help overlay for the deadlock history tab.
func DeadlockHelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Deadlock History Help")

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
		{"← / →", "Switch tabs"},
		{"", ""},
		{"Actions", ""},
		{"d / Enter", "View deadlock details"},
		{"P", "Reset log positions (re-parse logs)"},
		{"R", "Reset deadlock history (clear data)"},
		{"L", "Enable logging collector"},
		{"", ""},
		{"Detail View", ""},
		{"c", "Copy queries to clipboard"},
		{"j / k", "Scroll up/down"},
		{"g / G", "Go to top/bottom"},
		{"", ""},
		{"General", ""},
		{"h", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
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

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		lines,
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
