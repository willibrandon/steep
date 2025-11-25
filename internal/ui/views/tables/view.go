// Package tables provides the Tables view for schema/table statistics monitoring.
package tables

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// TablesMode represents the current interaction mode.
type TablesMode int

const (
	ModeNormal TablesMode = iota
	ModeDetails
	ModeConfirmInstall
	ModeConfirmVacuum
	ModeConfirmAnalyze
	ModeConfirmReindex
	ModeHelp
)

// FocusPanel indicates which panel has keyboard focus.
type FocusPanel int

const (
	FocusTables FocusPanel = iota
	FocusIndexes
)

// SortColumn represents available sort columns.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortBySize
	SortByRows
	SortByBloat
	SortByCacheHit
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByName:
		return "Name"
	case SortBySize:
		return "Size"
	case SortByRows:
		return "Rows"
	case SortByBloat:
		return "Bloat"
	case SortByCacheHit:
		return "Cache"
	default:
		return "Unknown"
	}
}

// TreeItem represents an item in the tree view (schema, table, or partition).
type TreeItem struct {
	// Type info
	IsSchema    bool
	IsTable     bool
	IsPartition bool

	// Data references
	Schema *models.Schema
	Table  *models.Table

	// Display info
	Depth    int  // Indentation level
	IsLast   bool // Is this the last child?
	Expanded bool // Is this item expanded?

	// Parent info for partitions
	ParentOID uint32
}

// Message types for the tables view
type (
	// TablesDataMsg contains refreshed table/index data.
	TablesDataMsg struct {
		Schemas              []models.Schema
		Tables               []models.Table
		Indexes              []models.Index
		Partitions           map[uint32][]uint32
		PgstattupleAvailable bool
		Error                error
	}

	// RefreshTablesMsg triggers data refresh.
	RefreshTablesMsg struct{}
)

// TablesView displays schema and table statistics.
type TablesView struct {
	width  int
	height int

	// Data
	schemas    []models.Schema
	tables     []models.Table
	indexes    []models.Index
	partitions map[uint32][]uint32 // parent OID -> child OIDs
	details    *models.TableDetails

	// Table lookup by OID for quick access
	tablesByOID map[uint32]*models.Table

	// Tree state
	treeItems    []TreeItem // Flattened tree for rendering
	selectedIdx  int
	scrollOffset int

	// UI state
	mode              TablesMode
	focusPanel        FocusPanel
	selectedIndex     int // Selected index in index list when FocusIndexes
	indexScrollOffset int
	sortColumn        SortColumn
	sortAscending     bool
	showSystemSchemas bool

	// Extension state
	pgstattupleAvailable bool
	pgstattupleChecked   bool
	installPromptShown   bool // Session-scoped: don't prompt again if declined

	// App state
	readonlyMode   bool
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	loading        bool
	err            error

	// Toast
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Detail view state
	detailScrollOffset int
	detailLines        []string

	// Spinner for loading states
	spinner spinner.Model

	// Clipboard
	clipboard *ui.ClipboardWriter

	// Database connection
	pool *pgxpool.Pool
}

// NewTablesView creates a new TablesView instance.
func NewTablesView() *TablesView {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &TablesView{
		mode:              ModeNormal,
		focusPanel:        FocusTables,
		sortColumn:        SortByName,
		partitions:        make(map[uint32][]uint32),
		tablesByOID:       make(map[uint32]*models.Table),
		clipboard:         ui.NewClipboardWriter(),
		spinner:           s,
		showSystemSchemas: false, // Hidden by default per spec
		loading:           true,  // Start in loading state
	}
}

// Init initializes the tables view (spinner only - data fetch triggered after pool is set).
func (v *TablesView) Init() tea.Cmd {
	return v.spinner.Tick
}

// FetchTablesData returns a command that loads all table data.
// This is exported for the app to trigger after setting the pool.
func (v *TablesView) FetchTablesData() tea.Cmd {
	return v.fetchTablesData()
}

