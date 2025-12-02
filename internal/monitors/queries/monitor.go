package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// Pre-compiled regex for detecting corrupted queries with embedded timestamps
var timestampInQueryRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}`)

// DataSourceType indicates the source of query data.
type DataSourceType int

const (
	DataSourceSampling DataSourceType = iota
	DataSourceLogParsing
	DataSourceAgent // Agent is collecting data, TUI just reads from SQLite
)

// MonitorStatus represents the current status of the monitor.
type MonitorStatus int

const (
	MonitorStatusStopped MonitorStatus = iota
	MonitorStatusRunning
	MonitorStatusError
)

// MonitorConfig holds configuration for the query monitor.
type MonitorConfig struct {
	// RefreshInterval is how often to poll for new queries
	RefreshInterval time.Duration
	// RetentionDays is how long to keep query statistics
	RetentionDays int
	// LogDir is the directory containing PostgreSQL log files
	LogDir string
	// LogPattern is the glob pattern for log files (e.g., "postgresql-*.log")
	LogPattern string
	// LogLinePrefix is the log_line_prefix setting (for log parsing mode)
	LogLinePrefix string
	// AutoEnableLogging prompts to enable query logging if disabled
	AutoEnableLogging bool
}

// LoggingStatus represents the current state of PostgreSQL query logging.
type LoggingStatus struct {
	Enabled       bool
	LogDir        string
	LogPattern    string
	LogLinePrefix string
}

// DefaultMonitorConfig returns default configuration.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		RefreshInterval:   5 * time.Second,
		RetentionDays:     7,
		AutoEnableLogging: true,
	}
}

// Monitor orchestrates query data collection and storage.
type Monitor struct {
	pool        *pgxpool.Pool
	store       *sqlite.QueryStatsStore
	fingerprint *Fingerprinter
	config      MonitorConfig

	// State
	status          MonitorStatus
	dataSource      DataSourceType
	accessMethod    LogAccessMethod
	cancel          context.CancelFunc
	loggingChecked  bool // Track if we've already checked logging status
}

// NewMonitor creates a new query monitor.
func NewMonitor(pool *pgxpool.Pool, store *sqlite.QueryStatsStore, config MonitorConfig) *Monitor {
	return &Monitor{
		pool:        pool,
		store:       store,
		fingerprint: NewFingerprinter(),
		config:      config,
		status:      MonitorStatusStopped,
		dataSource:  DataSourceSampling,
	}
}

// Configure checks logging status and configures the monitor without starting collection.
// Call this once at startup to avoid running SHOW queries on every Start/Stop cycle.
func (m *Monitor) Configure(ctx context.Context) error {
	if m.loggingChecked {
		return nil
	}
	status, err := m.CheckLoggingStatus(ctx)
	if err == nil && status.Enabled && status.LogDir != "" {
		m.config.LogDir = status.LogDir
		m.config.LogPattern = status.LogPattern
		m.config.LogLinePrefix = status.LogLinePrefix
	}
	m.loggingChecked = true
	return err
}

// Start begins monitoring queries.
func (m *Monitor) Start(ctx context.Context) error {
	ctx, m.cancel = context.WithCancel(ctx)
	m.status = MonitorStatusRunning

	// Configure if not already done (should be called separately at startup)
	_ = m.Configure(ctx)

	// Determine data source and access method
	if m.config.LogDir != "" && m.config.LogPattern != "" {
		// Try filesystem access first
		if _, statErr := os.Stat(m.config.LogDir); statErr == nil {
			m.dataSource = DataSourceLogParsing
			m.accessMethod = LogAccessFileSystem
			return m.startLogCollector(ctx)
		}

		// Filesystem not accessible, try pg_read_file
		if pgErr := m.canUsePgReadFile(ctx, m.config.LogDir); pgErr == nil {
			m.dataSource = DataSourceLogParsing
			m.accessMethod = LogAccessPgReadFile
			return m.startLogCollector(ctx)
		}
		// Neither access method works, fall through to sampling
	}

	m.dataSource = DataSourceSampling
	return m.startSamplingCollector(ctx)
}

// canUsePgReadFile checks if pg_read_file is available and the user has permissions.
func (m *Monitor) canUsePgReadFile(ctx context.Context, logDir string) error {
	// Try to list the log directory using pg_ls_dir
	query := `SELECT count(*) FROM (SELECT pg_ls_dir($1) LIMIT 1) AS dirs`
	var count int
	err := m.pool.QueryRow(ctx, query, logDir).Scan(&count)
	if err != nil {
		return err
	}
	return nil
}

// CheckLoggingStatus checks if PostgreSQL query logging is enabled and returns the log directory/pattern.
func (m *Monitor) CheckLoggingStatus(ctx context.Context) (*LoggingStatus, error) {
	var logMinDuration string
	var dataDir string
	var logDir string
	var logFilename string
	var logLinePrefix string

	// Query configured setting from pg_file_settings (ignores session overrides)
	// Falls back to pg_settings.reset_val if not in config files
	err := m.pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT setting FROM pg_file_settings
			 WHERE name = 'log_min_duration_statement' AND error IS NULL
			 ORDER BY seqno DESC LIMIT 1),
			(SELECT reset_val FROM pg_settings WHERE name = 'log_min_duration_statement')
		)
	`).Scan(&logMinDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to check log_min_duration_statement: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW data_directory").Scan(&dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get data_directory: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW log_directory").Scan(&logDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get log_directory: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW log_filename").Scan(&logFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to get log_filename: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW log_line_prefix").Scan(&logLinePrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to get log_line_prefix: %w", err)
	}

	// -1 means disabled, any other value means enabled
	enabled := logMinDuration != "-1"

	// Resolve log directory path
	if !filepath.IsAbs(logDir) {
		logDir = filepath.Join(dataDir, logDir)
	}

	// Convert log_filename pattern to glob pattern
	logPattern := convertLogFilenameToGlob(logFilename)

	return &LoggingStatus{
		Enabled:       enabled,
		LogDir:        logDir,
		LogPattern:    logPattern,
		LogLinePrefix: logLinePrefix,
	}, nil
}

