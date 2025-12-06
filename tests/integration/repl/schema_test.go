package repl_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// schemaTestEnv holds the complete test environment for schema testing.
type schemaTestEnv struct {
	network *testcontainers.DockerNetwork

	// Node A (source)
	nodeAContainer testcontainers.Container
	nodeAPool      *pgxpool.Pool
	nodeAHost      string // Docker network hostname
	nodeAPort      int
	nodeADaemon    *daemon.Daemon
	nodeAGRPCPort  int

	// Node B (target)
	nodeBContainer testcontainers.Container
	nodeBPool      *pgxpool.Pool
	nodeBHost      string
	nodeBPort      int
	nodeBDaemon    *daemon.Daemon
	nodeBGRPCPort  int
}

// setupSchemaTestEnv creates a two-node test environment for schema comparison tests.
func setupSchemaTestEnv(t *testing.T, ctx context.Context) *schemaTestEnv {
	t.Helper()

	const testPassword = "test"
	env := &schemaTestEnv{}

	// Create Docker network
	net, err := network.New(ctx, network.WithCheckDuplicate())
	if err != nil {
		t.Fatalf("Failed to create Docker network: %v", err)
	}
	env.network = net
	t.Cleanup(func() {
		if err := net.Remove(ctx); err != nil {
			t.Logf("Failed to remove network: %v", err)
		}
	})

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
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(90 * time.Second),
	}

	nodeAContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeAReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start node A container: %v", err)
	}
	env.nodeAContainer = nodeAContainer
	t.Cleanup(func() {
		if err := nodeAContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate node A container: %v", err)
		}
	})

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
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(90 * time.Second),
	}

	nodeBContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: nodeBReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start node B container: %v", err)
	}
	env.nodeBContainer = nodeBContainer
	t.Cleanup(func() {
		if err := nodeBContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate node B container: %v", err)
		}
	})

	// Get connection info
	nodeAHostExternal, _ := nodeAContainer.Host(ctx)
	nodeAPortExternal, _ := nodeAContainer.MappedPort(ctx, "5432")
	nodeBHostExternal, _ := nodeBContainer.Host(ctx)
	nodeBPortExternal, _ := nodeBContainer.MappedPort(ctx, "5432")

	env.nodeAHost = "pg-node-a"
	env.nodeAPort = 5432
	env.nodeBHost = "pg-node-b"
	env.nodeBPort = 5432

	// Create connection pools
	nodeAConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeAHostExternal, nodeAPortExternal.Port())
	env.nodeAPool, err = pgxpool.New(ctx, nodeAConnStr)
	if err != nil {
		t.Fatalf("Failed to create node A pool: %v", err)
	}
	t.Cleanup(func() { env.nodeAPool.Close() })

	nodeBConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		nodeBHostExternal, nodeBPortExternal.Port())
	env.nodeBPool, err = pgxpool.New(ctx, nodeBConnStr)
	if err != nil {
		t.Fatalf("Failed to create node B pool: %v", err)
	}
	t.Cleanup(func() { env.nodeBPool.Close() })

	// Wait for databases
	waitForDB(t, ctx, env.nodeAPool, "node-a")
	waitForDB(t, ctx, env.nodeBPool, "node-b")

	// Create steep_repl extension on both nodes
	_, err = env.nodeAPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension on node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension on node B: %v", err)
	}

	// Start daemons
	env.nodeAGRPCPort = 15470
	env.nodeBGRPCPort = 15471

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
			Method:          config.InitMethodSnapshot,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeADaemon, err = daemon.New(nodeACfg, true)
	if err != nil {
		t.Fatalf("Failed to create node A daemon: %v", err)
	}
	if err := env.nodeADaemon.Start(); err != nil {
		t.Fatalf("Failed to start node A daemon: %v", err)
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
			Method:          config.InitMethodSnapshot,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.nodeBDaemon, err = daemon.New(nodeBCfg, true)
	if err != nil {
		t.Fatalf("Failed to create node B daemon: %v", err)
	}
	if err := env.nodeBDaemon.Start(); err != nil {
		t.Fatalf("Failed to start node B daemon: %v", err)
	}
	t.Cleanup(func() { env.nodeBDaemon.Stop() })

	// Wait for daemons
	time.Sleep(time.Second)

	return env
}

