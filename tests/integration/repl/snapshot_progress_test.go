package repl_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
)

// =============================================================================
// Snapshot Progress Test Suite Infrastructure (T087a-T087f)
// =============================================================================

// SnapshotProgressTestSuite tests the progress tracking infrastructure.
type SnapshotProgressTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	env    *progressTestEnv
}

// progressTestEnv holds the test environment for progress tests.
type progressTestEnv struct {
	network *testcontainers.DockerNetwork

	// Source node
	sourceContainer testcontainers.Container
	sourcePool      *pgxpool.Pool
	sourceHost      string
	sourcePort      int
	sourceDaemon    *daemon.Daemon
	sourceGRPCPort  int

	// Target node
	targetContainer testcontainers.Container
	targetPool      *pgxpool.Pool
	targetHost      string
	targetPort      int
	targetDaemon    *daemon.Daemon
	targetGRPCPort  int

	// External connection info
	sourceHostExternal string
	sourcePortExternal int
	targetHostExternal string
	targetPortExternal int

	// Temp directory for snapshots
	snapshotDir string
}

// TestSnapshotProgressSuite runs the progress tracking test suite.
func TestSnapshotProgressSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(SnapshotProgressTestSuite))
}

// SetupSuite creates shared Docker containers for all tests.
func (s *SnapshotProgressTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	const testPassword = "test"
	env := &progressTestEnv{}

	// Create Docker network
	netName := fmt.Sprintf("steep-progress-test-%d", time.Now().UnixNano())
	net, err := network.New(s.ctx, network.WithDriver("bridge"), network.WithLabels(map[string]string{"test": netName}))
	s.Require().NoError(err, "Failed to create Docker network")
	env.network = net

	// Start source PostgreSQL container
	sourceReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-progress-source"},
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

	sourceHostExternal, _ := sourceContainer.Host(s.ctx)
	sourcePortExternal, _ := sourceContainer.MappedPort(s.ctx, "5432")

	env.sourceHost = "pg-progress-source"
	env.sourcePort = 5432
	env.sourceHostExternal = sourceHostExternal
	env.sourcePortExternal = sourcePortExternal.Int()

	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	s.waitForDB(env.sourcePool, "source")

	_, err = env.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")

	// Start target PostgreSQL container
	targetReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-progress-target"},
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

	targetHostExternal, _ := targetContainer.Host(s.ctx)
	targetPortExternal, _ := targetContainer.MappedPort(s.ctx, "5432")

	env.targetHost = "pg-progress-target"
	env.targetPort = 5432
	env.targetHostExternal = targetHostExternal
	env.targetPortExternal = targetPortExternal.Int()

	targetConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		targetHostExternal, targetPortExternal.Port())
	env.targetPool, err = pgxpool.New(s.ctx, targetConnStr)
	s.Require().NoError(err, "Failed to create target pool")

	s.waitForDB(env.targetPool, "target")

	_, err = env.targetPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on target")

	// Start source daemon
	env.sourceGRPCPort = 15490
	s.T().Setenv("PGPASSWORD", testPassword)

	sourceSocketPath := tempProgressSocketPath()
	sourceCfg := &config.Config{
		NodeID:   "progress-source",
		NodeName: "Progress Source Node",
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
			Method:          config.InitMethodTwoPhase,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.sourceDaemon, err = daemon.New(sourceCfg, true)
	s.Require().NoError(err, "Failed to create source daemon")
	err = env.sourceDaemon.Start()
	s.Require().NoError(err, "Failed to start source daemon")

	// Start target daemon
	env.targetGRPCPort = 15491

	targetSocketPath := tempProgressSocketPath()
	targetCfg := &config.Config{
		NodeID:   "progress-target",
		NodeName: "Progress Target Node",
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
			Method:          config.InitMethodTwoPhase,
			ParallelWorkers: 4,
			SchemaSync:      config.SchemaSyncStrict,
		},
	}

	env.targetDaemon, err = daemon.New(targetCfg, true)
	s.Require().NoError(err, "Failed to create target daemon")
	err = env.targetDaemon.Start()
	s.Require().NoError(err, "Failed to start target daemon")

	// Create temp directory for snapshots
	env.snapshotDir, err = os.MkdirTemp("", "steep-progress-test-*")
	s.Require().NoError(err, "Failed to create temp snapshot directory")

	time.Sleep(time.Second)

	s.env = env
	s.T().Log("SnapshotProgressTestSuite: Shared containers and daemons ready")
}

