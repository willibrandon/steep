package queries_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// =============================================================================
// Query Storage Test Suite - shares a single container across PostgreSQL tests
// =============================================================================

type QueryStorageTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestQueryStorageSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(QueryStorageTestSuite))
}

func (s *QueryStorageTestSuite) SetupSuite() {
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
	pool, err := pgxpool.New(s.ctx, connStr)
	s.Require().NoError(err)
	s.pool = pool

	s.T().Log("QueryStorageTestSuite: Shared container ready")
}

func (s *QueryStorageTestSuite) TearDownSuite() {
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

func (s *QueryStorageTestSuite) SetupTest() {
	// Clean up test tables
	_, _ = s.pool.Exec(s.ctx, "DROP TABLE IF EXISTS test_users CASCADE")
}

// =============================================================================
// PostgreSQL-dependent tests
// =============================================================================

func (s *QueryStorageTestSuite) TestQueryMonitor_WithSamplingCollector() {
	// Setup SQLite storage
	tmpDir := s.T().TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	s.Require().NoError(err, "Failed to open SQLite database")
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Create monitor
	config := queries.MonitorConfig{
		RefreshInterval: 100 * time.Millisecond,
		RetentionDays:   7,
	}

	monitor := queries.NewMonitor(s.pool, store, config)

	// Start monitoring
	monitorCtx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	err = monitor.Start(monitorCtx)
	s.Require().NoError(err, "Failed to start monitor")

	// Verify monitor is running
	s.Assert().Equal(queries.MonitorStatusRunning, monitor.Status(), "Expected monitor status Running")
	s.Assert().Equal(queries.DataSourceSampling, monitor.DataSource(), "Expected data source Sampling")

	// Run some queries in PostgreSQL
	_, err = s.pool.Exec(s.ctx, "SELECT pg_sleep(0.1)")
	s.Require().NoError(err, "Failed to execute test query")

	// Create a table and run some queries
	_, err = s.pool.Exec(s.ctx, "CREATE TABLE IF NOT EXISTS test_users (id SERIAL PRIMARY KEY, name TEXT)")
	s.Require().NoError(err, "Failed to create test table")

	for i := 0; i < 5; i++ {
		_, err = s.pool.Exec(s.ctx, "INSERT INTO test_users (name) VALUES ($1)", "user"+string(rune('0'+i)))
		s.Require().NoError(err, "Failed to insert test data")
	}

	// Give the monitor time to capture the queries
	time.Sleep(500 * time.Millisecond)

	// Stop monitor
	err = monitor.Stop()
	s.Require().NoError(err, "Failed to stop monitor")

	s.Assert().Equal(queries.MonitorStatusStopped, monitor.Status(), "Expected monitor status Stopped")
}

// =============================================================================
// Standalone tests (SQLite only, no PostgreSQL container needed)
// =============================================================================

func TestIntegration_QueryStatsStore_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Test full workflow: Insert -> Update -> Query -> Search -> Cleanup

	// 1. Insert multiple queries
	testQueries := []struct {
		fingerprint uint64
		query       string
		duration    float64
		rows        int64
	}{
		{100, "SELECT * FROM users WHERE id = $1", 50.0, 1},
		{200, "INSERT INTO users (name) VALUES ($1)", 25.0, 1},
		{300, "UPDATE users SET name = $1 WHERE id = $2", 75.0, 1},
		{400, "DELETE FROM users WHERE id = $1", 30.0, 1},
		{500, "SELECT * FROM orders WHERE user_id = $1", 100.0, 10},
	}

	for _, q := range testQueries {
		if err := store.Upsert(ctx, q.fingerprint, q.query, 0, q.duration, q.duration, q.rows, "", ""); err != nil {
			t.Fatalf("Upsert failed for fingerprint %d: %v", q.fingerprint, err)
		}
	}

	// 2. Verify count
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 queries, got %d", count)
	}

	// 3. Update existing query (should aggregate)
	if err := store.Upsert(ctx, 100, "SELECT * FROM users WHERE id = $1", 0, 100.0, 100.0, 5, "", ""); err != nil {
		t.Fatalf("Second Upsert failed: %v", err)
	}

	stat, err := store.GetByFingerprint(ctx, 100)
	if err != nil {
		t.Fatalf("GetByFingerprint failed: %v", err)
	}
	if stat.Calls != 2 {
		t.Errorf("Expected 2 calls, got %d", stat.Calls)
	}
	if stat.TotalTimeMs != 150.0 {
		t.Errorf("Expected 150.0ms total time, got %f", stat.TotalTimeMs)
	}
	if stat.TotalRows != 6 {
		t.Errorf("Expected 6 total rows, got %d", stat.TotalRows)
	}

	// 4. Test sorting
	stats, err := store.GetTopQueries(ctx, sqlite.SortByTotalTime, false, 10)
	if err != nil {
		t.Fatalf("GetTopQueries by time failed: %v", err)
	}
	if len(stats) != 5 {
		t.Fatalf("Expected 5 results, got %d", len(stats))
	}
	// First should be fingerprint 100 with 150ms total
	if stats[0].Fingerprint != 100 {
		t.Errorf("Expected first by time to be fingerprint 100, got %d", stats[0].Fingerprint)
	}

	// 5. Test search
	stats, err = store.SearchQueries(ctx, "users", sqlite.SortByTotalTime, false, 10)
	if err != nil {
		t.Fatalf("SearchQueries failed: %v", err)
	}
	if len(stats) != 4 {
		t.Errorf("Expected 4 results for 'users', got %d", len(stats))
	}

	// 6. Test limit
	stats, err = store.GetTopQueries(ctx, sqlite.SortByTotalTime, false, 2)
	if err != nil {
		t.Fatalf("GetTopQueries with limit failed: %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("Expected 2 results with limit, got %d", len(stats))
	}

	// 7. Test cleanup (all should be recent, so nothing deleted)
	deleted, err := store.Cleanup(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted (all recent), got %d", deleted)
	}

	// 8. Test reset
	if err := store.Reset(ctx); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("Count after reset failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 after reset, got %d", count)
	}
}

