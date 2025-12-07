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
	conn       *grpc.ClientConn
	client     pb.CoordinatorClient
	initClient pb.InitServiceClient
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
		conn:       conn,
		client:     pb.NewCoordinatorClient(conn),
		initClient: pb.NewInitServiceClient(conn),
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

// StartInit starts initialization via the InitService.
func (c *Client) StartInit(ctx context.Context, req *pb.StartInitRequest) (*pb.StartInitResponse, error) {
	return c.initClient.StartInit(ctx, req)
}

// CancelInit cancels an in-progress initialization.
func (c *Client) CancelInit(ctx context.Context, req *pb.CancelInitRequest) (*pb.CancelInitResponse, error) {
	return c.initClient.CancelInit(ctx, req)
}

// GetProgress retrieves initialization progress.
func (c *Client) GetProgress(ctx context.Context, req *pb.GetProgressRequest) (*pb.GetProgressResponse, error) {
	return c.initClient.GetProgress(ctx, req)
}

// PrepareInit prepares for initialization.
func (c *Client) PrepareInit(ctx context.Context, req *pb.PrepareInitRequest) (*pb.PrepareInitResponse, error) {
	return c.initClient.PrepareInit(ctx, req)
}

// CompleteInit completes initialization.
func (c *Client) CompleteInit(ctx context.Context, req *pb.CompleteInitRequest) (*pb.CompleteInitResponse, error) {
	return c.initClient.CompleteInit(ctx, req)
}

// StartReinit starts reinitialization.
func (c *Client) StartReinit(ctx context.Context, req *pb.StartReinitRequest) (*pb.StartReinitResponse, error) {
	return c.initClient.StartReinit(ctx, req)
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

// GetSchemaFingerprints retrieves schema fingerprints for all tables.
func (c *Client) GetSchemaFingerprints(ctx context.Context, req *pb.GetSchemaFingerprintsRequest) (*pb.GetSchemaFingerprintsResponse, error) {
	return c.initClient.GetSchemaFingerprints(ctx, req)
}

// CompareSchemas compares schemas between the local node and a remote node.
func (c *Client) CompareSchemas(ctx context.Context, req *pb.CompareSchemasRequest) (*pb.CompareSchemasResponse, error) {
	return c.initClient.CompareSchemas(ctx, req)
}

// GetColumnDiff retrieves column-level differences for a specific table.
func (c *Client) GetColumnDiff(ctx context.Context, req *pb.GetColumnDiffRequest) (*pb.GetColumnDiffResponse, error) {
	return c.initClient.GetColumnDiff(ctx, req)
}

// CaptureFingerprints captures and stores fingerprints for all tables.
func (c *Client) CaptureFingerprints(ctx context.Context, req *pb.CaptureFingerprintsRequest) (*pb.CaptureFingerprintsResponse, error) {
	return c.initClient.CaptureFingerprints(ctx, req)
}

// StartBidirectionalMerge starts bidirectional merge initialization.
func (c *Client) StartBidirectionalMerge(ctx context.Context, req *pb.StartBidirectionalMergeRequest) (*pb.StartBidirectionalMergeResponse, error) {
	return c.initClient.StartBidirectionalMerge(ctx, req)
}