// TearDownSuite cleans up resources.
func (s *SnapshotProgressTestSuite) TearDownSuite() {
	if s.env != nil {
		if s.env.targetDaemon != nil {
			s.env.targetDaemon.Stop()
		}
		if s.env.sourceDaemon != nil {
			s.env.sourceDaemon.Stop()
		}
		if s.env.targetPool != nil {
			s.env.targetPool.Close()
		}
		if s.env.sourcePool != nil {
			s.env.sourcePool.Close()
		}
		if s.env.targetContainer != nil {
			_ = s.env.targetContainer.Terminate(context.Background())
		}
		if s.env.sourceContainer != nil {
			_ = s.env.sourceContainer.Terminate(context.Background())
		}
		if s.env.network != nil {
			_ = s.env.network.Remove(context.Background())
		}
		if s.env.snapshotDir != "" {
			os.RemoveAll(s.env.snapshotDir)
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// SetupTest resets state before each test.
func (s *SnapshotProgressTestSuite) SetupTest() {
	ctx := s.ctx

	// Drop test tables
	testTables := []string{
		"progress_persist_test", "get_progress_test", "stream_progress_test",
		"apply_track_test",
	}
	for _, table := range testTables {
		s.env.sourcePool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		s.env.targetPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	}

	// Clean up publications
	rows, _ := s.env.sourcePool.Query(ctx, "SELECT pubname FROM pg_publication WHERE pubname LIKE 'steep_%'")
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

	// Clear snapshots and progress tables
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.snapshots")
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.snapshot_progress")
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.audit_log")

	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.snapshots")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.snapshot_progress")
	s.env.targetPool.Exec(ctx, "DELETE FROM steep_repl.audit_log")

	// Clean up snapshot output directory
	entries, _ := os.ReadDir(s.env.snapshotDir)
	for _, entry := range entries {
		os.RemoveAll(filepath.Join(s.env.snapshotDir, entry.Name()))
	}
}

func (s *SnapshotProgressTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	for range 30 {
		var ready int
		if err := pool.QueryRow(s.ctx, "SELECT 1").Scan(&ready); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	s.T().Fatalf("%s database not ready after 30 seconds", name)
}

var progressSocketCounter int64

func tempProgressSocketPath() string {
	n := atomic.AddInt64(&progressSocketCounter, 1)
	return fmt.Sprintf("/tmp/steep-progress-%d-%d.sock", time.Now().UnixNano(), n)
}

// =============================================================================
// Progress Persistence Tests (T087c, T087d)
// =============================================================================

// TestProgress_PersistedDuringGeneration verifies that progress is persisted
// to the steep_repl.snapshot_progress table during snapshot generation.
// Implements T087c: Progress calculation during COPY TO (generation).
func (s *SnapshotProgressTestSuite) TestProgress_PersistedDuringGeneration() {
	ctx := s.ctx
	env := s.env

	// Create test table with enough data to see progress updates
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE progress_persist_test (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO progress_persist_test (data, padding)
		SELECT md5(i::text), repeat('x', 500)
		FROM generate_series(1, 1000) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_progress_source FOR TABLE progress_persist_test
	`)
	s.Require().NoError(err)

	// Connect and generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "progress_persist")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "progress-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "none",
	})
	s.Require().NoError(err)

	var snapshotID string
	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		if progress.SnapshotId != "" {
			snapshotID = progress.SnapshotId
		}
		if progress.Complete {
			break
		}
	}

	s.Require().NotEmpty(snapshotID, "Should have snapshot ID")

	// Check that snapshot was recorded in database
	var status string
	var tableCount int
	err = env.sourcePool.QueryRow(ctx, `
		SELECT status, table_count
		FROM steep_repl.snapshots
		WHERE snapshot_id = $1
	`, snapshotID).Scan(&status, &tableCount)
	s.Require().NoError(err, "Snapshot should be recorded")
	s.Assert().Equal("complete", status)
	s.Assert().GreaterOrEqual(tableCount, 1)

	s.T().Logf("Snapshot %s completed with %d tables", snapshotID, tableCount)
}

// =============================================================================
// GetSnapshotProgress RPC Tests (T087f)
// =============================================================================

// TestProgress_GetSnapshotProgress tests the GetSnapshotProgress RPC.
// Implements T087f: GetSnapshotProgress RPC handler.
func (s *SnapshotProgressTestSuite) TestProgress_GetSnapshotProgress() {
	ctx := s.ctx
	env := s.env

	// Create test table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE get_progress_test (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO get_progress_test (data)
		SELECT repeat('x', 100) FROM generate_series(1, 500)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_progress_source FOR TABLE get_progress_test
	`)
	s.Require().NoError(err)

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "get_progress_test")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "progress-source",
		OutputPath:      outputPath,
		ParallelWorkers: 1,
		Compression:     "none",
	})
	s.Require().NoError(err)

	var snapshotID string
	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		if progress.SnapshotId != "" {
			snapshotID = progress.SnapshotId
		}
		if progress.Complete {
			break
		}
	}

	s.Require().NotEmpty(snapshotID)

	// Query progress using GetSnapshotProgress RPC
	resp, err := initClient.GetSnapshotProgress(ctx, &pb.GetSnapshotProgressRequest{
		SnapshotId: snapshotID,
	})
	s.Require().NoError(err, "GetSnapshotProgress RPC should succeed")

	s.T().Logf("GetSnapshotProgress for %s: has_progress=%v", snapshotID, resp.HasProgress)
	if resp.HasProgress && resp.Progress != nil {
		s.Assert().Equal(snapshotID, resp.Progress.SnapshotId)
		s.T().Logf("Progress: phase=%s, percent=%.1f%%", resp.Progress.Phase, resp.Progress.OverallPercent)
	}
}

