// Package models defines data structures for the replication daemon.
package models

import (
	"encoding/json"
	"time"
)

// SnapshotStatus represents the status of a snapshot.
type SnapshotStatus string

const (
	// SnapshotStatusPending means snapshot generation is in progress.
	SnapshotStatusPending SnapshotStatus = "pending"
	// SnapshotStatusComplete means snapshot is ready for application.
	SnapshotStatusComplete SnapshotStatus = "complete"
	// SnapshotStatusApplied means snapshot has been applied to a target node.
	SnapshotStatusApplied SnapshotStatus = "applied"
	// SnapshotStatusExpired means snapshot has expired and should be cleaned up.
	SnapshotStatusExpired SnapshotStatus = "expired"
)

// AllSnapshotStatuses returns all valid snapshot statuses.
func AllSnapshotStatuses() []SnapshotStatus {
	return []SnapshotStatus{
		SnapshotStatusPending,
		SnapshotStatusComplete,
		SnapshotStatusApplied,
		SnapshotStatusExpired,
	}
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

// Snapshot represents a generated snapshot manifest.
// This maps to the steep_repl.snapshots table.
type Snapshot struct {
	SnapshotID   string          `db:"snapshot_id" json:"snapshot_id"`
	SourceNodeID string          `db:"source_node_id" json:"source_node_id"`
	LSN          string          `db:"lsn" json:"lsn"`
	StoragePath  string          `db:"storage_path" json:"storage_path"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
	SizeBytes    int64           `db:"size_bytes" json:"size_bytes"`
	TableCount   int             `db:"table_count" json:"table_count"`
	Compression  CompressionType `db:"compression" json:"compression"`
	Checksum     string          `db:"checksum" json:"checksum"`
	ExpiresAt    *time.Time      `db:"expires_at" json:"expires_at,omitempty"`
	Status       SnapshotStatus  `db:"status" json:"status"`
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