// fetchTablesData returns a command that loads all table data.
func (v *TablesView) fetchTablesData() tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return TablesDataMsg{Error: fmt.Errorf("database connection not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Check pgstattuple extension availability
		pgstattupleAvailable, err := queries.CheckPgstattupleExtension(ctx, v.pool)
		if err != nil {
			// Non-fatal: continue without pgstattuple
			pgstattupleAvailable = false
		}

		schemas, err := queries.GetSchemas(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch schemas: %w", err)}
		}

		tables, err := queries.GetTablesWithStats(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch tables: %w", err)}
		}

		indexes, err := queries.GetIndexesWithStats(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch indexes: %w", err)}
		}

		partitions, err := queries.GetPartitionHierarchy(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch partitions: %w", err)}
		}

		return TablesDataMsg{
			Schemas:              schemas,
			Tables:               tables,
			Indexes:              indexes,
			Partitions:           partitions,
			PgstattupleAvailable: pgstattupleAvailable,
		}
	}
}

// scheduleRefresh returns a command for 30-second auto-refresh.
func (v *TablesView) scheduleRefresh() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return RefreshTablesMsg{}
	})
}

// Update handles messages for the tables view.
func (v *TablesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			return v, cmd
		}

	case TablesDataMsg:
		v.loading = false
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			// Preserve expanded state before updating
			expandedSchemas := make(map[uint32]bool)
			for _, s := range v.schemas {
				if s.Expanded {
					expandedSchemas[s.OID] = true
				}
			}
			expandedTables := make(map[uint32]bool)
			for _, t := range v.tables {
				if t.Expanded {
					expandedTables[t.OID] = true
				}
			}

			v.schemas = msg.Schemas
			v.tables = msg.Tables
			v.indexes = msg.Indexes
			v.partitions = msg.Partitions
			v.pgstattupleAvailable = msg.PgstattupleAvailable
			v.pgstattupleChecked = true
			v.lastUpdate = time.Now()
			v.err = nil

			// Restore expanded state
			for i := range v.schemas {
				if expandedSchemas[v.schemas[i].OID] {
					v.schemas[i].Expanded = true
				}
			}
			for i := range v.tables {
				if expandedTables[v.tables[i].OID] {
					v.tables[i].Expanded = true
				}
			}

			// Build table lookup
			v.tablesByOID = make(map[uint32]*models.Table)
			for i := range v.tables {
				v.tablesByOID[v.tables[i].OID] = &v.tables[i]
			}

			// Rebuild tree
			v.buildTreeItems()

			// Ensure selection is valid
			if v.selectedIdx >= len(v.treeItems) {
				v.selectedIdx = max(0, len(v.treeItems)-1)
			}
			v.ensureVisible()
		}
		return v, v.scheduleRefresh()

	case RefreshTablesMsg:
		if !v.refreshing {
			v.refreshing = true
			return v, v.fetchTablesData()
		}

	case tea.MouseMsg:
		switch v.mode {
		case ModeNormal:
			// Calculate panel boundaries for split view
			indexes := v.getSelectedTableIndexes()
			showIndexPanel := len(indexes) > 0

			// Table panel starts at Y=7 (after app header, status, title, header)
			tableStartY := 7
			var indexStartY int
			var tablePanelRows int

			if showIndexPanel {
				tablePanelRows = v.tablePanelHeight()
				// Index panel starts after table panel + index title(1) + index header(1)
				indexStartY = tableStartY + tablePanelRows + 2
			} else {
				tablePanelRows = v.tableHeight()
				indexStartY = -1 // No index panel
			}

			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if showIndexPanel && msg.Y >= indexStartY && v.focusPanel == FocusIndexes {
					v.moveIndexSelection(-1)
				} else {
					v.moveSelection(-1)
				}
			case tea.MouseButtonWheelDown:
				if showIndexPanel && msg.Y >= indexStartY && v.focusPanel == FocusIndexes {
					v.moveIndexSelection(1)
				} else {
					v.moveSelection(1)
				}
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					// Check if click is in index panel
					if showIndexPanel && msg.Y >= indexStartY {
						// Click in index panel
						v.focusPanel = FocusIndexes
						clickedRow := msg.Y - indexStartY
						if clickedRow >= 0 && clickedRow < len(indexes) {
							v.selectedIndex = v.indexScrollOffset + clickedRow
							if v.selectedIndex >= len(indexes) {
								v.selectedIndex = len(indexes) - 1
							}
						}
					} else if msg.Y >= tableStartY && msg.Y < tableStartY+tablePanelRows {
						// Click in table panel
						v.focusPanel = FocusTables
						clickedRow := msg.Y - tableStartY
						if clickedRow >= 0 {
							newIdx := v.scrollOffset + clickedRow
							if newIdx >= 0 && newIdx < len(v.treeItems) {
								v.selectedIdx = newIdx
								// Toggle expand/collapse if item is expandable
								v.toggleExpand()
							}
						}
					}
				}
			}
		case ModeHelp:
			switch msg.Button {
			case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
				// Could add help scroll here if needed
			case tea.MouseButtonLeft:
				// Click anywhere to close help
				if msg.Action == tea.MouseActionPress {
					v.mode = ModeNormal
				}
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		v.spinner, cmd = v.spinner.Update(msg)
		return v, cmd

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return v, nil
}

