package replication

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/views/replication/setup"
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

	// Handle confirm wizard execute mode
	if v.mode == ModeConfirmWizardExecute {
		switch key {
		case "y", "Y":
			v.mode = v.wizardExecSource
			return v.executeWizardCmd()
		case "n", "N", "esc", "q":
			v.mode = v.wizardExecSource
			v.wizardExecCommand = ""
			v.wizardExecLabel = ""
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

	// Handle topology mode with navigation and expandable pipelines
	if v.mode == ModeTopology {
		switch key {
		case "t", "esc", "q":
			v.mode = ModeNormal
			v.showTopology = false
		case "j", "down":
			v.topologyNavigateDown()
		case "k", "up":
			v.topologyNavigateUp()
		case "enter", " ":
			v.toggleTopologyExpansion()
		case "g", "home":
			v.topologySelectedIdx = 0
		case "G", "end":
			if len(v.data.Replicas) > 0 {
				v.topologySelectedIdx = len(v.data.Replicas) - 1
			}
		case "a":
			// Toggle expand all
			allExpanded := true
			for _, r := range v.data.Replicas {
				if !v.topologyExpanded[r.ApplicationName] {
					allExpanded = false
					break
				}
			}
			for _, r := range v.data.Replicas {
				v.topologyExpanded[r.ApplicationName] = !allExpanded
			}
		}
		return nil
	}

	// Handle physical wizard mode
	if v.mode == ModePhysicalWizard {
		return v.handlePhysicalWizardKeys(key)
	}

	// Handle logical wizard mode
	if v.mode == ModeLogicalWizard {
		return v.handleLogicalWizardKeys(key)
	}

	// Handle connection string builder mode
	if v.mode == ModeConnStringBuilder {
		return v.handleConnStringBuilderKeys(key)
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
		// Cycle time window and request historical data
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
		// Request lag history from SQLite for the new window
		return func() tea.Msg {
			return ui.LagHistoryRequestMsg{Window: v.timeWindow}
		}
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
		// T053: Block wizard in read-only mode
		if v.readOnly {
			v.showToast("Physical wizard is disabled in read-only mode", true)
			return nil
		}
		// T054: Launch physical replication setup wizard
		v.initPhysicalWizard()
		v.mode = ModePhysicalWizard
	case "o":
		// T074: Block wizard in read-only mode
		if v.readOnly {
			v.showToast("Logical wizard is disabled in read-only mode", true)
			return nil
		}
		// T074: Request tables for logical wizard
		return func() tea.Msg { return ui.TablesRequestMsg{} }
	case "n":
		// T080: Launch connection string builder
		v.initConnStringBuilder()
		v.mode = ModeConnStringBuilder
	case "c":
		// T045: Integrate configuration checker into Setup tab
		v.mode = ModeConfigCheck
	}
	return nil
}

// handlePhysicalWizardKeys handles keys specific to the physical wizard.
func (v *ReplicationView) handlePhysicalWizardKeys(key string) tea.Cmd {
	if v.physicalWizard == nil {
		v.mode = ModeNormal
		return nil
	}

	w := v.physicalWizard

	// If editing a text field, handle text input
	if w.EditingField >= 0 {
		return v.handleWizardTextInput(key)
	}

	// Normal navigation mode
	switch key {
	case "esc", "q":
		// Cancel wizard
		v.physicalWizard = nil
		v.mode = ModeNormal
		return nil

	case "j", "down":
		// Move selection down
		w.SelectedField++
		v.ensureWizardFieldValid()

	case "k", "up":
		// Move selection up
		if w.SelectedField > 0 {
			w.SelectedField--
		}

	case ">":
		// Move to next step
		if w.Step < setup.StepReview {
			w.Step++
			w.SelectedField = 0
		}

	case "<":
		// Move to previous step
		if w.Step > setup.StepUserConfig {
			w.Step--
			w.SelectedField = 0
		}

	case "enter":
		// Start editing text field (only for text-editable fields)
		// Toggle/cycle fields (password mode, SSL, auth method, sync mode, replica count)
		// should use Space key instead - Enter does nothing on these fields
		if v.isEditableField() {
			v.startEditingField()
		}
		// Note: We intentionally don't advance to next step on Enter
		// Use ">" key to advance between steps

	case " ":
		// Toggle/cycle for specific fields
		v.handleWizardSpaceKey()

	case "v":
		// Toggle password visibility (Step 1)
		if w.Step == setup.StepUserConfig {
			w.Config.PasswordShown = !w.Config.PasswordShown
		}

	case "r":
		// Regenerate password (Step 1)
		if w.Step == setup.StepUserConfig && w.Config.AutoGenPass {
			newPass, err := setup.GenerateReplicationPassword()
			if err == nil {
				w.Config.Password = newPass
				v.showToast("Password regenerated", false)
			}
		}

	case "+":
		// Increase replica count (Step 3: Replication Mode)
		if w.Step == setup.StepReplicationMode && w.Config.ReplicaCount < 5 {
			w.Config.ReplicaCount++
			// Add new replica name if needed
			for len(w.Config.ReplicaNames) < w.Config.ReplicaCount {
				w.Config.ReplicaNames = append(w.Config.ReplicaNames,
					fmt.Sprintf("replica%d", len(w.Config.ReplicaNames)+1))
			}
		}

	case "-":
		// Decrease replica count (Step 3: Replication Mode)
		if w.Step == setup.StepReplicationMode && w.Config.ReplicaCount > 1 {
			w.Config.ReplicaCount--
		}

	case "y":
		// Copy to clipboard (Step 4: Review)
		if w.Step == setup.StepReview {
			cmd := setup.GetSelectedCommand(w)
			if cmd != "" {
				if !v.clipboard.IsAvailable() {
					v.showToast("Clipboard unavailable", true)
					return nil
				}
				if err := v.clipboard.Write(cmd); err != nil {
					v.showToast("Copy failed: "+err.Error(), true)
					return nil
				}
				v.showToast("Copied to clipboard", false)
			}
		}

	case "x":
		// Execute selected command (Step 4: Review)
		if w.Step == setup.StepReview {
			if v.readOnly {
				v.showToast("Cannot execute in read-only mode", true)
				return nil
			}
			if !setup.IsSelectedCommandExecutable(w) {
				v.showToast("This command cannot be executed remotely", true)
				return nil
			}
			// Store command details and show confirmation
			v.wizardExecCommand = setup.GetSelectedCommand(w)
			v.wizardExecLabel = setup.GetSelectedCommandLabel(w)
			v.wizardExecSource = ModePhysicalWizard
			v.mode = ModeConfirmWizardExecute
		}
	}

	return nil
}

// handleWizardTextInput handles text input when editing a field.
func (v *ReplicationView) handleWizardTextInput(key string) tea.Cmd {
	w := v.physicalWizard

	switch key {
	case "enter":
		// Commit edit
		v.commitEditingField()
		w.EditingField = -1
	case "esc":
		// Cancel edit
		w.EditingField = -1
		w.InputBuffer = ""
	case "backspace":
		// Delete character
		if len(w.InputBuffer) > 0 {
			w.InputBuffer = w.InputBuffer[:len(w.InputBuffer)-1]
		}
	default:
		// Add character (only printable)
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			w.InputBuffer += key
		}
	}
	return nil
}

