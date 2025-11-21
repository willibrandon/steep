// Demo Dashboard: Shows the dashboard with hardcoded activity data
// Used for visual verification of the complete dashboard layout
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
		{PID: 12345, User: "postgres", Database: "mydb", State: models.StateActive, DurationSeconds: 323, Query: "SELECT * FROM users WHERE id > 100 ORDER BY created_at DESC LIMIT 50"},
		{PID: 12346, User: "webapp", Database: "mydb", State: models.StateIdleInTransaction, DurationSeconds: 765, Query: "UPDATE orders SET status = 'completed' WHERE id = 12345"},
		{PID: 12347, User: "analytics", Database: "reporting", State: models.StateActive, DurationSeconds: 1, Query: "SELECT COUNT(*) FROM events WHERE date > '2024-01-01'"},
		{PID: 12348, User: "admin", Database: "postgres", State: models.StateIdle, DurationSeconds: 0, Query: ""},
		{PID: 12349, User: "webapp", Database: "mydb", State: models.StateActive, DurationSeconds: 32, Query: "INSERT INTO logs (level, message) VALUES ('info', 'User logged in')"},
		{PID: 12350, User: "monitor", Database: "mydb", State: models.StateActive, DurationSeconds: 0, Query: "SELECT * FROM pg_stat_activity"},
		{PID: 12351, User: "webapp", Database: "mydb", State: models.StateIdleInTransaction, DurationSeconds: 2712, Query: "BEGIN; SELECT * FROM products WHERE category = 'electronics'"},
		{PID: 12352, User: "batch", Database: "mydb", State: models.StateIdle, DurationSeconds: 0, Query: ""},
	}

	dash.SetConnections(connections, 42)

	// Hardcoded metrics data
	metrics := models.Metrics{
		TPS:             1234.5,
		CacheHitRatio:   98.7,
		ConnectionCount: 42,
		DatabaseSize:    1073741824, // 1 GB
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
	// Initialize with a default size
	m := initialModel()
	m.dashboard.SetSize(80, 24)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
