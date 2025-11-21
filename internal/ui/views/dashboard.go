package views

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// DashboardMode represents the current interaction mode.
type DashboardMode int

const (
	ModeNormal DashboardMode = iota
	ModeFilter
	ModeDetail
	ModeConfirm
)

// DashboardView represents the main dashboard with activity table.
type DashboardView struct {
	width  int
	height int

	// Components
	table        *components.ActivityTable
	detailView   *components.DetailView
	metricsPanel *components.MetricsPanel

	// State
	mode           DashboardMode
	connected      bool
	connectionInfo string
	filter         models.ActivityFilter
	pagination     *models.Pagination
	lastUpdate     time.Time
	refreshing     bool

	// Data
	connections []models.Connection
	totalCount  int
	metrics     models.Metrics
	err         error

	// Filter input
	filterInput string

	// Confirm dialog
	confirmAction string
	confirmPID    int

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Read-only mode
	readOnly bool

	// Our own PIDs (to warn about self-kill)
	ownPIDs []int
}

// NewDashboard creates a new dashboard view.
func NewDashboard() *DashboardView {
	return &DashboardView{
		table:        components.NewActivityTable(),
		detailView:   components.NewDetailView(),
		metricsPanel: components.NewMetricsPanel(),
		pagination:   models.NewPagination(),
		filter:       models.ActivityFilter{ShowAllDatabases: true},
		mode:         ModeNormal,
	}
}

// Init initializes the dashboard view.
func (d *DashboardView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the dashboard view.
func (d *DashboardView) Update(msg tea.Msg) (ViewModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := d.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case ui.ActivityDataMsg:
		d.refreshing = false
		if msg.Error != nil {
			d.err = msg.Error
		} else {
			d.connections = msg.Connections
			d.totalCount = msg.TotalCount
			d.lastUpdate = msg.FetchedAt
			d.err = nil
			d.table.SetConnections(d.connections)
			d.pagination.Update(d.totalCount)
		}

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

	case ui.CancelQueryResultMsg:
		if msg.Error != nil {
			d.showToast(fmt.Sprintf("Failed to cancel PID %d: %s", msg.PID, msg.Error), true)
		} else if msg.Success {
			d.showToast(fmt.Sprintf("Query cancelled (PID %d)", msg.PID), false)
		} else {
			d.showToast(fmt.Sprintf("Cancel failed for PID %d (process may have ended)", msg.PID), true)
		}

	case ui.TerminateConnectionResultMsg:
		if msg.Error != nil {
			d.showToast(fmt.Sprintf("Failed to terminate PID %d: %s", msg.PID, msg.Error), true)
		} else if msg.Success {
			d.showToast(fmt.Sprintf("Connection terminated (PID %d)", msg.PID), false)
		} else {
			d.showToast(fmt.Sprintf("Terminate failed for PID %d (process may have ended)", msg.PID), true)
		}
	}

	// Update table component
	var cmd tea.Cmd
	d.table, cmd = d.table.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Update detail view if in detail mode
	if d.mode == ModeDetail {
		d.detailView, cmd = d.detailView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return d, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input.
func (d *DashboardView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle mode-specific keys
	switch d.mode {
	case ModeFilter:
		return d.handleFilterMode(key, msg)
	case ModeDetail:
		return d.handleDetailMode(key)
	case ModeConfirm:
		return d.handleConfirmMode(key)
	}

	// Normal mode keys
	switch key {
	// Navigation
	case "j", "down":
		d.table.MoveDown()
	case "k", "up":
		d.table.MoveUp()
	case "g", "home":
		d.table.GotoTop()
	case "G", "end":
		d.table.GotoBottom()
	case "pgup", "ctrl+u":
		d.table.PageUp()
	case "pgdown", "ctrl+d":
		d.table.PageDown()

	// Actions
	case "d", "enter":
		d.enterDetailMode()
	case "c":
		d.enterConfirmMode("cancel")
	case "x":
		d.enterConfirmMode("terminate")
	case "r":
		d.refreshing = true
		// The actual refresh command would be sent by the parent

	// Filter
	case "/":
		d.enterFilterMode()
	case "s":
		// TODO: Cycle sort column

	// Help
	case "?":
		// TODO: Show help overlay
	}

	return nil
}

// handleFilterMode processes keys in filter mode.
func (d *DashboardView) handleFilterMode(key string, msg tea.KeyMsg) tea.Cmd {
	switch key {
	case "esc":
		d.mode = ModeNormal
		d.filterInput = ""
	case "enter":
		d.filter.QueryFilter = d.filterInput
		d.mode = ModeNormal
		// Return command to refresh with new filter
	case "backspace":
		if len(d.filterInput) > 0 {
			d.filterInput = d.filterInput[:len(d.filterInput)-1]
		}
	default:
		// Add character to filter input
		if len(key) == 1 {
			d.filterInput += key
		}
	}
	return nil
}

// handleDetailMode processes keys in detail mode.
func (d *DashboardView) handleDetailMode(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		d.mode = ModeNormal
	case "c":
		d.enterConfirmMode("cancel")
	case "x":
		d.enterConfirmMode("terminate")
	}
	return nil
}

// handleConfirmMode processes keys in confirm mode.
func (d *DashboardView) handleConfirmMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		d.mode = ModeNormal
		// Return command to execute the action
		if d.confirmAction == "cancel" {
			return func() tea.Msg {
				return ui.CancelQueryMsg{PID: d.confirmPID}
			}
		}
		return func() tea.Msg {
			return ui.TerminateConnectionMsg{PID: d.confirmPID}
		}
	case "n", "N", "esc":
		d.mode = ModeNormal
	}
	return nil
}

