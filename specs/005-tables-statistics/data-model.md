# Data Model: Tables & Statistics Viewer

**Feature**: 005-tables-statistics
**Date**: 2025-11-24

## Overview

This document defines the data structures for the Tables & Statistics Viewer feature. All models follow Go conventions and integrate with the existing `internal/db/models/` package structure.

## Entity Definitions

### Schema

Represents a PostgreSQL schema (namespace) containing tables.

```go
// Schema represents a PostgreSQL schema with metadata.
type Schema struct {
    OID       uint32   // pg_namespace.oid
    Name      string   // nspname (e.g., "public", "pg_catalog")
    Owner     string   // owner name
    IsSystem  bool     // true for pg_catalog, information_schema, pg_toast*
    Tables    []Table  // tables in this schema (populated separately)
    Expanded  bool     // UI state: whether schema is expanded in tree view
}
```

**Attributes**:
| Field | Type | Source | Description |
|-------|------|--------|-------------|
| OID | uint32 | pg_namespace.oid | Unique identifier |
| Name | string | pg_namespace.nspname | Schema name |
| Owner | string | pg_roles.rolname | Owner role name |
| IsSystem | bool | Derived | true if pg_catalog, information_schema, or pg_toast* |
| Tables | []Table | pg_class | Tables in schema |
| Expanded | bool | UI state | Tree expansion state |

### Table

Represents a database table with statistics and size information.

```go
// Table represents a PostgreSQL table with size and statistics.
type Table struct {
    OID            uint32     // pg_class.oid
    SchemaName     string     // namespace name
    Name           string     // relname
    TotalSize      int64      // total size including indexes and TOAST (bytes)
    TableSize      int64      // heap size only (bytes)
    IndexesSize    int64      // total size of all indexes (bytes)
    ToastSize      int64      // TOAST table size (bytes)
    RowCount       int64      // n_live_tup estimate
    DeadRows       int64      // n_dead_tup (for bloat estimation)
    BloatPct       float64    // bloat percentage (from pgstattuple or estimated)
    BloatEstimated bool       // true if bloat is estimated, false if from pgstattuple
    CacheHitRatio  float64    // heap_blks_hit / (hit + read) * 100
    SeqScans       int64      // sequential scan count
    IndexScans     int64      // index scan count
    LastVacuum     *time.Time // last manual vacuum
    LastAutoVacuum *time.Time // last autovacuum
    LastAnalyze    *time.Time // last manual analyze
    LastAutoAnalyze *time.Time // last autoanalyze
    IsPartitioned  bool       // true if this is a partitioned parent
    IsPartition    bool       // true if this is a partition child
    ParentOID      *uint32    // parent table OID if partition
    Partitions     []Table    // child partitions (populated separately)
    Indexes        []Index    // indexes on this table (populated separately)
    Expanded       bool       // UI state: whether partitions are expanded
}
```

**Attributes**:
| Field | Type | Source | Description |
|-------|------|--------|-------------|
| OID | uint32 | pg_class.oid | Unique identifier |
| SchemaName | string | pg_namespace.nspname | Parent schema |
| Name | string | pg_class.relname | Table name |
| TotalSize | int64 | pg_total_relation_size() | Total including indexes/TOAST |
| TableSize | int64 | pg_relation_size() | Heap only |
| IndexesSize | int64 | pg_indexes_size() | All indexes combined |
| ToastSize | int64 | pg_total_relation_size(reltoastrelid) | TOAST data |
| RowCount | int64 | pg_stat_all_tables.n_live_tup | Live row estimate |
| DeadRows | int64 | pg_stat_all_tables.n_dead_tup | Dead tuples |
| BloatPct | float64 | pgstattuple or estimated | Bloat percentage |
| BloatEstimated | bool | Derived | Source of bloat value |
| CacheHitRatio | float64 | pg_statio_all_tables | Buffer cache efficiency |
| SeqScans | int64 | pg_stat_all_tables.seq_scan | Sequential scans |
| IndexScans | int64 | pg_stat_all_tables.idx_scan | Index scans |
| LastVacuum | *time.Time | pg_stat_all_tables.last_vacuum | Manual vacuum time |
| LastAutoVacuum | *time.Time | pg_stat_all_tables.last_autovacuum | Autovacuum time |
| LastAnalyze | *time.Time | pg_stat_all_tables.last_analyze | Manual analyze time |
| LastAutoAnalyze | *time.Time | pg_stat_all_tables.last_autoanalyze | Autoanalyze time |
| IsPartitioned | bool | pg_class.relkind = 'p' | Partitioned parent |
| IsPartition | bool | pg_inherits | Partition child |
| ParentOID | *uint32 | pg_inherits.inhparent | Parent table reference |
| Partitions | []Table | Derived | Child partitions |
| Indexes | []Index | pg_index | Table indexes |
| Expanded | bool | UI state | Partition expansion state |

