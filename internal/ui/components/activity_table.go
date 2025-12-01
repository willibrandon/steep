package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/metrics"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// Minimum width to show the Trend column with sparklines
const minWidthForSparklines = 90

// SortColumn represents the column to sort by.
type SortColumn int

const (
	SortByPID SortColumn = iota
	SortByUser
	SortByDatabase
	SortByState
	SortByDuration
)

// ActivityTable displays PostgreSQL connections in a sortable table.
type ActivityTable struct {
	table             table.Model
	connections       []models.Connection
	width             int
	height            int
	queryColWidth     int // Dynamic width for query column
	showSparklines    bool
	connectionMetrics *metrics.ConnectionMetrics

	// Sorting
	sortColumn SortColumn
	sortAsc    bool

	// Styles for focused and unfocused states
	focusedStyles   table.Styles
	unfocusedStyles table.Styles
}

// NewActivityTable creates a new activity table component.
func NewActivityTable() *ActivityTable {
	columns := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "User", Width: 10},
		{Title: "Database", Width: 10},
		{Title: "State", Width: 12},
		{Title: "Duration", Width: 8},
		{Title: "Query", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	// Create focused styles (with selection highlighting)
	focusedStyles := table.DefaultStyles()
	focusedStyles.Header = focusedStyles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(styles.ColorBorder).
		BorderBottom(true).
		Bold(false)
	focusedStyles.Selected = focusedStyles.Selected.
		Foreground(styles.ColorSelectedFg).
		Background(styles.ColorSelectedBg).
		Bold(false)

	// Create unfocused styles (no selection highlighting)
	unfocusedStyles := table.DefaultStyles()
	unfocusedStyles.Header = unfocusedStyles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(styles.ColorBorder).
		BorderBottom(true).
		Bold(false)
	// Selected row has no special styling when unfocused (empty style)
	// Note: We use an empty style, not Cell style, because Cell has padding
	// that would be applied to the whole row causing indentation
	unfocusedStyles.Selected = lipgloss.NewStyle()

	t.SetStyles(focusedStyles)

	return &ActivityTable{
		table:           t,
		focusedStyles:   focusedStyles,
		unfocusedStyles: unfocusedStyles,
	}
}

// SetConnections updates the table with new connection data.
func (a *ActivityTable) SetConnections(connections []models.Connection) {
	a.connections = connections
	a.refreshRows()
}

// SetConnectionMetrics sets the connection metrics for sparklines.
func (a *ActivityTable) SetConnectionMetrics(cm *metrics.ConnectionMetrics) {
	a.connectionMetrics = cm
}

// refreshRows rebuilds the table rows with current query column width.
func (a *ActivityTable) refreshRows() {
	queryWidth := a.queryColWidth
	if queryWidth < 30 {
		queryWidth = 30 // Minimum width
	}

	rows := make([]table.Row, len(a.connections))
	for i, conn := range a.connections {
		if a.showSparklines {
			trend := a.renderTrend(conn.PID)
			rows[i] = table.Row{
				fmt.Sprintf("%d", conn.PID),
				truncate(conn.User, 10),
				truncate(conn.Database, 10),
				truncate(string(conn.State), 12),
				conn.FormatDuration(),
				trend,
				truncate(conn.Query, queryWidth),
			}
		} else {
			rows[i] = table.Row{
				fmt.Sprintf("%d", conn.PID),
				truncate(conn.User, 10),
				truncate(conn.Database, 10),
				truncate(string(conn.State), 12),
				conn.FormatDuration(),
				truncate(conn.Query, queryWidth),
			}
		}
	}

	a.table.SetRows(rows)
}

