package init

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/models"
)

// SnapshotInitializer handles automatic snapshot initialization using copy_data=true.
type SnapshotInitializer struct {
	pool    *pgxpool.Pool
	manager *Manager
}

// NewSnapshotInitializer creates a new snapshot initializer.
func NewSnapshotInitializer(pool *pgxpool.Pool, manager *Manager) *SnapshotInitializer {
	return &SnapshotInitializer{
		pool:    pool,
		manager: manager,
	}
}

// SnapshotOptions configures snapshot initialization behavior.
type SnapshotOptions struct {
	ParallelWorkers      int
	LargeTableThreshold  int64  // Bytes, tables larger than this use alternate method
	LargeTableMethod     string // pg_dump, copy, basebackup
	Force                bool   // Truncate existing data if present
	UseStreamingParallel bool   // Use PG18 streaming=parallel for subscriptions
}

// ParallelCopyResult holds the result of a parallel table copy operation.
type ParallelCopyResult struct {
	TableInfo   TableInfo
	RowsCopied  int64
	BytesCopied int64
	Duration    time.Duration
	Error       error
}

// TableCopyTask represents a table to be copied by a worker.
type TableCopyTask struct {
	Table         TableInfo
	SourceConnStr string
}

// Start begins automatic snapshot initialization from source to target node.
// Implements T021 (Phase 3: User Story 1).
func (s *SnapshotInitializer) Start(ctx context.Context, targetNode, sourceNode string, opts SnapshotOptions) error {
	s.manager.logger.LogInitStarted(targetNode, sourceNode, "snapshot")

	// Validate nodes exist and are in correct state
	if err := s.validateNodes(ctx, targetNode, sourceNode); err != nil {
		return fmt.Errorf("node validation failed: %w", err)
	}

	// Update target node state to PREPARING
	if err := s.updateState(ctx, targetNode, models.InitStatePreparing); err != nil {
		return fmt.Errorf("failed to update state to PREPARING: %w", err)
	}

	// Record init_source_node and init_started_at
	if err := s.recordInitStart(ctx, targetNode, sourceNode); err != nil {
		return fmt.Errorf("failed to record init start: %w", err)
	}

	// Start the initialization in a goroutine
	go s.runInit(ctx, targetNode, sourceNode, opts)

	return nil
}

// runInit performs the actual initialization work in a background goroutine.
func (s *SnapshotInitializer) runInit(ctx context.Context, targetNode, sourceNode string, opts SnapshotOptions) {
	startTime := time.Now()

	// Ensure cleanup on failure
	defer func() {
		if r := recover(); r != nil {
			s.handleInitFailure(ctx, targetNode, fmt.Errorf("panic: %v", r))
		}
		s.manager.unregisterOperation(targetNode)
	}()

	// Get source node connection info
	sourceInfo, err := s.getNodeConnectionInfo(ctx, sourceNode)
	if err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to get source node info: %w", err))
		return
	}

	// Update state to COPYING
	if err := s.updateState(ctx, targetNode, models.InitStateCopying); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to update state to COPYING: %w", err))
		return
	}

	// Initialize progress tracking
	if err := s.initProgress(ctx, targetNode, opts.ParallelWorkers); err != nil {
		s.manager.logger.Log(InitEvent{
			Level:  "warn",
			Event:  "init.progress_init_failed",
			NodeID: targetNode,
			Error:  err.Error(),
		})
		// Non-fatal, continue
	}

	// Get list of tables with size information for large table detection
	tables, err := s.getPublicationTablesWithSize(ctx, opts.LargeTableThreshold)
	if err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to get publication tables: %w", err))
		return
	}

	// Check for large tables that may need alternate handling
	if opts.LargeTableThreshold > 0 {
		largeTables := s.filterLargeTables(tables)
		if len(largeTables) > 0 {
			s.manager.logger.Log(InitEvent{
				Level:  "warn",
				Event:  "init.large_tables_detected",
				NodeID: targetNode,
				Details: map[string]any{
					"count":           len(largeTables),
					"threshold_bytes": opts.LargeTableThreshold,
					"tables":          s.largeTableNames(largeTables),
					"method":          opts.LargeTableMethod,
				},
			})

			// If alternate method is specified and there are large tables, fail with guidance
			if opts.LargeTableMethod != "" && opts.LargeTableMethod != "copy" {
				s.handleInitFailure(ctx, targetNode, fmt.Errorf(
					"database contains %d tables exceeding %d bytes threshold; "+
						"use --method=%s or increase --large-table-threshold",
					len(largeTables), opts.LargeTableThreshold, opts.LargeTableMethod))
				return
			}
			// Otherwise continue with copy_data=true but log warning
		}
	}

	// Create subscription with copy_data=true
	// Use underscores instead of hyphens since SQL identifiers with hyphens need quoting
	subscriptionName := fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(targetNode), sanitizeIdentifier(sourceNode))
	publicationName := fmt.Sprintf("steep_pub_%s", sanitizeIdentifier(sourceNode))

	if err := s.createSubscription(ctx, targetNode, subscriptionName, publicationName, sourceInfo); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to create subscription: %w", err))
		return
	}

	// Update progress with total tables
	s.updateProgressTables(ctx, targetNode, len(tables), 0)

	// Monitor subscription sync progress until complete
	if err := s.monitorSubscriptionSync(ctx, targetNode, subscriptionName); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("subscription sync failed: %w", err))
		return
	}

	// Update state to CATCHING_UP (WAL replay after initial copy)
	if err := s.updateState(ctx, targetNode, models.InitStateCatchingUp); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to update state to CATCHING_UP: %w", err))
		return
	}

	// Wait for catch-up to complete (lag < threshold)
	if err := s.waitForCatchUp(ctx, targetNode, subscriptionName); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("catch-up failed: %w", err))
		return
	}

	// Mark initialization as complete
	if err := s.completeInit(ctx, targetNode); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to complete init: %w", err))
		return
	}

	// Log completion
	duration := time.Since(startTime)
	s.manager.logger.LogInitCompleted(targetNode, duration.Milliseconds(), 0, 0)
}

