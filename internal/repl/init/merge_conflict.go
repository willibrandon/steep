package init

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// =============================================================================
// Conflict Resolution (T070)
// =============================================================================

// ResolveConflicts resolves conflicts for a table using the specified strategy.
func (m *Merger) ResolveConflicts(ctx context.Context, mergeID uuid.UUID, schema, table string, pkColumns []string, remoteServer string, strategy ConflictStrategy) (int64, error) {
	// Get all conflicts
	conflicts, err := m.getConflicts(ctx, schema, table, pkColumns, remoteServer)
	if err != nil {
		return 0, err
	}

	if len(conflicts) == 0 {
		return 0, nil
	}

	var resolved int64
	resolvedBy := fmt.Sprintf("strategy:%s", strategy)

	for _, conflict := range conflicts {
		var resolution string
		var keepValue map[string]interface{}

		switch strategy {
		case StrategyPreferNodeA:
			resolution = "kept_a"
			keepValue = conflict.NodeAValue
		case StrategyPreferNodeB:
			resolution = "kept_b"
			keepValue = conflict.NodeBValue
		case StrategyLastModified:
			// Compare timestamps if available
			resolution, keepValue = m.ResolveByLastModified(conflict)
		case StrategyManual:
			// For manual, just log the conflict without resolving
			resolution = "skipped"
			keepValue = nil
		default:
			return resolved, fmt.Errorf("unknown strategy: %s", strategy)
		}

		// Apply resolution (update the losing node)
		if keepValue != nil && strategy != StrategyManual {
			if err := m.applyResolution(ctx, schema, table, pkColumns, conflict.PKValue, keepValue, resolution); err != nil {
				return resolved, fmt.Errorf("apply resolution for %v: %w", conflict.PKValue, err)
			}
		}

		// Log the decision to audit log
		if err := m.logMergeDecision(ctx, mergeID, schema, table, conflict.PKValue, CategoryConflict, &resolution, conflict.NodeAValue, conflict.NodeBValue, &resolvedBy); err != nil {
			return resolved, fmt.Errorf("log merge decision: %w", err)
		}

		resolved++
	}

	return resolved, nil
}

// getConflicts retrieves conflict details for a table.
func (m *Merger) getConflicts(ctx context.Context, schema, table string, pkColumns []string, remoteServer string) ([]ConflictDetail, error) {
	// Get rows that are conflicts
	results, err := m.AnalyzeOverlapDetailed(ctx, schema, table, pkColumns, remoteServer)
	if err != nil {
		return nil, err
	}

	var conflicts []ConflictDetail
	for _, r := range results {
		if r.Category == CategoryConflict {
			// Fetch full row data for conflict
			localRow, err := m.fetchRow(ctx, m.localPool, schema, table, pkColumns, r.PKValue)
			if err != nil {
				return nil, fmt.Errorf("fetch local row: %w", err)
			}

			remoteRow, err := m.fetchRow(ctx, m.remotePool, schema, table, pkColumns, r.PKValue)
			if err != nil {
				return nil, fmt.Errorf("fetch remote row: %w", err)
			}

			conflicts = append(conflicts, ConflictDetail{
				PKValue:             r.PKValue,
				NodeAValue:          localRow,
				NodeBValue:          remoteRow,
				SuggestedResolution: "prefer-node-a", // Default suggestion
			})
		}
	}

	return conflicts, nil
}

