package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoggingConfig contains PostgreSQL logging configuration settings.
type LoggingConfig struct {
	LoggingCollector bool
	LogDirectory     string
	LogFilename      string
	DataDirectory    string
	LogDestination   string
}

// GetLoggingConfig retrieves PostgreSQL logging configuration.
func GetLoggingConfig(ctx context.Context, pool *pgxpool.Pool) (LoggingConfig, error) {
	var config LoggingConfig

	// Check logging_collector setting
	var loggingCollector string
	err := pool.QueryRow(ctx, "SHOW logging_collector").Scan(&loggingCollector)
	if err != nil {
		return config, fmt.Errorf("get logging_collector: %w", err)
	}
	config.LoggingCollector = loggingCollector == "on"

	// Get log directory
	err = pool.QueryRow(ctx, "SHOW log_directory").Scan(&config.LogDirectory)
	if err != nil {
		return config, fmt.Errorf("get log_directory: %w", err)
	}

	// Get log filename pattern
	err = pool.QueryRow(ctx, "SHOW log_filename").Scan(&config.LogFilename)
	if err != nil {
		return config, fmt.Errorf("get log_filename: %w", err)
	}

	// Get data directory (log_directory may be relative to this)
	err = pool.QueryRow(ctx, "SHOW data_directory").Scan(&config.DataDirectory)
	if err != nil {
		return config, fmt.Errorf("get data_directory: %w", err)
	}

	// Get log destination (stderr, csvlog, jsonlog)
	err = pool.QueryRow(ctx, "SHOW log_destination").Scan(&config.LogDestination)
	if err != nil {
		return config, fmt.Errorf("get log_destination: %w", err)
	}

	return config, nil
}

// GetDatabaseName returns the current database name.
func GetDatabaseName(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var name string
	err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name)
	if err != nil {
		return "", fmt.Errorf("get current_database: %w", err)
	}
	return name, nil
}
