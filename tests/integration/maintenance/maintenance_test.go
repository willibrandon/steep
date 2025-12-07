package maintenance_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// =============================================================================
// Maintenance Test Suite - shares a single container across all tests
// =============================================================================

type MaintenanceTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestMaintenanceSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(MaintenanceTestSuite))
}

func (s *MaintenanceTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	req := testcontainers.ContainerRequest{
		Image:        "postgres:18-alpine",
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

	s.T().Log("MaintenanceTestSuite: Shared container ready")
}

func (s *MaintenanceTestSuite) TearDownSuite() {
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

func (s *MaintenanceTestSuite) SetupTest() {
	// Clean up any test tables from previous tests
	tables := []string{
		"test_maintenance", "test_vacuum_progress", "test_cancel_vacuum",
	}
	for _, t := range tables {
		_, _ = s.pool.Exec(s.ctx, "DROP TABLE IF EXISTS "+t+" CASCADE")
	}
}

// createTestTable creates a test table with some data.
func (s *MaintenanceTestSuite) createTestTable() {
	_, err := s.pool.Exec(s.ctx, `
		CREATE TABLE IF NOT EXISTS test_maintenance (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create test table")

	// Insert some data
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO test_maintenance (data)
		SELECT md5(random()::text)
		FROM generate_series(1, 100)
	`)
	s.Require().NoError(err, "Failed to insert test data")

	// Delete some rows to create dead tuples
	_, err = s.pool.Exec(s.ctx, `DELETE FROM test_maintenance WHERE id <= 50`)
	s.Require().NoError(err, "Failed to delete test data")
}

// =============================================================================
// Tests
// =============================================================================

func (s *MaintenanceTestSuite) TestExecuteVacuum() {
	s.createTestTable()

	// Execute VACUUM
	err := queries.ExecuteVacuumWithOptions(s.ctx, s.pool, "public", "test_maintenance", queries.VacuumOptions{})
	s.Require().NoError(err, "ExecuteVacuum failed")
}

func (s *MaintenanceTestSuite) TestExecuteVacuumAnalyze() {
	s.createTestTable()

	// Execute VACUUM ANALYZE
	err := queries.ExecuteVacuumWithOptions(s.ctx, s.pool, "public", "test_maintenance", queries.VacuumOptions{Analyze: true})
	s.Require().NoError(err, "ExecuteVacuumAnalyze failed")
}

func (s *MaintenanceTestSuite) TestExecuteVacuumFull() {
	s.createTestTable()

	// Execute VACUUM FULL
	err := queries.ExecuteVacuumWithOptions(s.ctx, s.pool, "public", "test_maintenance", queries.VacuumOptions{Full: true})
	s.Require().NoError(err, "ExecuteVacuumFull failed")
}

func (s *MaintenanceTestSuite) TestExecuteAnalyze() {
	s.createTestTable()

	// Execute ANALYZE
	err := queries.ExecuteAnalyze(s.ctx, s.pool, "public", "test_maintenance")
	s.Require().NoError(err, "ExecuteAnalyze failed")
}

func (s *MaintenanceTestSuite) TestExecuteReindex() {
	s.createTestTable()

	// Execute REINDEX
	err := queries.ExecuteReindex(s.ctx, s.pool, "public", "test_maintenance")
	s.Require().NoError(err, "ExecuteReindex failed")
}

func (s *MaintenanceTestSuite) TestVacuumProgress() {
	s.createTestTable()

	// When no vacuum is running, progress should be nil
	progress, err := queries.GetVacuumProgress(s.ctx, s.pool, "public", "test_maintenance")
	s.Require().NoError(err, "GetVacuumProgress failed")
	s.Assert().Nil(progress, "Expected nil progress when no vacuum running")
}

func (s *MaintenanceTestSuite) TestCancelBackend() {
	// Try to cancel a non-existent PID (should return false, not error)
	cancelled, err := queries.CancelBackend(s.ctx, s.pool, 99999999)
	s.Require().NoError(err, "CancelBackend failed")
	s.Assert().False(cancelled, "Expected cancelled=false for non-existent PID")
}

func (s *MaintenanceTestSuite) TestGetRunningMaintenanceOperations() {
	// When no maintenance is running, should return empty slice
	ops, err := queries.GetRunningMaintenanceOperations(s.ctx, s.pool)
	s.Require().NoError(err, "GetRunningMaintenanceOperations failed")
	s.Assert().Empty(ops, "Expected 0 running operations")
}

func (s *MaintenanceTestSuite) TestCancelBackendWithRunningQuery() {
	// Channel to receive PID from the long-running query
	pidChan := make(chan int, 1)
	errChan := make(chan error, 1)

	// Start a long-running query in a goroutine
	go func() {
		// Get a dedicated connection for the long-running query
		conn, err := s.pool.Acquire(s.ctx)
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Release()

		// Get the backend PID
		var pid int
		err = conn.QueryRow(s.ctx, "SELECT pg_backend_pid()").Scan(&pid)
		if err != nil {
			errChan <- err
			return
		}
		pidChan <- pid

		// Run a long sleep (will be cancelled)
		_, err = conn.Exec(s.ctx, "SELECT pg_sleep(30)")
		// We expect this to fail due to cancellation
		errChan <- err
	}()

	// Wait for the PID
	var pid int
	select {
	case pid = <-pidChan:
		s.T().Logf("Long-running query started with PID %d", pid)
	case err := <-errChan:
		s.T().Fatalf("Failed to start long-running query: %v", err)
	case <-time.After(5 * time.Second):
		s.T().Fatal("Timeout waiting for long-running query to start")
	}

	// Give the query a moment to start sleeping
	time.Sleep(100 * time.Millisecond)

	// Cancel the backend
	cancelled, err := queries.CancelBackend(s.ctx, s.pool, pid)
	s.Require().NoError(err, "CancelBackend failed")
	s.Assert().True(cancelled, "Expected CancelBackend to return true for running query")

	// Wait for the query to finish (with error due to cancellation)
	select {
	case err := <-errChan:
		s.Assert().Error(err, "Expected error from cancelled query")
		s.T().Logf("Query cancelled as expected: %v", err)
	case <-time.After(5 * time.Second):
		s.T().Error("Timeout waiting for cancelled query to finish")
	}
}

func (s *MaintenanceTestSuite) TestVacuumProgressIncludesPID() {
	// Create a large table to have enough time to catch progress
	_, err := s.pool.Exec(s.ctx, `
		CREATE TABLE IF NOT EXISTS test_vacuum_progress (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create test table")

	// Insert 1M rows to make vacuum take long enough to catch
	s.T().Log("Inserting 1M rows...")
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO test_vacuum_progress (data)
		SELECT repeat(md5(random()::text), 10)
		FROM generate_series(1, 1000000)
	`)
	s.Require().NoError(err, "Failed to insert test data")

	// Delete half to create dead tuples
	s.T().Log("Deleting 500K rows to create dead tuples...")
	_, err = s.pool.Exec(s.ctx, `DELETE FROM test_vacuum_progress WHERE id <= 500000`)
	s.Require().NoError(err, "Failed to delete test data")

	// Start VACUUM in a goroutine
	vacuumDone := make(chan error, 1)
	go func() {
		_, err := s.pool.Exec(s.ctx, "VACUUM test_vacuum_progress")
		vacuumDone <- err
	}()

	// Poll for progress - with 1M rows we should reliably catch it
	var foundProgress bool
	s.T().Log("Starting VACUUM and polling for progress...")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress, err := queries.GetVacuumProgress(s.ctx, s.pool, "public", "test_vacuum_progress")
		if err != nil {
			s.T().Logf("Progress poll error (may be expected): %v", err)
		}
		if progress != nil {
			foundProgress = true
			s.T().Logf("Found vacuum progress: PID=%d, Phase=%s, Percent=%.1f%%",
				progress.PID, progress.Phase, progress.PercentComplete)
			s.Assert().NotZero(progress.PID, "Expected non-zero PID in progress")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for VACUUM to complete
	select {
	case err := <-vacuumDone:
		s.Require().NoError(err, "VACUUM failed")
	case <-time.After(120 * time.Second):
		s.T().Fatal("VACUUM timed out")
	}

	// With 1M rows we should have caught the progress
	if !foundProgress {
		s.T().Error("Expected to catch vacuum progress with 1M rows")
	}
}

func (s *MaintenanceTestSuite) TestCancelRunningVacuum() {
	// Create a large table to ensure VACUUM takes long enough to cancel
	_, err := s.pool.Exec(s.ctx, `
		CREATE TABLE IF NOT EXISTS test_cancel_vacuum (
			id SERIAL PRIMARY KEY,
			data TEXT
		)
	`)
	s.Require().NoError(err, "Failed to create test table")

	// Insert 1M rows
	s.T().Log("Inserting 1M rows...")
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO test_cancel_vacuum (data)
		SELECT repeat(md5(random()::text), 10)
		FROM generate_series(1, 1000000)
	`)
	s.Require().NoError(err, "Failed to insert test data")

	// Delete half to create dead tuples
	s.T().Log("Deleting 500K rows to create dead tuples...")
	_, err = s.pool.Exec(s.ctx, `DELETE FROM test_cancel_vacuum WHERE id <= 500000`)
	s.Require().NoError(err, "Failed to delete test data")

	// Start VACUUM in a goroutine
	s.T().Log("Starting VACUUM...")
	vacuumDone := make(chan error, 1)
	go func() {
		_, err := s.pool.Exec(s.ctx, "VACUUM test_cancel_vacuum")
		vacuumDone <- err
	}()

	// Wait a bit for VACUUM to start, then try to find and cancel it
	time.Sleep(100 * time.Millisecond)

	var cancelled bool
	var foundPID int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress, err := queries.GetVacuumProgress(s.ctx, s.pool, "public", "test_cancel_vacuum")
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if progress != nil && progress.PID > 0 {
			foundPID = progress.PID
			s.T().Logf("Found VACUUM with PID %d at %.1f%%, attempting to cancel...",
				progress.PID, progress.PercentComplete)
			cancelled, err = queries.CancelBackend(s.ctx, s.pool, progress.PID)
			if err != nil {
				s.T().Logf("Cancel error: %v", err)
			} else if cancelled {
				s.T().Log("Successfully sent cancel signal")
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
				s.T().Logf("VACUUM cancelled as expected: %v", err)
			} else {
				s.T().Error("Expected VACUUM to fail after cancellation, but it succeeded")
			}
		} else {
			s.T().Errorf("Failed to cancel VACUUM (foundPID=%d)", foundPID)
		}
	case <-time.After(120 * time.Second):
		s.T().Fatal("VACUUM timed out")
	}
}

func (s *MaintenanceTestSuite) TestGetTablesWithVacuumStatus() {
	s.createTestTable()

	// Run VACUUM and ANALYZE to populate timestamps
	err := queries.ExecuteVacuum(s.ctx, s.pool, "public", "test_maintenance")
	s.Require().NoError(err, "ExecuteVacuum failed")
	err = queries.ExecuteAnalyze(s.ctx, s.pool, "public", "test_maintenance")
	s.Require().NoError(err, "ExecuteAnalyze failed")

	// Get tables with stats
	tables, err := queries.GetTablesWithStats(s.ctx, s.pool)
	s.Require().NoError(err, "GetTablesWithStats failed")

	// Find our test table
	var found bool
	for _, table := range tables {
		if table.SchemaName == "public" && table.Name == "test_maintenance" {
			found = true
			// Verify vacuum status fields are populated
			s.Assert().NotNil(table.LastVacuum, "Expected LastVacuum to be set after VACUUM")
			s.Assert().NotNil(table.LastAnalyze, "Expected LastAnalyze to be set after ANALYZE")
			s.Assert().GreaterOrEqual(table.VacuumCount, int64(1), "Expected VacuumCount >= 1")
			break
		}
	}
	s.Assert().True(found, "test_maintenance table not found in results")
}
