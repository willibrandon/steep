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

	// Agent mode (client mode)
	agentMode        bool
	agentLastCollect time.Time

	// Multi-instance support (T054)
	instances       []InstanceDisplayInfo
	currentInstance string // Name of currently selected instance ("" = all)
}

// InstanceDisplayInfo holds instance information for status bar display.
type InstanceDisplayInfo struct {
	Name   string
	Status string // connected, disconnected, error, unknown
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

// SetAgentStatus sets the agent running status and last collection time
func (s *StatusBar) SetAgentStatus(running bool, lastCollect time.Time) {
	s.agentMode = running
	s.agentLastCollect = lastCollect
}

// UpdateAgentLastCollect updates the last collection timestamp
func (s *StatusBar) UpdateAgentLastCollect(lastCollect time.Time) {
	s.agentLastCollect = lastCollect
}

// SetInstances sets the list of monitored instances (T054: multi-instance support).
func (s *StatusBar) SetInstances(instances []InstanceDisplayInfo) {
	s.instances = instances
}

// SetCurrentInstance sets the currently selected instance filter.
// Empty string means show data from all instances.
func (s *StatusBar) SetCurrentInstance(name string) {
	s.currentInstance = name
}

// GetCurrentInstance returns the currently selected instance filter.
func (s *StatusBar) GetCurrentInstance() string {
	return s.currentInstance
}

// GetInstances returns the list of monitored instances.
func (s *StatusBar) GetInstances() []InstanceDisplayInfo {
	return s.instances
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

	// Agent status indicator (always shown)
	var agentSection string
	if s.agentMode {
		agentSection = " | " + styles.StatusConnectedStyle.Render("Agent: Running")
	} else {
		agentSection = " | " + styles.MutedStyle.Render("Agent: Stopped")
	}

	// Instance indicator (T054: multi-instance support)
	// Only show when agent is running and there are instances to display
	var instanceSection string
	if s.agentMode && len(s.instances) > 0 {
		if len(s.instances) == 1 {
			// Single instance - just show the name
			inst := s.instances[0]
			if inst.Status == "connected" {
				instanceSection = " | " + styles.SuccessStyle.Render(fmt.Sprintf("[%s]", inst.Name))
			} else {
				instanceSection = " | " + styles.WarningStyle.Render(fmt.Sprintf("[%s: %s]", inst.Name, inst.Status))
			}
		} else {
			// Multiple instances - show filter or count
			if s.currentInstance != "" {
				// Filtering by specific instance - use accent color to highlight current selection
				instanceSection = " | " + styles.AccentStyle.Render(fmt.Sprintf("[%s]", s.currentInstance))
			} else {
				// Show instance count with health summary
				connectedCount := 0
				for _, inst := range s.instances {
					if inst.Status == "connected" {
						connectedCount++
					}
				}
				if connectedCount == len(s.instances) {
					instanceSection = " | " + styles.SuccessStyle.Render(fmt.Sprintf("[%d instances]", len(s.instances)))
				} else {
					instanceSection = " | " + styles.WarningStyle.Render(fmt.Sprintf("[%d/%d instances]", connectedCount, len(s.instances)))
				}
			}
		}
	}

	// Build status line
	statusLine := fmt.Sprintf("%s | %s | %s%s%s%s%s%s%s",
		statusIndicator,
		dbName,
		timestamp,
		metricsSection,
		agentSection,
		instanceSection,
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
