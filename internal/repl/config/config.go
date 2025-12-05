package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the steep-repl daemon configuration.
type Config struct {
	Enabled    bool             `mapstructure:"enabled"`
	NodeID     string           `mapstructure:"node_id"`
	NodeName   string           `mapstructure:"node_name"`
	PostgreSQL PostgreSQLConfig `mapstructure:"postgresql"`
	GRPC       GRPCConfig       `mapstructure:"grpc"`
	HTTP       HTTPConfig       `mapstructure:"http"`
	IPC        IPCConfig        `mapstructure:"ipc"`
	Audit      AuditConfig      `mapstructure:"audit"`
}

// PostgreSQLConfig holds PostgreSQL connection configuration.
type PostgreSQLConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	Database        string `mapstructure:"database"`
	User            string `mapstructure:"user"`
	PasswordCommand string `mapstructure:"password_command"`
	SSLMode         string `mapstructure:"sslmode"`
}

// GRPCConfig holds gRPC server configuration for node-to-node communication.
type GRPCConfig struct {
	Port int       `mapstructure:"port"`
	TLS  TLSConfig `mapstructure:"tls"`
}

// TLSConfig holds mTLS certificate configuration.
type TLSConfig struct {
	CertFile       string `mapstructure:"cert_file"`
	KeyFile        string `mapstructure:"key_file"`
	CAFile         string `mapstructure:"ca_file"`
	ClientCertFile string `mapstructure:"client_cert_file"`
	ClientKeyFile  string `mapstructure:"client_key_file"`
}

// HTTPConfig holds HTTP health endpoint configuration.
type HTTPConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Bind    string `mapstructure:"bind"`
}

// IPCConfig holds IPC (named pipes/Unix sockets) configuration.
type IPCConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"` // Auto-detected if empty
}

// AuditConfig holds audit log configuration.
type AuditConfig struct {
	Retention string `mapstructure:"retention"` // Duration string, e.g., "8760h" (1 year)
}

// Load loads the repl configuration from the main steep config file.
// It reads the "repl" section from config.yaml.
func Load() (*Config, error) {
	return LoadFromPath("")
}

// LoadFromPath loads configuration from a specific path.
// If configPath is empty, it searches default locations.
func LoadFromPath(configPath string) (*Config, error) {
	v := viper.New()

	// Environment variable support
	v.AutomaticEnv()
	v.SetEnvPrefix("STEEP_REPL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Apply defaults
	applyDefaults(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")

		// Platform-specific config directories
		if configDir, err := os.UserConfigDir(); err == nil {
			v.AddConfigPath(filepath.Join(configDir, "steep"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "steep"))
		}
		v.AddConfigPath(".")
	}

	// Try to read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, use defaults
			return configFromViper(v)
		}
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	return configFromViper(v)
}

// configFromViper extracts the repl config from a viper instance.
func configFromViper(v *viper.Viper) (*Config, error) {
	var cfg Config

	// Extract the "repl" section
	replViper := v.Sub("repl")
	if replViper == nil {
		// No repl section, use defaults
		replViper = viper.New()
		applyDefaults(replViper)
	}

	if err := replViper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling repl config: %w", err)
	}

	// Expand paths
	cfg.GRPC.TLS.CertFile = expandPath(cfg.GRPC.TLS.CertFile)
	cfg.GRPC.TLS.KeyFile = expandPath(cfg.GRPC.TLS.KeyFile)
	cfg.GRPC.TLS.CAFile = expandPath(cfg.GRPC.TLS.CAFile)

	// Auto-detect IPC path if not set
	if cfg.IPC.Path == "" {
		cfg.IPC.Path = DefaultIPCPath()
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyDefaults sets default configuration values.
func applyDefaults(v *viper.Viper) {
	v.SetDefault("repl.enabled", false)
	v.SetDefault("repl.node_id", "")
	v.SetDefault("repl.node_name", "")

	// PostgreSQL defaults
	v.SetDefault("repl.postgresql.host", "localhost")
	v.SetDefault("repl.postgresql.port", 5432)
	v.SetDefault("repl.postgresql.database", "postgres")
	if user := os.Getenv("USER"); user != "" {
		v.SetDefault("repl.postgresql.user", user)
	} else {
		v.SetDefault("repl.postgresql.user", "postgres")
	}
	v.SetDefault("repl.postgresql.sslmode", "prefer")

	// gRPC defaults
	v.SetDefault("repl.grpc.port", 5433)

	// HTTP defaults
	v.SetDefault("repl.http.enabled", true)
	v.SetDefault("repl.http.port", 8080)
	v.SetDefault("repl.http.bind", "0.0.0.0")

	// IPC defaults
	v.SetDefault("repl.ipc.enabled", true)

	// Audit defaults
	v.SetDefault("repl.audit.retention", "17520h") // 2 years
}

// Validate checks that the configuration has valid values.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if disabled
	}

	if c.NodeID == "" {
		return fmt.Errorf("repl.node_id is required when repl is enabled")
	}
	if c.NodeName == "" {
		return fmt.Errorf("repl.node_name is required when repl is enabled")
	}

	// PostgreSQL validation
	if c.PostgreSQL.Host == "" {
		return fmt.Errorf("repl.postgresql.host is required")
	}
	if c.PostgreSQL.Port < 1 || c.PostgreSQL.Port > 65535 {
		return fmt.Errorf("repl.postgresql.port must be between 1 and 65535")
	}

	// gRPC validation
	if c.GRPC.Port < 1 || c.GRPC.Port > 65535 {
		return fmt.Errorf("repl.grpc.port must be between 1 and 65535")
	}

	// HTTP validation
	if c.HTTP.Enabled {
		if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
			return fmt.Errorf("repl.http.port must be between 1 and 65535")
		}
	}

	return nil
}

// DefaultIPCPath returns the platform-appropriate IPC endpoint path.
func DefaultIPCPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\steep-repl`
	}
	return "/tmp/steep-repl.sock"
}

// DefaultCertsDir returns the platform-appropriate certificates directory.
func DefaultCertsDir() string {
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "steep", "certs")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "steep", "certs")
	}
	return "certs"
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// HasTLSConfig returns true if all TLS certificate paths are configured.
func (c *Config) HasTLSConfig() bool {
	return c.GRPC.TLS.CertFile != "" &&
		c.GRPC.TLS.KeyFile != "" &&
		c.GRPC.TLS.CAFile != ""
}
