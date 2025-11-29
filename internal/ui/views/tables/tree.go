package tables

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/willibrandon/steep/internal/db/models"
)

// buildTreeItems builds the flattened tree for rendering.
func (v *TablesView) buildTreeItems() {
	v.treeItems = nil

	// Group tables by schema
	tablesBySchema := make(map[string][]models.Table)
	for _, t := range v.tables {
		// Skip partitions - they'll be shown under their parent
		if t.IsPartition {
			continue
		}
		tablesBySchema[t.SchemaName] = append(tablesBySchema[t.SchemaName], t)
	}

	// Build tree from schemas
	for i := range v.schemas {
		schema := &v.schemas[i]

		// Filter system schemas if not showing
		if !v.showSystemSchemas && schema.IsSystem {
			continue
		}

		tables := tablesBySchema[schema.Name]
		// Sort tables within each schema
		v.sortTables(tables)
		isLastSchema := v.isLastVisibleSchema(i)

		// Add schema item
		v.treeItems = append(v.treeItems, TreeItem{
			IsSchema: true,
			Schema:   schema,
			Depth:    0,
			IsLast:   isLastSchema,
			Expanded: schema.Expanded,
		})

		// Add tables if schema is expanded
		if schema.Expanded {
			for j, table := range tables {
				isLastTable := j == len(tables)-1
				tableCopy := table // Make a copy for stable pointer
				tableCopy.Indexes = v.getIndexesForTable(table.OID)

				v.treeItems = append(v.treeItems, TreeItem{
					IsTable:  true,
					Table:    &tableCopy,
					Depth:    1,
					IsLast:   isLastTable,
					Expanded: tableCopy.Expanded,
				})

				// Add partitions if table is partitioned and expanded
				if table.IsPartitioned && tableCopy.Expanded {
					childOIDs := v.partitions[table.OID]
					for k, childOID := range childOIDs {
						if childTable, ok := v.tablesByOID[childOID]; ok {
							isLastPartition := k == len(childOIDs)-1
							childCopy := *childTable

							v.treeItems = append(v.treeItems, TreeItem{
								IsPartition: true,
								Table:       &childCopy,
								Depth:       2,
								IsLast:      isLastPartition,
								ParentOID:   table.OID,
							})
						}
					}
				}
			}
		}
	}
}

// isLastVisibleSchema checks if this is the last visible schema.
func (v *TablesView) isLastVisibleSchema(idx int) bool {
	for i := idx + 1; i < len(v.schemas); i++ {
		if v.showSystemSchemas || !v.schemas[i].IsSystem {
			return false
		}
	}
	return true
}

// getIndexesForTable returns indexes for a given table OID, sorted with primary/unique first.
func (v *TablesView) getIndexesForTable(tableOID uint32) []models.Index {
	var result []models.Index
	for _, idx := range v.indexes {
		if idx.TableOID == tableOID {
			result = append(result, idx)
		}
	}
	// Sort: primary keys first, then unique indexes, then regular indexes (alphabetically within each group)
	sort.Slice(result, func(i, j int) bool {
		// Primary keys come first
		if result[i].IsPrimary != result[j].IsPrimary {
			return result[i].IsPrimary
		}
		// Then unique indexes
		if result[i].IsUnique != result[j].IsUnique {
			return result[i].IsUnique
		}
		// Then sort by name
		return result[i].Name < result[j].Name
	})
	return result
}

// sortTables sorts a slice of tables by the current sort column and direction.
func (v *TablesView) sortTables(tables []models.Table) {
	sort.Slice(tables, func(i, j int) bool {
		var less bool
		switch v.sortColumn {
		case SortByName:
			less = tables[i].Name < tables[j].Name
		case SortBySize:
			less = tables[i].TotalSize < tables[j].TotalSize
		case SortByRows:
			less = tables[i].RowCount < tables[j].RowCount
		case SortByBloat:
			less = tables[i].BloatPct < tables[j].BloatPct
		case SortByCacheHit:
			less = tables[i].CacheHitRatio < tables[j].CacheHitRatio
		default:
			less = tables[i].Name < tables[j].Name
		}
		if v.sortAscending {
			return less
		}
		return !less
	})
}

// cycleSortColumn cycles to the next sort column.
func (v *TablesView) cycleSortColumn() {
	switch v.sortColumn {
	case SortByName:
		v.sortColumn = SortBySize
	case SortBySize:
		v.sortColumn = SortByRows
	case SortByRows:
		v.sortColumn = SortByBloat
	case SortByBloat:
		v.sortColumn = SortByCacheHit
	case SortByCacheHit:
		v.sortColumn = SortByName
	default:
		v.sortColumn = SortByName
	}
	v.buildTreeItems()
}

// toggleSortDirection toggles between ascending and descending sort.
func (v *TablesView) toggleSortDirection() {
	v.sortAscending = !v.sortAscending
	v.buildTreeItems()
}

