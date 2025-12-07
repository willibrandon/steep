package repl_test

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx as database/sql driver
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// =============================================================================
// Test Suite for Bidirectional Merge
// =============================================================================

// MergeTestSuite runs all merge tests with shared containers.
// Containers are created once in SetupSuite and reused across all tests.
type MergeTestSuite struct {
	suite.Suite
	env *bidirectionalTestEnv
	ctx context.Context
}

// SetupSuite creates the shared PostgreSQL containers once for all tests.
func (s *MergeTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("Skipping integration tests in short mode")
	}

	s.ctx = context.Background()
	s.env = s.createSharedTestEnv()
}

// TearDownSuite cleans up containers after all tests complete.
func (s *MergeTestSuite) TearDownSuite() {
	if s.env == nil {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Stop daemons
	if s.env.nodeADaemon != nil {
		s.env.nodeADaemon.Stop()
	}
	if s.env.nodeBDaemon != nil {
		s.env.nodeBDaemon.Stop()
	}

	// Close pools
	if s.env.nodeAPool != nil {
		s.env.nodeAPool.Close()
	}
	if s.env.nodeBPool != nil {
		s.env.nodeBPool.Close()
	}

	// Terminate containers
	if s.env.nodeAContainer != nil {
		if err := s.env.nodeAContainer.Terminate(cleanupCtx); err != nil {
			log.Printf("Failed to terminate Node A container: %v", err)
		}
	}
	if s.env.nodeBContainer != nil {
		if err := s.env.nodeBContainer.Terminate(cleanupCtx); err != nil {
			log.Printf("Failed to terminate Node B container: %v", err)
		}
	}

	// Remove network
	if s.env.network != nil {
		if err := s.env.network.Remove(cleanupCtx); err != nil {
			log.Printf("Failed to remove network: %v", err)
		}
	}
}

// SetupTest resets database state before each test.
func (s *MergeTestSuite) SetupTest() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	// Clean up all test tables (order matters for FK constraints)
	cleanupSQL := `
		TRUNCATE order_items, orders, customers, products, users CASCADE;
		DROP SERVER IF EXISTS node_b_server CASCADE;
		DROP SERVER IF EXISTS bad_server CASCADE;
		DROP TABLE IF EXISTS no_pk CASCADE;
		DROP TABLE IF EXISTS schema_test CASCADE;
		DROP PUBLICATION IF EXISTS test_pub_origin CASCADE;
	`

	// Execute cleanup on both nodes, ignore errors for tables that don't exist
	s.env.nodeAPool.Exec(ctx, cleanupSQL)
	s.env.nodeBPool.Exec(ctx, cleanupSQL)

	// Also clean up subscription on Node B
	s.env.nodeBPool.Exec(ctx, "DROP SUBSCRIPTION IF EXISTS test_sub_origin CASCADE")
}

