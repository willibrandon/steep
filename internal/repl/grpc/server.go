package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/willibrandon/steep/internal/repl/db"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// ServerConfig holds gRPC server configuration.
type ServerConfig struct {
	Port     int
	CertFile string
	KeyFile  string
	CAFile   string
}

// DaemonProvider is an interface for accessing daemon state (avoids import cycle).
type DaemonProvider interface {
	GetNodeID() string
	GetNodeName() string
	GetVersion() string
	GetStartTime() time.Time
	GetPool() *db.Pool
	IsPostgreSQLConnected() bool
	GetPostgreSQLVersion() string
}

// Server is the gRPC server for node-to-node communication.
type Server struct {
	pb.UnimplementedCoordinatorServer

	config   ServerConfig
	provider DaemonProvider
	logger   *log.Logger
	debug    bool

	server   *grpc.Server
	listener net.Listener

	mu      sync.Mutex
	running bool
}

// NewServer creates a new gRPC server.
func NewServer(config ServerConfig, provider DaemonProvider, logger *log.Logger, debug bool) (*Server, error) {
	return &Server{
		config:   config,
		provider: provider,
		logger:   logger,
		debug:    debug,
	}, nil
}

// Start starts the gRPC server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Load TLS credentials
	creds, err := LoadServerCredentials(s.config.CertFile, s.config.KeyFile, s.config.CAFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS credentials: %w", err)
	}

	// Create gRPC server
	var opts []grpc.ServerOption
	if creds != nil {
		opts = append(opts, grpc.Creds(creds))
		s.logger.Printf("gRPC server using mTLS")
	} else {
		s.logger.Printf("gRPC server using insecure transport (no TLS configured)")
	}

	s.server = grpc.NewServer(opts...)
	pb.RegisterCoordinatorServer(s.server, s)

	// Create listener
	addr := fmt.Sprintf(":%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	s.running = true
	s.logger.Printf("gRPC server listening on port %d", s.config.Port)

	// Start serving in background
	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Printf("gRPC server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the gRPC server gracefully.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.server.GracefulStop()
	s.running = false
	s.logger.Println("gRPC server stopped")

	return nil
}

// Port returns the server port.
func (s *Server) Port() int {
	return s.config.Port
}

// HealthCheck implements the Coordinator.HealthCheck RPC.
func (s *Server) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	s.logRequest(ctx, "HealthCheck", req.Service)

	// Build component health
	components := make(map[string]*pb.ComponentHealth)

	// PostgreSQL health
	pgConnected := s.provider.IsPostgreSQLConnected()
	pgStatus := "disconnected"
	if pgConnected {
		pgStatus = "connected"
	}
	components["postgresql"] = &pb.ComponentHealth{
		Healthy: pgConnected,
		Status:  pgStatus,
	}

	// Determine overall status
	status := pb.HealthCheckResponse_SERVING
	if !pgConnected {
		status = pb.HealthCheckResponse_NOT_SERVING
	}

	return &pb.HealthCheckResponse{
		Status:      status,
		Components:  components,
		NodeId:      s.provider.GetNodeID(),
		NodeName:    s.provider.GetNodeName(),
		Version:     s.provider.GetVersion(),
		UptimeSince: timestamppb.New(s.provider.GetStartTime()),
	}, nil
}

// RegisterNode implements the Coordinator.RegisterNode RPC.
func (s *Server) RegisterNode(ctx context.Context, req *pb.RegisterNodeRequest) (*pb.RegisterNodeResponse, error) {
	s.logRequest(ctx, "RegisterNode", req.NodeId)

	pool := s.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return &pb.RegisterNodeResponse{
			Success: false,
			Error:   "PostgreSQL not connected",
		}, nil
	}

	// Insert or update node in database
	sql := `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, last_seen, status)
		VALUES ($1, $2, $3, $4, $5, NOW(), 'healthy')
		ON CONFLICT (node_id) DO UPDATE SET
			node_name = EXCLUDED.node_name,
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			priority = EXCLUDED.priority,
			last_seen = NOW(),
			status = 'healthy'
	`

	err := pool.Exec(ctx, sql, req.NodeId, req.NodeName, req.Host, req.Port, req.Priority)
	if err != nil {
		s.logger.Printf("Failed to register node %s: %v", req.NodeId, err)
		return &pb.RegisterNodeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to register node: %v", err),
		}, nil
	}

	// Get current cluster state
	nodes, coordinatorID, err := s.getNodes(ctx, pool, nil)
	if err != nil {
		s.logger.Printf("Failed to get nodes after registration: %v", err)
	}

	s.logger.Printf("Node registered: %s (%s:%d)", req.NodeId, req.Host, req.Port)

	return &pb.RegisterNodeResponse{
		Success:       true,
		Nodes:         nodes,
		CoordinatorId: coordinatorID,
	}, nil
}