// renderTrend renders a sparkline with trend indicator for a connection.
// Note: We render without colors because the bubbles table component
// doesn't handle ANSI escape codes well in cell content.
func (a *ActivityTable) renderTrend(pid int) string {
	if a.connectionMetrics == nil {
		return strings.Repeat("─", 8) + "→"
	}

	durations := a.connectionMetrics.GetDurations(pid)
	if len(durations) == 0 {
		return strings.Repeat("─", 8) + "→"
	}

	// Render sparkline without colors (table can't handle ANSI codes)
	sparkline := renderPlainSparkline(durations, 8)

	// Add trend indicator
	trend := GetTrend(durations)
	indicator := TrendIndicator(trend)

	return sparkline + indicator
}

// renderPlainSparkline renders a sparkline without any ANSI color codes.
func renderPlainSparkline(data []float64, width int) string {
	if len(data) == 0 {
		return strings.Repeat("─", width)
	}

	// Unicode block characters from lowest to highest
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Find min and max for scaling
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// Avoid division by zero
	valueRange := maxVal - minVal
	if valueRange == 0 {
		valueRange = 1
	}

	// Resample data to fit width
	resampled := resampleForSparkline(data, width)

	// Build sparkline
	var sb strings.Builder
	for _, v := range resampled {
		// Normalize to 0-7 range (8 block characters)
		normalized := (v - minVal) / valueRange
		idx := int(normalized * 7)
		if idx > 7 {
			idx = 7
		}
		if idx < 0 {
			idx = 0
		}
		sb.WriteRune(blocks[idx])
	}

	return sb.String()
}

// resampleForSparkline resamples data to fit within the target width.
func resampleForSparkline(data []float64, targetWidth int) []float64 {
	if len(data) <= targetWidth {
		return data
	}

	result := make([]float64, targetWidth)
	bucketSize := float64(len(data)) / float64(targetWidth)

	for i := 0; i < targetWidth; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > len(data) {
			end = len(data)
		}
		if start >= end {
			start = end - 1
		}
		if start < 0 {
			start = 0
		}

		// Average the values in this bucket
		sum := 0.0
		count := 0
		for j := start; j < end; j++ {
			sum += data[j]
			count++
		}
		if count > 0 {
			result[i] = sum / float64(count)
		}
	}

	return result
}

// SetSize sets the dimensions of the table.
func (a *ActivityTable) SetSize(width, height int) {
	a.width = width
	a.height = height

	// Adjust table height (leave room for header)
	tableHeight := height - 2
	if tableHeight < 5 {
		tableHeight = 5
	}
	a.table.SetHeight(tableHeight)

	// Determine if we should show sparklines based on width
	a.showSparklines = width >= minWidthForSparklines

	// Calculate query column width to fill remaining space
	// Without sparklines: PID(8) + User(12) + Database(12) + State(14) + Duration(10) + spacing(10) = 66
	// With sparklines: add Trend(10) = 76
	var fixedWidth int
	if a.showSparklines {
		fixedWidth = 76
	} else {
		fixedWidth = 66
	}

	a.queryColWidth = width - fixedWidth
	if a.queryColWidth < 30 {
		a.queryColWidth = 30
	}

	// Rebuild columns with sort indicators
	a.rebuildColumns()
}

// Update handles messages for the activity table.
func (a *ActivityTable) Update(msg tea.Msg) (*ActivityTable, tea.Cmd) {
	var cmd tea.Cmd
	a.table, cmd = a.table.Update(msg)
	return a, cmd
}

// View renders the activity table.
func (a *ActivityTable) View() string {
	if len(a.connections) == 0 {
		emptyMsg := styles.InfoStyle.Render("No connections found")
		return styles.TableBorderStyle.Render(a.table.View() + "\n" + emptyMsg)
	}
	return styles.TableBorderStyle.Render(a.table.View())
}

// SelectedConnection returns the currently selected connection.
func (a *ActivityTable) SelectedConnection() *models.Connection {
	idx := a.table.Cursor()
	if idx < 0 || idx >= len(a.connections) {
		return nil
	}
	return &a.connections[idx]
}

// SelectedIndex returns the index of the selected row.
func (a *ActivityTable) SelectedIndex() int {
	return a.table.Cursor()
}

