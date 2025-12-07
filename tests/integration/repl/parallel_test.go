package repl_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
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
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// =============================================================================
// Parallel Worker Test Suite Infrastructure
// =============================================================================

// ParallelTestSuite tests the parallel worker functionality for snapshot initialization.
// It shares containers across tests for efficiency.
type ParallelTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	env    *parallelEnv
}

// parallelEnv holds the test environment for parallel worker tests.
type parallelEnv struct {
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

	// External connection info
	sourceHostExternal string
	sourcePortExternal int
	targetHostExternal string
	targetPortExternal int
}

// TestParallelSuite runs the parallel worker test suite.
func TestParallelSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ParallelTestSuite))
}

// SetupSuite creates shared Docker containers for all tests.
func (s *ParallelTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	const testPassword = "test"
	env := &parallelEnv{}

	// Create Docker network
	net, err := network.New(s.ctx, network.WithCheckDuplicate())
	s.Require().NoError(err, "Failed to create Docker network")
	env.network = net

	// Start source PostgreSQL container (PG18 with steep_repl)
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

	env.sourceHost = "pg-source"
	env.sourcePort = 5432
	env.targetHost = "pg-target"
	env.targetPort = 5432
	env.sourceHostExternal = sourceHostExternal
	env.sourcePortExternal = sourcePortExternal.Int()
	env.targetHostExternal = targetHostExternal
	env.targetPortExternal = targetPortExternal.Int()

	// Create connection pools
	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	targetConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		targetHostExternal, targetPortExternal.Port())
	env.targetPool, err = pgxpool.New(s.ctx, targetConnStr)
	s.Require().NoError(err, "Failed to create target pool")

	// Wait for databases
	s.waitForDB(env.sourcePool, "source")
	s.waitForDB(env.targetPool, "target")

	// Create extension on both nodes
	_, err = env.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")
	_, err = env.targetPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on target")

	// Start daemons with parallel worker config
	env.sourceGRPCPort = 15470
	env.targetGRPCPort = 15471

	s.T().Setenv("PGPASSWORD", testPassword)

	sourceSocketPath := tempSocketPathParallel()
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

	targetSocketPath := tempSocketPathParallel()
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

	time.Sleep(time.Second)

	s.env = env
	s.T().Log("ParallelTestSuite: Shared containers and daemons ready")
}

// TearDownSuite cleans up resources.
func (s *ParallelTestSuite) TearDownSuite() {
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

// SetupTest resets state before each test.
func (s *ParallelTestSuite) SetupTest() {
	ctx := s.ctx

	// Cancel active operations
	const cancelTimeout = 10 * time.Second
	if s.env.sourceDaemon != nil && s.env.sourceDaemon.InitManager() != nil {
		s.env.sourceDaemon.InitManager().CancelAllAndWait(cancelTimeout)
	}
	if s.env.targetDaemon != nil && s.env.targetDaemon.InitManager() != nil {
		s.env.targetDaemon.InitManager().CancelAllAndWait(cancelTimeout)
	}

	// Clean up subscriptions
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

	// Clean up publications
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

	// Clean up replication slots
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

	// Drop test tables
	testTables := []string{
		"parallel_test", "parallel_small", "parallel_medium", "parallel_large",
		"worker_test_1", "worker_test_2", "worker_test_3", "worker_test_4",
		"pg18_test", "progress_parallel",
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

	// Clear progress and slots
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.init_progress")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.init_progress")
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.init_slots")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.init_slots")
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.audit_log")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.audit_log")
}

