package collectors

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/db/queries"
)

// MetricsCollector collects dashboard metrics at regular intervals.
type MetricsCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	interval     time.Duration
	instanceName string

	// State for TPS calculation
	lastTotalXacts int64
	lastTimestamp  time.Time
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(pool *pgxpool.Pool, sqliteDB *sql.DB, interval time.Duration, instanceName string) *MetricsCollector {
	return &MetricsCollector{
		pool:         pool,
		sqliteDB:     sqliteDB,
		interval:     interval,
		instanceName: instanceName,
	}
}

// Name returns the collector name.
func (c *MetricsCollector) Name() string {
	return "metrics"
}

// Interval returns the collection interval.
func (c *MetricsCollector) Interval() time.Duration {
	return c.interval
}

// Collect fetches database metrics and stores them.
func (c *MetricsCollector) Collect(ctx context.Context) error {
	// Get database stats
	snapshot, err := queries.GetDatabaseStats(ctx, c.pool)
	if err != nil {
		return err
	}

	// Get database size
	dbSize, err := queries.GetDatabaseSize(ctx, c.pool)
	if err != nil {
		return err
	}

	// Get connection count
	connCount, err := queries.GetTotalConnectionCount(ctx, c.pool)
	if err != nil {
		return err
	}

	// Calculate TPS
	var tps float64
	now := time.Now()
	if c.lastTotalXacts > 0 && !c.lastTimestamp.IsZero() {
		elapsed := now.Sub(c.lastTimestamp).Seconds()
		if elapsed > 0 {
			xactDelta := snapshot.TotalXacts - c.lastTotalXacts
			if xactDelta >= 0 {
				tps = float64(xactDelta) / elapsed
			}
		}
	}
	c.lastTotalXacts = snapshot.TotalXacts
	c.lastTimestamp = now

	// Calculate cache hit ratio
	var cacheHitRatio float64
	totalBlocks := snapshot.BlksHit + snapshot.BlksRead
	if totalBlocks > 0 {
		cacheHitRatio = float64(snapshot.BlksHit) / float64(totalBlocks) * 100
	} else {
		cacheHitRatio = 100
	}

	// Store metrics
	return c.storeMetrics(ctx, tps, float64(connCount), cacheHitRatio, dbSize)
}

// storeMetrics stores the metrics in SQLite using the TUI's key-value schema.
func (c *MetricsCollector) storeMetrics(ctx context.Context, tps, connections, cacheHitRatio float64, dbSize int64) error {
	now := time.Now()
	query := `INSERT INTO metrics_history (timestamp, metric_name, key, value, instance_name) VALUES (?, ?, ?, ?, ?)`

	// Store each metric as a separate row
	metrics := []struct {
		name  string
		value float64
	}{
		{"tps", tps},
		{"connections", connections},
		{"cache_hit_ratio", cacheHitRatio},
		{"db_size", float64(dbSize)},
	}

	for _, m := range metrics {
		if _, err := c.sqliteDB.ExecContext(ctx, query, now, m.name, "", m.value, c.instanceName); err != nil {
			return err
		}
	}
	return nil
}