// isEditableField returns true if the current field is a text-editable field.
func (v *ReplicationView) isEditableField() bool {
	w := v.physicalWizard
	switch w.Step {
	case setup.StepUserConfig:
		return w.SelectedField == 0 || w.SelectedField == 2 // username, password
	case setup.StepNetworkSecurity:
		return w.SelectedField <= 2 // host, port, cidr (not ssl/auth which are cycled)
	case setup.StepReplicationMode:
		// Replica names and data dir are editable
		return w.SelectedField >= 2 // replica names start at 2, data dir after
	default:
		return false
	}
}

// startEditingField initializes editing mode for the current field.
func (v *ReplicationView) startEditingField() {
	w := v.physicalWizard
	w.EditingField = w.SelectedField

	// Initialize buffer with current value
	switch w.Step {
	case setup.StepUserConfig:
		if w.SelectedField == 0 {
			w.InputBuffer = w.Config.Username
		} else if w.SelectedField == 2 {
			w.InputBuffer = w.Config.Password
		}
	case setup.StepNetworkSecurity:
		switch w.SelectedField {
		case 0:
			w.InputBuffer = w.Config.PrimaryHost
		case 1:
			w.InputBuffer = w.Config.PrimaryPort
		case 2:
			w.InputBuffer = w.Config.ReplicaCIDR
		}
	case setup.StepReplicationMode:
		if w.SelectedField >= 2 && w.SelectedField < 2+w.Config.ReplicaCount {
			idx := w.SelectedField - 2
			w.InputBuffer = w.Config.ReplicaNames[idx]
		} else if w.SelectedField == 2+w.Config.ReplicaCount {
			w.InputBuffer = w.Config.DataDir
		}
	}
}

