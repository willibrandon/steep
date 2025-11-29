package sqlite

import (
	"strings"
	"time"
)

// LogHistoryEntry represents a command or search in the Log Viewer history.
type LogHistoryEntry struct {
	ID         int64
	Content    string
	ExecutedAt time.Time
}

// LogHistoryStore provides access to Log Viewer command and search history.
type LogHistoryStore struct {
	db *DB
}

// NewLogHistoryStore creates a new log history store.
func NewLogHistoryStore(db *DB) *LogHistoryStore {
	return &LogHistoryStore{db: db}
}

// AddCommand adds a command to history with shell-style deduplication.
// If the command exists, it updates the timestamp (moves to top).
func (s *LogHistoryStore) AddCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	now := time.Now()

	// Shell-style history: if command exists, update it (move to top)
	// Otherwise insert new entry
	result, err := s.db.conn.Exec(`
		UPDATE log_command_history
		SET executed_at = ?
		WHERE command = ?
	`, now, command)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// No existing entry, insert new one
		_, err = s.db.conn.Exec(`
			INSERT INTO log_command_history (command, executed_at)
			VALUES (?, ?)
		`, command, now)
		if err != nil {
			return err
		}
	}

	// Cleanup old entries (keep last 500)
	_, _ = s.db.conn.Exec(`
		DELETE FROM log_command_history
		WHERE id NOT IN (
			SELECT id FROM log_command_history
			ORDER BY executed_at DESC
			LIMIT 500
		)
	`)

	return nil
}

// GetRecentCommands returns the most recent command history entries.
func (s *LogHistoryStore) GetRecentCommands(limit int) ([]LogHistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.conn.Query(`
		SELECT id, command, executed_at
		FROM log_command_history
		ORDER BY executed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogHistoryEntry
	for rows.Next() {
		var entry LogHistoryEntry
		if err := rows.Scan(&entry.ID, &entry.Content, &entry.ExecutedAt); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// AddSearch adds a search pattern to history with shell-style deduplication.
// If the pattern exists, it updates the timestamp (moves to top).
func (s *LogHistoryStore) AddSearch(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}

	now := time.Now()

	// Shell-style history: if pattern exists, update it (move to top)
	// Otherwise insert new entry
	result, err := s.db.conn.Exec(`
		UPDATE log_search_history
		SET executed_at = ?
		WHERE pattern = ?
	`, now, pattern)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// No existing entry, insert new one
		_, err = s.db.conn.Exec(`
			INSERT INTO log_search_history (pattern, executed_at)
			VALUES (?, ?)
		`, pattern, now)
		if err != nil {
			return err
		}
	}

	// Cleanup old entries (keep last 500)
	_, _ = s.db.conn.Exec(`
		DELETE FROM log_search_history
		WHERE id NOT IN (
			SELECT id FROM log_search_history
			ORDER BY executed_at DESC
			LIMIT 500
		)
	`)

	return nil
}

// GetRecentSearches returns the most recent search history entries.
func (s *LogHistoryStore) GetRecentSearches(limit int) ([]LogHistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.conn.Query(`
		SELECT id, pattern, executed_at
		FROM log_search_history
		ORDER BY executed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogHistoryEntry
	for rows.Next() {
		var entry LogHistoryEntry
		if err := rows.Scan(&entry.ID, &entry.Content, &entry.ExecutedAt); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}
