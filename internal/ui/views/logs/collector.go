// Package logs provides the Log Viewer view.
package logs

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors"
)

// LogCollector reads and parses PostgreSQL log files.
type LogCollector struct {
	source *monitors.LogSource

	// Position tracking per file
	mu        sync.Mutex
	positions map[string]int64

	// Current file being read
	currentFile   string
	currentReader *os.File
	lastFileSize  int64
}

// NewLogCollector creates a new log collector.
func NewLogCollector(source *monitors.LogSource) *LogCollector {
	return &LogCollector{
		source:    source,
		positions: make(map[string]int64),
	}
}

// Collect reads new log entries since last collection.
func (c *LogCollector) Collect() ([]LogEntryData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find log files
	files, err := c.findLogFiles()
	if err != nil {
		return nil, fmt.Errorf("error finding log files in %s: %w", c.source.LogDir, err)
	}
	if len(files) == 0 {
		// Check if the directory exists
		if _, err := os.Stat(c.source.LogDir); os.IsNotExist(err) {
			return nil, fmt.Errorf("log directory does not exist: %s", c.source.LogDir)
		}
		// Directory exists but no matching files
		return nil, nil
	}

	// Use the most recent log file
	currentFile := files[len(files)-1]

	// Check for log rotation (file changed or size decreased)
	if currentFile != c.currentFile {
		c.handleFileChange(currentFile)
	} else {
		// Check if file was rotated (size decreased)
		info, err := os.Stat(currentFile)
		if err == nil && info.Size() < c.lastFileSize {
			c.handleLogRotation(currentFile)
		}
	}

	// Read new entries from current file
	entries, err := c.readNewEntries(currentFile)
	if err != nil {
		return nil, err
	}

	// Update last file size
	if info, err := os.Stat(currentFile); err == nil {
		c.lastFileSize = info.Size()
	}

	return entries, nil
}

