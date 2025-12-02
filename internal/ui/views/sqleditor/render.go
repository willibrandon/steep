package sqleditor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// fixedOverhead returns the height consumed by fixed UI elements.
// Connection bar (2 lines) + title (1 line) + footer (2 lines) = 5.
func (v *SQLEditorView) fixedOverhead() int {
	return 5
}

// resultsHeight returns the height available for results.
func (v *SQLEditorView) resultsHeight() int {
	// Use the same editorHeight calculated in SetSize for consistency
	return v.height - v.editorHeight - v.fixedOverhead()
}

// showToast displays a toast message.
func (v *SQLEditorView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// View renders the SQL Editor view.
func (v *SQLEditorView) View() string {
	if v.width == 0 || v.height == 0 {
		return "Initializing..."
	}

	// Help overlay
	if v.showHelp {
		return v.renderHelp()
	}

	// Search overlay (Ctrl+R)
	if v.searchMode {
		return v.renderSearchOverlay()
	}

	// Snippet browser overlay (Ctrl+O)
	if v.snippetBrowsing {
		return v.renderSnippetBrowser()
	}

	var sections []string

	// Connection info at the top (below app title bar)
	connectionBar := v.renderConnectionBar()
	sections = append(sections, connectionBar)

	// Title (styled like other views)
	title := v.renderTitle()
	sections = append(sections, title)

	// Calculate editorContentStartY: connection bar + title
	v.editorContentStartY = lipgloss.Height(connectionBar) + lipgloss.Height(title)

	// Editor section
	editorSection := v.renderEditor()
	sections = append(sections, editorSection)

	// Store actual rendered editor height for mouse coordinate translation
	v.editorSectionHeight = lipgloss.Height(editorSection)

	// Results section
	resultsSection := v.renderResults()
	sections = append(sections, resultsSection)

	// Calculate resultsDataStartY: everything before results data rows
	// This includes: connection bar + title + editor section + results title + header + separator
	// Use actual rendered heights to ensure mouse coordinate translation matches display
	v.resultsDataStartY = v.editorContentStartY + v.editorSectionHeight + v.resultsHeaderHeight

	// Footer with key hints
	sections = append(sections, v.renderFooter())

	// Use strings.Join instead of lipgloss.JoinVertical for performance
	return strings.Join(sections, "\n")
}

// renderTitle renders the view title (styled like other views).
func (v *SQLEditorView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)

	// Reserve space for focus indicator so title doesn't shift
	var prefix string
	if v.focus == FocusEditor {
		prefix = styles.AccentStyle.Render("● ")
	} else {
		prefix = "  "
	}

	return prefix + titleStyle.Render("SQL Editor")
}

// renderEditor renders the SQL editor with vimtea.
func (v *SQLEditorView) renderEditor() string {
	// Vimtea editor view (includes its own status bar with mode/command line)
	editorView := v.editor.View()

	// Editor content
	content := editorView

	// Use v.editorHeight set in SetSize for consistent height
	return lipgloss.NewStyle().
		Height(v.editorHeight).
		MaxHeight(v.editorHeight).
		Render(content)
}

