package agent

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db/models"
)

// Collector is the interface for all data collectors.
type Collector interface {
	// Name returns the collector name for logging.
	Name() string
	// Collect performs a single collection cycle.
	Collect(ctx context.Context) error
	// Interval returns the collection interval.
	Interval() time.Duration
}

// CollectorCoordinator manages multiple collectors with goroutine-per-collector pattern.
type CollectorCoordinator struct {
	collectors []Collector
	pool       *pgxpool.Pool
	sqliteDB   *sql.DB
	cfg        *config.AgentConfig
	logger     Logger

	statusStore *AgentStatusStore

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Error tracking
	mu         sync.RWMutex
	errorCount int
	lastError  string
}

// Logger is a simple logging interface.
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// NewCollectorCoordinator creates a new coordinator with the given configuration.
func NewCollectorCoordinator(pool *pgxpool.Pool, sqliteDB *sql.DB, cfg *config.AgentConfig, statusStore *AgentStatusStore, logger Logger) *CollectorCoordinator {
	return &CollectorCoordinator{
		collectors:  make([]Collector, 0),
		pool:        pool,
		sqliteDB:    sqliteDB,
		cfg:         cfg,
		statusStore: statusStore,
		logger:      logger,
	}
}

// RegisterCollector adds a collector to be managed.
func (c *CollectorCoordinator) RegisterCollector(collector Collector) {
	c.collectors = append(c.collectors, collector)
}

// Start begins all registered collectors.
func (c *CollectorCoordinator) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	c.logger.Printf("Starting %d collectors", len(c.collectors))

	for _, collector := range c.collectors {
		c.wg.Add(1)
		go c.runCollector(collector)
	}

	return nil
}

// Stop gracefully stops all collectors.
func (c *CollectorCoordinator) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	c.logger.Println("All collectors stopped")
}

// runCollector runs a single collector in its own goroutine.
func (c *CollectorCoordinator) runCollector(collector Collector) {
	defer c.wg.Done()

	interval := collector.Interval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.logger.Printf("Collector %s started (interval: %v)", collector.Name(), interval)

	// Initial collection
	c.doCollect(collector)

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Printf("Collector %s stopping", collector.Name())
			return
		case <-ticker.C:
			c.doCollect(collector)
		}
	}
}

// doCollect performs a single collection and handles errors.
func (c *CollectorCoordinator) doCollect(collector Collector) {
	err := collector.Collect(c.ctx)
	if err != nil {
		c.recordError(collector.Name(), err)
		return
	}

	// Update last_collect timestamp on successful collection
	c.updateLastCollect()
}

// recordError records a collection error.
func (c *CollectorCoordinator) recordError(collectorName string, err error) {
	c.mu.Lock()
	c.errorCount++
	c.lastError = collectorName + ": " + err.Error()
	c.mu.Unlock()

	// T077: Check for disk full errors - log warning but don't crash
	if IsDiskFullError(err) {
		c.logger.Printf("WARNING: Disk full detected during %s collection - data may not be persisted", collectorName)
		c.logger.Printf("Consider freeing disk space or adjusting retention settings")
	} else {
		c.logger.Printf("Collector %s error: %v", collectorName, err)
	}

	// Update error count in status store
	if c.statusStore != nil {
		_ = c.statusStore.IncrementErrorCount(c.lastError)
	}
}

// updateLastCollect updates the last_collect timestamp in the status store.
func (c *CollectorCoordinator) updateLastCollect() {
	if c.statusStore != nil {
		_ = c.statusStore.UpdateLastCollect(time.Now())
	}
}

// ErrorCount returns the total error count.
func (c *CollectorCoordinator) ErrorCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.errorCount
}

// LastError returns the most recent error message.
func (c *CollectorCoordinator) LastError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

// CollectionResult holds the result of a collection cycle.
type CollectionResult struct {
	CollectorName string
	Timestamp     time.Time
	Duration      time.Duration
	Error         error
	ItemsCount    int
}

// ActivityCollectorResult holds activity collection results.
type ActivityCollectorResult struct {
	Connections []models.Connection
	Timestamp   time.Time
	Error       error
}

// ReplicationCollectorResult holds replication collection results.
type ReplicationCollectorResult struct {
	Replicas  []models.Replica
	Slots     []models.ReplicationSlot
	IsPrimary bool
	Timestamp time.Time
	Error     error
}

// LocksCollectorResult holds locks collection results.
type LocksCollectorResult struct {
	Locks     []models.Lock
	Blocking  []models.BlockingRelationship
	Timestamp time.Time
	Error     error
}

// MetricsCollectorResult holds metrics collection results.
type MetricsCollectorResult struct {
	Metrics   models.Metrics
	Timestamp time.Time
	Error     error
}