// cycleIndexSortColumn cycles to the next index sort column.
func (v *TablesView) cycleIndexSortColumn() {
	switch v.indexSortColumn {
	case IndexSortByName:
		v.indexSortColumn = IndexSortBySize
	case IndexSortBySize:
		v.indexSortColumn = IndexSortByScans
	case IndexSortByScans:
		v.indexSortColumn = IndexSortByRowsRead
	case IndexSortByRowsRead:
		v.indexSortColumn = IndexSortByCacheHit
	case IndexSortByCacheHit:
		v.indexSortColumn = IndexSortByName
	default:
		v.indexSortColumn = IndexSortByName
	}
}

// toggleIndexSortDirection toggles between ascending and descending index sort.
func (v *TablesView) toggleIndexSortDirection() {
	v.indexSortAscending = !v.indexSortAscending
}

// getSelectedTableIndexes returns indexes for the currently selected table, sorted.
func (v *TablesView) getSelectedTableIndexes() []models.Index {
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return nil
	}
	item := v.treeItems[v.selectedIdx]
	if item.Table == nil {
		return nil
	}
	indexes := v.getIndexesForTable(item.Table.OID)
	return v.sortIndexes(indexes)
}

// indexTypeRank returns the sort rank for an index type (primary=0, unique=1, regular=2).
func indexTypeRank(idx *models.Index) int {
	if idx.IsPrimary {
		return 0
	}
	if idx.IsUnique {
		return 1
	}
	return 2
}

// sortIndexes sorts indexes by the current sort column and direction.
// When sorting by Name, type-based grouping is applied (primary → unique → regular).
func (v *TablesView) sortIndexes(indexes []models.Index) []models.Index {
	if len(indexes) == 0 {
		return indexes
	}

	// Make a copy to avoid modifying the original
	sorted := make([]models.Index, len(indexes))
	copy(sorted, indexes)

	sort.Slice(sorted, func(i, j int) bool {
		// Type-based grouping only when sorting by Name
		// Always primary → unique → regular, regardless of sort direction
		if v.indexSortColumn == IndexSortByName {
			rankI := indexTypeRank(&sorted[i])
			rankJ := indexTypeRank(&sorted[j])
			if rankI != rankJ {
				return rankI < rankJ // primary → unique → regular (always)
			}
		}

		// Sort by selected column
		var less bool
		switch v.indexSortColumn {
		case IndexSortByName:
			less = sorted[i].Name < sorted[j].Name
		case IndexSortBySize:
			less = sorted[i].Size < sorted[j].Size
		case IndexSortByScans:
			less = sorted[i].ScanCount < sorted[j].ScanCount
		case IndexSortByRowsRead:
			less = sorted[i].RowsRead < sorted[j].RowsRead
		case IndexSortByCacheHit:
			less = sorted[i].CacheHitRatio < sorted[j].CacheHitRatio
		default:
			less = sorted[i].Name < sorted[j].Name
		}
		if v.indexSortAscending {
			return less
		}
		return !less
	})

	return sorted
}

// moveIndexSelection moves the index selection by delta rows.
func (v *TablesView) moveIndexSelection(delta int) {
	indexes := v.getSelectedTableIndexes()
	if len(indexes) == 0 {
		return
	}
	v.selectedIndex += delta
	if v.selectedIndex < 0 {
		v.selectedIndex = 0
	}
	if v.selectedIndex >= len(indexes) {
		v.selectedIndex = len(indexes) - 1
	}
	v.ensureIndexVisible()
}

// ensureIndexVisible adjusts index scroll offset to keep selection visible.
func (v *TablesView) ensureIndexVisible() {
	indexHeight := v.indexPanelHeight()
	if indexHeight <= 0 {
		return
	}
	if v.selectedIndex < v.indexScrollOffset {
		v.indexScrollOffset = v.selectedIndex
	}
	if v.selectedIndex >= v.indexScrollOffset+indexHeight {
		v.indexScrollOffset = v.selectedIndex - indexHeight + 1
	}
}

// splitContentHeight returns the total available height for tables + indexes panels.
func (v *TablesView) splitContentHeight() int {
	// Fixed elements: status(3) + title(1) + header(2 w/border) + indexTitle(1) + indexHeader(2 w/border) + footer(3) = 12
	return max(6, v.height-12) // Min 6 to allow 3 for each panel
}

// indexPanelHeight returns the height of the index panel (content rows only).
func (v *TablesView) indexPanelHeight() int {
	// Index panel gets (1-splitRatio) of available content height, minimum 3 rows
	availableHeight := v.splitContentHeight()
	indexHeight := int(float64(availableHeight) * (1 - v.splitRatio))
	return max(3, indexHeight)
}

// tablePanelHeight returns the height of the table panel when index panel is shown.
func (v *TablesView) tablePanelHeight() int {
	// Table panel gets remaining space after index panel (avoids rounding issues)
	availableHeight := v.splitContentHeight()
	return max(3, availableHeight-v.indexPanelHeight())
}

