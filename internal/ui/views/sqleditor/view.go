// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui/components/vimtea"

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

	// Editor (vimtea with syntax highlighting)
	editor       vimtea.Editor
	editorHeight int

	// Results
	results         *ResultSet
	selectedRow     int
	scrollOffset    int // Vertical scroll (row offset)
	colScrollOffset int // Horizontal scroll (column offset)
	executedQuery   string
	lastError       error
	lastErrorInfo   *PgErrorInfo

	// Execution
	executor   *SessionExecutor
	executing  bool
	startTime  time.Time
	lastUpdate time.Time

	// Key bindings
	keys KeyMap

	// UI state
	splitRatio float64 // 0.0-1.0, portion for editor
	showHelp   bool
	helpScroll int

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Clipboard
	clipboard *ui.ClipboardWriter

	// Audit log
	auditLog []*QueryAuditEntry

	// History
	history          *HistoryManager
	historyBrowsing  bool   // Whether currently browsing history
	savedEditorState string // Editor content before browsing history

	// Search overlay (Ctrl+R)
	searchMode   bool
	searchQuery  string
	searchResult []HistoryEntry
	searchIndex  int

	// Snippets
	snippets           *SnippetManager
	snippetBrowsing    bool
	snippetList        []Snippet
	snippetIndex       int
	snippetSearchQuery string
	pendingSaveName    string // For overwrite confirmation
}

// NewSQLEditorView creates a new SQL Editor view.
// syntaxTheme is the Chroma theme for SQL highlighting (e.g., "monokai", "dracula", "nord").
func NewSQLEditorView(syntaxTheme string) *SQLEditorView {
	if syntaxTheme == "" {
		syntaxTheme = "monokai"
	}
	// Create vimtea editor with SQL syntax highlighting
	editor := vimtea.NewEditor(
		vimtea.WithFileName("query.sql"),           // Enables SQL syntax highlighting
		vimtea.WithDefaultSyntaxTheme(syntaxTheme), // Configurable theme
		vimtea.WithEnableStatusBar(true),         // Shows mode and :command line
		vimtea.WithEnableModeCommand(true),       // Enable :commands
		vimtea.WithRelativeNumbers(false),        // Standard line numbers
		vimtea.WithContent(""),                   // Start empty
		vimtea.WithTextStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("252"))),
		vimtea.WithLineNumberStyle(styles.EditorLineNumberStyle.PaddingRight(1)), // Add space after line number
		vimtea.WithCurrentLineNumberStyle(styles.EditorLineNumberStyle.Foreground(styles.ColorAccent).PaddingRight(1)),
		vimtea.WithCursorStyle(lipgloss.NewStyle().
			Background(styles.ColorAccent).
			Foreground(lipgloss.Color("0"))), // Black text on cyan cursor for contrast
		vimtea.WithStatusStyle(lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Bold(true)),
		vimtea.WithCommandStyle(lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252"))),
	)

	v := &SQLEditorView{
		mode:       ModeNormal,
		focus:      FocusEditor,
		editor:     editor,
		keys:       DefaultKeyMap(),
		splitRatio: 0.4, // 40% editor, 60% results
		clipboard:  ui.NewClipboardWriter(),
		results: &ResultSet{
			PageSize:    DefaultPageSize,
			CurrentPage: 1,
		},
		auditLog: make([]*QueryAuditEntry, 0, MaxAuditEntries),
		// history initialized via SetDatabase() after connection
	}

	// Add F5 binding for query execution
	v.editor.AddBinding(vimtea.KeyBinding{
		Key:         "f5",
		Mode:        vimtea.ModeNormal,
		Description: "Execute query",
		Handler: func(buf vimtea.Buffer) tea.Cmd {
			return v.executeQueryCmd()
		},
	})
	v.editor.AddBinding(vimtea.KeyBinding{
		Key:         "f5",
		Mode:        vimtea.ModeInsert,
		Description: "Execute query",
		Handler: func(buf vimtea.Buffer) tea.Cmd {
			return v.executeQueryCmd()
		},
	})

	// Add Ctrl+Enter binding for query execution (alternative)
	v.editor.AddBinding(vimtea.KeyBinding{
		Key:         "ctrl+enter",
		Mode:        vimtea.ModeInsert,
		Description: "Execute query",
		Handler: func(buf vimtea.Buffer) tea.Cmd {
			return v.executeQueryCmd()
		},
	})

	// Add Esc in results to return to editor
	v.editor.AddBinding(vimtea.KeyBinding{
		Key:         "tab",
		Mode:        vimtea.ModeNormal,
		Description: "Switch to results",
		Handler: func(buf vimtea.Buffer) tea.Cmd {
			v.focus = FocusResults
			return nil
		},
	})

	// Add custom commands for SQL execution
	v.editor.AddCommand("exec", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.executeQueryCmd()
	})
	v.editor.AddCommand("run", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.executeQueryCmd()
	})
	v.editor.AddCommand("clear", func(buf vimtea.Buffer, args []string) tea.Cmd {
		v.clearEditorAndResults()
		return nil
	})

	// Snippet commands
	v.editor.AddCommand("save", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.saveSnippetCmd(args)
	})
	v.editor.AddCommand("save!", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.saveSnippetForceCmd(args)
	})
	v.editor.AddCommand("load", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.loadSnippetCmd(args)
	})
	v.editor.AddCommand("snippets", func(buf vimtea.Buffer, args []string) tea.Cmd {
		v.openSnippetBrowser()
		return nil
	})
	v.editor.AddCommand("delete", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.deleteSnippetCmd(args)
	})

	// Initialize snippet manager
	if sm, err := NewSnippetManager(); err == nil {
		v.snippets = sm
	}

	return v
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
	return v.editor.Init()
}

