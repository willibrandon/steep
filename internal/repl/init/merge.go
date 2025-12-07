// Package init provides node initialization and snapshot management for bidirectional replication.
package init

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// =============================================================================
// Merger
// =============================================================================

// Merger handles bidirectional merge operations.
type Merger struct {
	localPool  *pgxpool.Pool
	remotePool *pgxpool.Pool
	manager    *Manager
}

// NewMerger creates a new merger for bidirectional operations.
func NewMerger(localPool, remotePool *pgxpool.Pool, manager *Manager) *Merger {
	return &Merger{
		localPool:  localPool,
		remotePool: remotePool,
		manager:    manager,
	}
}

// =============================================================================
// Helpers
// =============================================================================

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

// =============================================================================
// Pre-flight Checks
// =============================================================================

// RunPreflightChecks runs pre-flight checks before merge.
func (m *Merger) RunPreflightChecks(ctx context.Context, tables []MergeTableInfo) (*PreflightResult, error) {
	result := &PreflightResult{
		SchemaMatch:          true,
		AllTableshavePK:      true,
		NoActiveTransactions: true,
		TrackCommitTimestamp: false,
	}

	// Check track_commit_timestamp
	var trackTS string
	err := m.localPool.QueryRow(ctx, "SHOW track_commit_timestamp").Scan(&trackTS)
	if err == nil && trackTS == "on" {
		result.TrackCommitTimestamp = true
	} else {
		result.Warnings = append(result.Warnings, "track_commit_timestamp is off; last-modified strategy unavailable")
	}

	// Check tables have PKs
	for _, t := range tables {
		if len(t.PKColumns) == 0 {
			result.AllTableshavePK = false
			result.Errors = append(result.Errors, fmt.Sprintf("table %s.%s has no primary key", t.Schema, t.Name))
		}
	}

	// Check for active transactions
	var activeCount int
	query := `
		SELECT count(*) FROM pg_stat_activity
		WHERE state = 'active'
		AND pid <> pg_backend_pid()
		AND query NOT LIKE '%pg_stat_activity%'
	`
	if err := m.localPool.QueryRow(ctx, query).Scan(&activeCount); err == nil && activeCount > 0 {
		result.NoActiveTransactions = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d active transactions detected", activeCount))
	}

	return result, nil
}

// =============================================================================
// Quiesce Operations
// =============================================================================

// QuiesceTable blocks writes to a table during merge.
func (m *Merger) QuiesceTable(ctx context.Context, schema, table string, timeoutMs int) (bool, error) {
	var success bool
	err := m.localPool.QueryRow(ctx, "SELECT steep_repl.quiesce_writes($1, $2, $3)", schema, table, timeoutMs).Scan(&success)
	return success, err
}

// ReleaseQuiesce releases the quiesce lock on a table.
func (m *Merger) ReleaseQuiesce(ctx context.Context, schema, table string) error {
	_, err := m.localPool.Exec(ctx, "SELECT steep_repl.release_quiesce($1, $2)", schema, table)
	return err
}

// =============================================================================
// Full Merge Workflow (T071)
// =============================================================================

