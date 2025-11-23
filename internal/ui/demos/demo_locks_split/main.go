// Demo 2: Split view with lock table (top) and dependency tree (bottom)
// Tests the primary design layout with treeprint rendering
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xlab/treeprint"
)

// Lock represents a database lock for demo purposes
type Lock struct {
	PID      int
	LockType string
	Mode     string
	Granted  bool
	Database string
	Relation string
	Query    string
}

var (
	// Colors
	blockedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	blockingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8BE9FD"))
	treeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#F8F8F2"))
	borderStyle   = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#6272A4"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BD93F9"))
)

type model struct {
	table  table.Model
	locks  []Lock
	width  int
	height int
}

func initialModel() model {
	// Hardcoded test data
	locks := []Lock{
		{PID: 1234, LockType: "relation", Mode: "AccessExclusiveLock", Granted: true, Database: "mydb", Relation: "users", Query: "ALTER TABLE users ADD COLUMN email_verified BOOLEAN"},
		{PID: 5678, LockType: "relation", Mode: "RowExclusiveLock", Granted: false, Database: "mydb", Relation: "users", Query: "UPDATE users SET status = 'active' WHERE id = 100"},
		{PID: 9012, LockType: "relation", Mode: "RowExclusiveLock", Granted: false, Database: "mydb", Relation: "users", Query: "DELETE FROM users WHERE id = 123"},
		{PID: 3456, LockType: "relation", Mode: "RowExclusiveLock", Granted: true, Database: "mydb", Relation: "orders", Query: "INSERT INTO orders VALUES (1, 'pending')"},
		{PID: 7890, LockType: "transactionid", Mode: "ExclusiveLock", Granted: true, Database: "mydb", Relation: "", Query: "SELECT * FROM products FOR UPDATE"},
	}

	columns := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "Type", Width: 10},
		{Title: "Mode", Width: 18},
		{Title: "Grant", Width: 5},
		{Title: "DB", Width: 6},
		{Title: "Relation", Width: 10},
	}

	rows := make([]table.Row, len(locks))
	for i, lock := range locks {
		granted := "No"
		if lock.Granted {
			granted = "Yes"
		}
		rows[i] = table.Row{
			fmt.Sprintf("%d", lock.PID),
			truncate(lock.LockType, 10),
			truncate(lock.Mode, 18),
			granted,
			truncate(lock.Database, 6),
			truncate(lock.Relation, 10),
		}
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(6),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#6272A4")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#F8F8F2")).
		Background(lipgloss.Color("#44475A")).
		Bold(false)
	t.SetStyles(s)

	return model{
		table:  t,
		locks:  locks,
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
		// Allocate about 40% of height to table
		tableHeight := (msg.Height - 12) * 4 / 10
		if tableHeight < 4 {
			tableHeight = 4
		}
		m.table.SetHeight(tableHeight)
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder

	// Header
	header := headerStyle.Render("Steep - Locks Demo 2: Split View")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Stats line
	stats := fmt.Sprintf("Locks: %d  |  Blocking: 1  |  Blocked: 2", len(m.locks))
	b.WriteString(stats)
	b.WriteString("\n\n")

	// Table section
	tableView := m.table.View()
	b.WriteString(tableView)
	b.WriteString("\n\n")

	// Tree section header
	treeHeader := sectionStyle.Render("Lock Dependency Tree")
	b.WriteString(treeHeader)
	b.WriteString("\n")

	// Build the dependency tree
	tree := treeprint.New()

	// Blocker: PID 1234
	blockerMeta := blockingStyle.Render("PID:1234 AccessExclusiveLock")
	blockerQuery := truncate("ALTER TABLE users ADD COLUMN email_verified BOOLEAN", 45)
	blocker := tree.AddMetaBranch(blockerMeta, blockerQuery)

	// Blocked: PID 5678
	blocked1Meta := blockedStyle.Render("PID:5678 waiting")
	blocked1Query := truncate("UPDATE users SET status = 'active' WHERE id = 100", 45)
	blocker.AddMetaNode(blocked1Meta, blocked1Query)

	// Blocked: PID 9012
	blocked2Meta := blockedStyle.Render("PID:9012 waiting")
	blocked2Query := truncate("DELETE FROM users WHERE id = 123", 45)
	blocker.AddMetaNode(blocked2Meta, blocked2Query)

	treeStr := treeStyle.Render(tree.String())
	b.WriteString(treeStr)
	b.WriteString("\n")

	// Help
	help := "[j/k] navigate  [q] quit"
	b.WriteString(help)

	return borderStyle.Width(m.width - 4).Render(b.String())
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
