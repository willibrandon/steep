// Package logs provides the Log Viewer view.
package logs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/monitors"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// LogsMode represents the current interaction mode.
type LogsMode int

const (
	ModeNormal LogsMode = iota
	ModeHelp
	ModeSearch
	ModeCommand
	ModeConfirmEnableLogging
	ModeLoading
)

// LogsView displays PostgreSQL server logs in real-time.
type LogsView struct {
	width  int
	height int

	// State
	mode           LogsMode
	connected      bool
	connectionInfo string
	lastUpdate     time.Time

	// Database
	pool *pgxpool.Pool

	// Data
	buffer *LogBuffer
	filter *LogFilter
	source *monitors.LogSource
	err    error

	// Viewport for scrolling
	viewport      viewport.Model
	contentHeight int  // Height available for log content
	needsRebuild  bool // Whether viewport content needs rebuilding

	// Windowed rendering - only format/display visible entries
	scrollOffset      int      // Lines from bottom (0 = at bottom/follow mode)
	styledLines       []string // Pre-formatted lines (styled on arrival)
	filteredLineCount int      // Count of lines after filtering (for status bar)

	// Follow mode (auto-scroll to newest)
	followMode bool

	// Log collection
	collector      *LogCollector
	loggingEnabled bool
	loggingChecked bool

	// Search state
	searchInput  string // Current search input
	matchIndices []int  // Indices of matching entries
	currentMatch int    // Current match index for n/N navigation

	// Command state
	commandInput string // Current command input
	toastMessage string // Toast message to display
	toastIsError bool   // Whether toast is an error
	toastTime    time.Time

	// Read-only mode
	readOnly bool
}

// NewLogsView creates a new log viewer.
func NewLogsView() *LogsView {
	return &LogsView{
		mode:          ModeNormal,
		buffer:        NewLogBuffer(DefaultBufferCapacity),
		filter:        NewLogFilter(),
		source:        &monitors.LogSource{},
		followMode:    true,
		viewport:      viewport.New(80, 20),
		contentHeight: 20,
	}
}

// Init initializes the view.
func (v *LogsView) Init() tea.Cmd {
	// Will check logging status when pool is set
	return nil
}

// SetPool sets the database pool for log collection.
func (v *LogsView) SetPool(pool *pgxpool.Pool) {
	v.pool = pool
}

// CheckLoggingStatus triggers a check of PostgreSQL logging configuration.
func (v *LogsView) CheckLoggingStatus() tea.Cmd {
	if v.pool == nil {
		return nil
	}
	return func() tea.Msg {
		return CheckLoggingStatusMsg{}
	}
}

// Message types for logs view.

// CheckLoggingStatusMsg requests checking logging status.
type CheckLoggingStatusMsg struct{}

// LoggingStatusMsg contains the current logging status.
type LoggingStatusMsg struct {
	Enabled       bool
	LogDir        string
	LogPattern    string
	LogFormat     monitors.LogFormat
	Error         error
}

// EnableLoggingMsg requests enabling log collection.
type EnableLoggingMsg struct{}

// EnableLoggingResultMsg contains the result of enabling logging.
type EnableLoggingResultMsg struct {
	Success bool
	Error   error
}

// LogTickMsg triggers periodic log refresh.
type LogTickMsg struct{}

// LogEntriesMsg contains new log entries.
type LogEntriesMsg struct {
	Entries []LogEntryData
	Error   error
}

// LogEntryData is a simplified log entry for messages.
type LogEntryData struct {
	Timestamp   time.Time
	Severity    string
	PID         int
	Database    string
	User        string
	Application string
	Message     string
	Detail      string
	Hint        string
	RawLine     string
}

