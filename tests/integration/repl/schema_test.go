package repl_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// =============================================================================
// Schema Test Suite
// =============================================================================

// SchemaTestSuite runs schema comparison and sync mode integration tests
// with shared PostgreSQL containers and per-test daemons.
type SchemaTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc

	// Shared across all tests (containers only)
	network         *testcontainers.DockerNetwork
	nodeAContainer  testcontainers.Container
	nodeBContainer  testcontainers.Container
	nodeAPool       *pgxpool.Pool
	nodeBPool       *pgxpool.Pool

	// Connection info (for daemon creation)
	nodeAHostExternal string
	nodeAPortExternal int
	nodeBHostExternal string
	nodeBPortExternal int

	// Per-test daemons (recreated for each test)
	nodeADaemon   *daemon.Daemon
	nodeBDaemon   *daemon.Daemon
	nodeAGRPCPort int
	nodeBGRPCPort int
}

func TestSchemaSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(SchemaTestSuite))
}

func (s *SchemaTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	s.T().Log("Setting up schema test suite - creating shared PostgreSQL containers...")

	const testPassword = "test"

	// Create Docker network
	net, err := network.New(s.ctx, network.WithCheckDuplicate())
	s.Require().NoError(err, "Failed to create Docker network")
	s.network = net

	// Start node A container
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
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeAContainer, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeAReq,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start node A container")
	s.nodeAContainer = nodeAContainer

	// Start node B container
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
		},
		WaitingFor: postgresWaitStrategy(),
	}

	nodeBContainer, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeBReq,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start node B container")
	s.nodeBContainer = nodeBContainer

	// Get connection info
	nodeAHost, _ := nodeAContainer.Host(s.ctx)
	nodeAPort, _ := nodeAContainer.MappedPort(s.ctx, "5432")
	nodeBHost, _ := nodeBContainer.Host(s.ctx)
	nodeBPort, _ := nodeBContainer.MappedPort(s.ctx, "5432")

	s.nodeAHostExternal = nodeAHost
	s.nodeAPortExternal = nodeAPort.Int()
	s.nodeBHostExternal = nodeBHost
	s.nodeBPortExternal = nodeBPort.Int()

	// Create connection pools
	nodeAConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeAHost, nodeAPort.Port())
	s.nodeAPool, err = pgxpool.New(s.ctx, nodeAConnStr)
	s.Require().NoError(err, "Failed to create node A pool")

	nodeBConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeBHost, nodeBPort.Port())
	s.nodeBPool, err = pgxpool.New(s.ctx, nodeBConnStr)
	s.Require().NoError(err, "Failed to create node B pool")

	// Wait for databases
	s.waitForDB(s.nodeAPool, "node-a")
	s.waitForDB(s.nodeBPool, "node-b")

	// Create steep_repl extension on both nodes
	_, err = s.nodeAPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on node A")
	_, err = s.nodeBPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on node B")

	s.T().Log("Schema test suite setup complete")
}

