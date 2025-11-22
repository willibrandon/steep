package queries

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QueryEvent represents a single query execution from log or sample.
type QueryEvent struct {
	Query      string
	DurationMs float64
	Rows       int64
	Timestamp  time.Time
	Database   string
	User       string
}

// LogCollector parses PostgreSQL log files for query events.
type LogCollector struct {
	logPath       string
	logLinePrefix string
	lastPosition  int64
	events        chan QueryEvent
}

// NewLogCollector creates a new LogCollector.
func NewLogCollector(logPath, logLinePrefix string) *LogCollector {
	return &LogCollector{
		logPath:       logPath,
		logLinePrefix: logLinePrefix,
		events:        make(chan QueryEvent, 100),
	}
}

// Events returns the channel of query events.
func (c *LogCollector) Events() <-chan QueryEvent {
	return c.events
}

// Start begins collecting query events from the log file.
func (c *LogCollector) Start(ctx context.Context) error {
	go c.run(ctx)
	return nil
}

// Stop stops the collector.
func (c *LogCollector) Stop() error {
	return nil
}

// run is the main collection loop.
func (c *LogCollector) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(c.events)
			return
		case <-ticker.C:
			if err := c.readNewEntries(ctx); err != nil {
				// Log error but continue
				continue
			}
		}
	}
}

// readNewEntries reads new log entries since last position.
func (c *LogCollector) readNewEntries(ctx context.Context) error {
	file, err := os.Open(c.logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Seek to last position
	if c.lastPosition > 0 {
		_, err = file.Seek(c.lastPosition, 0)
		if err != nil {
			// File may have been rotated, start from beginning
			c.lastPosition = 0
		}
	}

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long queries
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		event, ok := c.parseLine(line)
		if ok {
			select {
			case c.events <- event:
			default:
				// Channel full, skip event
			}
		}
	}

	// Update position
	pos, _ := file.Seek(0, 1)
	c.lastPosition = pos

	return scanner.Err()
}

// parseLine parses a PostgreSQL log line into a QueryEvent.
// Supports multiple log formats based on log_line_prefix setting.
//
// Format 1 (with log_min_duration_statement):
//   2025-01-01 12:00:00.000 UTC [1234] user@db LOG: duration: 1.234 ms statement: SELECT 1
//
// Format 2 (with auto_explain or log_statement_stats):
//   2025-01-01 12:00:00.000 UTC [1234] user@db LOG: duration: 1.234 ms plan:
//   2025-01-01 12:00:00.000 UTC [1234] user@db LOG: duration: 1.234 ms rows: 100 statement: SELECT 1
func (c *LogCollector) parseLine(line string) (QueryEvent, bool) {
	// Match duration
	durationRe := regexp.MustCompile(`duration:\s+([\d.]+)\s+ms`)
	durationMatch := durationRe.FindStringSubmatch(line)
	if durationMatch == nil {
		return QueryEvent{}, false
	}

	durationMs, err := strconv.ParseFloat(durationMatch[1], 64)
	if err != nil {
		return QueryEvent{}, false
	}

	// Match statement
	statementRe := regexp.MustCompile(`statement:\s+(.+)$`)
	statementMatch := statementRe.FindStringSubmatch(line)
	if statementMatch == nil {
		return QueryEvent{}, false
	}

	query := strings.TrimSpace(statementMatch[1])
	if query == "" {
		return QueryEvent{}, false
	}

	// Extract rows if present (from log_statement_stats or custom logging)
	var rows int64
	rowsRe := regexp.MustCompile(`rows:\s+(\d+)`)
	if rowsMatch := rowsRe.FindStringSubmatch(line); rowsMatch != nil {
		rows, _ = strconv.ParseInt(rowsMatch[1], 10, 64)
	}

	// Extract user and database from log line prefix
	// Common format: user@database or user=X,db=Y
	var user, database string

	// Try user@database format
	userDbRe := regexp.MustCompile(`\]\s+(\w+)@(\w+)\s+`)
	if match := userDbRe.FindStringSubmatch(line); match != nil {
		user = match[1]
		database = match[2]
	} else {
		// Try user=X,db=Y format
		userRe := regexp.MustCompile(`user=(\w+)`)
		dbRe := regexp.MustCompile(`db=(\w+)`)

		if match := userRe.FindStringSubmatch(line); match != nil {
			user = match[1]
		}
		if match := dbRe.FindStringSubmatch(line); match != nil {
			database = match[1]
		}
	}

	// Extract timestamp
	timestamp := time.Now()
	tsRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})`)
	if match := tsRe.FindStringSubmatch(line); match != nil {
		if parsed, err := time.Parse("2006-01-02 15:04:05", match[1]); err == nil {
			timestamp = parsed
		}
	}

	return QueryEvent{
		Query:      query,
		DurationMs: durationMs,
		Rows:       rows,
		Timestamp:  timestamp,
		Database:   database,
		User:       user,
	}, true
}