// Update handles messages and updates the view state.
func (v *LogsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v.handleKeyPress(msg)
	case tea.MouseMsg:
		return v.handleMouse(msg)

	case LoggingStatusMsg:
		v.loggingChecked = true
		if msg.Error != nil {
			v.err = msg.Error
			return v, nil
		}
		v.loggingEnabled = msg.Enabled
		if msg.Enabled {
			// Configure the log source
			v.source.LogDir = msg.LogDir
			v.source.LogPattern = msg.LogPattern
			v.source.Format = msg.LogFormat
			v.source.Enabled = true
			// Start collecting logs
			return v, v.startLogCollection()
		}
		// Logging is disabled - show confirmation dialog (unless read-only)
		if !v.readOnly {
			v.mode = ModeConfirmEnableLogging
		} else {
			v.showToast("Logging is disabled. Enable logging_collector in postgresql.conf", true)
		}
		return v, nil

	case EnableLoggingResultMsg:
		if msg.Error != nil {
			v.showToast("Failed to enable logging: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast("Logging enabled - restart PostgreSQL to apply", false)
			v.loggingEnabled = true
		}
		return v, nil

	case LogTickMsg:
		// Periodic log refresh
		if v.followMode && v.collector != nil {
			return v, v.fetchNewLogs()
		}
		return v, v.scheduleNextTick()

	case LogEntriesMsg:
		if msg.Error != nil {
			v.showToast("Error reading logs: "+msg.Error.Error(), true)
		} else if len(msg.Entries) > 0 {
			// Pre-style lines on arrival (not during render)
			for _, entry := range msg.Entries {
				logEntry := v.entryDataToLogEntry(entry)
				v.buffer.Add(logEntry)
				// Style once, store forever
				v.styledLines = append(v.styledLines, v.formatLogEntry(logEntry))
			}
			// Trim styled lines if buffer wrapped (keep in sync with ring buffer)
			if len(v.styledLines) > v.buffer.Cap() {
				excess := len(v.styledLines) - v.buffer.Cap()
				v.styledLines = v.styledLines[excess:]
			}
			v.lastUpdate = time.Now()
			v.needsRebuild = true
			v.rebuildViewport() // Rebuild in Update, not View
		}
		return v, v.scheduleNextTick()
	}
	return v, nil
}

// startLogCollection starts the log collector.
func (v *LogsView) startLogCollection() tea.Cmd {
	// Initialize collector if needed
	if v.collector == nil {
		v.collector = NewLogCollector(v.source)
	}
	return tea.Batch(
		v.fetchNewLogs(),
		v.scheduleNextTick(),
	)
}

// scheduleNextTick schedules the next log refresh tick.
func (v *LogsView) scheduleNextTick() tea.Cmd {
	// Use 500ms for smoother updates in follow mode
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return LogTickMsg{}
	})
}

// fetchNewLogs fetches new log entries.
func (v *LogsView) fetchNewLogs() tea.Cmd {
	if v.collector == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := v.collector.Collect()
		if err != nil {
			return LogEntriesMsg{Error: err}
		}
		return LogEntriesMsg{Entries: entries}
	}
}

// entryDataToLogEntry converts LogEntryData to a buffer entry.
func (v *LogsView) entryDataToLogEntry(data LogEntryData) models.LogEntry {
	return models.LogEntry{
		Timestamp:   data.Timestamp,
		Severity:    models.ParseSeverity(data.Severity),
		PID:         data.PID,
		Database:    data.Database,
		User:        data.User,
		Application: data.Application,
		Message:     data.Message,
		Detail:      data.Detail,
		Hint:        data.Hint,
		RawLine:     data.RawLine,
	}
}

