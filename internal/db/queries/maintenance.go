// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// Error types for maintenance operations
var (
	// ErrReadOnlyMode indicates an operation was attempted in read-only mode
	ErrReadOnlyMode = errors.New("operation blocked: application is in read-only mode")
	// ErrOperationInProgress indicates another maintenance operation is already running
	ErrOperationInProgress = errors.New("another maintenance operation is in progress")
	// ErrCancellationFailed indicates pg_cancel_backend returned false
	ErrCancellationFailed = errors.New("failed to cancel operation: process may have already completed")
	// ErrConnectionLost indicates the database connection was lost during operation
	ErrConnectionLost = errors.New("database connection lost during operation")
)

// VacuumOptions configures VACUUM behavior.
type VacuumOptions struct {
	Full    bool // Use VACUUM FULL (exclusive lock, returns space to OS)
	Analyze bool // Also run ANALYZE after VACUUM
	Verbose bool // Emit detailed logging
}

// RunningOperation represents a maintenance operation currently executing.
type RunningOperation struct {
	PID         int
	Database    string
	Schema      string
	Table       string
	Operation   string // "VACUUM", "VACUUM FULL", "ANALYZE", etc.
	Phase       string
	ProgressPct float64
	StartedAt   time.Time
}

// MaintenanceExecutor provides methods for executing and monitoring
// database maintenance operations.
type MaintenanceExecutor interface {
	// ExecuteVacuum runs VACUUM on a table.
	// Supports options: full, analyze, verbose.
	ExecuteVacuum(ctx context.Context, schema, table string, opts VacuumOptions) error

	// ExecuteAnalyze runs ANALYZE on a table.
	ExecuteAnalyze(ctx context.Context, schema, table string) error

	// ExecuteReindex runs REINDEX on a table or index.
	ExecuteReindex(ctx context.Context, schema, name string, isIndex bool) error

	// CancelOperation cancels a running maintenance operation by PID.
	// Returns true if cancellation signal was sent successfully.
	CancelOperation(ctx context.Context, pid int) (bool, error)

	// GetVacuumProgress returns current progress for a VACUUM operation.
	// Returns nil if no VACUUM is in progress for the given table.
	GetVacuumProgress(ctx context.Context, schema, table string) (*models.OperationProgress, error)

	// GetVacuumFullProgress returns current progress for a VACUUM FULL operation.
	// Uses pg_stat_progress_cluster since VACUUM FULL rewrites the table.
	GetVacuumFullProgress(ctx context.Context, schema, table string) (*models.OperationProgress, error)

	// GetRunningOperations returns all maintenance operations currently running
	// in the connected database.
	GetRunningOperations(ctx context.Context) ([]RunningOperation, error)
}

// ExecuteVacuumWithOptions runs VACUUM with the specified options.
// Uses quote_ident for safe identifier quoting.
func ExecuteVacuumWithOptions(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string, opts VacuumOptions) error {
	var sql string
	switch {
	case opts.Full && opts.Analyze:
		sql = fmt.Sprintf("VACUUM (FULL, ANALYZE) %s.%s",
			quoteIdentifier(schemaName),
			quoteIdentifier(tableName))
	case opts.Full:
		sql = fmt.Sprintf("VACUUM FULL %s.%s",
			quoteIdentifier(schemaName),
			quoteIdentifier(tableName))
	case opts.Analyze:
		sql = fmt.Sprintf("VACUUM ANALYZE %s.%s",
			quoteIdentifier(schemaName),
			quoteIdentifier(tableName))
	default:
		sql = fmt.Sprintf("VACUUM %s.%s",
			quoteIdentifier(schemaName),
			quoteIdentifier(tableName))
	}

	_, err := pool.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("vacuum %s.%s: %w", schemaName, tableName, err)
	}
	return nil
}

// ExecuteReindexIndex runs REINDEX on a specific index.
func ExecuteReindexIndex(ctx context.Context, pool *pgxpool.Pool, schemaName, indexName string) error {
	query := fmt.Sprintf("REINDEX INDEX %s.%s",
		quoteIdentifier(schemaName),
		quoteIdentifier(indexName))

	_, err := pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("reindex index %s.%s: %w", schemaName, indexName, err)
	}
	return nil
}

