// Package models defines data structures for the replication daemon.
package models

import (
	"encoding/json"
	"time"
)

// ComparisonStatus represents the result of comparing two fingerprints.
type ComparisonStatus string

const (
	// ComparisonMatch means fingerprints are identical.
	ComparisonMatch ComparisonStatus = "match"
	// ComparisonMismatch means fingerprints differ.
	ComparisonMismatch ComparisonStatus = "mismatch"
	// ComparisonLocalOnly means table exists only on local node.
	ComparisonLocalOnly ComparisonStatus = "local_only"
	// ComparisonRemoteOnly means table exists only on remote node.
	ComparisonRemoteOnly ComparisonStatus = "remote_only"
)

// AllComparisonStatuses returns all valid comparison statuses.
func AllComparisonStatuses() []ComparisonStatus {
	return []ComparisonStatus{
		ComparisonMatch,
		ComparisonMismatch,
		ComparisonLocalOnly,
		ComparisonRemoteOnly,
	}
}

// IsValid returns true if the status is a recognized value.
func (s ComparisonStatus) IsValid() bool {
	for _, valid := range AllComparisonStatuses() {
		if s == valid {
			return true
		}
	}
	return false
}

// String returns the string representation of the status.
func (s ComparisonStatus) String() string {
	return string(s)
}

// ColumnDefinition represents a single column's schema definition.
type ColumnDefinition struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Default  *string `json:"default,omitempty"`
	Nullable string  `json:"nullable"` // "YES" or "NO"
	Position int     `json:"position"`
}

// SchemaFingerprint represents a stored schema fingerprint for a table.
// This maps to the steep_repl.schema_fingerprints table.
type SchemaFingerprint struct {
	TableSchema       string             `db:"table_schema" json:"table_schema"`
	TableName         string             `db:"table_name" json:"table_name"`
	Fingerprint       string             `db:"fingerprint" json:"fingerprint"`
	ColumnCount       int                `db:"column_count" json:"column_count"`
	CapturedAt        time.Time          `db:"captured_at" json:"captured_at"`
	ColumnDefinitions json.RawMessage    `db:"column_definitions" json:"column_definitions,omitempty"`
}

// FullTableName returns the fully qualified table name (schema.table).
func (f *SchemaFingerprint) FullTableName() string {
	return f.TableSchema + "." + f.TableName
}

// GetColumns parses and returns the column definitions.
func (f *SchemaFingerprint) GetColumns() ([]ColumnDefinition, error) {
	if len(f.ColumnDefinitions) == 0 {
		return nil, nil
	}
	var cols []ColumnDefinition
	if err := json.Unmarshal(f.ColumnDefinitions, &cols); err != nil {
		return nil, err
	}
	return cols, nil
}

// FingerprintComparison represents the result of comparing two schema fingerprints.
type FingerprintComparison struct {
	TableSchema       string           `json:"table_schema"`
	TableName         string           `json:"table_name"`
	LocalFingerprint  string           `json:"local_fingerprint,omitempty"`
	RemoteFingerprint string           `json:"remote_fingerprint,omitempty"`
	Status            ComparisonStatus `json:"status"`
}

// FullTableName returns the fully qualified table name (schema.table).
func (c *FingerprintComparison) FullTableName() string {
	return c.TableSchema + "." + c.TableName
}

// IsMatch returns true if fingerprints match.
func (c *FingerprintComparison) IsMatch() bool {
	return c.Status == ComparisonMatch
}

// SchemaComparisonResult represents the overall result of comparing schemas between nodes.
type SchemaComparisonResult struct {
	LocalNodeID     string                  `json:"local_node_id"`
	RemoteNodeID    string                  `json:"remote_node_id"`
	ComparedAt      time.Time               `json:"compared_at"`
	TotalTables     int                     `json:"total_tables"`
	MatchingTables  int                     `json:"matching_tables"`
	MismatchedTables int                    `json:"mismatched_tables"`
	LocalOnlyTables int                     `json:"local_only_tables"`
	RemoteOnlyTables int                    `json:"remote_only_tables"`
	Comparisons     []FingerprintComparison `json:"comparisons"`
}

// HasMismatches returns true if there are any schema mismatches.
func (r *SchemaComparisonResult) HasMismatches() bool {
	return r.MismatchedTables > 0 || r.LocalOnlyTables > 0 || r.RemoteOnlyTables > 0
}

// AllMatch returns true if all schemas match.
func (r *SchemaComparisonResult) AllMatch() bool {
	return !r.HasMismatches()
}

// GetMismatches returns only the mismatched comparisons.
func (r *SchemaComparisonResult) GetMismatches() []FingerprintComparison {
	var mismatches []FingerprintComparison
	for _, c := range r.Comparisons {
		if c.Status != ComparisonMatch {
			mismatches = append(mismatches, c)
		}
	}
	return mismatches
}