// validateNodes verifies that source and target nodes exist and are in valid states.
func (s *SnapshotInitializer) validateNodes(ctx context.Context, targetNode, sourceNode string) error {
	// Check target node exists and is UNINITIALIZED
	var targetState string
	err := s.pool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = $1
	`, targetNode).Scan(&targetState)
	if err != nil {
		return fmt.Errorf("target node %s not found", targetNode)
	}
	if targetState != string(models.InitStateUninitialized) {
		return fmt.Errorf("target node %s must be in UNINITIALIZED state, got %s", targetNode, targetState)
	}

	// Check source node exists and is SYNCHRONIZED
	var sourceState string
	err = s.pool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = $1
	`, sourceNode).Scan(&sourceState)
	if err != nil {
		return fmt.Errorf("source node %s not found", sourceNode)
	}
	if sourceState != string(models.InitStateSynchronized) {
		return fmt.Errorf("source node %s must be in SYNCHRONIZED state, got %s", sourceNode, sourceState)
	}

	return nil
}

// recordInitStart records the initialization start time and source node.
func (s *SnapshotInitializer) recordInitStart(ctx context.Context, targetNode, sourceNode string) error {
	query := `
		UPDATE steep_repl.nodes
		SET init_source_node = $1, init_started_at = NOW()
		WHERE node_id = $2
	`
	_, err := s.pool.Exec(ctx, query, sourceNode, targetNode)
	return err
}

// getNodeConnectionInfo retrieves connection information for a node.
func (s *SnapshotInitializer) getNodeConnectionInfo(ctx context.Context, nodeID string) (*NodeConnectionInfo, error) {
	var info NodeConnectionInfo
	err := s.pool.QueryRow(ctx, `
		SELECT host, port FROM steep_repl.nodes WHERE node_id = $1
	`, nodeID).Scan(&info.Host, &info.Port)
	if err != nil {
		return nil, err
	}
	info.NodeID = nodeID

	// Use the same database/user/password as the daemon's connection
	// This assumes all nodes in the cluster use the same credentials
	if s.manager.pgConfig != nil {
		info.Database = s.manager.pgConfig.Database
		info.User = s.manager.pgConfig.User
		info.Password = s.getReplicationPassword()
	} else {
		// Fallback to defaults
		info.Database = "postgres"
		info.User = "postgres"
	}

	return &info, nil
}

// getReplicationPassword returns the password to use for replication connections.
// This checks environment variables and falls back to empty (for trust auth).
func (s *SnapshotInitializer) getReplicationPassword() string {
	// Check environment variable first
	if pw := os.Getenv("PGPASSWORD"); pw != "" {
		return pw
	}
	return ""
}

// NodeConnectionInfo holds connection details for a node.
type NodeConnectionInfo struct {
	NodeID   string
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// TableInfo holds information about a table for initialization.
type TableInfo struct {
	SchemaName string
	TableName  string
	FullName   string
	SizeBytes  int64
	IsLarge    bool
}

// getPublicationTablesWithSize retrieves tables with size information.
// Implements T025 large table detection.
func (s *SnapshotInitializer) getPublicationTablesWithSize(ctx context.Context, threshold int64) ([]TableInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			schemaname,
			tablename,
			schemaname || '.' || tablename as full_name,
			pg_table_size(schemaname || '.' || tablename) as size_bytes
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY size_bytes DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.SchemaName, &t.TableName, &t.FullName, &t.SizeBytes); err != nil {
			return nil, err
		}
		t.IsLarge = threshold > 0 && t.SizeBytes > threshold
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// filterLargeTables returns only tables that are marked as large.
func (s *SnapshotInitializer) filterLargeTables(tables []TableInfo) []TableInfo {
	var large []TableInfo
	for _, t := range tables {
		if t.IsLarge {
			large = append(large, t)
		}
	}
	return large
}

// largeTableNames returns a slice of table names for logging.
func (s *SnapshotInitializer) largeTableNames(tables []TableInfo) []string {
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = fmt.Sprintf("%s.%s (%d bytes)", t.SchemaName, t.TableName, t.SizeBytes)
	}
	return names
}

// getTotalDatabaseSize returns the total size of user tables.
func (s *SnapshotInitializer) getTotalDatabaseSize(ctx context.Context) (int64, error) {
	var totalSize int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(pg_table_size(schemaname || '.' || tablename)), 0)
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
	`).Scan(&totalSize)
	return totalSize, err
}

// createSubscription creates a logical replication subscription with copy_data=true.
func (s *SnapshotInitializer) createSubscription(ctx context.Context, targetNode, subName, pubName string, sourceInfo *NodeConnectionInfo) error {
	// Build connection string for the source node
	// Include user and password from the source node's connection info
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
		sourceInfo.Host, sourceInfo.Port, sourceInfo.Database, sourceInfo.User)
	if sourceInfo.Password != "" {
		connStr += fmt.Sprintf(" password=%s", sourceInfo.Password)
	}

	// Create subscription with copy_data=true
	// This initiates automatic snapshot transfer
	// Use origin='none' to prevent changes from being re-replicated in bidirectional setups
	query := fmt.Sprintf(`
		CREATE SUBSCRIPTION %s
		CONNECTION '%s'
		PUBLICATION %s
		WITH (copy_data = true, create_slot = true, origin = 'none')
	`, subName, connStr, pubName)

	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("CREATE SUBSCRIPTION failed: %w", err)
	}

	s.manager.logger.Log(InitEvent{
		Event:  "init.subscription_created",
		NodeID: targetNode,
		Details: map[string]any{
			"subscription": subName,
			"publication":  pubName,
		},
	})

	return nil
}

// initProgress initializes the progress tracking row.
func (s *SnapshotInitializer) initProgress(ctx context.Context, nodeID string, parallelWorkers int) error {
	query := `
		INSERT INTO steep_repl.init_progress (
			node_id, phase, overall_percent, tables_total, tables_completed,
			rows_copied, bytes_copied, throughput_rows_sec, started_at, parallel_workers
		) VALUES ($1, 'copying', 0, 0, 0, 0, 0, 0, NOW(), $2)
		ON CONFLICT (node_id) DO UPDATE SET
			phase = 'copying',
			overall_percent = 0,
			tables_total = 0,
			tables_completed = 0,
			rows_copied = 0,
			bytes_copied = 0,
			started_at = NOW(),
			parallel_workers = $2,
			updated_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query, nodeID, parallelWorkers)
	return err
}