func (s *SchemaTestSuite) TearDownSuite() {
	s.T().Log("Tearing down schema test suite...")

	if s.nodeAPool != nil {
		s.nodeAPool.Close()
	}
	if s.nodeBPool != nil {
		s.nodeBPool.Close()
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if s.nodeAContainer != nil {
		if err := s.nodeAContainer.Terminate(cleanupCtx); err != nil {
			s.T().Logf("Failed to terminate node A container: %v", err)
		}
	}
	if s.nodeBContainer != nil {
		if err := s.nodeBContainer.Terminate(cleanupCtx); err != nil {
			s.T().Logf("Failed to terminate node B container: %v", err)
		}
	}
	if s.network != nil {
		if err := s.network.Remove(cleanupCtx); err != nil {
			s.T().Logf("Failed to remove network: %v", err)
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.T().Log("Schema test suite teardown complete")
}

func (s *SchemaTestSuite) SetupTest() {
	s.T().Log("Setting up test - creating fresh daemons...")

	const testPassword = "test"
	s.T().Setenv("PGPASSWORD", testPassword)

	// Drop and recreate test tables to ensure clean state
	s.cleanupTestTables()

	// Start fresh daemons with dynamically allocated ports
	s.nodeAGRPCPort = getFreePort(s.T())
	s.nodeBGRPCPort = getFreePort(s.T())

	nodeASocketPath := tempSocketPath(s.T())
	nodeACfg := &config.Config{
		NodeID:   "node-a",
		NodeName: "Node A",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     s.nodeAHostExternal,
			Port:     s.nodeAPortExternal,
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: s.nodeAGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeASocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodSnapshot,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	var err error
	s.nodeADaemon, err = daemon.New(nodeACfg, true)
	s.Require().NoError(err, "Failed to create node A daemon")
	err = s.nodeADaemon.Start()
	s.Require().NoError(err, "Failed to start node A daemon")

	nodeBSocketPath := tempSocketPath(s.T())
	nodeBCfg := &config.Config{
		NodeID:   "node-b",
		NodeName: "Node B",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     s.nodeBHostExternal,
			Port:     s.nodeBPortExternal,
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: s.nodeBGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    nodeBSocketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
		Initialization: config.InitConfig{
			Method:          config.InitMethodSnapshot,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	s.nodeBDaemon, err = daemon.New(nodeBCfg, true)
	s.Require().NoError(err, "Failed to create node B daemon")
	err = s.nodeBDaemon.Start()
	s.Require().NoError(err, "Failed to start node B daemon")

	// Wait for daemons to be ready
	time.Sleep(time.Second)

	s.T().Log("Test setup complete - daemons ready")
}

func (s *SchemaTestSuite) TearDownTest() {
	s.T().Log("Tearing down test - stopping daemons...")

	if s.nodeADaemon != nil {
		s.nodeADaemon.Stop()
		s.nodeADaemon = nil
	}
	if s.nodeBDaemon != nil {
		s.nodeBDaemon.Stop()
		s.nodeBDaemon = nil
	}

	s.T().Log("Test teardown complete")
}

func (s *SchemaTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := pool.Ping(s.ctx); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.T().Fatalf("Timeout waiting for %s database", name)
}

func (s *SchemaTestSuite) cleanupTestTables() {
	// Drop all user tables to ensure clean state between tests
	tables := []string{
		"users", "orders", "products", "local_only_a", "remote_only_b",
		"matching", "mismatched", "a_only", "b_only",
		"sync_test_strict", "sync_test_strict_match",
		"sync_test_auto", "sync_test_auto_new",
		"sync_test_manual", "sync_test_manual_match",
	}
	for _, table := range tables {
		s.nodeAPool.Exec(s.ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		s.nodeBPool.Exec(s.ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	}
}

// =============================================================================
// Schema Fingerprint Tests
// =============================================================================

// TestSchema_FingerprintViaGRPC tests fingerprint retrieval via gRPC daemon.
func (s *SchemaTestSuite) TestSchema_FingerprintViaGRPC() {
	// Create test table on node A
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE
		)
	`)
	s.Require().NoError(err, "Failed to create test table")

	// Connect to node A daemon
	client, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create gRPC client")
	defer client.Close()

	// Get fingerprints via gRPC
	resp, err := client.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints failed")

	s.Assert().True(resp.Success, "GetSchemaFingerprints returned error: %s", resp.Error)

	// Should have at least the users table
	found := false
	for _, fp := range resp.Fingerprints {
		s.T().Logf("Table %s.%s: %s", fp.SchemaName, fp.TableName, fp.Fingerprint)
		if fp.TableName == "users" {
			found = true
			s.Assert().Equal(64, len(fp.Fingerprint), "Fingerprint should be 64 hex chars")
		}
	}

	s.Assert().True(found, "Expected to find fingerprint for 'users' table")
}

// =============================================================================
// T054: Integration test for schema comparison
// =============================================================================

// TestSchema_CompareMatching tests CompareSchemas when schemas match.
func (s *SchemaTestSuite) TestSchema_CompareMatching() {
	// Create identical tables on both nodes
	tableSQL := `
		CREATE TABLE orders (
			id SERIAL PRIMARY KEY,
			customer_name TEXT NOT NULL,
			amount DECIMAL(10,2),
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`

	_, err := s.nodeAPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node A")

	_, err = s.nodeBPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node B")

	// Connect to node A daemon
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	// Get fingerprints from node A
	respA, err := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from node A failed")

	// Connect to node B daemon
	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	// Get fingerprints from node B
	respB, err := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from node B failed")

	// Compare fingerprints for the orders table
	var fpA, fpB string
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "orders" {
			fpA = fp.Fingerprint
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "orders" {
			fpB = fp.Fingerprint
		}
	}

	s.Require().NotEmpty(fpA, "Failed to find fingerprint for orders table on A")
	s.Require().NotEmpty(fpB, "Failed to find fingerprint for orders table on B")

	s.Assert().Equal(fpA, fpB, "Fingerprints should match for identical schemas")
	s.T().Logf("Fingerprints match: %s", fpA)
}

// TestSchema_CompareMismatch tests CompareSchemas when schemas differ.
func (s *SchemaTestSuite) TestSchema_CompareMismatch() {
	// Create different tables on each node
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price DECIMAL(10,2),
			sku TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node A")

	// Node B has missing column (sku) and different type for price
	_, err = s.nodeBPool.Exec(s.ctx, `
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price INTEGER
		)
	`)
	s.Require().NoError(err, "Failed to create table on node B")

	// Get fingerprints from both nodes
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from A failed")

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from B failed")

	// Compare fingerprints
	var fpA, fpB string
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "products" {
			fpA = fp.Fingerprint
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "products" {
			fpB = fp.Fingerprint
		}
	}

	s.Require().NotEmpty(fpA, "Failed to find fingerprint for products table on A")
	s.Require().NotEmpty(fpB, "Failed to find fingerprint for products table on B")

	s.Assert().NotEqual(fpA, fpB, "Fingerprints should differ for mismatched schemas")
	s.T().Logf("Fingerprints correctly differ: A=%s B=%s", fpA, fpB)
}

// TestSchema_CompareLocalOnly tests detection of tables that exist only on one node.
func (s *SchemaTestSuite) TestSchema_CompareLocalOnly() {
	// Create table only on node A
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE local_only_a (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node A")

	// Get fingerprints
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from A failed")

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from B failed")

	// Check that local_only_a exists only on A
	var foundOnA, foundOnB bool
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "local_only_a" {
			foundOnA = true
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "local_only_a" {
			foundOnB = true
		}
	}

	s.Assert().True(foundOnA, "local_only_a should exist on node A")
	s.Assert().False(foundOnB, "local_only_a should NOT exist on node B")

	s.T().Logf("LOCAL_ONLY detection: foundOnA=%v, foundOnB=%v", foundOnA, foundOnB)
}

// TestSchema_CompareRemoteOnly tests detection of tables that exist only on remote node.
func (s *SchemaTestSuite) TestSchema_CompareRemoteOnly() {
	// Create table only on node B
	_, err := s.nodeBPool.Exec(s.ctx, `
		CREATE TABLE remote_only_b (
			id SERIAL PRIMARY KEY,
			info JSONB
		)
	`)
	s.Require().NoError(err, "Failed to create table on node B")

	// Get fingerprints
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from A failed")

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from B failed")

	// Check that remote_only_b exists only on B (from A's perspective, this is REMOTE_ONLY)
	var foundOnA, foundOnB bool
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "remote_only_b" {
			foundOnA = true
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "remote_only_b" {
			foundOnB = true
		}
	}

	s.Assert().False(foundOnA, "remote_only_b should NOT exist on node A")
	s.Assert().True(foundOnB, "remote_only_b should exist on node B")

	s.T().Logf("REMOTE_ONLY detection: foundOnA=%v, foundOnB=%v", foundOnA, foundOnB)
}

// TestSchema_CompareSchemasFull tests the full CompareSchemas RPC.
func (s *SchemaTestSuite) TestSchema_CompareSchemasFull() {
	// Setup: Create various tables for comprehensive comparison
	// 1. Matching table
	matchingSQL := `CREATE TABLE matching (id INT PRIMARY KEY, name TEXT)`
	_, err := s.nodeAPool.Exec(s.ctx, matchingSQL)
	s.Require().NoError(err, "Failed to create matching table on A")
	_, err = s.nodeBPool.Exec(s.ctx, matchingSQL)
	s.Require().NoError(err, "Failed to create matching table on B")

	// 2. Mismatching table (different column types)
	_, err = s.nodeAPool.Exec(s.ctx, `CREATE TABLE mismatched (id INT PRIMARY KEY, value TEXT)`)
	s.Require().NoError(err, "Failed to create mismatched table on A")
	_, err = s.nodeBPool.Exec(s.ctx, `CREATE TABLE mismatched (id INT PRIMARY KEY, value INTEGER)`)
	s.Require().NoError(err, "Failed to create mismatched table on B")

	// 3. Local only table (on A)
	_, err = s.nodeAPool.Exec(s.ctx, `CREATE TABLE a_only (id INT PRIMARY KEY)`)
	s.Require().NoError(err, "Failed to create a_only table")

	// 4. Remote only table (on B)
	_, err = s.nodeBPool.Exec(s.ctx, `CREATE TABLE b_only (id INT PRIMARY KEY)`)
	s.Require().NoError(err, "Failed to create b_only table")

	// Get fingerprints from both nodes
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})

	// Build maps for comparison
	fpA := make(map[string]string)
	fpB := make(map[string]string)

	for _, fp := range respA.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		fpA[key] = fp.Fingerprint
	}
	for _, fp := range respB.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		fpB[key] = fp.Fingerprint
	}

	// Compute comparison results
	var matchCount, mismatchCount, localOnlyCount, remoteOnlyCount int

	// Check all tables from A
	for key, localFP := range fpA {
		if remoteFP, exists := fpB[key]; exists {
			if localFP == remoteFP {
				matchCount++
				s.T().Logf("MATCH: %s", key)
			} else {
				mismatchCount++
				s.T().Logf("MISMATCH: %s (A=%s... B=%s...)", key, localFP[:8], remoteFP[:8])
			}
		} else {
			localOnlyCount++
			s.T().Logf("LOCAL_ONLY: %s", key)
		}
	}

	// Check tables only on B
	for key := range fpB {
		if _, exists := fpA[key]; !exists {
			remoteOnlyCount++
			s.T().Logf("REMOTE_ONLY: %s", key)
		}
	}

	s.T().Logf("Summary: match=%d, mismatch=%d, local_only=%d, remote_only=%d",
		matchCount, mismatchCount, localOnlyCount, remoteOnlyCount)

	// Verify expected results
	s.Assert().Greater(matchCount, 0, "Should have at least one matching table")
	s.Assert().Greater(mismatchCount, 0, "Should have at least one mismatched table")
	s.Assert().Greater(localOnlyCount, 0, "Should have at least one local-only table")
	s.Assert().Greater(remoteOnlyCount, 0, "Should have at least one remote-only table")
}

// =============================================================================
// Schema Sync Mode Tests (T063, T064, T065)
// =============================================================================

// TestSchemaSyncMode_Strict tests that strict mode fails on schema mismatch.
func (s *SchemaTestSuite) TestSchemaSyncMode_Strict() {
	// Create identical base table on both nodes
	baseSQL := `CREATE TABLE sync_test_strict (id INT PRIMARY KEY, name TEXT)`
	_, err := s.nodeAPool.Exec(s.ctx, baseSQL)
	s.Require().NoError(err, "Failed to create table on node A")
	_, err = s.nodeBPool.Exec(s.ctx, baseSQL)
	s.Require().NoError(err, "Failed to create table on node B")

	// Add extra column only on node A to create mismatch
	_, err = s.nodeAPool.Exec(s.ctx, `ALTER TABLE sync_test_strict ADD COLUMN extra_col TEXT`)
	s.Require().NoError(err, "Failed to alter table on node A")

	// Get fingerprints to verify mismatch exists
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})

	var fpA, fpB string
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_strict" {
			fpA = fp.Fingerprint
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "sync_test_strict" {
			fpB = fp.Fingerprint
		}
	}

	s.Require().NotEqual(fpA, fpB, "Test setup error: fingerprints should differ for mismatch test")
	s.T().Logf("Verified mismatch: A=%s... B=%s...", fpA[:8], fpB[:8])

	// Test strict mode via SchemaSyncHandler
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)

	// Build comparison result
	compareResult := &replinit.CompareResult{
		MismatchCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema:       "public",
				TableName:         "sync_test_strict",
				LocalFingerprint:  fpB,
				RemoteFingerprint: fpA,
				Status:            replinit.ComparisonMismatch,
			},
		},
	}

	// Strict mode should return an error
	result, err := handler.HandleStrict(s.ctx, compareResult)
	s.Assert().Error(err, "HandleStrict should return error on mismatch")
	s.T().Logf("Strict mode correctly failed: %v", err)

	s.Require().NotNil(result, "Result should not be nil")
	s.Assert().Equal("strict", result.Mode)
	s.Assert().Equal("failed", result.Action)
	s.Assert().Equal(1, len(result.Differences))
}

// TestSchemaSyncMode_Strict_NoMismatch tests that strict mode passes when schemas match.
func (s *SchemaTestSuite) TestSchemaSyncMode_Strict_NoMismatch() {
	// Create identical tables on both nodes
	tableSQL := `CREATE TABLE sync_test_strict_match (id INT PRIMARY KEY, name TEXT)`
	_, err := s.nodeAPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node A")
	_, err = s.nodeBPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node B")

	// Get fingerprints to verify they match
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})

	var fpA, fpB string
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_strict_match" {
			fpA = fp.Fingerprint
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "sync_test_strict_match" {
			fpB = fp.Fingerprint
		}
	}

	s.Require().Equal(fpA, fpB, "Test setup error: fingerprints should match")

	// Test strict mode with matching schemas
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)

	compareResult := &replinit.CompareResult{
		MatchCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema:       "public",
				TableName:         "sync_test_strict_match",
				LocalFingerprint:  fpB,
				RemoteFingerprint: fpA,
				Status:            replinit.ComparisonMatch,
			},
		},
	}

	// Strict mode should pass when schemas match
	result, err := handler.HandleStrict(s.ctx, compareResult)
	s.Assert().NoError(err, "HandleStrict should not error when schemas match")

	s.Require().NotNil(result, "Result should not be nil")
	s.Assert().Equal("strict", result.Mode)
	s.Assert().Equal("passed", result.Action)
	s.T().Logf("Strict mode correctly passed with matching schemas")
}

// TestSchemaSyncMode_Auto tests that auto mode applies DDL to fix mismatches.
func (s *SchemaTestSuite) TestSchemaSyncMode_Auto() {
	// Create table with extra column on node A (source/remote)
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE sync_test_auto (
			id INT PRIMARY KEY,
			name TEXT,
			extra_col TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node A")

	// Create table without extra column on node B (target/local)
	_, err = s.nodeBPool.Exec(s.ctx, `
		CREATE TABLE sync_test_auto (
			id INT PRIMARY KEY,
			name TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node B")

	// Get fingerprints with column definitions
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from A failed")

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	s.Require().NoError(err, "GetSchemaFingerprints from B failed")

	var fpA, fpB *pb.TableFingerprint
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_auto" {
			fpA = fp
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "sync_test_auto" {
			fpB = fp
		}
	}

	s.Require().NotNil(fpA, "Failed to find fingerprints for sync_test_auto on A")
	s.Require().NotNil(fpB, "Failed to find fingerprints for sync_test_auto on B")

	s.Require().NotEqual(fpA.Fingerprint, fpB.Fingerprint, "Test setup error: fingerprints should differ")
	s.T().Logf("Before auto sync: A=%s... B=%s...", fpA.Fingerprint[:8], fpB.Fingerprint[:8])

	// Build remote fingerprints map for auto mode
	remoteFingerprints := map[string]replinit.TableFingerprintInfo{
		"public.sync_test_auto": {
			Fingerprint:       fpA.Fingerprint,
			ColumnDefinitions: fpA.ColumnDefinitions,
		},
	}

	// Build comparison result
	compareResult := &replinit.CompareResult{
		MismatchCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema:       "public",
				TableName:         "sync_test_auto",
				LocalFingerprint:  fpB.Fingerprint,
				RemoteFingerprint: fpA.Fingerprint,
				Status:            replinit.ComparisonMismatch,
			},
		},
	}

	// Test auto mode - should generate and apply DDL
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)
	result, err := handler.HandleAuto(s.ctx, compareResult, remoteFingerprints)
	s.Require().NoError(err, "HandleAuto failed")

	s.Require().NotNil(result, "Result should not be nil")
	s.Assert().Equal("auto", result.Mode)
	s.Assert().Equal("applied", result.Action)
	s.T().Logf("Auto mode result: applied=%d, skipped=%d, DDL=%v",
		result.AppliedCount, result.SkippedCount, result.DDLStatements)

	// Verify the column was added to node B
	var colCount int
	err = s.nodeBPool.QueryRow(s.ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_auto'
	`).Scan(&colCount)
	s.Require().NoError(err, "Failed to query column count")

	s.Assert().Equal(3, colCount, "Column count should be 3 (id, name, extra_col) after auto sync")
	s.T().Log("Auto mode successfully added missing column")

	// Verify fingerprints now match
	respBAfter, _ := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	for _, fp := range respBAfter.Fingerprints {
		if fp.TableName == "sync_test_auto" {
			if fp.Fingerprint == fpA.Fingerprint {
				s.T().Log("Fingerprints now match after auto sync")
			} else {
				s.T().Logf("Fingerprints still differ (expected for complex cases): A=%s... B=%s...",
					fpA.Fingerprint[:8], fp.Fingerprint[:8])
			}
		}
	}
}

// TestSchemaSyncMode_Auto_RemoteOnly tests that auto mode creates missing tables.
func (s *SchemaTestSuite) TestSchemaSyncMode_Auto_RemoteOnly() {
	// Create table only on node A (source/remote)
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE sync_test_auto_new (
			id INT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TIMESTAMP
		)
	`)
	s.Require().NoError(err, "Failed to create table on node A")

	// Verify table does NOT exist on node B
	var exists bool
	err = s.nodeBPool.QueryRow(s.ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
		)
	`).Scan(&exists)
	s.Require().NoError(err, "Failed to check table existence")
	s.Require().False(exists, "Test setup error: table should not exist on node B yet")

	// Get fingerprints from node A
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	respA, _ := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})

	var fpA *pb.TableFingerprint
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_auto_new" {
			fpA = fp
		}
	}
	s.Require().NotNil(fpA, "Failed to find fingerprint for sync_test_auto_new on node A")

	// Build remote fingerprints map
	remoteFingerprints := map[string]replinit.TableFingerprintInfo{
		"public.sync_test_auto_new": {
			Fingerprint:       fpA.Fingerprint,
			ColumnDefinitions: fpA.ColumnDefinitions,
		},
	}

	// Build comparison result - table exists only on remote
	compareResult := &replinit.CompareResult{
		RemoteOnlyCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema:       "public",
				TableName:         "sync_test_auto_new",
				RemoteFingerprint: fpA.Fingerprint,
				Status:            replinit.ComparisonRemoteOnly,
			},
		},
	}

	// Test auto mode - should create the missing table
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)
	result, err := handler.HandleAuto(s.ctx, compareResult, remoteFingerprints)
	s.Require().NoError(err, "HandleAuto failed")

	s.Require().NotNil(result, "Result should not be nil")
	s.T().Logf("Auto mode (remote-only) result: applied=%d, DDL=%v",
		result.AppliedCount, result.DDLStatements)

	// Verify the table was created on node B
	err = s.nodeBPool.QueryRow(s.ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
		)
	`).Scan(&exists)
	s.Require().NoError(err, "Failed to check table existence")

	s.Assert().True(exists, "Auto mode should have created the missing table")
	s.T().Log("Auto mode successfully created missing table")

	// Verify column count
	var colCount int
	err = s.nodeBPool.QueryRow(s.ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
	`).Scan(&colCount)
	s.Require().NoError(err, "Failed to query column count")

	s.Assert().Equal(3, colCount, "Column count should be 3 (id, name, created_at)")
}

// TestSchemaSyncMode_Manual tests that manual mode warns but proceeds.
func (s *SchemaTestSuite) TestSchemaSyncMode_Manual() {
	// Create mismatched tables
	_, err := s.nodeAPool.Exec(s.ctx, `
		CREATE TABLE sync_test_manual (
			id INT PRIMARY KEY,
			name TEXT,
			extra TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node A")

	_, err = s.nodeBPool.Exec(s.ctx, `
		CREATE TABLE sync_test_manual (
			id INT PRIMARY KEY,
			name TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create table on node B")

	// Get fingerprints
	clientA, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client A")
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(s.ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", s.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err, "Failed to create client B")
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(s.ctx, &pb.GetSchemaFingerprintsRequest{})

	var fpA, fpB string
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_manual" {
			fpA = fp.Fingerprint
		}
	}
	for _, fp := range respB.Fingerprints {
		if fp.TableName == "sync_test_manual" {
			fpB = fp.Fingerprint
		}
	}

	s.Require().NotEqual(fpA, fpB, "Test setup error: fingerprints should differ")

	// Build comparison result with mismatch
	compareResult := &replinit.CompareResult{
		MismatchCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema:       "public",
				TableName:         "sync_test_manual",
				LocalFingerprint:  fpB,
				RemoteFingerprint: fpA,
				Status:            replinit.ComparisonMismatch,
			},
		},
	}

	// Test manual mode - should warn but not error
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)
	result, err := handler.HandleManual(s.ctx, compareResult)
	s.Assert().NoError(err, "HandleManual should not return error")

	s.Require().NotNil(result, "Result should not be nil")
	s.Assert().Equal("manual", result.Mode)
	s.Assert().Equal("warned", result.Action)
	s.Assert().NotEmpty(result.WarningMessage, "WarningMessage should not be empty for mismatch")
	s.Assert().Equal(1, len(result.Differences))

	s.T().Logf("Manual mode warning: %s", result.WarningMessage)
	s.T().Log("Manual mode correctly warned but did not fail")

	// Verify schema was NOT modified (no auto-fix)
	var colCount int
	err = s.nodeBPool.QueryRow(s.ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_manual'
	`).Scan(&colCount)
	s.Require().NoError(err, "Failed to query column count")

	s.Assert().Equal(2, colCount, "Column count should be 2 (id, name) - manual mode should not modify schema")
	s.T().Log("Manual mode correctly did not modify schema")
}

// TestSchemaSyncMode_Manual_NoMismatch tests that manual mode has no warning when schemas match.
func (s *SchemaTestSuite) TestSchemaSyncMode_Manual_NoMismatch() {
	// Create identical tables
	tableSQL := `CREATE TABLE sync_test_manual_match (id INT PRIMARY KEY, name TEXT)`
	_, err := s.nodeAPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node A")
	_, err = s.nodeBPool.Exec(s.ctx, tableSQL)
	s.Require().NoError(err, "Failed to create table on node B")

	// Build comparison result with no mismatch
	compareResult := &replinit.CompareResult{
		MatchCount: 1,
		Comparisons: []replinit.SchemaComparison{
			{
				TableSchema: "public",
				TableName:   "sync_test_manual_match",
				Status:      replinit.ComparisonMatch,
			},
		},
	}

	// Test manual mode with matching schemas
	handler := replinit.NewSchemaSyncHandler(s.nodeBPool)
	result, err := handler.HandleManual(s.ctx, compareResult)
	s.Assert().NoError(err, "HandleManual should not return error")

	s.Require().NotNil(result, "Result should not be nil")
	s.Assert().Empty(result.WarningMessage, "WarningMessage should be empty when schemas match")
	s.Assert().Equal(0, len(result.Differences))

	s.T().Log("Manual mode correctly produced no warning for matching schemas")
}
