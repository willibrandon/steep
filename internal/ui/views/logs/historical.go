// Package logs provides the Log Viewer view.
package logs

import (
	"bufio"
	"context"
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

// LogFileInfo contains metadata about a discovered log file.
type LogFileInfo struct {
	Path           string             // Absolute path to log file
	Name           string             // Filename (for display)
	Size           int64              // File size in bytes
	ModTime        time.Time          // Last modification time
	Format         monitors.LogFormat // Detected log format (CSV, JSON, Stderr)
	FirstTimestamp *time.Time         // Timestamp of first entry (if known)
	LastTimestamp  *time.Time         // Timestamp of last entry (if known)
}

// HistoricalLogRequest represents a request to load logs from a specific timestamp.
type HistoricalLogRequest struct {
	Timestamp time.Time     // Target timestamp to navigate to
	TimeRange time.Duration // How much context to load around timestamp (default: 5 min)
	Format    string        // Original user input format for error messages
}

// HistoricalLogResult contains the result of a historical log request.
type HistoricalLogResult struct {
	Entries        []LogEntryData // Loaded log entries
	ActualTime     time.Time      // Actual timestamp found (may differ from requested)
	FromFile       string         // Which file the entries came from
	TargetIndex    int            // Index of entry closest to requested timestamp
	Message        string         // Informational message about the result
	OutsideBuffer  bool           // True if data came from historical file, not buffer
	Error          error
}

// HistoricalLoader handles loading historical log entries from disk.
type HistoricalLoader struct {
	logDir     string
	logPattern string
	format     monitors.LogFormat

	// Cache of discovered log files
	logFiles []LogFileInfo
}

// NewHistoricalLoader creates a new historical log loader.
func NewHistoricalLoader(source *monitors.LogSource) *HistoricalLoader {
	return &HistoricalLoader{
		logDir:     source.LogDir,
		logPattern: source.LogPattern,
		format:     source.Format,
	}
}

// DiscoverLogFiles scans the log directory and returns all available log files.
func (h *HistoricalLoader) DiscoverLogFiles() ([]LogFileInfo, error) {
	if h.logDir == "" || h.logPattern == "" {
		return nil, fmt.Errorf("log directory or pattern not configured")
	}

	pattern := filepath.Join(h.logDir, h.logPattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("error matching log pattern %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, nil
	}

	var files []LogFileInfo
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		format := DetectFormat(path)
		logFile := LogFileInfo{
			Path:    path,
			Name:    filepath.Base(path),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Format:  format,
		}

		files = append(files, logFile)
	}

	// Sort by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	h.logFiles = files
	return files, nil
}

// GetTimestampRanges reads the first and last timestamps from each log file.
// This is used to quickly determine which file contains a given timestamp.
func (h *HistoricalLoader) GetTimestampRanges(ctx context.Context, files []LogFileInfo) []LogFileInfo {
	result := make([]LogFileInfo, len(files))
	copy(result, files)

	for i := range result {
		if ctx.Err() != nil {
			break
		}

		first, last := h.readFileTimestampRange(result[i].Path, result[i].Format)
		if first != nil {
			result[i].FirstTimestamp = first
		}
		if last != nil {
			result[i].LastTimestamp = last
		}
	}

	return result
}

// readFileTimestampRange reads the first and last timestamps from a log file.
func (h *HistoricalLoader) readFileTimestampRange(path string, format monitors.LogFormat) (*time.Time, *time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var first, last *time.Time

	switch format {
	case monitors.LogFormatCSV:
		first, last = h.readCSVTimestampRange(f)
	case monitors.LogFormatJSON:
		first, last = h.readJSONTimestampRange(f)
	default:
		first, last = h.readStderrTimestampRange(f)
	}

	return first, last
}

// readStderrTimestampRange reads first/last timestamps from stderr format log.
func (h *HistoricalLoader) readStderrTimestampRange(f *os.File) (*time.Time, *time.Time) {
	var first, last *time.Time

	// Read first few lines for first timestamp
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() && lineCount < 10 {
		line := scanner.Text()
		if ts := extractTimestampFromLine(line); ts != nil {
			first = ts
			break
		}
		lineCount++
	}

	// Seek to end and read backwards for last timestamp
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return first, first
	}

	// Read last 4KB for last timestamp
	readSize := int64(4096)
	if info.Size() < readSize {
		readSize = info.Size()
	}

	_, _ = f.Seek(-readSize, io.SeekEnd)
	scanner = bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if ts := extractTimestampFromLine(line); ts != nil {
			last = ts
		}
	}

	if last == nil {
		last = first
	}

	return first, last
}