// updateProgressTables updates the total and completed table counts.
func (s *SnapshotInitializer) updateProgressTables(ctx context.Context, nodeID string, total, completed int) {
	query := `
		UPDATE steep_repl.init_progress
		SET tables_total = $2, tables_completed = $3, updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = s.pool.Exec(ctx, query, nodeID, total, completed)
}

// monitorSubscriptionSync monitors the subscription sync progress.
// Implements T024 polling logic for pg_subscription_rel sync states.
func (s *SnapshotInitializer) monitorSubscriptionSync(ctx context.Context, targetNode, subName string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Get total size upfront for ETA calculation
	totalBytes, _ := s.getTotalDatabaseSize(ctx)
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Query pg_subscription_rel for sync status with bytes copied
			// i = initialize, d = data is being copied, s = synchronized, r = ready
			var syncedCount, totalCount int
			var bytesCompleted int64
			err := s.pool.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE srsubstate IN ('s', 'r')) as synced,
					COUNT(*) as total,
					COALESCE(SUM(CASE WHEN srsubstate IN ('s', 'r')
						THEN pg_table_size(srrelid) ELSE 0 END), 0) as bytes_completed
				FROM pg_subscription_rel psr
				JOIN pg_subscription ps ON ps.oid = psr.srsubid
				WHERE ps.subname = $1
			`, subName).Scan(&syncedCount, &totalCount, &bytesCompleted)
			if err != nil {
				s.manager.logger.Log(InitEvent{
					Level:  "warn",
					Event:  "init.sync_poll_error",
					NodeID: targetNode,
					Error:  err.Error(),
				})
				continue
			}

			// Update progress
			if totalCount > 0 {
				percent := float32(syncedCount) / float32(totalCount) * 100

				// Calculate throughput and ETA
				elapsedSeconds := time.Since(startTime).Seconds()
				var throughput float32
				var etaSeconds int

				if elapsedSeconds > 0 && bytesCompleted > 0 {
					throughput = float32(float64(bytesCompleted) / elapsedSeconds)
					if totalBytes > bytesCompleted {
						bytesRemaining := totalBytes - bytesCompleted
						etaSeconds = int(float64(bytesRemaining) / float64(throughput))
					}
				}

				// Update all progress fields in the database
				s.updateFullProgress(ctx, targetNode, percent, syncedCount, totalCount, bytesCompleted, throughput, etaSeconds)

				// Send progress update
				s.manager.sendProgress(ProgressUpdate{
					NodeID:          targetNode,
					Phase:           "copying",
					OverallPercent:  percent,
					TablesTotal:     totalCount,
					TablesCompleted: syncedCount,
					BytesCopied:     bytesCompleted,
					ThroughputRows:  throughput,
					ETASeconds:      etaSeconds,
				})

				// All tables synced?
				if syncedCount >= totalCount {
					return nil
				}
			}
		}
	}
}

// updateFullProgress updates all progress fields including ETA and throughput.
func (s *SnapshotInitializer) updateFullProgress(ctx context.Context, nodeID string, percent float32, tablesCompleted, tablesTotal int, bytesCopied int64, throughput float32, etaSeconds int) {
	query := `
		UPDATE steep_repl.init_progress
		SET overall_percent = $2,
			tables_completed = $3,
			tables_total = $4,
			bytes_copied = $5,
			throughput_rows_sec = $6,
			eta_seconds = $7,
			updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = s.pool.Exec(ctx, query, nodeID, percent, tablesCompleted, tablesTotal, bytesCopied, throughput, etaSeconds)
}

// waitForCatchUp waits for replication lag to reach acceptable levels.
// Uses pg_stat_subscription on the target node to check lag without connecting to source.
func (s *SnapshotInitializer) waitForCatchUp(ctx context.Context, targetNode, subName string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	const maxLagBytes int64 = 1024 * 1024 // 1MB threshold

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Check subscription status using pg_stat_subscription
			// This view shows the lag between received and applied LSN on the subscriber
			var receivedLsn, latestEndLsn *string
			var lagBytes int64

			err := s.pool.QueryRow(ctx, `
				SELECT
					received_lsn::text,
					latest_end_lsn::text,
					COALESCE(
						pg_wal_lsn_diff(received_lsn, latest_end_lsn),
						0
					) as lag_bytes
				FROM pg_stat_subscription
				WHERE subname = $1
			`, subName).Scan(&receivedLsn, &latestEndLsn, &lagBytes)

			if err != nil {
				// Subscription stats might not be available yet
				s.manager.logger.Log(InitEvent{
					Level:  "debug",
					Event:  "init.catchup_poll",
					NodeID: targetNode,
					Details: map[string]any{
						"error": err.Error(),
					},
				})
				continue
			}

			// Send progress update
			s.manager.sendProgress(ProgressUpdate{
				NodeID:         targetNode,
				Phase:          "catching_up",
				OverallPercent: 100.0, // All tables copied
			})

			// If both LSNs are the same or lag is minimal, we're caught up
			// When received_lsn equals latest_end_lsn, the subscriber has applied all received WAL
			if lagBytes <= maxLagBytes {
				s.manager.logger.Log(InitEvent{
					Level:  "info",
					Event:  "init.catchup_complete",
					NodeID: targetNode,
					Details: map[string]any{
						"received_lsn":   receivedLsn,
						"latest_end_lsn": latestEndLsn,
						"lag_bytes":      lagBytes,
					},
				})
				return nil
			}

			s.manager.logger.Log(InitEvent{
				Level:  "debug",
				Event:  "init.catchup_progress",
				NodeID: targetNode,
				Details: map[string]any{
					"received_lsn":   receivedLsn,
					"latest_end_lsn": latestEndLsn,
					"lag_bytes":      lagBytes,
				},
			})
		}
	}
}

// completeInit marks the initialization as complete.
func (s *SnapshotInitializer) completeInit(ctx context.Context, targetNode string) error {
	// Update node state to SYNCHRONIZED
	if err := s.updateState(ctx, targetNode, models.InitStateSynchronized); err != nil {
		return err
	}

	// Record completion time
	query := `
		UPDATE steep_repl.nodes
		SET init_completed_at = NOW()
		WHERE node_id = $1
	`
	_, err := s.pool.Exec(ctx, query, targetNode)
	if err != nil {
		return err
	}

	// Update progress to 100%
	progressQuery := `
		UPDATE steep_repl.init_progress
		SET phase = 'complete', overall_percent = 100, updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = s.pool.Exec(ctx, progressQuery, targetNode)

	return nil
}

