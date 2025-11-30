// Package roles provides the Roles view for database user/role management.
package roles

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// Message types for the roles view.
type (
	// RolesDataMsg contains refreshed roles data.
	RolesDataMsg struct {
		Roles       []models.Role
		Memberships []models.RoleMembership
		Error       error
	}

	// RefreshRolesMsg triggers data refresh.
	RefreshRolesMsg struct{}

	// RoleDetailsMsg contains detailed information for a role.
	RoleDetailsMsg struct {
		Details *models.RoleDetails
		Error   error
	}
)

// RolesMode represents the current interaction mode.
type RolesMode int

const (
	ModeNormal RolesMode = iota
	ModeHelp
	ModeDetails
	ModeCreateRole
	ModeDropConfirm
	ModeAlterRole
)

// SortColumn represents the available sort columns.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortByAttributes
	SortByConnLimit
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByName:
		return "Name"
	case SortByAttributes:
		return "Attributes"
	case SortByConnLimit:
		return "Conn Limit"
	default:
		return "Unknown"
	}
}

// RolesView displays database roles and their attributes.
type RolesView struct {
	width            int
	height           int
	viewHeaderHeight int // Calculated height of view header elements for mouse coordinate translation

	// State
	mode           RolesMode
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	loading        bool

	// Data
	roles       []models.Role
	memberships []models.RoleMembership
	details     *models.RoleDetails
	err         error

	// Table state
	selectedIdx  int
	scrollOffset int
	sortColumn   SortColumn
	sortAsc      bool

	// Details view state
	detailsScrollOffset int
	detailsLines        []string

	// Operation forms
	createForm *CreateRoleForm
	alterForm  *AlterRoleForm

	// UI components
	spinner spinner.Model

	// Toast
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Read-only mode
	readOnly bool

	// Clipboard
	clipboard *ui.ClipboardWriter

	// Database connection
	pool *pgxpool.Pool
}

// NewRolesView creates a new RolesView instance.
func NewRolesView() *RolesView {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &RolesView{
		mode:       ModeNormal,
		sortColumn: SortByName,
		sortAsc:    true,
		clipboard:  ui.NewClipboardWriter(),
		spinner:    s,
		loading:    true,
	}
}

// Init initializes the roles view.
func (v *RolesView) Init() tea.Cmd {
	return v.spinner.Tick
}

// SetSize sets the dimensions of the view.
func (v *RolesView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *RolesView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *RolesView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *RolesView) SetReadOnly(readOnly bool) {
	v.readOnly = readOnly
}

// SetPool sets the database connection pool.
func (v *RolesView) SetPool(pool *pgxpool.Pool) {
	v.pool = pool
}

// IsInputMode returns true when the view is in a mode that should consume keys.
func (v *RolesView) IsInputMode() bool {
	return v.mode == ModeHelp || v.mode == ModeDetails ||
		v.mode == ModeCreateRole || v.mode == ModeDropConfirm || v.mode == ModeAlterRole
}

