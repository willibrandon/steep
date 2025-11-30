package views

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/metrics"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// DashboardView represents the main dashboard with metrics overview.
type DashboardView struct {
	width  int
	height int

	// Components
	metricsPanel     *components.MetricsPanel
	timeSeriesPanel  *components.TimeSeriesPanel
	heatmapPanel     *components.HeatmapPanel

	// Metrics collector for graph data
	metricsCollector *metrics.Collector

	// Metrics store for heatmap data
	metricsStore *sqlite.MetricsStore

	// State
	connected        bool
	connectionInfo   string
	lastUpdate       time.Time
	chartsVisible    bool
	heatmapVisible   bool
	timeWindow       metrics.TimeWindow

	// Data
	metrics models.Metrics
	err     error
}

// NewDashboard creates a new dashboard view.
func NewDashboard() *DashboardView {
	heatmapConfig := components.DefaultHeatmapConfig()
	heatmapConfig.Title = "TPS Heatmap (7 days)"

	return &DashboardView{
		metricsPanel:    components.NewMetricsPanel(),
		timeSeriesPanel: components.NewTimeSeriesPanel(),
		heatmapPanel:    components.NewHeatmapPanel(heatmapConfig),
		chartsVisible:   true,
		heatmapVisible:  false, // Hidden by default
		timeWindow:      metrics.TimeWindow1h,
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
			d.updateChartData()
		}

	case tea.WindowSizeMsg:
		d.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		return d.handleKeyPress(msg)
	}

	return d, nil
}

// handleKeyPress handles keyboard input for the dashboard.
func (d *DashboardView) handleKeyPress(msg tea.KeyMsg) (ViewModel, tea.Cmd) {
	switch msg.String() {
	// Time window cycling
	case "w":
		d.cycleTimeWindow(true) // Forward
	case "W":
		d.cycleTimeWindow(false) // Backward

	// Heatmap visibility toggle
	case "H":
		d.heatmapVisible = !d.heatmapVisible
		d.heatmapPanel.SetVisible(d.heatmapVisible)
		if d.heatmapVisible {
			d.updateHeatmapData()
		}
	}

	return d, nil
}

// cycleTimeWindow cycles through time windows.
func (d *DashboardView) cycleTimeWindow(forward bool) {
	windows := []metrics.TimeWindow{
		metrics.TimeWindow1m,
		metrics.TimeWindow5m,
		metrics.TimeWindow15m,
		metrics.TimeWindow1h,
		metrics.TimeWindow24h,
	}

	currentIdx := 0
	for i, w := range windows {
		if w == d.timeWindow {
			currentIdx = i
			break
		}
	}

	if forward {
		currentIdx = (currentIdx + 1) % len(windows)
	} else {
		currentIdx = (currentIdx - 1 + len(windows)) % len(windows)
	}

	d.setTimeWindow(windows[currentIdx])
}

// setTimeWindow updates the time window and refreshes chart data.
func (d *DashboardView) setTimeWindow(window metrics.TimeWindow) {
	d.timeWindow = window
	d.timeSeriesPanel.SetWindow(window)
	d.updateChartData()
}

// updateChartData fetches data from the metrics collector and updates charts.
func (d *DashboardView) updateChartData() {
	if d.metricsCollector == nil {
		return
	}

	// Get data for current time window
	tpsData := d.metricsCollector.GetValues(metrics.MetricTPS, d.timeWindow)
	connData := d.metricsCollector.GetValues(metrics.MetricConnections, d.timeWindow)
	cacheData := d.metricsCollector.GetValues(metrics.MetricCacheHitRatio, d.timeWindow)

	d.timeSeriesPanel.SetTPSData(tpsData)
	d.timeSeriesPanel.SetConnectionsData(connData)
	d.timeSeriesPanel.SetCacheHitData(cacheData)

	// Update heatmap if visible
	if d.heatmapVisible {
		d.updateHeatmapData()
	}
}

// updateHeatmapData fetches aggregated data for the heatmap.
func (d *DashboardView) updateHeatmapData() {
	if d.metricsStore == nil {
		return
	}

	// Get last 7 days of TPS data aggregated by day/hour
	since := time.Now().AddDate(0, 0, -7)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	matrix, minVal, maxVal, err := d.metricsStore.GetHourlyAggregatesMatrix(
		ctx,
		string(metrics.MetricTPS),
		"", // Global metric (no key)
		since,
	)
	if err != nil {
		// Log error but don't crash - heatmap will show "collecting data"
		return
	}

	d.heatmapPanel.SetData(matrix, minVal, maxVal)
}

// View renders the dashboard view.
func (d *DashboardView) View() string {
	if !d.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	return d.renderMain()
}

