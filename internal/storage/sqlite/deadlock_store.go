package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/willibrandon/steep/internal/logger"
)

// DeadlockEvent represents a single deadlock incident.
type DeadlockEvent struct {
	ID              int64
	DetectedAt      time.Time
	DatabaseName    string
	ResolvedByPID   *int
	DetectionTimeMs *int
	CreatedAt       time.Time
	InstanceName    string // PostgreSQL instance name for multi-instance support
	Processes       []DeadlockProcess
}

// DeadlockProcess represents a process involved in a deadlock.
type DeadlockProcess struct {
	ID              int64
	EventID         int64
	PID             int
	Username        string
	ApplicationName string
	ClientAddr      string
	BackendStart    *time.Time
	XactStart       *time.Time
	LockType        string
	LockMode        string
	RelationName    string
	Query           string
	QueryFingerprint *uint64
	BlockedByPID    *int
}

// DeadlockSummary provides a summary view of deadlock events.
type DeadlockSummary struct {
	ID              int64
	DetectedAt      time.Time
	DatabaseName    string
	ProcessCount    int
	Tables          string // Comma-separated list
	DetectionTimeMs *int
}

// DeadlockStore provides CRUD operations for deadlock history.
type DeadlockStore struct {
	db *DB
}

// NewDeadlockStore creates a new DeadlockStore.
func NewDeadlockStore(db *DB) *DeadlockStore {
	return &DeadlockStore{db: db}
}

