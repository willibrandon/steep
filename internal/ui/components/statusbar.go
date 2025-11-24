package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
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
	reconnecting      bool
	reconnectAttempt  int
	reconnectMaxAttempts int
}

// NewStatusBar creates a new status bar component
func NewStatusBar() *StatusBar {
	return &StatusBar{
		dateFormat: "2006-01-02 15:04:05",
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

	// Build status line
	statusLine := fmt.Sprintf("%s | %s | %s%s",
		statusIndicator,
		dbName,
		timestamp,
		metricsSection,
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
