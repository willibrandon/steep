// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// SQLEditorMode represents the current interaction mode.
type SQLEditorMode int

const (
	ModeNormal SQLEditorMode = iota
	ModeExecuting
	ModeHelp
	ModeCommandLine
	ModeHistorySearch
	ModeSnippetBrowser
)

// SQLEditorView displays the SQL Editor interface.
type SQLEditorView struct {
	width  int
	height int

	// State
	mode           SQLEditorMode
	focus          FocusArea
	connected      bool
	connectionInfo string
	readOnly       bool

	// Editor
	textarea    textarea.Model
	placeholder string

	// Results
	results       *ResultSet
	selectedRow   int
	scrollOffset  int
	executedQuery string
	lastError     error

	// Execution
	executor   *SessionExecutor
	executing  bool
	startTime  time.Time
	lastUpdate time.Time

	// Key bindings
	keys KeyMap

	// UI state
	splitRatio  float64 // 0.0-1.0, portion for editor
	showHelp    bool
	helpScroll  int

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewSQLEditorView creates a new SQL Editor view.
func NewSQLEditorView() *SQLEditorView {
	ta := textarea.New()
	ta.Placeholder = "Enter SQL query... (Ctrl+Enter to execute)"
	ta.ShowLineNumbers = true
	ta.CharLimit = MaxCharLimit
	ta.SetWidth(80)
	ta.SetHeight(10)

	// Apply focused style
	ta.FocusedStyle.Base = styles.EditorBorderStyle
	ta.FocusedStyle.CursorLine = styles.EditorCursorLineStyle
	ta.BlurredStyle.Base = styles.EditorBlurredBorderStyle

	ta.Focus()

	return &SQLEditorView{
		mode:       ModeNormal,
		focus:      FocusEditor,
		textarea:   ta,
		keys:       DefaultKeyMap(),
		splitRatio: 0.4, // 40% editor, 60% results
		clipboard:  ui.NewClipboardWriter(),
		results: &ResultSet{
			PageSize:    DefaultPageSize,
			CurrentPage: 1,
		},
	}
}

// Init initializes the SQL Editor view.
func (v *SQLEditorView) Init() tea.Cmd {
	return textarea.Blink
}

// SetSize sets the dimensions of the view.
func (v *SQLEditorView) SetSize(width, height int) {
	v.width = width
	v.height = height

	// Calculate editor and results heights
	editorHeight := int(float64(height-4) * v.splitRatio) // -4 for status bar and padding
	if editorHeight < 3 {
		editorHeight = 3
	}

	// Set textarea dimensions (account for border)
	v.textarea.SetWidth(width - 4)
	v.textarea.SetHeight(editorHeight - 2)
}

// SetPool sets the database connection pool and creates executor.
func (v *SQLEditorView) SetPool(pool *pgxpool.Pool) {
	v.executor = NewSessionExecutor(pool, v.readOnly)
}

// SetConnected sets the connection status.
func (v *SQLEditorView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *SQLEditorView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *SQLEditorView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
	if v.executor != nil {
		v.executor.readOnly = readOnly
	}
}

// IsInputMode returns true when the view is in a mode that should consume keys.
func (v *SQLEditorView) IsInputMode() bool {
	return v.focus == FocusEditor || v.mode == ModeCommandLine || v.mode == ModeHistorySearch || v.mode == ModeSnippetBrowser
}

// Update handles messages for the SQL Editor view.
func (v *SQLEditorView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case QueryExecutingMsg:
		v.mode = ModeExecuting
		v.executing = true
		v.startTime = msg.StartTime
		v.executedQuery = msg.SQL
		v.lastError = nil

	case QueryCompletedMsg:
		v.mode = ModeNormal
		v.executing = false
		v.lastUpdate = time.Now()

		if msg.Result != nil {
			if msg.Result.Error != nil {
				v.lastError = msg.Result.Error
				v.showToast(msg.Result.Error.Error(), true)
			} else if msg.Result.Message != "" {
				v.showToast(msg.Result.Message, false)
			}

			// Convert results to display format
			if msg.Result.Rows != nil {
				v.results = &ResultSet{
					Columns:     msg.Result.Columns,
					Rows:        FormatResultSet(msg.Result.Rows),
					TotalRows:   len(msg.Result.Rows),
					CurrentPage: 1,
					PageSize:    DefaultPageSize,
					ExecutionMs: msg.Result.Duration.Milliseconds(),
				}
				v.selectedRow = 0
				v.scrollOffset = 0
			}
		}

	case QueryCancelledMsg:
		v.mode = ModeNormal
		v.executing = false
		v.showToast("Query cancelled", false)

	case TransactionStateChangedMsg:
		// Transaction state is tracked in executor

	case ConnectionStatusMsg:
		v.connected = msg.Connected
		if msg.Reconnected {
			v.showToast("Reconnected to database", false)
		} else if msg.Error != nil {
			v.showToast("Connection lost: "+msg.Error.Error(), true)
		}

	case CellCopiedMsg:
		if msg.Error != nil {
			v.showToast("Copy failed: "+msg.Error.Error(), true)
		} else {
			v.showToast("Cell value copied", false)
		}

	case RowCopiedMsg:
		if msg.Error != nil {
			v.showToast("Copy failed: "+msg.Error.Error(), true)
		} else {
			v.showToast("Row copied", false)
		}
	}

	// Update textarea if editor has focus
	if v.focus == FocusEditor && v.mode == ModeNormal {
		var cmd tea.Cmd
		v.textarea, cmd = v.textarea.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return v, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input.
func (v *SQLEditorView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "?", "esc", "q":
			v.mode = ModeNormal
			v.showHelp = false
		case "j", "down":
			v.helpScroll++
		case "k", "up":
			if v.helpScroll > 0 {
				v.helpScroll--
			}
		}
		return nil
	}

	// Handle executing mode - only allow cancel
	if v.mode == ModeExecuting {
		if key == "esc" {
			if v.executor != nil {
				v.executor.CancelQuery()
			}
			return func() tea.Msg { return QueryCancelledMsg{} }
		}
		return nil
	}

	// Global keys
	switch key {
	case "?":
		v.mode = ModeHelp
		v.showHelp = true
		v.helpScroll = 0
		return nil

	case "ctrl+enter":
		return v.executeQuery()

	case "tab":
		v.switchFocus()
		return nil

	case "ctrl+up":
		v.growEditor()
		return nil

	case "ctrl+down":
		v.shrinkEditor()
		return nil
	}

	// Focus-specific keys
	if v.focus == FocusResults {
		return v.handleResultsKeys(key)
	}

	return nil
}

