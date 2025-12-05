package init

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/willibrandon/steep/internal/repl/models"
)

// EventType identifies the type of initialization event.
type EventType string

const (
	EventInitStarted      EventType = "init.started"
	EventInitCompleted    EventType = "init.completed"
	EventInitFailed       EventType = "init.failed"
	EventInitCancelled    EventType = "init.cancelled"
	EventPhaseStarted     EventType = "init.phase_started"
	EventPhaseCompleted   EventType = "init.phase_completed"
	EventTableStarted     EventType = "init.table_started"
	EventTableCompleted   EventType = "init.table_complete"
	EventTableFailed      EventType = "init.table_failed"
	EventStateChange      EventType = "init.state_change"
	EventSchemaMismatch   EventType = "schema.mismatch_detected"
	EventSchemaSyncApplied EventType = "schema.sync_applied"
)

// InitEvent represents a structured initialization event for logging.
type InitEvent struct {
	Timestamp       time.Time         `json:"timestamp"`
	Level           string            `json:"level"`
	Event           EventType         `json:"event"`
	NodeID          string            `json:"node_id"`
	SourceNode      string            `json:"source_node,omitempty"`
	Table           string            `json:"table,omitempty"`
	RowsCopied      int64             `json:"rows_copied,omitempty"`
	BytesCopied     int64             `json:"bytes_copied,omitempty"`
	DurationMs      int64             `json:"duration_ms,omitempty"`
	Phase           string            `json:"phase,omitempty"`
	OverallProgress float32           `json:"overall_progress,omitempty"`
	PreviousState   models.InitState  `json:"previous_state,omitempty"`
	NewState        models.InitState  `json:"new_state,omitempty"`
	Error           string            `json:"error,omitempty"`
	Details         map[string]any    `json:"details,omitempty"`
}

// Logger provides structured logging for initialization events.
type Logger struct {
	slog *slog.Logger
}

// NewLogger creates a new initialization event logger.
func NewLogger(logger *slog.Logger) *Logger {
	return &Logger{slog: logger}
}

// Log emits a structured initialization event.
func (l *Logger) Log(event InitEvent) {
	event.Timestamp = time.Now()
	if event.Level == "" {
		event.Level = "info"
	}

	// Convert to JSON for structured logging
	data, _ := json.Marshal(event)

	// Log at appropriate level
	switch event.Level {
	case "error":
		l.slog.Error(string(event.Event), "event", string(data))
	case "warn":
		l.slog.Warn(string(event.Event), "event", string(data))
	case "debug":
		l.slog.Debug(string(event.Event), "event", string(data))
	default:
		l.slog.Info(string(event.Event), "event", string(data))
	}
}

// LogInitStarted logs the start of initialization.
func (l *Logger) LogInitStarted(nodeID, sourceNode, method string) {
	l.Log(InitEvent{
		Event:      EventInitStarted,
		NodeID:     nodeID,
		SourceNode: sourceNode,
		Details:    map[string]any{"method": method},
	})
}

// LogInitCompleted logs successful completion of initialization.
func (l *Logger) LogInitCompleted(nodeID string, durationMs int64, rowsCopied, bytesCopied int64) {
	l.Log(InitEvent{
		Event:      EventInitCompleted,
		NodeID:     nodeID,
		DurationMs: durationMs,
		RowsCopied: rowsCopied,
		BytesCopied: bytesCopied,
	})
}

// LogInitFailed logs initialization failure.
func (l *Logger) LogInitFailed(nodeID string, err error) {
	l.Log(InitEvent{
		Level:  "error",
		Event:  EventInitFailed,
		NodeID: nodeID,
		Error:  err.Error(),
	})
}

// LogStateChange logs a state transition.
func (l *Logger) LogStateChange(nodeID string, previousState, newState models.InitState) {
	l.Log(InitEvent{
		Event:         EventStateChange,
		NodeID:        nodeID,
		PreviousState: previousState,
		NewState:      newState,
	})
}

// LogTableComplete logs completion of a single table.
func (l *Logger) LogTableComplete(nodeID, table string, rowsCopied int64, durationMs int64, overallProgress float32) {
	l.Log(InitEvent{
		Event:           EventTableCompleted,
		NodeID:          nodeID,
		Table:           table,
		RowsCopied:      rowsCopied,
		DurationMs:      durationMs,
		OverallProgress: overallProgress,
	})
}

// LogPhaseStarted logs the start of an initialization phase.
func (l *Logger) LogPhaseStarted(nodeID, phase string) {
	l.Log(InitEvent{
		Event:  EventPhaseStarted,
		NodeID: nodeID,
		Phase:  phase,
	})
}

// LogPhaseCompleted logs the completion of an initialization phase.
func (l *Logger) LogPhaseCompleted(nodeID, phase string, durationMs int64) {
	l.Log(InitEvent{
		Event:      EventPhaseCompleted,
		NodeID:     nodeID,
		Phase:      phase,
		DurationMs: durationMs,
	})
}

// LogSchemaMismatch logs detection of schema mismatch.
func (l *Logger) LogSchemaMismatch(nodeID, table string, differences []ColumnDifference) {
	l.Log(InitEvent{
		Level:  "warn",
		Event:  EventSchemaMismatch,
		NodeID: nodeID,
		Table:  table,
		Details: map[string]any{
			"difference_count": len(differences),
		},
	})
}
