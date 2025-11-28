// Package models provides data models for Steep.
package models

import "time"

// LogSeverity represents the severity level of a log entry.
type LogSeverity int

const (
	// SeverityDebug represents DEBUG1-5 log levels.
	SeverityDebug LogSeverity = iota
	// SeverityInfo represents LOG, INFO, NOTICE log levels.
	SeverityInfo
	// SeverityWarning represents WARNING log level.
	SeverityWarning
	// SeverityError represents ERROR, FATAL, PANIC log levels.
	SeverityError
)

// String returns the display string for a log severity.
func (s LogSeverity) String() string {
	switch s {
	case SeverityDebug:
		return "DEBUG"
	case SeverityInfo:
		return "INFO"
	case SeverityWarning:
		return "WARN"
	case SeverityError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseSeverity parses a PostgreSQL severity string into a LogSeverity.
func ParseSeverity(s string) LogSeverity {
	switch s {
	case "DEBUG", "DEBUG1", "DEBUG2", "DEBUG3", "DEBUG4", "DEBUG5":
		return SeverityDebug
	case "LOG", "INFO", "NOTICE":
		return SeverityInfo
	case "WARNING", "WARN":
		return SeverityWarning
	case "ERROR", "FATAL", "PANIC":
		return SeverityError
	default:
		return SeverityInfo
	}
}

// LogEntry represents a single parsed log record from PostgreSQL server logs.
type LogEntry struct {
	// Timestamp is when the log entry was written.
	Timestamp time.Time
	// Severity is the log level (ERROR, WARNING, INFO, DEBUG).
	Severity LogSeverity
	// PID is the PostgreSQL backend process ID.
	PID int
	// Database is the database name (optional).
	Database string
	// User is the PostgreSQL username (optional).
	User string
	// Application is the application name from connection (optional).
	Application string
	// Message is the log message content.
	Message string
	// Detail is the DETAIL line if present (optional).
	Detail string
	// Hint is the HINT line if present (optional).
	Hint string
	// RawLine is the original unparsed log line.
	RawLine string
	// SourceFile is the log file this entry came from.
	SourceFile string
	// SourceLine is the byte offset in source file.
	SourceLine int64
}