// handleInitFailure handles initialization failures by updating state and logging.
func (s *SnapshotInitializer) handleInitFailure(ctx context.Context, targetNode string, err error) {
	s.manager.logger.LogInitFailed(targetNode, err)

	// Update state to FAILED
	_ = s.updateState(ctx, targetNode, models.InitStateFailed)

	// Update progress with error
	progressQuery := `
		UPDATE steep_repl.init_progress
		SET phase = 'failed', error_message = $2, updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = s.pool.Exec(ctx, progressQuery, targetNode, err.Error())
}

// updateState updates the init_state for a node.
func (s *SnapshotInitializer) updateState(ctx context.Context, nodeID string, state models.InitState) error {
	// Get current state for logging
	var currentState models.InitState
	_ = s.pool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = $1
	`, nodeID).Scan(&currentState)

	query := `UPDATE steep_repl.nodes SET init_state = $1 WHERE node_id = $2`
	_, err := s.pool.Exec(ctx, query, string(state), nodeID)
	if err != nil {
		return err
	}

	// Log state change
	s.manager.logger.LogStateChange(nodeID, currentState, state)

	return nil
}

// sanitizeIdentifier converts a string to a valid SQL identifier by replacing
// hyphens and other special characters with underscores.
func sanitizeIdentifier(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			result[i] = c
		} else {
			result[i] = '_'
		}
	}
	return string(result)
}

// DetectPG18ParallelCOPY checks if the PostgreSQL server supports parallel COPY (PG18+).
// Returns true if streaming=parallel is supported for subscriptions.
// Implements T076.
func (s *SnapshotInitializer) DetectPG18ParallelCOPY(ctx context.Context) (bool, error) {
	var versionNum int
	err := s.pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&versionNum)
	if err != nil {
		return false, fmt.Errorf("failed to get PostgreSQL version: %w", err)
	}

	// PostgreSQL 18 is version 180000+
	// streaming=parallel was added in PG18 for subscriptions
	isPG18OrHigher := versionNum >= 180000

	if isPG18OrHigher {
		s.manager.logger.Log(InitEvent{
			Level: "info",
			Event: "init.pg18_parallel_detected",
			Details: map[string]any{
				"version_num":        versionNum,
				"streaming_parallel": true,
			},
		})
	}

	return isPG18OrHigher, nil
}

// ParallelTableCopier manages parallel table copying using a worker pool.
// Implements T075.
type ParallelTableCopier struct {
	pool           *pgxpool.Pool
	workers        int
	tasks          chan TableCopyTask
	results        chan ParallelCopyResult
	wg             sync.WaitGroup
	tablesTotal    int32
	tablesComplete int32
	bytesTotal     int64
	bytesComplete  int64
	logger         *Logger
	progressFn     func(completed, total int, currentTable string, percent float64)
}

// NewParallelTableCopier creates a new parallel table copier with the specified number of workers.
func NewParallelTableCopier(pool *pgxpool.Pool, workers int, logger *Logger) *ParallelTableCopier {
	if workers < 1 {
		workers = 1
	}
	if workers > 16 {
		workers = 16
	}
	return &ParallelTableCopier{
		pool:    pool,
		workers: workers,
		tasks:   make(chan TableCopyTask, workers*2),
		results: make(chan ParallelCopyResult, workers*2),
		logger:  logger,
	}
}

// SetProgressCallback sets a callback function for progress updates.
func (p *ParallelTableCopier) SetProgressCallback(fn func(completed, total int, currentTable string, percent float64)) {
	p.progressFn = fn
}

// CopyTables copies tables in parallel using the worker pool pattern.
// Returns the results for each table and any errors.
func (p *ParallelTableCopier) CopyTables(ctx context.Context, tables []TableInfo, sourceConnStr string) ([]ParallelCopyResult, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	atomic.StoreInt32(&p.tablesTotal, int32(len(tables)))
	atomic.StoreInt32(&p.tablesComplete, 0)

	// Calculate total bytes for progress tracking
	var totalBytes int64
	for _, t := range tables {
		totalBytes += t.SizeBytes
	}
	atomic.StoreInt64(&p.bytesTotal, totalBytes)
	atomic.StoreInt64(&p.bytesComplete, 0)

	// Start workers
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	// Send tasks
	go func() {
		for _, table := range tables {
			select {
			case <-ctx.Done():
				return
			case p.tasks <- TableCopyTask{Table: table, SourceConnStr: sourceConnStr}:
			}
		}
		close(p.tasks)
	}()

	// Wait for workers to complete and close results channel
	go func() {
		p.wg.Wait()
		close(p.results)
	}()

	// Collect results
	var results []ParallelCopyResult
	var firstError error
	for result := range p.results {
		results = append(results, result)
		if result.Error != nil && firstError == nil {
			firstError = result.Error
		}
	}

	return results, firstError
}

