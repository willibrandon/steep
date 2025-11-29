// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// Error types for table queries
var (
	// ErrPgstattupleNotInstalled indicates pgstattuple extension is not installed
	ErrPgstattupleNotInstalled = errors.New("pgstattuple extension not installed")
	// ErrInsufficientPrivileges indicates the user lacks required privileges
	ErrInsufficientPrivileges = errors.New("insufficient privileges for operation")
	// ErrTableNotFound indicates the requested table was not found
	ErrTableNotFound = errors.New("table not found")
)

// GetSchemas retrieves all schemas with basic metadata.
// Returns all schemas including system schemas (filtering done in UI layer for toggle support).
func GetSchemas(ctx context.Context, pool *pgxpool.Pool) ([]models.Schema, error) {
	query := `
		SELECT
			nsp.oid,
			nsp.nspname,
			COALESCE(r.rolname, '') as owner
		FROM pg_namespace nsp
		LEFT JOIN pg_roles r ON r.oid = nsp.nspowner
		WHERE nsp.nspname NOT LIKE 'pg_temp_%'
		  AND nsp.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY nsp.nspname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_namespace: %w", err)
	}
	defer rows.Close()

	var schemas []models.Schema
	for rows.Next() {
		var schema models.Schema
		err := rows.Scan(
			&schema.OID,
			&schema.Name,
			&schema.Owner,
		)
		if err != nil {
			return nil, fmt.Errorf("scan schema row: %w", err)
		}
		// Determine if this is a system schema
		schema.IsSystem = schema.Name == "pg_catalog" ||
			schema.Name == "information_schema" ||
			strings.HasPrefix(schema.Name, "pg_toast")
		schemas = append(schemas, schema)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schemas: %w", err)
	}

	return schemas, nil
}

// GetTablesWithStats retrieves all tables with size and statistics.
// Includes partition relationships but not detailed column/constraint info.
// Also includes vacuum/analyze status from pg_stat_all_tables.
func GetTablesWithStats(ctx context.Context, pool *pgxpool.Pool) ([]models.Table, error) {
	query := `
		SELECT
			t.oid as table_oid,
			nsp.oid as schema_oid,
			nsp.nspname as schema_name,
			t.relname as table_name,
			pg_total_relation_size(t.oid) as total_size_bytes,
			pg_relation_size(t.oid) as table_size_bytes,
			pg_indexes_size(t.oid) as indexes_size_bytes,
			COALESCE(pg_total_relation_size(t.reltoastrelid), 0) as toast_size_bytes,
			COALESCE(s.n_live_tup, 0) as row_count,
			COALESCE(s.n_dead_tup, 0) as dead_rows,
			COALESCE(
				ROUND(100.0 * io.heap_blks_hit /
					NULLIF(io.heap_blks_hit + io.heap_blks_read, 0), 2),
				0) as cache_hit_ratio,
			COALESCE(s.seq_scan, 0) as seq_scans,
			COALESCE(s.idx_scan, 0) as index_scans,
			t.relkind = 'p' as is_partitioned,
			s.last_vacuum,
			s.last_autovacuum,
			s.last_analyze,
			s.last_autoanalyze,
			COALESCE(s.vacuum_count, 0) as vacuum_count,
			COALESCE(s.autovacuum_count, 0) as autovacuum_count,
			COALESCE(
				(SELECT (string_to_array(unnest, '='))[2]::boolean
				 FROM unnest(t.reloptions)
				 WHERE unnest LIKE 'autovacuum_enabled=%'),
				true
			) AS autovacuum_enabled
		FROM pg_class t
		JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
		LEFT JOIN pg_stat_all_tables s ON s.relid = t.oid
		LEFT JOIN pg_statio_all_tables io ON io.relid = t.oid
		WHERE t.relkind IN ('r', 'p')
		ORDER BY nsp.nspname, t.relname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tables with stats: %w", err)
	}
	defer rows.Close()

	var tables []models.Table
	for rows.Next() {
		var table models.Table
		err := rows.Scan(
			&table.OID,
			&table.SchemaOID,
			&table.SchemaName,
			&table.Name,
			&table.TotalSize,
			&table.TableSize,
			&table.IndexesSize,
			&table.ToastSize,
			&table.RowCount,
			&table.DeadRows,
			&table.CacheHitRatio,
			&table.SeqScans,
			&table.IndexScans,
			&table.IsPartitioned,
			&table.LastVacuum,
			&table.LastAutovacuum,
			&table.LastAnalyze,
			&table.LastAutoanalyze,
			&table.VacuumCount,
			&table.AutovacuumCount,
			&table.AutovacuumEnabled,
		)
		if err != nil {
			return nil, fmt.Errorf("scan table row: %w", err)
		}
		// Calculate estimated bloat from dead rows ratio
		totalRows := table.RowCount + table.DeadRows
		if totalRows > 0 {
			table.BloatPct = float64(table.DeadRows) / float64(totalRows) * 100
		}
		table.BloatEstimated = true // All initial bloat values are estimates
		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}

	return tables, nil
}

