// Package logs provides the Log Viewer view.
package logs

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

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

	// Data
	buffer *LogBuffer
	filter *LogFilter
	source *monitors.LogSource
	err    error

	// Viewport for scrolling
	viewport viewport.Model

	// Follow mode (auto-scroll to newest)
	followMode bool

	// Search state
	searchInput   string // Current search input
	matchIndices  []int  // Indices of matching entries
	currentMatch  int    // Current match index for n/N navigation

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
		mode:       ModeNormal,
		buffer:     NewLogBuffer(DefaultBufferCapacity),
		filter:     NewLogFilter(),
		source:     &monitors.LogSource{},
		followMode: true, // Default to follow mode
		viewport:   viewport.New(80, 20),
	}
}

// Init initializes the view.
func (v *LogsView) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the view state.
func (v *LogsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v.handleKeyPress(msg)
	case tea.MouseMsg:
		return v.handleMouse(msg)
	}
	return v, nil
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
	case "?":
		v.mode = ModeHelp
	case "f":
		v.followMode = !v.followMode
		if v.followMode {
			v.viewport.GotoBottom()
		}
	case "/":
		v.mode = ModeSearch
		v.searchInput = ""
	case ":":
		v.mode = ModeCommand
		v.commandInput = ""
	case "j", "down":
		v.followMode = false
		v.viewport.LineDown(1)
	case "k", "up":
		v.followMode = false
		v.viewport.LineUp(1)
	case "g":
		v.followMode = false
		v.viewport.GotoTop()
	case "G":
		v.followMode = true
		v.viewport.GotoBottom()
	case "ctrl+d":
		v.followMode = false
		v.viewport.HalfViewDown()
	case "ctrl+u":
		v.followMode = false
		v.viewport.HalfViewUp()
	case "n":
		v.nextMatch()
	case "N":
		v.prevMatch()
	case "esc":
		v.clearToast()
		if v.filter.HasFilters() {
			v.filter.Clear()
			v.updateViewport()
		}
	}

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
			v.updateViewport()
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
		// TODO: Enable logging via EnableLogging() call
		v.showToast("Logging enabled", false)
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
		v.followMode = false
		v.viewport.LineUp(3)
	case tea.MouseButtonWheelDown:
		v.viewport.LineDown(3)
	}
	return v, nil
}

// executeCommand parses and executes a command.
func (v *LogsView) executeCommand(cmd string) {
	// TODO: Implement command parsing in Phase 4 (US2)
	// Commands: :level <level>, :level clear, :goto <timestamp>
	v.showToast("Command not implemented: "+cmd, true)
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
		// Calculate line number for the match
		lineNum := v.matchIndices[v.currentMatch]
		v.viewport.SetYOffset(lineNum)
	}
}

// updateViewport refreshes the viewport content.
func (v *LogsView) updateViewport() {
	// TODO: Implement rendering in Phase 3 (US1)
	// For now, just show placeholder content
	v.viewport.SetContent("Log viewer placeholder - implementation pending")
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

// View renders the view.
func (v *LogsView) View() string {
	if v.mode == ModeHelp {
		return v.renderHelp()
	}

	// Build the view
	content := v.renderContent()

	// Add status bar at bottom
	content += "\n" + v.renderStatusBar()

	// Add input line if in input mode
	if v.mode == ModeSearch {
		content += "\n" + styles.AccentStyle.Render("/") + v.searchInput + "▏"
	} else if v.mode == ModeCommand {
		content += "\n" + styles.AccentStyle.Render(":") + v.commandInput + "▏"
	}

	// Add toast if present
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		if v.toastIsError {
			content += "\n" + styles.ErrorStyle.Render(v.toastMessage)
		} else {
			content += "\n" + styles.SuccessStyle.Render(v.toastMessage)
		}
	}

	return content
}

// renderContent renders the main log content.
func (v *LogsView) renderContent() string {
	if v.err != nil {
		return styles.ErrorStyle.Render("Error: " + v.err.Error())
	}

	if v.buffer.Len() == 0 {
		return styles.MutedStyle.Render("No log entries. Waiting for data...")
	}

	return v.viewport.View()
}

// renderStatusBar renders the status bar.
func (v *LogsView) renderStatusBar() string {
	// Follow indicator
	var followStr string
	if v.followMode {
		followStr = styles.LogFollowIndicator.Render("FOLLOW")
	} else {
		followStr = styles.LogPausedIndicator.Render("PAUSED")
	}

	// Entry count
	countStr := styles.MutedStyle.Render(
		" | " + formatCount(v.buffer.Len()) + " entries",
	)

	// Filter indicators
	filterStr := ""
	if v.filter.Severity != nil {
		filterStr += styles.AccentStyle.Render(" [level:" + v.filter.LevelString() + "]")
	}
	if v.filter.SearchText != "" {
		filterStr += styles.AccentStyle.Render(" [search:/" + v.filter.SearchText + "/]")
	}

	// Last update
	updateStr := ""
	if !v.lastUpdate.IsZero() {
		updateStr = styles.MutedStyle.Render(" | Updated: " + v.lastUpdate.Format("15:04:05"))
	}

	return followStr + countStr + filterStr + updateStr
}

// renderHelp renders the help overlay.
func (v *LogsView) renderHelp() string {
	// Delegate to help.go
	return RenderHelp(v.width, v.height)
}

// formatCount formats an integer with commas.
func formatCount(n int) string {
	// Simple implementation for now
	if n < 1000 {
		return string(rune('0'+n%10) + 48)
	}
	// Just return the number as string for now
	return string(rune(n))
}

// SetSize sets the dimensions of the view.
func (v *LogsView) SetSize(width, height int) {
	v.width = width
	v.height = height
	// Reserve space for status bar and input line
	v.viewport.Width = width
	v.viewport.Height = height - 3
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
