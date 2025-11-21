package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// DialogType represents the type of confirmation dialog.
type DialogType int

const (
	DialogCancel DialogType = iota
	DialogTerminate
)

// ConfirmDialog represents a confirmation dialog for destructive actions.
type ConfirmDialog struct {
	width      int
	height     int
	dialogType DialogType
	pid        int
	user       string
	query      string
	visible    bool
}

// NewConfirmDialog creates a new confirmation dialog.
func NewConfirmDialog() *ConfirmDialog {
	return &ConfirmDialog{}
}

// Show displays the dialog with the given parameters.
func (d *ConfirmDialog) Show(dialogType DialogType, pid int, user, query string) {
	d.dialogType = dialogType
	d.pid = pid
	d.user = user
	d.query = query
	d.visible = true
}

// Hide hides the dialog.
func (d *ConfirmDialog) Hide() {
	d.visible = false
}

// IsVisible returns whether the dialog is visible.
func (d *ConfirmDialog) IsVisible() bool {
	return d.visible
}

// GetType returns the dialog type.
func (d *ConfirmDialog) GetType() DialogType {
	return d.dialogType
}

// GetPID returns the PID being acted upon.
func (d *ConfirmDialog) GetPID() int {
	return d.pid
}

// SetSize sets the dialog dimensions.
func (d *ConfirmDialog) SetSize(width, height int) {
	d.width = width
	d.height = height
}

// View renders the confirmation dialog.
func (d *ConfirmDialog) View() string {
	if !d.visible {
		return ""
	}

	var title, warning string
	if d.dialogType == DialogCancel {
		title = "Cancel Query"
		warning = "This will cancel the running query."
	} else {
		title = "Terminate Connection"
		warning = "This will forcefully terminate the connection."
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorCriticalFg).
		MarginBottom(1)

	// Truncate query for display
	queryDisplay := d.query
	if len(queryDisplay) > 50 {
		queryDisplay = queryDisplay[:47] + "..."
	}
	if queryDisplay == "" {
		queryDisplay = "(no query)"
	}

	details := fmt.Sprintf("PID: %d\nUser: %s\nQuery: %s", d.pid, d.user, queryDisplay)

	warningStyle := lipgloss.NewStyle().
		Foreground(styles.ColorWarningFg).
		MarginTop(1)

	promptStyle := lipgloss.NewStyle().
		MarginTop(1).
		Bold(true)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(title),
		details,
		warningStyle.Render(warning),
		promptStyle.Render("[y] Confirm  [n] Cancel"),
	)

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorCriticalFg).
		Padding(1, 2).
		Width(60)

	return dialogStyle.Render(content)
}
