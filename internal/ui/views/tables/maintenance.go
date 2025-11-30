package tables

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"

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

	// Check if operation already in progress (T075: single-operation enforcement)
	if v.currentOperation != nil {
		v.showToast("Another maintenance operation is in progress. Wait for it to complete or cancel it.", true)
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

		// T080: Check connection is available before starting
		if v.pool == nil {
			return MaintenanceResultMsg{
				Operation: string(opType),
				TableName: tableName,
				Success:   false,
				Error:     queries.ErrConnectionLost,
				Elapsed:   time.Since(startTime),
			}
		}

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

		// T080: Wrap connection-related errors with clearer message
		if err != nil && isConnectionError(err) {
			err = fmt.Errorf("%w: %v", queries.ErrConnectionLost, err)
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

// isConnectionError checks if an error is connection-related (T080).
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "reset by peer") ||
		strings.Contains(errStr, "broken pipe")
}

// openOperationHistory opens the operation history overlay.
func (v *TablesView) openOperationHistory() {
	if v.operationHistory == nil || len(v.operationHistory.Operations) == 0 {
		v.showToast("No operations in history", false)
		return
	}
	v.historySelectedIdx = 0
	v.mode = ModeOperationHistory
}

// handleHistoryKey handles key presses in the operation history overlay.
func (v *TablesView) handleHistoryKey(key string) tea.Cmd {
	if v.operationHistory == nil {
		v.mode = ModeNormal
		return nil
	}

	ops := v.operationHistory.Recent(100)
	maxIdx := len(ops) - 1

	switch key {
	case "j", "down":
		if v.historySelectedIdx < maxIdx {
			v.historySelectedIdx++
		}
	case "k", "up":
		if v.historySelectedIdx > 0 {
			v.historySelectedIdx--
		}
	case "g", "home":
		v.historySelectedIdx = 0
	case "G", "end":
		v.historySelectedIdx = maxIdx
	case "esc", "q", "H":
		v.mode = ModeNormal
	}
	return nil
}

