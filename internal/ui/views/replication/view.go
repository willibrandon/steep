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

// IsInputMode returns true when the view is in a mode that should consume keys
// (detail view, topology view, help overlay, or confirmation dialog).
func (v *ReplicationView) IsInputMode() bool {
	return v.mode != ModeNormal
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

	// Status bar (boxed, like Tables view)
	b.WriteString(v.renderStatusBar())
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

// renderStatusBar renders the boxed status bar (like Tables view).
func (v *ReplicationView) renderStatusBar() string {
	// Connection info (left side)
	connInfo := v.connectionInfo
	if connInfo == "" {
		connInfo = "PostgreSQL"
	}
	title := styles.StatusTitleStyle.Render(connInfo)

	// Server role indicator
	var roleIndicator string
	if v.data.IsPrimary {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			Render(" [PRIMARY]")
	} else {
		roleIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Render(" [STANDBY]")
	}

	// Additional indicators
	var indicators string
	if v.readOnly {
		indicators += lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(" [read-only]")
	}
	if v.err != nil {
		indicators += styles.ErrorStyle.Render(" [ERROR]")
	}
	if v.refreshing {
		indicators += lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(" [refreshing...]")
	}

	// Stale indicator
	var staleIndicator string
	if !v.lastUpdate.IsZero() && time.Since(v.lastUpdate) > 35*time.Second {
		staleIndicator = styles.ErrorStyle.Render(" [STALE]")
	}

	// Timestamp (right side)
	updateStr := "never"
	if !v.lastUpdate.IsZero() {
		updateStr = v.lastUpdate.Format("15:04:05")
	}
	timestamp := styles.StatusTimeStyle.Render("Last refresh: " + updateStr)

	// Calculate gap
	leftContent := title + roleIndicator + indicators + staleIndicator
	gap := v.width - lipgloss.Width(leftContent) - lipgloss.Width(timestamp) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(leftContent + spaces + timestamp)
}

// renderOverview renders the Overview tab content.
func (v *ReplicationView) renderOverview() string {
	// Check for permission errors and display guidance
	if v.err != nil {
		return v.renderError()
	}

	if !v.data.IsPrimary {
		// Connected to standby - show WAL receiver status
		return v.renderStandbyOverview()
	}

	if len(v.data.Replicas) == 0 {
		return v.renderNoReplicas()
	}

	return v.renderReplicaTable()
}

// renderError renders an error message with guidance.
func (v *ReplicationView) renderError() string {
	errMsg := v.err.Error()

	// Detect permission-related errors
	isPermissionErr := strings.Contains(strings.ToLower(errMsg), "permission") ||
		strings.Contains(strings.ToLower(errMsg), "denied") ||
		strings.Contains(strings.ToLower(errMsg), "insufficient_privilege")

	var b strings.Builder

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(errorStyle.Render("Error: " + errMsg))
	b.WriteString("\n\n")

	if isPermissionErr {
		b.WriteString(hintStyle.Render("Permission denied. To view replication status:\n\n"))
		b.WriteString(hintStyle.Render("  1. Connect as a superuser, or\n"))
		b.WriteString(hintStyle.Render("  2. Grant the pg_monitor role:\n"))
		b.WriteString(hintStyle.Render("     GRANT pg_monitor TO your_user;\n\n"))
		b.WriteString(hintStyle.Render("This grants read-only access to monitoring views."))
	} else {
		b.WriteString(hintStyle.Render("Check your database connection and permissions.\n"))
		b.WriteString(hintStyle.Render("Press r to retry."))
	}

	return lipgloss.Place(
		v.width, v.height-4,
		lipgloss.Center, lipgloss.Center,
		b.String(),
	)
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

	// Footer
	b.WriteString(v.renderFooter())

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
	b.WriteString(v.renderFooter())

	return b.String()
}

// renderSlotRow renders a single slot row.
func (v *ReplicationView) renderSlotRow(s models.ReplicationSlot, selected bool, headers []struct{ name string; width int }) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Check for orphaned slot (inactive for >24 hours) - T034
	isOrphaned := s.IsOrphaned(24 * time.Hour)

	// Name with orphaned indicator
	nameStyle := baseStyle
	slotName := s.SlotName
	if isOrphaned {
		nameStyle = nameStyle.Foreground(lipgloss.Color("214")) // Yellow for orphaned
		slotName = "!" + s.SlotName                              // Prefix with warning
	}

	// Active status style
	activeStyle := baseStyle
	activeStr := "No"
	if s.Active {
		activeStyle = activeStyle.Foreground(lipgloss.Color("42"))
		activeStr = "Yes"
	} else {
		activeStyle = activeStyle.Foreground(lipgloss.Color("214"))
		if isOrphaned {
			activeStr = "No*" // Asterisk indicates orphaned
		}
	}

	// Retained WAL with warning indicator - T033
	// Use 80% of 1GB as threshold for significant retention warning
	retainedStyle := baseStyle
	retainedStr := s.FormatRetainedBytes()
	if !s.Active && s.RetainedBytes > 800*1024*1024 { // >800MB and inactive
		retainedStyle = retainedStyle.Foreground(lipgloss.Color("196")) // Red for high retention
		retainedStr = "!" + retainedStr
	} else if !s.Active && s.RetainedBytes > 100*1024*1024 { // >100MB and inactive
		retainedStyle = retainedStyle.Foreground(lipgloss.Color("214")) // Yellow for moderate retention
	}

	// WAL status color
	walStyle := baseStyle
	if s.WALStatus == "lost" {
		walStyle = walStyle.Foreground(lipgloss.Color("196"))
	} else if s.WALStatus == "unreserved" {
		walStyle = walStyle.Foreground(lipgloss.Color("214"))
	}

	var row strings.Builder
	row.WriteString(nameStyle.Render(padRight(truncateWithEllipsis(slotName, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(s.SlotType.String(), headers[1].width)))
	row.WriteString(activeStyle.Render(padRight(activeStr, headers[2].width)))
	row.WriteString(retainedStyle.Render(padRight(retainedStr, headers[3].width)))
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
		pubHeader = "▶ " + pubHeader
	} else {
		pubHeader = "  " + pubHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(pubHeader))
	b.WriteString("\n")

	if len(v.data.Publications) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No publications"))
		b.WriteString("\n")
	} else {
		// Publication table headers
		pubHeaders := []struct {
			name  string
			width int
		}{
			{"Name", 22},
			{"Tables", 8},
			{"All", 5},
			{"Operations", 20},
			{"Subscribers", 12},
		}
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range pubHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, pub := range v.data.Publications {
			if i >= halfHeight-3 {
				break
			}
			selected := v.logicalFocusPubs && i == v.pubSelectedIdx
			b.WriteString(v.renderPubRow(pub, selected, pubHeaders))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Subscriptions section
	subHeader := "Subscriptions"
	if !v.logicalFocusPubs {
		subHeader = "▶ " + subHeader
	} else {
		subHeader = "  " + subHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(subHeader))
	b.WriteString("\n")

	if len(v.data.Subscriptions) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No subscriptions"))
		b.WriteString("\n")
	} else {
		// Subscription table headers
		subHeaders := []struct {
			name  string
			width int
		}{
			{"Name", 22},
			{"Enabled", 9},
			{"Publications", 20},
			{"Lag", 12},
		}
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range subHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, sub := range v.data.Subscriptions {
			if i >= halfHeight-3 {
				break
			}
			selected := !v.logicalFocusPubs && i == v.subSelectedIdx
			b.WriteString(v.renderSubRow(sub, selected, subHeaders))
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString(v.renderFooter())

	return b.String()
}

// renderPubRow renders a publication row with styling.
func (v *ReplicationView) renderPubRow(p models.Publication, selected bool, headers []struct{ name string; width int }) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// All tables indicator styling
	allTablesStyle := baseStyle
	allTablesStr := "No"
	if p.AllTables {
		allTablesStyle = allTablesStyle.Foreground(lipgloss.Color("42")) // Green
		allTablesStr = "Yes"
	}

	// Operations styling - green for all enabled, yellow for partial
	opsStyle := baseStyle
	ops := p.OperationFlags()
	allOps := p.Insert && p.Update && p.Delete && p.Truncate
	if allOps {
		opsStyle = opsStyle.Foreground(lipgloss.Color("42")) // Green for full
	} else if p.Insert || p.Update || p.Delete {
		opsStyle = opsStyle.Foreground(lipgloss.Color("214")) // Yellow for partial
	}

	// Subscriber count styling
	subStyle := baseStyle
	subStr := fmt.Sprintf("%d", p.SubscriberCount)
	if p.SubscriberCount > 0 {
		subStyle = subStyle.Foreground(lipgloss.Color("42")) // Green
	} else {
		subStyle = subStyle.Foreground(lipgloss.Color("241")) // Dim
		subStr = "0"
	}

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(p.Name, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(fmt.Sprintf("%d", p.TableCount), headers[1].width)))
	row.WriteString(allTablesStyle.Render(padRight(allTablesStr, headers[2].width)))
	row.WriteString(opsStyle.Render(padRight(ops, headers[3].width)))
	row.WriteString(subStyle.Render(padRight(subStr, headers[4].width)))

	return row.String()
}

// renderSubRow renders a subscription row with styling.
func (v *ReplicationView) renderSubRow(s models.Subscription, selected bool, headers []struct{ name string; width int }) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Enabled status styling
	enabledStyle := baseStyle
	enabledStr := "No"
	if s.Enabled {
		enabledStyle = enabledStyle.Foreground(lipgloss.Color("42")) // Green
		enabledStr = "Yes"
	} else {
		enabledStyle = enabledStyle.Foreground(lipgloss.Color("214")) // Yellow
	}

	// Publications list
	pubsStr := strings.Join(s.Publications, ", ")
	if len(pubsStr) > headers[2].width {
		pubsStr = truncateWithEllipsis(pubsStr, headers[2].width)
	}

	// Lag styling
	lagStyle := baseStyle
	lagStr := s.FormatByteLag()
	if s.ByteLag > 10*1024*1024 { // > 10MB
		lagStyle = lagStyle.Foreground(lipgloss.Color("196")) // Red
	} else if s.ByteLag > 1024*1024 { // > 1MB
		lagStyle = lagStyle.Foreground(lipgloss.Color("214")) // Yellow
	} else if s.ByteLag > 0 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("42")) // Green
	}

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(s.Name, headers[0].width), headers[0].width)))
	row.WriteString(enabledStyle.Render(padRight(enabledStr, headers[1].width)))
	row.WriteString(baseStyle.Render(padRight(pubsStr, headers[2].width)))
	row.WriteString(lagStyle.Render(padRight(lagStr, headers[3].width)))

	return row.String()
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