// SetSize sets the dimensions of the view.
func (v *SQLEditorView) SetSize(width, height int) {
	v.width = width
	v.height = height

	// Calculate editor and results heights
	v.editorHeight = int(float64(height-4) * v.splitRatio) // -4 for status bar and padding
	if v.editorHeight < 7 {
		v.editorHeight = 7
	}

	// Set vimtea editor dimensions (subtract 1 for our title bar)
	// vimtea internally reserves 2 lines for its own status bar when enabled
	v.editor.SetSize(width-2, v.editorHeight-1)
}

// SetPool sets the database connection pool and creates executor.
func (v *SQLEditorView) SetPool(pool *pgxpool.Pool) {
	v.executor = NewSessionExecutor(pool, v.readOnly)
	// Set up query audit logging
	v.executor.SetLogFunc(v.logQuery)
}

// SetDatabase initializes the history manager with the shared SQLite database.
func (v *SQLEditorView) SetDatabase(db *sqlite.DB) {
	v.history = NewHistoryManager(db, MaxHistoryEntries)
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

// clearEditorAndResults clears both the editor content and query results.
func (v *SQLEditorView) clearEditorAndResults() {
	// Clear editor content
	v.editor.SetContent("")

	// Clear results
	v.results = nil
	v.selectedRow = 0
	v.scrollOffset = 0
	v.colScrollOffset = 0
	v.executedQuery = ""
	v.lastError = nil
	v.lastErrorInfo = nil

	// Reset history navigation
	if v.history != nil {
		v.history.ResetNavigation()
	}
	v.historyBrowsing = false
}

// IsInputMode returns true when the view is in a mode that should consume keys.
// For SQL Editor:
// - When focus is on editor and in insert/command mode, consume all keys
// - When focus is on results, allow view switching (number keys)
// - During query execution, block keys
func (v *SQLEditorView) IsInputMode() bool {
	switch v.mode {
	case ModeHelp:
		return false
	case ModeExecuting:
		return true // Block keys during execution
	default:
		// When editor has focus and in insert or command mode, consume keys
		if v.focus == FocusEditor {
			editorMode := v.editor.GetMode()
			return editorMode == vimtea.ModeInsert || editorMode == vimtea.ModeCommand
		}
		return false
	}
}

// Update handles messages for the SQL Editor view.
func (v *SQLEditorView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle our custom modes first
		if cmd := v.handleKeyPress(msg); cmd != nil {
			cmds = append(cmds, cmd)
			return v, tea.Batch(cmds...)
		}

		// Pass keys to editor when it has focus
		if v.focus == FocusEditor && v.mode == ModeNormal {
			newModel, cmd := v.editor.Update(msg)
			if editor, ok := newModel.(vimtea.Editor); ok {
				v.editor = editor
			}
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
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

		// Switch editor to NORMAL mode for quick \ access to results
		cmds = append(cmds, v.editor.SetMode(vimtea.ModeNormal))

		// Reset history browsing state
		v.historyBrowsing = false
		v.savedEditorState = ""
		if v.history != nil {
			v.history.ResetNavigation()
		}

		// Add to history
		if v.history != nil && v.executedQuery != "" {
			var errMsg string
			if msg.Result != nil && msg.Result.Error != nil {
				errMsg = msg.Result.Error.Error()
			}
			var rowCount int64
			if msg.Result != nil {
				if len(msg.Result.Rows) > 0 {
					rowCount = int64(len(msg.Result.Rows))
				} else {
					rowCount = msg.Result.RowsAffected
				}
			}
			var durationMs int64
			if msg.Result != nil {
				durationMs = msg.Result.Duration.Milliseconds()
			}
			v.history.Add(v.executedQuery, durationMs, rowCount, errMsg)
		}

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
				RawRows:     rows,
				TotalRows:   len(rows),
				CurrentPage: 1,
				PageSize:    DefaultPageSize,
				ExecutionMs: msg.Result.Duration.Milliseconds(),
				SortColumn:  -1, // No sort initially
				SortAsc:     true,
			}
			v.selectedRow = 0
			v.scrollOffset = 0
			v.colScrollOffset = 0
			// Keep focus on editor - user can Tab to results
			// Show success toast with row count (or warning if DDL in transaction)
			if msg.Result.Warning != "" {
				v.showToast(msg.Result.Warning, true)
			} else {
				v.showToast(fmt.Sprintf("Query OK: %d rows (%dms)", len(rows), msg.Result.Duration.Milliseconds()), false)
			}
		} else if msg.Result.RowsAffected > 0 {
			// For INSERT/UPDATE/DELETE without returning rows
			if msg.Result.Warning != "" {
				v.showToast(msg.Result.Warning, true)
			} else {
				v.showToast(fmt.Sprintf("Query OK: %d rows affected (%dms)", msg.Result.RowsAffected, msg.Result.Duration.Milliseconds()), false)
			}
		} else if msg.Result.Error == nil && msg.Result.Message == "" {
			// Query completed but returned no rows (DDL like CREATE TABLE)
			if msg.Result.Warning != "" {
				v.showToast(msg.Result.Warning, true)
			} else {
				v.showToast(fmt.Sprintf("Query OK (%dms)", msg.Result.Duration.Milliseconds()), false)
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

	case vimtea.CommandMsg:
		// Pass to vimtea to execute the registered command
		newModel, cmd := v.editor.Update(msg)
		if editor, ok := newModel.(vimtea.Editor); ok {
			v.editor = editor
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case vimtea.EditorModeMsg, vimtea.UndoRedoMsg:
		// Pass vimtea internal messages directly to editor
		newModel, cmd := v.editor.Update(msg)
		if editor, ok := newModel.(vimtea.Editor); ok {
			v.editor = editor
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Always pass timing messages to editor (cursor blink, yank highlight timeout)
		// regardless of focus, so animations continue to work
		newModel, cmd := v.editor.Update(msg)
		if editor, ok := newModel.(vimtea.Editor); ok {
			v.editor = editor
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return v, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input for custom modes.
// Returns a tea.Cmd if the key was handled, nil otherwise (let vimtea handle it).
func (v *SQLEditorView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
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
		return nil // Handled, but no command
	}

	// Handle executing mode - only allow cancel
	if v.mode == ModeExecuting {
		if key == "esc" {
			if v.executor != nil {
				v.executor.CancelQuery()
			}
			return func() tea.Msg { return QueryCancelledMsg{} }
		}
		return nil // Block all keys during execution except esc
	}

	// Handle search mode (Ctrl+R)
	if v.searchMode {
		return v.handleSearchInput(key)
	}

	// Handle snippet browser mode
	if v.snippetBrowsing {
		return v.handleSnippetBrowserInput(key)
	}

	// Ctrl+R to enter reverse search mode (editor must be focused and in normal mode)
	if key == "ctrl+r" && v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal {
		v.searchMode = true
		v.searchQuery = ""
		v.searchResult = v.history.Search("")
		v.searchIndex = 0
		return nil
	}

	// Ctrl+O to open snippet browser (editor must be focused and in normal mode)
	if key == "ctrl+o" && v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal {
		v.openSnippetBrowser()
		return nil
	}

	// History navigation with Up/Down arrows
	// Start browsing: cursor must be at (0,0) to initiate
	// Continue browsing: once in history mode, up/down navigate regardless of cursor
	if v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal && v.history != nil {
		row, col := v.editor.GetCursorPosition()
		atStart := row == 0 && col == 0

		if key == "up" && (atStart || v.historyBrowsing) {
			// Navigate to previous history entry
			if !v.historyBrowsing {
				// Save current editor state before browsing
				v.savedEditorState = v.editor.GetBuffer().Text()
				v.historyBrowsing = true
			}
			if sql := v.history.Previous(); sql != "" {
				v.editor.SetContent(sql)
				v.editor.SetCursorPosition(0, 0) // Start at beginning for history
				return nil
			}
			return nil // At beginning of history
		}

		if key == "down" && v.historyBrowsing {
			// Navigate forward in history
			if sql := v.history.Next(); sql != "" {
				v.editor.SetContent(sql)
				v.editor.SetCursorPosition(0, 0) // Start at beginning for history
			} else {
				// Past end of history - restore original content
				v.editor.SetContent(v.savedEditorState)
				v.editor.SetCursorPosition(0, 0)
				v.historyBrowsing = false
				v.savedEditorState = ""
				v.history.ResetNavigation()
			}
			return nil
		}
	}

	// Backslash toggles focus between editor and results (vim leader key style)
	// Works from Results or Editor in normal mode
	if key == "\\" {
		if v.focus == FocusResults {
			v.focus = FocusEditor
			return nil
		}
		if v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal {
			v.focus = FocusResults
			return nil
		}
	}

	// Enter key on results focuses editor and enters insert mode
	if key == "enter" && v.focus == FocusResults {
		v.focus = FocusEditor
		return v.editor.SetMode(vimtea.ModeInsert)
	}

	// 'h' for help (only when results have focus)
	if key == "h" && v.focus == FocusResults {
		v.mode = ModeHelp
		v.showHelp = true
		v.helpScroll = 0
		return nil
	}

	// +/- and Ctrl+Up/Down to resize panes (works in results focus, or editor normal mode)
	if key == "+" || key == "=" || key == "ctrl+down" {
		if v.focus == FocusResults || (v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal) {
			v.growResults()
			return nil
		}
	}
	if key == "-" || key == "_" || key == "ctrl+up" {
		if v.focus == FocusResults || (v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal) {
			v.shrinkResults()
			return nil
		}
	}

	// Focus-specific keys when results pane has focus
	if v.focus == FocusResults {
		// Let number keys pass through to app for view switching
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			return nil
		}
		// Let 'q' pass through to app for quitting
		if key == "q" {
			return nil
		}
		return v.handleResultsKeys(key)
	}

	// Key not handled - let vimtea process it
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
	case "left":
		v.scrollColumnsLeft()
	case "right":
		v.scrollColumnsRight()
	case "s":
		v.cycleSortColumn()
	case "S":
		v.toggleSortDirection()
	case "y":
		return v.copyCell()
	case "Y":
		return v.copyRow()
	}

	return nil
}

// scrollColumnsLeft scrolls the results table one column to the left.
func (v *SQLEditorView) scrollColumnsLeft() {
	if v.colScrollOffset > 0 {
		v.colScrollOffset--
	}
}

// scrollColumnsRight scrolls the results table one column to the right.
func (v *SQLEditorView) scrollColumnsRight() {
	if v.results != nil && v.colScrollOffset < len(v.results.Columns)-1 {
		v.colScrollOffset++
	}
}

// handleMouseMsg handles mouse events for scrolling and clicking.
func (v *SQLEditorView) handleMouseMsg(msg tea.MouseMsg) {
	// Don't handle mouse during execution or help mode
	if v.mode == ModeExecuting || v.mode == ModeHelp {
		return
	}

	// Calculate where the results table data starts
	// Account for: connection bar, editor, results title, column info, header, separator
	editorHeight := int(float64(v.height-5) * v.splitRatio)
	resultsDataStartY := 10 + editorHeight

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if v.results != nil && v.results.TotalRows > 0 {
			if msg.Shift {
				// Shift+scroll = horizontal scroll left
				v.scrollColumnsLeft()
			} else {
				// Normal scroll = vertical
				v.moveSelection(-1)
			}
		}

	case tea.MouseButtonWheelDown:
		if v.results != nil && v.results.TotalRows > 0 {
			if msg.Shift {
				// Shift+scroll = horizontal scroll right
				v.scrollColumnsRight()
			} else {
				// Normal scroll = vertical
				v.moveSelection(1)
			}
		}

	case tea.MouseButtonWheelLeft:
		// Horizontal scroll wheel left
		if v.results != nil && v.results.TotalRows > 0 {
			v.scrollColumnsLeft()
		}

	case tea.MouseButtonWheelRight:
		// Horizontal scroll wheel right
		if v.results != nil && v.results.TotalRows > 0 {
			v.scrollColumnsRight()
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
					v.focus = FocusResults
				}
			} else if msg.Y < resultsDataStartY-3 && msg.Y > 4 {
				// Click in editor area - switch focus to editor
				v.focus = FocusEditor
			}
		}
	}
}

