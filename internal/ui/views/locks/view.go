// Package locks provides the Locks view for lock and blocking detection monitoring.
package locks

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views/locks/deadviz"
)

// SortColumn represents the available sort columns.
type SortColumn int

const (
	SortByPID SortColumn = iota
	SortByType
	SortByMode
	SortByGranted
	SortByDuration
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByPID:
		return "PID"
	case SortByType:
		return "Type"
	case SortByMode:
		return "Mode"
	case SortByGranted:
		return "Granted"
	case SortByDuration:
		return "Duration"
	default:
		return "Unknown"
	}
}

// DeadlockSortColumn represents the available sort columns for deadlock history.
type DeadlockSortColumn int

const (
	DeadlockSortByTime DeadlockSortColumn = iota
	DeadlockSortByDatabase
	DeadlockSortByProcesses
)

// String returns the display name for the deadlock sort column.
func (s DeadlockSortColumn) String() string {
	switch s {
	case DeadlockSortByTime:
		return "Time"
	case DeadlockSortByDatabase:
		return "Database"
	case DeadlockSortByProcesses:
		return "Processes"
	default:
		return "Unknown"
	}
}

// LocksMode represents the current interaction mode.
type LocksMode int

const (
	ModeNormal LocksMode = iota
	ModeDetail
	ModeConfirmKill
	ModeHelp
	ModeDeadlockDetail
	ModeConfirmEnableLogging
	ModeConfirmResetDeadlocks
	ModeConfirmResetLogPositions
)

