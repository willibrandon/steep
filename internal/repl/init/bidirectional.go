package init

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
)

// BidirectionalMergeInitializer handles initialization by merging existing data
// on both nodes before enabling bidirectional replication.
type BidirectionalMergeInitializer struct {
	pool    *pgxpool.Pool
	manager *Manager
}

// NewBidirectionalMergeInitializer creates a new bidirectional merge initializer.
func NewBidirectionalMergeInitializer(pool *pgxpool.Pool, manager *Manager) *BidirectionalMergeInitializer {
	return &BidirectionalMergeInitializer{
		pool:    pool,
		manager: manager,
	}
}

// BidirectionalMergeRequest contains parameters for bidirectional merge initialization.
type BidirectionalMergeRequest struct {
	NodeAID          string           // Local node ID
	NodeBID          string           // Remote node ID
	NodeBConnStr     string           // Connection string for remote node
	Tables           []string         // Tables to merge (schema.table format)
	Strategy         ConflictStrategy // Conflict resolution strategy
	RemoteServerName string           // Name for postgres_fdw foreign server
	QuiesceTimeoutMs int              // Timeout for quiescing writes
	DryRun           bool             // Preview without applying changes
	SchemaSync       config.SchemaSyncMode
}

// BidirectionalMergeResult contains the result of bidirectional merge initialization.
type BidirectionalMergeResult struct {
	MergeResult      *MergeResult
	ReplicationSetup bool
	SlotAToB         string
	SlotBToA         string
	Error            error
}

// Initialize performs bidirectional merge and sets up replication.
// This is the main entry point for the bidirectional-merge init mode.
func (b *BidirectionalMergeInitializer) Initialize(ctx context.Context, req BidirectionalMergeRequest) (*BidirectionalMergeResult, error) {
	result := &BidirectionalMergeResult{}

	b.manager.logger.LogPhaseStarted(req.NodeAID, "bidirectional_merge")

	// Update node state
	if err := b.updateNodeState(ctx, req.NodeAID, "initializing", "bidirectional_merge", req.NodeBID); err != nil {
		return nil, fmt.Errorf("update node state: %w", err)
	}

	// Send initial progress
	b.manager.sendProgress(ProgressUpdate{
		NodeID:         req.NodeAID,
		Phase:          "connecting",
		OverallPercent: 5,
	})

	// Connect to remote node
	remotePool, err := pgxpool.New(ctx, req.NodeBConnStr)
	if err != nil {
		return nil, fmt.Errorf("connect to node B (%s): %w", req.NodeBID, err)
	}
	defer remotePool.Close()

	// Create merger
	merger := NewMerger(b.pool, remotePool, b.manager)

	// Build table info
	tables, err := b.buildTableInfo(ctx, req.Tables)
	if err != nil {
		return nil, fmt.Errorf("build table info: %w", err)
	}

	b.manager.sendProgress(ProgressUpdate{
		NodeID:         req.NodeAID,
		Phase:          "preflight",
		OverallPercent: 10,
	})

	// Run preflight checks
	preflight, err := merger.RunPreflightChecks(ctx, tables)
	if err != nil {
		return nil, fmt.Errorf("preflight checks: %w", err)
	}

	if len(preflight.Errors) > 0 {
		return nil, fmt.Errorf("preflight failed: %v", preflight.Errors)
	}

	// Validate strategy requirements
	if req.Strategy == StrategyLastModified && !preflight.TrackCommitTimestamp {
		return nil, fmt.Errorf("last-modified strategy requires track_commit_timestamp=on")
	}

	b.manager.sendProgress(ProgressUpdate{
		NodeID:         req.NodeAID,
		Phase:          "merging",
		OverallPercent: 20,
		TablesTotal:    len(tables),
	})

	// Execute merge
	mergeConfig := MergeConfig{
		Tables:           tables,
		Strategy:         req.Strategy,
		RemoteServer:     req.RemoteServerName,
		QuiesceTimeoutMs: req.QuiesceTimeoutMs,
		DryRun:           req.DryRun,
	}

	mergeResult, err := merger.ExecuteMerge(ctx, mergeConfig)
	if err != nil {
		return nil, fmt.Errorf("execute merge: %w", err)
	}
	result.MergeResult = mergeResult

	if req.DryRun {
		b.manager.logger.LogPhaseCompleted(req.NodeAID, "bidirectional_merge_dryrun", 0)
		return result, nil
	}

	b.manager.sendProgress(ProgressUpdate{
		NodeID:         req.NodeAID,
		Phase:          "replication_setup",
		OverallPercent: 80,
	})

	// Set up bidirectional replication with origin=none
	if err := b.setupReplication(ctx, req, remotePool); err != nil {
		result.Error = fmt.Errorf("setup replication: %w", err)
		// Don't fail completely - merge succeeded
		b.manager.logger.LogInitFailed(req.NodeAID, err)
	} else {
		result.ReplicationSetup = true
		result.SlotAToB = fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(req.NodeBID), sanitizeIdentifier(req.NodeAID))
		result.SlotBToA = fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(req.NodeAID), sanitizeIdentifier(req.NodeBID))
	}

	// Update node state to ready
	if err := b.updateNodeState(ctx, req.NodeAID, "ready", "", ""); err != nil {
		b.manager.logger.LogInitFailed(req.NodeAID, err)
	}

	b.manager.sendProgress(ProgressUpdate{
		NodeID:         req.NodeAID,
		Phase:          "complete",
		OverallPercent: 100,
	})

	b.manager.logger.LogPhaseCompleted(req.NodeAID, "bidirectional_merge", 0)

	return result, nil
}

