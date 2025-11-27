package sqleditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// copyCell copies the current cell value to clipboard.
func (v *SQLEditorView) copyCell() tea.Cmd {
	if v.results == nil || len(v.results.Rows) == 0 || v.selectedRow < 0 {
		return nil
	}

	row := v.results.Rows[v.selectedRow]
	if len(row) == 0 {
		return nil
	}

	// Use selected column, default to 0 if out of bounds
	col := v.selectedCol
	if col < 0 || col >= len(row) {
		col = 0
	}
	value := row[col]

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return CellCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(value)
		return CellCopiedMsg{Value: value, Error: err}
	}
}

// copyRow copies the entire row to clipboard (tab-separated).
func (v *SQLEditorView) copyRow() tea.Cmd {
	if v.results == nil || len(v.results.Rows) == 0 || v.selectedRow < 0 {
		return nil
	}

	row := v.results.Rows[v.selectedRow]
	values := strings.Join(row, "\t")

	return func() tea.Msg {
		if !v.clipboard.IsAvailable() {
			return RowCopiedMsg{Error: fmt.Errorf("clipboard not available")}
		}
		err := v.clipboard.Write(values)
		return RowCopiedMsg{Values: row, Error: err}
	}
}