// handleKeyPress processes keyboard input in normal mode.
func (v *TablesView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "?", "esc", "q":
			v.mode = ModeNormal
		}
		return nil
	}

	// Normal mode keys
	switch key {
	// Help
	case "h", "?":
		v.mode = ModeHelp

	// Panel switching
	case "i":
		v.toggleFocusPanel()

	// Clipboard
	case "y":
		if v.focusPanel == FocusIndexes {
			v.copySelectedIndexName()
		} else if v.focusPanel == FocusTables {
			// Copy table name
			if v.selectedIdx >= 0 && v.selectedIdx < len(v.treeItems) {
				item := v.treeItems[v.selectedIdx]
				if item.Table != nil {
					fullName := fmt.Sprintf("%s.%s", item.Table.SchemaName, item.Table.Name)
					if err := v.clipboard.Write(fullName); err != nil {
						v.showToast("Failed to copy: "+err.Error(), true)
					} else {
						v.showToast("Copied: "+fullName, false)
					}
				}
			}
		}

	// Navigation - depends on focus panel
	case "j", "down":
		if v.focusPanel == FocusIndexes {
			v.moveIndexSelection(1)
		} else {
			v.moveSelection(1)
		}
	case "k", "up":
		if v.focusPanel == FocusIndexes {
			v.moveIndexSelection(-1)
		} else {
			v.moveSelection(-1)
		}
	case "g", "home":
		if v.focusPanel == FocusIndexes {
			v.selectedIndex = 0
			v.indexScrollOffset = 0
		} else {
			v.selectedIdx = 0
			v.scrollOffset = 0
		}
	case "G", "end":
		if v.focusPanel == FocusIndexes {
			indexes := v.getSelectedTableIndexes()
			v.selectedIndex = max(0, len(indexes)-1)
			v.ensureIndexVisible(len(indexes))
		} else {
			v.selectedIdx = max(0, len(v.treeItems)-1)
			v.ensureVisible()
		}
	case "ctrl+d", "pgdown":
		if v.focusPanel == FocusTables {
			v.pageDown()
		}
	case "ctrl+u", "pgup":
		if v.focusPanel == FocusTables {
			v.pageUp()
		}

	// Expand/collapse - only for tables panel
	case "enter", "right", "l":
		if v.focusPanel == FocusTables {
			v.toggleExpand()
		}
	case "left":
		if v.focusPanel == FocusTables {
			v.collapseOrMoveUp()
		}

	// System schema toggle
	case "P":
		v.showSystemSchemas = !v.showSystemSchemas
		v.buildTreeItems()
		// Reset selection if it's now invalid
		if v.selectedIdx >= len(v.treeItems) {
			v.selectedIdx = max(0, len(v.treeItems)-1)
		}
		v.ensureVisible()

	// Sorting
	case "s":
		v.cycleSortColumn()
	case "S":
		v.toggleSortDirection()

	// Refresh
	case "r":
		if !v.refreshing {
			v.refreshing = true
			return v.fetchTablesData()
		}
	}

	return nil
}

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