// commitEditingField saves the edited value to the config.
func (v *ReplicationView) commitEditingField() {
	w := v.physicalWizard
	if w.InputBuffer == "" {
		return // Don't commit empty values
	}

	switch w.Step {
	case setup.StepUserConfig:
		if w.SelectedField == 0 {
			w.Config.Username = w.InputBuffer
		} else if w.SelectedField == 2 {
			w.Config.Password = w.InputBuffer
		}
	case setup.StepNetworkSecurity:
		switch w.SelectedField {
		case 0:
			w.Config.PrimaryHost = w.InputBuffer
		case 1:
			w.Config.PrimaryPort = w.InputBuffer
		case 2:
			w.Config.ReplicaCIDR = w.InputBuffer
		}
	case setup.StepReplicationMode:
		if w.SelectedField >= 2 && w.SelectedField < 2+w.Config.ReplicaCount {
			idx := w.SelectedField - 2
			w.Config.ReplicaNames[idx] = w.InputBuffer
		} else if w.SelectedField == 2+w.Config.ReplicaCount {
			w.Config.DataDir = w.InputBuffer
		}
	}
	w.InputBuffer = ""
}

// ensureWizardFieldValid ensures the selected field is within valid range.
func (v *ReplicationView) ensureWizardFieldValid() {
	if v.physicalWizard == nil {
		return
	}
	w := v.physicalWizard
	maxField := setup.GetMaxFieldForStep(w)
	if w.SelectedField > maxField {
		w.SelectedField = maxField
	}
}

// handleWizardSpaceKey handles space key for toggles in the wizard.
func (v *ReplicationView) handleWizardSpaceKey() {
	if v.physicalWizard == nil {
		return
	}
	w := v.physicalWizard

	switch w.Step {
	case setup.StepUserConfig:
		// Toggle password mode if on that field
		if w.SelectedField == 1 {
			w.Config.AutoGenPass = !w.Config.AutoGenPass
			if w.Config.AutoGenPass {
				// Regenerate password when switching to auto
				newPass, err := setup.GenerateReplicationPassword()
				if err == nil {
					w.Config.Password = newPass
				}
			}
		}
	case setup.StepNetworkSecurity:
		// Cycle SSL mode or Auth method
		if w.SelectedField == 3 {
			w.Config.SSLMode = setup.CycleSSLMode(w.Config.SSLMode)
		} else if w.SelectedField == 4 {
			w.Config.AuthMethod = setup.CycleAuthMethod(w.Config.AuthMethod)
		}
	case setup.StepReplicationMode:
		// Toggle sync mode if on that field
		if w.SelectedField == 0 {
			if w.Config.SyncMode == "async" {
				w.Config.SyncMode = "sync"
			} else {
				w.Config.SyncMode = "async"
			}
		}
	}
}

// handleLogicalWizardKeys handles keys specific to the logical wizard.
func (v *ReplicationView) handleLogicalWizardKeys(key string) tea.Cmd {
	if v.logicalWizard == nil {
		v.mode = ModeNormal
		return nil
	}

	w := v.logicalWizard

	// If editing a text field, handle text input
	if w.EditingField >= 0 {
		return v.handleLogicalWizardTextInput(key)
	}

	// Normal navigation mode
	switch key {
	case "esc", "q":
		// Cancel wizard
		v.logicalWizard = nil
		v.mode = ModeNormal
		return nil

	case "j", "down":
		// Move selection down
		w.SelectedField++
		v.ensureLogicalWizardFieldValid()

	case "k", "up":
		// Move selection up
		if w.SelectedField > 0 {
			w.SelectedField--
		}

	case ">":
		// Move to next step
		nextStep := setup.GetNextLogicalStep(w)
		if nextStep != w.Step {
			w.Step = nextStep
			w.SelectedField = 0
		}

	case "<":
		// Move to previous step
		prevStep := setup.GetPrevLogicalStep(w)
		if prevStep != w.Step {
			w.Step = prevStep
			w.SelectedField = 0
		}

	case "enter":
		// Start editing text field or select mode
		if w.Step == setup.LogicalStepType {
			// Select the mode and advance
			if w.SelectedField == 0 {
				w.Config.Mode = setup.LogicalModePublication
			} else {
				w.Config.Mode = setup.LogicalModeSubscription
			}
			w.Step = setup.GetNextLogicalStep(w)
			w.SelectedField = 0
		} else if v.isLogicalEditableField() {
			v.startLogicalEditingField()
		}

	case " ":
		// Toggle/select
		v.handleLogicalWizardSpaceKey()

	case "y":
		// Copy to clipboard (Review step)
		if w.Step == setup.LogicalStepReview {
			cmd := setup.GetLogicalSelectedCommand(w)
			if cmd != "" {
				if !v.clipboard.IsAvailable() {
					v.showToast("Clipboard unavailable", true)
					return nil
				}
				if err := v.clipboard.Write(cmd); err != nil {
					v.showToast("Copy failed: "+err.Error(), true)
					return nil
				}
				v.showToast("Copied to clipboard", false)
			}
		}

	case "x":
		// Execute selected command (Review step)
		if w.Step == setup.LogicalStepReview {
			if v.readOnly {
				v.showToast("Cannot execute in read-only mode", true)
				return nil
			}
			if !setup.IsLogicalSelectedCommandExecutable(w) {
				v.showToast("This command cannot be executed remotely", true)
				return nil
			}
			// Store command details and show confirmation
			v.wizardExecCommand = setup.GetLogicalSelectedCommand(w)
			v.wizardExecLabel = "Logical Replication SQL"
			v.wizardExecSource = ModeLogicalWizard
			v.mode = ModeConfirmWizardExecute
		}
	}

	return nil
}

