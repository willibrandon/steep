package roles

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// Operation messages
type (
	// CreateRoleResultMsg is the result of creating a role.
	CreateRoleResultMsg struct {
		RoleName string
		Error    error
	}

	// DropRoleResultMsg is the result of dropping a role.
	DropRoleResultMsg struct {
		RoleName string
		Error    error
	}

	// AlterRoleResultMsg is the result of altering a role.
	AlterRoleResultMsg struct {
		RoleName string
		Error    error
	}
)

// CreateRoleForm holds the form state for creating a role.
type CreateRoleForm struct {
	nameInput     textinput.Model
	passwordInput textinput.Model
	focusIndex    int // 0=name, 1=password, 2+=attributes

	// Attributes
	canLogin      bool
	superuser     bool
	createDB      bool
	createRole    bool
	inherit       bool
	replication   bool
	bypassRLS     bool
	connLimit     int // -1 = unlimited
}

// NewCreateRoleForm creates a new form for creating roles.
func NewCreateRoleForm() *CreateRoleForm {
	nameInput := textinput.New()
	nameInput.Placeholder = "role_name"
	nameInput.Focus()
	nameInput.CharLimit = 63 // PostgreSQL identifier limit

	passwordInput := textinput.New()
	passwordInput.Placeholder = "password (optional)"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.CharLimit = 100

	return &CreateRoleForm{
		nameInput:     nameInput,
		passwordInput: passwordInput,
		focusIndex:    0,
		canLogin:      true, // Default to creating a "user" (role with login)
		inherit:       true, // PostgreSQL default
		connLimit:     -1,   // Unlimited
	}
}

// Update handles input for the create role form.
func (f *CreateRoleForm) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			f.focusIndex = (f.focusIndex + 1) % 9
			f.updateFocus()
		case "shift+tab", "up":
			f.focusIndex = (f.focusIndex - 1 + 9) % 9
			f.updateFocus()
		case " ":
			// Toggle attributes
			switch f.focusIndex {
			case 2:
				f.canLogin = !f.canLogin
			case 3:
				f.superuser = !f.superuser
			case 4:
				f.createDB = !f.createDB
			case 5:
				f.createRole = !f.createRole
			case 6:
				f.inherit = !f.inherit
			case 7:
				f.replication = !f.replication
			case 8:
				f.bypassRLS = !f.bypassRLS
			}
		}
	}

	var cmd tea.Cmd
	if f.focusIndex == 0 {
		f.nameInput, cmd = f.nameInput.Update(msg)
	} else if f.focusIndex == 1 {
		f.passwordInput, cmd = f.passwordInput.Update(msg)
	}
	return cmd
}

func (f *CreateRoleForm) updateFocus() {
	f.nameInput.Blur()
	f.passwordInput.Blur()
	if f.focusIndex == 0 {
		f.nameInput.Focus()
	} else if f.focusIndex == 1 {
		f.passwordInput.Focus()
	}
}

// Name returns the entered role name.
func (f *CreateRoleForm) Name() string {
	return strings.TrimSpace(f.nameInput.Value())
}

// Password returns the entered password.
func (f *CreateRoleForm) Password() string {
	return f.passwordInput.Value()
}

// BuildSQL builds the CREATE ROLE SQL statement.
func (f *CreateRoleForm) BuildSQL() string {
	var opts []string

	if f.canLogin {
		opts = append(opts, "LOGIN")
	} else {
		opts = append(opts, "NOLOGIN")
	}

	if f.superuser {
		opts = append(opts, "SUPERUSER")
	}

	if f.createDB {
		opts = append(opts, "CREATEDB")
	}

	if f.createRole {
		opts = append(opts, "CREATEROLE")
	}

	if !f.inherit {
		opts = append(opts, "NOINHERIT")
	}

	if f.replication {
		opts = append(opts, "REPLICATION")
	}

	if f.bypassRLS {
		opts = append(opts, "BYPASSRLS")
	}

	if f.Password() != "" {
		opts = append(opts, fmt.Sprintf("PASSWORD '%s'", f.Password()))
	}

	if f.connLimit >= 0 {
		opts = append(opts, fmt.Sprintf("CONNECTION LIMIT %d", f.connLimit))
	}

	return fmt.Sprintf("CREATE ROLE %s %s", quoteIdent(f.Name()), strings.Join(opts, " "))
}

