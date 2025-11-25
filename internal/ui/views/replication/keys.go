package replication

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyPress processes keyboard input.
func (v *ReplicationView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Handle help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "?", "esc", "q":
			v.mode = ModeNormal
		}
		return nil
	}

	// Handle confirm drop slot mode
	if v.mode == ModeConfirmDropSlot {
		switch key {
		case "y", "Y":
			v.mode = ModeNormal
			return v.dropSlotCmd()
		case "n", "N", "esc", "q":
			v.mode = ModeNormal
			v.dropSlotName = ""
		}
		return nil
	}

	// Handle detail mode
	if v.mode == ModeDetail {
		switch key {
		case "esc", "q":
			v.mode = ModeNormal
		case "j", "down":
			v.detailScrollDown(1)
		case "k", "up":
			v.detailScrollUp(1)
		case "g", "home":
			v.detailScrollOffset = 0
		case "G", "end":
			maxScroll := max(0, len(v.detailLines)-(v.height-4))
			v.detailScrollOffset = maxScroll
		case "ctrl+d", "pgdown":
			v.detailScrollDown(10)
		case "ctrl+u", "pgup":
			v.detailScrollUp(10)
		}
		return nil
	}

	// Handle topology mode
	if v.mode == ModeTopology {
		switch key {
		case "t", "esc", "q":
			v.mode = ModeNormal
			v.showTopology = false
		}
		return nil
	}

	// Normal mode - global keys
	switch key {
	case "h", "?":
		v.mode = ModeHelp
		return nil
	case "tab", "right", "l":
		v.activeTab = NextTab(v.activeTab)
		return nil
	case "shift+tab", "left", "H":
		v.activeTab = PrevTab(v.activeTab)
		return nil
	case "r":
		v.refreshing = true
		return nil
	}

	// Tab-specific keys
	switch v.activeTab {
	case TabOverview:
		return v.handleOverviewKeys(key)
	case TabSlots:
		return v.handleSlotsKeys(key)
	case TabLogical:
		return v.handleLogicalKeys(key)
	case TabSetup:
		return v.handleSetupKeys(key)
	}

	return nil
}

// handleOverviewKeys handles keys specific to the Overview tab.
func (v *ReplicationView) handleOverviewKeys(key string) tea.Cmd {
	switch key {
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g", "home":
		v.selectedIdx = 0
		v.ensureVisible()
	case "G", "end":
		if len(v.data.Replicas) > 0 {
			v.selectedIdx = len(v.data.Replicas) - 1
			v.ensureVisible()
		}
	case "ctrl+d", "pgdown":
		v.moveSelection(10)
	case "ctrl+u", "pgup":
		v.moveSelection(-10)
	case "s":
		v.sortColumn = SortColumn((int(v.sortColumn) + 1) % 4)
		v.sortReplicas()
	case "S":
		v.sortAsc = !v.sortAsc
		v.sortReplicas()
	case "t":
		v.showTopology = true
		v.mode = ModeTopology
	case "d", "enter":
		if len(v.data.Replicas) > 0 && v.selectedIdx < len(v.data.Replicas) {
			v.prepareReplicaDetail()
			v.mode = ModeDetail
		}
	case "w":
		// Cycle time window
		switch v.timeWindow {
		case time.Minute:
			v.timeWindow = 5 * time.Minute
		case 5 * time.Minute:
			v.timeWindow = 15 * time.Minute
		case 15 * time.Minute:
			v.timeWindow = time.Hour
		default:
			v.timeWindow = time.Minute
		}
		v.showToast(fmt.Sprintf("Time window: %s", formatDuration(v.timeWindow)), false)
	case "y":
		v.copySelectedReplica()
	}
	return nil
}

// handleSlotsKeys handles keys specific to the Slots tab.
func (v *ReplicationView) handleSlotsKeys(key string) tea.Cmd {
	switch key {
	case "j", "down":
		v.moveSlotSelection(1)
	case "k", "up":
		v.moveSlotSelection(-1)
	case "g", "home":
		v.slotSelectedIdx = 0
		v.ensureSlotVisible()
	case "G", "end":
		if len(v.data.Slots) > 0 {
			v.slotSelectedIdx = len(v.data.Slots) - 1
			v.ensureSlotVisible()
		}
	case "D":
		if v.readOnly {
			v.showToast("Cannot drop slots in read-only mode", true)
			return nil
		}
		if len(v.data.Slots) > 0 && v.slotSelectedIdx < len(v.data.Slots) {
			slot := v.data.Slots[v.slotSelectedIdx]
			if slot.Active {
				v.showToast("Cannot drop active slot", true)
				return nil
			}
			v.dropSlotName = slot.SlotName
			v.mode = ModeConfirmDropSlot
		}
	case "d", "enter":
		if len(v.data.Slots) > 0 && v.slotSelectedIdx < len(v.data.Slots) {
			v.prepareSlotDetail()
			v.mode = ModeDetail
		}
	}
	return nil
}

// handleLogicalKeys handles keys specific to the Logical tab.
func (v *ReplicationView) handleLogicalKeys(key string) tea.Cmd {
	switch key {
	case "p", "P":
		v.logicalFocusPubs = !v.logicalFocusPubs
	case "j", "down":
		if v.logicalFocusPubs {
			v.movePubSelection(1)
		} else {
			v.moveSubSelection(1)
		}
	case "k", "up":
		if v.logicalFocusPubs {
			v.movePubSelection(-1)
		} else {
			v.moveSubSelection(-1)
		}
	case "d", "enter":
		if v.logicalFocusPubs {
			if len(v.data.Publications) > 0 {
				v.preparePubDetail()
				v.mode = ModeDetail
			}
		} else {
			if len(v.data.Subscriptions) > 0 {
				v.prepareSubDetail()
				v.mode = ModeDetail
			}
		}
	}
	return nil
}

// handleSetupKeys handles keys specific to the Setup tab.
func (v *ReplicationView) handleSetupKeys(key string) tea.Cmd {
	// Handle config check mode
	if v.mode == ModeConfigCheck {
		switch key {
		case "esc", "q":
			v.mode = ModeNormal
		}
		return nil
	}

	// Normal setup tab keys
	switch key {
	case "p":
		v.showToast("Physical wizard (not yet implemented)", false)
	case "o":
		v.showToast("Logical wizard (not yet implemented)", false)
	case "n":
		v.showToast("Connection builder (not yet implemented)", false)
	case "c":
		// T045: Integrate configuration checker into Setup tab
		v.mode = ModeConfigCheck
	}
	return nil
}
