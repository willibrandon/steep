package init

import (
	"context"
	"fmt"
	"strings"

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
// Uses the steep_repl.get_column_diff() SQL function to get differences via dblink.
func (s *SchemaComparator) GetDiff(ctx context.Context, peerNodeID, tableSchema, tableName string) ([]ColumnDifference, error) {
	query := `
		SELECT column_name, difference_type, local_definition, remote_definition
		FROM steep_repl.get_column_diff($1, $2, $3)
		WHERE difference_type <> 'match'
	`

	rows, err := s.pool.Query(ctx, query, peerNodeID, tableSchema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get column diff: %w", err)
	}
	defer rows.Close()

	var diffs []ColumnDifference
	for rows.Next() {
		var diff ColumnDifference
		if err := rows.Scan(
			&diff.ColumnName,
			&diff.DifferenceType,
			&diff.LocalDefinition,
			&diff.RemoteDefinition,
		); err != nil {
			return nil, fmt.Errorf("failed to scan column diff: %w", err)
		}
		diffs = append(diffs, diff)
	}

	return diffs, nil
}

// TableFingerprint represents a single table's fingerprint.
type TableFingerprint struct {
	SchemaName  string
	TableName   string
	Fingerprint string
}

// GetLocalFingerprints retrieves fingerprints for all local tables.
// This is used when a remote node requests fingerprints via gRPC.
func (s *SchemaComparator) GetLocalFingerprints(ctx context.Context, nodeID string) (map[string]string, error) {
	// Capture all fingerprints first (now requires node_id)
	_, err := s.pool.Exec(ctx, "SELECT steep_repl.capture_all_fingerprints($1)", nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to capture fingerprints: %w", err)
	}

	// Query the stored fingerprints for this node
	query := `
		SELECT table_schema, table_name, fingerprint
		FROM steep_repl.schema_fingerprints
		WHERE node_id = $1
		ORDER BY table_schema, table_name
	`

	rows, err := s.pool.Query(ctx, query, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query fingerprints: %w", err)
	}
	defer rows.Close()

	fingerprints := make(map[string]string)
	for rows.Next() {
		var schema, table, fp string
		if err := rows.Scan(&schema, &table, &fp); err != nil {
			return nil, fmt.Errorf("failed to scan fingerprint: %w", err)
		}
		key := schema + "." + table
		fingerprints[key] = fp
	}

	return fingerprints, nil
}

// RemoteFingerprints represents fingerprints retrieved from a remote node.
type RemoteFingerprints struct {
	NodeID       string
	Fingerprints map[string]string // key: "schema.table", value: fingerprint
}

// CompareWithRemote compares local fingerprints against remote fingerprints.
// The remote fingerprints should be obtained via gRPC GetSchemaFingerprints.
func (s *SchemaComparator) CompareWithRemote(ctx context.Context, localNodeID string, remoteFingerprints map[string]string) (*CompareResult, error) {
	// Get local fingerprints
	localFingerprints, err := s.GetLocalFingerprints(ctx, localNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get local fingerprints: %w", err)
	}

	result := &CompareResult{}

	// Check all local tables
	for key, localFP := range localFingerprints {
		parts := strings.SplitN(key, ".", 2)
		schema := "public"
		table := key
		if len(parts) == 2 {
			schema = parts[0]
			table = parts[1]
		}

		comp := SchemaComparison{
			TableSchema:      schema,
			TableName:        table,
			LocalFingerprint: localFP,
		}

		if remoteFP, exists := remoteFingerprints[key]; exists {
			comp.RemoteFingerprint = remoteFP
			if localFP == remoteFP {
				comp.Status = ComparisonMatch
				result.MatchCount++
			} else {
				comp.Status = ComparisonMismatch
				result.MismatchCount++
			}
		} else {
			comp.Status = ComparisonLocalOnly
			result.LocalOnlyCount++
		}

		result.Comparisons = append(result.Comparisons, comp)
	}

	// Check for remote-only tables
	for key, remoteFP := range remoteFingerprints {
		if _, exists := localFingerprints[key]; !exists {
			parts := strings.SplitN(key, ".", 2)
			schema := "public"
			table := key
			if len(parts) == 2 {
				schema = parts[0]
				table = parts[1]
			}

			comp := SchemaComparison{
				TableSchema:       schema,
				TableName:         table,
				RemoteFingerprint: remoteFP,
				Status:            ComparisonRemoteOnly,
			}
			result.Comparisons = append(result.Comparisons, comp)
			result.RemoteOnlyCount++
		}
	}

	return result, nil
}

// ComputeFingerprint computes the fingerprint for a single table.
func (s *SchemaComparator) ComputeFingerprint(ctx context.Context, tableSchema, tableName string) (string, error) {
	var fingerprint string
	err := s.pool.QueryRow(ctx,
		"SELECT steep_repl.compute_fingerprint($1, $2)",
		tableSchema, tableName,
	).Scan(&fingerprint)
	if err != nil {
		return "", fmt.Errorf("failed to compute fingerprint: %w", err)
	}
	return fingerprint, nil
}

// CaptureFingerprint captures and stores the fingerprint for a single table.
func (s *SchemaComparator) CaptureFingerprint(ctx context.Context, tableSchema, tableName string) error {
	_, err := s.pool.Exec(ctx,
		"SELECT steep_repl.capture_fingerprint($1, $2)",
		tableSchema, tableName,
	)
	return err
}

// HasMismatch returns true if the comparison result contains any mismatches.
func (r *CompareResult) HasMismatch() bool {
	return r.MismatchCount > 0 || r.LocalOnlyCount > 0 || r.RemoteOnlyCount > 0
}

// Summary returns a human-readable summary of the comparison result.
func (r *CompareResult) Summary() string {
	return fmt.Sprintf(
		"match=%d, mismatch=%d, local_only=%d, remote_only=%d",
		r.MatchCount, r.MismatchCount, r.LocalOnlyCount, r.RemoteOnlyCount,
	)
}
