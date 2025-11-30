// Package integration provides integration tests that verify component interactions.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/metrics"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// TestMetricsStore_PersistenceAcrossRestarts verifies that data persists
// across database close/reopen cycles.
func TestMetricsStore_PersistenceAcrossRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "metrics_persist_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	now := time.Now()

	// First session: write data
	{
		db, err := sqlite.Open(dbPath)
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		store := sqlite.NewMetricsStore(db)

		// Write TPS data
		for i := 0; i < 100; i++ {
			dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(100+i))
			if err := store.SaveDataPoint(ctx, "tps", "", dp); err != nil {
				t.Fatalf("SaveDataPoint failed: %v", err)
			}
		}

		// Write table size data with keys
		tables := []string{"public.users", "public.orders", "public.products"}
		for _, table := range tables {
			for i := 0; i < 10; i++ {
				dp := metrics.NewDataPointAt(now.Add(time.Duration(i)*time.Hour), float64((i+1)*1024))
				if err := store.SaveDataPoint(ctx, "table_size", table, dp); err != nil {
					t.Fatalf("SaveDataPoint failed for %s: %v", table, err)
				}
			}
		}

		db.Close()
	}

	// Second session: verify data persisted
	{
		db, err := sqlite.Open(dbPath)
		if err != nil {
			t.Fatalf("failed to reopen database: %v", err)
		}
		defer db.Close()

		store := sqlite.NewMetricsStore(db)

		// Verify TPS data count
		count, err := store.Count(ctx, "tps", "")
		if err != nil {
			t.Fatalf("Count failed: %v", err)
		}
		if count != 100 {
			t.Errorf("expected 100 TPS points, got %d", count)
		}

		// Verify table size data
		tables := []string{"public.users", "public.orders", "public.products"}
		for _, table := range tables {
			count, err := store.Count(ctx, "table_size", table)
			if err != nil {
				t.Fatalf("Count failed for %s: %v", table, err)
			}
			if count != 10 {
				t.Errorf("expected 10 points for %s, got %d", table, count)
			}

			// Verify latest value
			latest, ok, err := store.GetLatest(ctx, "table_size", table)
			if err != nil {
				t.Fatalf("GetLatest failed: %v", err)
			}
			if !ok {
				t.Errorf("expected data for %s", table)
			}
			if latest.Value != 10240 { // (10)*1024
				t.Errorf("expected latest value 10240 for %s, got %f", table, latest.Value)
			}
		}
	}
}

// TestMetricsStore_ConcurrentWritesAndReads tests thread safety under load.
func TestMetricsStore_ConcurrentWritesAndReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "metrics_concurrent_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 5 writer goroutines
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			metric := "concurrent_metric"
			for i := 0; i < 100; i++ {
				dp := metrics.NewDataPointAt(now.Add(time.Duration(workerID*100+i)*time.Millisecond), float64(workerID*100+i))
				if err := store.SaveDataPoint(ctx, metric, "", dp); err != nil {
					errors <- err
					return
				}
			}
		}(w)
	}

	// 3 reader goroutines
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := store.GetHistory(ctx, "concurrent_metric", "", now.Add(-time.Hour), 1000); err != nil {
					errors <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify total count
	count, err := store.Count(ctx, "concurrent_metric", "")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	expected := int64(5 * 100) // 5 workers * 100 points each
	if count != expected {
		t.Errorf("expected %d points, got %d", expected, count)
	}
}

// TestMetricsStore_HeatmapAggregation tests the hourly aggregation for heatmaps.
func TestMetricsStore_HeatmapAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "metrics_heatmap_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewMetricsStore(db)
	ctx := context.Background()

	// Insert data across multiple days and hours
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local) // Monday

	// Add TPS data for each hour across a week
	for day := 0; day < 7; day++ {
		for hour := 0; hour < 24; hour++ {
			ts := baseTime.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)
			// Value varies by day and hour for testing
			value := float64(day*100 + hour*10)
			dp := metrics.NewDataPointAt(ts, value)
			if err := store.SaveDataPoint(ctx, "tps", "", dp); err != nil {
				t.Fatalf("SaveDataPoint failed: %v", err)
			}
		}
	}

	// Get heatmap matrix
	since := baseTime.Add(-time.Hour)
	matrix, minVal, maxVal, err := store.GetHourlyAggregatesMatrix(ctx, "tps", "", since)
	if err != nil {
		t.Fatalf("GetHourlyAggregatesMatrix failed: %v", err)
	}

	// Verify we have data
	dataCount := 0
	for day := 0; day < 7; day++ {
		for hour := 0; hour < 24; hour++ {
			if matrix[day][hour] >= 0 {
				dataCount++
			}
		}
	}

	if dataCount == 0 {
		t.Error("expected some heatmap data")
	}

	// Verify min/max are reasonable
	if minVal < 0 {
		t.Errorf("expected minVal >= 0, got %f", minVal)
	}
	if maxVal <= minVal {
		t.Errorf("expected maxVal > minVal, got max=%f min=%f", maxVal, minVal)
	}
}

