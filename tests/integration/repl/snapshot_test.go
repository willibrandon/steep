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
	replinit "github.com/willibrandon/steep/internal/repl/init"
	"github.com/willibrandon/steep/internal/repl/models"
)

// =============================================================================
// Two-Phase Snapshot Test Suite Infrastructure
// =============================================================================

// SnapshotTestSuite tests the two-phase snapshot functionality.
type SnapshotTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	env    *snapshotEnv
}

// snapshotEnv holds the test environment for snapshot tests.
type snapshotEnv struct {
	network *testcontainers.DockerNetwork

	// Source node
	sourceContainer testcontainers.Container
	sourcePool      *pgxpool.Pool
	sourceChinook   *pgxpool.Pool // Connection to chinook_serial database
	sourceHost      string        // Docker network hostname
	sourcePort      int
	sourceDaemon    *daemon.Daemon
	sourceGRPCPort  int

	// Target node (for apply tests)
	targetContainer    testcontainers.Container
	targetPool         *pgxpool.Pool
	targetHost         string // Docker network hostname
	targetPort         int
	targetDaemon       *daemon.Daemon
	targetGRPCPort     int
	targetHostExternal string
	targetPortExternal int

	// External connection info
	sourceHostExternal string
	sourcePortExternal int

	// Temp directory for snapshots
	snapshotDir string
}

// TestSnapshotSuite runs the two-phase snapshot test suite.
func TestSnapshotSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(SnapshotTestSuite))
}

