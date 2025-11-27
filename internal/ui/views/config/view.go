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
	ModeSearch
	ModeCategoryFilter
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

	// Filter state
	searchInput      string // Current search input
	searchFilter     string // Active search filter (applied on Enter)
	categoryFilter   string // Active category filter
	categoryIdx      int    // Selected index in category list
	filteredParams   []models.Parameter // Cached filtered parameters
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
					// WARNING: tableStartY=8 was determined empirically (2025-11).
					// Accounts for: app header(1) + status(1) + title(1) + table header(1) + separator(1) + blank lines
					// If mouse clicks select wrong rows, adjust this offset.
					tableStartY := 8
					clickedRow := msg.Y - tableStartY
					if clickedRow >= 0 {
						params := v.getDisplayParams()
						newIdx := v.scrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(params) {
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
			// Re-apply filters if active
			if v.searchFilter != "" || v.categoryFilter != "" {
				v.applyFilters()
			}
			// Ensure selection is valid
			params := v.getDisplayParams()
			if v.selectedIdx >= len(params) {
				v.selectedIdx = max(0, len(params)-1)
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

	// Search mode
	if v.mode == ModeSearch {
		switch key {
		case "esc":
			v.mode = ModeNormal
			v.searchInput = ""
		case "enter":
			v.searchFilter = v.searchInput
			v.searchInput = ""
			v.mode = ModeNormal
			v.applyFilters()
			v.selectedIdx = 0
			v.scrollOffset = 0
		case "backspace":
			if len(v.searchInput) > 0 {
				v.searchInput = v.searchInput[:len(v.searchInput)-1]
			}
		default:
			// Only add printable characters
			if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
				v.searchInput += key
			}
		}
		return nil
	}

	// Category filter mode
	if v.mode == ModeCategoryFilter {
		categories := v.getUniqueCategories()
		switch key {
		case "esc":
			v.mode = ModeNormal
		case "enter":
			if v.categoryIdx >= 0 && v.categoryIdx < len(categories) {
				if v.categoryIdx == 0 {
					v.categoryFilter = "" // "All Categories" option
				} else {
					v.categoryFilter = categories[v.categoryIdx]
				}
				v.applyFilters()
				v.selectedIdx = 0
				v.scrollOffset = 0
			}
			v.mode = ModeNormal
		case "j", "down":
			if v.categoryIdx < len(categories)-1 {
				v.categoryIdx++
			}
		case "k", "up":
			if v.categoryIdx > 0 {
				v.categoryIdx--
			}
		}
		return nil
	}

	// Normal mode
	switch key {
	case "h":
		v.mode = ModeHelp

	// Search and filter
	case "/":
		v.mode = ModeSearch
		v.searchInput = ""
	case "c":
		v.mode = ModeCategoryFilter
		v.categoryIdx = 0
	case "esc":
		// Clear active filters
		if v.searchFilter != "" || v.categoryFilter != "" {
			v.searchFilter = ""
			v.categoryFilter = ""
			v.applyFilters()
			v.selectedIdx = 0
			v.scrollOffset = 0
		}

	// Navigation
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g", "home":
		v.selectedIdx = 0
		v.ensureVisible()
	case "G", "end":
		params := v.getDisplayParams()
		if len(params) > 0 {
			v.selectedIdx = len(params) - 1
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
	params := v.getDisplayParams()
	if len(params) == 0 {
		return
	}
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(params) {
		v.selectedIdx = len(params) - 1
	}
	v.ensureVisible()
}

// getDisplayParams returns the parameters to display (filtered if filters active).
func (v *ConfigView) getDisplayParams() []models.Parameter {
	if v.searchFilter != "" || v.categoryFilter != "" {
		return v.filteredParams
	}
	if v.data == nil {
		return nil
	}
	return v.data.Parameters
}

// applyFilters applies current search and category filters.
func (v *ConfigView) applyFilters() {
	if v.data == nil {
		v.filteredParams = nil
		return
	}

	// Start with all parameters
	result := v.data.Parameters

	// Apply category filter first
	if v.categoryFilter != "" {
		result = v.data.FilterByCategory(v.categoryFilter)
	}

	// Apply search filter
	if v.searchFilter != "" {
		// Create a temporary ConfigData to use FilterBySearch
		temp := &models.ConfigData{Parameters: result}
		result = temp.FilterBySearch(v.searchFilter)
	}

	v.filteredParams = result
}

// getUniqueCategories returns unique top-level categories with "All Categories" first.
func (v *ConfigView) getUniqueCategories() []string {
	categories := []string{"All Categories"}
	if v.data == nil {
		return categories
	}

	// Use a map to track unique categories
	seen := make(map[string]bool)
	for _, p := range v.data.Parameters {
		cat := p.TopLevelCategory()
		if cat != "" && !seen[cat] {
			seen[cat] = true
			categories = append(categories, cat)
		}
	}

	// Sort categories (except "All Categories" which stays first)
	if len(categories) > 1 {
		sort.Strings(categories[1:])
	}

	return categories
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
	// Height minus: app header(1) + status bar(1) + title(1) + table header(1) + separator(1) + footer(1)
	rows := v.height - 7
	// Account for search input line when in search mode
	if v.mode == ModeSearch {
		rows--
	}
	return max(1, rows)
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

	// Category filter overlay
	if v.mode == ModeCategoryFilter {
		return v.renderCategoryFilter()
	}

	var b strings.Builder

	// Status bar
	b.WriteString(v.renderStatusBar())
	b.WriteString("\n")

	// Title with filter status
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	title := "Configuration"
	if v.searchFilter != "" || v.categoryFilter != "" {
		filterInfo := v.getFilterStatusText()
		title += " " + styles.WarningStyle.Render(filterInfo)
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	// Search input (shown in search mode)
	if v.mode == ModeSearch {
		b.WriteString(v.renderSearchInput())
		b.WriteString("\n")
	}

	// Table
	b.WriteString(v.renderTable())

	// Footer
	b.WriteString("\n")
	b.WriteString(v.renderFooter())

	return b.String()
}

// getFilterStatusText returns a text description of active filters.
func (v *ConfigView) getFilterStatusText() string {
	var parts []string
	if v.searchFilter != "" {
		parts = append(parts, fmt.Sprintf("[search: %s]", v.searchFilter))
	}
	if v.categoryFilter != "" {
		parts = append(parts, fmt.Sprintf("[category: %s]", v.categoryFilter))
	}
	return strings.Join(parts, " ")
}

// renderSearchInput renders the search input prompt.
func (v *ConfigView) renderSearchInput() string {
	prompt := styles.AccentStyle.Render("Search: ")
	input := v.searchInput + "_"
	return prompt + input
}

// renderCategoryFilter renders the category filter overlay.
func (v *ConfigView) renderCategoryFilter() string {
	categories := v.getUniqueCategories()

	var b strings.Builder
	b.WriteString(styles.AccentStyle.Render("Select Category"))
	b.WriteString("\n\n")

	for i, cat := range categories {
		if i == v.categoryIdx {
			b.WriteString(styles.TableSelectedStyle.Render("> " + cat))
		} else {
			b.WriteString("  " + cat)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.MutedStyle.Render("[j/k]nav [enter]select [esc]cancel"))

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(b.String())
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

	// Get filtered parameters
	params := v.getDisplayParams()

	// Show "No results found" when filters match zero parameters
	if len(params) == 0 {
		msg := "No results found"
		if v.searchFilter != "" {
			msg += fmt.Sprintf(" for search: %q", v.searchFilter)
		}
		if v.categoryFilter != "" {
			msg += fmt.Sprintf(" in category: %q", v.categoryFilter)
		}
		msg += "\n\nPress [esc] to clear filters"
		return styles.MutedStyle.Render(msg)
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

	header := fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %-*s",
		nameWidth, "Name"+sortIndicator(SortByName),
		valueWidth, "Value",
		unitWidth, "Unit",
		categoryWidth, "Category"+sortIndicator(SortByCategory),
		descWidth, "Description")
	lines = append(lines, styles.TableHeaderStyle.Render(header))

	// Separator - must match header width exactly
	sep := strings.Repeat("─", nameWidth) + "─┼─" +
		strings.Repeat("─", valueWidth) + "─┼─" +
		strings.Repeat("─", unitWidth) + "─┼─" +
		strings.Repeat("─", categoryWidth) + "─┼─" +
		strings.Repeat("─", descWidth)
	lines = append(lines, styles.BorderStyle.Render(sep))

	// Rows
	visible := v.visibleRows()
	endIdx := v.scrollOffset + visible
	if endIdx > len(params) {
		endIdx = len(params)
	}

	// Yellow style for modified parameters
	modifiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // Yellow

	for i := v.scrollOffset; i < endIdx; i++ {
		p := params[i]

		name := truncate(p.Name, nameWidth)
		value := truncate(p.Setting, valueWidth)
		unit := truncate(p.Unit, unitWidth)
		category := truncate(p.TopLevelCategory(), categoryWidth)
		desc := truncate(p.ShortDesc, descWidth)

		row := fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %-*s",
			nameWidth, name,
			valueWidth, value,
			unitWidth, unit,
			categoryWidth, category,
			descWidth, desc)

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
	// Build hints based on current filter state
	var hintParts []string
	hintParts = append(hintParts, "[j/k]nav", "[s/S]ort", "[/]search", "[c]ategory")
	if v.searchFilter != "" || v.categoryFilter != "" {
		hintParts = append(hintParts, "[esc]clear")
	}
	hintParts = append(hintParts, "[h]elp")
	hints := styles.FooterHintStyle.Render(strings.Join(hintParts, " "))

	arrow := "↓"
	if v.sortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)

	// Use filtered params for count
	params := v.getDisplayParams()
	totalParams := 0
	if v.data != nil {
		totalParams = len(v.data.Parameters)
	}
	count := fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(params)), len(params))
	if len(params) != totalParams {
		count += fmt.Sprintf(" (of %d)", totalParams)
	}
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

SEARCH & FILTER
  /              Enter search mode (filter by name/description)
  c              Show category filter menu
  Esc            Clear active filters

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