// executeQueryCmd executes the current query (called from vimtea key bindings).
func (v *SQLEditorView) executeQueryCmd() tea.Cmd {
	sql := strings.TrimSpace(v.editor.GetBuffer().Text())
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
	} else {
		v.focus = FocusEditor
	}
}

// pageRowBounds returns the start and end row indices for the current page.
func (v *SQLEditorView) pageRowBounds() (int, int) {
	if v.results == nil {
		return 0, 0
	}
	pageStart := (v.results.CurrentPage - 1) * v.results.PageSize
	pageEnd := pageStart + v.results.PageSize
	if pageEnd > len(v.results.Rows) {
		pageEnd = len(v.results.Rows)
	}
	return pageStart, pageEnd
}

// moveSelection moves the row selection by delta within the current page.
func (v *SQLEditorView) moveSelection(delta int) {
	if v.results == nil || len(v.results.Rows) == 0 {
		return
	}

	pageStart, pageEnd := v.pageRowBounds()

	v.selectedRow += delta
	if v.selectedRow < pageStart {
		v.selectedRow = pageStart
	}
	if v.selectedRow >= pageEnd {
		v.selectedRow = pageEnd - 1
	}

	v.ensureVisible()
}

// visibleResultRows returns the number of data rows that can be displayed.
// This accounts for: title bar (1), header row (1), separator (1), col info (1), pagination footer (1), margin (1)
func (v *SQLEditorView) visibleResultRows() int {
	visible := v.resultsHeight() - 6
	if v.results != nil && v.results.TotalPages() > 1 {
		visible-- // pagination footer takes extra line
	}
	if visible < 1 {
		visible = 1
	}
	return visible
}