// handleKeyPress processes keyboard input.
func (v *LogsView) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Handle mode-specific input
	switch v.mode {
	case ModeHelp:
		if key == "esc" || key == "?" || key == "q" {
			v.mode = ModeNormal
		}
		return v, nil

	case ModeSearch:
		return v.handleSearchMode(key, msg)

	case ModeCommand:
		return v.handleCommandMode(key, msg)

	case ModeConfirmEnableLogging:
		return v.handleConfirmMode(key)
	}

	// Normal mode key handling
	switch key {
	case "?", "h":
		v.mode = ModeHelp
	case "f":
		v.followMode = !v.followMode
		if v.followMode {
			v.scrollOffset = 0 // Return to bottom
			v.needsRebuild = true
		}
	case "/":
		v.mode = ModeSearch
		v.searchInput = ""
	case ":":
		v.mode = ModeCommand
		v.commandInput = ""
	case "j", "down":
		// Scroll toward newer (decrease offset from bottom)
		v.followMode = false
		if v.scrollOffset > 0 {
			v.scrollOffset--
			v.needsRebuild = true
		}
	case "k", "up":
		// Scroll toward older (increase offset from bottom)
		v.followMode = false
		maxOffset := len(v.styledLines) - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if v.scrollOffset < maxOffset {
			v.scrollOffset++
			v.needsRebuild = true
		}
	case "g":
		// Go to oldest (top)
		v.followMode = false
		maxOffset := len(v.styledLines) - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.scrollOffset = maxOffset
		v.needsRebuild = true
	case "G":
		// Go to newest (bottom) and enable follow
		v.followMode = true
		v.scrollOffset = 0
		v.needsRebuild = true
	case "ctrl+d":
		// Half page down (toward newer)
		v.followMode = false
		v.scrollOffset -= v.contentHeight / 2
		if v.scrollOffset < 0 {
			v.scrollOffset = 0
		}
		v.needsRebuild = true
	case "ctrl+u":
		// Half page up (toward older)
		v.followMode = false
		maxOffset := len(v.styledLines) - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.scrollOffset += v.contentHeight / 2
		if v.scrollOffset > maxOffset {
			v.scrollOffset = maxOffset
		}
		v.needsRebuild = true
	case "n":
		v.nextMatch()
	case "N":
		v.prevMatch()
	case "esc":
		v.clearToast()
		if v.filter.HasFilters() {
			v.filter.Clear()
			v.invalidateCache()
		}
	}

	v.rebuildViewport() // Rebuild in Update, not View
	return v, nil
}

// handleSearchMode handles input in search mode.
func (v *LogsView) handleSearchMode(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		v.mode = ModeNormal
		v.searchInput = ""
	case "enter":
		v.mode = ModeNormal
		if err := v.filter.SetSearch(v.searchInput); err != nil {
			v.showToast("Invalid regex: "+err.Error(), true)
		} else {
			v.updateMatchIndices()
			v.invalidateCache()
		}
	case "backspace":
		if len(v.searchInput) > 0 {
			v.searchInput = v.searchInput[:len(v.searchInput)-1]
		}
	default:
		if len(key) == 1 {
			v.searchInput += key
		}
	}
	return v, nil
}

// handleCommandMode handles input in command mode.
func (v *LogsView) handleCommandMode(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		v.mode = ModeNormal
		v.commandInput = ""
	case "enter":
		v.mode = ModeNormal
		v.executeCommand(v.commandInput)
		v.commandInput = ""
	case "backspace":
		if len(v.commandInput) > 0 {
			v.commandInput = v.commandInput[:len(v.commandInput)-1]
		}
	default:
		if len(key) == 1 {
			v.commandInput += key
		}
	}
	return v, nil
}

// handleConfirmMode handles input in confirmation dialog mode.
func (v *LogsView) handleConfirmMode(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		v.mode = ModeNormal
		return v, func() tea.Msg {
			return EnableLoggingMsg{}
		}
	case "n", "N", "esc":
		v.mode = ModeNormal
		v.showToast("Logging not enabled - manual configuration required", true)
	}
	return v, nil
}

