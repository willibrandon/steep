// Package queries provides the Queries view for query performance monitoring.
package queries

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// SortColumn represents the available sort columns.
type SortColumn int

const (
	SortByTotalTime SortColumn = iota
	SortByCalls
	SortByMeanTime
	SortByRows
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByTotalTime:
		return "Time"
	case SortByCalls:
		return "Calls"
	case SortByMeanTime:
		return "Mean"
	case SortByRows:
		return "Rows"
	default:
		return "Unknown"
	}
}

// QueriesMode represents the current interaction mode.
type QueriesMode int

const (
	ModeNormal QueriesMode = iota
	ModeFilter
	ModeConfirmReset
	ModeConfirmEnableLogging
)

// QueriesDataMsg contains query stats data from the monitor.
type QueriesDataMsg struct {
	Stats     []sqlite.QueryStats
	FetchedAt time.Time
	Error     error
}

// QueriesView displays query performance statistics.
type QueriesView struct {
	width  int
	height int

	// State
	mode           QueriesMode
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool

	// Data
	stats      []sqlite.QueryStats
	totalCount int
	err        error

	// Table state
	selectedIdx int
	scrollOffset int
	sortColumn  SortColumn

	// Filter
	filterInput  string
	filterActive string

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Logging status
	loggingEnabled bool
	loggingChecked bool
}

// NewQueriesView creates a new queries view.
func NewQueriesView() *QueriesView {
	return &QueriesView{
		mode:       ModeNormal,
		sortColumn: SortByTotalTime,
	}
}

// Init initializes the queries view.
func (v *QueriesView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the queries view.
func (v *QueriesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			return v, cmd
		}

	case QueriesDataMsg:
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			v.stats = msg.Stats
			v.totalCount = len(msg.Stats)
			v.lastUpdate = msg.FetchedAt
			v.err = nil
			// Ensure selection is valid
			if v.selectedIdx >= len(v.stats) {
				v.selectedIdx = max(0, len(v.stats)-1)
			}
		}

	case ResetQueryStatsResultMsg:
		if msg.Error != nil {
			v.showToast("Reset failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast("Query statistics cleared", false)
			v.stats = nil
			v.totalCount = 0
			v.selectedIdx = 0
		}

	case LoggingStatusMsg:
		v.loggingChecked = true
		if msg.Error == nil {
			v.loggingEnabled = msg.Enabled
			// If logging is disabled, show prompt to enable
			if !msg.Enabled {
				v.mode = ModeConfirmEnableLogging
			}
		}

	case EnableLoggingResultMsg:
		if msg.Error != nil {
			v.showToast("Failed to enable logging: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast("Query logging enabled", false)
			v.loggingEnabled = true
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return v, nil
}

// handleKeyPress processes keyboard input.
func (v *QueriesView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle filter mode
	if v.mode == ModeFilter {
		return v.handleFilterMode(key, msg)
	}

	// Handle confirm reset mode
	if v.mode == ModeConfirmReset {
		return v.handleConfirmResetMode(key)
	}

	// Handle confirm enable logging mode
	if v.mode == ModeConfirmEnableLogging {
		return v.handleConfirmEnableLoggingMode(key)
	}

	// Normal mode keys
	switch key {
	// Navigation
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g", "home":
		v.selectedIdx = 0
		v.scrollOffset = 0
	case "G", "end":
		v.selectedIdx = max(0, len(v.stats)-1)
		v.ensureVisible()
	case "ctrl+d", "pgdown":
		v.pageDown()
	case "ctrl+u", "pgup":
		v.pageUp()

	// Sort
	case "s":
		v.cycleSort()
		return v.requestRefresh()

	// Tab navigation (left/right arrows)
	case "left":
		v.sortColumn = PrevTab(v.sortColumn)
		return v.requestRefresh()
	case "right":
		v.sortColumn = NextTab(v.sortColumn)
		return v.requestRefresh()

	// Filter
	case "/":
		v.mode = ModeFilter
		v.filterInput = v.filterActive

	// Refresh
	case "r":
		if !v.refreshing {
			v.refreshing = true
			return v.requestRefresh()
		}

	// Reset statistics
	case "R":
		if len(v.stats) > 0 {
			v.mode = ModeConfirmReset
		}

	// Enable logging (manual trigger)
	case "L":
		v.mode = ModeConfirmEnableLogging
	}

	return nil
}

// handleConfirmResetMode processes keys in confirm reset mode.
func (v *QueriesView) handleConfirmResetMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		v.mode = ModeNormal
		return func() tea.Msg {
			return ResetQueryStatsMsg{}
		}
	case "n", "N", "esc":
		v.mode = ModeNormal
	}
	return nil
}

// handleConfirmEnableLoggingMode processes keys in enable logging confirmation mode.
func (v *QueriesView) handleConfirmEnableLoggingMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		v.mode = ModeNormal
		return func() tea.Msg {
			return EnableLoggingMsg{}
		}
	case "n", "N", "esc":
		v.mode = ModeNormal
	}
	return nil
}

