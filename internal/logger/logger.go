package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LogEntry represents a captured log entry for the debug panel.
type LogEntry struct {
	Time    time.Time
	Level   slog.Level
	Message string
}

// ringBuffer is a fixed-size circular buffer for log entries.
type ringBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	size    int
	head    int
	count   int

	// Counters
	warnCount  int
	errorCount int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		entries: make([]LogEntry, size),
		size:    size,
	}
}

func (rb *ringBuffer) add(entry LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}

	// Update counters
	if entry.Level == slog.LevelWarn {
		rb.warnCount++
	} else if entry.Level >= slog.LevelError {
		rb.errorCount++
	}
}

func (rb *ringBuffer) getAll() []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]LogEntry, rb.count)
	for i := 0; i < rb.count; i++ {
		idx := (rb.head - rb.count + i + rb.size) % rb.size
		result[i] = rb.entries[idx]
	}
	return result
}

func (rb *ringBuffer) getCounts() (warn, err int) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.warnCount, rb.errorCount
}

func (rb *ringBuffer) clearCounts() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.warnCount = 0
	rb.errorCount = 0
}

// debugHandler wraps another handler to capture entries for the debug panel.
type debugHandler struct {
	inner  slog.Handler
	buffer *ringBuffer
}

func (h *debugHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *debugHandler) Handle(ctx context.Context, r slog.Record) error {
	// Capture WARN and ERROR entries
	if r.Level >= slog.LevelWarn {
		h.buffer.add(LogEntry{
			Time:    r.Time,
			Level:   r.Level,
			Message: r.Message,
		})
	}
	return h.inner.Handle(ctx, r)
}

func (h *debugHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &debugHandler{
		inner:  h.inner.WithAttrs(attrs),
		buffer: h.buffer,
	}
}

func (h *debugHandler) WithGroup(name string) slog.Handler {
	return &debugHandler{
		inner:  h.inner.WithGroup(name),
		buffer: h.buffer,
	}
}

var (
	// Log is the global structured logger
	Log *slog.Logger
	// logWriter is the rotating log writer
	logWriter *lumberjack.Logger
	// LogPath is the path to the current log file
	LogPath string
	// debugBuffer holds recent WARN/ERROR entries for the debug panel
	debugBuffer *ringBuffer
	// debugEnabled tracks if debug mode is active
	debugEnabled bool
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
	debugEnabled = level == LevelDebug

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
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.TempDir()
		}
		logDir := filepath.Join(homeDir, ".config", "steep")
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

	// Initialize ring buffer for debug panel (last 100 entries)
	debugBuffer = newRingBuffer(100)

	// Create handler chain: debugHandler -> JSONHandler -> lumberjack
	jsonHandler := slog.NewJSONHandler(writer, opts)
	handler := &debugHandler{
		inner:  jsonHandler,
		buffer: debugBuffer,
	}

	Log = slog.New(handler)
	slog.SetDefault(Log)
}

// Close closes the log file
func Close() {
	if logWriter != nil {
		logWriter.Close()
	}
}

// getLogger returns the global logger, or the default slog logger if not initialized.
func getLogger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	getLogger().Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}

// With creates a new logger with additional attributes
func With(args ...any) *slog.Logger {
	return getLogger().With(args...)
}

// GetCounts returns the current warning and error counts.
func GetCounts() (warn, err int) {
	if debugBuffer == nil {
		return 0, 0
	}
	return debugBuffer.getCounts()
}

// ClearCounts resets the warning and error counters.
func ClearCounts() {
	if debugBuffer != nil {
		debugBuffer.clearCounts()
	}
}

// GetEntries returns all captured log entries.
func GetEntries() []LogEntry {
	if debugBuffer == nil {
		return nil
	}
	return debugBuffer.getAll()
}

// IsDebugEnabled returns true if debug mode is active.
func IsDebugEnabled() bool {
	return debugEnabled
}

// FormatEntry formats a log entry for display.
func (e LogEntry) Format() string {
	levelStr := "INFO"
	switch e.Level {
	case slog.LevelDebug:
		levelStr = "DEBUG"
	case slog.LevelInfo:
		levelStr = "INFO"
	case slog.LevelWarn:
		levelStr = "WARN"
	case slog.LevelError:
		levelStr = "ERROR"
	}
	return fmt.Sprintf("%s %-5s %s", e.Time.Format("15:04:05"), levelStr, e.Message)
}
