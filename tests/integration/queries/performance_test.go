package queries_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// TestPerformance_QueryExecution validates that query execution meets <500ms target.
func TestPerformance_QueryExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "perf_test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Insert test data (simulate 1000 unique queries)
	for i := range 1000 {
		fingerprint := uint64(i + 1)
		query := "SELECT * FROM table" + string(rune('A'+i%26)) + " WHERE id = $1"
		duration := float64(i%100) + 1.0
		rows := int64(i % 50)
		if err := store.Upsert(ctx, fingerprint, query, 0, duration, duration, rows, ""); err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}

	// Benchmark GetTopQueries - should complete in <500ms
	t.Run("GetTopQueries", func(t *testing.T) {
		start := time.Now()
		_, err := store.GetTopQueries(ctx, sqlite.SortByTotalTime, false, 100)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("GetTopQueries failed: %v", err)
		}

		if elapsed > 500*time.Millisecond {
			t.Errorf("GetTopQueries exceeded 500ms target: %v", elapsed)
		} else {
			t.Logf("GetTopQueries completed in %v", elapsed)
		}
	})

	// Benchmark SearchQueries - should complete in <500ms
	t.Run("SearchQueries", func(t *testing.T) {
		start := time.Now()
		_, err := store.SearchQueries(ctx, "SELECT", sqlite.SortByTotalTime, false, 100)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("SearchQueries failed: %v", err)
		}

		if elapsed > 500*time.Millisecond {
			t.Errorf("SearchQueries exceeded 500ms target: %v", elapsed)
		} else {
			t.Logf("SearchQueries completed in %v", elapsed)
		}
	})

	// Benchmark Upsert - should complete in <100ms
	t.Run("Upsert", func(t *testing.T) {
		start := time.Now()
		err := store.Upsert(ctx, 9999, "SELECT 1", 0, 10.0, 10.0, 1, "")
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}

		if elapsed > 100*time.Millisecond {
			t.Errorf("Upsert exceeded 100ms target: %v", elapsed)
		} else {
			t.Logf("Upsert completed in %v", elapsed)
		}
	})

	// Benchmark Reset - should complete in <500ms
	t.Run("Reset", func(t *testing.T) {
		start := time.Now()
		err := store.Reset(ctx)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Reset failed: %v", err)
		}

		if elapsed > 500*time.Millisecond {
			t.Errorf("Reset exceeded 500ms target: %v", elapsed)
		} else {
			t.Logf("Reset completed in %v", elapsed)
		}
	})
}

// TestPerformance_MonitorCapture validates monitor capture performance.
func TestPerformance_MonitorCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()

	// Setup PostgreSQL container
	pool := setupPostgres(t, ctx)

	// Setup SQLite storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "monitor_perf.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Create monitor with fast refresh
	config := queries.MonitorConfig{
		RefreshInterval: 50 * time.Millisecond,
		RetentionDays:   7,
	}

	monitor := queries.NewMonitor(pool, store, config)

	// Start monitoring
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := monitor.Start(monitorCtx); err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}

	// Create test table
	_, err = pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS perf_test (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Run 100 queries and measure capture time
	start := time.Now()
	for range 100 {
		_, err = pool.Exec(ctx, "INSERT INTO perf_test (data) VALUES ($1)", "test data")
		if err != nil {
			t.Fatalf("Failed to execute query: %v", err)
		}
	}
	queryTime := time.Since(start)

	// Wait for monitor to capture
	time.Sleep(200 * time.Millisecond)

	// Stop monitor
	if err := monitor.Stop(); err != nil {
		t.Fatalf("Failed to stop monitor: %v", err)
	}

	// Verify capture
	stats, err := store.GetTopQueries(ctx, sqlite.SortByCalls, false, 10)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	t.Logf("Executed 100 queries in %v", queryTime)
	t.Logf("Captured %d unique query patterns", len(stats))

	// Each query should complete in <5ms on average (100 queries in <500ms)
	if queryTime > 500*time.Millisecond {
		t.Errorf("Query execution exceeded 500ms target for 100 queries: %v", queryTime)
	}
}

// BenchmarkQueryStatsStore_GetTopQueries benchmarks the GetTopQueries operation.
func BenchmarkQueryStatsStore_GetTopQueries(b *testing.B) {
	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		b.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Insert test data
	for i := range 1000 {
		fingerprint := uint64(i + 1)
		query := "SELECT * FROM table WHERE id = $1"
		if err := store.Upsert(ctx, fingerprint, query, 0, float64(i), float64(i), int64(i%10), ""); err != nil {
			b.Fatalf("Failed to insert test data: %v", err)
		}
	}

	for b.Loop() {
		_, err := store.GetTopQueries(ctx, sqlite.SortByTotalTime, false, 100)
		if err != nil {
			b.Fatalf("GetTopQueries failed: %v", err)
		}
	}
}

// BenchmarkQueryStatsStore_Upsert benchmarks the Upsert operation.
func BenchmarkQueryStatsStore_Upsert(b *testing.B) {
	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		b.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	for i := 0; b.Loop(); i++ {
		fingerprint := uint64(i % 100)
		query := "SELECT * FROM table WHERE id = $1"
		if err := store.Upsert(ctx, fingerprint, query, 0, float64(i), float64(i), int64(i%10), ""); err != nil {
			b.Fatalf("Upsert failed: %v", err)
		}
	}
}

// BenchmarkQueryStatsStore_SearchQueries benchmarks the SearchQueries operation.
func BenchmarkQueryStatsStore_SearchQueries(b *testing.B) {
	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		b.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Insert test data with varied queries
	for i := range 1000 {
		fingerprint := uint64(i + 1)
		var query string
		switch i % 3 {
		case 0:
			query = "SELECT * FROM users WHERE id = $1"
		case 1:
			query = "INSERT INTO orders (user_id) VALUES ($1)"
		default:
			query = "UPDATE products SET price = $1 WHERE id = $2"
		}
		if err := store.Upsert(ctx, fingerprint, query, 0, float64(i), float64(i), int64(i%10), ""); err != nil {
			b.Fatalf("Failed to insert test data: %v", err)
		}
	}

	for b.Loop() {
		_, err := store.SearchQueries(ctx, "SELECT", sqlite.SortByTotalTime, false, 100)
		if err != nil {
			b.Fatalf("SearchQueries failed: %v", err)
		}
	}
}

// BenchmarkFingerprinter benchmarks the query fingerprinting operation.
func BenchmarkFingerprinter(b *testing.B) {
	fp := queries.NewFingerprinter()

	testQueries := []string{
		"SELECT * FROM users WHERE id = 123",
		"INSERT INTO orders (user_id, product_id, quantity) VALUES (1, 2, 3)",
		"UPDATE products SET price = 99.99, stock = 100 WHERE id = 456",
		"DELETE FROM sessions WHERE expires_at < '2024-01-01'",
		"SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE u.active = true",
	}

	for i := 0; b.Loop(); i++ {
		query := testQueries[i%len(testQueries)]
		_, _, _ = fp.Fingerprint(query)
	}
}