// renderDetail renders the detail view with improved styling.
func (v *ReplicationView) renderDetail() string {
	// Title style - MarginTop adds space before title, MarginBottom adds space after
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginTop(1)

	// Get title from first line if available
	title := "Details"
	startIdx := 0
	if len(v.detailLines) > 0 {
		title = v.detailLines[0]
		startIdx = 1 // Skip title in content
	}

	// Content
	var content string
	if len(v.detailLines) <= 1 {
		content = styles.InfoStyle.Render("No details available")
	} else {
		maxLines := v.height - 6 // Reserve space for title and footer
		contentLines := v.detailLines[startIdx:]

		endIdx := min(v.detailScrollOffset+maxLines, len(contentLines))
		startContent := v.detailScrollOffset
		if startContent > len(contentLines) {
			startContent = 0
		}
		visibleLines := contentLines[startContent:endIdx]

		// Pad to fill height
		for len(visibleLines) < maxLines {
			visibleLines = append(visibleLines, "")
		}
		content = strings.Join(visibleLines, "\n")
	}

	// Footer with scroll info
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	scrollInfo := ""
	contentLen := len(v.detailLines) - 1 // Exclude title
	maxLines := v.height - 6
	if contentLen > maxLines {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.detailScrollOffset+1, contentLen)
	}

	footer := footerStyle.Render("[j/k]scroll [g/G]top/bottom [esc/q]back" + scrollInfo)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(title),
		content,
		footer,
	)
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

	// Styles for detail view
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	lsnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Light blue for LSN

	// Color-coded state
	stateStyle := valueStyle
	switch r.State {
	case "streaming":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case "startup", "catchup":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case "backup":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")) // Blue
	}

	// Color-coded sync state
	syncStyle := valueStyle
	switch r.SyncState {
	case models.SyncStateSync:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case models.SyncStateAsync:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // Default
	case models.SyncStatePotential:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case models.SyncStateQuorum:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")) // Blue
	}

	// Color-coded lag values
	byteLagStyle := valueStyle
	if r.ByteLag > 100*1024*1024 { // > 100MB
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if r.ByteLag > 10*1024*1024 { // > 10MB
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if r.ByteLag > 0 {
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	} else {
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	timeLagStyle := valueStyle
	if r.ReplayLag > 5*time.Second {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if r.ReplayLag > time.Second {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if r.ReplayLag > 0 {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	} else {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	v.detailLines = []string{
		"Replica Details: " + r.ApplicationName,
		labelStyle.Render("Client Address:  ") + valueStyle.Render(r.ClientAddr),
		labelStyle.Render("State:           ") + stateStyle.Render(r.State),
		labelStyle.Render("Sync State:      ") + syncStyle.Render(r.SyncState.String()),
		"",
		sectionStyle.Render("WAL Positions"),
		labelStyle.Render("  Sent LSN:      ") + lsnStyle.Render(r.SentLSN),
		labelStyle.Render("  Write LSN:     ") + lsnStyle.Render(r.WriteLSN),
		labelStyle.Render("  Flush LSN:     ") + lsnStyle.Render(r.FlushLSN),
		labelStyle.Render("  Replay LSN:    ") + lsnStyle.Render(r.ReplayLSN),
		"",
		sectionStyle.Render("Lag"),
		labelStyle.Render("  Byte Lag:      ") + byteLagStyle.Render(r.FormatByteLag()),
		labelStyle.Render("  Write Lag:     ") + timeLagStyle.Render(formatDuration(r.WriteLag)),
		labelStyle.Render("  Flush Lag:     ") + timeLagStyle.Render(formatDuration(r.FlushLag)),
		labelStyle.Render("  Replay Lag:    ") + timeLagStyle.Render(formatDuration(r.ReplayLag)),
		"",
		labelStyle.Render("Backend Start:   ") + valueStyle.Render(r.BackendStart.Format("2006-01-02 15:04:05")),
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSlotDetail() {
	if v.slotSelectedIdx >= len(v.data.Slots) {
		return
	}
	s := v.data.Slots[v.slotSelectedIdx]

	// Styles for detail view
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	lsnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Light blue for LSN

	// Color-coded active status
	activeStr := "No"
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow for inactive
	if s.Active {
		activeStr = "Yes"
		activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for active
	}

	// Color-coded slot type
	typeStyle := valueStyle
	if s.SlotType == models.SlotTypeLogical {
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // Purple for logical
	} else {
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Blue for physical
	}

	// Color-coded WAL retention
	retainedStyle := valueStyle
	if !s.Active && s.RetainedBytes > 800*1024*1024 { // > 800MB and inactive
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if !s.Active && s.RetainedBytes > 100*1024*1024 { // > 100MB and inactive
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if s.RetainedBytes > 0 {
		retainedStyle = valueStyle
	} else {
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	// Color-coded WAL status
	walStatusStyle := valueStyle
	switch s.WALStatus {
	case "reserved":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case "extended":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case "unreserved":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	case "lost":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Bold red
	}

	// Active PID display
	activePIDStr := "-"
	if s.ActivePID > 0 {
		activePIDStr = fmt.Sprintf("%d", s.ActivePID)
	}

	// Check for orphaned slot
	isOrphaned := s.IsOrphaned(24 * time.Hour)
	orphanedWarning := ""
	if isOrphaned {
		orphanedWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(" (inactive >24h)")
	}

	v.detailLines = []string{
		"Slot Details: " + s.SlotName,
		labelStyle.Render("Type:            ") + typeStyle.Render(s.SlotType.String()),
		labelStyle.Render("Database:        ") + valueStyle.Render(s.Database),
		labelStyle.Render("Active:          ") + activeStyle.Render(activeStr) + orphanedWarning,
		labelStyle.Render("Active PID:      ") + valueStyle.Render(activePIDStr),
		"",
		sectionStyle.Render("WAL Retention"),
		labelStyle.Render("  Restart LSN:   ") + lsnStyle.Render(s.RestartLSN),
		labelStyle.Render("  Retained:      ") + retainedStyle.Render(s.FormatRetainedBytes()),
		labelStyle.Render("  WAL Status:    ") + walStatusStyle.Render(s.WALStatus),
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) preparePubDetail() {
	if v.pubSelectedIdx >= len(v.data.Publications) {
		return
	}
	p := v.data.Publications[v.pubSelectedIdx]

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(18)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	tableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// All tables styling
	allTablesStr := "No"
	allTablesStyle := valueStyle
	if p.AllTables {
		allTablesStr = "Yes"
		allTablesStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	// Operations styling
	opsStyle := valueStyle
	allOps := p.Insert && p.Update && p.Delete && p.Truncate
	if allOps {
		opsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	} else if p.Insert || p.Update || p.Delete {
		opsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	}

	// Subscriber count styling
	subStyle := valueStyle
	if p.SubscriberCount > 0 {
		subStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	} else {
		subStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	}

	v.detailLines = []string{
		titleStyle.Render("Publication Details: " + p.Name),
		labelStyle.Render("All Tables:") + allTablesStyle.Render(allTablesStr),
		labelStyle.Render("Table Count:") + valueStyle.Render(fmt.Sprintf("%d", p.TableCount)),
		labelStyle.Render("Subscribers:") + subStyle.Render(fmt.Sprintf("%d", p.SubscriberCount)),
		"",
		headerStyle.Render("Operations"),
		labelStyle.Render("INSERT:") + v.formatBoolValue(p.Insert),
		labelStyle.Render("UPDATE:") + v.formatBoolValue(p.Update),
		labelStyle.Render("DELETE:") + v.formatBoolValue(p.Delete),
		labelStyle.Render("TRUNCATE:") + v.formatBoolValue(p.Truncate),
		labelStyle.Render("Combined:") + opsStyle.Render(p.OperationFlags()),
	}

	if len(p.Tables) > 0 {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Published Tables"))
		for _, t := range p.Tables {
			v.detailLines = append(v.detailLines, "  "+tableStyle.Render(t))
		}
	}

	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSubDetail() {
	if v.subSelectedIdx >= len(v.data.Subscriptions) {
		return
	}
	s := v.data.Subscriptions[v.subSelectedIdx]

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(18)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	pubStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// Enabled styling
	enabledStr := "No"
	enabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	if s.Enabled {
		enabledStr = "Yes"
		enabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	// Lag styling
	lagStr := s.FormatByteLag()
	lagStyle := valueStyle
	if s.ByteLag > 10*1024*1024 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	} else if s.ByteLag > 1024*1024 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	} else if s.ByteLag > 0 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	v.detailLines = []string{
		titleStyle.Render("Subscription Details: " + s.Name),
		labelStyle.Render("Enabled:") + enabledStyle.Render(enabledStr),
		labelStyle.Render("Byte Lag:") + lagStyle.Render(lagStr),
		"",
		headerStyle.Render("Connection"),
		labelStyle.Render("Connection Info:") + valueStyle.Render(truncateWithEllipsis(s.ConnInfo, 60)),
		"",
		headerStyle.Render("LSN Positions"),
		labelStyle.Render("Received LSN:") + valueStyle.Render(s.ReceivedLSN),
		labelStyle.Render("Latest End LSN:") + valueStyle.Render(s.LatestEndLSN),
	}

	// Timing info if available
	if !s.LastMsgSendTime.IsZero() {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Timing"))
		v.detailLines = append(v.detailLines, labelStyle.Render("Last Msg Sent:")+valueStyle.Render(s.LastMsgSendTime.Format("2006-01-02 15:04:05")))
		v.detailLines = append(v.detailLines, labelStyle.Render("Last Msg Recv:")+valueStyle.Render(s.LastMsgReceiptTime.Format("2006-01-02 15:04:05")))
	}

	if len(s.Publications) > 0 {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Subscribed Publications"))
		for _, p := range s.Publications {
			v.detailLines = append(v.detailLines, "  "+pubStyle.Render(p))
		}
	}

	v.detailScrollOffset = 0
}

// formatBoolValue returns a styled Yes/No string
func (v *ReplicationView) formatBoolValue(val bool) string {
	if val {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Yes")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("No")
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