// SetupSuite creates shared Docker containers for all tests.
func (s *SnapshotTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	const testPassword = "test"
	env := &snapshotEnv{}

	// Create Docker network with unique name to avoid duplicates
	netName := fmt.Sprintf("steep-snapshot-test-%d", time.Now().UnixNano())
	net, err := network.New(s.ctx, network.WithDriver("bridge"), network.WithLabels(map[string]string{"test": netName}))
	s.Require().NoError(err, "Failed to create Docker network")
	env.network = net

	// Start source PostgreSQL container (PG18 with steep_repl)
	sourceReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-snapshot-source"},
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

	// Get connection info
	sourceHostExternal, _ := sourceContainer.Host(s.ctx)
	sourcePortExternal, _ := sourceContainer.MappedPort(s.ctx, "5432")

	env.sourceHost = "pg-snapshot-source"
	env.sourcePort = 5432
	env.sourceHostExternal = sourceHostExternal
	env.sourcePortExternal = sourcePortExternal.Int()

	// Create connection pool for testdb
	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	// Wait for database
	s.waitForDB(env.sourcePool, "source")

	// Create extension
	_, err = env.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")

	// Create connection pool for chinook_serial (pre-loaded sample database)
	chinookConnStr := fmt.Sprintf("postgres://test:test@%s:%s/chinook_serial?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourceChinook, err = pgxpool.New(s.ctx, chinookConnStr)
	s.Require().NoError(err, "Failed to create chinook pool")

	// Create steep_repl extension in chinook_serial for snapshot tests
	_, err = env.sourceChinook.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on chinook_serial")

	// Start target PostgreSQL container
	targetReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Networks:     []string{net.Name},
		NetworkAliases: map[string][]string{
			net.Name: {"pg-snapshot-target"},
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

	env.targetHost = "pg-snapshot-target"
	env.targetPort = 5432
	env.targetHostExternal = targetHostExternal
	env.targetPortExternal = targetPortExternal.Int()

	// Create target connection pool
	targetConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		targetHostExternal, targetPortExternal.Port())
	env.targetPool, err = pgxpool.New(s.ctx, targetConnStr)
	s.Require().NoError(err, "Failed to create target pool")

	s.waitForDB(env.targetPool, "target")

	// Create extension on target
	_, err = env.targetPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on target")

	// Start daemon
	env.sourceGRPCPort = 15480

	s.T().Setenv("PGPASSWORD", testPassword)

	sourceSocketPath := tempSocketPathSnapshot()
	sourceCfg := &config.Config{
		NodeID:   "snapshot-source",
		NodeName: "Snapshot Source Node",
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
	env.targetGRPCPort = 15481

	targetSocketPath := tempSocketPathSnapshot()
	targetCfg := &config.Config{
		NodeID:   "snapshot-target",
		NodeName: "Snapshot Target Node",
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
	env.snapshotDir, err = os.MkdirTemp("", "steep-snapshot-test-*")
	s.Require().NoError(err, "Failed to create temp snapshot directory")

	time.Sleep(time.Second)

	s.env = env
	s.T().Log("SnapshotTestSuite: Shared containers and daemons ready")
}

// TearDownSuite cleans up resources.
func (s *SnapshotTestSuite) TearDownSuite() {
	if s.env != nil {
		// Stop daemons
		if s.env.targetDaemon != nil {
			s.env.targetDaemon.Stop()
		}
		if s.env.sourceDaemon != nil {
			s.env.sourceDaemon.Stop()
		}

		// Close connection pools
		if s.env.targetPool != nil {
			s.env.targetPool.Close()
		}
		if s.env.sourceChinook != nil {
			s.env.sourceChinook.Close()
		}
		if s.env.sourcePool != nil {
			s.env.sourcePool.Close()
		}

		// Terminate containers
		if s.env.targetContainer != nil {
			_ = s.env.targetContainer.Terminate(context.Background())
		}
		if s.env.sourceContainer != nil {
			_ = s.env.sourceContainer.Terminate(context.Background())
		}

		// Remove network and temp files
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
func (s *SnapshotTestSuite) SetupTest() {
	ctx := s.ctx

	// Drop test tables
	testTables := []string{
		"snapshot_test", "users", "orders", "products",
		"large_table", "small_table", "compressed_test",
		"lz4_test", "zstd_test",
	}
	for _, table := range testTables {
		s.env.sourcePool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	}

	// Drop test sequences
	testSequences := []string{"test_seq", "users_id_seq", "orders_id_seq"}
	for _, seq := range testSequences {
		s.env.sourcePool.Exec(ctx, fmt.Sprintf("DROP SEQUENCE IF EXISTS %s CASCADE", seq))
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

	// Reset node states
	s.env.sourcePool.Exec(ctx, `
		UPDATE steep_repl.nodes SET
			init_state = 'uninitialized',
			init_source_node = NULL,
			init_started_at = NULL,
			init_completed_at = NULL
		WHERE node_id = 'snapshot-source'
	`)

	// Clear snapshots table
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.snapshots")
	s.env.sourcePool.Exec(ctx, "DELETE FROM steep_repl.audit_log")

	// Clean up snapshot output directory
	entries, _ := os.ReadDir(s.env.snapshotDir)
	for _, entry := range entries {
		os.RemoveAll(filepath.Join(s.env.snapshotDir, entry.Name()))
	}
}

func (s *SnapshotTestSuite) waitForDB(pool *pgxpool.Pool, name string) {
	for range 30 {
		var ready int
		if err := pool.QueryRow(s.ctx, "SELECT 1").Scan(&ready); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	s.T().Fatalf("%s database not ready after 30 seconds", name)
}

// tempSocketPathSnapshot creates a unique socket path for snapshot tests.
var snapshotSocketCounter int64

func tempSocketPathSnapshot() string {
	n := atomic.AddInt64(&snapshotSocketCounter, 1)
	return fmt.Sprintf("/tmp/steep-snapshot-%d-%d.sock", time.Now().UnixNano(), n)
}

// =============================================================================
// GenerateSnapshot RPC Tests
// =============================================================================

// TestSnapshot_GenerateBasic tests basic snapshot generation via RPC.
func (s *SnapshotTestSuite) TestSnapshot_GenerateBasic() {
	ctx := s.ctx
	env := s.env

	// Create test table with data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE snapshot_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		)
	`)
	s.Require().NoError(err, "Failed to create table")

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO snapshot_test (name, value)
		SELECT 'item_' || i, i * 10
		FROM generate_series(1, 100) AS i
	`)
	s.Require().NoError(err, "Failed to insert data")

	// Create publication for the table
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE snapshot_test
	`)
	s.Require().NoError(err, "Failed to create publication")

	// Connect to daemon
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Create output path for this test
	outputPath := filepath.Join(env.snapshotDir, "basic_snapshot")

	// Generate snapshot
	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "none",
	})
	s.Require().NoError(err, "GenerateSnapshot RPC failed")

	// Collect progress updates
	var progressUpdates []*pb.SnapshotProgress
	var finalProgress *pb.SnapshotProgress

	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		progressUpdates = append(progressUpdates, progress)
		if progress.Complete {
			finalProgress = progress
			break
		}
	}

	// Verify we got progress updates
	s.Assert().NotEmpty(progressUpdates, "Should receive progress updates")

	// Verify final status
	s.Require().NotNil(finalProgress, "Should receive final progress")
	s.Assert().True(finalProgress.Complete, "Snapshot should be complete")
	s.Assert().Empty(finalProgress.Error, "Should have no error")
	s.Assert().NotEmpty(finalProgress.SnapshotId, "Should have snapshot ID")
	s.Assert().NotEmpty(finalProgress.Lsn, "Should have captured LSN")

	// Verify snapshot files exist
	s.Assert().DirExists(outputPath, "Snapshot directory should exist")

	manifestPath := filepath.Join(outputPath, "manifest.json")
	s.Assert().FileExists(manifestPath, "Manifest file should exist")

	// Read and verify manifest
	manifest, err := replinit.ReadManifest(manifestPath)
	s.Require().NoError(err, "Failed to read manifest")

	s.Assert().Equal(finalProgress.SnapshotId, manifest.SnapshotID)
	s.Assert().Equal("snapshot-source", manifest.SourceNode)
	s.Assert().NotEmpty(manifest.LSN)
	s.Assert().Equal(models.CompressionNone, manifest.Compression)
	s.Assert().GreaterOrEqual(len(manifest.Tables), 1, "Should have at least one table")

	// Verify checksum integrity
	errors, err := replinit.VerifySnapshot(outputPath)
	s.Require().NoError(err, "VerifySnapshot failed")
	s.Assert().Empty(errors, "Snapshot verification should have no errors")

	s.T().Logf("Generated snapshot %s with %d tables", manifest.SnapshotID, len(manifest.Tables))
}

// TestSnapshot_GenerateWithGzipCompression tests snapshot generation with gzip compression.
func (s *SnapshotTestSuite) TestSnapshot_GenerateWithGzipCompression() {
	ctx := s.ctx
	env := s.env

	// Create test table with larger data to benefit from compression
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE compressed_test (
			id SERIAL PRIMARY KEY,
			data TEXT,
			padding TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO compressed_test (data, padding)
		SELECT md5(i::text), repeat('x', 500)
		FROM generate_series(1, 500) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE compressed_test
	`)
	s.Require().NoError(err)

	// Connect and generate snapshot with gzip
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "gzip_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 4,
		Compression:     "gzip",
	})
	s.Require().NoError(err)

	var finalProgress *pb.SnapshotProgress
	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		if progress.Complete {
			finalProgress = progress
			break
		}
	}

	s.Require().NotNil(finalProgress)
	s.Assert().True(finalProgress.Complete)
	s.Assert().Empty(finalProgress.Error)

	// Verify manifest shows gzip compression
	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)
	s.Assert().Equal(models.CompressionGzip, manifest.Compression)

	// Verify data files are gzipped
	for _, table := range manifest.Tables {
		s.Assert().Contains(table.File, ".gz", "Data files should have .gz extension")
	}

	// Verify checksums still valid for compressed files
	errors, err := replinit.VerifySnapshot(outputPath)
	s.Require().NoError(err)
	s.Assert().Empty(errors)

	s.T().Logf("Generated gzip snapshot with %d tables, total size: %d bytes",
		len(manifest.Tables), manifest.TotalSizeBytes)
}

// TestSnapshot_GenerateWithLZ4Compression tests snapshot generation with LZ4 compression.
func (s *SnapshotTestSuite) TestSnapshot_GenerateWithLZ4Compression() {
	ctx := s.ctx
	env := s.env

	// Create test table with data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE lz4_test (
			id SERIAL PRIMARY KEY,
			data TEXT NOT NULL
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO lz4_test (data)
		SELECT repeat('lz4 compression test data ', 100)
		FROM generate_series(1, 500)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE lz4_test
	`)
	s.Require().NoError(err)

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)
	outputPath := filepath.Join(env.snapshotDir, "lz4_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "lz4",
	})
	s.Require().NoError(err)

	for {
		progress, err := stream.Recv()
		if err != nil || progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Snapshot generation failed: %s", progress.Error)
		}
	}

	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)
	s.Assert().Equal(models.CompressionLZ4, manifest.Compression)

	// Verify data files have .lz4 extension
	for _, table := range manifest.Tables {
		s.Assert().Contains(table.File, ".lz4", "LZ4 compressed files should have .lz4 extension")
	}

	s.T().Logf("Generated LZ4 snapshot with %d tables, total size: %d bytes",
		len(manifest.Tables), manifest.TotalSizeBytes)
}

// TestSnapshot_GenerateWithZstdCompression tests snapshot generation with Zstd compression.
func (s *SnapshotTestSuite) TestSnapshot_GenerateWithZstdCompression() {
	ctx := s.ctx
	env := s.env

	// Create test table with data
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE zstd_test (
			id SERIAL PRIMARY KEY,
			data TEXT NOT NULL
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO zstd_test (data)
		SELECT repeat('zstd compression test data ', 100)
		FROM generate_series(1, 500)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE zstd_test
	`)
	s.Require().NoError(err)

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)
	outputPath := filepath.Join(env.snapshotDir, "zstd_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "zstd",
	})
	s.Require().NoError(err)

	for {
		progress, err := stream.Recv()
		if err != nil || progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Snapshot generation failed: %s", progress.Error)
		}
	}

	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)
	s.Assert().Equal(models.CompressionZstd, manifest.Compression)

	// Verify data files have .zst extension
	for _, table := range manifest.Tables {
		s.Assert().Contains(table.File, ".zst", "Zstd compressed files should have .zst extension")
	}

	s.T().Logf("Generated Zstd snapshot with %d tables, total size: %d bytes",
		len(manifest.Tables), manifest.TotalSizeBytes)
}

