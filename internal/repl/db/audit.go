package db

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// AuditAction defines the type of audit log action.
type AuditAction string

// Audit log action types for this feature.
const (
	ActionNodeRegistered     AuditAction = "node.registered"
	ActionNodeUpdated        AuditAction = "node.updated"
	ActionNodeRemoved        AuditAction = "node.removed"
	ActionCoordinatorElected AuditAction = "coordinator.elected"
	ActionStateUpdated       AuditAction = "state.updated"
	ActionDaemonStarted      AuditAction = "daemon.started"
	ActionDaemonStopped      AuditAction = "daemon.stopped"
	ActionInitStarted        AuditAction = "init.started"
	ActionInitCompleted      AuditAction = "init.completed"
	ActionInitCancelled      AuditAction = "init.cancelled"
	ActionInitFailed         AuditAction = "init.failed"
)

// AuditTargetType defines the type of target entity.
type AuditTargetType string

// Audit log target types.
const (
	TargetTypeNode   AuditTargetType = "node"
	TargetTypeState  AuditTargetType = "state"
	TargetTypeDaemon AuditTargetType = "daemon"
	TargetTypeInit   AuditTargetType = "init"
)

// AuditEntry represents an entry to be written to the audit log.
type AuditEntry struct {
	Action       AuditAction
	Actor        string
	TargetType   *AuditTargetType
	TargetID     *string
	OldValue     any
	NewValue     any
	ClientIP     net.IP
	Success      bool
	ErrorMessage *string
}

// AuditWriter writes entries to the steep_repl.audit_log table.
type AuditWriter struct {
	pool *Pool
}

// NewAuditWriter creates a new AuditWriter.
func NewAuditWriter(pool *Pool) *AuditWriter {
	return &AuditWriter{pool: pool}
}

// Write writes an audit entry to the database.
func (w *AuditWriter) Write(ctx context.Context, entry AuditEntry) error {
	if w.pool == nil || !w.pool.IsConnected() {
		return fmt.Errorf("audit writer: pool not connected")
	}

	// Serialize JSON values
	var oldValueJSON, newValueJSON []byte
	var err error

	if entry.OldValue != nil {
		oldValueJSON, err = json.Marshal(entry.OldValue)
		if err != nil {
			return fmt.Errorf("failed to marshal old_value: %w", err)
		}
	}

	if entry.NewValue != nil {
		newValueJSON, err = json.Marshal(entry.NewValue)
		if err != nil {
			return fmt.Errorf("failed to marshal new_value: %w", err)
		}
	}

	// Convert target type to string pointer
	var targetType *string
	if entry.TargetType != nil {
		t := string(*entry.TargetType)
		targetType = &t
	}

	// Convert client IP to string
	var clientIP *string
	if entry.ClientIP != nil {
		ip := entry.ClientIP.String()
		clientIP = &ip
	}

	// Insert audit log entry
	sql := `
		INSERT INTO steep_repl.audit_log
			(action, actor, target_type, target_id, old_value, new_value, client_ip, success, error_message)
		VALUES
			($1, $2, $3, $4, $5, $6, $7::inet, $8, $9)
	`

	return w.pool.Exec(ctx, sql,
		string(entry.Action),
		entry.Actor,
		targetType,
		entry.TargetID,
		oldValueJSON,
		newValueJSON,
		clientIP,
		entry.Success,
		entry.ErrorMessage,
	)
}

