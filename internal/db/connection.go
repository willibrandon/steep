package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
)

// NewConnectionPool creates a new PostgreSQL connection pool using the provided configuration
func NewConnectionPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	// Get password using precedence: password_command > PGPASSWORD > interactive prompt
	password, err := GetPassword(cfg.Connection.PasswordCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve password: %w", err)
	}

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

	// Parse connection string and create pool config
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	poolConfig.MaxConns = int32(cfg.Connection.PoolMaxConns)
	poolConfig.MinConns = int32(cfg.Connection.PoolMinConns)
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf(
			"connection refused: ensure PostgreSQL is running on %s:%d (error: %w)",
			cfg.Connection.Host,
			cfg.Connection.Port,
			err,
		)
	}

	// Validate connection with a simple query
	if err := ValidateConnection(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

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
	var version string
	err := pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}
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
