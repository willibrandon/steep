package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

var debugLog *os.File

func init() {
	debugLog, _ = os.OpenFile("/tmp/steep_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

func debugf(format string, args ...interface{}) {
	if debugLog != nil {
		fmt.Fprintf(debugLog, format+"\n", args...)
		debugLog.Sync()
	}
}

// DeadlockEvent represents a single deadlock incident.
type DeadlockEvent struct {
	ID              int64
	DetectedAt      time.Time
	DatabaseName    string
	ResolvedByPID   *int
	DetectionTimeMs *int
	CreatedAt       time.Time
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

	// Insert event
	result, err := tx.ExecContext(ctx, `
		INSERT INTO deadlock_events (detected_at, database_name, resolved_by_pid, detection_time_ms)
		VALUES (?, ?, ?, ?)
	`, event.DetectedAt.Format("2006-01-02 15:04:05"), event.DatabaseName, event.ResolvedByPID, event.DetectionTimeMs)
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
func (s *DeadlockStore) GetRecentEvents(ctx context.Context, days int, limit int) ([]DeadlockSummary, error) {
	query := `
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

	daysArg := fmt.Sprintf("-%d days", days)

	debugf("GetRecentEvents: days=%d limit=%d daysArg=%s", days, limit, daysArg)

	rows, err := s.db.conn.QueryContext(ctx, query, daysArg, limit)
	if err != nil {
		debugf("GetRecentEvents query error: %v", err)
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
			debugf("GetRecentEvents scan error: %v", err)
			return nil, err
		}

		debugf("row: ID=%d detectedAt=%q db=%s procs=%d", summary.ID, detectedAt, summary.DatabaseName, summary.ProcessCount)
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

	debugf("GetRecentEvents: returning %d summaries", len(summaries))
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

	event.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
	event.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)

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
			t, _ := time.Parse("2006-01-02 15:04:05", backendStart.String)
			proc.BackendStart = &t
		}
		if xactStart.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", xactStart.String)
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

		stat.LastOccurrence, _ = time.Parse("2006-01-02 15:04:05", lastOccurrence)
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

// Reset deletes all deadlock history.
func (s *DeadlockStore) Reset(ctx context.Context) error {
	_, err := s.db.conn.ExecContext(ctx, "DELETE FROM deadlock_processes")
	if err != nil {
		return err
	}
	_, err = s.db.conn.ExecContext(ctx, "DELETE FROM deadlock_events")
	return err
}
