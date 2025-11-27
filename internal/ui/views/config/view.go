// Package config provides the Configuration Viewer view.
package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ConfigDataMsg contains refreshed configuration data.
type ConfigDataMsg struct {
	Data  *models.ConfigData
	Error error
}

// RefreshConfigMsg triggers a data refresh.
type RefreshConfigMsg struct{}

// ConfigMode represents the current interaction mode.
type ConfigMode int

const (
	ModeNormal ConfigMode = iota
	ModeHelp
)

// SortColumn represents the available sort columns.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortByCategory
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByName:
		return "Name"
	case SortByCategory:
		return "Category"
	default:
		return "Unknown"
	}
}

// ConfigView displays PostgreSQL configuration parameters.
type ConfigView struct {
	width  int
	height int

	// State
	mode           ConfigMode
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool

	// Data
	data *models.ConfigData
	err  error

	// Table state
	selectedIdx  int
	scrollOffset int
	sortColumn   SortColumn
	sortAsc      bool // true = ascending (default for name)
}

// NewConfigView creates a new configuration view.
func NewConfigView() *ConfigView {
	return &ConfigView{
		mode:       ModeNormal,
		data:       models.NewConfigData(),
		sortColumn: SortByName,
		sortAsc:    true,
	}
}

// Init initializes the configuration view.
func (v *ConfigView) Init() tea.Cmd {
	return nil
}

// SetSize sets the dimensions of the view.
func (v *ConfigView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *ConfigView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *ConfigView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// IsInputMode returns true when the view is in a mode that should consume keys.
func (v *ConfigView) IsInputMode() bool {
	return v.mode != ModeNormal
}

// Update handles messages for the configuration view.
func (v *ConfigView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v, v.handleKeyPress(msg)

	case tea.MouseMsg:
		if v.mode == ModeNormal {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.moveSelection(-1)
			case tea.MouseButtonWheelDown:
				v.moveSelection(1)
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					// WARNING: tableStartY=7 was determined empirically (2025-11).
					// If mouse clicks select wrong rows, adjust this offset.
					tableStartY := 7
					clickedRow := msg.Y - tableStartY
					if clickedRow >= 0 {
						newIdx := v.scrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(v.data.Parameters) {
							v.selectedIdx = newIdx
						}
					}
				}
			}
		}

	case ConfigDataMsg:
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			v.data = msg.Data
			v.lastUpdate = time.Now()
			v.err = nil
			// Apply current sort order
			v.sortParameters()
			// Ensure selection is valid
			if v.selectedIdx >= len(v.data.Parameters) {
				v.selectedIdx = max(0, len(v.data.Parameters)-1)
			}
			v.ensureVisible()
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return v, nil
}

// handleKeyPress handles keyboard input.
func (v *ConfigView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "H", "q", "esc":
			v.mode = ModeNormal
		}
		return nil
	}

	// Normal mode
	switch key {
	case "h":
		v.mode = ModeHelp

	// Navigation
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g", "home":
		v.selectedIdx = 0
		v.ensureVisible()
	case "G", "end":
		if len(v.data.Parameters) > 0 {
			v.selectedIdx = len(v.data.Parameters) - 1
			v.ensureVisible()
		}
	case "ctrl+d", "pgdown":
		v.moveSelection(v.visibleRows() / 2)
	case "ctrl+u", "pgup":
		v.moveSelection(-v.visibleRows() / 2)

	// Sorting
	case "s":
		v.cycleSortColumn()
	case "S":
		v.toggleSortDirection()
	}

	return nil
}

// moveSelection moves the selection by delta rows.
func (v *ConfigView) moveSelection(delta int) {
	if len(v.data.Parameters) == 0 {
		return
	}
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(v.data.Parameters) {
		v.selectedIdx = len(v.data.Parameters) - 1
	}
	v.ensureVisible()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (v *ConfigView) ensureVisible() {
	visible := v.visibleRows()
	if visible <= 0 {
		return
	}

	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	}
	if v.selectedIdx >= v.scrollOffset+visible {
		v.scrollOffset = v.selectedIdx - visible + 1
	}
}

// visibleRows returns the number of visible table rows.
func (v *ConfigView) visibleRows() int {
	// Height minus: status bar(1) + title(1) + header(1) + separator(1) + footer(1) + padding
	return v.height - 6
}

// cycleSortColumn cycles to the next sort column.
func (v *ConfigView) cycleSortColumn() {
	v.sortColumn = (v.sortColumn + 1) % 2
	v.sortParameters()
}

// toggleSortDirection toggles the sort direction.
func (v *ConfigView) toggleSortDirection() {
	v.sortAsc = !v.sortAsc
	v.sortParameters()
}

// sortParameters sorts parameters by the current column and direction.
func (v *ConfigView) sortParameters() {
	if v.data == nil || len(v.data.Parameters) == 0 {
		return
	}

	sort.SliceStable(v.data.Parameters, func(i, j int) bool {
		var less bool
		switch v.sortColumn {
		case SortByName:
			less = strings.ToLower(v.data.Parameters[i].Name) < strings.ToLower(v.data.Parameters[j].Name)
		case SortByCategory:
			less = strings.ToLower(v.data.Parameters[i].Category) < strings.ToLower(v.data.Parameters[j].Category)
		default:
			less = v.data.Parameters[i].Name < v.data.Parameters[j].Name
		}
		if !v.sortAsc {
			return !less
		}
		return less
	})
}