**State Transitions**:
- Table can transition between healthy (<10% bloat) → warning (10-20%) → critical (>20%)
- BloatEstimated transitions to false when pgstattuple provides accurate measurement

### Index

Represents a table index with usage statistics.

```go
// Index represents a PostgreSQL index with usage statistics.
type Index struct {
    OID           uint32  // pg_class.oid (index)
    TableOID      uint32  // pg_index.indrelid
    SchemaName    string  // namespace name
    TableName     string  // parent table name
    Name          string  // index name
    Size          int64   // index size in bytes
    ScanCount     int64   // idx_scan
    RowsRead      int64   // idx_tup_read
    RowsFetched   int64   // idx_tup_fetch
    CacheHitRatio float64 // idx_blks_hit / (hit + read) * 100
    IsPrimary     bool    // indisprimary
    IsUnique      bool    // indisunique
    IsUnused      bool    // true if ScanCount == 0
}
```

**Attributes**:
| Field | Type | Source | Description |
|-------|------|--------|-------------|
| OID | uint32 | pg_class.oid | Index OID |
| TableOID | uint32 | pg_index.indrelid | Parent table |
| SchemaName | string | pg_namespace.nspname | Schema name |
| TableName | string | pg_class.relname | Parent table name |
| Name | string | pg_class.relname | Index name |
| Size | int64 | pg_relation_size() | Index size bytes |
| ScanCount | int64 | pg_stat_all_indexes.idx_scan | Scan count |
| RowsRead | int64 | pg_stat_all_indexes.idx_tup_read | Rows read |
| RowsFetched | int64 | pg_stat_all_indexes.idx_tup_fetch | Rows fetched |
| CacheHitRatio | float64 | pg_statio_all_indexes | Cache efficiency |
| IsPrimary | bool | pg_index.indisprimary | Primary key index |
| IsUnique | bool | pg_index.indisunique | Unique index |
| IsUnused | bool | Derived | ScanCount == 0 |

### TableColumn

Represents a column within a table (for details panel).

```go
// TableColumn represents a column definition.
type TableColumn struct {
    Position     int     // ordinal position (1-based)
    Name         string  // column name
    DataType     string  // formatted type (e.g., "character varying(255)")
    IsNullable   bool    // NOT NULL constraint
    DefaultValue *string // default expression (nil if none)
}
```

### Constraint

Represents a table constraint (for details panel).

```go
// Constraint represents a table constraint.
type Constraint struct {
    Name       string         // constraint name
    Type       ConstraintType // PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK
    Definition string         // constraint definition SQL
}

// ConstraintType enumerates constraint types.
type ConstraintType string

const (
    ConstraintPrimaryKey ConstraintType = "PRIMARY KEY"
    ConstraintForeignKey ConstraintType = "FOREIGN KEY"
    ConstraintUnique     ConstraintType = "UNIQUE"
    ConstraintCheck      ConstraintType = "CHECK"
)
```

### TableDetails

Aggregates detailed information for a selected table.

```go
// TableDetails contains full table information for the details panel.
type TableDetails struct {
    Table       Table           // base table info
    Columns     []TableColumn   // column definitions
    Constraints []Constraint    // all constraints
    Indexes     []Index         // index list with stats
}
```

## Relationships

```
Schema (1) ─────────────< Table (*)
                              │
                              ├─────────< Index (*)
                              │
                              ├─────────< TableColumn (*)
                              │
                              ├─────────< Constraint (*)
                              │
                              └─────────< Table (*) [partitions]
```

- Schema contains many Tables
- Table contains many Indexes, Columns, Constraints
- Partitioned Table contains many child Tables (partitions)
- Index belongs to one Table
- Column belongs to one Table
- Constraint belongs to one Table

## Validation Rules

### Table
- TotalSize >= TableSize + IndexesSize + ToastSize (accounting for overhead)
- RowCount >= 0
- BloatPct >= 0 and <= 100
- CacheHitRatio >= 0 and <= 100
- If IsPartition, ParentOID must be non-nil
- If IsPartitioned, relkind must be 'p'

### Index
- Size >= 0
- ScanCount >= 0
- IsUnused must equal (ScanCount == 0)
- CacheHitRatio >= 0 and <= 100

### Schema
- Name must not be empty
- IsSystem derived from name matching pg_catalog, information_schema, or pg_toast*

## Color Coding Rules (UI)

| Entity | Condition | Color |
|--------|-----------|-------|
| Table | BloatPct > 20 | Red (critical) |
| Table | BloatPct 10-20 | Yellow (warning) |
| Table | BloatPct < 10 | Default (healthy) |
| Index | IsUnused == true | Yellow (unused) |
| Schema | IsSystem == true | Muted (when visible) |

## Size Formatting

Sizes are stored in bytes but displayed in human-readable format:
- < 1KB: "{n} B"
- < 1MB: "{n.1} KB"
- < 1GB: "{n.1} MB"
- >= 1GB: "{n.2} GB"

```go
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
```