// FetchRolesData returns a command that loads all roles data.
func (v *RolesView) FetchRolesData() tea.Cmd {
	return func() tea.Msg {
		logger.Debug("RolesView: starting data fetch")
		if v.pool == nil {
			logger.Error("RolesView: pool is nil")
			return RolesDataMsg{Error: fmt.Errorf("database connection not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		roles, err := queries.GetRoles(ctx, v.pool)
		if err != nil {
			return RolesDataMsg{Error: fmt.Errorf("fetch roles: %w", err)}
		}

		memberships, err := queries.GetRoleMemberships(ctx, v.pool)
		if err != nil {
			return RolesDataMsg{Error: fmt.Errorf("fetch memberships: %w", err)}
		}

		// Populate MemberOf field for each role
		membershipMap := make(map[uint32][]string)
		for _, m := range memberships {
			membershipMap[m.MemberOID] = append(membershipMap[m.MemberOID], m.RoleName)
		}
		for i := range roles {
			roles[i].MemberOf = membershipMap[roles[i].OID]
		}

		logger.Debug("RolesView: fetch complete", "roleCount", len(roles), "membershipCount", len(memberships))
		return RolesDataMsg{
			Roles:       roles,
			Memberships: memberships,
		}
	}
}

// fetchRoleDetails returns a command that fetches detailed role information.
func (v *RolesView) fetchRoleDetails(roleOID uint32) tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return RoleDetailsMsg{Error: fmt.Errorf("database connection not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		details, err := queries.GetRoleDetails(ctx, v.pool, roleOID)
		if err != nil {
			return RoleDetailsMsg{Error: fmt.Errorf("fetch role details: %w", err)}
		}

		return RoleDetailsMsg{Details: details}
	}
}

// scheduleRefresh returns a command for 60-second auto-refresh.
func (v *RolesView) scheduleRefresh() tea.Cmd {
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return RefreshRolesMsg{}
	})
}

// Update handles messages for the roles view.
func (v *RolesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := v.handleKeyPress(msg)
		if cmd != nil {
			return v, cmd
		}

	case RolesDataMsg:
		v.loading = false
		v.refreshing = false
		if msg.Error != nil {
			v.err = msg.Error
			logger.Error("RolesView: failed to fetch roles", "error", msg.Error)
		} else {
			v.roles = msg.Roles
			v.memberships = msg.Memberships
			v.lastUpdate = time.Now()
			v.err = nil
			// Apply current sort
			v.sortRoles()
			// Ensure selection is valid
			if v.selectedIdx >= len(v.roles) {
				v.selectedIdx = max(0, len(v.roles)-1)
			}
			v.ensureVisible()
		}
		return v, v.scheduleRefresh()

	case RoleDetailsMsg:
		if msg.Error != nil {
			logger.Error("RolesView: failed to fetch role details", "error", msg.Error)
			v.showToast(fmt.Sprintf("Error loading details: %v", msg.Error), true)
			v.mode = ModeNormal
		} else {
			v.details = msg.Details
			v.detailsScrollOffset = 0
			v.detailsLines = v.buildDetailsLines()
			v.mode = ModeDetails
		}
		return v, nil

	case RefreshRolesMsg:
		if !v.refreshing {
			v.refreshing = true
			return v, v.FetchRolesData()
		}

	case CreateRoleResultMsg:
		v.mode = ModeNormal
		v.createForm = nil
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Failed to create role: %v", msg.Error), true)
		} else {
			v.showToast(fmt.Sprintf("Role %q created", msg.RoleName), false)
			return v, v.FetchRolesData()
		}

	case DropRoleResultMsg:
		v.mode = ModeNormal
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Failed to drop role: %v", msg.Error), true)
		} else {
			v.showToast(fmt.Sprintf("Role %q dropped", msg.RoleName), false)
			return v, v.FetchRolesData()
		}

	case AlterRoleResultMsg:
		v.mode = ModeNormal
		v.alterForm = nil
		if msg.Error != nil {
			v.showToast(fmt.Sprintf("Failed to alter role: %v", msg.Error), true)
		} else {
			v.showToast(fmt.Sprintf("Role %q updated", msg.RoleName), false)
			return v, v.FetchRolesData()
		}

	case tea.MouseMsg:
		if v.mode == ModeNormal {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.moveSelection(-1)
			case tea.MouseButtonWheelDown:
				v.moveSelection(1)
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					// msg.Y is relative to view top (app translates global to relative)
					// Subtract view's header height to get data row index
					clickedRow := msg.Y - v.viewHeaderHeight
					if clickedRow >= 0 {
						newIdx := v.scrollOffset + clickedRow
						if newIdx >= 0 && newIdx < len(v.roles) {
							v.selectedIdx = newIdx
						}
					}
				}
			}
		} else if v.mode == ModeDetails {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				v.scrollDetailsUp(3)
			case tea.MouseButtonWheelDown:
				v.scrollDetailsDown(3)
			}
		}

	case spinner.TickMsg:
		// Only tick spinner when loading to avoid unnecessary updates
		if v.loading {
			var cmd tea.Cmd
			v.spinner, cmd = v.spinner.Update(msg)
			return v, cmd
		}

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return v, nil
}

