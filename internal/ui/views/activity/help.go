package activity

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay for the activity view.
func HelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Activity View Help")

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
		{"Sorting", ""},
		{"s", "Cycle sort column (PID → User → Database → State → Duration)"},
		{"S", "Toggle sort direction (↓ desc / ↑ asc)"},
		{"", ""},
		{"Filtering", ""},
		{"/", "Enter filter mode"},
		{"a", "Toggle show all databases"},
		{"C", "Clear all filters"},
		{"", ""},
		{"Actions", ""},
		{"d / Enter", "View connection details"},
		{"c", "Cancel query (in detail view)"},
		{"x", "Terminate connection (in detail view)"},
		{"y", "Copy query to clipboard"},
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
		key := styles.HelpKeyStyle.Render(padRightHelp(kb.key, maxKeyWidth+2))
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

// padRightHelp pads a string to the specified width.
func padRightHelp(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + spaces(width-len(s))
}

// spaces returns a string of n spaces.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
