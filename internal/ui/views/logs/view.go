// Package logs provides the Log Viewer view.
package logs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/monitors"
	"github.com/willibrandon/steep/internal/ui"
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

	// Rendering state
	filteredLineCount int // Count of lines after filtering (for status bar)
	targetLine        int // Target line to scroll to (-1 = none, use normal scroll)

	// Follow mode (auto-scroll to newest)
	followMode bool

	// Log collection
	collector      *LogCollector
	loggingEnabled bool
	loggingChecked bool

	// Search state
	searchInput  textinput.Model // Search input with cursor support
	matchIndices []int           // Indices of matching entries
	currentMatch int             // Current match index for n/N navigation

	// Command state
	commandInput textinput.Model // Command input with cursor support
	toastMessage string          // Toast message to display
	toastIsError bool            // Whether toast is an error
	toastTime    time.Time

	// Read-only mode
	readOnly bool

	// Row selection
	selectedIdx int // Currently selected entry index (0 = oldest in view)

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewLogsView creates a new log viewer.
func NewLogsView() *LogsView {
	// Create search input
	si := textinput.New()
	si.CharLimit = 256

	// Create command input
	ci := textinput.New()
	ci.CharLimit = 256

	return &LogsView{
		mode:          ModeNormal,
		buffer:        NewLogBuffer(DefaultBufferCapacity),
		filter:        NewLogFilter(),
		source:        &monitors.LogSource{},
		followMode:    true,
		viewport:      viewport.New(80, 20),
		contentHeight: 20,
		targetLine:    -1,
		searchInput:   si,
		commandInput:  ci,
		clipboard:     ui.NewClipboardWriter(),
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
			// Add entries to buffer
			for _, entry := range msg.Entries {
				logEntry := v.entryDataToLogEntry(entry)
				v.buffer.Add(logEntry)
			}
			v.lastUpdate = time.Now()
			// In follow mode, keep selection at newest entry
			if v.followMode {
				v.selectedIdx = v.buffer.Len() - 1
			}
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
		if key == "esc" || key == "?" || key == "h" || key == "q" {
			v.mode = ModeNormal
		}
		return v, nil

	case ModeSearch:
		return v.handleSearchMode(msg)

	case ModeCommand:
		return v.handleCommandMode(msg)

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
			// In follow mode, select newest entry
			if v.buffer.Len() > 0 {
				v.selectedIdx = v.buffer.Len() - 1
			}
			v.needsRebuild = true
		}
	case "/":
		v.mode = ModeSearch
		v.searchInput.Reset()
		v.searchInput.Focus()
	case ":":
		v.mode = ModeCommand
		v.commandInput.Reset()
		v.commandInput.Focus()
	case "j", "down":
		v.followMode = false
		v.moveSelection(1)
	case "k", "up":
		v.followMode = false
		v.moveSelection(-1)
	case "g":
		// Go to oldest (top)
		v.followMode = false
		v.selectedIdx = 0
		v.ensureSelectionVisible()
	case "G":
		// Go to newest (bottom) and enable follow
		v.followMode = true
		totalEntries := v.getEntryCount()
		if totalEntries > 0 {
			v.selectedIdx = totalEntries - 1
		}
		v.ensureSelectionVisible()
	case "ctrl+d":
		// Half page down (toward newer)
		v.followMode = false
		v.moveSelection(v.contentHeight / 2)
	case "ctrl+u":
		// Half page up (toward older)
		v.followMode = false
		v.moveSelection(-v.contentHeight / 2)
	case "n":
		v.nextMatch()
	case "N":
		v.prevMatch()
	case "y":
		v.yankSelectedEntry()
	case "Y":
		v.yankAllFiltered()
	case "esc":
		v.clearToast()
		if v.filter.HasFilters() {
			v.filter.Clear()
			v.matchIndices = nil
			v.currentMatch = -1
			v.invalidateCache()
			v.rebuildViewport()
		}
	}

	v.rebuildViewport() // Rebuild in Update, not View
	return v, nil
}

// handleSearchMode handles input in search mode.
func (v *LogsView) handleSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = ModeNormal
		v.searchInput.Reset()
		return v, nil
	case tea.KeyEnter:
		v.mode = ModeNormal
		searchText := v.searchInput.Value()
		if err := v.filter.SetSearch(searchText); err != nil {
			v.showToast("Invalid regex: "+err.Error(), true)
		} else {
			v.updateMatchIndices()
			v.invalidateCache()
			// Show indicator on first match immediately
			if len(v.matchIndices) > 0 {
				v.scrollToMatch()
			} else {
				v.rebuildViewport()
			}
		}
		return v, nil
	}

	// Delegate to textinput for all other keys (typing, paste, cursor movement)
	var cmd tea.Cmd
	v.searchInput, cmd = v.searchInput.Update(msg)
	return v, cmd
}

