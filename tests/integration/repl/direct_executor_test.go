package repl_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/cmd/steep-repl/direct"
)

// =============================================================================
// Log Consumer for PostgreSQL logs
// =============================================================================

// TestLogConsumer captures PostgreSQL logs and outputs steep_repl messages to test log.
type TestLogConsumer struct {
	t      *testing.T
	filter string // only log lines containing this string (empty = all)
}

func (lc *TestLogConsumer) Accept(l testcontainers.Log) {
	content := string(l.Content)
	// Filter for steep_repl messages if filter is set
	if lc.filter == "" || strings.Contains(content, lc.filter) {
		// Trim trailing newline for cleaner output
		content = strings.TrimRight(content, "\n")
		if content != "" {
			lc.t.Logf("[PG %s] %s", l.LogType, content)
		}
	}
}

// =============================================================================
// Direct Executor Test Suite
// =============================================================================

// DirectExecutorTestSuite tests the direct executor functionality.
// This suite tests CLI commands connecting directly to PostgreSQL
// via the steep_repl extension without requiring the daemon.
type DirectExecutorTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	connStr   string
	host      string
	port      string
}

func TestDirectExecutorSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(DirectExecutorTestSuite))
}

func (s *DirectExecutorTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	// Create log consumer to capture steep_repl messages from PostgreSQL
	logConsumer := &TestLogConsumer{
		t:      s.T(),
		filter: "steep_repl", // Only show steep_repl messages
	}

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
			WithStartupTimeout(90 * time.Second),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{logConsumer},
		},
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start PostgreSQL container")
	s.container = container

	host, err := container.Host(s.ctx)
	s.Require().NoError(err)
	s.host = host

	port, err := container.MappedPort(s.ctx, "5432")
	s.Require().NoError(err)
	s.port = port.Port()

	s.connStr = fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, port.Port())

	s.T().Log("DirectExecutorTestSuite: Container ready")
}

func (s *DirectExecutorTestSuite) TearDownSuite() {
	if s.container != nil {
		_ = s.container.Terminate(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// =============================================================================
// Executor Creation Tests
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_CreateWithConnString() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err, "Should create executor with connection string")
	s.Require().NotNil(executor)
	defer executor.Close()

	// Verify connection is working
	s.Assert().NotEmpty(executor.ExtensionVersion(), "Should have extension version")
	s.Assert().NotEmpty(executor.PostgresVersion(), "Should have PostgreSQL version")

	s.T().Logf("Extension version: %s", executor.ExtensionVersion())
	s.T().Logf("PostgreSQL version: %s", executor.PostgresVersion())
}

func (s *DirectExecutorTestSuite) TestExecutor_CreateFromEnv() {
	ctx := s.ctx

	// Set environment variables
	s.T().Setenv("PGHOST", s.host)
	s.T().Setenv("PGPORT", s.port)
	s.T().Setenv("PGDATABASE", "testdb")
	s.T().Setenv("PGUSER", "test")
	s.T().Setenv("PGPASSWORD", "test")
	s.T().Setenv("PGSSLMODE", "disable")

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		// No ConnString - should use environment variables
	})
	s.Require().NoError(err, "Should create executor from environment")
	s.Require().NotNil(executor)
	defer executor.Close()

	// Verify connection is working
	s.Assert().NotEmpty(executor.ExtensionVersion())
}

func (s *DirectExecutorTestSuite) TestExecutor_InvalidConnString() {
	ctx := s.ctx

	_, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: "postgres://invalid:invalid@localhost:9999/nonexistent?sslmode=disable",
		Timeout:    5 * time.Second,
	})
	s.Require().Error(err, "Should fail with invalid connection string")
}

