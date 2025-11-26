package sqleditor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StatementType identifies transaction-related SQL statements.
type StatementType int

const (
	StatementTypeQuery StatementType = iota
	StatementTypeBegin
	StatementTypeCommit
	StatementTypeRollback
	StatementTypeSavepoint
	StatementTypeRollbackToSavepoint
	StatementTypeReleaseSavepoint
)

// SessionExecutor manages query execution with transaction state tracking.
// It maintains a persistent connection for transactions and handles
// BEGIN/COMMIT/ROLLBACK/SAVEPOINT commands.
type SessionExecutor struct {
	pool     *pgxpool.Pool
	conn     *pgxpool.Conn // Dedicated connection for transactions
	tx       pgx.Tx
	txState  *TransactionState
	readOnly bool

	// Cancellation support
	cancelFunc context.CancelFunc
	cancelMu   sync.Mutex

	// Logging callback (optional)
	logFunc func(sql string, duration time.Duration, rowCount int64, err error)
}

// NewSessionExecutor creates a new executor with the given connection pool.
func NewSessionExecutor(pool *pgxpool.Pool, readOnly bool) *SessionExecutor {
	return &SessionExecutor{
		pool:     pool,
		readOnly: readOnly,
		txState: &TransactionState{
			Active:         false,
			SavepointStack: make([]string, 0),
		},
	}
}

// SetLogFunc sets an optional callback for query audit logging.
func (se *SessionExecutor) SetLogFunc(fn func(sql string, duration time.Duration, rowCount int64, err error)) {
	se.logFunc = fn
}

// ExecuteQuery executes a SQL query with timeout and returns results.
// Handles transaction commands (BEGIN/COMMIT/ROLLBACK/SAVEPOINT) internally.
func (se *SessionExecutor) ExecuteQuery(ctx context.Context, sql string, timeout time.Duration) (*ExecutionResult, error) {
	stmtType := DetectTransactionStatement(sql)

	// Check read-only mode for write operations
	if se.readOnly && isWriteOperation(sql) {
		return &ExecutionResult{
			Error:   fmt.Errorf("operation blocked: read-only mode is enabled"),
			Message: "Read-only mode blocks DDL/DML statements",
		}, nil
	}

	// Create timeout context with cancellation support
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	se.cancelMu.Lock()
	se.cancelFunc = cancel
	se.cancelMu.Unlock()
	defer func() {
		se.cancelMu.Lock()
		se.cancelFunc = nil
		se.cancelMu.Unlock()
		cancel()
	}()

	start := time.Now()
	var result *ExecutionResult
	var err error

	switch stmtType {
	case StatementTypeBegin:
		result, err = se.handleBegin(queryCtx, sql)
	case StatementTypeCommit:
		result, err = se.handleCommit(queryCtx)
	case StatementTypeRollback:
		result, err = se.handleRollback(queryCtx)
	case StatementTypeRollbackToSavepoint:
		result, err = se.handleRollbackToSavepoint(queryCtx, sql)
	case StatementTypeSavepoint:
		result, err = se.handleSavepoint(queryCtx, sql)
	case StatementTypeReleaseSavepoint:
		result, err = se.handleReleaseSavepoint(queryCtx, sql)
	default:
		result, err = se.executeStatement(queryCtx, sql)
	}

	duration := time.Since(start)

	// Update result duration
	if result != nil {
		result.Duration = duration
	}

	// Call log function if set
	if se.logFunc != nil && result != nil {
		var rowCount int64
		if result.RowsAffected > 0 {
			rowCount = result.RowsAffected
		} else if result.Rows != nil {
			rowCount = int64(len(result.Rows))
		}
		se.logFunc(sql, duration, rowCount, result.Error)
	}

	return result, err
}

// CancelQuery cancels the currently executing query.
func (se *SessionExecutor) CancelQuery() {
	se.cancelMu.Lock()
	defer se.cancelMu.Unlock()
	if se.cancelFunc != nil {
		se.cancelFunc()
	}
}

