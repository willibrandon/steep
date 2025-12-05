// Demo 3: Combined layout with borders
// Tests: Full dashboard layout - status bar, metrics, table, footer
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Colors
var (
	borderColor  = lipgloss.Color("240")
	accentColor  = lipgloss.Color("6")
	mutedColor   = lipgloss.Color("8")
	activeColor  = lipgloss.Color("10")
	idleColor    = lipgloss.Color("8")
	idleTxnColor = lipgloss.Color("11")
	warningColor = lipgloss.Color("11")
)

type model struct {
	table  table.Model
	width  int
	height int
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
		table.WithHeight(8),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return model{
		table:  t,
		width:  80,
		height: 24,
	}
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Adjust table height based on available space
		tableHeight := m.height - 12 // Header, metrics, footer
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.table.SetHeight(tableHeight)
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) renderStatusBar() string {
	titleStyle := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(mutedColor)

	title := titleStyle.Render("steep - postgres@localhost:5432/mydb")
	timestamp := timeStyle.Render(time.Now().Format("2006-01-02 15:04:05"))

	// Fill space between
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Render(title + spaces + timestamp)
}

func (m model) renderMetricsPanel() string {
	panelWidth := (m.width - 12) / 4
	if panelWidth < 12 {
		panelWidth = 12
	}

	renderPanel := func(label, value string, isWarning bool) string {
		labelStyle := lipgloss.NewStyle().
			Width(panelWidth).
			Align(lipgloss.Center).
			Foreground(mutedColor)

		valueStyle := lipgloss.NewStyle().
			Width(panelWidth).
			Align(lipgloss.Center).
			Bold(true)

		if isWarning {
			valueStyle = valueStyle.
				Foreground(lipgloss.Color("0")).
				Background(warningColor)
		} else {
			valueStyle = valueStyle.Foreground(activeColor)
		}

		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Center,
					labelStyle.Render(label),
					valueStyle.Render(value),
				),
			)
	}

	panels := lipgloss.JoinHorizontal(
		lipgloss.Top,
		renderPanel("TPS", "1,234/s", false),
		renderPanel("Cache Hit", "85.2%", true),
		renderPanel("Connections", "42/100", false),
		renderPanel("DB Size", "1.2 GB", false),
	)

	return panels
}

func (m model) renderFooter() string {
	hintStyle := lipgloss.NewStyle().
		Foreground(mutedColor)

	countStyle := lipgloss.NewStyle().
		Foreground(accentColor)

	hints := hintStyle.Render("[/]filter [s]ort [d]etail [c]ancel [x]kill [r]efresh [?]help [q]uit")
	count := countStyle.Render("8/500")

	gap := m.width - lipgloss.Width(hints) - lipgloss.Width(count) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Render(hints + spaces + count)
}

func (m model) View() string {
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderStatusBar(),
		m.renderMetricsPanel(),
		tableStyle.Render(m.table.View()),
		m.renderFooter(),
	)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
