package sqleditor

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExportFormat represents the export file format.
type ExportFormat int

const (
	ExportFormatCSV ExportFormat = iota
	ExportFormatJSON
)

// ExportResult contains the result of an export operation.
type ExportResult struct {
	FilePath string // Absolute path to the exported file
	RowCount int    // Number of rows exported
	Format   ExportFormat
	Error    error
}

// ExportCSV exports query results to a CSV file with proper escaping.
// Uses Go's encoding/csv which handles RFC 4180 compliant CSV escaping:
// - Fields containing commas, quotes, or newlines are quoted
// - Quotes within fields are escaped by doubling them
func ExportCSV(results *ResultSet, filename string) *ExportResult {
	if results == nil {
		return &ExportResult{Error: fmt.Errorf("no results to export")}
	}

	if len(results.Columns) == 0 {
		return &ExportResult{Error: fmt.Errorf("no columns in result set")}
	}

	// Expand tilde and resolve path
	absPath, err := expandPath(filename)
	if err != nil {
		return &ExportResult{Error: fmt.Errorf("invalid path: %w", err)}
	}

	// Ensure .csv extension
	if !strings.HasSuffix(strings.ToLower(absPath), ".csv") {
		absPath += ".csv"
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to create directory: %w", err)}
	}

	// Create file
	file, err := os.Create(absPath)
	if err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to create file: %w", err)}
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header row with column names
	header := make([]string, len(results.Columns))
	for i, col := range results.Columns {
		header[i] = col.Name
	}
	if err := writer.Write(header); err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to write header: %w", err)}
	}

	// Write data rows using formatted string values
	for _, row := range results.Rows {
		// Handle rows that might have fewer columns than header
		record := make([]string, len(results.Columns))
		for i := range results.Columns {
			if i < len(row) {
				// Convert NULL display value back to empty for CSV
				if row[i] == NullDisplayValue {
					record[i] = ""
				} else {
					record[i] = row[i]
				}
			}
		}
		if err := writer.Write(record); err != nil {
			return &ExportResult{Error: fmt.Errorf("failed to write row: %w", err)}
		}
	}

	// Check for write errors
	writer.Flush()
	if err := writer.Error(); err != nil {
		return &ExportResult{Error: fmt.Errorf("csv write error: %w", err)}
	}

	return &ExportResult{
		FilePath: absPath,
		RowCount: len(results.Rows),
		Format:   ExportFormatCSV,
	}
}

// ExportJSON exports query results to a JSON file as an array of objects.
// Each row is represented as an object with column names as keys.
// NULL values are represented as JSON null.
func ExportJSON(results *ResultSet, filename string) *ExportResult {
	if results == nil {
		return &ExportResult{Error: fmt.Errorf("no results to export")}
	}

	if len(results.Columns) == 0 {
		return &ExportResult{Error: fmt.Errorf("no columns in result set")}
	}

	// Expand tilde and resolve path
	absPath, err := expandPath(filename)
	if err != nil {
		return &ExportResult{Error: fmt.Errorf("invalid path: %w", err)}
	}

	// Ensure .json extension
	if !strings.HasSuffix(strings.ToLower(absPath), ".json") {
		absPath += ".json"
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to create directory: %w", err)}
	}

	// Build array of objects using raw values for proper JSON types
	records := make([]map[string]any, len(results.Rows))
	for i, row := range results.RawRows {
		record := make(map[string]any)
		for j, col := range results.Columns {
			if j < len(row) {
				// Use raw value directly - nil becomes JSON null
				record[col.Name] = row[j]
			} else {
				record[col.Name] = nil
			}
		}
		records[i] = record
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to marshal JSON: %w", err)}
	}

	// Write file
	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return &ExportResult{Error: fmt.Errorf("failed to write file: %w", err)}
	}

	return &ExportResult{
		FilePath: absPath,
		RowCount: len(results.Rows),
		Format:   ExportFormatJSON,
	}
}

// expandPath expands ~ to home directory and returns absolute path.
func expandPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	// Expand tilde
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	} else if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = home
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	return absPath, nil
}

// FormatExportSuccess returns a success message for export operation.
func FormatExportSuccess(result *ExportResult) string {
	format := "CSV"
	if result.Format == ExportFormatJSON {
		format = "JSON"
	}
	return fmt.Sprintf("Exported %d rows to %s: %s", result.RowCount, format, result.FilePath)
}
