package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	// Log is the global structured logger
	Log *slog.Logger
	// logWriter is the rotating log writer
	logWriter *lumberjack.Logger
	// LogPath is the path to the current log file
	LogPath string
)

// LogLevel represents the logging level
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// InitLogger initializes the global logger with the specified level and optional path.
// If logPath is empty, defaults to ~/.config/steep/steep.log
func InitLogger(level LogLevel, logPath string) {
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

	// Determine log path
	if logPath == "" {
		// Default to ~/.config/steep/steep.log
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.TempDir()
		}
		logDir := filepath.Join(homeDir, ".config", "steep")
		// Ensure directory exists
		_ = os.MkdirAll(logDir, 0755)
		logPath = filepath.Join(logDir, "steep.log")
	}

	LogPath = logPath

	// Use lumberjack for log rotation
	var writer io.Writer
	logWriter = &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // MB
		MaxBackups: 3,
		MaxAge:     7, // days
		Compress:   true,
	}
	writer = logWriter

	handler := slog.NewJSONHandler(writer, opts)
	Log = slog.New(handler)
	slog.SetDefault(Log)
}

// Close closes the log file
func Close() {
	if logWriter != nil {
		logWriter.Close()
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
