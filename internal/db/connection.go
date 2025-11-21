package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/logger"
)

// NewConnectionPool creates a new PostgreSQL connection pool using the provided configuration
func NewConnectionPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	logger.Debug("Creating new database connection pool",
		"host", cfg.Connection.Host,
		"port", cfg.Connection.Port,
		"database", cfg.Connection.Database,
		"user", cfg.Connection.User,
		"sslmode", cfg.Connection.SSLMode,
	)

	// Get password using precedence: password_command > PGPASSWORD > interactive prompt
	password, err := GetPassword(cfg.Connection.PasswordCommand)
	if err != nil {
		logger.Error("Failed to retrieve password", "error", err)
		return nil, fmt.Errorf("failed to retrieve password: %w", err)
	}
	logger.Debug("Password retrieved successfully")

	// Build connection string
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Connection.User,
		password,
		cfg.Connection.Host,
		cfg.Connection.Port,
		cfg.Connection.Database,
		cfg.Connection.SSLMode,
	)

	// Add SSL certificate paths if configured
	if cfg.Connection.SSLRootCert != "" {
		connString += fmt.Sprintf("&sslrootcert=%s", cfg.Connection.SSLRootCert)
	}
	if cfg.Connection.SSLCert != "" {
		connString += fmt.Sprintf("&sslcert=%s", cfg.Connection.SSLCert)
	}
	if cfg.Connection.SSLKey != "" {
		connString += fmt.Sprintf("&sslkey=%s", cfg.Connection.SSLKey)
	}

	// Parse connection string and create pool config
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		logger.Error("Failed to parse connection string", "error", err)
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	poolConfig.MaxConns = int32(cfg.Connection.PoolMaxConns)
	poolConfig.MinConns = int32(cfg.Connection.PoolMinConns)
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "steep"

	logger.Debug("Connection pool configuration",
		"max_conns", cfg.Connection.PoolMaxConns,
		"min_conns", cfg.Connection.PoolMinConns,
	)

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		logger.Error("Failed to create connection pool",
			"host", cfg.Connection.Host,
			"port", cfg.Connection.Port,
			"error", err,
		)
		return nil, fmt.Errorf(
			"connection refused: ensure PostgreSQL is running on %s:%d (error: %w)",
			cfg.Connection.Host,
			cfg.Connection.Port,
			err,
		)
	}

	// Validate connection with a simple query
	logger.Debug("Validating database connection")
	if err := ValidateConnection(ctx, pool); err != nil {
		logger.Error("Connection validation failed", "error", err)
		pool.Close()
		return nil, err
	}

	logger.Info("Database connection pool created successfully",
		"host", cfg.Connection.Host,
		"port", cfg.Connection.Port,
		"database", cfg.Connection.Database,
	)

	return pool, nil
}

// ValidateConnection validates the database connection by executing a version query
func ValidateConnection(ctx context.Context, pool *pgxpool.Pool) error {
	var version string
	err := pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return fmt.Errorf("connection validation failed: %w", err)
	}

	// Connection is valid
	return nil
}

// GetServerVersion retrieves the PostgreSQL server version
func GetServerVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	logger.Debug("Querying PostgreSQL server version")
	var version string
	err := pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		logger.Error("Failed to get server version", "error", err)
		return "", fmt.Errorf("failed to get server version: %w", err)
	}
	logger.Debug("Server version retrieved", "version", version)
	return version, nil
}

// TestConnection tests if a connection can be established with the given configuration
func TestConnection(ctx context.Context, cfg *config.Config) error {
	pool, err := NewConnectionPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return nil
}
