package monitors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// DeadlockParser parses PostgreSQL log files for deadlock events.
type DeadlockParser struct {
	logDir       string
	logPattern   string
	store        *sqlite.DeadlockStore
	dbName       string
	sessionCache *SessionCache
	lastPosition map[string]int64 // Track position in each log file
	mu           sync.Mutex       // Protects lastPosition
}

// NewDeadlockParser creates a new deadlock log parser.
func NewDeadlockParser(logDir, logPattern string, store *sqlite.DeadlockStore, dbName string, sessionCache *SessionCache) *DeadlockParser {
	return &DeadlockParser{
		logDir:       logDir,
		logPattern:   logPattern,
		store:        store,
		dbName:       dbName,
		sessionCache: sessionCache,
		lastPosition: make(map[string]int64),
	}
}

// ParseNewEntries scans log files for new deadlock events.
func (p *DeadlockParser) ParseNewEntries(ctx context.Context) (int, error) {
	return p.ParseNewEntriesWithProgress(ctx, nil)
}

// ParseNewEntriesWithProgress scans log files with progress reporting.
func (p *DeadlockParser) ParseNewEntriesWithProgress(ctx context.Context, progress ProgressFunc) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find log files matching pattern
	files, err := filepath.Glob(filepath.Join(p.logDir, p.logPattern))
	if err != nil {
		return 0, fmt.Errorf("glob log files: %w", err)
	}

	totalFiles := len(files)
	totalParsed := 0
	for i, file := range files {
		if progress != nil {
			progress(i+1, totalFiles)
		}
		count, err := p.parseFile(ctx, file)
		if err != nil {
			// Log error but continue with other files
			continue
		}
		totalParsed += count
	}

	return totalParsed, nil
}

// SetPositions sets the initial file positions from persisted storage.
func (p *DeadlockParser) SetPositions(positions map[string]int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range positions {
		p.lastPosition[k] = v
	}
}

// GetPositions returns the current file positions for persistence.
func (p *DeadlockParser) GetPositions() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make(map[string]int64, len(p.lastPosition))
	for k, v := range p.lastPosition {
		result[k] = v
	}
	return result
}

// ResetPositions clears all file positions to start fresh.
func (p *DeadlockParser) ResetPositions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastPosition = make(map[string]int64)
}