// handleResultsKeys handles keys when results pane has focus.
func (v *SQLEditorView) handleResultsKeys(key string) tea.Cmd {
	if v.results == nil || v.results.TotalRows == 0 {
		return nil
	}

	switch key {
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g", "home":
		v.selectedRow = 0
		v.ensureVisible()
	case "G", "end":
		v.selectedRow = len(v.results.Rows) - 1
		v.ensureVisible()
	case "ctrl+d", "pgdown":
		v.moveSelection(10)
	case "ctrl+u", "pgup":
		v.moveSelection(-10)
	case "n":
		v.nextPage()
	case "p":
		v.prevPage()
	case "y":
		return v.copyCell()
	case "Y":
		return v.copyRow()
	}

	return nil
}

// executeQuery executes the current query.
func (v *SQLEditorView) executeQuery() tea.Cmd {
	sql := strings.TrimSpace(v.textarea.Value())
	if sql == "" {
		v.showToast("No query to execute", true)
		return nil
	}

	if v.executor == nil {
		v.showToast("No database connection", true)
		return nil
	}

	v.mode = ModeExecuting
	v.executing = true
	v.startTime = time.Now()
	v.executedQuery = sql

	return func() tea.Msg {
		// Return executing message immediately
		return QueryExecutingMsg{
			SQL:       sql,
			StartTime: time.Now(),
		}
	}
}

