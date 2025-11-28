package activity

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views"
)

// Mode represents the current interaction mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeFilter
	ModeDetail
	ModeConfirm
)

// ActivityView displays and manages PostgreSQL connections.
type ActivityView struct {
	width  int
	height int

	// Components
	table      *components.ActivityTable
	detailView *components.DetailView

	// State
	mode           Mode
	connected      bool
	connectionInfo string
	filter         models.ActivityFilter
	pagination     *models.Pagination
	lastUpdate     time.Time
	refreshing     bool

	// Data
	connections []models.Connection
	totalCount  int
	err         error

	// Filter input
	filterInput string

	// Confirm dialog
	confirmAction   string
	confirmPID      int
	confirmSelfKill bool

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Read-only mode
	readOnly bool

	// Our own PIDs (to warn about self-kill)
	ownPIDs []int

	// Layout tracking for relative mouse coordinates
	// Height of view elements above data rows (statusBar + title + table header)
	viewHeaderHeight int
}

// New creates a new activity view.
func New() *ActivityView {
	return &ActivityView{
		table:      components.NewActivityTable(),
		detailView: components.NewDetailView(),
		pagination: models.NewPagination(),
		filter:     models.ActivityFilter{ShowAllDatabases: true},
		mode:       ModeNormal,
	}
}

// Init initializes the activity view.
func (v *ActivityView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the activity view.
func (v *ActivityView) Update(msg tea.Msg) (views.ViewModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMsg:
		if v.mode == ModeNormal && len(v.connections) > 0 {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.table.MoveUp()
			case tea.MouseButtonWheelDown:
				v.table.MoveDown()
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					// msg.Y is relative to view top (app translates global to relative)
					// Subtract view's own header height to get data row index
					row := msg.Y - v.viewHeaderHeight
					if row >= 0 && row < len(v.connections) {
						v.table.GotoTop()
						for i := 0; i < row; i++ {
							v.table.MoveDown()
						}
					}
				}
			}
		}
		return v, nil

	case ui.ActivityDataMsg:
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			v.connections = msg.Connections
			v.totalCount = msg.TotalCount
			v.lastUpdate = msg.FetchedAt
			v.err = nil
			v.table.SetConnections(v.connections)
			v.pagination.Update(v.totalCount)
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case ui.CancelQueryResultMsg:
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Failed to cancel PID %d: %s", msg.PID, msg.Error), true)
		} else if msg.Success {
			v.showToast(fmt.Sprintf("Query cancelled (PID %d)", msg.PID), false)
		} else {
			v.showToast(fmt.Sprintf("Cancel failed for PID %d (process may have ended)", msg.PID), true)
		}

	case ui.TerminateConnectionResultMsg:
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Failed to terminate PID %d: %s", msg.PID, msg.Error), true)
		} else if msg.Success {
			v.showToast(fmt.Sprintf("Connection terminated (PID %d)", msg.PID), false)
		} else {
			v.showToast(fmt.Sprintf("Terminate failed for PID %d (process may have ended)", msg.PID), true)
		}
	}

	// Update table component
	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Update detail view if in detail mode
	if v.mode == ModeDetail {
		v.detailView, cmd = v.detailView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return v, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input.
func (v *ActivityView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle mode-specific keys
	switch v.mode {
	case ModeFilter:
		return v.handleFilterMode(key)
	case ModeDetail:
		return v.handleDetailMode(key)
	case ModeConfirm:
		return v.handleConfirmMode(key)
	}

	// Normal mode keys
	switch key {
	// Navigation - j/k/up/down handled by table.Update()
	case "g", "home":
		v.table.GotoTop()
	case "G", "end":
		v.table.GotoBottom()
	case "pgup", "ctrl+u":
		v.table.PageUp()
	case "pgdown", "ctrl+d":
		v.table.PageDown()

	// Actions
	case "d", "enter":
		v.enterDetailMode()
	case "c":
		v.enterConfirmMode("cancel")
	case "x":
		v.enterConfirmMode("terminate")
	case "r":
		if !v.refreshing {
			v.refreshing = true
			return func() tea.Msg {
				return ui.RefreshRequestMsg{}
			}
		}

	// Filter
	case "/":
		v.enterFilterMode()
	case "a":
		// Toggle all databases filter and clear any specific database filter
		if v.filter.DatabaseFilter != "" {
			// If there's a specific db filter, clear it and show all
			v.filter.DatabaseFilter = ""
			v.filter.ShowAllDatabases = true
		} else {
			// Otherwise toggle
			v.filter.ShowAllDatabases = !v.filter.ShowAllDatabases
		}
		return func() tea.Msg {
			return ui.FilterChangedMsg{Filter: v.filter}
		}
	case "C":
		// Clear all filters
		v.filter.Clear()
		v.filterInput = ""
		return func() tea.Msg {
			return ui.FilterChangedMsg{Filter: v.filter}
		}
	case "s":
		// TODO: Cycle sort column

	// Help
	case "?":
		// TODO: Show help overlay
	}

	return nil
}

// handleFilterMode processes keys in filter mode.
func (v *ActivityView) handleFilterMode(key string) tea.Cmd {
	switch key {
	case "esc":
		v.mode = ModeNormal
		v.filterInput = ""
	case "enter":
		// Parse prefix syntax: db:, state:, query:, user:
		input := v.filterInput

		// Clear all filters first
		v.filter.StateFilter = ""
		v.filter.DatabaseFilter = ""
		v.filter.QueryFilter = ""

		if strings.HasPrefix(strings.ToLower(input), "db:") {
			v.filter.DatabaseFilter = strings.TrimPrefix(input[3:], " ")
		} else if strings.HasPrefix(strings.ToLower(input), "state:") {
			v.filter.StateFilter = strings.ToLower(strings.TrimPrefix(input[6:], " "))
		} else if strings.HasPrefix(strings.ToLower(input), "query:") {
			v.filter.QueryFilter = strings.TrimPrefix(input[6:], " ")
		} else {
			// Default: filter by query text
			v.filter.QueryFilter = input
		}

		v.mode = ModeNormal
		// Return command to refresh with new filter
		return func() tea.Msg {
			return ui.FilterChangedMsg{Filter: v.filter}
		}
	case "backspace":
		if len(v.filterInput) > 0 {
			v.filterInput = v.filterInput[:len(v.filterInput)-1]
		}
	default:
		// Add character to filter input
		if len(key) == 1 {
			v.filterInput += key
		}
	}
	return nil
}

// handleDetailMode processes keys in detail mode.
func (v *ActivityView) handleDetailMode(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		v.mode = ModeNormal
	case "c":
		v.enterConfirmMode("cancel")
	case "x":
		v.enterConfirmMode("terminate")
	}
	return nil
}

