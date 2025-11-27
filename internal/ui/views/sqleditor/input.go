package sqleditor

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleResultsKeys handles keys when results pane has focus.
func (v *SQLEditorView) handleResultsKeys(key string) tea.Cmd {
	if v.results == nil || v.results.TotalRows == 0 {
		return nil
	}

	switch key {
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "h":
		v.moveColumnSelection(-1)
	case "l":
		v.moveColumnSelection(1)
	case "g", "home":
		v.selectedRow = 0
		v.ensureVisible()
	case "G", "end":
		v.selectedRow = len(v.results.Rows) - 1
		v.ensureVisible()
	case "0":
		// First column (vim-style)
		v.selectedCol = 0
		v.ensureColumnVisible()
	case "$":
		// Last column (vim-style)
		if len(v.results.Columns) > 0 {
			v.selectedCol = len(v.results.Columns) - 1
			v.ensureColumnVisible()
		}
	case "ctrl+d", "pgdown":
		v.moveSelection(10)
	case "ctrl+u", "pgup":
		v.moveSelection(-10)
	case "n":
		// Server-side pagination if we have a base query
		if v.paginationBaseSQL != "" {
			return v.fetchPage(v.paginationPage + 1)
		}
		v.nextPage()
	case "p":
		// Server-side pagination if we have a base query
		if v.paginationBaseSQL != "" {
			return v.fetchPage(v.paginationPage - 1)
		}
		v.prevPage()
	case "left":
		v.scrollColumnsLeft()
	case "right":
		v.scrollColumnsRight()
	case "s":
		v.cycleSortColumn()
	case "S":
		v.toggleSortDirection()
	case "y":
		return v.copyCell()
	case "Y":
		return v.copyRow()
	case "esc":
		// Deselect row
		v.selectedRow = -1
	}

	return nil
}

// scrollColumnsLeft scrolls the results table one column to the left.
func (v *SQLEditorView) scrollColumnsLeft() {
	if v.colScrollOffset > 0 {
		v.colScrollOffset--
	}
}

// scrollColumnsRight scrolls the results table one column to the right.
func (v *SQLEditorView) scrollColumnsRight() {
	if v.results != nil && v.colScrollOffset < len(v.results.Columns)-1 {
		v.colScrollOffset++
	}
}

// moveColumnSelection moves the column selection by delta.
func (v *SQLEditorView) moveColumnSelection(delta int) {
	if v.results == nil || len(v.results.Columns) == 0 {
		return
	}

	v.selectedCol += delta
	if v.selectedCol < 0 {
		v.selectedCol = 0
	}
	if v.selectedCol >= len(v.results.Columns) {
		v.selectedCol = len(v.results.Columns) - 1
	}

	v.ensureColumnVisible()
}

// ensureColumnVisible scrolls horizontally to make the selected column visible.
func (v *SQLEditorView) ensureColumnVisible() {
	if v.results == nil || v.selectedCol < 0 {
		return
	}

	// If selected column is before the scroll offset, scroll left
	if v.selectedCol < v.colScrollOffset {
		v.colScrollOffset = v.selectedCol
	}

	// If selected column is too far right, scroll right keeping 2 columns of context
	// (We don't know exact visible count, so use a reasonable heuristic)
	contextCols := 2
	if v.selectedCol > v.colScrollOffset+contextCols {
		v.colScrollOffset = v.selectedCol - contextCols
	}
}

