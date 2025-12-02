package queries

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/logger"
)

// LogAccessMethod represents how to read PostgreSQL log files.
type LogAccessMethod int

const (
	// LogAccessFileSystem reads logs directly from the file system.
	LogAccessFileSystem LogAccessMethod = iota
	// LogAccessPgReadFile reads logs via pg_read_file() function.
	LogAccessPgReadFile
)

// Pre-compiled regexes for log parsing (compile once at startup)
var (
	statementOnlyRe = regexp.MustCompile(`LOG:\s+statement:\s*(.+)$`)
	executeRe       = regexp.MustCompile(`LOG:\s+(?:execute|bind)\s+\S+:\s*(.+)$`)
	durationRe      = regexp.MustCompile(`duration:\s+([\d.]+)\s+ms`)
	statementRe     = regexp.MustCompile(`(?:statement|execute\s+\S+|bind\s+\S+):\s*(.+)$`)
	cmdTagRe        = regexp.MustCompile(`\]\s+(SELECT|INSERT|UPDATE|DELETE|COPY)\s+(?:\d+\s+)?(\d+)\s+duration:`)
	userDbRe        = regexp.MustCompile(`\]\s+(\w+)@(\w+)\s+`)
	userRe          = regexp.MustCompile(`user=(\w+)`)
	dbRe            = regexp.MustCompile(`db=(\w+)`)
	tsRe            = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})`)
	paramRe         = regexp.MustCompile(`\$(\d+)\s*=\s*'([^']*)'`)
	pidRe           = regexp.MustCompile(`\[(\d+)\]`) // Extract PID from log line
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

// PositionStore defines the interface for persisting log positions.
type PositionStore interface {
	GetLogPosition(ctx context.Context, filePath string) (int64, error)
	SaveLogPosition(ctx context.Context, filePath string, position int64) error
}

// LogCollector parses PostgreSQL log files for query events.
type LogCollector struct {
	logDir        string
	logPattern    string
	logLinePrefix string
	positions     map[string]int64  // Position per file
	events        chan QueryEvent
	errors        chan error
	lastParams    map[string]map[string]string // Parameters per PID from most recent DETAIL line
	lastQuery     map[string]string            // Query per PID from most recent execute line
	lineBuffer    string                       // Buffer for multi-line log entries
	store         PositionStore                // For persisting position across restarts
	pool          *pgxpool.Pool                // Database pool for pg_read_file access
	accessMethod  LogAccessMethod              // How to read log files
	seenEvents    map[string]time.Time         // Deduplication: key is timestamp+query hash, value is when we saw it
}

// isNewLogEntry checks if a line starts a new log entry (has timestamp prefix)
func isNewLogEntry(line string) bool {
	// PostgreSQL log entries start with: 2025-01-01 12:00:00.000
	return len(line) >= 23 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':'
}

// NewLogCollector creates a new LogCollector.
func NewLogCollector(logDir, logPattern, logLinePrefix string, store PositionStore, pool *pgxpool.Pool, accessMethod LogAccessMethod) *LogCollector {
	return &LogCollector{
		logDir:        logDir,
		logPattern:    logPattern,
		logLinePrefix: logLinePrefix,
		positions:     make(map[string]int64),
		events:        make(chan QueryEvent, 10000),
		errors:        make(chan error, 10),
		lastParams:    make(map[string]map[string]string),
		lastQuery:     make(map[string]string),
		store:         store,
		pool:          pool,
		accessMethod:  accessMethod,
		seenEvents:    make(map[string]time.Time),
	}
}

// eventKey generates a deduplication key for a query event.
// Events with the same timestamp and query are considered duplicates.
func (c *LogCollector) eventKey(event QueryEvent) string {
	// Use timestamp (to second precision) + query text as the key
	// This ensures the same query execution from different log formats
	// (e.g., .log and .json) is only counted once
	return event.Timestamp.Format("2006-01-02 15:04:05") + "|" + event.Query
}

// isDuplicate checks if we've already seen this event.
func (c *LogCollector) isDuplicate(event QueryEvent) bool {
	key := c.eventKey(event)
	_, exists := c.seenEvents[key]
	return exists
}

// markSeen marks an event as seen for deduplication.
func (c *LogCollector) markSeen(event QueryEvent) {
	key := c.eventKey(event)
	c.seenEvents[key] = time.Now()
}

