package monitors

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// JSONLogParser parses PostgreSQL JSON format log files for deadlock events.
type JSONLogParser struct {
	logDir       string
	logPattern   string
	store        *sqlite.DeadlockStore
	dbName       string
	sessionCache *SessionCache
	lastPosition map[string]int64
	mu           sync.Mutex
	instanceName string // PostgreSQL instance name for multi-instance support
}

// JSONLogEntry represents a single JSON log entry from PostgreSQL.
type JSONLogEntry struct {
	Timestamp       string `json:"timestamp"`
	User            string `json:"user"`
	Dbname          string `json:"dbname"`
	Pid             int    `json:"pid"`
	RemoteHost      string `json:"remote_host"`
	RemotePort      int    `json:"remote_port"`
	SessionID       string `json:"session_id"`
	LineNum         int    `json:"line_num"`
	PS              string `json:"ps"`
	SessionStart    string `json:"session_start"`
	Vxid            string `json:"vxid"`
	Txid            int64  `json:"txid"`
	ErrorSeverity   string `json:"error_severity"`
	StateCode       string `json:"state_code"`
	Message         string `json:"message"`
	Detail          string `json:"detail"`
	Hint            string `json:"hint"`
	InternalQuery   string `json:"internal_query"`
	Context         string `json:"context"`
	Statement       string `json:"statement"`
	ApplicationName string `json:"application_name"`
	BackendType     string `json:"backend_type"`
	LeaderPid       int    `json:"leader_pid"`
	QueryID         int64  `json:"query_id"`
}

// NewJSONLogParser creates a new JSON log parser.
func NewJSONLogParser(logDir, logPattern string, store *sqlite.DeadlockStore, dbName string, sessionCache *SessionCache) *JSONLogParser {
	return &JSONLogParser{
		logDir:       logDir,
		logPattern:   logPattern,
		store:        store,
		dbName:       dbName,
		sessionCache: sessionCache,
		lastPosition: make(map[string]int64),
	}
}

// ParseNewEntries scans JSON log files for new deadlock events.
func (p *JSONLogParser) ParseNewEntries(ctx context.Context) (int, error) {
	return p.ParseNewEntriesWithProgress(ctx, nil)
}

// ParseNewEntriesWithProgress scans JSON log files with progress reporting.
func (p *JSONLogParser) ParseNewEntriesWithProgress(ctx context.Context, progress ProgressFunc) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find log files matching pattern (look for .json files)
	pattern := strings.TrimSuffix(p.logPattern, ".log") + ".json"
	fullPattern := filepath.Join(p.logDir, pattern)

	files, err := filepath.Glob(fullPattern)
	if err != nil {
		return 0, fmt.Errorf("glob log files: %w", err)
	}

	// Also check for pattern as-is if it includes .json
	if strings.HasSuffix(p.logPattern, ".json") {
		files2, _ := filepath.Glob(filepath.Join(p.logDir, p.logPattern))
		files = append(files, files2...)
	}

	totalFiles := len(files)
	totalParsed := 0
	for i, file := range files {
		if progress != nil {
			progress(i+1, totalFiles)
		}
		count, err := p.parseFile(ctx, file)
		if err != nil {
			continue
		}
		totalParsed += count
	}

	return totalParsed, nil
}

// SetPositions sets the initial file positions from persisted storage.
func (p *JSONLogParser) SetPositions(positions map[string]int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range positions {
		p.lastPosition[k] = v
	}
}

// GetPositions returns the current file positions for persistence.
func (p *JSONLogParser) GetPositions() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make(map[string]int64, len(p.lastPosition))
	for k, v := range p.lastPosition {
		result[k] = v
	}
	return result
}

// ResetPositions clears all file positions to start fresh.
func (p *JSONLogParser) ResetPositions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastPosition = make(map[string]int64)
}

// SetInstanceName sets the instance name for multi-instance support.
// Deadlocks will be tagged with this instance name when saved.
func (p *JSONLogParser) SetInstanceName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.instanceName = name
}