// enterFilterMode switches to filter input mode.
func (d *DashboardView) enterFilterMode() {
	d.mode = ModeFilter
	d.filterInput = d.filter.QueryFilter
}

// enterDetailMode switches to detail view mode.
func (d *DashboardView) enterDetailMode() {
	conn := d.table.SelectedConnection()
	if conn != nil {
		d.mode = ModeDetail
		d.detailView.SetConnection(conn)
	}
}

// enterConfirmMode switches to confirmation dialog mode.
func (d *DashboardView) enterConfirmMode(action string) {
	// Check read-only mode
	if d.readOnly {
		d.showToast("Read-only mode: kill actions disabled", true)
		return
	}

	conn := d.table.SelectedConnection()
	if conn == nil {
		return
	}

	// Check for self-kill
	for _, pid := range d.ownPIDs {
		if conn.PID == pid {
			d.showToast(fmt.Sprintf("Warning: PID %d is your own connection!", conn.PID), true)
			// Still allow it, just warn
			break
		}
	}

	d.mode = ModeConfirm
	d.confirmAction = action
	d.confirmPID = conn.PID
}

// showToast displays a toast message.
func (d *DashboardView) showToast(message string, isError bool) {
	d.toastMessage = message
	d.toastError = isError
	d.toastTime = time.Now()
}

// SetReadOnly sets read-only mode.
func (d *DashboardView) SetReadOnly(readOnly bool) {
	d.readOnly = readOnly
}

// SetOwnPIDs sets the PIDs of our own connections.
func (d *DashboardView) SetOwnPIDs(pids []int) {
	d.ownPIDs = pids
}

// View renders the dashboard view.
func (d *DashboardView) View() string {
	if !d.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for detail mode overlay
	if d.mode == ModeDetail {
		return d.renderWithOverlay(d.detailView.View())
	}

	// Check for confirm dialog overlay
	if d.mode == ModeConfirm {
		return d.renderWithOverlay(d.renderConfirmDialog())
	}

	return d.renderMain()
}