// convertLogFilenameToGlob converts PostgreSQL log_filename pattern to a glob pattern.
// For example: "postgresql-%Y-%m-%d_%H%M%S.log" becomes "postgresql-*.log"
func convertLogFilenameToGlob(pattern string) string {
	result := pattern
	placeholders := []string{
		"%Y", "%m", "%d", "%H", "%M", "%S", "%a", "%b",
		"%j", "%W", "%y", "%I", "%p", "%e", "%c", "%n",
	}

	for _, ph := range placeholders {
		result = strings.ReplaceAll(result, ph, "*")
	}

	// Collapse multiple * into single *
	for strings.Contains(result, "**") {
		result = strings.ReplaceAll(result, "**", "*")
	}

	return result
}

// EnableLogging enables query logging with parameter capture.
func (m *Monitor) EnableLogging(ctx context.Context) error {
	// Set log_min_duration_statement to log all completed queries with duration
	_, err := m.pool.Exec(ctx, "ALTER SYSTEM SET log_min_duration_statement = 0")
	if err != nil {
		return fmt.Errorf("failed to set log_min_duration_statement: %w", err)
	}

	// Set log_statement to capture bound parameters for accurate EXPLAIN estimates
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_statement = 'all'")
	if err != nil {
		return fmt.Errorf("failed to set log_statement: %w", err)
	}

	// Enable parameter logging in DETAIL lines
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_parameter_max_length = -1")
	if err != nil {
		return fmt.Errorf("failed to set log_parameter_max_length: %w", err)
	}

	// Ensure DETAIL lines are included
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_error_verbosity = 'default'")
	if err != nil {
		return fmt.Errorf("failed to set log_error_verbosity: %w", err)
	}

	// Disable executor stats that interfere with parameter DETAIL lines
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_executor_stats = off")
	if err != nil {
		return fmt.Errorf("failed to set log_executor_stats: %w", err)
	}

	// Set log_line_prefix to include useful metadata
	// %t=timestamp, %p=PID, %a=application, %u=username, %h=client host
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_line_prefix = '%t [%p] [%a] [%u@%h] '")
	if err != nil {
		return fmt.Errorf("failed to set log_line_prefix: %w", err)
	}

	// Enable JSON logging for rich metadata (includes session_start for backend_start)
	// Also enable logging_collector which is required for jsonlog
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET logging_collector = on")
	if err != nil {
		return fmt.Errorf("failed to set logging_collector: %w", err)
	}

	// Add jsonlog and csvlog to log_destination (keeps stderr for console output)
	// jsonlog is preferred but csvlog provides backup
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_destination = 'stderr,jsonlog,csvlog'")
	if err != nil {
		return fmt.Errorf("failed to set log_destination: %w", err)
	}

	_, err = m.pool.Exec(ctx, "SELECT pg_reload_conf()")
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	return nil
}