// handleConfirmMode processes keys in confirm mode.
func (v *ActivityView) handleConfirmMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		v.mode = ModeNormal
		// Return command to execute the action
		if v.confirmAction == "cancel" {
			return func() tea.Msg {
				return ui.CancelQueryMsg{PID: v.confirmPID}
			}
		}
		return func() tea.Msg {
			return ui.TerminateConnectionMsg{PID: v.confirmPID}
		}
	case "n", "N", "esc":
		v.mode = ModeNormal
	}
	return nil
}

// enterFilterMode switches to filter input mode.
func (v *ActivityView) enterFilterMode() {
	v.mode = ModeFilter
	v.filterInput = v.filter.QueryFilter
}

// enterDetailMode switches to detail view mode.
func (v *ActivityView) enterDetailMode() {
	conn := v.table.SelectedConnection()
	if conn != nil {
		v.mode = ModeDetail
		v.detailView.SetConnection(conn)
	}
}

// enterConfirmMode switches to confirmation dialog mode.
func (v *ActivityView) enterConfirmMode(action string) {
	// Check read-only mode
	if v.readOnly {
		v.showToast("Read-only mode: kill actions disabled", true)
		return
	}

	conn := v.table.SelectedConnection()
	if conn == nil {
		return
	}

	// Check for self-kill
	v.confirmSelfKill = false
	for _, pid := range v.ownPIDs {
		if conn.PID == pid {
			v.confirmSelfKill = true
			break
		}
	}

	v.mode = ModeConfirm
	v.confirmAction = action
	v.confirmPID = conn.PID
}

// showToast displays a toast message.
func (v *ActivityView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// SetReadOnly sets read-only mode.
func (v *ActivityView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
}

// SetOwnPIDs sets the PIDs of our own connections.
func (v *ActivityView) SetOwnPIDs(pids []int) {
	v.ownPIDs = pids
}

// IsInputMode returns true if the view is in an input mode (filter, detail, confirm).
func (v *ActivityView) IsInputMode() bool {
	return v.mode == ModeFilter || v.mode == ModeDetail || v.mode == ModeConfirm
}

// View renders the activity view.
func (v *ActivityView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for detail mode overlay
	if v.mode == ModeDetail {
		return v.renderWithOverlay(v.detailView.View())
	}

	// Check for confirm dialog overlay
	if v.mode == ModeConfirm {
		return v.renderWithOverlay(v.renderConfirmDialog())
	}

	return v.renderMain()
}

// renderMain renders the main activity view.
func (v *ActivityView) renderMain() string {
	// Status bar (connection info)
	statusBar := v.renderStatusBar()

	// View title
	title := v.renderTitle()

	// Activity table
	tableView := v.table.View()

	// Footer
	footer := v.renderFooter()

	// Calculate view header height for mouse coordinate translation
	// This is the height of everything above the data rows within this view
	statusBarHeight := lipgloss.Height(statusBar)
	titleHeight := lipgloss.Height(title)
	// ActivityTable component has fixed header: top border (1) + header row (1) + separator (1) = 3 lines
	tableComponentHeaderHeight := 3
	v.viewHeaderHeight = statusBarHeight + titleHeight + tableComponentHeaderHeight

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		title,
		tableView,
		footer,
	)
}