// resizeSplitUp increases the tables panel size (decreases index panel).
func (v *TablesView) resizeSplitUp() {
	v.splitRatio += 0.1
	if v.splitRatio > 0.85 {
		v.splitRatio = 0.85
	}
}

// resizeSplitDown decreases the tables panel size (increases index panel).
func (v *TablesView) resizeSplitDown() {
	v.splitRatio -= 0.1
	if v.splitRatio < 0.3 {
		v.splitRatio = 0.3
	}
}

// toggleFocusPanel switches focus between tables and indexes.
func (v *TablesView) toggleFocusPanel() {
	if v.focusPanel == FocusTables {
		// Only switch to indexes if a table is selected and has indexes
		indexes := v.getSelectedTableIndexes()
		if len(indexes) > 0 {
			v.focusPanel = FocusIndexes
			if v.selectedIndex >= len(indexes) {
				v.selectedIndex = 0
			}
		}
	} else {
		v.focusPanel = FocusTables
	}
}

// copySelectedIndexName copies the selected index name to clipboard.
func (v *TablesView) copySelectedIndexName() {
	indexes := v.getSelectedTableIndexes()
	if v.focusPanel != FocusIndexes || len(indexes) == 0 {
		return
	}
	if v.selectedIndex >= 0 && v.selectedIndex < len(indexes) {
		idx := indexes[v.selectedIndex]
		fullName := fmt.Sprintf("%s.%s", idx.SchemaName, idx.Name)
		if err := v.clipboard.Write(fullName); err != nil {
			v.showToast("Failed to copy: "+err.Error(), true)
		} else {
			v.showToast("Copied: "+fullName, false)
		}
	}
}

// copyTableName copies the qualified table name to clipboard.
func (v *TablesView) copyTableName() {
	if v.details == nil {
		return
	}
	t := &v.details.Table
	fullName := fmt.Sprintf("%s.%s", t.SchemaName, t.Name)
	if err := v.clipboard.Write(fullName); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
	} else {
		v.showToast("Copied table name", false)
	}
}

// copySelectSQL copies an executable SELECT statement to clipboard.
func (v *TablesView) copySelectSQL() {
	if v.details == nil {
		return
	}
	t := &v.details.Table
	var cols []string
	for _, col := range v.details.Columns {
		cols = append(cols, col.Name)
	}
	colList := "*"
	if len(cols) > 0 {
		colList = strings.Join(cols, ", ")
	}
	// Executable query with LIMIT 100 (safe default)
	sql := fmt.Sprintf("SELECT %s FROM %s.%s LIMIT 100;", colList, t.SchemaName, t.Name)
	sql = v.formatSQL(sql)
	if err := v.clipboard.Write(sql); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
	} else {
		v.showToast("Copied SELECT statement", false)
	}
}

// copyInsertSQL copies an INSERT statement template to clipboard.
func (v *TablesView) copyInsertSQL() {
	if v.details == nil {
		return
	}
	t := &v.details.Table
	var cols []string
	var placeholders []string
	i := 1
	for _, col := range v.details.Columns {
		cols = append(cols, col.Name)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	sql := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s);",
		t.SchemaName, t.Name,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "))
	sql = v.formatSQL(sql)
	if err := v.clipboard.Write(sql); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
	} else {
		v.showToast("Copied INSERT statement", false)
	}
}

// copyUpdateSQL copies an UPDATE statement template to clipboard.
func (v *TablesView) copyUpdateSQL() {
	if v.details == nil {
		return
	}
	t := &v.details.Table
	var setClauses []string
	i := 1
	for _, col := range v.details.Columns {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col.Name, i))
		i++
	}
	sql := fmt.Sprintf("UPDATE %s.%s SET %s WHERE <condition>;",
		t.SchemaName, t.Name,
		strings.Join(setClauses, ", "))
	sql = v.formatSQL(sql)
	if err := v.clipboard.Write(sql); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
	} else {
		v.showToast("Copied UPDATE statement", false)
	}
}

// copyDeleteSQL copies a DELETE statement template to clipboard.
func (v *TablesView) copyDeleteSQL() {
	if v.details == nil {
		return
	}
	t := &v.details.Table
	sql := fmt.Sprintf("DELETE FROM %s.%s WHERE <condition>;", t.SchemaName, t.Name)
	sql = v.formatSQL(sql)
	if err := v.clipboard.Write(sql); err != nil {
		v.showToast("Failed to copy: "+err.Error(), true)
	} else {
		v.showToast("Copied DELETE statement", false)
	}
}

// formatSQL formats SQL using pgFormatter via Docker if available.
func (v *TablesView) formatSQL(sql string) string {
	// Try pg_format via Docker
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2")
	cmd.Stdin = strings.NewReader(sql)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		formatted := strings.TrimSpace(out.String())
		if formatted != "" {
			return formatted
		}
	}
	// Fallback: return as-is
	return sql
}
