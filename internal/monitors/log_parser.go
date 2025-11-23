package monitors

import (
	"context"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// LogFormat represents the PostgreSQL log format type.
type LogFormat string

const (
	LogFormatStderr  LogFormat = "stderr"
	LogFormatCSV     LogFormat = "csvlog"
	LogFormatJSON    LogFormat = "jsonlog"
	LogFormatUnknown LogFormat = "unknown"
)

// LogParser defines the interface for parsing PostgreSQL log files.
type LogParser interface {
	// ParseNewEntries scans log files for new deadlock events.
	// Returns the number of events parsed.
	ParseNewEntries(ctx context.Context) (int, error)

	// SetPositions sets the initial file positions from persisted storage.
	SetPositions(positions map[string]int64)

	// GetPositions returns the current file positions for persistence.
	GetPositions() map[string]int64

	// ResetPositions clears all file positions to start fresh.
	ResetPositions()
}

// NewLogParser creates a log parser based on the detected format.
func NewLogParser(format LogFormat, logDir, logPattern string, store *sqlite.DeadlockStore, dbName string, sessionCache *SessionCache) LogParser {
	switch format {
	case LogFormatJSON:
		return NewJSONLogParser(logDir, logPattern, store, dbName, sessionCache)
	case LogFormatCSV:
		return NewCSVLogParser(logDir, logPattern, store, dbName, sessionCache)
	default:
		// Fall back to stderr parser
		return NewDeadlockParser(logDir, logPattern, store, dbName, sessionCache)
	}
}