// IsInTransaction returns true if a transaction is active.
func (se *SessionExecutor) IsInTransaction() bool {
	return se.txState.Active
}

// TransactionState returns current transaction information.
func (se *SessionExecutor) TransactionState() *TransactionState {
	return se.txState
}

// Close releases any held resources.
func (se *SessionExecutor) Close() {
	if se.tx != nil {
		// Rollback any pending transaction
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = se.tx.Rollback(ctx)
		se.tx = nil
	}
	if se.conn != nil {
		se.conn.Release()
		se.conn = nil
	}
	se.txState.Active = false
	se.txState.SavepointStack = nil
}

// handleBegin starts a new transaction.
func (se *SessionExecutor) handleBegin(ctx context.Context, sql string) (*ExecutionResult, error) {
	if se.txState.Active {
		return &ExecutionResult{
			Error:   fmt.Errorf("transaction already in progress"),
			Message: "Use SAVEPOINT for nested transactions",
		}, nil
	}

	// Acquire a dedicated connection for the transaction
	conn, err := se.pool.Acquire(ctx)
	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}
	se.conn = conn

	// Start transaction with the original SQL (may include isolation level)
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		se.conn = nil
		return &ExecutionResult{Error: err}, nil
	}

	// If the user specified isolation level in BEGIN, execute it
	if strings.Contains(strings.ToUpper(sql), "ISOLATION") {
		_, execErr := tx.Exec(ctx, sql)
		if execErr != nil {
			_ = tx.Rollback(ctx)
			conn.Release()
			se.conn = nil
			return &ExecutionResult{Error: execErr}, nil
		}
	}

	se.tx = tx
	se.txState.Active = true
	se.txState.StartedAt = time.Now()
	se.txState.StateType = TxActive
	se.txState.IsolationLevel = extractIsolationLevel(sql)
	se.txState.SavepointStack = make([]string, 0)

	return &ExecutionResult{
		Message: "Transaction started",
	}, nil
}

// handleCommit commits the current transaction.
func (se *SessionExecutor) handleCommit(ctx context.Context) (*ExecutionResult, error) {
	if !se.txState.Active || se.tx == nil {
		return &ExecutionResult{
			Error:   fmt.Errorf("no transaction in progress"),
			Message: "Use BEGIN to start a transaction",
		}, nil
	}

	err := se.tx.Commit(ctx)
	se.tx = nil
	if se.conn != nil {
		se.conn.Release()
		se.conn = nil
	}

	se.txState.Active = false
	se.txState.StateType = TxNone
	se.txState.SavepointStack = nil

	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}

	return &ExecutionResult{
		Message: "Transaction committed",
	}, nil
}

// handleRollback rolls back the current transaction.
func (se *SessionExecutor) handleRollback(ctx context.Context) (*ExecutionResult, error) {
	if !se.txState.Active || se.tx == nil {
		return &ExecutionResult{
			Error:   fmt.Errorf("no transaction in progress"),
			Message: "Use BEGIN to start a transaction",
		}, nil
	}

	err := se.tx.Rollback(ctx)
	se.tx = nil
	if se.conn != nil {
		se.conn.Release()
		se.conn = nil
	}

	se.txState.Active = false
	se.txState.StateType = TxNone
	se.txState.SavepointStack = nil

	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}

	return &ExecutionResult{
		Message: "Transaction rolled back",
	}, nil
}