// Minimum height for placeholder content area
const minPlaceholderHeight = 3

// renderMain renders the main dashboard view.
func (d *DashboardView) renderMain() string {
	statusBar := d.renderStatusBar()
	metricsPanel := d.renderMetricsPanel()
	footer := d.renderFooter()

	// Calculate heatmap height if visible
	heatmapHeight := 0
	if d.heatmapVisible {
		heatmapHeight = d.heatmapPanel.Height()
	}

	// Calculate remaining height for charts or placeholder
	footerHeight := lipgloss.Height(footer)
	chrome := lipgloss.Height(statusBar) + lipgloss.Height(metricsPanel) + footerHeight + heatmapHeight
	contentHeight := max(minPlaceholderHeight, d.height-chrome)

	var content string
	if d.chartsVisible {
		content = d.renderTimeSeriesPanel(contentHeight)
	} else {
		content = d.renderPlaceholderWithHeight(contentHeight)
	}

	// Build sections list
	sections := []string{
		statusBar,
		metricsPanel,
		content,
	}

	// Add heatmap if visible
	if d.heatmapVisible {
		d.heatmapPanel.SetSize(d.width - 2)
		sections = append(sections, d.heatmapPanel.View())
	}

	// Top section (status bar, metrics, content, heatmap)
	topSection := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Push footer to bottom of view
	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Height(d.height - footerHeight).Render(topSection),
		footer,
	)
}

// renderTimeSeriesPanel renders the time-series charts panel.
func (d *DashboardView) renderTimeSeriesPanel(height int) string {
	// Update panel size with available height
	d.timeSeriesPanel.SetSize(d.width-2, height)
	return d.timeSeriesPanel.View()
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

// renderPlaceholderWithHeight renders a placeholder for future dashboard content with specified height.
func (d *DashboardView) renderPlaceholderWithHeight(height int) string {
	var content string
	if !d.chartsVisible {
		// Charts are hidden by user
		content = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			styles.MutedStyle.Render("Charts hidden - press [v] to show"),
			"",
			styles.MutedStyle.Render("• TPS graphs"),
			styles.MutedStyle.Render("• Connection trends"),
			styles.MutedStyle.Render("• Cache hit ratio"),
			"",
		)
	} else {
		// Fallback placeholder (shouldn't normally be seen)
		content = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			styles.MutedStyle.Render("Dashboard Overview"),
			"",
			styles.MutedStyle.Render("Press [2] for Activity monitoring"),
			"",
		)
	}

	return lipgloss.NewStyle().
		Width(d.width - 4).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

// renderFooter renders the bottom footer with hints.
func (d *DashboardView) renderFooter() string {
	// Build time window display with current selection
	activeStyle := lipgloss.NewStyle().Foreground(styles.ColorActive).Bold(true)
	windowHint := "[w]Window: " + activeStyle.Render(d.timeWindow.String())

	// Heatmap toggle hint
	var heatmapHint string
	if d.heatmapVisible {
		heatmapHint = "[H]Hide Heatmap"
	} else {
		heatmapHint = "[H]Show Heatmap"
	}

	// Dashboard-specific hints
	dashboardHints := windowHint + " " + heatmapHint + " [?]Help"

	// Navigation hints
	navHints := "[1]Dashboard [2]Activity [3]Queries [4]Locks [5]Tables [6]Replication [7]SQL [8]Config [9]Logs [0]Roles"

	hints := styles.FooterHintStyle.Render(dashboardHints) + "\n" + styles.FooterHintStyle.Render(navHints)

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

// SetMetricsCollector sets the metrics collector for chart data.
func (d *DashboardView) SetMetricsCollector(collector *metrics.Collector) {
	d.metricsCollector = collector
	d.timeSeriesPanel.SetWindow(d.timeWindow)
	d.updateChartData()
}

// GetChartsVisible returns the current chart visibility state.
func (d *DashboardView) GetChartsVisible() bool {
	return d.chartsVisible
}

// SetChartsVisible sets the chart visibility state (for global toggle).
func (d *DashboardView) SetChartsVisible(visible bool) {
	d.chartsVisible = visible
}

// SetMetricsStore sets the metrics store for heatmap data.
func (d *DashboardView) SetMetricsStore(store *sqlite.MetricsStore) {
	d.metricsStore = store
}

// GetHeatmapVisible returns the current heatmap visibility state.
func (d *DashboardView) GetHeatmapVisible() bool {
	return d.heatmapVisible
}

// SetHeatmapVisible sets the heatmap visibility state.
func (d *DashboardView) SetHeatmapVisible(visible bool) {
	d.heatmapVisible = visible
	d.heatmapPanel.SetVisible(visible)
	if visible {
		d.updateHeatmapData()
	}
}
