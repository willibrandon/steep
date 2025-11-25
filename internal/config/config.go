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
	Connection ConnectionConfig `mapstructure:"connection"`
	UI         UIConfig         `mapstructure:"ui"`
	Queries    QueriesConfig    `mapstructure:"queries"`
	Debug      bool             `mapstructure:"debug"`
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
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	DateFormat      string        `mapstructure:"date_format"`
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
		Debug:   viper.GetBool("debug"),
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
	viper.SetDefault("ui.refresh_interval", "1s")
	viper.SetDefault("ui.date_format", "2006-01-02 15:04:05")

	// Debug default
	viper.SetDefault("debug", false)
}