// handleSavepoint creates a savepoint within the transaction.
func (se *SessionExecutor) handleSavepoint(ctx context.Context, sql string) (*ExecutionResult, error) {
	if !se.txState.Active || se.tx == nil {
		return &ExecutionResult{
			Error:   fmt.Errorf("no transaction in progress"),
			Message: "Use BEGIN to start a transaction first",
		}, nil
	}

	name := extractSavepointName(sql)
	if name == "" {
		return &ExecutionResult{
			Error: fmt.Errorf("invalid SAVEPOINT syntax"),
		}, nil
	}

	_, err := se.tx.Exec(ctx, sql)
	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}

	se.txState.SavepointStack = append(se.txState.SavepointStack, name)

	return &ExecutionResult{
		Message: fmt.Sprintf("Savepoint '%s' created", name),
	}, nil
}

// handleRollbackToSavepoint rolls back to a savepoint.
func (se *SessionExecutor) handleRollbackToSavepoint(ctx context.Context, sql string) (*ExecutionResult, error) {
	if !se.txState.Active || se.tx == nil {
		return &ExecutionResult{
			Error:   fmt.Errorf("no transaction in progress"),
			Message: "Use BEGIN to start a transaction first",
		}, nil
	}

	name := extractRollbackToSavepointName(sql)
	if name == "" {
		return &ExecutionResult{
			Error: fmt.Errorf("invalid ROLLBACK TO syntax"),
		}, nil
	}

	_, err := se.tx.Exec(ctx, sql)
	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}

	// Remove savepoints after the one we rolled back to
	for i, sp := range se.txState.SavepointStack {
		if strings.EqualFold(sp, name) {
			se.txState.SavepointStack = se.txState.SavepointStack[:i+1]
			break
		}
	}

	return &ExecutionResult{
		Message: fmt.Sprintf("Rolled back to savepoint '%s'", name),
	}, nil
}

// handleReleaseSavepoint releases a savepoint.
func (se *SessionExecutor) handleReleaseSavepoint(ctx context.Context, sql string) (*ExecutionResult, error) {
	if !se.txState.Active || se.tx == nil {
		return &ExecutionResult{
			Error:   fmt.Errorf("no transaction in progress"),
			Message: "Use BEGIN to start a transaction first",
		}, nil
	}

	name := extractReleaseSavepointName(sql)
	if name == "" {
		return &ExecutionResult{
			Error: fmt.Errorf("invalid RELEASE SAVEPOINT syntax"),
		}, nil
	}

	_, err := se.tx.Exec(ctx, sql)
	if err != nil {
		return &ExecutionResult{Error: err}, nil
	}

	// Remove the released savepoint from stack
	for i, sp := range se.txState.SavepointStack {
		if strings.EqualFold(sp, name) {
			se.txState.SavepointStack = append(se.txState.SavepointStack[:i], se.txState.SavepointStack[i+1:]...)
			break
		}
	}

	return &ExecutionResult{
		Message: fmt.Sprintf("Savepoint '%s' released", name),
	}, nil
}

// executeStatement executes a regular SQL statement.
func (se *SessionExecutor) executeStatement(ctx context.Context, sql string) (*ExecutionResult, error) {
	var rows pgx.Rows
	var err error

	if se.txState.Active && se.tx != nil {
		rows, err = se.tx.Query(ctx, sql)
	} else {
		rows, err = se.pool.Query(ctx, sql)
	}

	if err != nil {
		// Check for context cancellation
		if ctx.Err() == context.Canceled {
			return &ExecutionResult{
				Cancelled: true,
				Error:     fmt.Errorf("query cancelled"),
			}, nil
		}
		if ctx.Err() == context.DeadlineExceeded {
			return &ExecutionResult{
				Error: fmt.Errorf("query timeout exceeded"),
			}, nil
		}

		// Mark transaction as aborted on error
		if se.txState.Active {
			se.txState.StateType = TxAborted
		}

		// Extract detailed PostgreSQL error info
		return &ExecutionResult{
			Error:     err,
			ErrorInfo: extractPgErrorInfo(err),
		}, nil
	}
	defer rows.Close()

	// Get column metadata
	fieldDescs := rows.FieldDescriptions()
	columns := make([]Column, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = Column{
			Name:    string(fd.Name),
			TypeOID: fd.DataTypeOID,
		}
	}

	// Collect all rows
	var resultRows [][]any
	for rows.Next() {
		values, valErr := rows.Values()
		if valErr != nil {
			return &ExecutionResult{Error: valErr}, nil
		}
		// Make a copy of values since they may be reused
		rowCopy := make([]any, len(values))
		copy(rowCopy, values)
		resultRows = append(resultRows, rowCopy)
	}

	if rows.Err() != nil {
		return &ExecutionResult{Error: rows.Err()}, nil
	}

	// Get rows affected from command tag
	cmdTag := rows.CommandTag()
	rowsAffected := cmdTag.RowsAffected()

	return &ExecutionResult{
		Columns:      columns,
		Rows:         resultRows,
		RowsAffected: rowsAffected,
	}, nil
}

