// Package models defines data structures for the replication daemon.
package models

import (
	"encoding/json"
	"time"
)

// SnapshotStatus represents the status of a snapshot.
type SnapshotStatus string

const (
	// SnapshotStatusPending means snapshot is pending start.
	SnapshotStatusPending SnapshotStatus = "pending"
	// SnapshotStatusGenerating means snapshot generation is in progress.
	SnapshotStatusGenerating SnapshotStatus = "generating"
	// SnapshotStatusComplete means snapshot is ready for application.
	SnapshotStatusComplete SnapshotStatus = "complete"
	// SnapshotStatusApplying means snapshot application is in progress.
	SnapshotStatusApplying SnapshotStatus = "applying"
	// SnapshotStatusApplied means snapshot has been applied to a target node.
	SnapshotStatusApplied SnapshotStatus = "applied"
	// SnapshotStatusFailed means snapshot operation failed.
	SnapshotStatusFailed SnapshotStatus = "failed"
	// SnapshotStatusCancelled means snapshot operation was cancelled.
	SnapshotStatusCancelled SnapshotStatus = "cancelled"
	// SnapshotStatusExpired means snapshot has expired and should be cleaned up.
	SnapshotStatusExpired SnapshotStatus = "expired"
)

// AllSnapshotStatuses returns all valid snapshot statuses.
func AllSnapshotStatuses() []SnapshotStatus {
	return []SnapshotStatus{
		SnapshotStatusPending,
		SnapshotStatusGenerating,
		SnapshotStatusComplete,
		SnapshotStatusApplying,
		SnapshotStatusApplied,
		SnapshotStatusFailed,
		SnapshotStatusCancelled,
		SnapshotStatusExpired,
	}
}

// SnapshotPhase represents the current phase of a snapshot operation.
type SnapshotPhase string

const (
	PhaseIdle        SnapshotPhase = "idle"
	PhaseSchema      SnapshotPhase = "schema"
	PhaseData        SnapshotPhase = "data"
	PhaseIndexes     SnapshotPhase = "indexes"
	PhaseConstraints SnapshotPhase = "constraints"
	PhaseSequences   SnapshotPhase = "sequences"
	PhaseVerify      SnapshotPhase = "verify"
)

// AllSnapshotPhases returns all valid snapshot phases.
func AllSnapshotPhases() []SnapshotPhase {
	return []SnapshotPhase{
		PhaseIdle,
		PhaseSchema,
		PhaseData,
		PhaseIndexes,
		PhaseConstraints,
		PhaseSequences,
		PhaseVerify,
	}
}

// String returns the string representation of the phase.
func (p SnapshotPhase) String() string {
	return string(p)
}

// IsValid returns true if the status is a recognized value.
func (s SnapshotStatus) IsValid() bool {
	for _, valid := range AllSnapshotStatuses() {
		if s == valid {
			return true
		}
	}
	return false
}

// String returns the string representation of the status.
func (s SnapshotStatus) String() string {
	return string(s)
}

// CompressionType represents the compression type used for snapshots.
type CompressionType string

const (
	CompressionNone CompressionType = "none"
	CompressionGzip CompressionType = "gzip"
	CompressionLZ4  CompressionType = "lz4"
	CompressionZstd CompressionType = "zstd"
)

// AllCompressionTypes returns all valid compression types.
func AllCompressionTypes() []CompressionType {
	return []CompressionType{
		CompressionNone,
		CompressionGzip,
		CompressionLZ4,
		CompressionZstd,
	}
}

// IsValid returns true if the compression type is recognized.
func (c CompressionType) IsValid() bool {
	for _, valid := range AllCompressionTypes() {
		if c == valid {
			return true
		}
	}
	return false
}

// String returns the string representation of the compression type.
func (c CompressionType) String() string {
	return string(c)
}

// Snapshot represents a generated snapshot manifest with progress tracking.
// This maps to the steep_repl.snapshots table.
type Snapshot struct {
	// Identity
	SnapshotID   string  `db:"snapshot_id" json:"snapshot_id"`
	SourceNodeID string  `db:"source_node_id" json:"source_node_id"`
	TargetNodeID *string `db:"target_node_id" json:"target_node_id,omitempty"`

	// Snapshot metadata
	LSN         *string         `db:"lsn" json:"lsn,omitempty"`
	StoragePath *string         `db:"storage_path" json:"storage_path,omitempty"`
	Compression CompressionType `db:"compression" json:"compression"`
	Checksum    *string         `db:"checksum" json:"checksum,omitempty"`

	// Status tracking
	Status       SnapshotStatus `db:"status" json:"status"`
	Phase        SnapshotPhase  `db:"phase" json:"phase"`
	ErrorMessage *string        `db:"error_message" json:"error_message,omitempty"`

	// Progress tracking
	OverallPercent     float32 `db:"overall_percent" json:"overall_percent"`
	CurrentTable       *string `db:"current_table" json:"current_table,omitempty"`
	TableCount         int     `db:"table_count" json:"table_count"`
	TablesCompleted    int     `db:"tables_completed" json:"tables_completed"`
	SizeBytes          int64   `db:"size_bytes" json:"size_bytes"`
	BytesWritten       int64   `db:"bytes_written" json:"bytes_written"`
	RowsTotal          int64   `db:"rows_total" json:"rows_total"`
	RowsWritten        int64   `db:"rows_written" json:"rows_written"`
	ThroughputBytesSec float32 `db:"throughput_bytes_sec" json:"throughput_bytes_sec"`
	ETASeconds         int     `db:"eta_seconds" json:"eta_seconds"`
	CompressionRatio   float32 `db:"compression_ratio" json:"compression_ratio"`

	// Timestamps
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	StartedAt   *time.Time `db:"started_at" json:"started_at,omitempty"`
	CompletedAt *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	ExpiresAt   *time.Time `db:"expires_at" json:"expires_at,omitempty"`
}

