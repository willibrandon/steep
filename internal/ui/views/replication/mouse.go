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
				// Calculate clicked row based on tab and layout
				// Layout: status bar (3 lines with border) + newline + title + newline + tabs + newline + header = 8 lines
				clickedRow := msg.Y - 8
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
	case ModeDetail:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			v.detailScrollUp(1)
		case tea.MouseButtonWheelDown:
			v.detailScrollDown(1)
		}
	}
}