// cleanupSeenEvents removes old entries from the seenEvents map to prevent memory growth.
// Also cleans up stale per-PID query and params entries.
func (c *LogCollector) cleanupSeenEvents() {
	cutoff := time.Now().Add(-5 * time.Minute)
	for key, seenAt := range c.seenEvents {
		if seenAt.Before(cutoff) {
			delete(c.seenEvents, key)
		}
	}
	// Clean up stale per-PID entries (connections that ended without completing their query)
	// Keep the maps small by clearing them periodically - any incomplete queries
	// are stale after 5 minutes anyway
	if len(c.lastQuery) > 1000 {
		c.lastQuery = make(map[string]string)
	}
	if len(c.lastParams) > 1000 {
		c.lastParams = make(map[string]map[string]string)
	}
}

// loadPositions loads persisted positions for all matching files.
func (c *LogCollector) loadPositions(ctx context.Context) {
	if c.store == nil {
		return
	}

	files, err := c.findLogFiles()
	if err != nil {
		return
	}

	for _, file := range files {
		pos, err := c.store.GetLogPosition(ctx, file)
		if err == nil && pos > 0 {
			c.positions[file] = pos
		}
	}
}

// findLogFiles returns all log files matching the pattern, sorted by modification time.
func (c *LogCollector) findLogFiles() ([]string, error) {
	if c.accessMethod == LogAccessPgReadFile {
		return c.findLogFilesPgReadFile()
	}
	return c.findLogFilesFilesystem()
}

// findLogFilesFilesystem finds log files using the local filesystem.
func (c *LogCollector) findLogFilesFilesystem() ([]string, error) {
	pattern := filepath.Join(c.logDir, c.logPattern)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Sort by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		statI, errI := os.Stat(files[i])
		statJ, errJ := os.Stat(files[j])
		if errI != nil || errJ != nil {
			return files[i] < files[j]
		}
		return statI.ModTime().Before(statJ.ModTime())
	})

	return files, nil
}

// findLogFilesPgReadFile finds log files using pg_ls_dir().
func (c *LogCollector) findLogFilesPgReadFile() ([]string, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("pg_read_file mode requires database pool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List files in log directory using pg_ls_dir
	query := `SELECT pg_ls_dir($1) ORDER BY 1`
	rows, err := c.pool.Query(ctx, query, c.logDir)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") ||
			strings.Contains(err.Error(), "must be superuser") {
			return nil, &LogCollectorError{
				Err:      fmt.Errorf("pg_ls_dir requires superuser or pg_read_server_files role: %w", err),
				Guidance: "Grant pg_read_server_files role to the database user",
			}
		}
		return nil, fmt.Errorf("pg_ls_dir failed: %w", err)
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}

		// Match against pattern
		matched, err := filepath.Match(c.logPattern, name)
		if err != nil {
			continue
		}
		if matched {
			files = append(files, filepath.Join(c.logDir, name))
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by name (which for PostgreSQL log files includes timestamp)
	sort.Strings(files)

	return files, nil
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
	// Load persisted positions on startup
	c.loadPositions(ctx)

	// Read immediately on start
	if err := c.readAllFiles(ctx); err != nil {
		c.sendError(err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(c.events)
			close(c.errors)
			return
		case <-ticker.C:
			if err := c.readAllFiles(ctx); err != nil {
				c.sendError(err)
			}
		case <-cleanupTicker.C:
			c.cleanupSeenEvents()
		}
	}
}

// readAllFiles reads all matching log files.
func (c *LogCollector) readAllFiles(ctx context.Context) error {
	files, err := c.findLogFiles()
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return &LogCollectorError{
			Err:      fmt.Errorf("no log files found matching pattern: %s", filepath.Join(c.logDir, c.logPattern)),
			Guidance: "Verify log_directory and log_filename in postgresql.conf",
		}
	}

	for _, file := range files {
		if err := c.readFile(ctx, file); err != nil {
			// Continue with other files on error
			c.sendError(err)
		}
	}

	return nil
}

// readFile reads new entries from a single log file.
func (c *LogCollector) readFile(ctx context.Context, filePath string) error {
	if c.accessMethod == LogAccessPgReadFile {
		return c.readFilePgReadFile(ctx, filePath)
	}
	return c.readFileFilesystem(ctx, filePath)
}

