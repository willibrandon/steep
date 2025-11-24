# Contract: Tables Data Provider

**Feature**: 005-tables-statistics
**Package**: `internal/db/queries`

## Interface Definition

```go
// TableDataProvider defines the interface for fetching table statistics.
// This interface enables testing with mock implementations.
type TableDataProvider interface {
    // GetSchemas returns all schemas with basic metadata.
    // Does not populate Tables slice (call GetTablesForSchema separately).
    GetSchemas(ctx context.Context) ([]models.Schema, error)

    // GetTablesWithStats returns all tables with size and statistics.
    // Includes partition relationships but not detailed column/constraint info.
    GetTablesWithStats(ctx context.Context) ([]models.Table, error)

    // GetIndexesWithStats returns all indexes with usage statistics.
    GetIndexesWithStats(ctx context.Context) ([]models.Index, error)

    // GetPartitionHierarchy returns parent-child partition relationships.
    GetPartitionHierarchy(ctx context.Context) (map[uint32][]uint32, error)

    // GetTableDetails returns full details for a specific table.
    // Includes columns, constraints, and indexes.
    GetTableDetails(ctx context.Context, tableOID uint32) (*models.TableDetails, error)

    // CheckPgstattupleExtension returns true if pgstattuple is installed.
    CheckPgstattupleExtension(ctx context.Context) (bool, error)

    // InstallPgstattupleExtension attempts to CREATE EXTENSION pgstattuple.
    // Returns error if insufficient privileges or other failure.
    InstallPgstattupleExtension(ctx context.Context) error

    // GetTableBloat returns accurate bloat percentage using pgstattuple.
    // Returns error if pgstattuple not available.
    GetTableBloat(ctx context.Context, tableOID uint32) (float64, error)

    // ExecuteVacuum runs VACUUM on the specified table.
    // Returns error if readonly mode or insufficient privileges.
    ExecuteVacuum(ctx context.Context, schemaName, tableName string) error

    // ExecuteAnalyze runs ANALYZE on the specified table.
    ExecuteAnalyze(ctx context.Context, schemaName, tableName string) error

    // ExecuteReindex runs REINDEX TABLE on the specified table.
    ExecuteReindex(ctx context.Context, schemaName, tableName string) error
}
```

## Function Signatures

### GetSchemas

```go
func GetSchemas(ctx context.Context, pool *pgxpool.Pool) ([]models.Schema, error)
```

**SQL Query**:
```sql
SELECT
  nsp.oid,
  nsp.nspname,
  COALESCE(r.rolname, '') as owner
FROM pg_namespace nsp
LEFT JOIN pg_roles r ON r.oid = nsp.nspowner
WHERE nsp.nspname NOT LIKE 'pg_temp_%'
  AND nsp.nspname NOT LIKE 'pg_toast_temp_%'
ORDER BY nsp.nspname;
```

**Returns**: All schemas including system schemas (filtered in UI layer for toggle support)

### GetTablesWithStats

```go
func GetTablesWithStats(ctx context.Context, pool *pgxpool.Pool) ([]models.Table, error)
```

**SQL Query**: See research.md "Table Statistics Query"

**Performance**: ~250ms for 1000+ tables

### GetIndexesWithStats

```go
func GetIndexesWithStats(ctx context.Context, pool *pgxpool.Pool) ([]models.Index, error)
```

**SQL Query**: See research.md "Index Statistics Query"

**Performance**: ~150ms

### GetPartitionHierarchy

```go
func GetPartitionHierarchy(ctx context.Context, pool *pgxpool.Pool) (map[uint32][]uint32, error)
```

**Returns**: Map of parent OID → slice of child OIDs

**SQL Query**: See research.md "Partition Hierarchy Detection"

**Performance**: ~50ms

### GetTableDetails

```go
func GetTableDetails(ctx context.Context, pool *pgxpool.Pool, tableOID uint32) (*models.TableDetails, error)
```

**Executes**:
1. Column definitions query
2. Constraints query
3. Returns aggregated TableDetails

**Performance**: ~100ms total (2 queries)

### CheckPgstattupleExtension

```go
func CheckPgstattupleExtension(ctx context.Context, pool *pgxpool.Pool) (bool, error)
```

**SQL Query**:
```sql
SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pgstattuple');
```

**Performance**: <10ms

### InstallPgstattupleExtension

```go
func InstallPgstattupleExtension(ctx context.Context, pool *pgxpool.Pool) error
```

**SQL Query**:
```sql
CREATE EXTENSION pgstattuple;
```

**Error Handling**: Returns descriptive error for permission denied

### GetTableBloat

```go
func GetTableBloat(ctx context.Context, pool *pgxpool.Pool, tableOID uint32) (float64, error)
```

**SQL Query**:
```sql
SELECT dead_tuple_percent FROM pgstattuple($1);
```

**Performance**: Variable (can be slow for large tables)

### Maintenance Operations

```go
func ExecuteVacuum(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error
func ExecuteAnalyze(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error
func ExecuteReindex(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error
```

**SQL Queries**:
```sql
VACUUM "schema"."table";
ANALYZE "schema"."table";
REINDEX TABLE "schema"."table";
```

**Note**: Schema and table names must be properly quoted to prevent SQL injection.

## Error Types

```go
var (
    ErrPgstattupleNotInstalled = errors.New("pgstattuple extension not installed")
    ErrInsufficientPrivileges  = errors.New("insufficient privileges for operation")
    ErrReadOnlyMode            = errors.New("operation not allowed in readonly mode")
    ErrTableNotFound           = errors.New("table not found")
)
```

## Usage Example

```go
provider := queries.NewTableDataProvider(pool)

// Initial load
schemas, _ := provider.GetSchemas(ctx)
tables, _ := provider.GetTablesWithStats(ctx)
indexes, _ := provider.GetIndexesWithStats(ctx)
partitions, _ := provider.GetPartitionHierarchy(ctx)

// Build tree structure
tree := buildTableTree(schemas, tables, indexes, partitions)

// On-demand details
details, _ := provider.GetTableDetails(ctx, selectedTable.OID)

// Maintenance (if not readonly)
if !readonlyMode {
    provider.ExecuteVacuum(ctx, "public", "users")
}
```
