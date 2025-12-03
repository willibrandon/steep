package agent

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/config"
)

// RetentionManager handles automatic data pruning based on configured retention periods.
// It runs an hourly ticker that prunes data older than the configured retention
// for each data type, using DELETE with LIMIT to avoid long-running transactions.
type RetentionManager struct {
	db        *sql.DB
	retention *config.AgentRetentionConfig
	logger    *log.Logger
	debug     bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// pruneLimit is the maximum rows to delete per prune operation
	// to avoid long-running transactions that could block readers.
	pruneLimit int
}

// tableRetention maps table names to their timestamp column and retention config getter.
type tableRetention struct {
	tableName       string
	timestampColumn string
	getRetention    func(*config.AgentRetentionConfig) time.Duration
}

// tables defines the tables to prune and their retention config mappings.
// Note: Different tables use different timestamp column names.
//
// Tables pruned by agent retention manager:
// - activity_snapshots: timestamp (ActivityHistory - 24h)
// - lock_snapshots: timestamp (LockHistory - 24h)
// - deadlock_events: detected_at (LockHistory - 24h, CASCADE deletes deadlock_processes)
// - replication_lag_history: timestamp (ReplicationLag - 24h)
// - metrics_history: timestamp (Metrics - 24h)
// - query_stats: last_seen (QueryStats - 7d)
//
// Tables NOT pruned by agent (handled elsewhere or user data):
// - alert_events: pruned by TUI's startAlertHistoryPruner (alerts.history_retention - 30d)
// - query_history: SQL editor history (user preference, not agent-collected)
// - log_command_history: user command history
// - log_search_history: user search history
// - log_positions: position tracking metadata
// - agent_status: single row, managed by agent lifecycle
// - agent_instances: instance metadata, managed by agent
var tables = []tableRetention{
	{
		tableName:       "activity_snapshots",
		timestampColumn: "timestamp",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.ActivityHistory },
	},
	{
		tableName:       "lock_snapshots",
		timestampColumn: "timestamp",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.LockHistory },
	},
	{
		tableName:       "deadlock_events",
		timestampColumn: "detected_at",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.LockHistory },
	},
	{
		tableName:       "replication_lag_history",
		timestampColumn: "timestamp",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.ReplicationLag },
	},
	{
		tableName:       "metrics_history",
		timestampColumn: "timestamp",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.Metrics },
	},
	{
		tableName:       "query_stats",
		timestampColumn: "last_seen",
		getRetention:    func(r *config.AgentRetentionConfig) time.Duration { return r.QueryStats },
	},
}

// NewRetentionManager creates a new RetentionManager with the given configuration.
func NewRetentionManager(db *sql.DB, retention *config.AgentRetentionConfig, logger *log.Logger, debug bool) *RetentionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &RetentionManager{
		db:         db,
		retention:  retention,
		logger:     logger,
		debug:      debug,
		ctx:        ctx,
		cancel:     cancel,
		pruneLimit: 10000, // Delete at most 10,000 rows per prune operation
	}
}

// Start begins the retention manager's hourly prune cycle.
// It also runs an initial prune immediately on startup.
func (rm *RetentionManager) Start() {
	rm.logger.Println("Starting retention manager")

	// Run initial prune immediately on startup (T047)
	rm.pruneAll()

	// Start hourly ticker
	rm.wg.Add(1)
	go rm.runHourlyPrune()
}

// Stop gracefully shuts down the retention manager.
func (rm *RetentionManager) Stop() {
	rm.logger.Println("Stopping retention manager")
	rm.cancel()
	rm.wg.Wait()
}

// runHourlyPrune runs the prune cycle every hour.
func (rm *RetentionManager) runHourlyPrune() {
	defer rm.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.pruneAll()
		}
	}
}

// pruneAll prunes all tables based on their configured retention periods.
func (rm *RetentionManager) pruneAll() {
	if rm.debug {
		rm.logger.Println("Running retention prune cycle")
	}

	totalPruned := 0
	for _, table := range tables {
		retention := table.getRetention(rm.retention)
		pruned, err := rm.pruneTable(table.tableName, table.timestampColumn, retention)
		if err != nil {
			rm.logger.Printf("Failed to prune %s: %v", table.tableName, err)
			continue
		}
		totalPruned += pruned
		if rm.debug && pruned > 0 {
			rm.logger.Printf("Pruned %d rows from %s (retention: %v)", pruned, table.tableName, retention)
		}
	}

	if rm.debug && totalPruned > 0 {
		rm.logger.Printf("Retention prune cycle complete: %d total rows pruned", totalPruned)
	}
}

// pruneTable deletes rows older than the retention period from the specified table.
// It uses DELETE with LIMIT to avoid long-running transactions that could block
// concurrent TUI reads. The pruning is done in batches until no more rows need deletion.
//
// T044: Per-data-type pruning with DELETE LIMIT
// T048: WAL mode ensures pruning does not block concurrent TUI reads
func (rm *RetentionManager) pruneTable(tableName, timestampColumn string, retention time.Duration) (int, error) {
	cutoff := time.Now().Add(-retention)
	totalPruned := 0

	// Use batched deletes to avoid long transactions
	// SQLite with WAL mode allows concurrent readers during writes
	for {
		// Check context for shutdown
		select {
		case <-rm.ctx.Done():
			return totalPruned, rm.ctx.Err()
		default:
		}

		// Delete a batch of rows
		// Note: SQLite doesn't support DELETE ... LIMIT directly in all versions,
		// but we can use a subquery with rowid for efficient batched deletes.
		query := `DELETE FROM ` + tableName + ` WHERE rowid IN (
			SELECT rowid FROM ` + tableName + `
			WHERE ` + timestampColumn + ` < ?
			LIMIT ?
		)`

		result, err := rm.db.ExecContext(rm.ctx, query, cutoff, rm.pruneLimit)
		if err != nil {
			return totalPruned, err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalPruned, err
		}

		totalPruned += int(rowsAffected)

		// If we deleted fewer than the limit, we're done
		if rowsAffected < int64(rm.pruneLimit) {
			break
		}

		// Small sleep between batches to reduce contention with readers
		time.Sleep(10 * time.Millisecond)
	}

	return totalPruned, nil
}

// PruneNow runs an immediate prune cycle. Useful for testing.
func (rm *RetentionManager) PruneNow() {
	rm.pruneAll()
}
