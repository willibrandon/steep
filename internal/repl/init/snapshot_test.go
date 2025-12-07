package init_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	replinit "github.com/willibrandon/steep/internal/repl/init"
	"github.com/willibrandon/steep/internal/repl/models"
)

// =============================================================================
// ReadManifest Tests
// =============================================================================

func TestReadManifest_ValidManifest(t *testing.T) {
	// Create a temp directory with a valid manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")

	manifest := &models.SnapshotManifest{
		SnapshotID:      "snap_20240115_120000_node1234",
		SourceNode:      "node1234",
		LSN:             "0/1234567",
		CreatedAt:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		Compression:     models.CompressionGzip,
		ParallelWorkers: 4,
		TotalSizeBytes:  1024 * 1024,
		Tables: []models.SnapshotTableEntry{
			{
				Schema:    "public",
				Name:      "users",
				RowCount:  1000,
				SizeBytes: 512 * 1024,
				Checksum:  "sha256:abc123",
				File:      "data/public.users.csv.gz",
			},
		},
		Sequences: []models.SnapshotSequenceEntry{
			{
				Schema: "public",
				Name:   "users_id_seq",
				Value:  1001,
			},
		},
	}

	data, err := manifest.ToJSON()
	if err != nil {
		t.Fatalf("Failed to serialize manifest: %v", err)
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("Failed to write manifest file: %v", err)
	}

	// Test reading the manifest
	got, err := replinit.ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}

	if got.SnapshotID != manifest.SnapshotID {
		t.Errorf("SnapshotID = %q; want %q", got.SnapshotID, manifest.SnapshotID)
	}
	if got.SourceNode != manifest.SourceNode {
		t.Errorf("SourceNode = %q; want %q", got.SourceNode, manifest.SourceNode)
	}
	if got.LSN != manifest.LSN {
		t.Errorf("LSN = %q; want %q", got.LSN, manifest.LSN)
	}
	if got.Compression != manifest.Compression {
		t.Errorf("Compression = %q; want %q", got.Compression, manifest.Compression)
	}
	if len(got.Tables) != 1 {
		t.Errorf("len(Tables) = %d; want 1", len(got.Tables))
	}
	if len(got.Sequences) != 1 {
		t.Errorf("len(Sequences) = %d; want 1", len(got.Sequences))
	}
}

func TestReadManifest_FileNotFound(t *testing.T) {
	_, err := replinit.ReadManifest("/nonexistent/path/manifest.json")
	if err == nil {
		t.Error("ReadManifest() expected error for nonexistent file, got nil")
	}
}

func TestReadManifest_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")

	// Write invalid JSON
	if err := os.WriteFile(manifestPath, []byte("not valid json {"), 0644); err != nil {
		t.Fatalf("Failed to write invalid manifest: %v", err)
	}

	_, err := replinit.ReadManifest(manifestPath)
	if err == nil {
		t.Error("ReadManifest() expected error for invalid JSON, got nil")
	}
}

func TestReadManifest_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")

	// Write empty file
	if err := os.WriteFile(manifestPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty manifest: %v", err)
	}

	_, err := replinit.ReadManifest(manifestPath)
	if err == nil {
		t.Error("ReadManifest() expected error for empty file, got nil")
	}
}

// =============================================================================
// VerifySnapshot Tests
// =============================================================================

func TestVerifySnapshot_ValidSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create data directory
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create data directory: %v", err)
	}

	// Create a test data file
	testData := []byte("id,name,email\n1,alice,alice@example.com\n2,bob,bob@example.com\n")
	dataFile := filepath.Join(dataDir, "public.users.csv")
	if err := os.WriteFile(dataFile, testData, 0644); err != nil {
		t.Fatalf("Failed to write data file: %v", err)
	}

	// Calculate checksum
	hasher := sha256.New()
	hasher.Write(testData)
	checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	// Create manifest with correct checksum
	manifest := &models.SnapshotManifest{
		SnapshotID:     "snap_test",
		SourceNode:     "node1",
		LSN:            "0/1234567",
		CreatedAt:      time.Now(),
		Compression:    models.CompressionNone,
		TotalSizeBytes: int64(len(testData)),
		Tables: []models.SnapshotTableEntry{
			{
				Schema:    "public",
				Name:      "users",
				RowCount:  2,
				SizeBytes: int64(len(testData)),
				Checksum:  checksum,
				File:      "data/public.users.csv",
			},
		},
	}

	data, _ := manifest.ToJSON()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	// Verify should pass
	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 0 {
		t.Errorf("VerifySnapshot() errors = %v; want empty", errors)
	}
}

