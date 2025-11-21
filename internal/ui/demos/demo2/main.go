// Demo 2: Metrics panel with lipgloss
// Tests: 4-panel layout, color states, formatting
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Color definitions
var (
	normalColor   = lipgloss.Color("7")   // White
	warningColor  = lipgloss.Color("11")  // Yellow
	criticalColor = lipgloss.Color("9")   // Red
	borderColor   = lipgloss.Color("240") // Gray
)

type panelStatus int

const (
	statusNormal panelStatus = iota
	statusWarning
	statusCritical
)

type panel struct {
	label  string
	value  string
	unit   string
	status panelStatus
}

type model struct {
	panels []panel
	width  int
}

func initialModel() model {
	return model{
		panels: []panel{
			{label: "TPS", value: "1,234", unit: "/s", status: statusNormal},
			{label: "Cache Hit", value: "85.2", unit: "%", status: statusWarning},
			{label: "Connections", value: "42", unit: "/100", status: statusNormal},
			{label: "DB Size", value: "1.2", unit: "GB", status: statusNormal},
		},
		width: 80,
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
		case "w": // Toggle warning
			m.panels[1].status = statusWarning
			m.panels[1].value = "85.2"
		case "c": // Toggle critical
			m.panels[1].status = statusCritical
			m.panels[1].value = "72.1"
		case "n": // Normal
			m.panels[1].status = statusNormal
			m.panels[1].value = "98.5"
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func (m model) renderPanel(p panel) string {
	panelWidth := (m.width - 8) / 4 // 4 panels with spacing
	if panelWidth < 15 {
		panelWidth = 15
	}

	// Determine colors based on status
	var valueColor lipgloss.Color
	var bgColor lipgloss.Color
	switch p.status {
	case statusWarning:
		valueColor = lipgloss.Color("0")  // Black text
		bgColor = lipgloss.Color("11")    // Yellow background
	case statusCritical:
		valueColor = lipgloss.Color("15") // White text
		bgColor = lipgloss.Color("9")     // Red background
	default:
		valueColor = lipgloss.Color("10") // Green
		bgColor = lipgloss.Color("0")     // No background
	}

	labelStyle := lipgloss.NewStyle().
		Width(panelWidth).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color("8"))

	valueStyle := lipgloss.NewStyle().
		Width(panelWidth).
		Align(lipgloss.Center).
		Bold(true).
		Foreground(valueColor)

	if p.status != statusNormal {
		valueStyle = valueStyle.Background(bgColor)
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		labelStyle.Render(p.label),
		valueStyle.Render(p.value+p.unit),
	)

	return panelStyle.Render(content)
}

func (m model) View() string {
	// Render all panels
	panels := make([]string, len(m.panels))
	for i, p := range m.panels {
		panels[i] = m.renderPanel(p)
	}

	// Join horizontally
	metricsRow := lipgloss.JoinHorizontal(lipgloss.Top, panels...)

	// Add title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("Demo 2: Metrics Panel"),
		"",
		metricsRow,
		"",
		helpStyle.Render("  [w]arning  [c]ritical  [n]ormal  [q]uit"),
	)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