// readFileFilesystem reads log entries using the local filesystem.
func (c *LogCollector) readFileFilesystem(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // File may have been rotated away
		}
		if errors.Is(err, os.ErrPermission) {
			return &LogCollectorError{
				Err:      fmt.Errorf("permission denied reading log file: %s", filePath),
				Guidance: "Ensure steep has read access to PostgreSQL log directory",
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

	// Get last position for this file
	lastPosition := c.positions[filePath]

	// If file is smaller than last position, it was rotated
	if fileSize < lastPosition {
		lastPosition = 0
	}

	// Seek to last position
	if lastPosition > 0 {
		_, err = file.Seek(lastPosition, 0)
		if err != nil {
			lastPosition = 0
		}
	}

	// Use bufio.Reader instead of Scanner to track position accurately
	reader := bufio.NewReader(file)
	bytesRead := int64(0)

	for {
		select {
		case <-ctx.Done():
			// Save position before exiting to avoid re-reading on next start
			newPosition := lastPosition + bytesRead
			c.positions[filePath] = newPosition
			if c.store != nil && bytesRead > 0 {
				_ = c.store.SaveLogPosition(context.Background(), filePath, newPosition)
			}
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF reached - process any remaining buffered entry
			if c.lineBuffer != "" {
				event, ok := c.parseLine(c.lineBuffer)
				if ok && !c.isDuplicate(event) {
					c.markSeen(event)
					select {
					case c.events <- event:
					default:
						logger.Warn("log_collector: events channel full, dropping event")
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
				if ok && !c.isDuplicate(event) {
					c.markSeen(event)
					select {
					case c.events <- event:
					default:
						logger.Warn("log_collector: events channel full, dropping event")
					}
				}
			}
			// Start new buffer
			c.lineBuffer = line
		} else if c.lineBuffer != "" {
			// Continuation line - append with newline to preserve line comment semantics
			// Using space would break "-- comment\nSELECT" into "-- comment SELECT" (all comment)
			c.lineBuffer += "\n" + strings.TrimSpace(line)
		}
	}

	// Update position based on bytes actually read
	newPosition := lastPosition + bytesRead
	c.positions[filePath] = newPosition

	// Persist position for next restart
	if c.store != nil && bytesRead > 0 {
		_ = c.store.SaveLogPosition(ctx, filePath, newPosition)
	}

	return nil
}

// readFilePgReadFile reads log entries using pg_read_file().
func (c *LogCollector) readFilePgReadFile(ctx context.Context, filePath string) error {
	if c.pool == nil {
		return fmt.Errorf("pg_read_file mode requires database pool")
	}

	// Get file size using pg_stat_file
	var fileSize int64
	err := c.pool.QueryRow(ctx, `SELECT size FROM pg_stat_file($1)`, filePath).Scan(&fileSize)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil // File may have been rotated away
		}
		if strings.Contains(err.Error(), "permission denied") ||
			strings.Contains(err.Error(), "must be superuser") {
			return &LogCollectorError{
				Err:      fmt.Errorf("pg_stat_file requires superuser or pg_read_server_files role: %w", err),
				Guidance: "Grant pg_read_server_files role to the database user",
			}
		}
		return fmt.Errorf("pg_stat_file failed: %w", err)
	}

	// Get last position for this file
	lastPosition := c.positions[filePath]

	// If file is smaller than last position, it was rotated
	if fileSize < lastPosition {
		lastPosition = 0
	}

	// Nothing new to read
	if lastPosition >= fileSize {
		return nil
	}

	// Read new content using pg_read_file(filename, offset, length)
	bytesToRead := fileSize - lastPosition
	if bytesToRead > 1024*1024 { // Cap at 1MB per read
		bytesToRead = 1024 * 1024
	}

	var content string
	err = c.pool.QueryRow(ctx, `SELECT pg_read_file($1, $2, $3)`, filePath, lastPosition, bytesToRead).Scan(&content)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") ||
			strings.Contains(err.Error(), "must be superuser") {
			return &LogCollectorError{
				Err:      fmt.Errorf("pg_read_file requires superuser or pg_read_server_files role: %w", err),
				Guidance: "Grant pg_read_server_files role to the database user",
			}
		}
		return fmt.Errorf("pg_read_file failed: %w", err)
	}

	// Trim content to last complete line to avoid parsing partial lines
	if lastNewline := strings.LastIndex(content, "\n"); lastNewline >= 0 {
		content = content[:lastNewline+1]
	} else if len(content) > 0 {
		// No complete line yet, wait for more data
		return nil
	}

	// Parse each line
	bytesRead := int64(0)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if line == "" {
			bytesRead++ // Count the newline
			continue
		}

		bytesRead += int64(len(line)) + 1 // +1 for newline

		// Handle multi-line log entries
		if isNewLogEntry(line) {
			// Process previous buffered entry
			if c.lineBuffer != "" {
				event, ok := c.parseLine(c.lineBuffer)
				if ok && !c.isDuplicate(event) {
					c.markSeen(event)
					select {
					case c.events <- event:
					default:
						logger.Warn("log_collector: events channel full, dropping event")
					}
				}
			}
			// Start new buffer
			c.lineBuffer = line
		} else if c.lineBuffer != "" {
			// Continuation line - append with newline to preserve line comment semantics
			c.lineBuffer += "\n" + strings.TrimSpace(line)
		}
	}

	// Update position based on bytes actually read
	newPosition := lastPosition + bytesRead
	c.positions[filePath] = newPosition

	// Persist position for next restart
	if c.store != nil && bytesRead > 0 {
		_ = c.store.SaveLogPosition(ctx, filePath, newPosition)
	}

	return nil
}

