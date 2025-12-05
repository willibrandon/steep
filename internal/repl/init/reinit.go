package init

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/models"
)

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

// Start begins reinitialization for the specified scope.
func (r *Reinitializer) Start(ctx context.Context, opts ReinitOptions) error {
	// For full reinit, we do a simple reset to UNINITIALIZED
	// This drops the subscription and resets state so init can run again
	if opts.Scope.Full {
		return r.fullReinit(ctx, opts.NodeID)
	}

	// Table/schema-level reinit is more complex - not yet implemented
	// Update node state to REINITIALIZING
	if err := r.updateState(ctx, opts.NodeID, models.InitStateReinitializing); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Determine tables to reinitialize
	tables, err := r.resolveTables(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to resolve tables: %w", err)
	}

	// Pause replication for affected tables
	if err := r.pauseReplication(ctx, opts.NodeID, tables); err != nil {
		return fmt.Errorf("failed to pause replication: %w", err)
	}

	// Truncate and recopy tables
	for _, table := range tables {
		if err := r.reinitTable(ctx, opts, table); err != nil {
			return fmt.Errorf("failed to reinit table %s: %w", table, err)
		}
	}

	// Resume replication
	if err := r.resumeReplication(ctx, opts.NodeID, tables); err != nil {
		return fmt.Errorf("failed to resume replication: %w", err)
	}

	return nil
}

// fullReinit performs a full node reinitialization by dropping subscriptions,
// truncating replicated tables, and resetting state to UNINITIALIZED.
func (r *Reinitializer) fullReinit(ctx context.Context, nodeID string) error {
	// Find and drop any existing subscriptions for this node
	// Sanitize nodeID the same way as snapshot.go does for subscription names
	sanitizedID := sanitizeIdentifier(nodeID)
	query := `SELECT subname FROM pg_subscription WHERE subname LIKE $1`
	pattern := "steep_sub_" + sanitizedID + "_%"

	rows, err := r.pool.Query(ctx, query, pattern)
	if err != nil {
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	var dropped int
	for rows.Next() {
		var subName string
		if err := rows.Scan(&subName); err != nil {
			continue
		}
		// Drop the subscription
		dropQuery := fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", subName)
		if _, err := r.pool.Exec(ctx, dropQuery); err != nil {
			return fmt.Errorf("failed to drop subscription %s: %w", subName, err)
		}
		dropped++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating subscriptions: %w", err)
	}

	// Truncate all user tables so they can be re-copied by the next init
	// This is necessary because CREATE SUBSCRIPTION with copy_data=true will
	// try to COPY all data, which fails if the target table already has data
	if err := r.truncateUserTables(ctx); err != nil {
		return fmt.Errorf("failed to truncate user tables: %w", err)
	}

	// Clear progress record
	_, _ = r.pool.Exec(ctx, "DELETE FROM steep_repl.init_progress WHERE node_id = $1", nodeID)

	// Reset node state to UNINITIALIZED
	if err := r.updateState(ctx, nodeID, models.InitStateUninitialized); err != nil {
		return fmt.Errorf("failed to reset state: %w", err)
	}

	return nil
}

// truncateUserTables truncates all user tables (excluding system schemas).
func (r *Reinitializer) truncateUserTables(ctx context.Context) error {
	// Get list of user tables
	rows, err := r.pool.Query(ctx, `
		SELECT schemaname || '.' || tablename as full_name
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		return fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating tables: %w", err)
	}

	// Truncate each table with CASCADE to handle foreign keys
	for _, table := range tables {
		truncateQuery := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		if _, err := r.pool.Exec(ctx, truncateQuery); err != nil {
			return fmt.Errorf("failed to truncate %s: %w", table, err)
		}
	}

	return nil
}

func (r *Reinitializer) updateState(ctx context.Context, nodeID string, state models.InitState) error {
	query := `UPDATE steep_repl.nodes SET init_state = $1 WHERE node_id = $2`
	_, err := r.pool.Exec(ctx, query, string(state), nodeID)
	return err
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

// Implemented in T047 (Phase 6: User Story 4).
func (r *Reinitializer) getAllTables(ctx context.Context, nodeID string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

// Implemented in T049 (Phase 6: User Story 4).
func (r *Reinitializer) getTablesInSchema(ctx context.Context, schema string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

// Implemented in T047 (Phase 6: User Story 4).
func (r *Reinitializer) pauseReplication(ctx context.Context, nodeID string, tables []string) error {
	return fmt.Errorf("not implemented")
}

// Implemented in T047 (Phase 6: User Story 4).
func (r *Reinitializer) reinitTable(ctx context.Context, opts ReinitOptions, table string) error {
	return fmt.Errorf("not implemented")
}

// Implemented in T047 (Phase 6: User Story 4).
func (r *Reinitializer) resumeReplication(ctx context.Context, nodeID string, tables []string) error {
	return fmt.Errorf("not implemented")
}
