package init

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/willibrandon/steep/internal/repl/config"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	"github.com/willibrandon/steep/internal/repl/models"
)

// ManualInitializer handles manual initialization from user-provided backups.
type ManualInitializer struct {
	pool    *pgxpool.Pool
	manager *Manager
}

// NewManualInitializer creates a new manual initializer.
func NewManualInitializer(pool *pgxpool.Pool, manager *Manager) *ManualInitializer {
	return &ManualInitializer{
		pool:    pool,
		manager: manager,
	}
}

// PrepareResult contains the result of a prepare operation.
type PrepareResult struct {
	SlotName  string
	LSN       string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Prepare creates a replication slot and records the LSN for manual initialization.
// This should be called on the SOURCE node before the user runs pg_basebackup/pg_dump.
func (m *ManualInitializer) Prepare(ctx context.Context, nodeID, slotName string, expiresDuration time.Duration) (*PrepareResult, error) {
	// Check if there's already an active subscription slot for this source
	// This indicates replication is already set up and the prepared slot won't be needed
	var activeSubSlot string
	err := m.pool.QueryRow(ctx, `
		SELECT slot_name FROM pg_replication_slots
		WHERE slot_type = 'logical'
		AND active = true
		AND slot_name LIKE 'steep_sub_%'
		LIMIT 1
	`).Scan(&activeSubSlot)
	if err == nil && activeSubSlot != "" {
		return nil, fmt.Errorf("replication already active via slot %q; prepared slot would be orphaned", activeSubSlot)
	}

	// First check if slot already exists
	var existing int
	err = m.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_replication_slots WHERE slot_name = $1
	`, slotName).Scan(&existing)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing slots: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("replication slot %q already exists", slotName)
	}

	// Create logical replication slot - returns name and lsn
	var resultSlotName, lsn string
	query := `SELECT slot_name, lsn FROM pg_create_logical_replication_slot($1, 'pgoutput')`
	if err := m.pool.QueryRow(ctx, query, slotName).Scan(&resultSlotName, &lsn); err != nil {
		return nil, fmt.Errorf("failed to create replication slot: %w", err)
	}

	// Record slot in init_slots table
	now := time.Now()
	expiresAt := now.Add(expiresDuration)
	insertQuery := `
		INSERT INTO steep_repl.init_slots (slot_name, node_id, lsn, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (slot_name) DO UPDATE SET
			lsn = EXCLUDED.lsn,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
	`
	if _, err := m.pool.Exec(ctx, insertQuery, slotName, nodeID, lsn, now, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to record slot: %w", err)
	}

	// Log the prepare event
	m.manager.logger.Log(InitEvent{
		Event:  "init.prepare",
		NodeID: nodeID,
		Details: map[string]any{
			"slot_name":  slotName,
			"lsn":        lsn,
			"expires_at": expiresAt.Format(time.RFC3339),
		},
	})

	return &PrepareResult{
		SlotName:  slotName,
		LSN:       lsn,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

// CompleteOptions configures the complete operation.
type CompleteOptions struct {
	TargetNodeID    string
	SourceNodeID    string
	SourceLSN       string // Optional - can be looked up from init_slots
	SourceHost      string // Required for subscription connection (what PostgreSQL uses)
	SourcePort      int
	SourceDatabase  string
	SourceUser      string
	SourceRemote    string // gRPC address of source daemon for schema verification
	SchemaSyncMode  config.SchemaSyncMode
	SkipSchemaCheck bool
}

// SchemaDifference represents a difference found during schema comparison.
type SchemaDifference struct {
	TableSchema string
	TableName   string
	Type        string // missing_local, missing_remote, column_mismatch
	Details     string
}

// Complete finishes manual initialization after user has restored their backup.
// This should be called on the TARGET node after the user has restored the backup.
func (m *ManualInitializer) Complete(ctx context.Context, opts CompleteOptions) error {
	// Verify schema matches BEFORE changing state (so retries work on validation failures)
	if !opts.SkipSchemaCheck {
		diffs, err := m.verifySchema(ctx, opts)
		if err != nil {
			return fmt.Errorf("schema verification failed: %w", err)
		}
		if len(diffs) > 0 && opts.SchemaSyncMode == config.SchemaSyncStrict {
			return fmt.Errorf("schema mismatch detected: %d differences found (use --schema-sync=manual to proceed anyway)", len(diffs))
		}
	}

	// Update state to PREPARING (only after validation passes)
	if err := m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStatePreparing); err != nil {
		return fmt.Errorf("failed to update state to preparing: %w", err)
	}

	// Look up LSN from init_slots if not provided
	if opts.SourceLSN == "" {
		lsn, err := m.lookupSlotLSN(ctx, opts.SourceNodeID)
		if err != nil {
			m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStateFailed)
			return fmt.Errorf("failed to lookup LSN from init_slots: %w", err)
		}
		opts.SourceLSN = lsn
	}

	// Create subscription with copy_data=false
	if err := m.createSubscription(ctx, opts); err != nil {
		m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStateFailed)
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Register source node in local nodes table if it doesn't exist
	if err := m.ensureSourceNodeRegistered(ctx, opts); err != nil {
		// Log but don't fail - this is not critical for replication
		m.manager.logger.Log(InitEvent{
			Event:  "init.source_node_registration_failed",
			NodeID: opts.TargetNodeID,
			Details: map[string]any{
				"source_node": opts.SourceNodeID,
				"error":       err.Error(),
			},
		})
	}

	// Update state to CATCHING_UP (subscription will sync WAL changes)
	if err := m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStateCatchingUp); err != nil {
		return fmt.Errorf("failed to update state to catching_up: %w", err)
	}

	// Log the complete event
	m.manager.logger.Log(InitEvent{
		Event:  "init.complete_started",
		NodeID: opts.TargetNodeID,
		Details: map[string]any{
			"source_node_id": opts.SourceNodeID,
			"source_lsn":     opts.SourceLSN,
		},
	})

	// Start catch-up monitoring in background
	go m.monitorCatchUp(context.Background(), opts.TargetNodeID)

	return nil
}

// lookupSlotLSN looks up the LSN from a prepared slot in init_slots table.
func (m *ManualInitializer) lookupSlotLSN(ctx context.Context, sourceNodeID string) (string, error) {
	var lsn string
	query := `
		SELECT lsn FROM steep_repl.init_slots
		WHERE node_id = $1
		AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
		LIMIT 1
	`
	err := m.pool.QueryRow(ctx, query, sourceNodeID).Scan(&lsn)
	if err != nil {
		return "", fmt.Errorf("no valid slot found for node %s: %w", sourceNodeID, err)
	}
	return lsn, nil
}

// lookupSlotNameFromSource queries the source database for the slot name using dblink.
// We use dblink because the subscription will connect from target PG to source PG,
// so we use the same network path to look up the slot name.
func (m *ManualInitializer) lookupSlotNameFromSource(ctx context.Context, opts CompleteOptions) (string, error) {
	// Build connection string for dblink
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable",
		opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

	// Use dblink from target PostgreSQL to query source PostgreSQL.
	// The subscription connects target->source, so we validate connectivity the same way.
	query := `
		SELECT slot_name FROM dblink(
			$1,
			$2
		) AS t(slot_name text)
	`
	remoteQuery := fmt.Sprintf(`
		SELECT slot_name FROM steep_repl.init_slots
		WHERE node_id = '%s'
		AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
		LIMIT 1
	`, opts.SourceNodeID)

	var slotName string
	err := m.pool.QueryRow(ctx, query, connStr, remoteQuery).Scan(&slotName)
	if err != nil {
		return "", fmt.Errorf("no valid slot found for node %s on source: %w", opts.SourceNodeID, err)
	}
	return slotName, nil
}

// lookupPublicationFromSource queries the source database for an existing publication.
// Returns the first publication found that publishes all tables.
func (m *ManualInitializer) lookupPublicationFromSource(ctx context.Context, opts CompleteOptions) (string, error) {
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable",
		opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

	query := `
		SELECT pubname FROM dblink(
			$1,
			'SELECT pubname FROM pg_publication WHERE puballtables = true ORDER BY pubname LIMIT 1'
		) AS t(pubname text)
	`

	var pubName string
	err := m.pool.QueryRow(ctx, query, connStr).Scan(&pubName)
	if err != nil {
		return "", fmt.Errorf("no FOR ALL TABLES publication found on source: %w", err)
	}
	return pubName, nil
}

// verifySlotExists checks that the replication slot actually exists on the source database.
func (m *ManualInitializer) verifySlotExists(ctx context.Context, opts CompleteOptions, slotName string) error {
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable",
		opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

	query := `
		SELECT slot_name FROM dblink(
			$1,
			$2
		) AS t(slot_name text)
	`
	remoteQuery := fmt.Sprintf(`
		SELECT slot_name FROM pg_replication_slots WHERE slot_name = '%s'
	`, slotName)

	var foundSlot string
	err := m.pool.QueryRow(ctx, query, connStr, remoteQuery).Scan(&foundSlot)
	if err != nil {
		return fmt.Errorf("slot does not exist: %w", err)
	}
	return nil
}

// verifySchema compares schema between local and source tables.
// It uses fingerprinting to detect differences.
func (m *ManualInitializer) verifySchema(ctx context.Context, opts CompleteOptions) ([]SchemaDifference, error) {
	var diffs []SchemaDifference

	// Get local tables
	localTables, err := m.getTableFingerprints(ctx, m.pool)
	if err != nil {
		return nil, fmt.Errorf("failed to get local table fingerprints: %w", err)
	}

	// Get remote tables via gRPC if SourceRemote is provided, otherwise direct connection
	var remoteTables map[string]string
	if opts.SourceRemote != "" {
		remoteTables, err = m.getRemoteTableFingerprintsViaGRPC(ctx, opts.SourceRemote)
		if err != nil {
			return nil, fmt.Errorf("failed to get source table fingerprints via gRPC: %w", err)
		}
	} else {
		// Fall back to direct connection (legacy behavior)
		connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable",
			opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

		sourcePool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to source for schema comparison: %w", err)
		}
		defer sourcePool.Close()

		remoteTables, err = m.getTableFingerprints(ctx, sourcePool)
		if err != nil {
			return nil, fmt.Errorf("failed to get source table fingerprints: %w", err)
		}
	}

	// Compare tables
	for key, localFP := range localTables {
		if remoteFP, ok := remoteTables[key]; ok {
			if localFP != remoteFP {
				parts := strings.SplitN(key, ".", 2)
				diffs = append(diffs, SchemaDifference{
					TableSchema: parts[0],
					TableName:   parts[1],
					Type:        "column_mismatch",
					Details:     fmt.Sprintf("local fingerprint %s != remote fingerprint %s", localFP[:16], remoteFP[:16]),
				})
			}
		} else {
			parts := strings.SplitN(key, ".", 2)
			diffs = append(diffs, SchemaDifference{
				TableSchema: parts[0],
				TableName:   parts[1],
				Type:        "missing_remote",
				Details:     "table exists locally but not on source",
			})
		}
	}

	for key := range remoteTables {
		if _, ok := localTables[key]; !ok {
			parts := strings.SplitN(key, ".", 2)
			diffs = append(diffs, SchemaDifference{
				TableSchema: parts[0],
				TableName:   parts[1],
				Type:        "missing_local",
				Details:     "table exists on source but not locally",
			})
		}
	}

	return diffs, nil
}

// getRemoteTableFingerprintsViaGRPC fetches schema fingerprints from a remote daemon via gRPC.
func (m *ManualInitializer) getRemoteTableFingerprintsViaGRPC(ctx context.Context, remoteAddr string) (map[string]string, error) {
	// Connect to remote daemon
	conn, err := grpc.NewClient(remoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source daemon: %w", err)
	}
	defer conn.Close()

	client := pb.NewInitServiceClient(conn)

	resp, err := client.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get schema fingerprints: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("source daemon error: %s", resp.Error)
	}

	// Convert response to map
	fingerprints := make(map[string]string)
	for _, fp := range resp.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		fingerprints[key] = fp.Fingerprint
	}

	return fingerprints, nil
}

// getTableFingerprints computes fingerprints for all user tables.
func (m *ManualInitializer) getTableFingerprints(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	return GetTableFingerprints(ctx, pool)
}

// GetTableFingerprints computes fingerprints for all user tables.
// Exported for use by gRPC handlers.
func GetTableFingerprints(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	fingerprints := make(map[string]string)

	query := `
		SELECT table_schema, table_name, column_name, data_type,
			   COALESCE(column_default, ''), is_nullable, ordinal_position
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY table_schema, table_name, ordinal_position
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group columns by table
	type columnInfo struct {
		Name      string
		Type      string
		Default   string
		Nullable  string
		Position  int
	}
	tableColumns := make(map[string][]columnInfo)

	for rows.Next() {
		var schema, table, colName, dataType, colDefault, isNullable string
		var position int
		if err := rows.Scan(&schema, &table, &colName, &dataType, &colDefault, &isNullable, &position); err != nil {
			return nil, err
		}
		key := schema + "." + table
		tableColumns[key] = append(tableColumns[key], columnInfo{
			Name:     colName,
			Type:     dataType,
			Default:  colDefault,
			Nullable: isNullable,
			Position: position,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute fingerprint for each table
	for key, columns := range tableColumns {
		// Sort by position to ensure consistent fingerprint
		sort.Slice(columns, func(i, j int) bool {
			return columns[i].Position < columns[j].Position
		})

		// Build fingerprint string
		var parts []string
		for _, col := range columns {
			parts = append(parts, fmt.Sprintf("%s:%s:%s:%s",
				col.Name, col.Type, col.Default, col.Nullable))
		}
		fpString := strings.Join(parts, "|")

		// Hash it
		hash := sha256.Sum256([]byte(fpString))
		fingerprints[key] = hex.EncodeToString(hash[:])
	}

	return fingerprints, nil
}

// createSubscription creates the logical replication subscription.
func (m *ManualInitializer) createSubscription(ctx context.Context, opts CompleteOptions) error {
	// Sanitize node IDs for use in PostgreSQL identifiers (no hyphens allowed in slot names)
	targetSafe := SanitizeSlotName(opts.TargetNodeID)
	sourceSafe := SanitizeSlotName(opts.SourceNodeID)

	subName := fmt.Sprintf("steep_sub_%s_from_%s", targetSafe, sourceSafe)

	// Look up the actual publication name from the source database
	pubName, err := m.lookupPublicationFromSource(ctx, opts)
	if err != nil {
		// Fall back to generated name if lookup fails
		pubName = fmt.Sprintf("steep_pub_%s", sourceSafe)
	}

	// Look up the actual slot name from init_slots table on the SOURCE database
	// (it was recorded during prepare on the source)
	slotName, err := m.lookupSlotNameFromSource(ctx, opts)
	usePreparedSlot := err == nil && slotName != ""

	// If we found a slot name, verify it actually exists
	if usePreparedSlot {
		if err := m.verifySlotExists(ctx, opts, slotName); err != nil {
			// Slot was recorded but doesn't exist anymore - fall back to auto-create
			usePreparedSlot = false
		}
	}

	// Build connection string for subscription
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
		opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

	// Get password from env for connection string
	if pw := getPasswordFromEnv(); pw != "" {
		connStr += fmt.Sprintf(" password=%s", pw)
	}

	// Create subscription with copy_data=false since data was already restored
	// and use the origin='none' to avoid issues with the restored data
	var query string
	if usePreparedSlot {
		// Use the prepared slot
		query = fmt.Sprintf(`
			CREATE SUBSCRIPTION %s
			CONNECTION '%s'
			PUBLICATION %s
			WITH (
				copy_data = false,
				create_slot = false,
				slot_name = '%s',
				origin = 'none'
			)
		`, quoteIdent(subName), connStr, quoteIdent(pubName), slotName)
	} else {
		// No prepared slot - create_slot=true creates a slot on the remote source
		query = fmt.Sprintf(`
			CREATE SUBSCRIPTION %s
			CONNECTION '%s'
			PUBLICATION %s
			WITH (
				copy_data = false,
				create_slot = true,
				origin = 'none'
			)
		`, quoteIdent(subName), connStr, quoteIdent(pubName))
	}

	_, err = m.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Mark the slot as used
	updateQuery := `
		UPDATE steep_repl.init_slots
		SET used_by_node = $1, used_at = NOW()
		WHERE node_id = $2
		AND used_by_node IS NULL
	`
	m.pool.Exec(ctx, updateQuery, opts.TargetNodeID, opts.SourceNodeID)

	return nil
}

// ensureSourceNodeRegistered registers the source node in the local nodes table if it doesn't exist.
// This ensures both nodes in a bidirectional replication setup have consistent node views.
func (m *ManualInitializer) ensureSourceNodeRegistered(ctx context.Context, opts CompleteOptions) error {
	// Check if source node already exists
	var exists bool
	err := m.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM steep_repl.nodes WHERE node_id = $1)",
		opts.SourceNodeID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if source node exists: %w", err)
	}

	if exists {
		// Update gRPC info if we have it and it's not set
		if opts.SourceRemote != "" {
			grpcHost, grpcPort := parseGRPCAddress(opts.SourceRemote)
			if grpcHost != "" && grpcPort > 0 {
				_, _ = m.pool.Exec(ctx, `
					UPDATE steep_repl.nodes
					SET grpc_host = COALESCE(grpc_host, $1), grpc_port = COALESCE(grpc_port, $2)
					WHERE node_id = $3 AND (grpc_host IS NULL OR grpc_port IS NULL)
				`, grpcHost, grpcPort, opts.SourceNodeID)
			}
		}
		return nil // Already registered
	}

	// Generate a display name from the source info
	nodeName := fmt.Sprintf("%s (%s)", opts.SourceNodeID, opts.SourceHost)

	// Parse gRPC address if provided
	var grpcHost *string
	var grpcPort *int
	if opts.SourceRemote != "" {
		host, port := parseGRPCAddress(opts.SourceRemote)
		if host != "" && port > 0 {
			grpcHost = &host
			grpcPort = &port
		}
	}

	// Insert source node with basic info and gRPC info
	_, err = m.pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, grpc_host, grpc_port, status, init_state)
		VALUES ($1, $2, $3, $4, $5, $6, 'unknown', 'synchronized')
	`, opts.SourceNodeID, nodeName, opts.SourceHost, opts.SourcePort, grpcHost, grpcPort)
	if err != nil {
		return fmt.Errorf("failed to register source node: %w", err)
	}

	return nil
}

// parseGRPCAddress parses a gRPC address like "host:port" into components.
func parseGRPCAddress(addr string) (string, int) {
	if addr == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0
	}
	return host, port
}

// monitorCatchUp monitors the catch-up phase and transitions to SYNCHRONIZED when complete.
func (m *ManualInitializer) monitorCatchUp(ctx context.Context, nodeID string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute) // Max 30 minutes to catch up
	startTime := time.Now()

	// Sanitize node ID for subscription name matching
	nodeSafe := SanitizeSlotName(nodeID)

	// Initialize progress record for catching_up phase
	m.initCatchUpProgress(ctx, nodeID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			m.manager.logger.Log(InitEvent{
				Level:  "warn",
				Event:  "init.catchup_timeout",
				NodeID: nodeID,
				Error:  "catch-up timed out after 30 minutes",
			})
			m.manager.UpdateState(ctx, nodeID, models.InitStateFailed)
			return
		case <-ticker.C:
			// For manual init with copy_data=false, check if subscription is enabled
			// and has active workers (meaning it's receiving data from the publisher)
			subPattern := fmt.Sprintf("steep_sub_%s_from_%%", nodeSafe)

			// Check if subscription exists and is enabled
			var subEnabled bool
			err := m.pool.QueryRow(ctx, `
				SELECT subenabled FROM pg_subscription WHERE subname LIKE $1
			`, subPattern).Scan(&subEnabled)

			if err != nil {
				// Subscription doesn't exist yet
				continue
			}

			if !subEnabled {
				// Subscription disabled
				continue
			}

			// Query subscription stats for progress tracking
			var receivedLsn, latestEndLsn *string
			var lagBytes int64
			err = m.pool.QueryRow(ctx, `
				SELECT
					received_lsn::text,
					latest_end_lsn::text,
					COALESCE(
						pg_wal_lsn_diff(received_lsn, latest_end_lsn),
						0
					) as lag_bytes
				FROM pg_stat_subscription
				WHERE subname LIKE $1
			`, subPattern).Scan(&receivedLsn, &latestEndLsn, &lagBytes)

			if err == nil {
				// Update progress with lag information
				elapsedSeconds := time.Since(startTime).Seconds()
				m.updateCatchUpProgress(ctx, nodeID, lagBytes, elapsedSeconds)
			}

			// For copy_data=false, once subscription is enabled and connected,
			// we're essentially synchronized (streaming changes in real-time)
			// Check that the subscription has established a connection
			var workerCount int
			err = m.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM pg_stat_subscription
				WHERE subname LIKE $1
			`, subPattern).Scan(&workerCount)

			if err == nil && workerCount > 0 {
				// Check if lag is minimal (< 1MB)
				if lagBytes < 1024*1024 {
					// Subscription is active and caught up - transition to synchronized
					if err := m.manager.UpdateState(ctx, nodeID, models.InitStateSynchronized); err != nil {
						m.manager.logger.Log(InitEvent{
							Level:  "error",
							Event:  "init.state_update_failed",
							NodeID: nodeID,
							Error:  err.Error(),
						})
						return
					}

					// Update completed time
					m.pool.Exec(ctx, `
						UPDATE steep_repl.nodes
						SET init_completed_at = NOW()
						WHERE node_id = $1
					`, nodeID)

					// Mark progress complete
					m.completeCatchUpProgress(ctx, nodeID)

					// Log completion
					duration := time.Since(startTime).Milliseconds()
					m.manager.logger.LogInitCompleted(nodeID, duration, 0, 0)
					return
				}
			}
		}
	}
}