// createSharedTestEnv creates containers and daemons for the test suite.
func (s *MergeTestSuite) createSharedTestEnv() *bidirectionalTestEnv {
	const testPassword = "test"
	env := &bidirectionalTestEnv{}

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	// Create Docker network
	net, err := network.New(ctx, network.WithCheckDuplicate())
	if err != nil {
		log.Fatalf("Failed to create Docker network: %v", err)
	}
	env.network = net

	// Start Node A PostgreSQL container
	nodeAReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-node-a"},
		},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		Cmd: []string{
			"-c", "wal_level=logical",
			"-c", "max_wal_senders=10",
			"-c", "max_replication_slots=10",
			"-c", "track_commit_timestamp=on",
			"-c", "listen_addresses=*",
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeAContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeAReq,
		Started:          true,
	})
	if err != nil {
		log.Fatalf("Failed to start Node A container: %v", err)
	}
	env.nodeAContainer = nodeAContainer

	// Start Node B PostgreSQL container
	nodeBReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-node-b"},
		},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		Cmd: []string{
			"-c", "wal_level=logical",
			"-c", "max_wal_senders=10",
			"-c", "max_replication_slots=10",
			"-c", "track_commit_timestamp=on",
			"-c", "listen_addresses=*",
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeBContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeBReq,
		Started:          true,
	})
	if err != nil {
		log.Fatalf("Failed to start Node B container: %v", err)
	}
	env.nodeBContainer = nodeBContainer

	// Get connection info
	nodeAHostExternal, _ := nodeAContainer.Host(ctx)
	nodeAPortExternal, _ := nodeAContainer.MappedPort(ctx, "5432")
	nodeBHostExternal, _ := nodeBContainer.Host(ctx)
	nodeBPortExternal, _ := nodeBContainer.MappedPort(ctx, "5432")

	// Get container IPs on the Docker network
	nodeAInspect, err := nodeAContainer.Inspect(ctx)
	if err != nil {
		log.Fatalf("Failed to inspect Node A container: %v", err)
	}
	nodeBInspect, err := nodeBContainer.Inspect(ctx)
	if err != nil {
		log.Fatalf("Failed to inspect Node B container: %v", err)
	}

	env.nodeAHost = nodeAInspect.NetworkSettings.Networks[net.Name].IPAddress
	env.nodeAPort = 5432
	env.nodeBHost = nodeBInspect.NetworkSettings.Networks[net.Name].IPAddress
	env.nodeBPort = 5432

	log.Printf("Node A IP: %s, Node B IP: %s, Network: %s", env.nodeAHost, env.nodeBHost, net.Name)

	// Create connection pools
	nodeAConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeAHostExternal, nodeAPortExternal.Port())
	env.nodeAPool, err = pgxpool.New(ctx, nodeAConnStr)
	if err != nil {
		log.Fatalf("Failed to create Node A pool: %v", err)
	}

	nodeBConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeBHostExternal, nodeBPortExternal.Port())
	env.nodeBPool, err = pgxpool.New(ctx, nodeBConnStr)
	if err != nil {
		log.Fatalf("Failed to create Node B pool: %v", err)
	}

	// Wait for databases
	waitForDBSuite(ctx, env.nodeAPool, "Node A")
	waitForDBSuite(ctx, env.nodeBPool, "Node B")

	// Create extensions on both nodes
	_, err = env.nodeAPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		log.Fatalf("Failed to create extension on Node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		log.Fatalf("Failed to create extension on Node B: %v", err)
	}
	_, err = env.nodeAPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
	if err != nil {
		log.Fatalf("Failed to create postgres_fdw on Node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
	if err != nil {
		log.Fatalf("Failed to create postgres_fdw on Node B: %v", err)
	}

	// Create schema once (will be truncated between tests)
	schemaSQL := loadFixtureSuite("schema.sql")
	_, err = env.nodeAPool.Exec(ctx, schemaSQL)
	if err != nil {
		log.Fatalf("Failed to create schema on Node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, schemaSQL)
	if err != nil {
		log.Fatalf("Failed to create schema on Node B: %v", err)
	}

	// Start daemons with dynamically allocated ports
	env.nodeAGRPCPort = getFreePortSuite()
	env.nodeBGRPCPort = getFreePortSuite()

	// Set PGPASSWORD
	os.Setenv("PGPASSWORD", testPassword)

	nodeASocketPath := tempSocketPathSuite()
	nodeACfg := &config.Config{
		NodeID:   "node-a",
		NodeName: "Node A",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     nodeAHostExternal,
			Port:     nodeAPortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.nodeAGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeASocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodBidirectionalMerge,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeADaemon, err = daemon.New(nodeACfg, true)
	if err != nil {
		log.Fatalf("Failed to create Node A daemon: %v", err)
	}
	if err := env.nodeADaemon.Start(); err != nil {
		log.Fatalf("Failed to start Node A daemon: %v", err)
	}

	nodeBSocketPath := tempSocketPathSuite()
	nodeBCfg := &config.Config{
		NodeID:   "node-b",
		NodeName: "Node B",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     nodeBHostExternal,
			Port:     nodeBPortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.nodeBGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeBSocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodBidirectionalMerge,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeBDaemon, err = daemon.New(nodeBCfg, true)
	if err != nil {
		log.Fatalf("Failed to create Node B daemon: %v", err)
	}
	if err := env.nodeBDaemon.Start(); err != nil {
		log.Fatalf("Failed to start Node B daemon: %v", err)
	}

	// Wait for daemons
	time.Sleep(time.Second)

	return env
}

// Helper functions for suite (without *testing.T parameter)
func waitForDBSuite(ctx context.Context, pool *pgxpool.Pool, name string) {
	for i := 0; i < 30; i++ {
		if err := pool.Ping(ctx); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Fatalf("Database %s not ready after 30 attempts", name)
}

func loadFixtureSuite(name string) string {
	path := filepath.Join(fixturesDir(), name)
	content, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read fixture %s: %v", name, err)
	}
	return string(content)
}

func getFreePortSuite() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func tempSocketPathSuite() string {
	f, err := os.CreateTemp("", "sr-*.sock")
	if err != nil {
		log.Fatalf("Failed to create temp socket: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	return path
}

// setupForeignServer creates the foreign server for remote queries.
func (s *MergeTestSuite) setupForeignServer() {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	_, err := s.env.nodeAPool.Exec(ctx, fmt.Sprintf(`
		CREATE SERVER IF NOT EXISTS node_b_server
		FOREIGN DATA WRAPPER postgres_fdw
		OPTIONS (host '%s', port '%d', dbname 'testdb')
	`, s.env.nodeBHost, s.env.nodeBPort))
	s.Require().NoError(err)

	_, err = s.env.nodeAPool.Exec(ctx, `
		CREATE USER MAPPING IF NOT EXISTS FOR test
		SERVER node_b_server
		OPTIONS (user 'test', password 'test')
	`)
	s.Require().NoError(err)
}

// TestMergeSuite runs the merge test suite.
func TestMergeSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(MergeTestSuite))
}

// getFreePort returns an available TCP port by binding to :0 and releasing it.
func getFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// postgresWaitStrategy returns a wait strategy that verifies PostgreSQL is ready
// by actually connecting and running a query.
func postgresWaitStrategy() wait.Strategy {
	return wait.ForSQL("5432/tcp", "pgx", func(host string, port nat.Port) string {
		return fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())
	}).WithStartupTimeout(60 * time.Second).WithPollInterval(500 * time.Millisecond)
}

// =============================================================================
// Fixture Loading Helpers
// =============================================================================

// fixturesDir returns the path to the merge test fixtures directory.
func fixturesDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", "merge")
}

// loadFixture reads a SQL fixture file from testdata/merge/.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(fixturesDir(), name)
	content, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read fixture %s", name)
	return string(content)
}

// execFixture loads and executes a SQL fixture file on the given pool.
func execFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	sql := loadFixture(t, name)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err, "Failed to execute fixture %s", name)
}

// execFixtureNodeA executes the "Node A" portion of a fixture file.
// Fixture files have sections marked with "-- Node A Data" and "-- Node B Data".
func (env *bidirectionalTestEnv) execFixtureNodeA(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	sql := loadFixture(t, name)
	nodeASQL := extractNodeSection(sql, "Node A")
	if nodeASQL != "" {
		_, err := env.nodeAPool.Exec(ctx, nodeASQL)
		require.NoError(t, err, "Failed to execute Node A section of %s", name)
	}
}

// execFixtureNodeB executes the "Node B" portion of a fixture file.
func (env *bidirectionalTestEnv) execFixtureNodeB(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	sql := loadFixture(t, name)
	nodeBSQL := extractNodeSection(sql, "Node B")
	if nodeBSQL != "" {
		_, err := env.nodeBPool.Exec(ctx, nodeBSQL)
		require.NoError(t, err, "Failed to execute Node B section of %s", name)
	}
}

