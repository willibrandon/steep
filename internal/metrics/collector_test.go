package metrics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/logger"
)

func init() {
	// Initialize logger for tests (use LevelWarn to reduce noise)
	logger.InitLogger(logger.LevelWarn, "")
}

func TestCollector_Record(t *testing.T) {
	c := NewCollector()

	c.Record("test_metric", 42.5)

	if !c.HasData("test_metric") {
		t.Error("expected data for test_metric")
	}

	value, ts, ok := c.GetLatest("test_metric")
	if !ok {
		t.Error("expected GetLatest to return true")
	}
	if value != 42.5 {
		t.Errorf("expected value 42.5, got %f", value)
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestCollector_RecordAt(t *testing.T) {
	c := NewCollector()

	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	c.RecordAt("test_metric", ts, 100.0)

	value, gotTs, ok := c.GetLatest("test_metric")
	if !ok {
		t.Error("expected GetLatest to return true")
	}
	if value != 100.0 {
		t.Errorf("expected value 100.0, got %f", value)
	}
	if !gotTs.Equal(ts) {
		t.Errorf("expected timestamp %v, got %v", ts, gotTs)
	}
}

func TestCollector_GetValues(t *testing.T) {
	c := NewCollector()

	// Record some values
	now := time.Now()
	for i := 0; i < 10; i++ {
		c.RecordAt("test_metric", now.Add(time.Duration(i)*time.Second), float64(i+1))
	}

	// Get all values (within 1m window)
	values := c.GetValues("test_metric", TimeWindow1m)
	if len(values) != 10 {
		t.Errorf("expected 10 values, got %d", len(values))
	}

	// Verify order (chronological)
	for i, v := range values {
		expected := float64(i + 1)
		if v != expected {
			t.Errorf("expected value[%d]=%f, got %f", i, expected, v)
		}
	}
}

func TestCollector_GetValues_NonexistentMetric(t *testing.T) {
	c := NewCollector()

	values := c.GetValues("nonexistent", TimeWindow1m)
	if values != nil {
		t.Error("expected nil for nonexistent metric")
	}
}

func TestCollector_GetDataPoints(t *testing.T) {
	c := NewCollector()

	now := time.Now()
	for i := 0; i < 5; i++ {
		c.RecordAt("test_metric", now.Add(time.Duration(i)*time.Second), float64(i*10))
	}

	points := c.GetDataPoints("test_metric", TimeWindow1m)
	if len(points) != 5 {
		t.Errorf("expected 5 points, got %d", len(points))
	}

	// Verify timestamps exist
	for _, p := range points {
		if p.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	}
}

func TestCollector_GetLatest_NoData(t *testing.T) {
	c := NewCollector()

	_, _, ok := c.GetLatest("nonexistent")
	if ok {
		t.Error("expected false for nonexistent metric")
	}
}

func TestCollector_MetricNames(t *testing.T) {
	c := NewCollector()

	c.Record("metric_a", 1.0)
	c.Record("metric_b", 2.0)
	c.Record("metric_c", 3.0)

	names := c.MetricNames()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}

	// Check all names are present
	nameMap := make(map[string]bool)
	for _, n := range names {
		nameMap[n] = true
	}

	for _, expected := range []string{"metric_a", "metric_b", "metric_c"} {
		if !nameMap[expected] {
			t.Errorf("expected metric %s to be present", expected)
		}
	}
}

func TestCollector_HasData(t *testing.T) {
	c := NewCollector()

	if c.HasData("test_metric") {
		t.Error("expected HasData false for empty metric")
	}

	c.Record("test_metric", 1.0)

	if !c.HasData("test_metric") {
		t.Error("expected HasData true after recording")
	}
}

func TestCollector_WithCapacity(t *testing.T) {
	c := NewCollector(WithCapacity(5))

	// Record 10 values
	for i := 0; i < 10; i++ {
		c.Record("test_metric", float64(i))
	}

	values := c.GetValues("test_metric", TimeWindow1m)
	if len(values) != 5 {
		t.Errorf("expected 5 values (capacity), got %d", len(values))
	}

	// Should have last 5 values
	expected := []float64{5, 6, 7, 8, 9}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], v)
		}
	}
}

func TestCollector_Concurrent(t *testing.T) {
	c := NewCollector(WithCapacity(1000))
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Record("concurrent_metric", float64(id*100+j))
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.GetValues("concurrent_metric", TimeWindow1m)
				_, _, _ = c.GetLatest("concurrent_metric")
				_ = c.HasData("concurrent_metric")
			}
		}()
	}

	wg.Wait()

	// Verify we have data
	if !c.HasData("concurrent_metric") {
		t.Error("expected data after concurrent operations")
	}
}

func TestCollector_StartStop(t *testing.T) {
	c := NewCollector()

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Record some data
	c.Record("test_metric", 1.0)

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Should still be able to access data after stop
	if !c.HasData("test_metric") {
		t.Error("expected data after stop")
	}
}

func TestCollector_InvalidValues(t *testing.T) {
	c := NewCollector()

	// These should be silently ignored
	c.RecordAt("test_metric", time.Time{}, 1.0) // Zero timestamp

	if c.HasData("test_metric") {
		t.Error("expected no data for invalid timestamp")
	}
}

func TestCollector_MultipleMetrics(t *testing.T) {
	c := NewCollector()

	c.Record(MetricTPS, 100.0)
	c.Record(MetricConnections, 50.0)
	c.Record(MetricCacheHitRatio, 0.95)

	// Verify each metric
	tests := []struct {
		name  string
		value float64
	}{
		{MetricTPS, 100.0},
		{MetricConnections, 50.0},
		{MetricCacheHitRatio, 0.95},
	}

	for _, tt := range tests {
		v, _, ok := c.GetLatest(tt.name)
		if !ok {
			t.Errorf("expected data for %s", tt.name)
		}
		if v != tt.value {
			t.Errorf("expected %s=%f, got %f", tt.name, tt.value, v)
		}
	}
}