func TestVerifySnapshot_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manifest referencing non-existent file
	manifest := &models.SnapshotManifest{
		SnapshotID:  "snap_test",
		SourceNode:  "node1",
		LSN:         "0/1234567",
		CreatedAt:   time.Now(),
		Compression: models.CompressionNone,
		Tables: []models.SnapshotTableEntry{
			{
				Schema:   "public",
				Name:     "users",
				Checksum: "sha256:abc123",
				File:     "data/public.users.csv",
			},
		},
	}

	data, _ := manifest.ToJSON()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 1 {
		t.Errorf("VerifySnapshot() len(errors) = %d; want 1", len(errors))
	}
	if len(errors) > 0 && errors[0] != "missing file: data/public.users.csv" {
		t.Errorf("VerifySnapshot() errors[0] = %q; want missing file error", errors[0])
	}
}

func TestVerifySnapshot_ChecksumMismatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Create data directory
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create data directory: %v", err)
	}

	// Create a test data file
	testData := []byte("id,name,email\n1,alice,alice@example.com\n")
	dataFile := filepath.Join(dataDir, "public.users.csv")
	if err := os.WriteFile(dataFile, testData, 0644); err != nil {
		t.Fatalf("Failed to write data file: %v", err)
	}

	// Create manifest with wrong checksum
	manifest := &models.SnapshotManifest{
		SnapshotID:  "snap_test",
		SourceNode:  "node1",
		LSN:         "0/1234567",
		CreatedAt:   time.Now(),
		Compression: models.CompressionNone,
		Tables: []models.SnapshotTableEntry{
			{
				Schema:   "public",
				Name:     "users",
				Checksum: "sha256:wrongchecksum123456789",
				File:     "data/public.users.csv",
			},
		},
	}

	data, _ := manifest.ToJSON()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 1 {
		t.Errorf("VerifySnapshot() len(errors) = %d; want 1", len(errors))
	}
	if len(errors) > 0 {
		found := false
		for _, e := range errors {
			if len(e) > 0 && e[:8] == "checksum" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("VerifySnapshot() expected checksum mismatch error, got %v", errors)
		}
	}
}

func TestVerifySnapshot_MultipleTables(t *testing.T) {
	tmpDir := t.TempDir()

	// Create data directory
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create data directory: %v", err)
	}

	// Create multiple test files with correct checksums
	tables := []struct {
		name string
		data []byte
	}{
		{"public.users.csv", []byte("id,name\n1,alice\n2,bob\n")},
		{"public.orders.csv", []byte("id,user_id,total\n1,1,100.00\n")},
		{"public.products.csv", []byte("id,name,price\n1,widget,9.99\n")},
	}

	var tableEntries []models.SnapshotTableEntry
	for _, tbl := range tables {
		filePath := filepath.Join(dataDir, tbl.name)
		if err := os.WriteFile(filePath, tbl.data, 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", tbl.name, err)
		}

		hasher := sha256.New()
		hasher.Write(tbl.data)
		checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

		tableEntries = append(tableEntries, models.SnapshotTableEntry{
			Schema:    "public",
			Name:      tbl.name[:len(tbl.name)-4], // remove .csv
			SizeBytes: int64(len(tbl.data)),
			Checksum:  checksum,
			File:      "data/" + tbl.name,
		})
	}

	manifest := &models.SnapshotManifest{
		SnapshotID:  "snap_multi",
		SourceNode:  "node1",
		LSN:         "0/1234567",
		CreatedAt:   time.Now(),
		Compression: models.CompressionNone,
		Tables:      tableEntries,
	}

	data, _ := manifest.ToJSON()
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 0 {
		t.Errorf("VerifySnapshot() errors = %v; want empty", errors)
	}
}

