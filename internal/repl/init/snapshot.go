package init

import (
	"context"
	"fmt"

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

// Start begins automatic snapshot initialization from source to target node.
func (s *SnapshotInitializer) Start(ctx context.Context, targetNode, sourceNode string, opts SnapshotOptions) error {
	// Validate nodes exist and are in correct state
	if err := s.validateNodes(ctx, targetNode, sourceNode); err != nil {
		return fmt.Errorf("node validation failed: %w", err)
	}

	// Update target node state to PREPARING
	if err := s.updateState(ctx, targetNode, models.InitStatePreparing); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Create subscription with copy_data=true
	// This is implemented in the actual initialization logic
	return nil
}

// SnapshotOptions configures snapshot initialization behavior.
type SnapshotOptions struct {
	ParallelWorkers     int
	LargeTableThreshold int64
	LargeTableMethod    string
	Force               bool
}

// Implemented in T020 (Phase 3: User Story 1).
func (s *SnapshotInitializer) validateNodes(ctx context.Context, targetNode, sourceNode string) error {
	return fmt.Errorf("not implemented")
}

func (s *SnapshotInitializer) updateState(ctx context.Context, nodeID string, state models.InitState) error {
	query := `UPDATE steep_repl.nodes SET init_state = $1 WHERE node_id = $2`
	_, err := s.pool.Exec(ctx, query, string(state), nodeID)
	return err
}
