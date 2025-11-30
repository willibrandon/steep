package tables

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// PermissionsMode represents the current permissions dialog mode.
type PermissionsMode int

const (
	PermissionsModeList PermissionsMode = iota
	PermissionsModeGrant
	PermissionsModeRevokeConfirm
)

// TablePrivilegeType represents available table privileges.
type TablePrivilegeType string

const (
	PrivilegeSelect     TablePrivilegeType = "SELECT"
	PrivilegeInsert     TablePrivilegeType = "INSERT"
	PrivilegeUpdate     TablePrivilegeType = "UPDATE"
	PrivilegeDelete     TablePrivilegeType = "DELETE"
	PrivilegeTruncate   TablePrivilegeType = "TRUNCATE"
	PrivilegeReferences TablePrivilegeType = "REFERENCES"
	PrivilegeTrigger    TablePrivilegeType = "TRIGGER"
	PrivilegeAll        TablePrivilegeType = "ALL"
)

// AllTablePrivileges returns all available table privileges.
func AllTablePrivileges() []TablePrivilegeType {
	return []TablePrivilegeType{
		PrivilegeSelect,
		PrivilegeInsert,
		PrivilegeUpdate,
		PrivilegeDelete,
		PrivilegeTruncate,
		PrivilegeReferences,
		PrivilegeTrigger,
		PrivilegeAll,
	}
}

// PermissionsDialog manages the permissions view for a table.
type PermissionsDialog struct {
	TableOID      uint32
	SchemaName    string
	TableName     string
	Permissions   []queries.TablePermission
	RoleNames     []string // Available roles for grant
	SelectedIndex int
	ScrollOffset  int
	Mode          PermissionsMode
	Width         int
	Height        int
	ReadOnlyMode  bool
	Loading       bool
	Error         error

	// Grant dialog state
	grantDialog *GrantDialog

	// Revoke confirmation state
	revokeTarget *queries.TablePermission
}

// NewPermissionsDialog creates a new permissions dialog for a table.
func NewPermissionsDialog(tableOID uint32, schemaName, tableName string, width, height int, readOnly bool) *PermissionsDialog {
	return &PermissionsDialog{
		TableOID:     tableOID,
		SchemaName:   schemaName,
		TableName:    tableName,
		Mode:         PermissionsModeList,
		Width:        width,
		Height:       height,
		ReadOnlyMode: readOnly,
		Loading:      true,
	}
}

// SetSize updates the dialog dimensions.
func (d *PermissionsDialog) SetSize(width, height int) {
	d.Width = width
	d.Height = height
}

// Update handles key presses for the permissions dialog.
func (d *PermissionsDialog) Update(key string) (done bool, cmd tea.Cmd) {
	switch d.Mode {
	case PermissionsModeList:
		return d.updateList(key)
	case PermissionsModeGrant:
		return d.updateGrant(key)
	case PermissionsModeRevokeConfirm:
		return d.updateRevokeConfirm(key)
	}
	return false, nil
}

// updateList handles key presses in list mode.
func (d *PermissionsDialog) updateList(key string) (done bool, cmd tea.Cmd) {
	switch key {
	case "esc", "q":
		return true, nil
	case "j", "down":
		d.moveSelection(1)
	case "k", "up":
		d.moveSelection(-1)
	case "g", "home":
		d.SelectedIndex = 0
		d.ScrollOffset = 0
	case "G", "end":
		d.SelectedIndex = max(0, len(d.Permissions)-1)
		d.ensureVisible()
	case "a", "+":
		// Add permission (grant)
		if d.ReadOnlyMode {
			return false, nil
		}
		d.grantDialog = NewGrantDialog(d.SchemaName, d.TableName, d.RoleNames, d.Width)
		d.Mode = PermissionsModeGrant
	case "r", "d", "-", "delete", "backspace":
		// Revoke permission
		if d.ReadOnlyMode || len(d.Permissions) == 0 {
			return false, nil
		}
		if d.SelectedIndex >= 0 && d.SelectedIndex < len(d.Permissions) {
			d.revokeTarget = &d.Permissions[d.SelectedIndex]
			d.Mode = PermissionsModeRevokeConfirm
		}
	case "R":
		// Refresh permissions
		return false, d.fetchPermissions()
	}
	return false, nil
}