// ExecuteQueryCmd returns a command that executes the query.
// This should be called from the app to perform actual execution.
func (v *SQLEditorView) ExecuteQueryCmd() tea.Cmd {
	if v.executor == nil || v.executedQuery == "" {
		return nil
	}

	sql := v.executedQuery

	return func() tea.Msg {
		result, _ := v.executor.ExecuteQuery(
			context.Background(),
			sql,
			DefaultQueryTimeout,
		)
		return QueryCompletedMsg{Result: result}
	}
}

// switchFocus toggles focus between editor and results.
func (v *SQLEditorView) switchFocus() {
	if v.focus == FocusEditor {
		v.focus = FocusResults
		v.textarea.Blur()
	} else {
		v.focus = FocusEditor
		v.textarea.Focus()
	}
}

// moveSelection moves the row selection by delta.
func (v *SQLEditorView) moveSelection(delta int) {
	if v.results == nil || len(v.results.Rows) == 0 {
		return
	}

	v.selectedRow += delta
	if v.selectedRow < 0 {
		v.selectedRow = 0
	}
	maxRow := len(v.results.Rows) - 1
	if v.selectedRow > maxRow {
		v.selectedRow = maxRow
	}

	v.ensureVisible()
}

// ensureVisible scrolls to make the selected row visible.
func (v *SQLEditorView) ensureVisible() {
	visibleRows := v.resultsHeight() - 3 // Account for header and footer
	if visibleRows < 1 {
		visibleRows = 1
	}

	if v.selectedRow < v.scrollOffset {
		v.scrollOffset = v.selectedRow
	} else if v.selectedRow >= v.scrollOffset+visibleRows {
		v.scrollOffset = v.selectedRow - visibleRows + 1
	}
}

// nextPage advances to the next page of results.
func (v *SQLEditorView) nextPage() {
	if v.results == nil {
		return
	}
	if v.results.HasNextPage() {
		v.results.CurrentPage++
		v.selectedRow = 0
		v.scrollOffset = 0
	}
}

// prevPage goes to the previous page of results.
func (v *SQLEditorView) prevPage() {
	if v.results == nil {
		return
	}
	if v.results.HasPrevPage() {
		v.results.CurrentPage--
		v.selectedRow = 0
		v.scrollOffset = 0
	}
}

// copyCell copies the current cell value to clipboard.
func (v *SQLEditorView) copyCell() tea.Cmd {
	if v.results == nil || len(v.results.Rows) == 0 {
		return nil
	}

	// For now, copy first column of selected row
	// TODO: Track column selection
	row := v.results.Rows[v.selectedRow]
	if len(row) == 0 {
		return nil
	}

	value := row[0]

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return CellCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(value)
		return CellCopiedMsg{Value: value, Error: err}
	}
}

// copyRow copies the entire row to clipboard (tab-separated).
func (v *SQLEditorView) copyRow() tea.Cmd {
	if v.results == nil || len(v.results.Rows) == 0 {
		return nil
	}

	row := v.results.Rows[v.selectedRow]
	values := strings.Join(row, "\t")

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return RowCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(values)
		return RowCopiedMsg{Values: row, Error: err}
	}
}

// growEditor increases the editor portion of the split.
func (v *SQLEditorView) growEditor() {
	v.splitRatio += 0.1
	if v.splitRatio > 0.8 {
		v.splitRatio = 0.8
	}
	v.SetSize(v.width, v.height)
}

// shrinkEditor decreases the editor portion of the split.
func (v *SQLEditorView) shrinkEditor() {
	v.splitRatio -= 0.1
	if v.splitRatio < 0.2 {
		v.splitRatio = 0.2
	}
	v.SetSize(v.width, v.height)
}