// View renders the create role form.
func (f *CreateRoleForm) View(width int) string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(titleStyle.Render("Create Role"))
	b.WriteString("\n\n")

	// Name input
	label := "Name:     "
	if f.focusIndex == 0 {
		label = styles.AccentStyle.Render("> ") + label
	} else {
		label = "  " + label
	}
	b.WriteString(label)
	b.WriteString(f.nameInput.View())
	b.WriteString("\n")

	// Password input
	label = "Password: "
	if f.focusIndex == 1 {
		label = styles.AccentStyle.Render("> ") + label
	} else {
		label = "  " + label
	}
	b.WriteString(label)
	b.WriteString(f.passwordInput.View())
	b.WriteString("\n\n")

	// Attributes
	b.WriteString(styles.HeaderStyle.Render("Attributes"))
	b.WriteString("\n")

	attrs := []struct {
		idx   int
		name  string
		value bool
	}{
		{2, "Login", f.canLogin},
		{3, "Superuser", f.superuser},
		{4, "Create DB", f.createDB},
		{5, "Create Role", f.createRole},
		{6, "Inherit", f.inherit},
		{7, "Replication", f.replication},
		{8, "Bypass RLS", f.bypassRLS},
	}

	for _, attr := range attrs {
		checkbox := "[ ]"
		if attr.value {
			checkbox = "[x]"
		}
		line := fmt.Sprintf("  %s %s", checkbox, attr.name)
		if f.focusIndex == attr.idx {
			line = styles.AccentStyle.Render("> ") + line[2:]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.MutedStyle.Render("[Tab] next  [Space] toggle  [Enter] create  [Esc] cancel"))

	return b.String()
}