// LocksView displays lock information and blocking relationships.
type LocksView struct {
	width            int
	height           int
	viewHeaderHeight int // Calculated height of view header elements for mouse coordinate translation

	// State
	mode           LocksMode
	activeTab      ViewTab
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	readOnly       bool

	// Data
	data       *models.LocksData
	totalCount int
	err        error

	// Deadlock history data
	deadlocks            []sqlite.DeadlockSummary
	deadlockSelectedIdx  int
	deadlockScrollOffset int
	deadlockSortColumn   DeadlockSortColumn
	deadlockSortAsc      bool // false = descending (default), true = ascending
	deadlockEnabled      bool
	deadlockLoading      bool
	deadlockCurrentFile  int
	deadlockTotalFiles   int
	deadlockSpinner      spinner.Model
	deadlockLastUpdate   time.Time

	// Deadlock detail view state
	deadlockDetail       *sqlite.DeadlockEvent
	deadlockDetailScroll int
	deadlockDetailLines  []string

	// Table state
	selectedIdx  int
	scrollOffset int
	sortColumn   SortColumn
	sortAsc      bool // false = descending (default), true = ascending

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Detail view state
	detailQuery          string
	detailFormattedQuery string // formatted version for clipboard (no ANSI)
	detailScrollOffset   int
	detailLines          []string

	// Kill confirmation state
	killPID          int
	killUser         string
	killQuery        string
	killScrollOffset int
	killLines        []string

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewLocksView creates a new locks view.
func NewLocksView() *LocksView {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &LocksView{
		mode:            ModeNormal,
		sortColumn:      SortByPID,
		data:            models.NewLocksData(),
		clipboard:       ui.NewClipboardWriter(),
		deadlockSpinner: s,
		deadlockLoading: true, // Start in loading state
	}
}

// Init initializes the locks view.
func (v *LocksView) Init() tea.Cmd {
	return v.deadlockSpinner.Tick
}

// Update handles messages for the locks view.
func (v *LocksView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			return v, cmd
		}

	case ui.LocksDataMsg:
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			v.data = msg.Data
			v.totalCount = len(msg.Data.Locks)
			v.lastUpdate = msg.FetchedAt
			v.err = nil
			// Apply current sort order
			v.sortLocks()
			// Ensure selection is valid
			if v.selectedIdx >= len(v.data.Locks) {
				v.selectedIdx = max(0, len(v.data.Locks)-1)
			}
			// Ensure selected row is visible (reset scroll if needed)
			v.ensureVisible()
		}

	case ui.KillQueryResultMsg:
		if msg.Error != nil {
			v.showToast("Kill failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast(fmt.Sprintf("Process %d terminated", msg.PID), false)
		} else {
			v.showToast(fmt.Sprintf("Failed to terminate process %d", msg.PID), true)
		}

	case ui.DeadlockScanProgressMsg:
		v.deadlockCurrentFile = msg.CurrentFile
		v.deadlockTotalFiles = msg.TotalFiles

	case ui.DeadlockHistoryMsg:
		v.deadlockEnabled = msg.Enabled
		v.deadlockLoading = false // Stop loading spinner
		v.deadlockLastUpdate = time.Now()
		if msg.Error == nil {
			v.deadlocks = msg.Deadlocks
			// Apply current sort order
			v.sortDeadlocks()
			// Ensure selection is valid
			if v.deadlockSelectedIdx >= len(v.deadlocks) {
				v.deadlockSelectedIdx = max(0, len(v.deadlocks)-1)
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		v.deadlockSpinner, cmd = v.deadlockSpinner.Update(msg)
		return v, cmd

	case ui.DeadlockDetailMsg:
		if msg.Error == nil && msg.Event != nil {
			v.deadlockDetail = msg.Event
			v.deadlockDetailScroll = 0
			v.deadlockDetailLines = v.formatDeadlockDetail(msg.Event)
			v.mode = ModeDeadlockDetail
		}

	case EnableLoggingCollectorResultMsg:
		if msg.Error != nil {
			v.showToast("Enable logging failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast("Logging enabled - PostgreSQL restarting...", false)
			v.deadlockEnabled = true
		}

	case ui.ResetDeadlocksResultMsg:
		logger.Info("LocksView: ResetDeadlocksResultMsg received", "success", msg.Success, "error", msg.Error)
		if msg.Error != nil {
			v.showToast("Reset failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			logger.Info("LocksView: clearing deadlocks")
			v.deadlocks = nil
			v.deadlockSelectedIdx = 0
			v.deadlockScrollOffset = 0
			v.showToast("Deadlock history cleared", false)
			logger.Info("LocksView: toast shown")
		}

	case ui.ResetLogPositionsResultMsg:
		if msg.Error != nil {
			v.showToast("Reset log positions failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.deadlockLoading = true
			v.deadlockCurrentFile = 0
			v.deadlockTotalFiles = 0
			v.showToast("Log positions reset - re-parsing logs...", false)
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case tea.MouseMsg:
		switch v.mode {
		case ModeNormal:
			switch v.activeTab {
			case TabActiveLocks:
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					v.moveSelection(-1)
				case tea.MouseButtonWheelDown:
					v.moveSelection(1)
				case tea.MouseButtonLeft:
					if msg.Action == tea.MouseActionPress {
						// msg.Y is relative to view top (app translates global to relative)
						// Subtract view's header height to get data row index
						clickedRow := msg.Y - v.viewHeaderHeight
						if clickedRow >= 0 {
							newIdx := v.scrollOffset + clickedRow
							if newIdx >= 0 && newIdx < len(v.data.Locks) {
								v.selectedIdx = newIdx
							}
						}
					}
				}
			case TabDeadlockHistory:
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					v.moveDeadlockSelection(-1)
				case tea.MouseButtonWheelDown:
					v.moveDeadlockSelection(1)
				case tea.MouseButtonLeft:
					if msg.Action == tea.MouseActionPress {
						// Deadlock tab has additional header rows (2 extra for deadlock-specific header)
						clickedRow := msg.Y - v.viewHeaderHeight - 2
						if clickedRow >= 0 {
							newIdx := v.deadlockScrollOffset + clickedRow
							if newIdx >= 0 && newIdx < len(v.deadlocks) {
								v.deadlockSelectedIdx = newIdx
							}
						}
					}
				}
			}
		case ModeDetail:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.detailScrollUp(1)
			case tea.MouseButtonWheelDown:
				v.detailScrollDown(1)
			}
		case ModeDeadlockDetail:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.deadlockDetailScrollUp(1)
			case tea.MouseButtonWheelDown:
				v.deadlockDetailScrollDown(1)
			}
		}
	}

	return v, nil
}

// handleKeyPress processes keyboard input.
func (v *LocksView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle confirm kill mode
	if v.mode == ModeConfirmKill {
		return v.handleConfirmKillMode(key)
	}

	// Handle detail mode
	if v.mode == ModeDetail {
		return v.handleDetailMode(key)
	}

	// Handle deadlock detail mode
	if v.mode == ModeDeadlockDetail {
		switch key {
		case "esc", "q":
			v.mode = ModeNormal
		case "j", "down":
			v.deadlockDetailScrollDown(1)
		case "k", "up":
			v.deadlockDetailScrollUp(1)
		case "g", "home":
			v.deadlockDetailScroll = 0
		case "G", "end":
			maxScroll := max(0, len(v.deadlockDetailLines)-(v.height-4))
			v.deadlockDetailScroll = maxScroll
		case "ctrl+d", "pgdown":
			v.deadlockDetailScrollDown(10)
		case "ctrl+u", "pgup":
			v.deadlockDetailScrollUp(10)
		case "c":
			// Copy all queries to clipboard
			if v.deadlockDetail != nil {
				var queries []string
				for i, proc := range v.deadlockDetail.Processes {
					if proc.Query != "" {
						formatted := v.formatDeadlockSQLPlain(proc.Query)
						queries = append(queries, fmt.Sprintf("-- Process %d (PID %d)\n%s", i+1, proc.PID, formatted))
					}
				}
				if len(queries) > 0 {
					allQueries := strings.Join(queries, "\n\n")
					if !v.clipboard.IsAvailable() {
						v.showToast("Clipboard unavailable: "+v.clipboard.Error(), true)
					} else if err := v.clipboard.Write(allQueries); err != nil {
						v.showToast("Copy failed: "+err.Error(), true)
					} else {
						v.showToast("Queries copied to clipboard", false)
					}
				}
			}
		}
		return nil
	}

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "esc", "q":
			v.mode = ModeNormal
		}
		return nil
	}

	// Handle enable logging confirmation mode
	if v.mode == ModeConfirmEnableLogging {
		switch key {
		case "y", "Y":
			v.mode = ModeNormal
			return func() tea.Msg {
				return EnableLoggingCollectorMsg{}
			}
		case "n", "N", "esc":
			v.mode = ModeNormal
		}
		return nil
	}

	// Handle reset deadlocks confirmation mode
	if v.mode == ModeConfirmResetDeadlocks {
		switch key {
		case "y", "Y":
			v.mode = ModeNormal
			return func() tea.Msg {
				return ui.ResetDeadlocksMsg{}
			}
		case "n", "N", "esc":
			v.mode = ModeNormal
		}
		return nil
	}

	// Handle reset log positions confirmation mode
	if v.mode == ModeConfirmResetLogPositions {
		switch key {
		case "y", "Y":
			v.mode = ModeNormal
			return func() tea.Msg {
				return ui.ResetLogPositionsMsg{}
			}
		case "n", "N", "esc":
			v.mode = ModeNormal
		}
		return nil
	}

	// Normal mode keys
	switch key {
	// Help
	case "h":
		v.mode = ModeHelp

	// Tab switching
	case "left":
		v.activeTab = PrevTab(v.activeTab)
	case "right":
		v.activeTab = NextTab(v.activeTab)

	// Navigation
	case "j", "down":
		if v.activeTab == TabActiveLocks {
			v.moveSelection(1)
		} else {
			v.moveDeadlockSelection(1)
		}
	case "k", "up":
		if v.activeTab == TabActiveLocks {
			v.moveSelection(-1)
		} else {
			v.moveDeadlockSelection(-1)
		}
	case "g", "home":
		v.selectedIdx = 0
		v.scrollOffset = 0
	case "G", "end":
		v.selectedIdx = max(0, len(v.data.Locks)-1)
		v.ensureVisible()
	case "ctrl+d", "pgdown":
		v.pageDown()
	case "ctrl+u", "pgup":
		v.pageUp()

	// Sort
	case "s":
		switch v.activeTab {
		case TabActiveLocks:
			v.cycleSort()
		case TabDeadlockHistory:
			v.cycleDeadlockSort()
		}
	case "S":
		switch v.activeTab {
		case TabActiveLocks:
			v.toggleSortDirection()
		case TabDeadlockHistory:
			v.toggleDeadlockSortDirection()
		}

	// Detail view
	case "d", "enter":
		if v.activeTab == TabActiveLocks {
			if len(v.data.Locks) > 0 && v.selectedIdx < len(v.data.Locks) {
				v.openDetailView()
			}
		} else {
			// Deadlock history detail - return command to fetch full event
			if len(v.deadlocks) > 0 && v.deadlockSelectedIdx < len(v.deadlocks) {
				eventID := v.deadlocks[v.deadlockSelectedIdx].ID
				return func() tea.Msg {
					return FetchDeadlockDetailMsg{EventID: eventID}
				}
			}
		}

	// Refresh
	case "r":
		if !v.refreshing {
			v.refreshing = true
			return v.requestRefresh()
		}

	// Kill query
	case "x":
		if len(v.data.Locks) > 0 && v.selectedIdx < len(v.data.Locks) {
			if v.readOnly {
				v.showToast("Cannot kill in read-only mode", true)
			} else {
				lock := v.data.Locks[v.selectedIdx]
				// Only allow killing blocking processes
				if v.data.BlockingPIDs[lock.PID] {
					v.openKillConfirmation(lock)
				} else {
					v.showToast("Can only kill blocking processes", true)
				}
			}
		}

	// Copy query to clipboard
	case "y":
		if len(v.data.Locks) > 0 && v.selectedIdx < len(v.data.Locks) {
			lock := v.data.Locks[v.selectedIdx]
			if !v.clipboard.IsAvailable() {
				v.showToast("Clipboard unavailable: "+v.clipboard.Error(), true)
			} else if err := v.clipboard.Write(lock.Query); err != nil {
				v.showToast("Copy failed: "+err.Error(), true)
			} else {
				v.showToast("Query copied to clipboard", false)
			}
		}

	// Enable logging collector (for deadlock history)
	case "L":
		if v.activeTab == TabDeadlockHistory && !v.deadlockEnabled {
			v.mode = ModeConfirmEnableLogging
		}

	// Reset deadlock history
	case "R":
		if v.activeTab == TabDeadlockHistory && v.deadlockEnabled {
			v.mode = ModeConfirmResetDeadlocks
		}

	// Reset log positions
	case "P":
		if v.activeTab == TabDeadlockHistory && v.deadlockEnabled {
			v.mode = ModeConfirmResetLogPositions
		}
	}

	return nil
}

