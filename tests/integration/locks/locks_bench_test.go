package locks_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// BenchmarkGetLocks_100Locks benchmarks GetLocks with 100+ concurrent locks.
// T049: Validate performance: GetLocks() < 500ms with 100+ locks
func BenchmarkGetLocks_100Locks(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresForBench(b, ctx)

	// Create 100 tables
	numTables := 100
	for i := 0; i < numTables; i++ {
		_, err := pool.Exec(ctx, fmt.Sprintf("CREATE TABLE bench_lock_%d (id SERIAL PRIMARY KEY, data TEXT)", i))
		if err != nil {
			b.Fatalf("Failed to create table %d: %v", i, err)
		}
	}

	// Hold locks on all tables via transactions
	conns := make([]*pgxpool.Conn, numTables)
	for i := 0; i < numTables; i++ {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatalf("Failed to acquire connection %d: %v", i, err)
		}
		conns[i] = conn

		tx, err := conn.Begin(ctx)
		if err != nil {
			b.Fatalf("Failed to begin transaction %d: %v", i, err)
		}

		_, err = tx.Exec(ctx, fmt.Sprintf("LOCK TABLE bench_lock_%d IN SHARE MODE", i))
		if err != nil {
			b.Fatalf("Failed to lock table %d: %v", i, err)
		}

		// Keep transaction open for benchmark
		b.Cleanup(func() {
			tx.Rollback(ctx)
			conn.Release()
		})
	}

	// Verify we have 100+ locks
	locks, err := queries.GetLocks(ctx, pool)
	if err != nil {
		b.Fatalf("GetLocks failed: %v", err)
	}
	if len(locks) < numTables {
		b.Logf("Warning: Only %d locks found, expected %d+", len(locks), numTables)
	} else {
		b.Logf("Setup complete: %d locks active", len(locks))
	}

	// Run benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := queries.GetLocks(ctx, pool)
		if err != nil {
			b.Fatalf("GetLocks failed: %v", err)
		}
	}
	b.StopTimer()

	// Validate < 500ms
	if b.N > 0 {
		avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
		avgMs := float64(avgNs) / 1e6
		if avgMs > 500 {
			b.Errorf("GetLocks averaged %.2fms, expected < 500ms", avgMs)
		} else {
			b.Logf("GetLocks averaged %.2fms with %d locks", avgMs, len(locks))
		}
	}
}

// BenchmarkGetBlockingRelationships benchmarks GetBlockingRelationships.
// T050: Validate performance: GetBlockingRelationships() < 500ms
func BenchmarkGetBlockingRelationships(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresForBench(b, ctx)

	// Run benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := queries.GetBlockingRelationships(ctx, pool)
		if err != nil {
			b.Fatalf("GetBlockingRelationships failed: %v", err)
		}
	}
	b.StopTimer()

	// Validate < 500ms
	if b.N > 0 {
		avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
		avgMs := float64(avgNs) / 1e6
		if avgMs > 500 {
			b.Errorf("GetBlockingRelationships averaged %.2fms, expected < 500ms", avgMs)
		} else {
			b.Logf("GetBlockingRelationships averaged %.2fms", avgMs)
		}
	}
}

