// Package setup provides setup wizard and configuration check components.
package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// ConfigCheckConfig holds configuration for the config checker rendering.
type ConfigCheckConfig struct {
	Width  int
	Height int
}

// ConfigEditorState holds state for the configuration editor.
type ConfigEditorState struct {
	Params       []ConfigEditorParam
	SelectedIdx  int
	Editing      bool
	InputBuffer  string
	ReadOnly     bool
}

// ConfigEditorParam represents an editable configuration parameter.
type ConfigEditorParam struct {
	Name         string
	CurrentValue string
	TargetValue  string
	Unit         string
	NeedsRestart bool
	ParamType    string // "string", "int", "bool"
}

// NewConfigEditorState creates a new config editor from current config.
func NewConfigEditorState(config *models.ReplicationConfig, readOnly bool) *ConfigEditorState {
	state := &ConfigEditorState{
		ReadOnly: readOnly,
	}

	if config == nil {
		return state
	}

	// Build editable params from current config
	params := config.AllParams()
	for _, p := range params {
		editorParam := ConfigEditorParam{
			Name:         p.Name,
			CurrentValue: p.CurrentValue,
			TargetValue:  p.CurrentValue, // Start with current value
			Unit:         p.Unit,
			NeedsRestart: p.NeedsRestart,
			ParamType:    getParamType(p.Name),
		}
		state.Params = append(state.Params, editorParam)
	}

	return state
}

// getParamType returns the type of parameter for validation.
func getParamType(name string) string {
	switch name {
	case "wal_level":
		return "string"
	case "max_wal_senders", "max_replication_slots":
		return "int"
	case "wal_keep_size":
		return "size"
	case "hot_standby", "archive_mode":
		return "bool"
	default:
		return "string"
	}
}

// GetSelectedParam returns the currently selected parameter.
func (s *ConfigEditorState) GetSelectedParam() *ConfigEditorParam {
	if s.SelectedIdx >= 0 && s.SelectedIdx < len(s.Params) {
		return &s.Params[s.SelectedIdx]
	}
	return nil
}

// MoveUp moves selection up.
func (s *ConfigEditorState) MoveUp() {
	if s.SelectedIdx > 0 {
		s.SelectedIdx--
	}
}

// MoveDown moves selection down.
func (s *ConfigEditorState) MoveDown() {
	if s.SelectedIdx < len(s.Params)-1 {
		s.SelectedIdx++
	}
}

// StartEditing begins editing the selected parameter.
func (s *ConfigEditorState) StartEditing() {
	if s.ReadOnly {
		return
	}
	p := s.GetSelectedParam()
	if p != nil {
		s.Editing = true
		s.InputBuffer = p.TargetValue
	}
}

// CommitEdit commits the current edit.
func (s *ConfigEditorState) CommitEdit() {
	if s.Editing {
		p := s.GetSelectedParam()
		if p != nil {
			p.TargetValue = s.InputBuffer
		}
		s.Editing = false
		s.InputBuffer = ""
	}
}

// CancelEdit cancels the current edit.
func (s *ConfigEditorState) CancelEdit() {
	s.Editing = false
	s.InputBuffer = ""
}

// HasChanges returns true if any target differs from current.
func (s *ConfigEditorState) HasChanges() bool {
	for _, p := range s.Params {
		if p.TargetValue != p.CurrentValue {
			return true
		}
	}
	return false
}

// GetChangedParams returns only params that have been changed.
func (s *ConfigEditorState) GetChangedParams() []ConfigEditorParam {
	var changed []ConfigEditorParam
	for _, p := range s.Params {
		if p.TargetValue != p.CurrentValue {
			changed = append(changed, p)
		}
	}
	return changed
}

