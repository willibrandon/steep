package repl_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// =============================================================================
// Reinit Test Suite
// =============================================================================

// ReinitTestSuite runs reinit integration tests with shared PostgreSQL containers.
// Containers are shared across tests for efficiency, but daemons are recreated
// for each test to ensure clean state.
type ReinitTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc

	// Shared across all tests (containers only)
	network         *testcontainers.DockerNetwork
	sourceContainer testcontainers.Container
	targetContainer testcontainers.Container
	sourcePool      *pgxpool.Pool
	targetPool      *pgxpool.Pool

	// Connection info
	sourceHost         string // Docker network hostname
	sourcePort         int
	targetHost         string
	targetPort         int
	sourceHostExternal string
	sourcePortExternal int
	targetHostExternal string
	targetPortExternal int

	// Per-test daemons (recreated for each test)
	sourceDaemon   *daemon.Daemon
	targetDaemon   *daemon.Daemon
	sourceGRPCPort int
	targetGRPCPort int
}

func TestReinitSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(ReinitTestSuite))
}

func (s *ReinitTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.T().Log("Setting up Reinit test suite - creating shared PostgreSQL containers...")

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	// Create Docker network for inter-container communication
	net, err := network.New(s.ctx, network.WithCheckDuplicate())
	s.Require().NoError(err, "Failed to create Docker network")
	s.network = net

	// Start source PostgreSQL container
	sourceReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-source"},
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

	sourceContainer, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: sourceReq,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start source container")
	s.sourceContainer = sourceContainer

	// Start target PostgreSQL container
	targetReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-target"},
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

	targetContainer, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: targetReq,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start target container")
	s.targetContainer = targetContainer

	// Get connection info
	sourceHostExternal, _ := sourceContainer.Host(s.ctx)
	sourcePortExternal, _ := sourceContainer.MappedPort(s.ctx, "5432")
	targetHostExternal, _ := targetContainer.Host(s.ctx)
	targetPortExternal, _ := targetContainer.MappedPort(s.ctx, "5432")

	s.sourceHost = "pg-source"
	s.sourcePort = 5432
	s.targetHost = "pg-target"
	s.targetPort = 5432
	s.sourceHostExternal = sourceHostExternal
	s.sourcePortExternal = sourcePortExternal.Int()
	s.targetHostExternal = targetHostExternal
	s.targetPortExternal = targetPortExternal.Int()

	// Create connection pools
	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	s.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	targetConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		targetHostExternal, targetPortExternal.Port())
	s.targetPool, err = pgxpool.New(s.ctx, targetConnStr)
	s.Require().NoError(err, "Failed to create target pool")

	// Wait for databases to be ready
	s.waitForDB(s.sourcePool, "source")
	s.waitForDB(s.targetPool, "target")

	// Create steep_repl extension on both nodes
	_, err = s.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")
	_, err = s.targetPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on target")

	s.T().Log("Reinit test suite setup complete - containers ready")
}