// IsExpired returns true if the snapshot has expired.
func (s *Snapshot) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}

// IsAvailable returns true if the snapshot is complete and not expired.
func (s *Snapshot) IsAvailable() bool {
	return s.Status == SnapshotStatusComplete && !s.IsExpired()
}

// IsActive returns true if the snapshot operation is in progress.
func (s *Snapshot) IsActive() bool {
	return s.Status == SnapshotStatusGenerating || s.Status == SnapshotStatusApplying
}

// IsFailed returns true if the snapshot operation failed.
func (s *Snapshot) IsFailed() bool {
	return s.Status == SnapshotStatusFailed || s.Status == SnapshotStatusCancelled
}

// ProgressPercent returns the overall progress as a percentage.
func (s *Snapshot) ProgressPercent() float32 {
	return s.OverallPercent
}

// GetLSN returns the LSN or empty string if nil.
func (s *Snapshot) GetLSN() string {
	if s.LSN == nil {
		return ""
	}
	return *s.LSN
}

// GetStoragePath returns the storage path or empty string if nil.
func (s *Snapshot) GetStoragePath() string {
	if s.StoragePath == nil {
		return ""
	}
	return *s.StoragePath
}

// SizeMB returns the snapshot size in megabytes.
func (s *Snapshot) SizeMB() float64 {
	return float64(s.SizeBytes) / (1024 * 1024)
}

// SizeGB returns the snapshot size in gigabytes.
func (s *Snapshot) SizeGB() float64 {
	return float64(s.SizeBytes) / (1024 * 1024 * 1024)
}

// SnapshotTableEntry represents a single table in a snapshot manifest.
type SnapshotTableEntry struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	RowCount  int64  `json:"row_count"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum"`
	File      string `json:"file"`
}

// FullTableName returns the fully qualified table name (schema.name).
func (e *SnapshotTableEntry) FullTableName() string {
	return e.Schema + "." + e.Name
}

// SnapshotSequenceEntry represents a sequence value in a snapshot manifest.
type SnapshotSequenceEntry struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Value  int64  `json:"value"`
}

// FullSequenceName returns the fully qualified sequence name.
func (e *SnapshotSequenceEntry) FullSequenceName() string {
	return e.Schema + "." + e.Name
}

// SnapshotManifest represents the manifest.json file in a snapshot.
type SnapshotManifest struct {
	SnapshotID      string                  `json:"snapshot_id"`
	SourceNode      string                  `json:"source_node"`
	LSN             string                  `json:"lsn"`
	CreatedAt       time.Time               `json:"created_at"`
	Tables          []SnapshotTableEntry    `json:"tables"`
	Sequences       []SnapshotSequenceEntry `json:"sequences"`
	TotalSizeBytes  int64                   `json:"total_size_bytes"`
	Compression     CompressionType         `json:"compression"`
	ParallelWorkers int                     `json:"parallel_workers"`
}

// TableCount returns the number of tables in the manifest.
func (m *SnapshotManifest) TableCount() int {
	return len(m.Tables)
}

// SequenceCount returns the number of sequences in the manifest.
func (m *SnapshotManifest) SequenceCount() int {
	return len(m.Sequences)
}