// =============================================================================
// Health Check Tests
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_Health() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	health, err := executor.Health(ctx)
	s.Require().NoError(err, "Health check should succeed")
	s.Require().NotNil(health)

	// Log health status for debugging
	s.T().Logf("Health: status=%s, ext=%s, bgworker=%v, shmem=%v",
		health.Status, health.ExtensionVersion,
		health.BackgroundWorkerRunning, health.SharedMemoryAvailable)

	// Verify health fields
	s.Assert().NotEmpty(health.ExtensionVersion, "Extension version should be set")
	s.Assert().NotEmpty(health.PGVersion, "PostgreSQL version should be set")
	s.Assert().Contains(health.PGVersion, "PostgreSQL", "PG version should contain 'PostgreSQL'")

	// These are hard requirements - extension must be fully operational
	s.Require().True(health.SharedMemoryAvailable,
		"Shared memory must be available (extension must be in shared_preload_libraries)")
	s.Require().True(health.BackgroundWorkerRunning,
		"Background worker must be running (static worker should be registered at startup)")
	s.Require().Equal("healthy", health.Status,
		"Health status must be 'healthy', got '%s'", health.Status)
}

func (s *DirectExecutorTestSuite) TestExecutor_ExtensionVersion() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	version := executor.ExtensionVersion()
	s.Assert().NotEmpty(version, "Extension version should not be empty")

	// Version should be a valid semver-like format
	s.Assert().Regexp(`^\d+\.\d+\.\d+`, version, "Version should start with x.y.z")
}

func (s *DirectExecutorTestSuite) TestExecutor_PostgresVersion() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	version := executor.PostgresVersion()
	s.Assert().NotEmpty(version, "PostgreSQL version should not be empty")
	s.Assert().Contains(version, "18", "Should be PostgreSQL 18")
}

// =============================================================================
// Node Management Tests (T019-T021)
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_RegisterNode() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register a new node
	nodeInfo, err := executor.RegisterNode(ctx, "test-node-1", "Test Node", "localhost", 5432, 50)
	s.Require().NoError(err, "RegisterNode should succeed")
	s.Require().NotNil(nodeInfo)

	s.Assert().Equal("test-node-1", nodeInfo.NodeID)
	s.Assert().Equal("Test Node", nodeInfo.NodeName)
	s.Assert().Equal("localhost", nodeInfo.Host)
	s.Assert().Equal(5432, nodeInfo.Port)
	s.Assert().Equal(50, nodeInfo.Priority)
	s.Assert().Equal("healthy", nodeInfo.Status)

	s.T().Logf("Registered node: %s (%s)", nodeInfo.NodeID, nodeInfo.NodeName)
}

func (s *DirectExecutorTestSuite) TestExecutor_RegisterNode_Update() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register node
	_, err = executor.RegisterNode(ctx, "update-node", "Initial Name", "localhost", 5432, 50)
	s.Require().NoError(err)

	// Update the same node
	updatedInfo, err := executor.RegisterNode(ctx, "update-node", "Updated Name", "127.0.0.1", 5433, 75)
	s.Require().NoError(err)

	// Verify update took effect
	s.Assert().Equal("update-node", updatedInfo.NodeID)
	s.Assert().Equal("Updated Name", updatedInfo.NodeName)
	s.Assert().Equal("127.0.0.1", updatedInfo.Host)
	s.Assert().Equal(5433, updatedInfo.Port)
	s.Assert().Equal(75, updatedInfo.Priority)
}

func (s *DirectExecutorTestSuite) TestExecutor_Heartbeat() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register a node first
	_, err = executor.RegisterNode(ctx, "heartbeat-node", "Heartbeat Test", "localhost", 5432, 50)
	s.Require().NoError(err)

	// Send heartbeat
	err = executor.Heartbeat(ctx, "heartbeat-node")
	s.Require().NoError(err, "Heartbeat should succeed for existing node")
}

func (s *DirectExecutorTestSuite) TestExecutor_Heartbeat_NonexistentNode() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Try to heartbeat non-existent node
	err = executor.Heartbeat(ctx, "nonexistent-node")
	s.Require().Error(err, "Heartbeat should fail for non-existent node")
	s.Assert().Contains(err.Error(), "not found")
}

func (s *DirectExecutorTestSuite) TestExecutor_NodeStatus_SingleNode() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register a node
	_, err = executor.RegisterNode(ctx, "status-node", "Status Test", "localhost", 5432, 50)
	s.Require().NoError(err)

	// Get status of specific node
	nodes, err := executor.NodeStatus(ctx, "status-node")
	s.Require().NoError(err)
	s.Require().Len(nodes, 1, "Should return exactly one node")

	node := nodes[0]
	s.Assert().Equal("status-node", node.NodeID)
	s.Assert().Equal("Status Test", node.NodeName)
	s.Assert().NotEmpty(node.Status)
}