// getSelectedTableIndexes returns indexes for the currently selected table.
func (v *TablesView) getSelectedTableIndexes() []models.Index {
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return nil
	}
	item := v.treeItems[v.selectedIdx]
	if item.Table == nil {
		return nil
	}
	return v.getIndexesForTable(item.Table.OID)
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
	v.ensureIndexVisible(len(indexes))
}

// ensureIndexVisible adjusts index scroll offset to keep selection visible.
func (v *TablesView) ensureIndexVisible(totalIndexes int) {
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

// indexPanelHeight returns the height of the index panel.
func (v *TablesView) indexPanelHeight() int {
	// Index panel gets ~1/3 of available height, minimum 3 rows
	return max(3, (v.height-6)/3)
}

// tablePanelHeight returns the height of the table panel when index panel is shown.
func (v *TablesView) tablePanelHeight() int {
	// Table panel gets remaining height after index panel
	return v.height - 6 - v.indexPanelHeight() - 2 // -2 for index header
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

// moveSelection moves the selection by delta rows.
func (v *TablesView) moveSelection(delta int) {
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(v.treeItems) {
		v.selectedIdx = max(0, len(v.treeItems)-1)
	}
	v.ensureVisible()
}

// pageDown moves down by one page.
func (v *TablesView) pageDown() {
	pageSize := v.visibleTableHeight()
	v.selectedIdx += pageSize
	if v.selectedIdx >= len(v.treeItems) {
		v.selectedIdx = max(0, len(v.treeItems)-1)
	}
	v.ensureVisible()
}

// pageUp moves up by one page.
func (v *TablesView) pageUp() {
	pageSize := v.visibleTableHeight()
	v.selectedIdx -= pageSize
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	v.ensureVisible()
}

// visibleTableHeight returns the height of the table panel accounting for split view.
func (v *TablesView) visibleTableHeight() int {
	indexes := v.getSelectedTableIndexes()
	if len(indexes) > 0 {
		return v.tablePanelHeight()
	}
	return v.tableHeight()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (v *TablesView) ensureVisible() {
	visibleHeight := v.visibleTableHeight()
	if visibleHeight <= 0 {
		return
	}

	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	}
	if v.selectedIdx >= v.scrollOffset+visibleHeight {
		v.scrollOffset = v.selectedIdx - visibleHeight + 1
	}
}

// tableHeight returns the number of visible table rows.
func (v *TablesView) tableHeight() int {
	// height - status(1) - title(1) - header(1) - footer(1) - app chrome(2)
	return max(1, v.height-6)
}

// toggleExpand toggles expand/collapse for the selected item.
func (v *TablesView) toggleExpand() {
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return
	}

	item := &v.treeItems[v.selectedIdx]

	if item.IsSchema && item.Schema != nil {
		// Toggle schema expansion
		for i := range v.schemas {
			if v.schemas[i].OID == item.Schema.OID {
				v.schemas[i].Expanded = !v.schemas[i].Expanded
				break
			}
		}
		v.buildTreeItems()
	} else if item.IsTable && item.Table != nil && item.Table.IsPartitioned {
		// Toggle partition expansion for partitioned tables
		if table, ok := v.tablesByOID[item.Table.OID]; ok {
			table.Expanded = !table.Expanded
		}
		v.buildTreeItems()
	}

	// Ensure selection stays valid
	if v.selectedIdx >= len(v.treeItems) {
		v.selectedIdx = max(0, len(v.treeItems)-1)
	}
}

// collapseOrMoveUp collapses the current item or moves to parent.
func (v *TablesView) collapseOrMoveUp() {
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return
	}

	item := &v.treeItems[v.selectedIdx]

	if item.IsSchema && item.Schema != nil && item.Schema.Expanded {
		// Collapse schema
		for i := range v.schemas {
			if v.schemas[i].OID == item.Schema.OID {
				v.schemas[i].Expanded = false
				break
			}
		}
		v.buildTreeItems()
	} else if item.IsTable && item.Table != nil && item.Table.Expanded && item.Table.IsPartitioned {
		// Collapse partitioned table
		if table, ok := v.tablesByOID[item.Table.OID]; ok {
			table.Expanded = false
		}
		v.buildTreeItems()
	} else if item.IsPartition || item.IsTable {
		// Move to parent schema
		for i := v.selectedIdx - 1; i >= 0; i-- {
			if v.treeItems[i].IsSchema {
				v.selectedIdx = i
				v.ensureVisible()
				break
			}
		}
	}
}