// handleLogicalWizardTextInput handles text input when editing a field in logical wizard.
func (v *ReplicationView) handleLogicalWizardTextInput(key string) tea.Cmd {
	w := v.logicalWizard

	switch key {
	case "enter":
		// Commit edit
		v.commitLogicalEditingField()
		w.EditingField = -1
	case "esc":
		// Cancel edit
		w.EditingField = -1
		w.InputBuffer = ""
	case "backspace":
		// Delete character
		if len(w.InputBuffer) > 0 {
			w.InputBuffer = w.InputBuffer[:len(w.InputBuffer)-1]
		}
	default:
		// Add character (only printable)
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			w.InputBuffer += key
		}
	}
	return nil
}

// isLogicalEditableField returns true if the current field is a text-editable field.
func (v *ReplicationView) isLogicalEditableField() bool {
	w := v.logicalWizard
	switch w.Step {
	case setup.LogicalStepTableSelection:
		return w.SelectedField == 0 // publication name
	case setup.LogicalStepConnection:
		return w.SelectedField <= 6 // text fields before toggles
	default:
		return false
	}
}

// startLogicalEditingField initializes editing mode for the current field.
func (v *ReplicationView) startLogicalEditingField() {
	w := v.logicalWizard
	w.EditingField = w.SelectedField

	// Initialize buffer with current value
	switch w.Step {
	case setup.LogicalStepTableSelection:
		if w.SelectedField == 0 {
			w.InputBuffer = w.Config.PublicationName
		}
	case setup.LogicalStepConnection:
		switch w.SelectedField {
		case 0:
			w.InputBuffer = w.Config.SubscriptionName
		case 1:
			w.InputBuffer = w.Config.PublicationNames[0]
		case 2:
			w.InputBuffer = w.Config.ConnectionHost
		case 3:
			w.InputBuffer = w.Config.ConnectionPort
		case 4:
			w.InputBuffer = w.Config.ConnectionDB
		case 5:
			w.InputBuffer = w.Config.ConnectionUser
		case 6:
			w.InputBuffer = w.Config.ConnectionPass
		}
	}
}

// commitLogicalEditingField saves the edited value to the config.
func (v *ReplicationView) commitLogicalEditingField() {
	w := v.logicalWizard
	if w.InputBuffer == "" {
		return // Don't commit empty values
	}

	switch w.Step {
	case setup.LogicalStepTableSelection:
		if w.SelectedField == 0 {
			w.Config.PublicationName = w.InputBuffer
		}
	case setup.LogicalStepConnection:
		switch w.SelectedField {
		case 0:
			w.Config.SubscriptionName = w.InputBuffer
		case 1:
			w.Config.PublicationNames = []string{w.InputBuffer}
		case 2:
			w.Config.ConnectionHost = w.InputBuffer
		case 3:
			w.Config.ConnectionPort = w.InputBuffer
		case 4:
			w.Config.ConnectionDB = w.InputBuffer
		case 5:
			w.Config.ConnectionUser = w.InputBuffer
		case 6:
			w.Config.ConnectionPass = w.InputBuffer
		}
	}
	w.InputBuffer = ""
}

// ensureLogicalWizardFieldValid ensures the selected field is within valid range.
func (v *ReplicationView) ensureLogicalWizardFieldValid() {
	if v.logicalWizard == nil {
		return
	}
	w := v.logicalWizard
	maxField := setup.GetLogicalMaxFieldForStep(w)
	if w.SelectedField > maxField {
		w.SelectedField = maxField
	}
}

