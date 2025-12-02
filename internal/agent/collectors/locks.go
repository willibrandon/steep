package collectors

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/db/queries"
)

// LocksCollector collects lock and blocking information at regular intervals.
type LocksCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	interval     time.Duration
	instanceName string
}

// NewLocksCollector creates a new locks collector.
func NewLocksCollector(pool *pgxpool.Pool, sqliteDB *sql.DB, interval time.Duration, instanceName string) *LocksCollector {
	return &LocksCollector{
		pool:         pool,
		sqliteDB:     sqliteDB,
		interval:     interval,
		instanceName: instanceName,
	}
}

// Name returns the collector name.
func (c *LocksCollector) Name() string {
	return "locks"
}

// Interval returns the collection interval.
func (c *LocksCollector) Interval() time.Duration {
	return c.interval
}

// Collect fetches lock data and stores blocking information.
func (c *LocksCollector) Collect(ctx context.Context) error {
	// Get locks
	locks, err := queries.GetLocks(ctx, c.pool)
	if err != nil {
		return err
	}

	// Get blocking relationships
	blocking, err := queries.GetBlockingRelationships(ctx, c.pool)
	if err != nil {
		// Continue without blocking info
		blocking = nil
	}

	// Store lock snapshot
	return c.storeSnapshot(ctx, len(locks), len(blocking), blocking)
}

// storeSnapshot stores the lock snapshot in SQLite.
func (c *LocksCollector) storeSnapshot(ctx context.Context, lockCount, blockingCount int, blocking interface{}) error {
	// Ensure table exists
	if err := c.initSchema(); err != nil {
		return err
	}

	query := `
		INSERT INTO lock_snapshots (instance_name, timestamp, lock_count, blocking_count)
		VALUES (?, ?, ?, ?)`

	_, err := c.sqliteDB.ExecContext(ctx, query,
		c.instanceName,
		time.Now(),
		lockCount,
		blockingCount,
	)
	return err
}

// initSchema creates the lock_snapshots table if it doesn't exist.
func (c *LocksCollector) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS lock_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		instance_name TEXT NOT NULL DEFAULT 'default',
		timestamp TIMESTAMP NOT NULL,
		lock_count INTEGER NOT NULL,
		blocking_count INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_lock_snapshots_instance_time
		ON lock_snapshots(instance_name, timestamp);`
	_, err := c.sqliteDB.Exec(schema)
	return err
}
