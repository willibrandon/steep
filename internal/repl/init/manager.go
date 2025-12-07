// Package init provides node initialization and snapshot management for bidirectional replication.
package init

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/models"
)

// Manager orchestrates node initialization operations.
type Manager struct {
	pool        *pgxpool.Pool
	config      *config.InitConfig
	pgConfig    *config.PostgreSQLConfig // PostgreSQL connection config for replication
	logger      *Logger
	auditWriter AuditWriter // Interface for audit logging

	mu       sync.RWMutex
	active   map[string]*Operation // node_id -> active operation
	progress chan ProgressUpdate

	// Sub-initializers for different methods
	snapshot      *SnapshotInitializer
	manual        *ManualInitializer
	reinit        *Reinitializer
	bidirectional *BidirectionalMergeInitializer
}

// AuditWriter is the interface for writing audit log entries.
type AuditWriter interface {
	LogInitCancelled(ctx context.Context, targetNodeID string) error
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
func NewManager(pool *pgxpool.Pool, cfg *config.InitConfig, pgCfg *config.PostgreSQLConfig, auditWriter AuditWriter, slogger *slog.Logger) *Manager {
	m := &Manager{
		pool:        pool,
		config:      cfg,
		pgConfig:    pgCfg,
		auditWriter: auditWriter,
		logger:      NewLogger(slogger),
		active:      make(map[string]*Operation),
		progress:    make(chan ProgressUpdate, 100),
	}

	// Initialize sub-initializers
	m.snapshot = NewSnapshotInitializer(pool, m)
	m.manual = NewManualInitializer(pool, m)
	m.reinit = NewReinitializer(pool, m)
	m.bidirectional = NewBidirectionalMergeInitializer(pool, m)

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
	// Use a background context so the operation survives the RPC returning
	opCtx, cancel := context.WithCancel(context.Background())
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
			ParallelWorkers:     req.ParallelWorkers,
			LargeTableThreshold: m.parseSizeThreshold(m.config.LargeTableThreshold),
			LargeTableMethod:    m.config.LargeTableMethod,
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

	// Write to audit log
	if m.auditWriter != nil {
		if err := m.auditWriter.LogInitCancelled(ctx, nodeID); err != nil {
			// Log error but don't fail the cancel operation
			m.logger.Log(InitEvent{
				Level:  "warn",
				Event:  "init.audit_write_failed",
				NodeID: nodeID,
				Error:  err.Error(),
			})
		}
	}

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

// GetTableFingerprints returns schema fingerprints for all user tables.
// Used by gRPC handlers for remote schema verification.
func (m *Manager) GetTableFingerprints(ctx context.Context) (map[string]string, error) {
	return GetTableFingerprints(ctx, m.pool)
}

// GetTableFingerprintsWithDefs returns fingerprints with column definitions for all user tables.
func (m *Manager) GetTableFingerprintsWithDefs(ctx context.Context) (map[string]TableFingerprintInfo, error) {
	return GetTableFingerprintsWithDefs(ctx, m.pool)
}

// GetNodeState returns the current init_state for a node.
func (m *Manager) GetNodeState(ctx context.Context, nodeID string) (models.InitState, error) {
	var state models.InitState
	err := m.pool.QueryRow(ctx,
		"SELECT init_state FROM steep_repl.nodes WHERE node_id = $1",
		nodeID,
	).Scan(&state)
	if err != nil {
		return "", fmt.Errorf("failed to get node state: %w", err)
	}
	return state, nil
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

// StartReinit starts reinitialization for a node.
func (m *Manager) StartReinit(ctx context.Context, opts ReinitOptions) (*ReinitResult, error) {
	return m.reinit.Start(ctx, opts)
}

// PrepareInit prepares for manual initialization by creating a replication slot.
// This should be called on the SOURCE node.
func (m *Manager) PrepareInit(ctx context.Context, nodeID, slotName string, expiresDuration time.Duration) (*PrepareResult, error) {
	return m.manual.Prepare(ctx, nodeID, slotName, expiresDuration)
}

// CompleteInit finishes manual initialization after user has restored backup.
// This should be called on the TARGET node.
func (m *Manager) CompleteInit(ctx context.Context, opts CompleteOptions) error {
	return m.manual.Complete(ctx, opts)
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

// SourceNodeInfo contains connection info for registering a source node.
type SourceNodeInfo struct {
	Host     string
	Port     int
	Database string
	User     string
}

// RegisterSourceNode registers the source node in steep_repl.nodes if not already present.
// This allows the target to know how to connect to the source for replication.
func (m *Manager) RegisterSourceNode(ctx context.Context, nodeID string, info SourceNodeInfo) error {
	query := `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status, init_state, last_seen)
		VALUES ($1, $2, $3, $4, 100, 'healthy', 'synchronized', NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			init_state = 'synchronized',
			last_seen = NOW()
	`

	// Use nodeID as node_name if not provided separately
	nodeName := nodeID
	if info.Host != "" {
		nodeName = fmt.Sprintf("%s (%s)", nodeID, info.Host)
	}

	_, err := m.pool.Exec(ctx, query,
		nodeID,
		nodeName,
		info.Host,
		info.Port,
	)
	if err != nil {
		return fmt.Errorf("failed to register source node: %w", err)
	}

	m.logger.Log(InitEvent{
		Event:  "init.source_registered",
		NodeID: nodeID,
		Details: map[string]any{
			"host": info.Host,
			"port": info.Port,
		},
	})

	return nil
}

// CompareSchemas compares schema fingerprints between the local node and a remote node.
// It retrieves the remote fingerprints from the nodes table or via direct query,
// then compares them against the local database schema.
func (m *Manager) CompareSchemas(ctx context.Context, localNodeID, remoteNodeID string, schemas []string) (*CompareResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create schema comparator
	comparator := NewSchemaComparator(m.pool)

	// Use the Compare method which handles both local and remote comparison
	return comparator.Compare(ctx, localNodeID, remoteNodeID, schemas)
}

// CompareWithRemoteFingerprints compares local schema against remotely-provided fingerprints.
// This is used when fingerprints are retrieved via gRPC GetSchemaFingerprints.
func (m *Manager) CompareWithRemoteFingerprints(ctx context.Context, localNodeID string, remoteFingerprints map[string]string) (*CompareResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	comparator := NewSchemaComparator(m.pool)
	return comparator.CompareWithRemote(ctx, localNodeID, remoteFingerprints)
}

// GetColumnDiff returns detailed column differences for a mismatched table.
func (m *Manager) GetColumnDiff(ctx context.Context, peerNodeID, tableSchema, tableName string) ([]ColumnDifference, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	comparator := NewSchemaComparator(m.pool)
	return comparator.GetDiff(ctx, peerNodeID, tableSchema, tableName)
}

// CaptureFingerprints captures fingerprints for all tables in the specified schemas.
func (m *Manager) CaptureFingerprints(ctx context.Context, nodeID string, schemas []string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	comparator := NewSchemaComparator(m.pool)
	return comparator.CaptureFingerprints(ctx, nodeID, schemas)
}

// parseSizeThreshold parses a human-readable size string (e.g., "10GB", "500MB")
// and returns the size in bytes. Returns 0 if empty or invalid.
func (m *Manager) parseSizeThreshold(s string) int64 {
	if s == "" {
		return 0
	}

	s = strings.TrimSpace(strings.ToUpper(s))

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "TB"):
		multiplier = 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		numStr = strings.TrimSuffix(s, "B")
	default:
		numStr = s
	}

	numStr = strings.TrimSpace(numStr)
	var num float64
	if _, err := fmt.Sscanf(numStr, "%f", &num); err != nil {
		return 0
	}

	return int64(num * float64(multiplier))
}
