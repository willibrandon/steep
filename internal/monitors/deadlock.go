package monitors

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// DeadlockMonitor monitors PostgreSQL logs for deadlock events.
type DeadlockMonitor struct {
	pool         *pgxpool.Pool
	store        *sqlite.DeadlockStore
	parser       LogParser
	parserMu     sync.Mutex // Protects parser field
	sessionCache *SessionCache
	interval     time.Duration
	enabled      bool
	logDir       string
	logPattern   string
	dbName       string
	instanceName string // PostgreSQL instance name for multi-instance support
}

// NewDeadlockMonitor creates a new DeadlockMonitor.
// It checks if logging_collector is enabled and configures accordingly.
func NewDeadlockMonitor(ctx context.Context, pool *pgxpool.Pool, store *sqlite.DeadlockStore, interval time.Duration) (*DeadlockMonitor, error) {
	m := &DeadlockMonitor{
		pool:     pool,
		store:    store,
		interval: interval,
	}

	// Check logging configuration
	config, err := queries.GetLoggingConfig(ctx, pool)
	if err != nil {
		return m, nil // Return disabled monitor on error
	}

	if !config.LoggingCollector {
		// Logging collector not enabled
		return m, nil
	}

	// Resolve log directory path
	logDir := config.LogDirectory
	if !filepath.IsAbs(logDir) {
		logDir = filepath.Join(config.DataDirectory, logDir)
	}

	// Convert log_filename pattern to glob pattern
	// PostgreSQL uses %Y, %m, %d, etc. - convert to *
	logPattern := convertLogFilenameToGlob(config.LogFilename)

	// Get database name
	dbName, err := queries.GetDatabaseName(ctx, pool)
	if err != nil {
		dbName = "unknown"
	}

	// Store config for dynamic format detection
	m.logDir = logDir
	m.logPattern = logPattern
	m.dbName = dbName

	// Create session cache for capturing xact_start and start it
	m.sessionCache = NewSessionCache(pool)
	m.sessionCache.Start(ctx)

	// Create initial parser based on current log_destination
	format := detectLogFormat(config.LogDestination)
	m.parser = NewLogParser(format, logDir, logPattern, store, dbName, m.sessionCache)
	m.enabled = true

	// Load persisted log positions for faster startup
	if positions, err := store.GetLogPositions(ctx); err == nil && len(positions) > 0 {
		m.parser.SetPositions(positions)
	}

	return m, nil
}

// detectLogFormat determines which log format to use based on log_destination.
// Prefers jsonlog > csvlog > stderr for richest metadata.
func detectLogFormat(logDestination string) LogFormat {
	// log_destination can be comma-separated list like "stderr,jsonlog"
	dest := strings.ToLower(logDestination)

	// Prefer JSON (easiest to parse, has all metadata)
	if strings.Contains(dest, "jsonlog") {
		return LogFormatJSON
	}
	// Then CSV (has session_start_time)
	if strings.Contains(dest, "csvlog") {
		return LogFormatCSV
	}
	// Default to stderr
	return LogFormatStderr
}

// IsEnabled returns true if deadlock monitoring is available.
func (m *DeadlockMonitor) IsEnabled() bool {
	return m.enabled
}

// GetLogDirectory returns the configured log directory (for display purposes).
func (m *DeadlockMonitor) GetLogDirectory() string {
	return m.logDir
}

// ParseOnce parses log files once and returns number of new deadlocks found.
func (m *DeadlockMonitor) ParseOnce(ctx context.Context) (int, error) {
	return m.ParseOnceWithProgress(ctx, nil)
}

// ParseOnceWithProgress parses log files with progress reporting.
func (m *DeadlockMonitor) ParseOnceWithProgress(ctx context.Context, progress ProgressFunc) (int, error) {
	if !m.enabled {
		return 0, nil
	}

	// Lock to protect parser access during the entire operation
	m.parserMu.Lock()
	defer m.parserMu.Unlock()

	// Re-detect log format in case it changed (e.g., user enabled logging)
	config, err := queries.GetLoggingConfig(ctx, m.pool)
	if err == nil {
		newFormat := detectLogFormat(config.LogDestination)
		currentFormat := LogFormatStderr
		switch m.parser.(type) {
		case *JSONLogParser:
			currentFormat = LogFormatJSON
		case *CSVLogParser:
			currentFormat = LogFormatCSV
		}
		if newFormat != currentFormat {
			m.parser = NewLogParser(newFormat, m.logDir, m.logPattern, m.store, m.dbName, m.sessionCache)
			// Load persisted positions for new parser
			if positions, err := m.store.GetLogPositions(ctx); err == nil && len(positions) > 0 {
				m.parser.SetPositions(positions)
			}
		}
	}

	if m.parser == nil {
		return 0, nil
	}

	count, err := m.parser.ParseNewEntriesWithProgress(ctx, progress)

	// Save log positions for faster startup next time
	if m.store != nil {
		positions := m.parser.GetPositions()
		for filePath, pos := range positions {
			m.store.SaveLogPosition(ctx, filePath, pos)
		}
	}

	return count, err
}

// Run starts the monitor goroutine that parses logs periodically.
// It runs until the context is cancelled.
func (m *DeadlockMonitor) Run(ctx context.Context) {
	if !m.enabled {
		return
	}

	// Start session cache for capturing xact_start
	if m.sessionCache != nil {
		m.sessionCache.Start(ctx)
	}

	// Parse once immediately
	m.ParseOnce(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.ParseOnce(ctx)
		}
	}
}

// convertLogFilenameToGlob converts PostgreSQL log_filename pattern to a glob pattern.
// For example: "postgresql-%Y-%m-%d_%H%M%S.log" becomes "postgresql-*.log"
func convertLogFilenameToGlob(pattern string) string {
	// Replace PostgreSQL date/time placeholders with *
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

// GetRecentDeadlocks returns recent deadlock summaries from storage.
// Filters by the current instance name if set.
func (m *DeadlockMonitor) GetRecentDeadlocks(ctx context.Context, days int, limit int) ([]sqlite.DeadlockSummary, error) {
	return m.store.GetRecentEvents(ctx, days, limit, m.instanceName)
}

// SetInstanceName sets the instance name for multi-instance support.
// This affects both storage (new deadlocks tagged with instance) and retrieval (filter by instance).
func (m *DeadlockMonitor) SetInstanceName(name string) {
	m.parserMu.Lock()
	defer m.parserMu.Unlock()
	m.instanceName = name
	if m.parser != nil {
		m.parser.SetInstanceName(name)
	}
}

// GetDeadlockEvent returns a single deadlock event with all processes.
func (m *DeadlockMonitor) GetDeadlockEvent(ctx context.Context, eventID int64) (*sqlite.DeadlockEvent, error) {
	return m.store.GetEvent(ctx, eventID)
}

// GetDeadlockCount returns the total number of stored deadlock events.
func (m *DeadlockMonitor) GetDeadlockCount(ctx context.Context) (int64, error) {
	return m.store.Count(ctx)
}

// CleanupOldEvents removes deadlock events older than the retention period.
func (m *DeadlockMonitor) CleanupOldEvents(ctx context.Context, retention time.Duration) (int64, error) {
	return m.store.Cleanup(ctx, retention)
}

// ResetPositions clears in-memory log positions after a reset.
func (m *DeadlockMonitor) ResetPositions() {
	m.parserMu.Lock()
	defer m.parserMu.Unlock()
	if m.parser != nil {
		m.parser.ResetPositions()
	}
}