// openOperationsMenu opens the maintenance operations menu.
func (v *TablesView) openOperationsMenu() tea.Cmd {
	// Check if operation already in progress (T075: single-operation enforcement)
	if v.currentOperation != nil {
		v.showToast("Another maintenance operation is in progress. Wait for it to complete or cancel it.", true)
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

	// Create and show operations menu (read-only status affects which items are enabled)
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
		logger.Debug("handleOperationsMenuKey: enter pressed",
			"selected", selected,
			"disabled", selected != nil && selected.Disabled)
		if selected != nil && !selected.Disabled {
			// Map operation type to confirmation mode
			switch selected.Operation {
			case models.OpCheckBloat:
				// CHECK BLOAT is read-only - execute immediately without confirmation
				logger.Debug("handleOperationsMenuKey: executing CHECK BLOAT")
				v.mode = ModeNormal
				v.operationsMenu = nil
				return v.executeCheckBloat()
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

// openPermissionsDialog opens the permissions dialog for the selected table.
func (v *TablesView) openPermissionsDialog() tea.Cmd {
	// Must have a table selected
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.treeItems) {
		logger.Debug("openPermissionsDialog: no item selected")
		return nil
	}
	item := v.treeItems[v.selectedIdx]
	if item.Table == nil {
		v.showToast("Select a table first", true)
		logger.Debug("openPermissionsDialog: selected item is not a table")
		return nil
	}

	logger.Debug("openPermissionsDialog: opening for table",
		"schema", item.Table.SchemaName,
		"table", item.Table.Name,
		"oid", item.Table.OID)

	// Create permissions dialog
	v.permissionsDialog = NewPermissionsDialog(
		item.Table.OID,
		item.Table.SchemaName,
		item.Table.Name,
		min(80, v.width-4),
		min(25, v.height-4),
		v.readonlyMode,
	)
	v.mode = ModePermissions

	// Fetch permissions and role names
	return v.fetchPermissionsData(item.Table.OID)
}

// fetchPermissionsData returns a command to fetch permissions for a table.
func (v *TablesView) fetchPermissionsData(tableOID uint32) tea.Cmd {
	logger.Debug("fetchPermissionsData: creating command", "tableOID", tableOID)
	return func() tea.Msg {
		logger.Debug("fetchPermissionsData: executing command", "tableOID", tableOID)
		if v.pool == nil {
			logger.Error("fetchPermissionsData: pool is nil")
			return PermissionsDataMsg{
				TableOID: tableOID,
				Error:    fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Fetch permissions
		logger.Debug("fetchPermissionsData: fetching permissions", "tableOID", tableOID)
		permissions, err := queries.GetTablePermissions(ctx, v.pool, tableOID)
		if err != nil {
			logger.Error("fetchPermissionsData: GetTablePermissions failed", "error", err)
			return PermissionsDataMsg{
				TableOID: tableOID,
				Error:    fmt.Errorf("fetch permissions: %w", err),
			}
		}
		logger.Debug("fetchPermissionsData: got permissions", "count", len(permissions))

		// Fetch role names for grant dialog
		roleNames, err := queries.GetAllRoleNames(ctx, v.pool)
		if err != nil {
			// Non-fatal: we can still show permissions
			logger.Debug("fetchPermissionsData: GetAllRoleNames failed (non-fatal)", "error", err)
			roleNames = nil
		} else {
			logger.Debug("fetchPermissionsData: got role names", "count", len(roleNames))
		}

		return PermissionsDataMsg{
			TableOID:    tableOID,
			Permissions: permissions,
			RoleNames:   roleNames,
		}
	}
}

// handlePermissionsKey handles key presses in the permissions dialog.
func (v *TablesView) handlePermissionsKey(key string) tea.Cmd {
	if v.permissionsDialog == nil {
		v.mode = ModeNormal
		return nil
	}

	done, cmd := v.permissionsDialog.Update(key)
	if done {
		v.permissionsDialog = nil
		v.mode = ModeNormal
		return nil
	}
	return cmd
}

// executeGrant executes a GRANT operation.
func (v *TablesView) executeGrant(schema, table, role, privilege string, withGrantOption bool) tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return GrantPermissionResultMsg{
				Schema:    schema,
				Table:     table,
				Role:      role,
				Privilege: privilege,
				Success:   false,
				Error:     fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := queries.GrantTablePrivilege(ctx, v.pool, schema, table, role, privilege, withGrantOption)
		if err != nil {
			logger.Error("GRANT failed", "error", err, "schema", schema, "table", table, "role", role, "privilege", privilege)
			return GrantPermissionResultMsg{
				Schema:    schema,
				Table:     table,
				Role:      role,
				Privilege: privilege,
				Success:   false,
				Error:     err,
			}
		}

		logger.Debug("GRANT succeeded", "schema", schema, "table", table, "role", role, "privilege", privilege)
		return GrantPermissionResultMsg{
			Schema:    schema,
			Table:     table,
			Role:      role,
			Privilege: privilege,
			Success:   true,
		}
	}
}

// executeRevoke executes a REVOKE operation.
func (v *TablesView) executeRevoke(schema, table, role, privilege string, cascade bool) tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return RevokePermissionResultMsg{
				Schema:    schema,
				Table:     table,
				Role:      role,
				Privilege: privilege,
				Success:   false,
				Error:     fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := queries.RevokeTablePrivilege(ctx, v.pool, schema, table, role, privilege, cascade)
		if err != nil {
			logger.Error("REVOKE failed", "error", err, "schema", schema, "table", table, "role", role, "privilege", privilege)
			return RevokePermissionResultMsg{
				Schema:    schema,
				Table:     table,
				Role:      role,
				Privilege: privilege,
				Success:   false,
				Error:     err,
			}
		}

		logger.Debug("REVOKE succeeded", "schema", schema, "table", table, "role", role, "privilege", privilege)
		return RevokePermissionResultMsg{
			Schema:    schema,
			Table:     table,
			Role:      role,
			Privilege: privilege,
			Success:   true,
		}
	}
}

// cancelOperation attempts to cancel the currently running operation.
func (v *TablesView) cancelOperation() tea.Cmd {
	if v.currentOperation == nil || v.maintenanceTarget == nil || v.pool == nil {
		v.mode = ModeNormal
		return nil
	}

	op := v.currentOperation
	target := v.maintenanceTarget

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// First, find the PID of the running operation
		var pid int
		var progress *models.OperationProgress
		var err error

		switch op.Type {
		case models.OpVacuum, models.OpVacuumAnalyze:
			progress, err = queries.GetVacuumProgress(ctx, v.pool, target.SchemaName, target.Name)
		case models.OpVacuumFull:
			progress, err = queries.GetVacuumFullProgress(ctx, v.pool, target.SchemaName, target.Name)
		default:
			// For operations without progress tracking, we can't get the PID easily
			// Try querying pg_stat_activity directly
			var found bool
			pid, found, err = findMaintenancePID(ctx, v.pool, target.SchemaName, target.Name, op.Type)
			if err != nil || !found {
				return OperationCancelledMsg{
					PID:       0,
					Cancelled: false,
					Error:     fmt.Errorf("could not find operation PID: operation may have already completed"),
				}
			}
		}

		if err != nil {
			return OperationCancelledMsg{
				PID:       0,
				Cancelled: false,
				Error:     fmt.Errorf("get progress: %w", err),
			}
		}

		// Get PID from progress if we have it
		if progress != nil && progress.PID > 0 {
			pid = progress.PID
		}

		if pid == 0 {
			// Operation may have already completed
			return OperationCancelledMsg{
				PID:       0,
				Cancelled: false,
				Error:     fmt.Errorf("operation not found: may have already completed"),
			}
		}

		logger.Debug("cancelling operation", "pid", pid, "operation", op.Type, "table", target.Name)

		// Call pg_cancel_backend
		cancelled, err := queries.CancelBackend(ctx, v.pool, pid)
		if err != nil {
			return OperationCancelledMsg{
				PID:       pid,
				Cancelled: false,
				Error:     err,
			}
		}

		return OperationCancelledMsg{
			PID:       pid,
			Cancelled: cancelled,
			Error:     nil,
		}
	}
}

// findMaintenancePID queries pg_stat_activity to find a running maintenance operation.
func findMaintenancePID(ctx context.Context, pool *pgxpool.Pool, schema, table string, opType models.OperationType) (int, bool, error) {
	var queryPattern string
	switch opType {
	case models.OpAnalyze:
		queryPattern = fmt.Sprintf("ANALYZE.*%s\\.%s", schema, table)
	case models.OpReindexTable, models.OpReindexConcurrently:
		queryPattern = fmt.Sprintf("REINDEX.*%s\\.%s", schema, table)
	default:
		queryPattern = fmt.Sprintf("(VACUUM|ANALYZE).*%s\\.%s", schema, table)
	}

	query := `
		SELECT pid FROM pg_stat_activity
		WHERE state = 'active'
		  AND query ~* $1
		LIMIT 1
	`

	var pid int
	err := pool.QueryRow(ctx, query, queryPattern).Scan(&pid)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return 0, false, nil
		}
		return 0, false, err
	}
	return pid, true, nil
}

