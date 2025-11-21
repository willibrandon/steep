// Package models defines data structures for PostgreSQL monitoring.
package models

import (
	"fmt"
	"time"
)

// ConnectionState represents the state of a PostgreSQL backend process.
type ConnectionState string

const (
	StateActive                   ConnectionState = "active"
	StateIdle                     ConnectionState = "idle"
	StateIdleInTransaction        ConnectionState = "idle in transaction"
	StateIdleInTransactionAborted ConnectionState = "idle in transaction (aborted)"
	StateFastpath                 ConnectionState = "fastpath function call"
	StateDisabled                 ConnectionState = "disabled"
)

// Connection represents a PostgreSQL backend process from pg_stat_activity.
type Connection struct {
	PID             int             `json:"pid"`
	User            string          `json:"user"`
	Database        string          `json:"database"`
	State           ConnectionState `json:"state"`
	DurationSeconds int             `json:"duration_seconds"`
	Query           string          `json:"query"`
	ClientAddr      string          `json:"client_addr"`
	ApplicationName string          `json:"application_name"`
	WaitEventType   string          `json:"wait_event_type"`
	WaitEvent       string          `json:"wait_event"`
	QueryStart      time.Time       `json:"query_start"`
	BackendStart    time.Time       `json:"backend_start"`
}

// IsActive returns true if the connection is actively executing a query.
func (c *Connection) IsActive() bool {
	return c.State == StateActive
}

// IsIdle returns true if the connection is idle.
func (c *Connection) IsIdle() bool {
	return c.State == StateIdle
}

// IsInTransaction returns true if the connection is idle in a transaction.
func (c *Connection) IsInTransaction() bool {
	return c.State == StateIdleInTransaction || c.State == StateIdleInTransactionAborted
}

// FormatDuration returns the duration as HH:MM:SS string.
// Uses DurationSeconds calculated server-side for synchronized display.
func (c *Connection) FormatDuration() string {
	if c.DurationSeconds <= 0 {
		return "00:00:00"
	}
	hours := c.DurationSeconds / 3600
	minutes := (c.DurationSeconds % 3600) / 60
	seconds := c.DurationSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// TruncateQuery returns the query truncated to maxLen with "..." suffix.
func (c *Connection) TruncateQuery(maxLen int) string {
	if len(c.Query) <= maxLen {
		return c.Query
	}
	if maxLen <= 3 {
		return c.Query[:maxLen]
	}
	return c.Query[:maxLen-3] + "..."
}
