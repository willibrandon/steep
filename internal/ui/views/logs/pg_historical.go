// Package logs provides the Log Viewer view.
package logs

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors"
)

// PgHistoricalLoader handles loading historical log entries via pg_read_file.
type PgHistoricalLoader struct {
	pool       *pgxpool.Pool
	logDir     string
	logPattern string
	format     monitors.LogFormat
}

// NewPgHistoricalLoader creates a new pg_read_file-based historical loader.
func NewPgHistoricalLoader(pool *pgxpool.Pool, source *monitors.LogSource) *PgHistoricalLoader {
	return &PgHistoricalLoader{
		pool:       pool,
		logDir:     source.LogDir,
		logPattern: source.LogPattern,
		format:     source.Format,
	}
}

// DiscoverLogFiles lists log files using pg_ls_dir.
func (h *PgHistoricalLoader) DiscoverLogFiles(ctx context.Context) ([]LogFileInfo, error) {
	if h.logDir == "" || h.logPattern == "" {
		return nil, fmt.Errorf("log directory or pattern not configured")
	}

	// List files in log directory
	query := `/* steep:internal */ SELECT pg_ls_dir($1) ORDER BY 1`
	rows, err := h.pool.Query(ctx, query, h.logDir)
	if err != nil {
		return nil, fmt.Errorf("pg_ls_dir failed: %w", err)
	}
	defer rows.Close()

	var files []LogFileInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}

		// Match against pattern
		matched, err := filepath.Match(h.logPattern, name)
		if err != nil {
			continue
		}
		if !matched {
			continue
		}

		// Use path.Join (forward slashes) for paths sent to PostgreSQL server.
		// filepath.Join uses client OS separators which breaks when client
		// OS differs from server OS (e.g., Windows client, Linux server).
		fullPath := path.Join(h.logDir, name)

		// Get file info via pg_stat_file
		var size int64
		var modTime time.Time
		statQuery := `/* steep:internal */ SELECT size, modification FROM pg_stat_file($1)`
		err = h.pool.QueryRow(ctx, statQuery, fullPath).Scan(&size, &modTime)
		if err != nil {
			continue
		}

		format := DetectFormat(name)
		logFile := LogFileInfo{
			Path:    fullPath,
			Name:    name,
			Size:    size,
			ModTime: modTime,
			Format:  format,
		}
		files = append(files, logFile)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	return files, nil
}

// GetTimestampRanges reads first/last timestamps from each log file.
func (h *PgHistoricalLoader) GetTimestampRanges(ctx context.Context, files []LogFileInfo) []LogFileInfo {
	result := make([]LogFileInfo, len(files))
	copy(result, files)

	for i := range result {
		if ctx.Err() != nil {
			break
		}

		first, last := h.readFileTimestampRange(ctx, result[i].Path, result[i].Format, result[i].Size)
		if first != nil {
			result[i].FirstTimestamp = first
		}
		if last != nil {
			result[i].LastTimestamp = last
		}
	}

	return result
}

// readFileTimestampRange reads first/last timestamps using pg_read_file.
func (h *PgHistoricalLoader) readFileTimestampRange(ctx context.Context, path string, format monitors.LogFormat, fileSize int64) (*time.Time, *time.Time) {
	// Read first 4KB for first timestamp
	firstContent, err := h.readFileChunk(ctx, path, 0, 4096)
	if err != nil {
		return nil, nil
	}

	var first *time.Time
	switch format {
	case monitors.LogFormatCSV:
		first = h.extractFirstCSVTimestamp(firstContent)
	case monitors.LogFormatJSON:
		first = h.extractFirstJSONTimestamp(firstContent)
	default:
		first = h.extractFirstStderrTimestamp(firstContent)
	}

	// Read last 4KB for last timestamp
	var lastOffset int64
	if fileSize > 4096 {
		lastOffset = fileSize - 4096
	}
	lastContent, err := h.readFileChunk(ctx, path, lastOffset, 4096)
	if err != nil {
		return first, first
	}

	var last *time.Time
	switch format {
	case monitors.LogFormatCSV:
		last = h.extractLastCSVTimestamp(lastContent)
	case monitors.LogFormatJSON:
		last = h.extractLastJSONTimestamp(lastContent)
	default:
		last = h.extractLastStderrTimestamp(lastContent)
	}

	if last == nil {
		last = first
	}

	return first, last
}

