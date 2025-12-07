// Package models defines data structures for the replication daemon.
package models

import (
	"slices"
	"time"
)

// SnapshotPhase represents the current phase of a two-phase snapshot operation.
type SnapshotPhase string

const (
	// SnapshotPhaseGeneration means snapshot generation is in progress.
	SnapshotPhaseGeneration SnapshotPhase = "generation"
	// SnapshotPhaseApplication means snapshot application is in progress.
	SnapshotPhaseApplication SnapshotPhase = "application"
)

// AllSnapshotPhases returns all valid snapshot phases.
func AllSnapshotPhases() []SnapshotPhase {
	return []SnapshotPhase{
		SnapshotPhaseGeneration,
		SnapshotPhaseApplication,
	}
}

// IsValid returns true if the phase is a recognized value.
func (p SnapshotPhase) IsValid() bool {
	for _, valid := range AllSnapshotPhases() {
		if p == valid {
			return true
		}
	}
	return false
}

// String returns the string representation of the phase.
func (p SnapshotPhase) String() string {
	return string(p)
}

// SnapshotStep represents the current step within a snapshot phase.
type SnapshotStep string

const (
	// SnapshotStepSchema means schema export/import is in progress.
	SnapshotStepSchema SnapshotStep = "schema"
	// SnapshotStepTables means table data export/import is in progress.
	SnapshotStepTables SnapshotStep = "tables"
	// SnapshotStepSequences means sequence capture/restore is in progress.
	SnapshotStepSequences SnapshotStep = "sequences"
	// SnapshotStepChecksums means checksum verification is in progress.
	SnapshotStepChecksums SnapshotStep = "checksums"
	// SnapshotStepFinalizing means final manifest/metadata operations.
	SnapshotStepFinalizing SnapshotStep = "finalizing"
)

// AllSnapshotSteps returns all valid snapshot steps.
func AllSnapshotSteps() []SnapshotStep {
	return []SnapshotStep{
		SnapshotStepSchema,
		SnapshotStepTables,
		SnapshotStepSequences,
		SnapshotStepChecksums,
		SnapshotStepFinalizing,
	}
}

// IsValid returns true if the step is a recognized value.
func (s SnapshotStep) IsValid() bool {
	return slices.Contains(AllSnapshotSteps(), s)
}

// String returns the string representation of the step.
func (s SnapshotStep) String() string {
	return string(s)
}

// SnapshotProgress represents real-time progress of a two-phase snapshot operation.
// This enables visibility into generation and application progress for DBAs.
// Implements T087a: Progress tracking infrastructure.
type SnapshotProgress struct {
	// Identity
	SnapshotID string `db:"snapshot_id" json:"snapshot_id"`

	// Phase tracking
	Phase          SnapshotPhase `db:"phase" json:"phase"`
	OverallPercent float64       `db:"overall_percent" json:"overall_percent"`
	CurrentStep    SnapshotStep  `db:"current_step" json:"current_step"`

	// Table tracking
	TablesTotal            int     `db:"tables_total" json:"tables_total"`
	TablesCompleted        int     `db:"tables_completed" json:"tables_completed"`
	CurrentTable           *string `db:"current_table" json:"current_table,omitempty"`
	CurrentTableBytes      int64   `db:"current_table_bytes" json:"current_table_bytes"`
	CurrentTableTotalBytes int64   `db:"current_table_total_bytes" json:"current_table_total_bytes"`

	// Byte/row tracking
	BytesWritten int64 `db:"bytes_written" json:"bytes_written"`
	BytesTotal   int64 `db:"bytes_total" json:"bytes_total"`
	RowsWritten  int64 `db:"rows_written" json:"rows_written"`
	RowsTotal    int64 `db:"rows_total" json:"rows_total"`

	// Throughput (rolling 10-second average)
	ThroughputBytesSec float64 `db:"throughput_bytes_sec" json:"throughput_bytes_sec"`
	ThroughputRowsSec  float64 `db:"throughput_rows_sec" json:"throughput_rows_sec"`

	// Timing
	StartedAt  time.Time `db:"started_at" json:"started_at"`
	ETASeconds int       `db:"eta_seconds" json:"eta_seconds"`

	// Compression
	CompressionEnabled bool    `db:"compression_enabled" json:"compression_enabled"`
	CompressionRatio   float64 `db:"compression_ratio" json:"compression_ratio"`

	// Checksum verification (application phase)
	ChecksumVerifications int `db:"checksum_verifications" json:"checksum_verifications"`
	ChecksumsVerified     int `db:"checksums_verified" json:"checksums_verified"`
	ChecksumsFailed       int `db:"checksums_failed" json:"checksums_failed"`

	// Metadata
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
	ErrorMessage *string   `db:"error_message" json:"error_message,omitempty"`
}

// NewSnapshotProgress creates a new SnapshotProgress with default values.
func NewSnapshotProgress(snapshotID string, phase SnapshotPhase) *SnapshotProgress {
	now := time.Now()
	return &SnapshotProgress{
		SnapshotID:     snapshotID,
		Phase:          phase,
		OverallPercent: 0,
		CurrentStep:    SnapshotStepSchema,
		StartedAt:      now,
		UpdatedAt:      now,
	}
}

// IsComplete returns true if the snapshot operation is complete (100%).
func (p *SnapshotProgress) IsComplete() bool {
	return p.OverallPercent >= 100.0
}

// HasError returns true if there is an error message set.
func (p *SnapshotProgress) HasError() bool {
	return p.ErrorMessage != nil && *p.ErrorMessage != ""
}

// ProgressRatio returns the progress as a ratio between 0.0 and 1.0.
func (p *SnapshotProgress) ProgressRatio() float64 {
	return p.OverallPercent / 100.0
}