// handleConfirmKillMode processes keys in confirm kill mode.
func (v *LocksView) handleConfirmKillMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		pid := v.killPID
		v.mode = ModeNormal
		return func() tea.Msg {
			return KillLockMsg{PID: pid}
		}
	case "n", "N", "esc":
		v.mode = ModeNormal
	case "j", "down":
		v.killScrollDown(1)
	case "k", "up":
		v.killScrollUp(1)
	case "ctrl+d", "pgdown":
		v.killScrollDown(v.killContentHeight())
	case "ctrl+u", "pgup":
		v.killScrollUp(v.killContentHeight())
	}
	return nil
}

// openKillConfirmation opens the kill confirmation dialog with captured data.
func (v *LocksView) openKillConfirmation(lock models.Lock) {
	v.killPID = lock.PID
	v.killUser = lock.User
	v.killQuery = lock.Query
	v.killScrollOffset = 0

	// Build dialog content
	var lines []string

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorCriticalFg)
	lines = append(lines, headerStyle.Render("Terminate Process"))
	lines = append(lines, "")

	// Lock info
	lines = append(lines, fmt.Sprintf("PID:  %d", lock.PID))
	lines = append(lines, fmt.Sprintf("User: %s", lock.User))
	lines = append(lines, "")

	// Format and highlight SQL
	lines = append(lines, "Query:")
	formatted := v.formatSQLPlain(lock.Query)
	highlighted := v.formatSQLHighlighted(formatted)
	sqlLines := strings.Split(highlighted, "\n")
	lines = append(lines, sqlLines...)

	lines = append(lines, "")
	lines = append(lines, styles.WarningStyle.Render("This will terminate the blocking process."))

	v.killLines = lines
	v.mode = ModeConfirmKill
}

// killScrollDown scrolls the kill dialog down.
func (v *LocksView) killScrollDown(n int) {
	maxOffset := max(0, len(v.killLines)-v.killContentHeight())
	v.killScrollOffset = min(v.killScrollOffset+n, maxOffset)
}

// killScrollUp scrolls the kill dialog up.
func (v *LocksView) killScrollUp(n int) {
	v.killScrollOffset = max(0, v.killScrollOffset-n)
}

// killContentHeight returns the height available for kill dialog content.
func (v *LocksView) killContentHeight() int {
	// Dialog height minus title, footer, borders
	return max(1, min(20, v.height-10))
}

// handleDetailMode processes keys in detail view mode.
func (v *LocksView) handleDetailMode(key string) tea.Cmd {
	switch key {
	case "j", "down":
		v.detailScrollDown(1)
	case "k", "up":
		v.detailScrollUp(1)
	case "g", "home":
		v.detailScrollOffset = 0
	case "G", "end":
		maxOffset := max(0, len(v.detailLines)-v.detailContentHeight())
		v.detailScrollOffset = maxOffset
	case "ctrl+d", "pgdown":
		v.detailScrollDown(v.detailContentHeight())
	case "ctrl+u", "pgup":
		v.detailScrollUp(v.detailContentHeight())
	case "y":
		// Copy formatted query to clipboard
		if !v.clipboard.IsAvailable() {
			v.showToast("Clipboard unavailable: "+v.clipboard.Error(), true)
		} else if err := v.clipboard.Write(v.detailFormattedQuery); err != nil {
			v.showToast("Copy failed: "+err.Error(), true)
		} else {
			v.showToast("Query copied to clipboard", false)
		}
	case "esc", "q":
		v.mode = ModeNormal
	}
	return nil
}