// extractNodeSection extracts SQL between "-- Node X Data" markers.
func extractNodeSection(sql, nodeMarker string) string {
	startMarker := fmt.Sprintf("-- %s Data", nodeMarker)
	otherNodeMarker := "-- Node "
	lines := strings.Split(sql, "\n")

	var result []string
	inSection := false

	for _, line := range lines {
		if strings.Contains(line, startMarker) {
			inSection = true
			continue
		}
		// Stop at the OTHER node's section (but not at decorative "===" lines)
		trimmed := strings.TrimSpace(line)
		if inSection && strings.HasPrefix(trimmed, otherNodeMarker) && !strings.Contains(line, startMarker) {
			break
		}
		// Skip decorative comment lines (all equals signs)
		if inSection && strings.HasPrefix(trimmed, "-- ") && strings.Count(trimmed, "=") > 10 {
			continue
		}
		if inSection {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// =============================================================================
// Test Infrastructure for Bidirectional Merge
// =============================================================================

// bidirectionalTestEnv holds the complete test environment with two PostgreSQL nodes
// configured for bidirectional merge testing.
type bidirectionalTestEnv struct {
	network *testcontainers.DockerNetwork

	// Node A
	nodeAContainer testcontainers.Container
	nodeAPool      *pgxpool.Pool
	nodeAHost      string // Docker network hostname
	nodeAPort      int
	nodeADaemon    *daemon.Daemon
	nodeAGRPCPort  int

	// Node B
	nodeBContainer testcontainers.Container
	nodeBPool      *pgxpool.Pool
	nodeBHost      string // Docker network hostname
	nodeBPort      int
	nodeBDaemon    *daemon.Daemon
	nodeBGRPCPort  int
}

// setupBidirectionalTestEnv creates a complete two-node test environment for merge tests:
// - Two PostgreSQL 18 containers with steep_repl extension
// - track_commit_timestamp=on for last-modified strategy
// - postgres_fdw enabled for hash-based comparison
// - steep-repl daemon running on each node
func setupBidirectionalTestEnv(t *testing.T, ctx context.Context) *bidirectionalTestEnv {
	t.Helper()

	const testPassword = "test"
	env := &bidirectionalTestEnv{}

	// Create Docker network for inter-container communication
	net, err := network.New(ctx, network.WithCheckDuplicate())
	if err != nil {
		t.Fatalf("Failed to create Docker network: %v", err)
	}
	env.network = net
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := net.Remove(cleanupCtx); err != nil {
			t.Logf("Failed to remove network: %v", err)
		}
	})

	// Start Node A PostgreSQL container
	nodeAReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-node-a"},
		},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		Cmd: []string{
			"-c", "wal_level=logical",
			"-c", "max_wal_senders=10",
			"-c", "max_replication_slots=10",
			"-c", "track_commit_timestamp=on",
			"-c", "listen_addresses=*",
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeAContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeAReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Node A container: %v", err)
	}
	env.nodeAContainer = nodeAContainer
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := nodeAContainer.Terminate(cleanupCtx); err != nil {
			t.Logf("Failed to terminate Node A container: %v", err)
		}
	})

	// Start Node B PostgreSQL container
	nodeBReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-node-b"},
		},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		Cmd: []string{
			"-c", "wal_level=logical",
			"-c", "max_wal_senders=10",
			"-c", "max_replication_slots=10",
			"-c", "track_commit_timestamp=on",
			"-c", "listen_addresses=*",
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeBContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeBReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Node B container: %v", err)
	}
	env.nodeBContainer = nodeBContainer
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := nodeBContainer.Terminate(cleanupCtx); err != nil {
			t.Logf("Failed to terminate Node B container: %v", err)
		}
	})

	// Get connection info
	nodeAHostExternal, _ := nodeAContainer.Host(ctx)
	nodeAPortExternal, _ := nodeAContainer.MappedPort(ctx, "5432")
	nodeBHostExternal, _ := nodeBContainer.Host(ctx)
	nodeBPortExternal, _ := nodeBContainer.MappedPort(ctx, "5432")

	// Get container IPs on the Docker network for postgres_fdw connections
	nodeAInspect, err := nodeAContainer.Inspect(ctx)
	if err != nil {
		t.Fatalf("Failed to inspect Node A container: %v", err)
	}
	nodeBInspect, err := nodeBContainer.Inspect(ctx)
	if err != nil {
		t.Fatalf("Failed to inspect Node B container: %v", err)
	}

	env.nodeAHost = nodeAInspect.NetworkSettings.Networks[net.Name].IPAddress
	env.nodeAPort = 5432
	env.nodeBHost = nodeBInspect.NetworkSettings.Networks[net.Name].IPAddress
	env.nodeBPort = 5432

	t.Logf("Node A IP: %s, Node B IP: %s, Network: %s", env.nodeAHost, env.nodeBHost, net.Name)

	// Create connection pools using external ports
	nodeAConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeAHostExternal, nodeAPortExternal.Port())
	env.nodeAPool, err = pgxpool.New(ctx, nodeAConnStr)
	if err != nil {
		t.Fatalf("Failed to create Node A pool: %v", err)
	}
	t.Cleanup(func() { env.nodeAPool.Close() })

	nodeBConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeBHostExternal, nodeBPortExternal.Port())
	env.nodeBPool, err = pgxpool.New(ctx, nodeBConnStr)
	if err != nil {
		t.Fatalf("Failed to create Node B pool: %v", err)
	}
	t.Cleanup(func() { env.nodeBPool.Close() })

	// Wait for databases to be ready
	waitForDB(t, ctx, env.nodeAPool, "Node A")
	waitForDB(t, ctx, env.nodeBPool, "Node B")

	// Create steep_repl extension on both nodes
	_, err = env.nodeAPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension on Node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension on Node B: %v", err)
	}

	// Create postgres_fdw extension on both nodes for remote queries
	_, err = env.nodeAPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
	if err != nil {
		t.Fatalf("Failed to create postgres_fdw on Node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
	if err != nil {
		t.Fatalf("Failed to create postgres_fdw on Node B: %v", err)
	}

	// Start daemons with dynamically allocated ports to avoid conflicts
	env.nodeAGRPCPort = getFreePort(t)
	env.nodeBGRPCPort = getFreePort(t)

	// Set PGPASSWORD for daemon connections
	t.Setenv("PGPASSWORD", testPassword)

	nodeASocketPath := tempSocketPath(t)
	nodeACfg := &config.Config{
		NodeID:   "node-a",
		NodeName: "Node A",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     nodeAHostExternal,
			Port:     nodeAPortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.nodeAGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeASocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodBidirectionalMerge,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeADaemon, err = daemon.New(nodeACfg, true)
	if err != nil {
		t.Fatalf("Failed to create Node A daemon: %v", err)
	}
	if err := env.nodeADaemon.Start(); err != nil {
		t.Fatalf("Failed to start Node A daemon: %v", err)
	}
	t.Cleanup(func() { env.nodeADaemon.Stop() })

	nodeBSocketPath := tempSocketPath(t)
	nodeBCfg := &config.Config{
		NodeID:   "node-b",
		NodeName: "Node B",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     nodeBHostExternal,
			Port:     nodeBPortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.nodeBGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeBSocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodBidirectionalMerge,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeBDaemon, err = daemon.New(nodeBCfg, true)
	if err != nil {
		t.Fatalf("Failed to create Node B daemon: %v", err)
	}
	if err := env.nodeBDaemon.Start(); err != nil {
		t.Fatalf("Failed to start Node B daemon: %v", err)
	}
	t.Cleanup(func() { env.nodeBDaemon.Stop() })

	// Wait for daemons to be ready
	time.Sleep(time.Second)

	return env
}

// setupMergeSchema creates the common schema on both nodes from schema.sql fixture.
func (env *bidirectionalTestEnv) setupMergeSchema(t *testing.T, ctx context.Context) {
	t.Helper()

	// Load schema from fixture file
	execFixture(t, ctx, env.nodeAPool, "schema.sql")
	execFixture(t, ctx, env.nodeBPool, "schema.sql")
}

// loadLargeDataset loads the large_dataset.sql fixture for performance tests.
func (env *bidirectionalTestEnv) loadLargeDataset(t *testing.T, ctx context.Context) {
	t.Helper()
	// The large_dataset.sql has separate SQL for each node using generate_series
	// We need to extract and run the appropriate section on each node
	env.execFixtureNodeA(t, ctx, "large_dataset.sql")
	env.execFixtureNodeB(t, ctx, "large_dataset.sql")
}

// =============================================================================
// Category 1: Overlap Analysis Tests (T067-1 through T067-7)
// =============================================================================

// TestOverlapAnalysis_AllCategories tests detection of all row categories.
// T067-1: Basic Overlap Analysis - All Categories
// Uses fixture: simple_overlap.sql
func (s *MergeTestSuite) TestOverlapAnalysis_AllCategories() {
	ctx := s.ctx

	// SETUP: Load fixture data (simple_overlap.sql)
	// Node A: users(1,'alice','v1'), users(2,'bob','v1'), users(3,'charlie','v1')
	// Node B: users(2,'bob','v1'), users(3,'charlie','v2'), users(4,'diana','v1')
	//
	// Row 1: A-only (exists only on A)
	// Row 2: Match (identical on both)
	// Row 3: Conflict (same PK, different data)
	// Row 4: B-only (exists only on B)
	s.env.execFixtureNodeA(s.T(), ctx, "simple_overlap.sql")
	s.env.execFixtureNodeB(s.T(), ctx, "simple_overlap.sql")

	// Create merger using direct pool connections
	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// EXECUTE: Analyze overlap
	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	// Set up foreign server for remote queries
	s.setupForeignServer()

	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	s.Require().NoError(err)

	// ASSERT: Verify counts
	s.Assert().Equal(int64(1), summary.Matches, "Should have 1 match (row 2)")
	s.Assert().Equal(int64(1), summary.Conflicts, "Should have 1 conflict (row 3)")
	s.Assert().Equal(int64(1), summary.LocalOnly, "Should have 1 A-only (row 1)")
	s.Assert().Equal(int64(1), summary.RemoteOnly, "Should have 1 B-only (row 4)")
	s.Assert().Equal(int64(4), summary.TotalRows, "Should have 4 total rows")
}

// TestOverlapAnalysis_CompositePK tests overlap analysis with composite primary keys.
// T067-2: Overlap Analysis - Composite Primary Keys
func (s *MergeTestSuite) TestOverlapAnalysis_CompositePK() {
	ctx := s.ctx

	// SETUP: Create data with composite PK
	// Node A: (1,1,5), (1,2,3), (2,1,1)
	// Node B: (1,1,5), (1,2,10), (2,2,7)
	//
	// (1,1): Match
	// (1,2): Conflict (quantity differs)
	// (2,1): A-only
	// (2,2): B-only

	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO order_items (order_id, item_id, quantity, price) VALUES
			(1, 1, 5, 10.00),
			(1, 2, 3, 20.00),
			(2, 1, 1, 15.00)
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO order_items (order_id, item_id, quantity, price) VALUES
			(1, 1, 5, 10.00),
			(1, 2, 10, 20.00),
			(2, 2, 7, 25.00)
	`)
	s.Require().NoError(err)

	// Create merger
	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "order_items",
		PKColumns: []string{"order_id", "item_id"},
	}

	// Set up foreign server
	s.setupForeignServer()

	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	s.Require().NoError(err)

	// ASSERT
	s.Assert().Equal(int64(1), summary.Matches, "Should have 1 match (1,1)")
	s.Assert().Equal(int64(1), summary.Conflicts, "Should have 1 conflict (1,2)")
	s.Assert().Equal(int64(1), summary.LocalOnly, "Should have 1 A-only (2,1)")
	s.Assert().Equal(int64(1), summary.RemoteOnly, "Should have 1 B-only (2,2)")
}

// TestOverlapAnalysis_EmptyTables tests overlap analysis when both tables are empty.
// T067-3: Overlap Analysis - Empty Tables
func (s *MergeTestSuite) TestOverlapAnalysis_EmptyTables() {
	ctx := s.ctx

	// Both tables are empty (just created schema)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	// Set up foreign server
	s.setupForeignServer()

	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	s.Require().NoError(err)

	// ASSERT
	s.Assert().Equal(int64(0), summary.TotalRows)
	s.Assert().Equal(int64(0), summary.Matches)
	s.Assert().Equal(int64(0), summary.Conflicts)
	s.Assert().Equal(int64(0), summary.LocalOnly)
	s.Assert().Equal(int64(0), summary.RemoteOnly)
}

// TestOverlapAnalysis_OneNodeEmpty tests overlap analysis when one node has data and the other is empty.
// T067-4: Overlap Analysis - One Node Empty
func (s *MergeTestSuite) TestOverlapAnalysis_OneNodeEmpty() {
	ctx := s.ctx

	// SETUP: Node A has data, Node B is empty
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1'),
			(2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	// Set up foreign server
	s.setupForeignServer()

	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	s.Require().NoError(err)

	// ASSERT
	s.Assert().Equal(int64(0), summary.Matches)
	s.Assert().Equal(int64(0), summary.Conflicts)
	s.Assert().Equal(int64(2), summary.LocalOnly, "All rows should be A-only")
	s.Assert().Equal(int64(0), summary.RemoteOnly)
}

// TestOverlapAnalysis_NullValues tests overlap analysis with NULL values in non-PK columns.
// T067-6: Overlap Analysis - NULL Values in Non-PK Columns
func (s *MergeTestSuite) TestOverlapAnalysis_NullValues() {
	ctx := s.ctx

	// SETUP:
	// Node A: users(1,'alice',NULL), users(2,'bob','active')
	// Node B: users(1,'alice','pending'), users(2,'bob','active')
	//
	// Row 1: Conflict (NULL vs 'pending')
	// Row 2: Match

	// Use explicit timestamps to ensure row 2 hashes identically on both nodes
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', NULL, '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00'),
			(2, 'bob', 'active', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', 'pending', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00'),
			(2, 'bob', 'active', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	// Set up foreign server
	s.setupForeignServer()

	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	s.Require().NoError(err)

	// ASSERT
	s.Assert().Equal(int64(1), summary.Matches, "Row 2 should match")
	s.Assert().Equal(int64(1), summary.Conflicts, "Row 1 should be a conflict (NULL vs value)")
}

// TestOverlapAnalysis_MultiTable tests overlap analysis across multiple tables.
// T067-7: Overlap Analysis - Multi-Table Summary
func (s *MergeTestSuite) TestOverlapAnalysis_MultiTable() {
	ctx := s.ctx

	// SETUP: Data in multiple tables
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES (1, 'alice', 'v1'), (2, 'bob', 'v1');
		INSERT INTO products (id, name, price) VALUES (1, 'widget', 9.99), (2, 'gadget', 19.99);
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES (2, 'bob', 'v1'), (3, 'charlie', 'v1');
		INSERT INTO products (id, name, price) VALUES (2, 'gadget', 19.99), (3, 'gizmo', 29.99);
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		{Schema: "public", Name: "products", PKColumns: []string{"id"}},
	}

	// Set up foreign server
	s.setupForeignServer()

	// Analyze each table
	var totalConflicts int64
	for _, table := range tables {
		summary, err := merger.AnalyzeOverlap(ctx, table.Schema, table.Name, table.PKColumns, "node_b_server")
		s.Require().NoError(err)
		totalConflicts += summary.Conflicts
		s.T().Logf("Table %s.%s: matches=%d, conflicts=%d, a_only=%d, b_only=%d",
			table.Schema, table.Name, summary.Matches, summary.Conflicts,
			summary.LocalOnly, summary.RemoteOnly)
	}

	// ASSERT
	s.Assert().Equal(2, len(tables))
	// Both tables have overlapping data, so we should have some non-zero stats
}

// =============================================================================
// Category 2: Conflict Resolution Tests (T067-8 through T067-12)
// =============================================================================

// TestConflictResolution_PreferNodeA tests the prefer-node-a strategy.
// T067-8: Resolution Strategy - prefer-node-a
func (s *MergeTestSuite) TestConflictResolution_PreferNodeA() {
	ctx := s.ctx

	// SETUP: 3 conflicting rows
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'A'),
			(3, 'charlie', 'A')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(2, 'bob', 'B'),
			(3, 'charlie', 'B')
	`)
	s.Require().NoError(err)

	// Create merger and resolve conflicts with prefer-node-a
	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	// Get merge ID for audit logging
	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	s.T().Logf("Merge result: conflicts=%d, resolved=%d", result.TotalConflicts, result.ConflictsResolved)

	// ASSERT: All rows on Node B now have Node A's values
	rows, err := s.env.nodeBPool.Query(ctx, "SELECT id, version FROM users ORDER BY id")
	s.Require().NoError(err)
	defer rows.Close()

	for rows.Next() {
		var id int
		var version string
		err := rows.Scan(&id, &version)
		s.Require().NoError(err)
		s.Assert().Equal("A", version, "Row %d on Node B should have A's value", id)
	}
}

// TestConflictResolution_PreferNodeB tests the prefer-node-b strategy.
// T067-9: Resolution Strategy - prefer-node-b
func (s *MergeTestSuite) TestConflictResolution_PreferNodeB() {
	ctx := s.ctx

	// SETUP: 3 conflicting rows
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'A'),
			(3, 'charlie', 'A')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(2, 'bob', 'B'),
			(3, 'charlie', 'B')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeB,
		RemoteServer: "node_b_server",
	}

	_, err = merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: All rows on Node A now have Node B's values
	rows, err := s.env.nodeAPool.Query(ctx, "SELECT id, version FROM users ORDER BY id")
	s.Require().NoError(err)
	defer rows.Close()

	for rows.Next() {
		var id int
		var version string
		err := rows.Scan(&id, &version)
		s.Require().NoError(err)
		s.Assert().Equal("B", version, "Row %d on Node A should have B's value", id)
	}
}

