package locks_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// setupPostgres creates a PostgreSQL test container for standalone tests.
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

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("Failed to parse connection string: %v", err)
	}
	config.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// =============================================================================
// Locks Test Suite - shares a single container across all tests
// =============================================================================

type LocksTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestLocksSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(LocksTestSuite))
}

func (s *LocksTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

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

	config, err := pgxpool.ParseConfig(connStr)
	s.Require().NoError(err)
	config.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(s.ctx, config)
	s.Require().NoError(err)
	s.pool = pool

	s.T().Log("LocksTestSuite: Shared container ready")
}

func (s *LocksTestSuite) TearDownSuite() {
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

func (s *LocksTestSuite) SetupTest() {
	// Clean up any test tables from previous tests
	tables := []string{
		"test_locks", "test_blocking", "test_rel_blocking",
		"test_terminate", "test_multi_blocked", "test_lock_types",
		"test_duration",
	}
	for _, t := range tables {
		_, _ = s.pool.Exec(s.ctx, "DROP TABLE IF EXISTS "+t+" CASCADE")
	}
	// Clean up performance test tables
	for i := 0; i < 10; i++ {
		_, _ = s.pool.Exec(s.ctx, fmt.Sprintf("DROP TABLE IF EXISTS perf_test_%c CASCADE", 'a'+i))
	}
}

// =============================================================================
// Tests
// =============================================================================

func (s *LocksTestSuite) TestGetLocks_NoLocks() {
	locks, err := queries.GetLocks(s.ctx, s.pool)
	s.Require().NoError(err, "GetLocks failed")

	// GetLocks can return nil slice when no locks exist - that's valid
	s.T().Logf("GetLocks returned %d locks", len(locks))
}

func (s *LocksTestSuite) TestGetLocks_WithTableLock() {
	// Create a test table
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_locks (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")

	// Start a transaction that holds a lock
	conn, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection")
	defer conn.Release()

	tx, err := conn.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction")
	defer tx.Rollback(s.ctx)

	// Take an exclusive lock on the table
	_, err = tx.Exec(s.ctx, "LOCK TABLE test_locks IN EXCLUSIVE MODE")
	s.Require().NoError(err, "Failed to lock table")

	// Now query locks - should see our lock
	locks, err := queries.GetLocks(s.ctx, s.pool)
	s.Require().NoError(err, "GetLocks failed")

	// Find our lock
	found := false
	for _, lock := range locks {
		if lock.Relation == "test_locks" && lock.Mode == "ExclusiveLock" && lock.Granted {
			found = true
			break
		}
	}

	if !found {
		s.T().Errorf("Expected to find ExclusiveLock on test_locks, got %d locks", len(locks))
		for _, lock := range locks {
			s.T().Logf("Lock: relation=%s mode=%s granted=%v", lock.Relation, lock.Mode, lock.Granted)
		}
	}
}

func (s *LocksTestSuite) TestGetLocks_BlockingScenario() {
	// Create a test table with a row
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_blocking (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")
	_, err = s.pool.Exec(s.ctx, "INSERT INTO test_blocking (data) VALUES ('test')")
	s.Require().NoError(err, "Failed to insert test row")

	// Connection 1: Start transaction and lock row
	conn1, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 1")
	defer conn1.Release()

	tx1, err := conn1.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction 1")
	defer tx1.Rollback(s.ctx)

	// Lock the row with FOR UPDATE
	_, err = tx1.Exec(s.ctx, "SELECT * FROM test_blocking WHERE id = 1 FOR UPDATE")
	s.Require().NoError(err, "Failed to lock row")

	// Connection 2: Try to update same row (will block)
	conn2, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 2")
	defer conn2.Release()

	var wg sync.WaitGroup
	wg.Add(1)

	blockedStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		tx2, err := conn2.Begin(s.ctx)
		if err != nil {
			s.T().Logf("Failed to begin transaction 2: %v", err)
			return
		}
		defer tx2.Rollback(s.ctx)

		close(blockedStarted)

		// This will block waiting for tx1's lock
		_, _ = tx2.Exec(s.ctx, "UPDATE test_blocking SET data = 'blocked' WHERE id = 1")
	}()

	// Wait for blocked transaction to start
	<-blockedStarted
	time.Sleep(100 * time.Millisecond) // Give it time to actually block

	// Query locks - should see granted and waiting locks
	locks, err := queries.GetLocks(s.ctx, s.pool)
	s.Require().NoError(err, "GetLocks failed")

	// Check we have some locks
	s.Assert().NotEmpty(locks, "Expected to find locks in blocking scenario")

	// Look for both granted and waiting locks
	var grantedCount, waitingCount int
	for _, lock := range locks {
		if lock.Relation == "test_blocking" {
			if lock.Granted {
				grantedCount++
			} else {
				waitingCount++
			}
		}
	}

	s.T().Logf("Found %d granted and %d waiting locks on test_blocking", grantedCount, waitingCount)

	// Commit tx1 to unblock tx2
	tx1.Commit(s.ctx)

	// Wait for goroutine to finish
	wg.Wait()
}

func (s *LocksTestSuite) TestGetBlockingRelationships_NoBlocking() {
	rels, err := queries.GetBlockingRelationships(s.ctx, s.pool)
	s.Require().NoError(err, "GetBlockingRelationships failed")

	s.Assert().Empty(rels, "Expected 0 blocking relationships")
}

func (s *LocksTestSuite) TestGetBlockingRelationships_WithBlocking() {
	// Create test table with row
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_rel_blocking (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")
	_, err = s.pool.Exec(s.ctx, "INSERT INTO test_rel_blocking (data) VALUES ('test')")
	s.Require().NoError(err, "Failed to insert test row")

	// Connection 1: Hold lock
	conn1, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 1")
	defer conn1.Release()

	tx1, err := conn1.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction 1")
	defer tx1.Rollback(s.ctx)

	_, err = tx1.Exec(s.ctx, "SELECT * FROM test_rel_blocking WHERE id = 1 FOR UPDATE")
	s.Require().NoError(err, "Failed to lock row")

	// Connection 2: Will be blocked
	conn2, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 2")
	defer conn2.Release()

	var wg sync.WaitGroup
	wg.Add(1)

	blockedStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		tx2, err := conn2.Begin(s.ctx)
		if err != nil {
			return
		}
		defer tx2.Rollback(s.ctx)

		close(blockedStarted)

		// This blocks
		_, _ = tx2.Exec(s.ctx, "UPDATE test_rel_blocking SET data = 'blocked' WHERE id = 1")
	}()

	<-blockedStarted
	time.Sleep(200 * time.Millisecond)

	// Query blocking relationships
	rels, err := queries.GetBlockingRelationships(s.ctx, s.pool)
	s.Require().NoError(err, "GetBlockingRelationships failed")

	if len(rels) == 0 {
		s.T().Error("Expected to find blocking relationships")
	} else {
		s.T().Logf("Found %d blocking relationship(s)", len(rels))
		for _, rel := range rels {
			s.T().Logf("Blocked PID %d by PID %d", rel.BlockedPID, rel.BlockingPID)
		}
	}

	// Cleanup
	tx1.Commit(s.ctx)
	wg.Wait()
}

func (s *LocksTestSuite) TestTerminateBackend() {
	// Create a connection to terminate
	conn, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection")

	// Get the PID of this connection
	var pid int
	err = conn.QueryRow(s.ctx, "SELECT pg_backend_pid()").Scan(&pid)
	if err != nil {
		conn.Release()
		s.T().Fatalf("Failed to get backend PID: %v", err)
	}

	// Release before terminating
	conn.Release()

	// Terminate the backend
	success, err := queries.TerminateBackend(s.ctx, s.pool, pid)
	if err != nil {
		// Connection might already be closed
		s.T().Logf("TerminateBackend returned error (may be expected): %v", err)
	} else if !success {
		s.T().Log("TerminateBackend returned false (connection may have already closed)")
	}
}

func (s *LocksTestSuite) TestTerminateBackend_BlockingProcess() {
	// Create test table with row
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_terminate (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")
	_, err = s.pool.Exec(s.ctx, "INSERT INTO test_terminate (data) VALUES ('test')")
	s.Require().NoError(err, "Failed to insert test row")

	// Connection 1: Blocker - hold lock
	conn1, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 1")
	defer conn1.Release()

	// Get blocker PID
	var blockerPID int
	err = conn1.QueryRow(s.ctx, "SELECT pg_backend_pid()").Scan(&blockerPID)
	s.Require().NoError(err, "Failed to get blocker PID")

	tx1, err := conn1.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction 1")
	defer tx1.Rollback(s.ctx)

	// Lock the row
	_, err = tx1.Exec(s.ctx, "SELECT * FROM test_terminate WHERE id = 1 FOR UPDATE")
	s.Require().NoError(err, "Failed to lock row")

	// Connection 2: Blocked - will wait for lock
	conn2, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 2")
	defer conn2.Release()

	var wg sync.WaitGroup
	wg.Add(1)

	blockedStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		tx2, err := conn2.Begin(s.ctx)
		if err != nil {
			return
		}
		defer tx2.Rollback(s.ctx)

		close(blockedStarted)

		// This will block waiting for tx1's lock
		_, _ = tx2.Exec(s.ctx, "UPDATE test_terminate SET data = 'updated' WHERE id = 1")
	}()

	// Wait for blocked transaction to start
	<-blockedStarted
	time.Sleep(200 * time.Millisecond)

	// Verify blocking relationship exists
	rels, err := queries.GetBlockingRelationships(s.ctx, s.pool)
	s.Require().NoError(err, "GetBlockingRelationships failed")
	s.Require().NotEmpty(rels, "Expected blocking relationship before termination")

	// Find the blocking relationship
	var foundBlocking bool
	for _, rel := range rels {
		if rel.BlockingPID == blockerPID {
			foundBlocking = true
			s.T().Logf("Found blocking: PID %d blocking PID %d", rel.BlockingPID, rel.BlockedPID)
			break
		}
	}
	s.Require().True(foundBlocking, "Blocker PID %d not found in relationships", blockerPID)

	// Terminate the blocker
	success, err := queries.TerminateBackend(s.ctx, s.pool, blockerPID)
	s.Require().NoError(err, "TerminateBackend failed")
	s.Assert().True(success, "TerminateBackend should return true")

	// Wait for blocked transaction to complete (should error due to terminated connection)
	wg.Wait()

	// Check that blocking relationship is gone
	time.Sleep(100 * time.Millisecond)
	rels, err = queries.GetBlockingRelationships(s.ctx, s.pool)
	s.Require().NoError(err, "GetBlockingRelationships failed after termination")

	// Should have no blocking relationships now
	for _, rel := range rels {
		s.Assert().NotEqual(blockerPID, rel.BlockingPID, "Blocker PID %d still present after termination", blockerPID)
	}

	s.T().Log("Successfully terminated blocking process")
}

func (s *LocksTestSuite) TestTerminateBackend_NonExistentPID() {
	// Use a PID that definitely doesn't exist
	success, err := queries.TerminateBackend(s.ctx, s.pool, 999999)
	s.Require().NoError(err, "TerminateBackend failed")

	// Should return false for non-existent PID
	s.Assert().False(success, "Expected false for non-existent PID")
}

func (s *LocksTestSuite) TestTerminateBackend_MultipleBlocked() {
	// Create test table with row
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_multi_blocked (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")
	_, err = s.pool.Exec(s.ctx, "INSERT INTO test_multi_blocked (data) VALUES ('test')")
	s.Require().NoError(err, "Failed to insert test row")

	// Connection 1: Blocker
	conn1, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection 1")
	defer conn1.Release()

	var blockerPID int
	err = conn1.QueryRow(s.ctx, "SELECT pg_backend_pid()").Scan(&blockerPID)
	s.Require().NoError(err, "Failed to get blocker PID")

	tx1, err := conn1.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction 1")
	defer tx1.Rollback(s.ctx)

	_, err = tx1.Exec(s.ctx, "SELECT * FROM test_multi_blocked WHERE id = 1 FOR UPDATE")
	s.Require().NoError(err, "Failed to lock row")

	// Create multiple blocked connections
	var wg sync.WaitGroup
	numBlocked := 3

	for i := 0; i < numBlocked; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			conn, err := s.pool.Acquire(s.ctx)
			if err != nil {
				s.T().Logf("Blocked %d: Failed to acquire connection: %v", idx, err)
				return
			}
			defer conn.Release()

			tx, err := conn.Begin(s.ctx)
			if err != nil {
				return
			}
			defer tx.Rollback(s.ctx)

			// This will block
			_, _ = tx.Exec(s.ctx, "UPDATE test_multi_blocked SET data = 'blocked' WHERE id = 1")
		}(i)
	}

	// Wait for all to start blocking
	time.Sleep(300 * time.Millisecond)

	// Verify multiple blocked
	rels, err := queries.GetBlockingRelationships(s.ctx, s.pool)
	s.Require().NoError(err, "GetBlockingRelationships failed")

	blockedCount := 0
	for _, rel := range rels {
		if rel.BlockingPID == blockerPID {
			blockedCount++
		}
	}

	if blockedCount < 2 {
		s.T().Logf("Expected at least 2 blocked by PID %d, found %d", blockerPID, blockedCount)
	} else {
		s.T().Logf("Found %d blocked by PID %d", blockedCount, blockerPID)
	}

	// Terminate blocker
	success, err := queries.TerminateBackend(s.ctx, s.pool, blockerPID)
	s.Require().NoError(err, "TerminateBackend failed")
	s.Assert().True(success, "TerminateBackend should return true")

	// Wait for all blocked to complete
	wg.Wait()

	s.T().Logf("Successfully terminated blocker, all %d blocked connections released", numBlocked)
}

func (s *LocksTestSuite) TestGetLocks_Performance() {
	// Create some tables to generate locks
	for i := 0; i < 10; i++ {
		_, err := s.pool.Exec(s.ctx, fmt.Sprintf("CREATE TABLE perf_test_%c (id SERIAL)", 'a'+i))
		s.Require().NoError(err, "Failed to create table")
	}

	// Measure GetLocks performance
	start := time.Now()
	_, err := queries.GetLocks(s.ctx, s.pool)
	elapsed := time.Since(start)

	s.Require().NoError(err, "GetLocks failed")

	if elapsed > 500*time.Millisecond {
		s.T().Errorf("GetLocks took %v, expected < 500ms", elapsed)
	} else {
		s.T().Logf("GetLocks completed in %v", elapsed)
	}
}

func (s *LocksTestSuite) TestGetBlockingRelationships_Performance() {
	start := time.Now()
	_, err := queries.GetBlockingRelationships(s.ctx, s.pool)
	elapsed := time.Since(start)

	s.Require().NoError(err, "GetBlockingRelationships failed")

	if elapsed > 500*time.Millisecond {
		s.T().Errorf("GetBlockingRelationships took %v, expected < 500ms", elapsed)
	} else {
		s.T().Logf("GetBlockingRelationships completed in %v", elapsed)
	}
}

func (s *LocksTestSuite) TestGetLocks_LockTypes() {
	// Create test table
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_lock_types (id SERIAL PRIMARY KEY, data TEXT)")
	s.Require().NoError(err, "Failed to create test table")

	// Test different lock modes
	lockModes := []string{
		"ACCESS SHARE",
		"ROW SHARE",
		"ROW EXCLUSIVE",
		"SHARE UPDATE EXCLUSIVE",
		"SHARE",
		"SHARE ROW EXCLUSIVE",
		"EXCLUSIVE",
		"ACCESS EXCLUSIVE",
	}

	for _, mode := range lockModes {
		s.Run(mode, func() {
			conn, err := s.pool.Acquire(s.ctx)
			s.Require().NoError(err, "Failed to acquire connection")
			defer conn.Release()

			tx, err := conn.Begin(s.ctx)
			s.Require().NoError(err, "Failed to begin transaction")
			defer tx.Rollback(s.ctx)

			_, err = tx.Exec(s.ctx, "LOCK TABLE test_lock_types IN "+mode+" MODE")
			s.Require().NoError(err, "Failed to acquire %s lock", mode)

			locks, err := queries.GetLocks(s.ctx, s.pool)
			s.Require().NoError(err, "GetLocks failed")

			// Find our lock
			found := false
			for _, lock := range locks {
				if lock.Relation == "test_lock_types" && lock.Granted {
					found = true
					s.T().Logf("Found lock: mode=%s", lock.Mode)
					break
				}
			}

			s.Assert().True(found, "Failed to find %s lock", mode)
		})
	}
}

func (s *LocksTestSuite) TestGetLocks_Duration() {
	// Create test table
	_, err := s.pool.Exec(s.ctx, "CREATE TABLE test_duration (id SERIAL PRIMARY KEY)")
	s.Require().NoError(err, "Failed to create test table")

	conn, err := s.pool.Acquire(s.ctx)
	s.Require().NoError(err, "Failed to acquire connection")
	defer conn.Release()

	tx, err := conn.Begin(s.ctx)
	s.Require().NoError(err, "Failed to begin transaction")
	defer tx.Rollback(s.ctx)

	_, err = tx.Exec(s.ctx, "LOCK TABLE test_duration IN EXCLUSIVE MODE")
	s.Require().NoError(err, "Failed to lock table")

	// Wait a bit to accumulate duration
	time.Sleep(100 * time.Millisecond)

	locks, err := queries.GetLocks(s.ctx, s.pool)
	s.Require().NoError(err, "GetLocks failed")

	for _, lock := range locks {
		if lock.Relation == "test_duration" {
			if lock.Duration < 100*time.Millisecond {
				s.T().Errorf("Duration %v less than expected 100ms", lock.Duration)
			} else {
				s.T().Logf("Lock duration: %v", lock.Duration)
			}
			return
		}
	}

	s.T().Error("Lock on test_duration not found")
}
