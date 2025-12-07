package repl_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// =============================================================================
// gRPC Test Suite - shares a single container across all tests
// =============================================================================

type GRPCTestSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	container  testcontainers.Container
	pool       *pgxpool.Pool
	connConfig *config.PostgreSQLConfig
	// Per-test daemon (recreated for each test for isolation)
	daemon     *daemon.Daemon
	grpcPort   int
	socketPath string
}

func TestGRPCSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(GRPCTestSuite))
}

func (s *GRPCTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start PostgreSQL container")
	s.container = container

	host, err := container.Host(s.ctx)
	s.Require().NoError(err)

	port, err := container.MappedPort(s.ctx, "5432")
	s.Require().NoError(err)

	connStr := fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, port.Port())
	pool, err := pgxpool.New(s.ctx, connStr)
	s.Require().NoError(err)
	s.pool = pool

	// Create extension
	_, err = pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension")

	// Store connection config for daemon creation
	connConfig := pool.Config().ConnConfig
	s.connConfig = &config.PostgreSQLConfig{
		Host:     connConfig.Host,
		Port:     int(connConfig.Port),
		Database: connConfig.Database,
		User:     connConfig.User,
	}

	s.T().Log("GRPCTestSuite: Shared container ready")
}

func (s *GRPCTestSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.container != nil {
		_ = s.container.Terminate(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *GRPCTestSuite) SetupTest() {
	// Clean up nodes table before each test
	_, _ = s.pool.Exec(s.ctx, "DELETE FROM steep_repl.nodes")

	// Generate unique socket path for this test
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	s.socketPath = fmt.Sprintf("/tmp/sr-%s.sock", hex.EncodeToString(b))

	// Use a unique port for each test (base port + test index)
	s.grpcPort = 15450
}

func (s *GRPCTestSuite) TearDownTest() {
	if s.daemon != nil {
		s.daemon.Stop()
		s.daemon = nil
	}
	if s.socketPath != "" {
		os.Remove(s.socketPath)
	}
}

func (s *GRPCTestSuite) createDaemon(nodeID, nodeName string) *daemon.Daemon {
	cfg := &config.Config{
		NodeID:     nodeID,
		NodeName:   nodeName,
		PostgreSQL: *s.connConfig,
		GRPC: config.GRPCConfig{
			Port: s.grpcPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    s.socketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	s.Require().NoError(err, "Failed to create daemon")

	err = d.Start()
	s.Require().NoError(err, "Failed to start daemon")

	s.daemon = d
	time.Sleep(200 * time.Millisecond) // Brief wait for server to be ready
	return d
}

// =============================================================================
// Tests
// =============================================================================

func (s *GRPCTestSuite) TestServerStart() {
	d := s.createDaemon("grpc-server-test", "gRPC Server Test")

	status := d.Status()
	s.Assert().Equal("listening", status.GRPC.Status)
	s.Assert().Equal(s.grpcPort, status.GRPC.Port)
}

func (s *GRPCTestSuite) TestHealthCheck() {
	s.createDaemon("grpc-health-test", "gRPC Health Test")

	conn, err := replgrpc.Dial(s.ctx, fmt.Sprintf("localhost:%d", s.grpcPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)
	resp, err := client.HealthCheck(s.ctx, &pb.HealthCheckRequest{})
	s.Require().NoError(err)

	s.Assert().Equal(pb.HealthCheckResponse_SERVING, resp.Status)
	s.Assert().Equal("grpc-health-test", resp.NodeId)
	s.Assert().Equal("gRPC Health Test", resp.NodeName)

	pgComp, ok := resp.Components["postgresql"]
	s.Assert().True(ok, "Response should include postgresql component")
	if ok {
		s.Assert().True(pgComp.Healthy, "PostgreSQL component should be healthy")
	}
}

func (s *GRPCTestSuite) TestRegisterNode() {
	s.createDaemon("grpc-register-test", "gRPC Register Test")

	conn, err := replgrpc.Dial(s.ctx, fmt.Sprintf("localhost:%d", s.grpcPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Register a node
	resp, err := client.RegisterNode(s.ctx, &pb.RegisterNodeRequest{
		NodeId:   "test-node-1",
		NodeName: "Test Node 1",
		Host:     "192.168.1.100",
		Port:     5432,
		Priority: 50,
	})
	s.Require().NoError(err)
	s.Assert().True(resp.Success, "RegisterNode should succeed, got error: %s", resp.Error)

	// Verify node was registered
	nodesResp, err := client.GetNodes(s.ctx, &pb.GetNodesRequest{})
	s.Require().NoError(err)

	found := false
	for _, n := range nodesResp.Nodes {
		if n.NodeId == "test-node-1" {
			found = true
			s.Assert().Equal("Test Node 1", n.NodeName)
			s.Assert().Equal("192.168.1.100", n.Host)
			s.Assert().Equal(int32(5432), n.Port)
			s.Assert().Equal(int32(50), n.Priority)
		}
	}
	s.Assert().True(found, "Registered node not found in GetNodes response")
}

func (s *GRPCTestSuite) TestGetNodes() {
	s.createDaemon("grpc-getnodes-test", "gRPC GetNodes Test")

	conn, err := replgrpc.Dial(s.ctx, fmt.Sprintf("localhost:%d", s.grpcPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Initially only daemon's self-registered node
	resp, err := client.GetNodes(s.ctx, &pb.GetNodesRequest{})
	s.Require().NoError(err)
	initialCount := len(resp.Nodes)
	s.Assert().Equal(1, initialCount, "Expected 1 node initially (daemon self-registration)")

	// Register multiple nodes
	nodes := []struct {
		id       string
		name     string
		host     string
		priority int32
	}{
		{"node-a", "Node A", "192.168.1.1", 100},
		{"node-b", "Node B", "192.168.1.2", 50},
		{"node-c", "Node C", "192.168.1.3", 75},
	}

	for _, node := range nodes {
		_, err := client.RegisterNode(s.ctx, &pb.RegisterNodeRequest{
			NodeId:   node.id,
			NodeName: node.name,
			Host:     node.host,
			Port:     5432,
			Priority: node.priority,
		})
		s.Require().NoError(err, "Failed to register node %s", node.id)
	}

	// Get all nodes
	resp, err = client.GetNodes(s.ctx, &pb.GetNodesRequest{})
	s.Require().NoError(err)
	s.Assert().Equal(initialCount+3, len(resp.Nodes))

	// Verify ordered by priority DESC
	if len(resp.Nodes) >= 2 {
		s.Assert().GreaterOrEqual(resp.Nodes[0].Priority, resp.Nodes[1].Priority,
			"Nodes should be ordered by priority descending")
	}
}

func (s *GRPCTestSuite) TestHeartbeat() {
	s.createDaemon("grpc-heartbeat-test", "gRPC Heartbeat Test")

	conn, err := replgrpc.Dial(s.ctx, fmt.Sprintf("localhost:%d", s.grpcPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Register a node first
	_, err = client.RegisterNode(s.ctx, &pb.RegisterNodeRequest{
		NodeId:   "heartbeat-node",
		NodeName: "Heartbeat Node",
		Host:     "192.168.1.50",
		Port:     5432,
		Priority: 50,
	})
	s.Require().NoError(err)

	// Send heartbeat
	resp, err := client.Heartbeat(s.ctx, &pb.HeartbeatRequest{
		NodeId: "heartbeat-node",
	})
	s.Require().NoError(err)
	s.Assert().True(resp.Acknowledged, "Heartbeat should be acknowledged")
}

func (s *GRPCTestSuite) TestMultipleClients() {
	s.createDaemon("grpc-multiclient-test", "gRPC MultiClient Test")

	const numClients = 5
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		go func() {
			conn, err := replgrpc.Dial(s.ctx, fmt.Sprintf("localhost:%d", s.grpcPort), "", "", "")
			if err != nil {
				errors <- err
				return
			}
			defer conn.Close()

			client := pb.NewCoordinatorClient(conn)
			_, err = client.HealthCheck(s.ctx, &pb.HealthCheckRequest{})
			errors <- err
		}()
	}

	var errCount int
	for i := 0; i < numClients; i++ {
		if err := <-errors; err != nil {
			errCount++
			s.T().Logf("Client error: %v", err)
		}
	}

	s.Assert().Equal(0, errCount, "%d/%d clients failed", errCount, numClients)
}
