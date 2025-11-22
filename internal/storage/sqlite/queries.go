package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

// SortField defines the column to sort query results by.
type SortField int

const (
	SortByTotalTime SortField = iota
	SortByCalls
	SortByRows
	SortByMeanTime
)

// QueryStats represents aggregated statistics for a normalized query pattern.
type QueryStats struct {
	Fingerprint     uint64
	NormalizedQuery string
	Calls           int64
	TotalTimeMs     float64
	MinTimeMs       *float64
	MaxTimeMs       *float64
	TotalRows       int64
	FirstSeen       time.Time
	LastSeen        time.Time
}

// MeanTimeMs returns average execution time.
func (q *QueryStats) MeanTimeMs() float64 {
	if q.Calls == 0 {
		return 0
	}
	return q.TotalTimeMs / float64(q.Calls)
}

// QueryStatsStore provides CRUD operations for query statistics.
type QueryStatsStore struct {
	db *DB
}

// NewQueryStatsStore creates a new QueryStatsStore.
func NewQueryStatsStore(db *DB) *QueryStatsStore {
	return &QueryStatsStore{db: db}
}

// Upsert inserts a new query stat or updates an existing one.
func (s *QueryStatsStore) Upsert(ctx context.Context, fingerprint uint64, query string, durationMs float64, rows int64) error {
	_, err := s.db.conn.ExecContext(ctx, `
		INSERT INTO query_stats (fingerprint, normalized_query, calls, total_time_ms, min_time_ms, max_time_ms, total_rows, last_seen)
		VALUES (?, ?, 1, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(fingerprint) DO UPDATE SET
			calls = calls + 1,
			total_time_ms = total_time_ms + excluded.total_time_ms,
			min_time_ms = MIN(COALESCE(min_time_ms, excluded.min_time_ms), excluded.min_time_ms),
			max_time_ms = MAX(COALESCE(max_time_ms, excluded.max_time_ms), excluded.max_time_ms),
			total_rows = total_rows + excluded.total_rows,
			last_seen = datetime('now')
	`, fingerprint, query, durationMs, durationMs, durationMs, rows)
	return err
}

// GetTopQueries returns top N queries sorted by the specified field.
func (s *QueryStatsStore) GetTopQueries(ctx context.Context, sortBy SortField, limit int) ([]QueryStats, error) {
	orderBy := "total_time_ms DESC"
	switch sortBy {
	case SortByCalls:
		orderBy = "calls DESC"
	case SortByRows:
		orderBy = "total_rows DESC"
	case SortByMeanTime:
		orderBy = "total_time_ms/calls DESC"
	}

	query := fmt.Sprintf(`
		SELECT fingerprint, normalized_query, calls, total_time_ms,
			   min_time_ms, max_time_ms, total_rows, first_seen, last_seen
		FROM query_stats
		ORDER BY %s
		LIMIT ?
	`, orderBy)

	rows, err := s.db.conn.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanQueryStats(rows)
}

// SearchQueries returns queries matching the regex pattern.
func (s *QueryStatsStore) SearchQueries(ctx context.Context, pattern string, sortBy SortField, limit int) ([]QueryStats, error) {
	// Validate regex pattern
	_, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	orderBy := "total_time_ms DESC"
	switch sortBy {
	case SortByCalls:
		orderBy = "calls DESC"
	case SortByRows:
		orderBy = "total_rows DESC"
	case SortByMeanTime:
		orderBy = "total_time_ms/calls DESC"
	}

	// SQLite doesn't have native REGEXP, so we fetch all and filter in Go
	query := fmt.Sprintf(`
		SELECT fingerprint, normalized_query, calls, total_time_ms,
			   min_time_ms, max_time_ms, total_rows, first_seen, last_seen
		FROM query_stats
		ORDER BY %s
	`, orderBy)

	rows, err := s.db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allStats, err := scanQueryStats(rows)
	if err != nil {
		return nil, err
	}

	// Filter by regex
	re := regexp.MustCompile(pattern)
	var filtered []QueryStats
	for _, stat := range allStats {
		if re.MatchString(stat.NormalizedQuery) {
			filtered = append(filtered, stat)
			if len(filtered) >= limit {
				break
			}
		}
	}

	return filtered, nil
}

// GetByFingerprint returns a single query stat by fingerprint.
func (s *QueryStatsStore) GetByFingerprint(ctx context.Context, fingerprint uint64) (*QueryStats, error) {
	row := s.db.conn.QueryRowContext(ctx, `
		SELECT fingerprint, normalized_query, calls, total_time_ms,
			   min_time_ms, max_time_ms, total_rows, first_seen, last_seen
		FROM query_stats
		WHERE fingerprint = ?
	`, fingerprint)

	var stat QueryStats
	var firstSeen, lastSeen string
	err := row.Scan(
		&stat.Fingerprint,
		&stat.NormalizedQuery,
		&stat.Calls,
		&stat.TotalTimeMs,
		&stat.MinTimeMs,
		&stat.MaxTimeMs,
		&stat.TotalRows,
		&firstSeen,
		&lastSeen,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	stat.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
	stat.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)

	return &stat, nil
}

// Cleanup removes records older than the retention period.
func (s *QueryStatsStore) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).Format("2006-01-02 15:04:05")
	result, err := s.db.conn.ExecContext(ctx, `
		DELETE FROM query_stats
		WHERE last_seen < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Reset deletes all statistics.
func (s *QueryStatsStore) Reset(ctx context.Context) error {
	_, err := s.db.conn.ExecContext(ctx, "DELETE FROM query_stats")
	return err
}

// Count returns the total number of query stats.
func (s *QueryStatsStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM query_stats").Scan(&count)
	return count, err
}

// scanQueryStats scans rows into QueryStats slice.
func scanQueryStats(rows *sql.Rows) ([]QueryStats, error) {
	var stats []QueryStats
	for rows.Next() {
		var stat QueryStats
		var firstSeen, lastSeen string
		err := rows.Scan(
			&stat.Fingerprint,
			&stat.NormalizedQuery,
			&stat.Calls,
			&stat.TotalTimeMs,
			&stat.MinTimeMs,
			&stat.MaxTimeMs,
			&stat.TotalRows,
			&firstSeen,
			&lastSeen,
		)
		if err != nil {
			return nil, err
		}
		stat.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
		stat.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}