// View renders the tables view.
func (v *TablesView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for overlay modes
	if v.mode == ModeHelp {
		return v.renderHelp()
	}

	return v.renderMainView()
}

// renderMainView renders the main tree table view.
func (v *TablesView) renderMainView() string {
	// Status bar
	statusBar := v.renderStatusBar()

	// Title
	title := v.renderTitle()

	// Show loading/error state
	if v.loading {
		content := lipgloss.NewStyle().
			Width(v.width - 2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Render(v.spinner.View() + " Loading tables...")
		footer := v.renderFooter()
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, content, footer)
	}

	if v.err != nil {
		content := lipgloss.NewStyle().
			Width(v.width - 2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorCriticalFg).
			Render("Error: " + v.err.Error())
		footer := v.renderFooter()
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, content, footer)
	}

	// Column headers
	header := v.renderHeader()

	// Check if we should show the index panel (when a table is selected)
	indexes := v.getSelectedTableIndexes()
	showIndexPanel := len(indexes) > 0

	if showIndexPanel {
		// Split view: table panel + index panel
		tablePanel := v.renderTableSplit()
		indexPanel := v.renderIndexPanel(indexes)
		footer := v.renderFooter()
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, header, tablePanel, indexPanel, footer)
	}

	// Table content (full height)
	table := v.renderTable()

	// Footer
	footer := v.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, header, table, footer)
}

// renderStatusBar renders the top status bar.
func (v *TablesView) renderStatusBar() string {
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	var staleIndicator string
	if !v.lastUpdate.IsZero() && time.Since(v.lastUpdate) > 35*time.Second {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	timestamp := styles.StatusTimeStyle.Render("Last refresh: " + v.lastUpdate.Format("15:04:05"))

	gap := v.width - lipgloss.Width(title) - lipgloss.Width(staleIndicator) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + staleIndicator + spaces + timestamp)
}

// renderTitle renders the view title.
func (v *TablesView) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)

	sysIndicator := ""
	if v.showSystemSchemas {
		sysIndicator = " [+sys]"
	}

	return titleStyle.Render("Tables" + sysIndicator)
}

// renderHeader renders the column headers with sort indicators.
func (v *TablesView) renderHeader() string {
	// Column widths
	nameWidth := 40
	sizeWidth := 10
	rowsWidth := 12
	bloatWidth := 10
	cacheWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - rowsWidth - bloatWidth - cacheWidth - 8
	if remaining > 20 {
		nameWidth = remaining
	}

	// Sort indicator
	sortIndicator := "▼" // descending (larger first)
	if v.sortAscending {
		sortIndicator = "▲" // ascending (smaller first)
	}

	// Build headers with sort indicator on active column
	nameHeader := "Schema/Table"
	sizeHeader := "Size"
	rowsHeader := "Rows"
	bloatHeader := "Bloat %"
	cacheHeader := "Cache %"

	switch v.sortColumn {
	case SortByName:
		nameHeader = nameHeader + " " + sortIndicator
	case SortBySize:
		sizeHeader = sizeHeader + " " + sortIndicator
	case SortByRows:
		rowsHeader = rowsHeader + " " + sortIndicator
	case SortByBloat:
		bloatHeader = bloatHeader + " " + sortIndicator
	case SortByCacheHit:
		cacheHeader = cacheHeader + " " + sortIndicator
	}

	headers := []string{
		padRight(nameHeader, nameWidth),
		padRight(sizeHeader, sizeWidth),
		padRight(rowsHeader, rowsWidth),
		padRight(bloatHeader, bloatWidth),
		padRight(cacheHeader, cacheWidth),
	}

	headerLine := strings.Join(headers, " ")
	return styles.TableHeaderStyle.Width(v.width - 2).Render(headerLine)
}

