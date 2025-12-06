package init

import (
	"context"
	"encoding/json"
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

// SchemaSyncHandler handles schema synchronization based on the configured mode.
type SchemaSyncHandler struct {
	pool       *pgxpool.Pool
	comparator *SchemaComparator
}

// NewSchemaSyncHandler creates a new schema sync handler.
func NewSchemaSyncHandler(pool *pgxpool.Pool) *SchemaSyncHandler {
	return &SchemaSyncHandler{
		pool:       pool,
		comparator: NewSchemaComparator(pool),
	}
}

// SchemaSyncResult contains the result of a schema sync operation.
type SchemaSyncResult struct {
	Mode           string // strict, auto, manual
	Action         string // failed, applied, warned
	Differences    []SchemaDifferenceDetail
	DDLStatements  []string // Only for auto mode
	AppliedCount   int      // Only for auto mode
	SkippedCount   int      // Only for auto mode
	WarningMessage string   // Only for manual mode
}

// SchemaDifferenceDetail provides detailed information about a schema difference.
type SchemaDifferenceDetail struct {
	TableSchema string
	TableName   string
	Type        string // mismatch, local_only, remote_only
	Description string
	DDL         string // Generated DDL to fix (only for auto mode)
}

// HandleStrict implements strict mode behavior: fail with error listing differences.
// Returns an error if any schema differences are found.
func (h *SchemaSyncHandler) HandleStrict(ctx context.Context, result *CompareResult) (*SchemaSyncResult, error) {
	if !result.HasMismatch() {
		return &SchemaSyncResult{
			Mode:   "strict",
			Action: "passed",
		}, nil
	}

	// Build detailed error message listing all differences
	details := h.buildDifferenceDetails(result)

	syncResult := &SchemaSyncResult{
		Mode:        "strict",
		Action:      "failed",
		Differences: details,
	}

	// Format error message with all differences
	var errMsg strings.Builder
	errMsg.WriteString(fmt.Sprintf("schema mismatch detected (%d differences):\n", len(details)))

	for i, diff := range details {
		if i >= 10 {
			errMsg.WriteString(fmt.Sprintf("  ... and %d more differences\n", len(details)-10))
			break
		}
		errMsg.WriteString(fmt.Sprintf("  - %s.%s: %s\n", diff.TableSchema, diff.TableName, diff.Description))
	}

	errMsg.WriteString("\nOptions:\n")
	errMsg.WriteString("  --schema-sync=auto   : Automatically apply DDL to fix mismatches\n")
	errMsg.WriteString("  --schema-sync=manual : Proceed with warning (data may be inconsistent)")

	return syncResult, fmt.Errorf("%s", errMsg.String())
}

// HandleAuto implements auto mode behavior: generate and apply DDL to fix mismatches.
// Returns the result of applying DDL changes.
func (h *SchemaSyncHandler) HandleAuto(ctx context.Context, result *CompareResult, remoteFingerprints map[string]TableFingerprintInfo) (*SchemaSyncResult, error) {
	if !result.HasMismatch() {
		return &SchemaSyncResult{
			Mode:   "auto",
			Action: "no_changes",
		}, nil
	}

	// Build difference details with DDL generation
	details := h.buildDifferenceDetailsWithDDL(ctx, result, remoteFingerprints)

	syncResult := &SchemaSyncResult{
		Mode:          "auto",
		Action:        "applied",
		Differences:   details,
		DDLStatements: make([]string, 0),
	}

	// Collect all DDL statements
	for _, diff := range details {
		if diff.DDL != "" {
			syncResult.DDLStatements = append(syncResult.DDLStatements, diff.DDL)
		}
	}

	// Apply DDL statements in a transaction
	if len(syncResult.DDLStatements) > 0 {
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			return syncResult, fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		for _, ddl := range syncResult.DDLStatements {
			_, err := tx.Exec(ctx, ddl)
			if err != nil {
				syncResult.SkippedCount++
				// Log the error but continue with other statements
				continue
			}
			syncResult.AppliedCount++
		}

		if err := tx.Commit(ctx); err != nil {
			return syncResult, fmt.Errorf("failed to commit DDL changes: %w", err)
		}
	}

	return syncResult, nil
}

// HandleManual implements manual mode behavior: warn but proceed with confirmation.
// Returns a warning message about the differences.
func (h *SchemaSyncHandler) HandleManual(ctx context.Context, result *CompareResult) (*SchemaSyncResult, error) {
	details := h.buildDifferenceDetails(result)

	var warningMsg strings.Builder
	if result.HasMismatch() {
		warningMsg.WriteString(fmt.Sprintf("WARNING: Schema mismatch detected (%d differences)\n", len(details)))
		warningMsg.WriteString("Proceeding with initialization may cause data inconsistency.\n\n")

		for i, diff := range details {
			if i >= 5 {
				warningMsg.WriteString(fmt.Sprintf("  ... and %d more differences\n", len(details)-5))
				break
			}
			warningMsg.WriteString(fmt.Sprintf("  - %s.%s: %s\n", diff.TableSchema, diff.TableName, diff.Description))
		}
	}

	return &SchemaSyncResult{
		Mode:           "manual",
		Action:         "warned",
		Differences:    details,
		WarningMessage: warningMsg.String(),
	}, nil
}

// buildDifferenceDetails builds detailed difference information from comparison result.
func (h *SchemaSyncHandler) buildDifferenceDetails(result *CompareResult) []SchemaDifferenceDetail {
	var details []SchemaDifferenceDetail

	for _, comp := range result.Comparisons {
		var detail SchemaDifferenceDetail
		detail.TableSchema = comp.TableSchema
		detail.TableName = comp.TableName

		switch comp.Status {
		case ComparisonMismatch:
			detail.Type = "mismatch"
			detail.Description = fmt.Sprintf("column definition mismatch (local: %s..., remote: %s...)",
				truncateFingerprint(comp.LocalFingerprint, 8),
				truncateFingerprint(comp.RemoteFingerprint, 8))
		case ComparisonLocalOnly:
			detail.Type = "local_only"
			detail.Description = "table exists locally but not on remote"
		case ComparisonRemoteOnly:
			detail.Type = "remote_only"
			detail.Description = "table exists on remote but not locally"
		default:
			continue // Skip matches
		}

		details = append(details, detail)
	}

	return details
}

// buildDifferenceDetailsWithDDL builds difference details and generates DDL to fix them.
func (h *SchemaSyncHandler) buildDifferenceDetailsWithDDL(ctx context.Context, result *CompareResult, remoteFingerprints map[string]TableFingerprintInfo) []SchemaDifferenceDetail {
	var details []SchemaDifferenceDetail

	for _, comp := range result.Comparisons {
		var detail SchemaDifferenceDetail
		detail.TableSchema = comp.TableSchema
		detail.TableName = comp.TableName
		fullName := comp.TableSchema + "." + comp.TableName

		switch comp.Status {
		case ComparisonMismatch:
			detail.Type = "mismatch"
			detail.Description = "column definition mismatch"
			// For mismatches, we need to generate ALTER TABLE statements
			// This requires detailed column comparison which we'll get from remote info
			if remoteInfo, ok := remoteFingerprints[fullName]; ok {
				detail.DDL = h.generateAlterTableDDL(ctx, comp.TableSchema, comp.TableName, remoteInfo.ColumnDefinitions)
			}
		case ComparisonLocalOnly:
			detail.Type = "local_only"
			detail.Description = "table exists locally but not on remote - will not be replicated"
			// No DDL needed - local table is not on remote
		case ComparisonRemoteOnly:
			detail.Type = "remote_only"
			detail.Description = "table exists on remote but not locally - creating table"
			// Generate CREATE TABLE from remote schema
			if remoteInfo, ok := remoteFingerprints[fullName]; ok {
				detail.DDL = h.generateCreateTableDDL(comp.TableSchema, comp.TableName, remoteInfo.ColumnDefinitions)
			}
		default:
			continue // Skip matches
		}

		details = append(details, detail)
	}

	return details
}

// generateCreateTableDDL generates a CREATE TABLE statement from column definitions JSON.
func (h *SchemaSyncHandler) generateCreateTableDDL(schema, table, columnDefsJSON string) string {
	if columnDefsJSON == "" {
		return ""
	}

	// Parse column definitions
	type columnDef struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Default  string `json:"default"`
		Nullable string `json:"nullable"`
		Position int    `json:"position"`
	}

	var cols []columnDef
	if err := json.Unmarshal([]byte(columnDefsJSON), &cols); err != nil {
		return ""
	}

	if len(cols) == 0 {
		return ""
	}

	var ddl strings.Builder
	ddl.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n", quoteIdentifier(schema), quoteIdentifier(table)))

	for i, col := range cols {
		ddl.WriteString(fmt.Sprintf("  %s %s", quoteIdentifier(col.Name), col.Type))
		if col.Default != "" {
			ddl.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
		}
		if col.Nullable == "NO" {
			ddl.WriteString(" NOT NULL")
		}
		if i < len(cols)-1 {
			ddl.WriteString(",")
		}
		ddl.WriteString("\n")
	}
	ddl.WriteString(")")

	return ddl.String()
}

