package styles

import "github.com/charmbracelet/lipgloss"

// Common border styles
var (
	// BorderNormal is the standard border for most UI elements
	BorderNormal = lipgloss.NormalBorder()

	// BorderRounded is used for metric panels
	BorderRounded = lipgloss.RoundedBorder()
)

// Panel styles
var (
	// PanelStyle is the base style for metric panels
	PanelStyle = lipgloss.NewStyle().
			Border(BorderRounded).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	// PanelLabelStyle is for metric panel labels
	PanelLabelStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Align(lipgloss.Center)

	// PanelValueStyle is for metric panel values
	PanelValueStyle = lipgloss.NewStyle().
			Bold(true).
			Align(lipgloss.Center)
)

// Table styles
var (
	// TableBorderStyle wraps the activity table
	TableBorderStyle = lipgloss.NewStyle().
				Border(BorderNormal).
				BorderForeground(ColorBorder)

	// TableHeaderStyle is for table column headers
	TableHeaderStyle = lipgloss.NewStyle().
				BorderStyle(BorderNormal).
				BorderForeground(ColorBorder).
				BorderBottom(true).
				Bold(false)

	// TableSelectedStyle is for the selected row
	TableSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorSelectedFg).
				Background(ColorSelectedBg).
				Bold(false)
)

// Status bar styles
var (
	// StatusBarStyle wraps the status bar
	StatusBarStyle = lipgloss.NewStyle().
			Border(BorderNormal).
			BorderForeground(ColorBorder)

	// StatusTitleStyle is for the connection string
	StatusTitleStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	// StatusTimeStyle is for the timestamp
	StatusTimeStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// Footer styles
var (
	// FooterStyle wraps the footer
	FooterStyle = lipgloss.NewStyle().
			Border(BorderNormal).
			BorderForeground(ColorBorder)

	// FooterHintStyle is for keyboard hints
	FooterHintStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// FooterCountStyle is for pagination count
	FooterCountStyle = lipgloss.NewStyle().
				Foreground(ColorAccent)
)

// Dialog styles
var (
	// DialogStyle wraps confirmation dialogs
	DialogStyle = lipgloss.NewStyle().
			Border(BorderRounded).
			BorderForeground(ColorAccent).
			Padding(1, 2)

	// DialogTitleStyle is for dialog titles
	DialogTitleStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true).
				Align(lipgloss.Center)

	// DialogButtonStyle is for dialog buttons
	DialogButtonStyle = lipgloss.NewStyle().
				Padding(0, 2).
				Margin(0, 1)

	// DialogButtonActiveStyle is for the focused button
	DialogButtonActiveStyle = DialogButtonStyle.
				Foreground(ColorSelectedFg).
				Background(ColorSelectedBg)
)

// Message styles
var (
	// SuccessStyle is for success messages
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	// ErrorStyle is for error messages
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError)

	// WarningStyle is for warning messages
	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorIdleTxn)
)

// Help overlay styles
var (
	// HelpStyle wraps the help overlay
	HelpStyle = lipgloss.NewStyle().
			Border(BorderRounded).
			BorderForeground(ColorAccent).
			Padding(1, 2)

	// HelpTitleStyle is for the help title
	HelpTitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Align(lipgloss.Center)

	// HelpKeyStyle is for keyboard shortcuts
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorAccent)

	// HelpDescStyle is for shortcut descriptions
	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// HelpDialogStyle is for the help dialog box
	HelpDialogStyle = lipgloss.NewStyle().
			Border(BorderRounded).
			BorderForeground(ColorAccent).
			Padding(1, 2)
)

// Common UI styles
var (
	// TitleStyle is for section titles
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// AccentStyle is for accented text
	AccentStyle = lipgloss.NewStyle().
			Foreground(ColorAccent)

	// MutedStyle is for muted/secondary text
	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// BorderStyle is for borders using muted color
	BorderStyle = lipgloss.NewStyle().
			Foreground(ColorBorder)
)

// Legacy compatibility styles (for existing components)
var (
	// InfoStyle is for informational messages
	InfoStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// DimStyle is for dimmed/secondary text
	DimStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// ViewTitleStyle is for view titles
	ViewTitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// HeaderStyle is for section headers
	HeaderStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			MarginTop(1)

	// StatusConnectedStyle is for connected status indicator
	StatusConnectedStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess)

	// StatusDisconnectedStyle is for disconnected status indicator
	StatusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(ColorError)

	// ColorPrimary is the primary accent color
	ColorPrimary = ColorAccent

	// ColorText is the default text color
	ColorText = lipgloss.Color("7")

	// ColorTextDim is for dimmed text
	ColorTextDim = ColorMuted

	// TableCellStyle is for table cells
	TableCellStyle = lipgloss.NewStyle()

	// TableRowAltStyle is for alternating row backgrounds
	TableRowAltStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("235"))
)