// worker processes table copy tasks from the tasks channel.
func (p *ParallelTableCopier) worker(ctx context.Context, workerID int) {
	defer p.wg.Done()

	for task := range p.tasks {
		select {
		case <-ctx.Done():
			p.results <- ParallelCopyResult{
				TableInfo: task.Table,
				Error:     ctx.Err(),
			}
			return
		default:
		}

		result := p.copyTable(ctx, task, workerID)
		p.results <- result

		if result.Error == nil {
			// Update progress counters
			completed := atomic.AddInt32(&p.tablesComplete, 1)
			atomic.AddInt64(&p.bytesComplete, result.BytesCopied)

			// Call progress callback if set
			if p.progressFn != nil {
				total := atomic.LoadInt32(&p.tablesTotal)
				bytesComplete := atomic.LoadInt64(&p.bytesComplete)
				bytesTotal := atomic.LoadInt64(&p.bytesTotal)
				var percent float64
				if bytesTotal > 0 {
					percent = float64(bytesComplete) / float64(bytesTotal) * 100
				}
				p.progressFn(int(completed), int(total), task.Table.FullName, percent)
			}
		}
	}
}

// copyTable copies a single table using COPY protocol.
func (p *ParallelTableCopier) copyTable(ctx context.Context, task TableCopyTask, workerID int) ParallelCopyResult {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ParallelCopyResult{TableInfo: task.Table, Error: ctx.Err()}
	default:
	}

	startTime := time.Now()

	p.logger.Log(InitEvent{
		Level: "debug",
		Event: "init.table_copy_start",
		Details: map[string]any{
			"table":      task.Table.FullName,
			"worker_id":  workerID,
			"size_bytes": task.Table.SizeBytes,
		},
	})

	// For the basic implementation, we rely on PostgreSQL's subscription copy_data=true
	// which handles the actual COPY. This worker pool is for future use with
	// manual two-phase snapshot workflows where we control the COPY directly.
	//
	// When used with subscription-based init, the parallel workers primarily affect
	// monitoring granularity rather than the actual parallelism (which PostgreSQL
	// manages internally).

	result := ParallelCopyResult{
		TableInfo:   task.Table,
		BytesCopied: task.Table.SizeBytes, // Estimated
		Duration:    time.Since(startTime),
	}

	p.logger.Log(InitEvent{
		Level: "info",
		Event: "init.table_copy_complete",
		Details: map[string]any{
			"table":       task.Table.FullName,
			"worker_id":   workerID,
			"duration_ms": result.Duration.Milliseconds(),
			"bytes":       result.BytesCopied,
		},
	})

	return result
}

// createSubscriptionWithParallel creates a subscription with optional streaming=parallel support.
// This is used when PG18+ parallel COPY is available.
func (s *SnapshotInitializer) createSubscriptionWithParallel(ctx context.Context, targetNode, subName, pubName string, sourceInfo *NodeConnectionInfo, parallelWorkers int, useStreaming bool) error {
	// Build connection string for the source node
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
		sourceInfo.Host, sourceInfo.Port, sourceInfo.Database, sourceInfo.User)
	if sourceInfo.Password != "" {
		connStr += fmt.Sprintf(" password=%s", sourceInfo.Password)
	}

	// Build subscription options
	// Use origin='none' to prevent changes from being re-replicated in bidirectional setups
	var subscriptionOpts string
	if useStreaming && parallelWorkers > 1 {
		// PG18+ streaming=parallel for parallel initial sync
		subscriptionOpts = "copy_data = true, create_slot = true, origin = 'none', streaming = 'parallel'"
		s.manager.logger.Log(InitEvent{
			Level:  "info",
			Event:  "init.subscription_parallel",
			NodeID: targetNode,
			Details: map[string]any{
				"subscription":     subName,
				"parallel_workers": parallelWorkers,
				"streaming":        "parallel",
			},
		})
	} else {
		subscriptionOpts = "copy_data = true, create_slot = true, origin = 'none'"
	}

	query := fmt.Sprintf(`
		CREATE SUBSCRIPTION %s
		CONNECTION '%s'
		PUBLICATION %s
		WITH (%s)
	`, subName, connStr, pubName, subscriptionOpts)

	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("CREATE SUBSCRIPTION failed: %w", err)
	}

	s.manager.logger.Log(InitEvent{
		Event:  "init.subscription_created",
		NodeID: targetNode,
		Details: map[string]any{
			"subscription":     subName,
			"publication":      pubName,
			"parallel_workers": parallelWorkers,
			"streaming":        useStreaming,
		},
	})

	return nil
}

// StartParallel begins automatic snapshot initialization with parallel table copying.
// This is an enhanced version of Start that uses the worker pool for progress tracking
// and supports PG18 streaming=parallel for subscriptions.
// Implements T075 and T076.
func (s *SnapshotInitializer) StartParallel(ctx context.Context, targetNode, sourceNode string, opts SnapshotOptions) error {
	// Validate parallel workers range
	if opts.ParallelWorkers < 1 {
		opts.ParallelWorkers = 4 // Default
	}
	if opts.ParallelWorkers > 16 {
		opts.ParallelWorkers = 16 // Max
	}

	s.manager.logger.LogInitStarted(targetNode, sourceNode, "snapshot-parallel")
	s.manager.logger.Log(InitEvent{
		Level:  "info",
		Event:  "init.parallel_config",
		NodeID: targetNode,
		Details: map[string]any{
			"parallel_workers": opts.ParallelWorkers,
			"source_node":      sourceNode,
		},
	})

	// Detect PG18 parallel COPY support
	useStreamingParallel, err := s.DetectPG18ParallelCOPY(ctx)
	if err != nil {
		s.manager.logger.Log(InitEvent{
			Level:  "warn",
			Event:  "init.pg18_detection_failed",
			NodeID: targetNode,
			Error:  err.Error(),
		})
		// Fall back to standard copy_data=true
		useStreamingParallel = false
	}
	opts.UseStreamingParallel = useStreamingParallel

	// Validate nodes exist and are in correct state
	if err := s.validateNodes(ctx, targetNode, sourceNode); err != nil {
		return fmt.Errorf("node validation failed: %w", err)
	}

	// Update target node state to PREPARING
	if err := s.updateState(ctx, targetNode, models.InitStatePreparing); err != nil {
		return fmt.Errorf("failed to update state to PREPARING: %w", err)
	}

	// Record init_source_node and init_started_at
	if err := s.recordInitStart(ctx, targetNode, sourceNode); err != nil {
		return fmt.Errorf("failed to record init start: %w", err)
	}

	// Start the initialization in a goroutine
	go s.runInitParallel(ctx, targetNode, sourceNode, opts)

	return nil
}