// readFileChunk reads a chunk of a file using pg_read_file.
func (h *PgHistoricalLoader) readFileChunk(ctx context.Context, path string, offset, length int64) (string, error) {
	query := `/* steep:internal */ SELECT pg_read_file($1, $2, $3)`
	var content string
	err := h.pool.QueryRow(ctx, query, path, offset, length).Scan(&content)
	return content, err
}

// extractFirstJSONTimestamp extracts the first timestamp from JSON content.
func (h *PgHistoricalLoader) extractFirstJSONTimestamp(content string) *time.Time {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(scanner.Text()), &entry) == nil {
			if ts, err := parseTimestamp(entry.Timestamp); err == nil && !ts.IsZero() {
				return &ts
			}
		}
	}
	return nil
}

// extractLastJSONTimestamp extracts the last timestamp from JSON content.
func (h *PgHistoricalLoader) extractLastJSONTimestamp(content string) *time.Time {
	var last *time.Time
	scanner := bufio.NewScanner(strings.NewReader(content))
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
	return last
}

// extractFirstStderrTimestamp extracts the first timestamp from stderr content.
func (h *PgHistoricalLoader) extractFirstStderrTimestamp(content string) *time.Time {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		if ts := extractTimestampFromLine(scanner.Text()); ts != nil {
			return ts
		}
	}
	return nil
}

// extractLastStderrTimestamp extracts the last timestamp from stderr content.
func (h *PgHistoricalLoader) extractLastStderrTimestamp(content string) *time.Time {
	var last *time.Time
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		if ts := extractTimestampFromLine(scanner.Text()); ts != nil {
			last = ts
		}
	}
	return last
}

// extractFirstCSVTimestamp extracts the first timestamp from CSV content.
func (h *PgHistoricalLoader) extractFirstCSVTimestamp(content string) *time.Time {
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	if record, err := reader.Read(); err == nil && len(record) > 0 {
		if ts, err := parseTimestamp(record[0]); err == nil && !ts.IsZero() {
			return &ts
		}
	}
	return nil
}

// extractLastCSVTimestamp extracts the last timestamp from CSV content.
func (h *PgHistoricalLoader) extractLastCSVTimestamp(content string) *time.Time {
	var last *time.Time
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

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
	return last
}