// renderCreateRoleDialog renders the create role dialog.
func (v *RolesView) renderCreateRoleDialog() string {
	content := v.createForm.View(v.width)

	dialog := styles.HelpDialogStyle.
		Width(50).
		Render(content)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// renderDropRoleConfirm renders the drop role confirmation dialog.
func (v *RolesView) renderDropRoleConfirm() string {
	if v.selectedIdx < 0 || v.selectedIdx >= len(v.roles) {
		return ""
	}

	role := v.roles[v.selectedIdx]

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorCriticalFg)
	b.WriteString(titleStyle.Render("Drop Role"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Are you sure you want to drop role %s?\n\n", styles.AccentStyle.Render(role.Name)))

	if role.IsSuperuser {
		b.WriteString(styles.WarningStyle.Render("Warning: This is a superuser role!"))
		b.WriteString("\n\n")
	}

	b.WriteString("This action cannot be undone.\n\n")
	b.WriteString(styles.MutedStyle.Render("[y] Yes, drop  [n/Esc] Cancel"))

	dialog := styles.HelpDialogStyle.
		Width(50).
		Render(b.String())

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// renderAlterRoleDialog renders the alter role dialog.
func (v *RolesView) renderAlterRoleDialog() string {
	if v.alterForm == nil {
		return ""
	}

	content := v.alterForm.View(v.width)

	dialog := styles.HelpDialogStyle.
		Width(50).
		Render(content)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// AlterRoleForm holds the form state for altering a role.
type AlterRoleForm struct {
	roleName      string
	passwordInput textinput.Model
	focusIndex    int

	// Attributes (current values)
	canLogin    bool
	superuser   bool
	createDB    bool
	createRole  bool
	inherit     bool
	replication bool
	bypassRLS   bool
	connLimit   int
}

// NewAlterRoleForm creates a form pre-filled with the role's current attributes.
func NewAlterRoleForm(roleName string, canLogin, superuser, createDB, createRole, inherit, replication, bypassRLS bool, connLimit int) *AlterRoleForm {
	passwordInput := textinput.New()
	passwordInput.Placeholder = "new password (leave empty to keep)"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.CharLimit = 100

	return &AlterRoleForm{
		roleName:      roleName,
		passwordInput: passwordInput,
		focusIndex:    0,
		canLogin:      canLogin,
		superuser:     superuser,
		createDB:      createDB,
		createRole:    createRole,
		inherit:       inherit,
		replication:   replication,
		bypassRLS:     bypassRLS,
		connLimit:     connLimit,
	}
}

// Update handles input for the alter role form.
func (f *AlterRoleForm) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			f.focusIndex = (f.focusIndex + 1) % 8
			f.updateFocus()
		case "shift+tab", "up":
			f.focusIndex = (f.focusIndex - 1 + 8) % 8
			f.updateFocus()
		case " ":
			// Toggle attributes
			switch f.focusIndex {
			case 1:
				f.canLogin = !f.canLogin
			case 2:
				f.superuser = !f.superuser
			case 3:
				f.createDB = !f.createDB
			case 4:
				f.createRole = !f.createRole
			case 5:
				f.inherit = !f.inherit
			case 6:
				f.replication = !f.replication
			case 7:
				f.bypassRLS = !f.bypassRLS
			}
		}
	}

	var cmd tea.Cmd
	if f.focusIndex == 0 {
		f.passwordInput, cmd = f.passwordInput.Update(msg)
	}
	return cmd
}

func (f *AlterRoleForm) updateFocus() {
	f.passwordInput.Blur()
	if f.focusIndex == 0 {
		f.passwordInput.Focus()
	}
}

// BuildSQL builds the ALTER ROLE SQL statement.
func (f *AlterRoleForm) BuildSQL() string {
	var opts []string

	if f.canLogin {
		opts = append(opts, "LOGIN")
	} else {
		opts = append(opts, "NOLOGIN")
	}

	if f.superuser {
		opts = append(opts, "SUPERUSER")
	} else {
		opts = append(opts, "NOSUPERUSER")
	}

	if f.createDB {
		opts = append(opts, "CREATEDB")
	} else {
		opts = append(opts, "NOCREATEDB")
	}

	if f.createRole {
		opts = append(opts, "CREATEROLE")
	} else {
		opts = append(opts, "NOCREATEROLE")
	}

	if f.inherit {
		opts = append(opts, "INHERIT")
	} else {
		opts = append(opts, "NOINHERIT")
	}

	if f.replication {
		opts = append(opts, "REPLICATION")
	} else {
		opts = append(opts, "NOREPLICATION")
	}

	if f.bypassRLS {
		opts = append(opts, "BYPASSRLS")
	} else {
		opts = append(opts, "NOBYPASSRLS")
	}

	if f.passwordInput.Value() != "" {
		opts = append(opts, fmt.Sprintf("PASSWORD '%s'", f.passwordInput.Value()))
	}

	return fmt.Sprintf("ALTER ROLE %s %s", quoteIdent(f.roleName), strings.Join(opts, " "))
}

// View renders the alter role form.
func (f *AlterRoleForm) View(width int) string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(titleStyle.Render(fmt.Sprintf("Alter Role: %s", f.roleName)))
	b.WriteString("\n\n")

	// Password input
	label := "Password: "
	if f.focusIndex == 0 {
		label = styles.AccentStyle.Render("> ") + label
	} else {
		label = "  " + label
	}
	b.WriteString(label)
	b.WriteString(f.passwordInput.View())
	b.WriteString("\n\n")

	// Attributes
	b.WriteString(styles.HeaderStyle.Render("Attributes"))
	b.WriteString("\n")

	attrs := []struct {
		idx   int
		name  string
		value bool
	}{
		{1, "Login", f.canLogin},
		{2, "Superuser", f.superuser},
		{3, "Create DB", f.createDB},
		{4, "Create Role", f.createRole},
		{5, "Inherit", f.inherit},
		{6, "Replication", f.replication},
		{7, "Bypass RLS", f.bypassRLS},
	}

	for _, attr := range attrs {
		checkbox := "[ ]"
		if attr.value {
			checkbox = "[x]"
		}
		line := fmt.Sprintf("  %s %s", checkbox, attr.name)
		if f.focusIndex == attr.idx {
			line = styles.AccentStyle.Render("> ") + line[2:]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.MutedStyle.Render("[Tab] next  [Space] toggle  [Enter] save  [Esc] cancel"))

	return b.String()
}

// quoteIdent quotes a PostgreSQL identifier.
func quoteIdent(s string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(s, `"`, `""`))
}