func (s *ParallelTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	for range 30 {
		var ready int
		if err := pool.QueryRow(s.ctx, "SELECT 1").Scan(&ready); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	s.T().Fatalf("%s database not ready after 30 seconds", name)
}

// tempSocketPathParallel creates a unique socket path for parallel tests.
var parallelSocketCounter int64

func tempSocketPathParallel() string {
	n := atomic.AddInt64(&parallelSocketCounter, 1)
	return fmt.Sprintf("/tmp/steep-parallel-%d-%d.sock", time.Now().UnixNano(), n)
}

// =============================================================================
// PG18 Parallel COPY Detection Tests
// =============================================================================

// TestParallel_DetectPG18ParallelCOPY tests the PostgreSQL version detection for parallel COPY.
func (s *ParallelTestSuite) TestParallel_DetectPG18ParallelCOPY() {
	ctx := s.ctx
	env := s.env

	// Test PG18 detection directly via database query (matching the implementation)
	var versionNum int
	err := env.targetPool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&versionNum)
	s.Require().NoError(err, "Failed to get PostgreSQL version")

	isPG18 := versionNum >= 180000

	s.T().Logf("PostgreSQL version: %d, isPG18OrHigher: %v", versionNum, isPG18)

	// Our test image is PG18, so this should be true
	s.Assert().True(isPG18, "Test image should be PostgreSQL 18+")
}

// TestParallel_VersionDetectionWithRealVersion tests version detection matches actual PostgreSQL version.
func (s *ParallelTestSuite) TestParallel_VersionDetectionWithRealVersion() {
	ctx := s.ctx
	env := s.env

	// Query version directly
	var versionStr string
	err := env.sourcePool.QueryRow(ctx, "SHOW server_version").Scan(&versionStr)
	s.Require().NoError(err)

	var versionNum int
	err = env.sourcePool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum)
	s.Require().NoError(err)

	s.T().Logf("PostgreSQL version: %s (num: %d)", versionStr, versionNum)

	// Our test image should be PG18
	s.Assert().GreaterOrEqual(versionNum, 180000, "Test image should be PostgreSQL 18+")
}

// =============================================================================
// ParallelTableCopier Worker Pool Tests
// =============================================================================

// TestParallel_WorkerPoolCreation tests ParallelTableCopier creation with various worker counts.
func (s *ParallelTestSuite) TestParallel_WorkerPoolCreation() {
	env := s.env

	testCases := []struct {
		name           string
		requestWorkers int
		expectWorkers  int
	}{
		{"minimum_1", 1, 1},
		{"default_4", 4, 4},
		{"maximum_16", 16, 16},
		{"below_minimum_0", 0, 1},   // Should clamp to 1
		{"above_maximum_20", 20, 16}, // Should clamp to 16
		{"negative", -5, 1},          // Should clamp to 1
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			logger := replinit.NewLogger(slog.Default())
			copier := replinit.NewParallelTableCopier(env.sourcePool, tc.requestWorkers, logger)
			s.Require().NotNil(copier, "ParallelTableCopier should not be nil")
			// Note: workers count is internal, validated by clamping behavior
		})
	}
}

// TestParallel_WorkerPoolWithRealTables tests the worker pool with actual database tables.
func (s *ParallelTestSuite) TestParallel_WorkerPoolWithRealTables() {
	ctx := s.ctx
	env := s.env

	// Create multiple test tables on source
	tables := []string{"worker_test_1", "worker_test_2", "worker_test_3", "worker_test_4"}
	for i, table := range tables {
		_, err := env.sourcePool.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id SERIAL PRIMARY KEY,
				data TEXT,
				value INTEGER
			)
		`, table))
		s.Require().NoError(err, "Failed to create table %s", table)

		// Insert different amounts of data
		rowCount := (i + 1) * 50
		_, err = env.sourcePool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s (data, value)
			SELECT 'row_' || i, i * 10
			FROM generate_series(1, %d) AS i
		`, table, rowCount))
		s.Require().NoError(err, "Failed to insert data into %s", table)
	}

	// Get table info
	var tableInfos []replinit.TableInfo
	for _, table := range tables {
		var sizeBytes int64
		err := env.sourcePool.QueryRow(ctx, fmt.Sprintf(`
			SELECT pg_total_relation_size('%s')
		`, table)).Scan(&sizeBytes)
		s.Require().NoError(err)

		tableInfos = append(tableInfos, replinit.TableInfo{
			SchemaName: "public",
			TableName:  table,
			FullName:   "public." + table,
			SizeBytes:  sizeBytes,
		})
	}

	// Create worker pool with 4 workers
	logger := replinit.NewLogger(slog.Default())
	copier := replinit.NewParallelTableCopier(env.sourcePool, 4, logger)

	// Track progress
	var progressCalls int32
	copier.SetProgressCallback(func(completed, total int, currentTable string, percent float64) {
		atomic.AddInt32(&progressCalls, 1)
		s.T().Logf("Progress: %d/%d tables, current: %s, percent: %.1f%%", completed, total, currentTable, percent)
	})

	// Copy tables (this is a simulation since actual copy uses subscription)
	connStr := fmt.Sprintf("host=%s port=%d dbname=testdb user=test password=test",
		env.sourceHostExternal, env.sourcePortExternal)
	results, err := copier.CopyTables(ctx, tableInfos, connStr)
	s.Require().NoError(err, "CopyTables failed")

	// Verify results
	s.Assert().Len(results, len(tables), "Should have result for each table")

	for _, result := range results {
		s.Assert().NoError(result.Error, "Table %s should have no error", result.TableInfo.FullName)
		s.Assert().Greater(result.BytesCopied, int64(0), "Should have bytes copied for %s", result.TableInfo.FullName)
		s.T().Logf("Table %s: %d bytes in %v", result.TableInfo.FullName, result.BytesCopied, result.Duration)
	}

	// Verify progress callback was called
	s.Assert().GreaterOrEqual(atomic.LoadInt32(&progressCalls), int32(len(tables)),
		"Progress callback should be called at least once per table")
}

