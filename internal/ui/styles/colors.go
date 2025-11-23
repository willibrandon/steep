// Package styles provides centralized Lipgloss styling for Steep UI.
package styles

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
)

// Color palette for Steep UI
var (
	// Connection state colors
	ColorActive   = lipgloss.Color("10")  // Green - active connections
	ColorIdle     = lipgloss.Color("8")   // Gray - idle connections
	ColorIdleTxn  = lipgloss.Color("11")  // Yellow - idle in transaction
	ColorAborted  = lipgloss.Color("208") // Orange - idle in txn (aborted)
	ColorFastpath = lipgloss.Color("12")  // Blue - fastpath function call
	ColorDisabled = lipgloss.Color("9")   // Red - disabled

	// Panel status colors
	ColorWarningBg  = lipgloss.Color("11") // Yellow background
	ColorCriticalBg = lipgloss.Color("9")  // Red background
	ColorWarningFg  = lipgloss.Color("0")  // Black text on warning
	ColorCriticalFg = lipgloss.Color("15") // White text on critical

	// UI element colors
	ColorBorder  = lipgloss.Color("240") // Gray - all borders
	ColorAccent  = lipgloss.Color("6")   // Cyan - titles, highlights
	ColorMuted   = lipgloss.Color("8")   // Dark gray - secondary text
	ColorSuccess = lipgloss.Color("10")  // Green - success messages
	ColorError   = lipgloss.Color("9")   // Red - error messages

	// Selection colors
	ColorSelectedFg = lipgloss.Color("229") // Light yellow text
	ColorSelectedBg = lipgloss.Color("57")  // Purple background

	// Lock blocking colors
	ColorBlocked  = lipgloss.Color("9")   // Red - blocked queries (waiting)
	ColorBlocking = lipgloss.Color("11")  // Yellow - blocking queries (holding lock)
)

// ConnectionStateColor returns the appropriate color for a connection state.
func ConnectionStateColor(state models.ConnectionState) lipgloss.Color {
	switch state {
	case models.StateActive:
		return ColorActive
	case models.StateIdle:
		return ColorIdle
	case models.StateIdleInTransaction:
		return ColorIdleTxn
	case models.StateIdleInTransactionAborted:
		return ColorAborted
	case models.StateFastpath:
		return ColorFastpath
	case models.StateDisabled:
		return ColorDisabled
	default:
		return ColorMuted
	}
}

// PanelStatusColors returns foreground and background colors for panel status.
func PanelStatusColors(status models.PanelStatus) (fg, bg lipgloss.Color) {
	switch status {
	case models.PanelWarning:
		return ColorWarningFg, ColorWarningBg
	case models.PanelCritical:
		return ColorCriticalFg, ColorCriticalBg
	default:
		return ColorActive, lipgloss.Color("") // Normal: green text, no background
	}
}