// renderResults renders the query results table.
func (v *SQLEditorView) renderResults() string {
	// Title bar (known to be 1 line)
	title := "Results"
	if v.focus == FocusResults {
		title = styles.AccentStyle.Render("● ") + title
	} else {
		title = "  " + title
	}

	if v.executing {
		elapsed := time.Since(v.startTime)
		title += fmt.Sprintf(" - Executing... (%s)", elapsed.Truncate(time.Millisecond))
	} else if v.results != nil && v.results.TotalRows > 0 {
		title += fmt.Sprintf(" - %d rows (%dms)", v.results.TotalRows, v.results.ExecutionMs)
	}

	titleBar := styles.TitleStyle.Render(title)

	// Calculate resultsHeaderHeight for mouse coordinate translation
	// Title bar (1) + content header (scroll info + header with border + separator)
	contentHeaderHeight := 0
	if v.results != nil && v.results.TotalRows > 0 && len(v.results.Columns) > 0 {
		// Results table has: optional scroll info + header row (with bottom border = 2 lines) + separator
		// ResultsHeaderStyle has BorderBottom(true) which adds an extra line
		contentHeaderHeight = 3 // header row (1) + header border (1) + separator (1)
		if len(v.results.Columns) > 1 {
			contentHeaderHeight++ // scroll info line ("Cols X-Y of Z")
		}
	}
	v.resultsHeaderHeight = 1 + contentHeaderHeight // titleBar is always 1 line

	// Content - renderResultsTable already produces exact number of lines needed
	var content string
	if v.executing {
		content = styles.ExecutingStyle.Render("Executing query...")
	} else if v.lastError != nil {
		content = v.renderError()
	} else if v.results == nil || v.results.TotalRows == 0 {
		if v.executedQuery != "" {
			if v.results != nil {
				content = styles.MutedStyle.Render(fmt.Sprintf("Query returned 0 rows (%dms).", v.results.ExecutionMs))
			} else {
				content = styles.MutedStyle.Render("Query returned 0 rows.")
			}
		} else {
			content = styles.MutedStyle.Render("No results. Execute a query with F5.")
		}
	} else {
		content = v.renderResultsTable()
	}

	// Pagination footer
	hasMultiplePages := v.results != nil && v.results.TotalPages() > 1

	// Simple string concatenation instead of lipgloss.JoinVertical (much faster)
	if hasMultiplePages {
		footer := styles.PaginationStyle.Render(
			fmt.Sprintf("Page %d/%d (n/p to navigate)",
				v.results.CurrentPage, v.results.TotalPages()))
		return titleBar + "\n" + content + "\n" + footer
	}
	return titleBar + "\n" + content
}

// renderResultsTable renders the results as a table.
func (v *SQLEditorView) renderResultsTable() string {
	if v.results == nil || len(v.results.Columns) == 0 {
		return ""
	}

	var lines []string

	totalCols := len(v.results.Columns)

	// Use pre-calculated column widths (calculated once when results arrive)
	allColWidths := v.results.ColWidths
	if len(allColWidths) == 0 {
		// Fallback if not calculated (shouldn't happen, but safe)
		v.results.CalculateColWidths(32)
		allColWidths = v.results.ColWidths
	}

	// Apply horizontal scroll offset
	startCol := v.colScrollOffset
	if startCol >= totalCols {
		startCol = totalCols - 1
	}
	if startCol < 0 {
		startCol = 0
	}

	// Horizontal scroll indicator (always show column info)
	if totalCols > 1 {
		scrollInfo := fmt.Sprintf("Cols %d-%d of %d (←/→ to scroll)", startCol+1, totalCols, totalCols)
		lines = append(lines, styles.MutedStyle.Render(scrollInfo))
	}

	// Header with type indicators and sort indicator - only visible columns
	var headerParts []string
	for i := startCol; i < totalCols; i++ {
		col := v.results.Columns[i]
		headerText := col.Name
		if col.TypeName != "" {
			headerText = fmt.Sprintf("%s (%s)", col.Name, col.TypeName)
		}
		// Add sort indicator
		if v.results.SortColumn == i {
			if v.results.SortAsc {
				headerText += " ↑"
			} else {
				headerText += " ↓"
			}
		}
		headerParts = append(headerParts, padOrTruncate(headerText, allColWidths[i]))
	}
	header := styles.ResultsHeaderStyle.Render(strings.Join(headerParts, " │ "))
	lines = append(lines, header)

	// Separator
	var sepParts []string
	for i := startCol; i < totalCols; i++ {
		sepParts = append(sepParts, strings.Repeat("─", allColWidths[i]))
	}
	lines = append(lines, styles.BorderStyle.Render(strings.Join(sepParts, "─┼─")))

	// Calculate which rows to show based on pagination
	// For server-side pagination, v.results.Rows only contains current page's data (indices 0 to len-1)
	// For client-side pagination, v.results.Rows contains all data
	var pageStartRow, pageEndRow int
	if v.paginationBaseSQL != "" {
		// Server-side: rows array only has current page's data
		pageStartRow = 0
		pageEndRow = len(v.results.Rows)
	} else {
		// Client-side: rows array has all data, slice by page
		pageStartRow = (v.results.CurrentPage - 1) * v.results.PageSize
		pageEndRow = pageStartRow + v.results.PageSize
		if pageEndRow > len(v.results.Rows) {
			pageEndRow = len(v.results.Rows)
		}
	}

	// Apply vertical scroll within the current page
	visibleRows := v.visibleResultRows()
	startRow := pageStartRow + v.scrollOffset
	endRow := startRow + visibleRows
	if endRow > pageEndRow {
		endRow = pageEndRow
	}

	for i := startRow; i < endRow; i++ {
		row := v.results.Rows[i]
		// For server-side pagination, selectedRow is relative to current page (0-based)
		isRowSelected := i == v.selectedRow

		var rowParts []string
		for j := startCol; j < len(row); j++ {
			val := row[j]
			cellVal := padOrTruncate(val, allColWidths[j])

			if isRowSelected && j == v.selectedCol {
				// This is the selected cell - highlight it distinctly
				cellVal = styles.ResultsCellSelectedStyle.Render(cellVal)
			} else if isRowSelected {
				// Row is selected but not this cell
				cellVal = styles.ResultsRowSelectedStyle.Render(cellVal)
			} else if val == NullDisplayValue {
				cellVal = styles.ResultsNullStyle.Render(cellVal)
			}
			rowParts = append(rowParts, cellVal)
		}

		// Join with styled separators for selected row
		var rowStr string
		if isRowSelected {
			sep := styles.ResultsRowSelectedStyle.Render(" │ ")
			rowStr = strings.Join(rowParts, sep)
		} else {
			rowStr = strings.Join(rowParts, " │ ")
		}
		lines = append(lines, rowStr)
	}

	return strings.Join(lines, "\n")
}