// readJSONTimestampRange reads first/last timestamps from JSON format log.
func (h *HistoricalLoader) readJSONTimestampRange(f *os.File) (*time.Time, *time.Time) {
	var first, last *time.Time

	// Read first line for first timestamp
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(scanner.Text()), &entry) == nil {
			if ts, err := parseTimestamp(entry.Timestamp); err == nil && !ts.IsZero() {
				first = &ts
			}
		}
	}

	// Seek to end and read backwards for last timestamp
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return first, first
	}

	readSize := int64(4096)
	if info.Size() < readSize {
		readSize = info.Size()
	}

	_, _ = f.Seek(-readSize, io.SeekEnd)
	scanner = bufio.NewScanner(f)
	for scanner.Scan() {
		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(scanner.Text()), &entry) == nil {
			if ts, err := parseTimestamp(entry.Timestamp); err == nil && !ts.IsZero() {
				last = &ts
			}
		}
	}

	if last == nil {
		last = first
	}

	return first, last
}

// readCSVTimestampRange reads first/last timestamps from CSV format log.
func (h *HistoricalLoader) readCSVTimestampRange(f *os.File) (*time.Time, *time.Time) {
	var first, last *time.Time

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	// Read first record
	if record, err := reader.Read(); err == nil && len(record) > 0 {
		if ts, err := parseTimestamp(record[0]); err == nil && !ts.IsZero() {
			first = &ts
		}
	}

	// Read all records to find last (CSV doesn't support seeking well)
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}
		if len(record) > 0 {
			if ts, err := parseTimestamp(record[0]); err == nil && !ts.IsZero() {
				last = &ts
			}
		}
	}

	if last == nil {
		last = first
	}

	return first, last
}

// extractTimestampFromLine extracts a timestamp from a stderr log line.
func extractTimestampFromLine(line string) *time.Time {
	// PostgreSQL stderr format typically starts with timestamp
	// e.g., "2025-11-27 12:34:56 PST"
	if len(line) < 19 {
		return nil
	}

	// Try to parse the first part as a timestamp
	for _, length := range []int{26, 23, 19} {
		if len(line) < length {
			continue
		}
		prefix := line[:length]
		if ts, err := parseTimestamp(prefix); err == nil && !ts.IsZero() {
			return &ts
		}
	}

	return nil
}

// FindLogFileForTimestamp finds the log file(s) that should contain the given timestamp.
func (h *HistoricalLoader) FindLogFileForTimestamp(ts time.Time, files []LogFileInfo) []LogFileInfo {
	var candidates []LogFileInfo

	for _, f := range files {
		// If we have timestamp ranges, use them
		if f.FirstTimestamp != nil && f.LastTimestamp != nil {
			// Check if timestamp falls within file's range (with some margin)
			margin := 5 * time.Minute
			start := f.FirstTimestamp.Add(-margin)
			end := f.LastTimestamp.Add(margin)

			if (ts.Equal(start) || ts.After(start)) && (ts.Equal(end) || ts.Before(end)) {
				candidates = append(candidates, f)
			}
		} else {
			// Fall back to modification time heuristic
			// File could contain logs from before its mod time
			if ts.Before(f.ModTime) || ts.Equal(f.ModTime) {
				candidates = append(candidates, f)
			}
		}
	}

	// If no exact matches, find the closest file
	if len(candidates) == 0 && len(files) > 0 {
		// Find the file with the closest timestamp
		var closestFile LogFileInfo
		var closestDiff time.Duration = -1

		for _, f := range files {
			var fileMid time.Time
			if f.FirstTimestamp != nil && f.LastTimestamp != nil {
				fileMid = f.FirstTimestamp.Add(f.LastTimestamp.Sub(*f.FirstTimestamp) / 2)
			} else {
				fileMid = f.ModTime
			}

			diff := ts.Sub(fileMid)
			if diff < 0 {
				diff = -diff
			}

			if closestDiff < 0 || diff < closestDiff {
				closestDiff = diff
				closestFile = f
			}
		}

		candidates = []LogFileInfo{closestFile}
	}

	return candidates
}