// ResolveByLastModified resolves a conflict by comparing timestamps.
func (m *Merger) ResolveByLastModified(conflict ConflictDetail) (string, map[string]interface{}) {
	// Look for updated_at or modified_at columns
	timestampCols := []string{"updated_at", "modified_at", "last_modified", "timestamp"}

	var aTime, bTime time.Time
	var aFound, bFound bool

	for _, col := range timestampCols {
		if v, ok := conflict.NodeAValue[col]; ok {
			if t, err := ParseTimestamp(v); err == nil {
				aTime = t
				aFound = true
				break
			}
		}
	}

	for _, col := range timestampCols {
		if v, ok := conflict.NodeBValue[col]; ok {
			if t, err := ParseTimestamp(v); err == nil {
				bTime = t
				bFound = true
				break
			}
		}
	}

	// If both have timestamps, compare
	if aFound && bFound {
		if bTime.After(aTime) {
			return "kept_b", conflict.NodeBValue
		}
		return "kept_a", conflict.NodeAValue
	}

	// If only one has timestamp, prefer that one
	if aFound && !bFound {
		return "kept_a", conflict.NodeAValue
	}
	if bFound && !aFound {
		return "kept_b", conflict.NodeBValue
	}

	// Fallback: prefer A (deterministic tiebreaker)
	return "kept_a", conflict.NodeAValue
}

// ParseTimestamp attempts to parse a timestamp from various formats.
func ParseTimestamp(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		// Try common formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05.999999",
		}
		for _, f := range formats {
			if parsed, err := time.Parse(f, t); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("could not parse timestamp: %s", t)
	default:
		return time.Time{}, fmt.Errorf("unexpected timestamp type: %T", v)
	}
}

// applyResolution applies a conflict resolution by updating the losing node.
func (m *Merger) applyResolution(ctx context.Context, schema, table string, pkColumns []string, pkValue, keepValue map[string]any, resolution string) error {
	// Determine which pool to update
	var targetPool = m.remotePool
	if resolution == "kept_b" {
		// B won, update A to match B
		targetPool = m.localPool
	}

	// Build UPDATE statement
	var setParts []string
	var args []any
	argIdx := 1

	for col, val := range keepValue {
		// Skip PK columns in SET clause
		isPK := slices.Contains(pkColumns, col)
		if !isPK {
			setParts = append(setParts, fmt.Sprintf("%s = $%d", pgx.Identifier{col}.Sanitize(), argIdx))
			args = append(args, val)
			argIdx++
		}
	}

	// Build WHERE clause
	var whereParts []string
	for _, col := range pkColumns {
		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", pgx.Identifier{col}.Sanitize(), argIdx))
		args = append(args, pkValue[col])
		argIdx++
	}

	query := fmt.Sprintf("UPDATE %s.%s SET %s WHERE %s",
		pgx.Identifier{schema}.Sanitize(),
		pgx.Identifier{table}.Sanitize(),
		joinStrings(setParts, ", "),
		joinStrings(whereParts, " AND "))

	_, err := targetPool.Exec(ctx, query, args...)
	return err
}

// logMergeDecision logs a merge decision to the audit log.
func (m *Merger) logMergeDecision(ctx context.Context, mergeID uuid.UUID, schema, table string, pkValue map[string]any, category OverlapCategory, resolution *string, nodeAValue, nodeBValue map[string]any, resolvedBy *string) error {
	pkJSON, err := json.Marshal(pkValue)
	if err != nil {
		return err
	}

	var nodeAJSON, nodeBJSON []byte
	if nodeAValue != nil {
		nodeAJSON, _ = json.Marshal(nodeAValue)
	}
	if nodeBValue != nil {
		nodeBJSON, _ = json.Marshal(nodeBValue)
	}

	query := `
		SELECT steep_repl.log_merge_decision($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = m.localPool.Exec(ctx, query, mergeID, schema, table, pkJSON, string(category), resolution, nodeAJSON, nodeBJSON, resolvedBy)
	return err
}

// GenerateConflictReport generates a report of conflicts for manual resolution.
func (m *Merger) GenerateConflictReport(ctx context.Context, schema, table string, pkColumns []string, remoteServer string) (*ConflictReport, error) {
	conflicts, err := m.getConflicts(ctx, schema, table, pkColumns, remoteServer)
	if err != nil {
		return nil, err
	}

	return &ConflictReport{
		MergeID:    uuid.New(),
		Table:      fmt.Sprintf("%s.%s", schema, table),
		Conflicts:  conflicts,
		TotalCount: len(conflicts),
	}, nil
}
