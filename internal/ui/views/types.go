package views

import tea "github.com/charmbracelet/bubbletea"

// ViewType represents the different monitoring views available
type ViewType int

const (
	ViewDashboard ViewType = iota
	ViewActivity
	ViewQueries
	ViewLocks
	ViewTables
	ViewReplication
	ViewSQLEditor
	ViewConfig
	ViewLogs
	ViewRoles
)

// String returns the string representation of the view type
func (v ViewType) String() string {
	switch v {
	case ViewDashboard:
		return "Dashboard"
	case ViewActivity:
		return "Activity"
	case ViewQueries:
		return "Queries"
	case ViewLocks:
		return "Locks"
	case ViewTables:
		return "Tables"
	case ViewReplication:
		return "Replication"
	case ViewSQLEditor:
		return "SQL Editor"
	case ViewConfig:
		return "Configuration"
	case ViewLogs:
		return "Logs"
	case ViewRoles:
		return "Roles"
	default:
		return "Unknown"
	}
}

// ShortName returns a short name for the view
func (v ViewType) ShortName() string {
	switch v {
	case ViewDashboard:
		return "dash"
	case ViewActivity:
		return "act"
	case ViewQueries:
		return "qry"
	case ViewLocks:
		return "lck"
	case ViewTables:
		return "tbl"
	case ViewReplication:
		return "rep"
	case ViewSQLEditor:
		return "sql"
	case ViewConfig:
		return "cfg"
	case ViewLogs:
		return "log"
	case ViewRoles:
		return "rol"
	default:
		return "unk"
	}
}

// ViewModel defines the interface that all views must implement
type ViewModel interface {
	// Init initializes the view and returns any initial commands
	Init() tea.Cmd

	// Update handles messages and updates the view state
	Update(tea.Msg) (ViewModel, tea.Cmd)

	// View renders the view to a string
	View() string

	// SetSize sets the dimensions of the view
	SetSize(width, height int)
}