// LoadHistoricalEntries loads log entries from a specific file around a timestamp.
func (h *HistoricalLoader) LoadHistoricalEntries(ctx context.Context, file LogFileInfo, targetTime time.Time, maxEntries int, direction SearchDirection) (*HistoricalLogResult, error) {
	if maxEntries <= 0 {
		maxEntries = 500 // Default: load up to 500 entries around the timestamp
	}

	f, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("error opening log file %s: %w", file.Path, err)
	}
	defer f.Close()

	// Load ALL entries from the file first, then filter around target
	// This is simpler and more reliable than trying to seek
	var allEntries []LogEntryData

	switch file.Format {
	case monitors.LogFormatCSV:
		allEntries, err = h.loadCSVEntries(f, -1) // -1 = no limit
	case monitors.LogFormatJSON:
		allEntries, err = h.loadJSONEntries(f, -1)
	default:
		allEntries, err = h.loadStderrEntries(f, -1)
	}

	if err != nil {
		return nil, err
	}

	if len(allEntries) == 0 {
		return &HistoricalLogResult{
			FromFile:      file.Name,
			OutsideBuffer: true,
			Message:       "No entries found in " + file.Name,
		}, nil
	}

	// Find the entry based on direction
	targetIndex := findEntryIndex(allEntries, targetTime, direction)

	// Handle case where no entry found in requested direction
	if targetIndex < 0 {
		var msg string
		switch direction {
		case SearchAfter:
			msg = fmt.Sprintf("No entries at or after %s in %s",
				targetTime.Format("2006-01-02 15:04:05"), file.Name)
		case SearchBefore:
			msg = fmt.Sprintf("No entries at or before %s in %s",
				targetTime.Format("2006-01-02 15:04:05"), file.Name)
		default:
			msg = "No matching entries found in " + file.Name
		}
		return &HistoricalLogResult{
			FromFile:      file.Name,
			OutsideBuffer: true,
			Message:       msg,
		}, nil
	}

	// Extract entries around the target based on direction
	var startIdx, endIdx int
	switch direction {
	case SearchAfter:
		// Show entries starting from target
		startIdx = targetIndex
		endIdx = targetIndex + maxEntries
	case SearchBefore:
		// Show entries ending at target
		startIdx = targetIndex - maxEntries + 1
		endIdx = targetIndex + 1
	default:
		// Center around target
		halfWindow := maxEntries / 2
		startIdx = targetIndex - halfWindow
		endIdx = targetIndex + halfWindow
	}

	// Clamp to valid range
	if startIdx < 0 {
		endIdx -= startIdx
		startIdx = 0
	}
	if endIdx > len(allEntries) {
		startIdx -= (endIdx - len(allEntries))
		endIdx = len(allEntries)
	}
	if startIdx < 0 {
		startIdx = 0
	}

	entries := allEntries[startIdx:endIdx]
	// Adjust targetIndex relative to the new slice
	adjustedTargetIndex := targetIndex - startIdx

	result := &HistoricalLogResult{
		Entries:       entries,
		FromFile:      file.Name,
		TargetIndex:   adjustedTargetIndex,
		OutsideBuffer: true,
	}

	if adjustedTargetIndex >= 0 && adjustedTargetIndex < len(entries) {
		result.ActualTime = entries[adjustedTargetIndex].Timestamp
		diff := targetTime.Sub(result.ActualTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Minute {
			var directionHint string
			switch direction {
			case SearchAfter:
				directionHint = "first entry at/after"
			case SearchBefore:
				directionHint = "last entry at/before"
			default:
				directionHint = "nearest entry"
			}
			result.Message = fmt.Sprintf("Found %s %s (requested: %s)",
				directionHint,
				result.ActualTime.Format("2006-01-02 15:04:05"),
				targetTime.Format("2006-01-02 15:04:05"))
		}
	} else if len(entries) > 0 {
		result.ActualTime = entries[0].Timestamp
	}

	return result, nil
}

