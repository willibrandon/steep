package metrics

import (
	"testing"
	"time"
)

func TestNewConnectionMetrics(t *testing.T) {
	cm := NewConnectionMetrics()
	if cm == nil {
		t.Fatal("NewConnectionMetrics returned nil")
	}
	if cm.Len() != 0 {
		t.Errorf("expected empty metrics, got %d", cm.Len())
	}
	if cm.maxConns != DefaultMaxConnections {
		t.Errorf("expected maxConns %d, got %d", DefaultMaxConnections, cm.maxConns)
	}
	if cm.historySize != DefaultConnectionHistorySize {
		t.Errorf("expected historySize %d, got %d", DefaultConnectionHistorySize, cm.historySize)
	}
}

func TestNewConnectionMetricsWithSize(t *testing.T) {
	cm := NewConnectionMetricsWithSize(100, 5)
	if cm.maxConns != 100 {
		t.Errorf("expected maxConns 100, got %d", cm.maxConns)
	}
	if cm.historySize != 5 {
		t.Errorf("expected historySize 5, got %d", cm.historySize)
	}

	// Test with zero/negative values
	cm = NewConnectionMetricsWithSize(0, -1)
	if cm.maxConns != DefaultMaxConnections {
		t.Errorf("expected default maxConns, got %d", cm.maxConns)
	}
	if cm.historySize != DefaultConnectionHistorySize {
		t.Errorf("expected default historySize, got %d", cm.historySize)
	}
}

func TestRecord(t *testing.T) {
	cm := NewConnectionMetricsWithSize(10, 5)

	// Record first duration
	cm.Record(1234, 100*time.Millisecond)
	if cm.Len() != 1 {
		t.Errorf("expected 1 connection, got %d", cm.Len())
	}

	durations := cm.GetDurations(1234)
	if len(durations) != 1 {
		t.Errorf("expected 1 duration, got %d", len(durations))
	}
	if durations[0] != 0.1 {
		t.Errorf("expected 0.1 seconds, got %f", durations[0])
	}

	// Record more durations
	cm.Record(1234, 200*time.Millisecond)
	cm.Record(1234, 300*time.Millisecond)

	durations = cm.GetDurations(1234)
	if len(durations) != 3 {
		t.Errorf("expected 3 durations, got %d", len(durations))
	}

	// Verify chronological order
	if durations[0] != 0.1 || durations[1] != 0.2 || durations[2] != 0.3 {
		t.Errorf("durations not in chronological order: %v", durations)
	}
}

func TestRecordHistorySizeLimit(t *testing.T) {
	cm := NewConnectionMetricsWithSize(10, 3) // Only 3 samples

	// Record 5 durations
	for i := 1; i <= 5; i++ {
		cm.Record(1234, time.Duration(i)*time.Second)
	}

	durations := cm.GetDurations(1234)
	if len(durations) != 3 {
		t.Errorf("expected 3 durations (capped), got %d", len(durations))
	}

	// Should have the last 3 values: 3s, 4s, 5s
	expected := []float64{3.0, 4.0, 5.0}
	for i, d := range durations {
		if d != expected[i] {
			t.Errorf("expected %f at index %d, got %f", expected[i], i, d)
		}
	}
}

func TestGetDurationsNotFound(t *testing.T) {
	cm := NewConnectionMetrics()

	durations := cm.GetDurations(9999)
	if durations != nil {
		t.Errorf("expected nil for non-existent PID, got %v", durations)
	}
}

func TestGetHistory(t *testing.T) {
	cm := NewConnectionMetrics()

	cm.Record(1234, 100*time.Millisecond)
	cm.Record(1234, 200*time.Millisecond)

	history := cm.GetHistory(1234)
	if history == nil {
		t.Fatal("expected history, got nil")
	}
	if history.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", history.PID)
	}
	if len(history.Durations) != 2 {
		t.Errorf("expected 2 durations, got %d", len(history.Durations))
	}

	// Verify it's a copy
	history.Durations[0].Duration = 999 * time.Second
	original := cm.GetHistory(1234)
	if original.Durations[0].Duration == 999*time.Second {
		t.Error("GetHistory should return a copy, not the original")
	}
}

func TestGetHistoryNotFound(t *testing.T) {
	cm := NewConnectionMetrics()

	history := cm.GetHistory(9999)
	if history != nil {
		t.Errorf("expected nil for non-existent PID, got %v", history)
	}
}

func TestRemove(t *testing.T) {
	cm := NewConnectionMetrics()

	cm.Record(1234, 100*time.Millisecond)
	cm.Record(5678, 200*time.Millisecond)

	if cm.Len() != 2 {
		t.Errorf("expected 2 connections, got %d", cm.Len())
	}

	cm.Remove(1234)

	if cm.Len() != 1 {
		t.Errorf("expected 1 connection after remove, got %d", cm.Len())
	}
	if cm.GetDurations(1234) != nil {
		t.Error("expected nil after remove")
	}
	if cm.GetDurations(5678) == nil {
		t.Error("other connection should still exist")
	}
}

