// Package direct provides direct PostgreSQL connection for steep-repl CLI.
//
// T013: Create internal/repl/direct/progress.go for NOTIFY payload parsing
package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProgressChannel is the PostgreSQL NOTIFY channel for progress updates.
const ProgressChannel = "steep_repl_progress"

// WorkChannel is the PostgreSQL NOTIFY channel for work queue notifications.
const WorkChannel = "steep_repl_work"

// ProgressNotification represents a parsed progress notification payload.
type ProgressNotification struct {
	// Operation type: snapshot_generate, snapshot_apply, bidirectional_merge
	OperationType string `json:"op"`

	// Operation ID (snapshot_id or merge_id)
	OperationID string `json:"id"`

	// Current phase: schema, data, sequences, indexes, constraints, verify, finalizing
	Phase string `json:"phase,omitempty"`

	// Status: complete, failed (only for terminal notifications)
	Status string `json:"status,omitempty"`

	// Progress percentage (0-100)
	Percent float32 `json:"percent,omitempty"`

	// Tables completed so far
	TablesCompleted int `json:"tables_completed,omitempty"`

	// Total tables to process
	TablesTotal int `json:"tables_total,omitempty"`

	// Current table being processed
	CurrentTable string `json:"table,omitempty"`

	// Bytes processed so far
	BytesProcessed int64 `json:"bytes,omitempty"`

	// Estimated time remaining in seconds
	ETASeconds int `json:"eta,omitempty"`

	// Error message (only for failed status)
	Error string `json:"error,omitempty"`

	// When the notification was received
	ReceivedAt time.Time `json:"-"`
}

// IsComplete returns true if this is a completion notification.
func (p *ProgressNotification) IsComplete() bool {
	return p.Status == "complete" || p.Phase == "complete"
}

// IsFailed returns true if this is a failure notification.
func (p *ProgressNotification) IsFailed() bool {
	return p.Status == "failed" || p.Phase == "failed"
}

// IsTerminal returns true if this is a terminal notification (complete or failed).
func (p *ProgressNotification) IsTerminal() bool {
	return p.IsComplete() || p.IsFailed()
}

// ParseProgressPayload parses a NOTIFY payload into a ProgressNotification.
func ParseProgressPayload(payload string) (*ProgressNotification, error) {
	var notification ProgressNotification
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		return nil, fmt.Errorf("failed to parse progress payload: %w", err)
	}
	notification.ReceivedAt = time.Now()
	return &notification, nil
}

// ProgressListener listens for progress notifications on the steep_repl_progress channel.
type ProgressListener struct {
	client *Client
	conn   *pgx.Conn

	// Filter by operation ID (empty = all operations)
	operationID string

	// Channel for notifications
	notifications chan *ProgressNotification

	// Done channel
	done chan struct{}
}

// NewProgressListener creates a new progress listener.
// If operationID is empty, it receives all progress notifications.
func NewProgressListener(client *Client, operationID string) *ProgressListener {
	return &ProgressListener{
		client:        client,
		operationID:   operationID,
		notifications: make(chan *ProgressNotification, 100),
		done:          make(chan struct{}),
	}
}

// Start begins listening for progress notifications.
// This should be called in a goroutine.
func (l *ProgressListener) Start(ctx context.Context) error {
	// Acquire a dedicated connection for LISTEN
	poolConn, err := l.client.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer poolConn.Release()

	// Get the underlying connection
	conn := poolConn.Conn()

	// Start listening
	_, err = conn.Exec(ctx, fmt.Sprintf("LISTEN %s", ProgressChannel))
	if err != nil {
		return fmt.Errorf("failed to LISTEN: %w", err)
	}

	// Listen loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.done:
			return nil
		default:
			// Wait for notification with timeout
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Log error but continue
				continue
			}

			// Parse payload
			progress, err := ParseProgressPayload(notification.Payload)
			if err != nil {
				// Log error but continue
				continue
			}

			// Filter by operation ID if specified
			if l.operationID != "" && progress.OperationID != l.operationID {
				continue
			}

			// Send to channel (non-blocking)
			select {
			case l.notifications <- progress:
			default:
				// Channel full, drop notification
			}
		}
	}
}

// Notifications returns the channel for receiving progress notifications.
func (l *ProgressListener) Notifications() <-chan *ProgressNotification {
	return l.notifications
}

// Stop stops the listener.
func (l *ProgressListener) Stop() {
	close(l.done)
}

