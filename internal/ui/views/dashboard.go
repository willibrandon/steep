package views

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// DashboardView represents the main dashboard with metrics overview.
type DashboardView struct {
	width  int
	height int

	// Components
	metricsPanel *components.MetricsPanel

	// State
	connected      bool
	connectionInfo string
	lastUpdate     time.Time

	// Data
	metrics models.Metrics
	err     error
}

// NewDashboard creates a new dashboard view.
func NewDashboard() *DashboardView {
	return &DashboardView{
		metricsPanel: components.NewMetricsPanel(),
	}
}

// Init initializes the dashboard view.
func (d *DashboardView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the dashboard view.
func (d *DashboardView) Update(msg tea.Msg) (ViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.MetricsDataMsg:
		if msg.Error != nil {
			d.err = msg.Error
		} else {
			d.metrics = msg.Metrics
			d.metricsPanel.SetMetrics(d.metrics)
			d.lastUpdate = msg.FetchedAt
		}

	case tea.WindowSizeMsg:
		d.SetSize(msg.Width, msg.Height)
	}

	return d, nil
}

// View renders the dashboard view.
func (d *DashboardView) View() string {
	if !d.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	return d.renderMain()
}

// renderMain renders the main dashboard view.
func (d *DashboardView) renderMain() string {
	// Status bar
	statusBar := d.renderStatusBar()

	// Metrics panel
	metricsPanel := d.renderMetricsPanel()

	// Placeholder for future dashboard content
	placeholder := d.renderPlaceholder()

	// Footer
	footer := d.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		metricsPanel,
		placeholder,
		footer,
	)
}

// renderStatusBar renders the top status bar.
func (d *DashboardView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(d.connectionInfo)

	// Show stale indicator if data is older than 5 seconds
	var staleIndicator string
	if !d.lastUpdate.IsZero() && time.Since(d.lastUpdate) > 5*time.Second {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	timestamp := styles.StatusTimeStyle.Render(d.lastUpdate.Format("2006-01-02 15:04:05"))

	gap := d.width - lipgloss.Width(title) - lipgloss.Width(staleIndicator) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(d.width - 2).
		Render(title + staleIndicator + spaces + timestamp)
}

// renderMetricsPanel renders the metrics panel.
func (d *DashboardView) renderMetricsPanel() string {
	return d.metricsPanel.View()
}

// renderPlaceholder renders a placeholder for future dashboard content.
func (d *DashboardView) renderPlaceholder() string {
	placeholderHeight := d.height - 10 // Account for status bar, metrics, footer
	if placeholderHeight < 3 {
		placeholderHeight = 3
	}

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		"",
		styles.MutedStyle.Render("Dashboard Overview"),
		"",
		styles.MutedStyle.Render("Future enhancements:"),
		styles.MutedStyle.Render("• TPS graphs and sparklines"),
		styles.MutedStyle.Render("• Cache hit ratio trends"),
		styles.MutedStyle.Render("• Connection pool status"),
		styles.MutedStyle.Render("• Alert summary panel"),
		styles.MutedStyle.Render("• Quick stats overview"),
		"",
		styles.MutedStyle.Render("Press [2] for Activity monitoring"),
	)

	return lipgloss.NewStyle().
		Width(d.width - 4).
		Height(placeholderHeight).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

// renderFooter renders the bottom footer with hints.
func (d *DashboardView) renderFooter() string {
	hints := styles.FooterHintStyle.Render("[1]Dashboard [2]Activity [3]Queries [4]Locks [5]Tables [6]Replication [7]SQL Editor [8]Configuration")

	return styles.FooterStyle.
		Width(d.width - 2).
		Render(hints)
}

// SetSize sets the dimensions of the dashboard view.
func (d *DashboardView) SetSize(width, height int) {
	d.width = width
	d.height = height
	d.metricsPanel.SetWidth(width - 2)
}

// SetMetrics updates the metrics data.
func (d *DashboardView) SetMetrics(metrics models.Metrics) {
	d.metrics = metrics
	d.metricsPanel.SetMetrics(metrics)
}

// SetConnected sets the connection status.
func (d *DashboardView) SetConnected(connected bool) {
	d.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (d *DashboardView) SetConnectionInfo(info string) {
	d.connectionInfo = info
}

// IsInputMode returns true if the dashboard is in an input mode.
func (d *DashboardView) IsInputMode() bool {
	return false
}

// SetReadOnly sets read-only mode (no-op for dashboard).
func (d *DashboardView) SetReadOnly(readOnly bool) {
	// Dashboard has no destructive operations
}

// SetOwnPIDs sets the PIDs of our own connections (no-op for dashboard).
func (d *DashboardView) SetOwnPIDs(pids []int) {
	// Dashboard doesn't track PIDs
}

// GetFilter returns the current filter settings (empty for dashboard).
func (d *DashboardView) GetFilter() models.ActivityFilter {
	return models.ActivityFilter{}
}

// GetPagination returns nil for dashboard (no pagination).
func (d *DashboardView) GetPagination() *models.Pagination {
	return nil
}

// IsRefreshing returns false for dashboard.
func (d *DashboardView) IsRefreshing() bool {
	return false
}

// SetConnections is a no-op for the simplified dashboard.
func (d *DashboardView) SetConnections(connections []models.Connection, totalCount int) {
	// Dashboard no longer displays connections - use Activity view
}

// SetServerVersion sets the server version (for compatibility).
func (d *DashboardView) SetServerVersion(version string) {
	// Version could be included in connection info
}

// SetDatabase sets the database name (for compatibility).
func (d *DashboardView) SetDatabase(database string) {
	// Database is included in connection info
}