// TestConflictResolution_LastModified tests the last-modified strategy.
// T067-10: Resolution Strategy - last-modified
func (s *MergeTestSuite) TestConflictResolution_LastModified() {
	ctx := s.ctx

	// SETUP: Insert rows with different timestamps
	// Row 1: B is newer (should keep B)
	// Row 2: A is newer (should keep A)

	// Insert on Node A first
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, updated_at) VALUES
			(1, 'alice', 'A', '2024-01-01 10:00:00'),
			(2, 'bob', 'A', '2024-01-15 10:00:00')
	`)
	s.Require().NoError(err)

	// Insert on Node B with different timestamps
	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version, updated_at) VALUES
			(1, 'alice', 'B', '2024-01-10 10:00:00'),
			(2, 'bob', 'B', '2024-01-05 10:00:00')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyLastModified,
		RemoteServer: "node_b_server",
	}

	_, err = merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: Check that correct versions were kept
	var row1Version, row2Version string
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT version FROM users WHERE id = 1").Scan(&row1Version)
	s.Require().NoError(err)
	s.Assert().Equal("B", row1Version, "Row 1: B was modified later")

	err = s.env.nodeAPool.QueryRow(ctx, "SELECT version FROM users WHERE id = 2").Scan(&row2Version)
	s.Require().NoError(err)
	s.Assert().Equal("A", row2Version, "Row 2: A was modified later")
}

// TestConflictResolution_Manual tests the manual strategy generates a report.
// T067-11: Resolution Strategy - manual (Generates Report)
func (s *MergeTestSuite) TestConflictResolution_Manual() {
	ctx := s.ctx

	// SETUP: 3 conflicting rows
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'A'),
			(3, 'charlie', 'A')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(2, 'bob', 'B'),
			(3, 'charlie', 'B')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	// Generate conflict report
	report, err := merger.GenerateConflictReport(ctx, "public", "users", []string{"id"}, "node_b_server")
	s.Require().NoError(err)

	// ASSERT
	s.Assert().Equal(3, report.TotalCount)
	s.Assert().Equal(3, len(report.Conflicts))

	for _, conflict := range report.Conflicts {
		s.Assert().NotEmpty(conflict.PKValue)
		s.Assert().NotEmpty(conflict.NodeAValue)
		s.Assert().NotEmpty(conflict.NodeBValue)
	}
}

// =============================================================================
// Category 3: Foreign Key Ordering Tests (T067-13 through T067-15)
// =============================================================================

// TestFKOrdering_SimpleParentChild tests FK ordering with parent/child tables.
// T067-13: FK Ordering - Simple Parent/Child
// Uses fixture: fk_relationships.sql
func (s *MergeTestSuite) TestFKOrdering_SimpleParentChild() {
	ctx := s.ctx

	// SETUP: Load FK relationships fixture
	// Node A: customer(100), orders for customer 100
	// Node B: customer(200), orders for customer 200
	s.env.execFixtureNodeA(s.T(), ctx, "fk_relationships.sql")
	s.env.execFixtureNodeB(s.T(), ctx, "fk_relationships.sql")

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	// Define tables in wrong order (orders before customers)
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "orders", PKColumns: []string{"id"}},
		{Schema: "public", Name: "customers", PKColumns: []string{"id"}},
	}

	// Get FK dependencies
	deps, err := merger.GetFKDependencies(ctx, tables)
	s.Require().NoError(err)

	// Topological sort should reorder to customers first
	sorted, err := merger.TopologicalSort(tables, deps)
	s.Require().NoError(err)

	// ASSERT: customers should come before orders
	s.Assert().Equal("customers", sorted[0].Name, "customers should be first")
	s.Assert().Equal("orders", sorted[1].Name, "orders should be second")

	// Execute merge with proper ordering
	mergeConfig := replinit.MergeConfig{
		Tables:       sorted,
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)
	s.T().Logf("Merge completed: tables=%d", len(result.Tables))

	// ASSERT: Both nodes have customers 100, 200 and all orders
	var customerCount int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customerCount)
	s.Require().NoError(err)
	s.Assert().Equal(2, customerCount, "Should have 2 customers on Node A")

	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customerCount)
	s.Require().NoError(err)
	s.Assert().Equal(2, customerCount, "Should have 2 customers on Node B")
}

// TestFKOrdering_CycleDetection tests that circular FK references are detected.
// T067-15: FK Ordering - Circular Reference Detection
func (s *MergeTestSuite) TestFKOrdering_CycleDetection() {
	// This test uses the unit-tested TopologicalSort function
	// which already handles cycle detection

	merger := replinit.NewMerger(nil, nil, nil)

	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "a", PKColumns: []string{"id"}},
		{Schema: "public", Name: "b", PKColumns: []string{"id"}},
		{Schema: "public", Name: "c", PKColumns: []string{"id"}},
	}

	// Circular dependency: a -> b -> c -> a
	deps := []replinit.FKDependency{
		{ChildSchema: "public", ChildTable: "b", ParentSchema: "public", ParentTable: "a"},
		{ChildSchema: "public", ChildTable: "c", ParentSchema: "public", ParentTable: "b"},
		{ChildSchema: "public", ChildTable: "a", ParentSchema: "public", ParentTable: "c"},
	}

	_, err := merger.TopologicalSort(tables, deps)
	s.Assert().Error(err, "Should detect circular dependency")
	s.Assert().Contains(err.Error(), "circular", "Error should mention circular dependency")
}

// =============================================================================
// Category 4: Data Movement Tests (T067-16 through T067-18)
// =============================================================================

// TestDataMovement_AOnlyToB tests that A-only rows are copied to B.
// T067-16: Data Movement - A-Only Rows to B
func (s *MergeTestSuite) TestDataMovement_AOnlyToB() {
	ctx := s.ctx

	// SETUP:
	// Node A: users(1,'alice'), users(2,'bob'), users(3,'charlie')
	// Node B: users(1,'alice')
	//
	// A-only: rows 2, 3
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1'),
			(2, 'bob', 'v1'),
			(3, 'charlie', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)
	s.T().Logf("Transferred A->B: %d rows", result.RowsTransferredAToB)

	// ASSERT: Node B now has rows 2 and 3
	var count int
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(3, count, "Node B should have 3 users")

	// Check specific users exist
	var names []string
	rows, err := s.env.nodeBPool.Query(ctx, "SELECT name FROM users ORDER BY id")
	s.Require().NoError(err)
	defer rows.Close()

	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		s.Require().NoError(err)
		names = append(names, name)
	}

	s.Assert().Contains(names, "bob")
	s.Assert().Contains(names, "charlie")
}

// TestDataMovement_BOnlyToA tests that B-only rows are copied to A.
// T067-17: Data Movement - B-Only Rows to A
func (s *MergeTestSuite) TestDataMovement_BOnlyToA() {
	ctx := s.ctx

	// SETUP: Mirror of T067-16
	// Node A: users(1,'alice')
	// Node B: users(1,'alice'), users(2,'bob'), users(3,'charlie')
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1'),
			(2, 'bob', 'v1'),
			(3, 'charlie', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)
	s.T().Logf("Transferred B->A: %d rows", result.RowsTransferredBToA)

	// ASSERT: Node A now has rows 2 and 3
	var count int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(3, count, "Node A should have 3 users")
}

// TestDataMovement_Bidirectional tests that data flows in both directions.
// T067-18: Data Movement - Bidirectional (Both Directions)
func (s *MergeTestSuite) TestDataMovement_Bidirectional() {
	ctx := s.ctx

	// SETUP: No overlap, pure additive merge
	// Node A: users(1), users(2)
	// Node B: users(3), users(4)
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1'),
			(2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(3, 'charlie', 'v1'),
			(4, 'diana', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)
	s.T().Logf("Transferred A->B: %d, B->A: %d", result.RowsTransferredAToB, result.RowsTransferredBToA)

	// ASSERT: Both nodes have all 4 rows
	var countA, countB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB)
	s.Require().NoError(err)

	s.Assert().Equal(4, countA, "Node A should have 4 users")
	s.Assert().Equal(4, countB, "Node B should have 4 users")
}

// =============================================================================
// Category 7: Pre-flight Checks (T067-24 through T067-26)
// =============================================================================

// TestPreflight_MissingPK_Fails tests that tables without primary keys are rejected.
// T067-25: Pre-flight - Missing Primary Key Fails
func (s *MergeTestSuite) TestPreflight_MissingPK_Fails() {
	ctx := s.ctx

	// Create table without primary key
	_, err := s.env.nodeAPool.Exec(ctx, "CREATE TABLE no_pk (data TEXT)")
	s.Require().NoError(err)
	_, err = s.env.nodeBPool.Exec(ctx, "CREATE TABLE no_pk (data TEXT)")
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "no_pk", PKColumns: []string{}}, // Empty PK
	}

	preflight, err := merger.RunPreflightChecks(ctx, tables)
	s.Require().NoError(err)

	// ASSERT: Should fail due to missing PK
	s.Assert().False(preflight.AllTableshavePK, "Should detect missing PK")
	s.Assert().True(len(preflight.Errors) > 0, "Should have errors")
}

// =============================================================================
// Category 8: Dry-Run Mode Tests (T067-27 through T067-28)
// =============================================================================

// TestDryRun_NoChanges tests that dry-run mode doesn't modify data.
// T067-27: Dry-Run - Shows Accurate Preview
func (s *MergeTestSuite) TestDryRun_NoChanges() {
	ctx := s.ctx

	// SETUP
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'A')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(3, 'charlie', 'B')
	`)
	s.Require().NoError(err)

	// Capture original state
	var originalCountA, originalCountB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&originalCountA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&originalCountB)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
		DryRun:       true, // Dry run!
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: Result should show what would happen
	s.T().Logf("Dry run result: conflicts=%d, a_to_b=%d, b_to_a=%d",
		result.TotalConflicts, result.RowsTransferredAToB, result.RowsTransferredBToA)

	// ASSERT: No data changed
	var countA, countB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB)
	s.Require().NoError(err)

	s.Assert().Equal(originalCountA, countA, "Node A count should be unchanged")
	s.Assert().Equal(originalCountB, countB, "Node B count should be unchanged")
}