// handleMouse handles mouse input.
func (v *LogsView) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		// Scroll toward older (increase offset)
		v.followMode = false
		maxOffset := len(v.styledLines) - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.scrollOffset += 3
		if v.scrollOffset > maxOffset {
			v.scrollOffset = maxOffset
		}
		v.needsRebuild = true
	case tea.MouseButtonWheelDown:
		// Scroll toward newer (decrease offset)
		v.scrollOffset -= 3
		if v.scrollOffset < 0 {
			v.scrollOffset = 0
		}
		v.needsRebuild = true
	}
	v.rebuildViewport() // Rebuild in Update, not View
	return v, nil
}

// executeCommand parses and executes a command.
func (v *LogsView) executeCommand(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}

	parts := strings.Fields(cmd)
	command := strings.ToLower(parts[0])

	switch command {
	case "level":
		v.executeLevelCommand(parts[1:])
	case "goto":
		v.executeGotoCommand(parts[1:])
	default:
		v.showToast("Unknown command: "+command, true)
	}
}

// executeLevelCommand handles :level <level> command.
func (v *LogsView) executeLevelCommand(args []string) {
	if len(args) == 0 {
		// Show current level
		if levelStr := v.filter.LevelString(); levelStr != "" {
			v.showToast("Current level filter: "+levelStr, false)
		} else {
			v.showToast("No level filter active (showing all)", false)
		}
		return
	}

	level := strings.ToLower(args[0])
	if err := v.filter.SetLevel(level); err != nil {
		v.showToast("Invalid level: "+level, true)
		return
	}

	// Update viewport with filtered content
	v.invalidateCache()
	v.rebuildViewport()

	if level == "all" || level == "clear" {
		v.showToast("Level filter cleared", false)
	} else {
		v.showToast("Filtering by level: "+v.filter.LevelString(), false)
	}
}

// executeGotoCommand handles :goto <timestamp> command.
func (v *LogsView) executeGotoCommand(args []string) {
	// TODO: Implement in Phase 6 (US4)
	v.showToast("Goto command not yet implemented", true)
}

// updateMatchIndices finds all entries matching the current search.
func (v *LogsView) updateMatchIndices() {
	v.matchIndices = nil
	v.currentMatch = -1

	if v.filter.SearchPattern == nil {
		return
	}

	entries := v.buffer.GetAll()
	for i, entry := range entries {
		if v.filter.Matches(entry) {
			v.matchIndices = append(v.matchIndices, i)
		}
	}

	if len(v.matchIndices) > 0 {
		v.currentMatch = 0
	}
}

// nextMatch navigates to the next search match.
func (v *LogsView) nextMatch() {
	if len(v.matchIndices) == 0 {
		return
	}
	v.currentMatch = (v.currentMatch + 1) % len(v.matchIndices)
	v.scrollToMatch()
}

// prevMatch navigates to the previous search match.
func (v *LogsView) prevMatch() {
	if len(v.matchIndices) == 0 {
		return
	}
	v.currentMatch--
	if v.currentMatch < 0 {
		v.currentMatch = len(v.matchIndices) - 1
	}
	v.scrollToMatch()
}

// scrollToMatch scrolls the viewport to show the current match.
func (v *LogsView) scrollToMatch() {
	if v.currentMatch >= 0 && v.currentMatch < len(v.matchIndices) {
		v.followMode = false
		lineNum := v.matchIndices[v.currentMatch]
		// Convert line number to offset from bottom
		totalLines := len(v.styledLines)
		v.scrollOffset = totalLines - lineNum - v.contentHeight/2
		if v.scrollOffset < 0 {
			v.scrollOffset = 0
		}
		maxOffset := totalLines - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if v.scrollOffset > maxOffset {
			v.scrollOffset = maxOffset
		}
		v.needsRebuild = true
	}
}

