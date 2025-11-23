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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
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

// LocksMode represents the current interaction mode.
type LocksMode int

const (
	ModeNormal LocksMode = iota
	ModeDetail
	ModeConfirmKill
	ModeHelp
)

// LocksView displays lock information and blocking relationships.
type LocksView struct {
	width  int
	height int

	// State
	mode           LocksMode
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	readOnly       bool

	// Data
	data       *models.LocksData
	totalCount int
	err        error

	// Table state
	selectedIdx  int
	scrollOffset int
	sortColumn   SortColumn

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Detail view state
	detailQuery          string
	detailFormattedQuery string // formatted version for clipboard (no ANSI)
	detailScrollOffset   int
	detailLines          []string

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewLocksView creates a new locks view.
func NewLocksView() *LocksView {
	return &LocksView{
		mode:       ModeNormal,
		sortColumn: SortByPID,
		data:       models.NewLocksData(),
		clipboard:  ui.NewClipboardWriter(),
	}
}

// Init initializes the locks view.
func (v *LocksView) Init() tea.Cmd {
	return nil
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

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case tea.MouseMsg:
		switch v.mode {
		case ModeNormal:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.moveSelection(-1)
			case tea.MouseButtonWheelDown:
				v.moveSelection(1)
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					// Table starts after: status(1) + title(1) + header(1)
					clickedRow := msg.Y - 7
					if clickedRow >= 0 {
						newIdx := v.scrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(v.data.Locks) {
							v.selectedIdx = newIdx
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

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "esc", "q":
			v.mode = ModeNormal
		}
		return nil
	}

	// Normal mode keys
	switch key {
	// Help
	case "h":
		v.mode = ModeHelp

	// Navigation
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
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
		v.cycleSort()

	// Detail view
	case "d", "enter":
		if len(v.data.Locks) > 0 && v.selectedIdx < len(v.data.Locks) {
			v.openDetailView()
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
					v.mode = ModeConfirmKill
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
	}

	return nil
}

// handleConfirmKillMode processes keys in confirm kill mode.
func (v *LocksView) handleConfirmKillMode(key string) tea.Cmd {
	switch key {
	case "y", "Y":
		if v.selectedIdx < len(v.data.Locks) {
			lock := v.data.Locks[v.selectedIdx]
			pid := lock.PID
			v.mode = ModeNormal
			return func() tea.Msg {
				return KillLockMsg{PID: pid}
			}
		}
		v.mode = ModeNormal
	case "n", "N", "esc":
		v.mode = ModeNormal
	}
	return nil
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
	// height - status(1) - title(1) - header(1) - footer(1) - padding
	return max(1, v.height-5)
}

// renderTitle renders the view title.
func (v *LocksView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)
	return titleStyle.Render("Active Locks")
}

// cycleSort cycles through sort columns.
func (v *LocksView) cycleSort() {
	v.sortColumn = (v.sortColumn + 1) % 5 // PID, Type, Mode, Duration, Granted
	v.sortLocks()
}

// sortLocks sorts the locks by the current sort column.
func (v *LocksView) sortLocks() {
	if v.data == nil || len(v.data.Locks) == 0 {
		return
	}

	sort.SliceStable(v.data.Locks, func(i, j int) bool {
		a, b := v.data.Locks[i], v.data.Locks[j]
		switch v.sortColumn {
		case SortByPID:
			return a.PID < b.PID
		case SortByType:
			return a.LockType < b.LockType
		case SortByMode:
			return a.Mode < b.Mode
		case SortByGranted:
			// Waiting (not granted) first
			if a.Granted != b.Granted {
				return !a.Granted
			}
			return a.PID < b.PID
		case SortByDuration:
			return a.Duration > b.Duration // Longest first
		default:
			return a.PID < b.PID
		}
	})
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

// View renders the locks view.
func (v *LocksView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for overlay modes
	if v.mode == ModeConfirmKill {
		return v.renderWithOverlay(v.renderConfirmKillDialog())
	}
	if v.mode == ModeDetail {
		return v.renderDetailView()
	}
	if v.mode == ModeHelp {
		return HelpOverlay(v.width, v.height)
	}

	// Status bar
	statusBar := v.renderStatusBar()

	// Title
	title := v.renderTitle()

	// Column headers
	header := v.renderHeader()

	// Table
	table := v.renderTable()

	// Footer
	footer := v.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		title,
		header,
		table,
		footer,
	)
}

// renderConfirmKillDialog renders the kill confirmation dialog.
func (v *LocksView) renderConfirmKillDialog() string {
	if v.selectedIdx >= len(v.data.Locks) {
		return ""
	}

	lock := v.data.Locks[v.selectedIdx]
	title := styles.DialogTitleStyle.Render("Terminate Process")

	query := lock.Query
	if len(query) > 60 {
		query = query[:57] + "..."
	}

	details := fmt.Sprintf(
		"PID: %d\nUser: %s\nQuery: %s\n\nThis will terminate the blocking process.",
		lock.PID,
		lock.User,
		query,
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
		hints = styles.FooterHintStyle.Render("[j/k]nav [d]etail [s]ort [y]ank [x]kill [r]efresh [h]elp")
	}

	sortInfo := fmt.Sprintf("Sort: %s ↓", v.sortColumn.String())
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
	return v.mode == ModeDetail
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
