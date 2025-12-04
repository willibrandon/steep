package collectors

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
)

// ActivityCollector collects pg_stat_activity data at regular intervals.
type ActivityCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	interval     time.Duration
	instanceName string
}

// NewActivityCollector creates a new activity collector.
func NewActivityCollector(pool *pgxpool.Pool, sqliteDB *sql.DB, interval time.Duration, instanceName string) *ActivityCollector {
	return &ActivityCollector{
		pool:         pool,
		sqliteDB:     sqliteDB,
		interval:     interval,
		instanceName: instanceName,
	}
}

// Name returns the collector name.
func (c *ActivityCollector) Name() string {
	return "activity"
}

// Interval returns the collection interval.
func (c *ActivityCollector) Interval() time.Duration {
	return c.interval
}

// Collect fetches activity data and persists to SQLite.
func (c *ActivityCollector) Collect(ctx context.Context) error {
	// Fetch all connections without filtering for comprehensive snapshot
	filter := models.ActivityFilter{ShowAllDatabases: true}
	connections, err := queries.GetActivityConnections(ctx, c.pool, filter, 1000, 0)
	if err != nil {
		return err
	}

	// Get connection counts by state for summary storage
	stats := summarizeConnections(connections)

	// Store snapshot in SQLite
	return c.storeSnapshot(ctx, stats)
}

// connectionStats holds summarized connection statistics.
type connectionStats struct {
	TotalConnections   int
	ActiveConnections  int
	IdleConnections    int
	IdleInTx           int
	WaitingConnections int
	Timestamp          time.Time
}

// summarizeConnections creates a summary of connection states.
func summarizeConnections(connections []models.Connection) connectionStats {
	stats := connectionStats{
		TotalConnections: len(connections),
		Timestamp:        time.Now(),
	}

	for _, conn := range connections {
		switch conn.State {
		case "active":
			stats.ActiveConnections++
		case "idle":
			stats.IdleConnections++
		case "idle in transaction", "idle in transaction (aborted)":
			stats.IdleInTx++
		}
		if conn.WaitEventType != "" {
			stats.WaitingConnections++
		}
	}

	return stats
}

// storeSnapshot stores the activity snapshot in SQLite.
func (c *ActivityCollector) storeSnapshot(ctx context.Context, stats connectionStats) error {
	// Ensure table exists
	if err := c.initSchema(); err != nil {
		return err
	}

	query := `
		INSERT INTO activity_snapshots (instance_name, timestamp, total_connections,
			active_connections, idle_connections, idle_in_tx, waiting_connections)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := c.sqliteDB.ExecContext(ctx, query,
		c.instanceName,
		stats.Timestamp,
		stats.TotalConnections,
		stats.ActiveConnections,
		stats.IdleConnections,
		stats.IdleInTx,
		stats.WaitingConnections,
	)
	return err
}

// initSchema creates the activity_snapshots table if it doesn't exist.
func (c *ActivityCollector) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS activity_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		instance_name TEXT NOT NULL DEFAULT 'default',
		timestamp TIMESTAMP NOT NULL,
		total_connections INTEGER NOT NULL,
		active_connections INTEGER NOT NULL,
		idle_connections INTEGER NOT NULL,
		idle_in_tx INTEGER NOT NULL,
		waiting_connections INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_activity_snapshots_instance_time
		ON activity_snapshots(instance_name, timestamp);`
	_, err := c.sqliteDB.Exec(schema)
	return err
}