// GetIndexesWithStats retrieves all indexes with usage statistics.
func GetIndexesWithStats(ctx context.Context, pool *pgxpool.Pool) ([]models.Index, error) {
	query := `
		SELECT
			idx.oid as index_oid,
			i.indrelid as table_oid,
			nsp.nspname as schema_name,
			t.relname as table_name,
			idx.relname as index_name,
			pg_relation_size(idx.oid) as index_size_bytes,
			COALESCE(s.idx_scan, 0) as scan_count,
			COALESCE(s.idx_tup_read, 0) as rows_read,
			COALESCE(s.idx_tup_fetch, 0) as rows_fetched,
			COALESCE(
				ROUND(100.0 * io.idx_blks_hit /
					NULLIF(io.idx_blks_hit + io.idx_blks_read, 0), 2),
				0) as cache_hit_ratio,
			i.indisprimary as is_primary,
			i.indisunique as is_unique
		FROM pg_index i
		JOIN pg_class idx ON i.indexrelid = idx.oid
		JOIN pg_class t ON i.indrelid = t.oid
		JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
		LEFT JOIN pg_stat_all_indexes s ON s.indexrelid = idx.oid
		LEFT JOIN pg_statio_all_indexes io ON io.indexrelid = idx.oid
		WHERE t.relkind IN ('r', 'p')
		ORDER BY nsp.nspname, t.relname, idx.relname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query indexes with stats: %w", err)
	}
	defer rows.Close()

	var indexes []models.Index
	for rows.Next() {
		var index models.Index
		err := rows.Scan(
			&index.OID,
			&index.TableOID,
			&index.SchemaName,
			&index.TableName,
			&index.Name,
			&index.Size,
			&index.ScanCount,
			&index.RowsRead,
			&index.RowsFetched,
			&index.CacheHitRatio,
			&index.IsPrimary,
			&index.IsUnique,
		)
		if err != nil {
			return nil, fmt.Errorf("scan index row: %w", err)
		}
		// Mark as unused if never scanned
		index.IsUnused = index.ScanCount == 0
		indexes = append(indexes, index)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexes: %w", err)
	}

	return indexes, nil
}

// CheckPgstattupleExtension checks if the pgstattuple extension is installed.
// Returns true if available, false otherwise.
func CheckPgstattupleExtension(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM pg_extension WHERE extname = 'pgstattuple'
		)
	`

	var exists bool
	err := pool.QueryRow(ctx, query).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pgstattuple extension: %w", err)
	}

	return exists, nil
}

// InstallPgstattupleExtension installs the pgstattuple extension.
// Requires sufficient privileges (typically superuser or extension creation rights).
func InstallPgstattupleExtension(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
	if err != nil {
		// Check for permission denied errors
		if strings.Contains(err.Error(), "permission denied") {
			return ErrInsufficientPrivileges
		}
		return fmt.Errorf("install pgstattuple extension: %w", err)
	}
	return nil
}

// GetTableBloat retrieves accurate bloat percentages using pgstattuple extension.
// Returns a map of table OID -> bloat percentage.
// Only call this when pgstattuple is available.
// Excludes system schemas (pg_catalog, information_schema, pg_toast*) since DBAs
// typically focus on user table bloat and running pgstattuple on system tables is slow.
func GetTableBloat(ctx context.Context, pool *pgxpool.Pool) (map[uint32]float64, error) {
	query := `
		SELECT
			c.oid,
			COALESCE((pgstattuple(c.oid)).dead_tuple_percent, 0) as bloat_pct
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_toast%'
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query table bloat: %w", err)
	}
	defer rows.Close()

	bloat := make(map[uint32]float64)
	for rows.Next() {
		var oid uint32
		var pct float64
		if err := rows.Scan(&oid, &pct); err != nil {
			return nil, fmt.Errorf("scan bloat row: %w", err)
		}
		bloat[oid] = pct
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bloat: %w", err)
	}

	return bloat, nil
}

// GetPartitionHierarchy retrieves parent-child partition relationships.
// Returns a map of parent OID -> slice of child OIDs.
func GetPartitionHierarchy(ctx context.Context, pool *pgxpool.Pool) (map[uint32][]uint32, error) {
	query := `
		SELECT
			parent.oid as parent_oid,
			child.oid as child_oid
		FROM pg_inherits i
		JOIN pg_class parent ON parent.oid = i.inhparent
		JOIN pg_class child ON child.oid = i.inhrelid
		WHERE parent.relkind = 'p'
		ORDER BY parent.oid, child.relname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query partition hierarchy: %w", err)
	}
	defer rows.Close()

	partitions := make(map[uint32][]uint32)
	for rows.Next() {
		var parentOID, childOID uint32
		err := rows.Scan(&parentOID, &childOID)
		if err != nil {
			return nil, fmt.Errorf("scan partition row: %w", err)
		}
		partitions[parentOID] = append(partitions[parentOID], childOID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partitions: %w", err)
	}

	return partitions, nil
}

