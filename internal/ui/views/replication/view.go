// Package replication provides the Replication view for monitoring PostgreSQL replication.
package replication

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ReplicationMode represents the current interaction mode.
type ReplicationMode int

const (
	ModeNormal ReplicationMode = iota
	ModeDetail
	ModeHelp
	ModeTopology
	ModeConfirmDropSlot
)

// SortColumn represents the available sort columns for replicas.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortByState
	SortByLag
	SortBySyncState
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByName:
		return "Name"
	case SortByState:
		return "State"
	case SortByLag:
		return "Lag"
	case SortBySyncState:
		return "Sync"
	default:
		return "Unknown"
	}
}

// ReplicationView displays replication monitoring information.
type ReplicationView struct {
	width  int
	height int

	// State
	mode           ReplicationMode
	activeTab      ViewTab
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	readOnly       bool

	// Data
	data       *models.ReplicationData
	totalCount int
	err        error

	// Table state
	selectedIdx  int
	scrollOffset int
	sortColumn   SortColumn
	sortAsc      bool // false = descending (default), true = ascending

	// Slots table state
	slotSelectedIdx  int
	slotScrollOffset int

	// Logical table state (pubs/subs)
	logicalFocusPubs bool // true = publications, false = subscriptions
	pubSelectedIdx   int
	pubScrollOffset  int
	subSelectedIdx   int
	subScrollOffset  int

	// Topology view state
	showTopology bool

	// Detail view state
	detailScrollOffset int
	detailLines        []string

	// Drop slot confirmation
	dropSlotName string

	// Toast message
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Time window for sparklines
	timeWindow time.Duration

	// Clipboard
	clipboard *ui.ClipboardWriter
}

// NewReplicationView creates a new replication view.
func NewReplicationView() *ReplicationView {
	return &ReplicationView{
		mode:             ModeNormal,
		activeTab:        TabOverview,
		data:             models.NewReplicationData(),
		sortColumn:       SortByName,
		clipboard:        ui.NewClipboardWriter(),
		timeWindow:       5 * time.Minute,
		logicalFocusPubs: true,
	}
}

// Init initializes the replication view.
func (v *ReplicationView) Init() tea.Cmd {
	return nil
}

// SetSize sets the dimensions of the view.
func (v *ReplicationView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetReadOnly sets the read-only mode.
func (v *ReplicationView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
}

// SetConnected sets the connection status.
func (v *ReplicationView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *ReplicationView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// Update handles messages for the replication view.
func (v *ReplicationView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			return v, cmd
		}

	case ui.ReplicationDataMsg:
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
		} else {
			v.data = msg.Data
			v.totalCount = len(msg.Data.Replicas)
			v.lastUpdate = msg.FetchedAt
			v.err = nil
			// Apply current sort order
			v.sortReplicas()
			// Ensure selection is valid
			if v.selectedIdx >= len(v.data.Replicas) {
				v.selectedIdx = max(0, len(v.data.Replicas)-1)
			}
			v.ensureVisible()
		}

	case ui.DropSlotResultMsg:
		if msg.Error != nil {
			v.showToast("Drop slot failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast(fmt.Sprintf("Slot '%s' dropped", msg.SlotName), false)
		} else {
			v.showToast(fmt.Sprintf("Failed to drop slot '%s'", msg.SlotName), true)
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case tea.MouseMsg:
		v.handleMouseMsg(msg)
	}

	return v, nil
}

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
	// Setup tab keys will be implemented in later user stories
	switch key {
	case "1":
		v.showToast("Physical wizard (not yet implemented)", false)
	case "2":
		v.showToast("Logical wizard (not yet implemented)", false)
	case "3":
		v.showToast("Connection builder (not yet implemented)", false)
	case "c":
		v.showToast("Config checker (not yet implemented)", false)
	}
	return nil
}

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
				clickedRow := msg.Y - 5 // Adjust for header
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

