package queries_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/monitors/queries"
)

func TestLogCollector_ParseLine_Statement(t *testing.T) {
	// Test standard statement format
	logContent := `2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 1.483 ms  statement: SELECT 1
`
	testLogParsing(t, logContent, 1, "SELECT 1", 1.483)
}

func TestLogCollector_ParseLine_Execute(t *testing.T) {
	// Test execute format (prepared statements)
	logContent := `2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 1.483 ms  execute stmtcache_95b470e8e2dde5f8a633776b765a0c8662cf13c2f1890f1f: SELECT pg_database_size(current_database())
`
	testLogParsing(t, logContent, 1, "SELECT pg_database_size(current_database())", 1.483)
}

func TestLogCollector_ParseLine_MultipleQueries(t *testing.T) {
	logContent := `2025-11-21 18:25:26.180 PST [43292] LOG:  duration: 0.112 ms  execute stmtcache_a8c8e45b577db16b04b2a2646871a545232126ad7a431686: SELECT * FROM users
2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 1.483 ms  execute stmtcache_95b470e8e2dde5f8a633776b765a0c8662cf13c2f1890f1f: SELECT pg_database_size(current_database())
2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 0.056 ms  statement: SELECT COUNT(*) FROM pg_stat_activity
`
	testLogParsing(t, logContent, 3, "", 0) // Just check count
}

func TestLogCollector_ParseLine_BindWithDuration(t *testing.T) {
	// Bind statements with duration are captured (needed for dynamic updates)
	logContent := `2025-11-21 18:25:26.180 PST [43293] LOG:  duration: 0.007 ms  bind stmtcache_95b470e8e2dde5f8a633776b765a0c8662cf13c2f1890f1f: SELECT pg_database_size(current_database())
`
	testLogParsing(t, logContent, 1, "SELECT pg_database_size(current_database())", 0.007)
}

func TestLogCollector_ParseLine_DetailIgnored(t *testing.T) {
	// DETAIL lines should be ignored
	logContent := `2025-11-21 18:25:26.180 PST [43292] DETAIL:  Parameters: $1 = '500', $2 = '0'
`
	testLogParsing(t, logContent, 0, "", 0)
}

func TestLogCollector_ParseLine_SlowQuery(t *testing.T) {
	// Test slow query (pg_sleep)
	logContent := `2025-11-21 18:20:00.000 PST [12345] LOG:  duration: 300000.123 ms  statement: SELECT pg_sleep(300)
`
	testLogParsing(t, logContent, 1, "SELECT pg_sleep(300)", 300000.123)
}

func TestLogCollector_ParseLine_RealWorldFormat(t *testing.T) {
	// Test the exact format from the user's log
	logContent := `2025-11-21 18:25:26.180 PST [43293] LOG:  duration: 0.032 ms  execute stmtcache_bd39f05fe9940c936d0fd9a5686b54579902a6cbb619bfb0: SELECT COALESCE(SUM(xact_commit + xact_rollback), 0) as total_xacts FROM pg_stat_database
`
	testLogParsing(t, logContent, 1, "SELECT COALESCE(SUM(xact_commit + xact_rollback), 0) as total_xacts FROM pg_stat_database", 0.032)
}

func TestLogCollector_IncrementalReading(t *testing.T) {
	// Create temp log file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "postgresql.log")

	// Write initial content
	initialContent := `2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 1.0 ms  statement: SELECT 1
`
	err := os.WriteFile(logPath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Start collector
	collector := queries.NewLogCollector(logPath, "%m [%p] ")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for first read
	time.Sleep(100 * time.Millisecond)

	// Get first event
	select {
	case event := <-collector.Events():
		if event.Query != "SELECT 1" {
			t.Errorf("Expected 'SELECT 1', got '%s'", event.Query)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for first event")
	}

	// Append new content
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open log file for append: %v", err)
	}
	_, err = f.WriteString(`2025-11-21 18:25:27.000 PST [43294] LOG:  duration: 2.0 ms  statement: SELECT 2
`)
	f.Close()
	if err != nil {
		t.Fatalf("Failed to append to log file: %v", err)
	}

	// Wait for incremental read
	time.Sleep(1500 * time.Millisecond)

	// Get second event
	select {
	case event := <-collector.Events():
		if event.Query != "SELECT 2" {
			t.Errorf("Expected 'SELECT 2', got '%s'", event.Query)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for second event")
	}
}

func TestLogCollector_ImmediateRead(t *testing.T) {
	// Test that collector reads immediately on start
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "postgresql.log")

	content := `2025-11-21 18:25:26.182 PST [43293] LOG:  duration: 1.0 ms  statement: SELECT 1
`
	err := os.WriteFile(logPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	collector := queries.NewLogCollector(logPath, "%m [%p] ")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Should get event very quickly (not after 1 second tick)
	select {
	case event := <-collector.Events():
		if event.Query != "SELECT 1" {
			t.Errorf("Expected 'SELECT 1', got '%s'", event.Query)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Collector did not read immediately on start")
	}
}

// testLogParsing is a helper that tests log parsing
func testLogParsing(t *testing.T, logContent string, expectedCount int, expectedQuery string, expectedDuration float64) {
	t.Helper()

	// Create temp log file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "postgresql.log")
	err := os.WriteFile(logPath, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Start collector
	collector := queries.NewLogCollector(logPath, "%m [%p] ")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Collect events
	var events []queries.QueryEvent
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event := <-collector.Events():
			events = append(events, event)
			if len(events) >= expectedCount {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:

	// Check count
	if len(events) != expectedCount {
		t.Errorf("Expected %d events, got %d", expectedCount, len(events))
		return
	}

	// Check specific values if provided
	if expectedCount > 0 && expectedQuery != "" {
		if events[0].Query != expectedQuery {
			t.Errorf("Expected query '%s', got '%s'", expectedQuery, events[0].Query)
		}
	}

	if expectedCount > 0 && expectedDuration > 0 {
		if events[0].DurationMs != expectedDuration {
			t.Errorf("Expected duration %f, got %f", expectedDuration, events[0].DurationMs)
		}
	}
}