// renderError renders the error message with position info.
func (v *SQLEditorView) renderError() string {
	if v.lastError == nil {
		return ""
	}

	var lines []string

	// If we have detailed error info, format it nicely
	if v.lastErrorInfo != nil {
		// Error header with severity and code
		header := v.lastErrorInfo.Severity
		if v.lastErrorInfo.Code != "" {
			header += fmt.Sprintf(" [%s]", v.lastErrorInfo.Code)
		}
		lines = append(lines, styles.ErrorStyle.Render(header))

		// Main error message
		lines = append(lines, styles.ErrorStyle.Render(v.lastErrorInfo.Message))

		// Position information
		if v.lastErrorInfo.Position > 0 && v.executedQuery != "" {
			line, col := positionToLineCol(v.executedQuery, v.lastErrorInfo.Position)
			lines = append(lines, styles.MutedStyle.Render(fmt.Sprintf("At line %d, column %d", line, col)))

			// Show the problematic line with an indicator
			lineText := getLineAtPosition(v.executedQuery, v.lastErrorInfo.Position)
			if lineText != "" {
				lines = append(lines, "")
				lines = append(lines, styles.MutedStyle.Render(lineText))
				// Add caret at the error position within the line
				offset := v.lastErrorInfo.Position - getLineStartOffset(v.executedQuery, v.lastErrorInfo.Position)
				if offset > 0 && offset <= len(lineText) {
					caret := strings.Repeat(" ", offset-1) + "^"
					lines = append(lines, styles.ErrorStyle.Render(caret))
				}
			}
		}

		// Detail message
		if v.lastErrorInfo.Detail != "" {
			lines = append(lines, "")
			lines = append(lines, styles.MutedStyle.Render("Detail: "+v.lastErrorInfo.Detail))
		}

		// Hint message
		if v.lastErrorInfo.Hint != "" {
			lines = append(lines, styles.SuccessStyle.Render("Hint: "+v.lastErrorInfo.Hint))
		}

		// Table/column/constraint info
		if v.lastErrorInfo.TableName != "" {
			info := "Table: " + v.lastErrorInfo.TableName
			if v.lastErrorInfo.SchemaName != "" {
				info = "Table: " + v.lastErrorInfo.SchemaName + "." + v.lastErrorInfo.TableName
			}
			lines = append(lines, styles.MutedStyle.Render(info))
		}
		if v.lastErrorInfo.ColumnName != "" {
			lines = append(lines, styles.MutedStyle.Render("Column: "+v.lastErrorInfo.ColumnName))
		}
		if v.lastErrorInfo.ConstraintName != "" {
			lines = append(lines, styles.MutedStyle.Render("Constraint: "+v.lastErrorInfo.ConstraintName))
		}

		// Context
		if v.lastErrorInfo.Where != "" {
			lines = append(lines, styles.MutedStyle.Render("Context: "+v.lastErrorInfo.Where))
		}
	} else {
		// Simple error without detailed info
		lines = append(lines, styles.ErrorStyle.Render(v.lastError.Error()))
	}

	return strings.Join(lines, "\n")
}