func (s *DirectExecutorTestSuite) TestExecutor_NodeStatus_AllNodes() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register multiple nodes
	_, err = executor.RegisterNode(ctx, "multi-node-1", "Multi Node 1", "host1", 5432, 50)
	s.Require().NoError(err)
	_, err = executor.RegisterNode(ctx, "multi-node-2", "Multi Node 2", "host2", 5432, 60)
	s.Require().NoError(err)

	// Get status of all nodes (empty string = all)
	nodes, err := executor.NodeStatus(ctx, "")
	s.Require().NoError(err)
	s.Assert().GreaterOrEqual(len(nodes), 2, "Should return at least 2 nodes")

	// Verify our nodes are in the list
	foundNode1 := false
	foundNode2 := false
	for _, n := range nodes {
		if n.NodeID == "multi-node-1" {
			foundNode1 = true
		}
		if n.NodeID == "multi-node-2" {
			foundNode2 = true
		}
	}
	s.Assert().True(foundNode1, "Should find multi-node-1")
	s.Assert().True(foundNode2, "Should find multi-node-2")

	s.T().Logf("Found %d nodes total", len(nodes))
}

func (s *DirectExecutorTestSuite) TestExecutor_NodeStatus_WithHealthCheck() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Register a node
	_, err = executor.RegisterNode(ctx, "health-check-node", "Health Check Test", "localhost", 5432, 50)
	s.Require().NoError(err)

	// Send heartbeat
	err = executor.Heartbeat(ctx, "health-check-node")
	s.Require().NoError(err)

	// Get status and verify IsHealthy
	nodes, err := executor.NodeStatus(ctx, "health-check-node")
	s.Require().NoError(err)
	s.Require().Len(nodes, 1)

	// Node should be healthy since we just sent heartbeat
	s.Assert().True(nodes[0].IsHealthy, "Node should be healthy after heartbeat")
}

// =============================================================================
// Client Access Tests
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_Client() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Get the underlying client
	client := executor.Client()
	s.Require().NotNil(client, "Should have underlying client")

	// Verify client is functional
	s.Assert().True(client.IsConnected(), "Client should be connected")
	s.Assert().True(client.ExtensionInstalled(), "Extension should be installed")
}

func (s *DirectExecutorTestSuite) TestExecutor_BackgroundWorkerStatus() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Check background worker status
	// The extension is loaded via shared_preload_libraries in our Docker image,
	// so the static background worker MUST be running.
	active := executor.BackgroundWorkerActive()
	s.T().Logf("Background worker active: %v", active)

	s.Require().True(active,
		"Background worker must be running (extension loaded via shared_preload_libraries)")
}

// =============================================================================
// Close and Cleanup Tests
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_Close() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)

	// Verify connected
	client := executor.Client()
	s.Assert().True(client.IsConnected())

	// Close executor
	executor.Close()

	// Verify disconnected
	s.Assert().False(client.IsConnected(), "Client should be disconnected after Close")
}

func (s *DirectExecutorTestSuite) TestExecutor_DoubleClose() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
	})
	s.Require().NoError(err)

	// Double close should not panic
	executor.Close()
	s.NotPanics(func() {
		executor.Close()
	}, "Double close should not panic")
}

// =============================================================================
// Timeout and Retry Tests
// =============================================================================

func (s *DirectExecutorTestSuite) TestExecutor_WithCustomTimeout() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString: s.connStr,
		Timeout:    1 * time.Hour,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// Verify executor works with custom timeout
	s.Assert().NotEmpty(executor.ExtensionVersion())
}

func (s *DirectExecutorTestSuite) TestExecutor_WithProgress() {
	ctx := s.ctx

	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString:   s.connStr,
		ShowProgress: true,
	})
	s.Require().NoError(err)
	defer executor.Close()

	// ShowProgress is a configuration option - verify executor works
	s.Assert().NotEmpty(executor.ExtensionVersion())
}
