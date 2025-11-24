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
			t.relkind = 'p' as is_partitioned
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