// View renders the replication view.
func (v *ReplicationView) View() string {
	if v.width == 0 || v.height == 0 {
		return ""
	}

	// Handle modal overlays
	switch v.mode {
	case ModeHelp:
		return HelpOverlay(v.width, v.height, v.activeTab)
	case ModeConfirmDropSlot:
		return v.renderDropSlotConfirm()
	}

	var content string

	switch v.activeTab {
	case TabOverview:
		if v.mode == ModeTopology {
			content = v.renderTopology()
		} else if v.mode == ModeDetail {
			content = v.renderDetail()
		} else {
			content = v.renderOverview()
		}
	case TabSlots:
		if v.mode == ModeDetail {
			content = v.renderDetail()
		} else {
			content = v.renderSlots()
		}
	case TabLogical:
		if v.mode == ModeDetail {
			content = v.renderDetail()
		} else {
			content = v.renderLogical()
		}
	case TabSetup:
		content = v.renderSetup()
	}

	// Build final view
	var b strings.Builder

	// Status line
	b.WriteString(v.renderStatusLine())
	b.WriteString("\n")

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(titleStyle.Render("Replication"))
	b.WriteString("\n")

	// Tab bar
	b.WriteString(TabBar(v.activeTab, v.width))
	b.WriteString("\n")

	// Content
	b.WriteString(content)

	// Toast if active
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		return v.overlayToast(b.String())
	}

	return b.String()
}

// renderStatusLine renders the status bar line.
func (v *ReplicationView) renderStatusLine() string {
	// Server role indicator
	var roleIndicator string
	if v.data.IsPrimary {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Render("PRIMARY")
	} else {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render("STANDBY")
	}

	// Last update time
	updateStr := "never"
	if !v.lastUpdate.IsZero() {
		updateStr = v.lastUpdate.Format("15:04:05")
	}

	// Error indicator
	var errStr string
	if v.err != nil {
		errStr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render(" [ERROR]")
	}

	// Refreshing indicator
	var refreshStr string
	if v.refreshing {
		refreshStr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(" [refreshing...]")
	}

	// Read-only indicator
	var readOnlyStr string
	if v.readOnly {
		readOnlyStr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(" [read-only]")
	}

	left := roleIndicator + readOnlyStr + errStr + refreshStr
	right := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("Updated: %s", updateStr))

	gap := v.width - runewidth.StringWidth(left) - runewidth.StringWidth(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

// renderOverview renders the Overview tab content.
func (v *ReplicationView) renderOverview() string {
	if !v.data.IsPrimary {
		// Connected to standby - show WAL receiver status
		return v.renderStandbyOverview()
	}

	if len(v.data.Replicas) == 0 {
		return v.renderNoReplicas()
	}

	return v.renderReplicaTable()
}

// renderNoReplicas renders the empty state message.
func (v *ReplicationView) renderNoReplicas() string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("No streaming replicas connected.\n\n" +
			"To set up replication:\n" +
			"  1. Press Tab to go to Setup tab\n" +
			"  2. Run configuration checker (c)\n" +
			"  3. Use physical replication wizard (1)")

	return lipgloss.Place(
		v.width, v.height-4,
		lipgloss.Center, lipgloss.Center,
		msg,
	)
}

// renderStandbyOverview renders the standby server overview.
func (v *ReplicationView) renderStandbyOverview() string {
	if v.data.WALReceiverStatus == nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Connected to standby server.\nWAL receiver status not available.")
	}

	wal := v.data.WALReceiverStatus
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(20)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	b.WriteString(headerStyle.Render("WAL Receiver Status"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Status:"))
	b.WriteString(valueStyle.Render(wal.Status))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Primary Host:"))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%s:%d", wal.SenderHost, wal.SenderPort)))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Received LSN:"))
	b.WriteString(valueStyle.Render(wal.ReceivedLSN))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Lag:"))
	lagStr := models.FormatBytes(wal.LagBytes)
	lagStyle := valueStyle
	if wal.LagBytes > 10*1024*1024 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("196"))
	} else if wal.LagBytes > 1024*1024 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("214"))
	} else {
		lagStyle = lagStyle.Foreground(lipgloss.Color("42"))
	}
	b.WriteString(lagStyle.Render(lagStr))
	b.WriteString("\n")

	if wal.SlotName != "" {
		b.WriteString(labelStyle.Render("Slot:"))
		b.WriteString(valueStyle.Render(wal.SlotName))
		b.WriteString("\n")
	}

	return b.String()
}

