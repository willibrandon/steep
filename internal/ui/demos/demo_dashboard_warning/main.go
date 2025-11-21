// Demo Dashboard Warning: Shows the dashboard with low cache hit ratio warning
// Used for visual verification of the warning highlight
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/views"
)

type model struct {
	dashboard *views.DashboardView
}

func initialModel() model {
	dash := views.NewDashboard()
	dash.SetConnected(true)
	dash.SetConnectionInfo("steep - postgres@localhost:5432/mydb")

	// Hardcoded test data
	connections := []models.Connection{
		{PID: 12345, User: "postgres", Database: "mydb", State: models.StateActive, DurationSeconds: 323, Query: "SELECT * FROM users WHERE id > 100"},
		{PID: 12346, User: "webapp", Database: "mydb", State: models.StateActive, DurationSeconds: 765, Query: "UPDATE orders SET status = 'completed'"},
	}

	dash.SetConnections(connections, 2)

	// Low cache hit ratio to trigger warning
	metrics := models.Metrics{
		TPS:             456.7,
		CacheHitRatio:   75.2, // Below 80% - should be Critical
		ConnectionCount: 85,
		DatabaseSize:    5368709120, // 5 GB
	}
	dash.SetMetrics(metrics)

	return model{dashboard: dash}
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
		m.dashboard.SetSize(msg.Width, msg.Height)
	}

	var cmd tea.Cmd
	_, cmd = m.dashboard.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return m.dashboard.View()
}

func main() {
	m := initialModel()
	m.dashboard.SetSize(80, 24)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