// handleKeyPress handles keyboard input.
func (v *RolesView) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Help mode
	if v.mode == ModeHelp {
		switch key {
		case "h", "H", "q", "esc", "?":
			v.mode = ModeNormal
		}
		return nil
	}

	// Details mode
	if v.mode == ModeDetails {
		switch key {
		case "esc", "q":
			v.mode = ModeNormal
			v.details = nil
		case "j", "down":
			v.scrollDetailsDown(1)
		case "k", "up":
			v.scrollDetailsUp(1)
		case "g":
			v.detailsScrollOffset = 0
		case "G":
			v.scrollDetailsToBottom()
		case "ctrl+d":
			v.scrollDetailsDown(10)
		case "ctrl+u":
			v.scrollDetailsUp(10)
		case "y":
			// Copy role name to clipboard
			if v.details != nil {
				if err := v.clipboard.Write(v.details.Name); err == nil {
					v.showToast("Role name copied to clipboard", false)
				}
			}
		}
		return nil
	}

	// Create role mode
	if v.mode == ModeCreateRole {
		logger.Debug("handleKeyPress: ModeCreateRole", "key", key, "name", v.createForm.Name())
		switch key {
		case "esc":
			v.mode = ModeNormal
			v.createForm = nil
		case "enter":
			logger.Debug("handleKeyPress: enter pressed in create mode", "name", v.createForm.Name())
			if v.createForm.Name() == "" {
				v.showToast("Role name is required", true)
				return nil
			}
			v.mode = ModeNormal // Close dialog immediately
			return v.executeCreateRole()
		default:
			return v.createForm.Update(msg)
		}
		return nil
	}

	// Drop confirm mode
	if v.mode == ModeDropConfirm {
		logger.Debug("handleKeyPress: ModeDropConfirm", "key", key)
		switch key {
		case "y", "Y":
			if v.selectedIdx >= 0 && v.selectedIdx < len(v.roles) {
				roleName := v.roles[v.selectedIdx].Name
				logger.Debug("handleKeyPress: confirming drop", "roleName", roleName)
				v.mode = ModeNormal // Close dialog immediately
				return v.executeDropRole(roleName)
			}
		case "n", "N", "esc":
			v.mode = ModeNormal
		}
		return nil
	}

	// Alter role mode
	if v.mode == ModeAlterRole {
		switch key {
		case "esc":
			v.mode = ModeNormal
			v.alterForm = nil
		case "enter":
			v.mode = ModeNormal // Close dialog immediately
			return v.executeAlterRole()
		default:
			return v.alterForm.Update(msg)
		}
		return nil
	}

	// Normal mode
	switch key {
	case "j", "down":
		v.moveSelection(1)
	case "k", "up":
		v.moveSelection(-1)
	case "g":
		v.selectedIdx = 0
		v.scrollOffset = 0
	case "G":
		v.selectedIdx = max(0, len(v.roles)-1)
		v.ensureVisible()
	case "ctrl+d":
		v.pageDown()
	case "ctrl+u":
		v.pageUp()
	case "enter":
		// Show role details
		if v.selectedIdx >= 0 && v.selectedIdx < len(v.roles) {
			return v.fetchRoleDetails(v.roles[v.selectedIdx].OID)
		}
	case "s":
		v.cycleSortColumn()
	case "S":
		v.toggleSortDirection()
	case "r":
		// Refresh
		if !v.refreshing {
			v.refreshing = true
			return v.FetchRolesData()
		}
	case "y":
		// Copy role name to clipboard
		if v.selectedIdx >= 0 && v.selectedIdx < len(v.roles) {
			role := v.roles[v.selectedIdx]
			if err := v.clipboard.Write(role.Name); err == nil {
				v.showToast("Role name copied to clipboard", false)
			}
		}
	case "h":
		v.mode = ModeHelp
	case "c":
		// Create role
		if v.readOnly {
			v.showToast("Cannot create roles in read-only mode", true)
			return nil
		}
		v.createForm = NewCreateRoleForm()
		v.mode = ModeCreateRole
	case "x":
		// Drop role
		if v.readOnly {
			v.showToast("Cannot drop roles in read-only mode", true)
			return nil
		}
		if v.selectedIdx >= 0 && v.selectedIdx < len(v.roles) {
			v.mode = ModeDropConfirm
		}
	case "a":
		// Alter role
		if v.readOnly {
			v.showToast("Cannot alter roles in read-only mode", true)
			return nil
		}
		if v.selectedIdx >= 0 && v.selectedIdx < len(v.roles) {
			role := v.roles[v.selectedIdx]
			v.alterForm = NewAlterRoleForm(
				role.Name,
				role.CanLogin,
				role.IsSuperuser,
				role.CanCreateDB,
				role.CanCreateRole,
				role.Inherit,
				role.Replication,
				role.CanBypassRLS,
				int(role.ConnectionLimit),
			)
			v.mode = ModeAlterRole
		}
	}

	return nil
}