// resultsHeight returns the height available for results.
func (v *SQLEditorView) resultsHeight() int {
	editorHeight := int(float64(v.height-4) * v.splitRatio)
	return v.height - editorHeight - 4 // -4 for status and padding
}

// showToast displays a toast message.
func (v *SQLEditorView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// View renders the SQL Editor view.
func (v *SQLEditorView) View() string {
	if v.width == 0 || v.height == 0 {
		return "Initializing..."
	}

	// Help overlay
	if v.showHelp {
		return v.renderHelp()
	}

	var sections []string

	// Editor section
	sections = append(sections, v.renderEditor())

	// Results section
	sections = append(sections, v.renderResults())

	// Status bar
	sections = append(sections, v.renderStatusBar())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderEditor renders the SQL editor textarea.
func (v *SQLEditorView) renderEditor() string {
	editorHeight := int(float64(v.height-4) * v.splitRatio)

	// Title bar
	title := "SQL Editor"
	if v.focus == FocusEditor {
		title = styles.AccentStyle.Render("● ") + title
	} else {
		title = "  " + title
	}

	// Transaction indicator
	if v.executor != nil && v.executor.IsInTransaction() {
		txState := v.executor.TransactionState()
		if txState.StateType == TxAborted {
			title += " " + styles.TransactionAbortedBadgeStyle.Render("TX ABORTED")
		} else {
			title += " " + styles.TransactionBadgeStyle.Render("TX")
		}
	}

	titleBar := styles.TitleStyle.Render(title)

	// Textarea
	editor := v.textarea.View()

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left, titleBar, editor)

	return lipgloss.NewStyle().
		Height(editorHeight).
		MaxHeight(editorHeight).
		Render(content)
}

// renderResults renders the query results table.
func (v *SQLEditorView) renderResults() string {
	resultsHeight := v.resultsHeight()

	// Title bar
	title := "Results"
	if v.focus == FocusResults {
		title = styles.AccentStyle.Render("● ") + title
	} else {
		title = "  " + title
	}

	if v.executing {
		elapsed := time.Since(v.startTime)
		title += fmt.Sprintf(" - Executing... (%s)", elapsed.Truncate(time.Millisecond))
	} else if v.results != nil && v.results.TotalRows > 0 {
		title += fmt.Sprintf(" - %d rows (%dms)", v.results.TotalRows, v.results.ExecutionMs)
	}

	titleBar := styles.TitleStyle.Render(title)

	// Content
	var content string
	if v.executing {
		content = styles.ExecutingStyle.Render("Executing query...")
	} else if v.lastError != nil {
		content = styles.ErrorStyle.Render(v.lastError.Error())
	} else if v.results == nil || v.results.TotalRows == 0 {
		content = styles.MutedStyle.Render("No results. Execute a query with Ctrl+Enter.")
	} else {
		content = v.renderResultsTable()
	}

	// Pagination footer
	var footer string
	if v.results != nil && v.results.TotalPages() > 1 {
		footer = styles.PaginationStyle.Render(
			fmt.Sprintf("Page %d/%d (n/p to navigate)",
				v.results.CurrentPage, v.results.TotalPages()))
	}

	// Combine
	result := lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
	if footer != "" {
		result = lipgloss.JoinVertical(lipgloss.Left, result, footer)
	}

	return lipgloss.NewStyle().
		Height(resultsHeight).
		MaxHeight(resultsHeight).
		Render(result)
}

// renderResultsTable renders the results as a table.
func (v *SQLEditorView) renderResultsTable() string {
	if v.results == nil || len(v.results.Columns) == 0 {
		return ""
	}

	// Calculate column widths
	colWidths := make([]int, len(v.results.Columns))
	maxColWidth := (v.width - 4) / len(v.results.Columns)
	if maxColWidth < 10 {
		maxColWidth = 10
	}

	for i, col := range v.results.Columns {
		colWidths[i] = len(col.Name)
	}

	for _, row := range v.results.Rows {
		for i, val := range row {
			if i < len(colWidths) && len(val) > colWidths[i] {
				colWidths[i] = len(val)
			}
		}
	}

	// Cap column widths
	for i := range colWidths {
		if colWidths[i] > maxColWidth {
			colWidths[i] = maxColWidth
		}
	}

	var lines []string

	// Header
	var headerParts []string
	for i, col := range v.results.Columns {
		headerParts = append(headerParts, padOrTruncate(col.Name, colWidths[i]))
	}
	header := styles.ResultsHeaderStyle.Render(strings.Join(headerParts, " │ "))
	lines = append(lines, header)

	// Separator
	var sepParts []string
	for _, w := range colWidths {
		sepParts = append(sepParts, strings.Repeat("─", w))
	}
	lines = append(lines, styles.BorderStyle.Render(strings.Join(sepParts, "─┼─")))

	// Rows
	visibleRows := v.resultsHeight() - 5 // Account for header, separator, footer
	if visibleRows < 1 {
		visibleRows = 1
	}

	startRow := v.scrollOffset
	endRow := startRow + visibleRows
	if endRow > len(v.results.Rows) {
		endRow = len(v.results.Rows)
	}

	for i := startRow; i < endRow; i++ {
		row := v.results.Rows[i]
		var rowParts []string

		for j, val := range row {
			if j >= len(colWidths) {
				break
			}
			cellVal := padOrTruncate(val, colWidths[j])
			if val == NullDisplayValue {
				cellVal = styles.ResultsNullStyle.Render(cellVal)
			}
			rowParts = append(rowParts, cellVal)
		}

		rowStr := strings.Join(rowParts, " │ ")
		if i == v.selectedRow {
			rowStr = styles.ResultsRowSelectedStyle.Render(rowStr)
		}
		lines = append(lines, rowStr)
	}

	return strings.Join(lines, "\n")
}

// renderStatusBar renders the bottom status bar.
func (v *SQLEditorView) renderStatusBar() string {
	var parts []string

	// Connection info
	if v.connectionInfo != "" {
		parts = append(parts, styles.MutedStyle.Render(v.connectionInfo))
	}

	// Read-only indicator
	if v.readOnly {
		parts = append(parts, styles.WarningStyle.Render("[READ-ONLY]"))
	}

	// Toast message
	if v.toastMessage != "" && time.Since(v.toastTime) < 5*time.Second {
		if v.toastError {
			parts = append(parts, styles.ErrorStyle.Render(v.toastMessage))
		} else {
			parts = append(parts, styles.SuccessStyle.Render(v.toastMessage))
		}
	}

	// Key hints
	hints := "Ctrl+Enter: Execute | Tab: Switch Pane | ?: Help"
	parts = append(parts, styles.MutedStyle.Render(hints))

	return strings.Join(parts, " │ ")
}

// renderHelp renders the help overlay.
func (v *SQLEditorView) renderHelp() string {
	helpText := `SQL Editor Help

EXECUTION
  Ctrl+Enter   Execute query
  Esc          Cancel running query

NAVIGATION
  Tab          Switch between editor and results
  Ctrl+Up      Grow editor pane
  Ctrl+Down    Shrink editor pane

RESULTS (when focused)
  j/k          Move selection down/up
  g/G          Go to first/last row
  Ctrl+d/u     Page down/up
  n/p          Next/previous page
  y            Copy cell value
  Y            Copy entire row

GENERAL
  ?            Toggle this help
  q/Esc        Close help

Press ? or Esc to close this help.`

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(helpText)
}

// padOrTruncate pads or truncates a string to the given width.
func padOrTruncate(s string, width int) string {
	if len(s) > width {
		if width > 3 {
			return s[:width-3] + "..."
		}
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