// DetectTransactionStatement identifies transaction-related SQL statements.
func DetectTransactionStatement(sql string) StatementType {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	switch {
	case strings.HasPrefix(upper, "BEGIN"):
		return StatementTypeBegin
	case strings.HasPrefix(upper, "START TRANSACTION"):
		return StatementTypeBegin
	case strings.HasPrefix(upper, "COMMIT"):
		return StatementTypeCommit
	case strings.HasPrefix(upper, "END"):
		return StatementTypeCommit
	case strings.HasPrefix(upper, "ROLLBACK TO"):
		return StatementTypeRollbackToSavepoint
	case strings.HasPrefix(upper, "ROLLBACK"):
		return StatementTypeRollback
	case strings.HasPrefix(upper, "ABORT"):
		return StatementTypeRollback
	case strings.HasPrefix(upper, "SAVEPOINT"):
		return StatementTypeSavepoint
	case strings.HasPrefix(upper, "RELEASE SAVEPOINT"):
		return StatementTypeReleaseSavepoint
	case strings.HasPrefix(upper, "RELEASE"):
		return StatementTypeReleaseSavepoint
	default:
		return StatementTypeQuery
	}
}

// isWriteOperation checks if the SQL is a write operation (DDL/DML).
func isWriteOperation(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	writePatterns := []string{
		"INSERT", "UPDATE", "DELETE", "TRUNCATE",
		"CREATE", "ALTER", "DROP", "GRANT", "REVOKE",
		"VACUUM", "ANALYZE", "REINDEX", "CLUSTER",
	}
	for _, pattern := range writePatterns {
		if strings.HasPrefix(upper, pattern) {
			return true
		}
	}
	return false
}

// extractIsolationLevel extracts isolation level from BEGIN statement.
func extractIsolationLevel(sql string) string {
	upper := strings.ToUpper(sql)
	if strings.Contains(upper, "SERIALIZABLE") {
		return "SERIALIZABLE"
	}
	if strings.Contains(upper, "REPEATABLE READ") {
		return "REPEATABLE READ"
	}
	if strings.Contains(upper, "READ COMMITTED") {
		return "READ COMMITTED"
	}
	if strings.Contains(upper, "READ UNCOMMITTED") {
		return "READ UNCOMMITTED"
	}
	return "READ COMMITTED" // Default
}

var savepointRegex = regexp.MustCompile(`(?i)^\s*SAVEPOINT\s+(\w+)`)
var rollbackToRegex = regexp.MustCompile(`(?i)^\s*ROLLBACK\s+TO\s+(?:SAVEPOINT\s+)?(\w+)`)
var releaseRegex = regexp.MustCompile(`(?i)^\s*RELEASE\s+(?:SAVEPOINT\s+)?(\w+)`)

