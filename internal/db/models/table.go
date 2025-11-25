// Package models contains data structures for database entities.
package models

import "fmt"

// Schema represents a PostgreSQL schema (namespace) with metadata.
type Schema struct {
	// OID is the unique identifier from pg_namespace.oid
	OID uint32
	// Name is the schema name (e.g., "public", "pg_catalog")
	Name string
	// Owner is the owner role name
	Owner string
	// IsSystem is true for pg_catalog, information_schema, and pg_toast*
	IsSystem bool
	// Tables contains tables in this schema (populated separately)
	Tables []Table
	// Expanded indicates whether the schema is expanded in tree view (UI state)
	Expanded bool
}

// Table represents a PostgreSQL table with size and statistics.
type Table struct {
	// OID is the unique identifier from pg_class.oid
	OID uint32
	// SchemaOID is the parent schema OID
	SchemaOID uint32
	// SchemaName is the parent schema name
	SchemaName string
	// Name is the table name (relname)
	Name string
	// TotalSize is the total size including indexes and TOAST (bytes)
	TotalSize int64
	// TableSize is the heap size only (bytes)
	TableSize int64
	// IndexesSize is the total size of all indexes (bytes)
	IndexesSize int64
	// ToastSize is the TOAST table size (bytes)
	ToastSize int64
	// RowCount is the live row count estimate (n_live_tup)
	RowCount int64
	// DeadRows is the dead row count (n_dead_tup) for bloat estimation
	DeadRows int64
	// BloatPct is the bloat percentage (from pgstattuple or estimated)
	BloatPct float64
	// BloatEstimated is true if bloat is estimated, false if from pgstattuple
	BloatEstimated bool
	// CacheHitRatio is heap_blks_hit / (hit + read) * 100
	CacheHitRatio float64
	// SeqScans is the sequential scan count
	SeqScans int64
	// IndexScans is the index scan count
	IndexScans int64
	// IsPartitioned is true if this is a partitioned parent (relkind = 'p')
	IsPartitioned bool
	// IsPartition is true if this is a partition child
	IsPartition bool
	// ParentOID is the parent table OID if this is a partition
	ParentOID *uint32
	// Partitions contains child partitions (populated separately)
	Partitions []Table
	// Indexes contains indexes on this table (populated separately)
	Indexes []Index
	// Expanded indicates whether partitions are expanded in tree view (UI state)
	Expanded bool
}

// Index represents a PostgreSQL index with usage statistics.
type Index struct {
	// OID is the unique identifier from pg_class.oid (index)
	OID uint32
	// TableOID is the parent table OID (pg_index.indrelid)
	TableOID uint32
	// SchemaName is the parent schema name
	SchemaName string
	// TableName is the parent table name
	TableName string
	// Name is the index name
	Name string
	// Size is the index size in bytes
	Size int64
	// ScanCount is the number of index scans (idx_scan)
	ScanCount int64
	// RowsRead is the number of rows read from index (idx_tup_read)
	RowsRead int64
	// RowsFetched is the number of rows fetched via index (idx_tup_fetch)
	RowsFetched int64
	// CacheHitRatio is idx_blks_hit / (hit + read) * 100
	CacheHitRatio float64
	// IsPrimary is true if this is a primary key index
	IsPrimary bool
	// IsUnique is true if this is a unique index
	IsUnique bool
	// IsUnused is true if ScanCount == 0
	IsUnused bool
}

// TableColumn represents a column definition within a table.
type TableColumn struct {
	// Position is the ordinal position (1-based, from attnum)
	Position int
	// Name is the column name
	Name string
	// DataType is the formatted type (e.g., "character varying(255)")
	DataType string
	// IsNullable is true if the column allows NULL
	IsNullable bool
	// DefaultValue is the default expression (nil if none)
	DefaultValue *string
}

// ConstraintType enumerates table constraint types.
type ConstraintType string

const (
	// ConstraintPrimaryKey represents a PRIMARY KEY constraint
	ConstraintPrimaryKey ConstraintType = "PK"
	// ConstraintForeignKey represents a FOREIGN KEY constraint
	ConstraintForeignKey ConstraintType = "FK"
	// ConstraintUnique represents a UNIQUE constraint
	ConstraintUnique ConstraintType = "UQ"
	// ConstraintCheck represents a CHECK constraint
	ConstraintCheck ConstraintType = "CK"
	// ConstraintNotNull represents a NOT NULL constraint
	ConstraintNotNull ConstraintType = "NN"
	// ConstraintExclusion represents an EXCLUSION constraint
	ConstraintExclusion ConstraintType = "EX"
)

// Constraint represents a table constraint.
type Constraint struct {
	// Name is the constraint name
	Name string
	// Type is the constraint type (PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK)
	Type ConstraintType
	// Definition is the constraint definition SQL
	Definition string
}

// TableDetails contains full table information for the details panel.
type TableDetails struct {
	// Table is the base table info
	Table Table
	// Columns contains column definitions
	Columns []TableColumn
	// Constraints contains all constraints
	Constraints []Constraint
	// Indexes contains index list with stats
	Indexes []Index
}

// FormatBytes converts bytes to human-readable format.
// Examples: 512 B, 1.5 KB, 45.2 MB, 1.23 GB
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes < KB:
		return fmt.Sprintf("%d B", bytes)
	case bytes < MB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	case bytes < GB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	}
}
