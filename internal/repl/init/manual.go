package init

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
func (m *ManualInitializer) Prepare(ctx context.Context, nodeID, slotName string, expiresDuration time.Duration) (*PrepareResult, error) {
	// Create logical replication slot
	var lsn string
	query := `SELECT lsn FROM pg_create_logical_replication_slot($1, 'pgoutput')`
	if err := m.pool.QueryRow(ctx, query, slotName).Scan(&lsn); err != nil {
		return nil, fmt.Errorf("failed to create replication slot: %w", err)
	}

	// Record slot in init_slots table
	now := time.Now()
	expiresAt := now.Add(expiresDuration)
	insertQuery := `
		INSERT INTO steep_repl.init_slots (slot_name, node_id, lsn, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := m.pool.Exec(ctx, insertQuery, slotName, nodeID, lsn, now, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to record slot: %w", err)
	}

	return &PrepareResult{
		SlotName:  slotName,
		LSN:       lsn,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

// CompleteOptions configures the complete operation.
type CompleteOptions struct {
	TargetNodeID   string
	SourceNodeID   string
	SourceLSN      string
	SchemaSyncMode string
	SkipSchemaCheck bool
}

// Complete finishes manual initialization after user has restored their backup.
func (m *ManualInitializer) Complete(ctx context.Context, opts CompleteOptions) error {
	// Verify schema matches (unless skipped)
	if !opts.SkipSchemaCheck {
		if err := m.verifySchema(ctx, opts.TargetNodeID, opts.SourceNodeID, opts.SchemaSyncMode); err != nil {
			return fmt.Errorf("schema verification failed: %w", err)
		}
	}

	// Create subscription with copy_data=false
	if err := m.createSubscription(ctx, opts); err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	return nil
}

// Implemented in T033 (Phase 4: User Story 2).
func (m *ManualInitializer) verifySchema(ctx context.Context, targetNode, sourceNode, syncMode string) error {
	return fmt.Errorf("not implemented")
}

// Implemented in T033 (Phase 4: User Story 2).
func (m *ManualInitializer) createSubscription(ctx context.Context, opts CompleteOptions) error {
	return fmt.Errorf("not implemented")
}
