package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// StatusBar represents the status bar component
type StatusBar struct {
	width int

	// Status data
	connected         bool
	database          string
	timestamp         time.Time
	activeConnections int
	dateFormat        string

	// Reconnection status
	reconnecting         bool
	reconnectAttempt     int
	reconnectMaxAttempts int

	// Read-only mode
	readOnly bool

	// Chart visibility
	chartsVisible bool
}

// NewStatusBar creates a new status bar component
func NewStatusBar() *StatusBar {
	return &StatusBar{
		dateFormat:    "2006-01-02 15:04:05",
		chartsVisible: true,
	}
}

// SetSize sets the width of the status bar
func (s *StatusBar) SetSize(width int) {
	s.width = width
}

// SetConnected sets the connection status
func (s *StatusBar) SetConnected(connected bool) {
	s.connected = connected
}

// SetDatabase sets the database name
func (s *StatusBar) SetDatabase(database string) {
	s.database = database
}

// SetTimestamp sets the current timestamp
func (s *StatusBar) SetTimestamp(timestamp time.Time) {
	s.timestamp = timestamp
}

// SetActiveConnections sets the active connection count
func (s *StatusBar) SetActiveConnections(count int) {
	s.activeConnections = count
}

// SetDateFormat sets the date format string
func (s *StatusBar) SetDateFormat(format string) {
	s.dateFormat = format
}

// SetReconnecting sets the reconnection status
func (s *StatusBar) SetReconnecting(reconnecting bool, attempt, maxAttempts int) {
	s.reconnecting = reconnecting
	s.reconnectAttempt = attempt
	s.reconnectMaxAttempts = maxAttempts
}

// SetReadOnly sets the read-only mode flag
func (s *StatusBar) SetReadOnly(readOnly bool) {
	s.readOnly = readOnly
}

// SetChartsVisible sets the chart visibility state
func (s *StatusBar) SetChartsVisible(visible bool) {
	s.chartsVisible = visible
}

// View renders the status bar
func (s *StatusBar) View() string {
	// Connection status indicator
	var statusIndicator string
	if s.reconnecting {
		statusIndicator = styles.WarningStyle.Render(
			fmt.Sprintf("● Reconnecting (%d/%d)", s.reconnectAttempt, s.reconnectMaxAttempts))
	} else if s.connected {
		statusIndicator = styles.StatusConnectedStyle.Render("● Connected")
	} else {
		statusIndicator = styles.StatusDisconnectedStyle.Render("● Disconnected")
	}

	// Database name
	dbName := s.database
	if dbName == "" {
		dbName = "N/A"
	}

	// Timestamp
	timestamp := s.timestamp.Format(s.dateFormat)

	// Connection count (only show if connected)
	var metricsSection string
	if s.connected {
		metricsSection = fmt.Sprintf(" | Conns: %d", s.activeConnections)
	}

	// Debug indicator (warning/error counts) - only shown in debug mode
	var debugSection string
	if logger.IsDebugEnabled() {
		warnCount, errCount := logger.GetCounts()
		if warnCount > 0 || errCount > 0 {
			var parts []string
			if warnCount > 0 {
				parts = append(parts, styles.WarningStyle.Render(fmt.Sprintf("⚠ %d", warnCount)))
			}
			if errCount > 0 {
				parts = append(parts, styles.ErrorStyle.Render(fmt.Sprintf("✕ %d", errCount)))
			}
			debugSection = " | "
			for i, p := range parts {
				if i > 0 {
					debugSection += " "
				}
				debugSection += p
			}
		}
	}

	// Read-only mode indicator
	var readOnlySection string
	if s.readOnly {
		readOnlySection = " | " + styles.WarningStyle.Render("READ-ONLY")
	}

	// Chart visibility indicator (only show when hidden)
	var chartsSection string
	if !s.chartsVisible {
		chartsSection = " | " + styles.MutedStyle.Render("Charts OFF")
	}

	// Build status line
	statusLine := fmt.Sprintf("%s | %s | %s%s%s%s%s",
		statusIndicator,
		dbName,
		timestamp,
		metricsSection,
		debugSection,
		readOnlySection,
		chartsSection,
	)

	// Apply styling
	statusBar := styles.StatusBarStyle.Render(statusLine)

	// Pad to full width if needed
	if s.width > 0 {
		statusBar = lipgloss.NewStyle().
			Width(s.width).
			Render(statusLine)
	}

	return statusBar
}

// ShortView renders a compact version of the status bar
func (s *StatusBar) ShortView() string {
	if s.connected {
		return styles.StatusConnectedStyle.Render("●") + " " + s.database
	}
	return styles.StatusDisconnectedStyle.Render("●") + " Disconnected"
}
