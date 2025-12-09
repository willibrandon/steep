// Package direct provides direct PostgreSQL execution for steep-repl CLI.
//
// This package enables CLI commands to execute operations directly through
// PostgreSQL using the steep_repl extension, without requiring the daemon.
// It provides high-level operation abstractions built on top of the
// internal/repl/direct client package.
//
// T022: Create cmd/steep-repl/direct/executor.go with direct PostgreSQL execution logic
package direct

import (
	"context"
	"fmt"
	"time"

	"github.com/willibrandon/steep/internal/repl/direct"
)

// Executor provides direct PostgreSQL execution for CLI commands.
// It wraps the internal/repl/direct.Client with higher-level operation methods
// suitable for CLI commands.
type Executor struct {
	client *direct.Client

	// Configuration
	showProgress bool
	timeout      time.Duration
}

// ExecutorConfig configures the Executor.
type ExecutorConfig struct {
	// ConnString is a PostgreSQL connection string (DSN or URL format).
	// If provided, it takes precedence over environment variables.
	ConnString string

	// ShowProgress enables progress output during operations.
	ShowProgress bool

	// Timeout for operations (default: 24 hours for long operations).
	Timeout time.Duration
}

// NewExecutor creates a new direct execution executor.
func NewExecutor(ctx context.Context, cfg ExecutorConfig) (*Executor, error) {
	// Set defaults
	if cfg.Timeout == 0 {
		cfg.Timeout = 24 * time.Hour
	}

	// Create client based on connection string or environment
	var client *direct.Client
	var err error

	if cfg.ConnString != "" {
		client, err = direct.NewClientFromConnString(ctx, cfg.ConnString)
	} else {
		client, err = direct.NewClientFromEnv(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create direct client: %w", err)
	}

	return &Executor{
		client:       client,
		showProgress: cfg.ShowProgress,
		timeout:      cfg.Timeout,
	}, nil
}

// Close closes the executor's database connection.
func (e *Executor) Close() {
	if e.client != nil {
		e.client.Close()
	}
}

// Client returns the underlying direct client for advanced operations.
func (e *Executor) Client() *direct.Client {
	return e.client
}

// =============================================================================
// Health and Status Operations
// =============================================================================

// Health retrieves the health status from the extension.
func (e *Executor) Health(ctx context.Context) (*direct.HealthResult, error) {
	return e.client.Health(ctx)
}

// ExtensionVersion returns the installed extension version.
func (e *Executor) ExtensionVersion() string {
	return e.client.ExtensionVersion()
}

// PostgresVersion returns the PostgreSQL version string.
func (e *Executor) PostgresVersion() string {
	return e.client.VersionString()
}

// BackgroundWorkerActive returns true if the background worker is running.
func (e *Executor) BackgroundWorkerActive() bool {
	return e.client.BackgroundWorkerActive()
}

// =============================================================================
// Snapshot Operations
// =============================================================================

// SnapshotGenerateOptions configures snapshot generation.
type SnapshotGenerateOptions struct {
	OutputPath  string
	Compression string // none, gzip, lz4, zstd
	Parallel    int
}

// SnapshotGenerateResult contains the result of snapshot generation.
type SnapshotGenerateResult struct {
	SnapshotID string
	OutputPath string
	Duration   time.Duration
	Error      string
}

// GenerateSnapshot starts a snapshot generation operation.
// If the background worker is available, it queues the operation and returns immediately.
// Progress can be monitored using SnapshotProgress or WaitForSnapshot.
func (e *Executor) GenerateSnapshot(ctx context.Context, opts SnapshotGenerateOptions) (string, error) {
	// Set defaults
	if opts.Compression == "" {
		opts.Compression = "none"
	}
	if opts.Parallel <= 0 {
		opts.Parallel = 4
	}

	// Call the extension function to start the snapshot
	snapshotID, err := e.client.StartSnapshot(ctx, opts.OutputPath, opts.Compression, opts.Parallel)
	if err != nil {
		return "", fmt.Errorf("failed to start snapshot: %w", err)
	}

	return snapshotID, nil
}

// SnapshotProgress retrieves the current progress of a snapshot operation.
func (e *Executor) SnapshotProgress(ctx context.Context, snapshotID string) (*direct.ProgressState, error) {
	// Query snapshot_progress SQL function
	var state direct.ProgressState

	err := e.client.QueryRow(ctx, `
		SELECT
			COALESCE(snapshot_id, ''),
			COALESCE(phase, 'unknown'),
			COALESCE(overall_percent, 0),
			COALESCE(tables_completed, 0),
			COALESCE(tables_total, 0),
			COALESCE(current_table, ''),
			COALESCE(bytes_processed, 0),
			COALESCE(eta_seconds, 0),
			COALESCE(error, '')
		FROM steep_repl.snapshot_progress($1)
	`, snapshotID).Scan(
		&state.OperationID,
		&state.Phase,
		&state.Percent,
		&state.TablesCompleted,
		&state.TablesTotal,
		&state.CurrentTable,
		&state.BytesProcessed,
		&state.ETASeconds,
		&state.Error,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot progress: %w", err)
	}

	state.OperationType = "snapshot_generate"
	state.IsComplete = state.Phase == "complete"
	state.IsFailed = state.Phase == "failed" || state.Error != ""
	state.UpdatedAt = time.Now()

	return &state, nil
}

// CancelSnapshot cancels a running snapshot operation.
func (e *Executor) CancelSnapshot(ctx context.Context, snapshotID string) error {
	return e.client.CancelSnapshot(ctx, snapshotID)
}

// WaitForSnapshot waits for a snapshot operation to complete, calling progressCallback
// with progress updates. Returns the final state when complete or on error.
func (e *Executor) WaitForSnapshot(ctx context.Context, snapshotID string, progressCallback func(*direct.ProgressState)) (*direct.ProgressState, error) {
	return e.client.WaitForCompletion(ctx, snapshotID, progressCallback)
}

// =============================================================================
// Node Management Operations
// =============================================================================

// NodeInfo contains information about a registered node.
type NodeInfo struct {
	NodeID        string
	NodeName      string
	Host          string
	Port          int
	Priority      int
	IsCoordinator bool
	LastSeen      time.Time
	Status        string
	IsHealthy     bool
}

// RegisterNode registers or updates a node in the replication cluster.
func (e *Executor) RegisterNode(ctx context.Context, nodeID, nodeName string, host string, port, priority int) (*NodeInfo, error) {
	var info NodeInfo
	var lastSeen *time.Time

	err := e.client.QueryRow(ctx, `
		SELECT node_id, node_name, host, port, priority, is_coordinator, last_seen, status
		FROM steep_repl.register_node($1, $2, $3, $4, $5)
	`, nodeID, nodeName, host, port, priority).Scan(
		&info.NodeID,
		&info.NodeName,
		&info.Host,
		&info.Port,
		&info.Priority,
		&info.IsCoordinator,
		&lastSeen,
		&info.Status,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to register node: %w", err)
	}

	if lastSeen != nil {
		info.LastSeen = *lastSeen
		info.IsHealthy = time.Since(*lastSeen) < 30*time.Second
	}

	return &info, nil
}

// Heartbeat updates the heartbeat timestamp for a node.
func (e *Executor) Heartbeat(ctx context.Context, nodeID string) error {
	var success bool
	err := e.client.QueryRow(ctx, `SELECT steep_repl.heartbeat($1)`, nodeID).Scan(&success)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}
	if !success {
		return fmt.Errorf("node %s not found", nodeID)
	}
	return nil
}

