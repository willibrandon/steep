package config

import "time"

// QueriesConfig holds configuration for query performance monitoring.
type QueriesConfig struct {
	// Enabled indicates whether query monitoring is enabled
	Enabled bool `mapstructure:"enabled"`

	// RefreshInterval is how often to poll for new queries
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`

	// RetentionDays is how long to keep query statistics
	RetentionDays int `mapstructure:"retention_days"`

	// DataSource specifies the data collection method
	// Options: "sampling" (default), "log_parsing"
	DataSource string `mapstructure:"data_source"`

	// LogPath is the path to PostgreSQL log file (for log_parsing mode)
	LogPath string `mapstructure:"log_path"`

	// LogLinePrefix is the log_line_prefix PostgreSQL setting
	LogLinePrefix string `mapstructure:"log_line_prefix"`

	// StoragePath is the path to SQLite database for query stats
	StoragePath string `mapstructure:"storage_path"`
}

// DefaultQueriesConfig returns default query monitoring configuration.
func DefaultQueriesConfig() QueriesConfig {
	return QueriesConfig{
		Enabled:         true,
		RefreshInterval: 5 * time.Second,
		RetentionDays:   7,
		DataSource:      "sampling",
		StoragePath:     "", // Will use default cache directory
	}
}