// renderTable renders the tree table content.
func (v *TablesView) renderTable() string {
	if len(v.treeItems) == 0 {
		emptyMsg := "No tables found"
		if !v.showSystemSchemas {
			emptyMsg = "No user tables found (press P to show system schemas)"
		}
		return lipgloss.NewStyle().
			Width(v.width - 2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorTextDim).
			Render(emptyMsg)
	}

	var rows []string
	tableHeight := v.tableHeight()
	endIdx := min(v.scrollOffset+tableHeight, len(v.treeItems))

	for i := v.scrollOffset; i < endIdx; i++ {
		item := v.treeItems[i]
		isSelected := i == v.selectedIdx
		row := v.renderTreeRow(item, isSelected)
		rows = append(rows, row)
	}

	// Pad to fill height
	for len(rows) < tableHeight {
		rows = append(rows, lipgloss.NewStyle().Width(v.width-2).Render(""))
	}

	return strings.Join(rows, "\n")
}

// renderTableSplit renders the tree table with reduced height for split view.
func (v *TablesView) renderTableSplit() string {
	if len(v.treeItems) == 0 {
		emptyMsg := "No tables found"
		if !v.showSystemSchemas {
			emptyMsg = "No user tables found (press P to show system schemas)"
		}
		return lipgloss.NewStyle().
			Width(v.width - 2).
			Height(v.tablePanelHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorTextDim).
			Render(emptyMsg)
	}

	var rows []string
	tableHeight := v.tablePanelHeight()
	endIdx := min(v.scrollOffset+tableHeight, len(v.treeItems))

	for i := v.scrollOffset; i < endIdx; i++ {
		item := v.treeItems[i]
		isSelected := i == v.selectedIdx && v.focusPanel == FocusTables
		row := v.renderTreeRow(item, isSelected)
		rows = append(rows, row)
	}

	// Pad to fill height
	for len(rows) < tableHeight {
		rows = append(rows, lipgloss.NewStyle().Width(v.width-2).Render(""))
	}

	return strings.Join(rows, "\n")
}

// renderIndexPanel renders the index statistics panel.
func (v *TablesView) renderIndexPanel(indexes []models.Index) string {
	// Panel title with focus indicator
	var titleStyle lipgloss.Style
	if v.focusPanel == FocusIndexes {
		titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorAccent)
	} else {
		titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorTextDim)
	}

	// Get selected table name for title
	tableName := ""
	if v.selectedIdx >= 0 && v.selectedIdx < len(v.treeItems) {
		item := v.treeItems[v.selectedIdx]
		if item.Table != nil {
			tableName = item.Table.Name
		}
	}

	title := titleStyle.Render(fmt.Sprintf("Indexes for %s [i to switch]", tableName))

	// Index header
	indexHeader := v.renderIndexHeader()

	// Index rows
	var rows []string
	panelHeight := v.indexPanelHeight()
	endIdx := min(v.indexScrollOffset+panelHeight, len(indexes))

	for i := v.indexScrollOffset; i < endIdx; i++ {
		idx := indexes[i]
		isSelected := i == v.selectedIndex && v.focusPanel == FocusIndexes
		row := v.renderIndexRow(idx, isSelected)
		rows = append(rows, row)
	}

	// Pad to fill height
	for len(rows) < panelHeight {
		rows = append(rows, lipgloss.NewStyle().Width(v.width-2).Render(""))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, title, indexHeader, content)
}