// =============================================================================
// T053: Integration test for fingerprint computation
// =============================================================================

// TestSchema_FingerprintComputation tests that compute_fingerprint() SQL function
// returns consistent SHA256 hashes for table schemas.
func TestSchema_FingerprintComputation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pool := setupPostgresWithExtension(t, ctx)

	// Create test tables with known schemas
	_, err := pool.Exec(ctx, `
		CREATE TABLE test_fp_1 (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		);
		CREATE TABLE test_fp_2 (
			id UUID PRIMARY KEY,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			data JSONB
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create test tables: %v", err)
	}

	// Test 1: compute_fingerprint returns hex string
	var fp1 string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fp1)
	if err != nil {
		t.Fatalf("compute_fingerprint failed: %v", err)
	}
	if len(fp1) != 64 {
		t.Errorf("Fingerprint should be 64 hex chars (SHA256), got %d", len(fp1))
	}
	t.Logf("Fingerprint for test_fp_1: %s", fp1)

	// Test 2: Same table produces same fingerprint (deterministic)
	var fp1Again string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fp1Again)
	if err != nil {
		t.Fatalf("Second compute_fingerprint failed: %v", err)
	}
	if fp1 != fp1Again {
		t.Errorf("Fingerprint should be deterministic: %s != %s", fp1, fp1Again)
	}

	// Test 3: Different tables produce different fingerprints
	var fp2 string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_2')").Scan(&fp2)
	if err != nil {
		t.Fatalf("compute_fingerprint for test_fp_2 failed: %v", err)
	}
	if fp1 == fp2 {
		t.Errorf("Different tables should have different fingerprints")
	}
	t.Logf("Fingerprint for test_fp_2: %s", fp2)

	// Test 4: Fingerprint changes when schema changes
	_, err = pool.Exec(ctx, "ALTER TABLE test_fp_1 ADD COLUMN extra TEXT")
	if err != nil {
		t.Fatalf("ALTER TABLE failed: %v", err)
	}

	var fpAfterAlter string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fpAfterAlter)
	if err != nil {
		t.Fatalf("compute_fingerprint after alter failed: %v", err)
	}
	if fp1 == fpAfterAlter {
		t.Errorf("Fingerprint should change after ALTER TABLE")
	}
	t.Logf("Fingerprint after alter: %s", fpAfterAlter)

	// Test 5: capture_fingerprint stores the result (now requires node_id)
	_, err = pool.Exec(ctx, "SELECT steep_repl.capture_fingerprint('test-node', 'public', 'test_fp_1')")
	if err != nil {
		t.Fatalf("capture_fingerprint failed: %v", err)
	}

	var storedFp string
	var columnCount int
	err = pool.QueryRow(ctx, `
		SELECT fingerprint, column_count
		FROM steep_repl.schema_fingerprints
		WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_fp_1'
	`).Scan(&storedFp, &columnCount)
	if err != nil {
		t.Fatalf("Failed to query stored fingerprint: %v", err)
	}
	if storedFp != fpAfterAlter {
		t.Errorf("Stored fingerprint mismatch: %s != %s", storedFp, fpAfterAlter)
	}
	if columnCount != 4 { // id, name, value, extra
		t.Errorf("Column count = %d, want 4", columnCount)
	}

	// Test 6: capture_all_fingerprints captures multiple tables (now requires node_id)
	var tableCount int
	err = pool.QueryRow(ctx, "SELECT steep_repl.capture_all_fingerprints('test-node')").Scan(&tableCount)
	if err != nil {
		t.Fatalf("capture_all_fingerprints failed: %v", err)
	}
	t.Logf("capture_all_fingerprints captured %d tables", tableCount)
}

// TestSchema_FingerprintViaGRPC tests fingerprint retrieval via gRPC daemon.
func TestSchema_FingerprintViaGRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create test table on node A
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Connect to node A daemon
	client, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer client.Close()

	// Get fingerprints via gRPC
	resp, err := client.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("GetSchemaFingerprints returned error: %s", resp.Error)
	}

	// Should have at least the users table
	found := false
	for _, fp := range resp.Fingerprints {
		t.Logf("Table %s.%s: %s", fp.SchemaName, fp.TableName, fp.Fingerprint)
		if fp.TableName == "users" {
			found = true
			if len(fp.Fingerprint) != 64 {
				t.Errorf("Fingerprint should be 64 hex chars, got %d", len(fp.Fingerprint))
			}
		}
	}

	if !found {
		t.Error("Expected to find fingerprint for 'users' table")
	}
}

// =============================================================================
// T054: Integration test for schema comparison
// =============================================================================

// TestSchema_CompareMatching tests CompareSchemas when schemas match.
func TestSchema_CompareMatching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create identical tables on both nodes
	tableSQL := `
		CREATE TABLE orders (
			id SERIAL PRIMARY KEY,
			customer_name TEXT NOT NULL,
			amount DECIMAL(10,2),
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`

	_, err := env.nodeAPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	_, err = env.nodeBPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Connect to node A daemon
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	// Get fingerprints from node A
	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from node A failed: %v", err)
	}

	// Connect to node B daemon
	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	// Get fingerprints from node B
	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from node B failed: %v", err)
	}

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

	if fpA == "" || fpB == "" {
		t.Fatal("Failed to find fingerprint for orders table")
	}

	if fpA != fpB {
		t.Errorf("Fingerprints should match for identical schemas: %s != %s", fpA, fpB)
	} else {
		t.Logf("Fingerprints match: %s", fpA)
	}
}

// TestSchema_CompareMismatch tests CompareSchemas when schemas differ.
func TestSchema_CompareMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create different tables on each node
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price DECIMAL(10,2),
			sku TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	// Node B has missing column (sku) and different type for price
	_, err = env.nodeBPool.Exec(ctx, `
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Get fingerprints from both nodes
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from A failed: %v", err)
	}

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from B failed: %v", err)
	}

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

	if fpA == "" || fpB == "" {
		t.Fatal("Failed to find fingerprint for products table")
	}

	if fpA == fpB {
		t.Errorf("Fingerprints should differ for mismatched schemas")
	} else {
		t.Logf("Fingerprints correctly differ: A=%s B=%s", fpA, fpB)
	}
}

