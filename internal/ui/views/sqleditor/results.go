package sqleditor

import (
	"fmt"
	"sort"
	"time"
)

// pageRowBounds returns the start and end row indices for the current page.
func (v *SQLEditorView) pageRowBounds() (int, int) {
	if v.results == nil {
		return 0, 0
	}
	// For server-side pagination, rows array only contains current page (0 to len-1)
	if v.paginationBaseSQL != "" {
		return 0, len(v.results.Rows)
	}
	// Client-side pagination: calculate slice of all-in-memory rows
	pageStart := (v.results.CurrentPage - 1) * v.results.PageSize
	pageEnd := pageStart + v.results.PageSize
	if pageEnd > len(v.results.Rows) {
		pageEnd = len(v.results.Rows)
	}
	return pageStart, pageEnd
}

// moveSelection moves the row selection by delta within the current page.
func (v *SQLEditorView) moveSelection(delta int) {
	if v.results == nil || len(v.results.Rows) == 0 {
		return
	}

	pageStart, pageEnd := v.pageRowBounds()

	// If no row selected yet, select first row on any movement
	if v.selectedRow < 0 {
		v.selectedRow = pageStart
		v.ensureVisible()
		return
	}

	v.selectedRow += delta
	if v.selectedRow < pageStart {
		v.selectedRow = pageStart
	}
	if v.selectedRow >= pageEnd {
		v.selectedRow = pageEnd - 1
	}

	v.ensureVisible()
}

// visibleResultRows returns the number of data rows that can be displayed.
// This accounts for: title bar (1), header row (1), separator (1), col info (1), pagination footer (1), margin (1)
func (v *SQLEditorView) visibleResultRows() int {
	visible := v.resultsHeight() - 6
	if v.results != nil && v.results.TotalPages() > 1 {
		visible-- // pagination footer takes extra line
	}
	if visible < 1 {
		visible = 1
	}
	return visible
}

// ensureVisible scrolls to make the selected row visible within current page.
func (v *SQLEditorView) ensureVisible() {
	if v.results == nil || v.selectedRow < 0 {
		return
	}

	pageStart, pageEnd := v.pageRowBounds()
	visibleRows := v.visibleResultRows()
	pageRowCount := pageEnd - pageStart

	// scrollOffset is relative to page start
	relativeRow := v.selectedRow - pageStart

	if relativeRow < v.scrollOffset {
		v.scrollOffset = relativeRow
	} else if relativeRow >= v.scrollOffset+visibleRows {
		v.scrollOffset = relativeRow - visibleRows + 1
	}

	// Clamp scrollOffset
	if v.scrollOffset < 0 {
		v.scrollOffset = 0
	}
	maxScroll := pageRowCount - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scrollOffset > maxScroll {
		v.scrollOffset = maxScroll
	}
}

// nextPage advances to the next page of results.
func (v *SQLEditorView) nextPage() {
	if v.results == nil {
		return
	}
	if v.results.HasNextPage() {
		v.results.CurrentPage++
		// Set selectedRow to first row of new page
		v.selectedRow = (v.results.CurrentPage - 1) * v.results.PageSize
		v.scrollOffset = 0
	}
}

// prevPage goes to the previous page of results.
func (v *SQLEditorView) prevPage() {
	if v.results == nil {
		return
	}
	if v.results.HasPrevPage() {
		v.results.CurrentPage--
		// Set selectedRow to first row of new page
		v.selectedRow = (v.results.CurrentPage - 1) * v.results.PageSize
		v.scrollOffset = 0
	}
}

// cycleSortColumn cycles through sort columns (s key).
func (v *SQLEditorView) cycleSortColumn() {
	if v.results == nil || len(v.results.Columns) == 0 {
		return
	}

	v.results.SortColumn++
	if v.results.SortColumn >= len(v.results.Columns) {
		v.results.SortColumn = -1 // Back to no sort
	}

	if v.results.SortColumn >= 0 {
		v.sortResults()
	} else {
		// Restore original order
		v.restoreOriginalOrder()
	}

	// Reset to page 1 and first row
	v.results.CurrentPage = 1
	v.selectedRow = 0
	v.scrollOffset = 0
}

// toggleSortDirection toggles sort direction (S key).
func (v *SQLEditorView) toggleSortDirection() {
	if v.results == nil || v.results.SortColumn < 0 {
		return
	}

	v.results.SortAsc = !v.results.SortAsc
	v.sortResults()

	// Reset to page 1 and first row
	v.results.CurrentPage = 1
	v.selectedRow = 0
	v.scrollOffset = 0
}

// sortResults sorts the results by the current sort column.
func (v *SQLEditorView) sortResults() {
	if v.results == nil || v.results.SortColumn < 0 || len(v.results.RawRows) == 0 {
		return
	}

	col := v.results.SortColumn
	asc := v.results.SortAsc

	// Create index slice to sort both raw and formatted rows together
	n := len(v.results.RawRows)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	// Sort indices based on raw values
	sort.Slice(indices, func(i, j int) bool {
		ai, aj := indices[i], indices[j]
		if col >= len(v.results.RawRows[ai]) || col >= len(v.results.RawRows[aj]) {
			return false
		}
		valA := v.results.RawRows[ai][col]
		valB := v.results.RawRows[aj][col]

		less := compareValues(valA, valB)
		if asc {
			return less
		}
		return !less
	})

	// Reorder both slices
	newRaw := make([][]any, n)
	newFormatted := make([][]string, n)
	for i, idx := range indices {
		newRaw[i] = v.results.RawRows[idx]
		newFormatted[i] = v.results.Rows[idx]
	}
	v.results.RawRows = newRaw
	v.results.Rows = newFormatted
}

// restoreOriginalOrder restores original query order (re-formats from raw).
func (v *SQLEditorView) restoreOriginalOrder() {
	// We don't store original order separately, so just leave as-is
	// In practice, user would re-execute query to get original order
}

// compareValues compares two values for sorting.
func compareValues(a, b any) bool {
	// Handle nil values - sort nulls last
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false // nulls last
	}
	if b == nil {
		return true // nulls last
	}

	// Type-specific comparison
	switch va := a.(type) {
	case int64:
		if vb, ok := b.(int64); ok {
			return va < vb
		}
	case int32:
		if vb, ok := b.(int32); ok {
			return va < vb
		}
	case int16:
		if vb, ok := b.(int16); ok {
			return va < vb
		}
	case int:
		if vb, ok := b.(int); ok {
			return va < vb
		}
	case float64:
		if vb, ok := b.(float64); ok {
			return va < vb
		}
	case float32:
		if vb, ok := b.(float32); ok {
			return va < vb
		}
	case string:
		if vb, ok := b.(string); ok {
			return va < vb
		}
	case bool:
		if vb, ok := b.(bool); ok {
			return !va && vb // false < true
		}
	case time.Time:
		if vb, ok := b.(time.Time); ok {
			return va.Before(vb)
		}
	}

	// Fallback: compare string representations
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}