func TestClear(t *testing.T) {
	cm := NewConnectionMetrics()

	cm.Record(1234, 100*time.Millisecond)
	cm.Record(5678, 200*time.Millisecond)
	cm.Record(9012, 300*time.Millisecond)

	cm.Clear()

	if cm.Len() != 0 {
		t.Errorf("expected 0 connections after clear, got %d", cm.Len())
	}
}

func TestEvictLRU(t *testing.T) {
	cm := NewConnectionMetricsWithSize(3, 5) // Only 3 connections allowed

	// Add 3 connections
	cm.Record(1, 100*time.Millisecond)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	cm.Record(2, 200*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	cm.Record(3, 300*time.Millisecond)

	if cm.Len() != 3 {
		t.Errorf("expected 3 connections, got %d", cm.Len())
	}

	// Add a 4th connection - should evict PID 1 (oldest)
	cm.Record(4, 400*time.Millisecond)

	if cm.Len() != 3 {
		t.Errorf("expected 3 connections after eviction, got %d", cm.Len())
	}
	if cm.GetDurations(1) != nil {
		t.Error("PID 1 should have been evicted")
	}
	if cm.GetDurations(4) == nil {
		t.Error("PID 4 should exist")
	}
}

func TestEvictLRUUpdatesAccess(t *testing.T) {
	cm := NewConnectionMetricsWithSize(3, 5)

	// Add 3 connections
	cm.Record(1, 100*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	cm.Record(2, 200*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	cm.Record(3, 300*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	// Access PID 1 again (updates LastAccessed)
	cm.Record(1, 150*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	// Add a 4th connection - should evict PID 2 (now oldest)
	cm.Record(4, 400*time.Millisecond)

	if cm.GetDurations(1) == nil {
		t.Error("PID 1 should NOT have been evicted (recently accessed)")
	}
	if cm.GetDurations(2) != nil {
		t.Error("PID 2 should have been evicted (oldest)")
	}
}

func TestPrune(t *testing.T) {
	cm := NewConnectionMetrics()

	// Add connections at different times
	cm.Record(1, 100*time.Millisecond)
	oldTime := time.Now()
	time.Sleep(50 * time.Millisecond)
	cm.Record(2, 200*time.Millisecond)
	cm.Record(3, 300*time.Millisecond)

	// Prune connections accessed before threshold
	pruned := cm.Prune(oldTime.Add(10 * time.Millisecond))

	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if cm.Len() != 2 {
		t.Errorf("expected 2 connections after prune, got %d", cm.Len())
	}
	if cm.GetDurations(1) != nil {
		t.Error("PID 1 should have been pruned")
	}
}

func TestUpdateFromConnections(t *testing.T) {
	cm := NewConnectionMetrics()

	connections := []ConnectionInfo{
		{PID: 1234, DurationSeconds: 5},
		{PID: 5678, DurationSeconds: 10},
		{PID: 9012, DurationSeconds: 0}, // Idle, should be skipped
	}

	cm.UpdateFromConnections(connections)

	// Should have 2 connections (9012 skipped because duration is 0)
	if cm.Len() != 2 {
		t.Errorf("expected 2 connections, got %d", cm.Len())
	}

	durations := cm.GetDurations(1234)
	if len(durations) != 1 {
		t.Errorf("expected 1 duration for PID 1234, got %d", len(durations))
	}
	if durations[0] != 5.0 {
		t.Errorf("expected 5.0 seconds, got %f", durations[0])
	}

	durations = cm.GetDurations(5678)
	if len(durations) != 1 {
		t.Errorf("expected 1 duration for PID 5678, got %d", len(durations))
	}

	// 9012 should not exist (0 duration)
	if cm.GetDurations(9012) != nil {
		t.Error("PID 9012 should not have been recorded (0 duration)")
	}
}

func TestUpdateFromConnectionsDeduplication(t *testing.T) {
	cm := NewConnectionMetrics()

	connections := []ConnectionInfo{
		{PID: 1234, DurationSeconds: 5},
	}

	// Call multiple times quickly
	cm.UpdateFromConnections(connections)
	cm.UpdateFromConnections(connections)
	cm.UpdateFromConnections(connections)

	// Should only have 1 entry due to deduplication (500ms threshold)
	durations := cm.GetDurations(1234)
	if len(durations) != 1 {
		t.Errorf("expected 1 duration (deduplicated), got %d", len(durations))
	}
}

func TestConcurrentAccess(t *testing.T) {
	cm := NewConnectionMetrics()
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cm.Record(i%10, time.Duration(i)*time.Millisecond)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = cm.GetDurations(i % 10)
			_ = cm.GetHistory(i % 10)
			_ = cm.Len()
		}
		done <- true
	}()

	// Wait for both to finish
	<-done
	<-done

	// No race conditions should occur
	if cm.Len() == 0 {
		t.Error("expected some connections to be recorded")
	}
}
