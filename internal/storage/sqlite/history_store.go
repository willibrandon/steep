package sqlite

import (
	"database/sql"
	"strings"
	"time"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// HistoryEntry represents a query in the SQL Editor history.
type HistoryEntry struct {
	ID         int64
	SQL        string
	ExecutedAt time.Time
	DurationMs int64
	RowCount   int64
	Error      string
}

// HistoryStore provides access to SQL Editor query history.
type HistoryStore struct {
	db *DB
}

// NewHistoryStore creates a new history store.
func NewHistoryStore(db *DB) *HistoryStore {
	return &HistoryStore{db: db}
}

// fingerprint generates a fingerprint hash for a SQL query.
// Queries with the same structure but different literal values get the same fingerprint.
// Returns int64 for SQLite compatibility (signed 64-bit integer).
func fingerprint(sqlText string) int64 {
	normalized, err := pg_query.Normalize(sqlText)
	if err != nil {
		normalized = sqlText
	}
	return int64(pg_query.HashXXH3_64([]byte(normalized), 0))
}

// Add adds a query to history with shell-style deduplication.
// If a query with the same fingerprint exists, it updates the timestamp (moves to top).
// Otherwise, it inserts a new entry.
func (s *HistoryStore) Add(sqlText string, durationMs, rowCount int64, queryError string) error {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return nil
	}

	fp := fingerprint(sqlText)
	now := time.Now()

	// Shell-style history: if fingerprint exists, update it (move to top)
	// Otherwise insert new entry
	result, err := s.db.conn.Exec(`
		UPDATE query_history
		SET query = ?, executed_at = ?, duration_ms = ?, row_count = ?, error = ?
		WHERE fingerprint = ?
	`, sqlText, now, durationMs, rowCount, queryError, fp)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// No existing entry, insert new one
		_, err = s.db.conn.Exec(`
			INSERT INTO query_history (fingerprint, query, executed_at, duration_ms, row_count, error)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fp, sqlText, now, durationMs, rowCount, queryError)
		if err != nil {
			return err
		}
	}

	// Cleanup old entries (keep last 1000)
	_, _ = s.db.conn.Exec(`
		DELETE FROM query_history
		WHERE id NOT IN (
			SELECT id FROM query_history
			ORDER BY executed_at DESC
			LIMIT 1000
		)
	`)

	return nil
}

// GetRecent returns the most recent history entries.
func (s *HistoryStore) GetRecent(limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.conn.Query(`
		SELECT id, query, executed_at, duration_ms, row_count, error
		FROM query_history
		ORDER BY executed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.ID, &entry.SQL, &entry.ExecutedAt, &entry.DurationMs, &entry.RowCount, &entry.Error); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// Search returns entries matching the search query (case-insensitive).
func (s *HistoryStore) Search(query string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	if query == "" {
		rows, err = s.db.conn.Query(`
			SELECT id, query, executed_at, duration_ms, row_count, error
			FROM query_history
			ORDER BY executed_at DESC
			LIMIT ?
		`, limit)
	} else {
		rows, err = s.db.conn.Query(`
			SELECT id, query, executed_at, duration_ms, row_count, error
			FROM query_history
			WHERE query LIKE ?
			ORDER BY executed_at DESC
			LIMIT ?
		`, "%"+query+"%", limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.ID, &entry.SQL, &entry.ExecutedAt, &entry.DurationMs, &entry.RowCount, &entry.Error); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// Count returns the total number of history entries.
func (s *HistoryStore) Count() (int, error) {
	var count int
	err := s.db.conn.QueryRow("SELECT COUNT(*) FROM query_history").Scan(&count)
	return count, err
}
