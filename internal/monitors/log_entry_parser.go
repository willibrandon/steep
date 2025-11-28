package monitors

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// LogEntryParser provides functions to parse log files into LogEntry models.
// It adapts the existing CSV and JSON log parsing infrastructure for general log viewing.

// ParseJSONLogEntry parses a JSON log line into a LogEntry.
func ParseJSONLogEntry(line []byte, sourceFile string, offset int64) (models.LogEntry, error) {
	var jsonEntry JSONLogEntry
	if err := json.Unmarshal(line, &jsonEntry); err != nil {
		return models.LogEntry{}, err
	}

	ts, _ := time.Parse("2006-01-02 15:04:05.000 MST", jsonEntry.Timestamp)
	if ts.IsZero() {
		ts, _ = time.Parse("2006-01-02 15:04:05 MST", jsonEntry.Timestamp)
	}
	if ts.IsZero() {
		ts, _ = time.Parse(time.RFC3339, jsonEntry.Timestamp)
	}

	return models.LogEntry{
		Timestamp:   ts,
		Severity:    models.ParseSeverity(jsonEntry.ErrorSeverity),
		PID:         jsonEntry.Pid,
		Database:    jsonEntry.Dbname,
		User:        jsonEntry.User,
		Application: jsonEntry.ApplicationName,
		Message:     jsonEntry.Message,
		Detail:      jsonEntry.Detail,
		Hint:        jsonEntry.Hint,
		RawLine:     string(line),
		SourceFile:  sourceFile,
		SourceLine:  offset,
	}, nil
}

// ParseCSVLogEntry parses a CSV record into a LogEntry.
func ParseCSVLogEntry(record []string, sourceFile string, offset int64) (models.LogEntry, error) {
	if len(record) < 15 {
		return models.LogEntry{}, io.EOF // Not enough fields
	}

	// Parse timestamp
	ts, _ := time.Parse("2006-01-02 15:04:05.000 MST", record[csvLogTime])
	if ts.IsZero() {
		ts, _ = time.Parse("2006-01-02 15:04:05 MST", record[csvLogTime])
	}

	// Parse PID
	pid, _ := strconv.Atoi(record[csvProcessID])

	// Get application name if available
	var appName string
	if len(record) > csvApplicationName {
		appName = record[csvApplicationName]
	}

	return models.LogEntry{
		Timestamp:   ts,
		Severity:    models.ParseSeverity(record[csvErrorSeverity]),
		PID:         pid,
		Database:    record[csvDatabaseName],
		User:        record[csvUserName],
		Application: appName,
		Message:     record[csvMessage],
		Detail:      record[csvDetail],
		RawLine:     strings.Join(record, ","),
		SourceFile:  sourceFile,
		SourceLine:  offset,
	}, nil
}

// ParseStderrLogLine parses a plain text (stderr) log line into a LogEntry.
// Assumes a log_line_prefix like: '%t [%p] %q%u@%d '
// Example: 2025-11-27 14:30:15 UTC [12345] postgres@mydb LOG:  statement: SELECT 1
func ParseStderrLogLine(line string, sourceFile string, offset int64) (models.LogEntry, error) {
	entry := models.LogEntry{
		RawLine:    line,
		SourceFile: sourceFile,
		SourceLine: offset,
		Severity:   models.SeverityInfo,
	}

	// Try to parse common log_line_prefix patterns
	// Pattern 1: "2025-11-27 14:30:15 UTC [12345] user@db SEVERITY: message"
	// Pattern 2: "2025-11-27 14:30:15.123 UTC [12345] SEVERITY: message"

	// Find timestamp (first 23-26 chars typically)
	if len(line) > 19 {
		// Try with milliseconds
		ts, err := time.Parse("2006-01-02 15:04:05.000 MST", line[:27])
		if err != nil {
			// Try without milliseconds
			ts, err = time.Parse("2006-01-02 15:04:05 MST", line[:23])
			if err == nil {
				entry.Timestamp = ts
			}
		} else {
			entry.Timestamp = ts
		}
	}

	// Find PID in brackets [12345]
	if pidStart := strings.Index(line, "["); pidStart >= 0 {
		if pidEnd := strings.Index(line[pidStart:], "]"); pidEnd > 0 {
			pidStr := line[pidStart+1 : pidStart+pidEnd]
			if pid, err := strconv.Atoi(pidStr); err == nil {
				entry.PID = pid
			}
		}
	}

	// Find user@db pattern
	if atIdx := strings.Index(line, "@"); atIdx > 0 {
		// Look backwards for space before user
		spaceIdx := strings.LastIndex(line[:atIdx], " ")
		if spaceIdx >= 0 {
			entry.User = line[spaceIdx+1 : atIdx]
		}
		// Look forward for space after db
		nextSpace := strings.Index(line[atIdx:], " ")
		if nextSpace > 0 {
			entry.Database = line[atIdx+1 : atIdx+nextSpace]
		}
	}

	// Find severity and message
	// Look for patterns like "LOG:", "ERROR:", "WARNING:", etc.
	severities := []string{"DEBUG", "INFO", "NOTICE", "LOG", "WARNING", "ERROR", "FATAL", "PANIC"}
	for _, sev := range severities {
		if idx := strings.Index(line, sev+":"); idx >= 0 {
			entry.Severity = models.ParseSeverity(sev)
			// Message is everything after "SEVERITY:  "
			msgStart := idx + len(sev) + 1
			for msgStart < len(line) && line[msgStart] == ' ' {
				msgStart++
			}
			entry.Message = line[msgStart:]
			break
		}
	}

	// If no severity found, use the whole line as message
	if entry.Message == "" {
		entry.Message = line
	}

	return entry, nil
}