// renderConnectionBar renders the connection info bar at the top (matches other views).
func (v *SQLEditorView) renderConnectionBar() string {
	// Connection info title
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Build right-side indicators
	var indicators []string

	// Transaction indicator
	if v.executor != nil && v.executor.IsInTransaction() {
		txState := v.executor.TransactionState()
		if txState.StateType == TxAborted {
			indicators = append(indicators, styles.TransactionAbortedBadgeStyle.Render("TX ABORTED"))
		} else {
			indicators = append(indicators, styles.TransactionBadgeStyle.Render("TX"))
		}
	}

	// Read-only indicator
	if v.readOnly {
		indicators = append(indicators, styles.WarningStyle.Render("[READ-ONLY]"))
	}

	// Calculate spacing
	rightContent := strings.Join(indicators, " ")
	titleLen := lipgloss.Width(title)
	rightLen := lipgloss.Width(rightContent)
	gap := v.width - 2 - titleLen - rightLen
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + spaces + rightContent)
}

// renderFooter renders the bottom footer with key hints and toast messages.
func (v *SQLEditorView) renderFooter() string {
	var parts []string

	// Toast message (shows for 5 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 5*time.Second {
		if v.toastError {
			parts = append(parts, styles.ErrorStyle.Render(v.toastMessage))
		} else {
			parts = append(parts, styles.SuccessStyle.Render(v.toastMessage))
		}
	}

	// Key hints based on focus
	var hints string
	if v.focus == FocusEditor {
		hints = "F5: Execute │ \\: Results │ -/+: Resize │ H: Help"
	} else {
		hints = "\\: Editor │ hjkl: Nav │ y/Y: Copy │ s/S: Sort │ n/p: Page │ H: Help"
	}
	parts = append(parts, styles.MutedStyle.Render(hints))

	return strings.Join(parts, " │ ")
}

// helpKeyBinding represents a single key binding entry for help.
type helpKeyBinding struct {
	key  string
	desc string
}

// getHelpBindings returns all help key bindings for SQL Editor.
func getHelpBindings() []helpKeyBinding {
	return []helpKeyBinding{
		{"Focus Switching", ""},
		{"\\", "Toggle focus (editor / results)"},
		{"Enter", "From results: enter editor in insert mode"},
		{"", ""},
		{"Editor Mode", ""},
		{"F5", "Execute query"},
		{"Ctrl+Enter", "Execute query (insert mode)"},
		{"i/a/o", "Enter insert mode (vim-style)"},
		{"Esc", "Exit insert mode / switch to results"},
		{"", ""},
		{"History & Snippets", ""},
		{"Up/Down", "Navigate history (cursor at 0,0)"},
		{"Ctrl+R", "Search history (reverse search)"},
		{"Ctrl+O", "Open snippet browser"},
		{"", ""},
		{"Results Mode", ""},
		{"j/k", "Move selection down/up"},
		{"h/l", "Move selection left/right (cell)"},
		{"0/$", "First/last column"},
		{"g/G", "Go to first/last row"},
		{"Ctrl+d/u", "Page down/up (10 rows)"},
		{"Left/Right", "Scroll columns"},
		{"n/p", "Next/previous page (100 rows)"},
		{"s/S", "Cycle sort column / toggle direction"},
		{"y/Y", "Copy cell / copy row"},
		{"", ""},
		{"Resize Panes", ""},
		{"-/+", "Shrink/grow results pane"},
		{"Ctrl+Up/Down", "Resize panes (alternative)"},
		{"", ""},
		{"Commands", ""},
		{":exec", "Execute query"},
		{":save NAME", "Save query as snippet"},
		{":load NAME", "Load snippet into editor"},
		{":snippets", "Open snippet browser (Ctrl+O)"},
		{":export csv/json FILE", "Export results"},
		{":repl [pgcli|psql]", "Launch PostgreSQL REPL"},
		{":repl [sqlite|litecli|sqlite3]", "Launch SQLite REPL (steep.db)"},
		{":repl docker [pgcli|psql]", "Launch PostgreSQL REPL via Docker"},
		{":repl docker [sqlite|litecli|sqlite3]", "Launch SQLite REPL via Docker"},
		{"", ""},
		{"General", ""},
		{"1-9,0", "Switch views"},
		{"H", "Show this help"},
		{"q", "Quit application"},
	}
}

