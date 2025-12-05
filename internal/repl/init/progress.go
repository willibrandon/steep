package init

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProgressTracker monitors and reports initialization progress.
type ProgressTracker struct {
	pool     *pgxpool.Pool
	manager  *Manager
	interval time.Duration
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker(pool *pgxpool.Pool, manager *Manager, interval time.Duration) *ProgressTracker {
	if interval == 0 {
		interval = time.Second
	}
	return &ProgressTracker{
		pool:     pool,
		manager:  manager,
		interval: interval,
	}
}

// Start begins tracking progress for the specified node.
func (p *ProgressTracker) Start(ctx context.Context, nodeID string) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			update, err := p.getProgress(ctx, nodeID)
			if err != nil {
				continue // Log error but keep trying
			}
			select {
			case p.manager.progress <- update:
			default:
				// Channel full, skip update
			}
		}
	}
}

// GetProgress fetches current progress from the database.
func (p *ProgressTracker) GetProgress(ctx context.Context, nodeID string) (*ProgressUpdate, error) {
	update, err := p.getProgress(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return &update, nil
}

func (p *ProgressTracker) getProgress(ctx context.Context, nodeID string) (ProgressUpdate, error) {
	query := `
		SELECT
			node_id,
			phase,
			overall_percent,
			tables_total,
			tables_completed,
			COALESCE(current_table, ''),
			COALESCE(current_table_percent, 0),
			COALESCE(rows_copied, 0),
			COALESCE(bytes_copied, 0),
			COALESCE(throughput_rows_sec, 0),
			COALESCE(eta_seconds, 0),
			COALESCE(parallel_workers, 1),
			COALESCE(error_message, '')
		FROM steep_repl.init_progress
		WHERE node_id = $1
	`

	var update ProgressUpdate
	err := p.pool.QueryRow(ctx, query, nodeID).Scan(
		&update.NodeID,
		&update.Phase,
		&update.OverallPercent,
		&update.TablesTotal,
		&update.TablesCompleted,
		&update.CurrentTable,
		&update.CurrentPercent,
		&update.RowsCopied,
		&update.BytesCopied,
		&update.ThroughputRows,
		&update.ETASeconds,
		&update.ParallelWorkers,
		&update.Error,
	)
	return update, err
}

// UpdateProgress writes progress to the database.
func (p *ProgressTracker) UpdateProgress(ctx context.Context, update ProgressUpdate) error {
	query := `
		INSERT INTO steep_repl.init_progress (
			node_id, phase, overall_percent, tables_total, tables_completed,
			current_table, current_table_percent, rows_copied, bytes_copied,
			throughput_rows_sec, eta_seconds, parallel_workers, error_message, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
		ON CONFLICT (node_id) DO UPDATE SET
			phase = EXCLUDED.phase,
			overall_percent = EXCLUDED.overall_percent,
			tables_total = EXCLUDED.tables_total,
			tables_completed = EXCLUDED.tables_completed,
			current_table = EXCLUDED.current_table,
			current_table_percent = EXCLUDED.current_table_percent,
			rows_copied = EXCLUDED.rows_copied,
			bytes_copied = EXCLUDED.bytes_copied,
			throughput_rows_sec = EXCLUDED.throughput_rows_sec,
			eta_seconds = EXCLUDED.eta_seconds,
			parallel_workers = EXCLUDED.parallel_workers,
			error_message = EXCLUDED.error_message,
			updated_at = now()
	`
	_, err := p.pool.Exec(ctx, query,
		update.NodeID,
		update.Phase,
		update.OverallPercent,
		update.TablesTotal,
		update.TablesCompleted,
		update.CurrentTable,
		update.CurrentPercent,
		update.RowsCopied,
		update.BytesCopied,
		update.ThroughputRows,
		update.ETASeconds,
		update.ParallelWorkers,
		update.Error,
	)
	return err
}

// ClearProgress removes progress tracking for a completed or cancelled operation.
func (p *ProgressTracker) ClearProgress(ctx context.Context, nodeID string) error {
	query := `DELETE FROM steep_repl.init_progress WHERE node_id = $1`
	_, err := p.pool.Exec(ctx, query, nodeID)
	return err
}

// CalculateETA estimates remaining time based on current progress.
func CalculateETA(bytesCompleted, bytesTotal int64, elapsedSeconds float64) int {
	if bytesCompleted == 0 || elapsedSeconds == 0 {
		return 0
	}
	bytesPerSecond := float64(bytesCompleted) / elapsedSeconds
	bytesRemaining := bytesTotal - bytesCompleted
	if bytesPerSecond <= 0 {
		return 0
	}
	return int(float64(bytesRemaining) / bytesPerSecond)
}

// CalculateThroughput calculates rows per second.
func CalculateThroughput(rowsCompleted int64, elapsedSeconds float64) float32 {
	if elapsedSeconds == 0 {
		return 0
	}
	return float32(float64(rowsCompleted) / elapsedSeconds)
}