// LogDaemonStarted logs a daemon.started event.
func (w *AuditWriter) LogDaemonStarted(ctx context.Context, nodeID, nodeName, version string) error {
	targetType := TargetTypeDaemon
	targetID := nodeID

	entry := AuditEntry{
		Action:     ActionDaemonStarted,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"node_id":   nodeID,
			"node_name": nodeName,
			"version":   version,
			"pid":       os.Getpid(),
			"started":   time.Now().UTC().Format(time.RFC3339),
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// LogDaemonStopped logs a daemon.stopped event.
func (w *AuditWriter) LogDaemonStopped(ctx context.Context, nodeID string, uptime time.Duration) error {
	targetType := TargetTypeDaemon
	targetID := nodeID

	entry := AuditEntry{
		Action:     ActionDaemonStopped,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"node_id": nodeID,
			"uptime":  uptime.String(),
			"stopped": time.Now().UTC().Format(time.RFC3339),
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// LogNodeRegistered logs a node.registered event.
func (w *AuditWriter) LogNodeRegistered(ctx context.Context, nodeID, nodeName, host string, port int) error {
	targetType := TargetTypeNode
	targetID := nodeID

	entry := AuditEntry{
		Action:     ActionNodeRegistered,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"node_id":   nodeID,
			"node_name": nodeName,
			"host":      host,
			"port":      port,
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// buildActor creates the actor string in role@host format.
func buildActor() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "steep-repl"
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}

	return fmt.Sprintf("%s@%s", user, hostname)
}

// LogInitStarted logs an init.started event.
func (w *AuditWriter) LogInitStarted(ctx context.Context, targetNodeID, sourceNodeID, method string) error {
	targetType := TargetTypeInit
	targetID := targetNodeID

	entry := AuditEntry{
		Action:     ActionInitStarted,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"target_node": targetNodeID,
			"source_node": sourceNodeID,
			"method":      method,
			"started_at":  time.Now().UTC().Format(time.RFC3339),
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// LogInitCompleted logs an init.completed event.
func (w *AuditWriter) LogInitCompleted(ctx context.Context, targetNodeID string, duration time.Duration) error {
	targetType := TargetTypeInit
	targetID := targetNodeID

	entry := AuditEntry{
		Action:     ActionInitCompleted,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"target_node":  targetNodeID,
			"duration":     duration.String(),
			"completed_at": time.Now().UTC().Format(time.RFC3339),
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// LogInitCancelled logs an init.cancelled event.
func (w *AuditWriter) LogInitCancelled(ctx context.Context, targetNodeID string) error {
	targetType := TargetTypeInit
	targetID := targetNodeID

	entry := AuditEntry{
		Action:     ActionInitCancelled,
		Actor:      buildActor(),
		TargetType: &targetType,
		TargetID:   &targetID,
		NewValue: map[string]any{
			"target_node":  targetNodeID,
			"cancelled_at": time.Now().UTC().Format(time.RFC3339),
		},
		Success: true,
	}

	return w.Write(ctx, entry)
}

// LogInitFailed logs an init.failed event.
func (w *AuditWriter) LogInitFailed(ctx context.Context, targetNodeID string, err error) error {
	targetType := TargetTypeInit
	targetID := targetNodeID
	errMsg := err.Error()

	entry := AuditEntry{
		Action:       ActionInitFailed,
		Actor:        buildActor(),
		TargetType:   &targetType,
		TargetID:     &targetID,
		ErrorMessage: &errMsg,
		NewValue: map[string]any{
			"target_node": targetNodeID,
			"error":       errMsg,
			"failed_at":   time.Now().UTC().Format(time.RFC3339),
		},
		Success: false,
	}

	return w.Write(ctx, entry)
}

// Query audit log entries with filters.
type AuditQueryOptions struct {
	Action     *AuditAction
	Actor      *string
	TargetType *AuditTargetType
	TargetID   *string
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// QueryResult holds the result of an audit log query.
type AuditQueryResult struct {
	ID           int64
	OccurredAt   time.Time
	Action       string
	Actor        string
	TargetType   *string
	TargetID     *string
	OldValue     json.RawMessage
	NewValue     json.RawMessage
	ClientIP     *string
	Success      bool
	ErrorMessage *string
}

// Query retrieves audit log entries with the given options.
func (w *AuditWriter) Query(ctx context.Context, opts AuditQueryOptions) ([]AuditQueryResult, error) {
	if w.pool == nil || !w.pool.IsConnected() {
		return nil, fmt.Errorf("audit writer: pool not connected")
	}

	// Build query
	sql := `
		SELECT id, occurred_at, action, actor, target_type, target_id,
		       old_value, new_value, client_ip::text, success, error_message
		FROM steep_repl.audit_log
		WHERE 1=1
	`

	args := []any{}
	argNum := 1

	if opts.Action != nil {
		sql += fmt.Sprintf(" AND action = $%d", argNum)
		args = append(args, string(*opts.Action))
		argNum++
	}

	if opts.Actor != nil {
		sql += fmt.Sprintf(" AND actor = $%d", argNum)
		args = append(args, *opts.Actor)
		argNum++
	}

	if opts.TargetType != nil {
		sql += fmt.Sprintf(" AND target_type = $%d", argNum)
		args = append(args, string(*opts.TargetType))
		argNum++
	}

	if opts.TargetID != nil {
		sql += fmt.Sprintf(" AND target_id = $%d", argNum)
		args = append(args, *opts.TargetID)
		argNum++
	}

	if opts.Since != nil {
		sql += fmt.Sprintf(" AND occurred_at >= $%d", argNum)
		args = append(args, *opts.Since)
		argNum++
	}

	if opts.Until != nil {
		sql += fmt.Sprintf(" AND occurred_at <= $%d", argNum)
		args = append(args, *opts.Until)
		argNum++
	}

	sql += " ORDER BY occurred_at DESC"

	if opts.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	if opts.Offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", opts.Offset)
	}

	// Execute query
	pool := w.pool.Pool()
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit log: %w", err)
	}
	defer rows.Close()

	var results []AuditQueryResult
	for rows.Next() {
		var r AuditQueryResult
		err := rows.Scan(
			&r.ID, &r.OccurredAt, &r.Action, &r.Actor, &r.TargetType, &r.TargetID,
			&r.OldValue, &r.NewValue, &r.ClientIP, &r.Success, &r.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit log rows: %w", err)
	}

	return results, nil
}