// LogFileReader reads log entries from a file starting at a given position.
type LogFileReader struct {
	file       *os.File
	format     LogFormat
	sourceFile string
	position   int64
	reader     *bufio.Reader
	csvReader  *csv.Reader
}

// NewLogFileReader creates a new reader for the given log file.
func NewLogFileReader(filePath string, format LogFormat, startPos int64) (*LogFileReader, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	// Seek to start position
	if startPos > 0 {
		_, err = file.Seek(startPos, 0)
		if err != nil {
			file.Close()
			return nil, err
		}
	}

	reader := &LogFileReader{
		file:       file,
		format:     format,
		sourceFile: filePath,
		position:   startPos,
		reader:     bufio.NewReader(file),
	}

	if format == LogFormatCSV {
		reader.csvReader = csv.NewReader(reader.reader)
		reader.csvReader.FieldsPerRecord = -1 // Variable fields
		reader.csvReader.LazyQuotes = true
	}

	return reader, nil
}

// ReadEntry reads the next log entry.
// Returns io.EOF when no more entries are available.
func (r *LogFileReader) ReadEntry() (models.LogEntry, error) {
	startPos := r.position

	switch r.format {
	case LogFormatJSON:
		line, err := r.reader.ReadBytes('\n')
		if err != nil {
			return models.LogEntry{}, err
		}
		r.position += int64(len(line))
		return ParseJSONLogEntry(line, r.sourceFile, startPos)

	case LogFormatCSV:
		record, err := r.csvReader.Read()
		if err != nil {
			return models.LogEntry{}, err
		}
		// Estimate position (not exact for CSV due to buffering)
		for _, field := range record {
			r.position += int64(len(field) + 1) // +1 for comma/newline
		}
		return ParseCSVLogEntry(record, r.sourceFile, startPos)

	default: // LogFormatStderr
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return models.LogEntry{}, err
		}
		r.position += int64(len(line))
		return ParseStderrLogLine(strings.TrimSuffix(line, "\n"), r.sourceFile, startPos)
	}
}

// Position returns the current file position.
func (r *LogFileReader) Position() int64 {
	return r.position
}

// Close closes the file.
func (r *LogFileReader) Close() error {
	return r.file.Close()
}

// DetectLogFormat detects the log format from file content.
func DetectLogFormat(filePath string) LogFormat {
	// First try by extension
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".json":
		return LogFormatJSON
	case ".csv":
		return LogFormatCSV
	case ".log":
		// Could be stderr, need to check content
	}

	// Check file content
	file, err := os.Open(filePath)
	if err != nil {
		return LogFormatStderr
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	line, err := reader.ReadString('\n')
	if err != nil {
		return LogFormatStderr
	}

	// Check if it's JSON
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") {
		return LogFormatJSON
	}

	// Check if it looks like CSV (has many commas and possibly quoted fields)
	commaCount := strings.Count(line, ",")
	quoteCount := strings.Count(line, "\"")
	if commaCount > 5 && quoteCount >= 2 {
		return LogFormatCSV
	}

	return LogFormatStderr
}
