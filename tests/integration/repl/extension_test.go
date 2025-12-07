package repl_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// tempSocketPath creates a short socket path in /tmp to avoid Unix socket path length limits.
func tempSocketPath(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("Failed to generate random bytes: %v", err)
	}
	path := fmt.Sprintf("/tmp/sr-%s.sock", hex.EncodeToString(b))
	t.Cleanup(func() {
		os.Remove(path)
	})
	return path
}

// setupPostgresWithExtension creates a PostgreSQL 18 container with steep_repl extension pre-installed.
// The image ghcr.io/willibrandon/pg18-steep-repl contains PG18 + the compiled steep_repl extension.
// It also sets PGPASSWORD environment variable for the daemon tests.
func setupPostgresWithExtension(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	const testPassword = "test"

	// Set PGPASSWORD for daemon connections
	t.Setenv("PGPASSWORD", testPassword)

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
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	connStr := "postgres://test:test@" + host + ":" + port.Port() + "/testdb?sslmode=disable"

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// =============================================================================
// Extension Test Suite - shares a single container across all tests
// =============================================================================

type ExtensionTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestExtensionSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(ExtensionTestSuite))
}

func (s *ExtensionTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

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
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start PostgreSQL container")
	s.container = container

	host, err := container.Host(s.ctx)
	s.Require().NoError(err)

	port, err := container.MappedPort(s.ctx, "5432")
	s.Require().NoError(err)

	connStr := fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, port.Port())
	pool, err := pgxpool.New(s.ctx, connStr)
	s.Require().NoError(err)
	s.pool = pool

	s.T().Log("ExtensionTestSuite: Shared container ready")
}

func (s *ExtensionTestSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.container != nil {
		_ = s.container.Terminate(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ExtensionTestSuite) SetupTest() {
	// Drop and recreate extension for clean state each test
	_, _ = s.pool.Exec(s.ctx, "DROP EXTENSION IF EXISTS steep_repl CASCADE")
}

// =============================================================================
// Tests
// =============================================================================

func (s *ExtensionTestSuite) TestCreateExtension() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension")

	var installed bool
	err = s.pool.QueryRow(s.ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')").Scan(&installed)
	s.Require().NoError(err)
	s.Assert().True(installed, "Extension should be installed after CREATE EXTENSION")
}

func (s *ExtensionTestSuite) TestSchemaCreated() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	var schemaExists bool
	err = s.pool.QueryRow(s.ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'steep_repl')").Scan(&schemaExists)
	s.Require().NoError(err)
	s.Assert().True(schemaExists, "steep_repl schema should exist after extension creation")
}

func (s *ExtensionTestSuite) TestTablesCreated() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	expectedTables := []string{"nodes", "coordinator_state", "audit_log"}

	for _, table := range expectedTables {
		var tableExists bool
		query := "SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = $1)"
		err := s.pool.QueryRow(s.ctx, query, table).Scan(&tableExists)
		s.Require().NoError(err, "Failed to check table %s", table)
		s.Assert().True(tableExists, "Table steep_repl.%s should exist", table)
	}
}

func (s *ExtensionTestSuite) TestNodesTableStructure() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	expectedColumns := []string{
		"node_id", "node_name", "host", "port", "priority",
		"is_coordinator", "status", "last_seen",
	}

	for _, col := range expectedColumns {
		var columnExists bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'steep_repl'
				  AND table_name = 'nodes'
				  AND column_name = $1
			)
		`
		err := s.pool.QueryRow(s.ctx, query, col).Scan(&columnExists)
		s.Require().NoError(err, "Failed to check column nodes.%s", col)
		s.Assert().True(columnExists, "Column nodes.%s should exist", col)
	}
}

func (s *ExtensionTestSuite) TestAuditLogTableStructure() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	expectedColumns := []string{
		"id", "occurred_at", "action", "actor",
		"target_type", "target_id", "old_value", "new_value",
		"success", "error_message",
	}

	for _, col := range expectedColumns {
		var columnExists bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'steep_repl'
				  AND table_name = 'audit_log'
				  AND column_name = $1
			)
		`
		err := s.pool.QueryRow(s.ctx, query, col).Scan(&columnExists)
		s.Require().NoError(err, "Failed to check column audit_log.%s", col)
		s.Assert().True(columnExists, "Column audit_log.%s should exist", col)
	}
}

