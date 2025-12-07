package repl_test

import (
	"time"

	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// =============================================================================
// Additional Audit Trail Tests (T067-23)
// =============================================================================

// TestAuditLog_IncludesConflictDetails tests that audit log contains conflict details.
// T067-23: Audit Log - Includes Conflict Details
func (s *MergeTestSuite) TestAuditLog_IncludesConflictDetails() {
	ctx := s.ctx

	// SETUP: Create conflicting rows
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, email) VALUES
			(1, 'alice', 'A', 'alice@nodeA.com'),
			(2, 'bob', 'A', 'bob@nodeA.com')
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version, email) VALUES
			(1, 'alice', 'B', 'alice@nodeB.com'),
			(2, 'bob', 'B', 'bob@nodeB.com')
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// Query audit log for conflict details
	// Table schema: category (match, conflict, local_only, remote_only), resolution (kept_a, kept_b, skipped)
	rows, err := s.env.nodeAPool.Query(ctx, `
		SELECT table_schema, table_name, pk_value::text, category, node_a_value::text, node_b_value::text, resolution
		FROM steep_repl.merge_audit_log
		WHERE merge_id = $1 AND category = 'conflict'
		ORDER BY pk_value
	`, result.MergeID)
	s.Require().NoError(err)
	defer rows.Close()

	var conflicts []struct {
		Schema     string
		Table      string
		PKValue    string
		Category   string
		NodeAValue string
		NodeBValue string
		Resolution string
	}

	for rows.Next() {
		var c struct {
			Schema     string
			Table      string
			PKValue    string
			Category   string
			NodeAValue string
			NodeBValue string
			Resolution string
		}
		err := rows.Scan(&c.Schema, &c.Table, &c.PKValue, &c.Category, &c.NodeAValue, &c.NodeBValue, &c.Resolution)
		s.Require().NoError(err)
		conflicts = append(conflicts, c)
	}

	// ASSERT: Should have 2 conflict records with details
	s.Assert().Equal(2, len(conflicts), "Should have 2 conflict audit entries")

	for _, c := range conflicts {
		s.Assert().Equal("public", c.Schema)
		s.Assert().Equal("users", c.Table)
		s.Assert().Equal("conflict", c.Category)
		s.Assert().NotEmpty(c.NodeAValue, "Node A value should be recorded")
		s.Assert().NotEmpty(c.NodeBValue, "Node B value should be recorded")
		s.Assert().NotEmpty(c.Resolution, "Resolution should be recorded")
		s.T().Logf("Conflict: pk=%s, A=%s, B=%s, resolution=%s",
			c.PKValue, c.NodeAValue, c.NodeBValue, c.Resolution)
	}
}

// =============================================================================
// Additional Dry-Run Tests (T067-28)
// =============================================================================

// TestDryRun_DiffOutput tests that dry-run provides detailed diff output.
// T067-28: Dry-Run - Detailed Diff Output
func (s *MergeTestSuite) TestDryRun_DiffOutput() {
	ctx := s.ctx

	// SETUP: Create data with various categories
	// Row 1: Conflict (different version)
	// Row 2: Match (identical data including timestamps)
	// Row 3: A-only
	// Row 4: B-only
	// Must set explicit timestamps for row 2 to ensure identical hashes
	fixedTime := "2024-01-01 00:00:00+00"
	_, err := s.env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', 'A', now(), now()),
			(2, 'bob', 'v1', $1::timestamptz, $1::timestamptz),
			(3, 'charlie', 'v1', now(), now())
	`, fixedTime)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version, created_at, updated_at) VALUES
			(1, 'alice', 'B', now(), now()),
			(2, 'bob', 'v1', $1::timestamptz, $1::timestamptz),
			(4, 'diana', 'v1', now(), now())
	`, fixedTime)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
		DryRun:       true,
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: Result should contain accurate preview
	s.Assert().True(result.DryRun, "Result should indicate dry run")
	s.Assert().Equal(int64(1), result.TotalConflicts, "Should predict 1 conflict")
	s.Assert().Equal(int64(1), result.TotalMatches, "Should predict 1 match")
	s.Assert().Equal(int64(1), result.TotalLocalOnly, "Should predict 1 local-only (A->B transfer)")
	s.Assert().Equal(int64(1), result.TotalRemoteOnly, "Should predict 1 remote-only (B->A transfer)")

	s.T().Logf("Dry run preview: conflicts=%d, matches=%d, local_only=%d, remote_only=%d",
		result.TotalConflicts, result.TotalMatches, result.TotalLocalOnly, result.TotalRemoteOnly)

	// Verify per-table details
	s.Assert().Len(result.Tables, 1, "Should have 1 table result")
	if len(result.Tables) > 0 {
		s.Assert().Equal("public.users", result.Tables[0])
	}

	// Check totals from result
	s.T().Logf("Totals: matches=%d, conflicts=%d, local_only=%d, remote_only=%d",
		result.TotalMatches, result.TotalConflicts,
		result.TotalLocalOnly, result.TotalRemoteOnly)
}

