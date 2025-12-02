package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the root configuration structure
type Config struct {
	Connection  ConnectionConfig  `mapstructure:"connection"`
	UI          UIConfig          `mapstructure:"ui"`
	Queries     QueriesConfig     `mapstructure:"queries"`
	Replication ReplicationConfig `mapstructure:"replication"`
	Logs        LogsConfig        `mapstructure:"logs"`
	Alerts      AlertsConfig      `mapstructure:"alerts"`
	Agent       AgentConfig       `mapstructure:"agent"`
	Debug       bool              `mapstructure:"debug"`
	LogFile     string            `mapstructure:"log_file"`
}

// LogsConfig holds log viewer configuration
type LogsConfig struct {
	// AccessMethod controls how logs are read: "auto", "filesystem", or "pg_read_file"
	// - "auto": Try filesystem first, fall back to pg_read_file (default)
	// - "filesystem": Read logs directly from disk (requires local access)
	// - "pg_read_file": Read logs via SQL (works over network, requires superuser)
	AccessMethod string `mapstructure:"access_method"`
}

// ConnectionConfig holds database connection parameters
type ConnectionConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	Database       string `mapstructure:"database"`
	User           string `mapstructure:"user"`
	PasswordCommand string `mapstructure:"password_command"`
	SSLMode        string `mapstructure:"sslmode"`
	SSLRootCert    string `mapstructure:"sslrootcert"`
	SSLCert        string `mapstructure:"sslcert"`
	SSLKey         string `mapstructure:"sslkey"`
	PoolMaxConns   int    `mapstructure:"pool_max_conns"`
	PoolMinConns   int    `mapstructure:"pool_min_conns"`
}

