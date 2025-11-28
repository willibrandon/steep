package styles

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
)

// Log severity colors
var (
	// ColorSeverityError is red for ERROR, FATAL, PANIC entries.
	ColorSeverityError = lipgloss.Color("9")
	// ColorSeverityWarning is yellow for WARNING entries.
	ColorSeverityWarning = lipgloss.Color("11")
	// ColorSeverityInfo is white for LOG, INFO, NOTICE entries.
	ColorSeverityInfo = lipgloss.Color("7")
	// ColorSeverityDebug is gray for DEBUG entries.
	ColorSeverityDebug = lipgloss.Color("8")
)

// Log severity styles
var (
	// SeverityErrorStyle is the style for ERROR log entries.
	SeverityErrorStyle = lipgloss.NewStyle().Foreground(ColorSeverityError)
	// SeverityWarningStyle is the style for WARNING log entries.
	SeverityWarningStyle = lipgloss.NewStyle().Foreground(ColorSeverityWarning)
	// SeverityInfoStyle is the style for INFO log entries.
	SeverityInfoStyle = lipgloss.NewStyle().Foreground(ColorSeverityInfo)
	// SeverityDebugStyle is the style for DEBUG log entries.
	SeverityDebugStyle = lipgloss.NewStyle().Foreground(ColorSeverityDebug)
)

// Log severity badge styles (with background for column visibility)
var (
	// SeverityErrorBadge is a badge style for ERROR severity.
	SeverityErrorBadge = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(ColorSeverityError).
				Padding(0, 1)
	// SeverityWarningBadge is a badge style for WARNING severity.
	SeverityWarningBadge = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(ColorSeverityWarning).
				Padding(0, 1)
	// SeverityInfoBadge is a badge style for INFO severity.
	SeverityInfoBadge = lipgloss.NewStyle().
				Foreground(ColorSeverityInfo).
				Padding(0, 1)
	// SeverityDebugBadge is a badge style for DEBUG severity.
	SeverityDebugBadge = lipgloss.NewStyle().
				Foreground(ColorSeverityDebug).
				Padding(0, 1)
)

// Log viewer UI styles
var (
	// LogTimestampStyle is for timestamp column.
	LogTimestampStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	// LogPIDStyle is for PID column.
	LogPIDStyle = lipgloss.NewStyle().Foreground(ColorAccent)
	// LogMessageStyle is for message content.
	LogMessageStyle = lipgloss.NewStyle()
	// LogSearchHighlight is for search match highlighting.
	LogSearchHighlight = lipgloss.NewStyle().
				Background(lipgloss.Color("3")).
				Foreground(lipgloss.Color("0"))
	// LogFollowIndicator is for the FOLLOW mode indicator.
	LogFollowIndicator = lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Bold(true)
	// LogPausedIndicator is for the PAUSED mode indicator.
	LogPausedIndicator = lipgloss.NewStyle().
				Foreground(ColorMuted)
)

// SeverityStyle returns the appropriate style for a log severity.
func SeverityStyle(severity models.LogSeverity) lipgloss.Style {
	switch severity {
	case models.SeverityError:
		return SeverityErrorStyle
	case models.SeverityWarning:
		return SeverityWarningStyle
	case models.SeverityInfo:
		return SeverityInfoStyle
	case models.SeverityDebug:
		return SeverityDebugStyle
	default:
		return SeverityInfoStyle
	}
}

// SeverityBadge returns the badge style for a log severity.
func SeverityBadge(severity models.LogSeverity) lipgloss.Style {
	switch severity {
	case models.SeverityError:
		return SeverityErrorBadge
	case models.SeverityWarning:
		return SeverityWarningBadge
	case models.SeverityInfo:
		return SeverityInfoBadge
	case models.SeverityDebug:
		return SeverityDebugBadge
	default:
		return SeverityInfoBadge
	}
}