// openDetailView opens the detail view for the selected lock.
func (v *LocksView) openDetailView() {
	lock := v.data.Locks[v.selectedIdx]
	v.detailQuery = lock.Query
	v.detailScrollOffset = 0

	// Format the SQL (plain for clipboard, highlighted for display)
	v.detailFormattedQuery = v.formatSQLPlain(lock.Query)
	formatted := v.formatSQLHighlighted(v.detailFormattedQuery)

	// Build detail content
	var lines []string

	// Lock info header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	lines = append(lines, headerStyle.Render("Lock Details"))
	lines = append(lines, "")

	// Lock metadata
	infoStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	lines = append(lines, fmt.Sprintf("PID:      %d", lock.PID))
	lines = append(lines, fmt.Sprintf("User:     %s", lock.User))
	lines = append(lines, fmt.Sprintf("Database: %s", lock.Database))
	lines = append(lines, fmt.Sprintf("Type:     %s", lock.LockType))
	lines = append(lines, fmt.Sprintf("Mode:     %s", lock.Mode))
	lines = append(lines, fmt.Sprintf("Granted:  %v", lock.Granted))
	if lock.Relation != "" {
		lines = append(lines, fmt.Sprintf("Relation: %s", lock.Relation))
	}
	lines = append(lines, fmt.Sprintf("Duration: %s", formatDuration(lock.Duration)))
	lines = append(lines, fmt.Sprintf("State:    %s", lock.State))

	// Blocking status
	status := v.data.GetStatus(lock.PID)
	var statusStr string
	switch status {
	case models.LockStatusBlocking:
		statusStr = styles.WarningStyle.Render("BLOCKING")
	case models.LockStatusBlocked:
		statusStr = styles.ErrorStyle.Render("BLOCKED")
	default:
		statusStr = infoStyle.Render("Normal")
	}
	lines = append(lines, fmt.Sprintf("Status:   %s", statusStr))

	lines = append(lines, "")
	lines = append(lines, headerStyle.Render("Query"))
	lines = append(lines, "")

	// Add formatted SQL lines
	sqlLines := strings.Split(formatted, "\n")
	lines = append(lines, sqlLines...)

	// Add blocking chain tree if this lock is involved in blocking
	if status != models.LockStatusNormal && len(v.data.Chains) > 0 {
		lines = append(lines, "")
		lines = append(lines, headerStyle.Render("Blocking Chain"))
		lines = append(lines, "")

		// Render the tree
		tree := components.RenderLockTree(v.data.Chains, v.width-4)
		treeLines := strings.Split(tree, "\n")
		lines = append(lines, treeLines...)
	}

	v.detailLines = lines
	v.mode = ModeDetail
}

// formatSQLPlain formats SQL without syntax highlighting (for clipboard).
func (v *LocksView) formatSQLPlain(sql string) string {
	if sql == "" {
		return ""
	}

	// Workaround for pgFormatter bug: empty strings ('') prevent proper column wrapping
	// Replace with placeholder before formatting, then restore after
	const placeholder = "'__EMPTY_STRING_PLACEHOLDER__'"
	sqlToFormat := strings.ReplaceAll(sql, "''", placeholder)

	// Try pg_format via Docker with -W 1 for one column per line
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sqlToFormat)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		formatted := strings.TrimSpace(out.String())
		// Restore empty strings
		return strings.ReplaceAll(formatted, placeholder, "''")
	}

	return sql
}

// formatSQLHighlighted applies syntax highlighting to already-formatted SQL.
func (v *LocksView) formatSQLHighlighted(sql string) string {
	if sql == "" {
		return ""
	}

	// Apply syntax highlighting
	var buf bytes.Buffer
	if err := quick.Highlight(&buf, sql, "postgresql", "terminal256", "monokai"); err != nil {
		return sql
	}

	return buf.String()
}

// detailScrollDown scrolls the detail view down.
func (v *LocksView) detailScrollDown(n int) {
	maxOffset := max(0, len(v.detailLines)-v.detailContentHeight())
	v.detailScrollOffset = min(v.detailScrollOffset+n, maxOffset)
}

// detailScrollUp scrolls the detail view up.
func (v *LocksView) detailScrollUp(n int) {
	v.detailScrollOffset = max(0, v.detailScrollOffset-n)
}

// detailContentHeight returns the height available for detail content.
func (v *LocksView) detailContentHeight() int {
	// height - title(1) - footer(1) - margins(2)
	return max(1, v.height-4)
}