// handleCommandMode handles input in command mode.
func (v *LogsView) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = ModeNormal
		v.commandInput.Reset()
		return v, nil
	case tea.KeyEnter:
		v.mode = ModeNormal
		cmdText := v.commandInput.Value()
		v.commandInput.Reset()
		v.executeCommand(cmdText)
		return v, nil
	}

	// Delegate to textinput for all other keys (typing, paste, cursor movement)
	var cmd tea.Cmd
	v.commandInput, cmd = v.commandInput.Update(msg)
	return v, cmd
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
	// Use viewport's native scrolling for mouse wheel
	// Mouse scroll does NOT move selection - just scrolls the view
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
	if len(args) == 0 {
		v.showToast("Usage: :goto <timestamp> (e.g., 14:30, -1h, 2025-11-27 14:30)", true)
		return
	}

	// Join args to handle timestamps with spaces (e.g., "2025-11-27 14:30")
	timestampStr := strings.Join(args, " ")

	// Parse the timestamp
	parsed, err := ParseTimestampInput(timestampStr)
	if err != nil {
		v.showToast(err.Error(), true)
		return
	}

	// First, check if the timestamp is within the current buffer
	if v.buffer.Len() > 0 {
		oldest, _ := v.buffer.Oldest()
		newest, _ := v.buffer.Newest()

		// Check if timestamp is within buffer range
		if (parsed.Time.Equal(oldest.Timestamp) || parsed.Time.After(oldest.Timestamp)) &&
			(parsed.Time.Equal(newest.Timestamp) || parsed.Time.Before(newest.Timestamp)) {
			// Timestamp is in buffer - navigate within buffer
			v.navigateToBufferTimestamp(parsed)
			return
		}
	}

	// Timestamp outside buffer - need to load from historical files
	v.loadHistoricalLogs(parsed)
}

// navigateToBufferTimestamp navigates to a timestamp within the current buffer.
func (v *LogsView) navigateToBufferTimestamp(parsed *ParsedTimestamp) {
	idx := v.buffer.FindByTimestampWithDirection(parsed.Time, parsed.Direction)
	if idx < 0 {
		var msg string
		switch parsed.Direction {
		case SearchAfter:
			msg = "No entries at or after that time"
		case SearchBefore:
			msg = "No entries at or before that time"
		default:
			msg = "No entries at that time"
		}
		v.showToast(msg, true)
		return
	}

	// Get the entry to show actual time
	entry, ok := v.buffer.Get(idx)
	if !ok {
		v.showToast("Error finding entry", true)
		return
	}

	// Disable follow mode and navigate to the entry
	v.followMode = false
	v.selectedIdx = idx
	v.ensureSelectionVisible()

	// Show result
	v.showToast(fmt.Sprintf("Jumped to %s", entry.Timestamp.Format("2006-01-02 15:04:05")), false)
}