// ExecuteMerge performs a full bidirectional merge operation.
// This orchestrates the complete workflow:
// 1. Pre-flight checks
// 2. Quiesce writes on all tables (both nodes)
// 3. Analyze overlap for all tables
// 4. Resolve conflicts using the specified strategy
// 5. Transfer local-only rows A→B
// 6. Transfer remote-only rows B→A
// 7. Release quiesce locks
func (m *Merger) ExecuteMerge(ctx context.Context, config MergeConfig) (*MergeResult, error) {
	result := &MergeResult{
		MergeID:   uuid.New(),
		Strategy:  config.Strategy,
		DryRun:    config.DryRun,
		StartedAt: time.Now(),
	}

	for _, t := range config.Tables {
		result.Tables = append(result.Tables, fmt.Sprintf("%s.%s", t.Schema, t.Name))
	}

	// Sort tables by FK dependencies (parents before children)
	deps, err := m.GetFKDependencies(ctx, config.Tables)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("get FK dependencies: %v", err))
		return result, err
	}

	sortedTables, err := m.TopologicalSort(config.Tables, deps)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("topological sort: %v", err))
		return result, err
	}

	// Track quiesced tables for cleanup
	var quiescedTables []MergeTableInfo
	defer func() {
		// Release all quiesce locks on exit
		for _, t := range quiescedTables {
			_ = m.ReleaseQuiesce(context.Background(), t.Schema, t.Name)
		}
	}()

	// Quiesce all tables before analysis (unless dry run)
	if !config.DryRun {
		for _, t := range sortedTables {
			success, err := m.QuiesceTable(ctx, t.Schema, t.Name, config.QuiesceTimeoutMs)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("quiesce %s.%s: %v", t.Schema, t.Name, err))
				return result, err
			}
			if !success {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to acquire quiesce lock on %s.%s", t.Schema, t.Name))
				return result, fmt.Errorf("quiesce timeout on %s.%s", t.Schema, t.Name)
			}
			quiescedTables = append(quiescedTables, t)
		}
	}

	// Analyze overlap for all tables
	summaries, err := m.AnalyzeAllTables(ctx, sortedTables, config.RemoteServer)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("analyze overlap: %v", err))
		return result, err
	}

	// Accumulate totals
	for _, s := range summaries {
		result.TotalMatches += s.Matches
		result.TotalConflicts += s.Conflicts
		result.TotalLocalOnly += s.LocalOnly
		result.TotalRemoteOnly += s.RemoteOnly
	}

	if config.DryRun {
		result.CompletedAt = time.Now()
		return result, nil
	}

	// Process each table in order
	for i, t := range sortedTables {
		summary := summaries[i]

		// Resolve conflicts if any
		if summary.Conflicts > 0 {
			if config.Strategy == StrategyManual {
				// Skip conflict resolution for manual strategy
				result.Errors = append(result.Errors,
					fmt.Sprintf("%s.%s has %d conflicts requiring manual resolution", t.Schema, t.Name, summary.Conflicts))
				continue
			}

			resolved, err := m.ResolveConflicts(ctx, result.MergeID, t.Schema, t.Name, t.PKColumns, config.RemoteServer, config.Strategy)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("resolve conflicts for %s.%s: %v", t.Schema, t.Name, err))
				continue
			}
			result.ConflictsResolved += resolved
		}

		// Transfer local-only rows (A→B)
		if summary.LocalOnly > 0 {
			localOnlyPKs, err := m.getRowsByCategory(ctx, t.Schema, t.Name, t.PKColumns, config.RemoteServer, CategoryLocalOnly)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get local-only rows for %s.%s: %v", t.Schema, t.Name, err))
				continue
			}

			transferred, err := m.TransferRows(ctx, m.localPool, m.remotePool, t.Schema, t.Name, t.PKColumns, localOnlyPKs)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("transfer A→B for %s.%s: %v", t.Schema, t.Name, err))
			}
			result.RowsTransferredAToB += transferred

			// Log transfers to audit log
			for _, pk := range localOnlyPKs {
				resolution := "transferred_a_to_b"
				_ = m.logMergeDecision(ctx, result.MergeID, t.Schema, t.Name, pk, CategoryLocalOnly, &resolution, nil, nil, nil)
			}
		}

		// Transfer remote-only rows (B→A)
		if summary.RemoteOnly > 0 {
			remoteOnlyPKs, err := m.getRowsByCategory(ctx, t.Schema, t.Name, t.PKColumns, config.RemoteServer, CategoryRemoteOnly)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get remote-only rows for %s.%s: %v", t.Schema, t.Name, err))
				continue
			}

			transferred, err := m.TransferRows(ctx, m.remotePool, m.localPool, t.Schema, t.Name, t.PKColumns, remoteOnlyPKs)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("transfer B→A for %s.%s: %v", t.Schema, t.Name, err))
			}
			result.RowsTransferredBToA += transferred

			// Log transfers to audit log
			for _, pk := range remoteOnlyPKs {
				resolution := "transferred_b_to_a"
				_ = m.logMergeDecision(ctx, result.MergeID, t.Schema, t.Name, pk, CategoryRemoteOnly, &resolution, nil, nil, nil)
			}
		}
	}

	result.CompletedAt = time.Now()
	return result, nil
}