// extractSavepointName extracts savepoint name from SAVEPOINT statement.
func extractSavepointName(sql string) string {
	matches := savepointRegex.FindStringSubmatch(sql)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractRollbackToSavepointName extracts savepoint name from ROLLBACK TO statement.
func extractRollbackToSavepointName(sql string) string {
	matches := rollbackToRegex.FindStringSubmatch(sql)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractReleaseSavepointName extracts savepoint name from RELEASE statement.
func extractReleaseSavepointName(sql string) string {
	matches := releaseRegex.FindStringSubmatch(sql)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractPgErrorInfo extracts detailed error information from a PostgreSQL error.
func extractPgErrorInfo(err error) *PgErrorInfo {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &PgErrorInfo{
			Severity:       pgErr.Severity,
			Code:           pgErr.Code,
			Message:        pgErr.Message,
			Detail:         pgErr.Detail,
			Hint:           pgErr.Hint,
			Position:       int(pgErr.Position),
			InternalPos:    int(pgErr.InternalPosition),
			Where:          pgErr.Where,
			SchemaName:     pgErr.SchemaName,
			TableName:      pgErr.TableName,
			ColumnName:     pgErr.ColumnName,
			ConstraintName: pgErr.ConstraintName,
		}
	}
	return nil
}

// FormatErrorWithPosition returns a formatted error string with position indicator.
func FormatErrorWithPosition(err error, sql string) string {
	errInfo := extractPgErrorInfo(err)
	if errInfo == nil {
		return err.Error()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s: %s", errInfo.Severity, errInfo.Message))

	// Add error code
	if errInfo.Code != "" {
		sb.WriteString(fmt.Sprintf(" [%s]", errInfo.Code))
	}

	// Add position information
	if errInfo.Position > 0 && sql != "" {
		line, col := positionToLineCol(sql, errInfo.Position)
		sb.WriteString(fmt.Sprintf("\nAt line %d, column %d", line, col))

		// Show the problematic line with an indicator
		lineText := getLineAtPosition(sql, errInfo.Position)
		if lineText != "" {
			sb.WriteString("\n")
			sb.WriteString(lineText)
			sb.WriteString("\n")
			// Add caret at the error position within the line
			offset := errInfo.Position - getLineStartOffset(sql, errInfo.Position)
			if offset > 0 && offset <= len(lineText) {
				sb.WriteString(strings.Repeat(" ", offset-1))
				sb.WriteString("^")
			}
		}
	}

	// Add detail and hint
	if errInfo.Detail != "" {
		sb.WriteString("\nDetail: ")
		sb.WriteString(errInfo.Detail)
	}
	if errInfo.Hint != "" {
		sb.WriteString("\nHint: ")
		sb.WriteString(errInfo.Hint)
	}

	// Add context
	if errInfo.Where != "" {
		sb.WriteString("\nContext: ")
		sb.WriteString(errInfo.Where)
	}

	return sb.String()
}

// positionToLineCol converts a 1-indexed character position to line and column.
func positionToLineCol(sql string, pos int) (line, col int) {
	if pos <= 0 || pos > len(sql) {
		return 1, pos
	}

	line = 1
	lineStart := 0
	for i := 0; i < pos-1 && i < len(sql); i++ {
		if sql[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	col = pos - lineStart
	if col < 1 {
		col = 1
	}
	return line, col
}

// getLineAtPosition returns the line containing the given position.
func getLineAtPosition(sql string, pos int) string {
	if pos <= 0 || pos > len(sql) {
		return ""
	}

	// Find line start
	start := pos - 1
	for start > 0 && sql[start-1] != '\n' {
		start--
	}

	// Find line end
	end := pos - 1
	for end < len(sql) && sql[end] != '\n' {
		end++
	}

	if start <= end && start < len(sql) {
		return sql[start:end]
	}
	return ""
}

// getLineStartOffset returns the 1-indexed position of the start of the line.
func getLineStartOffset(sql string, pos int) int {
	if pos <= 0 || pos > len(sql) {
		return 1
	}

	start := pos - 1
	for start > 0 && sql[start-1] != '\n' {
		start--
	}
	return start + 1 // Convert to 1-indexed
}
