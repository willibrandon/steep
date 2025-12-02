package collectors

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// QueriesCollector collects query statistics using the existing query monitor infrastructure.
type QueriesCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	store        *sqlite.QueryStatsStore
	interval     time.Duration
	instanceName string
}

// NewQueriesCollector creates a new queries collector.
func NewQueriesCollector(pool *pgxpool.Pool, sqliteDB *sql.DB, store *sqlite.QueryStatsStore, interval time.Duration, instanceName string) *QueriesCollector {
	return &QueriesCollector{
		pool:         pool,
		sqliteDB:     sqliteDB,
		store:        store,
		interval:     interval,
		instanceName: instanceName,
	}
}

// Name returns the collector name.
func (c *QueriesCollector) Name() string {
	return "queries"
}

// Interval returns the collection interval.
func (c *QueriesCollector) Interval() time.Duration {
	return c.interval
}

// Collect fetches query statistics from pg_stat_statements if available.
// This collector samples currently running queries and stores them for analysis.
func (c *QueriesCollector) Collect(ctx context.Context) error {
	// Check if pg_stat_statements is available
	if !c.hasPgStatStatements(ctx) {
		// Fall back to sampling from pg_stat_activity
		return c.collectFromActivity(ctx)
	}

	return c.collectFromStatStatements(ctx)
}

// hasPgStatStatements checks if the pg_stat_statements extension is available.
func (c *QueriesCollector) hasPgStatStatements(ctx context.Context) bool {
	var count int
	err := c.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_extension WHERE extname = 'pg_stat_statements'
	`).Scan(&count)
	return err == nil && count > 0
}

// collectFromStatStatements collects from pg_stat_statements extension.
func (c *QueriesCollector) collectFromStatStatements(ctx context.Context) error {
	rows, err := c.pool.Query(ctx, `
		SELECT
			queryid,
			query,
			calls,
			total_exec_time,
			mean_exec_time,
			rows,
			shared_blks_hit,
			shared_blks_read
		FROM pg_stat_statements
		WHERE query NOT LIKE '%pg_stat_statements%'
		ORDER BY total_exec_time DESC
		LIMIT 100
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			queryID       int64
			query         string
			calls         int64
			totalExecTime float64
			meanExecTime  float64
			rowCount      int64
			blksHit       int64
			blksRead      int64
		)

		err := rows.Scan(&queryID, &query, &calls, &totalExecTime, &meanExecTime, &rowCount, &blksHit, &blksRead)
		if err != nil {
			continue
		}

		// Store the query stats
		if c.store != nil {
			fingerprint := formatQueryID(queryID)
			_ = c.store.Upsert(ctx, fingerprint, query, meanExecTime, rowCount, "")
		}
	}

	return rows.Err()
}

// collectFromActivity samples currently running queries from pg_stat_activity.
func (c *QueriesCollector) collectFromActivity(ctx context.Context) error {
	rows, err := c.pool.Query(ctx, `
		SELECT
			pid,
			query,
			EXTRACT(EPOCH FROM (now() - query_start)) * 1000 as duration_ms,
			state
		FROM pg_stat_activity
		WHERE state = 'active'
			AND query NOT LIKE '%pg_stat_activity%'
			AND pid != pg_backend_pid()
		ORDER BY query_start
		LIMIT 50
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			pid        int
			query      string
			durationMs float64
			state      string
		)

		err := rows.Scan(&pid, &query, &durationMs, &state)
		if err != nil {
			continue
		}

		// Store sampled query
		if c.store != nil && query != "" {
			fingerprint := hashQuery(query)
			_ = c.store.Upsert(ctx, fingerprint, query, durationMs, 0, "")
		}
	}

	return rows.Err()
}

// formatQueryID converts a pg_stat_statements queryid to uint64 fingerprint.
func formatQueryID(queryID int64) uint64 {
	// queryID is already a unique hash from PostgreSQL
	return uint64(queryID)
}

// hashQuery creates a simple hash for query fingerprinting.
func hashQuery(query string) uint64 {
	// Simple FNV-1a hash
	var h uint64 = 14695981039346656037
	for i := 0; i < len(query); i++ {
		h ^= uint64(query[i])
		h *= 1099511628211
	}
	return h
}
