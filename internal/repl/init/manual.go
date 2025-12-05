package init

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
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
	// First check if slot already exists
	var existing int
	err := m.pool.QueryRow(ctx, `
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
	SourceHost      string // Required for subscription connection
	SourcePort      int
	SourceDatabase  string
	SourceUser      string
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
	// Update state to PREPARING
	if err := m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStatePreparing); err != nil {
		return fmt.Errorf("failed to update state to preparing: %w", err)
	}

	// Verify schema matches (unless skipped)
	if !opts.SkipSchemaCheck {
		diffs, err := m.verifySchema(ctx, opts)
		if err != nil {
			m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStateFailed)
			return fmt.Errorf("schema verification failed: %w", err)
		}
		if len(diffs) > 0 && opts.SchemaSyncMode == config.SchemaSyncStrict {
			m.manager.UpdateState(ctx, opts.TargetNodeID, models.InitStateFailed)
			return fmt.Errorf("schema mismatch detected: %d differences found (use --schema-sync=manual to proceed anyway)", len(diffs))
		}
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

// verifySchema compares schema between local and source tables.
// It uses fingerprinting to detect differences.
func (m *ManualInitializer) verifySchema(ctx context.Context, opts CompleteOptions) ([]SchemaDifference, error) {
	var diffs []SchemaDifference

	// Get local tables
	localTables, err := m.getTableFingerprints(ctx, m.pool)
	if err != nil {
		return nil, fmt.Errorf("failed to get local table fingerprints: %w", err)
	}

	// Connect to source to get remote tables
	// Build connection string using source info from opts
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable",
		opts.SourceHost, opts.SourcePort, opts.SourceDatabase, opts.SourceUser)

	// Get password from environment
	sourcePool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source for schema comparison: %w", err)
	}
	defer sourcePool.Close()

	remoteTables, err := m.getTableFingerprints(ctx, sourcePool)
	if err != nil {
		return nil, fmt.Errorf("failed to get source table fingerprints: %w", err)
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

// getTableFingerprints computes fingerprints for all user tables.
func (m *ManualInitializer) getTableFingerprints(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
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
	targetSafe := sanitizeSlotName(opts.TargetNodeID)
	sourceSafe := sanitizeSlotName(opts.SourceNodeID)

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
	if err != nil {
		// Fall back to generated name if lookup fails
		slotName = fmt.Sprintf("steep_init_%s", sourceSafe)
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
	query := fmt.Sprintf(`
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

// monitorCatchUp monitors the catch-up phase and transitions to SYNCHRONIZED when complete.
func (m *ManualInitializer) monitorCatchUp(ctx context.Context, nodeID string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute) // Max 30 minutes to catch up

	// Sanitize node ID for subscription name matching
	nodeSafe := sanitizeSlotName(nodeID)

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

			// For copy_data=false, once subscription is enabled and connected,
			// we're essentially synchronized (streaming changes in real-time)
			// Check that the subscription has established a connection
			var workerCount int
			err = m.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM pg_stat_subscription
				WHERE subname LIKE $1
			`, subPattern).Scan(&workerCount)

			if err == nil && workerCount > 0 {
				// Subscription is active - transition to synchronized
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

				// Log completion with 0s for metrics (manual init doesn't track these)
				m.manager.logger.LogInitCompleted(nodeID, 0, 0, 0)
				return
			}
		}
	}
}

// quoteIdent quotes a SQL identifier.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// sanitizeSlotName converts a node ID to a valid PostgreSQL replication slot name.
// Slot names can only contain lowercase letters, numbers, and underscores.
func sanitizeSlotName(nodeID string) string {
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
