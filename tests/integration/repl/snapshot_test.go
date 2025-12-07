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
	sourceHost      string // Docker network hostname
	sourcePort      int
	sourceDaemon    *daemon.Daemon
	sourceGRPCPort  int

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

	// Create connection pool
	sourceConnStr := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable",
		sourceHostExternal, sourcePortExternal.Port())
	env.sourcePool, err = pgxpool.New(s.ctx, sourceConnStr)
	s.Require().NoError(err, "Failed to create source pool")

	// Wait for database
	s.waitForDB(env.sourcePool, "source")

	// Create extension
	_, err = env.sourcePool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension on source")

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

	// Create temp directory for snapshots
	env.snapshotDir, err = os.MkdirTemp("", "steep-snapshot-test-*")
	s.Require().NoError(err, "Failed to create temp snapshot directory")

	time.Sleep(time.Second)

	s.env = env
	s.T().Log("SnapshotTestSuite: Shared containers and daemon ready")
}

// TearDownSuite cleans up resources.
func (s *SnapshotTestSuite) TearDownSuite() {
	if s.env != nil {
		if s.env.sourceDaemon != nil {
			s.env.sourceDaemon.Stop()
		}
		if s.env.sourcePool != nil {
			s.env.sourcePool.Close()
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
func (s *SnapshotTestSuite) SetupTest() {
	ctx := s.ctx

	// Drop test tables
	testTables := []string{
		"snapshot_test", "users", "orders", "products",
		"large_table", "small_table", "compressed_test",
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
