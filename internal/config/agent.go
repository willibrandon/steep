package config

import (
	"fmt"
	"regexp"
	"time"
)

// AgentConfig holds configuration for the steep-agent daemon.
type AgentConfig struct {
	// Enabled indicates whether agent features are active in config.
	Enabled bool `mapstructure:"enabled"`

	// Intervals configures collection frequency per data type.
	Intervals AgentIntervalsConfig `mapstructure:"intervals"`

	// Retention configures data retention periods per data type.
	Retention AgentRetentionConfig `mapstructure:"retention"`

	// Instances configures multiple PostgreSQL instances for monitoring.
	// If empty, uses the default connection settings.
	Instances []AgentInstanceConfig `mapstructure:"instances"`

	// Alerts configures background alerting via webhooks.
	Alerts AgentAlertsConfig `mapstructure:"alerts"`
}

// AgentIntervalsConfig configures collection intervals per data type.
// All intervals must be >= 1s and <= 60s.
type AgentIntervalsConfig struct {
	Activity    time.Duration `mapstructure:"activity"`    // pg_stat_activity (default: 2s)
	Queries     time.Duration `mapstructure:"queries"`     // Query stats (default: 5s)
	Replication time.Duration `mapstructure:"replication"` // Replication lag (default: 2s)
	Locks       time.Duration `mapstructure:"locks"`       // Lock monitoring (default: 2s)
	Tables      time.Duration `mapstructure:"tables"`      // Table statistics (default: 30s)
	Metrics     time.Duration `mapstructure:"metrics"`     // Dashboard metrics (default: 1s)
}

// AgentRetentionConfig configures data retention periods per data type.
// All periods must be >= 1h and <= 720h (30 days).
type AgentRetentionConfig struct {
	ActivityHistory time.Duration `mapstructure:"activity_history"` // Activity snapshots (default: 24h)
	QueryStats      time.Duration `mapstructure:"query_stats"`      // Query statistics (default: 168h/7d)
	ReplicationLag  time.Duration `mapstructure:"replication_lag"`  // Replication lag history (default: 24h)
	LockHistory     time.Duration `mapstructure:"lock_history"`     // Lock snapshots (default: 24h)
	Metrics         time.Duration `mapstructure:"metrics"`          // Metrics history (default: 24h)
}

// AgentInstanceConfig configures a single PostgreSQL instance for monitoring.
type AgentInstanceConfig struct {
	// Name is the unique identifier for this instance (e.g., "primary", "replica1").
	// Must be alphanumeric with hyphens/underscores only.
	Name string `mapstructure:"name"`

	// Connection is the PostgreSQL connection string (DSN format).
	Connection string `mapstructure:"connection"`
}

// AgentAlertsConfig configures background alerting for the agent.
type AgentAlertsConfig struct {
	// Enabled indicates whether background alerting is active.
	Enabled bool `mapstructure:"enabled"`

	// WebhookURL is the endpoint for alert notifications.
	WebhookURL string `mapstructure:"webhook_url"`
}

// DefaultAgentConfig returns the default agent configuration.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Enabled: true,
		Intervals: AgentIntervalsConfig{
			Activity:    2 * time.Second,
			Queries:     5 * time.Second,
			Replication: 2 * time.Second,
			Locks:       2 * time.Second,
			Tables:      30 * time.Second,
			Metrics:     1 * time.Second,
		},
		Retention: AgentRetentionConfig{
			ActivityHistory: 24 * time.Hour,
			QueryStats:      168 * time.Hour, // 7 days
			ReplicationLag:  24 * time.Hour,
			LockHistory:     24 * time.Hour,
			Metrics:         24 * time.Hour,
		},
		Instances: []AgentInstanceConfig{},
		Alerts: AgentAlertsConfig{
			Enabled:    false,
			WebhookURL: "",
		},
	}
}

// instanceNameRegex validates instance names (alphanumeric, hyphens, underscores).
var instanceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateAgentConfig validates the agent configuration.
func ValidateAgentConfig(cfg *AgentConfig) error {
	// Skip validation if agent is disabled
	if !cfg.Enabled {
		return nil
	}

	// Validate intervals (>= 1s, <= 60s)
	minInterval := 1 * time.Second
	maxInterval := 60 * time.Second

	if err := validateInterval("agent.intervals.activity", cfg.Intervals.Activity, minInterval, maxInterval); err != nil {
		return err
	}
	if err := validateInterval("agent.intervals.queries", cfg.Intervals.Queries, minInterval, maxInterval); err != nil {
		return err
	}
	if err := validateInterval("agent.intervals.replication", cfg.Intervals.Replication, minInterval, maxInterval); err != nil {
		return err
	}
	if err := validateInterval("agent.intervals.locks", cfg.Intervals.Locks, minInterval, maxInterval); err != nil {
		return err
	}
	if err := validateInterval("agent.intervals.tables", cfg.Intervals.Tables, minInterval, maxInterval); err != nil {
		return err
	}
	if err := validateInterval("agent.intervals.metrics", cfg.Intervals.Metrics, minInterval, maxInterval); err != nil {
		return err
	}

	// Validate retention (>= 1h, <= 720h)
	minRetention := 1 * time.Hour
	maxRetention := 720 * time.Hour // 30 days

	if err := validateRetention("agent.retention.activity_history", cfg.Retention.ActivityHistory, minRetention, maxRetention); err != nil {
		return err
	}
	if err := validateRetention("agent.retention.query_stats", cfg.Retention.QueryStats, minRetention, maxRetention); err != nil {
		return err
	}
	if err := validateRetention("agent.retention.replication_lag", cfg.Retention.ReplicationLag, minRetention, maxRetention); err != nil {
		return err
	}
	if err := validateRetention("agent.retention.lock_history", cfg.Retention.LockHistory, minRetention, maxRetention); err != nil {
		return err
	}
	if err := validateRetention("agent.retention.metrics", cfg.Retention.Metrics, minRetention, maxRetention); err != nil {
		return err
	}

	// Validate instances
	instanceNames := make(map[string]bool)
	for i, inst := range cfg.Instances {
		// Validate name
		if inst.Name == "" {
			return fmt.Errorf("agent.instances[%d]: name is required", i)
		}
		if !instanceNameRegex.MatchString(inst.Name) {
			return fmt.Errorf("agent.instances[%d]: name %q must be alphanumeric with hyphens/underscores only", i, inst.Name)
		}
		if instanceNames[inst.Name] {
			return fmt.Errorf("agent.instances[%d]: duplicate instance name %q", i, inst.Name)
		}
		instanceNames[inst.Name] = true

		// Validate connection string
		if inst.Connection == "" {
			return fmt.Errorf("agent.instances[%d] %q: connection is required", i, inst.Name)
		}
	}

	// Validate alerts
	if cfg.Alerts.Enabled && cfg.Alerts.WebhookURL == "" {
		return fmt.Errorf("agent.alerts.webhook_url is required when alerts are enabled")
	}

	return nil
}

// validateInterval validates a collection interval.
func validateInterval(field string, value, min, max time.Duration) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %v and %v, got %v", field, min, max, value)
	}
	return nil
}

// validateRetention validates a retention period.
func validateRetention(field string, value, min, max time.Duration) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %v and %v, got %v", field, min, max, value)
	}
	return nil
}