// rebuildViewport rebuilds the viewport content using windowing.
// Only renders visible lines (+ small buffer) instead of all 10K lines.
func (v *LogsView) rebuildViewport() {
	if !v.needsRebuild {
		return
	}
	v.needsRebuild = false

	totalLines := len(v.styledLines)
	if totalLines == 0 {
		v.viewport.SetContent("")
		v.filteredLineCount = 0
		return
	}

	// Window size: visible height * 2 for some scroll buffer
	// This is what we actually feed to the viewport
	windowSize := v.contentHeight * 2
	if windowSize < 50 {
		windowSize = 50
	}

	// Get the lines to display based on scroll position
	var linesToShow []string

	if v.filter.HasFilters() {
		// When filtering, we need to filter from raw entries
		// This is slower but necessary for search/level filters
		entries := v.buffer.GetAll()
		filtered := v.filter.FilterEntries(entries)
		for _, entry := range filtered {
			linesToShow = append(linesToShow, v.formatLogEntry(entry))
		}
	} else {
		// No filter - use pre-styled lines directly
		linesToShow = v.styledLines
	}

	totalFiltered := len(linesToShow)
	v.filteredLineCount = totalFiltered

	if totalFiltered == 0 {
		v.viewport.SetContent(styles.MutedStyle.Render("No entries match current filter"))
		return
	}

	// Clamp scrollOffset to filtered count (important when filter reduces entries)
	maxOffset := totalFiltered - v.contentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.scrollOffset > maxOffset {
		v.scrollOffset = maxOffset
	}

	// Calculate window bounds
	// scrollOffset = 0 means we're at the bottom (newest)
	// scrollOffset = N means we're N lines up from bottom
	endIdx := totalFiltered - v.scrollOffset
	if endIdx > totalFiltered {
		endIdx = totalFiltered
	}
	if endIdx < 0 {
		endIdx = 0
	}

	startIdx := endIdx - windowSize
	if startIdx < 0 {
		startIdx = 0
	}

	// Slice the window
	window := linesToShow[startIdx:endIdx]

	// Feed only the windowed content to viewport
	v.viewport.SetContent(strings.Join(window, "\n"))

	// Always position viewport at bottom of window.
	// Our window is constructed with buffer lines BEFORE the current view,
	// so the "current" visible content is always at the end of the window.
	v.viewport.GotoBottom()
}

// formatLogEntry formats a log entry for display.
func (v *LogsView) formatLogEntry(entry models.LogEntry) string {
	// Timestamp
	timestamp := styles.LogTimestampStyle.Render(entry.Timestamp.Format("15:04:05"))

	// Severity badge
	severityBadge := styles.SeverityBadge(entry.Severity).Render(entry.Severity.String())

	// PID if available
	pidStr := ""
	if entry.PID > 0 {
		pidStr = styles.LogPIDStyle.Render(fmt.Sprintf("[%d]", entry.PID)) + " "
	}

	// Context (database/user if available)
	contextStr := ""
	if entry.Database != "" || entry.User != "" {
		if entry.User != "" && entry.Database != "" {
			contextStr = styles.MutedStyle.Render(fmt.Sprintf("%s@%s ", entry.User, entry.Database))
		} else if entry.Database != "" {
			contextStr = styles.MutedStyle.Render(entry.Database + " ")
		}
	}

	// Message with severity color
	msgStyle := styles.SeverityStyle(entry.Severity)
	message := msgStyle.Render(entry.Message)

	// Combine parts
	line := timestamp + " " + severityBadge + " " + pidStr + contextStr + message

	// Add DETAIL if present
	if entry.Detail != "" {
		line += "\n  " + styles.MutedStyle.Render("DETAIL: "+entry.Detail)
	}

	// Add HINT if present
	if entry.Hint != "" {
		line += "\n  " + styles.AccentStyle.Render("HINT: "+entry.Hint)
	}

	return line
}

// showToast displays a toast message.
func (v *LogsView) showToast(msg string, isError bool) {
	v.toastMessage = msg
	v.toastIsError = isError
	v.toastTime = time.Now()
}

// clearToast clears the toast message.
func (v *LogsView) clearToast() {
	v.toastMessage = ""
}

