package sqleditor

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCSV(t *testing.T) {
	// Create test results
	results := &ResultSet{
		Columns: []Column{
			{Name: "id", TypeName: "integer"},
			{Name: "name", TypeName: "text"},
			{Name: "value", TypeName: "numeric"},
		},
		Rows: [][]string{
			{"1", "Alice", "100.50"},
			{"2", "Bob", "200.75"},
			{"3", "Charlie", "NULL"},
		},
		RawRows: [][]any{
			{int64(1), "Alice", 100.50},
			{int64(2), "Bob", 200.75},
			{int64(3), "Charlie", nil},
		},
		TotalRows: 3,
	}

	// Create temp file
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test.csv")

	// Export
	result := ExportCSV(results, filename)
	if result.Error != nil {
		t.Fatalf("ExportCSV failed: %v", result.Error)
	}

	if result.RowCount != 3 {
		t.Errorf("expected 3 rows, got %d", result.RowCount)
	}

	// Read and verify
	file, err := os.Open(result.FilePath)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	// Check header
	if len(records) != 4 { // 1 header + 3 rows
		t.Errorf("expected 4 records, got %d", len(records))
	}

	if records[0][0] != "id" || records[0][1] != "name" || records[0][2] != "value" {
		t.Errorf("unexpected header: %v", records[0])
	}

	// Check first data row
	if records[1][0] != "1" || records[1][1] != "Alice" || records[1][2] != "100.50" {
		t.Errorf("unexpected first row: %v", records[1])
	}

	// Check NULL handling (should be empty string)
	if records[3][2] != "" {
		t.Errorf("expected empty string for NULL, got %q", records[3][2])
	}
}

func TestExportCSV_Escaping(t *testing.T) {
	// Test values that need CSV escaping
	results := &ResultSet{
		Columns: []Column{
			{Name: "data", TypeName: "text"},
		},
		Rows: [][]string{
			{`value with "quotes"`},
			{"value, with, commas"},
			{"value\nwith\nnewlines"},
			{"normal value"},
		},
		RawRows: [][]any{
			{`value with "quotes"`},
			{"value, with, commas"},
			{"value\nwith\nnewlines"},
			{"normal value"},
		},
		TotalRows: 4,
	}

	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "escaping")

	result := ExportCSV(results, filename)
	if result.Error != nil {
		t.Fatalf("ExportCSV failed: %v", result.Error)
	}

	// Should add .csv extension
	if !strings.HasSuffix(result.FilePath, ".csv") {
		t.Errorf("expected .csv extension, got %s", result.FilePath)
	}

	// Read back and verify escaping worked correctly
	file, err := os.Open(result.FilePath)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	// Verify quotes are properly escaped
	if records[1][0] != `value with "quotes"` {
		t.Errorf("quote escaping failed, got: %s", records[1][0])
	}

	// Verify commas are handled
	if records[2][0] != "value, with, commas" {
		t.Errorf("comma handling failed, got: %s", records[2][0])
	}

	// Verify newlines are handled
	if records[3][0] != "value\nwith\nnewlines" {
		t.Errorf("newline handling failed, got: %s", records[3][0])
	}
}

func TestExportJSON(t *testing.T) {
	results := &ResultSet{
		Columns: []Column{
			{Name: "id", TypeName: "integer"},
			{Name: "name", TypeName: "text"},
			{Name: "active", TypeName: "boolean"},
		},
		Rows: [][]string{
			{"1", "Alice", "true"},
			{"2", "Bob", "false"},
			{"3", "Charlie", "NULL"},
		},
		RawRows: [][]any{
			{int64(1), "Alice", true},
			{int64(2), "Bob", false},
			{int64(3), "Charlie", nil},
		},
		TotalRows: 3,
	}

	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test.json")

	result := ExportJSON(results, filename)
	if result.Error != nil {
		t.Fatalf("ExportJSON failed: %v", result.Error)
	}

	if result.RowCount != 3 {
		t.Errorf("expected 3 rows, got %d", result.RowCount)
	}

	// Read and parse JSON
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}

	// Check first record
	if records[0]["id"].(float64) != 1 {
		t.Errorf("expected id=1, got %v", records[0]["id"])
	}
	if records[0]["name"].(string) != "Alice" {
		t.Errorf("expected name=Alice, got %v", records[0]["name"])
	}
	if records[0]["active"].(bool) != true {
		t.Errorf("expected active=true, got %v", records[0]["active"])
	}

	// Check NULL handling (should be nil in JSON)
	if records[2]["active"] != nil {
		t.Errorf("expected nil for NULL, got %v", records[2]["active"])
	}
}

func TestExportJSON_AutoExtension(t *testing.T) {
	results := &ResultSet{
		Columns: []Column{
			{Name: "col", TypeName: "text"},
		},
		Rows:    [][]string{{"value"}},
		RawRows: [][]any{{"value"}},
	}

	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "noext")

	result := ExportJSON(results, filename)
	if result.Error != nil {
		t.Fatalf("ExportJSON failed: %v", result.Error)
	}

	if !strings.HasSuffix(result.FilePath, ".json") {
		t.Errorf("expected .json extension, got %s", result.FilePath)
	}
}

func TestExportCSV_NoResults(t *testing.T) {
	result := ExportCSV(nil, "test.csv")
	if result.Error == nil {
		t.Error("expected error for nil results")
	}
}

func TestExportJSON_NoResults(t *testing.T) {
	result := ExportJSON(nil, "test.json")
	if result.Error == nil {
		t.Error("expected error for nil results")
	}
}

func TestExportCSV_NoColumns(t *testing.T) {
	results := &ResultSet{
		Columns: []Column{},
	}
	result := ExportCSV(results, "test.csv")
	if result.Error == nil {
		t.Error("expected error for empty columns")
	}
}

func TestExportJSON_NoColumns(t *testing.T) {
	results := &ResultSet{
		Columns: []Column{},
	}
	result := ExportJSON(results, "test.json")
	if result.Error == nil {
		t.Error("expected error for empty columns")
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty path", "", true},
		{"relative path", "foo/bar.csv", false},
		{"absolute path", "/tmp/test.csv", false},
		{"tilde path", "~/test.csv", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("expandPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestFormatExportSuccess(t *testing.T) {
	result := &ExportResult{
		FilePath: "/tmp/test.csv",
		RowCount: 42,
		Format:   ExportFormatCSV,
	}

	msg := FormatExportSuccess(result)
	if !strings.Contains(msg, "42 rows") {
		t.Errorf("expected row count in message, got: %s", msg)
	}
	if !strings.Contains(msg, "CSV") {
		t.Errorf("expected CSV in message, got: %s", msg)
	}
	if !strings.Contains(msg, "/tmp/test.csv") {
		t.Errorf("expected file path in message, got: %s", msg)
	}
}