// loadHistoricalLogs loads logs from historical files for a given timestamp.
func (v *LogsView) loadHistoricalLogs(parsed *ParsedTimestamp) {
	if v.source == nil || v.source.LogDir == "" {
		v.showToast("Log directory not configured", true)
		return
	}

	// Build loading message based on direction
	var loadingMsg string
	switch parsed.Direction {
	case SearchAfter:
		loadingMsg = fmt.Sprintf("Loading logs at/after %s...", parsed.Time.Format("2006-01-02 15:04:05"))
	case SearchBefore:
		loadingMsg = fmt.Sprintf("Loading logs at/before %s...", parsed.Time.Format("2006-01-02 15:04:05"))
	default:
		loadingMsg = fmt.Sprintf("Loading logs around %s...", parsed.Time.Format("2006-01-02 15:04:05"))
	}
	v.showToast(loadingMsg, false)

	// Create historical loader
	loader := NewHistoricalLoader(v.source)

	// Discover available log files
	files, err := loader.DiscoverLogFiles()
	if err != nil {
		v.showToast("Error scanning log files: "+err.Error(), true)
		return
	}

	if len(files) == 0 {
		v.showToast("No log files found in "+v.source.LogDir, true)
		return
	}

	// Get timestamp ranges for files (to find the right file)
	files = loader.GetTimestampRanges(v.getContext(), files)

	// Find the file(s) that should contain the timestamp
	candidates := loader.FindLogFileForTimestamp(parsed.Time, files)
	if len(candidates) == 0 {
		v.showToast("No log file found for that timestamp", true)
		return
	}

	// Load entries from the best candidate file
	result, err := loader.LoadHistoricalEntries(v.getContext(), candidates[0], parsed.Time, 500, parsed.Direction)
	if err != nil {
		v.showToast("Error loading logs: "+err.Error(), true)
		return
	}

	if len(result.Entries) == 0 {
		v.showToast("No entries found in "+result.FromFile, true)
		return
	}

	// Clear current buffer and load historical entries
	v.buffer.Clear()

	for _, entry := range result.Entries {
		logEntry := v.entryDataToLogEntry(entry)
		v.buffer.Add(logEntry)
	}

	// Navigate to the target entry
	v.followMode = false
	if result.TargetIndex >= 0 {
		v.selectedIdx = result.TargetIndex
	} else {
		v.selectedIdx = 0
	}

	v.ensureSelectionVisible()

	// Show result message
	if result.Message != "" {
		v.showToast(result.Message, false)
	} else {
		v.showToast(fmt.Sprintf("Loaded %d entries from %s", len(result.Entries), result.FromFile), false)
	}
}

// getContext returns a context for operations.
func (v *LogsView) getContext() context.Context {
	return context.Background()
}

// updateMatchIndices finds all entries matching the current search.
// When filters are active, indices are into the filtered view.
func (v *LogsView) updateMatchIndices() {
	v.matchIndices = nil
	v.currentMatch = -1

	if v.filter.SearchPattern == nil {
		return
	}

	// Get filtered entries (same as what's displayed)
	entries := v.buffer.GetAll()
	if v.filter.HasFilters() {
		entries = v.filter.FilterEntries(entries)
	}

	// All filtered entries match when search is active
	for i := range entries {
		v.matchIndices = append(v.matchIndices, i)
	}

	if len(v.matchIndices) > 0 {
		v.currentMatch = 0
		v.showToast(fmt.Sprintf("%d matches found", len(v.matchIndices)), false)
	} else {
		v.showToast("No matches found", true)
	}
}

// nextMatch navigates to the next search match.
func (v *LogsView) nextMatch() {
	// Ensure match indices are populated if search is active
	if v.filter.SearchPattern != nil && len(v.matchIndices) == 0 {
		v.updateMatchIndices()
	}

	if len(v.matchIndices) == 0 {
		v.showToast("No matches", true)
		return
	}

	// Don't wrap - stop at last match
	if v.currentMatch >= len(v.matchIndices)-1 {
		v.showToast(fmt.Sprintf("Last match (%d of %d)", len(v.matchIndices), len(v.matchIndices)), false)
		return
	}

	v.currentMatch++
	v.scrollToMatch()
	v.showToast(fmt.Sprintf("Match %d of %d", v.currentMatch+1, len(v.matchIndices)), false)
}

// prevMatch navigates to the previous search match.
func (v *LogsView) prevMatch() {
	// Ensure match indices are populated if search is active
	if v.filter.SearchPattern != nil && len(v.matchIndices) == 0 {
		v.updateMatchIndices()
	}

	if len(v.matchIndices) == 0 {
		v.showToast("No matches", true)
		return
	}

	// Don't wrap - stop at first match
	if v.currentMatch <= 0 {
		v.showToast(fmt.Sprintf("First match (1 of %d)", len(v.matchIndices)), false)
		return
	}

	v.currentMatch--
	v.scrollToMatch()
	v.showToast(fmt.Sprintf("Match %d of %d", v.currentMatch+1, len(v.matchIndices)), false)
}

// scrollToMatch scrolls the viewport to show the current match.
func (v *LogsView) scrollToMatch() {
	if v.currentMatch < 0 || v.currentMatch >= len(v.matchIndices) {
		return
	}

	v.followMode = false
	v.targetLine = v.currentMatch
	v.needsRebuild = true
	v.rebuildViewport()
}

// getEntryCount returns the total number of entries (filtered or all).
func (v *LogsView) getEntryCount() int {
	if v.filter.HasFilters() {
		return len(v.matchIndices)
	}
	return v.buffer.Len()
}

