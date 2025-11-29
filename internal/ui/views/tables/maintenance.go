package tables

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
)

// promptMaintenance initiates a maintenance operation with confirmation.
// Returns nil if readonly mode or no table selected.
func (v *TablesView) promptMaintenance(mode TablesMode) tea.Cmd {
	// Check readonly mode
	if v.readonlyMode {
		var opName string
		switch mode {
		case ModeConfirmVacuum:
			opName = "VACUUM"
		case ModeConfirmAnalyze:
			opName = "ANALYZE"
		case ModeConfirmReindex:
			opName = "REINDEX"
		case ModeConfirmReindexConcurrently:
			opName = "REINDEX CONCURRENTLY"
		}
		v.showToast(fmt.Sprintf("%s blocked: read-only mode", opName), true)
		return nil
	}

	// Must have a table selected
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return nil
	}
	item := v.treeItems[v.selectedIdx]
	if item.Table == nil {
		v.showToast("Select a table first", true)
		return nil
	}

	// Store target and show confirmation
	v.maintenanceTarget = item.Table
	v.mode = mode
	return nil
}

// executeMaintenance executes the pending maintenance operation.
func (v *TablesView) executeMaintenance() tea.Cmd {
	if v.maintenanceTarget == nil || v.pool == nil {
		return nil
	}

	target := v.maintenanceTarget
	tableName := fmt.Sprintf("%s.%s", target.SchemaName, target.Name)

	// Capture pending flags before we reset them
	vacuumFull := v.pendingVacuumFull
	vacuumAnalyze := v.pendingVacuumAnalyze
	v.pendingVacuumFull = false
	v.pendingVacuumAnalyze = false

	// Determine operation type
	var opType models.OperationType
	switch v.mode {
	case ModeConfirmVacuum:
		switch {
		case vacuumFull:
			opType = models.OpVacuumFull
		case vacuumAnalyze:
			opType = models.OpVacuumAnalyze
		default:
			opType = models.OpVacuum
		}
	case ModeConfirmAnalyze:
		opType = models.OpAnalyze
	case ModeConfirmReindex:
		opType = models.OpReindexTable
	case ModeConfirmReindexConcurrently:
		opType = models.OpReindexConcurrently
	default:
		return nil
	}

	// Create the operation object
	operation := &models.MaintenanceOperation{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:         opType,
		TargetSchema: target.SchemaName,
		TargetTable:  target.Name,
		Status:       models.StatusRunning,
		StartedAt:    time.Now(),
	}

	// Store operation and switch to progress mode immediately
	v.currentOperation = operation
	v.mode = ModeOperationProgress
	v.pollingInProgress = true // Mark poll as in progress

	logger.Debug("starting maintenance operation",
		"type", opType,
		"table", tableName)

	// Create the operation command
	operationCmd := v.createOperationCmd(target, tableName, opType, vacuumFull, vacuumAnalyze)

	// Start polling for progress immediately and schedule regular ticks
	return tea.Batch(v.progressTick(), v.pollProgress(), operationCmd)
}

// createOperationCmd creates the command to execute the maintenance operation.
func (v *TablesView) createOperationCmd(target *models.Table, tableName string, opType models.OperationType, vacuumFull, vacuumAnalyze bool) tea.Cmd {
	return func() tea.Msg {
		startTime := time.Now()
		var err error
		var opName string

		switch opType {
		case models.OpVacuum, models.OpVacuumFull, models.OpVacuumAnalyze:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			opts := queries.VacuumOptions{
				Full:    vacuumFull,
				Analyze: vacuumAnalyze,
			}
			switch {
			case vacuumFull && vacuumAnalyze:
				opName = "VACUUM FULL ANALYZE"
			case vacuumFull:
				opName = "VACUUM FULL"
			case vacuumAnalyze:
				opName = "VACUUM ANALYZE"
			default:
				opName = "VACUUM"
			}
			err = queries.ExecuteVacuumWithOptions(ctx, v.pool, target.SchemaName, target.Name, opts)

		case models.OpAnalyze:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			opName = "ANALYZE"
			err = queries.ExecuteAnalyze(ctx, v.pool, target.SchemaName, target.Name)

		case models.OpReindexTable:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			opName = "REINDEX"
			err = queries.ExecuteReindex(ctx, v.pool, target.SchemaName, target.Name)

		case models.OpReindexConcurrently:
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
			defer cancel()
			opName = "REINDEX CONCURRENTLY"
			err = queries.ExecuteReindexWithOptions(ctx, v.pool, target.SchemaName, target.Name, queries.ReindexOptions{Concurrently: true})
		}

		return MaintenanceResultMsg{
			Operation: opName,
			TableName: tableName,
			Success:   err == nil,
			Error:     err,
			Elapsed:   time.Since(startTime),
		}
	}
}

// openOperationsMenu opens the maintenance operations menu.
func (v *TablesView) openOperationsMenu() tea.Cmd {
	// Check readonly mode
	if v.readonlyMode {
		v.showToast("Operations blocked: read-only mode", true)
		return nil
	}

	// Must have a table selected
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		return nil
	}
	item := v.treeItems[v.selectedIdx]
	if item.Table == nil {
		v.showToast("Select a table first", true)
		return nil
	}

	// Create and show operations menu
	v.maintenanceTarget = item.Table
	v.operationsMenu = NewOperationsMenu(item.Table, v.readonlyMode)
	v.mode = ModeOperationsMenu
	return nil
}

// handleOperationsMenuKey handles key presses in the operations menu.
func (v *TablesView) handleOperationsMenuKey(key string) tea.Cmd {
	switch key {
	case "j", "down":
		v.operationsMenu.MoveDown()
	case "k", "up":
		v.operationsMenu.MoveUp()
	case "enter":
		selected := v.operationsMenu.SelectedItem()
		if selected != nil && !selected.Disabled {
			// Map operation type to confirmation mode
			switch selected.Operation {
			case models.OpVacuum:
				v.mode = ModeConfirmVacuum
			case models.OpVacuumFull:
				v.mode = ModeConfirmVacuum // Uses same confirm but with Full option
				// Store that we want VACUUM FULL
				v.pendingVacuumFull = true
			case models.OpVacuumAnalyze:
				v.mode = ModeConfirmVacuum // Uses same confirm but with Analyze option
				v.pendingVacuumAnalyze = true
			case models.OpAnalyze:
				v.mode = ModeConfirmAnalyze
			case models.OpReindexTable:
				v.mode = ModeConfirmReindex
			case models.OpReindexConcurrently:
				v.mode = ModeConfirmReindexConcurrently
			}
			v.operationsMenu = nil
		}
	case "esc", "q":
		v.mode = ModeNormal
		v.operationsMenu = nil
		v.maintenanceTarget = nil
	}
	return nil
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
	// Fixed elements: status(3 w/border) + title(1) + header(2 w/border) + footer(3 w/border) = 9
	return max(1, v.height-9)
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