// parseFile parses a single log file for deadlock events.
func (p *DeadlockParser) parseFile(ctx context.Context, filePath string) (int, error) {
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
			// File may have been rotated, start from beginning
			lastPos = 0
		}
	}

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentDeadlock *deadlockState
	var currentRelation string
	var pendingDetectionTimeMs *int
	parsed := 0

	for scanner.Scan() {
		line := scanner.Text()

		// Check for detection time line (comes before ERROR line)
		// Format: process 83853 detected deadlock while waiting for ShareLock on transaction 4370 after 1001.189 ms
		if strings.Contains(line, "detected deadlock") && strings.Contains(line, "after") && strings.Contains(line, "ms") {
			if ms := extractDetectionTime(line); ms != nil {
				pendingDetectionTimeMs = ms
			}
			continue
		}

		// Check for deadlock detected - this starts a new deadlock event
		// Format: 2025-11-23 00:15:52.554 PST [79638] [psql] ERROR:  deadlock detected
		// or: 2025-11-23 00:15:52.554 PST [79638] [psql] [brandon@localhost] ERROR:  deadlock detected
		if strings.Contains(line, "ERROR:") && strings.Contains(line, "deadlock detected") {
			if currentDeadlock != nil && len(currentDeadlock.processes) > 0 {
				// Save previous deadlock
				if err := p.saveDeadlock(ctx, currentDeadlock); err == nil {
					parsed++
				}
			}
			resolvedPID := extractLogPID(line)
			currentDeadlock = &deadlockState{
				detectedAt:      parseTimestamp(line),
				dbName:          p.dbName,
				detectionTimeMs: pendingDetectionTimeMs,
				metadata:        extractLogMetadata(line),
			}
			if resolvedPID > 0 {
				currentDeadlock.resolvedByPID = &resolvedPID
			}
			currentRelation = ""
			pendingDetectionTimeMs = nil
			continue
		}

		if currentDeadlock == nil {
			continue
		}

		// Parse DETAIL line for lock mode and blocked_by_pid
		// Format: DETAIL:  Process 83853 waits for ShareLock on transaction 4370; blocked by process 83850.
		if strings.Contains(line, "DETAIL:") || (strings.Contains(line, "waits for") && strings.Contains(line, "blocked by")) {
			if info := extractDetailInfo(line); info != nil {
				currentDeadlock.detailInfo = append(currentDeadlock.detailInfo, info)
			}
			continue
		}

		// Parse CONTEXT line for relation name
		// Format: CONTEXT:  while updating tuple (0,1) in relation "test_deadlock_79628"
		if strings.Contains(line, "CONTEXT:") && strings.Contains(line, "relation") {
			currentRelation = extractRelation(line)
			continue
		}

		// Parse process lines - these contain PID and query
		// Format: 	Process 79638: UPDATE test_deadlock_79628 SET data = 'session2_1' WHERE id = 1;
		if strings.Contains(line, "Process ") && strings.Contains(line, ":") && !strings.Contains(line, "waits for") {
			proc := parseDeadlockProcessLine(line)
			if proc != nil {
				// Extract relation name from query
				if rel := extractRelationFromQuery(proc.query); rel != "" {
					proc.relationName = rel
				} else {
					proc.relationName = currentRelation
				}
				// Generate fingerprint
				if fingerprint := generateFingerprint(proc.query); fingerprint != 0 {
					proc.queryFingerprint = &fingerprint
				}
				// Apply detail info if available for this PID
				for _, detail := range currentDeadlock.detailInfo {
					if detail.pid == proc.pid {
						proc.lockMode = detail.lockMode
						proc.lockType = detail.lockType
						proc.blockedByPID = detail.blockedByPID
						break
					}
				}
				currentDeadlock.processes = append(currentDeadlock.processes, proc)
			}
			continue
		}

		// End of deadlock block - next timestamp line that's not part of this event
		if len(currentDeadlock.processes) > 0 && timestampRegex.MatchString(line) && !strings.Contains(line, "CONTEXT") && !strings.Contains(line, "STATEMENT") {
			// Save completed deadlock
			if err := p.saveDeadlock(ctx, currentDeadlock); err == nil {
				parsed++
			}
			currentDeadlock = nil
			currentRelation = ""
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

// deadlockState holds state while parsing a deadlock event.
type deadlockState struct {
	detectedAt      time.Time
	dbName          string
	processes       []*processInfo
	resolvedByPID   *int
	detectionTimeMs *int
	detailInfo      []*detailInfo
	metadata        *logMetadata // metadata from ERROR line
}

// detailInfo holds lock and blocking info from DETAIL lines.
type detailInfo struct {
	pid          int
	lockMode     string
	lockType     string
	blockedByPID *int
}

// processInfo holds parsed process information.
type processInfo struct {
	pid              int
	lockType         string
	lockMode         string
	relationName     string
	query            string
	blockedByPID     *int
	queryFingerprint *uint64
	username         string
	applicationName  string
	clientAddr       string
}

// saveDeadlock saves a parsed deadlock to the store.
func (p *DeadlockParser) saveDeadlock(ctx context.Context, state *deadlockState) error {
	if len(state.processes) == 0 {
		return nil
	}

	event := &sqlite.DeadlockEvent{
		DetectedAt:      state.detectedAt,
		DatabaseName:    state.dbName,
		ResolvedByPID:   state.resolvedByPID,
		DetectionTimeMs: state.detectionTimeMs,
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
		}
		// Apply metadata from ERROR line if available
		if state.metadata != nil {
			dp.Username = state.metadata.username
			dp.ApplicationName = state.metadata.applicationName
			dp.ClientAddr = state.metadata.clientAddr
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

// Regex patterns for parsing PostgreSQL log lines
var (
	// 2025-11-23 00:15:52.554 PST [79638] [psql] ERROR:  deadlock detected
	// or with enhanced prefix: 2025-11-23 00:15:52.554 PST [79638] [psql] [brandon@localhost] ERROR:
	timestampRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
	logPIDRegex    = regexp.MustCompile(`\[(\d+)\]`)
	// Extract [application] and optionally [user@host] from log line
	// Format: [PID] [app] or [PID] [app] [user@host] or [PID] [app] [user@[local]]
	logMetadataRegex = regexp.MustCompile(`\[\d+\]\s*\[([^\]]*)\](?:\s*\[(\w+)@(\[local\]|[^\]]+)\])?`)
	// Process 79638: UPDATE test_deadlock_79628 SET data = 'session2_1' WHERE id = 1;
	deadlockProcessRegex = regexp.MustCompile(`^\s*Process (\d+): (.+)`)
	// relation "test_deadlock_79628"
	relationRegex = regexp.MustCompile(`relation "([^"]+)"`)
	// process 83853 detected deadlock while waiting for ShareLock on transaction 4370 after 1001.189 ms
	detectionTimeRegex = regexp.MustCompile(`detected deadlock.*after ([\d.]+) ms`)
	// Process 83853 waits for ShareLock on transaction 4370; blocked by process 83850.
	detailLineRegex = regexp.MustCompile(`Process (\d+) waits for (\w+) on (\w+).+; blocked by process (\d+)`)
)

// parseTimestamp extracts timestamp from log line.
func parseTimestamp(line string) time.Time {
	matches := timestampRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		t, err := time.Parse("2006-01-02 15:04:05", matches[1])
		if err == nil {
			return t
		}
	}
	return time.Now()
}

// extractLogPID extracts PID from log line format [PID]
func extractLogPID(line string) int {
	matches := logPIDRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		pid, _ := strconv.Atoi(matches[1])
		return pid
	}
	return 0
}

// logMetadata holds metadata extracted from log line prefix
type logMetadata struct {
	applicationName string
	username        string
	clientAddr      string
}

// extractLogMetadata extracts application, username, and host from log line
// Format: 2025-11-23 00:15:52.554 PST [79638] [psql] [brandon@localhost] ERROR:
// or: 2025-11-23 00:15:52.554 PST [79638] [psql] [local] ERROR:
func extractLogMetadata(line string) *logMetadata {
	matches := logMetadataRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		meta := &logMetadata{
			applicationName: matches[1],
		}
		if len(matches) >= 4 && matches[2] != "" {
			meta.username = matches[2]
			// Normalize [local] to just "local"
			if matches[3] == "[local]" {
				meta.clientAddr = "local"
			} else {
				meta.clientAddr = matches[3]
			}
		}
		return meta
	}
	return nil
}

// parseDeadlockProcessLine parses "Process PID: QUERY" format
func parseDeadlockProcessLine(line string) *processInfo {
	matches := deadlockProcessRegex.FindStringSubmatch(line)
	if len(matches) < 3 {
		return nil
	}

	pid, _ := strconv.Atoi(matches[1])
	query := strings.TrimSpace(matches[2])

	return &processInfo{
		pid:   pid,
		query: query,
	}
}

// extractRelation extracts relation name from CONTEXT line
func extractRelation(line string) string {
	matches := relationRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractDetectionTime extracts detection time in milliseconds from log line
func extractDetectionTime(line string) *int {
	matches := detectionTimeRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		// Parse float and convert to int (milliseconds)
		if f, err := strconv.ParseFloat(matches[1], 64); err == nil {
			ms := int(f)
			return &ms
		}
	}
	return nil
}

// extractDetailInfo extracts lock mode, lock type and blocked_by_pid from DETAIL line
func extractDetailInfo(line string) *detailInfo {
	matches := detailLineRegex.FindStringSubmatch(line)
	if len(matches) >= 5 {
		pid, _ := strconv.Atoi(matches[1])
		lockMode := matches[2]
		lockType := matches[3]
		blockedByPID, _ := strconv.Atoi(matches[4])
		return &detailInfo{
			pid:          pid,
			lockMode:     lockMode,
			lockType:     lockType,
			blockedByPID: &blockedByPID,
		}
	}
	return nil
}

// extractRelationFromQuery extracts the table name from a SQL query
func extractRelationFromQuery(query string) string {
	// Try to parse the query and extract table name
	result, err := pg_query.Parse(query)
	if err != nil {
		return ""
	}

	// Walk the parse tree to find table names
	for _, stmt := range result.Stmts {
		if stmt.Stmt == nil {
			continue
		}
		// Check for UPDATE statement
		if update := stmt.Stmt.GetUpdateStmt(); update != nil {
			if update.Relation != nil {
				return update.Relation.Relname
			}
		}
		// Check for DELETE statement
		if del := stmt.Stmt.GetDeleteStmt(); del != nil {
			if del.Relation != nil {
				return del.Relation.Relname
			}
		}
		// Check for INSERT statement
		if ins := stmt.Stmt.GetInsertStmt(); ins != nil {
			if ins.Relation != nil {
				return ins.Relation.Relname
			}
		}
		// Check for SELECT statement (FROM clause)
		if sel := stmt.Stmt.GetSelectStmt(); sel != nil {
			for _, from := range sel.FromClause {
				if rv := from.GetRangeVar(); rv != nil {
					return rv.Relname
				}
			}
		}
	}
	return ""
}

// generateFingerprint generates a fingerprint hash for a query
func generateFingerprint(query string) uint64 {
	normalized, err := pg_query.Normalize(query)
	if err != nil {
		return 0
	}
	return pg_query.HashXXH3_64([]byte(normalized), 0)
}