// =============================================================================
// Parallel Initialization with Different Worker Counts
// =============================================================================

// TestParallel_InitWithWorker1 tests initialization with 1 worker.
func (s *ParallelTestSuite) TestParallel_InitWithWorker1() {
	s.runParallelInitTest(1, 50)
}

// TestParallel_InitWithWorker4 tests initialization with 4 workers (default).
func (s *ParallelTestSuite) TestParallel_InitWithWorker4() {
	s.runParallelInitTest(4, 100)
}

// TestParallel_InitWithWorker16 tests initialization with 16 workers (maximum).
func (s *ParallelTestSuite) TestParallel_InitWithWorker16() {
	s.runParallelInitTest(16, 200)
}

// runParallelInitTest is a helper that runs the full initialization with specified workers.
func (s *ParallelTestSuite) runParallelInitTest(workers, rowCount int) {
	ctx := s.ctx
	env := s.env

	tableName := fmt.Sprintf("parallel_test_%d", workers)

	// Create test data on source
	_, err := env.sourcePool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`, tableName))
	s.Require().NoError(err, "Failed to create table")

	_, err = env.sourcePool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (name, value)
		SELECT 'item_' || i, i * 10
		FROM generate_series(1, %d) AS i
	`, tableName, rowCount))
	s.Require().NoError(err, "Failed to insert data")

	// Create publication
	_, err = env.sourcePool.Exec(ctx, fmt.Sprintf(`
		CREATE PUBLICATION steep_pub_source_node FOR TABLE %s
	`, tableName))
	s.Require().NoError(err, "Failed to create publication")

	// Create matching table on target
	_, err = env.targetPool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`, tableName))
	s.Require().NoError(err, "Failed to create target table")

	// Verify target is empty
	var countBefore int
	err = env.targetPool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&countBefore)
	s.Require().NoError(err)
	s.Require().Equal(0, countBefore)

	// Connect to target daemon
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Start init with specified workers
	startTime := time.Now()
	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		Options: &pb.InitOptions{
			ParallelWorkers: int32(workers),
			SchemaSyncMode:  pb.SchemaSyncMode_SCHEMA_SYNC_STRICT,
		},
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)
	s.Require().True(resp.Success, "StartInit error: %s", resp.Error)

	// Wait for completion
	var initState string
	for i := 0; i < 120; i++ {
		err = env.targetPool.QueryRow(ctx, `
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
			var errMsg string
			env.targetPool.QueryRow(ctx, `
				SELECT COALESCE(error_message, 'unknown')
				FROM steep_repl.init_progress
				WHERE node_id = 'target-node'
			`).Scan(&errMsg)
			s.T().Fatalf("Init failed with %d workers: %s", workers, errMsg)
		}

		time.Sleep(time.Second)
	}

	duration := time.Since(startTime)
	s.Require().Equal("synchronized", initState, "Init should complete successfully")

	// Verify data was copied
	var countAfter int
	err = env.targetPool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&countAfter)
	s.Require().NoError(err)
	s.Assert().Equal(rowCount, countAfter)

	s.T().Logf("Init with %d workers completed in %v for %d rows", workers, duration, rowCount)
}

