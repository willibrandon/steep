// Package logs provides the Log Viewer view.
package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors"
)

// PgCollector reads PostgreSQL logs via pg_read_file() function.
// This works over the network and doesn't require local filesystem access.
type PgCollector struct {
	source *monitors.LogSource
	pool   *pgxpool.Pool

	// Position tracking per file
	mu        sync.Mutex
	positions map[string]int64

	// Current file being read
	currentFile  string
	lastFileSize int64
}

// NewPgCollector creates a new pg_read_file() based log collector.
func NewPgCollector(source *monitors.LogSource, pool *pgxpool.Pool) *PgCollector {
	return &PgCollector{
		source:    source,
		pool:      pool,
		positions: make(map[string]int64),
	}
}

// Collect reads new log entries since last collection using pg_read_file().
func (c *PgCollector) Collect() ([]LogEntryData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find log files using pg_ls_dir
	files, err := c.findLogFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("error finding log files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Use the most recent log file
	currentFile := files[len(files)-1]

	// Get file size to detect rotation
	fileSize, err := c.getFileSize(ctx, currentFile)
	if err != nil {
		return nil, fmt.Errorf("error getting file size: %w", err)
	}

	// Check for log rotation (file changed or size decreased)
	if currentFile != c.currentFile {
		c.handleFileChange(currentFile)
	} else if fileSize < c.lastFileSize {
		c.handleLogRotation(currentFile)
	}

	// Read new entries from current file
	entries, err := c.readNewEntries(ctx, currentFile)
	if err != nil {
		return nil, err
	}

	c.lastFileSize = fileSize
	return entries, nil
}

// findLogFiles finds log files matching the pattern using pg_ls_dir.
func (c *PgCollector) findLogFiles(ctx context.Context) ([]string, error) {
	if c.source.LogDir == "" || c.source.LogPattern == "" {
		return nil, nil
	}

	// List files in log directory
	query := `/* steep:internal */ SELECT pg_ls_dir($1) ORDER BY 1`
	rows, err := c.pool.Query(ctx, query, c.source.LogDir)
	if err != nil {
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
		matched, err := filepath.Match(c.source.LogPattern, name)
		if err != nil {
			continue
		}
		if matched {
			// Use path.Join (forward slashes) for paths sent to PostgreSQL server.
			// filepath.Join uses client OS separators which breaks when client
			// OS differs from server OS (e.g., Windows client, Linux server).
			files = append(files, path.Join(c.source.LogDir, name))
		}
	}

	return files, rows.Err()
}

// getFileSize gets the size of a file using pg_stat_file.
func (c *PgCollector) getFileSize(ctx context.Context, path string) (int64, error) {
	var size int64
	query := `/* steep:internal */ SELECT size FROM pg_stat_file($1)`
	err := c.pool.QueryRow(ctx, query, path).Scan(&size)
	if err != nil {
		return 0, err
	}
	return size, nil
}

// handleFileChange handles when the current log file changes.
func (c *PgCollector) handleFileChange(newFile string) {
	c.currentFile = newFile
	c.positions[newFile] = 0
	c.lastFileSize = 0
}

// handleLogRotation handles log rotation (file size decreased).
func (c *PgCollector) handleLogRotation(file string) {
	c.positions[file] = 0
}

// readNewEntries reads new log entries from a file using pg_read_file.
func (c *PgCollector) readNewEntries(ctx context.Context, file string) ([]LogEntryData, error) {
	pos := c.positions[file]

	// Get file size to determine how much to read
	fileSize, err := c.getFileSize(ctx, file)
	if err != nil {
		return nil, err
	}

	// Nothing new to read
	if pos >= fileSize {
		return nil, nil
	}

	// Read the new content
	// pg_read_file(filename, offset, length) - length of -1 reads to end
	query := `/* steep:internal */ SELECT pg_read_file($1, $2, $3)`
	var content string
	bytesToRead := fileSize - pos
	if bytesToRead > 1024*1024 { // Cap at 1MB per read
		bytesToRead = 1024 * 1024
	}
	err = c.pool.QueryRow(ctx, query, file, pos, bytesToRead).Scan(&content)
	if err != nil {
		// Check if it's a permission error
		if strings.Contains(err.Error(), "permission denied") ||
			strings.Contains(err.Error(), "must be superuser") {
			return nil, fmt.Errorf("pg_read_file requires superuser or pg_read_server_files role: %w", err)
		}
		return nil, fmt.Errorf("pg_read_file failed: %w", err)
	}

	// Trim content to last complete line to avoid parsing partial lines
	// Always trim since the file might still be written to
	if lastNewline := strings.LastIndex(content, "\n"); lastNewline >= 0 {
		content = content[:lastNewline+1]
	} else if len(content) > 0 {
		// No complete line yet, wait for more data
		return nil, nil
	}

	// Parse the content based on format
	var entries []LogEntryData
	var newPos int64

	reader := strings.NewReader(content)
	switch c.source.Format {
	case monitors.LogFormatCSV:
		entries, newPos, err = c.parseCSVContent(reader, pos)
	case monitors.LogFormatJSON:
		entries, newPos, err = c.parseJSONContent(reader, pos)
	default:
		entries, newPos, err = c.parseStderrContent(reader, pos)
	}

	if err != nil {
		return nil, err
	}

	c.positions[file] = newPos
	return entries, nil
}

// parseStderrContent parses plain text PostgreSQL logs from a string reader.
func (c *PgCollector) parseStderrContent(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(r)
	pos := startPos

	var currentEntry *LogEntryData
	var unparsedCount int
	var firstUnparsed string

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := int64(len(line)) + 1 // +1 for newline

		// Try to parse as a new log entry by finding the severity marker
		entry := parseStderrLine(line)

		if entry != nil {
			// Save previous entry
			if currentEntry != nil {
				entries = append(entries, *currentEntry)
			}
			currentEntry = entry
		} else if currentEntry != nil {
			// Continuation line
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "DETAIL:") {
				currentEntry.Detail = strings.TrimSpace(strings.TrimPrefix(trimmed, "DETAIL:"))
			} else if strings.HasPrefix(trimmed, "HINT:") {
				currentEntry.Hint = strings.TrimSpace(strings.TrimPrefix(trimmed, "HINT:"))
			} else if strings.HasPrefix(trimmed, "CONTEXT:") {
				currentEntry.Message += "\nCONTEXT: " + strings.TrimSpace(strings.TrimPrefix(trimmed, "CONTEXT:"))
			} else if strings.HasPrefix(trimmed, "STATEMENT:") {
				currentEntry.Message += "\nSTATEMENT: " + strings.TrimSpace(strings.TrimPrefix(trimmed, "STATEMENT:"))
			} else if trimmed != "" {
				currentEntry.Message += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			unparsedCount++
			if firstUnparsed == "" {
				firstUnparsed = truncateLine(line, 100)
			}
		}

		pos += lineLen
	}

	// Don't forget the last entry
	if currentEntry != nil {
		entries = append(entries, *currentEntry)
	}

	if unparsedCount > 0 {
		logger.Warn("pg_read_file: failed to parse postgres log lines",
			"unparsed_count", unparsedCount,
			"first_line", firstUnparsed)
	}

	return entries, pos, scanner.Err()
}