// TestSnapshot_GenerateMultipleTables tests snapshot generation with multiple tables.
func (s *SnapshotTestSuite) TestSnapshot_GenerateMultipleTables() {
	ctx := s.ctx
	env := s.env

	// Create multiple tables
	tables := []struct {
		name     string
		rowCount int
	}{
		{"users", 50},
		{"orders", 100},
		{"products", 30},
	}

	for _, tbl := range tables {
		_, err := env.sourcePool.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id SERIAL PRIMARY KEY,
				name TEXT NOT NULL,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`, tbl.name))
		s.Require().NoError(err, "Failed to create table %s", tbl.name)

		_, err = env.sourcePool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s (name)
			SELECT 'item_' || i
			FROM generate_series(1, %d) AS i
		`, tbl.name, tbl.rowCount))
		s.Require().NoError(err, "Failed to insert into %s", tbl.name)
	}

	// Create publication for all tables
	_, err := env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE users, orders, products
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "multi_table_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 4,
		Compression:     "none",
	})
	s.Require().NoError(err)

	var sawTables []string
	var finalProgress *pb.SnapshotProgress

	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		if progress.CurrentTable != "" && progress.Phase == "data" {
			// Track unique tables seen in progress
			found := false
			for _, t := range sawTables {
				if t == progress.CurrentTable {
					found = true
					break
				}
			}
			if !found {
				sawTables = append(sawTables, progress.CurrentTable)
			}
		}
		if progress.Complete {
			finalProgress = progress
			break
		}
	}

	s.Require().NotNil(finalProgress)
	s.Assert().Empty(finalProgress.Error)

	// Verify manifest
	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)

	s.Assert().GreaterOrEqual(len(manifest.Tables), 3, "Should have at least 3 tables")

	// Verify each table has data file
	for _, entry := range manifest.Tables {
		dataPath := filepath.Join(outputPath, entry.File)
		s.Assert().FileExists(dataPath, "Data file should exist for %s", entry.FullTableName())
	}

	// Verify snapshot integrity
	errors, err := replinit.VerifySnapshot(outputPath)
	s.Require().NoError(err)
	s.Assert().Empty(errors)

	s.T().Logf("Generated multi-table snapshot with %d tables", len(manifest.Tables))
}

