package models

import (
	"encoding/json"
	"net"
	"time"
)

// AuditAction represents the type of action being logged.
type AuditAction string

// Audit actions for this feature (014-repl-foundation).
const (
	AuditActionNodeRegistered     AuditAction = "node.registered"
	AuditActionNodeUpdated        AuditAction = "node.updated"
	AuditActionNodeRemoved        AuditAction = "node.removed"
	AuditActionCoordinatorElected AuditAction = "coordinator.elected"
	AuditActionStateUpdated       AuditAction = "state.updated"
	AuditActionDaemonStarted      AuditAction = "daemon.started"
	AuditActionDaemonStopped      AuditAction = "daemon.stopped"
)

// AuditTargetType represents the type of entity being targeted.
type AuditTargetType string

const (
	AuditTargetNode   AuditTargetType = "node"
	AuditTargetState  AuditTargetType = "state"
	AuditTargetDaemon AuditTargetType = "daemon"
)

// AuditLogEntry represents an immutable record of system activity.
type AuditLogEntry struct {
	ID           int64      `db:"id" json:"id"`
	OccurredAt   time.Time  `db:"occurred_at" json:"occurred_at"`
	Action       string     `db:"action" json:"action"`
	Actor        string     `db:"actor" json:"actor"` // Format: role@host
	TargetType   *string    `db:"target_type" json:"target_type,omitempty"`
	TargetID     *string    `db:"target_id" json:"target_id,omitempty"`
	OldValue     []byte     `db:"old_value" json:"-"` // JSONB
	NewValue     []byte     `db:"new_value" json:"-"` // JSONB
	ClientIP     *string    `db:"client_ip" json:"client_ip,omitempty"`
	Success      bool       `db:"success" json:"success"`
	ErrorMessage *string    `db:"error_message" json:"error_message,omitempty"`
}

// GetOldValueAs unmarshals the old value JSONB into the provided target.
func (e *AuditLogEntry) GetOldValueAs(target interface{}) error {
	if e.OldValue == nil {
		return nil
	}
	return json.Unmarshal(e.OldValue, target)
}

// GetNewValueAs unmarshals the new value JSONB into the provided target.
func (e *AuditLogEntry) GetNewValueAs(target interface{}) error {
	if e.NewValue == nil {
		return nil
	}
	return json.Unmarshal(e.NewValue, target)
}

// SetOldValue marshals the provided value to JSONB bytes.
func (e *AuditLogEntry) SetOldValue(value interface{}) error {
	if value == nil {
		e.OldValue = nil
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	e.OldValue = data
	return nil
}

// SetNewValue marshals the provided value to JSONB bytes.
func (e *AuditLogEntry) SetNewValue(value interface{}) error {
	if value == nil {
		e.NewValue = nil
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	e.NewValue = data
	return nil
}

// SetClientIP sets the client IP from a net.IP or string.
func (e *AuditLogEntry) SetClientIP(ip interface{}) {
	switch v := ip.(type) {
	case net.IP:
		if v != nil {
			s := v.String()
			e.ClientIP = &s
		}
	case string:
		if v != "" {
			e.ClientIP = &v
		}
	}
}

// Validate checks that the audit entry has valid field values.
func (e *AuditLogEntry) Validate() error {
	if e.Action == "" {
		return ErrAuditActionRequired
	}
	if e.Actor == "" {
		return ErrAuditActorRequired
	}
	return nil
}

// NewAuditEntry creates a new audit log entry with common fields set.
func NewAuditEntry(action AuditAction, actor string) *AuditLogEntry {
	return &AuditLogEntry{
		OccurredAt: time.Now().UTC(),
		Action:     string(action),
		Actor:      actor,
		Success:    true,
	}
}

// WithTarget sets the target type and ID on the audit entry.
func (e *AuditLogEntry) WithTarget(targetType AuditTargetType, targetID string) *AuditLogEntry {
	tt := string(targetType)
	e.TargetType = &tt
	e.TargetID = &targetID
	return e
}

// WithError marks the entry as failed with an error message.
func (e *AuditLogEntry) WithError(err error) *AuditLogEntry {
	e.Success = false
	if err != nil {
		msg := err.Error()
		e.ErrorMessage = &msg
	}
	return e
}