// renderHelp renders the scrollable help overlay centered on screen.
func (v *SQLEditorView) renderHelp() string {
	keyBindings := getHelpBindings()

	// Calculate max key width for alignment
	maxKeyWidth := 0
	for _, kb := range keyBindings {
		if len(kb.key) > maxKeyWidth {
			maxKeyWidth = len(kb.key)
		}
	}

	// Build all lines
	var allLines []string
	for _, kb := range keyBindings {
		if kb.key == "" && kb.desc == "" {
			allLines = append(allLines, "")
			continue
		}
		if kb.desc == "" {
			// Section header
			allLines = append(allLines, styles.HelpTitleStyle.Render(kb.key))
			continue
		}
		key := styles.HelpKeyStyle.Render(padRight(kb.key, maxKeyWidth+2))
		desc := styles.HelpDescStyle.Render(kb.desc)
		allLines = append(allLines, key+desc)
	}

	totalLines := len(allLines)

	// Calculate visible height (leave room for title, footer, borders, padding)
	visibleHeight := v.height - 10
	if visibleHeight < 5 {
		visibleHeight = 5
	}
	if visibleHeight > totalLines {
		visibleHeight = totalLines
	}

	// Clamp scroll offset
	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.helpScroll > maxScroll {
		v.helpScroll = maxScroll
	}
	if v.helpScroll < 0 {
		v.helpScroll = 0
	}

	// Get visible slice
	endIdx := v.helpScroll + visibleHeight
	if endIdx > totalLines {
		endIdx = totalLines
	}
	visibleLines := allLines[v.helpScroll:endIdx]

	// Build content
	var b strings.Builder

	// Title
	b.WriteString(styles.HelpTitleStyle.Render("SQL Editor Help"))
	b.WriteString("\n\n")

	// Visible lines
	b.WriteString(strings.Join(visibleLines, "\n"))

	// Footer with scroll indicator
	b.WriteString("\n\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	if maxScroll > 0 {
		scrollPct := float64(v.helpScroll) / float64(maxScroll) * 100
		b.WriteString(footerStyle.Render(fmt.Sprintf("[j/k] scroll  [H/q/Esc] close  (%.0f%%)", scrollPct)))
	} else {
		b.WriteString(footerStyle.Render("[H/q/Esc] close"))
	}

	// Wrap in dialog with border
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2)

	// Center on screen - this fills the entire screen area
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(b.String()),
	)
}

