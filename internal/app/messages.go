package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseConnectedMsg is sent when the database connection is successfully established
type DatabaseConnectedMsg struct {
	Pool    *pgxpool.Pool
	Version string
}

// ConnectionFailedMsg is sent when the database connection fails
type ConnectionFailedMsg struct {
	Err error
}

// ReconnectAttemptMsg is sent when attempting to reconnect to the database
type ReconnectAttemptMsg struct {
	Attempt int
	MaxAttempts int
}

// StatusBarTickMsg is sent periodically to update the status bar
type StatusBarTickMsg struct {
	Timestamp time.Time
}

// MetricsUpdateMsg is sent when database metrics are updated
type MetricsUpdateMsg struct {
	ActiveConnections int
	TotalConnections  int
}

// ErrorMsg is sent when a general error occurs
type ErrorMsg struct {
	Err error
}