// renderReplicaTable renders the replica list table.
func (v *ReplicationView) renderReplicaTable() string {
	var b strings.Builder

	// Calculate available height for table
	tableHeight := v.height - 6 // status + title + tabs + header + footer

	// Column headers
	headers := []struct {
		name  string
		width int
	}{
		{"Name", 20},
		{"Client", 15},
		{"State", 12},
		{"Sync", 10},
		{"Byte Lag", 12},
		{"Time Lag", 10},
	}

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for i, h := range headers {
		text := truncateWithEllipsis(h.name, h.width)
		// Add sort indicator
		if SortColumn(i) == v.sortColumn {
			if v.sortAsc {
				text += " ↑"
			} else {
				text += " ↓"
			}
		}
		headerRow.WriteString(headerStyle.Render(padRight(text, h.width)))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Data rows
	visibleRows := min(tableHeight, len(v.data.Replicas)-v.scrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.scrollOffset + i
		if idx >= len(v.data.Replicas) {
			break
		}
		replica := v.data.Replicas[idx]
		b.WriteString(v.renderReplicaRow(replica, idx == v.selectedIdx, headers))
		b.WriteString("\n")
	}

	// Footer with counts
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("\n%d replica(s) | Sort: %s | Press h for help", len(v.data.Replicas), v.sortColumn))
	b.WriteString(footer)

	return b.String()
}

// renderReplicaRow renders a single replica row.
func (v *ReplicationView) renderReplicaRow(r models.Replica, selected bool, headers []struct{ name string; width int }) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Lag severity colors
	lagSeverity := r.GetLagSeverity()
	var lagStyle lipgloss.Style
	switch lagSeverity {
	case models.LagSeverityHealthy:
		lagStyle = baseStyle.Foreground(lipgloss.Color("42"))
	case models.LagSeverityWarning:
		lagStyle = baseStyle.Foreground(lipgloss.Color("214"))
	case models.LagSeverityCritical:
		lagStyle = baseStyle.Foreground(lipgloss.Color("196"))
	}

	// Sync state color
	var syncStyle lipgloss.Style
	switch r.SyncState {
	case models.SyncStateSync:
		syncStyle = baseStyle.Foreground(lipgloss.Color("42"))
	case models.SyncStatePotential:
		syncStyle = baseStyle.Foreground(lipgloss.Color("214"))
	case models.SyncStateQuorum:
		syncStyle = baseStyle.Foreground(lipgloss.Color("39"))
	default:
		syncStyle = baseStyle.Foreground(lipgloss.Color("245"))
	}

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.ApplicationName, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.ClientAddr, headers[1].width), headers[1].width)))
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(r.State, headers[2].width), headers[2].width)))
	row.WriteString(syncStyle.Render(padRight(r.SyncState.String(), headers[3].width)))
	row.WriteString(lagStyle.Render(padRight(r.FormatByteLag(), headers[4].width)))
	row.WriteString(lagStyle.Render(padRight(r.FormatReplayLag(), headers[5].width)))

	return row.String()
}

