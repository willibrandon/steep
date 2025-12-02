package agent

import (
	"database/sql"
	"time"
)

// InstanceStatus represents the connection status of a monitored PostgreSQL instance.
type InstanceStatus string

const (
	InstanceStatusUnknown      InstanceStatus = "unknown"
	InstanceStatusConnected    InstanceStatus = "connected"
	InstanceStatusDisconnected InstanceStatus = "disconnected"
	InstanceStatusError        InstanceStatus = "error"
)

// AgentInstance represents metadata for a monitored PostgreSQL instance.
type AgentInstance struct {
	Name             string         `db:"name"`              // Instance identifier (e.g., "primary", "replica1")
	ConnectionString string         `db:"connection_string"` // PostgreSQL DSN (password excluded from storage)
	Status           InstanceStatus `db:"status"`            // Current connection status
	LastSeen         *time.Time     `db:"last_seen"`         // Last successful connection
	ErrorMessage     string         `db:"error_message"`     // Most recent error if status=error
}

// AgentInstanceStore manages agent instance persistence.
type AgentInstanceStore struct {
	db *sql.DB
}

// NewAgentInstanceStore creates a new agent instance store.
func NewAgentInstanceStore(db *sql.DB) *AgentInstanceStore {
	return &AgentInstanceStore{db: db}
}

// InitSchema creates the agent_instances table if it doesn't exist.
func (s *AgentInstanceStore) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_instances (
		name TEXT PRIMARY KEY,
		connection_string TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'unknown'
			CHECK (status IN ('connected', 'disconnected', 'error', 'unknown')),
		last_seen TIMESTAMP,
		error_message TEXT
	);`
	_, err := s.db.Exec(schema)
	return err
}

// Upsert inserts or updates an instance record.
func (s *AgentInstanceStore) Upsert(instance *AgentInstance) error {
	query := `
	INSERT INTO agent_instances (name, connection_string, status, last_seen, error_message)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		connection_string = excluded.connection_string,
		status = excluded.status,
		last_seen = excluded.last_seen,
		error_message = excluded.error_message`

	_, err := s.db.Exec(query,
		instance.Name,
		instance.ConnectionString,
		instance.Status,
		instance.LastSeen,
		instance.ErrorMessage,
	)
	return err
}

// UpdateStatus updates the status and optionally the error message for an instance.
func (s *AgentInstanceStore) UpdateStatus(name string, status InstanceStatus, errMsg string) error {
	var query string
	var args []interface{}

	if status == InstanceStatusConnected {
		// Set last_seen when connected
		query = `UPDATE agent_instances SET status = ?, last_seen = ?, error_message = '' WHERE name = ?`
		args = []interface{}{status, time.Now(), name}
	} else {
		query = `UPDATE agent_instances SET status = ?, error_message = ? WHERE name = ?`
		args = []interface{}{status, errMsg, name}
	}

	_, err := s.db.Exec(query, args...)
	return err
}

// Get retrieves an instance by name.
func (s *AgentInstanceStore) Get(name string) (*AgentInstance, error) {
	row := s.db.QueryRow(`
		SELECT name, connection_string, status,
		       last_seen,
		       COALESCE(error_message, '')
		FROM agent_instances WHERE name = ?`, name)

	var instance AgentInstance
	var lastSeen sql.NullTime
	err := row.Scan(
		&instance.Name,
		&instance.ConnectionString,
		&instance.Status,
		&lastSeen,
		&instance.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		instance.LastSeen = &lastSeen.Time
	}
	return &instance, nil
}

// List retrieves all instances.
func (s *AgentInstanceStore) List() ([]*AgentInstance, error) {
	rows, err := s.db.Query(`
		SELECT name, connection_string, status,
		       last_seen,
		       COALESCE(error_message, '')
		FROM agent_instances ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*AgentInstance
	for rows.Next() {
		var instance AgentInstance
		var lastSeen sql.NullTime
		err := rows.Scan(
			&instance.Name,
			&instance.ConnectionString,
			&instance.Status,
			&lastSeen,
			&instance.ErrorMessage,
		)
		if err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			instance.LastSeen = &lastSeen.Time
		}
		instances = append(instances, &instance)
	}
	return instances, rows.Err()
}

// Delete removes an instance by name.
func (s *AgentInstanceStore) Delete(name string) error {
	_, err := s.db.Exec(`DELETE FROM agent_instances WHERE name = ?`, name)
	return err
}

// DeleteAll removes all instances (called on clean shutdown or reconfiguration).
func (s *AgentInstanceStore) DeleteAll() error {
	_, err := s.db.Exec(`DELETE FROM agent_instances`)
	return err
}

// CountByStatus counts instances by status.
func (s *AgentInstanceStore) CountByStatus() (map[InstanceStatus]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM agent_instances GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[InstanceStatus]int)
	for rows.Next() {
		var status InstanceStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}
