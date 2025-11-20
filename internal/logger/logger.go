package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	// Log is the global structured logger
	Log *slog.Logger
	// logFile is the log file handle
	logFile *os.File
)

// LogLevel represents the logging level
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// InitLogger initializes the global logger with the specified level
func InitLogger(level LogLevel) {
	var slogLevel slog.Level

	switch level {
	case LevelDebug:
		slogLevel = slog.LevelDebug
	case LevelInfo:
		slogLevel = slog.LevelInfo
	case LevelWarn:
		slogLevel = slog.LevelWarn
	case LevelError:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}

	// Write logs to file instead of stderr to avoid interfering with TUI
	var writer io.Writer
	logDir := os.TempDir()
	logPath := filepath.Join(logDir, "steep.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fallback to discard if we can't open log file
		writer = io.Discard
	} else {
		writer = file
		logFile = file
	}

	handler := slog.NewJSONHandler(writer, opts)
	Log = slog.New(handler)
	slog.SetDefault(Log)
}

// Close closes the log file
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	Log.Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	Log.Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	Log.Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	Log.Error(msg, args...)
}

// With creates a new logger with additional attributes
func With(args ...any) *slog.Logger {
	return Log.With(args...)
}
