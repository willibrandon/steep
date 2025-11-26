package sqleditor

import (
	"time"
)

// QueryExecutingMsg indicates a query has started.
type QueryExecutingMsg struct {
	SQL       string
	StartTime time.Time
}

// QueryCompletedMsg indicates a query has finished.
type QueryCompletedMsg struct {
	Result *ExecutionResult
}

// ExecutionResult contains query execution outcome.
type ExecutionResult struct {
	Columns      []Column      // Column metadata
	Rows         [][]any       // Raw row data
	RowsAffected int64         // For INSERT/UPDATE/DELETE
	Duration     time.Duration // Execution time
	Error        error         // Query error if any
	ErrorInfo    *PgErrorInfo  // Detailed PostgreSQL error info
	Cancelled    bool          // Whether query was cancelled
	Message      string        // Status message (e.g., "Transaction started")
}

// PgErrorInfo contains detailed PostgreSQL error information.
type PgErrorInfo struct {
	Severity       string // ERROR, WARNING, etc.
	Code           string // PostgreSQL error code (e.g., "42P01")
	Message        string // Primary error message
	Detail         string // Optional detail message
	Hint           string // Optional hint message
	Position       int    // Error position in query (1-indexed, 0 if not available)
	InternalPos    int    // Internal query position
	Where          string // Context string
	SchemaName     string // Schema name if applicable
	TableName      string // Table name if applicable
	ColumnName     string // Column name if applicable
	ConstraintName string // Constraint name if applicable
}

// QueryCancelledMsg indicates a query was cancelled.
type QueryCancelledMsg struct{}

// TransactionStateChangedMsg indicates transaction state changed.
type TransactionStateChangedMsg struct {
	State *TransactionState
}

// ConnectionStatusMsg indicates connection state changed.
type ConnectionStatusMsg struct {
	Connected   bool
	Reconnected bool
	Error       error
}

// HistoryNavigatedMsg indicates history navigation occurred.
type HistoryNavigatedMsg struct {
	SQL   string
	Index int
}

// HistoryLoadedMsg indicates history was loaded from persistence.
type HistoryLoadedMsg struct {
	Entries []HistoryEntry
	Error   error
}

// SnippetLoadedMsg indicates a snippet was loaded.
type SnippetLoadedMsg struct {
	Name string
	SQL  string
}

// SnippetsListedMsg indicates snippets were listed.
type SnippetsListedMsg struct {
	Snippets []Snippet
	Error    error
}

// SnippetSavedMsg indicates a snippet was saved.
type SnippetSavedMsg struct {
	Name  string
	Error error
}

// ExportCompletedMsg indicates export finished.
type ExportCompletedMsg struct {
	Filepath string
	Format   string
	Error    error
}

// PageChangedMsg indicates the results page changed.
type PageChangedMsg struct {
	Page int
}

// FocusChangedMsg indicates focus changed between editor and results.
type FocusChangedMsg struct {
	Focus FocusArea
}

// SplitResizedMsg indicates the editor/results split was resized.
type SplitResizedMsg struct {
	Ratio float64
}

// HelpToggledMsg indicates help overlay was toggled.
type HelpToggledMsg struct {
	Visible bool
}

// CommandEnteredMsg indicates a : command was entered.
type CommandEnteredMsg struct {
	Command string
	Args    []string
}

// RefreshSQLEditorMsg requests a data refresh.
type RefreshSQLEditorMsg struct{}

// QueryAuditEntry represents a logged query for audit purposes.
type QueryAuditEntry struct {
	SQL        string        // The SQL text
	ExecutedAt time.Time     // When query was executed
	Duration   time.Duration // Execution time
	RowCount   int64         // Number of rows returned/affected
	Error      string        // Error message if failed
	Success    bool          // Whether query succeeded
}

// MaxAuditEntries is the maximum number of audit entries to keep in memory.
const MaxAuditEntries = 500

// CellCopiedMsg indicates a cell value was copied to clipboard.
type CellCopiedMsg struct {
	Value string
	Error error
}

// RowCopiedMsg indicates a row was copied to clipboard.
type RowCopiedMsg struct {
	Values []string
	Error  error
}