// View renders the roles view.
func (v *RolesView) View() string {
	switch v.mode {
	case ModeHelp:
		return HelpOverlay(v.width, v.height)
	case ModeDetails:
		return v.renderDetails()
	case ModeCreateRole:
		return v.renderCreateRoleDialog()
	case ModeDropConfirm:
		return v.renderDropRoleConfirm()
	case ModeAlterRole:
		return v.renderAlterRoleDialog()
	default:
		return v.renderMainView()
	}
}

// renderMainView renders the main roles list view.
func (v *RolesView) renderMainView() string {
	// Render components
	statusBar := v.renderStatusBar()
	title := v.renderTitle()

	// Error or loading state
	if v.err != nil {
		var b strings.Builder
		b.WriteString(statusBar)
		b.WriteString("\n")
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", v.err)))
		return b.String()
	}

	if v.loading {
		var b strings.Builder
		b.WriteString(statusBar)
		b.WriteString("\n")
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(v.spinner.View())
		b.WriteString(" Loading roles...")
		return b.String()
	}

	header := v.renderHeader()
	table := v.renderTable()
	footer := v.renderFooter()

	// Calculate view header height for mouse coordinate translation
	// This is the number of rows from view top to first data row
	v.viewHeaderHeight = lipgloss.Height(statusBar) + lipgloss.Height(title) + lipgloss.Height(header)

	var b strings.Builder
	b.WriteString(statusBar)
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(table)
	b.WriteString("\n")
	b.WriteString(footer)

	return b.String()
}

// renderStatusBar renders the status bar at the top.
func (v *RolesView) renderStatusBar() string {
	// Connection info
	title := styles.StatusTitleStyle.Render(v.connectionInfo)

	// Right side indicators
	var indicators []string

	if v.refreshing {
		indicators = append(indicators, styles.WarningStyle.Render("Refreshing..."))
	} else if !v.lastUpdate.IsZero() {
		indicators = append(indicators, styles.MutedStyle.Render(fmt.Sprintf("Updated %s", v.lastUpdate.Format("15:04:05"))))
	}

	rightContent := strings.Join(indicators, " │ ")
	titleLen := lipgloss.Width(title)
	rightLen := lipgloss.Width(rightContent)
	gap := v.width - 2 - titleLen - rightLen
	if gap < 1 {
		gap = 1
	}
	spaces := strings.Repeat(" ", gap)

	return styles.StatusBarStyle.
		Width(v.width - 2).
		Render(title + spaces + rightContent)
}

// renderTitle renders the view title.
func (v *RolesView) renderTitle() string {
	title := "Database Roles"
	if len(v.roles) > 0 {
		title = fmt.Sprintf("Database Roles (%d)", len(v.roles))
	}
	return styles.TitleStyle.Render(title)
}