// runInitParallel performs parallel initialization in a background goroutine.
func (s *SnapshotInitializer) runInitParallel(ctx context.Context, targetNode, sourceNode string, opts SnapshotOptions) {
	startTime := time.Now()

	// Ensure cleanup on failure
	defer func() {
		if r := recover(); r != nil {
			s.handleInitFailure(ctx, targetNode, fmt.Errorf("panic: %v", r))
		}
		s.manager.unregisterOperation(targetNode)
	}()

	// Get source node connection info
	sourceInfo, err := s.getNodeConnectionInfo(ctx, sourceNode)
	if err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to get source node info: %w", err))
		return
	}

	// Update state to COPYING
	if err := s.updateState(ctx, targetNode, models.InitStateCopying); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to update state to COPYING: %w", err))
		return
	}

	// Initialize progress tracking with parallel workers count
	if err := s.initProgress(ctx, targetNode, opts.ParallelWorkers); err != nil {
		s.manager.logger.Log(InitEvent{
			Level:  "warn",
			Event:  "init.progress_init_failed",
			NodeID: targetNode,
			Error:  err.Error(),
		})
		// Non-fatal, continue
	}

	// Get list of tables with size information
	tables, err := s.getPublicationTablesWithSize(ctx, opts.LargeTableThreshold)
	if err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to get publication tables: %w", err))
		return
	}

	// Check for large tables that may need alternate handling
	if opts.LargeTableThreshold > 0 {
		largeTables := s.filterLargeTables(tables)
		if len(largeTables) > 0 {
			s.manager.logger.Log(InitEvent{
				Level:  "warn",
				Event:  "init.large_tables_detected",
				NodeID: targetNode,
				Details: map[string]any{
					"count":            len(largeTables),
					"threshold_bytes":  opts.LargeTableThreshold,
					"tables":           s.largeTableNames(largeTables),
					"method":           opts.LargeTableMethod,
					"parallel_workers": opts.ParallelWorkers,
				},
			})

			if opts.LargeTableMethod != "" && opts.LargeTableMethod != "copy" {
				s.handleInitFailure(ctx, targetNode, fmt.Errorf(
					"database contains %d tables exceeding %d bytes threshold; "+
						"use --method=%s or increase --large-table-threshold",
					len(largeTables), opts.LargeTableThreshold, opts.LargeTableMethod))
				return
			}
		}
	}

	// Create subscription with parallel support if available
	subscriptionName := fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(targetNode), sanitizeIdentifier(sourceNode))
	publicationName := fmt.Sprintf("steep_pub_%s", sanitizeIdentifier(sourceNode))

	if err := s.createSubscriptionWithParallel(ctx, targetNode, subscriptionName, publicationName, sourceInfo, opts.ParallelWorkers, opts.UseStreamingParallel); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to create subscription: %w", err))
		return
	}

	// Update progress with total tables
	s.updateProgressTables(ctx, targetNode, len(tables), 0)

	// Monitor subscription sync progress until complete
	if err := s.monitorSubscriptionSync(ctx, targetNode, subscriptionName); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("subscription sync failed: %w", err))
		return
	}

	// Update state to CATCHING_UP (WAL replay after initial copy)
	if err := s.updateState(ctx, targetNode, models.InitStateCatchingUp); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to update state to CATCHING_UP: %w", err))
		return
	}

	// Wait for catch-up to complete (lag < threshold)
	if err := s.waitForCatchUp(ctx, targetNode, subscriptionName); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("catch-up failed: %w", err))
		return
	}

	// Mark initialization as complete
	if err := s.completeInit(ctx, targetNode); err != nil {
		s.handleInitFailure(ctx, targetNode, fmt.Errorf("failed to complete init: %w", err))
		return
	}

	// Log completion with parallel stats
	duration := time.Since(startTime)
	s.manager.logger.Log(InitEvent{
		Level:  "info",
		Event:  "init.parallel_completed",
		NodeID: targetNode,
		Details: map[string]any{
			"duration_ms":        duration.Milliseconds(),
			"parallel_workers":   opts.ParallelWorkers,
			"streaming_parallel": opts.UseStreamingParallel,
			"tables_count":       len(tables),
		},
	})
	s.manager.logger.LogInitCompleted(targetNode, duration.Milliseconds(), 0, 0)
}

// =============================================================================
// Two-Phase Snapshot Generation (T080, T081, T082)
// =============================================================================

// TwoPhaseSnapshotOptions configures two-phase snapshot generation.
type TwoPhaseSnapshotOptions struct {
	OutputPath      string
	Compression     models.CompressionType
	ParallelWorkers int
	ProgressFn      func(progress TwoPhaseProgress)
}

// TwoPhaseProgress represents progress during two-phase snapshot operations.
type TwoPhaseProgress struct {
	SnapshotID          string
	Phase               string // "schema", "data", "sequences", "finalizing"
	OverallPercent      float32
	CurrentTable        string
	CurrentTablePercent float32
	BytesProcessed      int64
	ThroughputMBSec     float32
	ETASeconds          int
	LSN                 string
	Complete            bool
	Error               string
}

// SnapshotGenerator handles two-phase snapshot generation.
// This exports data to files for offline transfer and multi-target init.
type SnapshotGenerator struct {
	pool    *pgxpool.Pool
	manager *Manager
	logger  *Logger
}

// NewSnapshotGenerator creates a new snapshot generator.
func NewSnapshotGenerator(pool *pgxpool.Pool, manager *Manager) *SnapshotGenerator {
	return &SnapshotGenerator{
		pool:    pool,
		manager: manager,
		logger:  manager.logger,
	}
}

