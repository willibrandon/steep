package components

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/logger"
)

var (
	debugPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	debugTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("62"))

	debugWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	debugErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	debugInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
)

// DebugPanel displays recent warnings and errors.
type DebugPanel struct {
	viewport viewport.Model
	width    int
	height   int
	visible  bool
}

// NewDebugPanel creates a new debug panel.
func NewDebugPanel() *DebugPanel {
	return &DebugPanel{}
}

// SetSize sets the panel dimensions.
func (d *DebugPanel) SetSize(width, height int) {
	d.width = width
	d.height = height

	// Panel takes up 80% width, 60% height, centered
	panelWidth := width * 80 / 100
	panelHeight := height * 60 / 100
	if panelWidth < 60 {
		panelWidth = 60
	}
	if panelHeight < 10 {
		panelHeight = 10
	}

	d.viewport = viewport.New(panelWidth-4, panelHeight-4) // Account for border and padding
	d.viewport.Style = lipgloss.NewStyle()
}

// Toggle toggles panel visibility.
func (d *DebugPanel) Toggle() {
	d.visible = !d.visible
	if d.visible {
		d.refresh()
	}
}

// Show shows the panel.
func (d *DebugPanel) Show() {
	d.visible = true
	d.refresh()
}

// Hide hides the panel.
func (d *DebugPanel) Hide() {
	d.visible = false
}

// IsVisible returns whether the panel is visible.
func (d *DebugPanel) IsVisible() bool {
	return d.visible
}

// refresh updates the viewport content with current log entries.
func (d *DebugPanel) refresh() {
	entries := logger.GetEntries()

	var lines []string
	for _, e := range entries {
		var style lipgloss.Style
		switch e.Level {
		case slog.LevelWarn:
			style = debugWarnStyle
		case slog.LevelError:
			style = debugErrorStyle
		default:
			style = debugInfoStyle
		}
		lines = append(lines, style.Render(e.Format()))
	}

	if len(lines) == 0 {
		lines = append(lines, debugInfoStyle.Render("No warnings or errors"))
	}

	d.viewport.SetContent(strings.Join(lines, "\n"))
	d.viewport.GotoBottom()
}

// Update handles messages.
func (d *DebugPanel) Update(msg tea.Msg) (*DebugPanel, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "D", "d", "q":
			d.Hide()
			return d, nil
		case "c", "C":
			// Clear counts
			logger.ClearCounts()
			d.refresh()
			return d, nil
		case "r", "R":
			// Refresh
			d.refresh()
			return d, nil
		}
	}

	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return d, cmd
}

// View renders the panel as an overlay.
func (d *DebugPanel) View() string {
	if !d.visible {
		return ""
	}

	// Refresh content each render
	d.refresh()

	warnCount, errCount := logger.GetCounts()
	title := debugTitleStyle.Render("Debug Panel")
	counts := fmt.Sprintf(" (%d warnings, %d errors)", warnCount, errCount)
	help := debugInfoStyle.Render(" [D/Esc] close  [C] clear counts  [R] refresh  [j/k] scroll")

	header := title + debugInfoStyle.Render(counts)

	panelWidth := d.width * 80 / 100
	if panelWidth < 60 {
		panelWidth = 60
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		strings.Repeat("─", panelWidth-4),
		d.viewport.View(),
		strings.Repeat("─", panelWidth-4),
		help,
	)

	panel := debugPanelStyle.
		Width(panelWidth).
		Render(content)

	// Center the panel
	return lipgloss.Place(d.width, d.height,
		lipgloss.Center, lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}
