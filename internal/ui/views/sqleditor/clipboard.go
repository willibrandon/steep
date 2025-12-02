package sqleditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// copyCell copies the current cell value to clipboard.
// Uses raw values to preserve original formatting (newlines, etc).
func (v *SQLEditorView) copyCell() tea.Cmd {
	if v.results == nil || len(v.results.RawRows) == 0 || v.selectedRow < 0 {
		return nil
	}

	row := v.results.RawRows[v.selectedRow]
	if len(row) == 0 {
		return nil
	}

	// Use selected column, default to 0 if out of bounds
	col := v.selectedCol
	if col < 0 || col >= len(row) {
		col = 0
	}
	// Format raw value for clipboard (preserves newlines, no sanitization)
	value := formatValueForClipboard(row[col])

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return CellCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(value)
		return CellCopiedMsg{Value: value, Error: err}
	}
}

// copyRow copies the entire row to clipboard (tab-separated).
// Uses raw values to preserve original formatting.
func (v *SQLEditorView) copyRow() tea.Cmd {
	if v.results == nil || len(v.results.RawRows) == 0 || v.selectedRow < 0 {
		return nil
	}

	rawRow := v.results.RawRows[v.selectedRow]
	formattedValues := make([]string, len(rawRow))
	for i, val := range rawRow {
		formattedValues[i] = formatValueForClipboard(val)
	}
	values := strings.Join(formattedValues, "\t")

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return RowCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(values)
		return RowCopiedMsg{Values: formattedValues, Error: err}
	}
}

// formatValueForClipboard formats a raw value for clipboard.
// Unlike FormatValue, this preserves newlines and other whitespace.
func formatValueForClipboard(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		// Use FormatValue for non-string types, then unsanitize
		// (FormatValue handles all the type conversions)
		return FormatValue(val)
	}
}
