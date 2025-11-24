// Package queries provides the Queries view for query performance monitoring.
package queries

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// Tab represents a sort tab option.
type Tab struct {
	Name   string
	Column SortColumn
}

// Tabs defines the available sort tabs.
var Tabs = []Tab{
	{Name: "By Calls", Column: SortByCalls},
	{Name: "By Time", Column: SortByTotalTime},
	{Name: "By Rows", Column: SortByRows},
}

// TabBar renders the tab bar with the active tab highlighted.
func TabBar(activeColumn SortColumn, width int) string {
	var tabs []string

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

	for i, tab := range Tabs {
		var rendered string
		if tab.Column == activeColumn {
			rendered = activeStyle.Render(tab.Name)
		} else {
			rendered = inactiveStyle.Render(tab.Name)
		}
		tabs = append(tabs, rendered)

		// Add separator between tabs (not after last)
		if i < len(Tabs)-1 {
			tabs = append(tabs, separatorStyle.Render("│"))
		}
	}

	// Join tabs and add navigation hint
	tabBar := strings.Join(tabs, "")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("  ←/→ switch tabs")

	return tabBar + hint
}

// NextTab returns the next tab column (wraps around).
func NextTab(current SortColumn) SortColumn {
	for i, tab := range Tabs {
		if tab.Column == current {
			nextIdx := (i + 1) % len(Tabs)
			return Tabs[nextIdx].Column
		}
	}
	return SortByTotalTime
}

// PrevTab returns the previous tab column (wraps around).
func PrevTab(current SortColumn) SortColumn {
	for i, tab := range Tabs {
		if tab.Column == current {
			prevIdx := (i - 1 + len(Tabs)) % len(Tabs)
			return Tabs[prevIdx].Column
		}
	}
	return SortByTotalTime
}