// GenerateAlterCommands generates ALTER SYSTEM commands for changed params.
func (s *ConfigEditorState) GenerateAlterCommands() []string {
	var commands []string
	for _, p := range s.GetChangedParams() {
		cmd := generateAlterCommand(p)
		if cmd != "" {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// generateAlterCommand generates an ALTER SYSTEM command for a param.
func generateAlterCommand(p ConfigEditorParam) string {
	switch p.ParamType {
	case "string", "bool":
		return fmt.Sprintf("ALTER SYSTEM SET %s = '%s';", p.Name, p.TargetValue)
	case "int":
		return fmt.Sprintf("ALTER SYSTEM SET %s = %s;", p.Name, p.TargetValue)
	case "size":
		// Size values like '1GB' need quotes
		return fmt.Sprintf("ALTER SYSTEM SET %s = '%s';", p.Name, p.TargetValue)
	default:
		return fmt.Sprintf("ALTER SYSTEM SET %s = '%s';", p.Name, p.TargetValue)
	}
}

// RequiresRestart returns true if any changed param needs restart.
func (s *ConfigEditorState) RequiresRestart() bool {
	for _, p := range s.GetChangedParams() {
		if p.NeedsRestart {
			return true
		}
	}
	return false
}

// RenderConfigEditor renders the interactive configuration editor.
func RenderConfigEditor(state *ConfigEditorState, width, height int) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("Replication Configuration Editor"))
	b.WriteString("\n\n")

	if state == nil || len(state.Params) == 0 {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Configuration data not available."))
		b.WriteString("\n\n")
		b.WriteString(renderEditorFooter(state))
		return b.String()
	}

	// Column headers
	colHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	header := padToWidth("Parameter", 24) + padToWidth("Current", 14) + padToWidth("Target", 14) + "Status"
	b.WriteString(colHeaderStyle.Render(header))
	b.WriteString("\n")

	// Separator
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	b.WriteString(sepStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")

	// Render each parameter row
	for i, p := range state.Params {
		selected := i == state.SelectedIdx
		editing := selected && state.Editing
		b.WriteString(renderEditorRow(p, selected, editing, state.InputBuffer))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Show pending changes
	if state.HasChanges() {
		b.WriteString(renderPendingChanges(state))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(renderEditorFooter(state))

	return b.String()
}

// renderEditorRow renders a single parameter row in the editor.
func renderEditorRow(p ConfigEditorParam, selected, editing bool, inputBuffer string) string {
	// Base style
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Name column
	name := padToWidth(p.Name, 24)

	// Current value column
	currentVal := p.CurrentValue
	if p.Unit != "" {
		currentVal = p.CurrentValue + " " + p.Unit
	}
	current := padToWidth(truncate(currentVal, 13), 14)

	// Target value column
	var target string
	if editing {
		// Show input buffer with cursor
		targetStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Background(lipgloss.Color("236"))
		target = targetStyle.Render(padToWidth("["+inputBuffer+"_]", 14))
	} else {
		targetVal := p.TargetValue
		if p.Unit != "" && p.TargetValue != p.CurrentValue {
			targetVal = p.TargetValue + " " + p.Unit
		}
		if p.TargetValue != p.CurrentValue {
			// Changed - highlight in cyan
			targetStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
			target = targetStyle.Render(padToWidth(truncate(targetVal, 13), 14))
		} else {
			target = padToWidth(truncate(targetVal, 13), 14)
		}
	}

	// Status column
	var status string
	if p.TargetValue != p.CurrentValue {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("modified")
		if p.NeedsRestart {
			status += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(" *restart")
		}
	} else {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
	}

	// Selection indicator
	indicator := "  "
	if selected {
		indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render("▸ ")
	}

	return indicator + baseStyle.Render(name+current) + target + " " + status
}

// renderPendingChanges shows the commands that will be executed.
func renderPendingChanges(state *ConfigEditorState) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	b.WriteString(headerStyle.Render("Pending Changes"))
	b.WriteString("\n\n")

	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	commands := state.GenerateAlterCommands()
	for _, cmd := range commands {
		b.WriteString("  " + cmdStyle.Render(cmd) + "\n")
	}

	if state.RequiresRestart() {
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  * Some changes require PostgreSQL restart"))
	}

	return b.String()
}

// renderEditorFooter renders the footer with available actions.
func renderEditorFooter(state *ConfigEditorState) string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	if state != nil && state.Editing {
		return footerStyle.Render("[Enter]save  [Esc]cancel")
	}

	var parts []string
	parts = append(parts, "[j/k]navigate", "[Enter]edit")

	if state != nil && !state.ReadOnly && state.HasChanges() {
		parts = append(parts, "[x]execute", "[y]copy")
	} else if state != nil && state.HasChanges() {
		parts = append(parts, "[y]copy")
	}

	parts = append(parts, "[Esc]back")

	return footerStyle.Render(strings.Join(parts, "  "))
}

// RenderConfigCheck renders the configuration checker panel.
// T041: Implement configuration checker panel showing wal_level, max_wal_senders,
//
//	max_replication_slots, wal_keep_size, hot_standby, archive_mode
//
// T042: Display green checkmark for correctly configured parameters
// T043: Display red X with guidance text for misconfigured parameters
// T044: Show overall "READY" or "NOT READY" status summary
func RenderConfigCheck(config *models.ReplicationConfig, cfg ConfigCheckConfig) string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Configuration Readiness Check"))
	b.WriteString("\n\n")

	if config == nil {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Configuration data not available."))
		b.WriteString("\n\n")
		b.WriteString(renderConfigFooter())
		return b.String()
	}

	// Overall status summary (T044)
	b.WriteString(renderOverallStatus(config))
	b.WriteString("\n\n")

	// Parameter table
	b.WriteString(renderParameterTable(config))
	b.WriteString("\n")

	// Guidance section for misconfigured parameters
	issues := config.GetIssues()
	if len(issues) > 0 {
		b.WriteString(renderGuidance(issues))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(renderConfigFooter())
	return b.String()
}

// renderOverallStatus renders the READY or NOT READY status.
func renderOverallStatus(config *models.ReplicationConfig) string {
	var statusText string
	var statusStyle lipgloss.Style

	if config.IsReady() {
		statusText = "✓ READY"
		statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")). // Green
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("42"))
	} else {
		statusText = "✗ NOT READY"
		statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")). // Red
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196"))

		if config.RequiresRestart() {
			statusText += " (restart required)"
		}
	}

	return statusStyle.Render(statusText)
}

// renderParameterTable renders the configuration parameters as a table.
func renderParameterTable(config *models.ReplicationConfig) string {
	var b strings.Builder

	// Column headers matching row widths: name(22) + value(12) + required(30) + status
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	header := padToWidth("Parameter", 22) + padToWidth("Current", 12) + padToWidth("Required", 30) + "Status"
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Separator
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	b.WriteString(sepStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n")

	// Render each parameter
	params := config.AllParams()
	for _, p := range params {
		b.WriteString(renderParamRow(p))
		b.WriteString("\n")
	}

	return b.String()
}

// renderParamRow renders a single parameter row with status indicator.
// T042: Display green checkmark for correctly configured parameters
// T043: Display red X for misconfigured parameters
func renderParamRow(p models.ConfigParam) string {
	// Status indicator (T042 & T043)
	var statusIndicator string
	var statusColor lipgloss.Color

	if p.IsValid {
		statusIndicator = "✓"
		statusColor = lipgloss.Color("42") // Green
	} else {
		statusIndicator = "✗"
		statusColor = lipgloss.Color("196") // Red
	}

	// Add unit to current value if present
	currentValue := p.CurrentValue
	if p.Unit != "" && currentValue != "" {
		currentValue = fmt.Sprintf("%s %s", currentValue, p.Unit)
	}

	// Truncate and pad values to fixed widths
	name := padToWidth(p.Name, 22)
	value := padToWidth(truncate(currentValue, 11), 12)
	required := padToWidth(truncate(p.RequiredValue, 24), 32) // Wider to push status right

	// Apply colors
	valueStyle := lipgloss.NewStyle()
	if !p.IsValid {
		valueStyle = valueStyle.Foreground(lipgloss.Color("196"))
	}
	requiredStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle := lipgloss.NewStyle().Foreground(statusColor)

	// Add restart indicator
	restartIndicator := ""
	if p.NeedsRestart && !p.IsValid {
		restartIndicator = " *"
	}

	return name + valueStyle.Render(value) + requiredStyle.Render(required) + statusStyle.Render(statusIndicator) + restartIndicator
}

// padToWidth pads a string to exact width with spaces.
func padToWidth(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// renderGuidance renders guidance text for misconfigured parameters.
func renderGuidance(issues []models.ConfigParam) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	b.WriteString(headerStyle.Render("Guidance"))
	b.WriteString("\n\n")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	for _, p := range issues {
		guidance := getParamGuidance(p)
		b.WriteString(fmt.Sprintf("  • %s: %s\n", p.Name, guidance))
	}

	b.WriteString("\n")
	if hasRestartRequired(issues) {
		b.WriteString(hintStyle.Render("  * Parameters marked with * require PostgreSQL restart"))
		b.WriteString("\n")
	}

	// T096: Add ALTER SYSTEM command generation section
	alterCommands := renderAlterSystemCommands(issues)
	if alterCommands != "" {
		b.WriteString("\n")
		b.WriteString(alterCommands)
	}

	return b.String()
}

// renderAlterSystemCommands generates ALTER SYSTEM commands for misconfigured parameters.
// T096: Add ALTER SYSTEM command generation for wal_level, max_wal_senders, max_replication_slots
func renderAlterSystemCommands(issues []models.ConfigParam) string {
	if len(issues) == 0 {
		return ""
	}

	var b strings.Builder
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(headerStyle.Render("ALTER SYSTEM Commands"))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("  Run these commands as superuser to fix configuration:"))
	b.WriteString("\n\n")

	for _, p := range issues {
		cmd := getAlterSystemCommand(p)
		if cmd != "" {
			// T097: Add restart indicator for postmaster-context parameters
			restartHint := ""
			if p.NeedsRestart {
				restartHint = hintStyle.Render("  -- requires restart")
			}
			b.WriteString("  " + cmdStyle.Render(cmd) + restartHint + "\n")
		}
	}

	// Add reload/restart hint
	b.WriteString("\n")
	if hasRestartRequired(issues) {
		b.WriteString(hintStyle.Render("  -- After running commands, restart PostgreSQL:"))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  -- pg_ctl restart -D $PGDATA"))
	} else {
		b.WriteString(hintStyle.Render("  -- After running commands, reload configuration:"))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  -- SELECT pg_reload_conf();"))
	}
	b.WriteString("\n")

	return b.String()
}

// getAlterSystemCommand returns the ALTER SYSTEM command for a parameter.
func getAlterSystemCommand(p models.ConfigParam) string {
	switch p.Name {
	case "wal_level":
		// wal_level needs 'replica' or 'logical' - suggest replica as default
		return "ALTER SYSTEM SET wal_level = 'replica';"
	case "max_wal_senders":
		// Recommend at least 10 for flexibility
		return "ALTER SYSTEM SET max_wal_senders = 10;"
	case "max_replication_slots":
		// Recommend at least 10 for flexibility
		return "ALTER SYSTEM SET max_replication_slots = 10;"
	case "wal_keep_size":
		// Recommend 1GB for WAL retention
		return "ALTER SYSTEM SET wal_keep_size = '1GB';"
	case "hot_standby":
		return "ALTER SYSTEM SET hot_standby = 'on';"
	case "archive_mode":
		return "ALTER SYSTEM SET archive_mode = 'on';"
	default:
		return ""
	}
}

// getParamGuidance returns specific guidance for a misconfigured parameter.
func getParamGuidance(p models.ConfigParam) string {
	switch p.Name {
	case "wal_level":
		return fmt.Sprintf("Set to 'replica' or 'logical' in postgresql.conf (current: %s)", p.CurrentValue)
	case "max_wal_senders":
		return fmt.Sprintf("Set to at least 2 (one per replica + buffer) in postgresql.conf (current: %s)", p.CurrentValue)
	case "max_replication_slots":
		return fmt.Sprintf("Set to at least 1 per replica/subscriber in postgresql.conf (current: %s)", p.CurrentValue)
	case "wal_keep_size":
		return fmt.Sprintf("Recommended: Set to retain WAL for replica reconnection (current: %s)", p.CurrentValue)
	case "hot_standby":
		return fmt.Sprintf("Set to 'on' to allow queries on standby servers (current: %s)", p.CurrentValue)
	case "archive_mode":
		return fmt.Sprintf("Set to 'on' for point-in-time recovery (current: %s)", p.CurrentValue)
	default:
		return fmt.Sprintf("Current value: %s, required: %s", p.CurrentValue, p.RequiredValue)
	}
}

// hasRestartRequired checks if any issues require a restart.
func hasRestartRequired(issues []models.ConfigParam) bool {
	for _, p := range issues {
		if p.NeedsRestart {
			return true
		}
	}
	return false
}

// renderConfigFooter renders the navigation footer.
func renderConfigFooter() string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	return footerStyle.Render("[esc/q]back")
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
