package logs

import (
	"fmt"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

func TestBufferCapacity(t *testing.T) {
	buf := NewLogBuffer(DefaultBufferCapacity)
	if buf.Cap() != 10000 {
		t.Errorf("Expected capacity 10000, got %d", buf.Cap())
	}
}

func TestBufferAddAndGet(t *testing.T) {
	buf := NewLogBuffer(100)

	// Add entries
	for i := 0; i < 50; i++ {
		buf.Add(models.LogEntry{
			Timestamp: time.Now(),
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("Entry %d", i),
		})
	}

	if buf.Len() != 50 {
		t.Errorf("Expected length 50, got %d", buf.Len())
	}

	// Get oldest
	oldest, ok := buf.Oldest()
	if !ok {
		t.Error("Expected to get oldest entry")
	}
	if oldest.Message != "Entry 0" {
		t.Errorf("Expected oldest message 'Entry 0', got '%s'", oldest.Message)
	}

	// Get newest
	newest, ok := buf.Newest()
	if !ok {
		t.Error("Expected to get newest entry")
	}
	if newest.Message != "Entry 49" {
		t.Errorf("Expected newest message 'Entry 49', got '%s'", newest.Message)
	}
}

func TestBufferWrap(t *testing.T) {
	buf := NewLogBuffer(100)

	// Add more than capacity
	for i := 0; i < 150; i++ {
		buf.Add(models.LogEntry{
			Timestamp: time.Now(),
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("Entry %d", i),
		})
	}

	// Buffer should be at capacity
	if buf.Len() != 100 {
		t.Errorf("Expected length 100, got %d", buf.Len())
	}

	// Oldest should be entry 50 (first 50 were overwritten)
	oldest, ok := buf.Oldest()
	if !ok {
		t.Error("Expected to get oldest entry")
	}
	if oldest.Message != "Entry 50" {
		t.Errorf("Expected oldest message 'Entry 50', got '%s'", oldest.Message)
	}

	// Newest should be entry 149
	newest, ok := buf.Newest()
	if !ok {
		t.Error("Expected to get newest entry")
	}
	if newest.Message != "Entry 149" {
		t.Errorf("Expected newest message 'Entry 149', got '%s'", newest.Message)
	}
}

func BenchmarkBufferAdd10000(b *testing.B) {
	buf := NewLogBuffer(DefaultBufferCapacity)
	entry := models.LogEntry{
		Timestamp: time.Now(),
		Severity:  models.SeverityInfo,
		PID:       12345,
		Database:  "testdb",
		User:      "testuser",
		Message:   "This is a test log message with some typical content for benchmarking",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			buf.Add(entry)
		}
	}
}

func BenchmarkBufferGetAll10000(b *testing.B) {
	buf := NewLogBuffer(DefaultBufferCapacity)
	entry := models.LogEntry{
		Timestamp: time.Now(),
		Severity:  models.SeverityInfo,
		PID:       12345,
		Database:  "testdb",
		User:      "testuser",
		Message:   "This is a test log message with some typical content for benchmarking",
	}

	// Fill buffer
	for j := 0; j < 10000; j++ {
		buf.Add(entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.GetAll()
	}
}

func BenchmarkBufferGetRange(b *testing.B) {
	buf := NewLogBuffer(DefaultBufferCapacity)
	entry := models.LogEntry{
		Timestamp: time.Now(),
		Severity:  models.SeverityInfo,
		PID:       12345,
		Database:  "testdb",
		User:      "testuser",
		Message:   "This is a test log message with some typical content for benchmarking",
	}

	// Fill buffer
	for j := 0; j < 10000; j++ {
		buf.Add(entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Get a typical viewport range (50 entries)
		_ = buf.GetRange(5000, 50)
	}
}

func BenchmarkTimestampSearch(b *testing.B) {
	buf := NewLogBuffer(DefaultBufferCapacity)
	baseTime := time.Now().Add(-time.Hour)

	// Fill buffer with entries spanning 1 hour
	for j := 0; j < 10000; j++ {
		buf.Add(models.LogEntry{
			Timestamp: baseTime.Add(time.Duration(j) * 360 * time.Millisecond),
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("Entry %d", j),
		})
	}

	// Search for middle timestamp
	targetTime := baseTime.Add(30 * time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.FindByTimestamp(targetTime)
	}
}