// IsLoggingEnabled returns whether query logging is currently enabled.
func (m *Monitor) IsLoggingEnabled(ctx context.Context) bool {
	status, err := m.CheckLoggingStatus(ctx)
	if err != nil {
		return false
	}
	return status.Enabled
}

// Stop stops monitoring.
func (m *Monitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.status = MonitorStatusStopped
	return nil
}

// Status returns the current monitor status.
func (m *Monitor) Status() MonitorStatus {
	return m.status
}

// DataSource returns the current data source type.
func (m *Monitor) DataSource() DataSourceType {
	return m.dataSource
}

// SetAgentMode sets the monitor to agent mode, indicating the steep-agent is
// collecting data and the TUI should just read from SQLite without collecting.
func (m *Monitor) SetAgentMode() {
	m.dataSource = DataSourceAgent
	m.status = MonitorStatusRunning
}

// startSamplingCollector starts collecting via pg_stat_activity polling.
func (m *Monitor) startSamplingCollector(ctx context.Context) error {
	collector := NewSamplingCollector(m.pool, m.config.RefreshInterval)
	if err := collector.Start(ctx); err != nil {
		m.status = MonitorStatusError
		return err
	}

	go m.processEvents(ctx, collector.Events())
	return nil
}

// startLogCollector starts collecting via log file parsing.
func (m *Monitor) startLogCollector(ctx context.Context) error {
	collector := NewLogCollector(m.config.LogDir, m.config.LogPattern, m.config.LogLinePrefix, m.store, m.pool, m.accessMethod)
	if err := collector.Start(ctx); err != nil {
		m.status = MonitorStatusError
		return err
	}

	go m.processEvents(ctx, collector.Events())
	return nil
}

// processEvents processes query events from a collector.
func (m *Monitor) processEvents(ctx context.Context, events <-chan QueryEvent) {
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Drain remaining events before exiting
			for event := range events {
				m.processEvent(context.Background(), event)
			}
			return

		case event, ok := <-events:
			if !ok {
				return
			}
			m.processEvent(ctx, event)

		case <-cleanupTicker.C:
			// Cleanup old records
			retention := time.Duration(m.config.RetentionDays) * 24 * time.Hour
			_, _ = m.store.Cleanup(ctx, retention)
		}
	}
}

// processEvent processes a single query event.
func (m *Monitor) processEvent(ctx context.Context, event QueryEvent) {
	// Generate fingerprint
	fingerprint, normalized, err := m.fingerprint.Fingerprint(event.Query)
	if err != nil {
		// Use original query if fingerprinting fails
		normalized = event.Query
	}

	// Get row estimate via EXPLAIN if not available
	rows := event.Rows
	if rows == 0 {
		rows = m.estimateRows(ctx, event.Query, event.Params)
	}

	// Convert params to JSON for storage
	var sampleParams string
	if len(event.Params) > 0 {
		if paramsJSON, err := json.Marshal(event.Params); err == nil {
			sampleParams = string(paramsJSON)
		}
	}

	// Store in database (calls=0 triggers increment behavior for log-parsed queries)
	_ = m.store.Upsert(ctx, fingerprint, normalized, 0, event.DurationMs, event.DurationMs, rows, sampleParams)
}

