package replication

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay for the replication view.
func HelpOverlay(width, height int, activeTab ViewTab) string {
	switch activeTab {
	case TabSlots:
		return SlotsHelpOverlay(width, height)
	case TabLogical:
		return LogicalHelpOverlay(width, height)
	case TabSetup:
		return SetupHelpOverlay(width, height)
	default:
		return OverviewHelpOverlay(width, height)
	}
}

// OverviewHelpOverlay renders help for the Overview tab.
func OverviewHelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Replication Overview Help")

	keyBindings := []struct {
		key  string
		desc string
	}{
		{"Navigation", ""},
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"g / Home", "Go to top"},
		{"G / End", "Go to bottom"},
		{"Tab / → / ←", "Switch tabs"},
		{"", ""},
		{"Actions", ""},
		{"d / Enter", "View replica details"},
		{"t", "Toggle topology view"},
		{"s", "Cycle sort column"},
		{"S", "Toggle sort direction"},
		{"w", "Cycle time window (sparklines)"},
		{"y", "Copy selected value"},
		{"r", "Refresh data"},
		{"", ""},
		{"General", ""},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
	}

	return renderHelp(title, keyBindings, width, height)
}

// SlotsHelpOverlay renders help for the Slots tab.
func SlotsHelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Replication Slots Help")

	keyBindings := []struct {
		key  string
		desc string
	}{
		{"Navigation", ""},
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"g / Home", "Go to top"},
		{"G / End", "Go to bottom"},
		{"Tab / → / ←", "Switch tabs"},
		{"", ""},
		{"Actions", ""},
		{"d / Enter", "View slot details"},
		{"D", "Drop inactive slot (confirm)"},
		{"s", "Cycle sort column"},
		{"S", "Toggle sort direction"},
		{"r", "Refresh data"},
		{"", ""},
		{"General", ""},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
	}

	return renderHelp(title, keyBindings, width, height)
}

// LogicalHelpOverlay renders help for the Logical tab.
func LogicalHelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Logical Replication Help")

	keyBindings := []struct {
		key  string
		desc string
	}{
		{"Navigation", ""},
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"g / Home", "Go to top"},
		{"G / End", "Go to bottom"},
		{"Tab / → / ←", "Switch tabs"},
		{"p / P", "Focus publications/subscriptions"},
		{"", ""},
		{"Actions", ""},
		{"d / Enter", "View details"},
		{"r", "Refresh data"},
		{"", ""},
		{"General", ""},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
	}

	return renderHelp(title, keyBindings, width, height)
}

// SetupHelpOverlay renders help for the Setup tab.
func SetupHelpOverlay(width, height int) string {
	title := styles.HelpTitleStyle.Render("Replication Setup Help")

	keyBindings := []struct {
		key  string
		desc string
	}{
		{"Navigation", ""},
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"Tab / → / ←", "Switch tabs"},
		{"", ""},
		{"Setup Wizards", ""},
		{"p", "Physical replication wizard"},
		{"o", "Logical replication wizard"},
		{"n", "Connection string builder"},
		{"c", "Configuration checker"},
		{"", ""},
		{"General", ""},
		{"y", "Copy to clipboard"},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay / Back"},
	}

	return renderHelp(title, keyBindings, width, height)
}

// renderHelp renders a help overlay with the given title and key bindings.
func renderHelp(title string, keyBindings []struct{ key, desc string }, width, height int) string {
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

// padRight pads a string to the right with spaces.
func padRight(s string, length int) string {
	w := runewidth.StringWidth(s)
	if w >= length {
		return runewidth.Truncate(s, length, "")
	}
	return s + strings.Repeat(" ", length-w)
}