// =============================================================================
// Large Scale Performance Test (T067-5)
// =============================================================================

// TestOverlapAnalysis_Performance10K tests overlap analysis with 10K rows.
// T067-5: Performance - 10K Row Overlap Analysis (< 5 seconds)
// Uses fixture: large_dataset.sql
func (s *MergeTestSuite) TestOverlapAnalysis_Performance10K() {
	ctx := s.ctx

	// SETUP: Load large dataset fixture
	// Node A: 9,500 rows (IDs 1-9500): 8,000 matches + 1,000 conflicts + 500 local_only
	// Node B: 9,500 rows (IDs 1-9000 + 9501-10000): 8,000 matches + 1,000 conflicts + 500 remote_only
	s.T().Log("Loading large dataset fixture...")
	s.env.loadLargeDataset(s.T(), ctx)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	// EXECUTE: Time the overlap analysis
	s.T().Log("Running overlap analysis...")
	start := time.Now()
	summary, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
	elapsed := time.Since(start)

	s.Require().NoError(err)

	// ASSERT: Should complete within 5 seconds
	s.T().Logf("Overlap analysis completed in %v", elapsed)
	s.T().Logf("Results: total=%d, matches=%d, conflicts=%d, a_only=%d, b_only=%d",
		summary.TotalRows, summary.Matches, summary.Conflicts,
		summary.LocalOnly, summary.RemoteOnly)

	s.Assert().Less(elapsed, 5*time.Second, "Overlap analysis should complete in < 5 seconds")

	// Verify expected distribution from large_dataset.sql fixture:
	// Total unique IDs: 10,000 (1-9500 on A, 1-9000+9501-10000 on B)
	// - 8,000 matches (IDs 1-8000)
	// - 1,000 conflicts (IDs 8001-9000)
	// - 500 local_only (IDs 9001-9500)
	// - 500 remote_only (IDs 9501-10000)
	s.Assert().GreaterOrEqual(summary.TotalRows, int64(9500), "Should have at least 9500 rows")
	s.Assert().GreaterOrEqual(summary.Matches, int64(7000), "Should have ~8000 matches")
	s.Assert().GreaterOrEqual(summary.Conflicts, int64(900), "Should have ~1000 conflicts")
	s.Assert().GreaterOrEqual(summary.LocalOnly, int64(400), "Should have ~500 local_only")
	s.Assert().GreaterOrEqual(summary.RemoteOnly, int64(400), "Should have ~500 remote_only")
}

// =============================================================================
// FK Deep Chain Test (T067-14)
// =============================================================================

