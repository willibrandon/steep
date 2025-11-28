package logs

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpSection represents a section of help text.
type HelpSection struct {
	Title string
	Items []HelpItem
}

// HelpItem represents a single help item.
type HelpItem struct {
	Key  string
	Desc string
}

// GetHelpSections returns the help content for the log viewer.
func GetHelpSections() []HelpSection {
	return []HelpSection{
		{
			Title: "Navigation",
			Items: []HelpItem{
				{Key: "j/↓", Desc: "Scroll down"},
				{Key: "k/↑", Desc: "Scroll up"},
				{Key: "g", Desc: "Go to oldest"},
				{Key: "G", Desc: "Go to newest"},
				{Key: "Ctrl+d", Desc: "Page down"},
				{Key: "Ctrl+u", Desc: "Page up"},
			},
		},
		{
			Title: "Follow Mode",
			Items: []HelpItem{
				{Key: "f", Desc: "Toggle follow mode"},
			},
		},
		{
			Title: "Search",
			Items: []HelpItem{
				{Key: "/", Desc: "Start search"},
				{Key: "n", Desc: "Next match"},
				{Key: "N", Desc: "Previous match"},
				{Key: "Esc", Desc: "Clear search"},
			},
		},
		{
			Title: "Commands",
			Items: []HelpItem{
				{Key: ":", Desc: "Enter command mode"},
				{Key: ":level <lvl>", Desc: "Filter by severity"},
				{Key: ":level clear", Desc: "Clear severity filter"},
				{Key: ":goto <time>", Desc: "Jump to timestamp"},
			},
		},
		{
			Title: "General",
			Items: []HelpItem{
				{Key: "?", Desc: "Toggle help"},
				{Key: "Esc", Desc: "Clear filters/close"},
				{Key: "q", Desc: "Return to previous view"},
			},
		},
	}
}

// RenderHelp renders the help overlay.
func RenderHelp(width, height int) string {
	sections := GetHelpSections()

	// Calculate dimensions
	maxWidth := 50
	if width < maxWidth {
		maxWidth = width - 4
	}

	var sb strings.Builder

	// Title
	titleStyle := styles.HelpTitleStyle.Width(maxWidth).Align(lipgloss.Center)
	sb.WriteString(titleStyle.Render("Log Viewer Help"))
	sb.WriteString("\n\n")

	// Render each section
	keyStyle := styles.HelpKeyStyle.Width(14)
	descStyle := styles.HelpDescStyle

	for i, section := range sections {
		// Section title
		sb.WriteString(styles.AccentStyle.Render(section.Title))
		sb.WriteString("\n")

		// Items
		for _, item := range section.Items {
			sb.WriteString("  ")
			sb.WriteString(keyStyle.Render(item.Key))
			sb.WriteString(descStyle.Render(item.Desc))
			sb.WriteString("\n")
		}

		// Add spacing between sections (except last)
		if i < len(sections)-1 {
			sb.WriteString("\n")
		}
	}

	// Footer
	sb.WriteString("\n")
	sb.WriteString(styles.MutedStyle.Render("Press ? or Esc to close"))

	// Wrap in dialog style
	content := sb.String()
	dialog := styles.HelpDialogStyle.Width(maxWidth + 4).Render(content)

	// Center on screen
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog)
}
