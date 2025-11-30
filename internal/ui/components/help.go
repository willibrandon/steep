package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpText represents the help component
type HelpText struct {
	width  int
	height int
}

// NewHelp creates a new help component
func NewHelp() *HelpText {
	return &HelpText{}
}

// SetSize sets the size of the help component
func (h *HelpText) SetSize(width, height int) {
	h.width = width
	h.height = height
}

// View renders the help screen
func (h *HelpText) View() string {
	var b strings.Builder

	// Title
	title := styles.ViewTitleStyle.Render("Keyboard Shortcuts")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Navigation section
	b.WriteString(styles.HeaderStyle.Render("Navigation"))
	b.WriteString("\n")
	b.WriteString(h.formatShortcut("q, Ctrl+C", "Quit application"))
	b.WriteString(h.formatShortcut("h, ?", "Toggle help screen"))
	b.WriteString(h.formatShortcut("Esc", "Close dialog"))
	b.WriteString(h.formatShortcut("Tab", "Next view"))
	b.WriteString(h.formatShortcut("Shift+Tab", "Previous view"))
	b.WriteString("\n")

	// View Jumping section
	b.WriteString(styles.HeaderStyle.Render("View Jumping"))
	b.WriteString("\n")
	b.WriteString(h.formatShortcut("1", "Dashboard"))
	b.WriteString(h.formatShortcut("2", "Activity"))
	b.WriteString(h.formatShortcut("3", "Queries"))
	b.WriteString(h.formatShortcut("4", "Locks"))
	b.WriteString(h.formatShortcut("5", "Tables"))
	b.WriteString(h.formatShortcut("6", "Replication"))
	b.WriteString(h.formatShortcut("7", "SQL Editor"))
	b.WriteString(h.formatShortcut("8", "Config"))
	b.WriteString(h.formatShortcut("9", "Logs"))
	b.WriteString(h.formatShortcut("0", "Roles"))
	b.WriteString("\n")

	// Table Navigation section (for future table components)
	b.WriteString(styles.HeaderStyle.Render("Table Navigation"))
	b.WriteString("\n")
	b.WriteString(h.formatShortcut("↑/k", "Move up"))
	b.WriteString(h.formatShortcut("↓/j", "Move down"))
	b.WriteString(h.formatShortcut("PgUp/Ctrl+U", "Page up"))
	b.WriteString(h.formatShortcut("PgDn/Ctrl+D", "Page down"))
	b.WriteString(h.formatShortcut("Home/g", "Go to top"))
	b.WriteString(h.formatShortcut("End/G", "Go to bottom"))
	b.WriteString("\n")

	// Table Actions section
	b.WriteString(styles.HeaderStyle.Render("Table Actions"))
	b.WriteString("\n")
	b.WriteString(h.formatShortcut("s", "Sort column"))
	b.WriteString(h.formatShortcut("/", "Filter/search"))
	b.WriteString(h.formatShortcut("r", "Refresh data"))
	b.WriteString("\n")

	// Wrap in styled dialog
	content := b.String()
	dialog := styles.HelpDialogStyle.Render(content)

	// Center the dialog
	if h.width > 0 {
		dialog = lipgloss.Place(
			h.width,
			h.height,
			lipgloss.Center,
			lipgloss.Center,
			dialog,
		)
	}

	return dialog
}

// formatShortcut formats a keyboard shortcut with its description
func (h *HelpText) formatShortcut(keys, description string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(styles.ColorPrimary).
		Bold(true).
		Width(20).
		Align(lipgloss.Left)

	descStyle := lipgloss.NewStyle().
		Foreground(styles.ColorText)

	return keyStyle.Render(keys) + descStyle.Render(description) + "\n"
}

// ShortHelp returns a brief help text for the bottom of the screen
func (h *HelpText) ShortHelp() string {
	return styles.HelpStyle.Render("Press 'h' or '?' for help • 'q' to quit")
}
