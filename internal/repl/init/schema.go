package init

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaComparator handles schema fingerprinting and comparison.
type SchemaComparator struct {
	pool *pgxpool.Pool
}

// NewSchemaComparator creates a new schema comparator.
func NewSchemaComparator(pool *pgxpool.Pool) *SchemaComparator {
	return &SchemaComparator{pool: pool}
}

// ComparisonStatus indicates the result of comparing two table schemas.
type ComparisonStatus string

const (
	ComparisonMatch      ComparisonStatus = "MATCH"
	ComparisonMismatch   ComparisonStatus = "MISMATCH"
	ComparisonLocalOnly  ComparisonStatus = "LOCAL_ONLY"
	ComparisonRemoteOnly ComparisonStatus = "REMOTE_ONLY"
)

// SchemaComparison represents the comparison result for a single table.
type SchemaComparison struct {
	TableSchema       string
	TableName         string
	LocalFingerprint  string
	RemoteFingerprint string
	Status            ComparisonStatus
	Differences       []ColumnDifference
}

// ColumnDifference describes a difference between two column definitions.
type ColumnDifference struct {
	ColumnName       string
	DifferenceType   string // missing, extra, type_change, default_change
	LocalDefinition  string
	RemoteDefinition string
}

// CompareResult holds the full comparison result.
type CompareResult struct {
	Comparisons     []SchemaComparison
	MatchCount      int
	MismatchCount   int
	LocalOnlyCount  int
	RemoteOnlyCount int
}

// CaptureFingerprints computes and stores fingerprints for all tables.
func (s *SchemaComparator) CaptureFingerprints(ctx context.Context, nodeID string, schemas []string) error {
	// Build schema filter
	schemaFilter := ""
	if len(schemas) > 0 {
		schemaFilter = "AND table_schema = ANY($2)"
	}

	query := fmt.Sprintf(`
		INSERT INTO steep_repl.schema_fingerprints (node_id, table_schema, table_name, fingerprint, column_count, captured_at)
		SELECT
			$1,
			table_schema,
			table_name,
			steep_repl.compute_fingerprint(table_schema, table_name),
			(SELECT count(*) FROM information_schema.columns c
			 WHERE c.table_schema = t.table_schema AND c.table_name = t.table_name),
			now()
		FROM information_schema.tables t
		WHERE table_type = 'BASE TABLE'
		AND table_schema NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		%s
		ON CONFLICT (node_id, table_schema, table_name) DO UPDATE SET
			fingerprint = EXCLUDED.fingerprint,
			column_count = EXCLUDED.column_count,
			captured_at = EXCLUDED.captured_at
	`, schemaFilter)

	var err error
	if len(schemas) > 0 {
		_, err = s.pool.Exec(ctx, query, nodeID, schemas)
	} else {
		_, err = s.pool.Exec(ctx, query, nodeID)
	}
	return err
}

// Compare compares schema fingerprints between two nodes.
func (s *SchemaComparator) Compare(ctx context.Context, localNodeID, remoteNodeID string, schemas []string) (*CompareResult, error) {
	// Capture current fingerprints for local node
	if err := s.CaptureFingerprints(ctx, localNodeID, schemas); err != nil {
		return nil, fmt.Errorf("failed to capture local fingerprints: %w", err)
	}

	// Query for comparison
	query := `
		SELECT
			COALESCE(l.table_schema, r.table_schema) as table_schema,
			COALESCE(l.table_name, r.table_name) as table_name,
			COALESCE(l.fingerprint, '') as local_fingerprint,
			COALESCE(r.fingerprint, '') as remote_fingerprint,
			CASE
				WHEN l.fingerprint IS NULL THEN 'REMOTE_ONLY'
				WHEN r.fingerprint IS NULL THEN 'LOCAL_ONLY'
				WHEN l.fingerprint = r.fingerprint THEN 'MATCH'
				ELSE 'MISMATCH'
			END as status
		FROM steep_repl.schema_fingerprints l
		FULL OUTER JOIN steep_repl.schema_fingerprints r
			ON l.table_schema = r.table_schema
			AND l.table_name = r.table_name
			AND r.node_id = $2
		WHERE l.node_id = $1 OR r.node_id = $2
		ORDER BY table_schema, table_name
	`

	rows, err := s.pool.Query(ctx, query, localNodeID, remoteNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to compare fingerprints: %w", err)
	}
	defer rows.Close()

	result := &CompareResult{}
	for rows.Next() {
		var comp SchemaComparison
		var status string
		if err := rows.Scan(
			&comp.TableSchema,
			&comp.TableName,
			&comp.LocalFingerprint,
			&comp.RemoteFingerprint,
			&status,
		); err != nil {
			return nil, fmt.Errorf("failed to scan comparison: %w", err)
		}
		comp.Status = ComparisonStatus(status)

		// Count by status
		switch comp.Status {
		case ComparisonMatch:
			result.MatchCount++
		case ComparisonMismatch:
			result.MismatchCount++
		case ComparisonLocalOnly:
			result.LocalOnlyCount++
		case ComparisonRemoteOnly:
			result.RemoteOnlyCount++
		}

		result.Comparisons = append(result.Comparisons, comp)
	}

	return result, nil
}

// GetDiff returns detailed column differences for a mismatched table.
// Implemented in T059 (Phase 7: User Story 5).
func (s *SchemaComparator) GetDiff(ctx context.Context, localNodeID, remoteNodeID, tableSchema, tableName string) ([]ColumnDifference, error) {
	return nil, fmt.Errorf("not implemented")
}
