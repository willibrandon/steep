package styles

import (
	"github.com/charmbracelet/lipgloss"
)

// Color scheme
var (
	// Primary colors
	ColorPrimary   = lipgloss.Color("#7D56F4")
	ColorSecondary = lipgloss.Color("#5F9EA0")
	ColorAccent    = lipgloss.Color("#FF69B4")

	// Status colors
	ColorSuccess = lipgloss.Color("#04B575")
	ColorWarning = lipgloss.Color("#FFB86C")
	ColorError   = lipgloss.Color("#FF5555")
	ColorInfo    = lipgloss.Color("#8BE9FD")

	// Text colors
	ColorText       = lipgloss.Color("#FFFFFF")
	ColorTextDim    = lipgloss.Color("#6C7086")
	ColorTextBright = lipgloss.Color("#F8F8F2")

	// Background colors
	ColorBackground       = lipgloss.Color("#1E1E2E")
	ColorBackgroundAlt    = lipgloss.Color("#313244")
	ColorBackgroundBright = lipgloss.Color("#45475A")

	// Border colors
	ColorBorder       = lipgloss.Color("#6C7086")
	ColorBorderActive = lipgloss.Color("#7D56F4")
)

// Spacing constants
const (
	PaddingSmall  = 1
	PaddingMedium = 2
	PaddingLarge  = 3

	MarginSmall  = 1
	MarginMedium = 2
	MarginLarge  = 3
)

// Border styles
var (
	BorderNormal = lipgloss.NormalBorder()
	BorderRound  = lipgloss.RoundedBorder()
	BorderDouble = lipgloss.DoubleBorder()
	BorderThick  = lipgloss.ThickBorder()
)

// Common styles
var (
	// Base style for the application
	BaseStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(ColorBackground)

	// Header style
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	// Status bar style
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(ColorBackgroundAlt).
			Padding(0, 1)

	// Status bar - connected
	StatusConnectedStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)

	// Status bar - disconnected
	StatusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(ColorError).
				Bold(true)

	// Help text style
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Padding(0, 1)

	// Help dialog style
	HelpDialogStyle = lipgloss.NewStyle().
			Border(BorderRound).
			BorderForeground(ColorBorderActive).
			Padding(PaddingMedium).
			Background(ColorBackgroundAlt)

	// Table header style
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorPrimary).
				Background(ColorBackgroundAlt).
				Padding(0, 1)

	// Table cell style
	TableCellStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Padding(0, 1)

	// Table row (alternate) style
	TableRowAltStyle = lipgloss.NewStyle().
				Background(ColorBackgroundAlt)

	// Error message style
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true).
			Padding(PaddingSmall)

	// Warning message style
	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true).
			Padding(PaddingSmall)

	// Info message style
	InfoStyle = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Padding(PaddingSmall)

	// Success message style
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Padding(PaddingSmall)

	// View title style
	ViewTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(PaddingSmall, PaddingMedium).
			Border(lipgloss.Border{
		Bottom: "─",
	}).
		BorderForeground(ColorBorder)

	// Active view indicator
	ActiveViewStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// Inactive view indicator
	InactiveViewStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim)
)

// GetTheme returns the current theme configuration
// In the future, this could support light/dark theme switching
func GetTheme(themeName string) {
	// Currently only dark theme is implemented
	// Light theme colors could be added here in the future
}

// Helper function to truncate text to a maximum width
func Truncate(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}
	if maxWidth < 3 {
		return text[:maxWidth]
	}
	return text[:maxWidth-3] + "..."
}

// Helper function to center text within a given width
func Center(text string, width int) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, text)
}

// Helper function to right-align text within a given width
func AlignRight(text string, width int) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Right, text)
}