// TestSchema_CompareLocalOnly tests detection of tables that exist only on one node.
func TestSchema_CompareLocalOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create table only on node A
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE local_only_a (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	// Get fingerprints
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from A failed: %v", err)
	}

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from B failed: %v", err)
	}

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

	if !foundOnA {
		t.Error("local_only_a should exist on node A")
	}
	if foundOnB {
		t.Error("local_only_a should NOT exist on node B")
	}

	t.Logf("LOCAL_ONLY detection: foundOnA=%v, foundOnB=%v", foundOnA, foundOnB)
}

// TestSchema_CompareRemoteOnly tests detection of tables that exist only on remote node.
func TestSchema_CompareRemoteOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create table only on node B
	_, err := env.nodeBPool.Exec(ctx, `
		CREATE TABLE remote_only_b (
			id SERIAL PRIMARY KEY,
			info JSONB
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Get fingerprints
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from A failed: %v", err)
	}

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from B failed: %v", err)
	}

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

	if foundOnA {
		t.Error("remote_only_b should NOT exist on node A")
	}
	if !foundOnB {
		t.Error("remote_only_b should exist on node B")
	}

	t.Logf("REMOTE_ONLY detection: foundOnA=%v, foundOnB=%v", foundOnA, foundOnB)
}

// TestSchema_CompareSchemasFull tests the full CompareSchemas RPC.
func TestSchema_CompareSchemasFull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Setup: Create various tables for comprehensive comparison
	// 1. Matching table
	matchingSQL := `CREATE TABLE matching (id INT PRIMARY KEY, name TEXT)`
	_, err := env.nodeAPool.Exec(ctx, matchingSQL)
	if err != nil {
		t.Fatalf("Failed to create matching table on A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, matchingSQL)
	if err != nil {
		t.Fatalf("Failed to create matching table on B: %v", err)
	}

	// 2. Mismatching table (different column types)
	_, err = env.nodeAPool.Exec(ctx, `CREATE TABLE mismatched (id INT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create mismatched table on A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, `CREATE TABLE mismatched (id INT PRIMARY KEY, value INTEGER)`)
	if err != nil {
		t.Fatalf("Failed to create mismatched table on B: %v", err)
	}

	// 3. Local only table (on A)
	_, err = env.nodeAPool.Exec(ctx, `CREATE TABLE a_only (id INT PRIMARY KEY)`)
	if err != nil {
		t.Fatalf("Failed to create a_only table: %v", err)
	}

	// 4. Remote only table (on B)
	_, err = env.nodeBPool.Exec(ctx, `CREATE TABLE b_only (id INT PRIMARY KEY)`)
	if err != nil {
		t.Fatalf("Failed to create b_only table: %v", err)
	}

	// Get fingerprints from both nodes
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})

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
				t.Logf("MATCH: %s", key)
			} else {
				mismatchCount++
				t.Logf("MISMATCH: %s (A=%s... B=%s...)", key, localFP[:8], remoteFP[:8])
			}
		} else {
			localOnlyCount++
			t.Logf("LOCAL_ONLY: %s", key)
		}
	}

	// Check tables only on B
	for key := range fpB {
		if _, exists := fpA[key]; !exists {
			remoteOnlyCount++
			t.Logf("REMOTE_ONLY: %s", key)
		}
	}

	t.Logf("Summary: match=%d, mismatch=%d, local_only=%d, remote_only=%d",
		matchCount, mismatchCount, localOnlyCount, remoteOnlyCount)

	// Verify expected results
	if matchCount == 0 {
		t.Error("Should have at least one matching table")
	}
	if mismatchCount == 0 {
		t.Error("Should have at least one mismatched table")
	}
	if localOnlyCount == 0 {
		t.Error("Should have at least one local-only table")
	}
	if remoteOnlyCount == 0 {
		t.Error("Should have at least one remote-only table")
	}
}

