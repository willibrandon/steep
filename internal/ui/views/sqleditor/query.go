package sqleditor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

	// For SELECT queries without LIMIT/OFFSET, use server-side pagination
	if isSelectQuery(sql) && !hasLimitOrOffset(sql) {
		// Strip trailing semicolon for appending LIMIT/OFFSET
		baseSQL := strings.TrimSuffix(strings.TrimSpace(sql), ";")

		// Store pagination state
		v.paginationBaseSQL = baseSQL
		v.paginationPage = 1
		v.paginationTotal = -1

		// Capture values for the goroutine
		executor := v.executor
		timeout := v.queryTimeout

		return func() tea.Msg {
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Run COUNT(*) and actual query IN PARALLEL for performance
			// This makes Steep as fast as psql while still providing total row count
			var wg sync.WaitGroup
			var totalCount int64 = -1
			var result *ExecutionResult
			var queryErr error

			// COUNT query in parallel goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				countSQL := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS _cnt", baseSQL)
				countCtx, countCancel := context.WithTimeout(ctx, 30*time.Second)
				defer countCancel()
				_ = executor.pool.QueryRow(countCtx, countSQL).Scan(&totalCount)
			}()

			// Actual paginated query in parallel goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				paginatedSQL := fmt.Sprintf("%s LIMIT %d OFFSET 0", baseSQL, DefaultPageSize)
				result, queryErr = executor.ExecuteQuery(ctx, paginatedSQL, timeout)
			}()

			// Wait for both to complete
			wg.Wait()

			if queryErr != nil {
				return QueryCompletedMsg{Result: &ExecutionResult{Error: queryErr}}
			}

			// Total time is now max(count_time, query_time) instead of sum
			if result != nil {
				result.Duration = time.Since(start)
				result.Message = fmt.Sprintf("__PAGE__:%d:%d", 1, totalCount)
			}

			return QueryCompletedMsg{Result: result}
		}
	}

	// Non-SELECT or query with LIMIT/OFFSET: clear pagination state, execute as-is
	v.paginationBaseSQL = ""
	v.paginationPage = 0
	v.paginationTotal = -1

	executor := v.executor
	timeout := v.queryTimeout

	return func() tea.Msg {
		result, err := executor.ExecuteQuery(
			context.Background(),
			sql,
			timeout,
		)
		if err != nil {
			return QueryCompletedMsg{Result: &ExecutionResult{Error: err}}
		}
		return QueryCompletedMsg{Result: result}
	}
}

// fetchPage executes a paginated query and returns a tea.Cmd.
// This is used by n/p keys to fetch different pages of results.
func (v *SQLEditorView) fetchPage(page int) tea.Cmd {
	// Validate we have pagination state
	if v.paginationBaseSQL == "" {
		v.showToast("No query to paginate", true)
		return nil
	}
	if v.executor == nil || v.executor.pool == nil {
		v.showToast("No database connection", true)
		return nil
	}

	// Bounds check
	if page < 1 {
		return nil
	}
	if v.paginationTotal > 0 {
		maxPages := (int(v.paginationTotal) + DefaultPageSize - 1) / DefaultPageSize
		if page > maxPages {
			return nil
		}
	}

	// Set executing state BEFORE returning the command
	v.mode = ModeExecuting
	v.executing = true
	v.startTime = time.Now()

	// Capture all values needed by the goroutine
	baseSQL := v.paginationBaseSQL
	executor := v.executor
	timeout := v.queryTimeout
	offset := (page - 1) * DefaultPageSize
	targetPage := page
	storedTotal := v.paginationTotal

	return func() tea.Msg {
		// Build paginated SQL
		paginatedSQL := fmt.Sprintf("%s LIMIT %d OFFSET %d", baseSQL, DefaultPageSize, offset)

		// Execute with timeout
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		result, err := executor.ExecuteQuery(ctx, paginatedSQL, timeout)
		if err != nil {
			return QueryCompletedMsg{Result: &ExecutionResult{Error: err}}
		}

		// Attach pagination metadata to result
		if result != nil {
			result.Message = fmt.Sprintf("__PAGE__:%d:%d", targetPage, storedTotal)
		}

		return QueryCompletedMsg{Result: result}
	}
}