// View renders the configuration view.
func (v *ConfigView) View() string {
	if v.width == 0 || v.height == 0 {
		return ""
	}

	// Help overlay
	if v.mode == ModeHelp {
		return v.renderHelp()
	}

	var b strings.Builder

	// Status bar
	b.WriteString(v.renderStatusBar())
	b.WriteString("\n")

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(titleStyle.Render("Configuration"))
	b.WriteString("\n")

	// Table
	b.WriteString(v.renderTable())

	// Footer
	b.WriteString("\n")
	b.WriteString(v.renderFooter())

	return b.String()
}

// renderStatusBar renders the status bar at the top.
func (v *ConfigView) renderStatusBar() string {
	// Connection info
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Right side indicators
	var indicators []string

	if v.refreshing {
		indicators = append(indicators, styles.WarningStyle.Render("Refreshing..."))
	} else if !v.lastUpdate.IsZero() {
		indicators = append(indicators, styles.MutedStyle.Render(fmt.Sprintf("Updated %s", v.lastUpdate.Format("15:04:05"))))
	}

	if v.data != nil && v.data.ModifiedCount > 0 {
		indicators = append(indicators, styles.WarningStyle.Render(fmt.Sprintf("%d modified", v.data.ModifiedCount)))
	}

	if v.data != nil && v.data.PendingRestartCount > 0 {
		indicators = append(indicators, styles.ErrorStyle.Render(fmt.Sprintf("%d pending restart", v.data.PendingRestartCount)))
	}

	rightContent := strings.Join(indicators, " │ ")
	titleLen := lipgloss.Width(title)
	rightLen := lipgloss.Width(rightContent)
	gap := v.width - 2 - titleLen - rightLen
	if gap < 1 {
		gap = 1
	}
	spaces := strings.Repeat(" ", gap)

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + spaces + rightContent)
}

// renderTable renders the main parameter table.
func (v *ConfigView) renderTable() string {
	if v.err != nil {
		return styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", v.err))
	}

	if v.data == nil || len(v.data.Parameters) == 0 {
		return styles.MutedStyle.Render("No configuration parameters found.")
	}

	var lines []string

	// Column widths
	nameWidth := 30
	valueWidth := 20
	unitWidth := 6
	categoryWidth := 25
	descWidth := v.width - nameWidth - valueWidth - unitWidth - categoryWidth - 16 // separators
	if descWidth < 10 {
		descWidth = 10
	}

	// Header
	sortIndicator := func(col SortColumn) string {
		if v.sortColumn == col {
			if v.sortAsc {
				return " ↑"
			}
			return " ↓"
		}
		return ""
	}

	header := fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %s",
		nameWidth, "Name"+sortIndicator(SortByName),
		valueWidth, "Value",
		unitWidth, "Unit",
		categoryWidth, "Category"+sortIndicator(SortByCategory),
		"Description")
	lines = append(lines, styles.TableHeaderStyle.Render(header))

	// Separator
	sep := strings.Repeat("─", nameWidth) + "─┼─" +
		strings.Repeat("─", valueWidth) + "─┼─" +
		strings.Repeat("─", unitWidth) + "─┼─" +
		strings.Repeat("─", categoryWidth) + "─┼─" +
		strings.Repeat("─", descWidth)
	lines = append(lines, styles.BorderStyle.Render(sep))

	// Rows
	visible := v.visibleRows()
	endIdx := v.scrollOffset + visible
	if endIdx > len(v.data.Parameters) {
		endIdx = len(v.data.Parameters)
	}

	// Yellow style for modified parameters
	modifiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // Yellow

	for i := v.scrollOffset; i < endIdx; i++ {
		p := v.data.Parameters[i]

		name := truncate(p.Name, nameWidth)
		value := truncate(p.Setting, valueWidth)
		unit := truncate(p.Unit, unitWidth)
		category := truncate(p.TopLevelCategory(), categoryWidth)
		desc := truncate(p.ShortDesc, descWidth)

		row := fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %s",
			nameWidth, name,
			valueWidth, value,
			unitWidth, unit,
			categoryWidth, category,
			desc)

		// Apply styles
		if i == v.selectedIdx {
			row = styles.TableSelectedStyle.Render(row)
		} else if p.IsModified() {
			row = modifiedStyle.Render(row)
		}

		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

// renderFooter renders the footer with key hints.
func (v *ConfigView) renderFooter() string {
	hints := styles.FooterHintStyle.Render("[j/k]nav [s/S]ort [h]elp")

	arrow := "↓"
	if v.sortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)
	count := fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.data.Parameters)), len(v.data.Parameters))
	rightSide := styles.FooterCountStyle.Render(sortInfo + "  " + count)

	gap := v.width - lipgloss.Width(hints) - lipgloss.Width(rightSide) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.
		Width(v.width - 2).
		Render(hints + spaces + rightSide)
}

// renderHelp renders the help overlay.
func (v *ConfigView) renderHelp() string {
	helpText := `Configuration Viewer Help

NAVIGATION
  j/k or ↑/↓     Move selection up/down
  g/G            Go to first/last parameter
  Ctrl+d/u       Page down/up

SORTING
  s              Cycle sort column (Name → Category)
  S              Toggle sort direction (asc/desc)

OTHER
  h              Show this help
  q              Quit application
  1-8            Switch views

Press h, q, or Esc to close this help.`

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(helpText)
}

// truncate truncates a string to maxLen, adding ellipsis if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
