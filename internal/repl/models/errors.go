package models

import "errors"

// Node validation errors.
var (
	ErrNodeIDRequired   = errors.New("node_id is required")
	ErrNodeNameRequired = errors.New("node_name is required")
	ErrHostRequired     = errors.New("host is required")
	ErrInvalidPort      = errors.New("port must be between 1 and 65535")
	ErrInvalidPriority  = errors.New("priority must be between 1 and 100")
	ErrInvalidStatus    = errors.New("invalid node status")
)

// CoordinatorState validation errors.
var (
	ErrStateKeyRequired   = errors.New("state key is required")
	ErrStateValueRequired = errors.New("state value is required")
	ErrInvalidJSONValue   = errors.New("state value must be valid JSON")
)

// AuditLogEntry validation errors.
var (
	ErrAuditActionRequired = errors.New("audit action is required")
	ErrAuditActorRequired  = errors.New("audit actor is required")
)