// parseFile parses a single JSON log file for deadlock events.
func (p *JSONLogParser) parseFile(ctx context.Context, filePath string) (int, error) {
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

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentDeadlock *jsonDeadlockState
	parsed := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry JSONLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Check for deadlock detected
		if entry.ErrorSeverity == "ERROR" && strings.Contains(entry.Message, "deadlock detected") {

			// Save previous deadlock if exists
			if currentDeadlock != nil && len(currentDeadlock.processes) > 0 {
				if err := p.saveDeadlock(ctx, currentDeadlock); err == nil {
					parsed++
				}
			}

			// Parse timestamp
			detectedAt, _ := time.Parse("2006-01-02 15:04:05.000 MST", entry.Timestamp)
			if detectedAt.IsZero() {
				detectedAt = time.Now()
			}

			// Parse session start (backend_start)
			var backendStart *time.Time
			if entry.SessionStart != "" {
				if bs, err := time.Parse("2006-01-02 15:04:05 MST", entry.SessionStart); err == nil {
					backendStart = &bs
				}
			}

			currentDeadlock = &jsonDeadlockState{
				detectedAt:      detectedAt,
				dbName:          entry.Dbname,
				resolvedByPID:   &entry.Pid,
				applicationName: entry.ApplicationName,
				username:        entry.User,
				clientAddr:      normalizeClientAddr(entry.RemoteHost),
				backendStart:    backendStart,
			}

			// Parse DETAIL for process info
			if entry.Detail != "" {
				p.parseDetail(currentDeadlock, entry.Detail)
			}
			continue
		}

		// End of deadlock block on next entry
		if currentDeadlock != nil && len(currentDeadlock.processes) > 0 {
			if err := p.saveDeadlock(ctx, currentDeadlock); err == nil {
				parsed++
			}
			currentDeadlock = nil
		}
	}

	// Save any pending deadlock
	if currentDeadlock != nil && len(currentDeadlock.processes) > 0 {
		if err := p.saveDeadlock(ctx, currentDeadlock); err == nil {
			parsed++
		}
	}

	// Update position
	pos, _ := file.Seek(0, 1)
	p.lastPosition[filePath] = pos

	return parsed, scanner.Err()
}

// jsonDeadlockState holds state while parsing a deadlock event from JSON logs.
type jsonDeadlockState struct {
	detectedAt      time.Time
	dbName          string
	processes       []*processInfo
	resolvedByPID   *int
	detectionTimeMs *int
	applicationName string
	username        string
	clientAddr      string
	backendStart    *time.Time
}

// parseDetail parses the DETAIL field which contains process and query info.
func (p *JSONLogParser) parseDetail(state *jsonDeadlockState, detail string) {
	// DETAIL format:
	// Process 83853 waits for ShareLock on transaction 4370; blocked by process 83850.
	// Process 83850 waits for ShareLock on transaction 4371; blocked by process 83853.
	// Process 83853: UPDATE test SET data = 'x' WHERE id = 1;
	// Process 83850: UPDATE test SET data = 'y' WHERE id = 2;

	lines := strings.Split(detail, "\n")
	detailInfos := make(map[int]*detailInfo)

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
			// Apply detail info
			if info, ok := detailInfos[proc.pid]; ok {
				proc.lockMode = info.lockMode
				proc.lockType = info.lockType
				proc.blockedByPID = info.blockedByPID
			}

			// Extract relation from query
			if rel := extractRelationFromQuery(proc.query); rel != "" {
				proc.relationName = rel
			}

			// Generate fingerprint
			if fingerprint := generateFingerprint(proc.query); fingerprint != 0 {
				proc.queryFingerprint = &fingerprint
			}

			// Apply metadata from the deadlock state
			proc.username = state.username
			proc.applicationName = state.applicationName
			proc.clientAddr = state.clientAddr

			state.processes = append(state.processes, proc)
		}
	}
}

// saveDeadlock saves a parsed deadlock to the store.
func (p *JSONLogParser) saveDeadlock(ctx context.Context, state *jsonDeadlockState) error {
	if len(state.processes) == 0 {
		return nil
	}

	event := &sqlite.DeadlockEvent{
		DetectedAt:      state.detectedAt,
		DatabaseName:    state.dbName,
		ResolvedByPID:   state.resolvedByPID,
		DetectionTimeMs: state.detectionTimeMs,
		InstanceName:    p.instanceName,
	}

	for _, proc := range state.processes {
		dp := sqlite.DeadlockProcess{
			PID:              proc.pid,
			LockType:         proc.lockType,
			LockMode:         proc.lockMode,
			RelationName:     proc.relationName,
			Query:            proc.query,
			BlockedByPID:     proc.blockedByPID,
			QueryFingerprint: proc.queryFingerprint,
			Username:         proc.username,
			ApplicationName:  proc.applicationName,
			ClientAddr:       proc.clientAddr,
			BackendStart:     state.backendStart,
		}

		// Try to get xact_start from session cache
		if p.sessionCache != nil {
			if sessionState := p.sessionCache.GetSessionState(proc.pid); sessionState != nil {
				dp.XactStart = sessionState.XactStart
			}
		}

		event.Processes = append(event.Processes, dp)
	}

	_, err := p.store.InsertEvent(ctx, event)
	return err
}

// normalizeClientAddr normalizes client address (e.g., "[local]" -> "local")
func normalizeClientAddr(addr string) string {
	if addr == "[local]" || addr == "" {
		return "local"
	}
	return addr
}

// Reuse helper functions from deadlock_parser.go
var _ = pg_query.Parse // ensure import is used