// MoveUp moves the selection up by one row.
func (a *ActivityTable) MoveUp() {
	a.table.MoveUp(1)
}

// MoveDown moves the selection down by one row.
func (a *ActivityTable) MoveDown() {
	a.table.MoveDown(1)
}

// GotoTop moves the selection to the first row.
func (a *ActivityTable) GotoTop() {
	a.table.GotoTop()
}

// GotoBottom moves the selection to the last row.
func (a *ActivityTable) GotoBottom() {
	a.table.GotoBottom()
}

// PageUp moves the selection up by one page.
func (a *ActivityTable) PageUp() {
	for i := 0; i < a.height-2; i++ {
		a.table.MoveUp(1)
	}
}

// PageDown moves the selection down by one page.
func (a *ActivityTable) PageDown() {
	for i := 0; i < a.height-2; i++ {
		a.table.MoveDown(1)
	}
}

// Focus gives focus to the table and restores selection highlighting.
func (a *ActivityTable) Focus() {
	a.table.Focus()
	a.table.SetStyles(a.focusedStyles)
	// Refresh rows to apply new styles
	if len(a.connections) > 0 {
		a.refreshRows()
	}
}

// Blur removes focus from the table and removes selection highlighting.
func (a *ActivityTable) Blur() {
	a.table.Blur()
	a.table.SetStyles(a.unfocusedStyles)
	// Refresh rows to apply new styles
	if len(a.connections) > 0 {
		a.refreshRows()
	}
}

// Focused returns whether the table is focused.
func (a *ActivityTable) Focused() bool {
	return a.table.Focused()
}

// ConnectionCount returns the number of connections in the table.
func (a *ActivityTable) ConnectionCount() int {
	return len(a.connections)
}

// truncate truncates a string to maxLen with "..." suffix.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// SetSort sets the current sort column and direction, and updates column headers.
func (a *ActivityTable) SetSort(col SortColumn, asc bool) {
	a.sortColumn = col
	a.sortAsc = asc
	// Rebuild columns with updated sort indicators
	a.rebuildColumns()
}

// sortIndicator returns the column title with a sort arrow if it's the active sort column.
func (a *ActivityTable) sortIndicator(title string, col SortColumn) string {
	if a.sortColumn == col {
		if a.sortAsc {
			return title + " ↑"
		}
		return title + " ↓"
	}
	return title
}

// rebuildColumns rebuilds the table columns with current sort indicators.
func (a *ActivityTable) rebuildColumns() {
	var columns []table.Column
	if a.showSparklines {
		columns = []table.Column{
			{Title: a.sortIndicator("PID", SortByPID), Width: 8},
			{Title: a.sortIndicator("User", SortByUser), Width: 12},
			{Title: a.sortIndicator("Database", SortByDatabase), Width: 12},
			{Title: a.sortIndicator("State", SortByState), Width: 14},
			{Title: a.sortIndicator("Duration", SortByDuration), Width: 10},
			{Title: "Trend", Width: 10},
			{Title: "Query", Width: a.queryColWidth},
		}
	} else {
		columns = []table.Column{
			{Title: a.sortIndicator("PID", SortByPID), Width: 8},
			{Title: a.sortIndicator("User", SortByUser), Width: 12},
			{Title: a.sortIndicator("Database", SortByDatabase), Width: 12},
			{Title: a.sortIndicator("State", SortByState), Width: 14},
			{Title: a.sortIndicator("Duration", SortByDuration), Width: 10},
			{Title: "Query", Width: a.queryColWidth},
		}
	}

	// Preserve selection if possible
	currentIdx := a.table.Cursor()

	a.table.SetRows(nil)
	a.table.SetColumns(columns)

	// Refresh rows
	if len(a.connections) > 0 {
		a.refreshRows()
		// Restore cursor position if valid
		if currentIdx >= 0 && currentIdx < len(a.connections) {
			a.table.SetCursor(currentIdx)
		}
	}
}
