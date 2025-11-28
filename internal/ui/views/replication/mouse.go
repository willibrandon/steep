package replication

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleMouseMsg processes mouse input.
func (v *ReplicationView) handleMouseMsg(msg tea.MouseMsg) {
	switch v.mode {
	case ModeNormal:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			switch v.activeTab {
			case TabOverview:
				v.moveSelection(-1)
			case TabSlots:
				v.moveSlotSelection(-1)
			}
		case tea.MouseButtonWheelDown:
			switch v.activeTab {
			case TabOverview:
				v.moveSelection(1)
			case TabSlots:
				v.moveSlotSelection(1)
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				// msg.Y is relative to view top (app translates global to relative)
				// Subtract view's header height (calculated in View()) to get data row
				clickedRow := msg.Y - v.viewHeaderHeight
				if clickedRow >= 0 {
					switch v.activeTab {
					case TabOverview:
						newIdx := v.scrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(v.data.Replicas) {
							v.selectedIdx = newIdx
						}
					case TabSlots:
						newIdx := v.slotScrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(v.data.Slots) {
							v.slotSelectedIdx = newIdx
						}
					}
				}
			}
		}

	case ModeTopology:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			v.topologyNavigateUp()
		case tea.MouseButtonWheelDown:
			v.topologyNavigateDown()
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				// Topology layout has additional header (viewHeaderHeight + 1 for topology title area)
				// Line 0-3: topology header area
				// Line 4+: tree nodes (each node is 1 line, expanded adds 4 more lines)
				clickedRow := msg.Y - v.viewHeaderHeight - 1

				if clickedRow >= 4 && len(v.data.Replicas) > 0 {
					// Calculate which replica was clicked
					replicaIdx := v.getTopologyNodeAtRow(clickedRow - 4)
					if replicaIdx >= 0 && replicaIdx < len(v.data.Replicas) {
						// Select the node and toggle its expansion
						v.topologySelectedIdx = replicaIdx
						v.toggleTopologyExpansion()
					}
				}
			}
		}

	case ModeDetail:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			v.detailScrollUp(1)
		case tea.MouseButtonWheelDown:
			v.detailScrollDown(1)
		}
	}
}

// getTopologyNodeAtRow returns the replica index at the given row offset from first node.
// Returns -1 if the row doesn't correspond to a node line.
func (v *ReplicationView) getTopologyNodeAtRow(row int) int {
	if row < 0 || len(v.data.Replicas) == 0 {
		return -1
	}

	// Calculate row positions accounting for expanded pipelines
	// Each node takes 1 line, but expanded nodes add 4 more lines for pipeline
	currentRow := 0
	for i, r := range v.data.Replicas {
		// Check if click is on the node line itself
		if row == currentRow {
			return i
		}

		// If this node is expanded, check if click is within pipeline area
		if v.topologyExpanded[r.ApplicationName] {
			// Pipeline takes 4 lines: stage names, LSNs, lag indicators, bottom border
			// These are at rows currentRow+1 through currentRow+4
			if row > currentRow && row <= currentRow+4 {
				// Clicked on pipeline area - return the parent node
				return i
			}
			currentRow += 5 // node line + 4 pipeline lines
		} else {
			currentRow++ // just the node line
		}
	}

	return -1
}