// ensureVisible scrolls to make the selected row visible within current page.
func (v *SQLEditorView) ensureVisible() {
	if v.results == nil {
		return
	}

	pageStart, pageEnd := v.pageRowBounds()
	visibleRows := v.visibleResultRows()
	pageRowCount := pageEnd - pageStart

	// scrollOffset is relative to page start
	relativeRow := v.selectedRow - pageStart

	if relativeRow < v.scrollOffset {
		v.scrollOffset = relativeRow
	} else if relativeRow >= v.scrollOffset+visibleRows {
		v.scrollOffset = relativeRow - visibleRows + 1
	}

	// Clamp scrollOffset
	if v.scrollOffset < 0 {
		v.scrollOffset = 0
	}
	maxScroll := pageRowCount - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scrollOffset > maxScroll {
		v.scrollOffset = maxScroll
	}
}

// nextPage advances to the next page of results.
func (v *SQLEditorView) nextPage() {
	if v.results == nil {
		return
	}
	if v.results.HasNextPage() {
		v.results.CurrentPage++
		// Set selectedRow to first row of new page
		v.selectedRow = (v.results.CurrentPage - 1) * v.results.PageSize
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
		// Set selectedRow to first row of new page
		v.selectedRow = (v.results.CurrentPage - 1) * v.results.PageSize
		v.scrollOffset = 0
	}
}

// cycleSortColumn cycles through sort columns (s key).
func (v *SQLEditorView) cycleSortColumn() {
	if v.results == nil || len(v.results.Columns) == 0 {
		return
	}

	v.results.SortColumn++
	if v.results.SortColumn >= len(v.results.Columns) {
		v.results.SortColumn = -1 // Back to no sort
	}

	if v.results.SortColumn >= 0 {
		v.sortResults()
	} else {
		// Restore original order
		v.restoreOriginalOrder()
	}

	// Reset to page 1 and first row
	v.results.CurrentPage = 1
	v.selectedRow = 0
	v.scrollOffset = 0
}

// toggleSortDirection toggles sort direction (S key).
func (v *SQLEditorView) toggleSortDirection() {
	if v.results == nil || v.results.SortColumn < 0 {
		return
	}

	v.results.SortAsc = !v.results.SortAsc
	v.sortResults()

	// Reset to page 1 and first row
	v.results.CurrentPage = 1
	v.selectedRow = 0
	v.scrollOffset = 0
}