// renderTitle renders the view title.
func (v *ActivityView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)

	return titleStyle.Render("Activity Monitoring")
}

// renderStatusBar renders the status bar with connection info.
func (v *ActivityView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Show stale indicator if data is older than 5 seconds
	var staleIndicator string
	if !v.lastUpdate.IsZero() && time.Since(v.lastUpdate) > 5*time.Second {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	timestamp := styles.StatusTimeStyle.Render(v.lastUpdate.Format("2006-01-02 15:04:05"))

	gap := v.width - lipgloss.Width(title) - lipgloss.Width(staleIndicator) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + staleIndicator + spaces + timestamp)
}

// renderFooter renders the bottom footer with hints and pagination.
func (v *ActivityView) renderFooter() string {
	var hints string

	// Show toast message if recent (within 3 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else if v.mode == ModeFilter {
		hints = fmt.Sprintf("Filter: %s_", v.filterInput)
	} else {
		// Build filter indicator
		var filterIndicator string
		if !v.filter.IsEmpty() {
			filterIndicator = styles.FooterHintStyle.Foreground(styles.ColorActive).Render("[FILTERED] ")
		}
		if !v.filter.ShowAllDatabases {
			filterIndicator += styles.FooterHintStyle.Foreground(styles.ColorActive).Render("[DB] ")
		}
		hints = filterIndicator + styles.FooterHintStyle.Render("[/]filter [r]efresh [a]ll-dbs [C]lear [d]etail [c]ancel [x]kill")
	}

	count := styles.FooterCountStyle.Render(fmt.Sprintf("%d/%d", v.table.ConnectionCount(), v.totalCount))

	gap := v.width - lipgloss.Width(hints) - lipgloss.Width(count) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.
		Width(v.width - 2).
		Render(hints + spaces + count)
}

// renderConfirmDialog renders the confirmation dialog.
func (v *ActivityView) renderConfirmDialog() string {
	var actionText string
	if v.confirmAction == "cancel" {
		actionText = "Cancel Query"
	} else {
		actionText = "Terminate Connection"
	}

	title := styles.DialogTitleStyle.Render(actionText)

	conn := v.table.SelectedConnection()
	var details string
	if conn != nil {
		details = fmt.Sprintf(
			"PID: %d\nUser: %s\nQuery: %s",
			conn.PID,
			conn.User,
			conn.TruncateQuery(40),
		)
	}

	var warning string
	if v.confirmSelfKill {
		warning = styles.ErrorStyle.Render("⚠ WARNING: This is your own connection!")
	}

	prompt := "Are you sure? [y]es [n]o"

	var content string
	if warning != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			warning,
			"",
			details,
			"",
			prompt,
		)
	} else {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			details,
			"",
			prompt,
		)
	}

	return styles.DialogStyle.Render(content)
}

// renderWithOverlay renders the main view with an overlay on top.
func (v *ActivityView) renderWithOverlay(overlay string) string {
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}

// SetSize sets the dimensions of the activity view.
func (v *ActivityView) SetSize(width, height int) {
	v.width = width
	v.height = height

	// Allocate space: title (1) + title bar (3) + footer (3)
	tableHeight := height - 7
	if tableHeight < 5 {
		tableHeight = 5
	}

	v.table.SetSize(width-2, tableHeight)
	v.detailView.SetSize(width-10, height-10)
}

// SetConnected sets the connection status.
func (v *ActivityView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *ActivityView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetConnections updates the activity data.
func (v *ActivityView) SetConnections(connections []models.Connection, totalCount int) {
	v.connections = connections
	v.totalCount = totalCount
	v.table.SetConnections(connections)
	v.pagination.Update(totalCount)
	v.lastUpdate = time.Now()
}

// GetFilter returns the current filter settings.
func (v *ActivityView) GetFilter() models.ActivityFilter {
	return v.filter
}

// GetPagination returns the current pagination settings.
func (v *ActivityView) GetPagination() *models.Pagination {
	return v.pagination
}

// IsRefreshing returns whether a refresh is in progress.
func (v *ActivityView) IsRefreshing() bool {
	return v.refreshing
}
