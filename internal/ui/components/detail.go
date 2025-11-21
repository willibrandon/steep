package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// DetailView displays the full query details for a selected connection.
type DetailView struct {
	viewport   viewport.Model
	connection *models.Connection
	width      int
	height     int
	ready      bool
}

// NewDetailView creates a new detail view component.
func NewDetailView() *DetailView {
	return &DetailView{}
}

// SetConnection sets the connection to display details for.
func (d *DetailView) SetConnection(conn *models.Connection) {
	d.connection = conn
	d.updateContent()
}

// SetSize sets the dimensions of the detail view.
func (d *DetailView) SetSize(width, height int) {
	d.width = width
	d.height = height

	if !d.ready {
		d.viewport = viewport.New(width-4, height-6)
		d.viewport.HighPerformanceRendering = false
		d.ready = true
	} else {
		d.viewport.Width = width - 4
		d.viewport.Height = height - 6
	}

	d.updateContent()
}

// updateContent updates the viewport content with connection details.
func (d *DetailView) updateContent() {
	if d.connection == nil || !d.ready {
		return
	}

	var b strings.Builder

	// Connection metadata
	labelStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Width(15)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	stateStyle := lipgloss.NewStyle().
		Foreground(styles.ConnectionStateColor(d.connection.State))

	b.WriteString(labelStyle.Render("PID:"))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%d", d.connection.PID)))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("User:"))
	b.WriteString(valueStyle.Render(d.connection.User))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Database:"))
	b.WriteString(valueStyle.Render(d.connection.Database))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("State:"))
	b.WriteString(stateStyle.Render(string(d.connection.State)))
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Duration:"))
	b.WriteString(valueStyle.Render(d.connection.FormatDuration()))
	b.WriteString("\n")

	if d.connection.WaitEventType != "" {
		b.WriteString(labelStyle.Render("Wait Event:"))
		b.WriteString(valueStyle.Render(fmt.Sprintf("%s: %s", d.connection.WaitEventType, d.connection.WaitEvent)))
		b.WriteString("\n")
	}

	if d.connection.ClientAddr != "" {
		b.WriteString(labelStyle.Render("Client:"))
		b.WriteString(valueStyle.Render(d.connection.ClientAddr))
		b.WriteString("\n")
	}

	if d.connection.ApplicationName != "" {
		b.WriteString(labelStyle.Render("Application:"))
		b.WriteString(valueStyle.Render(d.connection.ApplicationName))
		b.WriteString("\n")
	}

	// Query section
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Bold(true).
		Render("Query:"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", d.width-6))
	b.WriteString("\n")

	if d.connection.Query != "" {
		// Format the query for better readability
		query := d.connection.Query
		b.WriteString(query)
	} else {
		b.WriteString(lipgloss.NewStyle().
			Foreground(styles.ColorMuted).
			Render("(no query)"))
	}

	d.viewport.SetContent(b.String())
}

// Update handles messages for the detail view.
func (d *DetailView) Update(msg tea.Msg) (*DetailView, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			d.viewport.LineUp(1)
		case "down", "j":
			d.viewport.LineDown(1)
		case "pgup":
			d.viewport.HalfViewUp()
		case "pgdown":
			d.viewport.HalfViewDown()
		case "home", "g":
			d.viewport.GotoTop()
		case "end", "G":
			d.viewport.GotoBottom()
		}
	}

	d.viewport, cmd = d.viewport.Update(msg)
	return d, cmd
}

// View renders the detail view.
func (d *DetailView) View() string {
	if d.connection == nil {
		return styles.InfoStyle.Render("No connection selected")
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Bold(true)

	footerStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted)

	title := titleStyle.Render(fmt.Sprintf("Query Detail - PID %d", d.connection.PID))
	footer := footerStyle.Render("[c]ancel query  [x]kill connection  [Esc]close")

	// Build the view
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		d.viewport.View(),
		"",
		footer,
	)

	return styles.DialogStyle.Render(content)
}