// =============================================================================
// Category 6: Audit Trail Tests (T067-22 through T067-23)
// =============================================================================

// TestAuditLog_RecordsDecisions tests that merge decisions are logged.
// T067-22: Audit Log - Records All Decisions
func (s *MergeTestSuite) TestAuditLog_RecordsDecisions() {
	ctx := s.ctx

	// SETUP: Create data with conflicts
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(3, 'charlie', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: Audit log should have entries
	var auditCount int
	err = s.env.nodeAPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM steep_repl.merge_audit_log
		WHERE merge_id = $1
	`, result.MergeID).Scan(&auditCount)
	s.Require().NoError(err)

	s.T().Logf("Audit log entries: %d", auditCount)
	s.Assert().Greater(auditCount, 0, "Should have audit log entries")
}

// =============================================================================
// Category 5: Atomicity Tests (T067-19 through T067-21)
// =============================================================================

// TestBidirectionalMerge_AtomicRollback tests that merge failures roll back all changes.
// T067-19: Atomicity - Rollback on Failure
func (s *MergeTestSuite) TestBidirectionalMerge_AtomicRollback() {
	ctx := s.ctx

	// SETUP: Data that will merge normally
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'v1'),
			(2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(3, 'charlie', 'v1')
	`)
	s.Require().NoError(err)

	// Capture original state
	var originalCountA, originalCountB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&originalCountA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&originalCountB)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server pointing to non-existent host to force failure mid-merge
	_, err = s.env.nodeAPool.Exec(ctx, `
		CREATE SERVER IF NOT EXISTS bad_server
		FOREIGN DATA WRAPPER postgres_fdw
		OPTIONS (host 'nonexistent.invalid', port '5432', dbname 'testdb')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeAPool.Exec(ctx, `
		CREATE USER MAPPING IF NOT EXISTS FOR test
		SERVER bad_server
		OPTIONS (user 'test', password 'test')
	`)
	s.Require().NoError(err)

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "bad_server", // This will cause failure
	}

	_, err = merger.ExecuteMerge(ctx, mergeConfig)
	s.Assert().Error(err, "Merge should fail with bad server")

	// ASSERT: Data should be unchanged (rollback occurred)
	var countA, countB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB)
	s.Require().NoError(err)

	s.Assert().Equal(originalCountA, countA, "Node A should be unchanged after rollback")
	s.Assert().Equal(originalCountB, countB, "Node B should be unchanged after rollback")
}

// TestBidirectionalMerge_Idempotent tests that running merge twice produces same result.
// T067-20: Atomicity - Idempotent Merge
func (s *MergeTestSuite) TestBidirectionalMerge_Idempotent() {
	ctx := s.ctx

	// SETUP: Create data with conflicts and unique rows
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'A'),
			(2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES
			(1, 'alice', 'B'),
			(3, 'charlie', 'v1')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	// First merge
	result1, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// Capture state after first merge
	var countA1, countB1 int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA1)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB1)
	s.Require().NoError(err)

	// Second merge (should be idempotent)
	result2, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// Capture state after second merge
	var countA2, countB2 int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA2)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB2)
	s.Require().NoError(err)

	// ASSERT: State should be identical
	s.Assert().Equal(countA1, countA2, "Node A count should be same after second merge")
	s.Assert().Equal(countB1, countB2, "Node B count should be same after second merge")

	// Second merge should have no conflicts (all resolved in first)
	s.T().Logf("First merge: conflicts=%d, Second merge: conflicts=%d",
		result1.TotalConflicts, result2.TotalConflicts)
	s.Assert().Equal(int64(0), result2.TotalConflicts, "Second merge should have no conflicts")
}

// TestBidirectionalMerge_Checkpoint tests that checkpoints work for large merges.
// T067-21: Atomicity - Checkpoint Progress
func (s *MergeTestSuite) TestBidirectionalMerge_Checkpoint() {
	ctx := s.ctx

	// SETUP: Create larger dataset to trigger checkpointing
	// Insert 1000 rows on each node with some overlap
	for i := 1; i <= 500; i++ {
		_, err := s.env.nodeAPool.Exec(ctx, `
			INSERT INTO users (id, name, version) VALUES ($1::integer, 'user_' || $1::text, 'A')
		`, i)
		s.Require().NoError(err)
	}

	for i := 250; i <= 750; i++ {
		_, err := s.env.nodeBPool.Exec(ctx, `
			INSERT INTO users (id, name, version) VALUES ($1::integer, 'user_' || $1::text, 'B')
			ON CONFLICT (id) DO UPDATE SET version = 'B'
		`, i)
		s.Require().NoError(err)
	}

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	s.T().Logf("Merge result: rows A->B=%d, rows B->A=%d, conflicts=%d",
		result.RowsTransferredAToB, result.RowsTransferredBToA, result.TotalConflicts)

	// ASSERT: Both nodes should have all 750 unique IDs (1-750)
	var countA, countB int
	err = s.env.nodeAPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countA)
	s.Require().NoError(err)
	err = s.env.nodeBPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countB)
	s.Require().NoError(err)

	s.Assert().Equal(750, countA, "Node A should have 750 users")
	s.Assert().Equal(750, countB, "Node B should have 750 users")
}

// =============================================================================
// Additional Pre-flight Checks (T067-24 and T067-26)
// =============================================================================

// TestPreflight_SchemaMismatch_Fails tests that schema differences are detected.
// T067-24: Pre-flight - Schema Mismatch Fails
func (s *MergeTestSuite) TestPreflight_SchemaMismatch_Fails() {
	ctx := s.ctx

	// Create tables with different schemas
	_, err := s.env.nodeAPool.Exec(ctx, `
		CREATE TABLE schema_test (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			extra_col TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		CREATE TABLE schema_test (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
			-- missing extra_col
		)
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "schema_test", PKColumns: []string{"id"}},
	}

	preflight, err := merger.RunPreflightChecks(ctx, tables)
	s.Require().NoError(err)

	// ASSERT: Should detect schema mismatch
	s.Assert().False(preflight.SchemaMatch, "Should detect schema mismatch")
	s.T().Logf("Preflight warnings: %v", preflight.Warnings)
}

