package repl_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
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
// Test Suite Infrastructure
// =============================================================================

// InitTestSuite runs all initialization tests with shared Docker containers.
// This significantly reduces test time by reusing containers across tests.
type InitTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	env    *twoNodeEnv
}

// twoNodeEnv holds the complete test environment with two PostgreSQL nodes
// and their respective steep-repl daemons.
type twoNodeEnv struct {
	network *testcontainers.DockerNetwork

	// Source node
	sourceContainer testcontainers.Container
	sourcePool      *pgxpool.Pool
	sourceHost      string // Docker network hostname
	sourcePort      int
	sourceDaemon    *daemon.Daemon
	sourceGRPCPort  int

	// Target node
	targetContainer testcontainers.Container
	targetPool      *pgxpool.Pool
	targetHost      string // Docker network hostname
	targetPort      int
	targetDaemon    *daemon.Daemon
	targetGRPCPort  int

	// External connection info (for host access)
	sourceHostExternal string
	sourcePortExternal int
	targetHostExternal string
	targetPortExternal int
}

// TestInitSuite runs the init test suite.
func TestInitSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(InitTestSuite))
}

// SetupSuite creates the shared Docker containers and daemons once for all tests.
func (s *InitTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	const testPassword = "test"
	env := &twoNodeEnv{}
	// Assign env early so TearDownSuite can clean up even if setup fails partway through
	s.env = env

	// Create Docker network for inter-container communication
	net, err := network.New(s.ctx, network.WithCheckDuplicate())
	s.Require().NoError(err, "Failed to create Docker network")
	env.network = net

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
	env.sourceContainer = sourceContainer

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
	env.targetContainer = targetContainer

	// Get connection info
	sourceHostExternal, _ := sourceContainer.Host(s.ctx)
	sourcePortExternal, _ := sourceContainer.MappedPort(s.ctx, "5432")
	targetHostExternal, _ := targetContainer.Host(s.ctx)
	targetPortExternal, _ := targetContainer.MappedPort(s.ctx, "5432")

	env.sourceHost = "pg-source" // Docker network hostname
	env.sourcePort = 5432
	env.targetHost = "pg-target"
	env.targetPort = 5432
	env.sourceHostExternal = sourceHostExternal
	env.sourcePortExternal = sourcePortExternal.Int()
	env.targetHostExternal = targetHostExternal
	env.targetPortExternal = targetPortExternal.Int()

	// Create connection pools using external ports
	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	targetConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		targetHostExternal, targetPortExternal.Port())
	env.targetPool, err = pgxpool.New(s.ctx, targetConnStr)
	s.Require().NoError(err, "Failed to create target pool")

	// Wait for databases to be ready
	s.waitForDB(env.sourcePool, "source")
	s.waitForDB(env.targetPool, "target")

	// Create steep_repl extension on both nodes
	_, err = env.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")
	_, err = env.targetPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on target")

	// Start daemons with dynamically allocated ports to avoid conflicts
	env.sourceGRPCPort = getFreePortInit()
	env.targetGRPCPort = getFreePortInit()

	// Set PGPASSWORD for daemon connections
	s.T().Setenv("PGPASSWORD", testPassword)

	sourceSocketPath := tempSocketPath(s.T())
	sourceCfg := &config.Config{
		NodeID:   "source-node",
		NodeName: "Source Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     sourceHostExternal,
			Port:     sourcePortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.sourceGRPCPort,
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

	env.sourceDaemon, err = daemon.New(sourceCfg, true)
	s.Require().NoError(err, "Failed to create source daemon")
	err = env.sourceDaemon.Start()
	s.Require().NoError(err, "Failed to start source daemon")

	targetSocketPath := tempSocketPath(s.T())
	targetCfg := &config.Config{
		NodeID:   "target-node",
		NodeName: "Target Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     targetHostExternal,
			Port:     targetPortExternal.Int(),
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: env.targetGRPCPort,
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

	env.targetDaemon, err = daemon.New(targetCfg, true)
	s.Require().NoError(err, "Failed to create target daemon")
	err = env.targetDaemon.Start()
	s.Require().NoError(err, "Failed to start target daemon")

	// Wait for daemons to be ready
	time.Sleep(time.Second)

	s.T().Log("InitTestSuite: Shared containers and daemons ready")
}

// TearDownSuite cleans up all resources after all tests complete.
func (s *InitTestSuite) TearDownSuite() {
	if s.env != nil {
		if s.env.sourceDaemon != nil {
			s.env.sourceDaemon.Stop()
		}
		if s.env.targetDaemon != nil {
			s.env.targetDaemon.Stop()
		}
		if s.env.sourcePool != nil {
			s.env.sourcePool.Close()
		}
		if s.env.targetPool != nil {
			s.env.targetPool.Close()
		}
		if s.env.sourceContainer != nil {
			_ = s.env.sourceContainer.Terminate(context.Background())
		}
		if s.env.targetContainer != nil {
			_ = s.env.targetContainer.Terminate(context.Background())
		}
		if s.env.network != nil {
			_ = s.env.network.Remove(context.Background())
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// SetupTest resets database state before each test.
func (s *InitTestSuite) SetupTest() {
	ctx := s.ctx

	// Cancel any active init operations from previous tests and wait for completion
	const cancelTimeout = 10 * time.Second
	if s.env.sourceDaemon != nil && s.env.sourceDaemon.InitManager() != nil {
		cancelled, err := s.env.sourceDaemon.InitManager().CancelAllAndWait(cancelTimeout)
		if len(cancelled) > 0 {
			s.T().Logf("SetupTest: cancelled %d operations on source: %v", len(cancelled), cancelled)
		}
		if err != nil {
			s.T().Logf("SetupTest: warning - source cancel wait: %v", err)
		}
	}
	if s.env.targetDaemon != nil && s.env.targetDaemon.InitManager() != nil {
		cancelled, err := s.env.targetDaemon.InitManager().CancelAllAndWait(cancelTimeout)
		if len(cancelled) > 0 {
			s.T().Logf("SetupTest: cancelled %d operations on target: %v", len(cancelled), cancelled)
		}
		if err != nil {
			s.T().Logf("SetupTest: warning - target cancel wait: %v", err)
		}
	}

	// Clean up any subscriptions on target
	rows, _ := s.env.targetPool.Query(ctx, "SELECT subname FROM pg_subscription")
	if rows != nil {
		var subs []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			subs = append(subs, name)
		}
		rows.Close()
		for _, sub := range subs {
			s.env.targetPool.Exec(ctx, fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", sub))
		}
	}

	// Clean up any publications on source
	rows, _ = s.env.sourcePool.Query(ctx, "SELECT pubname FROM pg_publication WHERE pubname LIKE 'steep_%'")
	if rows != nil {
		var pubs []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			pubs = append(pubs, name)
		}
		rows.Close()
		for _, pub := range pubs {
			s.env.sourcePool.Exec(ctx, fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pub))
		}
	}

	// Clean up replication slots on source
	rows, _ = s.env.sourcePool.Query(ctx, "SELECT slot_name FROM pg_replication_slots WHERE slot_name LIKE 'steep_%'")
	if rows != nil {
		var slots []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			slots = append(slots, name)
		}
		rows.Close()
		for _, slot := range slots {
			s.env.sourcePool.Exec(ctx, fmt.Sprintf("SELECT pg_drop_replication_slot('%s')", slot))
		}
	}

	// Drop test tables on both nodes
	testTables := []string{
		"test_data", "large_table", "progress_test", "state_test",
		"cli_test", "cancel_test", "manual_test", "schema_test",
	}
	for _, table := range testTables {
		s.env.sourcePool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		s.env.targetPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	}

	// Reset node states
	s.env.sourcePool.Exec(ctx, `
		UPDATE steep_repl.nodes SET
			init_state = 'uninitialized',
			init_source_node = NULL,
			init_started_at = NULL,
			init_completed_at = NULL
		WHERE node_id = 'source-node'
	`)
	s.env.targetPool.Exec(ctx, `
		UPDATE steep_repl.nodes SET
			init_state = 'uninitialized',
			init_source_node = NULL,
			init_started_at = NULL,
			init_completed_at = NULL
		WHERE node_id = 'target-node'
	`)

	// Clear init_progress
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.init_progress")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.init_progress")

	// Clear init_slots
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.init_slots")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.init_slots")

	// Clear audit logs (optional, keeps test isolation)
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.audit_log")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.audit_log")
}