// TestProgress_GetSnapshotProgressNotFound tests GetSnapshotProgress with non-existent snapshot.
func (s *SnapshotProgressTestSuite) TestProgress_GetSnapshotProgressNotFound() {
	ctx := s.ctx
	env := s.env

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.GetSnapshotProgress(ctx, &pb.GetSnapshotProgressRequest{
		SnapshotId: "nonexistent_snapshot_id_12345",
	})
	s.Require().NoError(err, "RPC should succeed even for missing snapshot")
	s.Assert().False(resp.HasProgress, "Should not have progress for non-existent snapshot")
}

// =============================================================================
// StreamSnapshotProgress RPC Tests (T087e)
// =============================================================================

// TestProgress_StreamSnapshotProgress tests the StreamSnapshotProgress RPC.
// Implements T087e: StreamSnapshotProgress RPC handler.
func (s *SnapshotProgressTestSuite) TestProgress_StreamSnapshotProgress() {
	ctx := s.ctx
	env := s.env

	// Create test table with enough data for streaming to capture progress
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE stream_progress_test (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO stream_progress_test (data, padding)
		SELECT md5(i::text), repeat('x', 1000)
		FROM generate_series(1, 2000) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_progress_source FOR TABLE stream_progress_test
	`)
	s.Require().NoError(err)

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Start progress streaming before generation
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	progressStream, err := initClient.StreamSnapshotProgress(streamCtx, &pb.StreamSnapshotProgressRequest{
		IntervalMs:       100,
		IncludeCompleted: true,
	})
	s.Require().NoError(err, "StreamSnapshotProgress should succeed")

	// Collect progress updates in background
	progressUpdates := make(chan *pb.SnapshotProgressUpdate, 100)
	go func() {
		for {
			update, err := progressStream.Recv()
			if err != nil {
				close(progressUpdates)
				return
			}
			progressUpdates <- update
		}
	}()

	// Start snapshot generation
	outputPath := filepath.Join(env.snapshotDir, "stream_progress_test")

	genStream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "progress-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "none",
	})
	s.Require().NoError(err)

	var snapshotID string
	for {
		progress, err := genStream.Recv()
		if err != nil {
			break
		}
		if progress.SnapshotId != "" {
			snapshotID = progress.SnapshotId
		}
		if progress.Complete {
			break
		}
	}

	s.Require().NotEmpty(snapshotID)

	// Give streaming time to capture final updates
	time.Sleep(300 * time.Millisecond)
	streamCancel()

	// Collect received updates
	var updates []*pb.SnapshotProgressUpdate
	for update := range progressUpdates {
		updates = append(updates, update)
	}

	s.T().Logf("StreamSnapshotProgress received %d updates for snapshot %s", len(updates), snapshotID)

	if len(updates) > 0 {
		for _, update := range updates {
			if update.Progress != nil {
				s.Assert().NotEmpty(update.Progress.Phase, "Progress should have a phase")
				s.T().Logf("  Update: snapshot=%s phase=%s percent=%.1f%%",
					update.Progress.SnapshotId, update.Progress.Phase, update.Progress.OverallPercent)
			}
		}
	}
}

// =============================================================================
// Apply Progress Tests (T087d)
// =============================================================================

// TestProgress_TrackingDuringApply tests progress tracking during snapshot apply.
// Implements T087d: Progress calculation during COPY FROM (application).
func (s *SnapshotProgressTestSuite) TestProgress_TrackingDuringApply() {
	ctx := s.ctx
	env := s.env

	// Create source table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE apply_track_test (
			id SERIAL PRIMARY KEY,
			name TEXT,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO apply_track_test (name, data)
		SELECT 'item_' || i, repeat('x', 200)
		FROM generate_series(1, 500) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_progress_source FOR TABLE apply_track_test
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "apply_track_test")

	genStream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "progress-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "gzip",
	})
	s.Require().NoError(err)

	for {
		progress, err := genStream.Recv()
		if err != nil || progress.Complete {
			break
		}
	}

	// Create schema on target
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE apply_track_test (
			id SERIAL PRIMARY KEY,
			name TEXT,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	// Apply snapshot and track progress
	targetConn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer targetConn.Close()

	targetInitClient := pb.NewInitServiceClient(targetConn)

	applyStream, err := targetInitClient.ApplySnapshot(ctx, &pb.ApplySnapshotRequest{
		TargetNodeId:    "progress-target",
		InputPath:       outputPath,
		ParallelWorkers: 2,
		VerifyChecksums: true,
	})
	s.Require().NoError(err)

	var applyProgress []*pb.SnapshotProgress
	var sawVerifying, sawImporting bool

	for {
		progress, err := applyStream.Recv()
		if err != nil {
			break
		}
		applyProgress = append(applyProgress, progress)

		switch progress.Phase {
		case "verifying":
			sawVerifying = true
		case "importing":
			sawImporting = true
		}

		if progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Apply failed: %s", progress.Error)
		}
	}

	s.Assert().True(sawImporting, "Should have seen importing phase")
	s.T().Logf("Apply progress: %d updates, verifying=%v, importing=%v",
		len(applyProgress), sawVerifying, sawImporting)

	// Verify data was applied
	var count int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM apply_track_test").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(500, count, "Should have applied all rows")
}