// executeCheckBloat executes an on-demand bloat check using pgstattuple.
func (v *TablesView) executeCheckBloat() tea.Cmd {
	logger.Debug("executeCheckBloat called")

	if v.maintenanceTarget == nil {
		logger.Error("executeCheckBloat: maintenanceTarget is nil")
		return nil
	}
	if v.pool == nil {
		logger.Error("executeCheckBloat: pool is nil")
		return nil
	}

	target := v.maintenanceTarget
	tableName := fmt.Sprintf("%s.%s", target.SchemaName, target.Name)
	tableOID := target.OID

	// Create the operation object for tracking (same pattern as other maintenance ops)
	operation := &models.MaintenanceOperation{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:         models.OpCheckBloat,
		TargetSchema: target.SchemaName,
		TargetTable:  target.Name,
		Status:       models.StatusRunning,
		StartedAt:    time.Now(),
	}

	// Store operation (for history tracking, but no progress UI needed)
	v.currentOperation = operation

	logger.Debug("executeCheckBloat: starting bloat check",
		"table", tableName,
		"oid", tableOID)

	return func() tea.Msg {
		logger.Debug("executeCheckBloat: command func executing",
			"table", tableName,
			"oid", tableOID)

		startTime := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		bloatPct, err := queries.GetSingleTableBloat(ctx, v.pool, tableOID)
		elapsed := time.Since(startTime)

		if err != nil {
			logger.Error("executeCheckBloat: query failed",
				"table", tableName,
				"oid", tableOID,
				"error", err,
				"elapsed", elapsed)
			return CheckBloatResultMsg{
				TableOID:  tableOID,
				TableName: tableName,
				Success:   false,
				Error:     err,
				Elapsed:   elapsed,
			}
		}

		logger.Debug("executeCheckBloat: query succeeded",
			"table", tableName,
			"oid", tableOID,
			"bloatPct", bloatPct,
			"elapsed", elapsed)

		return CheckBloatResultMsg{
			TableOID:  tableOID,
			TableName: tableName,
			BloatPct:  bloatPct,
			Success:   true,
			Elapsed:   elapsed,
		}
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
