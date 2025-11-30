package metrics

import (
	"math"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestCircularBuffer_Push(t *testing.T) {
	buf := NewCircularBuffer(3)

	// Push first element
	buf.Push(NewDataPointAt(time.Now(), 1.0))
	if buf.Len() != 1 {
		t.Errorf("expected len 1, got %d", buf.Len())
	}

	// Push to capacity
	buf.Push(NewDataPointAt(time.Now(), 2.0))
	buf.Push(NewDataPointAt(time.Now(), 3.0))
	if buf.Len() != 3 {
		t.Errorf("expected len 3, got %d", buf.Len())
	}
	if !buf.IsFull() {
		t.Error("expected buffer to be full")
	}
}

func TestCircularBuffer_Eviction(t *testing.T) {
	buf := NewCircularBuffer(3)

	// Fill buffer
	buf.Push(NewDataPointAt(time.Now(), 1.0))
	buf.Push(NewDataPointAt(time.Now(), 2.0))
	buf.Push(NewDataPointAt(time.Now(), 3.0))

	// Push beyond capacity - should evict oldest
	buf.Push(NewDataPointAt(time.Now(), 4.0))

	if buf.Len() != 3 {
		t.Errorf("expected len 3 after eviction, got %d", buf.Len())
	}

	values := buf.GetValues()
	expected := []float64{2.0, 3.0, 4.0}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], v)
		}
	}
}

func TestCircularBuffer_GetRecent(t *testing.T) {
	buf := NewCircularBuffer(5)

	for i := 1; i <= 5; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	// Get last 3
	recent := buf.GetRecent(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 elements, got %d", len(recent))
	}

	expected := []float64{3.0, 4.0, 5.0}
	for i, dp := range recent {
		if dp.Value != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], dp.Value)
		}
	}

	// Request more than available
	all := buf.GetRecent(10)
	if len(all) != 5 {
		t.Errorf("expected 5 elements, got %d", len(all))
	}

	// Empty request
	none := buf.GetRecent(0)
	if none != nil {
		t.Error("expected nil for zero request")
	}
}

func TestCircularBuffer_GetSince(t *testing.T) {
	buf := NewCircularBuffer(10)

	now := time.Now()
	// Add points at different times
	buf.Push(NewDataPointAt(now.Add(-5*time.Minute), 1.0))
	buf.Push(NewDataPointAt(now.Add(-3*time.Minute), 2.0))
	buf.Push(NewDataPointAt(now.Add(-1*time.Minute), 3.0))
	buf.Push(NewDataPointAt(now, 4.0))

	// Get points from last 2 minutes
	since := now.Add(-2 * time.Minute)
	points := buf.GetSince(since)

	if len(points) != 2 {
		t.Errorf("expected 2 points, got %d", len(points))
	}

	expected := []float64{3.0, 4.0}
	for i, dp := range points {
		if dp.Value != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], dp.Value)
		}
	}
}

func TestCircularBuffer_GetValues(t *testing.T) {
	buf := NewCircularBuffer(5)

	// Empty buffer
	if buf.GetValues() != nil {
		t.Error("expected nil for empty buffer")
	}

	// Add some values
	for i := 1; i <= 3; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)*10))
	}

	values := buf.GetValues()
	expected := []float64{10.0, 20.0, 30.0}
	if len(values) != len(expected) {
		t.Errorf("expected %d values, got %d", len(expected), len(values))
	}

	for i, v := range values {
		if v != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], v)
		}
	}
}

func TestCircularBuffer_Latest(t *testing.T) {
	buf := NewCircularBuffer(5)

	// Empty buffer
	_, ok := buf.Latest()
	if ok {
		t.Error("expected false for empty buffer")
	}

	buf.Push(NewDataPointAt(time.Now(), 1.0))
	buf.Push(NewDataPointAt(time.Now(), 2.0))
	buf.Push(NewDataPointAt(time.Now(), 3.0))

	latest, ok := buf.Latest()
	if !ok {
		t.Error("expected true for non-empty buffer")
	}
	if latest.Value != 3.0 {
		t.Errorf("expected latest value 3.0, got %f", latest.Value)
	}
}

func TestCircularBuffer_Clear(t *testing.T) {
	buf := NewCircularBuffer(5)

	for i := 1; i <= 5; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	buf.Clear()

	if buf.Len() != 0 {
		t.Errorf("expected len 0 after clear, got %d", buf.Len())
	}
	if !buf.IsEmpty() {
		t.Error("expected buffer to be empty")
	}
}