// padRight pads a string with spaces to the specified width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// renderSearchOverlay renders the Ctrl+R reverse search overlay.
func (v *SQLEditorView) renderSearchOverlay() string {
	var sb strings.Builder

	// Title
	title := styles.HeaderStyle.Render("History Search (Ctrl+R)")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Search input
	searchLine := fmt.Sprintf("Search: %s█", v.searchQuery)
	sb.WriteString(styles.MutedStyle.Render(searchLine))
	sb.WriteString("\n\n")

	// Show results count
	resultCount := len(v.searchResult)
	if resultCount == 0 {
		sb.WriteString(styles.MutedStyle.Render("No matching queries found"))
		sb.WriteString("\n")
	} else {
		sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("Found %d matching queries", resultCount)))
		sb.WriteString("\n\n")

		// Show visible results (up to 10) with scrolling
		maxVisible := 10
		if maxVisible > resultCount {
			maxVisible = resultCount
		}

		// Calculate scroll offset to keep selected item visible
		startIdx := 0
		if v.searchIndex >= maxVisible {
			startIdx = v.searchIndex - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > resultCount {
			endIdx = resultCount
		}

		for i := startIdx; i < endIdx; i++ {
			entry := v.searchResult[i]

			// Truncate SQL to fit on one line
			sql := strings.ReplaceAll(entry.SQL, "\n", " ")
			sql = strings.ReplaceAll(sql, "\t", " ")
			maxLen := v.width - 10
			if len(sql) > maxLen {
				sql = sql[:maxLen-3] + "..."
			}

			// Highlight selected entry
			if i == v.searchIndex {
				// Apply syntax highlighting to selected entry
				highlighted := HighlightSQL(sql)
				sb.WriteString(styles.TableSelectedStyle.Render(fmt.Sprintf("► %s", highlighted)))
			} else {
				sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("  %s", sql)))
			}
			sb.WriteString("\n")
		}

		// Show scroll indicator if there are more items
		if startIdx > 0 || endIdx < resultCount {
			sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("\n  Showing %d-%d of %d", startIdx+1, endIdx, resultCount)))
		}
	}

	// Footer hints
	sb.WriteString("\n\n")
	hints := "Enter: Select │ ↑/Ctrl+S: Newer │ ↓/Ctrl+R: Older │ Esc: Cancel"
	sb.WriteString(styles.MutedStyle.Render(hints))

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(sb.String())
}

// renderSnippetBrowser renders the Ctrl+O snippet browser overlay.
func (v *SQLEditorView) renderSnippetBrowser() string {
	var sb strings.Builder

	// Title
	title := styles.HeaderStyle.Render("Snippets (Ctrl+O)")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Search input
	if v.snippetSearchQuery != "" {
		searchLine := fmt.Sprintf("Filter: %s█", v.snippetSearchQuery)
		sb.WriteString(styles.MutedStyle.Render(searchLine))
		sb.WriteString("\n\n")
	}

	// Show results count
	resultCount := len(v.snippetList)
	if resultCount == 0 {
		if v.snippetSearchQuery != "" {
			sb.WriteString(styles.MutedStyle.Render("No matching snippets found"))
		} else {
			sb.WriteString(styles.MutedStyle.Render("No snippets saved yet"))
			sb.WriteString("\n\n")
			sb.WriteString(styles.MutedStyle.Render("Use :save NAME to save the current query as a snippet"))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("%d snippet(s)", resultCount)))
		sb.WriteString("\n\n")

		// Show visible results (up to 12)
		maxVisible := 12
		if maxVisible > resultCount {
			maxVisible = resultCount
		}

		// Adjust scroll to keep selected item visible
		startIdx := 0
		if v.snippetIndex >= maxVisible {
			startIdx = v.snippetIndex - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > resultCount {
			endIdx = resultCount
		}

		for i := startIdx; i < endIdx; i++ {
			snippet := v.snippetList[i]

			// Truncate SQL to fit on one line
			sql := strings.ReplaceAll(snippet.SQL, "\n", " ")
			sql = strings.ReplaceAll(sql, "\t", " ")
			maxLen := v.width - 30
			if len(sql) > maxLen {
				sql = sql[:maxLen-3] + "..."
			}

			// Format: name - sql preview
			line := fmt.Sprintf("%-20s %s", snippet.Name, sql)

			// Highlight selected entry
			if i == v.snippetIndex {
				sb.WriteString(styles.TableSelectedStyle.Render(fmt.Sprintf("► %s", line)))
			} else {
				sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("  %s", line)))
			}
			sb.WriteString("\n")
		}

		if resultCount > maxVisible {
			sb.WriteString(styles.MutedStyle.Render(fmt.Sprintf("\n  ... showing %d-%d of %d", startIdx+1, endIdx, resultCount)))
		}
	}

	// Footer hints
	sb.WriteString("\n\n")
	hints := "Enter: Load │ j/k: Navigate │ d: Delete │ Type to filter │ Esc: Cancel"
	sb.WriteString(styles.MutedStyle.Render(hints))

	return lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Padding(2, 4).
		Render(sb.String())
}
