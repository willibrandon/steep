// Package init provides node initialization and snapshot management for bidirectional replication.
package init

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/models"
)

// Manager orchestrates node initialization operations.
type Manager struct {
	pool   *pgxpool.Pool
	config *config.InitConfig
	logger *Logger

	mu       sync.RWMutex
	active   map[string]*Operation // node_id -> active operation
	progress chan ProgressUpdate

	// Sub-initializers for different methods
	snapshot *SnapshotInitializer
	manual   *ManualInitializer
	reinit   *Reinitializer
}

// Operation represents an in-progress initialization operation.
type Operation struct {
	NodeID     string
	SourceNode string
	Method     config.InitMethod
	StartedAt  time.Time
	Cancel     context.CancelFunc
}

// ProgressUpdate represents a progress update for streaming to TUI.
type ProgressUpdate struct {
	NodeID           string
	Phase            string
	OverallPercent   float32
	TablesTotal      int
	TablesCompleted  int
	CurrentTable     string
	CurrentPercent   float32
	RowsCopied       int64
	BytesCopied      int64
	ThroughputRows   float32
	ETASeconds       int
	ParallelWorkers  int
	Error            string
}

// StartInitRequest contains parameters for starting initialization.
type StartInitRequest struct {
	TargetNodeID    string
	SourceNodeID    string
	Method          config.InitMethod
	ParallelWorkers int
	SchemaSync      config.SchemaSyncMode
}

// NewManager creates a new initialization manager.
func NewManager(pool *pgxpool.Pool, cfg *config.InitConfig, slogger *slog.Logger) *Manager {
	m := &Manager{
		pool:     pool,
		config:   cfg,
		logger:   NewLogger(slogger),
		active:   make(map[string]*Operation),
		progress: make(chan ProgressUpdate, 100),
	}

	// Initialize sub-initializers
	m.snapshot = NewSnapshotInitializer(pool, m)
	m.manual = NewManualInitializer(pool, m)
	m.reinit = NewReinitializer(pool, m)

	return m
}

// Progress returns the channel for receiving progress updates.
func (m *Manager) Progress() <-chan ProgressUpdate {
	return m.progress
}

// IsActive returns true if an initialization is in progress for the given node.
func (m *Manager) IsActive(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.active[nodeID]
	return ok
}

// GetOperation returns the active operation for a node, if any.
func (m *Manager) GetOperation(nodeID string) (*Operation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	op, ok := m.active[nodeID]
	return op, ok
}

// StartInit begins initialization for a target node from a source node.
// Implemented in T020 (Phase 3: User Story 1).
func (m *Manager) StartInit(ctx context.Context, req StartInitRequest) error {
	if m.IsActive(req.TargetNodeID) {
		return fmt.Errorf("initialization already in progress for node %s", req.TargetNodeID)
	}

	// Log the start
	m.logger.LogInitStarted(req.TargetNodeID, req.SourceNodeID, string(req.Method))

	// Register active operation
	opCtx, cancel := context.WithCancel(ctx)
	op := &Operation{
		NodeID:     req.TargetNodeID,
		SourceNode: req.SourceNodeID,
		Method:     req.Method,
		StartedAt:  time.Now(),
		Cancel:     cancel,
	}

	m.mu.Lock()
	m.active[req.TargetNodeID] = op
	m.mu.Unlock()

	// Dispatch to appropriate initializer based on method
	var err error
	switch req.Method {
	case config.InitMethodSnapshot:
		opts := SnapshotOptions{
			ParallelWorkers: req.ParallelWorkers,
		}
		if opts.ParallelWorkers <= 0 {
			opts.ParallelWorkers = 4
		}
		err = m.snapshot.Start(opCtx, req.TargetNodeID, req.SourceNodeID, opts)
	case config.InitMethodManual:
		// Manual init uses Prepare/Complete flow, not Start
		err = fmt.Errorf("manual initialization requires prepare/complete workflow")
	case config.InitMethodTwoPhase, config.InitMethodDirect:
		err = fmt.Errorf("not implemented: method %s", req.Method)
	default:
		err = fmt.Errorf("unknown initialization method: %s", req.Method)
	}

	if err != nil {
		m.unregisterOperation(req.TargetNodeID)
		m.logger.LogInitFailed(req.TargetNodeID, err)
		return err
	}

	return nil
}

// CancelInit cancels an in-progress initialization.
// Implemented in T023 (Phase 3: User Story 1).
func (m *Manager) CancelInit(ctx context.Context, nodeID string) error {
	op, ok := m.GetOperation(nodeID)
	if !ok {
		return fmt.Errorf("no active initialization for node %s", nodeID)
	}

	// Cancel the context
	op.Cancel()

	// Cleanup will be handled by the goroutine that's running the init
	// Log the cancellation
	m.logger.Log(InitEvent{
		Event:  EventInitCancelled,
		NodeID: nodeID,
	})

	return nil
}

// GetProgress returns the current progress for a node.
// Implemented in T037 (Phase 5: User Story 3).
func (m *Manager) GetProgress(ctx context.Context, nodeID string) (*models.InitProgress, error) {
	query := `
		SELECT node_id, phase, overall_percent, tables_total, tables_completed,
		       current_table, current_table_percent, rows_copied, bytes_copied,
		       throughput_rows_sec, started_at, eta_seconds, updated_at,
		       parallel_workers, error_message
		FROM steep_repl.init_progress
		WHERE node_id = $1
	`

	var p models.InitProgress
	var currentTable, errorMessage *string
	var etaSeconds *int

	err := m.pool.QueryRow(ctx, query, nodeID).Scan(
		&p.NodeID, &p.Phase, &p.OverallPercent, &p.TablesTotal, &p.TablesCompleted,
		&currentTable, &p.CurrentTablePercent, &p.RowsCopied, &p.BytesCopied,
		&p.ThroughputRowsSec, &p.StartedAt, &etaSeconds, &p.UpdatedAt,
		&p.ParallelWorkers, &errorMessage,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get progress: %w", err)
	}

	p.CurrentTable = currentTable
	p.ETASeconds = etaSeconds
	p.ErrorMessage = errorMessage

	return &p, nil
}

// UpdateState updates the initialization state for a node.
func (m *Manager) UpdateState(ctx context.Context, nodeID string, newState models.InitState) error {
	// Get current state first
	var currentState models.InitState
	err := m.pool.QueryRow(ctx,
		"SELECT init_state FROM steep_repl.nodes WHERE node_id = $1",
		nodeID,
	).Scan(&currentState)
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}

	// Validate transition
	if err := currentState.ValidateTransition(newState); err != nil {
		return err
	}

	// Update state
	query := `
		UPDATE steep_repl.nodes
		SET init_state = $1
		WHERE node_id = $2
	`
	_, err = m.pool.Exec(ctx, query, string(newState), nodeID)
	if err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Log state change
	m.logger.LogStateChange(nodeID, currentState, newState)

	return nil
}

// unregisterOperation removes an operation from the active map.
func (m *Manager) unregisterOperation(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, nodeID)
}

// sendProgress sends a progress update to subscribers.
func (m *Manager) sendProgress(update ProgressUpdate) {
	select {
	case m.progress <- update:
	default:
		// Channel full, skip update
	}
}

// Close shuts down the manager and cancels all active operations.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel all active operations
	for _, op := range m.active {
		op.Cancel()
	}
	m.active = make(map[string]*Operation)

	close(m.progress)
	return nil
}