// estimateRows runs EXPLAIN to get estimated row count for a query.
func (m *Monitor) estimateRows(ctx context.Context, query string, params map[string]string) int64 {
	// Only estimate for SELECT queries
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(trimmed, "SELECT") {
		return 0
	}

	// Skip queries that look corrupted (e.g., concatenated with log lines)
	if looksCorrupted(query) {
		return 0
	}

	// Take only the first statement if multiple are concatenated
	queryForExplain := extractFirstStatement(query)
	if queryForExplain == "" {
		return 0
	}

	// ALWAYS handle ANY/ALL first - params for ANY should be arrays, but log parsing
	// sometimes captures scalar values (like file paths). Replace with empty array.
	anyAllRe := regexp.MustCompile(`(=\s*ANY\s*\(\s*)\$\d+(\s*\))`)
	queryForExplain = anyAllRe.ReplaceAllString(queryForExplain, "${1}ARRAY[]::text[]${2}")

	if len(params) > 0 {
		// Use captured params for accurate estimates (excluding ANY params already handled)
		for param, value := range params {
			// Skip params that look like file paths (log parsing bug)
			if strings.HasPrefix(value, "/") {
				continue
			}
			// Quote string values for SQL
			quotedValue := "'" + strings.ReplaceAll(value, "'", "''") + "'"
			queryForExplain = strings.ReplaceAll(queryForExplain, param, quotedValue)
		}
	}

	// Replace any remaining params with NULL (type-compatible with most things)
	paramRe := regexp.MustCompile(`\$\d+`)
	queryForExplain = paramRe.ReplaceAllString(queryForExplain, "NULL")

	// Run EXPLAIN (FORMAT JSON) to get plan with row estimates
	explainQuery := fmt.Sprintf("EXPLAIN (FORMAT JSON) %s", queryForExplain)

	var planJSON string
	err := m.pool.QueryRow(ctx, explainQuery).Scan(&planJSON)
	if err != nil {
		return 0
	}

	// Parse JSON to extract Plan Rows
	// Format: [{"Plan": {"Plan Rows": 100, ...}}]
	var plans []struct {
		Plan struct {
			PlanRows float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}

	if err := json.Unmarshal([]byte(planJSON), &plans); err != nil {
		return 0
	}

	if len(plans) > 0 {
		return int64(plans[0].Plan.PlanRows)
	}

	return 0
}

// extractFirstStatement extracts the first SQL statement from a possibly multi-statement string.
// It finds the first semicolon that's not inside a string literal or comment.
func extractFirstStatement(query string) string {
	// Find the first semicolon that ends a statement
	// We need to be careful about semicolons inside string literals
	inString := false
	stringChar := byte(0)
	inBlockComment := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		// Handle block comments
		if !inString && i+1 < len(query) && c == '/' && query[i+1] == '*' {
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

		// Handle string literals
		if !inString && (c == '\'' || c == '"') {
			inString = true
			stringChar = c
			continue
		}
		if inString && c == stringChar {
			// Check for escaped quote
			if i+1 < len(query) && query[i+1] == stringChar {
				i++ // Skip escaped quote
				continue
			}
			inString = false
			continue
		}

		// Found a semicolon outside of string/comment - this ends the first statement
		if !inString && c == ';' {
			stmt := strings.TrimSpace(query[:i])
			if stmt != "" {
				return stmt
			}
		}
	}

	// No semicolon found, return the whole query
	return strings.TrimSpace(query)
}

// looksCorrupted checks if a query appears to have been corrupted by
// log parsing issues (e.g., concatenated with log lines from other entries).
func looksCorrupted(query string) bool {
	// Check for embedded log line patterns that indicate concatenation errors
	// These patterns should NOT appear in valid SQL outside of string literals
	logPatterns := []string{
		" UTC [",                       // Log line timestamp suffix
		" LOG:",                         // PostgreSQL log prefix
		"] LOG:",                        // Log prefix with bracket
	}

	// Check patterns against parts of query outside string literals
	unquoted := removeStringLiterals(query)
	upperUnquoted := strings.ToUpper(unquoted)

	for _, pattern := range logPatterns {
		if strings.Contains(upperUnquoted, strings.ToUpper(pattern)) {
			return true
		}
	}

	// Check for timestamp pattern outside of string literals (YYYY-MM-DD HH:MM:SS)
	// This catches cases like "...blocked.pid 2025-11-30 05:37:39..."
	if timestampInQueryRe.MatchString(unquoted) {
		return true
	}

	// Check for line comments (--) that are NOT followed by a newline.
	// Log parsing joins multi-line queries with spaces, which breaks line comments.
	// "SELECT 1 -- comment\nFROM" becomes "SELECT 1 -- comment FROM" where "FROM" is commented out.
	// If there's a -- without a following newline, the query structure is broken.
	if hasLineCommentWithoutNewline(query) {
		return true
	}

	// Check for query concatenation patterns - fragments of other queries appended
	// e.g., "...NULLS LAST  replay_lag::text as time_lag FROM pg_stat_replication"
	// These indicate two different queries were incorrectly merged
	concatPatterns := []string{
		"NULLS LAST  ",                  // Double space often indicates concatenation point
		" FROM PG_STAT_REPLICATION",     // Replication query fragment in non-replication query
		"::TEXT AS TIME_LAG",            // Common replication query fragment
	}

	for _, pattern := range concatPatterns {
		if strings.Contains(upperUnquoted, pattern) {
			return true
		}
	}

	return false
}

// removeStringLiterals replaces string literals with empty strings to allow
// pattern matching against SQL structure without matching quoted content.
func removeStringLiterals(query string) string {
	var result strings.Builder
	result.Grow(len(query))

	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); i++ {
		c := query[i]

		if !inString && (c == '\'' || c == '"') {
			inString = true
			stringChar = c
			result.WriteByte(c)
			continue
		}

		if inString {
			if c == stringChar {
				// Check for escaped quote
				if i+1 < len(query) && query[i+1] == stringChar {
					i++ // Skip escaped quote
					continue
				}
				inString = false
				result.WriteByte(c)
			}
			// Skip characters inside strings
			continue
		}

		result.WriteByte(c)
	}

	return result.String()
}