// sortResults sorts the results by the current sort column.
func (v *SQLEditorView) sortResults() {
	if v.results == nil || v.results.SortColumn < 0 || len(v.results.RawRows) == 0 {
		return
	}

	col := v.results.SortColumn
	asc := v.results.SortAsc

	// Create index slice to sort both raw and formatted rows together
	n := len(v.results.RawRows)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	// Sort indices based on raw values
	sort.Slice(indices, func(i, j int) bool {
		ai, aj := indices[i], indices[j]
		if col >= len(v.results.RawRows[ai]) || col >= len(v.results.RawRows[aj]) {
			return false
		}
		valA := v.results.RawRows[ai][col]
		valB := v.results.RawRows[aj][col]

		less := compareValues(valA, valB)
		if asc {
			return less
		}
		return !less
	})

	// Reorder both slices
	newRaw := make([][]any, n)
	newFormatted := make([][]string, n)
	for i, idx := range indices {
		newRaw[i] = v.results.RawRows[idx]
		newFormatted[i] = v.results.Rows[idx]
	}
	v.results.RawRows = newRaw
	v.results.Rows = newFormatted
}

// restoreOriginalOrder restores original query order (re-formats from raw).
func (v *SQLEditorView) restoreOriginalOrder() {
	// We don't store original order separately, so just leave as-is
	// In practice, user would re-execute query to get original order
}

// compareValues compares two values for sorting.
func compareValues(a, b any) bool {
	// Handle nil values - sort nulls last
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false // nulls last
	}
	if b == nil {
		return true // nulls last
	}

	// Type-specific comparison
	switch va := a.(type) {
	case int64:
		if vb, ok := b.(int64); ok {
			return va < vb
		}
	case int32:
		if vb, ok := b.(int32); ok {
			return va < vb
		}
	case int16:
		if vb, ok := b.(int16); ok {
			return va < vb
		}
	case int:
		if vb, ok := b.(int); ok {
			return va < vb
		}
	case float64:
		if vb, ok := b.(float64); ok {
			return va < vb
		}
	case float32:
		if vb, ok := b.(float32); ok {
			return va < vb
		}
	case string:
		if vb, ok := b.(string); ok {
			return va < vb
		}
	case bool:
		if vb, ok := b.(bool); ok {
			return !va && vb // false < true
		}
	case time.Time:
		if vb, ok := b.(time.Time); ok {
			return va.Before(vb)
		}
	}

	// Fallback: compare string representations
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

// handleSearchInput handles keyboard input during Ctrl+R search mode.
func (v *SQLEditorView) handleSearchInput(key string) tea.Cmd {
	switch key {
	case "esc":
		// Cancel search
		v.searchMode = false
		v.searchQuery = ""
		v.searchResult = nil
		return nil

	case "enter":
		// Accept current selection
		if len(v.searchResult) > 0 && v.searchIndex < len(v.searchResult) {
			v.editor.SetContent(v.searchResult[v.searchIndex].SQL)
		}
		v.searchMode = false
		v.searchQuery = ""
		v.searchResult = nil
		return nil

	case "ctrl+r", "up":
		// Navigate to next result (older)
		if len(v.searchResult) > 0 && v.searchIndex < len(v.searchResult)-1 {
			v.searchIndex++
		}
		return nil

	case "ctrl+s", "down":
		// Navigate to previous result (newer)
		if v.searchIndex > 0 {
			v.searchIndex--
		}
		return nil

	case "backspace":
		// Delete last character from search query
		if len(v.searchQuery) > 0 {
			v.searchQuery = v.searchQuery[:len(v.searchQuery)-1]
			v.searchResult = v.history.Search(v.searchQuery)
			v.searchIndex = 0
		}
		return nil

	default:
		// Add character to search query (single printable characters only)
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			v.searchQuery += key
			v.searchResult = v.history.Search(v.searchQuery)
			v.searchIndex = 0
		}
		return nil
	}
}

// saveSnippetCmd handles the :save NAME command.
// If snippet exists, warns user to use :save! to overwrite.
func (v *SQLEditorView) saveSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :save NAME", true)
		return nil
	}

	name := args[0]
	sql := strings.TrimSpace(v.editor.GetBuffer().Text())
	if sql == "" {
		v.showToast("No query to save", true)
		return nil
	}

	// Check if snippet exists - require :save! to overwrite
	if v.snippets.Exists(name) {
		v.showToast(fmt.Sprintf("Snippet '%s' exists. Use :save! %s to overwrite", name, name), true)
		return nil
	}

	_, err := v.snippets.Save(name, sql, "")
	if err != nil {
		v.showToast(fmt.Sprintf("Save failed: %s", err.Error()), true)
		return nil
	}

	v.showToast(fmt.Sprintf("Saved snippet '%s'", name), false)
	return nil
}

// saveSnippetForceCmd handles the :save! NAME command (force overwrite).
func (v *SQLEditorView) saveSnippetForceCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :save! NAME", true)
		return nil
	}

	name := args[0]
	sql := strings.TrimSpace(v.editor.GetBuffer().Text())
	if sql == "" {
		v.showToast("No query to save", true)
		return nil
	}

	overwritten, err := v.snippets.Save(name, sql, "")
	if err != nil {
		v.showToast(fmt.Sprintf("Save failed: %s", err.Error()), true)
		return nil
	}

	if overwritten {
		v.showToast(fmt.Sprintf("Updated snippet '%s'", name), false)
	} else {
		v.showToast(fmt.Sprintf("Saved snippet '%s'", name), false)
	}
	return nil
}

// loadSnippetCmd handles the :load NAME command.
func (v *SQLEditorView) loadSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :load NAME", true)
		return nil
	}

	name := args[0]
	snippet, err := v.snippets.Load(name)
	if err != nil {
		v.showToast(err.Error(), true)
		return nil
	}

	v.editor.SetContent(snippet.SQL)
	v.editor.SetCursorPosition(0, 0)
	v.showToast(fmt.Sprintf("Loaded snippet '%s'", name), false)
	return nil
}