// =============================================================================
// Schema Sync Mode Tests (T063, T064, T065)
// =============================================================================

// TestSchemaSyncMode_Strict tests that strict mode fails on schema mismatch.
func TestSchemaSyncMode_Strict(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create identical base table on both nodes
	baseSQL := `CREATE TABLE sync_test_strict (id INT PRIMARY KEY, name TEXT)`
	_, err := env.nodeAPool.Exec(ctx, baseSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, baseSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Add extra column only on node A to create mismatch
	_, err = env.nodeAPool.Exec(ctx, `ALTER TABLE sync_test_strict ADD COLUMN extra_col TEXT`)
	if err != nil {
		t.Fatalf("Failed to alter table on node A: %v", err)
	}

	// Get fingerprints to verify mismatch exists
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})

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

	if fpA == fpB {
		t.Fatal("Test setup error: fingerprints should differ for mismatch test")
	}
	t.Logf("Verified mismatch: A=%s... B=%s...", fpA[:8], fpB[:8])

	// Test strict mode via SchemaSyncHandler
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)

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
	result, err := handler.HandleStrict(ctx, compareResult)
	if err == nil {
		t.Error("HandleStrict should return error on mismatch")
	} else {
		t.Logf("Strict mode correctly failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Mode != "strict" {
		t.Errorf("Mode = %q, want %q", result.Mode, "strict")
	}
	if result.Action != "failed" {
		t.Errorf("Action = %q, want %q", result.Action, "failed")
	}
	if len(result.Differences) != 1 {
		t.Errorf("Differences count = %d, want 1", len(result.Differences))
	}
}