// invalidateCache forces a viewport rebuild (e.g., when filters change).
func (v *LogsView) invalidateCache() {
	v.needsRebuild = true
}

// View renders the view.
func (v *LogsView) View() string {
	if v.mode == ModeHelp {
		return v.renderHelp()
	}

	// Show enable logging dialog if in that mode
	if v.mode == ModeConfirmEnableLogging {
		return v.renderWithOverlay(v.renderEnableLoggingDialog())
	}

	return v.renderMain()
}

// renderMain renders the main logs view.
func (v *LogsView) renderMain() string {
	// Status bar (connection info)
	statusBar := v.renderStatusBar()

	// View title
	title := v.renderTitle()

	// Main content
	content := v.renderContent()

	// Footer with hints
	footer := v.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		title,
		content,
		footer,
	)
}

// renderWithOverlay renders content with a centered overlay dialog.
func (v *LogsView) renderWithOverlay(overlay string) string {
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}

// renderEnableLoggingDialog renders the enable logging confirmation dialog.
func (v *LogsView) renderEnableLoggingDialog() string {
	title := styles.DialogTitleStyle.Render("Enable Log Collection")

	details := "PostgreSQL log collection is not configured.\n\n" +
		"To view server logs, Steep needs to:\n" +
		"  • Enable logging_collector\n" +
		"  • Configure log_directory and log_filename\n\n" +
		"This requires a PostgreSQL restart to take effect.\n\n" +
		"Enable logging configuration now?"

	actions := styles.MutedStyle.Render("Press ") +
		styles.AccentStyle.Render("y") +
		styles.MutedStyle.Render(" to enable, ") +
		styles.AccentStyle.Render("n") +
		styles.MutedStyle.Render(" or ") +
		styles.AccentStyle.Render("Esc") +
		styles.MutedStyle.Render(" to cancel")

	content := title + "\n\n" + details + "\n\n" + actions
	return styles.DialogStyle.Render(content)
}

// renderContent renders the main log content.
func (v *LogsView) renderContent() string {
	if v.err != nil {
		return styles.ErrorStyle.Render("Error: " + v.err.Error())
	}

	if !v.loggingChecked {
		return styles.MutedStyle.Render("Checking PostgreSQL logging configuration...")
	}

	if !v.loggingEnabled {
		return styles.MutedStyle.Render("Logging is disabled.\nPress 'y' to enable logging_collector.")
	}

	if v.buffer.Len() == 0 {
		logInfo := fmt.Sprintf("Log directory: %s\nLog pattern: %s\nFormat: %s\n\nNo log entries yet. Waiting for data...",
			v.source.LogDir,
			v.source.LogPattern,
			string(v.source.Format))
		return styles.MutedStyle.Render(logInfo)
	}

	return v.viewport.View()
}

// renderTitle renders the view title with separator line.
func (v *LogsView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)

	title := titleStyle.Render("Log Viewer")

	// Add separator line
	separator := styles.MutedStyle.Render(strings.Repeat("─", v.width-2))

	return title + "\n" + separator
}