// moveSelection moves the selection by delta entries.
func (v *LogsView) moveSelection(delta int) {
	totalEntries := v.getEntryCount()
	if totalEntries == 0 {
		return
	}

	// Move selection
	v.selectedIdx += delta

	// Clamp to valid range
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= totalEntries {
		v.selectedIdx = totalEntries - 1
	}

	v.ensureSelectionVisible()
}

// ensureSelectionVisible adjusts scroll to keep selected row visible.
func (v *LogsView) ensureSelectionVisible() {
	totalEntries := v.getEntryCount()
	if totalEntries == 0 {
		return
	}

	// Clamp selection to valid range
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= totalEntries {
		v.selectedIdx = totalEntries - 1
	}

	// Trigger rebuild which handles viewport positioning
	v.needsRebuild = true
	v.rebuildViewport()
}

// getSelectedEntry returns the currently selected log entry.
func (v *LogsView) getSelectedEntry() (models.LogEntry, bool) {
	if v.filter.HasFilters() {
		// With filters, selectedIdx is index into matchIndices
		if v.selectedIdx < 0 || v.selectedIdx >= len(v.matchIndices) {
			return models.LogEntry{}, false
		}
		bufferIdx := v.matchIndices[v.selectedIdx]
		return v.buffer.Get(bufferIdx)
	}

	// Without filters, selectedIdx is direct buffer index
	return v.buffer.Get(v.selectedIdx)
}

// formatEntryForClipboard formats a log entry for clipboard (plain text).
func (v *LogsView) formatEntryForClipboard(entry models.LogEntry) string {
	var sb strings.Builder
	sb.WriteString(entry.Timestamp.Format("2006-01-02 15:04:05"))
	sb.WriteString(" ")
	sb.WriteString(entry.Severity.String())
	sb.WriteString(" ")
	if entry.PID > 0 {
		sb.WriteString(fmt.Sprintf("[%d] ", entry.PID))
	}
	if entry.User != "" && entry.Database != "" {
		sb.WriteString(fmt.Sprintf("%s@%s ", entry.User, entry.Database))
	} else if entry.Database != "" {
		sb.WriteString(entry.Database + " ")
	}
	sb.WriteString(entry.Message)
	if entry.Detail != "" {
		sb.WriteString("\n  DETAIL: " + entry.Detail)
	}
	if entry.Hint != "" {
		sb.WriteString("\n  HINT: " + entry.Hint)
	}
	return sb.String()
}

// yankSelectedEntry copies the selected entry to clipboard.
func (v *LogsView) yankSelectedEntry() {
	entry, ok := v.getSelectedEntry()
	if !ok {
		v.showToast("No entry selected", true)
		return
	}

	if !v.clipboard.IsAvailable() {
		v.showToast("Clipboard not available", true)
		return
	}

	text := v.formatEntryForClipboard(entry)
	if err := v.clipboard.Write(text); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
		return
	}

	v.showToast("Entry copied to clipboard", false)
}

// yankAllFiltered copies all filtered entries (or all if no filter) to clipboard.
func (v *LogsView) yankAllFiltered() {
	if !v.clipboard.IsAvailable() {
		v.showToast("Clipboard not available", true)
		return
	}

	var entries []models.LogEntry
	if v.filter.HasFilters() {
		allEntries := v.buffer.GetAll()
		entries = v.filter.FilterEntries(allEntries)
	} else {
		entries = v.buffer.GetAll()
	}

	if len(entries) == 0 {
		v.showToast("No entries to copy", true)
		return
	}

	var sb strings.Builder
	for i, entry := range entries {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(v.formatEntryForClipboard(entry))
	}

	if err := v.clipboard.Write(sb.String()); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
		return
	}

	v.showToast(fmt.Sprintf("Copied %d entries to clipboard", len(entries)), false)
}