// findLogFiles finds log files matching the pattern.
func (c *LogCollector) findLogFiles() ([]string, error) {
	if c.source.LogDir == "" || c.source.LogPattern == "" {
		return nil, nil
	}

	pattern := filepath.Join(c.source.LogDir, c.source.LogPattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Sort by modification time
	sort.Slice(matches, func(i, j int) bool {
		fi, _ := os.Stat(matches[i])
		fj, _ := os.Stat(matches[j])
		if fi == nil || fj == nil {
			return matches[i] < matches[j]
		}
		return fi.ModTime().Before(fj.ModTime())
	})

	return matches, nil
}

// handleFileChange handles when the current log file changes.
func (c *LogCollector) handleFileChange(newFile string) {
	if c.currentReader != nil {
		c.currentReader.Close()
		c.currentReader = nil
	}
	c.currentFile = newFile
	// Start from beginning of new file
	c.positions[newFile] = 0
	c.lastFileSize = 0
}

// handleLogRotation handles log rotation (file size decreased).
func (c *LogCollector) handleLogRotation(file string) {
	// Reset position to start of file
	c.positions[file] = 0
	if c.currentReader != nil {
		c.currentReader.Close()
		c.currentReader = nil
	}
}

// readNewEntries reads new log entries from a file.
func (c *LogCollector) readNewEntries(file string) ([]LogEntryData, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Seek to last position
	pos := c.positions[file]
	if pos > 0 {
		_, err = f.Seek(pos, io.SeekStart)
		if err != nil {
			return nil, err
		}
	}

	// Read based on format
	var entries []LogEntryData
	var newPos int64

	switch c.source.Format {
	case monitors.LogFormatCSV:
		entries, newPos, err = c.parseCSVLogs(f, pos)
	case monitors.LogFormatJSON:
		entries, newPos, err = c.parseJSONLogs(f, pos)
	default:
		entries, newPos, err = c.parseStderrLogs(f, pos)
	}

	if err != nil {
		return nil, err
	}

	c.positions[file] = newPos
	return entries, nil
}

// parseStderrLogs parses plain text PostgreSQL logs.
func (c *LogCollector) parseStderrLogs(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(r)
	pos := startPos

	// PostgreSQL stderr log patterns - matches various log_line_prefix formats
	// Pattern 1: 2025-11-27 12:34:56.123 PST [12345] [app] [user@host]LOG: message (no space before severity)
	// Pattern 2: 2025-11-27 12:34:56.123 PST [12345] user@db LOG: message
	// Pattern 3: 2025-11-27 12:34:56.123 PST [12345] LOG: message
	// Timezone can be: PST, +00, -08, America/Los_Angeles, etc.
	// Note: \s* before severity to handle missing space when log_line_prefix doesn't end with space
	logPatterns := []*regexp.Regexp{
		// Format with [app] [user@host]: 2025-11-27 12:34:56.123 PST [12345] [steep] [brandon@::1]LOG: msg
		regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\S+)\s+\[(\d+)\]\s+\[([^\]]*)\]\s+\[([^\]]*)\]\s*(\w+):\s*(.*)$`),
		// Format with user@db: 2025-11-27 12:34:56.123 PST [12345] postgres@db LOG: msg
		regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\S+)\s+\[(\d+)\]\s+(\w+)@(\w+)\s*(\w+):\s*(.*)$`),
		// Simple format: 2025-11-27 12:34:56.123 PST [12345] LOG: msg
		regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\S+)\s+\[(\d+)\]\s*(\w+):\s*(.*)$`),
	}

	var currentEntry *LogEntryData
	var unparsedCount int
	var firstUnparsed string

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := int64(len(line)) + 1 // +1 for newline

		var entry *LogEntryData

		// Try each pattern
		for i, pattern := range logPatterns {
			matches := pattern.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			timestamp, _ := parseTimestamp(matches[1])
			pid, _ := strconv.Atoi(matches[3])

			switch i {
			case 0: // [app] [user@host] format
				// matches[4] = app, matches[5] = user@host, matches[6] = severity, matches[7] = message
				userHost := matches[5]
				user := ""
				if atIdx := strings.Index(userHost, "@"); atIdx > 0 {
					user = userHost[:atIdx]
				}
				entry = &LogEntryData{
					Timestamp:   timestamp,
					Severity:    normalizeSeverity(matches[6]),
					PID:         pid,
					User:        user,
					Application: matches[4],
					Message:     matches[7],
					RawLine:     line,
				}
			case 1: // user@db format
				entry = &LogEntryData{
					Timestamp: timestamp,
					Severity:  normalizeSeverity(matches[6]),
					PID:       pid,
					User:      matches[4],
					Database:  matches[5],
					Message:   matches[7],
					RawLine:   line,
				}
			case 2: // simple format
				entry = &LogEntryData{
					Timestamp: timestamp,
					Severity:  normalizeSeverity(matches[4]),
					PID:       pid,
					Message:   matches[5],
					RawLine:   line,
				}
			}
			break
		}

		if entry != nil {
			// Save previous entry
			if currentEntry != nil {
				entries = append(entries, *currentEntry)
			}
			currentEntry = entry
		} else if currentEntry != nil {
			// Continuation line (DETAIL, HINT, or multi-line message)
			if strings.HasPrefix(line, "DETAIL:") {
				currentEntry.Detail = strings.TrimPrefix(line, "DETAIL:")
				currentEntry.Detail = strings.TrimSpace(currentEntry.Detail)
			} else if strings.HasPrefix(line, "HINT:") {
				currentEntry.Hint = strings.TrimPrefix(line, "HINT:")
				currentEntry.Hint = strings.TrimSpace(currentEntry.Hint)
			} else if strings.TrimSpace(line) != "" {
				// Append to message (skip empty lines)
				currentEntry.Message += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			// Line doesn't match any pattern and no current entry - truly unparsed
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

	// Log summary of unparsed lines
	if unparsedCount > 0 {
		logger.Warn("failed to parse postgres log lines",
			"unparsed_count", unparsedCount,
			"first_line", firstUnparsed)
	}

	return entries, pos, scanner.Err()
}

// truncateLine truncates a line to maxLen characters.
func truncateLine(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// parseCSVLogs parses PostgreSQL CSV log format.
func (c *LogCollector) parseCSVLogs(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
	var entries []LogEntryData
	csvReader := csv.NewReader(r)
	csvReader.FieldsPerRecord = -1 // Variable fields
	csvReader.LazyQuotes = true

	var malformedCount int
	var shortRecordCount int

	pos := startPos
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			malformedCount++
			continue
		}

		// PostgreSQL CSV format columns:
		// 0: log_time, 1: user_name, 2: database_name, 3: process_id, 4: connection_from,
		// 5: session_id, 6: session_line_num, 7: command_tag, 8: session_start_time,
		// 9: virtual_transaction_id, 10: transaction_id, 11: error_severity,
		// 12: sql_state_code, 13: message, 14: detail, 15: hint, ...
		if len(record) < 14 {
			shortRecordCount++
			continue
		}

		timestamp, _ := parseTimestamp(record[0])
		pid, _ := strconv.Atoi(record[3])

		entry := LogEntryData{
			Timestamp:   timestamp,
			User:        record[1],
			Database:    record[2],
			PID:         pid,
			Severity:    normalizeSeverity(record[11]),
			Message:     record[13],
		}

		if len(record) > 14 {
			entry.Detail = record[14]
		}
		if len(record) > 15 {
			entry.Hint = record[15]
		}

		entries = append(entries, entry)
	}

	// Log parsing errors
	if malformedCount > 0 || shortRecordCount > 0 {
		logger.Warn("CSV log parse errors",
			"malformed", malformedCount,
			"short_records", shortRecordCount)
	}

	// Approximate new position
	pos += int64(len(entries) * 200) // Rough estimate
	return entries, pos, nil
}

// parseJSONLogs parses PostgreSQL JSON log format.
func (c *LogCollector) parseJSONLogs(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
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

		if err := json.Unmarshal([]byte(line), &jsonEntry); err != nil {
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
			Message:     jsonEntry.Message,
			Detail:      jsonEntry.Detail,
			Hint:        jsonEntry.Hint,
			Application: jsonEntry.Application,
			RawLine:     line,
		}
		entries = append(entries, entry)

		pos += lineLen
	}

	// Log JSON parse errors
	if jsonErrors > 0 {
		logger.Warn("JSON log parse errors",
			"count", jsonErrors,
			"first_line", firstError)
	}

	return entries, pos, scanner.Err()
}

// parseTimestamp parses a PostgreSQL timestamp.
func parseTimestamp(s string) (time.Time, error) {
	// Try various PostgreSQL timestamp formats
	formats := []string{
		"2006-01-02 15:04:05.999999 MST",
		"2006-01-02 15:04:05.999 MST",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, nil
}

// normalizeSeverity normalizes PostgreSQL log severity strings.
func normalizeSeverity(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case "DEBUG", "DEBUG1", "DEBUG2", "DEBUG3", "DEBUG4", "DEBUG5":
		return "DEBUG"
	case "LOG", "INFO", "NOTICE":
		return "INFO"
	case "WARNING":
		return "WARN"
	case "ERROR", "FATAL", "PANIC":
		return "ERROR"
	default:
		return "INFO"
	}
}

// DetectFormat auto-detects the log format from file content.
func DetectFormat(filename string) monitors.LogFormat {
	// First try by extension
	format := monitors.DetectFormatFromFilename(filename)
	if format != monitors.LogFormatUnknown {
		return format
	}

	// Try reading first line
	f, err := os.Open(filename)
	if err != nil {
		return monitors.LogFormatStderr
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		// Check if it's JSON
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return monitors.LogFormatJSON
		}
		// Check if it looks like CSV (starts with timestamp and has many commas)
		if strings.Count(line, ",") > 10 {
			return monitors.LogFormatCSV
		}
	}

	return monitors.LogFormatStderr
}