// FindLogFileForTimestamp finds files containing the given timestamp.
func (h *PgHistoricalLoader) FindLogFileForTimestamp(ts time.Time, files []LogFileInfo) []LogFileInfo {
	// Reuse the same logic as HistoricalLoader
	var candidates []LogFileInfo

	for _, f := range files {
		if f.FirstTimestamp != nil && f.LastTimestamp != nil {
			margin := 5 * time.Minute
			start := f.FirstTimestamp.Add(-margin)
			end := f.LastTimestamp.Add(margin)

			if (ts.Equal(start) || ts.After(start)) && (ts.Equal(end) || ts.Before(end)) {
				candidates = append(candidates, f)
			}
		} else {
			if ts.Before(f.ModTime) || ts.Equal(f.ModTime) {
				candidates = append(candidates, f)
			}
		}
	}

	if len(candidates) == 0 && len(files) > 0 {
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

// LoadHistoricalEntries loads log entries from a file using pg_read_file.
func (h *PgHistoricalLoader) LoadHistoricalEntries(ctx context.Context, file LogFileInfo, targetTime time.Time, maxEntries int, direction SearchDirection) (*HistoricalLogResult, error) {
	if maxEntries <= 0 {
		maxEntries = 500
	}

	// Read entire file content
	content, err := h.readFileChunk(ctx, file.Path, 0, file.Size)
	if err != nil {
		return nil, fmt.Errorf("error reading log file %s: %w", file.Path, err)
	}

	// Parse entries based on format
	var allEntries []LogEntryData
	switch file.Format {
	case monitors.LogFormatCSV:
		allEntries, err = h.parseCSVEntries(content)
	case monitors.LogFormatJSON:
		allEntries, err = h.parseJSONEntries(content)
	default:
		allEntries, err = h.parseStderrEntries(content)
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

	// Find entry based on direction
	targetIndex := findEntryIndex(allEntries, targetTime, direction)

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

	// Extract entries around target
	var startIdx, endIdx int
	switch direction {
	case SearchAfter:
		startIdx = targetIndex
		endIdx = targetIndex + maxEntries
	case SearchBefore:
		startIdx = targetIndex - maxEntries + 1
		endIdx = targetIndex + 1
	default:
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

// parseJSONEntries parses JSON log entries from content.
func (h *PgHistoricalLoader) parseJSONEntries(content string) ([]LogEntryData, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(strings.NewReader(content))

	// Increase buffer for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var jsonErrors int
	var firstError string

	for scanner.Scan() {
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
			Message:     strings.TrimRight(jsonEntry.Message, " \t\n\r"),
			Detail:      strings.TrimRight(jsonEntry.Detail, " \t\n\r"),
			Hint:        strings.TrimRight(jsonEntry.Hint, " \t\n\r"),
			Application: jsonEntry.Application,
			RawLine:     line,
		}
		entries = append(entries, entry)
	}

	if jsonErrors > 0 {
		logger.Warn("pg_historical: JSON parse errors",
			"count", jsonErrors,
			"first_line", firstError)
	}

	return entries, scanner.Err()
}

// parseStderrEntries parses stderr log entries from content.
func (h *PgHistoricalLoader) parseStderrEntries(content string) ([]LogEntryData, error) {
	var entries []LogEntryData
	scanner := bufio.NewScanner(strings.NewReader(content))

	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentEntry *LogEntryData
	var unparsedCount int
	var firstUnparsed string

	for scanner.Scan() {
		line := scanner.Text()

		entry := parseStderrLine(line)
		if entry != nil {
			if currentEntry != nil {
				currentEntry.Message = strings.TrimRight(currentEntry.Message, " \t\n\r")
				entries = append(entries, *currentEntry)
			}
			currentEntry = entry
		} else if currentEntry != nil {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "DETAIL:") {
				currentEntry.Detail = strings.TrimSpace(strings.TrimPrefix(trimmed, "DETAIL:"))
			} else if strings.HasPrefix(trimmed, "HINT:") {
				currentEntry.Hint = strings.TrimSpace(strings.TrimPrefix(trimmed, "HINT:"))
			} else if trimmed != "" {
				currentEntry.Message += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			unparsedCount++
			if firstUnparsed == "" {
				firstUnparsed = truncateLine(line, 100)
			}
		}
	}

	if currentEntry != nil {
		currentEntry.Message = strings.TrimRight(currentEntry.Message, " \t\n\r")
		entries = append(entries, *currentEntry)
	}

	if unparsedCount > 0 {
		logger.Warn("pg_historical: failed to parse log lines",
			"unparsed_count", unparsedCount,
			"first_line", firstUnparsed)
	}

	return entries, scanner.Err()
}

// parseCSVEntries parses CSV log entries from content.
func (h *PgHistoricalLoader) parseCSVEntries(content string) ([]LogEntryData, error) {
	var entries []LogEntryData
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	var malformedCount int
	var shortRecordCount int

	for {
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
			Message:   strings.TrimRight(record[13], " \t\n\r"),
		}

		if len(record) > 14 {
			entry.Detail = strings.TrimRight(record[14], " \t\n\r")
		}
		if len(record) > 15 {
			entry.Hint = strings.TrimRight(record[15], " \t\n\r")
		}

		entries = append(entries, entry)
	}

	if malformedCount > 0 || shortRecordCount > 0 {
		logger.Warn("pg_historical: CSV log parse errors",
			"malformed", malformedCount,
			"short_records", shortRecordCount)
	}

	return entries, nil
}