// sendError sends an error to the errors channel without blocking.
func (c *LogCollector) sendError(err error) {
	select {
	case c.errors <- err:
	default:
		// Channel full, skip error
	}
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
	// Extract PID from log line - critical for correlating statement with duration
	// when multiple connections are interleaved in the log
	pid := ""
	if pidMatch := pidRe.FindStringSubmatch(line); pidMatch != nil {
		pid = pidMatch[1]
	}

	// Check for DETAIL line with parameters - store for association (per PID)
	if strings.Contains(line, "DETAIL:") && strings.Contains(strings.ToLower(line), "parameters:") {
		if pid != "" {
			c.lastParams[pid] = c.parseParams(line)
		}
		return QueryEvent{}, false
	}

	// Check for statement line (query without duration) - store for association (per PID)
	if stmtMatch := statementOnlyRe.FindStringSubmatch(line); stmtMatch != nil {
		query := strings.TrimSpace(stmtMatch[1])
		if query != "" && !strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") && pid != "" {
			c.lastQuery[pid] = query
		}
		return QueryEvent{}, false
	}

	// Check for execute/bind line (query without duration) - store for association (per PID)
	if executeMatch := executeRe.FindStringSubmatch(line); executeMatch != nil {
		query := strings.TrimSpace(executeMatch[1])
		if query != "" && !strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") && pid != "" {
			c.lastQuery[pid] = query
		}
		return QueryEvent{}, false
	}

	// Match duration
	durationMatch := durationRe.FindStringSubmatch(line)
	if durationMatch == nil {
		return QueryEvent{}, false
	}

	durationMs, err := strconv.ParseFloat(durationMatch[1], 64)
	if err != nil {
		return QueryEvent{}, false
	}

	// Try to get query from same line (old format) or from stored lastQuery (new format, per PID)
	var query string
	if statementMatch := statementRe.FindStringSubmatch(line); statementMatch != nil {
		query = strings.TrimSpace(statementMatch[1])
	} else if pid != "" && c.lastQuery[pid] != "" {
		// Use stored query from previous execute line for this PID
		query = c.lastQuery[pid]
		delete(c.lastQuery, pid)
	}

	if query == "" {
		return QueryEvent{}, false
	}

	// Filter out steep's internal EXPLAIN queries
	if strings.HasPrefix(strings.ToUpper(query), "EXPLAIN (FORMAT JSON)") {
		if pid != "" {
			delete(c.lastQuery, pid)
			delete(c.lastParams, pid)
		}
		return QueryEvent{}, false
	}

	// Filter out comment-only queries (e.g., "-- ping" health checks)
	if isCommentOnly(query) {
		if pid != "" {
			delete(c.lastQuery, pid)
			delete(c.lastParams, pid)
		}
		return QueryEvent{}, false
	}

	// Filter out noise queries that aren't useful for performance analysis
	if isNoiseQuery(query) {
		if pid != "" {
			delete(c.lastQuery, pid)
			delete(c.lastParams, pid)
		}
		return QueryEvent{}, false
	}

	// Extract rows from command tag (%i in log_line_prefix)
	// Format: "SELECT 42", "INSERT 0 5", "UPDATE 10", "DELETE 3", "COPY 100"
	var rows int64
	// Look for command tag after [pid] and before "duration:"
	// Pattern: ] COMMAND [OID] ROWS  duration:
	if cmdMatch := cmdTagRe.FindStringSubmatch(line); cmdMatch != nil {
		rows, _ = strconv.ParseInt(cmdMatch[2], 10, 64)
	}

	// Extract user and database from log line prefix
	// Common format: user@database or user=X,db=Y
	var user, database string

	// Try user@database format
	if match := userDbRe.FindStringSubmatch(line); match != nil {
		user = match[1]
		database = match[2]
	} else {
		// Try user=X,db=Y format
		if match := userRe.FindStringSubmatch(line); match != nil {
			user = match[1]
		}
		if match := dbRe.FindStringSubmatch(line); match != nil {
			database = match[1]
		}
	}

	// Extract timestamp
	timestamp := time.Now()
	if match := tsRe.FindStringSubmatch(line); match != nil {
		if parsed, err := time.Parse("2006-01-02 15:04:05", match[1]); err == nil {
			timestamp = parsed
		}
	}

	// Capture params and clear for next query (per PID)
	var params map[string]string
	if pid != "" {
		params = c.lastParams[pid]
		delete(c.lastParams, pid)
	}

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

