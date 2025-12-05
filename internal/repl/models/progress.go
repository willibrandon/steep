// Package models defines data structures for the replication daemon.
package models

import "time"

// Phase represents the current phase of initialization.
type Phase string

const (
	// PhaseGeneration means snapshot generation is in progress.
	PhaseGeneration Phase = "generation"
	// PhaseApplication means snapshot application is in progress.
	PhaseApplication Phase = "application"
	// PhaseCatchingUp means WAL catch-up is in progress after snapshot.
	PhaseCatchingUp Phase = "catching_up"
)

// AllPhases returns all valid initialization phases.
func AllPhases() []Phase {
	return []Phase{
		PhaseGeneration,
		PhaseApplication,
		PhaseCatchingUp,
	}
}

// IsValid returns true if the phase is a recognized value.
func (p Phase) IsValid() bool {
	for _, valid := range AllPhases() {
		if p == valid {
			return true
		}
	}
	return false
}

// String returns the string representation of the phase.
func (p Phase) String() string {
	return string(p)
}

// InitProgress represents the real-time initialization progress for a node.
// This maps to the steep_repl.init_progress table.
type InitProgress struct {
	NodeID              string    `db:"node_id" json:"node_id"`
	Phase               Phase     `db:"phase" json:"phase"`
	OverallPercent      float64   `db:"overall_percent" json:"overall_percent"`
	TablesTotal         int       `db:"tables_total" json:"tables_total"`
	TablesCompleted     int       `db:"tables_completed" json:"tables_completed"`
	CurrentTable        *string   `db:"current_table" json:"current_table,omitempty"`
	CurrentTablePercent float64   `db:"current_table_percent" json:"current_table_percent"`
	RowsCopied          int64     `db:"rows_copied" json:"rows_copied"`
	BytesCopied         int64     `db:"bytes_copied" json:"bytes_copied"`
	ThroughputRowsSec   float64   `db:"throughput_rows_sec" json:"throughput_rows_sec"`
	StartedAt           time.Time `db:"started_at" json:"started_at"`
	ETASeconds          *int      `db:"eta_seconds" json:"eta_seconds,omitempty"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
	ParallelWorkers     int       `db:"parallel_workers" json:"parallel_workers"`
	ErrorMessage        *string   `db:"error_message" json:"error_message,omitempty"`
}

// NewInitProgress creates a new InitProgress with default values.
func NewInitProgress(nodeID string, phase Phase) *InitProgress {
	now := time.Now()
	return &InitProgress{
		NodeID:          nodeID,
		Phase:           phase,
		OverallPercent:  0,
		TablesTotal:     0,
		TablesCompleted: 0,
		RowsCopied:      0,
		BytesCopied:     0,
		StartedAt:       now,
		UpdatedAt:       now,
		ParallelWorkers: 1,
	}
}

// IsComplete returns true if initialization is complete (100%).
func (p *InitProgress) IsComplete() bool {
	return p.OverallPercent >= 100.0
}

// HasError returns true if there is an error message set.
func (p *InitProgress) HasError() bool {
	return p.ErrorMessage != nil && *p.ErrorMessage != ""
}

// ProgressRatio returns the progress as a ratio between 0.0 and 1.0.
func (p *InitProgress) ProgressRatio() float64 {
	return p.OverallPercent / 100.0
}

// EstimatedTimeRemaining returns the estimated time remaining based on ETA seconds.
// Returns nil if ETA is not set.
func (p *InitProgress) EstimatedTimeRemaining() *time.Duration {
	if p.ETASeconds == nil {
		return nil
	}
	d := time.Duration(*p.ETASeconds) * time.Second
	return &d
}

// ElapsedTime returns the duration since initialization started.
func (p *InitProgress) ElapsedTime() time.Duration {
	return time.Since(p.StartedAt)
}
