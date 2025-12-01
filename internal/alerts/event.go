package alerts

import (
	"time"
)

// Event represents a persisted state transition (stored in SQLite).
type Event struct {
	// ID is the auto-increment primary key.
	ID int64

	// RuleName is the rule that triggered the event.
	RuleName string

	// PrevState is the state before the transition.
	PrevState AlertState

	// NewState is the state after the transition.
	NewState AlertState

	// MetricValue is the value that triggered the transition.
	MetricValue float64

	// ThresholdValue is the threshold that was crossed.
	ThresholdValue float64

	// TriggeredAt is when the transition occurred.
	TriggeredAt time.Time

	// AcknowledgedAt is when the event was acknowledged (nil if not acknowledged).
	AcknowledgedAt *time.Time

	// AcknowledgedBy is who acknowledged the event (optional).
	AcknowledgedBy string
}

// NewEvent creates a new event from a state change.
func NewEvent(state *State) *Event {
	return &Event{
		RuleName:       state.RuleName,
		PrevState:      state.PreviousState,
		NewState:       state.CurrentState,
		MetricValue:    state.MetricValue,
		ThresholdValue: state.Threshold,
		TriggeredAt:    state.TriggeredAt,
	}
}

// IsAcknowledged returns true if the event has been acknowledged.
func (e *Event) IsAcknowledged() bool {
	return e.AcknowledgedAt != nil
}

// Acknowledge marks the event as acknowledged.
func (e *Event) Acknowledge(by string) {
	now := time.Now()
	e.AcknowledgedAt = &now
	e.AcknowledgedBy = by
}

// Unacknowledge removes the acknowledgment from the event.
func (e *Event) Unacknowledge() {
	e.AcknowledgedAt = nil
	e.AcknowledgedBy = ""
}

// StateTransition returns a human-readable description of the state change.
func (e *Event) StateTransition() string {
	return e.PrevState.String() + " → " + e.NewState.String()
}