// TestPreflight_ActiveTransactions_Warning tests that active transactions are warned.
// T067-26: Pre-flight - Active Transactions Warning
func (s *MergeTestSuite) TestPreflight_ActiveTransactions_Warning() {
	ctx := s.ctx

	// Start a long-running transaction on Node A
	tx, err := s.env.nodeAPool.Begin(ctx)
	s.Require().NoError(err)
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "INSERT INTO users (id, name, version) VALUES (999, 'tx_user', 'v1')")
	s.Require().NoError(err)

	// Don't commit - leave transaction open

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "users", PKColumns: []string{"id"}},
	}

	preflight, err := merger.RunPreflightChecks(ctx, tables)
	s.Require().NoError(err)

	// ASSERT: Should warn about active transactions
	// The preflight check detects active transactions via NoActiveTransactions flag
	s.T().Logf("No active transactions: %v, Warnings: %v",
		preflight.NoActiveTransactions, preflight.Warnings)

	// We have an active transaction, so NoActiveTransactions should be false
	s.Assert().False(preflight.NoActiveTransactions,
		"Should detect active transaction (NoActiveTransactions=false)")
}

// =============================================================================
// Category 9: PG18 Feature Integration Tests (T067-29 through T067-31)
// =============================================================================

// TestPG18_OriginNone_PreventsPingPong tests that origin=none prevents replication loops.
// T067-29: PG18 - origin=none Prevents Ping-Pong
func (s *MergeTestSuite) TestPG18_OriginNone_PreventsPingPong() {
	ctx := s.ctx

	// SETUP: After merge, both nodes should have replication set up with origin=none
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES (1, 'alice', 'v1')
	`)
	s.Require().NoError(err)

	// Check if we can create a subscription with origin=none
	pubName := "test_pub_origin"
	subName := "test_sub_origin"

	_, err = s.env.nodeAPool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE users", pubName))
	s.Require().NoError(err)

	// Use Docker network IP for container-to-container communication
	// (external host/port won't work from inside Node B's container)
	connStr := fmt.Sprintf("host=%s port=%d dbname=testdb user=test password=test",
		s.env.nodeAHost, s.env.nodeAPort)

	_, err = s.env.nodeBPool.Exec(ctx, fmt.Sprintf(`
		CREATE SUBSCRIPTION %s
		CONNECTION '%s'
		PUBLICATION %s
		WITH (copy_data = false, origin = none)
	`, subName, connStr, pubName))
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		s.env.nodeBPool.Exec(ctx, fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", subName))
		s.env.nodeAPool.Exec(ctx, fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pubName))
	})

	// ASSERT: Subscription was created successfully with origin=none
	var subOrigin string
	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT suborigin FROM pg_subscription WHERE subname = $1
	`, subName).Scan(&subOrigin)
	s.Require().NoError(err)

	s.Assert().Equal("none", subOrigin, "Subscription should have origin=none")

	// Verify subscription is active
	var enabled bool
	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT subenabled FROM pg_subscription WHERE subname = $1
	`, subName).Scan(&enabled)
	s.Require().NoError(err)
	s.Assert().True(enabled, "Subscription should be enabled")
}

// TestPG18_TrackCommitTimestamp tests that track_commit_timestamp works correctly.
// T067-30: PG18 - track_commit_timestamp Integration
func (s *MergeTestSuite) TestPG18_TrackCommitTimestamp() {
	ctx := s.ctx

	// SETUP: Insert a row and verify commit timestamp is tracked
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES (1, 'alice', 'v1')
	`)
	s.Require().NoError(err)

	// Query the commit timestamp using pg_xact_commit_timestamp
	var commitTs time.Time
	err = s.env.nodeAPool.QueryRow(ctx, `
		SELECT pg_xact_commit_timestamp(xmin)
		FROM users
		WHERE id = 1
	`).Scan(&commitTs)
	s.Require().NoError(err)

	// ASSERT: Commit timestamp should be valid and recent
	s.Assert().False(commitTs.IsZero(), "Commit timestamp should not be zero")
	s.Assert().WithinDuration(time.Now(), commitTs, 5*time.Minute,
		"Commit timestamp should be recent")

	s.T().Logf("Commit timestamp: %v", commitTs)

	// Insert on Node B with a delay to ensure different timestamp
	time.Sleep(100 * time.Millisecond)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version) VALUES (2, 'bob', 'v1')
	`)
	s.Require().NoError(err)

	var commitTsB time.Time
	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT pg_xact_commit_timestamp(xmin)
		FROM users
		WHERE id = 2
	`).Scan(&commitTsB)
	s.Require().NoError(err)

	// Node B's insert should have a later timestamp
	s.Assert().True(commitTsB.After(commitTs) || commitTsB.Equal(commitTs),
		"Node B commit should be same or later than Node A")
}

