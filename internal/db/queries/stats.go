package queries

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/logger"
)

// pgStatStatementsAvailable caches the availability check per pool to avoid repeated queries.
var (
	pgssAvailability = make(map[*pgxpool.Pool]bool)
	pgssAvailMu      sync.RWMutex
)

// checkPgStatStatements checks if pg_stat_statements extension is available.
func checkPgStatStatements(ctx context.Context, pool *pgxpool.Pool) bool {
	pgssAvailMu.RLock()
	available, checked := pgssAvailability[pool]
	pgssAvailMu.RUnlock()

	if checked {
		return available
	}

	// Check if extension is installed
	var exists bool
	err := pool.QueryRow(ctx, `
		/* steep:internal */
		SELECT EXISTS(
			SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'
		)
	`).Scan(&exists)

	if err != nil {
		exists = false
	}

	pgssAvailMu.Lock()
	pgssAvailability[pool] = exists
	pgssAvailMu.Unlock()

	if exists {
		logger.Debug("stats: pg_stat_statements available, using for TPS calculation (excludes steep queries)")
	} else {
		logger.Debug("stats: pg_stat_statements not available, using pg_stat_database for TPS (includes all queries)")
	}

	return exists
}

// GetDatabaseStats retrieves statistics for TPS and cache ratio calculation.
// Uses pg_stat_statements if available (excludes steep queries from TPS),
// falls back to pg_stat_database otherwise.
func GetDatabaseStats(ctx context.Context, pool *pgxpool.Pool) (models.MetricsSnapshot, error) {
	var snapshot models.MetricsSnapshot

	// Always get cache stats from pg_stat_database
	cacheQuery := `
		/* steep:internal */
		SELECT
			COALESCE(SUM(blks_hit), 0) as blks_hit,
			COALESCE(SUM(blks_read), 0) as blks_read
		FROM pg_stat_database
	`
	err := pool.QueryRow(ctx, cacheQuery).Scan(&snapshot.BlksHit, &snapshot.BlksRead)
	if err != nil {
		return models.MetricsSnapshot{}, fmt.Errorf("query pg_stat_database cache stats: %w", err)
	}

	// Get transaction count - prefer pg_stat_statements to exclude steep queries
	if checkPgStatStatements(ctx, pool) {
		// Use pg_stat_statements: sum calls from non-steep queries only
		pgssQuery := `
			/* steep:internal */
			SELECT COALESCE(SUM(calls), 0)
			FROM pg_stat_statements
			WHERE query NOT LIKE '%steep:internal%'
		`
		err = pool.QueryRow(ctx, pgssQuery).Scan(&snapshot.TotalXacts)
		if err != nil {
			// Fall back to pg_stat_database on error
			logger.Debug("stats: pg_stat_statements query failed, falling back", "error", err)
			return getDatabaseStatsFallback(ctx, pool, snapshot)
		}
	} else {
		// Fallback: use pg_stat_database (includes all queries)
		return getDatabaseStatsFallback(ctx, pool, snapshot)
	}

	return snapshot, nil
}

// getDatabaseStatsFallback uses pg_stat_database for TPS when pg_stat_statements is unavailable.
func getDatabaseStatsFallback(ctx context.Context, pool *pgxpool.Pool, snapshot models.MetricsSnapshot) (models.MetricsSnapshot, error) {
	query := `
		/* steep:internal */
		SELECT COALESCE(SUM(xact_commit + xact_rollback), 0) as total_xacts
		FROM pg_stat_database
	`
	err := pool.QueryRow(ctx, query).Scan(&snapshot.TotalXacts)
	if err != nil {
		return models.MetricsSnapshot{}, fmt.Errorf("query pg_stat_database: %w", err)
	}
	return snapshot, nil
}

// GetDatabaseSize retrieves the size of the current database in bytes.
func GetDatabaseSize(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var size int64
	err := pool.QueryRow(ctx, "/* steep:internal */ SELECT pg_database_size(current_database())").Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("query database size: %w", err)
	}
	return size, nil
}

// GetConnectionCount retrieves the total number of connections.
// Excludes steep's own connections (application_name = 'steep-internal')
func GetTotalConnectionCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	err := pool.QueryRow(ctx, "/* steep:internal */ SELECT COUNT(*) FROM pg_stat_activity WHERE application_name != 'steep-internal'").Scan(&count)
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
