package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/alerts"
	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/logger"
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
	alertPanel       *components.AlertPanel

	// Metrics collector for graph data
	metricsCollector *metrics.Collector

	// Metrics store for heatmap data
	metricsStore *sqlite.MetricsStore

	// Instance filter (T054: multi-instance support)
	instanceFilter string // "" = all instances

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

	// Alert state
	warningCount  int
	criticalCount int
	activeAlerts  []alerts.ActiveAlert

	// Alert history overlay state
	showHistory   bool
	historyEvents []alerts.Event
	historyIndex  int
	alertStore    *sqlite.AlertStore
	alertEngine   *alerts.Engine
}

// NewDashboard creates a new dashboard view.
func NewDashboard() *DashboardView {
	heatmapConfig := components.DefaultHeatmapConfig()
	heatmapConfig.Title = "TPS Heatmap (7 days)"

	return &DashboardView{
		metricsPanel:    components.NewMetricsPanel(),
		timeSeriesPanel: components.NewTimeSeriesPanel(),
		heatmapPanel:    components.NewHeatmapPanel(heatmapConfig),
		alertPanel:      components.NewAlertPanel(),
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

	case ui.AlertStateMsg:
		d.warningCount = msg.WarningCount
		d.criticalCount = msg.CriticalCount
		d.activeAlerts = msg.ActiveAlerts
		d.alertPanel.SetAlerts(msg.ActiveAlerts)
		d.updateMetricsPanelAlertStates(msg.ActiveAlerts)

	case ui.AlertHistoryMsg:
		if msg.Error == nil {
			d.historyEvents = msg.Events
			d.historyIndex = 0
		}

	case ui.AlertAcknowledgedMsg:
		if msg.Error == nil {
			// Update the local event in historyEvents to show acknowledgment status
			for i := range d.historyEvents {
				if d.historyEvents[i].ID == msg.EventID {
					if msg.Unacknowledged {
						d.historyEvents[i].Unacknowledge()
					} else {
						d.historyEvents[i].Acknowledge("user")
					}
					break
				}
			}
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
	// Handle history overlay navigation when visible
	if d.showHistory {
		switch msg.String() {
		case "esc", "a", "q":
			d.showHistory = false
			return d, nil
		case "j", "down":
			if d.historyIndex < len(d.historyEvents)-1 {
				d.historyIndex++
			}
			return d, nil
		case "k", "up":
			if d.historyIndex > 0 {
				d.historyIndex--
			}
			return d, nil
		case "g":
			d.historyIndex = 0
			return d, nil
		case "G":
			if len(d.historyEvents) > 0 {
				d.historyIndex = len(d.historyEvents) - 1
			}
			return d, nil
		case "enter":
			// Toggle acknowledgment on the selected event
			if d.historyIndex >= 0 && d.historyIndex < len(d.historyEvents) {
				event := d.historyEvents[d.historyIndex]
				if event.IsAcknowledged() {
					return d, d.unacknowledgeAlert(event.ID, event.RuleName)
				}
				return d, d.acknowledgeAlert(event.ID, event.RuleName)
			}
			return d, nil
		}
		return d, nil
	}

	switch msg.String() {
	// Alert history toggle
	case "a":
		d.showHistory = true
		return d, d.loadAlertHistory()

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

	// Use instance filter (T054: multi-instance support)
	matrix, minVal, maxVal, err := d.metricsStore.GetHourlyAggregatesMatrixByInstance(
		ctx,
		string(metrics.MetricTPS),
		"", // Global metric (no key)
		d.instanceFilter,
		since,
	)
	if err != nil {
		// Log error but don't crash - heatmap will show "collecting data"
		return
	}

	d.heatmapPanel.SetData(matrix, minVal, maxVal)
}

// updateMetricsPanelAlertStates updates the metrics panel based on active alerts.
func (d *DashboardView) updateMetricsPanelAlertStates(activeAlerts []alerts.ActiveAlert) {
	// Reset to normal by default
	cacheState := components.MetricAlertNone

	// Check for cache_hit_ratio alert
	for _, alert := range activeAlerts {
		if alert.RuleName == "low_cache_hit" || alert.RuleName == "cache_hit_ratio" {
			if alert.IsCritical() {
				cacheState = components.MetricAlertCritical
			} else if alert.IsWarning() && cacheState != components.MetricAlertCritical {
				cacheState = components.MetricAlertWarning
			}
		}
	}

	d.metricsPanel.SetCacheHitAlertState(cacheState)
}

// loadAlertHistory returns a command to fetch alert history from the store.
func (d *DashboardView) loadAlertHistory() tea.Cmd {
	return func() tea.Msg {
		if d.alertStore == nil {
			logger.Debug("loadAlertHistory: alertStore is nil")
			return ui.AlertHistoryMsg{Events: nil, Error: nil}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		events, err := d.alertStore.GetHistory(ctx, 100)
		logger.Debug("loadAlertHistory: fetched events", "count", len(events), "error", err)
		return ui.AlertHistoryMsg{Events: events, Error: err}
	}
}

// acknowledgeAlert returns a command to acknowledge an alert event.
func (d *DashboardView) acknowledgeAlert(eventID int64, ruleName string) tea.Cmd {
	return func() tea.Msg {
		if d.alertStore == nil {
			logger.Debug("acknowledgeAlert: alertStore is nil")
			return ui.AlertAcknowledgedMsg{EventID: eventID, RuleName: ruleName, Error: fmt.Errorf("alert store not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Persist to SQLite
		err := d.alertStore.Acknowledge(ctx, eventID, "user")
		logger.Debug("acknowledgeAlert: acknowledged event", "eventID", eventID, "error", err)

		// Also update in-memory engine state
		if err == nil && d.alertEngine != nil {
			if ackErr := d.alertEngine.Acknowledge(ruleName); ackErr != nil {
				logger.Debug("acknowledgeAlert: engine acknowledge failed", "rule", ruleName, "error", ackErr)
				// Don't fail - the SQLite update succeeded
			}
		}

		return ui.AlertAcknowledgedMsg{EventID: eventID, RuleName: ruleName, Error: err}
	}
}

// unacknowledgeAlert returns a command to remove acknowledgment from an alert event.
func (d *DashboardView) unacknowledgeAlert(eventID int64, ruleName string) tea.Cmd {
	return func() tea.Msg {
		if d.alertStore == nil {
			logger.Debug("unacknowledgeAlert: alertStore is nil")
			return ui.AlertAcknowledgedMsg{EventID: eventID, RuleName: ruleName, Error: fmt.Errorf("alert store not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Remove from SQLite
		err := d.alertStore.Unacknowledge(ctx, eventID)
		logger.Debug("unacknowledgeAlert: unacknowledged event", "eventID", eventID, "error", err)

		// Also update in-memory engine state
		if err == nil && d.alertEngine != nil {
			if ackErr := d.alertEngine.Unacknowledge(ruleName); ackErr != nil {
				logger.Debug("unacknowledgeAlert: engine unacknowledge failed", "rule", ruleName, "error", ackErr)
				// Don't fail - the SQLite update succeeded
			}
		}

		return ui.AlertAcknowledgedMsg{EventID: eventID, RuleName: ruleName, Unacknowledged: true, Error: err}
	}
}

// renderHistoryOverlay renders the alert history overlay.
func (d *DashboardView) renderHistoryOverlay() string {
	// Calculate overlay dimensions (80% of screen, centered)
	overlayWidth := d.width * 80 / 100
	overlayHeight := d.height * 80 / 100
	if overlayWidth < 60 {
		overlayWidth = min(60, d.width-4)
	}
	if overlayHeight < 15 {
		overlayHeight = min(15, d.height-4)
	}

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	title := titleStyle.Render("Alert History")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	helpText := helpStyle.Render("[j/k] Navigate  [g/G] Top/Bottom  [Enter] Acknowledge  [Esc] Close")

	// Header
	header := lipgloss.JoinVertical(lipgloss.Left, title, helpText, "")

	// Content area height
	contentHeight := overlayHeight - lipgloss.Height(header) - 4 // 4 for border/padding

	// Render events list
	var content string
	if len(d.historyEvents) == 0 {
		content = lipgloss.NewStyle().
			Width(overlayWidth - 4).
			Height(contentHeight).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorMuted).
			Render("No alert history")
	} else {
		content = d.renderHistoryList(overlayWidth-4, contentHeight)
	}

	// Combine header and content
	innerContent := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Overlay box style
	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(overlayWidth).
		Height(overlayHeight)

	overlay := overlayStyle.Render(innerContent)

	// Center the overlay
	return lipgloss.Place(
		d.width,
		d.height,
		lipgloss.Center,
		lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// renderHistoryList renders the scrollable list of history events.
func (d *DashboardView) renderHistoryList(width, height int) string {
	var lines []string

	// Calculate visible range (subtract 2 for header and separator)
	visibleCount := height - 2
	if visibleCount < 1 {
		visibleCount = 1
	}
	startIdx := 0
	if d.historyIndex >= visibleCount {
		startIdx = d.historyIndex - visibleCount + 1
	}
	endIdx := startIdx + visibleCount
	if endIdx > len(d.historyEvents) {
		endIdx = len(d.historyEvents)
	}

	// Define column widths - dynamic Rule width to fit available space
	const (
		colTimestamp = 19 // "2006-01-02 15:04:05"
		colState     = 5  // "CRIT ", "WARN ", "OK   "
		colValue     = 8
		colAck       = 3 // "[x]" or "[ ]"
	)
	// Rule gets remaining space: width - fixed cols - gaps (4 single + 1 double = 6)
	colRule := width - colTimestamp - colState - colValue - colAck - 6
	if colRule < 8 {
		colRule = 8
	}

	// Column headers
	headerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted).Bold(true)
	header := fmt.Sprintf("%-*s %-*s %-*s %*s  %s",
		colTimestamp, "Timestamp",
		colState, "State",
		colRule, "Rule",
		colValue, "Value",
		"Ack")
	lines = append(lines, headerStyle.Render(header))
	lines = append(lines, headerStyle.Render(strings.Repeat("─", width)))

	for i := startIdx; i < endIdx; i++ {
		event := d.historyEvents[i]
		selected := i == d.historyIndex

		// Format timestamp
		timestamp := event.TriggeredAt.Format("2006-01-02 15:04:05")

		// State with color (pad to fixed width BEFORE coloring)
		var stateText string
		var stateColor lipgloss.Color
		switch event.NewState {
		case alerts.StateCritical:
			stateText = "CRIT "
			stateColor = styles.ColorAlertCritical
		case alerts.StateWarning:
			stateText = "WARN "
			stateColor = styles.ColorAlertWarning
		case alerts.StateNormal:
			stateText = "OK   "
			stateColor = styles.ColorAlertNormal
		default:
			stateText = fmt.Sprintf("%-5s", event.NewState)
			stateColor = styles.ColorMuted
		}

		// Rule name (padded)
		ruleName := fmt.Sprintf("%-*s", colRule, truncateString(event.RuleName, colRule))

		// Value
		value := fmt.Sprintf("%8.2f", event.MetricValue)

		// Acknowledged checkbox - ASCII style
		ackBox := "[ ]"
		if event.IsAcknowledged() {
			ackBox = "[x]"
		}

		// Build line without ANSI codes first for selection
		if selected {
			// For selected line, render without colors then apply selection style
			plainLine := fmt.Sprintf("%s %s %s %s  %s", timestamp, stateText, ruleName, value, ackBox)
			line := lipgloss.NewStyle().
				Background(styles.ColorSelectedBg).
				Foreground(styles.ColorSelectedFg).
				Width(width).
				Render(plainLine)
			lines = append(lines, line)
		} else {
			// For non-selected, apply color to state only
			coloredState := lipgloss.NewStyle().Foreground(stateColor).Render(stateText)
			line := fmt.Sprintf("%s %s %s %s  %s", timestamp, coloredState, ruleName, value, ackBox)
			lines = append(lines, line)
		}
	}

	// Add scroll indicator if needed
	if len(d.historyEvents) > visibleCount {
		scrollInfo := fmt.Sprintf("(%d/%d)", d.historyIndex+1, len(d.historyEvents))
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(scrollInfo))
	}

	return strings.Join(lines, "\n")
}

// truncateString truncates a string to maxLen with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// View renders the dashboard view.
func (d *DashboardView) View() string {
	if !d.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Show history overlay if active
	if d.showHistory {
		return d.renderHistoryOverlay()
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

	// Calculate alert panel height if active
	alertPanelHeight := 0
	if d.alertPanel.HasAlerts() {
		d.alertPanel.SetWidth(d.width - 2)
		alertPanelHeight = d.alertPanel.Height()
	}

	// Calculate heatmap height if visible
	heatmapHeight := 0
	if d.heatmapVisible {
		heatmapHeight = d.heatmapPanel.Height()
	}

	// Calculate remaining height for charts or placeholder
	footerHeight := lipgloss.Height(footer)
	chrome := lipgloss.Height(statusBar) + lipgloss.Height(metricsPanel) + footerHeight + heatmapHeight + alertPanelHeight
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
	}

	// Add alert panel if there are active alerts
	if d.alertPanel.HasAlerts() {
		sections = append(sections, d.alertPanel.View())
	}

	sections = append(sections, content)

	// Add heatmap if visible
	if d.heatmapVisible {
		d.heatmapPanel.SetSize(d.width - 2)
		sections = append(sections, d.heatmapPanel.View())
	}

	// Top section (status bar, metrics, alerts, content, heatmap)
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

	// Alert counts
	alertCounts := d.renderAlertCounts()

	timestamp := styles.StatusTimeStyle.Render(d.lastUpdate.Format("2006-01-02 15:04:05"))

	gap := d.width - lipgloss.Width(title) - lipgloss.Width(staleIndicator) - lipgloss.Width(alertCounts) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(d.width - 2).
		Render(title + staleIndicator + spaces + alertCounts + " " + timestamp)
}

// renderAlertCounts renders alert warning/critical counts with colors.
func (d *DashboardView) renderAlertCounts() string {
	if d.warningCount == 0 && d.criticalCount == 0 {
		return ""
	}

	var parts []string

	if d.criticalCount > 0 {
		criticalStyle := lipgloss.NewStyle().Foreground(styles.ColorAlertCritical).Bold(true)
		parts = append(parts, criticalStyle.Render(fmt.Sprintf("%d CRIT", d.criticalCount)))
	}

	if d.warningCount > 0 {
		warningStyle := lipgloss.NewStyle().Foreground(styles.ColorAlertWarning).Bold(true)
		parts = append(parts, warningStyle.Render(fmt.Sprintf("%d WARN", d.warningCount)))
	}

	return strings.Join(parts, " ")
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
	dashboardHints := windowHint + " " + heatmapHint + " [a]History [?]Help"

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
	return d.showHistory
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

// SetAlertStore sets the alert store for history data.
func (d *DashboardView) SetAlertStore(store *sqlite.AlertStore) {
	d.alertStore = store
}

// SetAlertEngine sets the alert engine for acknowledgment.
func (d *DashboardView) SetAlertEngine(engine *alerts.Engine) {
	d.alertEngine = engine
}

// SetInstanceFilter sets the instance filter and refreshes chart data (T054).
func (d *DashboardView) SetInstanceFilter(instance string) {
	d.instanceFilter = instance

	// Clear time series charts when switching instances to prevent showing stale data
	d.timeSeriesPanel.SetTPSData(nil)
	d.timeSeriesPanel.SetConnectionsData(nil)
	d.timeSeriesPanel.SetCacheHitData(nil)

	if d.heatmapVisible {
		d.updateHeatmapData()
	}
}

// GetInstanceFilter returns the current instance filter.
func (d *DashboardView) GetInstanceFilter() string {
	return d.instanceFilter
}