// executeCreateRole executes the CREATE ROLE command.
func (v *RolesView) executeCreateRole() tea.Cmd {
	return func() tea.Msg {
		logger.Debug("executeCreateRole: starting", "roleName", v.createForm.Name())

		if v.pool == nil {
			logger.Error("executeCreateRole: pool is nil")
			return CreateRoleResultMsg{Error: fmt.Errorf("database connection not available")}
		}

		sql := v.createForm.BuildSQL()
		logger.Debug("executeCreateRole: executing SQL", "sql", sql)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := v.pool.Exec(ctx, sql)
		if err != nil {
			logger.Error("executeCreateRole: failed", "error", err)
			return CreateRoleResultMsg{
				RoleName: v.createForm.Name(),
				Error:    err,
			}
		}

		logger.Info("executeCreateRole: success", "roleName", v.createForm.Name())
		return CreateRoleResultMsg{
			RoleName: v.createForm.Name(),
		}
	}
}

// executeDropRole executes the DROP ROLE command.
func (v *RolesView) executeDropRole(roleName string) tea.Cmd {
	return func() tea.Msg {
		logger.Debug("executeDropRole: starting", "roleName", roleName)

		if v.pool == nil {
			logger.Error("executeDropRole: pool is nil")
			return DropRoleResultMsg{Error: fmt.Errorf("database connection not available")}
		}

		sql := fmt.Sprintf("DROP ROLE %s", quoteIdent(roleName))
		logger.Debug("executeDropRole: executing SQL", "sql", sql)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := v.pool.Exec(ctx, sql)
		if err != nil {
			logger.Error("executeDropRole: failed", "error", err)
			return DropRoleResultMsg{
				RoleName: roleName,
				Error:    err,
			}
		}

		logger.Info("executeDropRole: success", "roleName", roleName)
		return DropRoleResultMsg{
			RoleName: roleName,
		}
	}
}

// executeAlterRole executes the ALTER ROLE command.
func (v *RolesView) executeAlterRole() tea.Cmd {
	return func() tea.Msg {
		logger.Debug("executeAlterRole: starting", "roleName", v.alterForm.roleName)

		if v.pool == nil {
			logger.Error("executeAlterRole: pool is nil")
			return AlterRoleResultMsg{Error: fmt.Errorf("database connection not available")}
		}

		sql := v.alterForm.BuildSQL()
		logger.Debug("executeAlterRole: executing SQL", "sql", sql)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := v.pool.Exec(ctx, sql)
		if err != nil {
			logger.Error("executeAlterRole: failed", "error", err)
			return AlterRoleResultMsg{
				RoleName: v.alterForm.roleName,
				Error:    err,
			}
		}

		logger.Info("executeAlterRole: success", "roleName", v.alterForm.roleName)
		return AlterRoleResultMsg{
			RoleName: v.alterForm.roleName,
		}
	}
}
