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
	"github.com/willibrandon/steep/internal/ui/views/replication/setup"
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
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	tableHeight := v.height - 8
	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	} else if v.selectedIdx >= v.scrollOffset+tableHeight {
		v.scrollOffset = v.selectedIdx - tableHeight + 1
	}
}

func (v *ReplicationView) ensureSlotVisible() {
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	tableHeight := v.height - 8
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
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	maxScroll := max(0, len(v.detailLines)-(v.height-8))
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
	// Send request to app.go to execute the actual query
	slotName := v.dropSlotName
	return func() tea.Msg {
		return ui.DropSlotRequestMsg{
			SlotName: slotName,
		}
	}
}

func (v *ReplicationView) executeWizardCmd() tea.Cmd {
	// Send a request message that app.go will handle to execute the command
	return func() tea.Msg {
		return ui.WizardExecRequestMsg{
			Command: v.wizardExecCommand,
			Label:   v.wizardExecLabel,
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
			hints = styles.FooterHintStyle.Render("[c]heck [e]dit [p]hysical l[o]gical con[n]str [h]elp")
		}
	}

	// Right side: sort info + count (not shown for Setup tab)
	var rightSide string
	if v.activeTab != TabSetup {
		arrow := "↓"
		if v.sortAsc {
			arrow = "↑"
		}
		sortInfo := fmt.Sprintf("Sort: %s %s", v.sortColumn.String(), arrow)

		var count string
		var windowInfo string
		switch v.activeTab {
		case TabOverview:
			count = fmt.Sprintf("%d / %d", min(v.selectedIdx+1, len(v.data.Replicas)), len(v.data.Replicas))
			windowInfo = fmt.Sprintf("  [%s]", formatDuration(v.timeWindow))
		case TabSlots:
			count = fmt.Sprintf("%d / %d", min(v.slotSelectedIdx+1, len(v.data.Slots)), len(v.data.Slots))
		case TabLogical:
			if v.logicalFocusPubs {
				count = fmt.Sprintf("%d pubs", len(v.data.Publications))
			} else {
				count = fmt.Sprintf("%d subs", len(v.data.Subscriptions))
			}
		}

		rightSide = styles.FooterCountStyle.Render(sortInfo + "  " + count + windowInfo)
	}

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

// renderConfigEditor renders the configuration editor view.
func (v *ReplicationView) renderConfigEditor() string {
	return setup.RenderConfigEditor(v.configEditor, v.width, v.height)
}

// renderAlterSystemConfirm renders the ALTER SYSTEM confirmation dialog.
func (v *ReplicationView) renderAlterSystemConfirm() string {
	if v.configEditor == nil {
		return ""
	}

	// Get pending commands
	commands := v.configEditor.GenerateAlterCommands()

	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("Confirm ALTER SYSTEM Execution"))
	b.WriteString("\n\n")

	// Warning
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	b.WriteString(warnStyle.Render("The following commands will be executed:"))
	b.WriteString("\n\n")

	// Commands
	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	for _, cmd := range commands {
		b.WriteString("  " + cmdStyle.Render(cmd) + "\n")
	}

	// Restart warning if needed
	if v.configEditor.RequiresRestart() {
		b.WriteString("\n")
		restartStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		b.WriteString(restartStyle.Render("⚠ Some changes require PostgreSQL restart to take effect!"))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Prompt
	promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	b.WriteString(promptStyle.Render("Execute these commands? [y]es / [n]o"))
	b.WriteString("\n")

	// Center the dialog
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Width(60)

	dialog := dialogStyle.Render(b.String())

	// Position in center of screen
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}
