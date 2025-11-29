package tables

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui/styles"
)

func (v *TablesView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Check for overlay modes
	if v.mode == ModeHelp {
		return HelpOverlay(v.width, v.height, v.pgstattupleAvailable)
	}

	if v.mode == ModeCopyMenu {
		return v.renderCopyMenu()
	}

	if v.mode == ModeDetails {
		return v.renderDetails()
	}

	if v.mode == ModeConfirmInstall {
		return v.renderConfirmInstall()
	}

	// Show install prompt if checked, not available, and user hasn't dismissed it
	if v.pgstattupleChecked && !v.pgstattupleAvailable && !v.installPromptShown {
		return v.renderConfirmInstall()
	}

	// Maintenance confirmation dialogs
	if v.mode == ModeConfirmVacuum || v.mode == ModeConfirmAnalyze || v.mode == ModeConfirmReindex || v.mode == ModeConfirmReindexConcurrently {
		return v.renderMaintenanceConfirm()
	}

	// Operations menu
	if v.mode == ModeOperationsMenu {
		return v.renderOperationsMenu()
	}

	// Operation progress
	if v.mode == ModeOperationProgress {
		return v.renderOperationProgress()
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
			Width(v.width-2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Render(v.spinner.View() + " Loading tables...")
		footer := v.renderFooter()
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, content, footer)
	}

	if v.err != nil {
		content := lipgloss.NewStyle().
			Width(v.width-2).
			Height(v.tableHeight()).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorCriticalFg).
			Render("Error: " + v.err.Error())
		footer := v.renderFooter()
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, title, content, footer)
	}

	// Column headers
	header := v.renderHeader()

	// Calculate view header height for mouse coordinate translation
	// This is the number of rows from view top to first data row
	v.viewHeaderHeight = lipgloss.Height(statusBar) + lipgloss.Height(title) + lipgloss.Height(header)

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

	timestamp := styles.StatusTimeStyle.Render(v.lastUpdate.Format("15:04:05"))

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
	bloatWidth := 8
	cacheWidth := 8
	vacuumWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - rowsWidth - bloatWidth - cacheWidth - vacuumWidth - 10
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
	bloatHeader := "Bloat"
	cacheHeader := "Cache"
	vacuumHeader := "Vacuum"

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
		padRight(vacuumHeader, vacuumWidth),
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
			Width(v.width-2).
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
			Width(v.width-2).
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

	// Sort indicator
	sortIndicator := "▼" // descending (larger first)
	if v.indexSortAscending {
		sortIndicator = "▲" // ascending (smaller first)
	}

	// Column headers with sort indicator on active column
	nameHeader := "Index Name"
	sizeHeader := "Size"
	scansHeader := "Scans"
	rowsHeader := "Rows Read"
	cacheHeader := "Cache %"

	switch v.indexSortColumn {
	case IndexSortByName:
		nameHeader = nameHeader + " " + sortIndicator
	case IndexSortBySize:
		sizeHeader = sizeHeader + " " + sortIndicator
	case IndexSortByScans:
		scansHeader = scansHeader + " " + sortIndicator
	case IndexSortByRowsRead:
		rowsHeader = rowsHeader + " " + sortIndicator
	case IndexSortByCacheHit:
		cacheHeader = cacheHeader + " " + sortIndicator
	}

	headers := []string{
		padRight(nameHeader, nameWidth),
		padRight(sizeHeader, sizeWidth),
		padRight(scansHeader, scansWidth),
		padRight(rowsHeader, rowsWidth),
		padRight(cacheHeader, cacheWidth),
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
	bloatWidth := 8
	cacheWidth := 8
	vacuumWidth := 10

	// Adjust name width based on terminal
	remaining := v.width - sizeWidth - rowsWidth - bloatWidth - cacheWidth - vacuumWidth - 10
	if remaining > 20 {
		nameWidth = remaining
	}

	var name, size, rowCount, bloat, cacheHit, vacuum string
	var bloatPct float64
	var vacuumIndicator queries.VacuumIndicator

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
		vacuum = ""
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
			bloat = fmt.Sprintf("~%.0f%%", bloatPct)
		} else {
			bloat = fmt.Sprintf("%.0f%%", bloatPct)
		}
		cacheHit = fmt.Sprintf("%.0f%%", item.Table.CacheHitRatio)
		// Format vacuum timestamp
		vacuum = queries.FormatVacuumTimestamp(queries.MaxVacuumTime(item.Table.LastVacuum, item.Table.LastAutovacuum))
		vacuumIndicator = queries.GetVacuumStatusIndicator(item.Table.LastVacuum, item.Table.LastAutovacuum, queries.DefaultStaleVacuumConfig())
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

	// Format vacuum with color coding for table rows
	vacuumStr := padRight(vacuum, vacuumWidth)
	if (item.IsTable || item.IsPartition) && !isSelected {
		switch vacuumIndicator {
		case queries.VacuumIndicatorCritical:
			// Critical: red (never vacuumed or overdue)
			vacuumStr = lipgloss.NewStyle().Foreground(styles.ColorError).Render(padRight(vacuum, vacuumWidth))
		case queries.VacuumIndicatorWarning:
			// Warning: yellow (approaching threshold)
			vacuumStr = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(padRight(vacuum, vacuumWidth))
		}
	}

	row := fmt.Sprintf("%s %s %s %s %s %s",
		padRight(displayName, nameWidth),
		padRight(size, sizeWidth),
		padRight(rowCount, rowsWidth),
		bloatStr,
		padRight(cacheHit, cacheWidth),
		vacuumStr,
	)

	// Apply styling
	if isSelected {
		// Reformat without color for selected row
		row = fmt.Sprintf("%s %s %s %s %s %s",
			padRight(displayName, nameWidth),
			padRight(size, sizeWidth),
			padRight(rowCount, rowsWidth),
			padRight(bloat, bloatWidth),
			padRight(cacheHit, cacheWidth),
			padRight(vacuum, vacuumWidth),
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

	// Show toast message if recent (within 5 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 5*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorError)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorSuccess)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		// Check if index panel is visible (to show resize hint)
		indexes := v.getSelectedTableIndexes()
		showIndexPanel := len(indexes) > 0

		if v.focusPanel == FocusIndexes {
			hints = styles.FooterHintStyle.Render("[j/k]nav [i]tables [y]copy [s/S]ort [-/+]resize [R]efresh [h]elp")
		} else if showIndexPanel {
			hints = styles.FooterHintStyle.Render("[j/k]nav [Enter/d]details [i]ndex [y]copy [s/S]ort [-/+]resize [x]ops [h]elp")
		} else {
			hints = styles.FooterHintStyle.Render("[j/k]nav [Enter/d]details [i]ndex [y]copy [s/S]ort [P]sys [x]ops [h]elp")
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

// renderCopyMenu renders the SQL copy menu overlay on top of details view.
func (v *TablesView) renderCopyMenu() string {
	if v.details == nil {
		return v.renderDetails()
	}

	// Build the menu content
	tableName := fmt.Sprintf("%s.%s", v.details.Table.SchemaName, v.details.Table.Name)
	menuContent := fmt.Sprintf(`Copy SQL for: %s

  [n] Table name
  [s] SELECT (with LIMIT 100)
  [i] INSERT template
  [u] UPDATE template
  [d] DELETE template

  [Esc] Cancel`, tableName)

	// Create dialog style
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(40)

	dialog := dialogStyle.Render(menuContent)

	// Center the dialog over the details view
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// renderConfirmInstall renders the pgstattuple install confirmation dialog.
func (v *TablesView) renderConfirmInstall() string {
	dialogContent := `Install pgstattuple Extension?

The pgstattuple extension provides accurate
table bloat measurements. Without it, bloat
values are estimated from dead row counts
(shown with ~ prefix).

This will execute:
  CREATE EXTENSION IF NOT EXISTS pgstattuple

[y] or [Enter] = Install
[n] or [Esc]   = Skip (won't ask again)`

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(50)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(dialogContent),
	)
}

// renderMaintenanceConfirm renders the maintenance operation confirmation dialog.
func (v *TablesView) renderMaintenanceConfirm() string {
	if v.maintenanceTarget == nil {
		return v.renderMainView()
	}

	tableName := fmt.Sprintf("%s.%s", v.maintenanceTarget.SchemaName, v.maintenanceTarget.Name)

	var operation, description, command string
	switch v.mode {
	case ModeConfirmVacuum:
		operation = "VACUUM"
		description = "Reclaims storage from dead tuples and updates\nvisibility map. May take a while on large tables."
		command = fmt.Sprintf("VACUUM %s", tableName)
	case ModeConfirmAnalyze:
		operation = "ANALYZE"
		description = "Updates table statistics for the query planner.\nFast operation, safe to run frequently."
		command = fmt.Sprintf("ANALYZE %s", tableName)
	case ModeConfirmReindex:
		operation = "REINDEX"
		description = "Rebuilds all indexes on the table. This will\nlock the table for writes during the operation."
		command = fmt.Sprintf("REINDEX TABLE %s", tableName)
	case ModeConfirmReindexConcurrently:
		operation = "REINDEX CONCURRENTLY"
		description = "Rebuilds all indexes without blocking writes.\nSlower than regular REINDEX but non-blocking."
		command = fmt.Sprintf("REINDEX TABLE CONCURRENTLY %s", tableName)
	}

	dialogContent := fmt.Sprintf(`%s %s?

%s

This will execute:
  %s

[y] or [Enter] = Execute
[n] or [Esc]   = Cancel`, operation, tableName, description, command)

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(55)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(dialogContent),
	)
}

// renderOperationsMenu renders the operations menu overlay.
func (v *TablesView) renderOperationsMenu() string {
	if v.operationsMenu == nil {
		return v.renderMainView()
	}

	// Get the menu content from the operations menu component
	menuContent := v.operationsMenu.View()

	// Wrap in a styled dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(60)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(menuContent),
	)
}

// renderOperationProgress renders the operation progress overlay.
func (v *TablesView) renderOperationProgress() string {
	if v.currentOperation == nil {
		return v.renderMainView()
	}

	var b strings.Builder
	op := v.currentOperation

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render(fmt.Sprintf("%s %s.%s", op.Type, op.TargetSchema, op.TargetTable)))
	b.WriteString("\n\n")

	// Progress bar and percentage
	if op.Progress != nil && op.Progress.HeapBlksTotal > 0 {
		percent := op.Progress.CalculatePercent()
		bar := v.renderProgressBar(percent, 40)
		b.WriteString(fmt.Sprintf("%s %5.1f%%\n", bar, percent))

		// Phase
		if op.Progress.Phase != "" {
			phaseStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
			b.WriteString(phaseStyle.Render(fmt.Sprintf("Phase: %s\n", op.Progress.Phase)))
		}

		// Blocks processed
		blocksStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
		b.WriteString(blocksStyle.Render(fmt.Sprintf("Blocks: %d / %d\n",
			op.Progress.HeapBlksScanned, op.Progress.HeapBlksTotal)))
	} else {
		// No progress tracking available (ANALYZE, REINDEX, or operation just started)
		spinnerChars := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		spinnerIdx := int(time.Now().UnixNano()/int64(100*time.Millisecond)) % len(spinnerChars)
		b.WriteString(fmt.Sprintf("%c Running...\n", spinnerChars[spinnerIdx]))
	}

	// Duration
	elapsed := time.Since(op.StartedAt)
	durationStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString("\n")
	b.WriteString(durationStyle.Render(fmt.Sprintf("Elapsed: %s\n", v.formatDuration(elapsed))))

	// Footer hints
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[Esc] Continue in background"))

	// Wrap in dialog
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(55)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(b.String()),
	)
}

// renderProgressBar creates an ASCII progress bar.
func (v *TablesView) renderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 20
	}
	filled := int(float64(width) * percent / 100)
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return "[" + bar + "]"
}

// formatDuration formats a duration for display.
func (v *TablesView) formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}