// renderSlots renders the Slots tab content.
func (v *ReplicationView) renderSlots() string {
	if len(v.data.Slots) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("No replication slots configured.")
		return lipgloss.Place(v.width, v.height-4, lipgloss.Center, lipgloss.Center, msg)
	}

	var b strings.Builder

	// Column headers
	headers := []struct {
		name  string
		width int
	}{
		{"Name", 25},
		{"Type", 10},
		{"Active", 8},
		{"Retained", 12},
		{"WAL Status", 12},
	}

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for _, h := range headers {
		headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Table height
	tableHeight := v.height - 6

	// Data rows
	visibleRows := min(tableHeight, len(v.data.Slots)-v.slotScrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.slotScrollOffset + i
		if idx >= len(v.data.Slots) {
			break
		}
		slot := v.data.Slots[idx]
		b.WriteString(v.renderSlotRow(slot, idx == v.slotSelectedIdx, headers))
		b.WriteString("\n")
	}

	// Footer
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("\n%d slot(s) | D to drop inactive slot | Press h for help", len(v.data.Slots)))
	b.WriteString(footer)

	return b.String()
}

// renderSlotRow renders a single slot row.
func (v *ReplicationView) renderSlotRow(s models.ReplicationSlot, selected bool, headers []struct{ name string; width int }) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Active status style
	activeStyle := baseStyle
	activeStr := "No"
	if s.Active {
		activeStyle = activeStyle.Foreground(lipgloss.Color("42"))
		activeStr = "Yes"
	} else {
		activeStyle = activeStyle.Foreground(lipgloss.Color("214"))
	}

	// WAL status color
	walStyle := baseStyle
	if s.WALStatus == "lost" {
		walStyle = walStyle.Foreground(lipgloss.Color("196"))
	} else if s.WALStatus == "unreserved" {
		walStyle = walStyle.Foreground(lipgloss.Color("214"))
	}

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(s.SlotName, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(s.SlotType.String(), headers[1].width)))
	row.WriteString(activeStyle.Render(padRight(activeStr, headers[2].width)))
	row.WriteString(baseStyle.Render(padRight(s.FormatRetainedBytes(), headers[3].width)))
	row.WriteString(walStyle.Render(padRight(s.WALStatus, headers[4].width)))

	return row.String()
}

// renderLogical renders the Logical tab content.
func (v *ReplicationView) renderLogical() string {
	if !v.data.HasLogicalReplication() {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("No logical replication configured.\n\n" +
				"To set up logical replication:\n" +
				"  1. Ensure wal_level = 'logical'\n" +
				"  2. Press Tab to go to Setup tab\n" +
				"  3. Use logical replication wizard (2)")
		return lipgloss.Place(v.width, v.height-4, lipgloss.Center, lipgloss.Center, msg)
	}

	var b strings.Builder

	// Split view: publications on top, subscriptions on bottom
	halfHeight := (v.height - 6) / 2

	// Publications section
	pubHeader := "Publications"
	if v.logicalFocusPubs {
		pubHeader = "> " + pubHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(pubHeader))
	b.WriteString("\n")

	if len(v.data.Publications) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No publications"))
		b.WriteString("\n")
	} else {
		for i, pub := range v.data.Publications {
			if i >= halfHeight-2 {
				break
			}
			selected := v.logicalFocusPubs && i == v.pubSelectedIdx
			b.WriteString(v.renderPubRow(pub, selected))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Subscriptions section
	subHeader := "Subscriptions"
	if !v.logicalFocusPubs {
		subHeader = "> " + subHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(subHeader))
	b.WriteString("\n")

	if len(v.data.Subscriptions) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No subscriptions"))
		b.WriteString("\n")
	} else {
		for i, sub := range v.data.Subscriptions {
			if i >= halfHeight-2 {
				break
			}
			selected := !v.logicalFocusPubs && i == v.subSelectedIdx
			b.WriteString(v.renderSubRow(sub, selected))
			b.WriteString("\n")
		}
	}

	// Footer
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("\np to switch focus | Press h for help"))
	b.WriteString(footer)

	return b.String()
}