// TestMetricsStore_LargeDataset tests performance with a large dataset.
func TestMetricsStore_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "metrics_large_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now()

	// Insert 10,000 points using batch
	const batchSize = 100
	const totalPoints = 10000

	start := time.Now()

	for batch := 0; batch < totalPoints/batchSize; batch++ {
		points := make([]metrics.DataPoint, batchSize)
		for i := 0; i < batchSize; i++ {
			idx := batch*batchSize + i
			points[i] = metrics.NewDataPointAt(now.Add(time.Duration(idx)*time.Second), float64(idx))
		}
		if err := store.SaveBatch(ctx, "large_metric", "", points); err != nil {
			t.Fatalf("SaveBatch failed: %v", err)
		}
	}

	insertDuration := time.Since(start)
	t.Logf("Inserted %d points in %v (%.2f points/sec)", totalPoints, insertDuration, float64(totalPoints)/insertDuration.Seconds())

	// Verify count
	count, err := store.Count(ctx, "large_metric", "")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != totalPoints {
		t.Errorf("expected %d points, got %d", totalPoints, count)
	}

	// Test query performance
	start = time.Now()
	points, err := store.GetHistory(ctx, "large_metric", "", now.Add(-time.Hour), 1000)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	queryDuration := time.Since(start)
	t.Logf("Retrieved %d points in %v", len(points), queryDuration)

	// Query should complete in under 500ms
	if queryDuration > 500*time.Millisecond {
		t.Errorf("query took too long: %v (target: <500ms)", queryDuration)
	}
}

// TestMetricsStore_PruneRetention tests data retention and pruning.
func TestMetricsStore_PruneRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "metrics_prune_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	store := sqlite.NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now()

	// Insert data at various ages
	// Note: Prune uses > retention_days (strict inequality), so 7 day old data stays
	ages := []struct {
		daysAgo int
		count   int
	}{
		{0, 10},   // Today - stays
		{1, 10},   // Yesterday - stays
		{6, 20},   // 6 days old - stays (within 7 day window)
		{14, 30},  // Two weeks old - pruned
		{30, 40},  // Month old - pruned
		{60, 50},  // Two months old - pruned
	}

	var totalInserted int64
	for _, age := range ages {
		for i := 0; i < age.count; i++ {
			ts := now.AddDate(0, 0, -age.daysAgo).Add(time.Duration(i) * time.Minute)
			dp := metrics.NewDataPointAt(ts, float64(i))
			if err := store.SaveDataPoint(ctx, "retention_metric", "", dp); err != nil {
				t.Fatalf("SaveDataPoint failed: %v", err)
			}
			totalInserted++
		}
	}

	// Verify total count
	count, err := store.Count(ctx, "retention_metric", "")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != totalInserted {
		t.Errorf("expected %d points, got %d", totalInserted, count)
	}

	// Prune with 7 day retention (should remove 14, 30, 60 day old data)
	deleted, err := store.Prune(ctx, 7)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	expectedDeleted := int64(30 + 40 + 50) // 14 + 30 + 60 day old data
	if deleted != expectedDeleted {
		t.Errorf("expected %d deleted, got %d", expectedDeleted, deleted)
	}

	// Verify remaining count
	remaining, err := store.Count(ctx, "retention_metric", "")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	expectedRemaining := int64(10 + 10 + 20) // 0 + 1 + 6 day old data
	if remaining != expectedRemaining {
		t.Errorf("expected %d remaining, got %d", expectedRemaining, remaining)
	}
}
