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
	lastErrorInfo *PgErrorInfo

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

	// Audit log
	auditLog []*QueryAuditEntry
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
		auditLog: make([]*QueryAuditEntry, 0, MaxAuditEntries),
	}
}

// GetAuditLog returns the query audit log for external access.
func (v *SQLEditorView) GetAuditLog() []*QueryAuditEntry {
	return v.auditLog
}

// GetLastAuditEntries returns the last n audit entries.
func (v *SQLEditorView) GetLastAuditEntries(n int) []*QueryAuditEntry {
	if n <= 0 || len(v.auditLog) == 0 {
		return nil
	}
	if n > len(v.auditLog) {
		n = len(v.auditLog)
	}
	return v.auditLog[len(v.auditLog)-n:]
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
	// Set up query audit logging
	v.executor.SetLogFunc(v.logQuery)
}

// logQuery logs query execution for audit purposes.
func (v *SQLEditorView) logQuery(sql string, duration time.Duration, rowCount int64, err error) {
	entry := &QueryAuditEntry{
		SQL:        sql,
		ExecutedAt: time.Now(),
		Duration:   duration,
		RowCount:   rowCount,
	}
	if err != nil {
		entry.Error = err.Error()
		entry.Success = false
	} else {
		entry.Success = true
	}

	// Add to audit log (in-memory for now)
	v.auditLog = append(v.auditLog, entry)

	// Keep last MaxAuditEntries
	if len(v.auditLog) > MaxAuditEntries {
		v.auditLog = v.auditLog[len(v.auditLog)-MaxAuditEntries:]
	}
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
// For SQL Editor:
// - When focus is on editor, we're typing SQL (true)
// - When focus is on results, number keys can switch views (false)
// - Special modes like command line also consume keys (true)
func (v *SQLEditorView) IsInputMode() bool {
	switch v.mode {
	case ModeHelp:
		return false
	case ModeCommandLine, ModeHistorySearch, ModeSnippetBrowser:
		return true
	case ModeExecuting:
		return true // Block keys during execution
	default:
		// In normal mode, only consume keys when editor has focus
		return v.focus == FocusEditor
	}
}

// Update handles messages for the SQL Editor view.
func (v *SQLEditorView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Track if we handled a key to prevent textarea from consuming it
	var keyHandled bool

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// F5 executes query (standard SQL editor shortcut)
		// Intercept BEFORE textarea to prevent any key consumption
		if msg.Type == tea.KeyF5 {
			cmd := v.executeQuery()
			// Return command directly - must not be batched or it won't run
			return v, cmd
		}

		cmd, handled := v.handleKeyPress(msg)
		keyHandled = handled
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

		if msg.Result == nil {
			v.showToast("Query completed but result is nil", true)
			return v, nil
		}

		if msg.Result.Error != nil {
			v.lastError = msg.Result.Error
			v.lastErrorInfo = msg.Result.ErrorInfo
			// Show short toast, detailed error shown in results pane
			v.showToast("Query failed - see error below", true)
		} else if msg.Result.Message != "" {
			v.showToast(msg.Result.Message, false)
			v.lastError = nil
			v.lastErrorInfo = nil
		} else {
			v.lastError = nil
			v.lastErrorInfo = nil
		}

		// Convert results to display format (even for 0 rows, to show column headers)
		if msg.Result.Columns != nil || msg.Result.Rows != nil {
			rows := msg.Result.Rows
			if rows == nil {
				rows = [][]any{} // Empty slice instead of nil
			}
			v.results = &ResultSet{
				Columns:     msg.Result.Columns,
				Rows:        FormatResultSet(rows),
				TotalRows:   len(rows),
				CurrentPage: 1,
				PageSize:    DefaultPageSize,
				ExecutionMs: msg.Result.Duration.Milliseconds(),
			}
			v.selectedRow = 0
			v.scrollOffset = 0
			// Focus results pane when rows are returned
			if len(rows) > 0 {
				v.focus = FocusResults
				v.textarea.Blur()
			}
			// Show success toast with row count
			v.showToast(fmt.Sprintf("Query OK: %d rows (%dms)", len(rows), msg.Result.Duration.Milliseconds()), false)
		} else if msg.Result.RowsAffected > 0 {
			// For INSERT/UPDATE/DELETE without returning rows
			v.showToast(fmt.Sprintf("Query OK: %d rows affected (%dms)", msg.Result.RowsAffected, msg.Result.Duration.Milliseconds()), false)
		} else if msg.Result.Error == nil && msg.Result.Message == "" {
			// Query completed but returned no rows (DDL like CREATE TABLE)
			v.showToast(fmt.Sprintf("Query OK (%dms)", msg.Result.Duration.Milliseconds()), false)
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

	case tea.MouseMsg:
		v.handleMouseMsg(msg)

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

	// Update textarea if editor has focus and key wasn't already handled
	if v.focus == FocusEditor && v.mode == ModeNormal && !keyHandled {
		var cmd tea.Cmd
		v.textarea, cmd = v.textarea.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return v, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input.
// Returns (cmd, handled) where handled indicates the key was consumed and shouldn't be passed to textarea.
func (v *SQLEditorView) handleKeyPress(msg tea.KeyMsg) (tea.Cmd, bool) {
	key := msg.String()

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "esc", "q":
			v.mode = ModeNormal
			v.showHelp = false
		case "j", "down":
			v.helpScroll++
		case "k", "up":
			if v.helpScroll > 0 {
				v.helpScroll--
			}
		}
		return nil, true // All keys in help mode are handled
	}

	// Handle executing mode - only allow cancel
	if v.mode == ModeExecuting {
		if key == "esc" {
			if v.executor != nil {
				v.executor.CancelQuery()
			}
			return func() tea.Msg { return QueryCancelledMsg{} }, true
		}
		return nil, true // Block all keys during execution except esc
	}

	// Esc exits editor focus (allows 'q' to quit, number keys to switch views)
	if key == "esc" {
		if v.focus == FocusEditor {
			v.focus = FocusResults
			v.textarea.Blur()
			return nil, true
		}
		// If already on results, let it pass through (does nothing)
		return nil, false
	}

	// Enter key on results focuses editor
	if key == "enter" && v.focus == FocusResults {
		v.focus = FocusEditor
		v.textarea.Focus()
		return nil, true
	}

	// 'h' for help (only when not in editor focus, to allow typing 'h')
	if key == "h" && v.focus == FocusResults {
		v.mode = ModeHelp
		v.showHelp = true
		v.helpScroll = 0
		return nil, true
	}

	// Focus-specific keys when results pane has focus
	if v.focus == FocusResults {
		// +/- to resize panes (only in results mode so users can type in editor)
		if key == "+" || key == "=" {
			v.growEditor()
			return nil, true
		}
		if key == "-" || key == "_" {
			v.shrinkEditor()
			return nil, true
		}
		// Let number keys pass through to app for view switching
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			return nil, false
		}
		// Let 'q' pass through to app for quitting
		if key == "q" {
			return nil, false
		}
		return v.handleResultsKeys(key), true
	}

	// Key not handled - let textarea process it
	return nil, false
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

// handleMouseMsg handles mouse events for scrolling and clicking.
func (v *SQLEditorView) handleMouseMsg(msg tea.MouseMsg) {
	// Don't handle mouse during execution or help mode
	if v.mode == ModeExecuting || v.mode == ModeHelp {
		return
	}

	// Calculate where the results table data starts
	editorHeight := int(float64(v.height-5) * v.splitRatio)
	resultsDataStartY := 9 + editorHeight

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		// Scroll results up
		if v.results != nil && v.results.TotalRows > 0 {
			v.moveSelection(-1)
		}

	case tea.MouseButtonWheelDown:
		// Scroll results down
		if v.results != nil && v.results.TotalRows > 0 {
			v.moveSelection(1)
		}

	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			// Check if click is in results area
			if msg.Y >= resultsDataStartY && v.results != nil && v.results.TotalRows > 0 {
				// Calculate which row was clicked
				clickedRow := msg.Y - resultsDataStartY + v.scrollOffset
				if clickedRow >= 0 && clickedRow < len(v.results.Rows) {
					v.selectedRow = clickedRow
					v.ensureVisible()
					// Switch focus to results when clicking in results area
					if v.focus == FocusEditor {
						v.focus = FocusResults
						v.textarea.Blur()
					}
				}
			} else if msg.Y < resultsDataStartY-3 && msg.Y > 4 {
				// Click in editor area - switch focus to editor
				if v.focus == FocusResults {
					v.focus = FocusEditor
					v.textarea.Focus()
				}
			}
		}
	}
}