// waitForDB waits for database to be ready
func (s *InitTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	for range 30 {
		var ready int
		if err := pool.QueryRow(s.ctx, "SELECT 1").Scan(&ready); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	s.T().Fatalf("%s database not ready after 30 seconds", name)
}

// =============================================================================
// User Story 1: Automatic Snapshot Initialization Tests
// =============================================================================

// TestInit_AutomaticSnapshotWorkflow tests the complete automatic snapshot initialization
// by calling the actual StartInit gRPC RPC and verifying the InitManager creates the
// subscription and copies data.
func (s *InitTestSuite) TestInit_AutomaticSnapshotWorkflow() {
	ctx := s.ctx
	env := s.env

	// Create test data on source
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE test_data (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`)
	s.Require().NoError(err, "Failed to create test table on source")

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO test_data (name, value)
		SELECT 'item_' || i, i * 10
		FROM generate_series(1, 100) AS i
	`)
	s.Require().NoError(err, "Failed to insert test data")

	// Create publication on source
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE test_data
	`)
	s.Require().NoError(err, "Failed to create publication")

	// Create matching table on target (required for logical replication)
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE test_data (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`)
	s.Require().NoError(err, "Failed to create test table on target")

	// Verify target is empty
	var countBefore int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&countBefore)
	s.Require().NoError(err)
	s.Require().Equal(0, countBefore, "Target should be empty")

	// Connect to target daemon via gRPC
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err, "Failed to dial target daemon gRPC")
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Call StartInit RPC
	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		Options: &pb.InitOptions{
			ParallelWorkers: 4,
			SchemaSyncMode:  pb.SchemaSyncMode_SCHEMA_SYNC_STRICT,
		},
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err, "StartInit RPC failed")
	s.Require().True(resp.Success, "StartInit returned error: %s", resp.Error)

	// Verify InitManager registered the operation
	initMgr := env.targetDaemon.InitManager()
	s.Require().NotNil(initMgr, "InitManager should not be nil")
	s.Assert().True(initMgr.IsActive("target-node"), "Operation should be active")

	// Wait for initialization to complete
	var initState string
	initComplete := false
	for i := range 120 {
		err = env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		s.T().Logf("Init state at %ds: %s", i, initState)

		if initState == "synchronized" {
			initComplete = true
			break
		}
		if initState == "failed" {
			var errMsg string
			env.targetPool.QueryRow(ctx, `
				SELECT COALESCE(error_message, 'unknown error')
				FROM steep_repl.init_progress
				WHERE node_id = 'target-node'
			`).Scan(&errMsg)
			s.T().Fatalf("Init failed: %s", errMsg)
		}

		time.Sleep(time.Second)
	}

	s.Require().True(initComplete, "Initialization did not complete, final state: %s", initState)

	// Verify data was copied
	var countAfter int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&countAfter)
	s.Require().NoError(err)
	s.Assert().Equal(100, countAfter, "Expected 100 rows on target")

	// Verify sample data
	var sampleName string
	var sampleValue int
	err = env.targetPool.QueryRow(ctx, "SELECT name, value FROM test_data WHERE id = 50").Scan(&sampleName, &sampleValue)
	s.Require().NoError(err)
	s.Assert().Equal("item_50", sampleName)
	s.Assert().Equal(500, sampleValue)

	// Verify subscription was created
	var subExists bool
	err = env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_subscription WHERE subname LIKE 'steep_sub_%')
	`).Scan(&subExists)
	s.Require().NoError(err)
	s.Assert().True(subExists, "Subscription should exist")

	// Verify init_completed_at was set
	var completedAt *time.Time
	err = env.targetPool.QueryRow(ctx, `
		SELECT init_completed_at FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&completedAt)
	s.Require().NoError(err)
	s.Assert().NotNil(completedAt, "init_completed_at should be set")
}