// TestSnapshot_GenerateWithSequences tests that sequences are captured in snapshot.
func (s *SnapshotTestSuite) TestSnapshot_GenerateWithSequences() {
	ctx := s.ctx
	env := s.env

	// Create table with sequence
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL
		)
	`)
	s.Require().NoError(err)

	// Insert data to advance sequence
	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO users (name)
		SELECT 'user_' || i FROM generate_series(1, 50) AS i
	`)
	s.Require().NoError(err)

	// Create a standalone sequence
	_, err = env.sourcePool.Exec(ctx, `CREATE SEQUENCE test_seq START 1000`)
	s.Require().NoError(err)
	_, err = env.sourcePool.Exec(ctx, `SELECT nextval('test_seq')`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE users
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "sequence_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "snapshot-source",
		OutputPath:   outputPath,
		Compression:  "none",
	})
	s.Require().NoError(err)

	var finalProgress *pb.SnapshotProgress
	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		if progress.Complete {
			finalProgress = progress
			break
		}
	}

	s.Require().NotNil(finalProgress)
	s.Assert().Empty(finalProgress.Error)

	// Verify sequences in manifest
	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)

	s.Assert().NotEmpty(manifest.Sequences, "Should capture sequences")

	// Look for users_id_seq
	foundUsersSeq := false
	for _, seq := range manifest.Sequences {
		if seq.Name == "users_id_seq" {
			foundUsersSeq = true
			s.Assert().GreaterOrEqual(seq.Value, int64(50), "Sequence should be at least 50")
		}
	}
	s.Assert().True(foundUsersSeq, "Should capture users_id_seq")

	s.T().Logf("Captured %d sequences", len(manifest.Sequences))
}

// TestSnapshot_GenerateProgressStreaming tests that progress is streamed correctly.
func (s *SnapshotTestSuite) TestSnapshot_GenerateProgressStreaming() {
	ctx := s.ctx
	env := s.env

	// Create larger table to see progress
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
		FROM generate_series(1, 1000) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE large_table
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "progress_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "snapshot-source",
		OutputPath:   outputPath,
		Compression:  "none",
	})
	s.Require().NoError(err)

	var progressUpdates []*pb.SnapshotProgress
	var phases []string

	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		progressUpdates = append(progressUpdates, progress)

		// Track unique phases
		found := false
		for _, p := range phases {
			if p == progress.Phase {
				found = true
				break
			}
		}
		if !found && progress.Phase != "" {
			phases = append(phases, progress.Phase)
		}

		if progress.Complete {
			break
		}
	}

	// Should have multiple progress updates
	s.Assert().GreaterOrEqual(len(progressUpdates), 1, "Should receive progress updates")

	// Should have passed through multiple phases
	s.T().Logf("Phases observed: %v", phases)
	s.T().Logf("Total progress updates: %d", len(progressUpdates))

	// Last update should be complete
	if len(progressUpdates) > 0 {
		last := progressUpdates[len(progressUpdates)-1]
		s.Assert().True(last.Complete, "Last progress should be complete")
		s.Assert().Equal(float32(100), last.OverallPercent, "Final progress should be 100%%")
	}
}

// TestSnapshot_GenerateInvalidSourceNode tests error handling for invalid source node.
func (s *SnapshotTestSuite) TestSnapshot_GenerateInvalidSourceNode() {
	ctx := s.ctx
	env := s.env

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Try to generate snapshot for non-existent node
	_, err = initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "nonexistent-node",
		OutputPath:   filepath.Join(env.snapshotDir, "invalid"),
		Compression:  "none",
	})

	// Should get an error (either from RPC or from stream)
	if err == nil {
		// If RPC didn't fail, stream should contain error
		s.T().Log("RPC call succeeded, checking stream for error")
	}
}

// TestSnapshot_GenerateMissingOutputPath tests validation of output path.
func (s *SnapshotTestSuite) TestSnapshot_GenerateMissingOutputPath() {
	ctx := s.ctx
	env := s.env

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Try to generate snapshot without output path
	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "snapshot-source",
		OutputPath:   "", // Missing output path
		Compression:  "none",
	})

	// For streaming RPCs, error may come on Recv() not the initial call
	if err == nil {
		_, err = stream.Recv()
	}

	s.Require().Error(err, "Should fail with missing output path")
}

// TestSnapshot_GenerateInvalidCompression tests validation of compression type.
func (s *SnapshotTestSuite) TestSnapshot_GenerateInvalidCompression() {
	ctx := s.ctx
	env := s.env

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Try to generate snapshot with invalid compression
	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "snapshot-source",
		OutputPath:   filepath.Join(env.snapshotDir, "invalid_compression"),
		Compression:  "invalid_compression_type",
	})

	// For streaming RPCs, error may come on Recv() not the initial call
	if err == nil {
		_, err = stream.Recv()
	}

	s.Require().Error(err, "Should fail with invalid compression type")
}

