package replication

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

func (v *ReplicationView) moveSelection(delta int) {
	if len(v.data.Replicas) == 0 {
		return
	}
	v.selectedIdx = clamp(v.selectedIdx+delta, 0, len(v.data.Replicas)-1)
	v.ensureVisible()
}

func (v *ReplicationView) moveSlotSelection(delta int) {
	if len(v.data.Slots) == 0 {
		return
	}
	v.slotSelectedIdx = clamp(v.slotSelectedIdx+delta, 0, len(v.data.Slots)-1)
	v.ensureSlotVisible()
}

func (v *ReplicationView) movePubSelection(delta int) {
	if len(v.data.Publications) == 0 {
		return
	}
	v.pubSelectedIdx = clamp(v.pubSelectedIdx+delta, 0, len(v.data.Publications)-1)
}

func (v *ReplicationView) moveSubSelection(delta int) {
	if len(v.data.Subscriptions) == 0 {
		return
	}
	v.subSelectedIdx = clamp(v.subSelectedIdx+delta, 0, len(v.data.Subscriptions)-1)
}

func (v *ReplicationView) ensureVisible() {
	tableHeight := v.height - 6
	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	} else if v.selectedIdx >= v.scrollOffset+tableHeight {
		v.scrollOffset = v.selectedIdx - tableHeight + 1
	}
}

func (v *ReplicationView) ensureSlotVisible() {
	tableHeight := v.height - 6
	if v.slotSelectedIdx < v.slotScrollOffset {
		v.slotScrollOffset = v.slotSelectedIdx
	} else if v.slotSelectedIdx >= v.slotScrollOffset+tableHeight {
		v.slotScrollOffset = v.slotSelectedIdx - tableHeight + 1
	}
}

func (v *ReplicationView) detailScrollUp(lines int) {
	v.detailScrollOffset = max(0, v.detailScrollOffset-lines)
}

func (v *ReplicationView) detailScrollDown(lines int) {
	maxScroll := max(0, len(v.detailLines)-(v.height-4))
	v.detailScrollOffset = min(maxScroll, v.detailScrollOffset+lines)
}

func (v *ReplicationView) sortReplicas() {
	if len(v.data.Replicas) == 0 {
		return
	}

	// Sort based on current column and direction
	replicas := v.data.Replicas
	switch v.sortColumn {
	case SortByName:
		if v.sortAsc {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.ApplicationName < b.ApplicationName })
		} else {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.ApplicationName > b.ApplicationName })
		}
	case SortByState:
		if v.sortAsc {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.State < b.State })
		} else {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.State > b.State })
		}
	case SortByLag:
		if v.sortAsc {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.ByteLag < b.ByteLag })
		} else {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.ByteLag > b.ByteLag })
		}
	case SortBySyncState:
		if v.sortAsc {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.SyncState < b.SyncState })
		} else {
			sortByFunc(replicas, func(a, b models.Replica) bool { return a.SyncState > b.SyncState })
		}
	}
}

func (v *ReplicationView) copySelectedReplica() {
	if len(v.data.Replicas) == 0 || v.selectedIdx >= len(v.data.Replicas) {
		return
	}
	r := v.data.Replicas[v.selectedIdx]
	text := fmt.Sprintf("%s\t%s\t%s\t%s", r.ApplicationName, r.ClientAddr, r.State, r.FormatByteLag())

	if !v.clipboard.IsAvailable() {
		v.showToast("Clipboard unavailable", true)
		return
	}
	if err := v.clipboard.Write(text); err != nil {
		v.showToast("Copy failed: "+err.Error(), true)
		return
	}
	v.showToast("Copied to clipboard", false)
}

func (v *ReplicationView) dropSlotCmd() tea.Cmd {
	// This will be connected to the actual query in app.go
	return func() tea.Msg {
		return ui.DropSlotResultMsg{
			SlotName: v.dropSlotName,
			Success:  false,
			Error:    fmt.Errorf("not implemented"),
		}
	}
}

// renderFooter renders the bottom footer (like other views).
func (v *ReplicationView) renderFooter() string {
	var hints string

	// Show toast message if recent (within 3 seconds)
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorSuccess)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		// Show tab-specific hints
		switch v.activeTab {
		case TabOverview:
			hints = styles.FooterHintStyle.Render("[j/k]nav [d]etail [t]opology [s/S]ort [w]indow [y]ank [h]elp")
		case TabSlots:
			hints = styles.FooterHintStyle.Render("[j/k]nav [d]etail [D]rop [h]elp")
		case TabLogical:
			hints = styles.FooterHintStyle.Render("[j/k]nav [p]ubs/subs [d]etail [h]elp")
		case TabSetup:
			hints = styles.FooterHintStyle.Render("[1]physical [2]logical [3]connstr [c]heck [h]elp")
		}
	}

	// Right side: sort info + count
	arrow := "↓"
	if v.sortAsc {
		arrow = "↑"
	}
	sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)

	var count string
	switch v.activeTab {
	case TabOverview:
		count = fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.data.Replicas)), len(v.data.Replicas))
	case TabSlots:
		count = fmt.Sprintf("%d / %d", min(v.slotSelectedIdx+1, len(v.data.Slots)), len(v.data.Slots))
	case TabLogical:
		if v.logicalFocusPubs {
			count = fmt.Sprintf("%d pubs", len(v.data.Publications))
		} else {
			count = fmt.Sprintf("%d subs", len(v.data.Subscriptions))
		}
	default:
		count = ""
	}

	rightSide := styles.FooterCountStyle.Render(sortInfo + "  " + count)

	gap := v.width - lipgloss.Width(hints) - lipgloss.Width(rightSide) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.Width(v.width - 2).Render(hints + spaces + rightSide)
}

func (v *ReplicationView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

func (v *ReplicationView) overlayToast(content string) string {
	toastStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Background(lipgloss.Color("236"))

	if v.toastError {
		toastStyle = toastStyle.Foreground(lipgloss.Color("196"))
	} else {
		toastStyle = toastStyle.Foreground(lipgloss.Color("42"))
	}

	toast := toastStyle.Render(v.toastMessage)

	// Place toast at bottom
	lines := strings.Split(content, "\n")
	if len(lines) > 0 {
		lines[len(lines)-1] = toast
	}
	return strings.Join(lines, "\n")
}