// buildTableInfo builds MergeTableInfo from table names.
func (b *BidirectionalMergeInitializer) buildTableInfo(ctx context.Context, tableNames []string) ([]MergeTableInfo, error) {
	var tables []MergeTableInfo

	for _, name := range tableNames {
		schema := "public"
		table := name

		// Parse schema.table format
		if idx := indexByte(name, '.'); idx != -1 {
			schema = name[:idx]
			table = name[idx+1:]
		}

		// Get primary key columns
		pkColumns, err := b.getPrimaryKeyColumns(ctx, schema, table)
		if err != nil {
			return nil, fmt.Errorf("get PK for %s.%s: %w", schema, table, err)
		}

		if len(pkColumns) == 0 {
			return nil, fmt.Errorf("table %s.%s has no primary key", schema, table)
		}

		tables = append(tables, MergeTableInfo{
			Schema:    schema,
			Name:      table,
			PKColumns: pkColumns,
		})
	}

	return tables, nil
}

// getPrimaryKeyColumns returns the primary key column names for a table.
func (b *BidirectionalMergeInitializer) getPrimaryKeyColumns(ctx context.Context, schema, table string) ([]string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE i.indisprimary
		AND n.nspname = $1
		AND c.relname = $2
		ORDER BY array_position(i.indkey, a.attnum)
	`

	rows, err := b.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// setupReplication sets up bidirectional replication with origin=none.
func (b *BidirectionalMergeInitializer) setupReplication(ctx context.Context, req BidirectionalMergeRequest, remotePool *pgxpool.Pool) error {
	// Create publication on node A (if not exists)
	pubNameA := fmt.Sprintf("steep_pub_%s", sanitizeIdentifier(req.NodeAID))
	_, err := b.pool.Exec(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = '%s') THEN
				CREATE PUBLICATION %s FOR ALL TABLES;
			END IF;
		END $$
	`, pubNameA, pubNameA))
	if err != nil {
		return fmt.Errorf("create publication on node A: %w", err)
	}

	// Create publication on node B (if not exists)
	pubNameB := fmt.Sprintf("steep_pub_%s", sanitizeIdentifier(req.NodeBID))
	_, err = remotePool.Exec(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = '%s') THEN
				CREATE PUBLICATION %s FOR ALL TABLES;
			END IF;
		END $$
	`, pubNameB, pubNameB))
	if err != nil {
		return fmt.Errorf("create publication on node B: %w", err)
	}

	// Create subscription on node A (subscribing to B) with copy_data=false, origin=none
	subNameAFromB := fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(req.NodeAID), sanitizeIdentifier(req.NodeBID))
	_, err = b.pool.Exec(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_subscription WHERE subname = '%s') THEN
				CREATE SUBSCRIPTION %s
				CONNECTION '%s'
				PUBLICATION %s
				WITH (copy_data = false, origin = none);
			END IF;
		END $$
	`, subNameAFromB, subNameAFromB, req.NodeBConnStr, pubNameB))
	if err != nil {
		return fmt.Errorf("create subscription on node A: %w", err)
	}

	// Create subscription on node B (subscribing to A) with copy_data=false, origin=none
	// Need connection string for node A - use the local pool's config
	nodeAConnStr := b.getLocalConnectionString()
	if nodeAConnStr == "" {
		return fmt.Errorf("cannot determine local connection string for node A")
	}

	subNameBFromA := fmt.Sprintf("steep_sub_%s_from_%s", sanitizeIdentifier(req.NodeBID), sanitizeIdentifier(req.NodeAID))
	_, err = remotePool.Exec(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_subscription WHERE subname = '%s') THEN
				CREATE SUBSCRIPTION %s
				CONNECTION '%s'
				PUBLICATION %s
				WITH (copy_data = false, origin = none);
			END IF;
		END $$
	`, subNameBFromA, subNameBFromA, nodeAConnStr, pubNameA))
	if err != nil {
		return fmt.Errorf("create subscription on node B: %w", err)
	}

	return nil
}

// getLocalConnectionString returns the connection string for the local node.
func (b *BidirectionalMergeInitializer) getLocalConnectionString() string {
	if b.manager.pgConfig == nil {
		return ""
	}
	// Note: Password is obtained via PasswordCommand, not stored directly
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=%s",
		b.manager.pgConfig.Host,
		b.manager.pgConfig.Port,
		b.manager.pgConfig.Database,
		b.manager.pgConfig.User,
		b.manager.pgConfig.SSLMode,
	)
}

// updateNodeState updates the node's initialization state.
func (b *BidirectionalMergeInitializer) updateNodeState(ctx context.Context, nodeID, status, initState, sourceNode string) error {
	query := `
		UPDATE steep_repl.nodes
		SET status = $2,
		    init_state = $3,
		    init_source_node = NULLIF($4, ''),
		    init_started_at = CASE WHEN $3 != '' THEN now() ELSE init_started_at END
		WHERE node_id = $1
	`
	_, err := b.pool.Exec(ctx, query, nodeID, status, initState, sourceNode)
	return err
}

// indexByte returns the index of the first instance of c in s, or -1 if c is not present.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Bidirectional returns the bidirectional merge initializer.
func (m *Manager) Bidirectional() *BidirectionalMergeInitializer {
	return m.bidirectional
}
