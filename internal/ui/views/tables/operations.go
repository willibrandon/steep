// Package tables provides the Tables view for schema/table statistics monitoring.
package tables

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// OperationMenuItem represents a menu item in the operations menu.
type OperationMenuItem struct {
	Label          string
	Operation      models.OperationType
	Description    string
	Disabled       bool
	DisabledReason string
}

// OperationsMenu represents the maintenance operations selection menu.
type OperationsMenu struct {
	Table         *models.Table
	SelectedIndex int
	Items         []OperationMenuItem
	ReadOnlyMode  bool
}

// NewOperationsMenu creates a new operations menu for the given table.
func NewOperationsMenu(table *models.Table, readOnly bool) *OperationsMenu {
	menu := &OperationsMenu{
		Table:        table,
		ReadOnlyMode: readOnly,
	}
	menu.Items = menu.buildMenuItems()
	return menu
}

// buildMenuItems creates the menu items based on current state.
func (m *OperationsMenu) buildMenuItems() []OperationMenuItem {
	items := []OperationMenuItem{
		{
			Label:          "VACUUM",
			Operation:      models.OpVacuum,
			Description:    "Reclaim dead tuple space",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
		{
			Label:          "VACUUM FULL",
			Operation:      models.OpVacuumFull,
			Description:    "Full table rewrite (LOCKS)",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
		{
			Label:          "VACUUM ANALYZE",
			Operation:      models.OpVacuumAnalyze,
			Description:    "Vacuum + update stats",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
		{
			Label:          "ANALYZE",
			Operation:      models.OpAnalyze,
			Description:    "Update planner stats",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
		{
			Label:          "REINDEX TABLE",
			Operation:      models.OpReindexTable,
			Description:    "Rebuild indexes (LOCKS)",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
		{
			Label:          "REINDEX CONCURRENTLY",
			Operation:      models.OpReindexConcurrently,
			Description:    "Rebuild indexes (no lock)",
			Disabled:       m.ReadOnlyMode,
			DisabledReason: "Read-only",
		},
	}
	return items
}

// MoveUp moves selection up in the menu.
func (m *OperationsMenu) MoveUp() {
	if m.SelectedIndex > 0 {
		m.SelectedIndex--
	}
}

// MoveDown moves selection down in the menu.
func (m *OperationsMenu) MoveDown() {
	if m.SelectedIndex < len(m.Items)-1 {
		m.SelectedIndex++
	}
}

// SelectedItem returns the currently selected menu item.
func (m *OperationsMenu) SelectedItem() *OperationMenuItem {
	if m.SelectedIndex >= 0 && m.SelectedIndex < len(m.Items) {
		return &m.Items[m.SelectedIndex]
	}
	return nil
}

// View renders the operations menu.
func (m *OperationsMenu) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render(fmt.Sprintf("Operations for %s.%s", m.Table.SchemaName, m.Table.Name)))
	b.WriteString("\n\n")

	// Menu items
	for i, item := range m.Items {
		cursor := "  "
		if i == m.SelectedIndex {
			cursor = "> "
		}

		var line string
		if item.Disabled {
			dimStyle := lipgloss.NewStyle().Foreground(styles.ColorTextDim)
			line = dimStyle.Render(fmt.Sprintf("%s%s - %s [%s]", cursor, item.Label, item.Description, item.DisabledReason))
		} else if i == m.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(styles.ColorAccent)
			line = selectedStyle.Render(fmt.Sprintf("%s%s", cursor, item.Label)) +
				lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(fmt.Sprintf(" - %s", item.Description))
		} else {
			line = fmt.Sprintf("%s%s - %s", cursor, item.Label, item.Description)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[Enter] Execute  [Esc] Cancel"))

	return b.String()
}

// ConfirmOperationDialog displays operation confirmation.
type ConfirmOperationDialog struct {
	Operation   models.OperationType
	Target      string // schema.table
	Description string
	Warning     string // Optional warning message
	Width       int
}

// NewConfirmOperationDialog creates a confirmation dialog for an operation.
func NewConfirmOperationDialog(op models.OperationType, schemaName, tableName string, width int) *ConfirmOperationDialog {
	return &ConfirmOperationDialog{
		Operation:   op,
		Target:      fmt.Sprintf("%s.%s", schemaName, tableName),
		Description: getOperationDescription(op),
		Warning:     GetOperationWarning(op),
		Width:       width,
	}
}

// View renders the confirmation dialog.
func (d *ConfirmOperationDialog) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(headerStyle.Render(fmt.Sprintf("%s %s?", d.Operation, d.Target)))
	b.WriteString("\n\n")

	// Description
	b.WriteString(d.Description)
	b.WriteString("\n")

	// Warning if present
	if d.Warning != "" {
		b.WriteString("\n")
		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")). // Orange
			Bold(true)
		b.WriteString(warningStyle.Render("⚠ " + d.Warning))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[y/Enter] Execute  [n/Esc] Cancel"))

	// Wrap in border
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(d.Width)

	return dialogStyle.Render(b.String())
}

// getOperationDescription returns a description for the operation type.
func getOperationDescription(op models.OperationType) string {
	switch op {
	case models.OpVacuum:
		return "Reclaim storage space from dead tuples without blocking reads."
	case models.OpVacuumFull:
		return "Rewrite the entire table to reclaim maximum space. Returns space to the operating system."
	case models.OpVacuumAnalyze:
		return "Reclaim space and update query planner statistics in one operation."
	case models.OpAnalyze:
		return "Update statistics used by the query planner for optimal execution plans."
	case models.OpReindexTable:
		return "Rebuild all indexes on this table to remove bloat and corruption."
	case models.OpReindexConcurrently:
		return "Rebuild all indexes without blocking writes. Slower but non-blocking."
	case models.OpReindexIndex:
		return "Rebuild a specific index."
	default:
		return "Execute maintenance operation."
	}
}

// GetOperationWarning returns appropriate warning for operation type.
func GetOperationWarning(op models.OperationType) string {
	switch op {
	case models.OpVacuumFull:
		return "VACUUM FULL acquires an exclusive lock. The table will be unavailable during the operation."
	case models.OpReindexTable, models.OpReindexIndex:
		return "REINDEX locks the table for writes during the operation."
	default:
		return ""
	}
}

// ProgressIndicator displays operation progress.
type ProgressIndicator struct {
	Operation   *models.MaintenanceOperation
	Width       int
	ShowPhase   bool
	ShowPercent bool
}

// View renders the progress indicator.
func (p *ProgressIndicator) View() string {
	if p.Operation == nil {
		return ""
	}

	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(headerStyle.Render(fmt.Sprintf("%s %s.%s",
		p.Operation.Type,
		p.Operation.TargetSchema,
		p.Operation.TargetTable)))
	b.WriteString("\n\n")

	// Progress bar
	if p.Operation.Progress != nil && p.Operation.Progress.PercentComplete > 0 {
		bar := renderProgressBar(p.Operation.Progress.PercentComplete, p.Width-10)
		b.WriteString(fmt.Sprintf("%s %5.1f%%\n", bar, p.Operation.Progress.PercentComplete))

		// Phase
		if p.ShowPhase && p.Operation.Progress.Phase != "" {
			phaseStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
			b.WriteString(phaseStyle.Render(fmt.Sprintf("Phase: %s\n", p.Operation.Progress.Phase)))
		}
	} else {
		// No progress tracking (ANALYZE, REINDEX)
		spinnerChars := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		spinnerIdx := int(time.Now().UnixNano()/int64(100*time.Millisecond)) % len(spinnerChars)
		b.WriteString(fmt.Sprintf("%c In progress...\n", spinnerChars[spinnerIdx]))
	}

	// Duration
	elapsed := time.Since(p.Operation.StartedAt)
	durationStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(durationStyle.Render(fmt.Sprintf("Elapsed: %s\n", formatDuration(elapsed))))

	// Cancel hint
	b.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(hintStyle.Render("[c] Cancel operation  [Esc] Continue in background"))

	return b.String()
}

// renderProgressBar creates an ASCII progress bar.
func renderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 20
	}
	filled := int(float64(width) * percent / 100)
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return "[" + bar + "]"
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

// CancelConfirmDialog displays cancellation confirmation.
type CancelConfirmDialog struct {
	Operation *models.MaintenanceOperation
	Width     int
}

// View renders the cancel confirmation dialog.
func (d *CancelConfirmDialog) View() string {
	if d.Operation == nil {
		return ""
	}

	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(headerStyle.Render("Cancel operation?"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Cancel %s on %s.%s?\n",
		d.Operation.Type,
		d.Operation.TargetSchema,
		d.Operation.TargetTable))

	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")).
		Bold(true)
	b.WriteString("\n")
	b.WriteString(warningStyle.Render("⚠ The operation may have partially completed."))
	b.WriteString("\n")

	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	b.WriteString(footerStyle.Render("[y/Enter] Cancel  [n/Esc] Continue"))

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(d.Width)

	return dialogStyle.Render(b.String())
}

// IsReadOnlyBlocked checks if an operation is blocked by read-only mode.
func IsReadOnlyBlocked(readOnly bool, op models.OperationType) bool {
	if !readOnly {
		return false
	}

	// All maintenance operations are blocked in read-only mode
	switch op {
	case models.OpVacuum, models.OpVacuumFull, models.OpVacuumAnalyze,
		models.OpAnalyze, models.OpReindexTable, models.OpReindexConcurrently, models.OpReindexIndex:
		return true
	default:
		return false
	}
}

// GetReadOnlyMessage returns a user-friendly message for read-only mode blocking.
func GetReadOnlyMessage(op models.OperationType) string {
	return fmt.Sprintf("%s blocked: application is in read-only mode", op)
}

// GetActionableErrorMessage converts an error to a user-friendly actionable message.
// Per contracts/maintenance.go.md error table (T079).
func GetActionableErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// Check for specific error patterns and return actionable messages
	switch {
	case strings.Contains(errStr, "permission denied"):
		return "Insufficient privileges. Ensure your database user has the required permissions on this table."
	case strings.Contains(errStr, "does not exist"):
		return "Table not found. It may have been dropped or renamed."
	case strings.Contains(errStr, "could not obtain lock"):
		return "Could not obtain lock. The table is being used by another operation. Try again later."
	case strings.Contains(errStr, "canceling statement"):
		return "Operation was cancelled."
	case strings.Contains(errStr, "read-only"):
		return "Operation blocked: application is in read-only mode."
	case strings.Contains(errStr, "connection"):
		return "Database connection lost. The operation may still be running server-side. Check pg_stat_activity."
	case strings.Contains(errStr, "timeout"):
		return "Operation timed out. For large tables, consider running maintenance during off-peak hours."
	default:
		return errStr
	}
}
