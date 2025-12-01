package alerts

import (
	"time"
)

// StateChange represents a state transition for notification.
type StateChange struct {
	RuleName    string
	PrevState   AlertState
	NewState    AlertState
	MetricValue float64
	Threshold   float64
	Timestamp   time.Time
}

// State represents the current state of an alert rule (in-memory runtime state).
type State struct {
	// RuleName is a reference to the Rule.Name.
	RuleName string

	// CurrentState is the current alert severity (Normal, Warning, Critical).
	CurrentState AlertState

	// PreviousState is the state before the last transition.
	PreviousState AlertState

	// MetricValue is the current evaluated metric value.
	MetricValue float64

	// Threshold is the threshold that was crossed (warning or critical).
	Threshold float64

	// TriggeredAt is when the current state was entered.
	TriggeredAt time.Time

	// LastEvaluated is when the rule was last evaluated.
	LastEvaluated time.Time

	// Acknowledged indicates whether the current alert is acknowledged.
	Acknowledged bool

	// AcknowledgedAt is when the alert was acknowledged (if applicable).
	AcknowledgedAt time.Time
}

// NewState creates a new state for a rule in the Normal state.
func NewState(ruleName string) *State {
	now := time.Now()
	return &State{
		RuleName:      ruleName,
		CurrentState:  StateNormal,
		PreviousState: StateNormal,
		TriggeredAt:   now,
		LastEvaluated: now,
	}
}

// IsActive returns true if the alert is in Warning or Critical state.
func (s *State) IsActive() bool {
	return s.CurrentState.IsActive()
}

// Duration returns how long the alert has been in its current state.
func (s *State) Duration() time.Duration {
	return time.Since(s.TriggeredAt)
}

// Transition updates the state based on a new metric value and thresholds.
// Returns true if a state transition occurred.
func (s *State) Transition(value float64, rule *Rule) bool {
	s.MetricValue = value
	s.LastEvaluated = time.Now()

	newState := s.evaluateState(value, rule)

	if newState == s.CurrentState {
		return false
	}

	// State transition occurred
	s.PreviousState = s.CurrentState
	s.CurrentState = newState
	s.TriggeredAt = time.Now()

	// Update threshold based on new state
	switch newState {
	case StateCritical:
		s.Threshold = rule.Critical
	case StateWarning:
		s.Threshold = rule.Warning
	default:
		s.Threshold = 0
	}

	// Reset acknowledgment on new transition (new incident)
	if newState.IsActive() && !s.PreviousState.IsActive() {
		s.Acknowledged = false
		s.AcknowledgedAt = time.Time{}
	}

	return true
}

// evaluateState determines the alert state based on the metric value and thresholds.
func (s *State) evaluateState(value float64, rule *Rule) AlertState {
	// Check critical threshold first
	if rule.Operator.Compare(value, rule.Critical) {
		return StateCritical
	}

	// Check warning threshold
	if rule.Operator.Compare(value, rule.Warning) {
		return StateWarning
	}

	return StateNormal
}

// Acknowledge marks the alert as acknowledged.
func (s *State) Acknowledge() {
	s.Acknowledged = true
	s.AcknowledgedAt = time.Now()
}

// Unacknowledge removes the acknowledgment from the alert.
func (s *State) Unacknowledge() {
	s.Acknowledged = false
	s.AcknowledgedAt = time.Time{}
}