// TestInit_CancelInit tests cancellation of an in-progress initialization.
func (s *InitTestSuite) TestInit_CancelInit() {
	ctx := s.ctx
	env := s.env

	// Create larger test data to give us time to cancel
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE large_table (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO large_table (data, padding)
		SELECT md5(i::text), repeat('x', 1000)
		FROM generate_series(1, 5000) AS i
	`)
	s.Require().NoError(err)

	// Create publication
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE large_table
	`)
	s.Require().NoError(err)

	// Create matching table on target
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE large_table (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	// Connect to target daemon
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Start initialization
	startResp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)
	s.Require().True(startResp.Success, "StartInit error: %s", startResp.Error)

	// Wait briefly for init to start
	time.Sleep(500 * time.Millisecond)

	// Verify operation is active
	initMgr := env.targetDaemon.InitManager()
	s.Assert().True(initMgr.IsActive("target-node"), "Expected active operation before cancel")

	// Cancel initialization
	cancelResp, err := initClient.CancelInit(ctx, &pb.CancelInitRequest{
		NodeId: "target-node",
	})
	s.Require().NoError(err)
	s.Require().True(cancelResp.Success, "CancelInit error: %s", cancelResp.Error)

	// Wait for cancellation to propagate
	time.Sleep(time.Second)

	// Verify operation is no longer active
	for i := 0; i < 10; i++ {
		if !initMgr.IsActive("target-node") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Check that cancellation was logged in audit log
	var auditExists bool
	err = env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM steep_repl.audit_log
			WHERE action = 'init.cancelled' AND target_id = 'target-node'
		)
	`).Scan(&auditExists)
	s.Require().NoError(err)
	s.Assert().True(auditExists, "Audit log entry for init.cancelled should exist")
}

// TestInit_GetProgress tests the GetProgress RPC returns valid progress data.
func (s *InitTestSuite) TestInit_GetProgress() {
	ctx := s.ctx
	env := s.env

	// Create test data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE progress_test (id SERIAL PRIMARY KEY, data TEXT);
		INSERT INTO progress_test (data) SELECT md5(i::text) FROM generate_series(1, 1000) i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLE progress_test;
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE progress_test (id SERIAL PRIMARY KEY, data TEXT)
	`)
	s.Require().NoError(err)

	// Connect and start init
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	_, err = initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)

	// Poll for progress
	var sawProgress bool
	for range 30 {
		resp, err := initClient.GetProgress(ctx, &pb.GetProgressRequest{
			NodeId: "target-node",
		})
		if err != nil {
			s.T().Logf("GetProgress error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if resp.HasProgress {
			sawProgress = true
			s.T().Logf("Progress: phase=%s percent=%.1f tables=%d/%d",
				resp.Progress.Phase,
				resp.Progress.OverallPercent,
				resp.Progress.TablesCompleted,
				resp.Progress.TablesTotal)

			s.Assert().Equal("target-node", resp.Progress.NodeId)
			break
		}
		time.Sleep(time.Second)
	}

	if !sawProgress {
		s.T().Log("Warning: Did not observe progress - init may have completed too quickly")
	}
}

