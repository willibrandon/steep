package init

import (
	"context"
	"fmt"
	"os"
	"time"

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
	ParallelWorkers     int
	LargeTableThreshold int64  // Bytes, tables larger than this use alternate method
	LargeTableMethod    string // pg_dump, copy, basebackup
	Force               bool   // Truncate existing data if present
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

	// Extract table names for sync monitoring
	tableNames := make([]string, len(tables))
	for i, t := range tables {
		tableNames[i] = t.FullName
	}

	// Monitor subscription sync progress until complete
	if err := s.monitorSubscriptionSync(ctx, targetNode, subscriptionName, tableNames); err != nil {
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

// getPublicationTables retrieves the list of tables in the source node's publication.
func (s *SnapshotInitializer) getPublicationTables(ctx context.Context, sourceNode string) ([]string, error) {
	// Query the pg_publication_tables view to get tables in the publication
	// Note: This requires a connection to the source node's database
	// For now, return all user tables from pg_tables
	rows, err := s.pool.Query(ctx, `
		SELECT schemaname || '.' || tablename as full_name
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	return tables, rows.Err()
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

// detectLargeTables identifies tables that exceed the size threshold.
// Implements T025 large table detection.
func (s *SnapshotInitializer) detectLargeTables(ctx context.Context, threshold int64) ([]TableInfo, error) {
	if threshold <= 0 {
		// No threshold, no large tables
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			schemaname,
			tablename,
			schemaname || '.' || tablename as full_name,
			pg_table_size(schemaname || '.' || tablename) as size_bytes
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		  AND pg_table_size(schemaname || '.' || tablename) > $1
		ORDER BY size_bytes DESC
	`, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var largeTables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.SchemaName, &t.TableName, &t.FullName, &t.SizeBytes); err != nil {
			return nil, err
		}
		t.IsLarge = true
		largeTables = append(largeTables, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Log large table detection
	if len(largeTables) > 0 {
		tableNames := make([]string, len(largeTables))
		for i, t := range largeTables {
			tableNames[i] = t.FullName
		}
		s.manager.logger.Log(InitEvent{
			Level: "warn",
			Event: "init.large_tables_detected",
			Details: map[string]any{
				"threshold_bytes": threshold,
				"table_count":     len(largeTables),
				"tables":          tableNames,
			},
		})
	}

	return largeTables, nil
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
	query := fmt.Sprintf(`
		CREATE SUBSCRIPTION %s
		CONNECTION '%s'
		PUBLICATION %s
		WITH (copy_data = true, create_slot = true)
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
func (s *SnapshotInitializer) monitorSubscriptionSync(ctx context.Context, targetNode, subName string, tables []string) error {
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

// updateProgress updates the overall progress percent.
func (s *SnapshotInitializer) updateProgress(ctx context.Context, nodeID string, percent float32, tablesCompleted int) {
	query := `
		UPDATE steep_repl.init_progress
		SET overall_percent = $2, tables_completed = $3, updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = s.pool.Exec(ctx, query, nodeID, percent, tablesCompleted)
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
