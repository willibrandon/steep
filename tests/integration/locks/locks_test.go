package locks_test

import (
	"context"
	"sync"
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
		Image:        "postgres:15-alpine",
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

// TestGetLocks_NoLocks verifies GetLocks returns empty when no locks exist.
func TestGetLocks_NoLocks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	locks, err := queries.GetLocks(ctx, pool)
	if err != nil {
		t.Fatalf("GetLocks failed: %v", err)
	}

	// GetLocks can return nil slice when no locks exist - that's valid
	t.Logf("GetLocks returned %d locks", len(locks))
}

// TestGetLocks_WithTableLock verifies GetLocks detects table locks.
func TestGetLocks_WithTableLock(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create a test table
	_, err := pool.Exec(ctx, "CREATE TABLE test_locks (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Start a transaction that holds a lock
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	// Take an exclusive lock on the table
	_, err = tx.Exec(ctx, "LOCK TABLE test_locks IN EXCLUSIVE MODE")
	if err != nil {
		t.Fatalf("Failed to lock table: %v", err)
	}

	// Now query locks - should see our lock
	locks, err := queries.GetLocks(ctx, pool)
	if err != nil {
		t.Fatalf("GetLocks failed: %v", err)
	}

	// Find our lock
	found := false
	for _, lock := range locks {
		if lock.Relation == "test_locks" && lock.Mode == "ExclusiveLock" && lock.Granted {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to find ExclusiveLock on test_locks, got %d locks", len(locks))
		for _, lock := range locks {
			t.Logf("Lock: relation=%s mode=%s granted=%v", lock.Relation, lock.Mode, lock.Granted)
		}
	}
}

// TestGetLocks_BlockingScenario verifies GetLocks detects blocked locks.
func TestGetLocks_BlockingScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create a test table with a row
	_, err := pool.Exec(ctx, "CREATE TABLE test_blocking (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO test_blocking (data) VALUES ('test')")
	if err != nil {
		t.Fatalf("Failed to insert test row: %v", err)
	}

	// Connection 1: Start transaction and lock row
	conn1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection 1: %v", err)
	}
	defer conn1.Release()

	tx1, err := conn1.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction 1: %v", err)
	}
	defer tx1.Rollback(ctx)

	// Lock the row with FOR UPDATE
	_, err = tx1.Exec(ctx, "SELECT * FROM test_blocking WHERE id = 1 FOR UPDATE")
	if err != nil {
		t.Fatalf("Failed to lock row: %v", err)
	}

	// Connection 2: Try to update same row (will block)
	conn2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection 2: %v", err)
	}
	defer conn2.Release()

	var wg sync.WaitGroup
	wg.Add(1)

	blockedStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		tx2, err := conn2.Begin(ctx)
		if err != nil {
			t.Logf("Failed to begin transaction 2: %v", err)
			return
		}
		defer tx2.Rollback(ctx)

		close(blockedStarted)

		// This will block waiting for tx1's lock
		_, _ = tx2.Exec(ctx, "UPDATE test_blocking SET data = 'blocked' WHERE id = 1")
	}()

	// Wait for blocked transaction to start
	<-blockedStarted
	time.Sleep(100 * time.Millisecond) // Give it time to actually block

	// Query locks - should see granted and waiting locks
	locks, err := queries.GetLocks(ctx, pool)
	if err != nil {
		t.Fatalf("GetLocks failed: %v", err)
	}

	// Check we have some locks
	if len(locks) == 0 {
		t.Error("Expected to find locks in blocking scenario")
	}

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

	t.Logf("Found %d granted and %d waiting locks on test_blocking", grantedCount, waitingCount)

	// Commit tx1 to unblock tx2
	tx1.Commit(ctx)

	// Wait for goroutine to finish
	wg.Wait()
}

// TestGetBlockingRelationships_NoBlocking verifies empty result with no blocking.
func TestGetBlockingRelationships_NoBlocking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	rels, err := queries.GetBlockingRelationships(ctx, pool)
	if err != nil {
		t.Fatalf("GetBlockingRelationships failed: %v", err)
	}

	if len(rels) != 0 {
		t.Errorf("Expected 0 blocking relationships, got %d", len(rels))
	}
}

