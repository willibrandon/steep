// Package styles provides centralized Lipgloss styling for Steep UI.
package styles

import "github.com/charmbracelet/lipgloss"

// SQL Editor specific styles
var (
	// EditorBorderStyle wraps the SQL editor textarea
	EditorBorderStyle = lipgloss.NewStyle().
				Border(BorderRounded).
				BorderForeground(ColorAccent)

	// EditorBlurredBorderStyle is for unfocused editor
	EditorBlurredBorderStyle = lipgloss.NewStyle().
					Border(BorderRounded).
					BorderForeground(ColorBorder)

	// EditorLineNumberStyle is for line numbers in the editor
	EditorLineNumberStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	// EditorCursorLineStyle highlights the current line
	EditorCursorLineStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("236"))

	// EditorPlaceholderStyle is for placeholder text
	EditorPlaceholderStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Italic(true)

	// ResultsHeaderStyle is for results table column headers
	ResultsHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorAccent).
				BorderStyle(BorderNormal).
				BorderForeground(ColorBorder).
				BorderBottom(true)

	// ResultsNullStyle is for NULL values in results
	ResultsNullStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Italic(true)

	// ResultsRowSelectedStyle is for selected row in results
	ResultsRowSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorSelectedFg).
				Background(ColorSelectedBg)

	// ResultsCellSelectedStyle is for the selected cell (brighter highlight)
	ResultsCellSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(ColorAccent).
				Bold(true)

	// TransactionBadgeStyle is for the TX indicator when in transaction
	TransactionBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(ColorSuccess).
				Bold(true).
				Padding(0, 1)

	// TransactionAbortedBadgeStyle is for aborted transaction indicator
	TransactionAbortedBadgeStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("15")).
					Background(ColorError).
					Bold(true).
					Padding(0, 1)

	// ExecutingStyle is for the executing indicator
	ExecutingStyle = lipgloss.NewStyle().
			Foreground(ColorIdleTxn).
			Bold(true)

	// QueryHeaderStyle is for displaying the executed query
	QueryHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Border(BorderNormal).
				BorderForeground(ColorBorder).
				BorderBottom(true).
				Padding(0, 1)

	// PaginationStyle is for page indicators
	PaginationStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// PaginationActiveStyle is for current page indicator
	PaginationActiveStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	// CommandLineStyle is for the : command input area
	CommandLineStyle = lipgloss.NewStyle().
				Border(BorderNormal).
				BorderForeground(ColorAccent).
				Padding(0, 1)

	// HistorySearchStyle is for the Ctrl+R search overlay
	HistorySearchStyle = lipgloss.NewStyle().
				Border(BorderRounded).
				BorderForeground(ColorAccent).
				Padding(1, 2)

	// SnippetBrowserStyle is for the snippet browser overlay
	SnippetBrowserStyle = lipgloss.NewStyle().
				Border(BorderRounded).
				BorderForeground(ColorAccent).
				Padding(1, 2)

	// ExportSuccessStyle is for export success messages
	ExportSuccessStyle = SuccessStyle

	// ExportErrorStyle is for export error messages
	ExportErrorStyle = ErrorStyle
)
