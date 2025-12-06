package init

import (
	"context"
	"fmt"
	"time"

	"github.com/VividCortex/ewma"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/repl/models"
)

// copyProgress holds the current state from pg_stat_progress_copy.
type copyProgress struct {
	BytesProcessed  int64
	BytesTotal      int64
	TuplesProcessed int64
}

// Reinitializer handles reinitialization of diverged or corrupted nodes.
type Reinitializer struct {
	pool    *pgxpool.Pool
	manager *Manager
}

// NewReinitializer creates a new reinitializer.
func NewReinitializer(pool *pgxpool.Pool, manager *Manager) *Reinitializer {
	return &Reinitializer{
		pool:    pool,
		manager: manager,
	}
}

// ReinitScope defines the scope of reinitialization.
type ReinitScope struct {
	Full   bool     // Full node reinit
	Tables []string // Specific tables (schema.table format)
	Schema string   // All tables in schema
}

// ReinitOptions configures reinitialization behavior.
type ReinitOptions struct {
	NodeID          string
	SourceNodeID    string // Auto-selected if empty
	Scope           ReinitScope
	ParallelWorkers int
}

// ReinitResult contains the result of a reinitialization operation.
type ReinitResult struct {
	TablesAffected int
	FinalState     models.InitState
}

// Start begins reinitialization for the specified scope and returns the result.
func (r *Reinitializer) Start(ctx context.Context, opts ReinitOptions) (*ReinitResult, error) {
	logger.Debug("Reinitializer.Start: beginning reinit",
		"node_id", opts.NodeID,
		"source_node_id", opts.SourceNodeID,
		"full", opts.Scope.Full,
		"schema", opts.Scope.Schema,
		"tables", opts.Scope.Tables,
	)

	// For full reinit, we do a simple reset to UNINITIALIZED
	// This drops the subscription and resets state so init can run again
	if opts.Scope.Full {
		tablesCount, err := r.fullReinit(ctx, opts.NodeID)
		if err != nil {
			logger.Error("Reinitializer.Start: full reinit failed", "node_id", opts.NodeID, "error", err)
			return nil, err
		}
		return &ReinitResult{
			TablesAffected: tablesCount,
			FinalState:     models.InitStateUninitialized,
		}, nil
	}

	// Table/schema-level reinit
	// Update node state to REINITIALIZING
	if err := r.updateState(ctx, opts.NodeID, models.InitStateReinitializing); err != nil {
		return nil, fmt.Errorf("failed to update state: %w", err)
	}

	// Determine tables to reinitialize
	tables, err := r.resolveTables(ctx, opts)
	if err != nil {
		logger.Error("Reinitializer.Start: failed to resolve tables", "node_id", opts.NodeID, "error", err)
		return nil, fmt.Errorf("failed to resolve tables: %w", err)
	}

	// Get table sizes for accurate ETA estimation
	totalBytes, err := r.getTableSizes(ctx, tables)
	if err != nil {
		logger.Error("Reinitializer.Start: failed to get table sizes", "node_id", opts.NodeID, "error", err)
		totalBytes = 0
	}

	// Get persisted throughput from previous sync, or use 50 MB/s baseline
	baselineThroughput, _ := r.getPersistedThroughput(ctx, opts.NodeID)
	if baselineThroughput <= 0 {
		baselineThroughput = 50.0 * 1024 * 1024 // Default: 50 MB/s
	}

	// Calculate initial ETA based on table sizes and baseline throughput
	var initialETA int
	if totalBytes > 0 {
		initialETA = max(int(float64(totalBytes)/baselineThroughput),
			// Minimum 1 second ETA
			1)
	} else {
		// Fallback if we couldn't get sizes
		initialETA = len(tables) * 5
	}
	r.updateProgressWithETA(ctx, opts.NodeID, "preparing", 0, len(tables), 0, "", initialETA)

	logger.Debug("Reinitializer.Start: calculated initial ETA",
		"node_id", opts.NodeID,
		"tables", len(tables),
		"total_bytes", totalBytes,
		"initial_eta_seconds", initialETA,
	)

	// Pause replication for affected tables
	if err := r.pauseReplication(ctx, opts.NodeID, tables); err != nil {
		logger.Error("Reinitializer.Start: failed to pause replication", "node_id", opts.NodeID, "error", err)
		return nil, fmt.Errorf("failed to pause replication: %w", err)
	}

	// Truncate and recopy tables
	truncateStart := time.Now()
	for i, table := range tables {
		percent := float64(i) / float64(len(tables)) * 50
		// Update ETA: remaining truncate time + sync time (based on table sizes)
		truncateElapsed := time.Since(truncateStart).Seconds()
		remainingTables := len(tables) - i
		// Estimate remaining truncate time based on elapsed rate, or 1s/table if no data yet
		var remainingTruncateTime float64
		if i > 0 {
			avgTruncatePerTable := truncateElapsed / float64(i)
			remainingTruncateTime = avgTruncatePerTable * float64(remainingTables)
		} else {
			remainingTruncateTime = float64(remainingTables)
		}
		// Sync time based on table sizes and persisted/default throughput
		var syncTime float64
		if totalBytes > 0 {
			syncTime = float64(totalBytes) / baselineThroughput
		} else {
			syncTime = float64(len(tables)) * 5
		}
		etaSeconds := int(remainingTruncateTime + syncTime)
		r.updateProgressWithETA(ctx, opts.NodeID, "preparing", percent, len(tables), i, table, etaSeconds)
		if err := r.reinitTable(ctx, opts, table); err != nil {
			logger.Error("Reinitializer.Start: failed to reinit table", "node_id", opts.NodeID, "table", table, "error", err)
			return nil, fmt.Errorf("failed to reinit table %s: %w", table, err)
		}
	}

	// Update progress to catching_up phase with size-based ETA
	var syncETA int
	if totalBytes > 0 {
		syncETA = int(float64(totalBytes) / baselineThroughput)
	} else {
		syncETA = len(tables) * 5 // Fallback
	}
	r.updateProgressWithETA(ctx, opts.NodeID, "catching_up", 50, len(tables), len(tables), "", syncETA)

	// Resume replication - state stays REINITIALIZING until data sync completes
	if err := r.resumeReplication(ctx, opts.NodeID, tables, totalBytes); err != nil {
		logger.Error("Reinitializer.Start: failed to resume replication", "node_id", opts.NodeID, "error", err)
		return nil, fmt.Errorf("failed to resume replication: %w", err)
	}

	return &ReinitResult{
		TablesAffected: len(tables),
		FinalState:     models.InitStateReinitializing,
	}, nil
}