// generateAlterTableDDL generates ALTER TABLE statements to sync column definitions.
// This is a complex operation - for now, we generate a comment indicating manual review needed.
func (h *SchemaSyncHandler) generateAlterTableDDL(ctx context.Context, schema, table, remoteColumnDefsJSON string) string {
	if remoteColumnDefsJSON == "" {
		return ""
	}

	// Parse remote column definitions
	type columnDef struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Default  string `json:"default"`
		Nullable string `json:"nullable"`
		Position int    `json:"position"`
	}

	var remoteCols []columnDef
	if err := json.Unmarshal([]byte(remoteColumnDefsJSON), &remoteCols); err != nil {
		return ""
	}

	// Get local columns for comparison
	query := `
		SELECT column_name, data_type, COALESCE(column_default, ''), is_nullable
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`
	rows, err := h.pool.Query(ctx, query, schema, table)
	if err != nil {
		return ""
	}
	defer rows.Close()

	localCols := make(map[string]columnDef)
	for rows.Next() {
		var col columnDef
		if err := rows.Scan(&col.Name, &col.Type, &col.Default, &col.Nullable); err != nil {
			continue
		}
		localCols[col.Name] = col
	}

	remoteCols_map := make(map[string]columnDef)
	for _, col := range remoteCols {
		remoteCols_map[col.Name] = col
	}

	var ddlStatements []string

	// Find columns to add (in remote but not local)
	for _, remoteCol := range remoteCols {
		if _, exists := localCols[remoteCol.Name]; !exists {
			ddl := fmt.Sprintf("ALTER TABLE %s.%s ADD COLUMN %s %s",
				quoteIdentifier(schema), quoteIdentifier(table),
				quoteIdentifier(remoteCol.Name), remoteCol.Type)
			if remoteCol.Default != "" {
				ddl += fmt.Sprintf(" DEFAULT %s", remoteCol.Default)
			}
			if remoteCol.Nullable == "NO" {
				ddl += " NOT NULL"
			}
			ddlStatements = append(ddlStatements, ddl)
		}
	}

	// Note: We don't automatically drop columns (in local but not remote) for safety
	// We also don't automatically alter column types as that can be destructive

	if len(ddlStatements) == 0 {
		return "-- Manual review required: column type or constraint differences detected"
	}

	return strings.Join(ddlStatements, ";\n")
}

// truncateFingerprint truncates a fingerprint for display.
func truncateFingerprint(fp string, maxLen int) string {
	if len(fp) <= maxLen {
		return fp
	}
	return fp[:maxLen]
}

// quoteIdentifier quotes a SQL identifier.
func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
