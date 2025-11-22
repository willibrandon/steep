package queries

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// HelpOverlay renders the help overlay with keybindings.
func HelpOverlay(width, height int) string {
	title := styles.DialogTitleStyle.Render("Queries View - Keybindings")

	sections := []struct {
		header string
		keys   []string
	}{
		{
			header: "Navigation",
			keys: []string{
				"j/↓      Move down",
				"k/↑      Move up",
				"g        Go to top",
				"G        Go to bottom",
				"←/→      Switch tabs",
			},
		},
		{
			header: "Actions",
			keys: []string{
				"e        Show EXPLAIN plan",
				"y        Copy query to clipboard",
				"/        Search/filter queries",
				"Esc      Clear filter",
				"r        Refresh data",
				"R        Reset all statistics",
				"L        Enable query logging",
			},
		},
		{
			header: "General",
			keys: []string{
				"h        Toggle this help",
				"q        Return to dashboard",
				"Ctrl+C   Quit application",
			},
		},
	}

	sectionStyle := lipgloss.NewStyle().MarginBottom(1)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	keyStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	var content string
	for _, section := range sections {
		content += headerStyle.Render(section.header) + "\n"
		for _, key := range section.keys {
			content += keyStyle.Render("  " + key) + "\n"
		}
		content = sectionStyle.Render(content)
	}

	prompt := styles.FooterHintStyle.Render("Press h or Esc to close")

	dialog := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		content,
		prompt,
	)

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(40)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(dialog),
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}
