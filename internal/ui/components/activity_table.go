package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ActivityTable displays PostgreSQL connections in a sortable table.
type ActivityTable struct {
	table          table.Model
	connections    []models.Connection
	width          int
	height         int
	queryColWidth  int // Dynamic width for query column
}

// NewActivityTable creates a new activity table component.
func NewActivityTable() *ActivityTable {
	columns := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "User", Width: 10},
		{Title: "Database", Width: 10},
		{Title: "State", Width: 15},
		{Title: "Duration", Width: 8},
		{Title: "Query", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(styles.ColorBorder).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(styles.ColorSelectedFg).
		Background(styles.ColorSelectedBg).
		Bold(false)
	t.SetStyles(s)

	return &ActivityTable{
		table: t,
	}
}

// SetConnections updates the table with new connection data.
func (a *ActivityTable) SetConnections(connections []models.Connection) {
	a.connections = connections
	a.refreshRows()
}

// refreshRows rebuilds the table rows with current query column width.
func (a *ActivityTable) refreshRows() {
	queryWidth := a.queryColWidth
	if queryWidth < 30 {
		queryWidth = 30 // Minimum width
	}

	rows := make([]table.Row, len(a.connections))
	for i, conn := range a.connections {
		rows[i] = table.Row{
			fmt.Sprintf("%d", conn.PID),
			truncate(conn.User, 10),
			truncate(conn.Database, 10),
			string(conn.State),
			conn.FormatDuration(),
			truncate(conn.Query, queryWidth),
		}
	}

	a.table.SetRows(rows)
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

	// Calculate query column width to fill remaining space
	// Other columns: PID(6) + User(10) + Database(10) + State(15) + Duration(8) + spacing(10) = 59
	a.queryColWidth = width - 59
	if a.queryColWidth < 30 {
		a.queryColWidth = 30
	}

	// Adjust query column to fill remaining space
	columns := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "User", Width: 10},
		{Title: "Database", Width: 10},
		{Title: "State", Width: 15},
		{Title: "Duration", Width: 8},
		{Title: "Query", Width: a.queryColWidth},
	}
	a.table.SetColumns(columns)

	// Refresh rows with new query width
	if len(a.connections) > 0 {
		a.refreshRows()
	}
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

// Focus gives focus to the table.
func (a *ActivityTable) Focus() {
	a.table.Focus()
}

// Blur removes focus from the table.
func (a *ActivityTable) Blur() {
	a.table.Blur()
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