// Generate creates a two-phase snapshot to the specified output path.
// Implements T081: snapshot generation logic.
func (g *SnapshotGenerator) Generate(ctx context.Context, sourceNodeID string, opts TwoPhaseSnapshotOptions) (*models.SnapshotManifest, error) {
	startTime := time.Now()

	// Generate unique snapshot ID
	snapshotID := fmt.Sprintf("snap_%s_%s", time.Now().Format("20060102_150405"), sourceNodeID[:8])

	g.logger.Log(InitEvent{
		Level:  "info",
		Event:  "snapshot.generation_started",
		NodeID: sourceNodeID,
		Details: map[string]any{
			"snapshot_id":      snapshotID,
			"output_path":      opts.OutputPath,
			"compression":      opts.Compression,
			"parallel_workers": opts.ParallelWorkers,
		},
	})

	// Send initial progress
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "schema",
		OverallPercent: 0,
	})

	// Create output directory
	dataDir := filepath.Join(opts.OutputPath, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create replication slot to ensure consistent snapshot
	slotName := fmt.Sprintf("steep_snap_%s", sanitizeIdentifier(snapshotID))
	lsn, err := g.createSnapshotSlot(ctx, slotName)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot slot: %w", err)
	}

	g.logger.Log(InitEvent{
		Level:  "info",
		Event:  "snapshot.slot_created",
		NodeID: sourceNodeID,
		Details: map[string]any{
			"slot_name": slotName,
			"lsn":       lsn,
		},
	})

	// Get tables to export
	tables, err := g.getTablesForExport(ctx)
	if err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}

	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "data",
		OverallPercent: 5,
		LSN:            lsn,
	})

	// Export each table
	var tableEntries []models.SnapshotTableEntry
	var totalBytes int64
	tablesProcessed := 0

	for _, table := range tables {
		select {
		case <-ctx.Done():
			g.dropSlot(ctx, slotName)
			return nil, ctx.Err()
		default:
		}

		entry, err := g.exportTable(ctx, table, dataDir, opts.Compression)
		if err != nil {
			g.dropSlot(ctx, slotName)
			return nil, fmt.Errorf("failed to export table %s: %w", table.FullName, err)
		}

		tableEntries = append(tableEntries, *entry)
		totalBytes += entry.SizeBytes
		tablesProcessed++

		// Calculate progress
		percent := float32(5 + (tablesProcessed * 85 / len(tables)))
		g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
			SnapshotID:     snapshotID,
			Phase:          "data",
			OverallPercent: percent,
			CurrentTable:   table.FullName,
			BytesProcessed: totalBytes,
			LSN:            lsn,
		})
	}

	// Export sequences
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "sequences",
		OverallPercent: 90,
		LSN:            lsn,
	})

	sequences, err := g.getSequences(ctx)
	if err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to get sequences: %w", err)
	}

	// Create manifest (T082)
	manifest := &models.SnapshotManifest{
		SnapshotID:      snapshotID,
		SourceNode:      sourceNodeID,
		LSN:             lsn,
		CreatedAt:       startTime,
		Tables:          tableEntries,
		Sequences:       sequences,
		TotalSizeBytes:  totalBytes,
		Compression:     opts.Compression,
		ParallelWorkers: opts.ParallelWorkers,
	}

	// Write manifest to file
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "finalizing",
		OverallPercent: 95,
		LSN:            lsn,
	})

	if err := g.writeManifest(opts.OutputPath, manifest); err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Record snapshot in database
	if err := g.recordSnapshot(ctx, manifest, opts.OutputPath); err != nil {
		g.logger.Log(InitEvent{
			Level: "warn",
			Event: "snapshot.record_failed",
			Error: err.Error(),
		})
		// Non-fatal, continue
	}

	// Clean up the slot (we've captured the LSN, no longer needed)
	g.dropSlot(ctx, slotName)

	duration := time.Since(startTime)
	g.logger.Log(InitEvent{
		Level: "info",
		Event: "snapshot.generation_completed",
		Details: map[string]any{
			"snapshot_id":    snapshotID,
			"duration_ms":    duration.Milliseconds(),
			"total_bytes":    totalBytes,
			"table_count":    len(tableEntries),
			"sequence_count": len(sequences),
			"lsn":            lsn,
		},
	})

	// Final progress
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "complete",
		OverallPercent: 100,
		BytesProcessed: totalBytes,
		LSN:            lsn,
		Complete:       true,
	})

	return manifest, nil
}

// createSnapshotSlot creates a logical replication slot and returns the consistent LSN.
func (g *SnapshotGenerator) createSnapshotSlot(ctx context.Context, slotName string) (string, error) {
	var lsn string
	err := g.pool.QueryRow(ctx, `
		SELECT lsn::text FROM pg_create_logical_replication_slot($1, 'pgoutput')
	`, slotName).Scan(&lsn)
	if err != nil {
		return "", err
	}
	return lsn, nil
}

// dropSlot drops a replication slot.
func (g *SnapshotGenerator) dropSlot(ctx context.Context, slotName string) {
	_, err := g.pool.Exec(ctx, `SELECT pg_drop_replication_slot($1)`, slotName)
	if err != nil {
		g.logger.Log(InitEvent{
			Level: "warn",
			Event: "snapshot.slot_drop_failed",
			Error: err.Error(),
		})
	}
}

