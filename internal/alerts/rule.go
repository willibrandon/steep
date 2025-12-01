package alerts

import (
	"fmt"
	"regexp"
)

// ruleNamePattern validates rule names (lowercase letters, digits, underscores, starting with letter).
var ruleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Rule represents a configured alert rule loaded from YAML configuration.
type Rule struct {
	// Name is the unique identifier for the rule (e.g., "high_replication_lag").
	Name string `mapstructure:"name"`

	// Metric is the metric expression to evaluate (e.g., "replication_lag_bytes" or "active_connections / max_connections").
	Metric string `mapstructure:"metric"`

	// Operator is the comparison operator (default: ">").
	Operator Operator `mapstructure:"operator"`

	// Warning is the threshold that triggers Warning state.
	Warning float64 `mapstructure:"warning"`

	// Critical is the threshold that triggers Critical state.
	Critical float64 `mapstructure:"critical"`

	// Enabled indicates whether the rule is active (default: true).
	Enabled bool `mapstructure:"enabled"`

	// Message is an optional custom message template.
	Message string `mapstructure:"message"`
}

// Validate checks if the rule configuration is valid.
// Returns an error describing the validation failure, or nil if valid.
func (r *Rule) Validate() error {
	// Name validation
	if r.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	if !ruleNamePattern.MatchString(r.Name) {
		return fmt.Errorf("rule name %q must match pattern ^[a-z][a-z0-9_]*$", r.Name)
	}

	// Metric validation
	if r.Metric == "" {
		return fmt.Errorf("rule %q: metric is required", r.Name)
	}

	// Operator validation
	if r.Operator == "" {
		r.Operator = OpGreaterThan // Default
	}
	if !r.Operator.IsValid() {
		return fmt.Errorf("rule %q: invalid operator %q", r.Name, r.Operator)
	}

	// Threshold ordering validation
	// For ">" operators, warning should be less than critical
	// For "<" operators, warning should be greater than critical
	switch r.Operator {
	case OpGreaterThan, OpGreaterOrEqual:
		if r.Warning >= r.Critical {
			return fmt.Errorf("rule %q: for operator %q, warning (%.2f) must be less than critical (%.2f)",
				r.Name, r.Operator, r.Warning, r.Critical)
		}
	case OpLessThan, OpLessOrEqual:
		if r.Warning <= r.Critical {
			return fmt.Errorf("rule %q: for operator %q, warning (%.2f) must be greater than critical (%.2f)",
				r.Name, r.Operator, r.Warning, r.Critical)
		}
	}

	return nil
}

// DefaultRule returns a rule with default values applied.
func DefaultRule() Rule {
	return Rule{
		Operator: OpGreaterThan,
		Enabled:  true,
	}
}