func TestIntegration_Fingerprinter_RealQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	fp := queries.NewFingerprinter()

	// Test with realistic PostgreSQL queries
	realQueries := []struct {
		name           string
		query          string
		wantNormalized string
	}{
		{
			name:           "complex select with joins",
			query:          "SELECT u.id, u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE o.total > 100.50",
			wantNormalized: "SELECT u.id, u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE o.total > $1",
		},
		{
			name:           "insert with returning",
			query:          "INSERT INTO users (name, email) VALUES ('John', 'john@example.com') RETURNING id",
			wantNormalized: "INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id",
		},
		{
			name:           "update with multiple conditions",
			query:          "UPDATE orders SET status = 'shipped', shipped_at = '2024-01-15' WHERE id = 123 AND status = 'pending'",
			wantNormalized: "UPDATE orders SET status = $1, shipped_at = $2 WHERE id = $3 AND status = $4",
		},
		{
			name:           "delete with subquery",
			query:          "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE last_login < '2023-01-01')",
			wantNormalized: "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE last_login < $1)",
		},
	}

	for _, tc := range realQueries {
		t.Run(tc.name, func(t *testing.T) {
			fingerprint, normalized, err := fp.Fingerprint(tc.query)
			if err != nil {
				t.Fatalf("Fingerprint failed: %v", err)
			}

			if fingerprint == 0 {
				t.Error("Expected non-zero fingerprint")
			}

			if normalized != tc.wantNormalized {
				t.Errorf("Normalized mismatch:\ngot:  %s\nwant: %s", normalized, tc.wantNormalized)
			}
		})
	}
}

// setupPostgres creates a PostgreSQL container for standalone tests that need PostgreSQL.
// This is used by performance_test.go and other tests that don't use the suite pattern.
func setupPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

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

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	connStr := fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, port.Port())
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func TestIntegration_ConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup SQLite storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)

	// Run concurrent upserts
	done := make(chan bool)
	numGoroutines := 10
	numOperations := 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				fingerprint := uint64(j % 10) // 10 unique fingerprints
				query := "SELECT * FROM table" + string(rune('0'+j%10))
				duration := float64(id*10 + j)
				rows := int64(1)

				if err := store.Upsert(ctx, fingerprint, query, 0, duration, duration, rows, "", ""); err != nil {
					t.Errorf("Concurrent Upsert failed: %v", err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify data integrity
	stats, err := store.GetTopQueries(ctx, sqlite.SortByCalls, false, 100)
	if err != nil {
		t.Fatalf("GetTopQueries failed: %v", err)
	}

	// Should have 10 unique fingerprints
	if len(stats) != 10 {
		t.Errorf("Expected 10 unique queries, got %d", len(stats))
	}

	// Each should have numGoroutines * numOperations / 10 calls
	expectedCalls := int64(numGoroutines * numOperations / 10)
	for _, stat := range stats {
		if stat.Calls != expectedCalls {
			t.Errorf("Fingerprint %d: expected %d calls, got %d", stat.Fingerprint, expectedCalls, stat.Calls)
		}
	}
}
