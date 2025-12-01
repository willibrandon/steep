package alerts

import (
	"fmt"
	"time"
)

// ActiveAlert represents a currently firing alert for UI display.
type ActiveAlert struct {
	// RuleName is the rule identifier.
	RuleName string

	// State is the current alert state (Warning or Critical).
	State AlertState

	// MetricValue is the current metric value.
	MetricValue float64

	// Threshold is the threshold that was crossed.
	Threshold float64

	// TriggeredAt is when the alert started.
	TriggeredAt time.Time

	// Duration is how long the alert has been active.
	Duration time.Duration

	// Acknowledged indicates whether the alert is acknowledged.
	Acknowledged bool

	// Message is the formatted alert message.
	Message string
}

// NewActiveAlert creates an ActiveAlert from an alert state.
func NewActiveAlert(state *State, rule *Rule) *ActiveAlert {
	msg := state.RuleName
	if rule.Message != "" {
		msg = formatMessage(rule.Message, state.MetricValue, state.Threshold)
	}

	return &ActiveAlert{
		RuleName:     state.RuleName,
		State:        state.CurrentState,
		MetricValue:  state.MetricValue,
		Threshold:    state.Threshold,
		TriggeredAt:  state.TriggeredAt,
		Duration:     state.Duration(),
		Acknowledged: state.Acknowledged,
		Message:      msg,
	}
}

// formatMessage replaces placeholders in the message template.
func formatMessage(template string, value, threshold float64) string {
	// Simple placeholder replacement
	// {value} -> current value
	// {threshold} -> threshold that was crossed
	msg := template
	// Use simple string replacement for now
	// More sophisticated templating can be added later if needed
	return fmt.Sprintf("%s (value: %.2f, threshold: %.2f)", msg, value, threshold)
}

// IsCritical returns true if the alert is in Critical state.
func (a *ActiveAlert) IsCritical() bool {
	return a.State == StateCritical
}

// IsWarning returns true if the alert is in Warning state.
func (a *ActiveAlert) IsWarning() bool {
	return a.State == StateWarning
}

// DurationString returns a human-readable duration string.
func (a *ActiveAlert) DurationString() string {
	d := a.Duration
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