// executeQuery executes the current query.
func (v *SQLEditorView) executeQuery() tea.Cmd {
	sql := strings.TrimSpace(v.textarea.Value())
	if sql == "" {
		v.showToast("No query to execute", true)
		return nil
	}

	if v.executor == nil {
		v.showToast("No database connection - executor is nil", true)
		return nil
	}

	if v.executor.pool == nil {
		v.showToast("No database connection - pool is nil", true)
		return nil
	}

	v.mode = ModeExecuting
	v.executing = true
	v.startTime = time.Now()
	v.executedQuery = sql
	v.lastError = nil
	v.lastErrorInfo = nil

	// Capture executor reference for the goroutine
	executor := v.executor

	return func() tea.Msg {
		// Actually execute the query
		result, err := executor.ExecuteQuery(
			context.Background(),
			sql,
			DefaultQueryTimeout,
		)
		if err != nil {
			return QueryCompletedMsg{Result: &ExecutionResult{Error: err}}
		}
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

// visibleResultRows returns the number of data rows that can be displayed.
// This accounts for: title bar (1), header row (1), separator (1), pagination footer (1), margin (1)
func (v *SQLEditorView) visibleResultRows() int {
	visible := v.resultsHeight() - 5
	if visible < 1 {
		visible = 1
	}
	return visible
}

// ensureVisible scrolls to make the selected row visible.
func (v *SQLEditorView) ensureVisible() {
	visibleRows := v.visibleResultRows()

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
	editorHeight := int(float64(v.height-5) * v.splitRatio)
	return v.height - editorHeight - 5 // -5 for connection bar, footer, and padding
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

	// Connection info at the top (below app title bar)
	sections = append(sections, v.renderConnectionBar())

	// Editor section
	sections = append(sections, v.renderEditor())

	// Results section
	sections = append(sections, v.renderResults())

	// Footer with key hints
	sections = append(sections, v.renderFooter())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderEditor renders the SQL editor textarea.
func (v *SQLEditorView) renderEditor() string {
	editorHeight := int(float64(v.height-5) * v.splitRatio) // -5 for connection bar and footer

	// Title bar
	title := "SQL Editor"
	if v.focus == FocusEditor {
		title = styles.AccentStyle.Render("● ") + title
	} else {
		title = "  " + title
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
		// Use enhanced error formatting with position info
		content = v.renderError()
	} else if v.results == nil || v.results.TotalRows == 0 {
		if v.executedQuery != "" {
			content = styles.MutedStyle.Render("Query returned 0 rows.")
		} else {
			content = styles.MutedStyle.Render("No results. Execute a query with F5.")
		}
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

	numCols := len(v.results.Columns)
	// Available width: total width minus borders and separators
	// Each column has " │ " separator (3 chars) except the last one
	separatorWidth := (numCols - 1) * 3
	availableWidth := v.width - 4 - separatorWidth // -4 for padding

	// Calculate minimum column widths based on content
	colWidths := make([]int, numCols)
	for i, col := range v.results.Columns {
		colWidths[i] = len(col.Name)
		if colWidths[i] < 3 {
			colWidths[i] = 3 // Minimum width
		}
	}

	for _, row := range v.results.Rows {
		for i, val := range row {
			if i < len(colWidths) && len(val) > colWidths[i] {
				colWidths[i] = len(val)
			}
		}
	}

	// Calculate total content width and distribute extra space
	totalContentWidth := 0
	for _, w := range colWidths {
		totalContentWidth += w
	}

	// If we have extra space, distribute it proportionally
	if totalContentWidth < availableWidth {
		extraSpace := availableWidth - totalContentWidth
		extraPerCol := extraSpace / numCols
		remainder := extraSpace % numCols
		for i := range colWidths {
			colWidths[i] += extraPerCol
			if i < remainder {
				colWidths[i]++ // Distribute remainder to first columns
			}
		}
	} else {
		// If content is wider than available, cap columns proportionally
		maxColWidth := availableWidth / numCols
		if maxColWidth < 10 {
			maxColWidth = 10
		}
		for i := range colWidths {
			if colWidths[i] > maxColWidth {
				colWidths[i] = maxColWidth
			}
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

	// Rows - use consistent visible rows calculation
	visibleRows := v.visibleResultRows()

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

// renderError renders the error message with position info.
func (v *SQLEditorView) renderError() string {
	if v.lastError == nil {
		return ""
	}

	var lines []string

	// If we have detailed error info, format it nicely
	if v.lastErrorInfo != nil {
		// Error header with severity and code
		header := v.lastErrorInfo.Severity
		if v.lastErrorInfo.Code != "" {
			header += fmt.Sprintf(" [%s]", v.lastErrorInfo.Code)
		}
		lines = append(lines, styles.ErrorStyle.Render(header))

		// Main error message
		lines = append(lines, styles.ErrorStyle.Render(v.lastErrorInfo.Message))

		// Position information
		if v.lastErrorInfo.Position > 0 && v.executedQuery != "" {
			line, col := positionToLineCol(v.executedQuery, v.lastErrorInfo.Position)
			lines = append(lines, styles.MutedStyle.Render(fmt.Sprintf("At line %d, column %d", line, col)))

			// Show the problematic line with an indicator
			lineText := getLineAtPosition(v.executedQuery, v.lastErrorInfo.Position)
			if lineText != "" {
				lines = append(lines, "")
				lines = append(lines, styles.MutedStyle.Render(lineText))
				// Add caret at the error position within the line
				offset := v.lastErrorInfo.Position - getLineStartOffset(v.executedQuery, v.lastErrorInfo.Position)
				if offset > 0 && offset <= len(lineText) {
					caret := strings.Repeat(" ", offset-1) + "^"
					lines = append(lines, styles.ErrorStyle.Render(caret))
				}
			}
		}

		// Detail message
		if v.lastErrorInfo.Detail != "" {
			lines = append(lines, "")
			lines = append(lines, styles.MutedStyle.Render("Detail: "+v.lastErrorInfo.Detail))
		}

		// Hint message
		if v.lastErrorInfo.Hint != "" {
			lines = append(lines, styles.SuccessStyle.Render("Hint: "+v.lastErrorInfo.Hint))
		}

		// Table/column/constraint info
		if v.lastErrorInfo.TableName != "" {
			info := "Table: " + v.lastErrorInfo.TableName
			if v.lastErrorInfo.SchemaName != "" {
				info = "Table: " + v.lastErrorInfo.SchemaName + "." + v.lastErrorInfo.TableName
			}
			lines = append(lines, styles.MutedStyle.Render(info))
		}
		if v.lastErrorInfo.ColumnName != "" {
			lines = append(lines, styles.MutedStyle.Render("Column: "+v.lastErrorInfo.ColumnName))
		}
		if v.lastErrorInfo.ConstraintName != "" {
			lines = append(lines, styles.MutedStyle.Render("Constraint: "+v.lastErrorInfo.ConstraintName))
		}

		// Context
		if v.lastErrorInfo.Where != "" {
			lines = append(lines, styles.MutedStyle.Render("Context: "+v.lastErrorInfo.Where))
		}
	} else {
		// Simple error without detailed info
		lines = append(lines, styles.ErrorStyle.Render(v.lastError.Error()))
	}

	return strings.Join(lines, "\n")
}

// renderConnectionBar renders the connection info bar at the top (matches other views).
func (v *SQLEditorView) renderConnectionBar() string {
	// Connection info title
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Build right-side indicators
	var indicators []string

	// Transaction indicator
	if v.executor != nil && v.executor.IsInTransaction() {
		txState := v.executor.TransactionState()
		if txState.StateType == TxAborted {
			indicators = append(indicators, styles.TransactionAbortedBadgeStyle.Render("TX ABORTED"))
		} else {
			indicators = append(indicators, styles.TransactionBadgeStyle.Render("TX"))
		}
	}

	// Read-only indicator
	if v.readOnly {
		indicators = append(indicators, styles.WarningStyle.Render("[READ-ONLY]"))
	}

	// Calculate spacing
	rightContent := strings.Join(indicators, " ")
	titleLen := lipgloss.Width(title)
	rightLen := lipgloss.Width(rightContent)
	gap := v.width - 2 - titleLen - rightLen
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + spaces + rightContent)
}

// renderFooter renders the bottom footer with key hints and toast messages.
func (v *SQLEditorView) renderFooter() string {
	var parts []string

	// Toast message (shows for 5 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 5*time.Second {
		if v.toastError {
			parts = append(parts, styles.ErrorStyle.Render(v.toastMessage))
		} else {
			parts = append(parts, styles.SuccessStyle.Render(v.toastMessage))
		}
	}

	// Key hints based on focus
	var hints string
	if v.focus == FocusEditor {
		hints = "F5: Execute │ Esc: Exit editor"
	} else {
		hints = "Enter: Edit │ j/k: Navigate │ +/-: Resize │ h: Help │ q: Quit"
	}
	parts = append(parts, styles.MutedStyle.Render(hints))

	return strings.Join(parts, " │ ")
}

// renderHelp renders the help overlay.
func (v *SQLEditorView) renderHelp() string {
	helpText := `SQL Editor Help

EDITOR MODE (● indicator shows focus)
  F5           Execute query
  Esc          Exit editor → results mode

RESULTS MODE (allows view switching and quit)
  Enter        Return to editor mode
  j/k          Move selection down/up
  g/G          Go to first/last row
  Ctrl+d/u     Page down/up
  n/p          Next/previous page
  y            Copy cell value
  Y            Copy entire row
  +/-          Resize editor/results split
  1-7          Switch views
  q            Quit application

DURING EXECUTION
  Esc          Cancel running query

Press h, q, or Esc to close this help.`

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
