package collectors

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// QueriesCollector collects query statistics using the same approach as the TUI.
// It delegates to the queries.Monitor which uses log parsing or pg_stat_activity sampling.
type QueriesCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	store        *sqlite.QueryStatsStore
	interval     time.Duration
	instanceName string

	// The actual monitor that does the work (same as TUI uses)
	monitor *queries.Monitor
	started bool
	mu      sync.Mutex
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

// Collect starts the query monitor on first call, then does nothing.
// The monitor handles its own collection loop internally.
func (c *QueriesCollector) Collect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Start monitor on first call
	if !c.started {
		logger.Info("queries_collector: starting query monitor")

		// Create monitor config
		config := queries.MonitorConfig{
			RefreshInterval:   c.interval,
			RetentionDays:     7,
			AutoEnableLogging: false, // Don't prompt in agent mode
		}

		// Create the monitor (same as TUI uses)
		c.monitor = queries.NewMonitor(c.pool, c.store, config)

		// Configure first - exactly like TUI does
		logger.Debug("queries_collector: calling Configure()")
		_ = c.monitor.Configure(context.Background())

		// Start monitoring - this loads saved positions and continues from where it left off
		// (same as TUI normal startup, NOT the manual reset which uses ParseWithProgress)
		logger.Debug("queries_collector: calling Start()")
		if err := c.monitor.Start(context.Background()); err != nil {
			logger.Error("queries_collector: Start() failed", "error", err)
			return err
		}
		logger.Info("queries_collector: started successfully")
		c.started = true
	}

	// The monitor handles its own collection loop internally.
	// Nothing to do here on subsequent calls.
	return nil
}