// ProgressState holds the current state of an operation's progress.
type ProgressState struct {
	OperationType string  `json:"operation_type"`
	OperationID   string  `json:"operation_id"`
	Phase         string  `json:"phase"`
	Percent       float32 `json:"percent"`

	TablesCompleted int    `json:"tables_completed"`
	TablesTotal     int    `json:"tables_total"`
	CurrentTable    string `json:"current_table,omitempty"`

	BytesProcessed int64 `json:"bytes_processed"`
	ETASeconds     int   `json:"eta_seconds,omitempty"`

	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`

	IsComplete bool `json:"is_complete"`
	IsFailed   bool `json:"is_failed"`
}

// GetProgress queries the current progress from shared memory via SQL.
func (c *Client) GetProgress(ctx context.Context) (*ProgressState, error) {
	var state ProgressState

	// Query all progress fields
	err := c.pool.QueryRow(ctx, `
		SELECT
			COALESCE(steep_repl.get_progress_operation_type(), ''),
			COALESCE(steep_repl.get_progress_operation_id(), ''),
			COALESCE(steep_repl.get_progress_phase(), 'idle'),
			COALESCE(steep_repl.get_progress_percent(), 0),
			COALESCE(steep_repl.get_progress_tables_completed(), 0),
			COALESCE(steep_repl.get_progress_tables_total(), 0),
			COALESCE(steep_repl.get_progress_current_table(), ''),
			COALESCE(steep_repl.get_progress_bytes_processed(), 0),
			COALESCE(steep_repl.get_progress_eta_seconds(), 0),
			COALESCE(steep_repl.get_progress_error(), ''),
			steep_repl.is_operation_active()
	`).Scan(
		&state.OperationType,
		&state.OperationID,
		&state.Phase,
		&state.Percent,
		&state.TablesCompleted,
		&state.TablesTotal,
		&state.CurrentTable,
		&state.BytesProcessed,
		&state.ETASeconds,
		&state.Error,
		&state.IsComplete, // Using active field temporarily
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get progress: %w", err)
	}

	// Determine completion/failure state
	isActive := state.IsComplete // We stored active in IsComplete temporarily
	state.IsComplete = state.Phase == "complete"
	state.IsFailed = state.Phase == "failed"

	// If not active and not complete/failed, it's idle
	if !isActive && !state.IsComplete && !state.IsFailed {
		return nil, nil // No active operation
	}

	state.UpdatedAt = time.Now()

	return &state, nil
}

// WaitForCompletion waits for an operation to complete, returning progress updates.
func (c *Client) WaitForCompletion(ctx context.Context, operationID string, progressCallback func(*ProgressState)) (*ProgressState, error) {
	// Start a progress listener
	listener := NewProgressListener(c, operationID)
	go func() {
		_ = listener.Start(ctx)
	}()
	defer listener.Stop()

	// Also poll progress periodically in case notifications are missed
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case notification := <-listener.Notifications():
			state := &ProgressState{
				OperationType:   notification.OperationType,
				OperationID:     notification.OperationID,
				Phase:           notification.Phase,
				Percent:         notification.Percent,
				TablesCompleted: notification.TablesCompleted,
				TablesTotal:     notification.TablesTotal,
				CurrentTable:    notification.CurrentTable,
				BytesProcessed:  notification.BytesProcessed,
				ETASeconds:      notification.ETASeconds,
				Error:           notification.Error,
				UpdatedAt:       notification.ReceivedAt,
				IsComplete:      notification.IsComplete(),
				IsFailed:        notification.IsFailed(),
			}

			if progressCallback != nil {
				progressCallback(state)
			}

			if state.IsComplete || state.IsFailed {
				return state, nil
			}

		case <-ticker.C:
			// Poll progress as backup
			state, err := c.GetProgress(ctx)
			if err != nil {
				continue
			}
			if state == nil {
				continue
			}

			// Only process if it matches our operation
			if operationID != "" && state.OperationID != operationID {
				continue
			}

			if progressCallback != nil {
				progressCallback(state)
			}

			if state.IsComplete || state.IsFailed {
				return state, nil
			}
		}
	}
}

// StartSnapshot starts a snapshot generation operation via steep_repl.start_snapshot().
func (c *Client) StartSnapshot(ctx context.Context, outputPath string, compression string, parallel int) (string, error) {
	var snapshotID string

	err := c.pool.QueryRow(ctx,
		"SELECT snapshot_id FROM steep_repl.start_snapshot($1, $2, $3)",
		outputPath, compression, parallel,
	).Scan(&snapshotID)

	if err != nil {
		return "", fmt.Errorf("failed to start snapshot: %w", err)
	}

	return snapshotID, nil
}

// CancelSnapshot cancels a running snapshot operation.
func (c *Client) CancelSnapshot(ctx context.Context, snapshotID string) error {
	var cancelled bool

	err := c.pool.QueryRow(ctx,
		"SELECT steep_repl.cancel_snapshot($1)",
		snapshotID,
	).Scan(&cancelled)

	if err != nil {
		return fmt.Errorf("failed to cancel snapshot: %w", err)
	}

	if !cancelled {
		return fmt.Errorf("snapshot %s could not be cancelled (may not exist or already complete)", snapshotID)
	}

	return nil
}

// BroadcastProgress triggers a progress notification broadcast.
func (c *Client) BroadcastProgress(ctx context.Context) error {
	_, err := c.pool.Exec(ctx, "SELECT steep_repl.broadcast_progress()")
	return err
}
