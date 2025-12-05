package ipc

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/willibrandon/steep/internal/repl/db"
)

// DaemonStatus holds daemon status information (avoids import cycle).
type DaemonStatus struct {
	State      string
	NodeID     string
	NodeName   string
	Uptime     time.Duration
	StartTime  time.Time
	Version    string
	PostgreSQL struct {
		Status  string
		Version string
		Port    int
	}
	GRPC struct {
		Status string
		Port   int
	}
	IPC struct {
		Status string
	}
	HTTP struct {
		Status string
		Port   int
	}
}

// DaemonConfig holds relevant daemon config (avoids import cycle).
type DaemonConfig struct {
	NodeID     string
	NodeName   string
	PostgreSQL struct {
		Host string
		Port int
	}
	GRPC struct {
		Port int
	}
}

// DaemonProvider is an interface for accessing daemon state.
// This avoids import cycles between ipc and daemon packages.
type DaemonProvider interface {
	GetStatus() DaemonStatus
	GetConfig() DaemonConfig
	GetPool() *db.Pool
	GetAuditWriter() *db.AuditWriter
	HealthCheckPool(ctx context.Context) error
}

// Handlers provides IPC method handlers.
type Handlers struct {
	provider DaemonProvider
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(provider DaemonProvider) *Handlers {
	return &Handlers{provider: provider}
}

// RegisterAll registers all handlers with the server.
func (h *Handlers) RegisterAll(s *Server) {
	s.RegisterHandler(MethodStatusGet, h.StatusGet)
	s.RegisterHandler(MethodHealthCheck, h.HealthCheck)
	s.RegisterHandler(MethodNodesList, h.NodesList)
	s.RegisterHandler(MethodNodesGet, h.NodesGet)
	s.RegisterHandler(MethodAuditQuery, h.AuditQuery)
}

// StatusGet handles status.get requests.
func (h *Handlers) StatusGet(ctx context.Context, params json.RawMessage) (any, error) {
	status := h.provider.GetStatus()
	cfg := h.provider.GetConfig()

	result := StatusResult{
		State:         status.State,
		PID:           os.Getpid(),
		UptimeSeconds: int64(status.Uptime.Seconds()),
		StartTime:     status.StartTime,
		Version:       status.Version,
		NodeID:        cfg.NodeID,
		NodeName:      cfg.NodeName,
		Uptime:        status.Uptime,
		PostgreSQL: PostgreSQLInfo{
			Status:    status.PostgreSQL.Status,
			Connected: status.PostgreSQL.Status == "connected",
			Version:   status.PostgreSQL.Version,
			Host:      cfg.PostgreSQL.Host,
			Port:      cfg.PostgreSQL.Port,
		},
		GRPC: GRPCInfo{
			Status:     status.GRPC.Status,
			Listening:  status.GRPC.Status == "listening",
			Port:       cfg.GRPC.Port,
			TLSEnabled: false, // TODO: Add TLS support
		},
		IPC: IPCInfo{
			Status: status.IPC.Status,
		},
		HTTP: HTTPInfo{
			Status: status.HTTP.Status,
			Port:   status.HTTP.Port,
		},
		Node: NodeInfo{
			NodeID:        cfg.NodeID,
			NodeName:      cfg.NodeName,
			IsCoordinator: false, // TODO: Implement coordinator election
			ClusterSize:   1,     // TODO: Count registered nodes
		},
	}

	return result, nil
}

// HealthCheck handles health.check requests.
func (h *Handlers) HealthCheck(ctx context.Context, params json.RawMessage) (any, error) {
	status := h.provider.GetStatus()

	components := make(map[string]ComponentHealth)

	// PostgreSQL health
	pgHealthy := status.PostgreSQL.Status == "connected"
	var latency int64
	if pgHealthy {
		// Measure query latency
		start := time.Now()
		if err := h.provider.HealthCheckPool(ctx); err != nil {
			pgHealthy = false
		} else {
			latency = time.Since(start).Milliseconds()
		}
	}
	components["postgresql"] = ComponentHealth{
		Healthy:   pgHealthy,
		Status:    status.PostgreSQL.Status,
		LatencyMs: latency,
	}

	// gRPC health
	components["grpc"] = ComponentHealth{
		Healthy: status.GRPC.Status == "listening",
		Status:  status.GRPC.Status,
	}

	// IPC health (always healthy if we got here)
	components["ipc"] = ComponentHealth{
		Healthy: true,
		Status:  "listening",
	}

	// Overall status
	overallStatus := "healthy"
	for _, comp := range components {
		if !comp.Healthy {
			overallStatus = "unhealthy"
			break
		}
	}

	return HealthCheckResult{
		Status:     overallStatus,
		Components: components,
	}, nil
}

// NodesList handles nodes.list requests.
func (h *Handlers) NodesList(ctx context.Context, params json.RawMessage) (any, error) {
	pool := h.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return nil, &HandlerError{
			Code:    ErrCodeNotConnected,
			Message: "PostgreSQL not connected",
		}
	}

	// Parse params
	var p NodesListParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &HandlerError{
				Code:    ErrCodeInvalidRequest,
				Message: "invalid params: " + err.Error(),
			}
		}
	}

	// Query nodes from database
	nodes, err := h.queryNodes(ctx, pool, p.StatusFilter)
	if err != nil {
		return nil, err
	}

	// Find coordinator
	var coordinatorID string
	for _, n := range nodes {
		if n.IsCoordinator {
			coordinatorID = n.NodeID
			break
		}
	}

	return NodesListResult{
		Nodes:         nodes,
		CoordinatorID: coordinatorID,
	}, nil
}

