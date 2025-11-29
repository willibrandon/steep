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
				{Key: "j/↓", Desc: "Select next entry"},
				{Key: "k/↑", Desc: "Select previous entry"},
				{Key: "g", Desc: "Select oldest"},
				{Key: "G", Desc: "Select newest"},
				{Key: "Ctrl+d", Desc: "Half page down"},
				{Key: "Ctrl+u", Desc: "Half page up"},
			},
		},
		{
			Title: "Copy",
			Items: []HelpItem{
				{Key: "y", Desc: "Copy selected entry"},
				{Key: "Y", Desc: "Copy all (filtered) entries"},
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
				{Key: ":level error", Desc: "Filter by severity"},
				{Key: ":level error+", Desc: "Filter level and above"},
				{Key: ":level error -1h", Desc: "Errors from last hour"},
				{Key: ":level warn+ >14:30", Desc: "Warn+ at/after 14:30"},
				{Key: ":level clear", Desc: "Clear severity filter"},
				{Key: ":goto 14:30", Desc: "Jump to closest entry"},
				{Key: ":goto >14:30", Desc: "First entry at/after"},
				{Key: ":goto <14:30", Desc: "Last entry at/before"},
			},
		},
		{
			Title: "History",
			Items: []HelpItem{
				{Key: "↑ (in : or /)", Desc: "Previous history entry"},
				{Key: "↓ (in : or /)", Desc: "Next history entry"},
			},
		},
		{
			Title: "Time Formats",
			Items: []HelpItem{
				{Key: "14:30", Desc: "Today"},
				{Key: "2025-11-27 14:30", Desc: "Date+time"},
				{Key: "-1h, -30m, -2d", Desc: "Relative"},
			},
		},
		{
			Title: "General",
			Items: []HelpItem{
				{Key: "h", Desc: "Toggle help"},
				{Key: "Esc", Desc: "Clear filters/close help"},
			},
		},
		{
			Title: "Tips",
			Items: []HelpItem{
				{Key: "Millis", Desc: "Use %m in log_line_prefix"},
			},
		},
	}
}

// RenderHelp renders the help overlay.
func RenderHelp(width, height int) string {
	sections := GetHelpSections()

	// Calculate max key width across all sections
	maxKeyWidth := 0
	for _, section := range sections {
		for _, item := range section.Items {
			if len(item.Key) > maxKeyWidth {
				maxKeyWidth = len(item.Key)
			}
		}
	}

	// Calculate dialog width based on content
	maxWidth := 55
	if width < maxWidth {
		maxWidth = width - 4
	}

	var sb strings.Builder

	// Title
	titleStyle := styles.HelpTitleStyle.Width(maxWidth).Align(lipgloss.Center)
	sb.WriteString(titleStyle.Render("Log Viewer Help"))
	sb.WriteString("\n\n")

	// Render each section
	descStyle := styles.HelpDescStyle

	for i, section := range sections {
		// Section title
		sb.WriteString(styles.AccentStyle.Render(section.Title))
		sb.WriteString("\n")

		// Items
		for _, item := range section.Items {
			sb.WriteString("  ")
			key := styles.HelpKeyStyle.Render(padRight(item.Key, maxKeyWidth+2))
			sb.WriteString(key)
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
	sb.WriteString(styles.MutedStyle.Render("Press h or Esc to close"))

	// Wrap in dialog style
	content := sb.String()
	dialog := styles.HelpDialogStyle.Width(maxWidth + 4).Render(content)

	// Center on screen
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog)
}

// padRight pads a string to the specified width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
