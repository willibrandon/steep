package config

import (
	"time"
)

// AlertsConfig holds alert system configuration.
type AlertsConfig struct {
	// Enabled is the master switch for the alert system (default: true).
	Enabled bool `mapstructure:"enabled"`

	// HistoryRetention is how long to keep alert history (default: 720h/30d).
	HistoryRetention time.Duration `mapstructure:"history_retention"`

	// Rules is the list of alert rules.
	Rules []AlertRuleConfig `mapstructure:"rules"`
}

// AlertRuleConfig represents a single alert rule in configuration.
type AlertRuleConfig struct {
	// Name is the unique identifier for the rule.
	Name string `mapstructure:"name"`

	// Metric is the metric expression to evaluate.
	Metric string `mapstructure:"metric"`

	// Operator is the comparison operator (default: ">").
	Operator string `mapstructure:"operator"`

	// Warning is the threshold that triggers Warning state.
	Warning float64 `mapstructure:"warning"`

	// Critical is the threshold that triggers Critical state.
	Critical float64 `mapstructure:"critical"`

	// Enabled indicates whether the rule is active (default: true).
	Enabled *bool `mapstructure:"enabled"`

	// Message is an optional custom message template.
	Message string `mapstructure:"message"`
}

// IsEnabled returns whether the rule is enabled (defaults to true if not set).
func (r *AlertRuleConfig) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// DefaultAlertsConfig returns the default alerts configuration.
func DefaultAlertsConfig() AlertsConfig {
	return AlertsConfig{
		Enabled:          true,
		HistoryRetention: 720 * time.Hour, // 30 days
		Rules:            []AlertRuleConfig{},
	}
}