func TestVerifySnapshot_GzippedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create data directory
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create data directory: %v", err)
	}

	// Create a gzipped test file
	testData := []byte("id,name,email\n1,alice,alice@example.com\n2,bob,bob@example.com\n")
	dataFile := filepath.Join(dataDir, "public.users.csv.gz")

	file, err := os.Create(dataFile)
	if err != nil {
		t.Fatalf("Failed to create gzip file: %v", err)
	}

	gzWriter := gzip.NewWriter(file)
	gzWriter.Write(testData)
	gzWriter.Close()
	file.Close()

	// Read the gzipped file to get checksum
	gzippedData, _ := os.ReadFile(dataFile)
	hasher := sha256.New()
	hasher.Write(gzippedData)
	checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	// Create manifest
	manifest := &models.SnapshotManifest{
		SnapshotID:  "snap_gzip",
		SourceNode:  "node1",
		LSN:         "0/1234567",
		CreatedAt:   time.Now(),
		Compression: models.CompressionGzip,
		Tables: []models.SnapshotTableEntry{
			{
				Schema:    "public",
				Name:      "users",
				RowCount:  2,
				SizeBytes: int64(len(gzippedData)),
				Checksum:  checksum,
				File:      "data/public.users.csv.gz",
			},
		},
	}

	data, _ := manifest.ToJSON()
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 0 {
		t.Errorf("VerifySnapshot() errors = %v; want empty", errors)
	}
}

func TestVerifySnapshot_NoManifest(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := replinit.VerifySnapshot(tmpDir)
	if err == nil {
		t.Error("VerifySnapshot() expected error for missing manifest, got nil")
	}
}

func TestVerifySnapshot_EmptySnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manifest with no tables
	manifest := &models.SnapshotManifest{
		SnapshotID:  "snap_empty",
		SourceNode:  "node1",
		LSN:         "0/1234567",
		CreatedAt:   time.Now(),
		Compression: models.CompressionNone,
		Tables:      []models.SnapshotTableEntry{},
	}

	data, _ := manifest.ToJSON()
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	errors, err := replinit.VerifySnapshot(tmpDir)
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if len(errors) != 0 {
		t.Errorf("VerifySnapshot() errors = %v; want empty for empty snapshot", errors)
	}
}

// =============================================================================
// TwoPhaseProgress Tests
// =============================================================================

func TestTwoPhaseProgress_Fields(t *testing.T) {
	progress := replinit.TwoPhaseProgress{
		SnapshotID:          "snap_test",
		Phase:               "data",
		OverallPercent:      50.5,
		CurrentTable:        "public.users",
		CurrentTablePercent: 75.0,
		BytesProcessed:      1024 * 1024 * 100, // 100MB
		ThroughputMBSec:     25.5,
		ETASeconds:          120,
		LSN:                 "0/1234567",
		Complete:            false,
		Error:               "",
	}

	if progress.SnapshotID != "snap_test" {
		t.Errorf("SnapshotID = %q; want %q", progress.SnapshotID, "snap_test")
	}
	if progress.Phase != "data" {
		t.Errorf("Phase = %q; want %q", progress.Phase, "data")
	}
	if progress.OverallPercent != 50.5 {
		t.Errorf("OverallPercent = %f; want %f", progress.OverallPercent, 50.5)
	}
	if progress.CurrentTable != "public.users" {
		t.Errorf("CurrentTable = %q; want %q", progress.CurrentTable, "public.users")
	}
	if progress.CurrentTablePercent != 75.0 {
		t.Errorf("CurrentTablePercent = %f; want %f", progress.CurrentTablePercent, 75.0)
	}
	if progress.BytesProcessed != 1024*1024*100 {
		t.Errorf("BytesProcessed = %d; want %d", progress.BytesProcessed, 1024*1024*100)
	}
	if progress.ThroughputMBSec != 25.5 {
		t.Errorf("ThroughputMBSec = %f; want %f", progress.ThroughputMBSec, 25.5)
	}
	if progress.ETASeconds != 120 {
		t.Errorf("ETASeconds = %d; want %d", progress.ETASeconds, 120)
	}
	if progress.LSN != "0/1234567" {
		t.Errorf("LSN = %q; want %q", progress.LSN, "0/1234567")
	}
	if progress.Complete {
		t.Error("Complete = true; want false")
	}
	if progress.Error != "" {
		t.Errorf("Error = %q; want empty", progress.Error)
	}
}