// NodeStatus retrieves the status of a node (or all nodes if nodeID is empty).
func (e *Executor) NodeStatus(ctx context.Context, nodeID string) ([]NodeInfo, error) {
	var nodes []NodeInfo

	// Convert empty string to nil (NULL) to get all nodes
	var nodeIDParam any
	if nodeID == "" {
		nodeIDParam = nil
	} else {
		nodeIDParam = nodeID
	}

	rows, err := e.client.Query(ctx, `
		SELECT node_id, node_name, status, last_seen, is_healthy
		FROM steep_repl.node_status($1)
	`, nodeIDParam)
	if err != nil {
		return nil, fmt.Errorf("failed to get node status: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var info NodeInfo
		var lastSeen *time.Time

		err := rows.Scan(
			&info.NodeID,
			&info.NodeName,
			&info.Status,
			&lastSeen,
			&info.IsHealthy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node status: %w", err)
		}

		if lastSeen != nil {
			info.LastSeen = *lastSeen
		}

		nodes = append(nodes, info)
	}

	return nodes, rows.Err()
}

// =============================================================================
// Schema Operations
// =============================================================================

// SchemaFingerprint contains fingerprint information for a table.
type SchemaFingerprint struct {
	SchemaName        string
	TableName         string
	Fingerprint       string
	ColumnDefinitions string
	CapturedAt        time.Time
}

// CaptureFingerprints captures schema fingerprints for a node.
// Returns the number of fingerprints captured.
func (e *Executor) CaptureFingerprints(ctx context.Context, nodeID string) (int, error) {
	var count int
	err := e.client.QueryRow(ctx, `SELECT steep_repl.capture_all_fingerprints($1)`, nodeID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to capture fingerprints: %w", err)
	}
	return count, nil
}

// GetFingerprints retrieves captured schema fingerprints for a node.
func (e *Executor) GetFingerprints(ctx context.Context, nodeID string) ([]SchemaFingerprint, error) {
	var fingerprints []SchemaFingerprint

	rows, err := e.client.Query(ctx, `
		SELECT table_schema, table_name, fingerprint, column_definitions, captured_at
		FROM steep_repl.schema_fingerprints
		WHERE node_id = $1
		ORDER BY table_schema, table_name
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fingerprints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fp SchemaFingerprint
		err := rows.Scan(
			&fp.SchemaName,
			&fp.TableName,
			&fp.Fingerprint,
			&fp.ColumnDefinitions,
			&fp.CapturedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan fingerprint: %w", err)
		}
		fingerprints = append(fingerprints, fp)
	}

	return fingerprints, rows.Err()
}

// CompareFingerprints compares schema fingerprints between two nodes.
func (e *Executor) CompareFingerprints(ctx context.Context, localNodeID, peerNodeID string) ([]FingerprintComparison, error) {
	var comparisons []FingerprintComparison

	rows, err := e.client.Query(ctx, `
		SELECT schema_name, table_name, local_fingerprint, peer_fingerprint, status
		FROM steep_repl.compare_fingerprints($1, $2)
	`, localNodeID, peerNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to compare fingerprints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var comp FingerprintComparison
		err := rows.Scan(
			&comp.SchemaName,
			&comp.TableName,
			&comp.LocalFingerprint,
			&comp.PeerFingerprint,
			&comp.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan comparison: %w", err)
		}
		comparisons = append(comparisons, comp)
	}

	return comparisons, rows.Err()
}

// FingerprintComparison contains the result of comparing fingerprints.
type FingerprintComparison struct {
	SchemaName      string
	TableName       string
	LocalFingerprint string
	PeerFingerprint  string
	Status           string // match, local_only, peer_only, mismatch
}

// =============================================================================
// Merge Operations
// =============================================================================

// MergeOptions configures a bidirectional merge operation.
type MergeOptions struct {
	PeerConnStr string
	Tables      []string
	Strategy    string // prefer-local, prefer-remote, last-modified
	DryRun      bool
}

// MergeResult contains the result of a merge operation.
type MergeResult struct {
	MergeID           string
	Status            string
	TablesProcessed   int
	ConflictsDetected int64
	ConflictsResolved int64
	RowsTransferredAB int64
	RowsTransferredBA int64
	Error             string
}

// AnalyzeOverlap analyzes data overlap between local and peer databases.
func (e *Executor) AnalyzeOverlap(ctx context.Context, peerConnStr string, tables []string) ([]OverlapAnalysis, error) {
	var analyses []OverlapAnalysis

	rows, err := e.client.Query(ctx, `
		SELECT table_name, local_only_count, remote_only_count, match_count, conflict_count
		FROM steep_repl.analyze_overlap($1, $2)
	`, peerConnStr, tables)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze overlap: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a OverlapAnalysis
		err := rows.Scan(
			&a.TableName,
			&a.LocalOnlyCount,
			&a.RemoteOnlyCount,
			&a.MatchCount,
			&a.ConflictCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan overlap analysis: %w", err)
		}
		analyses = append(analyses, a)
	}

	return analyses, rows.Err()
}

// OverlapAnalysis contains overlap analysis results for a table.
type OverlapAnalysis struct {
	TableName       string
	LocalOnlyCount  int64
	RemoteOnlyCount int64
	MatchCount      int64
	ConflictCount   int64
}

// StartMerge starts a bidirectional merge operation.
// Returns the merge ID for progress tracking.
func (e *Executor) StartMerge(ctx context.Context, opts MergeOptions) (string, error) {
	// Set defaults
	if opts.Strategy == "" {
		opts.Strategy = "prefer-local"
	}

	var mergeID string
	err := e.client.QueryRow(ctx, `
		SELECT merge_id FROM steep_repl.start_merge($1, $2, $3, $4)
	`, opts.PeerConnStr, opts.Tables, opts.Strategy, opts.DryRun).Scan(&mergeID)

	if err != nil {
		return "", fmt.Errorf("failed to start merge: %w", err)
	}

	return mergeID, nil
}

// MergeProgress retrieves the current progress of a merge operation.
func (e *Executor) MergeProgress(ctx context.Context, mergeID string) (*MergeResult, error) {
	var result MergeResult

	err := e.client.QueryRow(ctx, `
		SELECT
			merge_id,
			status,
			COALESCE(current_table, ''),
			tables_completed,
			tables_total,
			rows_merged,
			conflict_count,
			COALESCE(error, '')
		FROM steep_repl.merge_progress($1)
	`, mergeID).Scan(
		&result.MergeID,
		&result.Status,
		&result.TablesProcessed, // Using current_table count
		&result.TablesProcessed,
		&result.TablesProcessed,
		&result.RowsTransferredAB,
		&result.ConflictsDetected,
		&result.Error,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get merge progress: %w", err)
	}

	return &result, nil
}

// =============================================================================
// Work Queue Operations
// =============================================================================

// WorkQueueEntry represents an entry in the work queue.
type WorkQueueEntry struct {
	ID            int64
	OperationType string
	OperationID   string
	Status        string
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	ErrorMessage  string
}

// ListOperations lists pending and running operations in the work queue.
func (e *Executor) ListOperations(ctx context.Context, status string) ([]WorkQueueEntry, error) {
	var entries []WorkQueueEntry

	rows, err := e.client.Query(ctx, `
		SELECT id, operation_type, operation_id, status, created_at, started_at, completed_at, error_message
		FROM steep_repl.list_operations($1)
	`, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list operations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entry WorkQueueEntry
		err := rows.Scan(
			&entry.ID,
			&entry.OperationType,
			&entry.OperationID,
			&entry.Status,
			&entry.CreatedAt,
			&entry.StartedAt,
			&entry.CompletedAt,
			&entry.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan work queue entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// CancelOperation cancels a pending or running operation.
func (e *Executor) CancelOperation(ctx context.Context, operationID int64) error {
	var success bool
	err := e.client.QueryRow(ctx, `SELECT steep_repl.cancel_operation($1)`, operationID).Scan(&success)
	if err != nil {
		return fmt.Errorf("failed to cancel operation: %w", err)
	}
	if !success {
		return fmt.Errorf("operation %d could not be cancelled", operationID)
	}
	return nil
}