// showToast displays a toast message.
func (v *QueriesView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// SetLoggingDisabled shows the enable logging dialog.
func (v *QueriesView) SetLoggingDisabled() {
	v.loggingEnabled = false
	v.loggingChecked = true
	v.mode = ModeConfirmEnableLogging
}

// handleFilterMode processes keys in filter mode.
func (v *QueriesView) handleFilterMode(key string, msg tea.KeyMsg) tea.Cmd {
	switch key {
	case "esc":
		v.mode = ModeNormal
		v.filterInput = ""
	case "enter":
		v.filterActive = v.filterInput
		v.mode = ModeNormal
		return v.requestRefresh()
	case "backspace":
		if len(v.filterInput) > 0 {
			v.filterInput = v.filterInput[:len(v.filterInput)-1]
		}
	default:
		if len(key) == 1 {
			v.filterInput += key
		}
	}
	return nil
}

// moveSelection moves the selection by delta rows.
func (v *QueriesView) moveSelection(delta int) {
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(v.stats) {
		v.selectedIdx = max(0, len(v.stats)-1)
	}
	v.ensureVisible()
}

// pageDown moves down by one page.
func (v *QueriesView) pageDown() {
	pageSize := v.tableHeight()
	v.selectedIdx += pageSize
	if v.selectedIdx >= len(v.stats) {
		v.selectedIdx = max(0, len(v.stats)-1)
	}
	v.ensureVisible()
}

// pageUp moves up by one page.
func (v *QueriesView) pageUp() {
	pageSize := v.tableHeight()
	v.selectedIdx -= pageSize
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	v.ensureVisible()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (v *QueriesView) ensureVisible() {
	tableHeight := v.tableHeight()
	if tableHeight <= 0 {
		return
	}

	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	}
	if v.selectedIdx >= v.scrollOffset+tableHeight {
		v.scrollOffset = v.selectedIdx - tableHeight + 1
	}
}

// tableHeight returns the number of visible table rows.
func (v *QueriesView) tableHeight() int {
	// height - status(1) - header(1) - tabs(1) - footer(1) - padding
	return max(1, v.height-5)
}

// cycleSort cycles through sort columns.
func (v *QueriesView) cycleSort() {
	v.sortColumn = (v.sortColumn + 1) % 4
}

// requestRefresh returns a command to request data refresh.
func (v *QueriesView) requestRefresh() tea.Cmd {
	return func() tea.Msg {
		return RefreshQueriesMsg{
			SortColumn: v.sortColumn,
			Filter:     v.filterActive,
		}
	}
}

// RefreshQueriesMsg requests query data refresh.
type RefreshQueriesMsg struct {
	SortColumn SortColumn
	Filter     string
}

// ResetQueryStatsMsg requests clearing all query statistics.
type ResetQueryStatsMsg struct{}

// ResetQueryStatsResultMsg contains the result of a reset operation.
type ResetQueryStatsResultMsg struct {
	Success bool
	Error   error
}

// EnableLoggingMsg requests enabling query logging.
type EnableLoggingMsg struct{}

// EnableLoggingResultMsg contains the result of enabling logging.
type EnableLoggingResultMsg struct {
	Success bool
	Error   error
}

// CheckLoggingStatusMsg requests checking logging status.
type CheckLoggingStatusMsg struct{}

// LoggingStatusMsg contains the current logging status.
type LoggingStatusMsg struct {
	Enabled  bool
	LogPath  string
	Error    error
}

// View renders the queries view.
func (v *QueriesView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for confirm dialog overlay
	if v.mode == ModeConfirmReset {
		return v.renderWithOverlay(v.renderConfirmResetDialog())
	}
	if v.mode == ModeConfirmEnableLogging {
		return v.renderWithOverlay(v.renderEnableLoggingDialog())
	}

	// Status bar
	statusBar := v.renderStatusBar()

	// Header
	header := v.renderHeader()

	// Tab bar
	tabBar := TabBar(v.sortColumn, v.width)

	// Table
	table := v.renderTable()

	// Footer
	footer := v.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		header,
		tabBar,
		table,
		footer,
	)
}