func (s *ExtensionTestSuite) TestAuditLogIndexes() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	expectedIndexes := []string{
		"idx_audit_log_occurred_at",
		"idx_audit_log_actor",
		"idx_audit_log_action",
		"idx_audit_log_target",
	}

	for _, indexName := range expectedIndexes {
		var hasIndex bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'steep_repl'
				  AND indexname = $1
			)
		`
		err := s.pool.QueryRow(s.ctx, query, indexName).Scan(&hasIndex)
		s.Require().NoError(err, "Failed to check index %s", indexName)
		s.Assert().True(hasIndex, "Index %s should exist", indexName)
	}
}

func (s *ExtensionTestSuite) TestCoordinatorStateTable() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	expectedColumns := []string{"key", "value", "updated_at"}

	for _, col := range expectedColumns {
		var columnExists bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'steep_repl'
				  AND table_name = 'coordinator_state'
				  AND column_name = $1
			)
		`
		err := s.pool.QueryRow(s.ctx, query, col).Scan(&columnExists)
		s.Require().NoError(err, "Failed to check column coordinator_state.%s", col)
		s.Assert().True(columnExists, "Column coordinator_state.%s should exist", col)
	}
}

func (s *ExtensionTestSuite) TestInsertNode() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('test-node-1', 'Test Node', 'localhost', 5433, 80, 'healthy')
	`)
	s.Require().NoError(err, "Failed to insert node")

	var nodeID, status string
	err = s.pool.QueryRow(s.ctx, "SELECT node_id, status FROM steep_repl.nodes WHERE node_id = 'test-node-1'").Scan(&nodeID, &status)
	s.Require().NoError(err)

	s.Assert().Equal("test-node-1", nodeID)
	s.Assert().Equal("healthy", status)
}

func (s *ExtensionTestSuite) TestInsertAuditLog() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.audit_log (action, actor, target_type, target_id, success)
		VALUES ('daemon.started', 'test-node', 'daemon', 'test-node-1', true)
	`)
	s.Require().NoError(err, "Failed to insert audit log")

	var action, actor string
	var success bool
	err = s.pool.QueryRow(s.ctx, `
		SELECT action, actor, success
		FROM steep_repl.audit_log
		WHERE actor = 'test-node'
		ORDER BY occurred_at DESC
		LIMIT 1
	`).Scan(&action, &actor, &success)
	s.Require().NoError(err)

	s.Assert().Equal("daemon.started", action)
	s.Assert().True(success)
}

func (s *ExtensionTestSuite) TestNodesConstraints() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	// Test priority constraint (must be 1-100)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-priority', 'Bad Node', 'localhost', 5432, 0, 'healthy')
	`)
	s.Assert().Error(err, "Expected priority constraint violation for priority=0")

	// Test port constraint (must be 1-65535)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-port', 'Bad Node', 'localhost', 0, 50, 'healthy')
	`)
	s.Assert().Error(err, "Expected port constraint violation for port=0")

	// Test status constraint (must be valid status)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-status', 'Bad Node', 'localhost', 5432, 50, 'invalid_status')
	`)
	s.Assert().Error(err, "Expected status constraint violation for invalid status")

	// Test host constraint (must not be empty)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-host', 'Bad Node', '', 5432, 50, 'healthy')
	`)
	s.Assert().Error(err, "Expected host constraint violation for empty host")
}

func (s *ExtensionTestSuite) TestDropExtension() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	_, err = s.pool.Exec(s.ctx, "DROP EXTENSION steep_repl CASCADE")
	s.Require().NoError(err, "Failed to drop extension")

	var installed bool
	err = s.pool.QueryRow(s.ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')").Scan(&installed)
	s.Require().NoError(err)
	s.Assert().False(installed, "Extension should be removed after DROP EXTENSION")
}

func (s *ExtensionTestSuite) TestVersionFunction() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	var version string
	err = s.pool.QueryRow(s.ctx, "SELECT steep_repl_version()").Scan(&version)
	s.Require().NoError(err, "Failed to call steep_repl_version()")

	s.Assert().NotEmpty(version, "Version should not be empty")
	s.T().Logf("steep_repl version: %s", version)
}

func (s *ExtensionTestSuite) TestMinPGVersionFunction() {
	_, err := s.pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err)

	var minVersion int
	err = s.pool.QueryRow(s.ctx, "SELECT steep_repl_min_pg_version()").Scan(&minVersion)
	s.Require().NoError(err, "Failed to call steep_repl_min_pg_version()")

	s.Assert().Equal(180000, minVersion, "min_pg_version should be 180000")
}