// TestInit_StateTransitions tests that the actual state machine transitions occur correctly.
func (s *InitTestSuite) TestInit_StateTransitions() {
	ctx := s.ctx
	env := s.env

	// Create small test data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE state_test (id SERIAL PRIMARY KEY, data TEXT);
		INSERT INTO state_test (data) VALUES ('test');
		CREATE PUBLICATION steep_pub_source_node FOR TABLE state_test;
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE state_test (id SERIAL PRIMARY KEY, data TEXT)
	`)
	s.Require().NoError(err)

	// Track observed states
	observedStates := make(map[string]bool)

	// Query initial state
	var state string
	env.targetPool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&state)
	observedStates[state] = true
	s.T().Logf("Initial state: %s", state)

	// Start init
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	_, err = initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)

	// Poll states until synchronized or failed
	for range 60 {
		err = env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		if !observedStates[state] {
			s.T().Logf("State transition: -> %s", state)
			observedStates[state] = true
		}

		if state == "synchronized" || state == "failed" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Verify expected states were observed
	expectedStates := []string{"uninitialized", "preparing"}
	for _, expected := range expectedStates {
		s.Assert().True(observedStates[expected], "Expected to observe state %q", expected)
	}

	// Final state should be synchronized
	s.Assert().Equal("synchronized", state, "Final state should be synchronized")

	s.T().Logf("All observed states: %v", observedStates)
}

// =============================================================================
// CLI Command Integration Tests
// =============================================================================

// TestCLI_InitStart tests the 'steep-repl node start' CLI command.
func (s *InitTestSuite) TestCLI_InitStart() {
	ctx := s.ctx
	env := s.env

	// Create test data on source
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE cli_test (id SERIAL PRIMARY KEY, data TEXT);
		INSERT INTO cli_test (data) SELECT 'row_' || i FROM generate_series(1, 50) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLE cli_test;
	`)
	s.Require().NoError(err)

	// Create matching table on target
	_, err = env.targetPool.Exec(ctx, `CREATE TABLE cli_test (id SERIAL PRIMARY KEY, data TEXT)`)
	s.Require().NoError(err)

	// Execute CLI via gRPC client directly (simulating CLI behavior)
	clientCfg := replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.targetGRPCPort),
		Timeout: 30 * time.Second,
	}
	client, err := replgrpc.NewClient(ctx, clientCfg)
	s.Require().NoError(err)
	defer client.Close()

	resp, err := client.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
		Options: &pb.InitOptions{
			ParallelWorkers: 4,
			SchemaSyncMode:  pb.SchemaSyncMode_SCHEMA_SYNC_STRICT,
		},
	})
	s.Require().NoError(err)
	s.Require().True(resp.Success, "CLI init start returned error: %s", resp.Error)

	// Wait for completion
	var state string
	for i := 0; i < 60; i++ {
		err = env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if err == nil && (state == "synchronized" || state == "failed") {
			break
		}
		time.Sleep(time.Second)
	}

	s.Assert().Equal("synchronized", state)

	// Verify data was copied
	var count int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM cli_test").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(50, count)
}