// NodesGet handles nodes.get requests.
func (h *Handlers) NodesGet(ctx context.Context, params json.RawMessage) (any, error) {
	pool := h.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return nil, &HandlerError{
			Code:    ErrCodeNotConnected,
			Message: "PostgreSQL not connected",
		}
	}

	// Parse params
	var p NodesGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &HandlerError{
			Code:    ErrCodeInvalidRequest,
			Message: "invalid params: " + err.Error(),
		}
	}

	if p.NodeID == "" {
		return nil, &HandlerError{
			Code:    ErrCodeInvalidRequest,
			Message: "node_id is required",
		}
	}

	// Query single node
	node, err := h.queryNode(ctx, pool, p.NodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, &HandlerError{
			Code:    ErrCodeNodeNotFound,
			Message: "node not found: " + p.NodeID,
		}
	}

	return NodesGetResult{Node: *node}, nil
}

// AuditQuery handles audit.query requests.
func (h *Handlers) AuditQuery(ctx context.Context, params json.RawMessage) (any, error) {
	pool := h.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return nil, &HandlerError{
			Code:    ErrCodeNotConnected,
			Message: "PostgreSQL not connected",
		}
	}

	// Parse params
	var p AuditQueryParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &HandlerError{
				Code:    ErrCodeInvalidRequest,
				Message: "invalid params: " + err.Error(),
			}
		}
	}

	// Set defaults
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Limit > 1000 {
		p.Limit = 1000
	}

	// Query audit log using existing AuditWriter
	auditWriter := h.provider.GetAuditWriter()
	if auditWriter == nil {
		return nil, &HandlerError{
			Code:    ErrCodeInternalError,
			Message: "audit writer not initialized",
		}
	}

	opts := db.AuditQueryOptions{
		Limit:  p.Limit + 1, // +1 to detect has_more
		Offset: p.Offset,
	}

	if len(p.ActionFilter) > 0 && len(p.ActionFilter) == 1 {
		action := db.AuditAction(p.ActionFilter[0])
		opts.Action = &action
	}

	if !p.Since.IsZero() {
		opts.Since = &p.Since
	}

	results, err := auditWriter.Query(ctx, opts)
	if err != nil {
		return nil, &HandlerError{
			Code:    ErrCodeInternalError,
			Message: "failed to query audit log: " + err.Error(),
		}
	}

	// Convert results
	hasMore := len(results) > p.Limit
	if hasMore {
		results = results[:p.Limit]
	}

	entries := make([]AuditEntry, len(results))
	for i, r := range results {
		entries[i] = AuditEntry{
			ID:         r.ID,
			OccurredAt: r.OccurredAt,
			Action:     r.Action,
			Actor:      r.Actor,
			Success:    r.Success,
		}
		if r.TargetType != nil {
			entries[i].TargetType = *r.TargetType
		}
		if r.TargetID != nil {
			entries[i].TargetID = *r.TargetID
		}
	}

	return AuditQueryResult{
		Entries: entries,
		Total:   len(entries),
		HasMore: hasMore,
	}, nil
}

// queryNodes queries nodes from the database.
func (h *Handlers) queryNodes(ctx context.Context, pool *db.Pool, statusFilter []string) ([]NodeEntry, error) {
	sql := `
		SELECT node_id, node_name, host, port, priority, is_coordinator, last_seen, status
		FROM steep_repl.nodes
	`

	if len(statusFilter) > 0 {
		sql += " WHERE status = ANY($1)"
	}

	sql += " ORDER BY priority DESC, node_id"

	var rows interface {
		Close()
		Next() bool
		Scan(dest ...any) error
		Err() error
	}
	var err error

	pgpool := pool.Pool()
	if len(statusFilter) > 0 {
		rows, err = pgpool.Query(ctx, sql, statusFilter)
	} else {
		rows, err = pgpool.Query(ctx, sql)
	}
	if err != nil {
		return nil, &HandlerError{
			Code:    ErrCodeInternalError,
			Message: "failed to query nodes: " + err.Error(),
		}
	}
	defer rows.Close()

	var nodes []NodeEntry
	for rows.Next() {
		var n NodeEntry
		var lastSeen *time.Time
		if err := rows.Scan(&n.NodeID, &n.NodeName, &n.Host, &n.Port, &n.Priority, &n.IsCoordinator, &lastSeen, &n.Status); err != nil {
			return nil, &HandlerError{
				Code:    ErrCodeInternalError,
				Message: "failed to scan node: " + err.Error(),
			}
		}
		if lastSeen != nil {
			n.LastSeen = *lastSeen
		}
		nodes = append(nodes, n)
	}

	if err := rows.Err(); err != nil {
		return nil, &HandlerError{
			Code:    ErrCodeInternalError,
			Message: "error iterating nodes: " + err.Error(),
		}
	}

	return nodes, nil
}

// queryNode queries a single node from the database.
func (h *Handlers) queryNode(ctx context.Context, pool *db.Pool, nodeID string) (*NodeEntry, error) {
	sql := `
		SELECT node_id, node_name, host, port, priority, is_coordinator, last_seen, status
		FROM steep_repl.nodes
		WHERE node_id = $1
	`

	var n NodeEntry
	var lastSeen *time.Time
	err := pool.Pool().QueryRow(ctx, sql, nodeID).Scan(
		&n.NodeID, &n.NodeName, &n.Host, &n.Port, &n.Priority, &n.IsCoordinator, &lastSeen, &n.Status,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, &HandlerError{
			Code:    ErrCodeInternalError,
			Message: "failed to query node: " + err.Error(),
		}
	}

	if lastSeen != nil {
		n.LastSeen = *lastSeen
	}

	return &n, nil
}
