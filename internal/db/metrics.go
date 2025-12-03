package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseMetrics holds metrics retrieved from PostgreSQL
type DatabaseMetrics struct {
	ActiveConnections int
	TotalConnections  int
	ServerVersion     string
}

// QueryActiveConnections queries the number of active connections from pg_stat_activity
// Excludes steep's own connections (application_name = 'steep-internal')
func QueryActiveConnections(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	query := "/* steep:internal */ SELECT COUNT(*) FROM pg_stat_activity WHERE state = 'active' AND application_name != 'steep-internal'"

	err := pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to query active connections: %w", err)
	}

	return count, nil
}

// QueryTotalConnections queries the total number of connections from pg_stat_activity
// Excludes steep's own connections (application_name = 'steep-internal')
func QueryTotalConnections(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	query := "/* steep:internal */ SELECT COUNT(*) FROM pg_stat_activity WHERE application_name != 'steep-internal'"

	err := pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to query total connections: %w", err)
	}

	return count, nil
}

// QueryDatabaseMetrics retrieves all database metrics
func QueryDatabaseMetrics(ctx context.Context, pool *pgxpool.Pool) (*DatabaseMetrics, error) {
	metrics := &DatabaseMetrics{}

	// Query active connections
	activeConns, err := QueryActiveConnections(ctx, pool)
	if err != nil {
		return nil, err
	}
	metrics.ActiveConnections = activeConns

	// Query total connections
	totalConns, err := QueryTotalConnections(ctx, pool)
	if err != nil {
		return nil, err
	}
	metrics.TotalConnections = totalConns

	// Query server version
	version, err := GetServerVersion(ctx, pool)
	if err != nil {
		return nil, err
	}
	metrics.ServerVersion = version

	return metrics, nil
}