// handleLogicalWizardSpaceKey handles space key for toggles in the logical wizard.
func (v *ReplicationView) handleLogicalWizardSpaceKey() {
	if v.logicalWizard == nil {
		return
	}
	w := v.logicalWizard

	switch w.Step {
	case setup.LogicalStepType:
		// Toggle mode selection
		if w.SelectedField == 0 {
			w.Config.Mode = setup.LogicalModePublication
		} else {
			w.Config.Mode = setup.LogicalModeSubscription
		}

	case setup.LogicalStepTableSelection:
		// Toggle all tables or individual table
		if w.SelectedField == 1 {
			w.Config.AllTables = !w.Config.AllTables
		} else if w.SelectedField == 2 && len(w.Tables) > 0 {
			// Toggle table selection
			t := w.Tables[w.TableCursor]
			fullName := t.Schema + "." + t.Name
			w.Config.SelectedTables[fullName] = !w.Config.SelectedTables[fullName]
		}

	case setup.LogicalStepOperations:
		// Toggle operations
		switch w.SelectedField {
		case 0:
			w.Config.OpInsert = !w.Config.OpInsert
		case 1:
			w.Config.OpUpdate = !w.Config.OpUpdate
		case 2:
			w.Config.OpDelete = !w.Config.OpDelete
		case 3:
			w.Config.OpTruncate = !w.Config.OpTruncate
		}

	case setup.LogicalStepConnection:
		// Toggle copy_data and enabled
		if w.SelectedField == 7 {
			w.Config.CopyData = !w.Config.CopyData
		} else if w.SelectedField == 8 {
			w.Config.Enabled = !w.Config.Enabled
		}
	}
}

// handleConnStringBuilderKeys handles keys for the connection string builder.
func (v *ReplicationView) handleConnStringBuilderKeys(key string) tea.Cmd {
	if v.connStringBuilder == nil {
		v.mode = ModeNormal
		return nil
	}

	s := v.connStringBuilder

	// If editing a text field, handle text input
	if s.EditingField >= 0 {
		return v.handleConnStringTextInput(key)
	}

	// Normal navigation mode
	switch key {
	case "esc", "q":
		// Close builder
		v.connStringBuilder = nil
		v.mode = ModeNormal
		return nil

	case "j", "down":
		// Move selection down
		if s.SelectedField < setup.GetConnStringMaxField() {
			s.SelectedField++
		}

	case "k", "up":
		// Move selection up
		if s.SelectedField > 0 {
			s.SelectedField--
		}

	case "enter":
		// Start editing text field (except SSL mode)
		if setup.IsConnStringFieldEditable(s.SelectedField) {
			s.EditingField = s.SelectedField
			s.InputBuffer = setup.GetConnStringFieldValue(s, s.SelectedField)
		}

	case " ":
		// Cycle SSL mode if on that field
		if s.SelectedField == 6 {
			s.Config.SSLMode = setup.CycleSSLMode(s.Config.SSLMode)
		}

	case "t":
		// T077: Test connection
		if s.Testing {
			return nil // Already testing
		}
		s.Testing = true
		s.TestResult = ""
		connStr := setup.GetConnStringForTest(&s.Config)
		return func() tea.Msg {
			return ui.ConnTestRequestMsg{ConnString: connStr}
		}

	case "y":
		// T079: Copy to clipboard
		if !v.clipboard.IsAvailable() {
			v.showToast("Clipboard unavailable", true)
			return nil
		}
		connStr := setup.GeneratePrimaryConnInfoFromConfig(&s.Config)
		if err := v.clipboard.Write(connStr); err != nil {
			v.showToast("Copy failed: "+err.Error(), true)
			return nil
		}
		v.showToast("Copied primary_conninfo to clipboard", false)
	}

	return nil
}

// handleConnStringTextInput handles text input when editing a field.
func (v *ReplicationView) handleConnStringTextInput(key string) tea.Cmd {
	s := v.connStringBuilder

	switch key {
	case "enter":
		// Commit edit
		if s.InputBuffer != "" || s.SelectedField == 3 || s.SelectedField == 4 || s.SelectedField == 8 {
			// Allow empty for password, database, and target_session
			setup.SetConnStringFieldValue(s, s.EditingField, s.InputBuffer)
		}
		s.EditingField = -1
		s.InputBuffer = ""
	case "esc":
		// Cancel edit
		s.EditingField = -1
		s.InputBuffer = ""
	case "backspace":
		// Delete character
		if len(s.InputBuffer) > 0 {
			s.InputBuffer = s.InputBuffer[:len(s.InputBuffer)-1]
		}
	default:
		// Add character (only printable)
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			s.InputBuffer += key
		}
	}
	return nil
}