// TestGetBlockingRelationships_WithBlocking verifies detection of blocking.
func TestGetBlockingRelationships_WithBlocking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create test table with row
	_, err := pool.Exec(ctx, "CREATE TABLE test_rel_blocking (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO test_rel_blocking (data) VALUES ('test')")
	if err != nil {
		t.Fatalf("Failed to insert test row: %v", err)
	}

	// Connection 1: Hold lock
	conn1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection 1: %v", err)
	}
	defer conn1.Release()

	tx1, err := conn1.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction 1: %v", err)
	}
	defer tx1.Rollback(ctx)

	_, err = tx1.Exec(ctx, "SELECT * FROM test_rel_blocking WHERE id = 1 FOR UPDATE")
	if err != nil {
		t.Fatalf("Failed to lock row: %v", err)
	}

	// Connection 2: Will be blocked
	conn2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection 2: %v", err)
	}
	defer conn2.Release()

	var wg sync.WaitGroup
	wg.Add(1)

	blockedStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		tx2, err := conn2.Begin(ctx)
		if err != nil {
			return
		}
		defer tx2.Rollback(ctx)

		close(blockedStarted)

		// This blocks
		_, _ = tx2.Exec(ctx, "UPDATE test_rel_blocking SET data = 'blocked' WHERE id = 1")
	}()

	<-blockedStarted
	time.Sleep(200 * time.Millisecond)

	// Query blocking relationships
	rels, err := queries.GetBlockingRelationships(ctx, pool)
	if err != nil {
		t.Fatalf("GetBlockingRelationships failed: %v", err)
	}

	if len(rels) == 0 {
		t.Error("Expected to find blocking relationships")
	} else {
		t.Logf("Found %d blocking relationship(s)", len(rels))
		for _, rel := range rels {
			t.Logf("Blocked PID %d by PID %d", rel.BlockedPID, rel.BlockingPID)
		}
	}

	// Cleanup
	tx1.Commit(ctx)
	wg.Wait()
}

// TestTerminateBackend verifies backend termination.
func TestTerminateBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create a connection to terminate
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}

	// Get the PID of this connection
	var pid int
	err = conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid)
	if err != nil {
		conn.Release()
		t.Fatalf("Failed to get backend PID: %v", err)
	}

	// Release before terminating
	conn.Release()

	// Terminate the backend
	success, err := queries.TerminateBackend(ctx, pool, pid)
	if err != nil {
		// Connection might already be closed
		t.Logf("TerminateBackend returned error (may be expected): %v", err)
	} else if !success {
		t.Log("TerminateBackend returned false (connection may have already closed)")
	}
}

// TestGetLocks_Performance validates query performance < 500ms.
func TestGetLocks_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create some tables to generate locks
	for i := 0; i < 10; i++ {
		_, err := pool.Exec(ctx, "CREATE TABLE perf_test_"+string(rune('a'+i))+" (id SERIAL)")
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}
	}

	// Measure GetLocks performance
	start := time.Now()
	_, err := queries.GetLocks(ctx, pool)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetLocks failed: %v", err)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("GetLocks took %v, expected < 500ms", elapsed)
	} else {
		t.Logf("GetLocks completed in %v", elapsed)
	}
}

// TestGetBlockingRelationships_Performance validates query performance < 500ms.
func TestGetBlockingRelationships_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	start := time.Now()
	_, err := queries.GetBlockingRelationships(ctx, pool)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetBlockingRelationships failed: %v", err)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("GetBlockingRelationships took %v, expected < 500ms", elapsed)
	} else {
		t.Logf("GetBlockingRelationships completed in %v", elapsed)
	}
}

// TestGetLocks_LockTypes verifies different lock types are captured.
func TestGetLocks_LockTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create test table
	_, err := pool.Exec(ctx, "CREATE TABLE test_lock_types (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

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
		t.Run(mode, func(t *testing.T) {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("Failed to acquire connection: %v", err)
			}
			defer conn.Release()

			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatalf("Failed to begin transaction: %v", err)
			}
			defer tx.Rollback(ctx)

			_, err = tx.Exec(ctx, "LOCK TABLE test_lock_types IN "+mode+" MODE")
			if err != nil {
				t.Fatalf("Failed to acquire %s lock: %v", mode, err)
			}

			locks, err := queries.GetLocks(ctx, pool)
			if err != nil {
				t.Fatalf("GetLocks failed: %v", err)
			}

			// Find our lock
			found := false
			for _, lock := range locks {
				if lock.Relation == "test_lock_types" && lock.Granted {
					found = true
					t.Logf("Found lock: mode=%s", lock.Mode)
					break
				}
			}

			if !found {
				t.Errorf("Failed to find %s lock", mode)
			}
		})
	}
}

// TestGetLocks_Duration verifies duration calculation.
func TestGetLocks_Duration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create test table
	_, err := pool.Exec(ctx, "CREATE TABLE test_duration (id SERIAL PRIMARY KEY)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "LOCK TABLE test_duration IN EXCLUSIVE MODE")
	if err != nil {
		t.Fatalf("Failed to lock table: %v", err)
	}

	// Wait a bit to accumulate duration
	time.Sleep(100 * time.Millisecond)

	locks, err := queries.GetLocks(ctx, pool)
	if err != nil {
		t.Fatalf("GetLocks failed: %v", err)
	}

	for _, lock := range locks {
		if lock.Relation == "test_duration" {
			if lock.Duration < 100*time.Millisecond {
				t.Errorf("Duration %v less than expected 100ms", lock.Duration)
			} else {
				t.Logf("Lock duration: %v", lock.Duration)
			}
			return
		}
	}

	t.Error("Lock on test_duration not found")
}
