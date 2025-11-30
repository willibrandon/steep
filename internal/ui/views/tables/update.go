package tables

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/logger"
)

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

			// Apply accurate bloat values if available
			if msg.Bloat != nil {
				for i := range v.tables {
					if pct, ok := msg.Bloat[v.tables[i].OID]; ok {
						v.tables[i].BloatPct = pct
						v.tables[i].BloatEstimated = false
					}
				}
			}

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

			// Show install prompt if pgstattuple not available, not readonly, and not previously shown
			if !msg.PgstattupleAvailable && !v.readonlyMode && !v.installPromptShown {
				v.mode = ModeConfirmInstall
			}
		}
		return v, v.scheduleRefresh()

	case InstallExtensionMsg:
		if msg.Success {
			v.pgstattupleAvailable = true
			v.showToast("pgstattuple extension installed successfully", false)
			// Refresh data to get accurate bloat values
			return v, v.fetchTablesData()
		}
		errMsg := "Failed to install extension"
		if msg.Error != nil {
			errMsg = fmt.Sprintf("Install failed: %v", msg.Error)
		}
		v.showToast(errMsg, true)
		return v, nil

	case TableDetailsMsg:
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Error loading details: %v", msg.Error), true)
			v.mode = ModeNormal
			return v, nil
		}
		// Find the table and populate details
		if table, ok := v.tablesByOID[msg.TableOID]; ok {
			v.details = &models.TableDetails{
				Table:       *table,
				Columns:     msg.Columns,
				Constraints: msg.Constraints,
				Indexes:     v.getIndexesForTable(msg.TableOID),
			}
			v.detailsScrollOffset = 0
			v.detailsLines = v.buildDetailsLines()
			v.mode = ModeDetails
		}
		return v, nil

	case MaintenanceResultMsg:
		// T076: Track operation in session history before clearing currentOperation
		if v.currentOperation != nil && v.operationHistory != nil {
			completedOp := *v.currentOperation
			now := time.Now()
			completedOp.CompletedAt = &now
			completedOp.Duration = msg.Elapsed
			if msg.Success {
				completedOp.Status = models.StatusCompleted
			} else {
				completedOp.Status = models.StatusFailed
				completedOp.Error = msg.Error
			}
			v.operationHistory.Add(completedOp)
			logger.Debug("operation added to history",
				"operation", msg.Operation,
				"table", msg.TableName,
				"success", msg.Success,
				"historySize", len(v.operationHistory.Operations))
		}

		v.maintenanceTarget = nil
		v.currentOperation = nil
		v.pollingInProgress = false // Reset polling state
		v.lastUpdate = time.Now()   // Reset lastUpdate to prevent STALE indicator
		v.mode = ModeNormal
		if msg.Success {
			v.showToast(fmt.Sprintf("✓ %s completed: %s (%s)", msg.Operation, msg.TableName, formatDuration(msg.Elapsed)), false)
			return v, v.fetchTablesData()
		}
		errMsg := fmt.Sprintf("✗ %s failed: %s", msg.Operation, msg.TableName)
		if msg.Error != nil {
			// T079: Use actionable error message
			actionableMsg := GetActionableErrorMessage(msg.Error)
			errMsg = fmt.Sprintf("✗ %s failed on %s: %s", msg.Operation, msg.TableName, actionableMsg)
			logger.Error("maintenance operation failed",
				"operation", msg.Operation,
				"table", msg.TableName,
				"error", msg.Error)
		}
		v.showToast(errMsg, true)
		return v, nil

	case OperationStartedMsg:
		v.currentOperation = msg.Operation
		v.mode = ModeOperationProgress
		v.pollingInProgress = true // Mark poll as in progress
		return v, tea.Batch(v.progressTick(), v.pollProgress())

	case ProgressTickMsg:
		// Only poll if we're still showing progress and not already polling
		if v.mode == ModeOperationProgress && v.currentOperation != nil {
			if v.pollingInProgress {
				// Poll still in progress, just schedule next tick without new poll
				logger.Debug("skipping poll, previous poll still in progress")
				return v, v.progressTick()
			}
			v.pollingInProgress = true
			return v, tea.Batch(v.progressTick(), v.pollProgress())
		}
		return v, nil

	case OperationProgressMsg:
		v.pollingInProgress = false // Poll completed, allow next poll
		if v.currentOperation != nil {
			if msg.Progress != nil {
				v.currentOperation.Progress = msg.Progress
				logger.Debug("operation progress update",
					"phase", msg.Progress.Phase,
					"percent", msg.Progress.PercentComplete,
					"blocks_scanned", msg.Progress.HeapBlksScanned,
					"blocks_total", msg.Progress.HeapBlksTotal)
			} else {
				logger.Debug("operation progress poll returned nil")
			}
		}
		return v, nil

	case OperationCancelledMsg:
		v.mode = ModeNormal
		v.pollingInProgress = false
		if msg.Error != nil {
			// Cancel call itself failed
			v.showToast(fmt.Sprintf("Cancel failed: %v", msg.Error), true)
			logger.Error("cancel operation failed", "pid", msg.PID, "error", msg.Error)
		} else if msg.Cancelled {
			// Successfully sent cancel signal
			v.showToast("Operation cancelled", false)
			v.currentOperation = nil
			v.maintenanceTarget = nil
			logger.Debug("operation cancelled", "pid", msg.PID)
			// Refresh data after cancellation
			return v, v.fetchTablesData()
		} else {
			// pg_cancel_backend returned false - process may have already completed
			v.showToast("Cancel signal sent (process may have completed)", false)
			v.currentOperation = nil
			v.maintenanceTarget = nil
			logger.Debug("cancel signal sent but returned false", "pid", msg.PID)
		}
		return v, nil

	case PermissionsDataMsg:
		logger.Debug("received PermissionsDataMsg",
			"tableOID", msg.TableOID,
			"permCount", len(msg.Permissions),
			"roleCount", len(msg.RoleNames),
			"hasError", msg.Error != nil,
			"dialogExists", v.permissionsDialog != nil)
		if v.permissionsDialog != nil {
			v.permissionsDialog.Loading = false
			if msg.Error != nil {
				logger.Error("PermissionsDataMsg error", "error", msg.Error)
				v.permissionsDialog.Error = msg.Error
			} else {
				v.permissionsDialog.Permissions = msg.Permissions
				v.permissionsDialog.RoleNames = msg.RoleNames
				logger.Debug("permissions dialog updated", "permCount", len(msg.Permissions))
			}
		} else {
			logger.Debug("PermissionsDataMsg received but dialog is nil")
		}
		return v, nil

	case PermissionsRefreshMsg:
		if v.permissionsDialog != nil {
			v.permissionsDialog.Loading = true
			return v, v.fetchPermissionsData(v.permissionsDialog.TableOID)
		}
		return v, nil

	case GrantPermissionMsg:
		// Execute grant and refresh
		if v.readonlyMode {
			v.showToast("GRANT blocked: read-only mode", true)
			return v, nil
		}
		return v, v.executeGrant(msg.Schema, msg.Table, msg.Role, msg.Privilege, msg.WithGrantOption)

	case GrantPermissionResultMsg:
		if msg.Success {
			v.showToast(fmt.Sprintf("Granted %s to %s", msg.Privilege, msg.Role), false)
			// Refresh permissions
			if v.permissionsDialog != nil {
				v.permissionsDialog.Loading = true
				return v, v.fetchPermissionsData(v.permissionsDialog.TableOID)
			}
		} else {
			errMsg := fmt.Sprintf("Grant failed: %v", msg.Error)
			v.showToast(errMsg, true)
		}
		return v, nil

	case RevokePermissionMsg:
		// Execute revoke and refresh
		if v.readonlyMode {
			v.showToast("REVOKE blocked: read-only mode", true)
			return v, nil
		}
		return v, v.executeRevoke(msg.Schema, msg.Table, msg.Role, msg.Privilege, msg.Cascade)

	case RevokePermissionResultMsg:
		if msg.Success {
			v.showToast(fmt.Sprintf("Revoked %s from %s", msg.Privilege, msg.Role), false)
			// Refresh permissions
			if v.permissionsDialog != nil {
				v.permissionsDialog.Loading = true
				return v, v.fetchPermissionsData(v.permissionsDialog.TableOID)
			}
		} else {
			errMsg := fmt.Sprintf("Revoke failed: %v", msg.Error)
			v.showToast(errMsg, true)
		}
		return v, nil

	case RefreshTablesMsg:
		// Skip auto-refresh during maintenance operations to avoid timeout errors
		if v.mode == ModeOperationProgress || v.currentOperation != nil {
			return v, v.scheduleRefresh() // Just reschedule, don't fetch
		}
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

			// Table panel starts at viewHeaderHeight (calculated in View())
			tableStartY := v.viewHeaderHeight
			var indexStartY int
			var tablePanelRows int

			if showIndexPanel {
				tablePanelRows = v.tablePanelHeight()
				// Index panel starts after table panel + index title(1) + index header(1) + separator(1)
				indexStartY = tableStartY + tablePanelRows + 3
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
			case tea.MouseButtonWheelUp:
				if v.helpScrollOffset > 0 {
					v.helpScrollOffset--
				}
			case tea.MouseButtonWheelDown:
				v.helpScrollOffset++
			case tea.MouseButtonLeft:
				// Click anywhere to close help
				if msg.Action == tea.MouseActionPress {
					v.mode = ModeNormal
					v.helpScrollOffset = 0
				}
			}
		case ModeDetails:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if msg.Shift {
					v.scrollDetailsLeft(10) // Horizontal scroll with shift+wheel
				} else {
					v.scrollDetailsUp(3)
				}
			case tea.MouseButtonWheelDown:
				if msg.Shift {
					v.scrollDetailsRight(10) // Horizontal scroll with shift+wheel
				} else {
					v.scrollDetailsDown(3)
				}
			case tea.MouseButtonWheelLeft:
				v.scrollDetailsLeft(10)
			case tea.MouseButtonWheelRight:
				v.scrollDetailsRight(10)
			case tea.MouseButtonLeft:
				// Could handle clicks on specific elements
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