// TestFKOrdering_DeepChain tests FK ordering with a deep dependency chain.
// T067-14: FK Ordering - Deep Chain (A -> B -> C -> D)
func (s *MergeTestSuite) TestFKOrdering_DeepChain() {
	ctx := s.ctx

	// Create tables with deep FK chain: D -> C -> B -> A
	schema := `
		CREATE TABLE chain_a (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE chain_b (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES chain_a(id), name TEXT);
		CREATE TABLE chain_c (id INTEGER PRIMARY KEY, b_id INTEGER REFERENCES chain_b(id), name TEXT);
		CREATE TABLE chain_d (id INTEGER PRIMARY KEY, c_id INTEGER REFERENCES chain_c(id), name TEXT);
	`

	_, err := s.env.nodeAPool.Exec(ctx, schema)
	s.Require().NoError(err)
	_, err = s.env.nodeBPool.Exec(ctx, schema)
	s.Require().NoError(err)

	// Cleanup these custom tables at end of test
	s.T().Cleanup(func() {
		s.env.nodeAPool.Exec(ctx, "DROP TABLE IF EXISTS chain_d, chain_c, chain_b, chain_a CASCADE")
		s.env.nodeBPool.Exec(ctx, "DROP TABLE IF EXISTS chain_d, chain_c, chain_b, chain_a CASCADE")
	})

	// Insert data in correct order on Node A
	_, err = s.env.nodeAPool.Exec(ctx, `
		INSERT INTO chain_a (id, name) VALUES (1, 'A1');
		INSERT INTO chain_b (id, a_id, name) VALUES (1, 1, 'B1');
		INSERT INTO chain_c (id, b_id, name) VALUES (1, 1, 'C1');
		INSERT INTO chain_d (id, c_id, name) VALUES (1, 1, 'D1');
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Define tables in reverse order (wrong order)
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "chain_d", PKColumns: []string{"id"}},
		{Schema: "public", Name: "chain_c", PKColumns: []string{"id"}},
		{Schema: "public", Name: "chain_b", PKColumns: []string{"id"}},
		{Schema: "public", Name: "chain_a", PKColumns: []string{"id"}},
	}

	deps, err := merger.GetFKDependencies(ctx, tables)
	s.Require().NoError(err)

	sorted, err := merger.TopologicalSort(tables, deps)
	s.Require().NoError(err)

	// ASSERT: Should be in correct order: A, B, C, D
	s.Assert().Equal(4, len(sorted))
	s.Assert().Equal("chain_a", sorted[0].Name, "chain_a should be first")
	s.Assert().Equal("chain_b", sorted[1].Name, "chain_b should be second")
	s.Assert().Equal("chain_c", sorted[2].Name, "chain_c should be third")
	s.Assert().Equal("chain_d", sorted[3].Name, "chain_d should be fourth")

	s.T().Logf("Sorted order: %s -> %s -> %s -> %s",
		sorted[0].Name, sorted[1].Name, sorted[2].Name, sorted[3].Name)
}

// =============================================================================
// Conflict Resolution Mixed Strategy Test (T067-12)
// =============================================================================

// TestConflictResolution_MixedTypes tests conflict resolution with various data types.
// T067-12: Resolution - Mixed Data Types
func (s *MergeTestSuite) TestConflictResolution_MixedTypes() {
	ctx := s.ctx

	// Create table with various data types
	schema := `
		CREATE TABLE mixed_types (
			id INTEGER PRIMARY KEY,
			int_val INTEGER,
			float_val DOUBLE PRECISION,
			bool_val BOOLEAN,
			text_val TEXT,
			json_val JSONB,
			ts_val TIMESTAMPTZ,
			arr_val INTEGER[]
		)
	`

	_, err := s.env.nodeAPool.Exec(ctx, schema)
	s.Require().NoError(err)
	_, err = s.env.nodeBPool.Exec(ctx, schema)
	s.Require().NoError(err)

	// Cleanup custom table at end of test
	s.T().Cleanup(func() {
		s.env.nodeAPool.Exec(ctx, "DROP TABLE IF EXISTS mixed_types CASCADE")
		s.env.nodeBPool.Exec(ctx, "DROP TABLE IF EXISTS mixed_types CASCADE")
	})

	// Insert with conflicting values of all types
	_, err = s.env.nodeAPool.Exec(ctx, `
		INSERT INTO mixed_types (id, int_val, float_val, bool_val, text_val, json_val, ts_val, arr_val)
		VALUES (1, 100, 1.5, true, 'A', '{"source": "A"}', '2024-01-01 00:00:00Z', ARRAY[1,2,3])
	`)
	s.Require().NoError(err)

	_, err = s.env.nodeBPool.Exec(ctx, `
		INSERT INTO mixed_types (id, int_val, float_val, bool_val, text_val, json_val, ts_val, arr_val)
		VALUES (1, 200, 2.5, false, 'B', '{"source": "B"}', '2024-06-01 00:00:00Z', ARRAY[4,5,6])
	`)
	s.Require().NoError(err)

	merger := replinit.NewMerger(s.env.nodeAPool, s.env.nodeBPool, nil)

	// Set up foreign server
	s.setupForeignServer()

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "mixed_types", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	result, err := merger.ExecuteMerge(ctx, mergeConfig)
	s.Require().NoError(err)

	// ASSERT: Merge handled all data types
	s.Assert().Equal(int64(1), result.TotalConflicts, "Should have 1 conflict")

	// Verify Node A's values were kept
	var intVal int
	var textVal string
	var boolVal bool
	err = s.env.nodeBPool.QueryRow(ctx, `
		SELECT int_val, text_val, bool_val FROM mixed_types WHERE id = 1
	`).Scan(&intVal, &textVal, &boolVal)
	s.Require().NoError(err)

	s.Assert().Equal(100, intVal, "Should have Node A's int value")
	s.Assert().Equal("A", textVal, "Should have Node A's text value")
	s.Assert().True(boolVal, "Should have Node A's bool value")
}
