package ipc

import (
	"encoding/json"
	"time"
)

// Request represents an IPC request from the client (TUI).
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response represents an IPC response from the server (daemon).
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error represents an IPC error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes.
const (
	ErrCodeInvalidRequest   = "INVALID_REQUEST"
	ErrCodeMethodNotFound   = "METHOD_NOT_FOUND"
	ErrCodeInternalError    = "INTERNAL_ERROR"
	ErrCodeNotConnected     = "NOT_CONNECTED"
	ErrCodeNodeNotFound     = "NODE_NOT_FOUND"
	ErrCodePermissionDenied = "PERMISSION_DENIED"
)

// Method names.
const (
	MethodStatusGet   = "status.get"
	MethodHealthCheck = "health.check"
	MethodNodesList   = "nodes.list"
	MethodNodesGet    = "nodes.get"
	MethodAuditQuery  = "audit.query"
)

// StatusResult is the result of status.get.
type StatusResult struct {
	State         string         `json:"state"`
	PID           int            `json:"pid"`
	UptimeSeconds int64          `json:"uptime_seconds"`
	StartTime     time.Time      `json:"start_time"`
	Version       string         `json:"version"`
	PostgreSQL    PostgreSQLInfo `json:"postgresql"`
	GRPC          GRPCInfo       `json:"grpc"`
	Node          NodeInfo       `json:"node"`
}

// PostgreSQLInfo holds PostgreSQL connection info.
type PostgreSQLInfo struct {
	Connected bool   `json:"connected"`
	Version   string `json:"version,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
}

// GRPCInfo holds gRPC server info.
type GRPCInfo struct {
	Listening  bool `json:"listening"`
	Port       int  `json:"port,omitempty"`
	TLSEnabled bool `json:"tls_enabled"`
}

// NodeInfo holds local node info.
type NodeInfo struct {
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	IsCoordinator bool   `json:"is_coordinator"`
	ClusterSize   int    `json:"cluster_size"`
}

// HealthCheckResult is the result of health.check.
type HealthCheckResult struct {
	Status     string                     `json:"status"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth holds health info for a single component.
type ComponentHealth struct {
	Healthy   bool   `json:"healthy"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
}

// NodesListParams are the parameters for nodes.list.
type NodesListParams struct {
	StatusFilter []string `json:"status_filter,omitempty"`
}

// NodesListResult is the result of nodes.list.
type NodesListResult struct {
	Nodes         []NodeEntry `json:"nodes"`
	CoordinatorID string      `json:"coordinator_id"`
}

// NodeEntry represents a node in the cluster.
type NodeEntry struct {
	NodeID        string    `json:"node_id"`
	NodeName      string    `json:"node_name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Priority      int       `json:"priority"`
	IsCoordinator bool      `json:"is_coordinator"`
	LastSeen      time.Time `json:"last_seen"`
	Status        string    `json:"status"`
}

// NodesGetParams are the parameters for nodes.get.
type NodesGetParams struct {
	NodeID string `json:"node_id"`
}

// NodesGetResult is the result of nodes.get.
type NodesGetResult struct {
	Node NodeEntry `json:"node"`
}

// AuditQueryParams are the parameters for audit.query.
type AuditQueryParams struct {
	Limit        int       `json:"limit,omitempty"`
	Offset       int       `json:"offset,omitempty"`
	ActionFilter []string  `json:"action_filter,omitempty"`
	Since        time.Time `json:"since,omitempty"`
}

// AuditQueryResult is the result of audit.query.
type AuditQueryResult struct {
	Entries []AuditEntry `json:"entries"`
	Total   int          `json:"total"`
	HasMore bool         `json:"has_more"`
}

// AuditEntry represents an audit log entry.
type AuditEntry struct {
	ID         int64     `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	Action     string    `json:"action"`
	Actor      string    `json:"actor"`
	TargetType string    `json:"target_type,omitempty"`
	TargetID   string    `json:"target_id,omitempty"`
	Success    bool      `json:"success"`
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id string, code, message string) Response {
	return Response{
		ID: id,
		Error: &Error{
			Code:    code,
			Message: message,
		},
	}
}

// NewSuccessResponse creates a success response.
func NewSuccessResponse(id string, result any) (Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return Response{}, err
	}
	return Response{
		ID:     id,
		Result: data,
	}, nil
}
