package agent

import (
	"database/sql"
	"time"
)

// AgentStatus represents the running agent's state for TUI detection and health monitoring.
// This is a singleton table (id=1 always).
type AgentStatus struct {
	ID          int       `db:"id"`           // Always 1 (singleton)
	PID         int       `db:"pid"`          // Agent process ID
	StartTime   time.Time `db:"start_time"`   // Agent start time
	LastCollect time.Time `db:"last_collect"` // Last successful collection
	Version     string    `db:"version"`      // Agent version string
	ConfigHash  string    `db:"config_hash"`  // Hash of config for drift detection
	ErrorCount  int       `db:"error_count"`  // Total collection errors
	LastError   string    `db:"last_error"`   // Most recent error message
}

// AgentStatusStore manages agent status persistence.
type AgentStatusStore struct {
	db *sql.DB
}

// NewAgentStatusStore creates a new agent status store.
func NewAgentStatusStore(db *sql.DB) *AgentStatusStore {
	return &AgentStatusStore{db: db}
}

// InitSchema creates the agent_status table if it doesn't exist.
func (s *AgentStatusStore) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_status (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		pid INTEGER NOT NULL,
		start_time TIMESTAMP NOT NULL,
		last_collect TIMESTAMP NOT NULL,
		version TEXT NOT NULL,
		config_hash TEXT,
		error_count INTEGER NOT NULL DEFAULT 0,
		last_error TEXT
	);`
	_, err := s.db.Exec(schema)
	return err
}

// Upsert inserts or updates the agent status (singleton row).
func (s *AgentStatusStore) Upsert(status *AgentStatus) error {
	query := `
	INSERT INTO agent_status (id, pid, start_time, last_collect, version, config_hash, error_count, last_error)
	VALUES (1, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		pid = excluded.pid,
		start_time = excluded.start_time,
		last_collect = excluded.last_collect,
		version = excluded.version,
		config_hash = excluded.config_hash,
		error_count = excluded.error_count,
		last_error = excluded.last_error`

	_, err := s.db.Exec(query,
		status.PID,
		status.StartTime,
		status.LastCollect,
		status.Version,
		status.ConfigHash,
		status.ErrorCount,
		status.LastError,
	)
	return err
}

// UpdateLastCollect updates only the last_collect timestamp.
func (s *AgentStatusStore) UpdateLastCollect(timestamp time.Time) error {
	_, err := s.db.Exec(`UPDATE agent_status SET last_collect = ? WHERE id = 1`, timestamp)
	return err
}

// IncrementErrorCount increments error count and sets last error message.
func (s *AgentStatusStore) IncrementErrorCount(errMsg string) error {
	_, err := s.db.Exec(`
		UPDATE agent_status
		SET error_count = error_count + 1, last_error = ?
		WHERE id = 1`, errMsg)
	return err
}

// Get retrieves the current agent status.
func (s *AgentStatusStore) Get() (*AgentStatus, error) {
	row := s.db.QueryRow(`
		SELECT id, pid, start_time, last_collect, version,
		       COALESCE(config_hash, ''),
		       COALESCE(error_count, 0),
		       COALESCE(last_error, '')
		FROM agent_status WHERE id = 1`)

	var status AgentStatus
	err := row.Scan(
		&status.ID,
		&status.PID,
		&status.StartTime,
		&status.LastCollect,
		&status.Version,
		&status.ConfigHash,
		&status.ErrorCount,
		&status.LastError,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &status, nil
}

// Delete removes the agent status row (called on clean shutdown).
func (s *AgentStatusStore) Delete() error {
	_, err := s.db.Exec(`DELETE FROM agent_status WHERE id = 1`)
	return err
}

// IsHealthy checks if the agent is healthy based on last_collect freshness.
// The agent is considered healthy if last_collect is within 2x the given interval.
func (s *AgentStatusStore) IsHealthy(maxStaleness time.Duration) (bool, *AgentStatus, error) {
	status, err := s.Get()
	if err != nil {
		return false, nil, err
	}
	if status == nil {
		return false, nil, nil
	}

	staleness := time.Since(status.LastCollect)
	isHealthy := staleness <= maxStaleness

	return isHealthy, status, nil
}