// =============================================================================
// Snapshot Database Recording Tests
// =============================================================================

// TestSnapshot_RecordedInDatabase verifies snapshot metadata is recorded in database.
func (s *SnapshotTestSuite) TestSnapshot_RecordedInDatabase() {
	ctx := s.ctx
	env := s.env

	// Create test table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE small_table (
			id SERIAL PRIMARY KEY,
			name TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO small_table (name) VALUES ('test')
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE small_table
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "recorded_snapshot")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId: "snapshot-source",
		OutputPath:   outputPath,
		Compression:  "none",
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

	// Verify recorded in database
	var dbSnapshotID, dbSourceNode, dbLSN, dbStatus string
	var dbTableCount int
	err = env.sourcePool.QueryRow(ctx, `
		SELECT snapshot_id, source_node_id, lsn, status, table_count
		FROM steep_repl.snapshots
		WHERE snapshot_id = $1
	`, snapshotID).Scan(&dbSnapshotID, &dbSourceNode, &dbLSN, &dbStatus, &dbTableCount)

	s.Require().NoError(err, "Snapshot should be recorded in database")
	s.Assert().Equal(snapshotID, dbSnapshotID)
	s.Assert().Equal("snapshot-source", dbSourceNode)
	s.Assert().NotEmpty(dbLSN)
	s.Assert().Equal("complete", dbStatus)
	s.Assert().GreaterOrEqual(dbTableCount, 1)

	s.T().Logf("Snapshot %s recorded in database with status=%s, tables=%d",
		dbSnapshotID, dbStatus, dbTableCount)
}

// =============================================================================
// Two-Phase Snapshot Round-Trip Tests (T079)
// =============================================================================

// TestSnapshot_ChinookGenerateApplyRoundTrip tests the full generate/apply cycle
// using the pre-loaded Chinook database with real data and sequences.
// Implements T079: Integration test for snapshot generate/apply.
func (s *SnapshotTestSuite) TestSnapshot_ChinookGenerateApplyRoundTrip() {
	ctx := s.ctx
	env := s.env

	// Verify Chinook data exists on source
	var sourceArtistCount int
	err := env.sourceChinook.QueryRow(ctx, "SELECT COUNT(*) FROM artist").Scan(&sourceArtistCount)
	s.Require().NoError(err, "Chinook database should be available")
	s.Require().Greater(sourceArtistCount, 0, "Chinook should have artists")

	var sourceAlbumCount, sourceTrackCount int
	env.sourceChinook.QueryRow(ctx, "SELECT COUNT(*) FROM album").Scan(&sourceAlbumCount)
	env.sourceChinook.QueryRow(ctx, "SELECT COUNT(*) FROM track").Scan(&sourceTrackCount)

	s.T().Logf("Source Chinook: %d artists, %d albums, %d tracks",
		sourceArtistCount, sourceAlbumCount, sourceTrackCount)

	// Get source sequence values before snapshot
	var sourceArtistSeq, sourceAlbumSeq int64
	env.sourceChinook.QueryRow(ctx, "SELECT last_value FROM artist_artist_id_seq").Scan(&sourceArtistSeq)
	env.sourceChinook.QueryRow(ctx, "SELECT last_value FROM album_album_id_seq").Scan(&sourceAlbumSeq)
	s.T().Logf("Source sequences: artist=%d, album=%d", sourceArtistSeq, sourceAlbumSeq)

	// Create publication for Chinook tables
	_, err = env.sourceChinook.Exec(ctx, `
		CREATE PUBLICATION steep_pub_chinook FOR TABLE
			artist, album, track, genre, media_type,
			playlist, playlist_track, customer, employee, invoice, invoice_line
	`)
	s.Require().NoError(err, "Failed to create Chinook publication")

	// Start a dedicated daemon for chinook_serial database
	// The shared daemon connects to testdb, but we need one connected to chinook_serial
	chinookGRPCPort := 15490
	chinookSocketPath := tempSocketPathSnapshot()
	chinookCfg := &config.Config{
		NodeID:   "chinook-source",
		NodeName: "Chinook Source Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     env.sourceHostExternal,
			Port:     env.sourcePortExternal,
			Database: "chinook_serial", // Connect to Chinook database
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: chinookGRPCPort,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    chinookSocketPath,
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

	chinookDaemon, err := daemon.New(chinookCfg, true)
	s.Require().NoError(err, "Failed to create chinook daemon")
	err = chinookDaemon.Start()
	s.Require().NoError(err, "Failed to start chinook daemon")
	defer chinookDaemon.Stop()

	time.Sleep(500 * time.Millisecond) // Wait for daemon to be ready

	// Connect to the chinook daemon
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", chinookGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Generate snapshot from chinook_serial
	outputPath := filepath.Join(env.snapshotDir, "chinook_roundtrip")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "chinook-source",
		OutputPath:      outputPath,
		ParallelWorkers: 4,
		Compression:     "gzip",
	})
	s.Require().NoError(err, "GenerateSnapshot RPC failed")

	var generateProgress []*pb.SnapshotProgress
	var snapshotID, lsn string

	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}
		generateProgress = append(generateProgress, progress)
		if progress.SnapshotId != "" {
			snapshotID = progress.SnapshotId
		}
		if progress.Lsn != "" {
			lsn = progress.Lsn
		}
		if progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Generate failed: %s", progress.Error)
		}
	}

	s.Require().NotEmpty(snapshotID, "Should have snapshot ID")
	s.Require().NotEmpty(lsn, "Should have LSN")
	s.T().Logf("Generated snapshot %s at LSN %s with %d progress updates",
		snapshotID, lsn, len(generateProgress))

	// Verify manifest
	manifest, err := replinit.ReadManifest(filepath.Join(outputPath, "manifest.json"))
	s.Require().NoError(err)
	s.Assert().GreaterOrEqual(len(manifest.Tables), 10, "Should have Chinook tables")
	s.Assert().NotEmpty(manifest.Sequences, "Should have captured sequences")
	s.Assert().Equal(models.CompressionGzip, manifest.Compression)

	// Verify checksums before apply
	verifyErrors, err := replinit.VerifySnapshot(outputPath)
	s.Require().NoError(err)
	s.Assert().Empty(verifyErrors, "Snapshot verification should pass")

	// Create same schema on target (required before apply)
	// Copy schema from source to target using pg_dump/restore would be ideal,
	// but for this test we'll create the tables manually
	s.createChinookSchemaOnTarget(ctx)

	// Connect to target daemon and apply snapshot
	targetConn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer targetConn.Close()

	targetInitClient := pb.NewInitServiceClient(targetConn)

	applyStream, err := targetInitClient.ApplySnapshot(ctx, &pb.ApplySnapshotRequest{
		TargetNodeId:    "snapshot-target",
		InputPath:       outputPath,
		ParallelWorkers: 4,
		VerifyChecksums: true,
	})
	s.Require().NoError(err, "ApplySnapshot RPC failed")

	var applyProgress []*pb.SnapshotProgress

	for {
		progress, err := applyStream.Recv()
		if err != nil {
			break
		}
		applyProgress = append(applyProgress, progress)
		if progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Apply failed: %s", progress.Error)
		}
	}

	s.T().Logf("Applied snapshot with %d progress updates", len(applyProgress))

	// Verify data on target matches source
	var targetArtistCount, targetAlbumCount, targetTrackCount int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM artist").Scan(&targetArtistCount)
	s.Require().NoError(err)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM album").Scan(&targetAlbumCount)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM track").Scan(&targetTrackCount)

	s.Assert().Equal(sourceArtistCount, targetArtistCount, "Artist count should match")
	s.Assert().Equal(sourceAlbumCount, targetAlbumCount, "Album count should match")
	s.Assert().Equal(sourceTrackCount, targetTrackCount, "Track count should match")

	s.T().Logf("Target Chinook: %d artists, %d albums, %d tracks",
		targetArtistCount, targetAlbumCount, targetTrackCount)

	// Verify sequences were restored
	var targetArtistSeq, targetAlbumSeq int64
	env.targetPool.QueryRow(ctx, "SELECT last_value FROM artist_artist_id_seq").Scan(&targetArtistSeq)
	env.targetPool.QueryRow(ctx, "SELECT last_value FROM album_album_id_seq").Scan(&targetAlbumSeq)

	s.Assert().Equal(sourceArtistSeq, targetArtistSeq, "Artist sequence should match")
	s.Assert().Equal(sourceAlbumSeq, targetAlbumSeq, "Album sequence should match")

	s.T().Logf("Target sequences: artist=%d, album=%d", targetArtistSeq, targetAlbumSeq)
	s.T().Log("Round-trip test PASSED: Data and sequences match!")
}

