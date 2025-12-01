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

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()

	// Seek to last position
	pos := c.positions[file]
	if pos > 0 {
		_, err = f.Seek(pos, io.SeekStart)
		if err != nil {
			return nil, err
		}
	}

	// Nothing new to read
	if pos >= fileSize {
		return nil, nil
	}

	// Read remaining content (cap at 1MB)
	bytesToRead := fileSize - pos
	if bytesToRead > 1024*1024 {
		bytesToRead = 1024 * 1024
	}

	content := make([]byte, bytesToRead)
	n, err := io.ReadFull(f, content)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	content = content[:n]

	// Trim to last complete line to avoid parsing partial lines
	// This handles CRLF vs LF correctly
	contentStr := string(content)
	if lastNewline := strings.LastIndex(contentStr, "\n"); lastNewline >= 0 {
		contentStr = contentStr[:lastNewline+1]
	} else if len(contentStr) > 0 {
		// No complete line yet, wait for more data
		return nil, nil
	}

	// Parse the content based on format
	var entries []LogEntryData
	var newPos int64

	reader := strings.NewReader(contentStr)
	switch c.source.Format {
	case monitors.LogFormatCSV:
		entries, newPos, err = c.parseCSVLogs(reader, pos)
	case monitors.LogFormatJSON:
		entries, newPos, err = c.parseJSONLogs(reader, pos)
	default:
		entries, newPos, err = c.parseStderrLogs(reader, pos)
	}

	if err != nil {
		return nil, err
	}

	// Update position based on actual content processed
	c.positions[file] = pos + int64(len(contentStr))
	_ = newPos // Position from parser no longer needed
	return entries, nil
}

// severityPattern matches PostgreSQL severity markers.
// PostgreSQL always outputs: <log_line_prefix><SEVERITY>:  <message>
// The severity is followed by a colon and two spaces.
var severityPattern = regexp.MustCompile(`(DEBUG|LOG|INFO|NOTICE|WARNING|ERROR|FATAL|PANIC):\s{1,2}`)

// timestampPattern extracts timestamps from the prefix.
var timestampPattern = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)

// pidPattern extracts process ID from the prefix.
var pidPattern = regexp.MustCompile(`\[(\d+)\]`)

// parseStderrLogs parses plain text PostgreSQL logs.
// This parser anchors on the severity marker (LOG:, ERROR:, etc.) which is
// always present regardless of log_line_prefix configuration.
func (c *LogCollector) parseStderrLogs(r io.Reader, startPos int64) ([]LogEntryData, int64, error) {
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
			// Continuation line (DETAIL, HINT, CONTEXT, STATEMENT, or multi-line message)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "DETAIL:") {
				currentEntry.Detail = strings.TrimSpace(strings.TrimPrefix(trimmed, "DETAIL:"))
			} else if strings.HasPrefix(trimmed, "HINT:") {
				currentEntry.Hint = strings.TrimSpace(strings.TrimPrefix(trimmed, "HINT:"))
			} else if strings.HasPrefix(trimmed, "CONTEXT:") {
				// Append context to message
				currentEntry.Message += "\nCONTEXT: " + strings.TrimSpace(strings.TrimPrefix(trimmed, "CONTEXT:"))
			} else if strings.HasPrefix(trimmed, "STATEMENT:") {
				// Append statement to message
				currentEntry.Message += "\nSTATEMENT: " + strings.TrimSpace(strings.TrimPrefix(trimmed, "STATEMENT:"))
			} else if trimmed != "" {
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

// parseStderrLine parses a single stderr log line by anchoring on the severity marker.
func parseStderrLine(line string) *LogEntryData {
	// Find the severity marker (LOG:, ERROR:, etc.)
	loc := severityPattern.FindStringIndex(line)
	if loc == nil {
		return nil
	}

	// Split into prefix and message
	prefix := line[:loc[0]]
	matchedText := line[loc[0]:loc[1]]
	// Find colon position to extract just the severity word
	colonIdx := strings.Index(matchedText, ":")
	severity := matchedText[:colonIdx]
	message := ""
	if loc[1] < len(line) {
		message = line[loc[1]:]
	}

	// Extract metadata from prefix
	entry := &LogEntryData{
		Severity: normalizeSeverity(severity),
		Message:  message,
		RawLine:  line,
	}

	// Extract timestamp
	if ts := timestampPattern.FindString(prefix); ts != "" {
		entry.Timestamp, _ = parseTimestamp(ts)
	}

	// Extract PID - find the first [number] pattern
	if pidMatch := pidPattern.FindStringSubmatch(prefix); pidMatch != nil {
		entry.PID, _ = strconv.Atoi(pidMatch[1])
	}

	// Extract user, database, application from bracketed sections
	// Look for patterns like [app], [user@host], [user@db]
	extractBracketedMetadata(prefix, entry)

	return entry
}

// extractBracketedMetadata extracts user, database, and application from prefix.
func extractBracketedMetadata(prefix string, entry *LogEntryData) {
	// Find all bracketed sections after the PID
	// Skip the first [number] which is the PID
	pidLoc := pidPattern.FindStringIndex(prefix)
	if pidLoc == nil {
		return
	}

	remaining := prefix[pidLoc[1]:]

	// Parse bracketed sections - handle nested brackets like [brandon@[local]]
	var brackets []string
	i := 0
	for i < len(remaining) {
		if remaining[i] == '[' {
			// Find matching close bracket, handling nesting
			depth := 1
			start := i + 1
			j := start
			for j < len(remaining) && depth > 0 {
				switch remaining[j] {
				case '[':
					depth++
				case ']':
					depth--
				}
				j++
			}
			if depth == 0 {
				brackets = append(brackets, remaining[start:j-1])
			}
			i = j
		} else {
			i++
		}
	}

	// Interpret bracketed values based on content
	for _, b := range brackets {
		// Skip if it looks like a PID (pure number)
		if _, err := strconv.Atoi(b); err == nil {
			continue
		}

		// Check for user@host or user@db pattern
		if atIdx := strings.Index(b, "@"); atIdx > 0 {
			entry.User = b[:atIdx]
			// Don't set database/host - could be either
			continue
		}

		// Otherwise treat as application name if not already set
		if entry.Application == "" && b != "" {
			entry.Application = b
		}
	}
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
			Timestamp: timestamp,
			User:      record[1],
			Database:  record[2],
			PID:       pid,
			Severity:  normalizeSeverity(record[11]),
			Message:   record[13],
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