// isNoiseQuery checks if a query is "noise" that shouldn't be tracked for performance.
// This includes transaction control, session management, and health checks.
func isNoiseQuery(query string) bool {
	// Normalize: uppercase, trim whitespace, remove trailing semicolon
	q := strings.TrimSpace(strings.ToUpper(query))
	q = strings.TrimSuffix(q, ";")
	q = strings.TrimSpace(q)

	// Transaction control
	switch q {
	case "BEGIN", "START TRANSACTION", "COMMIT", "ROLLBACK", "END":
		return true
	}

	// Transaction control with options (BEGIN ISOLATION LEVEL..., etc.)
	if strings.HasPrefix(q, "BEGIN ") || strings.HasPrefix(q, "START TRANSACTION ") {
		return true
	}

	// Savepoints
	if strings.HasPrefix(q, "SAVEPOINT ") ||
		strings.HasPrefix(q, "RELEASE SAVEPOINT ") ||
		strings.HasPrefix(q, "RELEASE ") ||
		strings.HasPrefix(q, "ROLLBACK TO SAVEPOINT ") ||
		strings.HasPrefix(q, "ROLLBACK TO ") {
		return true
	}

	// Session management
	if strings.HasPrefix(q, "SET ") ||
		strings.HasPrefix(q, "RESET ") ||
		strings.HasPrefix(q, "SHOW ") ||
		q == "DISCARD ALL" ||
		q == "DISCARD TEMP" ||
		q == "DISCARD TEMPORARY" ||
		q == "DISCARD PLANS" ||
		q == "DISCARD SEQUENCES" {
		return true
	}

	// Health checks - SELECT 1, SELECT 1 AS ..., etc.
	if q == "SELECT 1" ||
		strings.HasPrefix(q, "SELECT 1 AS ") ||
		q == "SELECT 1 AS ONE" ||
		q == "SELECT TRUE" ||
		q == "SELECT 'PING'" {
		return true
	}

	// Empty statement
	if q == "" {
		return true
	}

	return false
}

// isCommentOnly checks if a query consists only of comments (no actual SQL).
// This filters out health check queries like "-- ping".
func isCommentOnly(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}

	// Remove all comments and whitespace, check if anything remains
	inBlockComment := false
	inLineComment := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		// Handle line comments
		if !inBlockComment && i+1 < len(query) && c == '-' && query[i+1] == '-' {
			inLineComment = true
			i++
			continue
		}
		if inLineComment && c == '\n' {
			inLineComment = false
			continue
		}
		if inLineComment {
			continue
		}

		// Handle block comments
		if i+1 < len(query) && c == '/' && query[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}
		if inBlockComment && i+1 < len(query) && c == '*' && query[i+1] == '/' {
			inBlockComment = false
			i++
			continue
		}
		if inBlockComment {
			continue
		}

		// Found non-whitespace, non-comment character
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}

	return true
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
	matches := paramRe.FindAllStringSubmatch(paramsStr, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			params["$"+match[1]] = match[2]
		}
	}

	return params
}
