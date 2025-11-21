package ui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all keyboard bindings for the application
type KeyMap struct {
	// Navigation
	Quit       key.Binding
	Help       key.Binding
	CloseDialog key.Binding
	NextView   key.Binding
	PrevView   key.Binding

	// View jumping (1-9)
	JumpToDashboard   key.Binding
	JumpToActivity    key.Binding
	JumpToQueries     key.Binding
	JumpToLocks       key.Binding
	JumpToTables      key.Binding
	JumpToReplication key.Binding

	// Table navigation
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding

	// Table actions
	Sort      key.Binding
	Filter    key.Binding
	Refresh   key.Binding
}

// DefaultKeyMap returns the default keyboard bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Navigation
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("h", "?"),
			key.WithHelp("h/?", "help"),
		),
		CloseDialog: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close dialog"),
		),
		NextView: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next view"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "previous view"),
		),

		// View jumping
		JumpToDashboard: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "dashboard"),
		),
		JumpToActivity: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "activity"),
		),
		JumpToQueries: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "queries"),
		),
		JumpToLocks: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "locks"),
		),
		JumpToTables: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "tables"),
		),
		JumpToReplication: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "replication"),
		),

		// Table navigation (vim-like)
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("home/g", "top"),
		),
		End: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("end/G", "bottom"),
		),

		// Table actions
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
	}
}

// ShortHelp returns a quick help view for the key bindings
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Help, k.Refresh, k.Filter}
}

// FullHelp returns the full help view for all key bindings
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Help, k.CloseDialog},
		{k.NextView, k.PrevView},
		{k.JumpToDashboard, k.JumpToActivity, k.JumpToQueries},
		{k.JumpToLocks, k.JumpToTables, k.JumpToReplication},
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.Home, k.End},
		{k.Sort, k.Filter, k.Refresh},
	}
}