// updateGrant handles key presses in grant mode.
func (d *PermissionsDialog) updateGrant(key string) (done bool, cmd tea.Cmd) {
	if d.grantDialog == nil {
		d.Mode = PermissionsModeList
		return false, nil
	}

	finished, grantCmd := d.grantDialog.Update(key)
	if finished {
		if grantCmd != nil {
			// Grant was confirmed, execute it
			d.Mode = PermissionsModeList
			return false, grantCmd
		}
		// Grant was cancelled
		d.grantDialog = nil
		d.Mode = PermissionsModeList
	}
	return false, nil
}

// updateRevokeConfirm handles key presses in revoke confirmation mode.
func (d *PermissionsDialog) updateRevokeConfirm(key string) (done bool, cmd tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		// Confirm revoke
		if d.revokeTarget != nil {
			cmd := d.executeRevoke(d.revokeTarget)
			d.revokeTarget = nil
			d.Mode = PermissionsModeList
			return false, cmd
		}
	case "n", "N", "esc", "q":
		// Cancel revoke
		d.revokeTarget = nil
		d.Mode = PermissionsModeList
	}
	return false, nil
}

// moveSelection moves the selection by delta rows.
func (d *PermissionsDialog) moveSelection(delta int) {
	d.SelectedIndex += delta
	if d.SelectedIndex < 0 {
		d.SelectedIndex = 0
	}
	if d.SelectedIndex >= len(d.Permissions) {
		d.SelectedIndex = max(0, len(d.Permissions)-1)
	}
	d.ensureVisible()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (d *PermissionsDialog) ensureVisible() {
	visibleHeight := d.contentHeight()
	if visibleHeight <= 0 {
		return
	}

	if d.SelectedIndex < d.ScrollOffset {
		d.ScrollOffset = d.SelectedIndex
	}
	if d.SelectedIndex >= d.ScrollOffset+visibleHeight {
		d.ScrollOffset = d.SelectedIndex - visibleHeight + 1
	}
}

// contentHeight returns the number of visible permission rows.
func (d *PermissionsDialog) contentHeight() int {
	// Account for header(2) + column header(1) + footer(2) + border padding(4)
	return max(1, d.Height-9)
}

// View renders the permissions dialog.
func (d *PermissionsDialog) View() string {
	switch d.Mode {
	case PermissionsModeGrant:
		if d.grantDialog != nil {
			return d.grantDialog.View()
		}
	case PermissionsModeRevokeConfirm:
		return d.viewRevokeConfirm()
	}
	return d.viewList()
}

// viewList renders the permissions list.
func (d *PermissionsDialog) viewList() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render(fmt.Sprintf("Permissions: %s.%s", d.SchemaName, d.TableName)))
	b.WriteString("\n\n")

	// Loading state
	if d.Loading {
		b.WriteString("Loading permissions...\n")
		return d.wrapInDialog(b.String())
	}

	// Error state
	if d.Error != nil {
		errorStyle := lipgloss.NewStyle().Foreground(styles.ColorError)
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", d.Error)))
		b.WriteString("\n")
		return d.wrapInDialog(b.String())
	}

	// Column header
	colHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorMuted)
	b.WriteString(colHeaderStyle.Render(fmt.Sprintf("  %-20s %-12s %-20s %s",
		"Grantee", "Privilege", "Grantor", "Grantable")))
	b.WriteString("\n")

	// Permissions list
	if len(d.Permissions) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(styles.ColorTextDim)
		b.WriteString(dimStyle.Render("  No explicit permissions granted"))
		b.WriteString("\n")
	} else {
		visibleHeight := d.contentHeight()
		startIdx := d.ScrollOffset
		endIdx := min(startIdx+visibleHeight, len(d.Permissions))

		for i := startIdx; i < endIdx; i++ {
			p := d.Permissions[i]
			cursor := "  "
			if i == d.SelectedIndex {
				cursor = "> "
			}

			grantable := ""
			if p.IsGrantable {
				grantable = "✓"
			}

			var line string
			if i == d.SelectedIndex {
				selectedStyle := lipgloss.NewStyle().
					Bold(true).
					Background(styles.ColorSelectedBg)
				line = selectedStyle.Render(fmt.Sprintf("%s%-20s %-12s %-20s %s",
					cursor, truncate(p.Grantee, 20), p.PrivilegeType, truncate(p.Grantor, 20), grantable))
			} else {
				line = fmt.Sprintf("%s%-20s %-12s %-20s %s",
					cursor, truncate(p.Grantee, 20), p.PrivilegeType, truncate(p.Grantor, 20), grantable)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}

		// Scroll indicator
		if len(d.Permissions) > visibleHeight {
			scrollInfo := fmt.Sprintf(" (%d-%d of %d)", startIdx+1, endIdx, len(d.Permissions))
			b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(scrollInfo))
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	if d.ReadOnlyMode {
		b.WriteString(footerStyle.Render("[j/k] Navigate  [R] Refresh  [Esc] Close"))
	} else {
		b.WriteString(footerStyle.Render("[j/k] Navigate  [a/+] Grant  [r/-] Revoke  [R] Refresh  [Esc] Close"))
	}

	return d.wrapInDialog(b.String())
}

// viewRevokeConfirm renders the revoke confirmation dialog.
func (d *PermissionsDialog) viewRevokeConfirm() string {
	if d.revokeTarget == nil {
		return ""
	}

	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(headerStyle.Render("Revoke Permission?"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Revoke %s from %s on %s.%s?\n",
		d.revokeTarget.PrivilegeType,
		d.revokeTarget.Grantee,
		d.SchemaName,
		d.TableName))

	if d.revokeTarget.IsGrantable {
		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)
		b.WriteString("\n")
		b.WriteString(warningStyle.Render("⚠ This permission has WITH GRANT OPTION"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[y/Enter] Revoke  [n/Esc] Cancel"))

	return d.wrapInDialog(b.String())
}

// wrapInDialog wraps content in a dialog border.
func (d *PermissionsDialog) wrapInDialog(content string) string {
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(d.Width).
		MaxHeight(d.Height)

	return dialogStyle.Render(content)
}

// fetchPermissions returns a command to fetch table permissions.
func (d *PermissionsDialog) fetchPermissions() tea.Cmd {
	return func() tea.Msg {
		return PermissionsRefreshMsg{}
	}
}

// executeRevoke returns a command to revoke a permission.
func (d *PermissionsDialog) executeRevoke(p *queries.TablePermission) tea.Cmd {
	schema := d.SchemaName
	table := d.TableName
	role := p.Grantee
	privilege := p.PrivilegeType

	return func() tea.Msg {
		return RevokePermissionMsg{
			Schema:    schema,
			Table:     table,
			Role:      role,
			Privilege: privilege,
			Cascade:   false,
		}
	}
}

// GrantDialog handles the grant permission form.
type GrantDialog struct {
	SchemaName      string
	TableName       string
	RoleNames       []string
	Width           int
	SelectedField   int // 0 = role, 1 = privilege, 2 = with grant option
	RoleIndex       int
	PrivilegeIndex  int
	WithGrantOption bool
}

// NewGrantDialog creates a new grant dialog.
func NewGrantDialog(schema, table string, roleNames []string, width int) *GrantDialog {
	return &GrantDialog{
		SchemaName:      schema,
		TableName:       table,
		RoleNames:       roleNames,
		Width:           width,
		SelectedField:   0,
		RoleIndex:       0,
		PrivilegeIndex:  0,
		WithGrantOption: false,
	}
}

// Update handles key presses for the grant dialog.
func (d *GrantDialog) Update(key string) (finished bool, cmd tea.Cmd) {
	logger.Debug("GrantDialog.Update", "key", key)
	switch key {
	case "esc", "q":
		// Cancel
		logger.Debug("GrantDialog: cancelled")
		return true, nil
	case "tab", "j", "down":
		d.SelectedField = (d.SelectedField + 1) % 3
	case "shift+tab", "k", "up":
		d.SelectedField = (d.SelectedField + 2) % 3 // -1 mod 3
	case "h", "left":
		switch d.SelectedField {
		case 0: // Role
			if d.RoleIndex > 0 {
				d.RoleIndex--
			}
		case 1: // Privilege
			if d.PrivilegeIndex > 0 {
				d.PrivilegeIndex--
			}
		case 2: // With grant option
			d.WithGrantOption = !d.WithGrantOption
		}
	case "l", "right":
		switch d.SelectedField {
		case 0: // Role
			if d.RoleIndex < len(d.RoleNames)-1 {
				d.RoleIndex++
			}
		case 1: // Privilege
			privileges := AllTablePrivileges()
			if d.PrivilegeIndex < len(privileges)-1 {
				d.PrivilegeIndex++
			}
		case 2: // With grant option
			d.WithGrantOption = !d.WithGrantOption
		}
	case " ":
		if d.SelectedField == 2 {
			d.WithGrantOption = !d.WithGrantOption
		}
	case "enter":
		// Execute grant
		if len(d.RoleNames) == 0 {
			logger.Debug("GrantDialog: no roles available")
			return true, nil
		}
		privileges := AllTablePrivileges()
		role := d.RoleNames[d.RoleIndex]
		privilege := string(privileges[d.PrivilegeIndex])
		logger.Debug("GrantDialog: executing grant",
			"schema", d.SchemaName,
			"table", d.TableName,
			"role", role,
			"privilege", privilege,
			"withGrantOption", d.WithGrantOption)
		return true, d.executeGrant(
			d.SchemaName,
			d.TableName,
			role,
			privilege,
			d.WithGrantOption,
		)
	}
	return false, nil
}

// executeGrant returns a command to grant a permission.
func (d *GrantDialog) executeGrant(schema, table, role, privilege string, withGrantOption bool) tea.Cmd {
	return func() tea.Msg {
		return GrantPermissionMsg{
			Schema:          schema,
			Table:           table,
			Role:            role,
			Privilege:       privilege,
			WithGrantOption: withGrantOption,
		}
	}
}

// View renders the grant dialog.
func (d *GrantDialog) View() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render(fmt.Sprintf("Grant Permission on %s.%s", d.SchemaName, d.TableName)))
	b.WriteString("\n\n")

	// Role selector
	roleLabel := "Role:"
	if d.SelectedField == 0 {
		roleLabel = lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("> Role:")
	} else {
		roleLabel = "  Role:"
	}
	roleName := "(no roles)"
	if len(d.RoleNames) > 0 {
		roleName = d.RoleNames[d.RoleIndex]
	}
	b.WriteString(fmt.Sprintf("%s  ◀ %s ▶\n", roleLabel, roleName))

	// Privilege selector
	privileges := AllTablePrivileges()
	privLabel := "Privilege:"
	if d.SelectedField == 1 {
		privLabel = lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("> Privilege:")
	} else {
		privLabel = "  Privilege:"
	}
	b.WriteString(fmt.Sprintf("%s  ◀ %s ▶\n", privLabel, privileges[d.PrivilegeIndex]))

	// With grant option checkbox
	checkLabel := "With Grant Option:"
	if d.SelectedField == 2 {
		checkLabel = lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("> With Grant Option:")
	} else {
		checkLabel = "  With Grant Option:"
	}
	checkbox := "[ ]"
	if d.WithGrantOption {
		checkbox = "[✓]"
	}
	b.WriteString(fmt.Sprintf("%s  %s\n", checkLabel, checkbox))

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[Tab/j/k] Navigate  [←/→] Change  [Space] Toggle  [Enter] Grant  [Esc] Cancel"))

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(d.Width)

	return dialogStyle.Render(b.String())
}

// Message types for permissions operations
type (
	// PermissionsDataMsg contains fetched permissions data.
	PermissionsDataMsg struct {
		TableOID    uint32
		Permissions []queries.TablePermission
		RoleNames   []string
		Error       error
	}

	// PermissionsRefreshMsg triggers a permissions refresh.
	PermissionsRefreshMsg struct{}

	// GrantPermissionMsg requests granting a permission.
	GrantPermissionMsg struct {
		Schema          string
		Table           string
		Role            string
		Privilege       string
		WithGrantOption bool
	}

	// GrantPermissionResultMsg contains the result of a grant operation.
	GrantPermissionResultMsg struct {
		Schema    string
		Table     string
		Role      string
		Privilege string
		Success   bool
		Error     error
	}

	// RevokePermissionMsg requests revoking a permission.
	RevokePermissionMsg struct {
		Schema    string
		Table     string
		Role      string
		Privilege string
		Cascade   bool
	}

	// RevokePermissionResultMsg contains the result of a revoke operation.
	RevokePermissionResultMsg struct {
		Schema    string
		Table     string
		Role      string
		Privilege string
		Success   bool
		Error     error
	}
)

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