// showToast displays a toast message.
func (v *LocksView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// moveSelection moves the selection by delta rows.
func (v *LocksView) moveSelection(delta int) {
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(v.data.Locks) {
		v.selectedIdx = max(0, len(v.data.Locks)-1)
	}
	v.ensureVisible()
}

// pageDown moves down by one page.
func (v *LocksView) pageDown() {
	pageSize := v.tableHeight()
	v.selectedIdx += pageSize
	if v.selectedIdx >= len(v.data.Locks) {
		v.selectedIdx = max(0, len(v.data.Locks)-1)
	}
	v.ensureVisible()
}

// pageUp moves up by one page.
func (v *LocksView) pageUp() {
	pageSize := v.tableHeight()
	v.selectedIdx -= pageSize
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	v.ensureVisible()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (v *LocksView) ensureVisible() {
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
func (v *LocksView) tableHeight() int {
	// height - app header(1) - status(1) - title(1) - tabs(1) - header(1) - footer(1)
	return max(1, v.height-7)
}

// renderTitle renders the view title.
func (v *LocksView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)
	return titleStyle.Render("Locks")
}

// cycleSort cycles through sort columns.
func (v *LocksView) cycleSort() {
	v.sortColumn = (v.sortColumn + 1) % 5 // PID, Type, Mode, Duration, Granted
	v.sortLocks()
}

// toggleSortDirection toggles between ascending and descending sort for active locks.
func (v *LocksView) toggleSortDirection() {
	v.sortAsc = !v.sortAsc
	v.sortLocks()
}

// cycleDeadlockSort cycles through deadlock sort columns.
func (v *LocksView) cycleDeadlockSort() {
	v.deadlockSortColumn = (v.deadlockSortColumn + 1) % 3 // Time, Database, Processes
	v.sortDeadlocks()
}

// toggleDeadlockSortDirection toggles between ascending and descending sort.
func (v *LocksView) toggleDeadlockSortDirection() {
	v.deadlockSortAsc = !v.deadlockSortAsc
	v.sortDeadlocks()
}

// sortDeadlocks sorts the deadlocks by the current sort column and direction.
func (v *LocksView) sortDeadlocks() {
	if len(v.deadlocks) == 0 {
		return
	}

	sort.SliceStable(v.deadlocks, func(i, j int) bool {
		a, b := v.deadlocks[i], v.deadlocks[j]

		var less bool
		switch v.deadlockSortColumn {
		case DeadlockSortByTime:
			less = a.DetectedAt.After(b.DetectedAt) // Default: most recent first
		case DeadlockSortByDatabase:
			if a.DatabaseName != b.DatabaseName {
				less = a.DatabaseName < b.DatabaseName
			} else {
				less = a.DetectedAt.After(b.DetectedAt)
			}
		case DeadlockSortByProcesses:
			if a.ProcessCount != b.ProcessCount {
				less = a.ProcessCount > b.ProcessCount // Default: most processes first
			} else {
				less = a.DetectedAt.After(b.DetectedAt)
			}
		default:
			less = a.DetectedAt.After(b.DetectedAt)
		}

		// Reverse if ascending
		if v.deadlockSortAsc {
			return !less
		}
		return less
	})
}

// sortLocks sorts the locks by the current sort column and direction.
// Blocking and blocked locks are always sorted to the top (priority section),
// then user's chosen sort is applied within each group.
func (v *LocksView) sortLocks() {
	if v.data == nil || len(v.data.Locks) == 0 {
		return
	}

	sort.SliceStable(v.data.Locks, func(i, j int) bool {
		a, b := v.data.Locks[i], v.data.Locks[j]

		// Get status for priority sorting
		statusA := v.data.GetStatus(a.PID)
		statusB := v.data.GetStatus(b.PID)

		// Priority: Blocking (1) > Blocked (2) > Normal (3)
		priorityA := v.lockPriority(statusA)
		priorityB := v.lockPriority(statusB)

		// If different priorities, sort by priority first
		if priorityA != priorityB {
			return priorityA < priorityB
		}

		// Same priority group - apply user's chosen sort
		var less bool
		switch v.sortColumn {
		case SortByPID:
			less = a.PID < b.PID
		case SortByType:
			less = a.LockType < b.LockType
		case SortByMode:
			less = a.Mode < b.Mode
		case SortByGranted:
			// Waiting (not granted) first
			if a.Granted != b.Granted {
				less = !a.Granted
			} else {
				less = a.PID < b.PID
			}
		case SortByDuration:
			less = a.Duration > b.Duration // Default: longest first
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

// lockPriority returns sort priority for a lock status.
// Lower number = higher priority (appears first).
func (v *LocksView) lockPriority(status models.LockStatus) int {
	switch status {
	case models.LockStatusBlocking:
		return 1 // Yellow - blocking others
	case models.LockStatusBlocked:
		return 2 // Red - waiting
	default:
		return 3 // Normal
	}
}

// requestRefresh returns a command to request data refresh.
func (v *LocksView) requestRefresh() tea.Cmd {
	return func() tea.Msg {
		return RefreshLocksMsg{}
	}
}

// KillLockMsg requests killing a blocking query.
type KillLockMsg struct {
	PID int
}

// RefreshLocksMsg requests lock data refresh.
type RefreshLocksMsg struct{}

// FetchDeadlockDetailMsg requests full deadlock event details.
type FetchDeadlockDetailMsg struct {
	EventID int64
}

// EnableLoggingCollectorMsg requests enabling logging_collector.
type EnableLoggingCollectorMsg struct{}

// EnableLoggingCollectorResultMsg contains the result of enabling logging.
type EnableLoggingCollectorResultMsg struct {
	Success bool
	Error   error
}

// View renders the locks view.
func (v *LocksView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for overlay modes
	if v.mode == ModeConfirmKill {
		return v.renderWithOverlay(v.renderConfirmKillDialog())
	}
	if v.mode == ModeConfirmEnableLogging {
		return v.renderWithOverlay(v.renderEnableLoggingDialog())
	}
	if v.mode == ModeConfirmResetDeadlocks {
		return v.renderWithOverlay(v.renderResetDeadlocksDialog())
	}
	if v.mode == ModeConfirmResetLogPositions {
		return v.renderWithOverlay(v.renderResetLogPositionsDialog())
	}
	if v.mode == ModeDetail {
		return v.renderDetailView()
	}
	if v.mode == ModeDeadlockDetail {
		return v.renderDeadlockDetailView()
	}
	if v.mode == ModeHelp {
		if v.activeTab == TabDeadlockHistory {
			return DeadlockHelpOverlay(v.width, v.height)
		}
		return HelpOverlay(v.width, v.height)
	}

	// Status bar
	statusBar := v.renderStatusBar()

	// Title
	title := v.renderTitle()

	// Tab bar
	tabBar := TabBar(v.activeTab, v.width)

	// Render based on active tab
	var content string
	var footer string

	if v.activeTab == TabActiveLocks {
		// Column headers
		header := v.renderHeader()
		// Table
		table := v.renderTable()
		content = lipgloss.JoinVertical(lipgloss.Left, header, table)
		footer = v.renderFooter()

		// Calculate view header height for mouse coordinate translation
		// This is the number of rows from view top to first data row
		v.viewHeaderHeight = lipgloss.Height(statusBar) + lipgloss.Height(title) +
			lipgloss.Height(tabBar) + lipgloss.Height(header)
	} else {
		// Deadlock history
		content = v.renderDeadlockHistory()
		footer = v.renderDeadlockFooter()

		// Calculate view header height for deadlock tab (same base + deadlock header)
		v.viewHeaderHeight = lipgloss.Height(statusBar) + lipgloss.Height(title) +
			lipgloss.Height(tabBar)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		title,
		tabBar,
		content,
		footer,
	)
}

// renderConfirmKillDialog renders the kill confirmation dialog.
func (v *LocksView) renderConfirmKillDialog() string {
	if len(v.killLines) == 0 {
		return ""
	}

	// Get visible content with scroll
	height := v.killContentHeight()
	endIdx := min(v.killScrollOffset+height, len(v.killLines))
	visibleLines := v.killLines[v.killScrollOffset:endIdx]
	content := strings.Join(visibleLines, "\n")

	// Footer with scroll info and prompt
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	scrollInfo := ""
	if len(v.killLines) > height {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.killScrollOffset+1, len(v.killLines))
	}

	prompt := "[y]es [n]o [j/k]scroll" + scrollInfo

	fullContent := lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		"",
		footerStyle.Render(prompt),
	)

	return styles.DialogStyle.Render(fullContent)
}

// renderEnableLoggingDialog renders the enable logging confirmation dialog.
func (v *LocksView) renderEnableLoggingDialog() string {
	content := `Enable PostgreSQL Logging Collector?

This will:
  • Modify postgresql.conf
  • Restart PostgreSQL server
  • Enable deadlock history capture

Connection will be briefly interrupted.

Continue? [y/n]`

	return content
}

// renderResetDeadlocksDialog renders the reset deadlocks confirmation dialog.
func (v *LocksView) renderResetDeadlocksDialog() string {
	title := styles.DialogTitleStyle.Render("Reset Deadlock History")

	details := fmt.Sprintf(
		"This will clear all %d recorded deadlocks.\n\nThis action cannot be undone.",
		len(v.deadlocks),
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

// renderResetLogPositionsDialog renders the reset log positions confirmation dialog.
func (v *LocksView) renderResetLogPositionsDialog() string {
	title := styles.DialogTitleStyle.Render("Reset Log Positions")

	details := "This will reset all log file positions.\n\nNext refresh will re-parse all log files from the beginning,\nwhich may re-detect previously recorded deadlocks."

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

// renderDetailView renders the detail view.
func (v *LocksView) renderDetailView() string {
	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)
	title := titleStyle.Render("Lock Details")

	// Content
	var content string
	if len(v.detailLines) == 0 {
		content = styles.InfoStyle.Render("No details available")
	} else {
		height := v.detailContentHeight()
		endIdx := min(v.detailScrollOffset+height, len(v.detailLines))
		visibleLines := v.detailLines[v.detailScrollOffset:endIdx]

		// Pad to fill height
		for len(visibleLines) < height {
			visibleLines = append(visibleLines, "")
		}
		content = strings.Join(visibleLines, "\n")
	}

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	scrollInfo := ""
	if len(v.detailLines) > v.detailContentHeight() {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.detailScrollOffset+1, len(v.detailLines))
	}

	footer := footerStyle.Render("[j/k]scroll [g/G]top/bottom [y]copy [esc/q]back" + scrollInfo)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content,
		footer,
	)
}

// renderWithOverlay renders the main view with an overlay on top.
func (v *LocksView) renderWithOverlay(overlay string) string {
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
	)
}

// renderStatusBar renders the top status bar.
func (v *LocksView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Use appropriate timestamp and stale threshold based on active tab
	var updateTime time.Time
	var staleThreshold time.Duration
	if v.activeTab == TabDeadlockHistory {
		updateTime = v.deadlockLastUpdate
		staleThreshold = 35 * time.Second // Deadlocks refresh every 30s
	} else {
		updateTime = v.lastUpdate
		staleThreshold = 5 * time.Second // Active locks refresh every 1s
	}

	var staleIndicator string
	if !updateTime.IsZero() && time.Since(updateTime) > staleThreshold {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	timestamp := styles.StatusTimeStyle.Render(updateTime.Format("2006-01-02 15:04:05"))

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
func (v *LocksView) renderHeader() string {
	// Column widths - designed for minimum 80 char terminal
	pidWidth := 6
	typeWidth := 10
	modeWidth := 12
	durationWidth := 7
	grantedWidth := 4
	dbWidth := 8
	relationWidth := 10
	queryWidth := v.width - pidWidth - typeWidth - modeWidth - durationWidth - grantedWidth - dbWidth - relationWidth - 9
	if queryWidth < 10 {
		queryWidth = 10
	}

	var headers []string
	headers = append(headers, padLeft(v.sortIndicator("PID", SortByPID), pidWidth))
	headers = append(headers, padRight(v.sortIndicator("Type", SortByType), typeWidth))
	headers = append(headers, padRight(v.sortIndicator("Mode", SortByMode), modeWidth))
	headers = append(headers, padRight(v.sortIndicator("Dur", SortByDuration), durationWidth))
	headers = append(headers, padCenter(v.sortIndicator("Grt", SortByGranted), grantedWidth))
	headers = append(headers, padRight("DB", dbWidth))
	headers = append(headers, padRight("Relation", relationWidth))
	headers = append(headers, padRight("Query", queryWidth))

	headerLine := strings.Join(headers, " ")
	return styles.TableHeaderStyle.Width(v.width - 2).Render(headerLine)
}

// sortIndicator adds an arrow to the column name if it's the sort column.
func (v *LocksView) sortIndicator(name string, col SortColumn) string {
	if v.sortColumn == col {
		if v.sortAsc {
			return name + " ↑"
		}
		return name + " ↓"
	}
	return name
}

// deadlockSortIndicator adds an arrow to the column name if it's the sort column.
func (v *LocksView) deadlockSortIndicator(name string, col DeadlockSortColumn) string {
	if v.deadlockSortColumn == col {
		if v.deadlockSortAsc {
			return name + " ↑"
		}
		return name + " ↓"
	}
	return name
}

// renderTable renders the lock table.
func (v *LocksView) renderTable() string {
	if len(v.data.Locks) == 0 {
		emptyMsg := "No locks detected"
		return lipgloss.NewStyle().
			Width(v.width-2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorTextDim).
			Render(emptyMsg)
	}

	// Column widths - designed for minimum 80 char terminal
	pidWidth := 6
	typeWidth := 10
	modeWidth := 12
	durationWidth := 7
	grantedWidth := 4
	dbWidth := 8
	relationWidth := 10
	queryWidth := v.width - pidWidth - typeWidth - modeWidth - durationWidth - grantedWidth - dbWidth - relationWidth - 9
	if queryWidth < 10 {
		queryWidth = 10
	}

	var rows []string
	tableHeight := v.tableHeight()
	endIdx := min(v.scrollOffset+tableHeight, len(v.data.Locks))

	// Define blocking styles using centralized colors
	blockedStyle := lipgloss.NewStyle().Foreground(styles.ColorBlocked)   // Red
	blockingStyle := lipgloss.NewStyle().Foreground(styles.ColorBlocking) // Yellow

	for i := v.scrollOffset; i < endIdx; i++ {
		lock := v.data.Locks[i]
		isSelected := i == v.selectedIdx

		// Format row
		pid := fmt.Sprintf("%d", lock.PID)
		lockType := truncate(lock.LockType, typeWidth-1)
		mode := truncate(lock.Mode, modeWidth-1)
		duration := formatDuration(lock.Duration)
		granted := "Yes"
		if !lock.Granted {
			granted = "No"
		}
		db := truncate(lock.Database, dbWidth-1)
		relation := truncate(lock.Relation, relationWidth-1)
		query := truncate(lock.Query, queryWidth-3)

		row := fmt.Sprintf("%s %s %s %s %s %s %s %s",
			padLeft(pid, pidWidth),
			padRight(lockType, typeWidth),
			padRight(mode, modeWidth),
			padRight(duration, durationWidth),
			padCenter(granted, grantedWidth),
			padRight(db, dbWidth),
			padRight(relation, relationWidth),
			padRight(query, queryWidth),
		)

		// Apply styling based on blocking status
		status := v.data.GetStatus(lock.PID)

		if isSelected {
			row = styles.TableSelectedStyle.Width(v.width - 2).Render(row)
		} else {
			switch status {
			case models.LockStatusBlocked:
				row = blockedStyle.Width(v.width - 2).Render(row)
			case models.LockStatusBlocking:
				row = blockingStyle.Width(v.width - 2).Render(row)
			default:
				row = styles.TableCellStyle.Width(v.width - 2).Render(row)
			}
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
func (v *LocksView) renderFooter() string {
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
	} else {
		hints = styles.FooterHintStyle.Render("[j/k]nav [d]etail [s/S]ort [y]ank [x]kill [r]efresh [h]elp")
	}

	arrow := "↓"
	if v.sortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)
	count := fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.data.Locks)), v.totalCount)
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

// renderDeadlockHistory renders the deadlock history table.
func (v *LocksView) renderDeadlockHistory() string {
	// Show spinner while loading (before we know if enabled)
	if v.deadlockLoading {
		msg := " Scanning logs for deadlock history..."
		if v.deadlockTotalFiles > 0 {
			msg = fmt.Sprintf(" Scanning log file %d/%d for deadlock history...", v.deadlockCurrentFile, v.deadlockTotalFiles)
		}
		return styles.InfoStyle.Render(v.deadlockSpinner.View() + msg)
	}

	if !v.deadlockEnabled {
		return styles.InfoStyle.Render("Deadlock history requires logging_collector = on in PostgreSQL")
	}

	if len(v.deadlocks) == 0 {
		return styles.InfoStyle.Render("No deadlocks recorded")
	}

	// Header with sort indicators
	headerStyle := styles.TableHeaderStyle.Width(v.width - 2)
	header := headerStyle.Render(fmt.Sprintf("  %-20s  %-15s  %8s  %-30s",
		v.deadlockSortIndicator("Detected", DeadlockSortByTime),
		v.deadlockSortIndicator("Database", DeadlockSortByDatabase),
		v.deadlockSortIndicator("Procs", DeadlockSortByProcesses),
		"Tables"))

	// Table rows
	tableHeight := v.deadlockTableHeight()
	endIdx := min(v.deadlockScrollOffset+tableHeight, len(v.deadlocks))
	visibleDeadlocks := v.deadlocks[v.deadlockScrollOffset:endIdx]

	var rows []string
	for i, dl := range visibleDeadlocks {
		actualIdx := v.deadlockScrollOffset + i
		detected := dl.DetectedAt.Format("2006-01-02 15:04:05")
		tables := dl.Tables
		if len(tables) > 30 {
			tables = tables[:27] + "..."
		}

		row := fmt.Sprintf("  %-20s  %-15s  %8d  %-30s",
			detected,
			truncateString(dl.DatabaseName, 15),
			dl.ProcessCount,
			tables)

		if actualIdx == v.deadlockSelectedIdx {
			row = styles.TableSelectedStyle.Width(v.width - 2).Render(row)
		} else {
			row = styles.TableCellStyle.Width(v.width - 2).Render(row)
		}
		rows = append(rows, row)
	}

	// Pad with empty rows
	for len(rows) < tableHeight {
		rows = append(rows, styles.TableCellStyle.Width(v.width-2).Render(""))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(rows, "\n"))
}

// renderDeadlockFooter renders the footer for deadlock history tab.
func (v *LocksView) renderDeadlockFooter() string {
	var hints string

	// Show toast message if recent
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		hints = styles.FooterHintStyle.Render("[j/k]nav [d]etail [s/S]ort [P/R]eset [h]elp")
	}

	arrow := "↓"
	if v.deadlockSortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.deadlockSortColumn.String(), arrow)
	count := fmt.Sprintf("%d / %d", min(v.deadlockSelectedIdx+1, len(v.deadlocks)), len(v.deadlocks))
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

// deadlockTableHeight returns the number of visible deadlock table rows.
func (v *LocksView) deadlockTableHeight() int {
	// height - status(1) - title(1) - tabs(1) - header(1) - footer(1) - padding
	return max(1, v.height-7)
}

// moveDeadlockSelection moves the deadlock selection by delta rows.
func (v *LocksView) moveDeadlockSelection(delta int) {
	if len(v.deadlocks) == 0 {
		return
	}
	v.deadlockSelectedIdx += delta
	if v.deadlockSelectedIdx < 0 {
		v.deadlockSelectedIdx = 0
	}
	if v.deadlockSelectedIdx >= len(v.deadlocks) {
		v.deadlockSelectedIdx = len(v.deadlocks) - 1
	}
	v.ensureDeadlockVisible()
}

// ensureDeadlockVisible adjusts scroll to keep selection visible.
func (v *LocksView) ensureDeadlockVisible() {
	tableHeight := v.deadlockTableHeight()
	if tableHeight <= 0 {
		return
	}
	if v.deadlockSelectedIdx < v.deadlockScrollOffset {
		v.deadlockScrollOffset = v.deadlockSelectedIdx
	}
	if v.deadlockSelectedIdx >= v.deadlockScrollOffset+tableHeight {
		v.deadlockScrollOffset = v.deadlockSelectedIdx - tableHeight + 1
	}
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

// formatDeadlockDetail formats a deadlock event for display using deadviz.
func (v *LocksView) formatDeadlockDetail(event *sqlite.DeadlockEvent) []string {
	var buf strings.Builder

	// Use the deadviz visualizer for the main output
	width := uint(v.width)
	if width < 60 {
		width = 60
	}
	if err := deadviz.Visualize(&buf, event, width, v.formatDeadlockSQL); err != nil {
		return []string{fmt.Sprintf("Error rendering deadlock: %v", err)}
	}

	// Split into lines
	lines := strings.Split(buf.String(), "\n")

	return lines
}

// formatDeadlockSQL formats a SQL query with pgFormatter and chroma syntax highlighting.
func (v *LocksView) formatDeadlockSQL(sql string) string {
	if sql == "" {
		return ""
	}

	// Workaround for pgFormatter bug: empty strings ('') prevent proper column wrapping
	const placeholder = "'__EMPTY_STRING_PLACEHOLDER__'"
	sqlToFormat := strings.ReplaceAll(sql, "''", placeholder)

	// Try pg_format via Docker
	formatted := sql
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sqlToFormat)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		formatted = strings.TrimSpace(out.String())
		formatted = strings.ReplaceAll(formatted, placeholder, "''")
	}

	// Apply syntax highlighting
	var buf bytes.Buffer
	if err := quick.Highlight(&buf, formatted, "postgresql", "terminal256", "monokai"); err != nil {
		return formatted
	}

	return buf.String()
}

// formatDeadlockSQLPlain formats a SQL query without highlighting (for clipboard).
func (v *LocksView) formatDeadlockSQLPlain(sql string) string {
	if sql == "" {
		return ""
	}

	const placeholder = "'__EMPTY_STRING_PLACEHOLDER__'"
	sqlToFormat := strings.ReplaceAll(sql, "''", placeholder)

	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sqlToFormat)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		formatted := strings.TrimSpace(out.String())
		return strings.ReplaceAll(formatted, placeholder, "''")
	}

	return sql
}

