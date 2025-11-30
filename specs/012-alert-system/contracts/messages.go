// Package contracts defines Bubbletea messages for the Alert System.
// This file documents the message types used for UI integration.
package contracts

import "time"

// AlertStateMsg carries current alert states to UI components.
// Sent by AlertEngine after each evaluation cycle.
type AlertStateMsg struct {
	// ActiveAlerts contains all currently firing alerts.
	ActiveAlerts []ActiveAlert

	// Changes contains state transitions from this evaluation.
	// Empty if no state changes occurred.
	Changes []StateChange

	// WarningCount is the number of alerts in Warning state.
	WarningCount int

	// CriticalCount is the number of alerts in Critical state.
	CriticalCount int

	// LastEvaluated is when the evaluation occurred.
	LastEvaluated time.Time

	// Error is set if evaluation failed.
	Error error
}

// AlertAcknowledgedMsg indicates an alert was acknowledged.
type AlertAcknowledgedMsg struct {
	RuleName string
	EventID  int64
	Error    error
}

// AlertHistoryMsg carries alert history for the history view.
type AlertHistoryMsg struct {
	Events []Event
	Error  error
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