// EstimatedTimeRemaining returns the estimated time remaining.
// Returns 0 if ETA is not set.
func (p *SnapshotProgress) EstimatedTimeRemaining() time.Duration {
	return time.Duration(p.ETASeconds) * time.Second
}

// ElapsedTime returns the duration since the operation started.
func (p *SnapshotProgress) ElapsedTime() time.Duration {
	return time.Since(p.StartedAt)
}

// TableProgressRatio returns the current table progress as a ratio (0.0-1.0).
func (p *SnapshotProgress) TableProgressRatio() float64 {
	if p.CurrentTableTotalBytes == 0 {
		return 0
	}
	return float64(p.CurrentTableBytes) / float64(p.CurrentTableTotalBytes)
}

// ThroughputMBSec returns throughput in megabytes per second.
func (p *SnapshotProgress) ThroughputMBSec() float64 {
	return p.ThroughputBytesSec / (1024 * 1024)
}

// BytesWrittenMB returns bytes written in megabytes.
func (p *SnapshotProgress) BytesWrittenMB() float64 {
	return float64(p.BytesWritten) / (1024 * 1024)
}

// BytesWrittenGB returns bytes written in gigabytes.
func (p *SnapshotProgress) BytesWrittenGB() float64 {
	return float64(p.BytesWritten) / (1024 * 1024 * 1024)
}

// BytesTotalMB returns total bytes in megabytes.
func (p *SnapshotProgress) BytesTotalMB() float64 {
	return float64(p.BytesTotal) / (1024 * 1024)
}

// BytesTotalGB returns total bytes in gigabytes.
func (p *SnapshotProgress) BytesTotalGB() float64 {
	return float64(p.BytesTotal) / (1024 * 1024 * 1024)
}

// ChecksumVerificationComplete returns true if all checksums have been verified.
func (p *SnapshotProgress) ChecksumVerificationComplete() bool {
	return p.ChecksumVerifications > 0 && p.ChecksumsVerified+p.ChecksumsFailed >= p.ChecksumVerifications
}

// ChecksumVerificationPassed returns true if all checksums passed.
func (p *SnapshotProgress) ChecksumVerificationPassed() bool {
	return p.ChecksumVerificationComplete() && p.ChecksumsFailed == 0
}

// SetError sets the error message on the progress.
func (p *SnapshotProgress) SetError(err error) {
	if err != nil {
		msg := err.Error()
		p.ErrorMessage = &msg
	}
}

// EffectiveCompressionRatio returns the compression ratio as a display string.
// Returns "N/A" if compression is not enabled.
func (p *SnapshotProgress) EffectiveCompressionRatio() string {
	if !p.CompressionEnabled {
		return "N/A"
	}
	if p.CompressionRatio == 0 {
		return "calculating..."
	}
	return formatCompressionRatio(p.CompressionRatio)
}

// formatCompressionRatio formats a compression ratio for display.
func formatCompressionRatio(ratio float64) string {
	if ratio >= 1 {
		return "1:" + formatFloat(ratio, 1)
	}
	return formatFloat(1/ratio, 1) + ":1"
}

// formatFloat formats a float with the given decimal places.
func formatFloat(f float64, decimals int) string {
	if decimals == 1 {
		return string([]byte{
			byte('0' + int(f)),
			'.',
			byte('0' + int((f-float64(int(f)))*10)),
		})
	}
	return string([]byte{byte('0' + int(f))})
}

// RollingThroughput tracks throughput over a rolling window.
// Used to calculate smooth throughput averages for display.
type RollingThroughput struct {
	samples    []throughputSample
	windowSize int // in seconds
}

type throughputSample struct {
	timestamp time.Time
	bytes     int64
	rows      int64
}

// NewRollingThroughput creates a new rolling throughput tracker.
// windowSize is the rolling average window in seconds (default 10).
func NewRollingThroughput(windowSize int) *RollingThroughput {
	if windowSize <= 0 {
		windowSize = 10
	}
	return &RollingThroughput{
		samples:    make([]throughputSample, 0, windowSize*2),
		windowSize: windowSize,
	}
}

// Add records a new throughput sample.
func (r *RollingThroughput) Add(bytes, rows int64) {
	now := time.Now()
	r.samples = append(r.samples, throughputSample{
		timestamp: now,
		bytes:     bytes,
		rows:      rows,
	})
	// Prune old samples outside the window
	cutoff := now.Add(-time.Duration(r.windowSize) * time.Second)
	start := 0
	for i, s := range r.samples {
		if s.timestamp.After(cutoff) {
			start = i
			break
		}
	}
	if start > 0 {
		r.samples = r.samples[start:]
	}
}

// BytesPerSec returns the rolling average bytes per second.
func (r *RollingThroughput) BytesPerSec() float64 {
	if len(r.samples) < 2 {
		return 0
	}
	first := r.samples[0]
	last := r.samples[len(r.samples)-1]
	duration := last.timestamp.Sub(first.timestamp).Seconds()
	if duration <= 0 {
		return 0
	}
	return float64(last.bytes-first.bytes) / duration
}

// RowsPerSec returns the rolling average rows per second.
func (r *RollingThroughput) RowsPerSec() float64 {
	if len(r.samples) < 2 {
		return 0
	}
	first := r.samples[0]
	last := r.samples[len(r.samples)-1]
	duration := last.timestamp.Sub(first.timestamp).Seconds()
	if duration <= 0 {
		return 0
	}
	return float64(last.rows-first.rows) / duration
}

// EstimateETA calculates ETA based on remaining bytes and current throughput.
func (r *RollingThroughput) EstimateETA(remainingBytes int64) int {
	bps := r.BytesPerSec()
	if bps <= 0 {
		return 0
	}
	return int(float64(remainingBytes) / bps)
}