// initCatchUpProgress initializes the progress record for the catch-up phase.
func (m *ManualInitializer) initCatchUpProgress(ctx context.Context, nodeID string) {
	query := `
		INSERT INTO steep_repl.init_progress (
			node_id, phase, overall_percent, tables_total, tables_completed,
			rows_copied, bytes_copied, throughput_rows_sec, eta_seconds, started_at, parallel_workers
		) VALUES ($1, 'catching_up', 0, 0, 0, 0, 0, 0, 0, NOW(), 1)
		ON CONFLICT (node_id) DO UPDATE SET
			phase = 'catching_up',
			overall_percent = 0,
			started_at = NOW(),
			updated_at = NOW()
	`
	_, _ = m.pool.Exec(ctx, query, nodeID)
}

// updateCatchUpProgress updates progress during catch-up phase.
func (m *ManualInitializer) updateCatchUpProgress(ctx context.Context, nodeID string, lagBytes int64, elapsedSeconds float64) {
	// For catch-up, we show progress as "lag remaining"
	// We estimate ETA based on how fast lag is decreasing
	// Since we don't have initial lag, we show the current state

	// Calculate a rough percentage (100% when lag < 1MB)
	var percent float32 = 100.0
	if lagBytes > 1024*1024 { // > 1MB
		// Show progress as inverse of lag - smaller lag = higher percentage
		// Cap at 99% until fully caught up
		percent = 99.0 - float32(min(lagBytes/(1024*1024), 99))
		if percent < 0 {
			percent = 0
		}
	}

	// ETA is hard to estimate without knowing initial lag
	// For now, show bytes remaining as a rough indicator
	etaSeconds := 0
	if lagBytes > 0 && elapsedSeconds > 0 {
		// Very rough estimate - assume we can process ~10MB/s
		etaSeconds = int(lagBytes / (10 * 1024 * 1024))
		if etaSeconds < 1 && lagBytes > 0 {
			etaSeconds = 1
		}
	}

	query := `
		UPDATE steep_repl.init_progress
		SET overall_percent = $2,
			bytes_copied = $3,
			eta_seconds = $4,
			updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = m.pool.Exec(ctx, query, nodeID, percent, lagBytes, etaSeconds)
}

// completeCatchUpProgress marks progress as complete.
func (m *ManualInitializer) completeCatchUpProgress(ctx context.Context, nodeID string) {
	query := `
		UPDATE steep_repl.init_progress
		SET phase = 'complete',
			overall_percent = 100,
			eta_seconds = 0,
			updated_at = NOW()
		WHERE node_id = $1
	`
	_, _ = m.pool.Exec(ctx, query, nodeID)
}

// quoteIdent quotes a SQL identifier.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// SanitizeSlotName converts a node ID to a valid PostgreSQL replication slot name.
// Slot names can only contain lowercase letters, numbers, and underscores.
func SanitizeSlotName(nodeID string) string {
	result := strings.ToLower(nodeID)
	// Replace any non-alphanumeric characters with underscores
	var sanitized strings.Builder
	for _, r := range result {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			sanitized.WriteRune(r)
		} else {
			sanitized.WriteRune('_')
		}
	}
	return sanitized.String()
}

// getPasswordFromEnv gets PostgreSQL password from environment.
func getPasswordFromEnv() string {
	return os.Getenv("PGPASSWORD")
}
