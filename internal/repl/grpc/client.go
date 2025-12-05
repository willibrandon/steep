package grpc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"

	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// Client is a gRPC client for communicating with remote steep-repl nodes.
type Client struct {
	conn   *grpc.ClientConn
	client pb.CoordinatorClient
}

// ClientConfig holds gRPC client configuration.
type ClientConfig struct {
	Address  string
	CertFile string
	KeyFile  string
	CAFile   string
	Timeout  time.Duration
}

// NewClient creates a new gRPC client.
func NewClient(ctx context.Context, config ClientConfig) (*Client, error) {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	dialCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	conn, err := Dial(dialCtx, config.Address, config.CertFile, config.KeyFile, config.CAFile)
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		client: pb.NewCoordinatorClient(conn),
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// HealthCheck performs a health check on the remote node.
func (c *Client) HealthCheck(ctx context.Context, service string) (*pb.HealthCheckResponse, error) {
	return c.client.HealthCheck(ctx, &pb.HealthCheckRequest{Service: service})
}

// RegisterNode registers this node with a remote coordinator.
func (c *Client) RegisterNode(ctx context.Context, nodeID, nodeName, host string, port, priority int32) (*pb.RegisterNodeResponse, error) {
	return c.client.RegisterNode(ctx, &pb.RegisterNodeRequest{
		NodeId:   nodeID,
		NodeName: nodeName,
		Host:     host,
		Port:     port,
		Priority: priority,
	})
}

// GetNodes retrieves the list of nodes from the remote coordinator.
func (c *Client) GetNodes(ctx context.Context, statusFilter []string) (*pb.GetNodesResponse, error) {
	return c.client.GetNodes(ctx, &pb.GetNodesRequest{StatusFilter: statusFilter})
}

// Heartbeat sends a heartbeat to the remote coordinator.
func (c *Client) Heartbeat(ctx context.Context, nodeID string, pgConnected bool, pgVersion string) (*pb.HeartbeatResponse, error) {
	return c.client.Heartbeat(ctx, &pb.HeartbeatRequest{
		NodeId: nodeID,
		NodeStatus: &pb.NodeStatus{
			PostgresqlConnected: pgConnected,
			PostgresqlVersion:   pgVersion,
		},
	})
}

// HealthCheckResult holds a formatted health check result.
type HealthCheckResult struct {
	Status      string
	NodeID      string
	NodeName    string
	Version     string
	UptimeSince time.Time
	Components  map[string]ComponentStatus
}

// ComponentStatus holds component health status.
type ComponentStatus struct {
	Healthy bool
	Status  string
	Message string
}

// GetHealthCheckResult performs a health check and returns a formatted result.
func (c *Client) GetHealthCheckResult(ctx context.Context) (*HealthCheckResult, error) {
	resp, err := c.HealthCheck(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	result := &HealthCheckResult{
		NodeID:     resp.NodeId,
		NodeName:   resp.NodeName,
		Version:    resp.Version,
		Components: make(map[string]ComponentStatus),
	}

	switch resp.Status {
	case pb.HealthCheckResponse_SERVING:
		result.Status = "healthy"
	case pb.HealthCheckResponse_NOT_SERVING:
		result.Status = "unhealthy"
	default:
		result.Status = "unknown"
	}

	if resp.UptimeSince != nil {
		result.UptimeSince = resp.UptimeSince.AsTime()
	}

	for name, comp := range resp.Components {
		result.Components[name] = ComponentStatus{
			Healthy: comp.Healthy,
			Status:  comp.Status,
			Message: comp.Message,
		}
	}

	return result, nil
}
