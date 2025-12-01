// Package alerts provides threshold-based alert monitoring for PostgreSQL metrics.
package alerts

// AlertState represents the severity level of an alert.
type AlertState string

const (
	// StateNormal indicates the metric is within acceptable thresholds.
	StateNormal AlertState = "normal"
	// StateWarning indicates the metric has crossed the warning threshold.
	StateWarning AlertState = "warning"
	// StateCritical indicates the metric has crossed the critical threshold.
	StateCritical AlertState = "critical"
)

// String returns the string representation of the alert state.
func (s AlertState) String() string {
	return string(s)
}

// IsActive returns true if the alert state is Warning or Critical.
func (s AlertState) IsActive() bool {
	return s == StateWarning || s == StateCritical
}

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

// String returns the string representation of the operator.
func (o Operator) String() string {
	return string(o)
}

// Compare evaluates the comparison between value and threshold using the operator.
// Returns true if the comparison is satisfied.
func (o Operator) Compare(value, threshold float64) bool {
	switch o {
	case OpGreaterThan:
		return value > threshold
	case OpLessThan:
		return value < threshold
	case OpGreaterOrEqual:
		return value >= threshold
	case OpLessOrEqual:
		return value <= threshold
	case OpEqual:
		return value == threshold
	case OpNotEqual:
		return value != threshold
	default:
		return false
	}
}

// IsValid returns true if the operator is a recognized operator.
func (o Operator) IsValid() bool {
	switch o {
	case OpGreaterThan, OpLessThan, OpGreaterOrEqual, OpLessOrEqual, OpEqual, OpNotEqual:
		return true
	default:
		return false
	}
}

// ParseOperator converts a string to an Operator, returning OpGreaterThan as default.
func ParseOperator(s string) Operator {
	op := Operator(s)
	if op.IsValid() {
		return op
	}
	return OpGreaterThan
}