// rebuildViewport rebuilds the viewport content.
func (v *LogsView) rebuildViewport() {
	if !v.needsRebuild {
		return
	}
	v.needsRebuild = false

	if v.buffer.Len() == 0 {
		v.viewport.SetContent("")
		v.filteredLineCount = 0
		return
	}

	// Get the lines to display based on scroll position
	var linesToShow []string

	// Check if we need search highlighting
	hasSearch := v.filter.SearchPattern != nil

	if v.filter.HasFilters() {
		// When filtering, we need to filter from raw entries
		// This is slower but necessary for search/level filters
		entries := v.buffer.GetAll()
		filtered := v.filter.FilterEntries(entries)
		for i, entry := range filtered {
			// Check if this is the current match (matchIndices[j] == j for all j)
			isCurrentMatch := hasSearch && v.currentMatch >= 0 && i == v.currentMatch
			isSelected := i == v.selectedIdx
			line := v.formatLogEntryWithHighlight(entry, hasSearch, isCurrentMatch, isSelected)
			linesToShow = append(linesToShow, line)
		}
	} else {
		// No filter - format entries with selection highlighting on timestamp only
		entries := v.buffer.GetAll()
		for i, entry := range entries {
			isSelected := i == v.selectedIdx
			line := v.formatLogEntryWithHighlight(entry, false, false, isSelected)
			linesToShow = append(linesToShow, line)
		}
	}

	totalFiltered := len(linesToShow)
	v.filteredLineCount = totalFiltered

	if totalFiltered == 0 {
		v.viewport.SetContent(styles.MutedStyle.Render("No entries match current filter"))
		return
	}

	// When filters are active, load all filtered content and use viewport's native scrolling
	// This is simpler and works better for search navigation
	if v.filter.HasFilters() {
		v.viewport.SetContent(strings.Join(linesToShow, "\n"))

		// Handle target line navigation (for n/N match jumping)
		if v.targetLine >= 0 && v.targetLine < totalFiltered {
			// Calculate actual line number by counting newlines in entries before target
			// Each entry can span multiple lines (DETAIL, HINT add extra lines)
			actualLine := 0
			for i := 0; i < v.targetLine && i < len(linesToShow); i++ {
				actualLine += strings.Count(linesToShow[i], "\n") + 1
			}

			// Calculate total actual lines
			totalActualLines := 0
			for _, line := range linesToShow {
				totalActualLines += strings.Count(line, "\n") + 1
			}

			// Check if target is already visible - if so, don't scroll
			currentYOffset := v.viewport.YOffset
			visibleEnd := currentYOffset + v.contentHeight

			if actualLine >= currentYOffset && actualLine < visibleEnd-1 {
				// Already visible, just reset targetLine
				v.targetLine = -1
				return
			}

			// Need to scroll - center the match in viewport
			yOffset := actualLine - v.contentHeight/2
			if yOffset < 0 {
				yOffset = 0
			}
			maxYOffset := totalActualLines - v.contentHeight
			if maxYOffset < 0 {
				maxYOffset = 0
			}
			if yOffset > maxYOffset {
				yOffset = maxYOffset
			}
			v.viewport.SetYOffset(yOffset)
			v.targetLine = -1 // Reset after use
		}
		// Don't change viewport position if just rebuilding (let native scroll work)
		return
	}

	// For non-filtered view, set all content and use viewport scrolling
	// This is simpler and handles multiline entries correctly
	v.viewport.SetContent(strings.Join(linesToShow, "\n"))

	// In follow mode, always stay at the bottom (no centering)
	if v.followMode {
		v.viewport.GotoBottom()
		return
	}

	// Calculate line offset for the selected entry (account for multiline entries)
	if v.selectedIdx >= 0 && v.selectedIdx < len(linesToShow) {
		// Count actual lines before the selected entry
		lineOffset := 0
		for i := 0; i < v.selectedIdx; i++ {
			lineOffset += strings.Count(linesToShow[i], "\n") + 1
		}

		// Position viewport to show selected entry
		// Try to keep selection roughly centered when possible
		targetOffset := lineOffset - v.contentHeight/2
		if targetOffset < 0 {
			targetOffset = 0
		}

		// Calculate max offset
		totalLines := 0
		for _, line := range linesToShow {
			totalLines += strings.Count(line, "\n") + 1
		}
		maxOffset := totalLines - v.contentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if targetOffset > maxOffset {
			targetOffset = maxOffset
		}

		// Only scroll if selection is not visible
		currentTop := v.viewport.YOffset
		currentBottom := currentTop + v.contentHeight
		if lineOffset < currentTop || lineOffset >= currentBottom {
			v.viewport.SetYOffset(targetOffset)
		}
	}
}

// formatLogEntry formats a log entry for display.
func (v *LogsView) formatLogEntry(entry models.LogEntry) string {
	return v.formatLogEntryWithHighlight(entry, false, false, false)
}

