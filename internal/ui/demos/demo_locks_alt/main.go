// Demo 3: Alternative layout - Tree-focused view
// Shows blocking chains as the primary view, with table as secondary
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xlab/treeprint"
)

var (
	// Colors matching Dracula theme
	blockedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	blockingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8BE9FD"))
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F8F8F2"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6272A4"))
	borderStyle   = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#6272A4"))
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#44475A")).Padding(0, 1)
)

type model struct {
	width  int
	height int
}

func initialModel() model {
	return model{
		width:  80,
		height: 24,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	// Header
	header := headerStyle.Render("Steep - Locks Demo 3: Tree-Focused View")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Stats summary in a box
	statsContent := fmt.Sprintf("Active Locks: 6  |  Blocking Chains: 2  |  Deadlocks: 0")
	stats := boxStyle.Render(statsContent)
	b.WriteString(stats)
	b.WriteString("\n\n")

	// Primary: Blocking chains tree
	tree := treeprint.NewWithRoot("Blocking Chains")

	// Chain 1: ALTER TABLE blocking updates
	chain1Meta := blockingStyle.Render("PID:1234")
	chain1 := tree.AddMetaBranch(chain1Meta, "ALTER TABLE users ADD COLUMN...")

	// Add lock info as child
	chain1.AddNode(dimStyle.Render("Lock: AccessExclusiveLock on users"))

	// Blocked queries
	blocked1Meta := blockedStyle.Render("PID:5678")
	chain1.AddMetaNode(blocked1Meta, "UPDATE users SET status = ...")

	blocked2Meta := blockedStyle.Render("PID:9012")
	chain1.AddMetaNode(blocked2Meta, "DELETE FROM users WHERE...")

	// Chain 2: Another blocking scenario
	chain2Meta := blockingStyle.Render("PID:3456")
	chain2 := tree.AddMetaBranch(chain2Meta, "VACUUM FULL orders")
	chain2.AddNode(dimStyle.Render("Lock: AccessExclusiveLock on orders"))

	blocked3Meta := blockedStyle.Render("PID:7890")
	chain2.AddMetaNode(blocked3Meta, "INSERT INTO orders VALUES...")

	b.WriteString(normalStyle.Render(tree.String()))
	b.WriteString("\n")

	// Summary of non-blocking locks
	summaryHeader := dimStyle.Render("Other Active Locks (not blocking):")
	b.WriteString(summaryHeader)
	b.WriteString("\n")

	otherLocks := []string{
		"PID:2345  AccessShareLock on products  (SELECT COUNT(*)...)",
		"PID:6789  RowShareLock on inventory    (SELECT FOR UPDATE...)",
	}
	for _, lock := range otherLocks {
		b.WriteString("  " + dimStyle.Render(lock) + "\n")
	}

	b.WriteString("\n")

	// Help
	help := "[j/k] navigate chains  [d] details  [x] kill  [q] quit"
	b.WriteString(help)

	return borderStyle.Width(m.width - 4).Render(b.String())
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