// deleteSnippetCmd handles the :delete NAME command for snippets.
func (v *SQLEditorView) deleteSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :delete NAME", true)
		return nil
	}

	name := args[0]
	if err := v.snippets.Delete(name); err != nil {
		v.showToast(err.Error(), true)
		return nil
	}

	v.showToast(fmt.Sprintf("Deleted snippet '%s'", name), false)
	return nil
}

// openSnippetBrowser opens the snippet browser overlay.
func (v *SQLEditorView) openSnippetBrowser() {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return
	}

	v.mode = ModeSnippetBrowser
	v.snippetBrowsing = true
	v.snippetSearchQuery = ""
	v.snippetList = v.snippets.List()
	v.snippetIndex = 0
}

// closeSnippetBrowser closes the snippet browser overlay.
func (v *SQLEditorView) closeSnippetBrowser() {
	v.mode = ModeNormal
	v.snippetBrowsing = false
	v.snippetSearchQuery = ""
	v.snippetList = nil
	v.snippetIndex = 0
}

// handleSnippetBrowserInput handles keyboard input in snippet browser mode.
func (v *SQLEditorView) handleSnippetBrowserInput(key string) tea.Cmd {
	switch key {
	case "esc", "ctrl+o":
		v.closeSnippetBrowser()
		return nil

	case "enter":
		if len(v.snippetList) > 0 && v.snippetIndex < len(v.snippetList) {
			snippet := v.snippetList[v.snippetIndex]
			v.editor.SetContent(snippet.SQL)
			v.editor.SetCursorPosition(0, 0)
			v.closeSnippetBrowser()
			v.showToast(fmt.Sprintf("Loaded snippet '%s'", snippet.Name), false)
		}
		return nil

	case "up", "k":
		if v.snippetIndex > 0 {
			v.snippetIndex--
		}
		return nil

	case "down", "j":
		if v.snippetIndex < len(v.snippetList)-1 {
			v.snippetIndex++
		}
		return nil

	case "g":
		v.snippetIndex = 0
		return nil

	case "G":
		if len(v.snippetList) > 0 {
			v.snippetIndex = len(v.snippetList) - 1
		}
		return nil

	case "d", "delete":
		// Delete current snippet
		if len(v.snippetList) > 0 && v.snippetIndex < len(v.snippetList) {
			name := v.snippetList[v.snippetIndex].Name
			if err := v.snippets.Delete(name); err == nil {
				v.showToast(fmt.Sprintf("Deleted snippet '%s'", name), false)
				v.snippetList = v.snippets.Search(v.snippetSearchQuery)
				if v.snippetIndex >= len(v.snippetList) && v.snippetIndex > 0 {
					v.snippetIndex--
				}
			}
		}
		return nil

	case "backspace":
		if len(v.snippetSearchQuery) > 0 {
			v.snippetSearchQuery = v.snippetSearchQuery[:len(v.snippetSearchQuery)-1]
			v.snippetList = v.snippets.Search(v.snippetSearchQuery)
			v.snippetIndex = 0
		}
		return nil

	default:
		// Add to search query if printable character
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			v.snippetSearchQuery += key
			v.snippetList = v.snippets.Search(v.snippetSearchQuery)
			v.snippetIndex = 0
		}
		return nil
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

// growResults increases the results portion (shrinks editor).
func (v *SQLEditorView) growResults() {
	v.splitRatio -= 0.1
	if v.splitRatio < 0.2 {
		v.splitRatio = 0.2
	}
	v.SetSize(v.width, v.height)
}

// shrinkResults decreases the results portion (grows editor).
func (v *SQLEditorView) shrinkResults() {
	v.splitRatio += 0.1
	if v.splitRatio > 0.8 {
		v.splitRatio = 0.8
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

	// Search overlay (Ctrl+R)
	if v.searchMode {
		return v.renderSearchOverlay()
	}

	// Snippet browser overlay (Ctrl+O)
	if v.snippetBrowsing {
		return v.renderSnippetBrowser()
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

// renderEditor renders the SQL editor with vimtea.
func (v *SQLEditorView) renderEditor() string {
	// Title bar (vimtea's status bar shows the mode)
	title := "SQL Editor"
	if v.focus == FocusEditor {
		title = styles.AccentStyle.Render("● ") + title
	} else {
		title = "  " + title
	}

	titleBar := styles.TitleStyle.Render(title)

	// Vimtea editor view (includes its own status bar with mode/command line)
	editorView := v.editor.View()

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left, titleBar, editorView)

	// Use v.editorHeight set in SetSize for consistent height
	return lipgloss.NewStyle().
		Height(v.editorHeight).
		MaxHeight(v.editorHeight).
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

	// Pagination footer (always reserve space if multiple pages)
	var footer string
	hasMultiplePages := v.results != nil && v.results.TotalPages() > 1
	if hasMultiplePages {
		footer = styles.PaginationStyle.Render(
			fmt.Sprintf("Page %d/%d (n/p to navigate)",
				v.results.CurrentPage, v.results.TotalPages()))
	}

	// Calculate content height (reserve 1 line for footer if needed)
	contentHeight := resultsHeight - 1 // -1 for title bar
	if hasMultiplePages {
		contentHeight-- // reserve line for pagination footer
	}

	// Constrain content to available height
	constrainedContent := lipgloss.NewStyle().
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(content)

	// Combine with footer OUTSIDE the constrained area
	result := lipgloss.JoinVertical(lipgloss.Left, titleBar, constrainedContent)
	if footer != "" {
		result = lipgloss.JoinVertical(lipgloss.Left, result, footer)
	}

	return result
}

// renderResultsTable renders the results as a table.
func (v *SQLEditorView) renderResultsTable() string {
	if v.results == nil || len(v.results.Columns) == 0 {
		return ""
	}

	var lines []string

	totalCols := len(v.results.Columns)

	// Calculate column widths for ALL columns first (for stability)
	allColWidths := make([]int, totalCols)
	for i, col := range v.results.Columns {
		// Build full header text to measure
		headerText := col.Name
		if col.TypeName != "" {
			headerText = fmt.Sprintf("%s (%s)", col.Name, col.TypeName)
		}
		// Add sort indicator for sorted column
		if v.results.SortColumn == i {
			headerText += " ↑" // Use actual arrow to measure correctly
		}
		allColWidths[i] = lipgloss.Width(headerText)
		if allColWidths[i] < 3 {
			allColWidths[i] = 3
		}
	}

	for _, row := range v.results.Rows {
		for i, val := range row {
			valWidth := lipgloss.Width(val)
			if i < len(allColWidths) && valWidth > allColWidths[i] {
				allColWidths[i] = valWidth
			}
		}
	}

	// Cap each column to a reasonable max width
	maxColWidth := 32
	for i := range allColWidths {
		if allColWidths[i] > maxColWidth {
			allColWidths[i] = maxColWidth
		}
	}

	// Apply horizontal scroll offset
	startCol := v.colScrollOffset
	if startCol >= totalCols {
		startCol = totalCols - 1
	}
	if startCol < 0 {
		startCol = 0
	}

	// Horizontal scroll indicator (always show column info)
	if totalCols > 1 {
		scrollInfo := fmt.Sprintf("Cols %d-%d of %d (←/→ to scroll)", startCol+1, totalCols, totalCols)
		lines = append(lines, styles.MutedStyle.Render(scrollInfo))
	}

	// Header with type indicators and sort indicator - only visible columns
	var headerParts []string
	for i := startCol; i < totalCols; i++ {
		col := v.results.Columns[i]
		headerText := col.Name
		if col.TypeName != "" {
			headerText = fmt.Sprintf("%s (%s)", col.Name, col.TypeName)
		}
		// Add sort indicator
		if v.results.SortColumn == i {
			if v.results.SortAsc {
				headerText += " ↑"
			} else {
				headerText += " ↓"
			}
		}
		headerParts = append(headerParts, padOrTruncate(headerText, allColWidths[i]))
	}
	header := styles.ResultsHeaderStyle.Render(strings.Join(headerParts, " │ "))
	lines = append(lines, header)

	// Separator
	var sepParts []string
	for i := startCol; i < totalCols; i++ {
		sepParts = append(sepParts, strings.Repeat("─", allColWidths[i]))
	}
	lines = append(lines, styles.BorderStyle.Render(strings.Join(sepParts, "─┼─")))

	// Calculate which rows to show based on pagination
	pageStartRow := (v.results.CurrentPage - 1) * v.results.PageSize
	pageEndRow := pageStartRow + v.results.PageSize
	if pageEndRow > len(v.results.Rows) {
		pageEndRow = len(v.results.Rows)
	}

	// Apply vertical scroll within the current page
	visibleRows := v.visibleResultRows()
	startRow := pageStartRow + v.scrollOffset
	endRow := startRow + visibleRows
	if endRow > pageEndRow {
		endRow = pageEndRow
	}

	for i := startRow; i < endRow; i++ {
		row := v.results.Rows[i]
		isSelected := i == v.selectedRow

		var rowParts []string
		for j := startCol; j < len(row); j++ {
			val := row[j]
			cellVal := padOrTruncate(val, allColWidths[j])

			if isSelected {
				// Apply selection style to each cell
				cellVal = styles.ResultsRowSelectedStyle.Render(cellVal)
			} else if val == NullDisplayValue {
				cellVal = styles.ResultsNullStyle.Render(cellVal)
			}
			rowParts = append(rowParts, cellVal)
		}

		// Join with styled separators for selected row
		var rowStr string
		if isSelected {
			sep := styles.ResultsRowSelectedStyle.Render(" │ ")
			rowStr = strings.Join(rowParts, sep)
		} else {
			rowStr = strings.Join(rowParts, " │ ")
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
		hints = "F5: Execute │ \\: Results │ +/-: Resize │ h: Help"
	} else {
		hints = "\\: Editor │ j/k: Nav │ y/Y: Copy │ s/S: Sort │ n/p: Page │ h: Help"
	}
	parts = append(parts, styles.MutedStyle.Render(hints))

	return strings.Join(parts, " │ ")
}

// renderHelp renders the help overlay.
func (v *SQLEditorView) renderHelp() string {
	helpText := `SQL Editor Help

FOCUS SWITCHING
  \            Toggle focus (editor ↔ results) in normal mode
  Enter        From results: enter editor in insert mode

EDITOR MODE (● indicator shows focus)
  F5           Execute query
  Ctrl+Enter   Execute query (insert mode)
  i/a/o        Enter insert mode (vim-style)
  Esc          Exit insert mode / switch to results

HISTORY (cursor at line 1, column 0)
  ↑            Previous query in history
  ↓            Next query in history
  Ctrl+R       Search history (reverse search)

RESULTS MODE (allows view switching and quit)
  j/k          Move selection down/up
  g/G          Go to first/last row
  Ctrl+d/u     Page down/up (10 rows)
  ←/→          Scroll columns left/right
  n/p          Next/previous page (100 rows)
  s/S          Cycle sort column / toggle direction
  y/Y          Copy cell / copy row

RESIZE EDITOR/RESULTS SPLIT
  +/-          Resize panes
  Ctrl+↑/↓     Resize panes (alternative)

NAVIGATION
  1-7          Switch views
  h            Show this help
  q            Quit application

Press h, q, or Esc to close this help.`

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(helpText)
}

// renderSearchOverlay renders the Ctrl+R reverse search overlay.
func (v *SQLEditorView) renderSearchOverlay() string {
	var sb strings.Builder

	// Title
	title := styles.HeaderStyle.Render("History Search (Ctrl+R)")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Search input
	searchLine := fmt.Sprintf("Search: %s█", v.searchQuery)
	sb.WriteString(styles.MutedStyle.Render(searchLine))
	sb.WriteString("\n\n")

	// Show results count
	resultCount := len(v.searchResult)
	if resultCount == 0 {
		sb.WriteString(styles.MutedStyle.Render("No matching queries found"))
		sb.WriteString("\n")
	} else {
		sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("Found %d matching queries", resultCount)))
		sb.WriteString("\n\n")

		// Show visible results (up to 10)
		maxVisible := 10
		if maxVisible > resultCount {
			maxVisible = resultCount
		}

		for i := 0; i < maxVisible; i++ {
			entry := v.searchResult[i]

			// Truncate SQL to fit on one line
			sql := strings.ReplaceAll(entry.SQL, "\n", " ")
			sql = strings.ReplaceAll(sql, "\t", " ")
			maxLen := v.width - 10
			if len(sql) > maxLen {
				sql = sql[:maxLen-3] + "..."
			}

			// Highlight selected entry
			if i == v.searchIndex {
				// Apply syntax highlighting to selected entry
				highlighted := HighlightSQL(sql)
				sb.WriteString(styles.TableSelectedStyle.Render(fmt.Sprintf("► %s", highlighted)))
			} else {
				sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("  %s", sql)))
			}
			sb.WriteString("\n")
		}

		if resultCount > maxVisible {
			sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("\n  ... and %d more", resultCount-maxVisible)))
		}
	}

	// Footer hints
	sb.WriteString("\n\n")
	hints := "Enter: Select │ Ctrl+R/↑: Older │ Ctrl+S/↓: Newer │ Esc: Cancel"
	sb.WriteString(styles.MutedStyle.Render(hints))

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(sb.String())
}

