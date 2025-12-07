package repl_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// =============================================================================
// Performance Benchmarks (T067-35 through T067-38)
// =============================================================================

// BenchmarkOverlapAnalysis_HashBased benchmarks hash-based overlap analysis.
// T067-35: Benchmark - Hash-Based Overlap Analysis Performance
func BenchmarkOverlapAnalysis_HashBased(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Use testing.T wrapper for setup
	t := &testing.T{}
	env := setupBidirectionalTestEnv(t, ctx)
	env.setupMergeSchema(t, ctx)

	// Create large dataset: 10K rows with various overlap patterns
	// 8,000 matches, 1,000 conflicts, 500 A-only, 500 B-only
	_, err := env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version)
		SELECT i, 'user_' || i,
			CASE
				WHEN i <= 9000 THEN 'v1'
				ELSE 'vA'
			END
		FROM generate_series(1, 10000) AS i
	`)
	require.NoError(t, err)

	_, err = env.nodeBPool.Exec(ctx, `
		INSERT INTO users (id, name, version)
		SELECT i, 'user_' || i,
			CASE
				WHEN i <= 8000 THEN 'v1'
				WHEN i <= 9000 THEN 'vB'
				ELSE 'v1'
			END
		FROM generate_series(501, 10500) AS i
	`)
	require.NoError(t, err)

	merger := replinit.NewMerger(env.nodeAPool, env.nodeBPool, nil)

	// Set up foreign server
	_, err = env.nodeAPool.Exec(ctx, fmt.Sprintf(`
		CREATE SERVER IF NOT EXISTS node_b_server
		FOREIGN DATA WRAPPER postgres_fdw
		OPTIONS (host '%s', port '%d', dbname 'testdb')
	`, env.nodeBHost, env.nodeBPort))
	require.NoError(t, err)

	_, err = env.nodeAPool.Exec(ctx, `
		CREATE USER MAPPING IF NOT EXISTS FOR test
		SERVER node_b_server
		OPTIONS (user 'test', password 'test')
	`)
	require.NoError(t, err)

	tableInfo := replinit.MergeTableInfo{
		Schema:    "public",
		Name:      "users",
		PKColumns: []string{"id"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := merger.AnalyzeOverlap(ctx, tableInfo.Schema, tableInfo.Name, tableInfo.PKColumns, "node_b_server")
		if err != nil {
			b.Fatalf("AnalyzeOverlap failed: %v", err)
		}
	}
	b.StopTimer()

	// Report: Target is < 5 seconds for 10K rows
	b.ReportMetric(float64(10000), "rows")
}

// BenchmarkDataTransfer benchmarks row transfer performance.
// T067-36: Benchmark - Data Transfer Performance
func BenchmarkDataTransfer(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t := &testing.T{}
	env := setupBidirectionalTestEnv(t, ctx)
	env.setupMergeSchema(t, ctx)

	// Create source data: 5K rows on Node A only
	_, err := env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, email)
		SELECT i, 'user_' || i, 'v1', 'user' || i || '@example.com'
		FROM generate_series(1, 5000) AS i
	`)
	require.NoError(t, err)

	merger := replinit.NewMerger(env.nodeAPool, env.nodeBPool, nil)

	// Get PKs for transfer
	pkValues := make([]map[string]any, 5000)
	for i := 0; i < 5000; i++ {
		pkValues[i] = map[string]any{"id": i + 1}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear target table
		_, _ = env.nodeBPool.Exec(ctx, "TRUNCATE users")

		// Transfer rows
		_, err := merger.TransferRows(ctx, env.nodeAPool, env.nodeBPool, "public", "users", []string{"id"}, pkValues)
		if err != nil {
			b.Fatalf("TransferRows failed: %v", err)
		}
	}
	b.StopTimer()

	// Report: Target is > 10K rows/second with COPY protocol
	b.ReportMetric(float64(5000), "rows")
}

// BenchmarkRowHash_Extension benchmarks the extension's row_hash function.
// T067-37: Benchmark - Extension Row Hash Performance
func BenchmarkRowHash_Extension(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t := &testing.T{}
	env := setupBidirectionalTestEnv(t, ctx)
	env.setupMergeSchema(t, ctx)

	// Create 10K rows to hash
	_, err := env.nodeAPool.Exec(ctx, `
		INSERT INTO users (id, name, version, email)
		SELECT i, 'user_' || i, 'v1', 'user' || i || '@example.com'
		FROM generate_series(1, 10000) AS i
	`)
	require.NoError(t, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := env.nodeAPool.Exec(ctx, `
			SELECT steep_repl.row_hash(u.*) FROM users u
		`)
		if err != nil {
			b.Fatalf("row_hash failed: %v", err)
		}
	}
	b.StopTimer()

	// Report: Target is > 500K rows/second hashing throughput
	b.ReportMetric(float64(10000), "rows")
}

// BenchmarkMergeComplete benchmarks the complete merge workflow.
// T067-38: Benchmark - Complete Merge Workflow
func BenchmarkMergeComplete(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t := &testing.T{}
	env := setupBidirectionalTestEnv(t, ctx)
	env.setupMergeSchema(t, ctx)

	// Set up foreign server once
	_, err := env.nodeAPool.Exec(ctx, fmt.Sprintf(`
		CREATE SERVER IF NOT EXISTS node_b_server
		FOREIGN DATA WRAPPER postgres_fdw
		OPTIONS (host '%s', port '%d', dbname 'testdb')
	`, env.nodeBHost, env.nodeBPort))
	require.NoError(t, err)

	_, err = env.nodeAPool.Exec(ctx, `
		CREATE USER MAPPING IF NOT EXISTS FOR test
		SERVER node_b_server
		OPTIONS (user 'test', password 'test')
	`)
	require.NoError(t, err)

	merger := replinit.NewMerger(env.nodeAPool, env.nodeBPool, nil)

	mergeConfig := replinit.MergeConfig{
		Tables: []replinit.MergeTableInfo{
			{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		},
		Strategy:     replinit.StrategyPreferNodeA,
		RemoteServer: "node_b_server",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Reset data for each iteration
		_, _ = env.nodeAPool.Exec(ctx, "TRUNCATE users")
		_, _ = env.nodeBPool.Exec(ctx, "TRUNCATE users")

		// Create fresh data: 1000 rows with 50% overlap
		_, _ = env.nodeAPool.Exec(ctx, `
			INSERT INTO users (id, name, version)
			SELECT i, 'user_' || i, 'vA'
			FROM generate_series(1, 1000) AS i
		`)
		_, _ = env.nodeBPool.Exec(ctx, `
			INSERT INTO users (id, name, version)
			SELECT i, 'user_' || i, 'vB'
			FROM generate_series(501, 1500) AS i
		`)
		b.StartTimer()

		_, err := merger.ExecuteMerge(ctx, mergeConfig)
		if err != nil {
			b.Fatalf("ExecuteMerge failed: %v", err)
		}
	}
	b.StopTimer()

	b.ReportMetric(float64(1000), "rows")
}
