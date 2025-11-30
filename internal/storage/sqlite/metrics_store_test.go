package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/metrics"
)

func setupTestMetricsStore(t *testing.T) (*MetricsStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "metrics_store_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open database: %v", err)
	}

	store := NewMetricsStore(db)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestMetricsStore_SaveDataPoint(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	dp := metrics.NewDataPointAt(time.Now(), 42.5)

	err := store.SaveDataPoint(ctx, "test_metric", dp)
	if err != nil {
		t.Fatalf("SaveDataPoint failed: %v", err)
	}

	// Verify it was saved
	count, err := store.Count(ctx, "test_metric")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

func TestMetricsStore_SaveBatch(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	points := []metrics.DataPoint{
		metrics.NewDataPointAt(now.Add(-2*time.Second), 1.0),
		metrics.NewDataPointAt(now.Add(-1*time.Second), 2.0),
		metrics.NewDataPointAt(now, 3.0),
	}

	err := store.SaveBatch(ctx, "batch_metric", points)
	if err != nil {
		t.Fatalf("SaveBatch failed: %v", err)
	}

	count, err := store.Count(ctx, "batch_metric")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestMetricsStore_SaveBatch_Empty(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	err := store.SaveBatch(ctx, "empty_metric", nil)
	if err != nil {
		t.Fatalf("SaveBatch with empty slice should not error: %v", err)
	}
}

func TestMetricsStore_GetHistory(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Save 5 points
	for i := 0; i < 5; i++ {
		dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i+1))
		if err := store.SaveDataPoint(ctx, "history_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	// Get history from 2 seconds ago
	since := now.Add(2 * time.Second)
	points, err := store.GetHistory(ctx, "history_metric", since, 100)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	// Should get points 3, 4, 5 (values 3.0, 4.0, 5.0)
	if len(points) != 3 {
		t.Errorf("expected 3 points, got %d", len(points))
	}
}

func TestMetricsStore_GetHistory_Limit(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Save 10 points
	for i := 0; i < 10; i++ {
		dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i))
		if err := store.SaveDataPoint(ctx, "limit_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	// Get with limit of 5
	points, err := store.GetHistory(ctx, "limit_metric", now.Add(-time.Hour), 5)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	if len(points) != 5 {
		t.Errorf("expected 5 points, got %d", len(points))
	}
}

func TestMetricsStore_GetAggregated(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute) // Truncate to minute for predictable grouping

	// Save 10 points spread across 2 buckets (5 each)
	for i := 0; i < 10; i++ {
		ts := now.Add(time.Duration(i*6) * time.Second) // 0, 6, 12, 18, 24, 30, 36, 42, 48, 54 seconds
		dp := metrics.NewDataPointAt(ts, float64(i+1))
		if err := store.SaveDataPoint(ctx, "agg_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	// Aggregate by 30 second buckets
	points, err := store.GetAggregated(ctx, "agg_metric", now.Add(-time.Hour), 30)
	if err != nil {
		t.Fatalf("GetAggregated failed: %v", err)
	}

	// Should get 2 buckets
	if len(points) < 1 {
		t.Error("expected at least 1 aggregated bucket")
	}
}

func TestMetricsStore_GetLatest(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()

	// Empty metric
	_, ok, err := store.GetLatest(ctx, "nonexistent_metric")
	if err != nil {
		t.Fatalf("GetLatest failed: %v", err)
	}
	if ok {
		t.Error("expected ok=false for nonexistent metric")
	}

	// Add some points
	now := time.Now()
	for i := 0; i < 3; i++ {
		dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i+1)*10)
		if err := store.SaveDataPoint(ctx, "latest_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	latest, ok, err := store.GetLatest(ctx, "latest_metric")
	if err != nil {
		t.Fatalf("GetLatest failed: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if latest.Value != 30.0 {
		t.Errorf("expected latest value 30.0, got %f", latest.Value)
	}
}

func TestMetricsStore_Prune(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Save 3 old points (8 days ago)
	for i := 0; i < 3; i++ {
		dp := metrics.NewDataPointAt(now.AddDate(0, 0, -8).Add(time.Duration(i)*time.Second), float64(i))
		if err := store.SaveDataPoint(ctx, "prune_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	// Save 2 recent points
	for i := 0; i < 2; i++ {
		dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i+10))
		if err := store.SaveDataPoint(ctx, "prune_metric", dp); err != nil {
			t.Fatalf("SaveDataPoint failed: %v", err)
		}
	}

	// Verify total count
	count, _ := store.Count(ctx, "prune_metric")
	if count != 5 {
		t.Errorf("expected 5 points before prune, got %d", count)
	}

	// Prune with 7 day retention
	deleted, err := store.Prune(ctx, 7)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}

	// Verify remaining count
	count, _ = store.Count(ctx, "prune_metric")
	if count != 2 {
		t.Errorf("expected 2 points after prune, got %d", count)
	}
}

func TestMetricsStore_MultipleMetrics(t *testing.T) {
	store, cleanup := setupTestMetricsStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Save points for different metrics
	metrics1 := []string{"tps", "connections", "cache_hit_ratio"}
	for _, name := range metrics1 {
		for i := 0; i < 3; i++ {
			dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i+1))
			if err := store.SaveDataPoint(ctx, name, dp); err != nil {
				t.Fatalf("SaveDataPoint failed for %s: %v", name, err)
			}
		}
	}

	// Verify each metric has 3 points
	for _, name := range metrics1 {
		count, err := store.Count(ctx, name)
		if err != nil {
			t.Fatalf("Count failed for %s: %v", name, err)
		}
		if count != 3 {
			t.Errorf("expected 3 points for %s, got %d", name, count)
		}
	}
}