// formatLogEntryWithHighlight formats a log entry with optional search highlighting.
// If isCurrentMatch is true, the entry gets a match indicator.
// If isSelected is true, only the timestamp gets selection highlighting (subtle cursor).
func (v *LogsView) formatLogEntryWithHighlight(entry models.LogEntry, highlight bool, isCurrentMatch bool, isSelected bool) string {
	// Timestamp - show date if not today
	now := time.Now()
	var tsFormat string
	if entry.Timestamp.Year() != now.Year() ||
		entry.Timestamp.Month() != now.Month() ||
		entry.Timestamp.Day() != now.Day() {
		tsFormat = "01-02 15:04:05" // Show month-day for historical entries
	} else {
		tsFormat = "15:04:05"
	}
	// Apply selection style only to timestamp for subtle cursor indication
	var timestamp string
	if isSelected {
		timestamp = styles.TableSelectedStyle.Render(entry.Timestamp.Format(tsFormat))
	} else {
		timestamp = styles.LogTimestampStyle.Render(entry.Timestamp.Format(tsFormat))
	}

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

	// Message with severity color and optional search highlighting
	msgStyle := styles.SeverityStyle(entry.Severity)
	message := entry.Message
	if highlight && v.filter.SearchPattern != nil {
		message = v.highlightMatches(message)
	} else {
		message = msgStyle.Render(message)
	}

	// Combine parts - add current match indicator if this is the selected match
	var prefix string
	if isCurrentMatch {
		prefix = styles.LogCurrentMatchStyle.Render("▶ ")
	} else if highlight {
		prefix = "  " // Align with indicator
	}
	line := prefix + timestamp + " " + severityBadge + " " + pidStr + contextStr + message

	// Add DETAIL if present
	if entry.Detail != "" {
		detail := entry.Detail
		if highlight && v.filter.SearchPattern != nil {
			detail = v.highlightMatches(detail)
		} else {
			detail = styles.MutedStyle.Render("DETAIL: " + detail)
		}
		if highlight && v.filter.SearchPattern != nil {
			line += "\n    " + styles.MutedStyle.Render("DETAIL: ") + detail
		} else {
			line += "\n    " + detail
		}
	}

	// Add HINT if present
	if entry.Hint != "" {
		line += "\n    " + styles.AccentStyle.Render("HINT: "+entry.Hint)
	}

	return line
}

// highlightMatches highlights search matches in text.
func (v *LogsView) highlightMatches(text string) string {
	if v.filter.SearchPattern == nil {
		return text
	}

	matches := v.filter.SearchPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	// Build result with highlighted matches
	var result strings.Builder
	lastEnd := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		// Add text before match
		if start > lastEnd {
			result.WriteString(text[lastEnd:start])
		}
		// Add highlighted match
		result.WriteString(styles.LogSearchHighlight.Render(text[start:end]))
		lastEnd = end
	}
	// Add remaining text after last match
	if lastEnd < len(text) {
		result.WriteString(text[lastEnd:])
	}

	return result.String()
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
	totalEntries := v.buffer.Len()
	displayEntries := totalEntries
	if v.filter.HasFilters() && v.filteredLineCount > 0 {
		displayEntries = v.filteredLineCount
	}

	if v.followMode {
		followStr = styles.LogFollowIndicator.Render(" FOLLOW")
		// In follow mode, show entry count (filtered if applicable)
		if v.filter.HasFilters() {
			countStr = styles.MutedStyle.Render(
				fmt.Sprintf(" | %s of %s entries",
					formatCount(v.filteredLineCount),
					formatCount(totalEntries)),
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
		// In paused mode, show selected entry position and entry count
		if displayEntries > 0 {
			// Show selection position (1-indexed for display)
			selectedPos := v.selectedIdx + 1
			if v.filter.HasFilters() {
				// Show filtered/total when filter active
				countStr = styles.MutedStyle.Render(
					fmt.Sprintf(" | %s of %s entries [%s total]",
						formatCount(selectedPos),
						formatCount(displayEntries),
						formatCount(totalEntries)),
				)
			} else {
				countStr = styles.MutedStyle.Render(
					fmt.Sprintf(" | %s of %s entries",
						formatCount(selectedPos),
						formatCount(displayEntries)),
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
		hints = styles.AccentStyle.Render("/") + v.searchInput.View()
	} else if v.mode == ModeCommand {
		hints = styles.AccentStyle.Render(":") + v.commandInput.View()
	} else {
		// Normal mode hints
		hints = styles.FooterHintStyle.Render("[f]ollow [/]search [j/k]nav [y]ank [g]top [G]bottom [h]elp")
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