func (s *ReinitTestSuite) TearDownSuite() {
	s.T().Log("Tearing down Reinit test suite...")

	if s.sourcePool != nil {
		s.sourcePool.Close()
	}
	if s.targetPool != nil {
		s.targetPool.Close()
	}
	if s.sourceContainer != nil {
		_ = s.sourceContainer.Terminate(context.Background())
	}
	if s.targetContainer != nil {
		_ = s.targetContainer.Terminate(context.Background())
	}
	if s.network != nil {
		_ = s.network.Remove(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}

	s.T().Log("Reinit test suite teardown complete")
}

func (s *ReinitTestSuite) SetupTest() {
	ctx := s.ctx

	// Clean up subscriptions on target
	rows, err := s.targetPool.Query(ctx, `
		SELECT subname FROM pg_subscription WHERE subname LIKE 'steep_sub_%'
	`)
	if err == nil {
		var subs []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			subs = append(subs, name)
		}
		rows.Close()
		for _, sub := range subs {
			s.targetPool.Exec(ctx, fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", sub))
			s.targetPool.Exec(ctx, fmt.Sprintf("ALTER SUBSCRIPTION %s SET (slot_name = NONE)", sub))
			s.targetPool.Exec(ctx, fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", sub))
		}
	}

	// Clean up publications on source
	s.sourcePool.Exec(ctx, "DROP PUBLICATION IF EXISTS steep_pub_source_node CASCADE")

	// Clean up replication slots on source
	rows, err = s.sourcePool.Query(ctx, `
		SELECT slot_name FROM pg_replication_slots WHERE slot_name LIKE 'steep_sub_%'
	`)
	if err == nil {
		var slots []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			slots = append(slots, name)
		}
		rows.Close()
		for _, slot := range slots {
			s.sourcePool.Exec(ctx, fmt.Sprintf("SELECT pg_drop_replication_slot('%s')", slot))
		}
	}

	// Drop test tables on both nodes
	testTables := []string{
		"orders", "products", "customers",
		"full_reinit_test",
	}
	for _, table := range testTables {
		s.sourcePool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		s.targetPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	}

	// Drop test schemas
	s.sourcePool.Exec(ctx, "DROP SCHEMA IF EXISTS sales CASCADE")
	s.targetPool.Exec(ctx, "DROP SCHEMA IF EXISTS sales CASCADE")

	// Delete node registrations (will be re-created by fresh daemons)
	s.sourcePool.Exec(ctx, "DELETE FROM steep_repl.nodes WHERE node_id = 'source-node'")
	s.targetPool.Exec(ctx, "DELETE FROM steep_repl.nodes WHERE node_id = 'target-node'")

	// Create fresh daemons for this test
	s.sourceGRPCPort = 15462
	s.targetGRPCPort = 15463

	sourceSocketPath := tempSocketPath(s.T())
	sourceCfg := &config.Config{
		NodeID:   "source-node",
		NodeName: "Source Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     s.sourceHostExternal,
			Port:     s.sourcePortExternal,
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: s.sourceGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    sourceSocketPath,
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

	s.sourceDaemon, err = daemon.New(sourceCfg, true)
	s.Require().NoError(err, "Failed to create source daemon")
	err = s.sourceDaemon.Start()
	s.Require().NoError(err, "Failed to start source daemon")

	targetSocketPath := tempSocketPath(s.T())
	targetCfg := &config.Config{
		NodeID:   "target-node",
		NodeName: "Target Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     s.targetHostExternal,
			Port:     s.targetPortExternal,
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: s.targetGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    targetSocketPath,
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

	s.targetDaemon, err = daemon.New(targetCfg, true)
	s.Require().NoError(err, "Failed to create target daemon")
	err = s.targetDaemon.Start()
	s.Require().NoError(err, "Failed to start target daemon")

	// Wait for daemons to be ready
	time.Sleep(500 * time.Millisecond)
}

func (s *ReinitTestSuite) TearDownTest() {
	// Stop daemons after each test
	if s.sourceDaemon != nil {
		s.sourceDaemon.Stop()
		s.sourceDaemon = nil
	}
	if s.targetDaemon != nil {
		s.targetDaemon.Stop()
		s.targetDaemon = nil
	}
}

// =============================================================================
// Helper Methods
// =============================================================================

func (s *ReinitTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	for range 30 {
		var result int
		if err := pool.QueryRow(s.ctx, "SELECT 1").Scan(&result); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.T().Fatalf("Database %s not ready", name)
}

// =============================================================================
// Partial Reinit Tests
// =============================================================================

// TestReinit_PartialByTableList tests partial reinitialization of specific tables.
// This is T045: Integration test for partial reinit by table list.
//
// Scenario:
// 1. Set up two nodes with data synchronized
// 2. Corrupt data in one table on target
// 3. Run reinit --node target --tables orders
// 4. Verify only that table was resynchronized
func (s *ReinitTestSuite) TestReinit_PartialByTableList() {
	t := s.T()
	ctx := s.ctx

	// === Step 1: Create test tables on source ===
	_, err := s.sourcePool.Exec(ctx, `
		CREATE TABLE orders (
			id SERIAL PRIMARY KEY,
			customer TEXT NOT NULL,
			total DECIMAL(10,2)
		);
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price DECIMAL(10,2)
		);
		CREATE TABLE customers (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables on source: %v", err)
	}

	// Insert test data
	_, err = s.sourcePool.Exec(ctx, `
		INSERT INTO orders (customer, total) SELECT 'customer_' || i, i * 10.00 FROM generate_series(1, 50) AS i;
		INSERT INTO products (name, price) SELECT 'product_' || i, i * 5.00 FROM generate_series(1, 30) AS i;
		INSERT INTO customers (name, email) SELECT 'cust_' || i, 'cust' || i || '@example.com' FROM generate_series(1, 20) AS i;
	`)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Create publication
	_, err = s.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE orders, products, customers
	`)
	if err != nil {
		t.Fatalf("Failed to create publication: %v", err)
	}

	// === Step 2: Create matching tables on target ===
	_, err = s.targetPool.Exec(ctx, `
		CREATE TABLE orders (id SERIAL PRIMARY KEY, customer TEXT NOT NULL, total DECIMAL(10,2));
		CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT NOT NULL, price DECIMAL(10,2));
		CREATE TABLE customers (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables on target: %v", err)
	}

	// === Step 3: Initialize target from source ===
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", s.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial target: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Start initialization
	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     s.sourceHost,
			Port:     int32(s.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for initialization to complete
	var initState string
	for range 120 {
		err = s.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if initState == "synchronized" {
			break
		}
		if initState == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}
	if initState != "synchronized" {
		t.Fatalf("Init did not complete, state: %s", initState)
	}

	// Verify data was copied
	var ordersCount, productsCount, customersCount int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersCount)
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM products").Scan(&productsCount)
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customersCount)

	if ordersCount != 50 || productsCount != 30 || customersCount != 20 {
		t.Fatalf("Data mismatch after init: orders=%d, products=%d, customers=%d",
			ordersCount, productsCount, customersCount)
	}

	t.Log("Initial sync completed successfully")

	// === Step 4: Corrupt data in orders table on target ===
	// This simulates data divergence that requires reinit
	_, err = s.targetPool.Exec(ctx, `
		UPDATE orders SET total = -999.99 WHERE id <= 10;
		DELETE FROM orders WHERE id > 40;
	`)
	if err != nil {
		t.Fatalf("Failed to corrupt orders data: %v", err)
	}

	// Verify corruption
	var corruptedCount int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE total = -999.99").Scan(&corruptedCount)
	if corruptedCount != 10 {
		t.Fatalf("Corruption check failed: expected 10 corrupted rows, got %d", corruptedCount)
	}

	var ordersAfterCorrupt int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersAfterCorrupt)
	t.Logf("Orders after corruption: %d rows (was 50)", ordersAfterCorrupt)

	// === Step 5: Call reinit for just the orders table ===
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Tables{
				Tables: &pb.TableList{
					Tables: []string{"public.orders"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Reinit started, state: %s", reinitResp.State.String())

	// === Step 6: Verify ONLY the orders table was affected ===
	// We specified only public.orders, so TablesAffected should be exactly 1
	if reinitResp.TablesAffected != 1 {
		t.Fatalf("Expected exactly 1 table affected (orders), got %d", reinitResp.TablesAffected)
	}
	t.Logf("Tables affected: %d (expected 1)", reinitResp.TablesAffected)

	// Verify orders table was truncated (should be empty now)
	var ordersAfterReinit int
	err = s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersAfterReinit)
	if err != nil {
		t.Fatalf("Failed to count orders: %v", err)
	}
	if ordersAfterReinit != 0 {
		t.Fatalf("orders table should be empty after reinit, got %d rows", ordersAfterReinit)
	}
	t.Log("Verified: orders table was truncated (0 rows)")

	// Verify state transition
	err = s.targetPool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&initState)
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}
	// For partial reinit, state should be catching_up (waiting for resync)
	if initState != "catching_up" && initState != "reinitializing" {
		t.Errorf("Expected state 'catching_up' or 'reinitializing', got %q", initState)
	}
	t.Logf("State after reinit: %s", initState)

	// === Step 7: Verify products and customers tables were NOT affected ===
	// These tables should still have their original data
	var productsAfter, customersAfter int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM products").Scan(&productsAfter)
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customersAfter)

	if productsAfter != 30 {
		t.Errorf("products table was affected by partial reinit: got %d rows, want 30", productsAfter)
	}
	if customersAfter != 20 {
		t.Errorf("customers table was affected by partial reinit: got %d rows, want 20", customersAfter)
	}

	t.Log("Verified other tables were not affected by partial reinit")
}

// =============================================================================
// Schema Scope Tests
// =============================================================================

// TestReinit_SchemaScope tests reinitialization of all tables in a schema.
func (s *ReinitTestSuite) TestReinit_SchemaScope() {
	t := s.T()
	ctx := s.ctx

	// Create schema with tables on source
	_, err := s.sourcePool.Exec(ctx, `
		CREATE SCHEMA sales;
		CREATE TABLE sales.orders (id SERIAL PRIMARY KEY, customer TEXT);
		CREATE TABLE sales.invoices (id SERIAL PRIMARY KEY, order_id INTEGER);
		INSERT INTO sales.orders (customer) SELECT 'cust_' || i FROM generate_series(1, 25) AS i;
		INSERT INTO sales.invoices (order_id) SELECT i FROM generate_series(1, 25) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLES IN SCHEMA sales;
	`)
	if err != nil {
		t.Fatalf("Failed to setup source: %v", err)
	}

	// Create matching schema on target
	_, err = s.targetPool.Exec(ctx, `
		CREATE SCHEMA sales;
		CREATE TABLE sales.orders (id SERIAL PRIMARY KEY, customer TEXT);
		CREATE TABLE sales.invoices (id SERIAL PRIMARY KEY, order_id INTEGER);
	`)
	if err != nil {
		t.Fatalf("Failed to setup target: %v", err)
	}

	// Initialize
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", s.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     s.sourceHost,
			Port:     int32(s.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for sync
	for range 60 {
		var state string
		s.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if state == "synchronized" {
			break
		}
		if state == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}

	// Verify data
	var ordersCount, invoicesCount int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.orders").Scan(&ordersCount)
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.invoices").Scan(&invoicesCount)
	if ordersCount != 25 || invoicesCount != 25 {
		t.Fatalf("Data mismatch: orders=%d invoices=%d", ordersCount, invoicesCount)
	}

	// Corrupt both tables
	_, err = s.targetPool.Exec(ctx, `
		DELETE FROM sales.orders WHERE id > 10;
		DELETE FROM sales.invoices WHERE id > 10;
	`)
	if err != nil {
		t.Fatalf("Failed to corrupt: %v", err)
	}

	// Reinit by schema
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Schema{
				Schema: "sales",
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Schema reinit started: state=%s tables_affected=%d",
		reinitResp.State.String(), reinitResp.TablesAffected)

	// Verify reinit affected exactly 2 tables (sales.orders and sales.invoices)
	if reinitResp.TablesAffected != 2 {
		t.Fatalf("Expected exactly 2 tables affected (sales.orders, sales.invoices), got %d", reinitResp.TablesAffected)
	}

	// Verify both tables were truncated
	var ordersAfter, invoicesAfter int
	err = s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.orders").Scan(&ordersAfter)
	if err != nil {
		t.Fatalf("Failed to count sales.orders: %v", err)
	}
	err = s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.invoices").Scan(&invoicesAfter)
	if err != nil {
		t.Fatalf("Failed to count sales.invoices: %v", err)
	}

	if ordersAfter != 0 {
		t.Errorf("sales.orders should be empty after schema reinit, got %d rows", ordersAfter)
	}
	if invoicesAfter != 0 {
		t.Errorf("sales.invoices should be empty after schema reinit, got %d rows", invoicesAfter)
	}

	t.Log("Verified: both schema tables were truncated")
}

// =============================================================================
// Full Node Reinit Tests
// =============================================================================

// TestReinit_FullNode tests complete node reinitialization.
func (s *ReinitTestSuite) TestReinit_FullNode() {
	t := s.T()
	ctx := s.ctx

	// Create test table
	_, err := s.sourcePool.Exec(ctx, `
		CREATE TABLE full_reinit_test (id SERIAL PRIMARY KEY, data TEXT);
		INSERT INTO full_reinit_test (data) SELECT 'row_' || i FROM generate_series(1, 50) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLE full_reinit_test;
	`)
	if err != nil {
		t.Fatalf("Failed to setup source: %v", err)
	}

	_, err = s.targetPool.Exec(ctx, `
		CREATE TABLE full_reinit_test (id SERIAL PRIMARY KEY, data TEXT)
	`)
	if err != nil {
		t.Fatalf("Failed to setup target: %v", err)
	}

	// Initialize
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", s.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     s.sourceHost,
			Port:     int32(s.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for sync
	var initState string
	for range 60 {
		s.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if initState == "synchronized" {
			break
		}
		if initState == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}
	if initState != "synchronized" {
		t.Fatalf("Init did not complete: %s", initState)
	}

	// Verify subscription exists
	var subExists bool
	s.targetPool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_subscription WHERE subname LIKE 'steep_sub_%')
	`).Scan(&subExists)
	if !subExists {
		t.Fatal("Subscription should exist after init")
	}

	// Full reinit
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Full{
				Full: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Full reinit completed: state=%s", reinitResp.State.String())

	// Verify state is UNINITIALIZED
	s.targetPool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&initState)

	if initState != "uninitialized" {
		t.Errorf("Expected state 'uninitialized' after full reinit, got %q", initState)
	}

	// Verify subscription was dropped
	s.targetPool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_subscription WHERE subname LIKE 'steep_sub_%')
	`).Scan(&subExists)
	if subExists {
		t.Error("Subscription should be dropped after full reinit")
	}

	// Verify data was truncated
	var count int
	s.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM full_reinit_test").Scan(&count)
	if count != 0 {
		t.Errorf("Table should be empty after full reinit, got %d rows", count)
	}

	t.Log("Full reinit verified: subscription dropped, data truncated, state reset")
}