// parseJSONContent parses PostgreSQL JSON log format from a string reader.
func (c *PgCollector) parseJSONContent(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(r)
	pos := startPos

	var jsonErrors int
	var firstError string

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := int64(len(line)) + 1

		var jsonEntry struct {
			Timestamp   string `json:"timestamp"`
			User        string `json:"user"`
			Dbname      string `json:"dbname"`
			Pid         int    `json:"pid"`
			ErrorSev    string `json:"error_severity"`
			Message     string `json:"message"`
			Detail      string `json:"detail"`
			Hint        string `json:"hint"`
			Application string `json:"application_name"`
		}

		if err := parseJSON([]byte(line), &jsonEntry); err != nil {
			jsonErrors++
			if firstError == "" {
				firstError = truncateLine(line, 100)
			}
			pos += lineLen
			continue
		}

		timestamp, _ := parseTimestamp(jsonEntry.Timestamp)
		entry := LogEntryData{
			Timestamp:   timestamp,
			User:        jsonEntry.User,
			Database:    jsonEntry.Dbname,
			PID:         jsonEntry.Pid,
			Severity:    normalizeSeverity(jsonEntry.ErrorSev),
			Message:     strings.TrimRight(jsonEntry.Message, " \t\n\r"),
			Detail:      strings.TrimRight(jsonEntry.Detail, " \t\n\r"),
			Hint:        strings.TrimRight(jsonEntry.Hint, " \t\n\r"),
			Application: jsonEntry.Application,
			RawLine:     line,
		}
		entries = append(entries, entry)

		pos += lineLen
	}

	if jsonErrors > 0 {
		logger.Warn("pg_read_file: JSON log parse errors",
			"count", jsonErrors,
			"first_line", firstError)
	}

	return entries, pos, scanner.Err()
}

// parseCSVContent parses PostgreSQL CSV log format from a string reader.
func (c *PgCollector) parseCSVContent(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
	// Use the same CSV parsing logic as the file collector
	// For simplicity, we'll convert to a simpler line-based approach for pg_read_file
	var entries []LogEntryData
	scanner := bufio.NewScanner(r)
	pos := startPos

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := int64(len(line)) + 1

		// Simple CSV parsing - split by comma
		// Note: This is simplified and may not handle all edge cases
		fields := parseCSVLine(line)
		if len(fields) < 14 {
			pos += lineLen
			continue
		}

		timestamp, _ := parseTimestamp(fields[0])
		pid := 0
		fmt.Sscanf(fields[3], "%d", &pid)

		entry := LogEntryData{
			Timestamp: timestamp,
			User:      fields[1],
			Database:  fields[2],
			PID:       pid,
			Severity:  normalizeSeverity(fields[11]),
			Message:   fields[13],
		}

		if len(fields) > 14 {
			entry.Detail = fields[14]
		}
		if len(fields) > 15 {
			entry.Hint = fields[15]
		}

		entries = append(entries, entry)
		pos += lineLen
	}

	return entries, pos, scanner.Err()
}

// parseCSVLine parses a single CSV line, handling quoted fields.
func parseCSVLine(line string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				// Escaped quote
				current.WriteByte('"')
				i++
			} else {
				inQuotes = !inQuotes
			}
		case c == ',' && !inQuotes:
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	fields = append(fields, current.String())

	return fields
}

// parseJSON is a simple JSON unmarshaler wrapper.
func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// CanUsePgReadFile checks if pg_read_file is available and the user has permissions.
func CanUsePgReadFile(ctx context.Context, pool *pgxpool.Pool, logDir string) error {
	// Try to list the log directory
	query := `/* steep:internal */ SELECT count(*) FROM (SELECT pg_ls_dir($1)) AS dirs`
	var count int
	err := pool.QueryRow(ctx, query, logDir).Scan(&count)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") ||
			strings.Contains(err.Error(), "must be superuser") {
			return fmt.Errorf("requires superuser or pg_read_server_files role")
		}
		if strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("log directory does not exist: %s", logDir)
		}
		return err
	}
	return nil
}
