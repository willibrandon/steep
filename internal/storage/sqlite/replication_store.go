package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// ReplicationStore provides persistence for replication lag history.
type ReplicationStore struct {
	db *DB
}

// NewReplicationStore creates a new ReplicationStore.
func NewReplicationStore(db *DB) *ReplicationStore {
	return &ReplicationStore{db: db}
}

// SaveLagEntry inserts a new lag history entry.
func (s *ReplicationStore) SaveLagEntry(ctx context.Context, entry models.LagHistoryEntry) error {
	_, err := s.db.conn.ExecContext(ctx, `
		INSERT INTO replication_lag_history (
			timestamp, replica_name, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			byte_lag, time_lag_ms, sync_state, direction, conflict_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.Timestamp.Format("2006-01-02 15:04:05"),
		entry.ReplicaName,
		entry.SentLSN,
		entry.WriteLSN,
		entry.FlushLSN,
		entry.ReplayLSN,
		entry.ByteLag,
		entry.TimeLagMs,
		entry.SyncState,
		entry.Direction,
		entry.ConflictCount,
	)
	if err != nil {
		return fmt.Errorf("insert lag entry: %w", err)
	}
	return nil
}

// SaveLagEntries inserts multiple lag history entries in a single transaction.
func (s *ReplicationStore) SaveLagEntries(ctx context.Context, entries []models.LagHistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO replication_lag_history (
			timestamp, replica_name, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			byte_lag, time_lag_ms, sync_state, direction, conflict_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		_, err := stmt.ExecContext(ctx,
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.ReplicaName,
			entry.SentLSN,
			entry.WriteLSN,
			entry.FlushLSN,
			entry.ReplayLSN,
			entry.ByteLag,
			entry.TimeLagMs,
			entry.SyncState,
			entry.Direction,
			entry.ConflictCount,
		)
		if err != nil {
			return fmt.Errorf("insert entry for %s: %w", entry.ReplicaName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetLagHistory retrieves lag history entries for a replica since a given time.
// Uses idx_lag_history_replica index (replica_name, timestamp) for efficient queries.
func (s *ReplicationStore) GetLagHistory(ctx context.Context, replicaName string, since time.Time) ([]models.LagHistoryEntry, error) {
	query := `
		SELECT
			id, timestamp, replica_name, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			byte_lag, time_lag_ms, sync_state, direction, conflict_count
		FROM replication_lag_history
		WHERE replica_name = ? AND timestamp >= ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.conn.QueryContext(ctx, query, replicaName, since.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("query lag history: %w", err)
	}
	defer rows.Close()

	var entries []models.LagHistoryEntry
	for rows.Next() {
		var entry models.LagHistoryEntry
		var timestampStr string
		var sentLSN, writeLSN, flushLSN, replayLSN, syncState, direction sql.NullString
		var byteLag, timeLagMs sql.NullInt64
		var conflictCount sql.NullInt64

		err := rows.Scan(
			&entry.ID,
			&timestampStr,
			&entry.ReplicaName,
			&sentLSN,
			&writeLSN,
			&flushLSN,
			&replayLSN,
			&byteLag,
			&timeLagMs,
			&syncState,
			&direction,
			&conflictCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan lag entry: %w", err)
		}

		// Parse timestamp - try RFC3339 first, then standard format
		entry.Timestamp, _ = time.Parse(time.RFC3339, timestampStr)
		if entry.Timestamp.IsZero() {
			entry.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
		}

		if sentLSN.Valid {
			entry.SentLSN = sentLSN.String
		}
		if writeLSN.Valid {
			entry.WriteLSN = writeLSN.String
		}
		if flushLSN.Valid {
			entry.FlushLSN = flushLSN.String
		}
		if replayLSN.Valid {
			entry.ReplayLSN = replayLSN.String
		}
		if byteLag.Valid {
			entry.ByteLag = byteLag.Int64
		}
		if timeLagMs.Valid {
			entry.TimeLagMs = timeLagMs.Int64
		}
		if syncState.Valid {
			entry.SyncState = syncState.String
		}
		if direction.Valid {
			entry.Direction = direction.String
		}
		if conflictCount.Valid {
			entry.ConflictCount = int(conflictCount.Int64)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lag entries: %w", err)
	}

	return entries, nil
}

// GetLagHistoryForAllReplicas retrieves lag history for all replicas since a given time.
// Uses idx_lag_history_time index (timestamp, replica_name) for efficient time-range queries.
// Limited to 10000 entries to prevent memory issues with very long time ranges.
func (s *ReplicationStore) GetLagHistoryForAllReplicas(ctx context.Context, since time.Time) (map[string][]models.LagHistoryEntry, error) {
	query := `
		SELECT
			id, timestamp, replica_name, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			byte_lag, time_lag_ms, sync_state, direction, conflict_count
		FROM replication_lag_history
		WHERE timestamp >= ?
		ORDER BY replica_name, timestamp ASC
		LIMIT 10000
	`

	rows, err := s.db.conn.QueryContext(ctx, query, since.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("query lag history: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]models.LagHistoryEntry)
	for rows.Next() {
		var entry models.LagHistoryEntry
		var timestampStr string
		var sentLSN, writeLSN, flushLSN, replayLSN, syncState, direction sql.NullString
		var byteLag, timeLagMs sql.NullInt64
		var conflictCount sql.NullInt64

		err := rows.Scan(
			&entry.ID,
			&timestampStr,
			&entry.ReplicaName,
			&sentLSN,
			&writeLSN,
			&flushLSN,
			&replayLSN,
			&byteLag,
			&timeLagMs,
			&syncState,
			&direction,
			&conflictCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan lag entry: %w", err)
		}

		entry.Timestamp, _ = time.Parse(time.RFC3339, timestampStr)
		if entry.Timestamp.IsZero() {
			entry.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
		}

		if sentLSN.Valid {
			entry.SentLSN = sentLSN.String
		}
		if writeLSN.Valid {
			entry.WriteLSN = writeLSN.String
		}
		if flushLSN.Valid {
			entry.FlushLSN = flushLSN.String
		}
		if replayLSN.Valid {
			entry.ReplayLSN = replayLSN.String
		}
		if byteLag.Valid {
			entry.ByteLag = byteLag.Int64
		}
		if timeLagMs.Valid {
			entry.TimeLagMs = timeLagMs.Int64
		}
		if syncState.Valid {
			entry.SyncState = syncState.String
		}
		if direction.Valid {
			entry.Direction = direction.String
		}
		if conflictCount.Valid {
			entry.ConflictCount = int(conflictCount.Int64)
		}

		result[entry.ReplicaName] = append(result[entry.ReplicaName], entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lag entries: %w", err)
	}

	return result, nil
}

// GetLagValues retrieves just the byte lag values as float64 for sparkline rendering.
// Uses idx_lag_history_replica index (replica_name, timestamp) for efficient queries.
func (s *ReplicationStore) GetLagValues(ctx context.Context, replicaName string, since time.Time, limit int) ([]float64, error) {
	query := `
		SELECT byte_lag
		FROM replication_lag_history
		WHERE replica_name = ? AND timestamp >= ?
		ORDER BY timestamp ASC
		LIMIT ?
	`

	rows, err := s.db.conn.QueryContext(ctx, query, replicaName, since.Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, fmt.Errorf("query lag values: %w", err)
	}
	defer rows.Close()

	var values []float64
	for rows.Next() {
		var byteLag sql.NullInt64
		if err := rows.Scan(&byteLag); err != nil {
			return nil, fmt.Errorf("scan lag value: %w", err)
		}
		if byteLag.Valid {
			values = append(values, float64(byteLag.Int64))
		} else {
			values = append(values, 0)
		}
	}

	return values, rows.Err()
}

// PruneLagHistory removes lag history entries older than the retention period.
// Returns the number of rows deleted.
func (s *ReplicationStore) PruneLagHistory(ctx context.Context, retentionHours int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour).Format("2006-01-02 15:04:05")

	result, err := s.db.conn.ExecContext(ctx, `
		DELETE FROM replication_lag_history
		WHERE timestamp < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune lag history: %w", err)
	}

	return result.RowsAffected()
}

// Count returns the total number of lag history entries.
func (s *ReplicationStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM replication_lag_history").Scan(&count)
	return count, err
}

// Reset deletes all lag history data.
func (s *ReplicationStore) Reset(ctx context.Context) error {
	_, err := s.db.conn.ExecContext(ctx, "DELETE FROM replication_lag_history")
	return err
}

// GetDistinctReplicas returns all replica names that have lag history.
func (s *ReplicationStore) GetDistinctReplicas(ctx context.Context) ([]string, error) {
	rows, err := s.db.conn.QueryContext(ctx, `
		SELECT DISTINCT replica_name
		FROM replication_lag_history
		ORDER BY replica_name
	`)
	if err != nil {
		return nil, fmt.Errorf("query distinct replicas: %w", err)
	}
	defer rows.Close()

	var replicas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan replica name: %w", err)
		}
		replicas = append(replicas, name)
	}

	return replicas, rows.Err()
}

// GetLatestEntry returns the most recent lag entry for a replica.
func (s *ReplicationStore) GetLatestEntry(ctx context.Context, replicaName string) (*models.LagHistoryEntry, error) {
	query := `
		SELECT
			id, timestamp, replica_name, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			byte_lag, time_lag_ms, sync_state, direction, conflict_count
		FROM replication_lag_history
		WHERE replica_name = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var entry models.LagHistoryEntry
	var timestampStr string
	var sentLSN, writeLSN, flushLSN, replayLSN, syncState, direction sql.NullString
	var byteLag, timeLagMs sql.NullInt64
	var conflictCount sql.NullInt64

	err := s.db.conn.QueryRowContext(ctx, query, replicaName).Scan(
		&entry.ID,
		&timestampStr,
		&entry.ReplicaName,
		&sentLSN,
		&writeLSN,
		&flushLSN,
		&replayLSN,
		&byteLag,
		&timeLagMs,
		&syncState,
		&direction,
		&conflictCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest entry: %w", err)
	}

	entry.Timestamp, _ = time.Parse(time.RFC3339, timestampStr)
	if entry.Timestamp.IsZero() {
		entry.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
	}

	if sentLSN.Valid {
		entry.SentLSN = sentLSN.String
	}
	if writeLSN.Valid {
		entry.WriteLSN = writeLSN.String
	}
	if flushLSN.Valid {
		entry.FlushLSN = flushLSN.String
	}
	if replayLSN.Valid {
		entry.ReplayLSN = replayLSN.String
	}
	if byteLag.Valid {
		entry.ByteLag = byteLag.Int64
	}
	if timeLagMs.Valid {
		entry.TimeLagMs = timeLagMs.Int64
	}
	if syncState.Valid {
		entry.SyncState = syncState.String
	}
	if direction.Valid {
		entry.Direction = direction.String
	}
	if conflictCount.Valid {
		entry.ConflictCount = int(conflictCount.Int64)
	}

	return &entry, nil
}
