// Package contracts defines interfaces for SQL Editor components.
// This file is a design artifact - actual implementation will be in internal/ui/views/sqleditor/.
package contracts

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// =============================================================================
// Core Interfaces
// =============================================================================

// QueryExecutor executes SQL queries with transaction support.
type QueryExecutor interface {
	// ExecuteQuery runs a SQL query with timeout and returns results.
	// Handles transaction state internally (BEGIN/COMMIT/ROLLBACK).
	ExecuteQuery(ctx context.Context, sql string, timeout time.Duration) (*ExecutionResult, error)

	// CancelQuery cancels the currently executing query.
	CancelQuery()

	// IsInTransaction returns true if a transaction is active.
	IsInTransaction() bool

	// TransactionState returns current transaction information.
	TransactionState() *TransactionInfo
}

// HistoryManager manages query history with persistence.
type HistoryManager interface {
	// Add stores a new query in history.
	Add(entry HistoryEntry) error

	// Previous returns the previous query in history navigation.
	// Returns empty string if at the beginning.
	Previous() string

	// Next returns the next query in history navigation.
	// Returns empty string if at the end.
	Next() string

	// Reset resets history navigation position.
	Reset()

	// Search returns queries matching the search term.
	Search(term string) []HistoryEntry

	// Recent returns the N most recent queries.
	Recent(n int) []HistoryEntry
}

// SnippetManager manages saved query snippets.
type SnippetManager interface {
	// Save stores a query as a named snippet.
	Save(name, sql string) error

	// Load retrieves a snippet by name.
	Load(name string) (string, error)

	// Delete removes a snippet by name.
	Delete(name string) error

	// List returns all saved snippets.
	List() []Snippet

	// Exists checks if a snippet name is taken.
	Exists(name string) bool
}

// ResultExporter exports query results to files.
type ResultExporter interface {
	// ExportCSV writes results to a CSV file.
	ExportCSV(results *ResultSet, filepath string) error

	// ExportJSON writes results to a JSON file.
	ExportJSON(results *ResultSet, filepath string) error
}

// =============================================================================
// Data Types
// =============================================================================

// ExecutionResult contains query execution outcome.
type ExecutionResult struct {
	Columns      []Column      // Column metadata
	Rows         [][]any       // Raw row data
	RowsAffected int64         // For INSERT/UPDATE/DELETE
	Duration     time.Duration // Execution time
	Error        error         // Query error if any
	Cancelled    bool          // Whether query was cancelled
	Message      string        // Status message (e.g., "Transaction started")
}

// Column represents result column metadata.
type Column struct {
	Name     string // Column name
	TypeOID  uint32 // PostgreSQL type OID
	TypeName string // Human-readable type
}

// TransactionInfo describes current transaction state.
type TransactionInfo struct {
	Active         bool      // Whether in transaction
	StartedAt      time.Time // When transaction started
	SavepointCount int       // Number of active savepoints
	IsolationLevel string    // Isolation level if known
}

// HistoryEntry represents a query in history.
type HistoryEntry struct {
	ID         int64
	SQL        string
	ExecutedAt time.Time
	DurationMs int64
	RowCount   int64
	Error      string
}

// Snippet represents a saved query.
type Snippet struct {
	Name        string
	Description string
	SQL         string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ResultSet holds paginated query results for display.
type ResultSet struct {
	Columns     []Column   // Column metadata
	Rows        [][]string // Formatted row data
	TotalRows   int        // Total before pagination
	CurrentPage int        // Current page (1-indexed)
	PageSize    int        // Rows per page
	ExecutionMs int64      // Query time
}

// =============================================================================
// Bubbletea Messages
// =============================================================================

// QueryExecutingMsg indicates a query has started.
type QueryExecutingMsg struct {
	SQL       string
	StartTime time.Time
}

// QueryCompletedMsg indicates a query has finished.
type QueryCompletedMsg struct {
	Result *ExecutionResult
}

// QueryCancelledMsg indicates a query was cancelled.
type QueryCancelledMsg struct{}

// TransactionStateChangedMsg indicates transaction state changed.
type TransactionStateChangedMsg struct {
	State *TransactionInfo
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

// SnippetLoadedMsg indicates a snippet was loaded.
type SnippetLoadedMsg struct {
	Name string
	SQL  string
}

// ExportCompletedMsg indicates export finished.
type ExportCompletedMsg struct {
	Filepath string
	Format   string
	Error    error
}

// =============================================================================
// View Interface (extends ViewModel)
// =============================================================================

// SQLEditorView extends ViewModel with SQL Editor specific methods.
type SQLEditorView interface {
	// Standard ViewModel methods
	Init() tea.Cmd
	Update(tea.Msg) (SQLEditorView, tea.Cmd)
	View() string
	SetSize(width, height int)

	// SQL Editor specific
	SetQuery(sql string)
	GetQuery() string
	GetResults() *ResultSet
	IsExecuting() bool
}
