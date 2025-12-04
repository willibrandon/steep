// Package config provides the Configuration Viewer view.
package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ConfigDataMsg contains refreshed configuration data.
type ConfigDataMsg struct {
	Data  *models.ConfigData
	Error error
}

// RefreshConfigMsg triggers a data refresh.
type RefreshConfigMsg struct{}

// ExportConfigResultMsg contains the result of a config export operation.
type ExportConfigResultMsg struct {
	Filename string
	Count    int
	Success  bool
	Error    error
}

// ConfigMode represents the current interaction mode.
type ConfigMode int

const (
	ModeNormal ConfigMode = iota
	ModeHelp
	ModeSearch
	ModeCategoryFilter
	ModeDetail
	ModeCommand
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
	searchInput    textinput.Model    // Search input with cursor/paste support
	searchFilter   string             // Active search filter (applied on Enter)
	categoryFilter string             // Active category filter
	categoryIdx    int                // Selected index in category list
	filteredParams []models.Parameter // Cached filtered parameters

	// Detail view state
	detailScrollOffset int // Scroll offset for detail view

	// Command mode state
	commandInput textinput.Model // Command input with cursor/paste support
	toastMessage string          // Toast message to display
	toastIsError bool            // Whether toast is an error message
	toastTime    time.Time       // When toast was shown

	// Read-only mode
	readOnly bool // If true, write operations are blocked

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewConfigView creates a new configuration view.
func NewConfigView() *ConfigView {
	// Create search input
	si := textinput.New()
	si.CharLimit = 256

	// Create command input
	ci := textinput.New()
	ci.CharLimit = 256

	return &ConfigView{
		mode:         ModeNormal,
		data:         models.NewConfigData(),
		sortColumn:   SortByName,
		sortAsc:      true,
		clipboard:    ui.NewClipboardWriter(),
		searchInput:  si,
		commandInput: ci,
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

// SetReadOnly sets the read-only mode.
func (v *ConfigView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
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
					// Accounts for: status bar(3) + title(1) + table header(1) + separator(2) = 7 lines
					// If mouse clicks select wrong rows, adjust this offset.
					tableStartY := 7
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

	case ExportConfigResultMsg:
		if msg.Success {
			v.toastMessage = fmt.Sprintf("Exported %d parameters to %s", msg.Count, msg.Filename)
			v.toastIsError = false
		} else {
			v.toastMessage = fmt.Sprintf("Export failed: %v", msg.Error)
			v.toastIsError = true
		}
		v.toastTime = time.Now()

	case ui.SetConfigResultMsg:
		if msg.Success {
			// Show context-aware success message
			switch msg.Context {
			case "postmaster":
				v.toastMessage = fmt.Sprintf("Set %s = %s (restart required to apply)", msg.Parameter, msg.Value)
			case "sighup":
				v.toastMessage = fmt.Sprintf("Set %s = %s (use :reload to apply)", msg.Parameter, msg.Value)
			default:
				v.toastMessage = fmt.Sprintf("Set %s = %s", msg.Parameter, msg.Value)
			}
			v.toastIsError = false
		} else {
			v.toastMessage = fmt.Sprintf("Failed to set %s: %v", msg.Parameter, msg.Error)
			v.toastIsError = true
		}
		v.toastTime = time.Now()

	case ui.ResetConfigResultMsg:
		if msg.Success {
			// Show context-aware success message
			switch msg.Context {
			case "postmaster":
				v.toastMessage = fmt.Sprintf("Reset %s to default (restart required to apply)", msg.Parameter)
			case "sighup":
				v.toastMessage = fmt.Sprintf("Reset %s to default (use :reload to apply)", msg.Parameter)
			default:
				v.toastMessage = fmt.Sprintf("Reset %s to default", msg.Parameter)
			}
			v.toastIsError = false
		} else {
			v.toastMessage = fmt.Sprintf("Failed to reset %s: %v", msg.Parameter, msg.Error)
			v.toastIsError = true
		}
		v.toastTime = time.Now()

	case ui.ReloadConfigResultMsg:
		if msg.Success {
			v.toastMessage = "Configuration reloaded successfully"
			v.toastIsError = false
		} else {
			v.toastMessage = fmt.Sprintf("Failed to reload config: %v", msg.Error)
			v.toastIsError = true
		}
		v.toastTime = time.Now()
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
		switch msg.Type {
		case tea.KeyEsc:
			v.mode = ModeNormal
			v.searchInput.Reset()
		case tea.KeyEnter:
			v.searchFilter = v.searchInput.Value()
			v.searchInput.Reset()
			v.mode = ModeNormal
			v.applyFilters()
			v.selectedIdx = 0
			v.scrollOffset = 0
		default:
			// Delegate to textinput for typing, paste, cursor movement
			var cmd tea.Cmd
			v.searchInput, cmd = v.searchInput.Update(msg)
			return cmd
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

	// Detail mode
	if v.mode == ModeDetail {
		switch key {
		case "esc", "q", "d":
			v.mode = ModeNormal
			v.detailScrollOffset = 0
		case "j", "down":
			v.detailScrollOffset++
		case "k", "up":
			if v.detailScrollOffset > 0 {
				v.detailScrollOffset--
			}
		case "g", "home":
			v.detailScrollOffset = 0
		case "G", "end":
			v.detailScrollOffset = 999 // Will be clamped in renderDetailView
		}
		return nil
	}

	// Command mode
	if v.mode == ModeCommand {
		switch msg.Type {
		case tea.KeyEsc:
			v.mode = ModeNormal
			v.commandInput.Reset()
		case tea.KeyEnter:
			cmd := v.commandInput.Value()
			v.commandInput.Reset()
			v.mode = ModeNormal
			return v.executeCommand(cmd)
		default:
			// Delegate to textinput for typing, paste, cursor movement
			var cmd tea.Cmd
			v.commandInput, cmd = v.commandInput.Update(msg)
			return cmd
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
		v.searchInput.Reset()
		v.searchInput.Focus()
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

	// Detail view
	case "d", "enter":
		params := v.getDisplayParams()
		if len(params) > 0 {
			v.mode = ModeDetail
			v.detailScrollOffset = 0
		}

	// Command mode
	case ":":
		v.mode = ModeCommand
		v.commandInput.Reset()
		v.commandInput.Focus()
		v.toastMessage = "" // Clear any existing toast

	// Manual refresh
	case "r":
		v.refreshing = true
		return func() tea.Msg {
			return RefreshConfigMsg{}
		}

	// Clipboard copy
	case "y":
		// Copy parameter name
		params := v.getDisplayParams()
		if len(params) > 0 && v.selectedIdx < len(params) {
			p := params[v.selectedIdx]
			if v.clipboard.IsAvailable() {
				v.clipboard.Write(p.Name)
				v.toastMessage = fmt.Sprintf("Copied name: %s", p.Name)
				v.toastIsError = false
				v.toastTime = time.Now()
			} else {
				v.toastMessage = "Clipboard not available"
				v.toastIsError = true
				v.toastTime = time.Now()
			}
		}
	case "Y":
		// Copy parameter value
		params := v.getDisplayParams()
		if len(params) > 0 && v.selectedIdx < len(params) {
			p := params[v.selectedIdx]
			if v.clipboard.IsAvailable() {
				v.clipboard.Write(p.Setting)
				v.toastMessage = fmt.Sprintf("Copied value: %s", p.Setting)
				v.toastIsError = false
				v.toastTime = time.Now()
			} else {
				v.toastMessage = "Clipboard not available"
				v.toastIsError = true
				v.toastTime = time.Now()
			}
		}
	}

	return nil
}

// executeCommand executes a command entered in command mode.
func (v *ConfigView) executeCommand(cmd string) tea.Cmd {
	// Try parsing as export command (read-only safe)
	if exportCmd := parseExportCommand(cmd); exportCmd != nil {
		// Export currently filtered parameters
		params := v.getDisplayParams()
		return func() tea.Msg {
			count, err := exportConfig(exportCmd.Filename, params, v.connectionInfo)
			return ExportConfigResultMsg{
				Filename: exportCmd.Filename,
				Count:    count,
				Success:  err == nil,
				Error:    err,
			}
		}
	}

	// Try parsing as set command (requires write access)
	if setCmd := parseSetCommand(cmd); setCmd != nil {
		if v.readOnly {
			v.toastMessage = "Cannot modify config in read-only mode"
			v.toastIsError = true
			v.toastTime = time.Now()
			return nil
		}

		// Look up parameter to get its context
		context := v.getParameterContext(setCmd.Parameter)
		if context == "" {
			v.toastMessage = fmt.Sprintf("Unknown parameter: %s", setCmd.Parameter)
			v.toastIsError = true
			v.toastTime = time.Now()
			return nil
		}

		return func() tea.Msg {
			return ui.SetConfigMsg{
				Parameter: setCmd.Parameter,
				Value:     setCmd.Value,
				Context:   context,
			}
		}
	}

	// Try parsing as reset command (requires write access)
	if resetCmd := parseResetCommand(cmd); resetCmd != nil {
		if v.readOnly {
			v.toastMessage = "Cannot modify config in read-only mode"
			v.toastIsError = true
			v.toastTime = time.Now()
			return nil
		}

		// Look up parameter to get its context
		context := v.getParameterContext(resetCmd.Parameter)
		if context == "" {
			v.toastMessage = fmt.Sprintf("Unknown parameter: %s", resetCmd.Parameter)
			v.toastIsError = true
			v.toastTime = time.Now()
			return nil
		}

		return func() tea.Msg {
			return ui.ResetConfigMsg{
				Parameter: resetCmd.Parameter,
				Context:   context,
			}
		}
	}

	// Try parsing as reload command (requires write access)
	if parseReloadCommand(cmd) {
		if v.readOnly {
			v.toastMessage = "Cannot reload config in read-only mode"
			v.toastIsError = true
			v.toastTime = time.Now()
			return nil
		}

		return func() tea.Msg {
			return ui.ReloadConfigMsg{}
		}
	}

	// Unknown command
	v.toastMessage = fmt.Sprintf("Unknown command: %s", cmd)
	v.toastIsError = true
	v.toastTime = time.Now()
	return nil
}

// getParameterContext looks up a parameter by name and returns its context.
// Returns empty string if parameter not found.
func (v *ConfigView) getParameterContext(name string) string {
	if v.data == nil {
		return ""
	}
	for _, p := range v.data.Parameters {
		if strings.EqualFold(p.Name, name) {
			return p.Context
		}
	}
	return ""
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
	// Height minus: status bar(3 with border) + title(1) + table header(1) + separator(1) + footer(3 with border) + 1 spacing
	rows := v.height - 10
	// Account for search input line when in search mode
	if v.mode == ModeSearch {
		rows--
	}
	// Account for command input line when in command mode
	if v.mode == ModeCommand {
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

	// Detail view overlay
	if v.mode == ModeDetail {
		return v.renderDetailView()
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
	return prompt + v.searchInput.View()
}

// renderCommandInput renders the command input prompt.
func (v *ConfigView) renderCommandInput() string {
	prompt := styles.AccentStyle.Render(":")
	return prompt + v.commandInput.View()
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

// renderDetailView renders the parameter detail overlay.
func (v *ConfigView) renderDetailView() string {
	params := v.getDisplayParams()
	if len(params) == 0 || v.selectedIdx >= len(params) {
		return styles.MutedStyle.Render("No parameter selected")
	}

	p := params[v.selectedIdx]
	var lines []string

	// Title
	lines = append(lines, styles.AccentStyle.Render("Parameter Details"))
	lines = append(lines, "")

	// Name (bold/highlight)
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	lines = append(lines, nameStyle.Render(p.Name))
	lines = append(lines, "")

	// Current value with unit
	valueStr := p.Setting
	if p.Unit != "" {
		valueStr += " " + p.Unit
	}
	lines = append(lines, fmt.Sprintf("  %-16s %s", "Value:", valueStr))

	// Type
	lines = append(lines, fmt.Sprintf("  %-16s %s", "Type:", p.VarType))

	// Context with explanation
	contextExplanation := v.getContextExplanation(p.Context)
	contextStyle := v.getContextStyle(p.Context)
	lines = append(lines, fmt.Sprintf("  %-16s %s", "Context:", contextStyle.Render(contextExplanation)))

	// Category
	lines = append(lines, fmt.Sprintf("  %-16s %s", "Category:", p.Category))

	// Source
	lines = append(lines, fmt.Sprintf("  %-16s %s", "Source:", p.Source))

	// Constraints (based on type)
	constraints := v.formatConstraints(p)
	if constraints != "" {
		lines = append(lines, fmt.Sprintf("  %-16s %s", "Constraints:", constraints))
	}

	// Default value
	if p.BootVal != "" {
		defaultStr := p.BootVal
		if p.Unit != "" {
			defaultStr += " " + p.Unit
		}
		lines = append(lines, fmt.Sprintf("  %-16s %s", "Default:", defaultStr))
	}

	// Reset value (if different from current)
	if p.ResetVal != "" && p.ResetVal != p.Setting {
		resetStr := p.ResetVal
		if p.Unit != "" {
			resetStr += " " + p.Unit
		}
		lines = append(lines, fmt.Sprintf("  %-16s %s", "Reset Value:", resetStr))
	}

	// Source file and line
	if p.SourceFile != "" {
		sourceLocation := p.SourceFile
		if p.SourceLine > 0 {
			sourceLocation += fmt.Sprintf(":%d", p.SourceLine)
		}
		lines = append(lines, fmt.Sprintf("  %-16s %s", "Config File:", sourceLocation))
	}

	// Pending restart warning
	if p.PendingRestart {
		lines = append(lines, "")
		lines = append(lines, styles.ErrorStyle.Render("  ⚠ Pending restart required for this change to take effect"))
	}

	// Modified indicator
	if p.IsModified() {
		lines = append(lines, "")
		lines = append(lines, styles.WarningStyle.Render("  ● Modified from default"))
	}

	// Description section
	lines = append(lines, "")
	lines = append(lines, styles.AccentStyle.Render("Description"))
	lines = append(lines, "")

	// Wrap short description
	descLines := v.wrapText(p.ShortDesc, v.width-12)
	for _, dl := range descLines {
		lines = append(lines, "  "+dl)
	}

	// Extra description if available
	if p.ExtraDesc != "" {
		lines = append(lines, "")
		extraLines := v.wrapText(p.ExtraDesc, v.width-12)
		for _, el := range extraLines {
			lines = append(lines, "  "+styles.MutedStyle.Render(el))
		}
	}

	// Footer hints
	lines = append(lines, "")
	lines = append(lines, styles.MutedStyle.Render("[esc/q]back [j/k]scroll"))

	// Apply scroll offset
	contentHeight := v.height - 9 // padding + borders
	totalLines := len(lines)

	// Limit scroll offset
	maxScroll := max(0, totalLines-contentHeight)
	if v.detailScrollOffset > maxScroll {
		v.detailScrollOffset = maxScroll
	}
	if v.detailScrollOffset < 0 {
		v.detailScrollOffset = 0
	}

	// Get visible lines
	startLine := v.detailScrollOffset
	endLine := min(startLine+contentHeight, totalLines)
	visibleLines := lines[startLine:endLine]

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(strings.Join(visibleLines, "\n"))
}

// getContextExplanation returns a human-readable explanation for the context value.
func (v *ConfigView) getContextExplanation(context string) string {
	switch context {
	case "internal":
		return "Read-only (internal)"
	case "postmaster":
		return "Restart Required"
	case "sighup":
		return "Reload Required"
	case "backend":
		return "New Connections Only"
	case "superuser":
		return "Superuser Session"
	case "user":
		return "User Session"
	default:
		return context
	}
}

// getContextStyle returns the appropriate style for the context.
func (v *ConfigView) getContextStyle(context string) lipgloss.Style {
	switch context {
	case "internal":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray
	case "postmaster":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	case "sighup":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // Yellow
	case "backend":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("51")) // Cyan
	case "superuser":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("33")) // Blue
	case "user":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")) // Green
	default:
		return lipgloss.NewStyle()
	}
}

// formatConstraints returns a formatted string of constraints based on parameter type.
func (v *ConfigView) formatConstraints(p models.Parameter) string {
	switch p.VarType {
	case "integer", "real":
		if p.MinVal != "" && p.MaxVal != "" {
			return fmt.Sprintf("%s to %s", p.MinVal, p.MaxVal)
		} else if p.MinVal != "" {
			return fmt.Sprintf("min: %s", p.MinVal)
		} else if p.MaxVal != "" {
			return fmt.Sprintf("max: %s", p.MaxVal)
		}
		return ""
	case "enum":
		if len(p.EnumVals) > 0 {
			return strings.Join(p.EnumVals, ", ")
		}
		return ""
	case "bool":
		return "on, off"
	case "string":
		return "" // No constraints for strings
	default:
		return ""
	}
}

// wrapText wraps text to the specified width.
func (v *ConfigView) wrapText(text string, width int) []string {
	if width <= 0 {
		width = 60
	}

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return lines
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
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

	// Column widths - adapt for small terminals (80x24 minimum)
	nameWidth := 30
	valueWidth := 20
	unitWidth := 6
	categoryWidth := 25
	descWidth := 20

	// For narrow terminals, reduce column widths proportionally
	if v.width < 120 {
		// Compact mode for 80-column terminals
		nameWidth = 25
		valueWidth = 15
		unitWidth = 5
		categoryWidth = 0                                             // Hide category in narrow mode
		descWidth = v.width - nameWidth - valueWidth - unitWidth - 12 // 3 separators
		if descWidth < 10 {
			descWidth = 10
		}
	} else {
		// Standard mode
		descWidth = v.width - nameWidth - valueWidth - unitWidth - categoryWidth - 16 // 4 separators
		if descWidth < 10 {
			descWidth = 10
		}
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

	var header, sep string
	if categoryWidth > 0 {
		// Full width mode with category column
		header = fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %-*s",
			nameWidth, "Name"+sortIndicator(SortByName),
			valueWidth, "Value",
			unitWidth, "Unit",
			categoryWidth, "Category"+sortIndicator(SortByCategory),
			descWidth, "Description")
		sep = strings.Repeat("─", nameWidth) + "─┼─" +
			strings.Repeat("─", valueWidth) + "─┼─" +
			strings.Repeat("─", unitWidth) + "─┼─" +
			strings.Repeat("─", categoryWidth) + "─┼─" +
			strings.Repeat("─", descWidth)
	} else {
		// Compact mode without category column
		header = fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s",
			nameWidth, "Name"+sortIndicator(SortByName),
			valueWidth, "Value",
			unitWidth, "Unit",
			descWidth, "Description")
		sep = strings.Repeat("─", nameWidth) + "─┼─" +
			strings.Repeat("─", valueWidth) + "─┼─" +
			strings.Repeat("─", unitWidth) + "─┼─" +
			strings.Repeat("─", descWidth)
	}
	lines = append(lines, styles.TableHeaderStyle.Render(header))
	lines = append(lines, styles.BorderStyle.Render(sep))

	// Rows
	visible := v.visibleRows()
	endIdx := v.scrollOffset + visible
	if endIdx > len(params) {
		endIdx = len(params)
	}

	// Yellow style for modified parameters
	modifiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // Yellow
	// Red style for pending restart parameters
	pendingRestartStyle := lipgloss.NewStyle().Foreground(styles.ColorCriticalFg)

	for i := v.scrollOffset; i < endIdx; i++ {
		p := params[i]

		// Add pending restart indicator to name
		name := p.Name
		if p.PendingRestart {
			name = "! " + name // Prefix with warning indicator
		}
		name = truncate(name, nameWidth)
		value := truncate(p.Setting, valueWidth)
		unit := truncate(p.Unit, unitWidth)
		desc := truncate(p.ShortDesc, descWidth)

		var row string
		if categoryWidth > 0 {
			category := truncate(p.TopLevelCategory(), categoryWidth)
			row = fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s │ %-*s",
				nameWidth, name,
				valueWidth, value,
				unitWidth, unit,
				categoryWidth, category,
				descWidth, desc)
		} else {
			row = fmt.Sprintf("%-*s │ %-*s │ %-*s │ %-*s",
				nameWidth, name,
				valueWidth, value,
				unitWidth, unit,
				descWidth, desc)
		}

		// Apply styles - pending restart takes priority over modified
		if i == v.selectedIdx {
			row = styles.TableSelectedStyle.Render(row)
		} else if p.PendingRestart {
			row = pendingRestartStyle.Render(row)
		} else if p.IsModified() {
			row = modifiedStyle.Render(row)
		}

		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

// renderFooter renders the footer with key hints.
func (v *ConfigView) renderFooter() string {
	var hints string

	// Show search/command input when in those modes
	if v.mode == ModeSearch {
		hints = v.renderSearchInput()
	} else if v.mode == ModeCommand {
		hints = v.renderCommandInput()
	} else if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		// Show toast message if recent (within 3 seconds)
		toastStyle := styles.FooterHintStyle
		if v.toastIsError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		// Build hints based on current filter state
		var hintParts []string
		hintParts = append(hintParts, "[j/k]nav", "[s/S]ort", "[d]etails", "[/]search", "[c]ategory", "[:]cmd", "[r]efresh", "[y]ank")
		if v.searchFilter != "" || v.categoryFilter != "" {
			hintParts = append(hintParts, "[esc]clear")
		}
		hintParts = append(hintParts, "[h]elp")
		hints = styles.FooterHintStyle.Render(strings.Join(hintParts, " "))
	}

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

DETAILS
  d or Enter     View parameter details
  (in detail view: j/k to scroll, esc/q/d to return)

CLIPBOARD
  y              Copy parameter name
  Y              Copy parameter value

COMMANDS
  :              Enter command mode
  :export config <file>  Export parameters to file
  :set <param> <value>   Change parameter (ALTER SYSTEM)
  :reset <param>         Reset parameter to default
  :reload                Reload configuration (pg_reload_conf)

OTHER
  r              Refresh configuration data
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
