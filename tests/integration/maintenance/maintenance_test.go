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

// TestCancelBackendWithRunningQuery verifies CancelBackend can cancel an actual running query.
func TestCancelBackendWithRunningQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Channel to receive PID from the long-running query
	pidChan := make(chan int, 1)
	errChan := make(chan error, 1)

	// Start a long-running query in a goroutine
	go func() {
		// Get a dedicated connection for the long-running query
		conn, err := pool.Acquire(ctx)
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Release()

		// Get the backend PID
		var pid int
		err = conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid)
		if err != nil {
			errChan <- err
			return
		}
		pidChan <- pid

		// Run a long sleep (will be cancelled)
		_, err = conn.Exec(ctx, "SELECT pg_sleep(30)")
		// We expect this to fail due to cancellation
		errChan <- err
	}()

	// Wait for the PID
	var pid int
	select {
	case pid = <-pidChan:
		t.Logf("Long-running query started with PID %d", pid)
	case err := <-errChan:
		t.Fatalf("Failed to start long-running query: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for long-running query to start")
	}

	// Give the query a moment to start sleeping
	time.Sleep(100 * time.Millisecond)

	// Cancel the backend
	cancelled, err := queries.CancelBackend(ctx, pool, pid)
	if err != nil {
		t.Fatalf("CancelBackend failed: %v", err)
	}
	if !cancelled {
		t.Error("Expected CancelBackend to return true for running query")
	}

	// Wait for the query to finish (with error due to cancellation)
	select {
	case err := <-errChan:
		if err == nil {
			t.Error("Expected error from cancelled query, got nil")
		} else {
			t.Logf("Query cancelled as expected: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for cancelled query to finish")
	}
}

// TestVacuumProgressIncludesPID verifies that vacuum progress tracking returns the PID.
func TestVacuumProgressIncludesPID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create a large table to have enough time to catch progress
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_vacuum_progress (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert 1M rows to make vacuum take long enough to catch
	t.Log("Inserting 1M rows...")
	_, err = pool.Exec(ctx, `
		INSERT INTO test_vacuum_progress (data)
		SELECT repeat(md5(random()::text), 10)
		FROM generate_series(1, 1000000)
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Delete half to create dead tuples
	t.Log("Deleting 500K rows to create dead tuples...")
	_, err = pool.Exec(ctx, `DELETE FROM test_vacuum_progress WHERE id <= 500000`)
	if err != nil {
		t.Fatalf("Failed to delete test data: %v", err)
	}

	// Start VACUUM in a goroutine
	vacuumDone := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, "VACUUM test_vacuum_progress")
		vacuumDone <- err
	}()

	// Poll for progress - with 1M rows we should reliably catch it
	var foundProgress bool
	t.Log("Starting VACUUM and polling for progress...")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress, err := queries.GetVacuumProgress(ctx, pool, "public", "test_vacuum_progress")
		if err != nil {
			t.Logf("Progress poll error (may be expected): %v", err)
		}
		if progress != nil {
			foundProgress = true
			t.Logf("Found vacuum progress: PID=%d, Phase=%s, Percent=%.1f%%",
				progress.PID, progress.Phase, progress.PercentComplete)
			if progress.PID == 0 {
				t.Error("Expected non-zero PID in progress")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for VACUUM to complete
	select {
	case err := <-vacuumDone:
		if err != nil {
			t.Fatalf("VACUUM failed: %v", err)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("VACUUM timed out")
	}

	// With 1M rows we should have caught the progress
	if !foundProgress {
		t.Error("Expected to catch vacuum progress with 1M rows")
	}
}

// TestCancelRunningVacuum verifies we can cancel a running VACUUM operation.
func TestCancelRunningVacuum(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create a large table to ensure VACUUM takes long enough to cancel
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_cancel_vacuum (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert 1M rows
	t.Log("Inserting 1M rows...")
	_, err = pool.Exec(ctx, `
		INSERT INTO test_cancel_vacuum (data)
		SELECT repeat(md5(random()::text), 10)
		FROM generate_series(1, 1000000)
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Delete half to create dead tuples
	t.Log("Deleting 500K rows to create dead tuples...")
	_, err = pool.Exec(ctx, `DELETE FROM test_cancel_vacuum WHERE id <= 500000`)
	if err != nil {
		t.Fatalf("Failed to delete test data: %v", err)
	}

	// Start VACUUM in a goroutine
	t.Log("Starting VACUUM...")
	vacuumDone := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, "VACUUM test_cancel_vacuum")
		vacuumDone <- err
	}()

	// Wait a bit for VACUUM to start, then try to find and cancel it
	time.Sleep(100 * time.Millisecond)

	var cancelled bool
	var foundPID int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress, err := queries.GetVacuumProgress(ctx, pool, "public", "test_cancel_vacuum")
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if progress != nil && progress.PID > 0 {
			foundPID = progress.PID
			t.Logf("Found VACUUM with PID %d at %.1f%%, attempting to cancel...",
				progress.PID, progress.PercentComplete)
			cancelled, err = queries.CancelBackend(ctx, pool, progress.PID)
			if err != nil {
				t.Logf("Cancel error: %v", err)
			} else if cancelled {
				t.Log("Successfully sent cancel signal")
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for VACUUM to complete (should be cancelled)
	select {
	case err := <-vacuumDone:
		if cancelled {
			// If we cancelled, we expect an error
			if err != nil {
				t.Logf("VACUUM cancelled as expected: %v", err)
			} else {
				t.Error("Expected VACUUM to fail after cancellation, but it succeeded")
			}
		} else {
			t.Errorf("Failed to cancel VACUUM (foundPID=%d)", foundPID)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("VACUUM timed out")
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
