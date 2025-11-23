package monitors

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// CSVLogParser parses PostgreSQL CSV format log files for deadlock events.
type CSVLogParser struct {
	logDir       string
	logPattern   string
	store        *sqlite.DeadlockStore
	dbName       string
	sessionCache *SessionCache
	lastPosition map[string]int64
	mu           sync.Mutex
}

// CSV column indices (PostgreSQL 18 format)
const (
	csvLogTime          = 0
	csvUserName         = 1
	csvDatabaseName     = 2
	csvProcessID        = 3
	csvConnectionFrom   = 4
	csvSessionStartTime = 8
	csvErrorSeverity    = 11
	csvMessage          = 13
	csvDetail           = 14
	csvApplicationName  = 22
)

// NewCSVLogParser creates a new CSV log parser.
func NewCSVLogParser(logDir, logPattern string, store *sqlite.DeadlockStore, dbName string, sessionCache *SessionCache) *CSVLogParser {
	return &CSVLogParser{
		logDir:       logDir,
		logPattern:   logPattern,
		store:        store,
		dbName:       dbName,
		sessionCache: sessionCache,
		lastPosition: make(map[string]int64),
	}
}

// ParseNewEntries scans CSV log files for new deadlock events.
func (p *CSVLogParser) ParseNewEntries(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find log files matching pattern (look for .csv files)
	pattern := strings.TrimSuffix(p.logPattern, ".log") + ".csv"
	files, err := filepath.Glob(filepath.Join(p.logDir, pattern))
	if err != nil {
		return 0, fmt.Errorf("glob log files: %w", err)
	}

	totalParsed := 0
	for _, file := range files {
		count, err := p.parseFile(ctx, file)
		if err != nil {
			continue
		}
		totalParsed += count
	}

	return totalParsed, nil
}

// SetPositions sets the initial file positions from persisted storage.
func (p *CSVLogParser) SetPositions(positions map[string]int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range positions {
		p.lastPosition[k] = v
	}
}

// GetPositions returns the current file positions for persistence.
func (p *CSVLogParser) GetPositions() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make(map[string]int64, len(p.lastPosition))
	for k, v := range p.lastPosition {
		result[k] = v
	}
	return result
}

// ResetPositions clears all file positions to start fresh.
func (p *CSVLogParser) ResetPositions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastPosition = make(map[string]int64)
}

// parseFile parses a single CSV log file for deadlock events.
func (p *CSVLogParser) parseFile(ctx context.Context, filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Seek to last known position
	lastPos := p.lastPosition[filePath]
	if lastPos > 0 {
		_, err = file.Seek(lastPos, 0)
		if err != nil {
			lastPos = 0
		}
	}

	reader := csv.NewReader(bufio.NewReader(file))
	reader.FieldsPerRecord = -1 // Allow variable fields
	reader.LazyQuotes = true

	parsed := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		// Need at least message field
		if len(record) <= csvMessage {
			continue
		}

		// Check for deadlock detected
		severity := ""
		if len(record) > csvErrorSeverity {
			severity = record[csvErrorSeverity]
		}
		message := record[csvMessage]

		if severity == "ERROR" && strings.Contains(message, "deadlock detected") {
			event, err := p.parseDeadlockRecord(record)
			if err != nil {
				continue
			}

			if _, err := p.store.InsertEvent(ctx, event); err == nil {
				parsed++
			}
		}
	}

	// Update position
	pos, _ := file.Seek(0, 1)
	p.lastPosition[filePath] = pos

	return parsed, nil
}

// parseDeadlockRecord parses a CSV record into a deadlock event.
func (p *CSVLogParser) parseDeadlockRecord(record []string) (*sqlite.DeadlockEvent, error) {
	// Parse timestamp
	detectedAt, _ := time.Parse("2006-01-02 15:04:05.000 MST", record[csvLogTime])
	if detectedAt.IsZero() {
		detectedAt = time.Now()
	}

	// Parse PID
	pid, _ := strconv.Atoi(record[csvProcessID])

	// Parse session start time (backend_start)
	var backendStart *time.Time
	if len(record) > csvSessionStartTime && record[csvSessionStartTime] != "" {
		if bs, err := time.Parse("2006-01-02 15:04:05 MST", record[csvSessionStartTime]); err == nil {
			backendStart = &bs
		}
	}

	// Get metadata
	username := ""
	if len(record) > csvUserName {
		username = record[csvUserName]
	}
	dbName := p.dbName
	if len(record) > csvDatabaseName && record[csvDatabaseName] != "" {
		dbName = record[csvDatabaseName]
	}
	applicationName := ""
	if len(record) > csvApplicationName {
		applicationName = record[csvApplicationName]
	}
	clientAddr := "local"
	if len(record) > csvConnectionFrom && record[csvConnectionFrom] != "" {
		// Format is "host:port" or empty for local
		addr := record[csvConnectionFrom]
		if idx := strings.LastIndex(addr, ":"); idx > 0 {
			addr = addr[:idx]
		}
		if addr != "" {
			clientAddr = addr
		}
	}

	event := &sqlite.DeadlockEvent{
		DetectedAt:   detectedAt,
		DatabaseName: dbName,
		ResolvedByPID: &pid,
	}

	// Parse DETAIL for process info
	detail := ""
	if len(record) > csvDetail {
		detail = record[csvDetail]
	}

	if detail != "" {
		processes := p.parseDetail(detail, username, applicationName, clientAddr, backendStart)
		event.Processes = processes
	}

	return event, nil
}

// parseDetail parses the DETAIL field which contains process and query info.
func (p *CSVLogParser) parseDetail(detail, username, applicationName, clientAddr string, backendStart *time.Time) []sqlite.DeadlockProcess {
	var processes []sqlite.DeadlockProcess
	detailInfos := make(map[int]*detailInfo)

	lines := strings.Split(detail, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse "waits for" lines
		if info := extractDetailInfo(line); info != nil {
			detailInfos[info.pid] = info
			continue
		}

		// Parse "Process PID: QUERY" lines
		if proc := parseDeadlockProcessLine(line); proc != nil {
			dp := sqlite.DeadlockProcess{
				PID:             proc.pid,
				Query:           proc.query,
				Username:        username,
				ApplicationName: applicationName,
				ClientAddr:      clientAddr,
				BackendStart:    backendStart,
			}

			// Apply detail info
			if info, ok := detailInfos[proc.pid]; ok {
				dp.LockMode = info.lockMode
				dp.LockType = info.lockType
				dp.BlockedByPID = info.blockedByPID
			}

			// Extract relation from query
			if rel := extractRelationFromQuery(proc.query); rel != "" {
				dp.RelationName = rel
			}

			// Generate fingerprint
			if fingerprint := generateFingerprint(proc.query); fingerprint != 0 {
				dp.QueryFingerprint = &fingerprint
			}

			// Try to get xact_start from session cache
			if p.sessionCache != nil {
				if sessionState := p.sessionCache.GetSessionState(proc.pid); sessionState != nil {
					dp.XactStart = sessionState.XactStart
				}
			}

			processes = append(processes, dp)
		}
	}

	return processes
}