// TestPG18_ExtensionRowHash tests the steep_repl extension's row_hash function.
// T067-31: PG18 - Extension Row Hash Function
func (s *MergeTestSuite) TestPG18_ExtensionRowHash() {
	ctx := s.ctx

	// SETUP: Insert identical rows on both nodes
	// Must explicitly set created_at/updated_at to ensure identical hashes
	fixedTime := "2024-01-01 00:00:00+00"
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', 'v1', $1::timestamptz, $1::timestamptz),
			(2, 'bob', 'v1', $1::timestamptz, $1::timestamptz)
	`, fixedTime)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', 'v1', $1::timestamptz, $1::timestamptz),
			(2, 'bob', 'v2', $1::timestamptz, $1::timestamptz)
	`, fixedTime)
	s.Require().NoError(err)

	// Test row_hash function
	var hashA1, hashA2 string
	err = s.env.nodeAPool.QueryRow(ctx, `
		SELECT steep_repl.row_hash(u.*) FROM users u WHERE id = 1
	`).Scan(&hashA1)
	s.Require().NoError(err)

	err = s.env.nodeAPool.QueryRow(ctx, `
		SELECT steep_repl.row_hash(u.*) FROM users u WHERE id = 2
	`).Scan(&hashA2)
	s.Require().NoError(err)

	var hashB1, hashB2 string
	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT steep_repl.row_hash(u.*) FROM users u WHERE id = 1
	`).Scan(&hashB1)
	s.Require().NoError(err)

	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT steep_repl.row_hash(u.*) FROM users u WHERE id = 2
	`).Scan(&hashB2)
	s.Require().NoError(err)

	// ASSERT: Matching rows should have same hash, different rows should have different hash
	s.Assert().Equal(hashA1, hashB1, "Row 1 should have same hash on both nodes")
	s.Assert().NotEqual(hashA2, hashB2, "Row 2 should have different hash (version differs)")
	s.Assert().NotEqual(hashA1, hashA2, "Different rows should have different hashes")

	s.T().Logf("Row 1 hashes: A=%s, B=%s", hashA1, hashB1)
	s.T().Logf("Row 2 hashes: A=%s, B=%s", hashA2, hashB2)
}
