package init

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// =============================================================================
// Data Transfer (T073c)
// =============================================================================

// copyBatchThreshold defines the minimum number of rows to use COPY protocol.
// Below this threshold, individual INSERTs are used for simplicity.
const copyBatchThreshold = 100

// TransferRows transfers rows from source to target.
// For batches >= copyBatchThreshold, uses PostgreSQL COPY protocol for efficiency.
// For smaller batches, uses individual INSERTs.
func (m *Merger) TransferRows(ctx context.Context, sourcePool, targetPool *pgxpool.Pool, schema, table string, pkColumns []string, pkValues []map[string]any) (int64, error) {
	if len(pkValues) == 0 {
		return 0, nil
	}

	// Use COPY protocol for large batches
	if len(pkValues) >= copyBatchThreshold {
		return m.transferRowsCopy(ctx, sourcePool, targetPool, schema, table, pkColumns, pkValues)
	}

	// Use individual INSERTs for small batches
	return m.transferRowsInsert(ctx, sourcePool, targetPool, schema, table, pkColumns, pkValues)
}

// transferRowsInsert transfers rows using individual INSERT statements.
func (m *Merger) transferRowsInsert(ctx context.Context, sourcePool, targetPool *pgxpool.Pool, schema, table string, pkColumns []string, pkValues []map[string]any) (int64, error) {
	var transferred int64
	for _, pk := range pkValues {
		row, err := m.fetchRow(ctx, sourcePool, schema, table, pkColumns, pk)
		if err != nil {
			return transferred, fmt.Errorf("fetch row from source: %w", err)
		}

		if err := m.insertRow(ctx, targetPool, schema, table, row); err != nil {
			return transferred, fmt.Errorf("insert row into target: %w", err)
		}
		transferred++
	}
	return transferred, nil
}

// transferRowsCopy transfers rows using PostgreSQL COPY protocol.
func (m *Merger) transferRowsCopy(ctx context.Context, sourcePool, targetPool *pgxpool.Pool, schema, table string, pkColumns []string, pkValues []map[string]any) (int64, error) {
	// Get column metadata from target table
	columns, err := m.getTableColumns(ctx, targetPool, schema, table)
	if err != nil {
		return 0, fmt.Errorf("get table columns: %w", err)
	}

	// Fetch all rows from source in bulk
	rows, err := m.fetchRowsBulk(ctx, sourcePool, schema, table, columns, pkColumns, pkValues)
	if err != nil {
		return 0, fmt.Errorf("fetch rows from source: %w", err)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	// Build column identifiers for COPY
	columnIdents := make([]string, len(columns))
	for i, col := range columns {
		columnIdents[i] = col.Name
	}

	// Use CopyFrom for efficient bulk insert
	copyCount, err := targetPool.CopyFrom(
		ctx,
		pgx.Identifier{schema, table},
		columnIdents,
		&rowsCopySource{rows: rows, columns: columns},
	)
	if err != nil {
		return 0, fmt.Errorf("copy to target: %w", err)
	}

	return copyCount, nil
}

// getTableColumns retrieves column metadata for a table in column order.
func (m *Merger) getTableColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]columnInfo, error) {
	query := `
		SELECT column_name, data_type, ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	rows, err := pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []columnInfo
	for rows.Next() {
		var col columnInfo
		if err := rows.Scan(&col.Name, &col.DataType, &col.Position); err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

// fetchRowsBulk fetches multiple rows from source using a single query.
func (m *Merger) fetchRowsBulk(ctx context.Context, pool *pgxpool.Pool, schema, table string, columns []columnInfo, pkColumns []string, pkValues []map[string]any) ([][]any, error) {
	if len(pkValues) == 0 {
		return nil, nil
	}

	// Build column list
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = pgx.Identifier{col.Name}.Sanitize()
	}
	colList := joinStrings(colNames, ", ")

	// Build WHERE clause with ANY for each PK column
	// For composite PKs, we need ROW(...) IN (VALUES ...)
	var query string
	var args []any

	if len(pkColumns) == 1 {
		// Simple case: single column PK
		pkCol := pgx.Identifier{pkColumns[0]}.Sanitize()
		values := make([]any, len(pkValues))
		for i, pk := range pkValues {
			values[i] = pk[pkColumns[0]]
		}
		query = fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = ANY($1)",
			colList,
			pgx.Identifier{schema}.Sanitize(),
			pgx.Identifier{table}.Sanitize(),
			pkCol)
		args = []any{values}
	} else {
		// Composite PK: use ROW() IN (VALUES ...)
		var valueClauses []string
		argIdx := 1
		for _, pk := range pkValues {
			var placeholders []string
			for _, col := range pkColumns {
				placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
				args = append(args, pk[col])
				argIdx++
			}
			valueClauses = append(valueClauses, fmt.Sprintf("(%s)", joinStrings(placeholders, ", ")))
		}

		pkColList := make([]string, len(pkColumns))
		for i, col := range pkColumns {
			pkColList[i] = pgx.Identifier{col}.Sanitize()
		}

		query = fmt.Sprintf("SELECT %s FROM %s.%s WHERE (%s) IN (VALUES %s)",
			colList,
			pgx.Identifier{schema}.Sanitize(),
			pgx.Identifier{table}.Sanitize(),
			joinStrings(pkColList, ", "),
			joinStrings(valueClauses, ", "))
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		result = append(result, values)
	}
	return result, rows.Err()
}

// rowsCopySource implements pgx.CopyFromSource for bulk COPY operations.
type rowsCopySource struct {
	rows    [][]any
	columns []columnInfo
	idx     int
}

func (r *rowsCopySource) Next() bool {
	r.idx++
	return r.idx <= len(r.rows)
}

func (r *rowsCopySource) Values() ([]any, error) {
	if r.idx < 1 || r.idx > len(r.rows) {
		return nil, fmt.Errorf("invalid row index %d", r.idx)
	}
	return r.rows[r.idx-1], nil
}

func (r *rowsCopySource) Err() error {
	return nil
}

// insertRow inserts a row into a table.
func (m *Merger) insertRow(ctx context.Context, pool *pgxpool.Pool, schema, table string, row map[string]any) error {
	var cols []string
	var placeholders []string
	var args []any

	i := 1
	for col, val := range row {
		cols = append(cols, pgx.Identifier{col}.Sanitize())
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		args = append(args, val)
		i++
	}

	query := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		pgx.Identifier{schema}.Sanitize(),
		pgx.Identifier{table}.Sanitize(),
		joinStrings(cols, ", "),
		joinStrings(placeholders, ", "))

	_, err := pool.Exec(ctx, query, args...)
	return err
}