// hasLineCommentWithoutNewline checks if query has a line comment (--) that extends to end of string
// without a newline to terminate it. This indicates log parsing broke the query structure.
func hasLineCommentWithoutNewline(query string) bool {
	inString := false
	stringChar := byte(0)
	inBlockComment := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		// Handle block comments
		if !inString && i+1 < len(query) && c == '/' && query[i+1] == '*' {
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

		// Handle string literals
		if !inString && (c == '\'' || c == '"') {
			inString = true
			stringChar = c
			continue
		}
		if inString && c == stringChar {
			if i+1 < len(query) && query[i+1] == stringChar {
				i++ // Skip escaped quote
				continue
			}
			inString = false
			continue
		}
		if inString {
			continue
		}

		// Found start of line comment
		if i+1 < len(query) && c == '-' && query[i+1] == '-' {
			// Look for newline after this point
			remainder := query[i:]
			if !strings.Contains(remainder, "\n") {
				// Line comment extends to end of string without newline - query is broken
				return true
			}
		}
	}

	return false
}

// GetExplainPlan runs EXPLAIN (FORMAT JSON) and returns the formatted plan.
// If analyze is true, runs EXPLAIN ANALYZE which actually executes the query.
func (m *Monitor) GetExplainPlan(ctx context.Context, query string, analyze bool) (string, error) {
	// Skip queries that look corrupted (e.g., concatenated with log lines)
	if looksCorrupted(query) {
		return "", fmt.Errorf("query appears corrupted (contains log line fragments)")
	}

	// Take only the first statement if multiple are concatenated
	queryForExplain := extractFirstStatement(query)
	if queryForExplain == "" {
		return "", fmt.Errorf("empty query")
	}

	// Handle EXTRACT - replace parameter in EXTRACT context with 'epoch'
	extractRe := regexp.MustCompile(`EXTRACT\s*\(\s*\$\d+`)
	queryForExplain = extractRe.ReplaceAllString(queryForExplain, "EXTRACT(epoch")

	// Handle LIMIT/OFFSET - need actual numbers
	limitRe := regexp.MustCompile(`LIMIT\s+\$\d+`)
	queryForExplain = limitRe.ReplaceAllString(queryForExplain, "LIMIT 100")
	offsetRe := regexp.MustCompile(`OFFSET\s+\$\d+`)
	queryForExplain = offsetRe.ReplaceAllString(queryForExplain, "OFFSET 0")

	// Handle ANY/ALL with parameters - replace with empty array
	anyAllRe := regexp.MustCompile(`(=\s*ANY\s*\(\s*)\$\d+(\s*\))`)
	queryForExplain = anyAllRe.ReplaceAllString(queryForExplain, "${1}ARRAY[]::text[]${2}")

	// Replace remaining parameters with NULL (type-compatible with most things)
	paramRe := regexp.MustCompile(`\$\d+`)
	queryForExplain = paramRe.ReplaceAllString(queryForExplain, "NULL")

	// Run EXPLAIN with appropriate options
	var explainQuery string
	if analyze {
		// ANALYZE actually runs the query - use for detailed timing info
		explainQuery = fmt.Sprintf("EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) %s", queryForExplain)
	} else {
		explainQuery = fmt.Sprintf("EXPLAIN (FORMAT JSON) %s", queryForExplain)
	}

	var planJSON string
	err := m.pool.QueryRow(ctx, explainQuery).Scan(&planJSON)
	if err != nil {
		return "", fmt.Errorf("EXPLAIN failed: %w", err)
	}

	return planJSON, nil
}