// renderConfirmResetDialog renders the reset confirmation dialog.
func (v *QueriesView) renderConfirmResetDialog() string {
	title := styles.DialogTitleStyle.Render("Reset Query Statistics")

	details := fmt.Sprintf(
		"This will clear all %d recorded queries.\n\nThis action cannot be undone.",
		v.totalCount,
	)

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

// renderEnableLoggingDialog renders the enable logging confirmation dialog.
func (v *QueriesView) renderEnableLoggingDialog() string {
	title := styles.DialogTitleStyle.Render("Enable Query Logging")

	details := "Query logging is disabled. To collect query statistics\n" +
		"with accurate row estimates, steep needs to configure logging.\n\n" +
		"This will set:\n" +
		"  log_min_duration_statement = 0\n" +
		"  log_statement = 'all'\n" +
		"  log_parameter_max_length = -1\n" +
		"  log_error_verbosity = 'default'\n" +
		"  log_executor_stats = off\n\n" +
		"No restart required."

	prompt := "Enable logging? [y]es [n]o"

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
func (v *QueriesView) renderWithOverlay(overlay string) string {
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}

// renderStatusBar renders the top status bar.
func (v *QueriesView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

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

// renderHeader renders the column headers.
func (v *QueriesView) renderHeader() string {
	// Calculate column widths
	queryWidth := v.width - 8 - 12 - 12 - 10 - 6 // remaining space
	if queryWidth < 20 {
		queryWidth = 20
	}

	// Build header with sort indicators
	var headers []string
	headers = append(headers, padRight("Query", queryWidth))
	headers = append(headers, padLeft(v.sortIndicator("Calls", SortByCalls), 8))
	headers = append(headers, padLeft(v.sortIndicator("Time", SortByTotalTime), 12))
	headers = append(headers, padLeft(v.sortIndicator("Mean", SortByMeanTime), 12))
	headers = append(headers, padLeft(v.sortIndicator("Rows", SortByRows), 10))

	headerLine := strings.Join(headers, " ")
	return styles.TableHeaderStyle.Width(v.width - 2).Render(headerLine)
}

// sortIndicator adds an arrow to the column name if it's the sort column.
func (v *QueriesView) sortIndicator(name string, col SortColumn) string {
	if v.sortColumn == col {
		return name + " ↓"
	}
	return name
}

// renderTable renders the query table.
func (v *QueriesView) renderTable() string {
	if len(v.stats) == 0 {
		emptyMsg := "No queries recorded yet"
		if v.filterActive != "" {
			emptyMsg = "No queries match filter"
		}
		return lipgloss.NewStyle().
			Width(v.width - 2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorTextDim).
			Render(emptyMsg)
	}

	queryWidth := v.width - 8 - 12 - 12 - 10 - 6
	if queryWidth < 20 {
		queryWidth = 20
	}

	var rows []string
	tableHeight := v.tableHeight()
	endIdx := min(v.scrollOffset+tableHeight, len(v.stats))

	for i := v.scrollOffset; i < endIdx; i++ {
		stat := v.stats[i]
		isSelected := i == v.selectedIdx

		// Format row
		query := truncate(stat.NormalizedQuery, queryWidth-3)
		calls := formatCount(stat.Calls)
		total := formatDuration(stat.TotalTimeMs)
		mean := formatDuration(stat.MeanTimeMs())
		rowsVal := formatCount(stat.TotalRows)

		row := fmt.Sprintf("%s %s %s %s %s",
			padRight(query, queryWidth),
			padLeft(calls, 8),
			padLeft(total, 12),
			padLeft(mean, 12),
			padLeft(rowsVal, 10),
		)

		if isSelected {
			row = styles.TableSelectedStyle.Width(v.width - 2).Render(row)
		} else {
			row = styles.TableCellStyle.Width(v.width - 2).Render(row)
		}

		rows = append(rows, row)
	}

	// Pad to fill height
	for len(rows) < tableHeight {
		rows = append(rows, lipgloss.NewStyle().Width(v.width-2).Render(""))
	}

	return strings.Join(rows, "\n")
}

// renderFooter renders the bottom footer.
func (v *QueriesView) renderFooter() string {
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
		var filterIndicator string
		if v.filterActive != "" {
			filterIndicator = styles.FooterHintStyle.Foreground(styles.ColorActive).Render(fmt.Sprintf("[FILTERED: %s] ", v.filterActive))
		}
		hints = filterIndicator + styles.FooterHintStyle.Render("[j/k]nav [←/→]tabs [/]filter [r]efresh [R]eset [q]uit")
	}

	sortInfo := fmt.Sprintf("Sort: %s ↓", v.sortColumn.String())
	count := fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.stats)), v.totalCount)
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

// SetSize sets the dimensions of the view.
func (v *QueriesView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *QueriesView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *QueriesView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// GetSortColumn returns the current sort column.
func (v *QueriesView) GetSortColumn() SortColumn {
	return v.sortColumn
}

// GetFilter returns the current filter string.
func (v *QueriesView) GetFilter() string {
	return v.filterActive
}

// IsInputMode returns true if in filter input mode.
func (v *QueriesView) IsInputMode() bool {
	return v.mode == ModeFilter
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}

func padLeft(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return strings.Repeat(" ", width-w) + s
}

func formatDuration(ms float64) string {
	if ms < 1 {
		return "<1ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	if ms < 3600000 {
		mins := int(ms / 60000)
		secs := int(ms/1000) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(ms / 3600000)
	mins := int(ms/60000) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

func formatCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
