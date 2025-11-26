// Package replication provides the Replication view for monitoring PostgreSQL replication.
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

// ReplicationMode represents the current interaction mode.
type ReplicationMode int

const (
	ModeNormal ReplicationMode = iota
	ModeDetail
	ModeHelp
	ModeTopology
	ModeConfirmDropSlot
	ModeConfigCheck
	ModePhysicalWizard
	ModeLogicalWizard
	ModeConfirmWizardExecute
	ModeConnStringBuilder
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
	showTopology         bool
	topologySelectedIdx  int                // Currently selected replica in topology
	topologyExpanded     map[string]bool    // Which replicas have expanded pipeline view (by app name)

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
	timeWindow          time.Duration
	sqliteLagHistory    map[string][]float64 // Lag history from SQLite for longer windows
	lastSqliteFetch     time.Time            // When we last fetched from SQLite

	// Clipboard
	clipboard *ui.ClipboardWriter

	// Physical wizard state
	physicalWizard *setup.PhysicalWizardState

	// Logical wizard state
	logicalWizard *setup.LogicalWizardState
	wizardTables  []models.Table // Tables for logical wizard

	// Wizard execute confirmation
	wizardExecCommand string
	wizardExecLabel   string
	wizardExecSource  ReplicationMode // Which wizard triggered the confirmation

	// Connection string builder state
	connStringBuilder *setup.ConnStringState
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
		topologyExpanded: make(map[string]bool),
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
			// Refresh detail view if open
			if v.mode == ModeDetail && v.activeTab == TabOverview {
				v.prepareReplicaDetail()
			} else if v.mode == ModeDetail && v.activeTab == TabSlots {
				v.prepareSlotDetail()
			}
			// Fetch SQLite lag history periodically when using longer windows
			if v.timeWindow > time.Minute {
				// Refresh every 30 seconds
				if v.sqliteLagHistory == nil || time.Since(v.lastSqliteFetch) > 30*time.Second {
					return v, func() tea.Msg {
						return ui.LagHistoryRequestMsg{Window: v.timeWindow}
					}
				}
			}
		}

	case ui.DropSlotResultMsg:
		if msg.Error != nil {
			v.showToast("Drop slot failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast(fmt.Sprintf("Slot '%s' dropped", msg.SlotName), false)
		} else {
			v.showToast(fmt.Sprintf("Failed to drop slot '%s'", msg.SlotName), true)
		}

	case ui.WizardExecResultMsg:
		// Clear the stored command
		v.wizardExecCommand = ""
		v.wizardExecLabel = ""
		if msg.Error != nil {
			v.showToast("Execute failed: "+msg.Error.Error(), true)
		} else if msg.Success {
			v.showToast("Command executed successfully", false)
		} else {
			v.showToast("Command execution failed", true)
		}

	case ui.LagHistoryResponseMsg:
		// Store SQLite lag history separately (not overwritten by monitor)
		if msg.Error != nil {
			v.showToast("Failed to fetch history: "+msg.Error.Error(), true)
		} else if msg.LagHistory != nil {
			v.sqliteLagHistory = msg.LagHistory
			v.lastSqliteFetch = time.Now()
		}

	case ui.TablesResponseMsg:
		// Open logical wizard with tables
		if msg.Error != nil {
			v.showToast("Failed to fetch tables: "+msg.Error.Error(), true)
		} else {
			v.wizardTables = msg.Tables
			v.initLogicalWizard(msg.Tables)
			v.mode = ModeLogicalWizard
		}

	case ui.ConnTestResponseMsg:
		// Handle connection test result
		if v.connStringBuilder != nil {
			v.connStringBuilder.Testing = false
			if msg.Error != nil {
				v.connStringBuilder.TestResult = msg.Error.Error()
				v.connStringBuilder.TestError = true
			} else if msg.Success {
				v.connStringBuilder.TestResult = msg.Message
				v.connStringBuilder.TestError = false
			} else {
				v.connStringBuilder.TestResult = msg.Message
				v.connStringBuilder.TestError = true
			}
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case tea.MouseMsg:
		v.handleMouseMsg(msg)
	}

	return v, nil
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
	case ModePhysicalWizard:
		content := v.renderPhysicalWizard()
		// Apply toast overlay if active
		if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
			return v.overlayToast(content)
		}
		return content
	case ModeLogicalWizard:
		content := v.renderLogicalWizard()
		// Apply toast overlay if active
		if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
			return v.overlayToast(content)
		}
		return content
	case ModeConfirmWizardExecute:
		return v.renderWizardExecConfirm()
	case ModeConnStringBuilder:
		content := v.renderConnStringBuilder()
		// Apply toast overlay if active
		if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
			return v.overlayToast(content)
		}
		return content
	}

	var content string

	switch v.activeTab {
	case TabOverview:
		switch v.mode {
		case ModeTopology:
			content = v.renderTopology()
		case ModeDetail:
			content = v.renderDetail()
		default:
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

	return b.String()
}