// renderPubRow renders a publication row.
func (v *ReplicationView) renderPubRow(p models.Publication, selected bool) string {
	style := lipgloss.NewStyle()
	if selected {
		style = style.Background(lipgloss.Color("236"))
	}

	return style.Render(fmt.Sprintf("  %-20s  Tables: %-4d  Ops: %s",
		truncateWithEllipsis(p.Name, 20),
		p.TableCount,
		p.OperationFlags()))
}

// renderSubRow renders a subscription row.
func (v *ReplicationView) renderSubRow(s models.Subscription, selected bool) string {
	style := lipgloss.NewStyle()
	if selected {
		style = style.Background(lipgloss.Color("236"))
	}

	enabledStr := "No"
	if s.Enabled {
		enabledStr = "Yes"
	}

	return style.Render(fmt.Sprintf("  %-20s  Enabled: %-3s  Lag: %s",
		truncateWithEllipsis(s.Name, 20),
		enabledStr,
		s.FormatByteLag()))
}

// renderSetup renders the Setup tab content.
func (v *ReplicationView) renderSetup() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(headerStyle.Render("Setup Wizards"))
	b.WriteString("\n\n")

	items := []struct {
		key  string
		name string
		desc string
	}{
		{"1", "Physical Replication", "Set up streaming replication with pg_basebackup"},
		{"2", "Logical Replication", "Create publications and subscriptions"},
		{"3", "Connection Builder", "Generate primary_conninfo connection strings"},
		{"c", "Configuration Checker", "Verify PostgreSQL settings for replication"},
	}

	for _, item := range items {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			hintStyle.Render(fmt.Sprintf("[%s]", item.key)),
			itemStyle.Render(item.name)))
		b.WriteString(fmt.Sprintf("      %s\n\n",
			hintStyle.Render(item.desc)))
	}

	if v.readOnly {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render("Note: Setup wizards are disabled in read-only mode"))
	}

	return b.String()
}

// renderTopology renders the topology view.
func (v *ReplicationView) renderTopology() string {
	// Basic topology rendering - will be enhanced in US3
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Replication Topology"))
	b.WriteString("\n\n")

	if len(v.data.Replicas) == 0 {
		b.WriteString("No replicas connected.\n")
		b.WriteString("\nPress t or Esc to return")
		return b.String()
	}

	// Simple tree representation
	b.WriteString("PRIMARY\n")
	for i, r := range v.data.Replicas {
		prefix := "├── "
		if i == len(v.data.Replicas)-1 {
			prefix = "└── "
		}
		lagStr := r.FormatByteLag()
		syncStr := r.SyncState.String()
		b.WriteString(fmt.Sprintf("%s%s (%s, %s lag)\n", prefix, r.ApplicationName, syncStr, lagStr))
	}

	b.WriteString("\nPress t or Esc to return")
	return b.String()
}

// renderDetail renders the detail view.
func (v *ReplicationView) renderDetail() string {
	if len(v.detailLines) == 0 {
		return "No details available."
	}

	var b strings.Builder
	maxLines := v.height - 4

	for i := v.detailScrollOffset; i < len(v.detailLines) && i < v.detailScrollOffset+maxLines; i++ {
		b.WriteString(v.detailLines[i])
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("\nPress Esc or q to return"))

	return b.String()
}

