package tables

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// helpKeyBinding represents a single key binding entry.
type helpKeyBinding struct {
	key  string
	desc string
}

// getHelpBindings returns all help key bindings.
func getHelpBindings() []helpKeyBinding {
	return []helpKeyBinding{
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
		{"Maintenance", ""},
		{"x", "Open operations menu"},
		{"v", "VACUUM table"},
		{"a", "ANALYZE table"},
		{"r", "REINDEX table"},
		{"p", "View/manage permissions"},
		{"H", "View operation history"},
		{"", ""},
		{"General", ""},
		{"R", "Refresh data"},
		{"h / ?", "Toggle this help"},
		{"Esc / q", "Close overlay"},
	}
}

// renderHelpOverlay renders the scrollable help overlay.
func (v *TablesView) renderHelpOverlay() string {
	keyBindings := getHelpBindings()

	// Calculate max key width for alignment
	maxKeyWidth := 0
	for _, kb := range keyBindings {
		if len(kb.key) > maxKeyWidth {
			maxKeyWidth = len(kb.key)
		}
	}

	// Build all lines
	var allLines []string
	for _, kb := range keyBindings {
		if kb.key == "" && kb.desc == "" {
			allLines = append(allLines, "")
			continue
		}
		if kb.desc == "" {
			// Section header
			allLines = append(allLines, styles.HelpTitleStyle.Render(kb.key))
			continue
		}
		key := styles.HelpKeyStyle.Render(padRight(kb.key, maxKeyWidth+2))
		desc := styles.HelpDescStyle.Render(kb.desc)
		allLines = append(allLines, key+desc)
	}

	// Add pgstattuple status at end
	var pgstatStatus string
	if v.pgstattupleAvailable {
		pgstatStatus = lipgloss.NewStyle().Foreground(styles.ColorSuccess).Render("+") + " pgstattuple installed"
	} else {
		pgstatStatus = lipgloss.NewStyle().Foreground(styles.ColorMuted).Render("-") + " pgstattuple not installed (estimated bloat)"
	}
	allLines = append(allLines, "", pgstatStatus)

	totalLines := len(allLines)

	// Calculate visible height (leave room for title, footer, borders, padding)
	visibleHeight := v.height - 10
	if visibleHeight < 5 {
		visibleHeight = 5
	}
	if visibleHeight > totalLines {
		visibleHeight = totalLines
	}

	// Clamp scroll offset
	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.helpScrollOffset > maxScroll {
		v.helpScrollOffset = maxScroll
	}
	if v.helpScrollOffset < 0 {
		v.helpScrollOffset = 0
	}

	// Get visible slice
	endIdx := v.helpScrollOffset + visibleHeight
	if endIdx > totalLines {
		endIdx = totalLines
	}
	visibleLines := allLines[v.helpScrollOffset:endIdx]

	// Build content
	var b strings.Builder

	// Title
	b.WriteString(styles.HelpTitleStyle.Render("Tables View Help"))
	b.WriteString("\n\n")

	// Visible lines
	b.WriteString(strings.Join(visibleLines, "\n"))

	// Footer with scroll indicator
	b.WriteString("\n\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	if maxScroll > 0 {
		scrollPct := float64(v.helpScrollOffset) / float64(maxScroll) * 100
		b.WriteString(footerStyle.Render(fmt.Sprintf("[j/k] scroll  [q/Esc] close  (%.0f%%)", scrollPct)))
	} else {
		b.WriteString(footerStyle.Render("[q/Esc] close"))
	}

	// Wrap in dialog
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(b.String()),
	)
}
