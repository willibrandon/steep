package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLEditorResult contains query execution results.
type SQLEditorResult struct {
	Columns      []SQLEditorColumn
	Rows         [][]any
	RowsAffected int64
	Duration     time.Duration
}

// SQLEditorColumn represents a result column.
type SQLEditorColumn struct {
	Name     string
	TypeOID  uint32
	TypeName string
}

// ExecuteSQL executes a SQL query with timeout and returns results.
// This is a lower-level function used by SessionExecutor for actual query execution.
func ExecuteSQL(ctx context.Context, pool *pgxpool.Pool, sql string) (*SQLEditorResult, error) {
	start := time.Now()

	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	result, err := collectResults(rows)
	if err != nil {
		return nil, err
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ExecuteSQLOnTx executes a SQL query within a transaction.
func ExecuteSQLOnTx(ctx context.Context, tx pgx.Tx, sql string) (*SQLEditorResult, error) {
	start := time.Now()

	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	result, err := collectResults(rows)
	if err != nil {
		return nil, err
	}

	result.Duration = time.Since(start)
	return result, nil
}

// collectResults gathers column metadata and row data from pgx.Rows.
func collectResults(rows pgx.Rows) (*SQLEditorResult, error) {
	// Get column metadata
	fieldDescs := rows.FieldDescriptions()
	columns := make([]SQLEditorColumn, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = SQLEditorColumn{
			Name:     string(fd.Name),
			TypeOID:  fd.DataTypeOID,
			TypeName: getTypeName(fd.DataTypeOID),
		}
	}

	// Collect all rows
	var resultRows [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to read row values: %w", err)
		}
		// Make a copy of values since they may be reused
		rowCopy := make([]any, len(values))
		copy(rowCopy, values)
		resultRows = append(resultRows, rowCopy)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("row iteration error: %w", rows.Err())
	}

	// Get rows affected from command tag
	cmdTag := rows.CommandTag()
	rowsAffected := cmdTag.RowsAffected()

	return &SQLEditorResult{
		Columns:      columns,
		Rows:         resultRows,
		RowsAffected: rowsAffected,
	}, nil
}

// GetResultCount executes a count query for the given SQL.
// Used for pagination - wraps the user query to get total count.
func GetResultCount(ctx context.Context, pool *pgxpool.Pool, sql string) (int64, error) {
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS count_subquery", sql)

	var count int64
	err := pool.QueryRow(ctx, countQuery).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count query failed: %w", err)
	}

	return count, nil
}

// ExecuteSQLPaginated executes a SQL query with LIMIT/OFFSET for pagination.
func ExecuteSQLPaginated(ctx context.Context, pool *pgxpool.Pool, sql string, limit, offset int) (*SQLEditorResult, error) {
	pagedSQL := fmt.Sprintf("%s LIMIT %d OFFSET %d", sql, limit, offset)
	return ExecuteSQL(ctx, pool, pagedSQL)
}

// getTypeName returns a human-readable type name for a PostgreSQL type OID.
func getTypeName(oid uint32) string {
	// Common PostgreSQL type OIDs
	typeNames := map[uint32]string{
		16:   "bool",
		17:   "bytea",
		18:   "char",
		19:   "name",
		20:   "int8",
		21:   "int2",
		23:   "int4",
		24:   "regproc",
		25:   "text",
		26:   "oid",
		27:   "tid",
		28:   "xid",
		29:   "cid",
		114:  "json",
		142:  "xml",
		600:  "point",
		601:  "lseg",
		602:  "path",
		603:  "box",
		604:  "polygon",
		628:  "line",
		650:  "cidr",
		700:  "float4",
		701:  "float8",
		718:  "circle",
		790:  "money",
		829:  "macaddr",
		869:  "inet",
		1042: "bpchar",
		1043: "varchar",
		1082: "date",
		1083: "time",
		1114: "timestamp",
		1184: "timestamptz",
		1186: "interval",
		1266: "timetz",
		1560: "bit",
		1562: "varbit",
		1700: "numeric",
		2950: "uuid",
		3802: "jsonb",
		3904: "int4range",
		3906: "numrange",
		3908: "tsrange",
		3910: "tstzrange",
		3912: "daterange",
		3926: "int8range",
	}

	if name, ok := typeNames[oid]; ok {
		return name
	}
	return fmt.Sprintf("oid:%d", oid)
}

// Note: CancelQuery and TerminateBackend functions are in activity.go and locks.go respectively.
// Use queries.CancelQuery from activity.go for canceling queries.
// Use queries.TerminateBackend from locks.go for terminating backends.
