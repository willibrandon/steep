// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components/vimtea"
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

	// Calculated layout dimensions for mouse coordinate translation (set in View())
	editorContentStartY int // Lines before editor content (connection bar + title)
	editorSectionHeight int // Actual rendered height of editor section
	resultsDataStartY   int // Lines before results data rows
	resultsHeaderHeight int // Lines in results header (title + column header + separator)

	// Results
	results         *ResultSet
	selectedRow     int
	selectedCol     int // Selected column for cell copy (-1 = none)
	scrollOffset    int // Vertical scroll (row offset)
	colScrollOffset int // Horizontal scroll (column offset)
	executedQuery   string
	lastError       error
	lastErrorInfo   *PgErrorInfo

	// Server-side pagination state
	paginationBaseSQL string // Original SELECT without LIMIT/OFFSET
	paginationPage    int    // Current page (1-indexed), 0 if not paginating
	paginationTotal   int64  // Total row count, -1 if unknown

	// Execution
	executor     *SessionExecutor
	executing    bool
	startTime    time.Time
	lastUpdate   time.Time
	queryTimeout time.Duration

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
		vimtea.WithEnableStatusBar(true),           // Shows mode and :command line
		vimtea.WithEnableModeCommand(true),         // Enable :commands
		vimtea.WithRelativeNumbers(false),          // Standard line numbers
		vimtea.WithContent(""),                     // Start empty
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
		mode:         ModeNormal,
		focus:        FocusEditor,
		editor:       editor,
		keys:         DefaultKeyMap(),
		splitRatio:   0.6, // 60% editor, 40% results
		queryTimeout: DefaultQueryTimeout,
		clipboard:    ui.NewClipboardWriter(),
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
			v.editor.Blur()
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

	// Export commands
	v.editor.AddCommand("export", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.exportCmd(args)
	})

	// REPL command - launch external pgcli or psql
	v.editor.AddCommand("repl", func(buf vimtea.Buffer, args []string) tea.Cmd {
		return v.replCmd(args)
	})

	// Initialize snippet manager
	if sm, err := NewSnippetManager(); err == nil {
		v.snippets = sm
	}

	return v
}

// Init initializes the SQL Editor view.
func (v *SQLEditorView) Init() tea.Cmd {
	return v.editor.Init()
}

// SetSize sets the dimensions of the view.
func (v *SQLEditorView) SetSize(width, height int) {
	v.width = width
	v.height = height

	// Fixed overhead: connection bar (2 lines) + title (1 line) + footer (2 lines) = 5
	const fixedOverhead = 5
	availableHeight := height - fixedOverhead

	// Calculate editor height based on split ratio
	v.editorHeight = max(int(float64(availableHeight)*v.splitRatio), 7)

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

// SetQueryTimeout sets the query execution timeout.
func (v *SQLEditorView) SetQueryTimeout(timeout time.Duration) {
	v.queryTimeout = timeout
}

// clearEditorAndResults clears both the editor content and query results.
func (v *SQLEditorView) clearEditorAndResults() {
	// Clear editor content and display name
	v.editor.SetContent("")
	v.editor.SetDisplayName("")

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
		} else {
			v.lastError = nil
			v.lastErrorInfo = nil
		}

		// Check for pagination metadata in Message field
		var pageNum int
		var totalCount int64 = -1
		isPaginated := false
		if msg.Result.Message != "" && strings.HasPrefix(msg.Result.Message, "__PAGE__:") {
			isPaginated = true
			// Parse __PAGE__:pageNum:totalCount
			parts := strings.Split(msg.Result.Message, ":")
			if len(parts) >= 3 {
				fmt.Sscanf(parts[1], "%d", &pageNum)
				fmt.Sscanf(parts[2], "%d", &totalCount)
			}
			// Update pagination state
			v.paginationPage = pageNum
			if totalCount >= 0 {
				v.paginationTotal = totalCount
			}
		} else if msg.Result.Message != "" {
			v.showToast(msg.Result.Message, false)
		}

		// Convert results to display format (even for 0 rows, to show column headers)
		if msg.Result.Columns != nil || msg.Result.Rows != nil {
			rows := msg.Result.Rows
			if rows == nil {
				rows = [][]any{} // Empty slice instead of nil
			}

			// For paginated results, use stored total; otherwise use row count
			totalRows := len(rows)
			currentPage := 1
			if isPaginated {
				currentPage = pageNum
				if v.paginationTotal > 0 {
					totalRows = int(v.paginationTotal)
				}
			}

			v.results = &ResultSet{
				Columns:     msg.Result.Columns,
				Rows:        FormatResultSet(rows),
				RawRows:     rows,
				TotalRows:   totalRows,
				CurrentPage: currentPage,
				PageSize:    DefaultPageSize,
				ExecutionMs: msg.Result.Duration.Milliseconds(),
				SortColumn:  -1, // No sort initially
				SortAsc:     true,
			}
			// Pre-calculate column widths once (avoids O(rows*cols) lipgloss.Width calls on each render)
			v.results.CalculateColWidths(32) // maxColWidth = 32
			v.selectedRow = -1               // No row selected initially
			v.selectedCol = 0                // Default to first column when row is selected
			v.scrollOffset = 0
			v.colScrollOffset = 0

			// Show appropriate toast
			if msg.Result.Warning != "" {
				v.showToast(msg.Result.Warning, true)
			} else if isPaginated && v.paginationTotal > 0 {
				maxPages := (int(v.paginationTotal) + DefaultPageSize - 1) / DefaultPageSize
				v.showToast(fmt.Sprintf("Page %d/%d (%d total rows, %dms)",
					currentPage, maxPages, v.paginationTotal, msg.Result.Duration.Milliseconds()), false)
			} else if isPaginated {
				v.showToast(fmt.Sprintf("Page %d - %d rows (%dms)",
					currentPage, len(rows), msg.Result.Duration.Milliseconds()), false)
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
		return v, v.handleMouseMsg(msg)

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

	case ReplExitedMsg:
		// REPL has exited, we're back in the TUI
		if msg.Err != nil {
			errMsg := fmt.Sprintf("REPL %s error: %s", msg.Tool, msg.Err.Error())
			logger.Error(errMsg)
			v.showToast(errMsg, true)
		} else {
			v.showToast("Returned from "+msg.Tool, false)
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
		case "H", "esc", "q":
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
			v.editor.Focus()
			return nil
		}
		if v.focus == FocusEditor && v.editor.GetMode() == vimtea.ModeNormal {
			v.focus = FocusResults
			v.editor.Blur()
			return nil
		}
	}

	// Enter key on results focuses editor and enters insert mode
	if key == "enter" && v.focus == FocusResults {
		v.focus = FocusEditor
		v.editor.Focus()
		return v.editor.SetMode(vimtea.ModeInsert)
	}

	// 'H' for SQL editor help (shift+h since h is column navigation)
	if key == "H" && v.focus == FocusResults {
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
		// Let 'q' and '?' pass through to app for quitting and help
		if key == "q" || key == "?" {
			return nil
		}
		return v.handleResultsKeys(key)
	}

	// Key not handled - let vimtea process it
	return nil
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