// renderStatusBar renders the status bar with connection info and log status.
func (v *LogsView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Follow indicator and position info
	var followStr string
	var countStr string

	// Use filtered count when filters are active, otherwise total
	totalLines := len(v.styledLines)
	displayLines := totalLines
	if v.filter.HasFilters() && v.filteredLineCount > 0 {
		displayLines = v.filteredLineCount
	}

	if v.followMode {
		followStr = styles.LogFollowIndicator.Render(" FOLLOW")
		// In follow mode, show entry count (filtered if applicable)
		if v.filter.HasFilters() {
			countStr = styles.MutedStyle.Render(
				fmt.Sprintf(" | %s of %s entries",
					formatCount(v.filteredLineCount),
					formatCount(totalLines)),
			)
		} else if v.buffer.IsFull() {
			countStr = styles.MutedStyle.Render(
				" | " + formatCount(v.buffer.Len()) + " entries (max)",
			)
		} else {
			countStr = styles.MutedStyle.Render(
				" | " + formatCount(v.buffer.Len()) + " entries",
			)
		}
	} else {
		followStr = styles.LogPausedIndicator.Render(" PAUSED")
		// In paused mode, show position within the log
		if displayLines > 0 {
			// Calculate visible range (1-indexed for display)
			bottomLine := displayLines - v.scrollOffset
			topLine := bottomLine - v.contentHeight + 1
			if topLine < 1 {
				topLine = 1
			}
			if bottomLine > displayLines {
				bottomLine = displayLines
			}
			// Calculate percentage (100% = at bottom/newest)
			pct := 100
			if displayLines > v.contentHeight {
				pct = (bottomLine * 100) / displayLines
			}
			if v.filter.HasFilters() {
				// Show filtered/total when filter active
				countStr = styles.MutedStyle.Render(
					fmt.Sprintf(" | %s-%s of %s (%d%%) [%s total]",
						formatCount(topLine),
						formatCount(bottomLine),
						formatCount(displayLines),
						pct,
						formatCount(totalLines)),
				)
			} else {
				countStr = styles.MutedStyle.Render(
					fmt.Sprintf(" | %s-%s of %s (%d%%)",
						formatCount(topLine),
						formatCount(bottomLine),
						formatCount(displayLines),
						pct),
				)
			}
		} else {
			countStr = styles.MutedStyle.Render(" | 0 entries")
		}
	}

	// Filter indicators
	filterStr := ""
	if levelStr := v.filter.LevelString(); levelStr != "" {
		filterStr += styles.AccentStyle.Render(" [level:" + levelStr + "]")
	}
	if v.filter.SearchText != "" {
		filterStr += styles.AccentStyle.Render(" [search:/" + v.filter.SearchText + "/]")
	}

	// Timestamp
	var timestamp string
	if !v.lastUpdate.IsZero() {
		timestamp = styles.StatusTimeStyle.Render(v.lastUpdate.Format("2006-01-02 15:04:05"))
	}

	left := title + followStr + countStr + filterStr
	gap := v.width - lipgloss.Width(left) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(left + spaces + timestamp)
}

// renderFooter renders the bottom footer with hints.
func (v *LogsView) renderFooter() string {
	var hints string

	// Show toast message if recent (within 3 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastIsError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else if v.mode == ModeSearch {
		hints = styles.AccentStyle.Render("/") + v.searchInput + "▏"
	} else if v.mode == ModeCommand {
		hints = styles.AccentStyle.Render(":") + v.commandInput + "▏"
	} else {
		// Normal mode hints
		hints = styles.FooterHintStyle.Render("[f]ollow [/]search [g]top [G]bottom [h]elp")
	}

	return styles.FooterStyle.
		Width(v.width - 2).
		Render(hints)
}

// renderHelp renders the help overlay.
func (v *LogsView) renderHelp() string {
	// Delegate to help.go
	return RenderHelp(v.width, v.height)
}

// formatCount formats an integer with commas.
func formatCount(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}

	// Add commas
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

// SetSize sets the dimensions of the view.
func (v *LogsView) SetSize(width, height int) {
	v.width = width
	v.height = height
	// Reserve space for:
	// - status bar with border: 3 lines (top border + content + bottom border)
	// - title + separator: 2 lines
	// - footer with border: 3 lines
	// Total overhead: 8 lines
	v.contentHeight = height - 8
	if v.contentHeight < 1 {
		v.contentHeight = 1
	}
	v.viewport.Width = width
	v.viewport.Height = v.contentHeight
}

// SetConnected sets the connection status.
func (v *LogsView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *LogsView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *LogsView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
}

// IsInputMode returns true when the view is in a mode that should consume keys.
func (v *LogsView) IsInputMode() bool {
	return v.mode == ModeSearch || v.mode == ModeCommand || v.mode == ModeConfirmEnableLogging
}