// CancelBackend cancels a running operation by PID using pg_cancel_backend.
// Returns true if the cancellation signal was sent successfully.
func CancelBackend(ctx context.Context, pool *pgxpool.Pool, pid int) (bool, error) {
	var cancelled bool
	err := pool.QueryRow(ctx, "SELECT pg_cancel_backend($1)", pid).Scan(&cancelled)
	if err != nil {
		return false, fmt.Errorf("cancel backend pid %d: %w", pid, err)
	}
	return cancelled, nil
}

// GetVacuumProgress returns current progress for a VACUUM operation.
// Returns nil if no VACUUM is in progress for the given table.
// Creates a direct connection to bypass pool contention during heavy I/O.
func GetVacuumProgress(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) (*models.OperationProgress, error) {
	// Note: max_dead_tuples and num_dead_tuples were removed in PostgreSQL 17
	// due to the TidStore redesign for dead tuple storage
	query := `
		SELECT
			v.pid,
			v.phase,
			v.heap_blks_total,
			v.heap_blks_scanned,
			v.heap_blks_vacuumed,
			v.index_vacuum_count,
			COALESCE(ROUND(100.0 * v.heap_blks_scanned / NULLIF(v.heap_blks_total, 0), 2), 0) AS progress_pct
		FROM pg_stat_progress_vacuum v
		JOIN pg_class c ON c.oid = v.relid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`

	// Create a direct connection to bypass pool contention
	// This ensures progress queries can always get through even when pool is saturated
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("connect for vacuum progress: %w", err)
	}
	defer conn.Close(ctx)

	var progress models.OperationProgress
	var pid int
	var progressPct float64

	err = conn.QueryRow(ctx, query, schemaName, tableName).Scan(
		&pid,
		&progress.Phase,
		&progress.HeapBlksTotal,
		&progress.HeapBlksScanned,
		&progress.HeapBlksVacuumed,
		&progress.IndexVacuumCount,
		&progressPct,
	)
	if err != nil {
		// No progress found - operation not running or completed
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get vacuum progress for %s.%s: %w", schemaName, tableName, err)
	}

	progress.PID = pid
	progress.PercentComplete = progressPct
	progress.LastUpdated = time.Now()
	return &progress, nil
}

// GetVacuumFullProgress returns current progress for a VACUUM FULL operation.
// Uses pg_stat_progress_cluster since VACUUM FULL rewrites the table.
// Creates a direct connection to bypass pool contention during heavy I/O.
func GetVacuumFullProgress(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) (*models.OperationProgress, error) {
	query := `
		SELECT
			pc.pid,
			pc.phase,
			pc.heap_blks_total,
			pc.heap_blks_scanned,
			COALESCE(ROUND(100.0 * pc.heap_blks_scanned / NULLIF(pc.heap_blks_total, 0), 2), 0) AS progress_pct
		FROM pg_stat_progress_cluster pc
		JOIN pg_class c ON c.oid = pc.relid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
		  AND pc.command = 'VACUUM FULL'
	`

	// Create a direct connection to bypass pool contention
	// This ensures progress queries can always get through even when pool is saturated
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("connect for vacuum full progress: %w", err)
	}
	defer conn.Close(ctx)

	var progress models.OperationProgress
	var pid int
	var progressPct float64

	err = conn.QueryRow(ctx, query, schemaName, tableName).Scan(
		&pid,
		&progress.Phase,
		&progress.HeapBlksTotal,
		&progress.HeapBlksScanned,
		&progressPct,
	)
	if err != nil {
		// No progress found - operation not running or completed
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get vacuum full progress for %s.%s: %w", schemaName, tableName, err)
	}

	progress.PID = pid
	progress.PercentComplete = progressPct
	progress.LastUpdated = time.Now()
	return &progress, nil
}

