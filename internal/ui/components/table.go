package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// Table represents a table component for displaying tabular data
type Table struct {
	width  int
	height int

	// Data
	headers []string
	rows    [][]string

	// Display options
	showHeaders bool
	columnWidths []int
}

// NewTable creates a new table component
func NewTable() *Table {
	return &Table{
		showHeaders: true,
	}
}

// SetSize sets the dimensions of the table
func (t *Table) SetSize(width, height int) {
	t.width = width
	t.height = height
}

// SetHeaders sets the table headers
func (t *Table) SetHeaders(headers []string) {
	t.headers = headers
	t.calculateColumnWidths()
}

// SetRows sets the table rows
func (t *Table) SetRows(rows [][]string) {
	t.rows = rows
	t.calculateColumnWidths()
}

// SetShowHeaders sets whether to display headers
func (t *Table) SetShowHeaders(show bool) {
	t.showHeaders = show
}

// calculateColumnWidths automatically calculates column widths based on content
func (t *Table) calculateColumnWidths() {
	if len(t.headers) == 0 {
		return
	}

	t.columnWidths = make([]int, len(t.headers))

	// Start with header widths
	for i, header := range t.headers {
		t.columnWidths[i] = len(header)
	}

	// Check all rows for maximum width
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(t.columnWidths) && len(cell) > t.columnWidths[i] {
				t.columnWidths[i] = len(cell)
			}
		}
	}

	// Add padding
	for i := range t.columnWidths {
		t.columnWidths[i] += 2 // Add 2 for padding
	}
}

// View renders the table
func (t *Table) View() string {
	if len(t.headers) == 0 && len(t.rows) == 0 {
		return styles.InfoStyle.Render("No data to display")
	}

	var b strings.Builder

	// Render headers
	if t.showHeaders && len(t.headers) > 0 {
		b.WriteString(t.renderRow(t.headers, styles.TableHeaderStyle))
		b.WriteString("\n")
	}

	// Render rows
	for i, row := range t.rows {
		style := styles.TableCellStyle
		if i%2 == 1 {
			style = styles.TableRowAltStyle
		}
		b.WriteString(t.renderRow(row, style))
		if i < len(t.rows)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderRow renders a single row with the given style
func (t *Table) renderRow(cells []string, style lipgloss.Style) string {
	var b strings.Builder

	for i, cell := range cells {
		width := 20 // Default width
		if i < len(t.columnWidths) {
			width = t.columnWidths[i]
		}

		// Truncate cell if too long
		displayCell := cell
		if len(displayCell) > width-2 {
			displayCell = displayCell[:width-5] + "..."
		}

		// Pad cell to width
		cellStyle := style.Copy().Width(width)
		b.WriteString(cellStyle.Render(displayCell))
	}

	return b.String()
}

// CompactView renders a compact version of the table
func (t *Table) CompactView() string {
	if len(t.rows) == 0 {
		return "No data"
	}

	return lipgloss.NewStyle().
		Foreground(styles.ColorTextDim).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			"Rows: "+string(rune(len(t.rows))),
			"Columns: "+string(rune(len(t.headers))),
		))
}