// ToJSON serializes the manifest to JSON.
func (m *SnapshotManifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ParseManifest deserializes a manifest from JSON.
func ParseManifest(data []byte) (*SnapshotManifest, error) {
	var m SnapshotManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SnapshotStep represents a step in the snapshot operation.
type SnapshotStep string

const (
	SnapshotStepSchema     SnapshotStep = "schema"
	SnapshotStepTables     SnapshotStep = "tables"
	SnapshotStepSequences  SnapshotStep = "sequences"
	SnapshotStepChecksums  SnapshotStep = "checksums"
	SnapshotStepFinalizing SnapshotStep = "finalizing"
)

// String returns the string representation of the step.
func (s SnapshotStep) String() string {
	return string(s)
}

// SnapshotProgress represents the current progress of a snapshot operation.
// This is an in-memory structure used for real-time progress tracking.
type SnapshotProgress struct {
	SnapshotID             string        `json:"snapshot_id"`
	Phase                  SnapshotPhase `json:"phase"`
	CurrentStep            SnapshotStep  `json:"current_step"`
	OverallPercent         float64       `json:"overall_percent"`
	TablesTotal            int           `json:"tables_total"`
	TablesCompleted        int           `json:"tables_completed"`
	CurrentTable           *string       `json:"current_table,omitempty"`
	CurrentTableBytes      int64         `json:"current_table_bytes"`
	CurrentTableTotalBytes int64         `json:"current_table_total_bytes"`
	BytesWritten           int64         `json:"bytes_written"`
	BytesTotal             int64         `json:"bytes_total"`
	RowsWritten            int64         `json:"rows_written"`
	RowsTotal              int64         `json:"rows_total"`
	ThroughputBytesSec     float64       `json:"throughput_bytes_sec"`
	ThroughputRowsSec      float64       `json:"throughput_rows_sec"`
	ETASeconds             int           `json:"eta_seconds"`
	StartedAt              time.Time     `json:"started_at"`
	UpdatedAt              time.Time     `json:"updated_at"`
	CompressionEnabled     bool          `json:"compression_enabled"`
	CompressionRatio       float64       `json:"compression_ratio"`
	ChecksumVerifications  int           `json:"checksum_verifications"`
	ChecksumsVerified      int           `json:"checksums_verified"`
	ChecksumsFailed        int           `json:"checksums_failed"`
	ErrorMessage           *string       `json:"error_message,omitempty"`
}

// RollingThroughput tracks throughput over a rolling time window.
type RollingThroughput struct {
	windowSize int           // Number of samples to keep
	samples    []throughputSample
}

type throughputSample struct {
	timestamp time.Time
	bytes     int64
	rows      int64
}

// NewRollingThroughput creates a new throughput tracker with the given window size.
func NewRollingThroughput(windowSizeSeconds int) *RollingThroughput {
	return &RollingThroughput{
		windowSize: windowSizeSeconds,
		samples:    make([]throughputSample, 0, windowSizeSeconds),
	}
}

// Add records a sample of bytes and rows processed.
func (r *RollingThroughput) Add(bytes, rows int64) {
	now := time.Now()
	r.samples = append(r.samples, throughputSample{
		timestamp: now,
		bytes:     bytes,
		rows:      rows,
	})

	// Remove samples outside the window
	cutoff := now.Add(-time.Duration(r.windowSize) * time.Second)
	newSamples := make([]throughputSample, 0, len(r.samples))
	for _, s := range r.samples {
		if s.timestamp.After(cutoff) {
			newSamples = append(newSamples, s)
		}
	}
	r.samples = newSamples
}

// BytesPerSec returns the rolling average bytes per second.
func (r *RollingThroughput) BytesPerSec() float64 {
	if len(r.samples) < 2 {
		return 0
	}

	var totalBytes int64
	for _, s := range r.samples {
		totalBytes += s.bytes
	}

	duration := r.samples[len(r.samples)-1].timestamp.Sub(r.samples[0].timestamp).Seconds()
	if duration <= 0 {
		return 0
	}

	return float64(totalBytes) / duration
}

// RowsPerSec returns the rolling average rows per second.
func (r *RollingThroughput) RowsPerSec() float64 {
	if len(r.samples) < 2 {
		return 0
	}

	var totalRows int64
	for _, s := range r.samples {
		totalRows += s.rows
	}

	duration := r.samples[len(r.samples)-1].timestamp.Sub(r.samples[0].timestamp).Seconds()
	if duration <= 0 {
		return 0
	}

	return float64(totalRows) / duration
}

// EstimateETA estimates the remaining time in seconds based on current throughput.
func (r *RollingThroughput) EstimateETA(remainingBytes int64) int {
	bytesPerSec := r.BytesPerSec()
	if bytesPerSec <= 0 {
		return 0
	}
	return int(float64(remainingBytes) / bytesPerSec)
}

// InitSlot represents a replication slot created for manual initialization.
// This maps to the steep_repl.init_slots table.
type InitSlot struct {
	SlotName   string     `db:"slot_name" json:"slot_name"`
	NodeID     string     `db:"node_id" json:"node_id"`
	LSN        string     `db:"lsn" json:"lsn"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	ExpiresAt  *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	UsedByNode *string    `db:"used_by_node" json:"used_by_node,omitempty"`
	UsedAt     *time.Time `db:"used_at" json:"used_at,omitempty"`
}

// IsExpired returns true if the slot has expired.
func (s *InitSlot) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}

// IsUsed returns true if the slot has been consumed.
func (s *InitSlot) IsUsed() bool {
	return s.UsedByNode != nil && *s.UsedByNode != ""
}

// IsAvailable returns true if the slot is not used and not expired.
func (s *InitSlot) IsAvailable() bool {
	return !s.IsUsed() && !s.IsExpired()
}
