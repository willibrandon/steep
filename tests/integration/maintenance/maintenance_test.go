package maintenance_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// setupPostgres creates a PostgreSQL test container.
func setupPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:18-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
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

// createTestTable creates a test table with some data.
func createTestTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_maintenance (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert some data
	_, err = pool.Exec(ctx, `
		INSERT INTO test_maintenance (data)
		SELECT md5(random()::text)
		FROM generate_series(1, 100)
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Delete some rows to create dead tuples
	_, err = pool.Exec(ctx, `DELETE FROM test_maintenance WHERE id <= 50`)
	if err != nil {
		t.Fatalf("Failed to delete test data: %v", err)
	}
}

// TestExecuteVacuum verifies VACUUM execution works correctly.
func TestExecuteVacuum(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Execute VACUUM
	err := queries.ExecuteVacuumWithOptions(ctx, pool, "public", "test_maintenance", queries.VacuumOptions{})
	if err != nil {
		t.Fatalf("ExecuteVacuum failed: %v", err)
	}
}

// TestExecuteVacuumAnalyze verifies VACUUM ANALYZE execution works correctly.
func TestExecuteVacuumAnalyze(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Execute VACUUM ANALYZE
	err := queries.ExecuteVacuumWithOptions(ctx, pool, "public", "test_maintenance", queries.VacuumOptions{Analyze: true})
	if err != nil {
		t.Fatalf("ExecuteVacuumAnalyze failed: %v", err)
	}
}

// TestExecuteVacuumFull verifies VACUUM FULL execution works correctly.
func TestExecuteVacuumFull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Execute VACUUM FULL
	err := queries.ExecuteVacuumWithOptions(ctx, pool, "public", "test_maintenance", queries.VacuumOptions{Full: true})
	if err != nil {
		t.Fatalf("ExecuteVacuumFull failed: %v", err)
	}
}

// TestExecuteAnalyze verifies ANALYZE execution works correctly.
func TestExecuteAnalyze(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Execute ANALYZE
	err := queries.ExecuteAnalyze(ctx, pool, "public", "test_maintenance")
	if err != nil {
		t.Fatalf("ExecuteAnalyze failed: %v", err)
	}
}

// TestExecuteReindex verifies REINDEX execution works correctly.
func TestExecuteReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Execute REINDEX
	err := queries.ExecuteReindex(ctx, pool, "public", "test_maintenance")
	if err != nil {
		t.Fatalf("ExecuteReindex failed: %v", err)
	}
}

// TestVacuumProgress verifies vacuum progress tracking returns nil for completed operations.
func TestVacuumProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// When no vacuum is running, progress should be nil
	progress, err := queries.GetVacuumProgress(ctx, pool, "public", "test_maintenance")
	if err != nil {
		t.Fatalf("GetVacuumProgress failed: %v", err)
	}
	if progress != nil {
		t.Errorf("Expected nil progress when no vacuum running, got: %+v", progress)
	}
}

// TestCancelBackend verifies cancel backend returns false for non-existent PID.
func TestCancelBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Try to cancel a non-existent PID (should return false, not error)
	cancelled, err := queries.CancelBackend(ctx, pool, 99999999)
	if err != nil {
		t.Fatalf("CancelBackend failed: %v", err)
	}
	if cancelled {
		t.Error("Expected cancelled=false for non-existent PID")
	}
}

// TestGetRunningMaintenanceOperations verifies we can query running operations.
func TestGetRunningMaintenanceOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// When no maintenance is running, should return empty slice
	ops, err := queries.GetRunningMaintenanceOperations(ctx, pool)
	if err != nil {
		t.Fatalf("GetRunningMaintenanceOperations failed: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("Expected 0 running operations, got %d", len(ops))
	}
}

// TestGetTablesWithVacuumStatus verifies tables are returned with vacuum status fields.
func TestGetTablesWithVacuumStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestTable(t, ctx, pool)

	// Run VACUUM and ANALYZE to populate timestamps
	err := queries.ExecuteVacuum(ctx, pool, "public", "test_maintenance")
	if err != nil {
		t.Fatalf("ExecuteVacuum failed: %v", err)
	}
	err = queries.ExecuteAnalyze(ctx, pool, "public", "test_maintenance")
	if err != nil {
		t.Fatalf("ExecuteAnalyze failed: %v", err)
	}

	// Get tables with stats
	tables, err := queries.GetTablesWithStats(ctx, pool)
	if err != nil {
		t.Fatalf("GetTablesWithStats failed: %v", err)
	}

	// Find our test table
	var found bool
	for _, table := range tables {
		if table.SchemaName == "public" && table.Name == "test_maintenance" {
			found = true
			// Verify vacuum status fields are populated
			if table.LastVacuum == nil {
				t.Error("Expected LastVacuum to be set after VACUUM")
			}
			if table.LastAnalyze == nil {
				t.Error("Expected LastAnalyze to be set after ANALYZE")
			}
			if table.VacuumCount < 1 {
				t.Errorf("Expected VacuumCount >= 1, got %d", table.VacuumCount)
			}
			break
		}
	}
	if !found {
		t.Error("test_maintenance table not found in results")
	}
}