// renderHeader renders the table header.
func (v *RolesView) renderHeader() string {
	// Column widths
	nameWidth := 30
	attrWidth := 8
	connWidth := 10
	validWidth := 12
	memberWidth := v.width - nameWidth - attrWidth - connWidth - validWidth - 8

	// Build header with sort indicator
	nameLabel := "Name"
	if v.sortColumn == SortByName {
		if v.sortAsc {
			nameLabel += " ▲"
		} else {
			nameLabel += " ▼"
		}
	}

	attrLabel := "Attrs"
	if v.sortColumn == SortByAttributes {
		if v.sortAsc {
			attrLabel += " ▲"
		} else {
			attrLabel += " ▼"
		}
	}

	header := fmt.Sprintf("%-*s %*s %*s %*s %-*s",
		nameWidth, nameLabel,
		attrWidth, attrLabel,
		connWidth, "Conn",
		validWidth, "Valid Until",
		memberWidth, "Member Of")

	return styles.TableHeaderStyle.Width(v.width).Render(header)
}

// renderTable renders the roles table content.
func (v *RolesView) renderTable() string {
	if len(v.roles) == 0 {
		return styles.InfoStyle.Render("No roles found")
	}

	tableHeight := v.tableHeight()
	var rows []string

	endIdx := min(v.scrollOffset+tableHeight, len(v.roles))
	for i := v.scrollOffset; i < endIdx; i++ {
		rows = append(rows, v.renderRow(i))
	}

	// Pad to fill height
	for len(rows) < tableHeight {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

// renderRow renders a single role row.
func (v *RolesView) renderRow(idx int) string {
	role := v.roles[idx]
	selected := idx == v.selectedIdx

	// Column widths
	nameWidth := 30
	attrWidth := 8
	connWidth := 10
	validWidth := 12
	memberWidth := v.width - nameWidth - attrWidth - connWidth - validWidth - 8

	// Format attributes
	attrs := queries.FormatRoleAttributes(queries.RoleAttributeInfo{
		IsSuperuser:   role.IsSuperuser,
		CanLogin:      role.CanLogin,
		CanCreateRole: role.CanCreateRole,
		CanCreateDB:   role.CanCreateDB,
		CanBypassRLS:  role.CanBypassRLS,
	})

	// Format connection limit
	connLimit := queries.FormatConnectionLimit(role.ConnectionLimit)

	// Format valid until
	validUntil := queries.FormatValidUntil(role.ValidUntil)

	// Format memberships
	memberOf := "-"
	if len(role.MemberOf) > 0 {
		memberOf = strings.Join(role.MemberOf, ", ")
		if runewidth.StringWidth(memberOf) > memberWidth {
			memberOf = runewidth.Truncate(memberOf, memberWidth-3, "...")
		}
	}

	// Truncate name if needed
	name := role.Name
	if runewidth.StringWidth(name) > nameWidth {
		name = runewidth.Truncate(name, nameWidth-3, "...")
	}

	row := fmt.Sprintf("%-*s %*s %*s %*s %-*s",
		nameWidth, name,
		attrWidth, attrs,
		connWidth, connLimit,
		validWidth, validUntil,
		memberWidth, memberOf)

	if selected {
		return styles.TableSelectedStyle.Width(v.width).Render(row)
	}

	// Color code superusers
	if role.IsSuperuser {
		return lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(row)
	}

	return row
}

// renderFooter renders the footer with hints.
func (v *RolesView) renderFooter() string {
	scrollInfo := ""
	if len(v.roles) > v.tableHeight() {
		scrollInfo = fmt.Sprintf(" %d/%d", v.selectedIdx+1, len(v.roles))
	}

	var hints string
	if v.toastMessage != "" && time.Since(v.toastTime) < 3*time.Second {
		// Show toast message in footer
		toastStyle := styles.FooterHintStyle
		if v.toastError {
			toastStyle = toastStyle.Foreground(styles.ColorCriticalFg)
		} else {
			toastStyle = toastStyle.Foreground(styles.ColorActive)
		}
		hints = toastStyle.Render(v.toastMessage)
	} else {
		// Normal hints
		hintText := "[j/k]↕ [Enter]details [c]create [x]drop [a]alter [s/S]sort [h]help"
		if v.readOnly {
			hintText = "[j/k]↕ [Enter]details [s/S]sort [h]help (read-only)"
		}
		hints = styles.FooterHintStyle.Render(fmt.Sprintf("%s%s", hintText, scrollInfo))
	}

	gap := v.width - lipgloss.Width(hints) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	return styles.FooterStyle.Width(v.width - 2).Render(hints + spaces)
}

// tableHeight returns the visible table height.
func (v *RolesView) tableHeight() int {
	// status bar(3 with border) + title(1) + header(2 with bottom border) + footer(3 with border) = 9
	return max(1, v.height-9)
}

// moveSelection moves the selection by delta rows.
func (v *RolesView) moveSelection(delta int) {
	v.selectedIdx += delta
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	if v.selectedIdx >= len(v.roles) {
		v.selectedIdx = max(0, len(v.roles)-1)
	}
	v.ensureVisible()
}

// pageDown moves down by one page.
func (v *RolesView) pageDown() {
	pageSize := v.tableHeight()
	v.selectedIdx += pageSize
	if v.selectedIdx >= len(v.roles) {
		v.selectedIdx = max(0, len(v.roles)-1)
	}
	v.ensureVisible()
}

// pageUp moves up by one page.
func (v *RolesView) pageUp() {
	pageSize := v.tableHeight()
	v.selectedIdx -= pageSize
	if v.selectedIdx < 0 {
		v.selectedIdx = 0
	}
	v.ensureVisible()
}

// ensureVisible adjusts scroll offset to keep selection visible.
func (v *RolesView) ensureVisible() {
	visibleHeight := v.tableHeight()
	if visibleHeight <= 0 {
		return
	}

	if v.selectedIdx < v.scrollOffset {
		v.scrollOffset = v.selectedIdx
	}
	if v.selectedIdx >= v.scrollOffset+visibleHeight {
		v.scrollOffset = v.selectedIdx - visibleHeight + 1
	}
}

// sortRoles sorts the roles list based on current sort settings.
func (v *RolesView) sortRoles() {
	switch v.sortColumn {
	case SortByName:
		if v.sortAsc {
			sortRolesByNameAsc(v.roles)
		} else {
			sortRolesByNameDesc(v.roles)
		}
	case SortByAttributes:
		// Sort by attribute count
		if v.sortAsc {
			sortRolesByAttrsAsc(v.roles)
		} else {
			sortRolesByAttrsDesc(v.roles)
		}
	case SortByConnLimit:
		if v.sortAsc {
			sortRolesByConnLimitAsc(v.roles)
		} else {
			sortRolesByConnLimitDesc(v.roles)
		}
	}
}

// cycleSortColumn cycles through sort columns.
func (v *RolesView) cycleSortColumn() {
	v.sortColumn = SortColumn((int(v.sortColumn) + 1) % 3)
	v.sortRoles()
}

// toggleSortDirection toggles the sort direction.
func (v *RolesView) toggleSortDirection() {
	v.sortAsc = !v.sortAsc
	v.sortRoles()
}

// showToast displays a toast message.
func (v *RolesView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// Sort helpers
func sortRolesByNameAsc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].Name > roles[j].Name {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}

func sortRolesByNameDesc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].Name < roles[j].Name {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}

func countAttrs(r models.Role) int {
	count := 0
	if r.IsSuperuser {
		count++
	}
	if r.CanLogin {
		count++
	}
	if r.CanCreateRole {
		count++
	}
	if r.CanCreateDB {
		count++
	}
	if r.CanBypassRLS {
		count++
	}
	return count
}

func sortRolesByAttrsAsc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if countAttrs(roles[i]) > countAttrs(roles[j]) {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}

func sortRolesByAttrsDesc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if countAttrs(roles[i]) < countAttrs(roles[j]) {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}

func sortRolesByConnLimitAsc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].ConnectionLimit > roles[j].ConnectionLimit {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}

func sortRolesByConnLimitDesc(roles []models.Role) {
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].ConnectionLimit < roles[j].ConnectionLimit {
				roles[i], roles[j] = roles[j], roles[i]
			}
		}
	}
}
