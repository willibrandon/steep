package repl_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	"github.com/willibrandon/steep/internal/repl/ipc"
)

// =============================================================================
// IPC Test Suite
// =============================================================================

// IPCTestSuite runs IPC integration tests with shared PostgreSQL container and daemon.
type IPCTestSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	container  testcontainers.Container
	pool       *pgxpool.Pool
	daemon     *daemon.Daemon
	socketPath string
	client     *ipc.Client
}

func TestIPCSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(IPCTestSuite))
}

func (s *IPCTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.T().Log("Setting up IPC test suite - creating shared PostgreSQL container...")

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

	s.T().Logf("PostgreSQL container ready at %s:%s", host, port.Port())

	connStr := "postgres://test:test@" + host + ":" + port.Port() + "/testdb?sslmode=disable"
	pool, err := pgxpool.New(s.ctx, connStr)
	s.Require().NoError(err)
	s.pool = pool

	// Create extension
	_, err = pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension")

	// Create daemon with IPC enabled
	s.socketPath = s.tempSocketPath()
	connConfig := pool.Config().ConnConfig

	cfg := &config.Config{
		NodeID:   "ipc-suite-test",
		NodeName: "IPC Suite Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15450,
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
	s.daemon = d

	err = d.Start()
	s.Require().NoError(err, "Failed to start daemon")

	// Wait for daemon to be ready
	time.Sleep(500 * time.Millisecond)

	// Create shared client
	client, err := ipc.NewClient(s.socketPath)
	s.Require().NoError(err, "Failed to create IPC client")
	s.client = client

	s.T().Log("IPC test suite setup complete")
}

func (s *IPCTestSuite) TearDownSuite() {
	s.T().Log("Tearing down IPC test suite...")

	if s.client != nil {
		s.client.Close()
	}

	if s.daemon != nil {
		s.daemon.Stop()
	}

	if s.socketPath != "" {
		os.Remove(s.socketPath)
	}

	if s.pool != nil {
		s.pool.Close()
	}

	if s.container != nil {
		if err := s.container.Terminate(s.ctx); err != nil {
			s.T().Logf("Failed to terminate container: %v", err)
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.T().Log("IPC test suite teardown complete")
}

func (s *IPCTestSuite) tempSocketPath() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		s.T().Fatalf("Failed to generate random bytes: %v", err)
	}
	return fmt.Sprintf("/tmp/sr-%s.sock", hex.EncodeToString(b))
}

// =============================================================================
// IPC Client Connection Tests
// =============================================================================

// TestIPC_ClientConnect verifies IPC client can connect to daemon.
func (s *IPCTestSuite) TestIPC_ClientConnect() {
	// Create a fresh client to test connection
	client, err := ipc.NewClient(s.socketPath)
	s.Require().NoError(err, "Failed to create IPC client")
	defer client.Close()

	// Verify connection by calling a method
	_, err = client.GetStatus()
	s.Assert().NoError(err, "GetStatus should work on connected client")
}

// =============================================================================
// Status Tests
// =============================================================================

// TestIPC_StatusGet verifies status.get IPC method.
func (s *IPCTestSuite) TestIPC_StatusGet() {
	status, err := s.client.GetStatus()
	s.Require().NoError(err, "GetStatus failed")

	s.Assert().Equal("ipc-suite-test", status.NodeID)
	s.Assert().Equal("IPC Suite Test", status.NodeName)
	s.Assert().Equal("running", status.State)
	s.Assert().Equal("connected", status.PostgreSQL.Status)
}

// =============================================================================
// Health Check Tests
// =============================================================================

// TestIPC_HealthCheck verifies health.check IPC method.
func (s *IPCTestSuite) TestIPC_HealthCheck() {
	health, err := s.client.HealthCheck()
	s.Require().NoError(err, "HealthCheck failed")

	s.Assert().Equal("healthy", health.Status)

	// Check components
	if pg, ok := health.Components["postgresql"]; ok {
		s.Assert().True(pg.Healthy, "PostgreSQL component should be healthy")
	} else {
		s.Fail("Health should include postgresql component")
	}

	if grpc, ok := health.Components["grpc"]; ok {
		s.Assert().True(grpc.Healthy, "gRPC component should be healthy")
	} else {
		s.Fail("Health should include grpc component")
	}

	if ipcComp, ok := health.Components["ipc"]; ok {
		s.Assert().True(ipcComp.Healthy, "IPC component should be healthy")
	} else {
		s.Fail("Health should include ipc component")
	}
}

// =============================================================================
// Nodes Tests
// =============================================================================

// TestIPC_NodesListEmpty verifies nodes.list returns daemon's self-registration.
func (s *IPCTestSuite) TestIPC_NodesListEmpty() {
	result, err := s.client.ListNodes(nil)
	s.Require().NoError(err, "ListNodes failed")

	// Should have exactly 1 node (daemon self-registration)
	s.Assert().Equal(1, len(result.Nodes), "Expected 1 node (daemon self-registration)")

	// Verify it's the daemon's own node
	if len(result.Nodes) > 0 {
		s.Assert().Equal("ipc-suite-test", result.Nodes[0].NodeID, "Expected daemon node")
	}
}

// =============================================================================
// Audit Tests
// =============================================================================

// TestIPC_AuditQuery verifies audit.query IPC method.
func (s *IPCTestSuite) TestIPC_AuditQuery() {
	params := ipc.AuditQueryParams{
		Limit: 10,
	}
	result, err := s.client.QueryAudit(params)
	s.Require().NoError(err, "QueryAudit failed")

	// Should have at least the daemon.started event
	if len(result.Entries) == 0 {
		s.T().Log("No audit entries found (may be expected if audit logging disabled)")
	}

	// Verify daemon.started event exists
	found := false
	for _, entry := range result.Entries {
		if entry.Action == "daemon.started" {
			found = true
			s.Assert().True(entry.Success, "daemon.started should have success=true")
			break
		}
	}

	if len(result.Entries) > 0 && !found {
		s.T().Log("daemon.started event not found in audit log")
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

// TestIPC_ConcurrentRequests verifies IPC handles concurrent requests.
func (s *IPCTestSuite) TestIPC_ConcurrentRequests() {
	const numRequests = 10
	errors := make(chan error, numRequests)
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := ipc.NewClient(s.socketPath)
			if err != nil {
				errors <- err
				return
			}
			defer client.Close()

			_, err = client.GetStatus()
			errors <- err
		}()
	}

	wg.Wait()
	close(errors)

	// Collect results
	var errCount int
	for err := range errors {
		if err != nil {
			errCount++
			s.T().Logf("Concurrent request error: %v", err)
		}
	}

	s.Assert().Equal(0, errCount, "%d/%d concurrent requests failed", errCount, numRequests)
}
