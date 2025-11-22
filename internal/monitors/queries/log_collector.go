package queries

import (
	"bufio"
	"context"
	"errors"
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
	Params     map[string]string // Captured bound parameters ($1 -> value)
}

// LogCollectorError represents an error from the log collector with guidance.
type LogCollectorError struct {
	Err      error
	Guidance string
}

func (e *LogCollectorError) Error() string {
	return e.Err.Error()
}

func (e *LogCollectorError) Unwrap() error {
	return e.Err
}

// LogCollector parses PostgreSQL log files for query events.
type LogCollector struct {
	logPath       string
	logLinePrefix string
	lastPosition  int64
	events        chan QueryEvent
	errors        chan error
	lastParams    map[string]string // Parameters from most recent DETAIL line
	lastQuery     string            // Query from most recent execute line
	lineBuffer    string            // Buffer for multi-line log entries
}

// isNewLogEntry checks if a line starts a new log entry (has timestamp prefix)
func isNewLogEntry(line string) bool {
	// PostgreSQL log entries start with: 2025-01-01 12:00:00.000
	return len(line) >= 23 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':'
}

// NewLogCollector creates a new LogCollector.
func NewLogCollector(logPath, logLinePrefix string) *LogCollector {
	return &LogCollector{
		logPath:       logPath,
		logLinePrefix: logLinePrefix,
		events:        make(chan QueryEvent, 100),
		errors:        make(chan error, 10),
	}
}

// Errors returns the channel of collector errors.
func (c *LogCollector) Errors() <-chan error {
	return c.errors
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

	// Read immediately on start
	if err := c.readNewEntries(ctx); err != nil {
		c.sendError(err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(c.events)
			close(c.errors)
			return
		case <-ticker.C:
			if err := c.readNewEntries(ctx); err != nil {
				c.sendError(err)
			}
		}
	}
}

// sendError sends an error to the errors channel without blocking.
func (c *LogCollector) sendError(err error) {
	select {
	case c.errors <- err:
	default:
		// Channel full, skip error
	}
}

// readNewEntries reads new log entries since last position.
func (c *LogCollector) readNewEntries(ctx context.Context) error {
	file, err := os.Open(c.logPath)
	if err != nil {
		// Provide helpful guidance for common permission errors
		if errors.Is(err, os.ErrNotExist) {
			return &LogCollectorError{
				Err:      fmt.Errorf("log file not found: %s", c.logPath),
				Guidance: "Verify log_directory and log_filename in postgresql.conf",
			}
		}
		if errors.Is(err, os.ErrPermission) {
			return &LogCollectorError{
				Err:      fmt.Errorf("permission denied reading log file: %s", c.logPath),
				Guidance: "Ensure steep has read access to PostgreSQL log directory. Try: chmod o+rx <log_dir> && chmod o+r <log_file>",
			}
		}
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Get current file size to detect rotation
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat log file: %w", err)
	}
	fileSize := stat.Size()

	// If file is smaller than last position, it was rotated
	if fileSize < c.lastPosition {
		c.lastPosition = 0
	}

	// Seek to last position
	if c.lastPosition > 0 {
		_, err = file.Seek(c.lastPosition, 0)
		if err != nil {
			c.lastPosition = 0
		}
	}

	// Use bufio.Reader instead of Scanner to track position accurately
	reader := bufio.NewReader(file)
	bytesRead := int64(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF reached - process any remaining buffered entry
			if c.lineBuffer != "" {
				event, ok := c.parseLine(c.lineBuffer)
				if ok {
					select {
					case c.events <- event:
					default:
						// Channel full, skip event
					}
				}
				c.lineBuffer = ""
			}
			break
		}

		bytesRead += int64(len(line))

		// Remove trailing newline for parsing
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		// Handle multi-line log entries
		if isNewLogEntry(line) {
			// Process previous buffered entry
			if c.lineBuffer != "" {
				event, ok := c.parseLine(c.lineBuffer)
				if ok {
					select {
					case c.events <- event:
					default:
						// Channel full, skip event
					}
				}
			}
			// Start new buffer
			c.lineBuffer = line
		} else if c.lineBuffer != "" {
			// Continuation line - append to buffer
			c.lineBuffer += " " + strings.TrimSpace(line)
		}
	}

	// Update position based on bytes actually read
	c.lastPosition += bytesRead

	return nil
}

