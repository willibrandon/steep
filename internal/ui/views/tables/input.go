package tables

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/willibrandon/steep/internal/logger"
)

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

	// Handle confirm install dialog
	if v.mode == ModeConfirmInstall || (v.pgstattupleChecked && !v.pgstattupleAvailable && !v.installPromptShown) {
		switch key {
		case "y", "Y", "enter":
			// User confirmed - install extension
			v.mode = ModeNormal
			v.installPromptShown = true
			return v.installExtension()
		case "n", "N", "esc", "q":
			// User declined - don't ask again this session
			v.mode = ModeNormal
			v.installPromptShown = true
			v.showToast("Skipped pgstattuple install (won't ask again)", false)
		}
		return nil
	}

	// Handle maintenance confirmation dialogs
	if v.mode == ModeConfirmVacuum || v.mode == ModeConfirmAnalyze || v.mode == ModeConfirmReindex || v.mode == ModeConfirmReindexConcurrently {
		switch key {
		case "y", "Y", "enter":
			// User confirmed - execute the operation (executeMaintenance sets mode to ModeOperationProgress)
			return v.executeMaintenance()
		case "n", "N", "esc", "q":
			// User cancelled
			v.mode = ModeNormal
			v.maintenanceTarget = nil
			v.pendingVacuumFull = false
			v.pendingVacuumAnalyze = false
		}
		return nil
	}

	// Handle operation progress mode
	if v.mode == ModeOperationProgress {
		switch key {
		case "esc":
			// Dismiss progress overlay but keep operation running in background
			v.mode = ModeNormal
			v.showToast("Operation continues in background", false)
		case "c", "C":
			// Show cancel confirmation dialog
			v.mode = ModeConfirmCancel
		}
		return nil
	}

	// Handle cancel confirmation mode
	if v.mode == ModeConfirmCancel {
		switch key {
		case "y", "Y", "enter":
			// User confirmed - cancel the operation
			return v.cancelOperation()
		case "n", "N", "esc", "q":
			// User declined - return to progress view
			v.mode = ModeOperationProgress
		}
		return nil
	}

	// Handle operations menu
	if v.mode == ModeOperationsMenu {
		return v.handleOperationsMenuKey(key)
	}

	// Handle permissions dialog
	if v.mode == ModePermissions {
		return v.handlePermissionsKey(key)
	}

	// Handle details mode
	if v.mode == ModeDetails {
		switch key {
		case "esc", "q":
			v.mode = ModeNormal
			v.details = nil
			v.detailsLines = nil
			v.detailsScrollOffset = 0
			v.detailsHScrollOffset = 0
		case "y":
			// Show copy menu
			if v.details != nil {
				v.mode = ModeCopyMenu
			}
		case "j", "down":
			v.scrollDetailsDown(1)
		case "k", "up":
			v.scrollDetailsUp(1)
		case "h", "left":
			v.scrollDetailsLeft(10)
		case "l", "right":
			v.scrollDetailsRight(10)
		case "0":
			v.detailsHScrollOffset = 0
		case "$":
			v.scrollDetailsToRight()
		case "ctrl+d", "pgdown":
			v.scrollDetailsDown(v.detailsContentHeight())
		case "ctrl+u", "pgup":
			v.scrollDetailsUp(v.detailsContentHeight())
		case "g", "home":
			v.detailsScrollOffset = 0
			v.detailsHScrollOffset = 0
		case "G", "end":
			v.scrollDetailsToBottom()
		}
		return nil
	}

	// Handle copy menu mode
	if v.mode == ModeCopyMenu {
		switch key {
		case "esc", "q":
			v.mode = ModeDetails
		case "n":
			v.copyTableName()
			v.mode = ModeDetails
		case "s":
			v.copySelectSQL()
			v.mode = ModeDetails
		case "i":
			v.copyInsertSQL()
			v.mode = ModeDetails
		case "u":
			v.copyUpdateSQL()
			v.mode = ModeDetails
		case "d":
			v.copyDeleteSQL()
			v.mode = ModeDetails
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
		switch v.focusPanel {
		case FocusIndexes:
			v.copySelectedIndexName()
		case FocusTables:
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
			v.ensureIndexVisible()
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

	// Expand/collapse or open details - only for tables panel
	case "enter":
		if v.focusPanel == FocusTables && v.selectedIdx >= 0 && v.selectedIdx < len(v.treeItems) {
			item := v.treeItems[v.selectedIdx]
			if item.Schema != nil {
				// Schema: toggle expand
				v.toggleExpand()
			} else if item.Table != nil {
				if item.Table.IsPartitioned {
					// Partitioned table: toggle expand to show partitions
					v.toggleExpand()
				} else {
					// Regular table: open details
					return v.fetchTableDetails(item.Table.OID)
				}
			}
		}
	case "d":
		// Open details for selected table
		if v.focusPanel == FocusTables && v.selectedIdx >= 0 && v.selectedIdx < len(v.treeItems) {
			item := v.treeItems[v.selectedIdx]
			if item.Table != nil {
				return v.fetchTableDetails(item.Table.OID)
			}
		}
	case "right", "l":
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
		if v.focusPanel == FocusIndexes {
			v.cycleIndexSortColumn()
		} else {
			v.cycleSortColumn()
		}
	case "S":
		if v.focusPanel == FocusIndexes {
			v.toggleIndexSortDirection()
		} else {
			v.toggleSortDirection()
		}

	// Refresh
	case "R":
		if !v.refreshing {
			v.refreshing = true
			return v.fetchTablesData()
		}

	// Maintenance operations - require a table to be selected
	case "v":
		return v.promptMaintenance(ModeConfirmVacuum)
	case "a":
		return v.promptMaintenance(ModeConfirmAnalyze)
	case "r":
		return v.promptMaintenance(ModeConfirmReindex)

	// Operations menu - shows all maintenance operations
	case "x":
		return v.openOperationsMenu()

	// Permissions dialog - show table permissions
	case "p":
		logger.Debug("input: 'p' key pressed, calling openPermissionsDialog")
		cmd := v.openPermissionsDialog()
		logger.Debug("input: openPermissionsDialog returned", "cmdIsNil", cmd == nil)
		return cmd

	// Resize split (only when index panel is visible)
	// - shrinks index panel, + grows index panel
	case "-":
		v.resizeSplitUp() // More space for tables, less for indexes
	case "+", "=":
		v.resizeSplitDown() // Less space for tables, more for indexes
	}

	return nil
}
