// Package ui provides Bubbletea TUI components for Steep.
package ui

import (
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// Data messages (from monitors to UI)

// ActivityDataMsg contains activity data from the monitor goroutine.
type ActivityDataMsg struct {
	Connections []models.Connection
	TotalCount  int
	FetchedAt   time.Time
	Error       error
}

// MetricsDataMsg contains metrics data from the monitor goroutine.
type MetricsDataMsg struct {
	Metrics   models.Metrics
	FetchedAt time.Time
	Error     error
}

// Command messages (UI to monitors)

// RefreshActivityCmd requests fresh activity data with filters.
type RefreshActivityCmd struct {
	Filter models.ActivityFilter
	Limit  int
	Offset int
}

// RefreshMetricsCmd requests fresh metrics data.
type RefreshMetricsCmd struct{}

// Action messages

// CancelQueryMsg requests cancellation of a running query.
type CancelQueryMsg struct {
	PID int
}

// CancelQueryResultMsg contains the result of a cancel attempt.
type CancelQueryResultMsg struct {
	PID     int
	Success bool
	Error   error
}

// TerminateConnectionMsg requests termination of a connection.
type TerminateConnectionMsg struct {
	PID int
}

// TerminateConnectionResultMsg contains the result of a terminate attempt.
type TerminateConnectionResultMsg struct {
	PID     int
	Success bool
	Error   error
}

// UI state messages

// TickMsg triggers periodic refresh.
type TickMsg time.Time

// ShowDialogMsg requests display of a confirmation dialog.
type ShowDialogMsg struct {
	Action    string // "cancel" or "terminate"
	TargetPID int
	Query     string // Truncated query for display
}

// DialogResponseMsg contains user response to a dialog.
type DialogResponseMsg struct {
	Confirmed bool
}

// FilterChangedMsg indicates user changed filter settings.
type FilterChangedMsg struct {
	Filter models.ActivityFilter
}

// SortChangedMsg indicates user changed sort column.
type SortChangedMsg struct {
	Column    string
	Ascending bool
}

// ConnectionErrorMsg indicates database connection lost.
type ConnectionErrorMsg struct {
	Error   error
	Attempt int
}

// ConnectionRestoredMsg indicates database connection restored.
type ConnectionRestoredMsg struct{}

// WindowTooSmallMsg indicates terminal is below minimum size.
type WindowTooSmallMsg struct {
	Width  int
	Height int
}