// GetTableColumns retrieves column definitions for a table.
func GetTableColumns(ctx context.Context, pool *pgxpool.Pool, tableOID uint32) ([]models.TableColumn, error) {
	query := `
		SELECT
			a.attnum as position,
			a.attname as name,
			pg_catalog.format_type(a.atttypid, a.atttypmod) as data_type,
			a.attnotnull as not_null,
			pg_get_expr(d.adbin, d.adrelid) as default_value
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum
	`

	rows, err := pool.Query(ctx, query, tableOID)
	if err != nil {
		return nil, fmt.Errorf("query table columns: %w", err)
	}
	defer rows.Close()

	var columns []models.TableColumn
	for rows.Next() {
		var col models.TableColumn
		var notNull bool
		err := rows.Scan(&col.Position, &col.Name, &col.DataType, &notNull, &col.DefaultValue)
		if err != nil {
			return nil, fmt.Errorf("scan column row: %w", err)
		}
		col.IsNullable = !notNull
		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	return columns, nil
}

// GetTableConstraints retrieves constraints for a table.
func GetTableConstraints(ctx context.Context, pool *pgxpool.Pool, tableOID uint32) ([]models.Constraint, error) {
	query := `
		SELECT
			c.conname as name,
			c.contype::text as type,
			pg_get_constraintdef(c.oid, true) as definition
		FROM pg_constraint c
		WHERE c.conrelid = $1
		ORDER BY
			CASE c.contype
				WHEN 'p' THEN 1
				WHEN 'u' THEN 2
				WHEN 'f' THEN 3
				WHEN 'c' THEN 4
				WHEN 'n' THEN 5
				WHEN 'x' THEN 6
				ELSE 7
			END,
			c.conname
	`

	rows, err := pool.Query(ctx, query, tableOID)
	if err != nil {
		return nil, fmt.Errorf("query table constraints: %w", err)
	}
	defer rows.Close()

	var constraints []models.Constraint
	for rows.Next() {
		var con models.Constraint
		var conType string
		err := rows.Scan(&con.Name, &conType, &con.Definition)
		if err != nil {
			return nil, fmt.Errorf("scan constraint row: %w", err)
		}
		switch conType {
		case "p":
			con.Type = models.ConstraintPrimaryKey
		case "f":
			con.Type = models.ConstraintForeignKey
		case "u":
			con.Type = models.ConstraintUnique
		case "c":
			con.Type = models.ConstraintCheck
		case "n":
			con.Type = models.ConstraintNotNull
		case "x":
			con.Type = models.ConstraintExclusion
		default:
			con.Type = models.ConstraintType(conType) // Fallback to raw type
		}
		constraints = append(constraints, con)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate constraints: %w", err)
	}

	return constraints, nil
}

// ExecuteVacuum runs VACUUM on a table.
// Uses quote_ident for safe identifier quoting.
func ExecuteVacuum(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error {
	// Build the query with properly quoted identifiers
	// VACUUM cannot use prepared statements, so we use quote_ident for safety
	query := fmt.Sprintf("VACUUM %s.%s",
		quoteIdentifier(schemaName),
		quoteIdentifier(tableName))

	_, err := pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("vacuum %s.%s: %w", schemaName, tableName, err)
	}
	return nil
}

// ExecuteAnalyze runs ANALYZE on a table.
func ExecuteAnalyze(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error {
	query := fmt.Sprintf("ANALYZE %s.%s",
		quoteIdentifier(schemaName),
		quoteIdentifier(tableName))

	_, err := pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("analyze %s.%s: %w", schemaName, tableName, err)
	}
	return nil
}

// ExecuteReindex runs REINDEX on a table.
func ExecuteReindex(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) error {
	query := fmt.Sprintf("REINDEX TABLE %s.%s",
		quoteIdentifier(schemaName),
		quoteIdentifier(tableName))

	_, err := pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("reindex %s.%s: %w", schemaName, tableName, err)
	}
	return nil
}

// quoteIdentifier safely quotes a PostgreSQL identifier.
// Doubles any internal double quotes and wraps in double quotes.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
