package init

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// =============================================================================
// Overlap Analysis (T068)
// =============================================================================

// AnalyzeOverlap analyzes the overlap between local and remote tables.
// This uses the extension's compare_table_summary function for efficient
// hash-based comparison via postgres_fdw.
func (m *Merger) AnalyzeOverlap(ctx context.Context, schema, table string, pkColumns []string, remoteServer string) (*OverlapSummary, error) {
	query := `
		SELECT
			table_schema,
			table_name,
			total_rows,
			matches,
			conflicts,
			local_only,
			remote_only
		FROM steep_repl.compare_table_summary($1, $2, $3, $4, $5, $6)
	`

	var summary OverlapSummary
	err := m.localPool.QueryRow(ctx, query, schema, table, remoteServer, schema, table, pkColumns).Scan(
		&summary.TableSchema,
		&summary.TableName,
		&summary.TotalRows,
		&summary.Matches,
		&summary.Conflicts,
		&summary.LocalOnly,
		&summary.RemoteOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("analyze overlap for %s.%s: %w", schema, table, err)
	}

	return &summary, nil
}

// AnalyzeOverlapDetailed returns row-by-row overlap analysis results.
// Use this for detailed conflict inspection or small tables.
func (m *Merger) AnalyzeOverlapDetailed(ctx context.Context, schema, table string, pkColumns []string, remoteServer string) ([]OverlapResult, error) {
	query := `
		SELECT
			pk_value,
			category::text,
			local_hash,
			remote_hash
		FROM steep_repl.compare_table_rows($1, $2, $3, $4, $5, $6)
	`

	rows, err := m.localPool.Query(ctx, query, schema, table, remoteServer, schema, table, pkColumns)
	if err != nil {
		return nil, fmt.Errorf("analyze overlap detailed for %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	var results []OverlapResult
	for rows.Next() {
		var result OverlapResult
		var pkJSON []byte
		var category string

		err := rows.Scan(&pkJSON, &category, &result.LocalHash, &result.RemoteHash)
		if err != nil {
			return nil, fmt.Errorf("scan overlap result: %w", err)
		}

		if err := json.Unmarshal(pkJSON, &result.PKValue); err != nil {
			return nil, fmt.Errorf("unmarshal pk_value: %w", err)
		}
		result.Category = OverlapCategory(category)
		results = append(results, result)
	}

	return results, rows.Err()
}

// AnalyzeAllTables analyzes overlap for multiple tables.
func (m *Merger) AnalyzeAllTables(ctx context.Context, tables []MergeTableInfo, remoteServer string) ([]OverlapSummary, error) {
	var summaries []OverlapSummary
	for _, t := range tables {
		summary, err := m.AnalyzeOverlap(ctx, t.Schema, t.Name, t.PKColumns, remoteServer)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}
	return summaries, nil
}

// getRowsByCategory returns PKs of rows matching a specific overlap category.
func (m *Merger) getRowsByCategory(ctx context.Context, schema, table string, pkColumns []string, remoteServer string, category OverlapCategory) ([]map[string]any, error) {
	results, err := m.AnalyzeOverlapDetailed(ctx, schema, table, pkColumns, remoteServer)
	if err != nil {
		return nil, err
	}

	var pks []map[string]any
	for _, r := range results {
		if r.Category == category {
			pks = append(pks, r.PKValue)
		}
	}
	return pks, nil
}

// fetchRow fetches a single row from a pool by PK.
func (m *Merger) fetchRow(ctx context.Context, pool *pgxpool.Pool, schema, table string, pkColumns []string, pkValue map[string]any) (map[string]any, error) {
	// Build WHERE clause
	var whereParts []string
	var args []any
	for i, col := range pkColumns {
		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", col, i+1))
		args = append(args, pkValue[col])
	}

	query := fmt.Sprintf("SELECT to_jsonb(t.*) FROM %s.%s t WHERE %s",
		sanitizeIdentifier(schema),
		sanitizeIdentifier(table),
		joinStrings(whereParts, " AND "))

	var rowJSON []byte
	err := pool.QueryRow(ctx, query, args...).Scan(&rowJSON)
	if err != nil {
		return nil, err
	}

	var row map[string]interface{}
	if err := json.Unmarshal(rowJSON, &row); err != nil {
		return nil, err
	}

	return row, nil
}
