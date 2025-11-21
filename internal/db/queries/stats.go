package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// GetDatabaseStats retrieves statistics from pg_stat_database for TPS and cache ratio calculation.
func GetDatabaseStats(ctx context.Context, pool *pgxpool.Pool) (models.MetricsSnapshot, error) {
	query := `
		SELECT
			COALESCE(SUM(xact_commit + xact_rollback), 0) as total_xacts,
			COALESCE(SUM(blks_hit), 0) as blks_hit,
			COALESCE(SUM(blks_read), 0) as blks_read
		FROM pg_stat_database
	`

	var snapshot models.MetricsSnapshot
	err := pool.QueryRow(ctx, query).Scan(
		&snapshot.TotalXacts,
		&snapshot.BlksHit,
		&snapshot.BlksRead,
	)
	if err != nil {
		return models.MetricsSnapshot{}, fmt.Errorf("query pg_stat_database: %w", err)
	}

	return snapshot, nil
}

// GetDatabaseSize retrieves the size of the current database in bytes.
func GetDatabaseSize(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var size int64
	err := pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("query database size: %w", err)
	}
	return size, nil
}

// GetConnectionCount retrieves the total number of connections.
func GetTotalConnectionCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count connections: %w", err)
	}
	return count, nil
}

// GetMaxConnections retrieves the maximum allowed connections.
func GetMaxConnections(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var max int
	err := pool.QueryRow(ctx, "SHOW max_connections").Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("get max_connections: %w", err)
	}
	return max, nil
}