// UIConfig holds user interface preferences
type UIConfig struct {
	Theme           string        `mapstructure:"theme"`
	SyntaxTheme     string        `mapstructure:"syntax_theme"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	DateFormat      string        `mapstructure:"date_format"`
	QueryTimeout    time.Duration `mapstructure:"query_timeout"`
}

// ReplicationConfig holds replication monitoring configuration
type ReplicationConfig struct {
	// LagHistoryRetention is how long to keep lag history data (default: 24h, max: 168h/7d)
	LagHistoryRetention time.Duration `mapstructure:"lag_history_retention"`
}

// LoadConfig loads configuration from YAML file and environment variables.
// It searches for config.yaml in ~/.config/steep/ and current directory.
func LoadConfig() (*Config, error) {
	return LoadConfigFromPath("")
}

// LoadConfigFromPath loads configuration from a specific path.
// If configPath is empty, it searches default locations.
// If configPath is provided, it loads only from that file.
func LoadConfigFromPath(configPath string) (*Config, error) {
	// Environment variable support
	viper.AutomaticEnv()
	viper.SetEnvPrefix("STEEP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Apply defaults
	applyDefaults()

	if configPath != "" {
		// Load from specific path
		viper.SetConfigFile(configPath)
	} else {
		// Set config file details for default search
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("$HOME/.config/steep")
		viper.AddConfigPath(".")
	}

	// Try to read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, create default
			return createDefaultConfig()
		}
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate configuration
	if err := ValidateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// createDefaultConfig creates a default configuration when no config file exists
func createDefaultConfig() (*Config, error) {
	config := &Config{
		Connection: ConnectionConfig{
			Host:         viper.GetString("connection.host"),
			Port:         viper.GetInt("connection.port"),
			Database:     viper.GetString("connection.database"),
			User:         viper.GetString("connection.user"),
			SSLMode:      viper.GetString("connection.sslmode"),
			PoolMaxConns: viper.GetInt("connection.pool_max_conns"),
			PoolMinConns: viper.GetInt("connection.pool_min_conns"),
		},
		UI: UIConfig{
			Theme:           viper.GetString("ui.theme"),
			RefreshInterval: viper.GetDuration("ui.refresh_interval"),
			DateFormat:      viper.GetString("ui.date_format"),
		},
		Queries: DefaultQueriesConfig(),
		Replication: ReplicationConfig{
			LagHistoryRetention: viper.GetDuration("replication.lag_history_retention"),
		},
		Logs: LogsConfig{
			AccessMethod: viper.GetString("logs.access_method"),
		},
		Alerts: AlertsConfig{
			Enabled:          viper.GetBool("alerts.enabled"),
			HistoryRetention: viper.GetDuration("alerts.history_retention"),
			Rules:            []AlertRuleConfig{},
		},
		Agent:   DefaultAgentConfig(),
		Debug:   viper.GetBool("debug"),
		LogFile: viper.GetString("log_file"),
	}

	return config, nil
}

// ValidateConfig validates the configuration values
func ValidateConfig(cfg *Config) error {
	// Validate connection config
	if cfg.Connection.Host == "" {
		return fmt.Errorf("connection.host cannot be empty")
	}
	if cfg.Connection.Port < 1 || cfg.Connection.Port > 65535 {
		return fmt.Errorf("connection.port must be between 1 and 65535, got %d", cfg.Connection.Port)
	}
	if cfg.Connection.Database == "" {
		return fmt.Errorf("connection.database cannot be empty")
	}

	// Validate SSL mode (all PostgreSQL SSL modes)
	validSSLModes := []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
	validMode := false
	for _, mode := range validSSLModes {
		if cfg.Connection.SSLMode == mode {
			validMode = true
			break
		}
	}
	if !validMode {
		return fmt.Errorf("connection.sslmode must be one of: %v, got %s", validSSLModes, cfg.Connection.SSLMode)
	}

	// Validate SSL certificate paths if using verify-ca or verify-full
	if cfg.Connection.SSLMode == "verify-ca" || cfg.Connection.SSLMode == "verify-full" {
		if cfg.Connection.SSLRootCert == "" {
			return fmt.Errorf("connection.sslrootcert is required when sslmode is %s", cfg.Connection.SSLMode)
		}
		// Check if root cert file exists
		if _, err := os.Stat(cfg.Connection.SSLRootCert); os.IsNotExist(err) {
			return fmt.Errorf("SSL root certificate file not found: %s", cfg.Connection.SSLRootCert)
		}
	}

	// Validate client certificate if provided
	if cfg.Connection.SSLCert != "" {
		if _, err := os.Stat(cfg.Connection.SSLCert); os.IsNotExist(err) {
			return fmt.Errorf("SSL client certificate file not found: %s", cfg.Connection.SSLCert)
		}
		// If cert is provided, key must also be provided
		if cfg.Connection.SSLKey == "" {
			return fmt.Errorf("connection.sslkey is required when sslcert is provided")
		}
		if _, err := os.Stat(cfg.Connection.SSLKey); os.IsNotExist(err) {
			return fmt.Errorf("SSL client key file not found: %s", cfg.Connection.SSLKey)
		}
	}

	// Validate pool settings
	if cfg.Connection.PoolMaxConns < 1 {
		return fmt.Errorf("connection.pool_max_conns must be >= 1, got %d", cfg.Connection.PoolMaxConns)
	}
	if cfg.Connection.PoolMinConns < 0 {
		return fmt.Errorf("connection.pool_min_conns must be >= 0, got %d", cfg.Connection.PoolMinConns)
	}
	if cfg.Connection.PoolMaxConns < cfg.Connection.PoolMinConns {
		return fmt.Errorf("connection.pool_max_conns (%d) must be >= pool_min_conns (%d)",
			cfg.Connection.PoolMaxConns, cfg.Connection.PoolMinConns)
	}

	// Validate UI config
	validThemes := []string{"dark", "light"}
	validTheme := false
	for _, theme := range validThemes {
		if cfg.UI.Theme == theme {
			validTheme = true
			break
		}
	}
	if !validTheme {
		return fmt.Errorf("ui.theme must be one of: %v, got %s", validThemes, cfg.UI.Theme)
	}

	if cfg.UI.RefreshInterval < 100*time.Millisecond || cfg.UI.RefreshInterval > 60*time.Second {
		return fmt.Errorf("ui.refresh_interval must be between 100ms and 60s, got %v", cfg.UI.RefreshInterval)
	}

	// Validate replication config
	minRetention := time.Hour
	maxRetention := 168 * time.Hour // 7 days
	if cfg.Replication.LagHistoryRetention < minRetention || cfg.Replication.LagHistoryRetention > maxRetention {
		return fmt.Errorf("replication.lag_history_retention must be between 1h and 168h (7d), got %v", cfg.Replication.LagHistoryRetention)
	}

	// Validate logs config
	validAccessMethods := []string{"auto", "filesystem", "pg_read_file"}
	validAccessMethod := false
	for _, method := range validAccessMethods {
		if cfg.Logs.AccessMethod == method {
			validAccessMethod = true
			break
		}
	}
	if !validAccessMethod {
		return fmt.Errorf("logs.access_method must be one of: %v, got %s", validAccessMethods, cfg.Logs.AccessMethod)
	}

	// Validate alerts config
	if err := validateAlertsConfig(&cfg.Alerts); err != nil {
		return err
	}

	// Validate agent config
	if err := ValidateAgentConfig(&cfg.Agent); err != nil {
		return err
	}

	return nil
}

// validateAlertsConfig validates the alerts configuration.
func validateAlertsConfig(cfg *AlertsConfig) error {
	// Validate history retention
	minRetention := time.Hour
	maxRetention := 720 * time.Hour // 30 days
	if cfg.HistoryRetention < minRetention || cfg.HistoryRetention > maxRetention {
		return fmt.Errorf("alerts.history_retention must be between 1h and 720h (30d), got %v", cfg.HistoryRetention)
	}

	// Validate rules
	ruleNames := make(map[string]bool)
	for i, rule := range cfg.Rules {
		// Check for duplicate names
		if ruleNames[rule.Name] {
			return fmt.Errorf("alerts.rules[%d]: duplicate rule name %q", i, rule.Name)
		}
		ruleNames[rule.Name] = true

		// Validate rule name
		if rule.Name == "" {
			return fmt.Errorf("alerts.rules[%d]: name is required", i)
		}

		// Validate metric
		if rule.Metric == "" {
			return fmt.Errorf("alerts.rules[%d] %q: metric is required", i, rule.Name)
		}

		// Validate operator if provided
		if rule.Operator != "" {
			validOperators := []string{">", "<", ">=", "<=", "==", "!="}
			validOp := false
			for _, op := range validOperators {
				if rule.Operator == op {
					validOp = true
					break
				}
			}
			if !validOp {
				return fmt.Errorf("alerts.rules[%d] %q: operator must be one of: %v, got %s", i, rule.Name, validOperators, rule.Operator)
			}
		}
	}

	return nil
}

// applyDefaults sets default configuration values
func applyDefaults() {
	// Connection defaults
	viper.SetDefault("connection.host", "localhost")
	viper.SetDefault("connection.port", 5432)
	viper.SetDefault("connection.database", "postgres")

	// Get current user for default username
	if user := os.Getenv("USER"); user != "" {
		viper.SetDefault("connection.user", user)
	} else {
		viper.SetDefault("connection.user", "postgres")
	}

	viper.SetDefault("connection.sslmode", "prefer")
	viper.SetDefault("connection.pool_max_conns", 10)
	viper.SetDefault("connection.pool_min_conns", 2)

	// UI defaults
	viper.SetDefault("ui.theme", "dark")
	viper.SetDefault("ui.syntax_theme", "monokai")
	viper.SetDefault("ui.refresh_interval", "1s")
	viper.SetDefault("ui.date_format", "2006-01-02 15:04:05")
	viper.SetDefault("ui.query_timeout", "30s")

	// Replication defaults
	viper.SetDefault("replication.lag_history_retention", "24h")

	// Logs defaults
	viper.SetDefault("logs.access_method", "auto")

	// Alerts defaults
	viper.SetDefault("alerts.enabled", true)
	viper.SetDefault("alerts.history_retention", "720h") // 30 days

	// Debug default
	viper.SetDefault("debug", false)

	// Log file default (empty = ~/.config/steep/steep.log)
	viper.SetDefault("log_file", "")

	// Agent defaults
	viper.SetDefault("agent.enabled", true)
	viper.SetDefault("agent.intervals.activity", "2s")
	viper.SetDefault("agent.intervals.queries", "5s")
	viper.SetDefault("agent.intervals.replication", "2s")
	viper.SetDefault("agent.intervals.locks", "2s")
	viper.SetDefault("agent.intervals.tables", "30s")
	viper.SetDefault("agent.intervals.metrics", "1s")
	viper.SetDefault("agent.retention.activity_history", "24h")
	viper.SetDefault("agent.retention.query_stats", "168h") // 7 days
	viper.SetDefault("agent.retention.replication_lag", "24h")
	viper.SetDefault("agent.retention.lock_history", "24h")
	viper.SetDefault("agent.retention.metrics", "24h")
	viper.SetDefault("agent.alerts.enabled", false)
	viper.SetDefault("agent.alerts.webhook_url", "")
}