// renderMain renders the main dashboard view.
func (d *DashboardView) renderMain() string {
	// Status bar
	statusBar := d.renderStatusBar()

	// Metrics panel
	metricsPanel := d.renderMetricsPanel()

	// Activity table
	tableView := d.table.View()

	// Footer
	footer := d.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		metricsPanel,
		tableView,
		footer,
	)
}

// renderStatusBar renders the top status bar.
func (d *DashboardView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(d.connectionInfo)
	timestamp := styles.StatusTimeStyle.Render(d.lastUpdate.Format("2006-01-02 15:04:05"))

	gap := d.width - lipgloss.Width(title) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(d.width - 2).
		Render(title + spaces + timestamp)
}

// renderMetricsPanel renders the metrics panel.
func (d *DashboardView) renderMetricsPanel() string {
	return d.metricsPanel.View()
}

// renderFooter renders the bottom footer with hints and pagination.
func (d *DashboardView) renderFooter() string {
	var hints string

	// Show toast message if recent (within 3 seconds)
	if d.toastMessage != "" && time.Since(d.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if d.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(d.toastMessage)
	} else if d.mode == ModeFilter {
		hints = fmt.Sprintf("Filter: %s_", d.filterInput)
	} else {
		hints = styles.FooterHintStyle.Render("[/]filter [s]ort [d]etail [c]ancel [x]kill [r]efresh [?]help [q]uit")
	}

	count := styles.FooterCountStyle.Render(fmt.Sprintf("%d/%d", d.table.ConnectionCount(), d.totalCount))

	gap := d.width - lipgloss.Width(hints) - lipgloss.Width(count) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.
		Width(d.width - 2).
		Render(hints + spaces + count)
}

// renderConfirmDialog renders the confirmation dialog.
func (d *DashboardView) renderConfirmDialog() string {
	var actionText string
	if d.confirmAction == "cancel" {
		actionText = "Cancel Query"
	} else {
		actionText = "Terminate Connection"
	}

	title := styles.DialogTitleStyle.Render(actionText)

	conn := d.table.SelectedConnection()
	var details string
	if conn != nil {
		details = fmt.Sprintf(
			"PID: %d\nUser: %s\nQuery: %s",
			conn.PID,
			conn.User,
			conn.TruncateQuery(40),
		)
	}

	prompt := "Are you sure? [y]es [n]o"

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		details,
		"",
		prompt,
	)

	return styles.DialogStyle.Render(content)
}

// renderWithOverlay renders the main view with an overlay on top.
func (d *DashboardView) renderWithOverlay(overlay string) string {
	// Simple overlay - in production would use proper compositing
	return lipgloss.Place(
		d.width, d.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}

// SetSize sets the dimensions of the dashboard view.
func (d *DashboardView) SetSize(width, height int) {
	d.width = width
	d.height = height

	// Allocate space: status bar (3) + metrics (2) + footer (3)
	tableHeight := height - 8
	if tableHeight < 5 {
		tableHeight = 5
	}

	d.table.SetSize(width-2, tableHeight)
	d.detailView.SetSize(width-10, height-10)
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

// SetConnections updates the activity data.
func (d *DashboardView) SetConnections(connections []models.Connection, totalCount int) {
	d.connections = connections
	d.totalCount = totalCount
	d.table.SetConnections(connections)
	d.pagination.Update(totalCount)
	d.lastUpdate = time.Now()
}

// GetFilter returns the current filter settings.
func (d *DashboardView) GetFilter() models.ActivityFilter {
	return d.filter
}

// GetPagination returns the current pagination settings.
func (d *DashboardView) GetPagination() *models.Pagination {
	return d.pagination
}

// IsRefreshing returns whether a refresh is in progress.
func (d *DashboardView) IsRefreshing() bool {
	return d.refreshing
}

// SetServerVersion sets the server version (for compatibility).
func (d *DashboardView) SetServerVersion(version string) {
	// Version could be included in connection info
}

// SetDatabase sets the database name (for compatibility).
func (d *DashboardView) SetDatabase(database string) {
	// Database is included in connection info
}