func TestTwoPhaseProgress_ErrorState(t *testing.T) {
	progress := replinit.TwoPhaseProgress{
		SnapshotID:     "snap_test",
		Phase:          "error",
		OverallPercent: 25.0,
		Complete:       true,
		Error:          "connection lost to PostgreSQL",
	}

	if progress.SnapshotID != "snap_test" {
		t.Errorf("SnapshotID = %q; want %q", progress.SnapshotID, "snap_test")
	}
	if progress.Phase != "error" {
		t.Errorf("Phase = %q; want %q", progress.Phase, "error")
	}
	if progress.OverallPercent != 25.0 {
		t.Errorf("OverallPercent = %f; want %f", progress.OverallPercent, 25.0)
	}
	if !progress.Complete {
		t.Error("Complete = false; want true for error state")
	}
	if progress.Error != "connection lost to PostgreSQL" {
		t.Errorf("Error = %q; want %q", progress.Error, "connection lost to PostgreSQL")
	}
}

// =============================================================================
// TwoPhaseSnapshotOptions Tests
// =============================================================================

func TestTwoPhaseSnapshotOptions_Defaults(t *testing.T) {
	opts := replinit.TwoPhaseSnapshotOptions{
		OutputPath: "/tmp/snapshot",
	}

	if opts.OutputPath != "/tmp/snapshot" {
		t.Errorf("OutputPath = %q; want %q", opts.OutputPath, "/tmp/snapshot")
	}
	// Default compression should be empty (none)
	if opts.Compression != "" {
		t.Errorf("Compression = %q; want empty", opts.Compression)
	}
	// Default workers should be 0 (use default)
	if opts.ParallelWorkers != 0 {
		t.Errorf("ParallelWorkers = %d; want 0", opts.ParallelWorkers)
	}
}

func TestTwoPhaseSnapshotOptions_WithCompression(t *testing.T) {
	tests := []struct {
		name        string
		compression models.CompressionType
	}{
		{"none", models.CompressionNone},
		{"gzip", models.CompressionGzip},
		{"lz4", models.CompressionLZ4},
		{"zstd", models.CompressionZstd},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := replinit.TwoPhaseSnapshotOptions{
				Compression: tc.compression,
			}

			if opts.Compression != tc.compression {
				t.Errorf("Compression = %q; want %q", opts.Compression, tc.compression)
			}
		})
	}
}

func TestTwoPhaseSnapshotOptions_WithProgressCallback(t *testing.T) {
	var called bool
	opts := replinit.TwoPhaseSnapshotOptions{
		OutputPath:      "/tmp/snapshot",
		ParallelWorkers: 4,
		ProgressFn: func(p replinit.TwoPhaseProgress) {
			called = true
		},
	}

	if opts.OutputPath != "/tmp/snapshot" {
		t.Errorf("OutputPath = %q; want %q", opts.OutputPath, "/tmp/snapshot")
	}
	if opts.ParallelWorkers != 4 {
		t.Errorf("ParallelWorkers = %d; want %d", opts.ParallelWorkers, 4)
	}
	if opts.ProgressFn == nil {
		t.Error("ProgressFn should not be nil")
	}

	// Call the callback
	opts.ProgressFn(replinit.TwoPhaseProgress{Phase: "test"})

	if !called {
		t.Error("ProgressFn was not called")
	}
}

// =============================================================================
// SnapshotTableEntry Model Tests (using models package)
// =============================================================================

