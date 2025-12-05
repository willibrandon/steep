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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// tempSocketPath creates a short socket path in /tmp to avoid Unix socket path length limits.
// Returns the socket path and a cleanup function.
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

// TestExtension_CreateExtension verifies the steep_repl extension can be created.
func TestExtension_CreateExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Verify the extension is installed
	var installed bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')").Scan(&installed)
	if err != nil {
		t.Fatalf("Failed to check extension: %v", err)
	}

	if !installed {
		t.Error("Extension should be installed after CREATE EXTENSION")
	}
}

// TestExtension_SchemaCreated verifies the steep_repl schema is created.
func TestExtension_SchemaCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Verify schema exists
	var schemaExists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'steep_repl')").Scan(&schemaExists)
	if err != nil {
		t.Fatalf("Failed to check schema: %v", err)
	}

	if !schemaExists {
		t.Error("steep_repl schema should exist after extension creation")
	}
}

// TestExtension_TablesCreated verifies all required tables are created.
func TestExtension_TablesCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Expected tables per data-model.md
	expectedTables := []string{"nodes", "coordinator_state", "audit_log"}

	for _, table := range expectedTables {
		var tableExists bool
		query := "SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = $1)"
		err := pool.QueryRow(ctx, query, table).Scan(&tableExists)
		if err != nil {
			t.Fatalf("Failed to check table %s: %v", table, err)
		}

		if !tableExists {
			t.Errorf("Table steep_repl.%s should exist", table)
		}
	}
}

// TestExtension_NodesTableStructure verifies the nodes table has correct structure.
func TestExtension_NodesTableStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Check required columns exist
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
		err := pool.QueryRow(ctx, query, col).Scan(&columnExists)
		if err != nil {
			t.Fatalf("Failed to check column nodes.%s: %v", col, err)
		}

		if !columnExists {
			t.Errorf("Column nodes.%s should exist", col)
		}
	}
}

// TestExtension_AuditLogTableStructure verifies the audit_log table has correct structure.
func TestExtension_AuditLogTableStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Check required columns exist
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
		err := pool.QueryRow(ctx, query, col).Scan(&columnExists)
		if err != nil {
			t.Fatalf("Failed to check column audit_log.%s: %v", col, err)
		}

		if !columnExists {
			t.Errorf("Column audit_log.%s should exist", col)
		}
	}
}

// TestExtension_AuditLogIndexes verifies the audit_log table has expected indexes.
func TestExtension_AuditLogIndexes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Check for indexes on commonly queried columns
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
		err := pool.QueryRow(ctx, query, indexName).Scan(&hasIndex)
		if err != nil {
			t.Fatalf("Failed to check index %s: %v", indexName, err)
		}

		if !hasIndex {
			t.Errorf("Index %s should exist", indexName)
		}
	}
}

// TestExtension_CoordinatorStateTable verifies the coordinator_state table structure.
func TestExtension_CoordinatorStateTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Check required columns
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
		err := pool.QueryRow(ctx, query, col).Scan(&columnExists)
		if err != nil {
			t.Fatalf("Failed to check column coordinator_state.%s: %v", col, err)
		}

		if !columnExists {
			t.Errorf("Column coordinator_state.%s should exist", col)
		}
	}
}

// TestExtension_InsertNode verifies we can insert into the nodes table.
func TestExtension_InsertNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Insert a test node
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('test-node-1', 'Test Node', 'localhost', 5433, 80, 'healthy')
	`)
	if err != nil {
		t.Fatalf("Failed to insert node: %v", err)
	}

	// Verify the insert
	var nodeID, status string
	err = pool.QueryRow(ctx, "SELECT node_id, status FROM steep_repl.nodes WHERE node_id = 'test-node-1'").Scan(&nodeID, &status)
	if err != nil {
		t.Fatalf("Failed to query node: %v", err)
	}

	if nodeID != "test-node-1" {
		t.Errorf("node_id = %q, want 'test-node-1'", nodeID)
	}
	if status != "healthy" {
		t.Errorf("status = %q, want 'healthy'", status)
	}
}

// TestExtension_InsertAuditLog verifies we can insert into the audit_log table.
func TestExtension_InsertAuditLog(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Insert a test audit log entry
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.audit_log (action, actor, target_type, target_id, success)
		VALUES ('daemon.started', 'test-node', 'daemon', 'test-node-1', true)
	`)
	if err != nil {
		t.Fatalf("Failed to insert audit log: %v", err)
	}

	// Verify the insert
	var action, actor string
	var success bool
	err = pool.QueryRow(ctx, `
		SELECT action, actor, success
		FROM steep_repl.audit_log
		WHERE actor = 'test-node'
		ORDER BY occurred_at DESC
		LIMIT 1
	`).Scan(&action, &actor, &success)
	if err != nil {
		t.Fatalf("Failed to query audit log: %v", err)
	}

	if action != "daemon.started" {
		t.Errorf("action = %q, want 'daemon.started'", action)
	}
	if !success {
		t.Error("success should be true")
	}
}

// TestExtension_NodesConstraints verifies the nodes table constraints work.
func TestExtension_NodesConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Test priority constraint (must be 1-100)
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-priority', 'Bad Node', 'localhost', 5432, 0, 'healthy')
	`)
	if err == nil {
		t.Error("Expected priority constraint violation for priority=0")
	}

	// Test port constraint (must be 1-65535)
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-port', 'Bad Node', 'localhost', 0, 50, 'healthy')
	`)
	if err == nil {
		t.Error("Expected port constraint violation for port=0")
	}

	// Test status constraint (must be valid status)
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-status', 'Bad Node', 'localhost', 5432, 50, 'invalid_status')
	`)
	if err == nil {
		t.Error("Expected status constraint violation for invalid status")
	}

	// Test host constraint (must not be empty)
	_, err = pool.Exec(ctx, `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
		VALUES ('bad-host', 'Bad Node', '', 5432, 50, 'healthy')
	`)
	if err == nil {
		t.Error("Expected host constraint violation for empty host")
	}
}

// TestExtension_DropExtension verifies the extension can be dropped cleanly.
func TestExtension_DropExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Drop the extension
	_, err = pool.Exec(ctx, "DROP EXTENSION steep_repl CASCADE")
	if err != nil {
		t.Fatalf("Failed to drop extension: %v", err)
	}

	// Verify the extension is gone
	var installed bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')").Scan(&installed)
	if err != nil {
		t.Fatalf("Failed to check extension: %v", err)
	}

	if installed {
		t.Error("Extension should be removed after DROP EXTENSION")
	}
}

// TestExtension_VersionFunction verifies the steep_repl_version() function works.
func TestExtension_VersionFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Call the version function
	var version string
	err = pool.QueryRow(ctx, "SELECT steep_repl_version()").Scan(&version)
	if err != nil {
		t.Fatalf("Failed to call steep_repl_version(): %v", err)
	}

	if version == "" {
		t.Error("Version should not be empty")
	}

	// Should be a semver-like version
	t.Logf("steep_repl version: %s", version)
}

// TestExtension_MinPGVersionFunction verifies the steep_repl_min_pg_version() function works.
func TestExtension_MinPGVersionFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)

	// Create the extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	// Call the min version function
	var minVersion int
	err = pool.QueryRow(ctx, "SELECT steep_repl_min_pg_version()").Scan(&minVersion)
	if err != nil {
		t.Fatalf("Failed to call steep_repl_min_pg_version(): %v", err)
	}

	// Should require PG18 (180000)
	if minVersion != 180000 {
		t.Errorf("min_pg_version = %d, want 180000", minVersion)
	}
}
