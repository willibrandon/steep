package queries_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

func TestQueryStats_MeanTimeMs(t *testing.T) {
	tests := []struct {
		name        string
		calls       int64
		totalTimeMs float64
		want        float64
	}{
		{
			name:        "normal calculation",
			calls:       10,
			totalTimeMs: 1000,
			want:        100,
		},
		{
			name:        "zero calls",
			calls:       0,
			totalTimeMs: 100,
			want:        0,
		},
		{
			name:        "single call",
			calls:       1,
			totalTimeMs: 50.5,
			want:        50.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stat := sqlite.QueryStats{
				Calls:       tt.calls,
				TotalTimeMs: tt.totalTimeMs,
			}
			got := stat.MeanTimeMs()
			if got != tt.want {
				t.Errorf("MeanTimeMs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueryStatsStore_Upsert(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// First insert (calls=0 triggers increment behavior)
	err = store.Upsert(ctx, 12345, "SELECT * FROM users WHERE id = $1", 0, 100.0, 100.0, 1, "", "")
	if err != nil {
		t.Fatalf("First Upsert failed: %v", err)
	}

	// Verify
	stat, err := store.GetByFingerprint(ctx, 12345)
	if err != nil {
		t.Fatalf("GetByFingerprint failed: %v", err)
	}
	if stat == nil {
		t.Fatal("Expected stat, got nil")
	}
	if stat.Calls != 1 {
		t.Errorf("Calls = %d, want 1", stat.Calls)
	}
	if stat.TotalTimeMs != 100.0 {
		t.Errorf("TotalTimeMs = %f, want 100.0", stat.TotalTimeMs)
	}

	// Second insert (should update)
	err = store.Upsert(ctx, 12345, "SELECT * FROM users WHERE id = $1", 0, 200.0, 200.0, 5, "", "")
	if err != nil {
		t.Fatalf("Second Upsert failed: %v", err)
	}

	// Verify aggregation
	stat, err = store.GetByFingerprint(ctx, 12345)
	if err != nil {
		t.Fatalf("GetByFingerprint failed: %v", err)
	}
	if stat.Calls != 2 {
		t.Errorf("Calls = %d, want 2", stat.Calls)
	}
	if stat.TotalTimeMs != 300.0 {
		t.Errorf("TotalTimeMs = %f, want 300.0", stat.TotalTimeMs)
	}
	if stat.TotalRows != 6 {
		t.Errorf("TotalRows = %d, want 6", stat.TotalRows)
	}
}

func TestQueryStatsStore_GetTopQueries(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert test data
	testData := []struct {
		fingerprint uint64
		query       string
		duration    float64
		rows        int64
	}{
		{1, "SELECT * FROM a", 100, 10},
		{2, "SELECT * FROM b", 300, 5},
		{3, "SELECT * FROM c", 200, 20},
	}

	for _, d := range testData {
		if err := store.Upsert(ctx, d.fingerprint, d.query, 0, d.duration, d.duration, d.rows, "", ""); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Test sort by total time
	stats, err := store.GetTopQueries(ctx, sqlite.SortByTotalTime, false, 10)
	if err != nil {
		t.Fatalf("GetTopQueries failed: %v", err)
	}
	if len(stats) != 3 {
		t.Fatalf("Expected 3 stats, got %d", len(stats))
	}
	if stats[0].TotalTimeMs != 300 {
		t.Errorf("First by time should have 300ms, got %f", stats[0].TotalTimeMs)
	}

	// Test sort by rows
	stats, err = store.GetTopQueries(ctx, sqlite.SortByRows, false, 10)
	if err != nil {
		t.Fatalf("GetTopQueries failed: %v", err)
	}
	if stats[0].TotalRows != 20 {
		t.Errorf("First by rows should have 20, got %d", stats[0].TotalRows)
	}
}

func TestQueryStatsStore_Reset(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert data
	if err := store.Upsert(ctx, 1, "SELECT 1", 0, 100, 100, 1, "", ""); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify data exists
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}

	// Reset
	if err := store.Reset(ctx); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify data cleared
	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Count after reset = %d, want 0", count)
	}
}

func TestQueryStatsStore_SearchQueries(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert test data
	testData := []struct {
		fingerprint uint64
		query       string
	}{
		{1, "SELECT * FROM users"},
		{2, "INSERT INTO users VALUES ($1)"},
		{3, "SELECT * FROM orders"},
		{4, "DELETE FROM users WHERE id = $1"},
	}

	for _, d := range testData {
		if err := store.Upsert(ctx, d.fingerprint, d.query, 0, 100, 100, 1, "", ""); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Search for "users"
	stats, err := store.SearchQueries(ctx, "users", sqlite.SortByTotalTime, false, 10)
	if err != nil {
		t.Fatalf("SearchQueries failed: %v", err)
	}
	if len(stats) != 3 {
		t.Errorf("Expected 3 results for 'users', got %d", len(stats))
	}

	// Search for SELECT
	stats, err = store.SearchQueries(ctx, "^SELECT", sqlite.SortByTotalTime, false, 10)
	if err != nil {
		t.Fatalf("SearchQueries failed: %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("Expected 2 results for '^SELECT', got %d", len(stats))
	}
}

func TestSQLiteDatabase_OpenClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open database
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}

	// Close database
	if err := db.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