func TestCircularBuffer_InvalidDataPoints(t *testing.T) {
	buf := NewCircularBuffer(5)

	// NaN should be rejected
	buf.Push(DataPoint{Timestamp: time.Now(), Value: math.NaN()})
	if buf.Len() != 0 {
		t.Error("NaN value should not be added")
	}

	// Inf should be rejected
	buf.Push(DataPoint{Timestamp: time.Now(), Value: math.Inf(1)})
	if buf.Len() != 0 {
		t.Error("Inf value should not be added")
	}

	// Zero timestamp should be rejected
	buf.Push(DataPoint{Timestamp: time.Time{}, Value: 1.0})
	if buf.Len() != 0 {
		t.Error("zero timestamp should not be added")
	}

	// Valid point should be added
	buf.Push(NewDataPointAt(time.Now(), 1.0))
	if buf.Len() != 1 {
		t.Error("valid point should be added")
	}
}

func TestCircularBuffer_Concurrent(t *testing.T) {
	buf := NewCircularBuffer(1000)
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf.Push(NewDataPointAt(time.Now(), float64(id*100+j)))
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = buf.GetValues()
				_ = buf.Len()
				_, _ = buf.Latest()
			}
		}()
	}

	wg.Wait()

	// Buffer should have exactly 1000 elements (at capacity)
	if buf.Len() != 1000 {
		t.Errorf("expected len 1000, got %d", buf.Len())
	}
}

func TestCircularBuffer_WrapAround(t *testing.T) {
	buf := NewCircularBuffer(3)

	// Add 6 elements to wrap around twice
	for i := 1; i <= 6; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	// Should contain last 3 values
	values := buf.GetValues()
	expected := []float64{4.0, 5.0, 6.0}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("expected value[%d]=%f, got %f", i, expected[i], v)
		}
	}
}

func BenchmarkCircularBuffer_Push(b *testing.B) {
	buf := NewCircularBuffer(DefaultBufferCapacity)
	dp := NewDataPointAt(time.Now(), 1.0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Push(dp)
	}
}

func BenchmarkCircularBuffer_GetValues(b *testing.B) {
	buf := NewCircularBuffer(DefaultBufferCapacity)
	for i := 0; i < DefaultBufferCapacity; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.GetValues()
	}
}

func BenchmarkCircularBuffer_GetRecent(b *testing.B) {
	buf := NewCircularBuffer(DefaultBufferCapacity)
	for i := 0; i < DefaultBufferCapacity; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.GetRecent(100)
	}
}

func BenchmarkCircularBuffer_GetSince(b *testing.B) {
	buf := NewCircularBuffer(DefaultBufferCapacity)
	now := time.Now()
	for i := 0; i < DefaultBufferCapacity; i++ {
		buf.Push(NewDataPointAt(now.Add(time.Duration(i)*time.Second), float64(i)))
	}
	// Query last 100 seconds
	since := now.Add(time.Duration(DefaultBufferCapacity-100) * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.GetSince(since)
	}
}

func BenchmarkCircularBuffer_Latest(b *testing.B) {
	buf := NewCircularBuffer(DefaultBufferCapacity)
	for i := 0; i < DefaultBufferCapacity; i++ {
		buf.Push(NewDataPointAt(time.Now(), float64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = buf.Latest()
	}
}

// TestCircularBuffer_MemoryFootprint verifies buffer memory stays under target.
func TestCircularBuffer_MemoryFootprint(t *testing.T) {
	var m1, m2 runtime.MemStats

	// Force GC and get baseline
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Create 3 buffers at max capacity (simulating TPS, connections, cache_hit_ratio)
	buffers := make([]*CircularBuffer, 3)
	for i := range buffers {
		buffers[i] = NewCircularBuffer(DefaultBufferCapacity)
		for j := 0; j < DefaultBufferCapacity; j++ {
			buffers[i].Push(NewDataPointAt(time.Now(), float64(j)))
		}
	}

	// Force GC and measure
	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocatedMB := float64(m2.HeapAlloc-m1.HeapAlloc) / 1024 / 1024
	targetMB := float64(10) // 10 MB target per spec

	t.Logf("Memory footprint: %.2f MB (3 buffers x %d points)", allocatedMB, DefaultBufferCapacity)
	t.Logf("Target: <%.0f MB", targetMB)

	if allocatedMB >= targetMB {
		t.Errorf("memory usage %.2f MB exceeds target %.0f MB", allocatedMB, targetMB)
	}

	// Keep buffers alive
	for _, buf := range buffers {
		_ = buf.Len()
	}
}
