// Package contracts defines interfaces for the Alert System.
// This file is documentation-only and will be implemented in internal/alerts/.
package contracts

import (
	"context"
	"time"
)

// AlertState represents the severity level of an alert.
type AlertState string

const (
	AlertStateNormal   AlertState = "normal"
	AlertStateWarning  AlertState = "warning"
	AlertStateCritical AlertState = "critical"
)

// Operator defines comparison operators for alert thresholds.
type Operator string

const (
	OpGreaterThan    Operator = ">"
	OpLessThan       Operator = "<"
	OpGreaterOrEqual Operator = ">="
	OpLessOrEqual    Operator = "<="
	OpEqual          Operator = "=="
	OpNotEqual       Operator = "!="
)

// Rule represents a configured alert rule.
type Rule struct {
	Name       string   // Unique identifier
	Metric     string   // Metric expression to evaluate
	Operator   Operator // Comparison operator
	Warning    float64  // Warning threshold
	Critical   float64  // Critical threshold
	Enabled    bool     // Whether rule is active
	Message    string   // Optional custom message template
}

// State represents the current state of an alert rule.
type State struct {
	RuleName      string
	CurrentState  AlertState
	PreviousState AlertState
	MetricValue   float64
	TriggeredAt   time.Time
	LastEvaluated time.Time
	Acknowledged  bool
	AcknowledgedAt time.Time
}

// Event represents a persisted state transition.
type Event struct {
	ID             int64
	RuleName       string
	PrevState      AlertState
	NewState       AlertState
	MetricValue    float64
	ThresholdValue float64
	TriggeredAt    time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy string
}

// StateChange represents a state transition for notification.
type StateChange struct {
	RuleName    string
	PrevState   AlertState
	NewState    AlertState
	MetricValue float64
	Threshold   float64
	Timestamp   time.Time
}

// ActiveAlert represents a currently firing alert for UI display.
type ActiveAlert struct {
	RuleName     string
	State        AlertState
	MetricValue  float64
	Threshold    float64
	TriggeredAt  time.Time
	Duration     time.Duration
	Acknowledged bool
	Message      string
}

// MetricValues provides current metric values for evaluation.
type MetricValues interface {
	// Get returns the value for a metric name.
	// Returns 0, false if metric is unavailable.
	Get(name string) (float64, bool)

	// Timestamp returns when metrics were collected.
	Timestamp() time.Time
}

// Engine evaluates alert rules against metrics.
type Engine interface {
	// LoadRules loads alert rules from configuration.
	// Invalid rules are logged as warnings and skipped.
	LoadRules(rules []Rule) error

	// Evaluate checks all rules against current metrics.
	// Returns state changes that occurred (for notifications).
	Evaluate(metrics MetricValues) []StateChange

	// GetActiveAlerts returns all currently firing alerts.
	GetActiveAlerts() []ActiveAlert

	// GetState returns the current state for a specific rule.
	GetState(ruleName string) (*State, bool)

	// Acknowledge marks an alert as acknowledged.
	// Returns error if alert not found or already acknowledged.
	Acknowledge(ruleName string) error

	// RuleCount returns the number of loaded rules.
	RuleCount() int

	// EnabledRuleCount returns the number of enabled rules.
	EnabledRuleCount() int
}

// Store persists alert events and provides history queries.
type Store interface {
	// SaveEvent persists a state transition event.
	SaveEvent(ctx context.Context, event Event) error

	// GetHistory returns recent alert events.
	// limit=0 returns all events within retention period.
	GetHistory(ctx context.Context, limit int) ([]Event, error)

	// GetHistoryForRule returns events for a specific rule.
	GetHistoryForRule(ctx context.Context, ruleName string, limit int) ([]Event, error)

	// GetHistoryByState returns events filtered by state.
	GetHistoryByState(ctx context.Context, state AlertState, limit int) ([]Event, error)

	// Acknowledge marks an event as acknowledged.
	Acknowledge(ctx context.Context, eventID int64, by string) error

	// Prune removes events older than retention period.
	// Returns number of deleted events.
	Prune(ctx context.Context, retention time.Duration) (int64, error)
}

// ExpressionParser parses metric expressions.
type ExpressionParser interface {
	// Parse parses a metric expression string.
	// Returns error if expression is invalid.
	Parse(expr string) (Expression, error)
}

// Expression represents a parsed metric expression.
type Expression interface {
	// Evaluate computes the expression value given metric values.
	Evaluate(metrics MetricValues) (float64, error)

	// MetricNames returns all metric names referenced in the expression.
	MetricNames() []string
}
