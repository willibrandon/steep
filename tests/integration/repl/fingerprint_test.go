package repl_test

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// T053: Integration test for fingerprint computation
// =============================================================================

// TestSchema_FingerprintComputation tests that compute_fingerprint() SQL function
// returns consistent SHA256 hashes for table schemas.
func TestSchema_FingerprintComputation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pool := setupPostgresWithExtension(t, ctx)

	// Create test tables with known schemas
	_, err := pool.Exec(ctx, `
		CREATE TABLE test_fp_1 (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER
		);
		CREATE TABLE test_fp_2 (
			id UUID PRIMARY KEY,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			data JSONB
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create test tables: %v", err)
	}

	// Test 1: compute_fingerprint returns hex string
	var fp1 string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fp1)
	if err != nil {
		t.Fatalf("compute_fingerprint failed: %v", err)
	}
	if len(fp1) != 64 {
		t.Errorf("Fingerprint should be 64 hex chars (SHA256), got %d", len(fp1))
	}
	t.Logf("Fingerprint for test_fp_1: %s", fp1)

	// Test 2: Same table produces same fingerprint (deterministic)
	var fp1Again string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fp1Again)
	if err != nil {
		t.Fatalf("Second compute_fingerprint failed: %v", err)
	}
	if fp1 != fp1Again {
		t.Errorf("Fingerprint should be deterministic: %s != %s", fp1, fp1Again)
	}

	// Test 3: Different tables produce different fingerprints
	var fp2 string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_2')").Scan(&fp2)
	if err != nil {
		t.Fatalf("compute_fingerprint for test_fp_2 failed: %v", err)
	}
	if fp1 == fp2 {
		t.Errorf("Different tables should have different fingerprints")
	}
	t.Logf("Fingerprint for test_fp_2: %s", fp2)

	// Test 4: Fingerprint changes when schema changes
	_, err = pool.Exec(ctx, "ALTER TABLE test_fp_1 ADD COLUMN extra TEXT")
	if err != nil {
		t.Fatalf("ALTER TABLE failed: %v", err)
	}

	var fpAfterAlter string
	err = pool.QueryRow(ctx, "SELECT steep_repl.compute_fingerprint('public', 'test_fp_1')").Scan(&fpAfterAlter)
	if err != nil {
		t.Fatalf("compute_fingerprint after alter failed: %v", err)
	}
	if fp1 == fpAfterAlter {
		t.Errorf("Fingerprint should change after ALTER TABLE")
	}
	t.Logf("Fingerprint after alter: %s", fpAfterAlter)

	// Test 5: capture_fingerprint stores the result (now requires node_id)
	_, err = pool.Exec(ctx, "SELECT steep_repl.capture_fingerprint('test-node', 'public', 'test_fp_1')")
	if err != nil {
		t.Fatalf("capture_fingerprint failed: %v", err)
	}

	var storedFp string
	var columnCount int
	err = pool.QueryRow(ctx, `
		SELECT fingerprint, column_count
		FROM steep_repl.schema_fingerprints
		WHERE node_id = 'test-node' AND table_schema = 'public' AND table_name = 'test_fp_1'
	`).Scan(&storedFp, &columnCount)
	if err != nil {
		t.Fatalf("Failed to query stored fingerprint: %v", err)
	}
	if storedFp != fpAfterAlter {
		t.Errorf("Stored fingerprint mismatch: %s != %s", storedFp, fpAfterAlter)
	}
	if columnCount != 4 { // id, name, value, extra
		t.Errorf("Column count = %d, want 4", columnCount)
	}

	// Test 6: capture_all_fingerprints captures multiple tables (now requires node_id)
	var tableCount int
	err = pool.QueryRow(ctx, "SELECT steep_repl.capture_all_fingerprints('test-node')").Scan(&tableCount)
	if err != nil {
		t.Fatalf("capture_all_fingerprints failed: %v", err)
	}
	t.Logf("capture_all_fingerprints captured %d tables", tableCount)
}
