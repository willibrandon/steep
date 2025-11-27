// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import "time"

// Query represents a SQL query with execution metadata.
type Query struct {
	SQL        string        // The SQL text
	ExecutedAt time.Time     // When query was executed
	Duration   time.Duration // Execution time
	RowCount   int64         // Number of rows returned/affected
	Error      string        // Error message if failed
	Cancelled  bool          // Whether query was cancelled
}

// ResultSet holds query results for display.
type ResultSet struct {
	Columns     []Column   // Column metadata
	Rows        [][]string // Row data (pre-formatted for display)
	RawRows     [][]any    // Raw row data (for sorting)
	TotalRows   int        // Total row count (before pagination)
	CurrentPage int        // Current page (1-indexed)
	PageSize    int        // Rows per page (default 100)
	ExecutionMs int64      // Query execution time in milliseconds
	SortColumn  int        // Currently sorted column (-1 for none)
	SortAsc     bool       // Sort direction (true = ascending)
}

// Column represents a result column.
type Column struct {
	Name     string // Column name from query
	TypeOID  uint32 // PostgreSQL type OID
	TypeName string // Human-readable type name (e.g., "integer", "text")
	Width    int    // Display width for TUI table
}

// TotalPages returns the total number of pages.
func (r *ResultSet) TotalPages() int {
	if r.PageSize <= 0 {
		return 1
	}
	pages := r.TotalRows / r.PageSize
	if r.TotalRows%r.PageSize > 0 {
		pages++
	}
	if pages == 0 {
		return 1
	}
	return pages
}

// HasNextPage returns true if there is a next page.
func (r *ResultSet) HasNextPage() bool {
	return r.CurrentPage < r.TotalPages()
}

// HasPrevPage returns true if there is a previous page.
func (r *ResultSet) HasPrevPage() bool {
	return r.CurrentPage > 1
}

// StartRow returns the 1-indexed start row of the current page.
func (r *ResultSet) StartRow() int {
	if r.TotalRows == 0 {
		return 0
	}
	return (r.CurrentPage-1)*r.PageSize + 1
}

// EndRow returns the 1-indexed end row of the current page.
func (r *ResultSet) EndRow() int {
	if r.TotalRows == 0 {
		return 0
	}
	end := r.CurrentPage * r.PageSize
	if end > r.TotalRows {
		end = r.TotalRows
	}
	return end
}

// TransactionStateType represents the transaction state for display.
type TransactionStateType int

const (
	TxNone TransactionStateType = iota
	TxActive
	TxAborted
)

// TransactionState represents transaction context.
type TransactionState struct {
	Active         bool          // Whether in a transaction
	StartedAt      time.Time     // When transaction began
	SavepointStack []string      // Nested savepoint names
	IsolationLevel string        // READ COMMITTED, SERIALIZABLE, etc.
	StateType      TransactionStateType
}

// HistoryEntry represents a query in history.
type HistoryEntry struct {
	ID         int64     // SQLite row ID
	SQL        string    // Query text
	ExecutedAt time.Time // Execution timestamp
	DurationMs int64     // Execution duration
	RowCount   int64     // Rows returned/affected
	Error      string    // Error if failed (empty if success)
}

// Snippet represents a saved query.
type Snippet struct {
	Name        string    `yaml:"name"`        // Unique identifier
	Description string    `yaml:"description,omitempty"` // Optional description
	SQL         string    `yaml:"sql"`         // Query text
	CreatedAt   time.Time `yaml:"created_at"`  // When snippet was created
	UpdatedAt   time.Time `yaml:"updated_at"`  // When snippet was last modified
}

// FocusArea indicates which component has keyboard focus.
type FocusArea int

const (
	FocusEditor FocusArea = iota
	FocusResults
	FocusCommandLine
	FocusSnippetBrowser
	FocusHistorySearch
)

// EditorState represents the editor's UI state.
type EditorState struct {
	Focus         FocusArea // Which pane has focus
	SplitRatio    float64   // Editor/results split (0.0-1.0)
	HistoryIndex  int       // Current position in history (-1 = not browsing)
	ShowHelp      bool      // Help overlay visible
	CommandBuffer string    // For : commands
}

// DefaultPageSize is the default number of rows per page.
const DefaultPageSize = 100

// DefaultQueryTimeout is the default query timeout.
const DefaultQueryTimeout = 30 * time.Second

// MaxHistoryEntries is the maximum number of in-memory history entries.
const MaxHistoryEntries = 100

// MaxCharLimit is the maximum characters allowed in the editor.
const MaxCharLimit = 100000