// renderIndexHeader renders the column headers for index panel.
func (v *TablesView) renderIndexHeader() string {
	nameWidth := 30
	sizeWidth := 10
	scansWidth := 12
	rowsWidth := 12
	cacheWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - scansWidth - rowsWidth - cacheWidth - 8
	if remaining > 20 {
		nameWidth = remaining
	}

	headers := []string{
		padRight("Index Name", nameWidth),
		padRight("Size", sizeWidth),
		padRight("Scans", scansWidth),
		padRight("Rows Read", rowsWidth),
		padRight("Cache %", cacheWidth),
	}

	headerLine := strings.Join(headers, " ")
	return styles.TableHeaderStyle.Width(v.width - 2).Render(headerLine)
}

// renderIndexRow renders a single index row with highlighting for unused indexes.
func (v *TablesView) renderIndexRow(idx models.Index, isSelected bool) string {
	nameWidth := 30
	sizeWidth := 10
	scansWidth := 12
	rowsWidth := 12
	cacheWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - scansWidth - rowsWidth - cacheWidth - 8
	if remaining > 20 {
		nameWidth = remaining
	}

	// Format index name with type indicators
	name := idx.Name
	if idx.IsPrimary {
		name = "🔑 " + name
	} else if idx.IsUnique {
		name = "◆ " + name
	}

	row := fmt.Sprintf("%s %s %s %s %s",
		padRight(truncateWithWidth(name, nameWidth-1), nameWidth),
		padRight(models.FormatBytes(idx.Size), sizeWidth),
		padRight(formatNumber(idx.ScanCount), scansWidth),
		padRight(formatNumber(idx.RowsRead), rowsWidth),
		padRight(fmt.Sprintf("%.1f%%", idx.CacheHitRatio), cacheWidth),
	)

	// Apply styling
	if isSelected {
		return styles.TableSelectedStyle.Width(v.width - 2).Render(row)
	}

	// Yellow highlighting for unused indexes (ScanCount == 0)
	if idx.IsUnused {
		return lipgloss.NewStyle().
			Foreground(styles.ColorIdleTxn). // Yellow for warning
			Width(v.width - 2).
			Render(row)
	}

	return styles.TableCellStyle.Width(v.width - 2).Render(row)
}

// renderTreeRow renders a single tree row.
func (v *TablesView) renderTreeRow(item TreeItem, isSelected bool) string {
	// Column widths (must match renderHeader)
	nameWidth := 40
	sizeWidth := 10
	rowsWidth := 12
	bloatWidth := 10
	cacheWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - rowsWidth - bloatWidth - cacheWidth - 8
	if remaining > 20 {
		nameWidth = remaining
	}

	var name, size, rowCount, bloat, cacheHit string
	var bloatPct float64

	if item.IsSchema {
		// Schema row
		prefix := "▶ "
		if item.Expanded {
			prefix = "▼ "
		}
		name = prefix + item.Schema.Name
		size = ""
		rowCount = ""
		bloat = ""
		cacheHit = ""
	} else if item.IsTable || item.IsPartition {
		// Table or partition row
		var prefix string
		if item.IsPartition {
			prefix = "      └─ "
		} else if item.IsLast {
			prefix = "   └─ "
		} else {
			prefix = "   ├─ "
		}

		// Add partition indicator
		if item.Table.IsPartitioned {
			expandIcon := "▶"
			if item.Expanded {
				expandIcon = "▼"
			}
			prefix += expandIcon + " "
		}

		name = prefix + item.Table.Name
		size = models.FormatBytes(item.Table.TotalSize)
		rowCount = formatNumber(item.Table.RowCount)
		bloatPct = item.Table.BloatPct
		if item.Table.BloatEstimated {
			bloat = fmt.Sprintf("~%.1f%%", bloatPct)
		} else {
			bloat = fmt.Sprintf("%.1f%%", bloatPct)
		}
		cacheHit = fmt.Sprintf("%.1f%%", item.Table.CacheHitRatio)
	}

	// Truncate name if too long
	displayName := truncateWithWidth(name, nameWidth-1)

	// Format bloat with color coding for table rows
	bloatStr := padRight(bloat, bloatWidth)
	if (item.IsTable || item.IsPartition) && !isSelected {
		if bloatPct > 20 {
			// High bloat: red
			bloatStr = lipgloss.NewStyle().Foreground(styles.ColorError).Render(padRight(bloat, bloatWidth))
		} else if bloatPct > 10 {
			// Moderate bloat: yellow
			bloatStr = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(padRight(bloat, bloatWidth))
		}
	}

	row := fmt.Sprintf("%s %s %s %s %s",
		padRight(displayName, nameWidth),
		padRight(size, sizeWidth),
		padRight(rowCount, rowsWidth),
		bloatStr,
		padRight(cacheHit, cacheWidth),
	)

	// Apply styling
	if isSelected {
		// Reformat without color for selected row
		row = fmt.Sprintf("%s %s %s %s %s",
			padRight(displayName, nameWidth),
			padRight(size, sizeWidth),
			padRight(rowCount, rowsWidth),
			padRight(bloat, bloatWidth),
			padRight(cacheHit, cacheWidth),
		)
		return styles.TableSelectedStyle.Width(v.width - 2).Render(row)
	}

	// Muted style for system schemas and partitions
	if item.IsSchema && item.Schema != nil && item.Schema.IsSystem {
		return lipgloss.NewStyle().
			Foreground(styles.ColorTextDim).
			Width(v.width - 2).
			Render(row)
	}

	if item.IsPartition {
		return lipgloss.NewStyle().
			Foreground(styles.ColorTextDim).
			Width(v.width - 2).
			Render(row)
	}

	return styles.TableCellStyle.Width(v.width - 2).Render(row)
}