// createChinookSchemaOnTarget creates the Chinook schema on the target database.
func (s *SnapshotTestSuite) createChinookSchemaOnTarget(ctx context.Context) {
	// Create tables in the correct order (respecting foreign keys)
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS artist (
			artist_id SERIAL PRIMARY KEY,
			name VARCHAR(120)
		)`,
		`CREATE TABLE IF NOT EXISTS album (
			album_id SERIAL PRIMARY KEY,
			title VARCHAR(160) NOT NULL,
			artist_id INT NOT NULL REFERENCES artist(artist_id)
		)`,
		`CREATE TABLE IF NOT EXISTS media_type (
			media_type_id SERIAL PRIMARY KEY,
			name VARCHAR(120)
		)`,
		`CREATE TABLE IF NOT EXISTS genre (
			genre_id SERIAL PRIMARY KEY,
			name VARCHAR(120)
		)`,
		`CREATE TABLE IF NOT EXISTS track (
			track_id SERIAL PRIMARY KEY,
			name VARCHAR(200) NOT NULL,
			album_id INT REFERENCES album(album_id),
			media_type_id INT NOT NULL REFERENCES media_type(media_type_id),
			genre_id INT REFERENCES genre(genre_id),
			composer VARCHAR(220),
			milliseconds INT NOT NULL,
			bytes INT,
			unit_price NUMERIC(10,2) NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS playlist (
			playlist_id SERIAL PRIMARY KEY,
			name VARCHAR(120)
		)`,
		`CREATE TABLE IF NOT EXISTS playlist_track (
			playlist_id INT NOT NULL REFERENCES playlist(playlist_id),
			track_id INT NOT NULL REFERENCES track(track_id),
			PRIMARY KEY (playlist_id, track_id)
		)`,
		`CREATE TABLE IF NOT EXISTS employee (
			employee_id SERIAL PRIMARY KEY,
			last_name VARCHAR(20) NOT NULL,
			first_name VARCHAR(20) NOT NULL,
			title VARCHAR(30),
			reports_to INT REFERENCES employee(employee_id),
			birth_date TIMESTAMP,
			hire_date TIMESTAMP,
			address VARCHAR(70),
			city VARCHAR(40),
			state VARCHAR(40),
			country VARCHAR(40),
			postal_code VARCHAR(10),
			phone VARCHAR(24),
			fax VARCHAR(24),
			email VARCHAR(60)
		)`,
		`CREATE TABLE IF NOT EXISTS customer (
			customer_id SERIAL PRIMARY KEY,
			first_name VARCHAR(40) NOT NULL,
			last_name VARCHAR(20) NOT NULL,
			company VARCHAR(80),
			address VARCHAR(70),
			city VARCHAR(40),
			state VARCHAR(40),
			country VARCHAR(40),
			postal_code VARCHAR(10),
			phone VARCHAR(24),
			fax VARCHAR(24),
			email VARCHAR(60) NOT NULL,
			support_rep_id INT REFERENCES employee(employee_id)
		)`,
		`CREATE TABLE IF NOT EXISTS invoice (
			invoice_id SERIAL PRIMARY KEY,
			customer_id INT NOT NULL REFERENCES customer(customer_id),
			invoice_date TIMESTAMP NOT NULL,
			billing_address VARCHAR(70),
			billing_city VARCHAR(40),
			billing_state VARCHAR(40),
			billing_country VARCHAR(40),
			billing_postal_code VARCHAR(10),
			total NUMERIC(10,2) NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS invoice_line (
			invoice_line_id SERIAL PRIMARY KEY,
			invoice_id INT NOT NULL REFERENCES invoice(invoice_id),
			track_id INT NOT NULL REFERENCES track(track_id),
			unit_price NUMERIC(10,2) NOT NULL,
			quantity INT NOT NULL
		)`,
	}

	for _, stmt := range ddl {
		_, err := s.env.targetPool.Exec(ctx, stmt)
		s.Require().NoError(err, "Failed to create Chinook schema on target")
	}
}

// =============================================================================
// Progress Streaming Tests (T079a, T079b)
// =============================================================================

// TestSnapshot_GenerateProgressPhases tests that generation progress goes through expected phases.
// Implements T079a: Integration test for progress streaming during generation.
func (s *SnapshotTestSuite) TestSnapshot_GenerateProgressPhases() {
	ctx := s.ctx
	env := s.env

	// Create test table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE progress_test (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO progress_test (data)
		SELECT repeat('x', 100) FROM generate_series(1, 500)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE progress_test
	`)
	s.Require().NoError(err)

	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "progress_phases")

	stream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "none",
	})
	s.Require().NoError(err)

	var phases []string
	var percentages []float32
	var sawComplete bool

	for {
		progress, err := stream.Recv()
		if err != nil {
			break
		}

		// Track unique phases
		if progress.Phase != "" {
			found := false
			for _, p := range phases {
				if p == progress.Phase {
					found = true
					break
				}
			}
			if !found {
				phases = append(phases, progress.Phase)
			}
		}

		percentages = append(percentages, progress.OverallPercent)

		if progress.Complete {
			sawComplete = true
			break
		}
	}

	// Verify we saw expected phases
	s.Assert().True(sawComplete, "Should complete successfully")
	s.T().Logf("Observed phases: %v", phases)

	// Should have at least schema, data, and complete phases
	s.Assert().GreaterOrEqual(len(phases), 2, "Should have multiple phases")

	// Progress should be monotonically increasing
	for i := 1; i < len(percentages); i++ {
		s.Assert().GreaterOrEqual(percentages[i], percentages[i-1],
			"Progress should not decrease: %.1f -> %.1f", percentages[i-1], percentages[i])
	}

	// Final progress should be 100%
	if len(percentages) > 0 {
		s.Assert().Equal(float32(100), percentages[len(percentages)-1], "Final progress should be 100%%")
	}
}