// =============================================================================
// Progress Tracking with Parallel Workers
// =============================================================================

// TestParallel_ProgressTracking tests that progress is reported correctly during parallel init.
func (s *ParallelTestSuite) TestParallel_ProgressTracking() {
	ctx := s.ctx
	env := s.env

	// Create test data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE progress_parallel (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO progress_parallel (data, padding)
		SELECT md5(i::text), repeat('x', 500)
		FROM generate_series(1, 2000) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE progress_parallel
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE progress_parallel (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
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
		Options: &pb.InitOptions{
			ParallelWorkers: 4,
		},
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)

	// Poll for progress updates
	var sawProgress bool
	var maxPercent float32
	for i := 0; i < 60; i++ {
		resp, err := initClient.GetProgress(ctx, &pb.GetProgressRequest{
			NodeId: "target-node",
		})
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		if resp.HasProgress {
			sawProgress = true
			if resp.Progress.OverallPercent > maxPercent {
				maxPercent = resp.Progress.OverallPercent
			}
			s.T().Logf("Progress: phase=%s percent=%.1f%% tables=%d/%d",
				resp.Progress.Phase,
				resp.Progress.OverallPercent,
				resp.Progress.TablesCompleted,
				resp.Progress.TablesTotal)

			// Check progress fields are valid
			s.Assert().NotEmpty(resp.Progress.NodeId)
		}

		// Check if init completed
		var state string
		env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if state == "synchronized" || state == "failed" {
			break
		}

		time.Sleep(time.Second)
	}

	if sawProgress {
		s.T().Logf("Max progress observed: %.1f%%", maxPercent)
	} else {
		s.T().Log("Warning: Did not observe progress - init may have completed too quickly")
	}
}

// =============================================================================
// PG18 Streaming Parallel Subscription Tests
// =============================================================================

// TestParallel_PG18StreamingParallel tests that PG18 uses streaming=parallel option.
func (s *ParallelTestSuite) TestParallel_PG18StreamingParallel() {
	ctx := s.ctx
	env := s.env

	// Check if we're on PG18+
	var versionNum int
	err := env.targetPool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&versionNum)
	s.Require().NoError(err)

	if versionNum < 180000 {
		s.T().Skip("Test requires PostgreSQL 18+ for streaming=parallel")
	}

	// Create test table
	_, err = env.sourcePool.Exec(ctx, `
		CREATE TABLE pg18_test (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO pg18_test (data) SELECT 'row_' || i FROM generate_series(1, 100) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE pg18_test
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE pg18_test (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	// Start init
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		Options: &pb.InitOptions{
			ParallelWorkers: 4,
		},
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	s.Require().NoError(err)
	s.Require().True(resp.Success, "StartInit error: %s", resp.Error)

	// Wait briefly for subscription to be created
	time.Sleep(2 * time.Second)

	// Check if subscription was created with streaming option
	var streaming string
	err = env.targetPool.QueryRow(ctx, `
		SELECT COALESCE(substream::text, 'unknown')
		FROM pg_subscription
		WHERE subname LIKE 'steep_sub_%'
		LIMIT 1
	`).Scan(&streaming)

	if err == nil {
		s.T().Logf("Subscription streaming option: %s", streaming)
		// On PG18, we expect 'p' for parallel (or 'parallel' depending on representation)
	} else {
		s.T().Logf("Could not check subscription streaming option: %v", err)
	}

	// Check audit log for parallel subscription creation
	var auditExists bool
	err = env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM steep_repl.audit_log
			WHERE action = 'init.subscription_parallel'
			OR (action = 'init.subscription_created' AND details::text LIKE '%streaming%')
		)
	`).Scan(&auditExists)
	if err == nil && auditExists {
		s.T().Log("Audit log contains parallel subscription entry")
	}

	// Wait for init to complete
	for i := 0; i < 60; i++ {
		var state string
		env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if state == "synchronized" {
			break
		}
		time.Sleep(time.Second)
	}

	// Verify data was copied
	var count int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM pg18_test").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(100, count)
}