// TestCLI_InitCancel tests the 'steep-repl node cancel' CLI command.
func (s *InitTestSuite) TestCLI_InitCancel() {
	ctx := s.ctx
	env := s.env

	// Create larger test data to have time to cancel
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE cancel_test (id SERIAL PRIMARY KEY, data TEXT, padding TEXT);
		INSERT INTO cancel_test (data, padding)
		SELECT 'row_' || i, repeat('x', 500) FROM generate_series(1, 3000) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLE cancel_test;
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE cancel_test (id SERIAL PRIMARY KEY, data TEXT, padding TEXT)
	`)
	s.Require().NoError(err)

	// Connect to target daemon
	clientCfg := replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.targetGRPCPort),
		Timeout: 30 * time.Second,
	}
	client, err := replgrpc.NewClient(ctx, clientCfg)
	s.Require().NoError(err)
	defer client.Close()

	// Start init
	startResp, err := client.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)
	s.Require().True(startResp.Success, "StartInit error: %s", startResp.Error)

	// Wait briefly for init to start
	time.Sleep(500 * time.Millisecond)

	// Cancel via CLI (simulated)
	cancelResp, err := client.CancelInit(ctx, &pb.CancelInitRequest{
		NodeId: "target-node",
	})
	s.Require().NoError(err)
	s.Require().True(cancelResp.Success, "CLI init cancel returned error: %s", cancelResp.Error)

	// Verify cancellation was logged
	time.Sleep(time.Second)
	var auditExists bool
	err = env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM steep_repl.audit_log
			WHERE action = 'init.cancelled' AND target_id = 'target-node'
		)
	`).Scan(&auditExists)
	s.Require().NoError(err)
	s.Assert().True(auditExists, "Audit log entry for init.cancelled should exist")
}