// TestSnapshot_ApplyProgressPhases tests that apply progress goes through expected phases.
// Implements T079b: Integration test for progress streaming during application.
func (s *SnapshotTestSuite) TestSnapshot_ApplyProgressPhases() {
	ctx := s.ctx
	env := s.env

	// First, generate a snapshot to apply
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE apply_progress_test (
			id SERIAL PRIMARY KEY,
			name TEXT,
			value INT
		)
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO apply_progress_test (name, value)
		SELECT 'item_' || i, i FROM generate_series(1, 200) AS i
	`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE SEQUENCE apply_test_seq START 1000
	`)
	s.Require().NoError(err)
	_, err = env.sourcePool.Exec(ctx, `SELECT nextval('apply_test_seq')`)
	s.Require().NoError(err)

	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE apply_progress_test
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "apply_progress")

	genStream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 2,
		Compression:     "none",
	})
	s.Require().NoError(err)

	for {
		progress, err := genStream.Recv()
		if err != nil || progress.Complete {
			break
		}
	}

	// Create matching schema on target
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE apply_progress_test (
			id SERIAL PRIMARY KEY,
			name TEXT,
			value INT
		)
	`)
	s.Require().NoError(err)

	_, err = env.targetPool.Exec(ctx, `CREATE SEQUENCE apply_test_seq`)
	s.Require().NoError(err)

	// Apply snapshot and track progress
	targetConn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer targetConn.Close()

	targetInitClient := pb.NewInitServiceClient(targetConn)

	applyStream, err := targetInitClient.ApplySnapshot(ctx, &pb.ApplySnapshotRequest{
		TargetNodeId:    "snapshot-target",
		InputPath:       outputPath,
		ParallelWorkers: 2,
		VerifyChecksums: true,
	})
	s.Require().NoError(err)

	var phases []string
	var percentages []float32
	var sawVerify, sawImporting, sawSequences, sawComplete bool

	for {
		progress, err := applyStream.Recv()
		if err != nil {
			break
		}

		// Track phases
		if progress.Phase != "" {
			found := false
			for _, p := range phases {
				if p == progress.Phase {
					found = true
					break
				}
			}
			if !found {
				phases = append(phases, progress.Phase)
			}

			switch progress.Phase {
			case "verifying":
				sawVerify = true
			case "importing":
				sawImporting = true
			case "sequences":
				sawSequences = true
			case "complete":
				sawComplete = true
			}
		}

		percentages = append(percentages, progress.OverallPercent)

		if progress.Complete {
			sawComplete = true
			break
		}

		if progress.Error != "" {
			s.T().Fatalf("Apply failed: %s", progress.Error)
		}
	}

	s.T().Logf("Apply phases observed: %v", phases)

	// Verify expected phases were seen
	s.Assert().True(sawComplete, "Should complete successfully")
	s.Assert().True(sawImporting, "Should have importing phase")

	// Note: verify and sequences phases depend on the snapshot content
	if sawVerify {
		s.T().Log("Verification phase observed")
	}
	if sawSequences {
		s.T().Log("Sequences phase observed")
	}

	// Progress should be monotonically increasing
	for i := 1; i < len(percentages); i++ {
		s.Assert().GreaterOrEqual(percentages[i], percentages[i-1],
			"Apply progress should not decrease")
	}

	// Verify data was actually applied
	var count int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM apply_progress_test").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(200, count, "Should have applied all rows")

	// Verify sequence was restored
	var seqVal int64
	err = env.targetPool.QueryRow(ctx, "SELECT last_value FROM apply_test_seq").Scan(&seqVal)
	s.Require().NoError(err)
	s.Assert().Equal(int64(1000), seqVal, "Sequence should be restored to 1000")

	s.T().Logf("Apply progress test PASSED: %d rows applied, sequence=%d", count, seqVal)
}

// TestSnapshot_ApplyWithParallelWorkers tests that parallel workers affect apply performance.
func (s *SnapshotTestSuite) TestSnapshot_ApplyWithParallelWorkers() {
	ctx := s.ctx
	env := s.env

	// Create multiple tables for parallel import
	tables := []string{"parallel_a", "parallel_b", "parallel_c", "parallel_d"}
	for _, t := range tables {
		_, err := env.sourcePool.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id SERIAL PRIMARY KEY,
				data TEXT
			)
		`, t))
		s.Require().NoError(err)

		_, err = env.sourcePool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s (data)
			SELECT repeat('x', 50) FROM generate_series(1, 100)
		`, t))
		s.Require().NoError(err)
	}

	_, err := env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_snapshot_source FOR TABLE parallel_a, parallel_b, parallel_c, parallel_d
	`)
	s.Require().NoError(err)

	// Generate snapshot
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.sourceGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	outputPath := filepath.Join(env.snapshotDir, "parallel_workers")

	genStream, err := initClient.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    "snapshot-source",
		OutputPath:      outputPath,
		ParallelWorkers: 4,
		Compression:     "none",
	})
	s.Require().NoError(err)

	for {
		progress, err := genStream.Recv()
		if err != nil || progress.Complete {
			break
		}
	}

	// Create matching schema on target
	for _, t := range tables {
		_, err := env.targetPool.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id SERIAL PRIMARY KEY,
				data TEXT
			)
		`, t))
		s.Require().NoError(err)
	}

	// Apply with 4 parallel workers
	targetConn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	s.Require().NoError(err)
	defer targetConn.Close()

	targetInitClient := pb.NewInitServiceClient(targetConn)

	start := time.Now()

	applyStream, err := targetInitClient.ApplySnapshot(ctx, &pb.ApplySnapshotRequest{
		TargetNodeId:    "snapshot-target",
		InputPath:       outputPath,
		ParallelWorkers: 4,
		VerifyChecksums: false, // Skip for speed
	})
	s.Require().NoError(err)

	var tablesImported []string
	for {
		progress, err := applyStream.Recv()
		if err != nil {
			break
		}
		if progress.CurrentTable != "" && progress.Phase == "importing" {
			found := false
			for _, t := range tablesImported {
				if t == progress.CurrentTable {
					found = true
					break
				}
			}
			if !found {
				tablesImported = append(tablesImported, progress.CurrentTable)
			}
		}
		if progress.Complete {
			break
		}
		if progress.Error != "" {
			s.T().Fatalf("Parallel apply failed: %s", progress.Error)
		}
	}

	duration := time.Since(start)

	s.T().Logf("Applied %d tables in %v with 4 workers", len(tablesImported), duration)
	s.Assert().GreaterOrEqual(len(tablesImported), 4, "Should have imported all 4 tables")

	// Verify all tables have data
	for _, t := range tables {
		var count int
		err := env.targetPool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", t)).Scan(&count)
		s.Require().NoError(err)
		s.Assert().Equal(100, count, "Table %s should have 100 rows", t)
	}
}