// GetNodes implements the Coordinator.GetNodes RPC.
func (s *Server) GetNodes(ctx context.Context, req *pb.GetNodesRequest) (*pb.GetNodesResponse, error) {
	s.logRequest(ctx, "GetNodes", "")

	pool := s.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return &pb.GetNodesResponse{}, nil
	}

	nodes, coordinatorID, err := s.getNodes(ctx, pool, req.StatusFilter)
	if err != nil {
		s.logger.Printf("Failed to get nodes: %v", err)
		return &pb.GetNodesResponse{}, nil
	}

	return &pb.GetNodesResponse{
		Nodes:         nodes,
		CoordinatorId: coordinatorID,
	}, nil
}

// Heartbeat implements the Coordinator.Heartbeat RPC.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	s.logRequest(ctx, "Heartbeat", req.NodeId)

	pool := s.provider.GetPool()
	if pool == nil || !pool.IsConnected() {
		return &pb.HeartbeatResponse{Acknowledged: false}, nil
	}

	// Update last_seen and status for the node
	sql := `
		UPDATE steep_repl.nodes
		SET last_seen = NOW(), status = 'healthy'
		WHERE node_id = $1
	`

	err := pool.Exec(ctx, sql, req.NodeId)
	if err != nil {
		s.logger.Printf("Failed to update heartbeat for node %s: %v", req.NodeId, err)
		return &pb.HeartbeatResponse{Acknowledged: false}, nil
	}

	// Get current coordinator
	var coordinatorID string
	coordSQL := `SELECT node_id FROM steep_repl.nodes WHERE is_coordinator = true LIMIT 1`
	_ = pool.QueryRow(ctx, coordSQL).Scan(&coordinatorID)

	return &pb.HeartbeatResponse{
		Acknowledged:  true,
		CoordinatorId: coordinatorID,
	}, nil
}

// getNodes retrieves nodes from the database.
func (s *Server) getNodes(ctx context.Context, pool *db.Pool, statusFilter []string) ([]*pb.NodeInfo, string, error) {
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
		return nil, "", err
	}
	defer rows.Close()

	var nodes []*pb.NodeInfo
	var coordinatorID string

	for rows.Next() {
		var n pb.NodeInfo
		var lastSeen *time.Time
		var isCoordinator bool

		if err := rows.Scan(&n.NodeId, &n.NodeName, &n.Host, &n.Port, &n.Priority, &isCoordinator, &lastSeen, &n.Status); err != nil {
			return nil, "", err
		}

		n.IsCoordinator = isCoordinator
		if lastSeen != nil {
			n.LastSeen = timestamppb.New(*lastSeen)
		}

		if isCoordinator {
			coordinatorID = n.NodeId
		}

		nodes = append(nodes, &n)
	}

	return nodes, coordinatorID, rows.Err()
}

// logRequest logs an incoming RPC request.
func (s *Server) logRequest(ctx context.Context, method string, detail string) {
	if !s.debug {
		return
	}

	clientAddr := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		clientAddr = p.Addr.String()
	}

	if detail != "" {
		s.logger.Printf("gRPC %s from %s: %s", method, clientAddr, detail)
	} else {
		s.logger.Printf("gRPC %s from %s", method, clientAddr)
	}
}

// Dial creates a gRPC client connection to a remote node.
func Dial(ctx context.Context, addr string, certFile, keyFile, caFile string) (*grpc.ClientConn, error) {
	creds, err := LoadClientCredentials(certFile, keyFile, caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client credentials: %w", err)
	}

	var opts []grpc.DialOption
	if creds != nil {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	return conn, nil
}