// renderDropSlotConfirm renders the drop slot confirmation dialog.
func (v *ReplicationView) renderDropSlotConfirm() string {
	content := fmt.Sprintf("Drop replication slot '%s'?\n\n"+
		"This may cause data loss if a replica is still using this slot.\n\n"+
		"Press Y to confirm, N or Esc to cancel",
		v.dropSlotName)

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// Helper methods

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

func (v *ReplicationView) prepareReplicaDetail() {
	if v.selectedIdx >= len(v.data.Replicas) {
		return
	}
	r := v.data.Replicas[v.selectedIdx]

	v.detailLines = []string{
		lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Replica Details: " + r.ApplicationName),
		"",
		fmt.Sprintf("Client Address:  %s", r.ClientAddr),
		fmt.Sprintf("State:           %s", r.State),
		fmt.Sprintf("Sync State:      %s", r.SyncState),
		"",
		"WAL Positions:",
		fmt.Sprintf("  Sent LSN:      %s", r.SentLSN),
		fmt.Sprintf("  Write LSN:     %s", r.WriteLSN),
		fmt.Sprintf("  Flush LSN:     %s", r.FlushLSN),
		fmt.Sprintf("  Replay LSN:    %s", r.ReplayLSN),
		"",
		"Lag:",
		fmt.Sprintf("  Byte Lag:      %s", r.FormatByteLag()),
		fmt.Sprintf("  Write Lag:     %s", formatDuration(r.WriteLag)),
		fmt.Sprintf("  Flush Lag:     %s", formatDuration(r.FlushLag)),
		fmt.Sprintf("  Replay Lag:    %s", formatDuration(r.ReplayLag)),
		"",
		fmt.Sprintf("Backend Start:   %s", r.BackendStart.Format("2006-01-02 15:04:05")),
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSlotDetail() {
	if v.slotSelectedIdx >= len(v.data.Slots) {
		return
	}
	s := v.data.Slots[v.slotSelectedIdx]

	activeStr := "No"
	if s.Active {
		activeStr = "Yes"
	}

	v.detailLines = []string{
		lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Slot Details: " + s.SlotName),
		"",
		fmt.Sprintf("Type:            %s", s.SlotType),
		fmt.Sprintf("Database:        %s", s.Database),
		fmt.Sprintf("Active:          %s", activeStr),
		fmt.Sprintf("Active PID:      %d", s.ActivePID),
		"",
		"WAL Retention:",
		fmt.Sprintf("  Restart LSN:   %s", s.RestartLSN),
		fmt.Sprintf("  Retained:      %s", s.FormatRetainedBytes()),
		fmt.Sprintf("  WAL Status:    %s", s.WALStatus),
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) preparePubDetail() {
	if v.pubSelectedIdx >= len(v.data.Publications) {
		return
	}
	p := v.data.Publications[v.pubSelectedIdx]

	allTablesStr := "No"
	if p.AllTables {
		allTablesStr = "Yes"
	}

	v.detailLines = []string{
		lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Publication: " + p.Name),
		"",
		fmt.Sprintf("All Tables:      %s", allTablesStr),
		fmt.Sprintf("Operations:      %s", p.OperationFlags()),
		fmt.Sprintf("Table Count:     %d", p.TableCount),
		"",
		"Tables:",
	}
	for _, t := range p.Tables {
		v.detailLines = append(v.detailLines, "  - "+t)
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSubDetail() {
	if v.subSelectedIdx >= len(v.data.Subscriptions) {
		return
	}
	s := v.data.Subscriptions[v.subSelectedIdx]

	enabledStr := "No"
	if s.Enabled {
		enabledStr = "Yes"
	}

	v.detailLines = []string{
		lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Subscription: " + s.Name),
		"",
		fmt.Sprintf("Enabled:         %s", enabledStr),
		fmt.Sprintf("Connection:      %s", s.ConnInfo),
		fmt.Sprintf("Lag:             %s", s.FormatByteLag()),
		"",
		"Publications:",
	}
	for _, p := range s.Publications {
		v.detailLines = append(v.detailLines, "  - "+p)
	}
	v.detailScrollOffset = 0
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

// Utility functions

func truncateWithEllipsis(s string, maxLen int) string {
	if runewidth.StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return runewidth.Truncate(s, maxLen-1, "…")
}

func clamp(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func sortByFunc(replicas []models.Replica, less func(a, b models.Replica) bool) {
	for i := 0; i < len(replicas)-1; i++ {
		for j := i + 1; j < len(replicas); j++ {
			if less(replicas[j], replicas[i]) {
				replicas[i], replicas[j] = replicas[j], replicas[i]
			}
		}
	}
}