// fullReinit performs a full node reinitialization by dropping subscriptions,
// truncating replicated tables, and resetting state to UNINITIALIZED.
// Returns the number of tables that were truncated.
func (r *Reinitializer) fullReinit(ctx context.Context, nodeID string) (int, error) {
	// Find and drop any existing subscriptions for this node
	// Sanitize nodeID the same way as snapshot.go does for subscription names
	sanitizedID := sanitizeIdentifier(nodeID)
	query := `SELECT subname FROM pg_subscription WHERE subname LIKE $1`
	pattern := "steep_sub_" + sanitizedID + "_%"

	rows, err := r.pool.Query(ctx, query, pattern)
	if err != nil {
		logger.Error("fullReinit: failed to query subscriptions", "node_id", nodeID, "error", err)
		return 0, fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var subName string
		if err := rows.Scan(&subName); err != nil {
			logger.Error("fullReinit: failed to scan subscription name", "node_id", nodeID, "error", err)
			continue
		}
		// Drop the subscription
		dropQuery := fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", subName)
		if _, err := r.pool.Exec(ctx, dropQuery); err != nil {
			logger.Error("fullReinit: failed to drop subscription", "node_id", nodeID, "subscription", subName, "error", err)
			return 0, fmt.Errorf("failed to drop subscription %s: %w", subName, err)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("fullReinit: error iterating subscriptions", "node_id", nodeID, "error", err)
		return 0, fmt.Errorf("error iterating subscriptions: %w", err)
	}

	// Truncate all user tables so they can be re-copied by the next init
	// This is necessary because CREATE SUBSCRIPTION with copy_data=true will
	// try to COPY all data, which fails if the target table already has data
	tablesCount, err := r.truncateUserTables(ctx)
	if err != nil {
		logger.Error("fullReinit: failed to truncate user tables", "node_id", nodeID, "error", err)
		return 0, fmt.Errorf("failed to truncate user tables: %w", err)
	}

	// Clear progress record
	if _, err := r.pool.Exec(ctx, "DELETE FROM steep_repl.init_progress WHERE node_id = $1", nodeID); err != nil {
		logger.Error("fullReinit: failed to clear progress record", "node_id", nodeID, "error", err)
	}

	// Reset node state to UNINITIALIZED
	if err := r.updateState(ctx, nodeID, models.InitStateUninitialized); err != nil {
		logger.Error("fullReinit: failed to reset state", "node_id", nodeID, "error", err)
		return 0, fmt.Errorf("failed to reset state: %w", err)
	}

	return tablesCount, nil
}

// truncateUserTables truncates all user tables (excluding system schemas).
// Returns the number of tables truncated.
func (r *Reinitializer) truncateUserTables(ctx context.Context) (int, error) {
	// Get list of user tables
	rows, err := r.pool.Query(ctx, `
		SELECT schemaname || '.' || tablename as full_name
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		logger.Error("truncateUserTables: failed to query tables", "error", err)
		return 0, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			logger.Error("truncateUserTables: failed to scan table name", "error", err)
			return 0, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		logger.Error("truncateUserTables: error iterating tables", "error", err)
		return 0, fmt.Errorf("error iterating tables: %w", err)
	}

	// Acquire a dedicated connection for the truncate operations
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		logger.Error("truncateUserTables: failed to acquire connection", "error", err)
		return 0, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Create a replication origin if it doesn't exist, then set it for this session.
	// This marks the TRUNCATEs with an origin, causing subscriptions with
	// 'origin = none' to filter them out (preventing replication to the source).
	const originName = "steep_reinit"
	conn.Exec(ctx, "SELECT pg_replication_origin_create($1)", originName)

	if _, err := conn.Exec(ctx, "SELECT pg_replication_origin_session_setup($1)", originName); err != nil {
		logger.Error("truncateUserTables: failed to setup replication origin", "error", err)
		return 0, fmt.Errorf("failed to setup replication origin: %w", err)
	}

	// Truncate each table with CASCADE to handle foreign keys
	for _, table := range tables {
		truncateQuery := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		if _, err := conn.Exec(ctx, truncateQuery); err != nil {
			logger.Error("truncateUserTables: failed to truncate table", "table", table, "error", err)
			conn.Exec(ctx, "SELECT pg_replication_origin_session_reset()")
			return 0, fmt.Errorf("failed to truncate %s: %w", table, err)
		}
	}

	// Reset session origin
	conn.Exec(ctx, "SELECT pg_replication_origin_session_reset()")

	return len(tables), nil
}

// persistThroughput saves the final EWMA throughput value to the nodes table.
// This value is used as a baseline for future ETA estimates.
func (r *Reinitializer) persistThroughput(ctx context.Context, nodeID string, throughputBytesPerSec float64) error {
	logger.Debug("Reinitializer.persistThroughput: saving throughput",
		"node_id", nodeID,
		"throughput_bytes_sec", throughputBytesPerSec,
	)
	query := `
		UPDATE steep_repl.nodes
		SET last_sync_throughput_bytes_sec = $1,
			last_sync_at = now()
		WHERE node_id = $2
	`
	result, err := r.pool.Exec(ctx, query, throughputBytesPerSec, nodeID)
	if err != nil {
		logger.Error("Reinitializer.persistThroughput: failed to persist",
			"node_id", nodeID,
			"throughput_bytes_sec", throughputBytesPerSec,
			"error", err,
		)
		return err
	}
	logger.Debug("Reinitializer.persistThroughput: saved successfully",
		"node_id", nodeID,
		"rows_affected", result.RowsAffected(),
	)
	return nil
}

// getPersistedThroughput retrieves the last sync throughput for a node.
// Returns 0 if no throughput has been persisted.
func (r *Reinitializer) getPersistedThroughput(ctx context.Context, nodeID string) (float64, error) {
	var throughput *float64
	err := r.pool.QueryRow(ctx,
		"SELECT last_sync_throughput_bytes_sec FROM steep_repl.nodes WHERE node_id = $1",
		nodeID,
	).Scan(&throughput)
	if err != nil {
		logger.Debug("Reinitializer.getPersistedThroughput: query failed (column may not exist yet)",
			"node_id", nodeID,
			"error", err,
		)
		return 0, err
	}
	if throughput == nil {
		logger.Debug("Reinitializer.getPersistedThroughput: no throughput persisted yet",
			"node_id", nodeID,
		)
		return 0, nil
	}
	logger.Debug("Reinitializer.getPersistedThroughput: using persisted throughput",
		"node_id", nodeID,
		"throughput_bytes_sec", *throughput,
		"throughput_mb_sec", *throughput/(1024*1024),
	)
	return *throughput, nil
}

func (r *Reinitializer) updateState(ctx context.Context, nodeID string, state models.InitState) error {
	logger.Debug("Reinitializer.updateState: updating node state",
		"node_id", nodeID,
		"new_state", string(state),
	)
	query := `UPDATE steep_repl.nodes SET init_state = $1 WHERE node_id = $2`
	result, err := r.pool.Exec(ctx, query, string(state), nodeID)
	if err != nil {
		logger.Error("Reinitializer.updateState: failed to update",
			"node_id", nodeID,
			"state", string(state),
			"error", err,
		)
		return err
	}
	logger.Debug("Reinitializer.updateState: update complete",
		"node_id", nodeID,
		"rows_affected", result.RowsAffected(),
	)
	return nil
}

// updateProgressWithETA updates progress including ETA calculation.
func (r *Reinitializer) updateProgressWithETA(ctx context.Context, nodeID, phase string, percent float64, tablesTotal, tablesCompleted int, currentTable string, etaSeconds int) {
	logger.Debug("updateProgressWithETA: updating progress",
		"node_id", nodeID,
		"phase", phase,
		"percent", percent,
		"tables_total", tablesTotal,
		"tables_completed", tablesCompleted,
		"eta_seconds", etaSeconds,
	)
	query := `
		INSERT INTO steep_repl.init_progress (
			node_id, phase, overall_percent, tables_total, tables_completed,
			current_table, current_table_percent, eta_seconds, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 0, $7, now(), now())
		ON CONFLICT (node_id) DO UPDATE SET
			phase = EXCLUDED.phase,
			overall_percent = EXCLUDED.overall_percent,
			tables_total = EXCLUDED.tables_total,
			tables_completed = EXCLUDED.tables_completed,
			current_table = EXCLUDED.current_table,
			current_table_percent = EXCLUDED.current_table_percent,
			eta_seconds = EXCLUDED.eta_seconds,
			started_at = CASE WHEN steep_repl.init_progress.phase = 'complete' THEN now() ELSE steep_repl.init_progress.started_at END,
			updated_at = now()
	`
	_, err := r.pool.Exec(ctx, query, nodeID, phase, percent, tablesTotal, tablesCompleted, currentTable, etaSeconds)
	if err != nil {
		logger.Error("updateProgressWithETA: failed to update progress",
			"node_id", nodeID,
			"phase", phase,
			"error", err,
		)
	}
}

// updateProgressFull updates progress with all fields including bytes and throughput.
func (r *Reinitializer) updateProgressFull(ctx context.Context, nodeID, phase string, percent float64,
	tablesTotal, tablesCompleted int, currentTable string, bytesCopied int64, throughputBytesPerSec float64, etaSeconds int) {
	logger.Debug("updateProgressFull: updating progress",
		"node_id", nodeID,
		"phase", phase,
		"percent", percent,
		"bytes_copied", bytesCopied,
		"throughput_bytes_sec", throughputBytesPerSec,
		"eta_seconds", etaSeconds,
	)
	query := `
		INSERT INTO steep_repl.init_progress (
			node_id, phase, overall_percent, tables_total, tables_completed,
			current_table, current_table_percent, bytes_copied, throughput_rows_sec,
			eta_seconds, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8, $9, now(), now())
		ON CONFLICT (node_id) DO UPDATE SET
			phase = EXCLUDED.phase,
			overall_percent = EXCLUDED.overall_percent,
			tables_total = EXCLUDED.tables_total,
			tables_completed = EXCLUDED.tables_completed,
			current_table = EXCLUDED.current_table,
			current_table_percent = EXCLUDED.current_table_percent,
			bytes_copied = EXCLUDED.bytes_copied,
			throughput_rows_sec = EXCLUDED.throughput_rows_sec,
			eta_seconds = EXCLUDED.eta_seconds,
			started_at = CASE WHEN steep_repl.init_progress.phase = 'complete' THEN now() ELSE steep_repl.init_progress.started_at END,
			updated_at = now()
	`
	_, err := r.pool.Exec(ctx, query, nodeID, phase, percent, tablesTotal, tablesCompleted,
		currentTable, bytesCopied, throughputBytesPerSec, etaSeconds)
	if err != nil {
		logger.Error("updateProgressFull: failed to update progress",
			"node_id", nodeID,
			"phase", phase,
			"error", err,
		)
	}
}

// getTableSizes returns the total heap size in bytes for the given tables.
// Uses pg_relation_size which returns just the main heap data (what COPY transfers),
// not pg_total_relation_size which includes indexes and TOAST.
func (r *Reinitializer) getTableSizes(ctx context.Context, tables []string) (int64, error) {
	var totalBytes int64
	for _, table := range tables {
		var size int64
		err := r.pool.QueryRow(ctx,
			"SELECT pg_relation_size($1::regclass, 'main')",
			table,
		).Scan(&size)
		if err != nil {
			logger.Error("getTableSizes: failed to get table size", "table", table, "error", err)
			// Continue with other tables, use 0 for this one
			continue
		}
		totalBytes += size
	}
	return totalBytes, nil
}

// getCopyProgress queries pg_stat_progress_copy for active COPY operations.
// Returns nil if no COPY is in progress.
func (r *Reinitializer) getCopyProgress(ctx context.Context) (*copyProgress, error) {
	var progress copyProgress
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(bytes_processed, 0),
			COALESCE(bytes_total, 0),
			COALESCE(tuples_processed, 0)
		FROM pg_stat_progress_copy
		WHERE command = 'COPY FROM'
		LIMIT 1
	`).Scan(&progress.BytesProcessed, &progress.BytesTotal, &progress.TuplesProcessed)
	if err != nil {
		// No COPY in progress is not an error
		return nil, nil
	}
	return &progress, nil
}

func (r *Reinitializer) resolveTables(ctx context.Context, opts ReinitOptions) ([]string, error) {
	if opts.Scope.Full {
		// Get all replicated tables
		return r.getAllTables(ctx, opts.NodeID)
	}
	if opts.Scope.Schema != "" {
		// Get all tables in schema
		return r.getTablesInSchema(ctx, opts.Scope.Schema)
	}
	return opts.Scope.Tables, nil
}

// getAllTables returns all user tables that are part of a publication.
// T047: Implementation for Phase 6: User Story 4.
func (r *Reinitializer) getAllTables(ctx context.Context, nodeID string) ([]string, error) {
	// Query all user tables (excluding system schemas)
	rows, err := r.pool.Query(ctx, `
		SELECT schemaname || '.' || tablename as full_name
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		logger.Error("getAllTables: failed to query tables", "node_id", nodeID, "error", err)
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			logger.Error("getAllTables: failed to scan table name", "node_id", nodeID, "error", err)
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		logger.Error("getAllTables: error iterating tables", "node_id", nodeID, "error", err)
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return tables, nil
}

// getTablesInSchema returns all tables in the specified schema.
// T049: Implementation for Phase 6: User Story 4.
func (r *Reinitializer) getTablesInSchema(ctx context.Context, schema string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT schemaname || '.' || tablename as full_name
		FROM pg_tables
		WHERE schemaname = $1
		ORDER BY tablename
	`, schema)
	if err != nil {
		logger.Error("getTablesInSchema: failed to query tables", "schema", schema, "error", err)
		return nil, fmt.Errorf("failed to query tables in schema %s: %w", schema, err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			logger.Error("getTablesInSchema: failed to scan table name", "schema", schema, "error", err)
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		logger.Error("getTablesInSchema: error iterating tables", "schema", schema, "error", err)
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	if len(tables) == 0 {
		logger.Error("getTablesInSchema: no tables found", "schema", schema)
		return nil, fmt.Errorf("no tables found in schema %s", schema)
	}

	return tables, nil
}

// pauseReplication disables the subscription to pause replication.
// Note: PostgreSQL subscriptions are all-or-nothing - we must disable the entire
// subscription even for partial table reinit. The tables parameter is for logging
// and API symmetry with resumeReplication.
// T047: Implementation for Phase 6: User Story 4.
func (r *Reinitializer) pauseReplication(ctx context.Context, nodeID string, tables []string) error {
	logger.Debug("pauseReplication: disabling subscription",
		"node_id", nodeID,
		"tables_affected", len(tables),
	)
	// Find subscriptions for this node and disable them
	sanitizedID := sanitizeIdentifier(nodeID)
	query := `SELECT subname FROM pg_subscription WHERE subname LIKE $1`
	pattern := "steep_sub_" + sanitizedID + "_%"

	rows, err := r.pool.Query(ctx, query, pattern)
	if err != nil {
		logger.Error("pauseReplication: failed to query subscriptions", "node_id", nodeID, "error", err)
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var subName string
		if err := rows.Scan(&subName); err != nil {
			logger.Error("pauseReplication: failed to scan subscription name", "node_id", nodeID, "error", err)
			continue
		}
		// Disable the subscription
		disableQuery := fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", subName)
		if _, err := r.pool.Exec(ctx, disableQuery); err != nil {
			logger.Error("pauseReplication: failed to disable subscription", "node_id", nodeID, "subscription", subName, "error", err)
			return fmt.Errorf("failed to disable subscription %s: %w", subName, err)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("pauseReplication: error iterating subscriptions", "node_id", nodeID, "error", err)
		return err
	}
	return nil
}

// reinitTable truncates and marks a table for recopy during next sync.
// T048: Implementation for Phase 6: User Story 4.
func (r *Reinitializer) reinitTable(ctx context.Context, opts ReinitOptions, table string) error {
	// Acquire a dedicated connection so session state persists across statements
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		logger.Error("reinitTable: failed to acquire connection", "node_id", opts.NodeID, "table", table, "error", err)
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Create a replication origin if it doesn't exist, then set it for this session.
	// This marks the TRUNCATE with an origin, causing subscriptions with
	// 'origin = none' to filter it out (preventing replication to the source).
	const originName = "steep_reinit"

	// Create origin if not exists (ignore error if already exists)
	conn.Exec(ctx, "SELECT pg_replication_origin_create($1)", originName)

	// Setup the origin for this session
	if _, err := conn.Exec(ctx, "SELECT pg_replication_origin_session_setup($1)", originName); err != nil {
		logger.Error("reinitTable: failed to setup replication origin", "node_id", opts.NodeID, "table", table, "error", err)
		return fmt.Errorf("failed to setup replication origin: %w", err)
	}

	// Truncate the table - this will be tagged with our origin
	truncateQuery := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
	if _, err := conn.Exec(ctx, truncateQuery); err != nil {
		logger.Error("reinitTable: failed to truncate table", "node_id", opts.NodeID, "table", table, "error", err)
		conn.Exec(ctx, "SELECT pg_replication_origin_session_reset()")
		return fmt.Errorf("failed to truncate %s: %w", table, err)
	}

	// Reset session origin
	if _, err := conn.Exec(ctx, "SELECT pg_replication_origin_session_reset()"); err != nil {
		logger.Error("reinitTable: failed to reset replication origin", "node_id", opts.NodeID, "table", table, "error", err)
		return fmt.Errorf("failed to reset replication origin: %w", err)
	}

	return nil
}

// resumeReplication re-enables the subscription to resume replication.
// It resets the sync state for truncated tables to trigger a re-copy.
// T047: Implementation for Phase 6: User Story 4.
func (r *Reinitializer) resumeReplication(ctx context.Context, nodeID string, tables []string, totalBytes int64) error {
	// Find subscriptions for this node
	sanitizedID := sanitizeIdentifier(nodeID)
	query := `SELECT oid, subname FROM pg_subscription WHERE subname LIKE $1`
	pattern := "steep_sub_" + sanitizedID + "_%"

	rows, err := r.pool.Query(ctx, query, pattern)
	if err != nil {
		logger.Error("resumeReplication: failed to query subscriptions", "node_id", nodeID, "error", err)
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	type subInfo struct {
		oid  uint32
		name string
	}
	var subs []subInfo
	for rows.Next() {
		var s subInfo
		if err := rows.Scan(&s.oid, &s.name); err != nil {
			logger.Error("resumeReplication: failed to scan subscription", "node_id", nodeID, "error", err)
			continue
		}
		subs = append(subs, s)
	}

	if err := rows.Err(); err != nil {
		logger.Error("resumeReplication: error iterating subscriptions", "node_id", nodeID, "error", err)
		return err
	}

	// Reset sync state for truncated tables to 'i' (initialize needed)
	// This tells PostgreSQL to re-copy the table data
	for _, sub := range subs {
		for _, table := range tables {
			// Reset the subscription relation state to 'i' to trigger re-copy
			// srsubstate: 'i' = initialize, 'd' = data copying, 's' = synced, 'r' = ready
			resetQuery := `
				UPDATE pg_subscription_rel
				SET srsubstate = 'i', srsublsn = NULL
				WHERE srsubid = $1
				AND srrelid = $2::regclass
			`
			if _, err := r.pool.Exec(ctx, resetQuery, sub.oid, table); err != nil {
				logger.Error("resumeReplication: failed to reset sync state", "node_id", nodeID, "table", table, "subscription", sub.name, "error", err)
				return fmt.Errorf("failed to reset sync state for %s: %w", table, err)
			}
		}

		// Enable the subscription
		enableQuery := fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", sub.name)
		if _, err := r.pool.Exec(ctx, enableQuery); err != nil {
			logger.Error("resumeReplication: failed to enable subscription", "node_id", nodeID, "subscription", sub.name, "error", err)
			return fmt.Errorf("failed to enable subscription %s: %w", sub.name, err)
		}
	}

	// State stays as REINITIALIZING during data re-sync
	// Start monitoring to transition to SYNCHRONIZED when re-copy completes
	// Get baseline throughput for fallback ETA calculation
	baselineThroughput, _ := r.getPersistedThroughput(ctx, nodeID)
	if baselineThroughput <= 0 {
		baselineThroughput = 50.0 * 1024 * 1024 // Default: 50 MB/s
	}
	go r.monitorReinitComplete(context.Background(), nodeID, tables, totalBytes, baselineThroughput)

	return nil
}

// monitorReinitComplete monitors the reinit progress and transitions to SYNCHRONIZED when complete.
// Uses EWMA (Exponentially Weighted Moving Average) for accurate ETA calculation based on actual throughput.
// baselineThroughput is used as fallback when no EWMA data is available yet.
func (r *Reinitializer) monitorReinitComplete(ctx context.Context, nodeID string, tables []string, totalBytes int64, baselineThroughput float64) {
	// Use 1-second ticker for more responsive ETA updates
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute)
	sanitizedID := sanitizeIdentifier(nodeID)
	startTime := time.Now()

	// EWMA for throughput smoothing - age of 5 gives ~33% weight to new samples
	// This makes the ETA responsive but stable
	throughputEWMA := ewma.NewMovingAverage(5)

	// Track previous bytes for throughput calculation
	var prevBytesCopied int64
	var prevTime time.Time = startTime

	logger.Debug("monitorReinitComplete: starting with EWMA",
		"node_id", nodeID,
		"tables", len(tables),
		"total_bytes", totalBytes,
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			logger.Error("monitorReinitComplete: reinit timed out", "node_id", nodeID, "timeout", "30m")
			r.manager.logger.Log(InitEvent{
				Level:  "warn",
				Event:  "reinit.timeout",
				NodeID: nodeID,
				Error:  "reinit timed out after 30 minutes",
			})
			r.updateState(ctx, nodeID, models.InitStateFailed)
			return
		case <-ticker.C:
			now := time.Now()
			pattern := "steep_sub_" + sanitizedID + "_from_%"

			// Get total and ready relation counts for progress tracking
			var totalRelations, readyRelations int
			if err := r.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM pg_subscription_rel sr
				JOIN pg_subscription s ON s.oid = sr.srsubid
				WHERE s.subname LIKE $1
			`, pattern).Scan(&totalRelations); err != nil {
				logger.Error("monitorReinitComplete: failed to count total relations", "node_id", nodeID, "error", err)
			}
			if err := r.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM pg_subscription_rel sr
				JOIN pg_subscription s ON s.oid = sr.srsubid
				WHERE s.subname LIKE $1 AND sr.srsubstate = 'r'
			`, pattern).Scan(&readyRelations); err != nil {
				logger.Error("monitorReinitComplete: failed to count ready relations", "node_id", nodeID, "error", err)
			}

			// Try to get actual COPY progress from pg_stat_progress_copy
			copyProg, _ := r.getCopyProgress(ctx)

			var currentBytesCopied int64
			var currentThroughput float64
			var etaSeconds int

			if copyProg != nil && copyProg.BytesProcessed > 0 {
				// We have real COPY progress data!
				currentBytesCopied = copyProg.BytesProcessed

				// Calculate instantaneous throughput
				elapsed := now.Sub(prevTime).Seconds()
				if elapsed > 0 && currentBytesCopied > prevBytesCopied {
					bytesThisPeriod := currentBytesCopied - prevBytesCopied
					instantThroughput := float64(bytesThisPeriod) / elapsed
					throughputEWMA.Add(instantThroughput)
					currentThroughput = throughputEWMA.Value()
				}

				// Calculate ETA based on throughput
				// Use EWMA if warmed up, otherwise use baseline throughput
				throughputForETA := currentThroughput
				if throughputForETA <= 0 {
					// EWMA hasn't warmed up yet (needs 10 samples), use baseline
					throughputForETA = baselineThroughput
				}

				var remainingBytes int64
				if copyProg.BytesTotal > 0 {
					// Use the COPY's own total if available
					remainingBytes = copyProg.BytesTotal - copyProg.BytesProcessed
				} else if totalBytes > 0 {
					// Fall back to our pre-calculated total
					remainingBytes = totalBytes - currentBytesCopied
				}
				if remainingBytes > 0 && throughputForETA > 0 {
					etaSeconds = int(float64(remainingBytes) / throughputForETA)
				}

				logger.Debug("monitorReinitComplete: COPY progress",
					"node_id", nodeID,
					"bytes_processed", copyProg.BytesProcessed,
					"bytes_total", copyProg.BytesTotal,
					"tuples_processed", copyProg.TuplesProcessed,
					"throughput_ewma", currentThroughput,
					"eta_seconds", etaSeconds,
				)
			} else {
				// No COPY in progress - use time-based countdown as fallback
				elapsed := time.Since(startTime).Seconds()

				// Progress based on relations: syncProgress goes from 0 to 1
				syncProgress := float64(readyRelations) / float64(max(totalRelations, 1))

				if syncProgress > 0 && syncProgress < 1 {
					// Some relations done - estimate based on rate
					remainingProgress := 1 - syncProgress
					etaSeconds = int(elapsed / syncProgress * remainingProgress)
				} else if syncProgress == 0 {
					// Nothing ready yet - use size-based estimate or fallback
					if totalBytes > 0 {
						// Use persisted/default baseline throughput if no EWMA data yet
						estimatedThroughput := baselineThroughput
						if throughputEWMA.Value() > 0 {
							estimatedThroughput = throughputEWMA.Value()
						}
						etaSeconds = int(float64(totalBytes) / estimatedThroughput)
						remaining := float64(etaSeconds) - elapsed
						if remaining < 0 {
							remaining = 0
						}
						etaSeconds = int(remaining)
					} else {
						// Pure time-based fallback
						estimatedTotal := float64(totalRelations) * 10.0
						remaining := estimatedTotal - elapsed
						if remaining < 0 {
							remaining = 0
						}
						etaSeconds = int(remaining)
					}
				}
			}

			// Update tracking for next iteration
			prevBytesCopied = currentBytesCopied
			prevTime = now

			// Calculate overall progress: 50% (truncate done) + up to 50% (sync progress)
			var overallPercent float64 = 50
			if totalRelations > 0 {
				overallPercent = 50 + (float64(readyRelations)/float64(totalRelations))*50
			}

			// Clamp negative ETA to 0 while still reinitializing.
			// This prevents "-" display just before completion.
			if etaSeconds < 0 {
				etaSeconds = 0
			}

			// Update progress with full data
			r.updateProgressFull(ctx, nodeID, "catching_up", overallPercent,
				totalRelations, readyRelations, "", currentBytesCopied, currentThroughput, etaSeconds)

			logger.Debug("monitorReinitComplete: status",
				"node_id", nodeID,
				"total_relations", totalRelations,
				"ready_relations", readyRelations,
				"overall_percent", overallPercent,
				"bytes_copied", currentBytesCopied,
				"throughput_bytes_sec", currentThroughput,
				"eta_seconds", etaSeconds,
			)

			allReady := (totalRelations-readyRelations) == 0 && totalRelations > 0

			if allReady {
				// All tables are synced, update progress to 100% and transition to SYNCHRONIZED
				r.updateProgressFull(ctx, nodeID, "complete", 100,
					totalRelations, totalRelations, "", currentBytesCopied, currentThroughput, 0)
				logger.Debug("monitorReinitComplete: transitioning to synchronized", "node_id", nodeID)

				// Calculate and persist actual throughput for future ETA estimates
				// Use EWMA if available, otherwise calculate from total time
				actualThroughput := currentThroughput
				if actualThroughput <= 0 && totalBytes > 0 {
					// EWMA not available (pg_stat_progress_copy doesn't show logical replication)
					// Calculate actual throughput from total bytes and elapsed time
					totalDuration := time.Since(startTime).Seconds()
					if totalDuration > 0 {
						// Adjust for detection latency: the 1-second ticker means we detect
						// completion up to 1s after it actually happens. Subtract half the
						// tick interval to get a more accurate throughput estimate.
						adjustedDuration := totalDuration - 0.5
						if adjustedDuration < 1.0 {
							adjustedDuration = totalDuration // Don't over-correct for very short syncs
						}
						actualThroughput = float64(totalBytes) / adjustedDuration
						logger.Debug("monitorReinitComplete: calculated throughput from duration",
							"node_id", nodeID,
							"total_bytes", totalBytes,
							"raw_duration_sec", totalDuration,
							"adjusted_duration_sec", adjustedDuration,
							"throughput_bytes_sec", actualThroughput,
							"throughput_mb_sec", actualThroughput/(1024*1024),
						)
					}
				}

				if actualThroughput > 0 {
					if err := r.persistThroughput(ctx, nodeID, actualThroughput); err != nil {
						logger.Error("monitorReinitComplete: failed to persist throughput", "node_id", nodeID, "error", err)
					} else {
						logger.Debug("monitorReinitComplete: throughput persisted",
							"node_id", nodeID,
							"throughput_bytes_sec", actualThroughput,
						)
					}
				}

				if err := r.manager.UpdateState(ctx, nodeID, models.InitStateSynchronized); err != nil {
					logger.Error("monitorReinitComplete: failed to update state", "node_id", nodeID, "error", err)
					r.manager.logger.Log(InitEvent{
						Level:  "error",
						Event:  "reinit.state_update_failed",
						NodeID: nodeID,
						Error:  err.Error(),
					})
				} else {
					logger.Debug("monitorReinitComplete: state updated to synchronized", "node_id", nodeID)
				}
				return
			}
		}
	}
}
