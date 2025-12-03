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

// ReconnectAttemptMsg is sent when attempting to reconnect to the database
type ReconnectAttemptMsg struct {
	Attempt     int
	MaxAttempts int
	NextDelay   time.Duration
}

// ReconnectSuccessMsg is sent when reconnection succeeds
type ReconnectSuccessMsg struct {
	Pool    *pgxpool.Pool
	Version string
}

// ReconnectFailedMsg is sent when all reconnection attempts are exhausted
type ReconnectFailedMsg struct {
	Err error
}

// InstanceConnectedMsg is sent when an additional instance connection is established
type InstanceConnectedMsg struct {
	Name string
	Pool *pgxpool.Pool
}

// InstanceConnectionFailedMsg is sent when an additional instance connection fails
type InstanceConnectionFailedMsg struct {
	Name string
	Err  error
}

// dataTickMsg triggers synchronized fetch of all data
type dataTickMsg struct{}