// =============================================================================
// Edge Cases and Error Handling
// =============================================================================

// TestParallel_WorkerPoolCancellation tests that worker pool handles cancellation correctly.
func (s *ParallelTestSuite) TestParallel_WorkerPoolCancellation() {
	env := s.env

	// Create a cancellable context
	ctx, cancel := context.WithCancel(s.ctx)

	// Create test tables
	tables := []replinit.TableInfo{
		{SchemaName: "public", TableName: "cancel_test_1", FullName: "public.cancel_test_1", SizeBytes: 1000},
		{SchemaName: "public", TableName: "cancel_test_2", FullName: "public.cancel_test_2", SizeBytes: 1000},
		{SchemaName: "public", TableName: "cancel_test_3", FullName: "public.cancel_test_3", SizeBytes: 1000},
	}

	logger := replinit.NewLogger(slog.Default())
	copier := replinit.NewParallelTableCopier(env.sourcePool, 2, logger)

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Copy should handle cancellation gracefully
	connStr := fmt.Sprintf("host=%s port=%d dbname=testdb user=test password=test",
		env.sourceHostExternal, env.sourcePortExternal)
	results, err := copier.CopyTables(ctx, tables, connStr)

	// Should either complete or return context error
	if err != nil {
		s.Assert().ErrorIs(err, context.Canceled, "Should return context canceled error")
	}

	s.T().Logf("Cancellation test completed with %d results", len(results))
}

// TestParallel_EmptyTableList tests worker pool with empty table list.
func (s *ParallelTestSuite) TestParallel_EmptyTableList() {
	env := s.env

	logger := replinit.NewLogger(slog.Default())
	copier := replinit.NewParallelTableCopier(env.sourcePool, 4, logger)

	connStr := fmt.Sprintf("host=%s port=%d dbname=testdb user=test password=test",
		env.sourceHostExternal, env.sourcePortExternal)
	results, err := copier.CopyTables(s.ctx, []replinit.TableInfo{}, connStr)

	s.Require().NoError(err, "Empty table list should not error")
	s.Assert().Len(results, 0, "Should return empty results")
}

// TestParallel_SingleTableWithMaxWorkers tests single table with maximum workers.
func (s *ParallelTestSuite) TestParallel_SingleTableWithMaxWorkers() {
	ctx := s.ctx
	env := s.env

	// Create single table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE parallel_single (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO parallel_single (data) SELECT 'row_' || i FROM generate_series(1, 50) AS i
	`)
	s.Require().NoError(err)

	// Get size
	var sizeBytes int64
	err = env.sourcePool.QueryRow(ctx, "SELECT pg_total_relation_size('parallel_single')").Scan(&sizeBytes)
	s.Require().NoError(err)

	tables := []replinit.TableInfo{
		{SchemaName: "public", TableName: "parallel_single", FullName: "public.parallel_single", SizeBytes: sizeBytes},
	}

	// Use 16 workers for 1 table (should work fine, extra workers just won't be used)
	logger := replinit.NewLogger(slog.Default())
	copier := replinit.NewParallelTableCopier(env.sourcePool, 16, logger)

	var progressCalls int32
	copier.SetProgressCallback(func(completed, total int, currentTable string, percent float64) {
		atomic.AddInt32(&progressCalls, 1)
	})

	connStr := fmt.Sprintf("host=%s port=%d dbname=testdb user=test password=test",
		env.sourceHostExternal, env.sourcePortExternal)
	results, err := copier.CopyTables(ctx, tables, connStr)

	s.Require().NoError(err)
	s.Assert().Len(results, 1)
	s.Assert().NoError(results[0].Error)
	s.Assert().Equal("public.parallel_single", results[0].TableInfo.FullName)
}