// InsertEvent inserts a new deadlock event with its processes.
func (s *DeadlockStore) InsertEvent(ctx context.Context, event *DeadlockEvent) (int64, error) {
	tx, err := s.db.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Use default instance_name if not set
	instanceName := event.InstanceName
	if instanceName == "" {
		instanceName = "default"
	}

	// Insert event
	result, err := tx.ExecContext(ctx, `
		INSERT INTO deadlock_events (detected_at, database_name, resolved_by_pid, detection_time_ms, instance_name)
		VALUES (?, ?, ?, ?, ?)
	`, event.DetectedAt.Format("2006-01-02 15:04:05"), event.DatabaseName, event.ResolvedByPID, event.DetectionTimeMs, instanceName)
	if err != nil {
		return 0, err
	}

	eventID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Insert processes
	for _, proc := range event.Processes {
		var backendStart, xactStart *string
		if proc.BackendStart != nil {
			t := proc.BackendStart.Format("2006-01-02 15:04:05")
			backendStart = &t
		}
		if proc.XactStart != nil {
			t := proc.XactStart.Format("2006-01-02 15:04:05")
			xactStart = &t
		}

		var fingerprint *int64
		if proc.QueryFingerprint != nil {
			fp := int64(*proc.QueryFingerprint)
			fingerprint = &fp
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO deadlock_processes (
				event_id, pid, username, application_name, client_addr,
				backend_start, xact_start, lock_type, lock_mode, relation_name,
				query, query_fingerprint, blocked_by_pid
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, eventID, proc.PID, proc.Username, proc.ApplicationName, proc.ClientAddr,
			backendStart, xactStart, proc.LockType, proc.LockMode, proc.RelationName,
			proc.Query, fingerprint, proc.BlockedByPID)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return eventID, nil
}

// GetRecentEvents returns recent deadlock events with summary info.
// If instanceName is empty, returns events for all instances.
func (s *DeadlockStore) GetRecentEvents(ctx context.Context, days int, limit int, instanceName string) ([]DeadlockSummary, error) {
	var query string
	var args []interface{}
	daysArg := fmt.Sprintf("-%d days", days)

	if instanceName == "" {
		// No filtering - return all instances
		query = `
			SELECT
				de.id,
				de.detected_at,
				de.database_name,
				COUNT(dp.id) as process_count,
				GROUP_CONCAT(DISTINCT dp.relation_name) as tables,
				de.detection_time_ms
			FROM deadlock_events de
			LEFT JOIN deadlock_processes dp ON dp.event_id = de.id
			WHERE de.detected_at > datetime('now', ?)
			GROUP BY de.id
			ORDER BY de.detected_at DESC
			LIMIT ?
		`
		args = []interface{}{daysArg, limit}
	} else {
		// Filter by instance_name
		query = `
			SELECT
				de.id,
				de.detected_at,
				de.database_name,
				COUNT(dp.id) as process_count,
				GROUP_CONCAT(DISTINCT dp.relation_name) as tables,
				de.detection_time_ms
			FROM deadlock_events de
			LEFT JOIN deadlock_processes dp ON dp.event_id = de.id
			WHERE de.detected_at > datetime('now', ?)
			  AND de.instance_name = ?
			GROUP BY de.id
			ORDER BY de.detected_at DESC
			LIMIT ?
		`
		args = []interface{}{daysArg, instanceName, limit}
	}

	rows, err := s.db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []DeadlockSummary
	for rows.Next() {
		var summary DeadlockSummary
		var detectedAt string
		var tables sql.NullString
		var detectionTimeMs sql.NullInt64

		err := rows.Scan(
			&summary.ID,
			&detectedAt,
			&summary.DatabaseName,
			&summary.ProcessCount,
			&tables,
			&detectionTimeMs,
		)
		if err != nil {
			return nil, err
		}

		// Try RFC3339 first (from _loc=auto), then standard format
		summary.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
		if summary.DetectedAt.IsZero() {
			summary.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
		}
		if tables.Valid {
			summary.Tables = tables.String
		}
		if detectionTimeMs.Valid {
			val := int(detectionTimeMs.Int64)
			summary.DetectionTimeMs = &val
		}

		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// GetEvent returns a single deadlock event with all its processes.
func (s *DeadlockStore) GetEvent(ctx context.Context, eventID int64) (*DeadlockEvent, error) {
	// Get event
	row := s.db.conn.QueryRowContext(ctx, `
		SELECT id, detected_at, database_name, resolved_by_pid, detection_time_ms, created_at
		FROM deadlock_events
		WHERE id = ?
	`, eventID)

	var event DeadlockEvent
	var detectedAt, createdAt string
	err := row.Scan(
		&event.ID,
		&detectedAt,
		&event.DatabaseName,
		&event.ResolvedByPID,
		&event.DetectionTimeMs,
		&createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Try RFC3339 first (from _loc=auto), then standard format
	event.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
	if event.DetectedAt.IsZero() {
		event.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
	}
	event.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if event.CreatedAt.IsZero() {
		event.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	}

	// Get processes
	rows, err := s.db.conn.QueryContext(ctx, `
		SELECT id, event_id, pid, username, application_name, client_addr,
			   backend_start, xact_start, lock_type, lock_mode, relation_name,
			   query, query_fingerprint, blocked_by_pid
		FROM deadlock_processes
		WHERE event_id = ?
		ORDER BY pid
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var proc DeadlockProcess
		var backendStart, xactStart sql.NullString
		var fingerprint sql.NullInt64

		err := rows.Scan(
			&proc.ID,
			&proc.EventID,
			&proc.PID,
			&proc.Username,
			&proc.ApplicationName,
			&proc.ClientAddr,
			&backendStart,
			&xactStart,
			&proc.LockType,
			&proc.LockMode,
			&proc.RelationName,
			&proc.Query,
			&fingerprint,
			&proc.BlockedByPID,
		)
		if err != nil {
			return nil, err
		}

		if backendStart.Valid {
			t, _ := time.Parse(time.RFC3339, backendStart.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", backendStart.String)
			}
			proc.BackendStart = &t
		}
		if xactStart.Valid {
			t, _ := time.Parse(time.RFC3339, xactStart.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", xactStart.String)
			}
			proc.XactStart = &t
		}
		if fingerprint.Valid {
			fp := uint64(fingerprint.Int64)
			proc.QueryFingerprint = &fp
		}

		event.Processes = append(event.Processes, proc)
	}

	return &event, rows.Err()
}

// GetTableStats returns deadlock statistics by table.
func (s *DeadlockStore) GetTableStats(ctx context.Context, days int, limit int) ([]TableDeadlockStats, error) {
	daysArg := fmt.Sprintf("-%d days", days)

	query := `
		SELECT
			dp.relation_name,
			COUNT(DISTINCT dp.event_id) as deadlock_count,
			MAX(de.detected_at) as last_occurrence,
			GROUP_CONCAT(DISTINCT dp.lock_mode) as lock_modes
		FROM deadlock_processes dp
		JOIN deadlock_events de ON de.id = dp.event_id
		WHERE
			de.detected_at > datetime('now', ?)
			AND dp.relation_name IS NOT NULL
			AND dp.relation_name != ''
		GROUP BY dp.relation_name
		ORDER BY deadlock_count DESC
		LIMIT ?
	`

	rows, err := s.db.conn.QueryContext(ctx, query, daysArg, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []TableDeadlockStats
	for rows.Next() {
		var stat TableDeadlockStats
		var lastOccurrence string

		err := rows.Scan(
			&stat.RelationName,
			&stat.DeadlockCount,
			&lastOccurrence,
			&stat.LockModes,
		)
		if err != nil {
			return nil, err
		}

		stat.LastOccurrence, _ = time.Parse(time.RFC3339, lastOccurrence)
		if stat.LastOccurrence.IsZero() {
			stat.LastOccurrence, _ = time.Parse("2006-01-02 15:04:05", lastOccurrence)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// TableDeadlockStats represents deadlock statistics for a table.
type TableDeadlockStats struct {
	RelationName   string
	DeadlockCount  int
	LastOccurrence time.Time
	LockModes      string
}

// Cleanup removes deadlock records older than the retention period.
func (s *DeadlockStore) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).Format("2006-01-02 15:04:05")

	// Delete processes first (foreign key)
	_, err := s.db.conn.ExecContext(ctx, `
		DELETE FROM deadlock_processes
		WHERE event_id IN (
			SELECT id FROM deadlock_events WHERE detected_at < ?
		)
	`, cutoff)
	if err != nil {
		return 0, err
	}

	// Delete events
	result, err := s.db.conn.ExecContext(ctx, `
		DELETE FROM deadlock_events
		WHERE detected_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Count returns the total number of deadlock events.
func (s *DeadlockStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM deadlock_events").Scan(&count)
	return count, err
}

// Reset deletes all deadlock history (events and processes only).
func (s *DeadlockStore) Reset(ctx context.Context) error {
	logger.Info("DeadlockStore.Reset: starting DELETE FROM deadlock_processes")
	result, err := s.db.conn.ExecContext(ctx, "DELETE FROM deadlock_processes")
	if err != nil {
		logger.Error("DeadlockStore.Reset: failed to delete processes", "error", err)
		return err
	}
	rows1, _ := result.RowsAffected()
	logger.Info("DeadlockStore.Reset: deleted processes", "count", rows1)

	logger.Info("DeadlockStore.Reset: starting DELETE FROM deadlock_events")
	result, err = s.db.conn.ExecContext(ctx, "DELETE FROM deadlock_events")
	if err != nil {
		logger.Error("DeadlockStore.Reset: failed to delete events", "error", err)
		return err
	}
	rows2, _ := result.RowsAffected()
	logger.Info("DeadlockStore.Reset: deleted events", "count", rows2)

	return nil
}

// ResetLogPositions deletes deadlock-related log positions (json/csv files) so parsing starts fresh.
func (s *DeadlockStore) ResetLogPositions(ctx context.Context) error {
	logger.Info("DeadlockStore.ResetLogPositions: starting DELETE for deadlock positions")
	result, err := s.db.conn.ExecContext(ctx,
		"DELETE FROM log_positions WHERE file_path LIKE '%.json' OR file_path LIKE '%.csv'")
	if err != nil {
		logger.Error("DeadlockStore.ResetLogPositions: failed to delete positions", "error", err)
		return err
	}
	rows, _ := result.RowsAffected()
	logger.Info("DeadlockStore.ResetLogPositions: deleted positions", "count", rows)

	return nil
}

// GetLogPositions returns all saved log file positions.
func (s *DeadlockStore) GetLogPositions(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.conn.QueryContext(ctx, "SELECT file_path, position FROM log_positions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := make(map[string]int64)
	for rows.Next() {
		var filePath string
		var position int64
		if err := rows.Scan(&filePath, &position); err != nil {
			return nil, err
		}
		positions[filePath] = position
	}
	return positions, rows.Err()
}

// SaveLogPosition saves or updates a log file position.
func (s *DeadlockStore) SaveLogPosition(ctx context.Context, filePath string, position int64) error {
	_, err := s.db.conn.ExecContext(ctx, `
		INSERT INTO log_positions (file_path, position, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(file_path) DO UPDATE SET
			position = excluded.position,
			updated_at = CURRENT_TIMESTAMP
	`, filePath, position)
	return err
}

// MigrateDefaultInstance updates all records with instance_name='default' to use the specified instance name.
// This is used to migrate legacy data when transitioning to multi-instance support.
func (s *DeadlockStore) MigrateDefaultInstance(ctx context.Context, newInstanceName string) (int64, error) {
	if newInstanceName == "" || newInstanceName == "default" {
		return 0, nil // Nothing to migrate
	}

	result, err := s.db.conn.ExecContext(ctx, `
		UPDATE deadlock_events
		SET instance_name = ?
		WHERE instance_name = 'default'
	`, newInstanceName)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