// renderFooter renders the bottom footer.
func (v *TablesView) renderFooter() string {
	var hints string

	// Show toast message if recent (within 3 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		if v.focusPanel == FocusIndexes {
			hints = styles.FooterHintStyle.Render("[j/k]nav [i]tables [y]copy [r]efresh [h]elp")
		} else {
			hints = styles.FooterHintStyle.Render("[j/k]nav [Enter]expand [i]ndex [y]copy [s/S]ort [P]sys [r]efresh [h]elp")
		}
	}

	count := fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.treeItems)), len(v.treeItems))
	rightSide := styles.FooterCountStyle.Render(count)

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
func (v *TablesView) renderHelp() string {
	helpContent := `Tables View - Keyboard Shortcuts

Navigation
  j / ↓         Move down
  k / ↑         Move up
  g / Home      Go to top
  G / End       Go to bottom
  Ctrl+D        Page down
  Ctrl+U        Page up

Tree
  Enter / →     Expand/collapse schema or partitions
  ←             Collapse or move to parent

Index Panel
  i             Toggle focus between tables and indexes
  y             Copy selected table/index name to clipboard

  Index Display:
    🔑 name     Primary key index
    ◆ name      Unique index
    yellow      Unused index (0 scans) - candidate for removal

  Indexes are sorted: primary keys first, then unique, then regular

Sorting (Tables)
  s             Cycle sort column (Name → Size → Rows → Bloat → Cache)
  S             Toggle sort direction (asc/desc)

Bloat Display:
  ~X.X%         Estimated bloat (from dead rows ratio)
  red           High bloat (>20%) - consider VACUUM
  yellow        Moderate bloat (10-20%)

Display
  P             Toggle system schemas
  r             Refresh data

General
  h / ?         Show this help
  Esc / q       Close help

Press any key to close this help`

	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(65)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		helpStyle.Render(helpContent),
	)
}

// SetSize sets the dimensions of the view.
func (v *TablesView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *TablesView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *TablesView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *TablesView) SetReadOnly(readOnly bool) {
	v.readonlyMode = readOnly
}

// SetPool sets the database connection pool.
func (v *TablesView) SetPool(pool *pgxpool.Pool) {
	v.pool = pool
}

// IsInputMode returns true if in an input mode.
func (v *TablesView) IsInputMode() bool {
	return v.mode == ModeDetails || v.mode == ModeHelp
}

// showToast displays a toast message.
func (v *TablesView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// Helper functions

func truncateWithWidth(s string, maxWidth int) string {
	w := runewidth.StringWidth(s)
	if w <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth-3, "...")
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
