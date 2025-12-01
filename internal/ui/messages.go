// Package ui provides Bubbletea TUI components for Steep.
package ui

import (
	"time"

	"github.com/willibrandon/steep/internal/alerts"
	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/storage/sqlite"
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

// RefreshRequestMsg requests manual data refresh.
type RefreshRequestMsg struct{}

// WindowTooSmallMsg indicates terminal is below minimum size.
type WindowTooSmallMsg struct {
	Width  int
	Height int
}

// LocksDataMsg contains lock data from the monitor goroutine.
type LocksDataMsg struct {
	Data      *models.LocksData
	FetchedAt time.Time
	Error     error
}

// KillQueryResultMsg contains the result of a kill query attempt.
type KillQueryResultMsg struct {
	PID     int
	Success bool
	Error   error
}

// DeadlockHistoryMsg contains deadlock history data.
type DeadlockHistoryMsg struct {
	Deadlocks []sqlite.DeadlockSummary
	Enabled   bool
	Error     error
}

// DeadlockScanProgressMsg reports file scanning progress.
type DeadlockScanProgressMsg struct {
	CurrentFile int
	TotalFiles  int
}

// DeadlockDetailMsg contains a single deadlock event with full details.
type DeadlockDetailMsg struct {
	Event *sqlite.DeadlockEvent
	Error error
}

// ResetDeadlocksMsg is sent to request resetting deadlock history.
type ResetDeadlocksMsg struct{}

// ResetDeadlocksResultMsg contains the result of resetting deadlock history.
type ResetDeadlocksResultMsg struct {
	Success bool
	Error   error
}

// ResetLogPositionsMsg is sent to request resetting log positions.
type ResetLogPositionsMsg struct{}

// ResetLogPositionsResultMsg contains the result of resetting log positions.
type ResetLogPositionsResultMsg struct {
	Success bool
	Error   error
}

// ReplicationDataMsg contains replication data from the monitor goroutine.
type ReplicationDataMsg struct {
	Data      *models.ReplicationData
	FetchedAt time.Time
	Error     error
}

// DropSlotRequestMsg requests dropping a replication slot.
type DropSlotRequestMsg struct {
	SlotName string
}

// DropSlotResultMsg contains the result of dropping a replication slot.
type DropSlotResultMsg struct {
	SlotName string
	Success  bool
	Error    error
}

// WizardExecResultMsg contains the result of executing a wizard command.
type WizardExecResultMsg struct {
	Command string
	Label   string
	Success bool
	Error   error
}

// WizardExecRequestMsg requests execution of a wizard command.
type WizardExecRequestMsg struct {
	Command string
	Label   string
}

// LagHistoryRequestMsg requests lag history data for a given time window.
type LagHistoryRequestMsg struct {
	Window time.Duration
}

// LagHistoryResponseMsg contains lag history data from SQLite.
type LagHistoryResponseMsg struct {
	LagHistory map[string][]float64
	Window     time.Duration
	Error      error
}

// TablesRequestMsg requests table list for the logical wizard.
type TablesRequestMsg struct{}

// TablesResponseMsg contains table list for the logical wizard.
type TablesResponseMsg struct {
	Tables []models.Table
	Error  error
}

// ConnTestRequestMsg requests a connection test for the connection string builder.
type ConnTestRequestMsg struct {
	ConnString string
}

// ConnTestResponseMsg contains the result of a connection test.
type ConnTestResponseMsg struct {
	Success bool
	Message string
	Error   error
}

// CreateReplicationUserMsg requests creation of a replication user.
type CreateReplicationUserMsg struct {
	Username string
	Password string
}

// CreateReplicationUserResultMsg contains the result of user creation.
type CreateReplicationUserResultMsg struct {
	Success  bool
	Username string
	Error    error
}

// AlterSystemRequestMsg requests execution of ALTER SYSTEM commands.
type AlterSystemRequestMsg struct {
	Commands []string
}

// AlterSystemResultMsg contains the result of ALTER SYSTEM execution.
type AlterSystemResultMsg struct {
	Success  bool
	Commands []string
	Error    error
}

// Configuration write messages

// SetConfigMsg requests changing a configuration parameter via ALTER SYSTEM.
type SetConfigMsg struct {
	Parameter string
	Value     string
	Context   string // Parameter context for warning messages
}

// SetConfigResultMsg contains the result of a configuration change.
type SetConfigResultMsg struct {
	Parameter string
	Value     string
	Context   string // Parameter context for appropriate messaging
	Success   bool
	Error     error
}

// ResetConfigMsg requests resetting a configuration parameter to default.
type ResetConfigMsg struct {
	Parameter string
	Context   string
}

// ResetConfigResultMsg contains the result of a configuration reset.
type ResetConfigResultMsg struct {
	Parameter string
	Context   string
	Success   bool
	Error     error
}

// ReloadConfigMsg requests reloading PostgreSQL configuration via pg_reload_conf().
type ReloadConfigMsg struct{}

// ReloadConfigResultMsg contains the result of a configuration reload.
type ReloadConfigResultMsg struct {
	Success bool
	Error   error
}

// Alert system messages

// AlertStateMsg carries current alert states to UI components.
// Sent by AlertEngine after each evaluation cycle.
type AlertStateMsg struct {
	// ActiveAlerts contains all currently firing alerts.
	ActiveAlerts []alerts.ActiveAlert

	// Changes contains state transitions from this evaluation.
	// Empty if no state changes occurred.
	Changes []alerts.StateChange

	// WarningCount is the number of alerts in Warning state.
	WarningCount int

	// CriticalCount is the number of alerts in Critical state.
	CriticalCount int

	// LastEvaluated is when the evaluation occurred.
	LastEvaluated time.Time

	// Error is set if evaluation failed.
	Error error
}

// HasActiveAlerts returns true if any alerts are firing.
func (m AlertStateMsg) HasActiveAlerts() bool {
	return len(m.ActiveAlerts) > 0
}

// HasCritical returns true if any critical alerts are firing.
func (m AlertStateMsg) HasCritical() bool {
	return m.CriticalCount > 0
}

// HasChanges returns true if any state changes occurred.
func (m AlertStateMsg) HasChanges() bool {
	return len(m.Changes) > 0
}

// AlertHistoryMsg carries alert history for the history view.
type AlertHistoryMsg struct {
	Events []alerts.Event
	Error  error
}

// AlertAcknowledgedMsg indicates an alert was acknowledged.
type AlertAcknowledgedMsg struct {
	RuleName string
	EventID  int64
	Error    error
}
