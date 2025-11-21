// Demo 1: Basic table rendering with bubbles/table
// Tests: Activity table layout, column widths, row selection
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table table.Model
}

func initialModel() model {
	columns := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "User", Width: 10},
		{Title: "Database", Width: 10},
		{Title: "State", Width: 15},
		{Title: "Duration", Width: 8},
		{Title: "Query", Width: 25},
	}

	// Hardcoded test data matching pg_stat_activity
	rows := []table.Row{
		{"12345", "postgres", "mydb", "active", "00:05:23", "SELECT * FROM users..."},
		{"12346", "webapp", "mydb", "idle in txn", "00:12:45", "UPDATE orders SET..."},
		{"12347", "analytics", "reporting", "active", "00:00:01", "SELECT COUNT(*) F..."},
		{"12348", "admin", "postgres", "idle", "00:00:00", ""},
		{"12349", "webapp", "mydb", "active", "00:00:32", "INSERT INTO logs ..."},
		{"12350", "monitor", "mydb", "active", "00:00:00", "SELECT * FROM pg_..."},
		{"12351", "webapp", "mydb", "idle in txn", "00:45:12", "BEGIN; SELECT ..."},
		{"12352", "batch", "mydb", "idle", "00:00:00", ""},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return model{table: t}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return baseStyle.Render(m.table.View()) + "\n\n  Press q to quit\n"
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