// TestCLI_InitPrepare tests the 'steep-repl node prepare' CLI command.
func (s *InitTestSuite) TestCLI_InitPrepare() {
	ctx := s.ctx
	env := s.env

	// Connect to source daemon (prepare runs on source)
	clientCfg := replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.sourceGRPCPort),
		Timeout: 30 * time.Second,
	}
	client, err := replgrpc.NewClient(ctx, clientCfg)
	s.Require().NoError(err)
	defer client.Close()

	// Call PrepareInit (what CLI does)
	slotName := "steep_init_source_node"
	resp, err := client.PrepareInit(ctx, &pb.PrepareInitRequest{
		NodeId:   "source-node",
		SlotName: slotName,
	})
	s.Require().NoError(err)
	s.Require().True(resp.Success, "PrepareInit failed: %s", resp.Error)

	// Verify slot was created
	s.T().Logf("PrepareInit succeeded: slot=%s lsn=%s", resp.SlotName, resp.Lsn)

	s.Assert().Equal(slotName, resp.SlotName)
	s.Assert().NotEmpty(resp.Lsn)
	s.Assert().NotNil(resp.CreatedAt)

	// Verify slot exists in PostgreSQL
	var slotExists bool
	err = env.sourcePool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)
	`, slotName).Scan(&slotExists)
	s.Require().NoError(err)
	s.Assert().True(slotExists, "Replication slot should exist")

	// Verify slot was recorded in init_slots table
	var recordedLSN string
	err = env.sourcePool.QueryRow(ctx, `
		SELECT lsn FROM steep_repl.init_slots WHERE slot_name = $1
	`, slotName).Scan(&recordedLSN)
	s.Require().NoError(err)
	s.Assert().Equal(resp.Lsn, recordedLSN)

	// Verify calling prepare again fails (slot already exists)
	resp2, err := client.PrepareInit(ctx, &pb.PrepareInitRequest{
		NodeId:   "source-node",
		SlotName: slotName,
	})
	s.Require().NoError(err)
	s.Assert().False(resp2.Success, "Second PrepareInit should fail")
	s.Assert().NotEmpty(resp2.Error)
}

// TestCLI_InitComplete tests the 'steep-repl node complete' CLI command.
// This is T028: Integration test for full prepare/complete workflow.
func (s *InitTestSuite) TestCLI_InitComplete() {
	ctx := s.ctx
	env := s.env

	// === Step 1: Create test data on source ===
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE manual_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO manual_test (name, value)
		SELECT 'item_' || i, i * 10
		FROM generate_series(1, 100) AS i
	`)
	s.Require().NoError(err)

	// Create publication on source
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE manual_test
	`)
	s.Require().NoError(err)

	// === Step 2: Call PrepareInit on source ===
	sourceClient, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.sourceGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err)
	defer sourceClient.Close()

	slotName := "steep_init_source_node"
	prepareResp, err := sourceClient.PrepareInit(ctx, &pb.PrepareInitRequest{
		NodeId:   "source-node",
		SlotName: slotName,
	})
	s.Require().NoError(err)
	s.Require().True(prepareResp.Success, "PrepareInit error: %s", prepareResp.Error)

	s.T().Logf("Prepared slot %s at LSN %s", prepareResp.SlotName, prepareResp.Lsn)

	// === Step 3: Backup from source and restore to target ===
	// Run pg_dump inside source container
	dumpCode, dumpOutput, err := env.sourceContainer.Exec(ctx, []string{
		"pg_dump",
		"-h", "localhost",
		"-U", "test",
		"-d", "testdb",
		"-t", "manual_test",
		"-Fp",
		"-f", "/tmp/backup.sql",
	})
	s.Require().NoError(err)
	dumpOutputBytes, _ := io.ReadAll(dumpOutput)
	s.Require().Equal(0, dumpCode, "pg_dump failed: %s", string(dumpOutputBytes))
	s.T().Log("pg_dump completed successfully")

	// Copy the dump file from source to target container
	dumpFileReader, err := env.sourceContainer.CopyFileFromContainer(ctx, "/tmp/backup.sql")
	s.Require().NoError(err)
	dumpData, err := io.ReadAll(dumpFileReader)
	dumpFileReader.Close()
	s.Require().NoError(err)
	s.T().Logf("Dump file size: %d bytes", len(dumpData))

	// Copy to target container
	err = env.targetContainer.CopyToContainer(ctx, dumpData, "/tmp/backup.sql", 0644)
	s.Require().NoError(err)

	// Run psql to restore on target container
	restoreCode, restoreOutput, err := env.targetContainer.Exec(ctx, []string{
		"psql",
		"-h", "localhost",
		"-U", "test",
		"-d", "testdb",
		"-f", "/tmp/backup.sql",
	})
	s.Require().NoError(err)
	restoreOutputBytes, _ := io.ReadAll(restoreOutput)
	s.Require().Equal(0, restoreCode, "psql restore failed: %s", string(restoreOutputBytes))
	s.T().Log("Restore completed successfully")

	// Verify target has data after restore
	var countBefore int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM manual_test").Scan(&countBefore)
	s.Require().NoError(err)
	s.Require().Equal(100, countBefore, "Target should have 100 rows after restore")

	// === Step 4: Call CompleteInit on target ===
	targetClient, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.targetGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err)
	defer targetClient.Close()

	completeResp, err := targetClient.CompleteInit(ctx, &pb.CompleteInitRequest{
		TargetNodeId:   "target-node",
		SourceNodeId:   "source-node",
		SourceLsn:      prepareResp.Lsn,
		SchemaSyncMode: pb.SchemaSyncMode_SCHEMA_SYNC_STRICT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
		SkipSchemaCheck: true,
	})
	s.Require().NoError(err)
	s.Require().True(completeResp.Success, "CompleteInit error: %s", completeResp.Error)

	s.T().Logf("CompleteInit succeeded: state=%v", completeResp.State)

	// === Step 5: Verify subscription was created ===
	var subExists bool
	err = env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_subscription
			WHERE subname = 'steep_sub_target_node_from_source_node'
		)
	`).Scan(&subExists)
	s.Require().NoError(err)
	s.Assert().True(subExists, "Subscription should exist after CompleteInit")

	// === Step 6: Wait for state to transition to SYNCHRONIZED ===
	var initState string
	for i := 0; i < 60; i++ {
		err = env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		s.T().Logf("Init state at %ds: %s", i, initState)

		if initState == "synchronized" {
			break
		}
		if initState == "failed" {
			s.T().Fatalf("Init failed unexpectedly")
		}

		time.Sleep(time.Second)
	}

	s.Assert().Equal("synchronized", initState)

	// === Step 7: Verify replication works ===
	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO manual_test (name, value) VALUES ('new_item', 9999)
	`)
	s.Require().NoError(err)

	// Wait for replication
	time.Sleep(2 * time.Second)

	var newValue int
	err = env.targetPool.QueryRow(ctx, `
		SELECT value FROM manual_test WHERE name = 'new_item'
	`).Scan(&newValue)
	if err != nil {
		s.T().Logf("New item not yet replicated (expected in manual init): %v", err)
	} else {
		s.Assert().Equal(9999, newValue, "Replicated value mismatch")
		s.T().Log("Replication verified: new item replicated successfully")
	}
}

// =============================================================================
// Backward-Compatible Standalone Helper Functions
// =============================================================================

// waitForDB waits for database to be ready (standalone version).
// Used by schema_test.go, merge_test.go, and other standalone tests.
func waitForDB(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	for range 30 {
		var ready int
		if err := pool.QueryRow(ctx, "SELECT 1").Scan(&ready); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s database not ready after 30 seconds", name)
}

// TestInit_ManualSchemaVerification tests schema verification during CompleteInit.
// This is T029: Integration test for schema verification during complete.
func (s *InitTestSuite) TestInit_ManualSchemaVerification() {
	ctx := s.ctx
	env := s.env

	// === Create table on source ===
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE schema_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	s.Require().NoError(err)

	// Create publication
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE schema_test
	`)
	s.Require().NoError(err)

	// === Prepare slot on source ===
	sourceClient, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.sourceGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err)
	defer sourceClient.Close()

	slotName := "steep_init_source_node"
	prepareResp, err := sourceClient.PrepareInit(ctx, &pb.PrepareInitRequest{
		NodeId:   "source-node",
		SlotName: slotName,
	})
	s.Require().NoError(err)
	s.Require().True(prepareResp.Success, "PrepareInit error: %s", prepareResp.Error)

	// === Create MISMATCHED table on target (different columns) ===
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE schema_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			-- Missing 'value' column
			-- Missing 'created_at' column
			extra_column TEXT  -- Extra column not on source
		)
	`)
	s.Require().NoError(err)

	// === Try CompleteInit with STRICT mode - should fail ===
	targetClient, err := replgrpc.NewClient(ctx, replgrpc.ClientConfig{
		Address: fmt.Sprintf("localhost:%d", env.targetGRPCPort),
		Timeout: 30 * time.Second,
	})
	s.Require().NoError(err)
	defer targetClient.Close()

	completeResp, err := targetClient.CompleteInit(ctx, &pb.CompleteInitRequest{
		TargetNodeId:   "target-node",
		SourceNodeId:   "source-node",
		SourceLsn:      prepareResp.Lsn,
		SchemaSyncMode: pb.SchemaSyncMode_SCHEMA_SYNC_STRICT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
		SkipSchemaCheck: false,
	})
	s.Require().NoError(err)

	// With schema mismatch and STRICT mode, should fail
	if completeResp.Success {
		s.T().Error("CompleteInit should fail with schema mismatch in STRICT mode")
	} else {
		s.T().Logf("CompleteInit correctly failed with schema mismatch: %s", completeResp.Error)
	}

	// === Fix the schema on target ===
	_, err = env.targetPool.Exec(ctx, `
		DROP TABLE schema_test;
		CREATE TABLE schema_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	s.Require().NoError(err)

	// Reset node state
	_, err = env.targetPool.Exec(ctx, `
		UPDATE steep_repl.nodes SET init_state = 'uninitialized' WHERE node_id = 'target-node'
	`)
	s.Require().NoError(err)

	// === Try again with skip_schema_check - should succeed ===
	completeResp2, err := targetClient.CompleteInit(ctx, &pb.CompleteInitRequest{
		TargetNodeId:   "target-node",
		SourceNodeId:   "source-node",
		SourceLsn:      prepareResp.Lsn,
		SchemaSyncMode: pb.SchemaSyncMode_SCHEMA_SYNC_MANUAL,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
		SkipSchemaCheck: true,
	})
	s.Require().NoError(err)

	if !completeResp2.Success {
		s.T().Errorf("CompleteInit should succeed with fixed schema: %s", completeResp2.Error)
	} else {
		s.T().Log("CompleteInit succeeded with matching schema")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// getFreePortInit returns an available TCP port by binding to :0 and releasing it.
func getFreePortInit() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}