// Pool returns the database connection pool.
func (m *Monitor) Pool() *pgxpool.Pool {
	return m.pool
}

// ResetPositions restarts the monitor to reload log positions from database.
// Call this after resetting log positions in the store.
func (m *Monitor) ResetPositions() {
	if m.status != MonitorStatusRunning {
		return
	}
	// Stop and restart to reload positions from database
	m.Stop()
	ctx := context.Background()
	_ = m.Start(ctx)
}

// ParseWithProgress parses log files with progress reporting via callback.
// The callback is called with (currentFile, totalFiles) for each file processed.
func (m *Monitor) ParseWithProgress(ctx context.Context, progressCallback func(current, total int)) {
	// Determine access method if not already set (allows calling before Start)
	accessMethod := m.accessMethod
	if m.dataSource != DataSourceLogParsing {
		// Check if we can use log parsing
		if m.config.LogDir == "" || m.config.LogPattern == "" {
			return
		}
		// Try filesystem access first
		if _, statErr := os.Stat(m.config.LogDir); statErr == nil {
			accessMethod = LogAccessFileSystem
		} else if pgErr := m.canUsePgReadFile(ctx, m.config.LogDir); pgErr == nil {
			accessMethod = LogAccessPgReadFile
		} else {
			return // Neither access method works
		}
	}

	// Create a temporary collector to parse files
	collector := NewLogCollector(m.config.LogDir, m.config.LogPattern, m.config.LogLinePrefix, m.store, m.pool, accessMethod)

	// Get list of log files
	files, err := collector.findLogFiles()
	if err != nil {
		return
	}

	// Start consuming and processing events in background
	eventsDone := make(chan struct{})
	go func() {
		for event := range collector.Events() {
			m.processEvent(ctx, event)
		}
		close(eventsDone)
	}()

	totalFiles := len(files)
	for i, file := range files {
		if progressCallback != nil {
			progressCallback(i+1, totalFiles)
		}
		// Read the file (sends events to collector.Events() channel)
		_ = collector.readFile(ctx, file)
	}

	// Close events channel to signal no more events
	close(collector.events)

	// Wait for all events to be processed
	<-eventsDone
}