// renderSnippetBrowser renders the Ctrl+O snippet browser overlay.
func (v *SQLEditorView) renderSnippetBrowser() string {
	var sb strings.Builder

	// Title
	title := styles.HeaderStyle.Render("Snippets (Ctrl+O)")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Search input
	if v.snippetSearchQuery != "" {
		searchLine := fmt.Sprintf("Filter: %s█", v.snippetSearchQuery)
		sb.WriteString(styles.MutedStyle.Render(searchLine))
		sb.WriteString("\n\n")
	}

	// Show results count
	resultCount := len(v.snippetList)
	if resultCount == 0 {
		if v.snippetSearchQuery != "" {
			sb.WriteString(styles.MutedStyle.Render("No matching snippets found"))
		} else {
			sb.WriteString(styles.MutedStyle.Render("No snippets saved yet"))
			sb.WriteString("\n\n")
			sb.WriteString(styles.MutedStyle.Render("Use :save NAME to save the current query as a snippet"))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("%d snippet(s)", resultCount)))
		sb.WriteString("\n\n")

		// Show visible results (up to 12)
		maxVisible := 12
		if maxVisible > resultCount {
			maxVisible = resultCount
		}

		// Adjust scroll to keep selected item visible
		startIdx := 0
		if v.snippetIndex >= maxVisible {
			startIdx = v.snippetIndex - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > resultCount {
			endIdx = resultCount
		}

		for i := startIdx; i < endIdx; i++ {
			snippet := v.snippetList[i]

			// Truncate SQL to fit on one line
			sql := strings.ReplaceAll(snippet.SQL, "\n", " ")
			sql = strings.ReplaceAll(sql, "\t", " ")
			maxLen := v.width - 30
			if len(sql) > maxLen {
				sql = sql[:maxLen-3] + "..."
			}

			// Format: name - sql preview
			line := fmt.Sprintf("%-20s %s", snippet.Name, sql)

			// Highlight selected entry
			if i == v.snippetIndex {
				sb.WriteString(styles.TableSelectedStyle.Render(fmt.Sprintf("► %s", line)))
			} else {
				sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("  %s", line)))
			}
			sb.WriteString("\n")
		}

		if resultCount > maxVisible {
			sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("\n  ... showing %d-%d of %d", startIdx+1, endIdx, resultCount)))
		}
	}

	// Footer hints
	sb.WriteString("\n\n")
	hints := "Enter: Load │ j/k: Navigate │ d: Delete │ Type to filter │ Esc: Cancel"
	sb.WriteString(styles.MutedStyle.Render(hints))

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(sb.String())
}

// padOrTruncate pads or truncates a string to the given display width.
// Uses lipgloss.Width for proper unicode handling.
func padOrTruncate(s string, width int) string {
	displayWidth := lipgloss.Width(s)
	if displayWidth > width {
		// Truncate by runes, not bytes
		if width > 3 {
			return truncateRunes(s, width-3) + "..."
		}
		return truncateRunes(s, width)
	}
	return s + strings.Repeat(" ", width-displayWidth)
}

// truncateRunes truncates a string to n display characters.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	width := 0
	for i, r := range runes {
		w := lipgloss.Width(string(r))
		if width+w > n {
			return string(runes[:i])
		}
		width += w
	}
	return s
}