func TestSnapshotTableEntry_FullTableName(t *testing.T) {
	tests := []struct {
		schema   string
		name     string
		expected string
	}{
		{"public", "users", "public.users"},
		{"myschema", "orders", "myschema.orders"},
		{"pg_catalog", "pg_tables", "pg_catalog.pg_tables"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			entry := models.SnapshotTableEntry{
				Schema: tc.schema,
				Name:   tc.name,
			}

			got := entry.FullTableName()
			if got != tc.expected {
				t.Errorf("FullTableName() = %q; want %q", got, tc.expected)
			}
		})
	}
}

func TestSnapshotSequenceEntry_FullSequenceName(t *testing.T) {
	entry := models.SnapshotSequenceEntry{
		Schema: "public",
		Name:   "users_id_seq",
		Value:  1000,
	}

	want := "public.users_id_seq"
	got := entry.FullSequenceName()
	if got != want {
		t.Errorf("FullSequenceName() = %q; want %q", got, want)
	}
}

// =============================================================================
// SnapshotManifest Methods Tests
// =============================================================================

func TestSnapshotManifest_TableCount(t *testing.T) {
	manifest := models.SnapshotManifest{
		Tables: []models.SnapshotTableEntry{
			{Schema: "public", Name: "users"},
			{Schema: "public", Name: "orders"},
			{Schema: "public", Name: "products"},
		},
	}

	if got := manifest.TableCount(); got != 3 {
		t.Errorf("TableCount() = %d; want 3", got)
	}
}

func TestSnapshotManifest_SequenceCount(t *testing.T) {
	manifest := models.SnapshotManifest{
		Sequences: []models.SnapshotSequenceEntry{
			{Schema: "public", Name: "users_id_seq", Value: 100},
			{Schema: "public", Name: "orders_id_seq", Value: 50},
		},
	}

	if got := manifest.SequenceCount(); got != 2 {
		t.Errorf("SequenceCount() = %d; want 2", got)
	}
}

func TestSnapshotManifest_RoundTrip(t *testing.T) {
	original := &models.SnapshotManifest{
		SnapshotID:      "snap_roundtrip_test",
		SourceNode:      "source-node-id",
		LSN:             "0/ABCDEF0",
		CreatedAt:       time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
		Compression:     models.CompressionGzip,
		ParallelWorkers: 8,
		TotalSizeBytes:  1024 * 1024 * 500, // 500MB
		Tables: []models.SnapshotTableEntry{
			{
				Schema:    "public",
				Name:      "large_table",
				RowCount:  1000000,
				SizeBytes: 1024 * 1024 * 450,
				Checksum:  "sha256:abc123def456",
				File:      "data/public.large_table.csv.gz",
			},
		},
		Sequences: []models.SnapshotSequenceEntry{
			{Schema: "public", Name: "large_table_id_seq", Value: 1000001},
		},
	}

	// Serialize
	data, err := original.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Deserialize
	parsed, err := models.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	// Verify all fields match
	if parsed.SnapshotID != original.SnapshotID {
		t.Errorf("SnapshotID = %q; want %q", parsed.SnapshotID, original.SnapshotID)
	}
	if parsed.SourceNode != original.SourceNode {
		t.Errorf("SourceNode = %q; want %q", parsed.SourceNode, original.SourceNode)
	}
	if parsed.LSN != original.LSN {
		t.Errorf("LSN = %q; want %q", parsed.LSN, original.LSN)
	}
	if parsed.Compression != original.Compression {
		t.Errorf("Compression = %q; want %q", parsed.Compression, original.Compression)
	}
	if parsed.ParallelWorkers != original.ParallelWorkers {
		t.Errorf("ParallelWorkers = %d; want %d", parsed.ParallelWorkers, original.ParallelWorkers)
	}
	if parsed.TotalSizeBytes != original.TotalSizeBytes {
		t.Errorf("TotalSizeBytes = %d; want %d", parsed.TotalSizeBytes, original.TotalSizeBytes)
	}
	if len(parsed.Tables) != len(original.Tables) {
		t.Errorf("len(Tables) = %d; want %d", len(parsed.Tables), len(original.Tables))
	}
	if len(parsed.Sequences) != len(original.Sequences) {
		t.Errorf("len(Sequences) = %d; want %d", len(parsed.Sequences), len(original.Sequences))
	}
}
