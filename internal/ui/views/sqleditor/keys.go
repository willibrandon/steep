package sqleditor

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines keyboard bindings for the SQL Editor view.
type KeyMap struct {
	// Editor actions
	Execute    key.Binding
	Cancel     key.Binding
	SwitchPane key.Binding

	// Results navigation
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Results pagination
	NextPage key.Binding
	PrevPage key.Binding

	// Copy actions
	CopyCell key.Binding
	CopyRow  key.Binding

	// History navigation
	HistoryPrev   key.Binding
	HistoryNext   key.Binding
	HistorySearch key.Binding

	// Snippets
	SnippetBrowser key.Binding

	// Split management
	SplitGrow   key.Binding
	SplitShrink key.Binding

	// General
	Help key.Binding
	Quit key.Binding
}

// DefaultKeyMap returns the default SQL Editor key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Editor actions
		Execute: key.NewBinding(
			key.WithKeys("ctrl+enter"),
			key.WithHelp("ctrl+enter", "execute query"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel query"),
		),
		SwitchPane: key.NewBinding(
			key.WithKeys("\\"),
			key.WithHelp("\\", "switch pane"),
		),

		// Results navigation (vim-style)
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
			key.WithHelp("g", "first row"),
		),
		End: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G", "last row"),
		),

		// Results pagination
		NextPage: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "prev page"),
		),

		// Copy actions
		CopyCell: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy cell"),
		),
		CopyRow: key.NewBinding(
			key.WithKeys("Y"),
			key.WithHelp("Y", "copy row"),
		),

		// History navigation
		HistoryPrev: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "history prev"),
		),
		HistoryNext: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "history next"),
		),
		HistorySearch: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "search history"),
		),

		// Snippets
		SnippetBrowser: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "snippets"),
		),

		// Split management
		SplitGrow: key.NewBinding(
			key.WithKeys("ctrl+up"),
			key.WithHelp("ctrl+↑", "grow editor"),
		),
		SplitShrink: key.NewBinding(
			key.WithKeys("ctrl+down"),
			key.WithHelp("ctrl+↓", "shrink editor"),
		),

		// General
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "back"),
		),
	}
}

// ShortHelp returns a condensed help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Execute, k.Cancel, k.SwitchPane, k.Help}
}

// FullHelp returns the complete help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Execute, k.Cancel, k.SwitchPane},
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.Home, k.End, k.NextPage, k.PrevPage},
		{k.CopyCell, k.CopyRow},
		{k.HistorySearch, k.SnippetBrowser},
		{k.SplitGrow, k.SplitShrink},
		{k.Help, k.Quit},
	}
}