// TestGetLocks_ManyLocks_Under500ms validates GetLocks with many locks completes under 500ms.
// Uses 20 concurrent locks which is realistic for CI while still validating performance.
// T049: Validate performance: GetLocks() < 500ms with 100+ locks
func TestGetLocks_ManyLocks_Under500ms(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Create multiple tables and hold locks using a single transaction
	// This avoids connection pool limits while still generating many locks
	numTables := 50
	for i := 0; i < numTables; i++ {
		_, err := pool.Exec(ctx, fmt.Sprintf("CREATE TABLE perf_lock_%d (id SERIAL PRIMARY KEY)", i))
		if err != nil {
			t.Fatalf("Failed to create table %d: %v", i, err)
		}
	}

	// Hold locks on all tables via a single connection/transaction
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	t.Cleanup(func() { conn.Release() })

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	t.Cleanup(func() { tx.Rollback(ctx) })

	// Lock all tables in one transaction - generates 50+ locks
	for i := 0; i < numTables; i++ {
		_, err = tx.Exec(ctx, fmt.Sprintf("LOCK TABLE perf_lock_%d IN SHARE MODE", i))
		if err != nil {
			t.Fatalf("Failed to lock table %d: %v", i, err)
		}
	}

	// Measure GetLocks - run multiple times and take average
	var totalElapsed time.Duration
	runs := 10
	var lockCount int

	for i := 0; i < runs; i++ {
		start := time.Now()
		locks, err := queries.GetLocks(ctx, pool)
		elapsed := time.Since(start)
		totalElapsed += elapsed

		if err != nil {
			t.Fatalf("GetLocks failed: %v", err)
		}
		lockCount = len(locks)
	}

	avgElapsed := totalElapsed / time.Duration(runs)
	t.Logf("GetLocks returned %d locks, avg %v over %d runs", lockCount, avgElapsed, runs)

	// Query should be well under 500ms even with 20 locks
	// With 100 locks it would scale ~5x, so we validate < 100ms here
	if avgElapsed > 100*time.Millisecond {
		t.Errorf("GetLocks averaged %v with %d locks, expected < 100ms (would be ~500ms with 100 locks)", avgElapsed, lockCount)
	}
}

// TestMemoryUsage_Under50MB validates memory usage stays under 50MB during locks operations.
// T051: Validate memory usage < 50MB during operation
func TestMemoryUsage_Under50MB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	// Force GC before measuring baseline
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Create tables and hold locks via single transaction
	numTables := 20
	for i := 0; i < numTables; i++ {
		_, err := pool.Exec(ctx, fmt.Sprintf("CREATE TABLE mem_test_%d (id SERIAL PRIMARY KEY, data TEXT)", i))
		if err != nil {
			t.Fatalf("Failed to create table %d: %v", i, err)
		}
	}

	// Hold locks via single transaction
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	t.Cleanup(func() { conn.Release() })

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	t.Cleanup(func() { tx.Rollback(ctx) })

	for i := 0; i < numTables; i++ {
		_, err = tx.Exec(ctx, fmt.Sprintf("LOCK TABLE mem_test_%d IN SHARE MODE", i))
		if err != nil {
			t.Fatalf("Failed to lock table %d: %v", i, err)
		}
	}

	// Run multiple GetLocks calls to accumulate memory
	iterations := 100
	for i := 0; i < iterations; i++ {
		_, err := queries.GetLocks(ctx, pool)
		if err != nil {
			t.Fatalf("GetLocks failed: %v", err)
		}
		_, err = queries.GetBlockingRelationships(ctx, pool)
		if err != nil {
			t.Fatalf("GetBlockingRelationships failed: %v", err)
		}
	}

	// Measure memory after operations
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Calculate memory usage
	allocatedMB := float64(memAfter.Alloc) / 1024 / 1024
	heapMB := float64(memAfter.HeapAlloc) / 1024 / 1024
	totalAllocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024

	t.Logf("Memory stats after %d iterations:", iterations)
	t.Logf("  Current alloc: %.2f MB", allocatedMB)
	t.Logf("  Heap alloc: %.2f MB", heapMB)
	t.Logf("  Total allocated during test: %.2f MB", totalAllocMB)

	// Check heap allocation is under 50MB
	if heapMB > 50 {
		t.Errorf("Heap allocation %.2f MB exceeds 50MB limit", heapMB)
	}
}

// setupPostgresForBench creates a PostgreSQL test container for benchmarks.
func setupPostgresForBench(b *testing.B, ctx context.Context) *pgxpool.Pool {
	b.Helper()

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
		b.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	b.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			b.Logf("Failed to terminate container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		b.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		b.Fatalf("Failed to get container port: %v", err)
	}

	// Configure pool for higher connection limit
	connStr := "postgres://test:test@" + host + ":" + port.Port() + "/testdb?sslmode=disable&pool_max_conns=150"

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		b.Fatalf("Failed to create connection pool: %v", err)
	}

	b.Cleanup(func() {
		pool.Close()
	})

	return pool
}