// loadStderrEntries loads entries from a stderr format log file.
// If maxEntries is -1, load all entries.
func (h *HistoricalLoader) loadStderrEntries(f *os.File, maxEntries int) ([]LogEntryData, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(f)

	// Increase scanner buffer for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentEntry *LogEntryData
	var unparsedCount int
	var firstUnparsed string
	noLimit := maxEntries < 0

	for scanner.Scan() {
		line := scanner.Text()

		// Try to parse as a new entry
		entry := parseStderrLine(line)
		if entry != nil {
			if currentEntry != nil {
				entries = append(entries, *currentEntry)
			}
			currentEntry = entry
		} else if currentEntry != nil {
			// Continuation line
			if strings.HasPrefix(line, "DETAIL:") {
				currentEntry.Detail = strings.TrimSpace(strings.TrimPrefix(line, "DETAIL:"))
			} else if strings.HasPrefix(line, "HINT:") {
				currentEntry.Hint = strings.TrimSpace(strings.TrimPrefix(line, "HINT:"))
			} else if strings.TrimSpace(line) != "" {
				currentEntry.Message += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			// Truly unparsed line
			unparsedCount++
			if firstUnparsed == "" {
				firstUnparsed = truncateLine(line, 100)
			}
		}

		// Limit entries (unless noLimit)
		if !noLimit && len(entries) >= maxEntries {
			break
		}
	}

	if currentEntry != nil && (noLimit || len(entries) < maxEntries) {
		entries = append(entries, *currentEntry)
	}

	// Log unparsed lines
	if unparsedCount > 0 {
		logger.Warn("historical: failed to parse log lines",
			"unparsed_count", unparsedCount,
			"first_line", firstUnparsed)
	}

	return entries, scanner.Err()
}

// loadJSONEntries loads entries from a JSON format log file.
// If maxEntries is -1, load all entries.
func (h *HistoricalLoader) loadJSONEntries(f *os.File, maxEntries int) ([]LogEntryData, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(f)

	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var jsonErrors int
	var firstError string
	noLimit := maxEntries < 0

	for scanner.Scan() {
		if !noLimit && len(entries) >= maxEntries {
			break
		}

		line := scanner.Text()

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
	}

	// Log JSON parse errors
	if jsonErrors > 0 {
		logger.Warn("historical: JSON parse errors",
			"count", jsonErrors,
			"first_line", firstError)
	}

	return entries, scanner.Err()
}

// loadCSVEntries loads entries from a CSV format log file.
// If maxEntries is -1, load all entries.
func (h *HistoricalLoader) loadCSVEntries(f *os.File, maxEntries int) ([]LogEntryData, error) {
	var entries []LogEntryData
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	noLimit := maxEntries < 0

	var malformedCount int
	var shortRecordCount int

	for {
		if !noLimit && len(entries) >= maxEntries {
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			malformedCount++
			continue
		}

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

	// Log CSV parse errors
	if malformedCount > 0 || shortRecordCount > 0 {
		logger.Warn("historical: CSV log parse errors",
			"malformed", malformedCount,
			"short_records", shortRecordCount)
	}

	return entries, nil
}

// parseStderrLine attempts to parse a stderr log line into a LogEntryData.
func parseStderrLine(line string) *LogEntryData {
	// Try each pattern
	for i, re := range stderrPatterns {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		switch i {
		case 0: // [app] [user@host] format
			ts, _ := parseTimestamp(m[1])
			pid, _ := strconv.Atoi(m[3])
			user := ""
			if idx := strings.Index(m[5], "@"); idx > 0 {
				user = m[5][:idx]
			}
			return &LogEntryData{
				Timestamp:   ts,
				Severity:    normalizeSeverity(m[6]),
				PID:         pid,
				User:        user,
				Application: m[4],
				Message:     m[7],
				RawLine:     line,
			}
		case 1: // user@db format
			ts, _ := parseTimestamp(m[1])
			pid, _ := strconv.Atoi(m[3])
			return &LogEntryData{
				Timestamp: ts,
				Severity:  normalizeSeverity(m[6]),
				PID:       pid,
				User:      m[4],
				Database:  m[5],
				Message:   m[7],
				RawLine:   line,
			}
		case 2: // simple format
			ts, _ := parseTimestamp(m[1])
			pid, _ := strconv.Atoi(m[3])
			return &LogEntryData{
				Timestamp: ts,
				Severity:  normalizeSeverity(m[4]),
				PID:       pid,
				Message:   m[5],
				RawLine:   line,
			}
		}
	}

	return nil
}

// stderrPatterns are compiled regex patterns for stderr log parsing.
var stderrPatterns []*regexp.Regexp

var stderrPatternsOnce sync.Once

func init() {
	stderrPatternsOnce.Do(func() {
		stderrPatterns = []*regexp.Regexp{
			// Format with [app] [user@host]
			regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\w+)\s+\[(\d+)\]\s+\[([^\]]*)\]\s+\[([^\]]*)\]\s+(\w+):\s*(.*)$`),
			// Format with user@db
			regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\w+)\s+\[(\d+)\]\s+(\w+)@(\w+)\s+(\w+):\s*(.*)$`),
			// Simple format
			regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\w+)\s+\[(\d+)\]\s+(\w+):\s*(.*)$`),
		}
	})
}

// findClosestEntryIndex finds the index of the entry closest to the target time.
func findClosestEntryIndex(entries []LogEntryData, target time.Time) int {
	return findEntryIndex(entries, target, SearchClosest)
}

// findEntryIndex finds an entry index based on the search direction.
// - SearchClosest: finds the entry closest to the target time
// - SearchAfter: finds the first entry at or after the target time
// - SearchBefore: finds the last entry at or before the target time
func findEntryIndex(entries []LogEntryData, target time.Time, direction SearchDirection) int {
	if len(entries) == 0 {
		return -1
	}

	// Binary search for the first entry after target
	idx := sort.Search(len(entries), func(i int) bool {
		return entries[i].Timestamp.After(target)
	})

	switch direction {
	case SearchAfter:
		// Find first entry at or after target
		// idx is the first entry strictly after target
		// Check if idx-1 is exactly at target
		if idx > 0 && !entries[idx-1].Timestamp.Before(target) {
			return idx - 1
		}
		if idx >= len(entries) {
			return -1 // No entry at or after target
		}
		return idx

	case SearchBefore:
		// Find last entry at or before target
		if idx == 0 {
			return -1 // No entry at or before target
		}
		return idx - 1

	default: // SearchClosest
		if idx == 0 {
			return 0
		}
		if idx >= len(entries) {
			return len(entries) - 1
		}

		// Check which is closer: idx or idx-1
		diffBefore := target.Sub(entries[idx-1].Timestamp)
		diffAfter := entries[idx].Timestamp.Sub(target)

		if diffBefore < 0 {
			diffBefore = -diffBefore
		}
		if diffAfter < 0 {
			diffAfter = -diffAfter
		}

		if diffBefore <= diffAfter {
			return idx - 1
		}
		return idx
	}
}
