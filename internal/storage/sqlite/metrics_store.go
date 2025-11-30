package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/willibrandon/steep/internal/metrics"
)

// MetricsStore handles persistence of time-series metrics to SQLite.
type MetricsStore struct {
	db *DB
}

// NewMetricsStore creates a new MetricsStore with the given database connection.
func NewMetricsStore(db *DB) *MetricsStore {
	return &MetricsStore{db: db}
}

// SaveDataPoint persists a single data point.
func (s *MetricsStore) SaveDataPoint(ctx context.Context, metricName string, dp metrics.DataPoint) error {
	query := `INSERT INTO metrics_history (timestamp, metric_name, value) VALUES (?, ?, ?)`
	_, err := s.db.conn.ExecContext(ctx, query, dp.Timestamp.Format(time.RFC3339Nano), metricName, dp.Value)
	if err != nil {
		return fmt.Errorf("failed to save data point: %w", err)
	}
	return nil
}

// SaveBatch persists multiple data points in a transaction.
func (s *MetricsStore) SaveBatch(ctx context.Context, metricName string, points []metrics.DataPoint) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := s.db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO metrics_history (timestamp, metric_name, value) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, dp := range points {
		if !dp.IsValid() {
			continue
		}
		_, err := stmt.ExecContext(ctx, dp.Timestamp.Format(time.RFC3339Nano), metricName, dp.Value)
		if err != nil {
			return fmt.Errorf("failed to insert data point: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetHistory retrieves historical data for a metric since the given time.
// Limited to prevent unbounded result sets.
func (s *MetricsStore) GetHistory(ctx context.Context, metricName string, since time.Time, limit int) ([]metrics.DataPoint, error) {
	if limit <= 0 {
		limit = 10000
	}

	query := `
		SELECT timestamp, value
		FROM metrics_history
		WHERE metric_name = ? AND timestamp >= ?
		ORDER BY timestamp ASC
		LIMIT ?
	`

	rows, err := s.db.conn.QueryContext(ctx, query, metricName, since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}
	defer rows.Close()

	return scanDataPoints(rows)
}

// GetAggregated retrieves aggregated data (AVG) grouped by interval.
// Used for long time windows (1h, 24h) to reduce data points.
func (s *MetricsStore) GetAggregated(ctx context.Context, metricName string, since time.Time, intervalSeconds int) ([]metrics.DataPoint, error) {
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}

	// SQLite doesn't have native interval grouping, so we use strftime with rounding
	// Group by timestamp divided by interval (in seconds)
	query := `
		SELECT
			datetime(
				(strftime('%s', timestamp) / ?) * ?,
				'unixepoch'
			) as bucket,
			AVG(value) as avg_value
		FROM metrics_history
		WHERE metric_name = ? AND timestamp >= ?
		GROUP BY bucket
		ORDER BY bucket ASC
	`

	rows, err := s.db.conn.QueryContext(ctx, query, intervalSeconds, intervalSeconds, metricName, since.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated: %w", err)
	}
	defer rows.Close()

	var result []metrics.DataPoint
	for rows.Next() {
		var timestampStr string
		var value float64
		if err := rows.Scan(&timestampStr, &value); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Parse the bucket timestamp
		t, err := time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			// Try RFC3339 format as fallback
			t, err = time.Parse(time.RFC3339, timestampStr)
			if err != nil {
				continue // Skip malformed timestamps
			}
		}

		result = append(result, metrics.DataPoint{
			Timestamp: t,
			Value:     value,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// Prune removes entries older than the retention period.
// Returns number of rows deleted.
func (s *MetricsStore) Prune(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 7
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	query := `DELETE FROM metrics_history WHERE timestamp < ?`

	result, err := s.db.conn.ExecContext(ctx, query, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("failed to prune: %w", err)
	}

	return result.RowsAffected()
}

// GetLatest returns the most recent data point for a metric.
func (s *MetricsStore) GetLatest(ctx context.Context, metricName string) (metrics.DataPoint, bool, error) {
	query := `
		SELECT timestamp, value
		FROM metrics_history
		WHERE metric_name = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var timestampStr string
	var value float64
	err := s.db.conn.QueryRowContext(ctx, query, metricName).Scan(&timestampStr, &value)
	if err == sql.ErrNoRows {
		return metrics.DataPoint{}, false, nil
	}
	if err != nil {
		return metrics.DataPoint{}, false, fmt.Errorf("failed to get latest: %w", err)
	}

	t, err := time.Parse(time.RFC3339Nano, timestampStr)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, timestampStr)
	}

	return metrics.DataPoint{Timestamp: t, Value: value}, true, nil
}

// Count returns the total number of data points for a metric.
func (s *MetricsStore) Count(ctx context.Context, metricName string) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM metrics_history WHERE metric_name = ?`
	err := s.db.conn.QueryRowContext(ctx, query, metricName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}
	return count, nil
}

// scanDataPoints scans rows into DataPoint slice.
func scanDataPoints(rows *sql.Rows) ([]metrics.DataPoint, error) {
	var result []metrics.DataPoint
	for rows.Next() {
		var timestampStr string
		var value float64
		if err := rows.Scan(&timestampStr, &value); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		t, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, timestampStr)
			if err != nil {
				continue // Skip malformed timestamps
			}
		}

		result = append(result, metrics.DataPoint{
			Timestamp: t,
			Value:     value,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}