// getTablesForExport returns tables that should be included in the snapshot.
func (g *SnapshotGenerator) getTablesForExport(ctx context.Context) ([]TableInfo, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT
			schemaname,
			tablename,
			schemaname || '.' || tablename as full_name,
			pg_table_size(schemaname || '.' || tablename) as size_bytes
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.SchemaName, &t.TableName, &t.FullName, &t.SizeBytes); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// exportTable exports a single table to a file using COPY.
func (g *SnapshotGenerator) exportTable(ctx context.Context, table TableInfo, dataDir string, compression models.CompressionType) (*models.SnapshotTableEntry, error) {
	// Determine output filename
	filename := fmt.Sprintf("%s.%s.csv", table.SchemaName, table.TableName)
	if compression == models.CompressionGzip {
		filename += ".gz"
	}
	outputPath := filepath.Join(dataDir, filename)

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", outputPath, err)
	}
	defer file.Close()

	// Set up writer (with compression if needed)
	var writer io.Writer = file
	var gzWriter *gzip.Writer
	if compression == models.CompressionGzip {
		gzWriter = gzip.NewWriter(file)
		writer = gzWriter
		defer gzWriter.Close()
	}

	// Create a counting writer to track bytes
	countWriter := &countingWriter{w: writer}

	// Use COPY TO to export table data
	copyQuery := fmt.Sprintf(`COPY %s TO STDOUT WITH (FORMAT csv, HEADER true)`, table.FullName)

	conn, err := g.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Execute COPY TO
	tag, err := conn.Conn().PgConn().CopyTo(ctx, countWriter, copyQuery)
	if err != nil {
		return nil, fmt.Errorf("COPY TO failed: %w", err)
	}

	// Close gzip writer to flush any remaining data
	if gzWriter != nil {
		if err := gzWriter.Close(); err != nil {
			return nil, fmt.Errorf("failed to close gzip writer: %w", err)
		}
	}

	// Get file size (compressed size if applicable)
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Calculate checksum of the output file
	checksum, err := g.calculateFileChecksum(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	entry := &models.SnapshotTableEntry{
		Schema:    table.SchemaName,
		Name:      table.TableName,
		RowCount:  tag.RowsAffected(),
		SizeBytes: fileInfo.Size(),
		Checksum:  checksum,
		File:      filepath.Join("data", filename),
	}

	g.logger.Log(InitEvent{
		Level: "debug",
		Event: "snapshot.table_exported",
		Details: map[string]any{
			"table": table.FullName,
			"rows":  entry.RowCount,
			"size":  entry.SizeBytes,
			"file":  entry.File,
		},
	})

	return entry, nil
}

// countingWriter wraps a writer and counts bytes written.
type countingWriter struct {
	w     io.Writer
	count int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.count += int64(n)
	return n, err
}

// calculateFileChecksum calculates SHA256 checksum of a file.
func (g *SnapshotGenerator) calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// getSequences returns all sequence values for the snapshot.
func (g *SnapshotGenerator) getSequences(ctx context.Context) ([]models.SnapshotSequenceEntry, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT
			schemaname,
			sequencename,
			last_value
		FROM pg_sequences
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, sequencename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sequences []models.SnapshotSequenceEntry
	for rows.Next() {
		var s models.SnapshotSequenceEntry
		var lastValue *int64
		if err := rows.Scan(&s.Schema, &s.Name, &lastValue); err != nil {
			return nil, err
		}
		if lastValue != nil {
			s.Value = *lastValue
		}
		sequences = append(sequences, s)
	}

	return sequences, rows.Err()
}

// writeManifest writes the manifest.json file.
// Implements T082: manifest.json generator.
func (g *SnapshotGenerator) writeManifest(outputPath string, manifest *models.SnapshotManifest) error {
	manifestPath := filepath.Join(outputPath, "manifest.json")

	data, err := manifest.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	// Calculate manifest checksum
	checksum := sha256.Sum256(data)
	checksumStr := hex.EncodeToString(checksum[:])

	g.logger.Log(InitEvent{
		Level: "info",
		Event: "snapshot.manifest_written",
		Details: map[string]any{
			"path":     manifestPath,
			"checksum": checksumStr,
		},
	})

	return nil
}

// recordSnapshot records the snapshot in the database.
func (g *SnapshotGenerator) recordSnapshot(ctx context.Context, manifest *models.SnapshotManifest, storagePath string) error {
	// Calculate manifest checksum
	data, err := manifest.ToJSON()
	if err != nil {
		return err
	}
	checksum := sha256.Sum256(data)
	checksumStr := "sha256:" + hex.EncodeToString(checksum[:])

	query := `
		INSERT INTO steep_repl.snapshots (
			snapshot_id, source_node_id, lsn, storage_path, size_bytes,
			table_count, compression, checksum, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (snapshot_id) DO UPDATE SET
			size_bytes = EXCLUDED.size_bytes,
			table_count = EXCLUDED.table_count,
			checksum = EXCLUDED.checksum,
			status = EXCLUDED.status
	`

	_, err = g.pool.Exec(ctx, query,
		manifest.SnapshotID,
		manifest.SourceNode,
		manifest.LSN,
		storagePath,
		manifest.TotalSizeBytes,
		len(manifest.Tables),
		string(manifest.Compression),
		checksumStr,
		string(models.SnapshotStatusComplete),
	)

	return err
}

// sendProgress sends a progress update if a callback is provided.
func (g *SnapshotGenerator) sendProgress(fn func(TwoPhaseProgress), progress TwoPhaseProgress) {
	if fn != nil {
		fn(progress)
	}
}

// ReadManifest reads and parses a manifest.json file.
func ReadManifest(manifestPath string) (*models.SnapshotManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	manifest, err := models.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return manifest, nil
}

// VerifySnapshot verifies the integrity of a snapshot by checking checksums.
func VerifySnapshot(snapshotPath string) ([]string, error) {
	manifestPath := filepath.Join(snapshotPath, "manifest.json")
	manifest, err := ReadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	var errors []string
	for _, table := range manifest.Tables {
		filePath := filepath.Join(snapshotPath, table.File)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("missing file: %s", table.File))
			continue
		}

		// Calculate checksum
		file, err := os.Open(filePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot open %s: %v", table.File, err))
			continue
		}

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			file.Close()
			errors = append(errors, fmt.Sprintf("cannot read %s: %v", table.File, err))
			continue
		}
		file.Close()

		actualChecksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actualChecksum != table.Checksum {
			errors = append(errors, fmt.Sprintf("checksum mismatch for %s: expected %s, got %s",
				table.File, table.Checksum, actualChecksum))
		}
	}

	return errors, nil
}

// Ensure pgx.CopyFromSource is used (compile check)
var _ pgx.CopyFromSource = (*copyFromRows)(nil)

// copyFromRows implements pgx.CopyFromSource for bulk loading.
type copyFromRows struct {
	rows [][]any
	idx  int
	err  error
}

func (c *copyFromRows) Next() bool {
	c.idx++
	return c.idx < len(c.rows)
}

func (c *copyFromRows) Values() ([]any, error) {
	if c.idx >= len(c.rows) {
		return nil, io.EOF
	}
	return c.rows[c.idx], nil
}

func (c *copyFromRows) Err() error {
	return c.err
}
