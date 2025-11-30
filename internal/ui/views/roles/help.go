package roles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay for the roles view.
func HelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Roles View Help")

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
		{"Enter", "View role details"},
		{"c", "Create new role"},
		{"x", "Drop selected role"},
		{"a", "Alter selected role"},
		{"s", "Cycle sort column"},
		{"S", "Toggle sort direction"},
		{"y", "Copy role name to clipboard"},
		{"r", "Refresh data"},
		{"", ""},
		{"Attribute Display Codes", ""},
		{"S (in attrs)", "Superuser"},
		{"L (in attrs)", "Can Login"},
		{"R (in attrs)", "Can Create Role"},
		{"D (in attrs)", "Can Create Database"},
		{"B (in attrs)", "Bypass Row-Level Security"},
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

// padRight pads a string to the specified width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
