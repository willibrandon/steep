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
	PoolMaxConns   int    `mapstructure:"pool_max_conns"`
	PoolMinConns   int    `mapstructure:"pool_min_conns"`
}

// UIConfig holds user interface preferences
type UIConfig struct {
	Theme           string        `mapstructure:"theme"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	DateFormat      string        `mapstructure:"date_format"`
}

// LoadConfig loads configuration from YAML file and environment variables
func LoadConfig() (*Config, error) {
	// Set config file details
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.config/steep")
	viper.AddConfigPath(".")

	// Environment variable support
	viper.AutomaticEnv()
	viper.SetEnvPrefix("STEEP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Apply defaults
	applyDefaults()

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
		Debug: viper.GetBool("debug"),
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

	// Validate SSL mode
	validSSLModes := []string{"disable", "prefer", "require"}
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
