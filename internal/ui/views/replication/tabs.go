// Package replication provides the Replication view for monitoring PostgreSQL replication.
package replication

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ViewTab represents which tab is active.
type ViewTab int

const (
	TabOverview ViewTab = iota
	TabSlots
	TabLogical
	TabSetup
)

// String returns the display name for the tab.
func (t ViewTab) String() string {
	switch t {
	case TabOverview:
		return "Overview"
	case TabSlots:
		return "Slots"
	case TabLogical:
		return "Logical"
	case TabSetup:
		return "Setup"
	default:
		return "Unknown"
	}
}

// TabBar renders the tab bar for replication view.
func TabBar(activeTab ViewTab, width int) string {
	tabs := []struct {
		name string
		tab  ViewTab
	}{
		{"Overview", TabOverview},
		{"Slots", TabSlots},
		{"Logical", TabLogical},
		{"Setup", TabSetup},
	}

	var rendered []string

	// Define styles
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		Background(lipgloss.Color("236")).
		Padding(0, 2)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Padding(0, 2)

	separatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238"))

	for i, tab := range tabs {
		var style lipgloss.Style
		if tab.tab == activeTab {
			style = activeStyle
		} else {
			style = inactiveStyle
		}
		rendered = append(rendered, style.Render(tab.name))

		// Add separator between tabs (not after last)
		if i < len(tabs)-1 {
			rendered = append(rendered, separatorStyle.Render("│"))
		}
	}

	// Join tabs and add navigation hint
	tabBar := strings.Join(rendered, "")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("  Tab/←/→ switch tabs")

	return tabBar + hint
}

// NextTab returns the next tab (wraps around).
func NextTab(current ViewTab) ViewTab {
	return ViewTab((int(current) + 1) % 4)
}

// PrevTab returns the previous tab (wraps around).
func PrevTab(current ViewTab) ViewTab {
	return ViewTab((int(current) + 3) % 4)
}