// handleMouseMsg handles mouse events for scrolling and clicking.
// Routes events to vimtea editor or results table based on focus.
func (v *SQLEditorView) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	// Don't handle mouse during execution or help mode
	if v.mode == ModeExecuting || v.mode == ModeHelp {
		return nil
	}

	// Calculate layout boundaries
	// App header: lines 0-3 (title, tabs, etc.)
	// Connection bar: line 4
	// Editor title: line 5
	// Editor content: lines 6+
	editorHeight := int(float64(v.height-5) * v.splitRatio)
	editorContentStartY := 6                                     // After app header, connection bar, and editor title
	editorContentEndY := editorContentStartY + editorHeight - 2  // -2 for title and status bar within editor
	resultsDataStartY := 10 + editorHeight

	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		// Route scroll based on focus, not mouse position
		if v.focus == FocusEditor {
			// Pass scroll to vimtea editor (adjust Y for editor's coordinate space)
			adjustedMsg := tea.MouseMsg{
				X:      msg.X,
				Y:      msg.Y - editorContentStartY,
				Button: msg.Button,
				Action: msg.Action,
			}
			_, cmd := v.editor.Update(adjustedMsg)
			return cmd
		}
		// Results focus - scroll results
		if v.results != nil && v.results.TotalRows > 0 {
			if msg.Shift {
				// Shift+scroll = horizontal scroll
				if msg.Button == tea.MouseButtonWheelUp {
					v.scrollColumnsLeft()
				} else {
					v.scrollColumnsRight()
				}
			} else {
				// Normal scroll = vertical (move selection)
				if msg.Button == tea.MouseButtonWheelUp {
					v.moveSelection(-1)
				} else {
					v.moveSelection(1)
				}
			}
		}
		return nil

	case tea.MouseButtonWheelLeft:
		if v.focus == FocusResults && v.results != nil && v.results.TotalRows > 0 {
			v.scrollColumnsLeft()
		}
		return nil

	case tea.MouseButtonWheelRight:
		if v.focus == FocusResults && v.results != nil && v.results.TotalRows > 0 {
			v.scrollColumnsRight()
		}
		return nil

	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			// Check if click is in editor area
			if msg.Y >= editorContentStartY && msg.Y <= editorContentEndY {
				v.focus = FocusEditor
				// Pass click to vimtea editor (adjust Y for editor's coordinate space)
				adjustedMsg := tea.MouseMsg{
					X:      msg.X,
					Y:      msg.Y - editorContentStartY,
					Button: msg.Button,
					Action: msg.Action,
				}
				_, cmd := v.editor.Update(adjustedMsg)
				return cmd
			}
			// Check if click is in results area
			if msg.Y >= resultsDataStartY && v.results != nil && v.results.TotalRows > 0 {
				v.focus = FocusResults
				// Shift+click to deselect
				if msg.Shift {
					v.selectedRow = -1
				} else {
					clickedRow := msg.Y - resultsDataStartY + v.scrollOffset
					if clickedRow >= 0 && clickedRow < len(v.results.Rows) {
						v.selectedRow = clickedRow
						// Also set column based on X position
						clickedCol := v.columnAtX(msg.X)
						if clickedCol >= 0 {
							v.selectedCol = clickedCol
						}
						v.ensureVisible()
					}
				}
			}
		}
		return nil
	}
	return nil
}

// switchFocus toggles focus between editor and results.
func (v *SQLEditorView) switchFocus() {
	if v.focus == FocusEditor {
		v.focus = FocusResults
	} else {
		v.focus = FocusEditor
	}
}

// columnAtX returns the column index at the given X position, or -1 if none.
// This mirrors the column width calculation in renderResultsTable.
func (v *SQLEditorView) columnAtX(x int) int {
	if v.results == nil || len(v.results.Columns) == 0 {
		return -1
	}

	// Calculate column widths (same logic as render)
	totalCols := len(v.results.Columns)
	colWidths := make([]int, totalCols)

	for i, col := range v.results.Columns {
		headerText := col.Name
		if col.TypeName != "" {
			headerText = col.Name + " (" + col.TypeName + ")"
		}
		if v.results.SortColumn == i {
			headerText += " ↑"
		}
		colWidths[i] = len(headerText)
		if colWidths[i] < 3 {
			colWidths[i] = 3
		}
	}

	for _, row := range v.results.Rows {
		for i, val := range row {
			if i < len(colWidths) && len(val) > colWidths[i] {
				colWidths[i] = len(val)
			}
		}
	}

	// Cap column widths
	maxColWidth := 32
	for i := range colWidths {
		if colWidths[i] > maxColWidth {
			colWidths[i] = maxColWidth
		}
	}

	// Walk through visible columns to find which one contains X
	startCol := v.colScrollOffset
	if startCol >= totalCols {
		startCol = totalCols - 1
	}
	if startCol < 0 {
		startCol = 0
	}

	currentX := 0
	for i := startCol; i < totalCols; i++ {
		colEnd := currentX + colWidths[i]
		if x >= currentX && x < colEnd {
			return i
		}
		currentX = colEnd + 3 // +3 for " │ " separator
	}

	// If past all columns, return last visible column
	if totalCols > 0 {
		return totalCols - 1
	}
	return -1
}