// parseLine parses a PostgreSQL log line into a QueryEvent.
// Also captures bound parameters from DETAIL lines.
//
// Format with %m [%p] %i:
//
//	2025-01-01 12:00:00.000 UTC [1234] SELECT 42  duration: 1.234 ms  statement: SELECT count(*) FROM table
//	2025-01-01 12:00:00.000 UTC [1234] INSERT 0 5  duration: 1.234 ms  statement: INSERT INTO table ...
//	2025-01-01 12:00:00.000 UTC [1234] UPDATE 10  duration: 1.234 ms  execute stmtcache_xxx: UPDATE table ...
//	2025-01-01 12:00:00.000 UTC [1234] DETAIL:  parameters: $1 = '500', $2 = 'text'
func (c *LogCollector) parseLine(line string) (QueryEvent, bool) {
	// Check for DETAIL line with parameters - store for association
	if strings.Contains(line, "DETAIL:") && strings.Contains(strings.ToLower(line), "parameters:") {
		c.lastParams = c.parseParams(line)
		return QueryEvent{}, false
	}

	// Check for statement line (query without duration) - store for association
	statementOnlyRe := regexp.MustCompile(`LOG:\s+statement:\s*(.+)$`)
	if stmtMatch := statementOnlyRe.FindStringSubmatch(line); stmtMatch != nil {
		query := strings.TrimSpace(stmtMatch[1])
		if query != "" && !strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") {
			c.lastQuery = query
		}
		return QueryEvent{}, false
	}

	// Check for execute/bind line (query without duration) - store for association
	executeRe := regexp.MustCompile(`LOG:\s+(?:execute|bind)\s+\S+:\s*(.+)$`)
	if executeMatch := executeRe.FindStringSubmatch(line); executeMatch != nil {
		query := strings.TrimSpace(executeMatch[1])
		if query != "" && !strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") {
			c.lastQuery = query
		}
		return QueryEvent{}, false
	}

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

	// Try to get query from same line (old format) or from stored lastQuery (new format)
	var query string
	statementRe := regexp.MustCompile(`(?:statement|execute\s+\S+|bind\s+\S+):\s*(.+)$`)
	if statementMatch := statementRe.FindStringSubmatch(line); statementMatch != nil {
		query = strings.TrimSpace(statementMatch[1])
	} else if c.lastQuery != "" {
		// Use stored query from previous execute line
		query = c.lastQuery
		c.lastQuery = ""
	}

	if query == "" {
		return QueryEvent{}, false
	}

	// Filter out steep's internal EXPLAIN queries
	if strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") {
		c.lastQuery = ""
		c.lastParams = nil
		return QueryEvent{}, false
	}

	// Extract rows from command tag (%i in log_line_prefix)
	// Format: "SELECT 42", "INSERT 0 5", "UPDATE 10", "DELETE 3", "COPY 100"
	var rows int64
	// Look for command tag after [pid] and before "duration:"
	// Pattern: ] COMMAND [OID] ROWS  duration:
	cmdTagRe := regexp.MustCompile(`\]\s+(SELECT|INSERT|UPDATE|DELETE|COPY)\s+(?:\d+\s+)?(\d+)\s+duration:`)
	if cmdMatch := cmdTagRe.FindStringSubmatch(line); cmdMatch != nil {
		rows, _ = strconv.ParseInt(cmdMatch[2], 10, 64)
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

	// Capture params and clear for next query
	params := c.lastParams
	c.lastParams = nil

	return QueryEvent{
		Query:      query,
		DurationMs: durationMs,
		Rows:       rows,
		Timestamp:  timestamp,
		Database:   database,
		User:       user,
		Params:     params,
	}, true
}

// parseParams extracts parameters from a DETAIL line.
// Format: "DETAIL:  Parameters: $1 = '500', $2 = 'text'"
func (c *LogCollector) parseParams(line string) map[string]string {
	params := make(map[string]string)

	// Find the parameters part (case-insensitive)
	lowerLine := strings.ToLower(line)
	paramsIdx := strings.Index(lowerLine, "parameters:")
	if paramsIdx == -1 {
		return params
	}

	paramsStr := line[paramsIdx+len("parameters:"):]

	// Parse each parameter: $1 = 'value', $2 = 'value'
	paramRe := regexp.MustCompile(`\$(\d+)\s*=\s*'([^']*)'`)
	matches := paramRe.FindAllStringSubmatch(paramsStr, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			params["$"+match[1]] = match[2]
		}
	}

	return params
}