// renderDeadlockDetailView renders the deadlock detail view.
func (v *LocksView) renderDeadlockDetailView() string {
	if len(v.deadlockDetailLines) == 0 {
		return ""
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		Padding(0, 1)
	title := titleStyle.Render("Deadlock Details")

	// Content area
	contentHeight := v.height - 4 // title + footer + padding
	endIdx := min(v.deadlockDetailScroll+contentHeight, len(v.deadlockDetailLines))
	visibleLines := v.deadlockDetailLines[v.deadlockDetailScroll:endIdx]

	contentStyle := lipgloss.NewStyle().
		Padding(0, 2)
	content := contentStyle.Render(strings.Join(visibleLines, "\n"))

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	scrollInfo := ""
	if len(v.deadlockDetailLines) > contentHeight {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.deadlockDetailScroll+1, len(v.deadlockDetailLines))
	}
	footer := footerStyle.Render(fmt.Sprintf("[j/k]scroll [g/G]top/bottom [c]copy [esc/q]back%s", scrollInfo))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content,
		footer,
	)
}

// deadlockDetailScrollDown scrolls down in the deadlock detail view.
func (v *LocksView) deadlockDetailScrollDown(n int) {
	maxScroll := max(0, len(v.deadlockDetailLines)-(v.height-4))
	v.deadlockDetailScroll = min(v.deadlockDetailScroll+n, maxScroll)
}

// deadlockDetailScrollUp scrolls up in the deadlock detail view.
func (v *LocksView) deadlockDetailScrollUp(n int) {
	v.deadlockDetailScroll = max(0, v.deadlockDetailScroll-n)
}

// SetSize sets the dimensions of the view.
func (v *LocksView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *LocksView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *LocksView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *LocksView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
}

// IsInputMode returns true if in an input mode.
func (v *LocksView) IsInputMode() bool {
	return v.mode == ModeDetail || v.mode == ModeDeadlockDetail
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
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

func padCenter(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	left := (width - w) / 2
	right := width - w - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
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
