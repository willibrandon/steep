package activity

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/metrics"
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
	ModeHelp
)

// SortColumn represents the available sort columns for activity view.
type SortColumn int

const (
	SortByPID SortColumn = iota
	SortByUser
	SortByDatabase
	SortByState
	SortByDuration
)

func (s SortColumn) String() string {
	switch s {
	case SortByPID:
		return "PID"
	case SortByUser:
		return "User"
	case SortByDatabase:
		return "Database"
	case SortByState:
		return "State"
	case SortByDuration:
		return "Duration"
	default:
		return "PID"
	}
}

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

	// Sorting
	sortColumn SortColumn
	sortAsc    bool // false = descending (default), true = ascending

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

	// Connection metrics for sparklines
	connectionMetrics *metrics.ConnectionMetrics

	// Layout tracking for relative mouse coordinates
	// Height of view elements above data rows (statusBar + title + table header)
	viewHeaderHeight int
}

// New creates a new activity view.
func New() *ActivityView {
	t := components.NewActivityTable()
	// Set default sort on table
	t.SetSort(components.SortByDuration, false)

	return &ActivityView{
		table:      t,
		detailView: components.NewDetailView(),
		pagination: models.NewPagination(),
		filter:     models.ActivityFilter{ShowAllDatabases: true},
		mode:       ModeNormal,
		sortColumn: SortByDuration, // Default: sort by duration (longest first)
		sortAsc:    false,          // Descending by default
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
					if msg.Shift {
						// Shift+click to unselect (blur table)
						v.table.Blur()
					} else {
						// Regular click to select row
						// msg.Y is relative to view top (app translates global to relative)
						// Subtract view's own header height to get data row index
						row := msg.Y - v.viewHeaderHeight
						if row >= 0 && row < len(v.connections) {
							v.table.Focus()
							v.table.GotoTop()
							for i := 0; i < row; i++ {
								v.table.MoveDown()
							}
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
			v.err = nil
			// Use SetConnections to maintain sort order
			v.SetConnections(msg.Connections, msg.TotalCount)
			// Override lastUpdate with fetch time from message
			v.lastUpdate = msg.FetchedAt
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
	case ModeHelp:
		return v.handleHelpMode(key)
	}

	// Normal mode keys
	switch key {
	// Navigation - j/k/up/down handled by table.Update()
	case "j", "k", "up", "down":
		// Re-focus table on navigation keys
		v.table.Focus()
	case "g", "home":
		v.table.Focus()
		v.table.GotoTop()
	case "G", "end":
		v.table.Focus()
		v.table.GotoBottom()
	case "pgup", "ctrl+u":
		v.table.Focus()
		v.table.PageUp()
	case "pgdown", "ctrl+d":
		v.table.Focus()
		v.table.PageDown()
	case "esc":
		// If filter is active, clear it first; otherwise blur table to unselect
		if !v.filter.IsEmpty() {
			v.filter.Clear()
			v.filterInput = ""
			return func() tea.Msg {
				return ui.FilterChangedMsg{Filter: v.filter}
			}
		}
		// No filter active, blur table to unselect row
		v.table.Blur()

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
		// Cycle sort column
		v.cycleSortColumn()
		v.sortConnections()
		v.table.SetSort(components.SortColumn(v.sortColumn), v.sortAsc)
		v.table.SetConnections(v.connections)
	case "S":
		// Toggle sort direction
		v.sortAsc = !v.sortAsc
		v.sortConnections()
		v.table.SetSort(components.SortColumn(v.sortColumn), v.sortAsc)
		v.table.SetConnections(v.connections)

	// Help
	case "h":
		v.mode = ModeHelp
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

// handleHelpMode processes keys in help overlay mode.
func (v *ActivityView) handleHelpMode(key string) tea.Cmd {
	switch key {
	case "h", "esc", "q":
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

// IsInputMode returns true if the view is in an input mode (filter, detail, confirm, help).
func (v *ActivityView) IsInputMode() bool {
	return v.mode == ModeFilter || v.mode == ModeDetail || v.mode == ModeConfirm || v.mode == ModeHelp
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

	// Check for help overlay
	if v.mode == ModeHelp {
		return v.renderWithOverlay(HelpOverlay(v.width, v.height))
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
		hints = filterIndicator + styles.FooterHintStyle.Render("[j/k]nav [d]etail [s/S]ort [y]ank [x]kill [r]efresh [h]elp")
	}

	// Sort info on right side
	arrow := "↓"
	if v.sortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)
	count := fmt.Sprintf("%d/%d", v.table.ConnectionCount(), v.totalCount)
	rightSide := styles.FooterCountStyle.Render(sortInfo + "  " + count)

	gap := v.width - lipgloss.Width(hints) - lipgloss.Width(rightSide) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.
		Width(v.width - 2).
		Render(hints + spaces + rightSide)
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
	v.sortConnections()
	v.table.SetConnections(v.connections)
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

// SetConnectionMetrics sets the connection metrics for sparklines.
func (v *ActivityView) SetConnectionMetrics(cm *metrics.ConnectionMetrics) {
	v.connectionMetrics = cm
	v.table.SetConnectionMetrics(cm)
}

// cycleSortColumn cycles to the next sort column.
func (v *ActivityView) cycleSortColumn() {
	v.sortColumn = SortColumn((int(v.sortColumn) + 1) % 5) // 5 sort columns
}

// sortConnections sorts the connections slice by the current sort column.
func (v *ActivityView) sortConnections() {
	if len(v.connections) == 0 {
		return
	}

	sort.Slice(v.connections, func(i, j int) bool {
		a, b := v.connections[i], v.connections[j]

		var less bool
		switch v.sortColumn {
		case SortByPID:
			less = a.PID < b.PID
		case SortByUser:
			less = a.User < b.User
		case SortByDatabase:
			less = a.Database < b.Database
		case SortByState:
			less = a.State < b.State
		case SortByDuration:
			// Sort by duration (longer durations first by default)
			less = a.DurationSeconds > b.DurationSeconds
		default:
			less = a.PID < b.PID
		}

		// Reverse if ascending
		if v.sortAsc {
			return !less
		}
		return less
	})
}

// GetSortColumn returns the current sort column.
func (v *ActivityView) GetSortColumn() SortColumn {
	return v.sortColumn
}

// IsSortAsc returns whether sorting is ascending.
func (v *ActivityView) IsSortAsc() bool {
	return v.sortAsc
}