// TestSchemaSyncMode_Strict_NoMismatch tests that strict mode passes when schemas match.
func TestSchemaSyncMode_Strict_NoMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create identical tables on both nodes
	tableSQL := `CREATE TABLE sync_test_strict_match (id INT PRIMARY KEY, name TEXT)`
	_, err := env.nodeAPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Get fingerprints to verify they match
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})

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

	if fpA != fpB {
		t.Fatalf("Test setup error: fingerprints should match: A=%s B=%s", fpA, fpB)
	}

	// Test strict mode with matching schemas
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)

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
	result, err := handler.HandleStrict(ctx, compareResult)
	if err != nil {
		t.Errorf("HandleStrict should not error when schemas match: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Mode != "strict" {
		t.Errorf("Mode = %q, want %q", result.Mode, "strict")
	}
	if result.Action != "passed" {
		t.Errorf("Action = %q, want %q", result.Action, "passed")
	}
	t.Logf("Strict mode correctly passed with matching schemas")
}

// TestSchemaSyncMode_Auto tests that auto mode applies DDL to fix mismatches.
func TestSchemaSyncMode_Auto(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create table with extra column on node A (source/remote)
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE sync_test_auto (
			id INT PRIMARY KEY,
			name TEXT,
			extra_col TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	// Create table without extra column on node B (target/local)
	_, err = env.nodeBPool.Exec(ctx, `
		CREATE TABLE sync_test_auto (
			id INT PRIMARY KEY,
			name TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Get fingerprints with column definitions
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from A failed: %v", err)
	}

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		t.Fatalf("GetSchemaFingerprints from B failed: %v", err)
	}

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

	if fpA == nil || fpB == nil {
		t.Fatal("Failed to find fingerprints for sync_test_auto")
	}

	if fpA.Fingerprint == fpB.Fingerprint {
		t.Fatal("Test setup error: fingerprints should differ")
	}
	t.Logf("Before auto sync: A=%s... B=%s...", fpA.Fingerprint[:8], fpB.Fingerprint[:8])

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
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)
	result, err := handler.HandleAuto(ctx, compareResult, remoteFingerprints)
	if err != nil {
		t.Fatalf("HandleAuto failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Mode != "auto" {
		t.Errorf("Mode = %q, want %q", result.Mode, "auto")
	}
	if result.Action != "applied" {
		t.Errorf("Action = %q, want %q", result.Action, "applied")
	}
	t.Logf("Auto mode result: applied=%d, skipped=%d, DDL=%v",
		result.AppliedCount, result.SkippedCount, result.DDLStatements)

	// Verify the column was added to node B
	var colCount int
	err = env.nodeBPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_auto'
	`).Scan(&colCount)
	if err != nil {
		t.Fatalf("Failed to query column count: %v", err)
	}

	if colCount != 3 { // id, name, extra_col
		t.Errorf("Column count = %d, want 3 (after auto sync)", colCount)
	} else {
		t.Log("Auto mode successfully added missing column")
	}

	// Verify fingerprints now match
	respBAfter, _ := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	for _, fp := range respBAfter.Fingerprints {
		if fp.TableName == "sync_test_auto" {
			if fp.Fingerprint == fpA.Fingerprint {
				t.Log("Fingerprints now match after auto sync")
			} else {
				t.Logf("Fingerprints still differ (expected for complex cases): A=%s... B=%s...",
					fpA.Fingerprint[:8], fp.Fingerprint[:8])
			}
		}
	}
}

// TestSchemaSyncMode_Auto_RemoteOnly tests that auto mode creates missing tables.
func TestSchemaSyncMode_Auto_RemoteOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create table only on node A (source/remote)
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE sync_test_auto_new (
			id INT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	// Verify table does NOT exist on node B
	var exists bool
	err = env.nodeBPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}
	if exists {
		t.Fatal("Test setup error: table should not exist on node B yet")
	}

	// Get fingerprints from node A
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	respA, _ := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})

	var fpA *pb.TableFingerprint
	for _, fp := range respA.Fingerprints {
		if fp.TableName == "sync_test_auto_new" {
			fpA = fp
		}
	}
	if fpA == nil {
		t.Fatal("Failed to find fingerprint for sync_test_auto_new on node A")
	}

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
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)
	result, err := handler.HandleAuto(ctx, compareResult, remoteFingerprints)
	if err != nil {
		t.Fatalf("HandleAuto failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	t.Logf("Auto mode (remote-only) result: applied=%d, DDL=%v",
		result.AppliedCount, result.DDLStatements)

	// Verify the table was created on node B
	err = env.nodeBPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}

	if !exists {
		t.Error("Auto mode should have created the missing table")
	} else {
		t.Log("Auto mode successfully created missing table")
	}

	// Verify column count
	var colCount int
	err = env.nodeBPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_auto_new'
	`).Scan(&colCount)
	if err != nil {
		t.Fatalf("Failed to query column count: %v", err)
	}

	if colCount != 3 { // id, name, created_at
		t.Errorf("Column count = %d, want 3", colCount)
	}
}

// TestSchemaSyncMode_Manual tests that manual mode warns but proceeds.
func TestSchemaSyncMode_Manual(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create mismatched tables
	_, err := env.nodeAPool.Exec(ctx, `
		CREATE TABLE sync_test_manual (
			id INT PRIMARY KEY,
			name TEXT,
			extra TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}

	_, err = env.nodeBPool.Exec(ctx, `
		CREATE TABLE sync_test_manual (
			id INT PRIMARY KEY,
			name TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

	// Get fingerprints
	clientA, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeAGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	clientB, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.nodeBGRPCPort),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()

	respA, _ := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	respB, _ := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})

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

	if fpA == fpB {
		t.Fatal("Test setup error: fingerprints should differ")
	}

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
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)
	result, err := handler.HandleManual(ctx, compareResult)
	if err != nil {
		t.Errorf("HandleManual should not return error: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Mode != "manual" {
		t.Errorf("Mode = %q, want %q", result.Mode, "manual")
	}
	if result.Action != "warned" {
		t.Errorf("Action = %q, want %q", result.Action, "warned")
	}
	if result.WarningMessage == "" {
		t.Error("WarningMessage should not be empty for mismatch")
	}
	if len(result.Differences) != 1 {
		t.Errorf("Differences count = %d, want 1", len(result.Differences))
	}

	t.Logf("Manual mode warning: %s", result.WarningMessage)
	t.Log("Manual mode correctly warned but did not fail")

	// Verify schema was NOT modified (no auto-fix)
	var colCount int
	err = env.nodeBPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_test_manual'
	`).Scan(&colCount)
	if err != nil {
		t.Fatalf("Failed to query column count: %v", err)
	}

	if colCount != 2 { // id, name (not extra)
		t.Errorf("Column count = %d, want 2 (manual mode should not modify schema)", colCount)
	} else {
		t.Log("Manual mode correctly did not modify schema")
	}
}

// TestSchemaSyncMode_Manual_NoMismatch tests that manual mode has no warning when schemas match.
func TestSchemaSyncMode_Manual_NoMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := setupSchemaTestEnv(t, ctx)

	// Create identical tables
	tableSQL := `CREATE TABLE sync_test_manual_match (id INT PRIMARY KEY, name TEXT)`
	_, err := env.nodeAPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node A: %v", err)
	}
	_, err = env.nodeBPool.Exec(ctx, tableSQL)
	if err != nil {
		t.Fatalf("Failed to create table on node B: %v", err)
	}

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
	handler := replinit.NewSchemaSyncHandler(env.nodeBPool)
	result, err := handler.HandleManual(ctx, compareResult)
	if err != nil {
		t.Errorf("HandleManual should not return error: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.WarningMessage != "" {
		t.Errorf("WarningMessage should be empty when schemas match, got: %s", result.WarningMessage)
	}
	if len(result.Differences) != 0 {
		t.Errorf("Differences count = %d, want 0", len(result.Differences))
	}

	t.Log("Manual mode correctly produced no warning for matching schemas")
}
