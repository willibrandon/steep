// Package init provides node initialization and snapshot management for bidirectional replication.
package init

import (
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// Types
// =============================================================================

// OverlapCategory represents the category of a row in overlap analysis.
type OverlapCategory string

const (
	CategoryMatch      OverlapCategory = "match"       // Same PK, identical data
	CategoryConflict   OverlapCategory = "conflict"    // Same PK, different data
	CategoryLocalOnly  OverlapCategory = "local_only"  // Only exists on local node (A)
	CategoryRemoteOnly OverlapCategory = "remote_only" // Only exists on remote node (B)
)

// ConflictStrategy defines how to resolve conflicts during bidirectional merge.
type ConflictStrategy string

const (
	StrategyPreferNodeA  ConflictStrategy = "prefer-node-a"
	StrategyPreferNodeB  ConflictStrategy = "prefer-node-b"
	StrategyLastModified ConflictStrategy = "last-modified"
	StrategyManual       ConflictStrategy = "manual"
)

// OverlapResult represents the result of comparing a single row.
type OverlapResult struct {
	PKValue    map[string]interface{} `json:"pk_value"`
	Category   OverlapCategory        `json:"category"`
	LocalHash  *int64                 `json:"local_hash,omitempty"`
	RemoteHash *int64                 `json:"remote_hash,omitempty"`
}

// OverlapSummary represents the summary of overlap analysis for a table.
type OverlapSummary struct {
	TableSchema string `json:"table_schema"`
	TableName   string `json:"table_name"`
	TotalRows   int64  `json:"total_rows"`
	Matches     int64  `json:"matches"`
	Conflicts   int64  `json:"conflicts"`
	LocalOnly   int64  `json:"local_only"`
	RemoteOnly  int64  `json:"remote_only"`
}

// MergeAuditEntry represents an entry in the merge audit log.
type MergeAuditEntry struct {
	ID          int64                  `json:"id"`
	MergeID     uuid.UUID              `json:"merge_id"`
	TableSchema string                 `json:"table_schema"`
	TableName   string                 `json:"table_name"`
	PKValue     map[string]interface{} `json:"pk_value"`
	Category    OverlapCategory        `json:"category"`
	Resolution  *string                `json:"resolution,omitempty"`
	NodeAValue  map[string]interface{} `json:"node_a_value,omitempty"`
	NodeBValue  map[string]interface{} `json:"node_b_value,omitempty"`
	ResolvedAt  time.Time              `json:"resolved_at"`
	ResolvedBy  *string                `json:"resolved_by,omitempty"`
}

// ConflictReport represents a report of conflicts for manual resolution.
type ConflictReport struct {
	MergeID    uuid.UUID        `json:"merge_id"`
	Table      string           `json:"table"`
	Conflicts  []ConflictDetail `json:"conflicts"`
	TotalCount int              `json:"total_count"`
}

// ConflictDetail represents details of a single conflict.
type ConflictDetail struct {
	PKValue             map[string]interface{} `json:"pk_value"`
	NodeAValue          map[string]interface{} `json:"node_a_value"`
	NodeBValue          map[string]interface{} `json:"node_b_value"`
	SuggestedResolution string                 `json:"suggested_resolution"`
}

// MergeResult represents the result of a bidirectional merge operation.
type MergeResult struct {
	MergeID             uuid.UUID        `json:"merge_id"`
	Tables              []string         `json:"tables"`
	Strategy            ConflictStrategy `json:"strategy"`
	DryRun              bool             `json:"dry_run"`
	StartedAt           time.Time        `json:"started_at"`
	CompletedAt         time.Time        `json:"completed_at"`
	TotalMatches        int64            `json:"total_matches"`
	TotalConflicts      int64            `json:"total_conflicts"`
	TotalLocalOnly      int64            `json:"total_local_only"`
	TotalRemoteOnly     int64            `json:"total_remote_only"`
	RowsTransferredAToB int64            `json:"rows_transferred_a_to_b"`
	RowsTransferredBToA int64            `json:"rows_transferred_b_to_a"`
	ConflictsResolved   int64            `json:"conflicts_resolved"`
	Errors              []string         `json:"errors,omitempty"`
}

// MergeTableInfo contains information about a table for merge operations.
type MergeTableInfo struct {
	Schema    string
	Name      string
	PKColumns []string
}

// MergeConfig contains configuration for a bidirectional merge operation.
type MergeConfig struct {
	Tables           []MergeTableInfo
	Strategy         ConflictStrategy
	RemoteServer     string
	QuiesceTimeoutMs int
	DryRun           bool
}

// PreflightResult contains the results of pre-flight checks.
type PreflightResult struct {
	SchemaMatch          bool     `json:"schema_match"`
	AllTableshavePK      bool     `json:"all_tables_have_pk"`
	NoActiveTransactions bool     `json:"no_active_transactions"`
	TrackCommitTimestamp bool     `json:"track_commit_timestamp"`
	Errors               []string `json:"errors,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
}

// FKDependency represents a foreign key dependency between tables.
type FKDependency struct {
	ChildSchema  string
	ChildTable   string
	ParentSchema string
	ParentTable  string
}

// columnInfo holds column metadata for COPY operations.
type columnInfo struct {
	Name     string
	DataType string
	Position int
}
