package queries_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

func TestQueryStatsStore_Cleanup(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert some query stats
	err = store.Upsert(ctx, 1, "SELECT 1", 10.0, 1, "")
	if err != nil {
		t.Fatalf("Failed to insert query 1: %v", err)
	}
	err = store.Upsert(ctx, 2, "SELECT 2", 20.0, 2, "")
	if err != nil {
		t.Fatalf("Failed to insert query 2: %v", err)
	}
	err = store.Upsert(ctx, 3, "SELECT 3", 30.0, 3, "")
	if err != nil {
		t.Fatalf("Failed to insert query 3: %v", err)
	}

	// Verify all 3 records exist
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 records, got %d", count)
	}

	// Fudge last_seen for records 1 and 2 to be older than 7 days
	conn := db.Conn()
	oldDate := time.Now().Add(-8 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err = conn.ExecContext(ctx, "UPDATE query_stats SET last_seen = ? WHERE fingerprint IN (1, 2)", oldDate)
	if err != nil {
		t.Fatalf("Failed to update last_seen: %v", err)
	}

	// Run cleanup with 7 day retention
	retention := 7 * 24 * time.Hour
	deleted, err := store.Cleanup(ctx, retention)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Should have deleted 2 records
	if deleted != 2 {
		t.Errorf("Expected 2 deleted records, got %d", deleted)
	}

	// Should have 1 record remaining
	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count after cleanup: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record remaining, got %d", count)
	}

	// Verify it's the right record (fingerprint 3)
	stat, err := store.GetByFingerprint(ctx, 3)
	if err != nil {
		t.Fatalf("Failed to get fingerprint 3: %v", err)
	}
	if stat == nil {
		t.Error("Expected fingerprint 3 to still exist")
	}

	// Verify old records are gone
	stat, err = store.GetByFingerprint(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to check fingerprint 1: %v", err)
	}
	if stat != nil {
		t.Error("Expected fingerprint 1 to be deleted")
	}

	stat, err = store.GetByFingerprint(ctx, 2)
	if err != nil {
		t.Fatalf("Failed to check fingerprint 2: %v", err)
	}
	if stat != nil {
		t.Error("Expected fingerprint 2 to be deleted")
	}
}

func TestQueryStatsStore_Cleanup_NoOldRecords(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert fresh records
	err = store.Upsert(ctx, 1, "SELECT 1", 10.0, 1, "")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Run cleanup - should delete nothing
	retention := 7 * 24 * time.Hour
	deleted, err := store.Cleanup(ctx, retention)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	if deleted != 0 {
		t.Errorf("Expected 0 deleted records, got %d", deleted)
	}

	// Record should still exist
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}
}

func TestQueryStatsStore_Cleanup_EmptyDatabase(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Run cleanup on empty database - should not error
	retention := 7 * 24 * time.Hour
	deleted, err := store.Cleanup(ctx, retention)
	if err != nil {
		t.Fatalf("Cleanup failed on empty database: %v", err)
	}

	if deleted != 0 {
		t.Errorf("Expected 0 deleted records, got %d", deleted)
	}
}

func TestQueryStatsStore_Cleanup_BoundaryCondition(t *testing.T) {
	// Test that records exactly at the boundary are handled correctly
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewQueryStatsStore(db)
	ctx := context.Background()

	// Insert record
	err = store.Upsert(ctx, 1, "SELECT 1", 10.0, 1, "")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Set last_seen to exactly 7 days ago (should NOT be deleted - we delete < not <=)
	conn := db.Conn()
	exactlySevenDays := time.Now().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err = conn.ExecContext(ctx, "UPDATE query_stats SET last_seen = ? WHERE fingerprint = 1", exactlySevenDays)
	if err != nil {
		t.Fatalf("Failed to update last_seen: %v", err)
	}

	// Run cleanup with 7 day retention
	retention := 7 * 24 * time.Hour
	deleted, err := store.Cleanup(ctx, retention)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Record at exactly 7 days should NOT be deleted (< not <=)
	if deleted != 0 {
		t.Errorf("Expected 0 deleted (boundary case), got %d", deleted)
	}
}