// GetRunningMaintenanceOperations returns all maintenance operations currently running.
func GetRunningMaintenanceOperations(ctx context.Context, pool *pgxpool.Pool) ([]RunningOperation, error) {
	query := `
		SELECT
			a.pid,
			a.datname,
			n.nspname AS schema_name,
			c.relname AS table_name,
			CASE
				WHEN pv.pid IS NOT NULL THEN 'VACUUM'
				WHEN pc.pid IS NOT NULL THEN
					CASE pc.command
						WHEN 'VACUUM FULL' THEN 'VACUUM FULL'
						ELSE pc.command
					END
				ELSE 'ANALYZE'
			END AS operation,
			COALESCE(pv.phase, pc.phase, 'running') AS phase,
			COALESCE(
				ROUND(100.0 * pv.heap_blks_scanned / NULLIF(pv.heap_blks_total, 0), 2),
				ROUND(100.0 * pc.heap_blks_scanned / NULLIF(pc.heap_blks_total, 0), 2),
				0
			) AS progress_pct,
			a.backend_start
		FROM pg_stat_activity a
		LEFT JOIN pg_stat_progress_vacuum pv ON pv.pid = a.pid
		LEFT JOIN pg_stat_progress_cluster pc ON pc.pid = a.pid
		LEFT JOIN pg_class c ON c.oid = COALESCE(pv.relid, pc.relid)
		LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE a.query ~* '^(VACUUM|ANALYZE|REINDEX)'
		  AND a.state = 'active'
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query running maintenance operations: %w", err)
	}
	defer rows.Close()

	var ops []RunningOperation
	for rows.Next() {
		var op RunningOperation
		err := rows.Scan(
			&op.PID,
			&op.Database,
			&op.Schema,
			&op.Table,
			&op.Operation,
			&op.Phase,
			&op.ProgressPct,
			&op.StartedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan running operation row: %w", err)
		}
		ops = append(ops, op)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate running operations: %w", err)
	}

	return ops, nil
}

// VacuumIndicator represents the visual status of vacuum freshness.
type VacuumIndicator int

const (
	// VacuumIndicatorOK indicates vacuum is recent (green).
	VacuumIndicatorOK VacuumIndicator = iota
	// VacuumIndicatorWarning indicates vacuum is approaching stale threshold (yellow).
	VacuumIndicatorWarning
	// VacuumIndicatorCritical indicates vacuum is overdue or never performed (red).
	VacuumIndicatorCritical
)

// StaleVacuumConfig defines thresholds for vacuum status indicators.
type StaleVacuumConfig struct {
	// StaleThreshold is the duration after which vacuum is considered stale.
	// Default: 7 days.
	StaleThreshold time.Duration
	// WarningThreshold is the duration for showing warning (yellow) indicator.
	// Default: 3 days.
	WarningThreshold time.Duration
}

// DefaultStaleVacuumConfig returns the default configuration for stale vacuum detection.
func DefaultStaleVacuumConfig() StaleVacuumConfig {
	return StaleVacuumConfig{
		StaleThreshold:   7 * 24 * time.Hour,
		WarningThreshold: 3 * 24 * time.Hour,
	}
}

// FormatVacuumTimestamp formats a vacuum timestamp for display.
// Returns "never" for nil, relative time for recent, date for old.
func FormatVacuumTimestamp(t *time.Time) string {
	if t == nil {
		return "never"
	}

	age := time.Since(*t)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

// GetVacuumStatusIndicator returns the visual indicator status based on last vacuum times.
func GetVacuumStatusIndicator(lastVacuum, lastAutovacuum *time.Time, config StaleVacuumConfig) VacuumIndicator {
	// Get the most recent maintenance time
	lastMaintenance := maxTime(lastVacuum, lastAutovacuum)
	if lastMaintenance == nil {
		return VacuumIndicatorCritical // Never vacuumed
	}

	age := time.Since(*lastMaintenance)
	switch {
	case age > config.StaleThreshold:
		return VacuumIndicatorCritical // Red: overdue
	case age > config.WarningThreshold:
		return VacuumIndicatorWarning // Yellow: approaching threshold
	default:
		return VacuumIndicatorOK // Green/normal: recent
	}
}

// MaxVacuumTime returns the more recent of two vacuum time pointers, or nil if both are nil.
// Exported for use by UI components.
func MaxVacuumTime(a, b *time.Time) *time.Time {
	return maxTime(a, b)
}

// maxTime returns the more recent of two time pointers, or nil if both are nil.
func maxTime(a, b *time.Time) *time.Time {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.After(*b) {
		return a
	}
	return b
}
