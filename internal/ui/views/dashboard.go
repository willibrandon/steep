package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// DashboardView represents the main dashboard view
type DashboardView struct {
	width  int
	height int

	// Connection info
	connected     bool
	serverVersion string
	database      string
}

// NewDashboard creates a new dashboard view
func NewDashboard() *DashboardView {
	return &DashboardView{}
}

// Init initializes the dashboard view
func (d *DashboardView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the dashboard view
func (d *DashboardView) Update(msg tea.Msg) (ViewModel, tea.Cmd) {
	return d, nil
}

// View renders the dashboard view
func (d *DashboardView) View() string {
	if !d.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	title := styles.ViewTitleStyle.Render("Dashboard")

	content := fmt.Sprintf(
		"\n%s\n\n"+
			"Database: %s\n"+
			"Version: %s\n\n"+
			"This is a placeholder dashboard view.\n"+
			"Future features will include:\n"+
			"  • Database overview statistics\n"+
			"  • Current activity summary\n"+
			"  • Performance metrics\n"+
			"  • Quick health checks\n",
		title,
		d.database,
		d.serverVersion,
	)

	return content
}

// SetSize sets the dimensions of the dashboard view
func (d *DashboardView) SetSize(width, height int) {
	d.width = width
	d.height = height
}

// SetConnected sets the connection status
func (d *DashboardView) SetConnected(connected bool) {
	d.connected = connected
}

// SetServerVersion sets the server version
func (d *DashboardView) SetServerVersion(version string) {
	d.serverVersion = version
}

// SetDatabase sets the database name
func (d *DashboardView) SetDatabase(database string) {
	d.database = database
}
