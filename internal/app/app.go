package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/monitors"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views"
)

// Model represents the main Bubbletea application model
type Model struct {
	// Configuration
	config *config.Config

	// Database connection
	dbPool    *pgxpool.Pool
	connected bool
	connectionErr error
	serverVersion string

	// UI state
	width  int
	height int

	// Keyboard bindings
	keys ui.KeyMap

	// UI components
	help      *components.HelpText
	statusBar *components.StatusBar

	// Views
	currentView views.ViewType
	viewList    []views.ViewType
	dashboard   *views.DashboardView

	// Application state
	helpVisible bool
	quitting    bool
	ready       bool

	// Reconnection state
	reconnectionState *db.ReconnectionState
	reconnecting      bool

	// Status bar data
	statusTimestamp   time.Time
	activeConnections int

	// Monitors
	activityMonitor *monitors.ActivityMonitor
	statsMonitor    *monitors.StatsMonitor
}

// New creates a new application model
func New(readonly bool) (*Model, error) {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	statusBar := components.NewStatusBar()
	statusBar.SetDatabase(cfg.Connection.Database)
	statusBar.SetDateFormat(cfg.UI.DateFormat)

	// Initialize dashboard view
	dashboard := views.NewDashboard()
	dashboard.SetDatabase(cfg.Connection.Database)
	dashboard.SetReadOnly(readonly)

	// Define available views
	viewList := []views.ViewType{
		views.ViewDashboard,
		views.ViewActivity,
		views.ViewQueries,
		views.ViewLocks,
		views.ViewTables,
		views.ViewReplication,
	}

	return &Model{
		config:            cfg,
		keys:              ui.DefaultKeyMap(),
		help:              components.NewHelp(),
		statusBar:         statusBar,
		currentView:       views.ViewDashboard,
		viewList:          viewList,
		dashboard:         dashboard,
		connected:         false,
		reconnectionState: db.NewReconnectionState(5), // Max 5 attempts
		reconnecting:      false,
	}, nil
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		connectToDatabase(m.config),
		tickStatusBar(),
	)
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetSize(msg.Width, msg.Height)
		m.statusBar.SetSize(msg.Width)
		m.dashboard.SetSize(msg.Width, msg.Height-5) // Reserve space for header and status bar
		if !m.ready {
			m.ready = true
		}
		return m, nil

	case DatabaseConnectedMsg:
		m.connected = true
		m.dbPool = msg.Pool
		m.serverVersion = msg.Version
		m.connectionErr = nil
		m.statusBar.SetConnected(true)
		m.dashboard.SetConnected(true)
		m.dashboard.SetServerVersion(msg.Version)
		m.dashboard.SetConnectionInfo(fmt.Sprintf("steep - %s@%s:%d/%s",
			m.config.Connection.User,
			m.config.Connection.Host,
			m.config.Connection.Port,
			m.config.Connection.Database))

		// Initialize monitors
		refreshInterval := m.config.UI.RefreshInterval
		m.activityMonitor = monitors.NewActivityMonitor(msg.Pool, refreshInterval)
		m.statsMonitor = monitors.NewStatsMonitor(msg.Pool, refreshInterval)

		// Start fetching data
		return m, tea.Batch(
			fetchActivityData(m.activityMonitor),
			fetchStatsData(m.statsMonitor),
		)

	case ConnectionFailedMsg:
		m.connected = false
		m.connectionErr = msg.Err
		m.statusBar.SetConnected(false)
		// Trigger reconnection
		if !m.reconnecting {
			m.reconnecting = true
			return m, attemptReconnection(m.config, m.reconnectionState)
		}
		return m, nil

	case StatusBarTickMsg:
		m.statusTimestamp = msg.Timestamp
		m.statusBar.SetTimestamp(msg.Timestamp)
		// Query metrics if connected
		if m.connected && m.dbPool != nil {
			return m, tea.Batch(
				tickStatusBar(),
				queryMetrics(m.dbPool),
			)
		}
		return m, tickStatusBar()

	case MetricsUpdateMsg:
		m.activeConnections = msg.ActiveConnections
		m.statusBar.SetActiveConnections(msg.ActiveConnections)
		return m, nil

	case ui.ActivityDataMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		// Schedule next fetch after delay
		if m.activityMonitor != nil {
			return m, tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
				return activityTickMsg{}
			})
		}
		return m, nil

	case activityTickMsg:
		// Fetch activity data
		if m.activityMonitor != nil && m.connected {
			return m, fetchActivityData(m.activityMonitor)
		}
		return m, nil

	case ui.MetricsDataMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		// Update status bar active connections
		m.activeConnections = msg.Metrics.ConnectionCount
		m.statusBar.SetActiveConnections(msg.Metrics.ConnectionCount)
		// Schedule next fetch after delay
		if m.statsMonitor != nil {
			return m, tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
				return statsTickMsg{}
			})
		}
		return m, nil

	case statsTickMsg:
		// Fetch stats data
		if m.statsMonitor != nil && m.connected {
			return m, fetchStatsData(m.statsMonitor)
		}
		return m, nil

	case ErrorMsg:
		m.connectionErr = msg.Err
		return m, nil

	case ui.CancelQueryMsg:
		if m.dbPool != nil {
			return m, cancelQuery(m.dbPool, msg.PID)
		}
		return m, nil

	case ui.CancelQueryResultMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		return m, nil

	case ui.TerminateConnectionMsg:
		if m.dbPool != nil {
			return m, terminateConnection(m.dbPool, msg.PID)
		}
		return m, nil

	case ui.TerminateConnectionResultMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		return m, nil

	case ReconnectAttemptMsg:
		// Update reconnection status display
		m.statusBar.SetReconnecting(true, msg.Attempt, msg.MaxAttempts)
		// Continue reconnection attempts
		return m, tea.Tick(msg.NextDelay, func(t time.Time) tea.Msg {
			return attemptReconnection(m.config, m.reconnectionState)()
		})

	case ReconnectSuccessMsg:
		m.connected = true
		m.dbPool = msg.Pool
		m.serverVersion = msg.Version
		m.connectionErr = nil
		m.reconnecting = false
		m.statusBar.SetConnected(true)
		m.statusBar.SetReconnecting(false, 0, 0)
		m.dashboard.SetConnected(true)
		m.dashboard.SetServerVersion(msg.Version)
		return m, nil

	case ReconnectFailedMsg:
		m.reconnecting = false
		m.connectionErr = FormatReconnectionFailure(msg.Err, m.reconnectionState.MaxAttempts)
		m.statusBar.SetReconnecting(false, 0, 0)
		return m, nil
	}

	return m, nil
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check for quit
	if msg.String() == "q" || msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	// Check for help toggle
	if msg.String() == "?" {
		m.helpVisible = !m.helpVisible
		return m, nil
	}

	// Check for escape (close help)
	if msg.String() == "esc" && m.helpVisible {
		m.helpVisible = false
		return m, nil
	}

	// Check for view jumping (1-6)
	switch msg.String() {
	case "1":
		m.currentView = views.ViewDashboard
		return m, nil
	case "2":
		m.currentView = views.ViewActivity
		return m, nil
	case "3":
		m.currentView = views.ViewQueries
		return m, nil
	case "4":
		m.currentView = views.ViewLocks
		return m, nil
	case "5":
		m.currentView = views.ViewTables
		return m, nil
	case "6":
		m.currentView = views.ViewReplication
		return m, nil
	case "tab":
		m.nextView()
		return m, nil
	case "shift+tab":
		m.prevView()
		return m, nil
	}

	// Forward key events to current view
	if m.currentView == views.ViewDashboard && m.connected {
		var cmd tea.Cmd
		_, cmd = m.dashboard.Update(msg)
		return m, cmd
	}

	return m, nil
}

// nextView cycles to the next view
func (m *Model) nextView() {
	currentIndex := 0
	for i, v := range m.viewList {
		if v == m.currentView {
			currentIndex = i
			break
		}
	}
	nextIndex := (currentIndex + 1) % len(m.viewList)
	m.currentView = m.viewList[nextIndex]
}

// prevView cycles to the previous view
func (m *Model) prevView() {
	currentIndex := 0
	for i, v := range m.viewList {
		if v == m.currentView {
			currentIndex = i
			break
		}
	}
	prevIndex := (currentIndex - 1 + len(m.viewList)) % len(m.viewList)
	m.currentView = m.viewList[prevIndex]
}

// View renders the application UI
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	if !m.ready {
		return "Initializing..."
	}

	// Build the UI
	var view string

	// Header with current view indicator
	view += m.renderHeader()

	// Main content area - render current view
	if m.helpVisible {
		view += "\n" + m.renderHelp()
	} else {
		view += "\n" + m.renderCurrentView()
	}

	return view
}

// renderCurrentView renders the currently selected view
func (m Model) renderCurrentView() string {
	// If not connected and have error, show error
	if !m.connected && m.connectionErr != nil {
		return styles.ErrorStyle.Render(fmt.Sprintf("Connection Error: %s", m.connectionErr.Error()))
	}

	// If not connected and no error, show connecting message
	if !m.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Render the appropriate view based on currentView
	switch m.currentView {
	case views.ViewDashboard:
		return m.dashboard.View()
	case views.ViewActivity:
		return styles.InfoStyle.Render("Activity monitoring view - Coming soon!")
	case views.ViewQueries:
		return styles.InfoStyle.Render("Query performance view - Coming soon!")
	case views.ViewLocks:
		return styles.InfoStyle.Render("Lock monitoring view - Coming soon!")
	case views.ViewTables:
		return styles.InfoStyle.Render("Table statistics view - Coming soon!")
	case views.ViewReplication:
		return styles.InfoStyle.Render("Replication status view - Coming soon!")
	default:
		return styles.ErrorStyle.Render("Unknown view")
	}
}

// renderHeader renders the application header
func (m Model) renderHeader() string {
	return styles.HeaderStyle.Render("Steep - PostgreSQL Monitoring")
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	return m.statusBar.View()
}

// renderHelp renders the help screen
func (m Model) renderHelp() string {
	return m.help.View()
}

// Cleanup performs cleanup operations before the application exits
func (m *Model) Cleanup() {
	if m.dbPool != nil {
		m.dbPool.Close()
	}
}

// connectToDatabase creates a command to connect to the database
func connectToDatabase(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		pool, err := db.NewConnectionPool(ctx, cfg)
		if err != nil {
			return ConnectionFailedMsg{Err: err}
		}

		// Get server version
		version, err := db.GetServerVersion(ctx, pool)
		if err != nil {
			version = "Unknown"
		}

		return DatabaseConnectedMsg{
			Pool:    pool,
			Version: version,
		}
	}
}

// tickStatusBar creates a command to update the status bar timestamp
func tickStatusBar() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return StatusBarTickMsg{Timestamp: t}
	})
}

// queryMetrics creates a command to query database metrics
func queryMetrics(pool *pgxpool.Pool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Query active connections
		var activeConns int
		err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state = 'active'").Scan(&activeConns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("failed to query metrics: %w", err)}
		}

		return MetricsUpdateMsg{
			ActiveConnections: activeConns,
		}
	}
}

// attemptReconnection creates a command to attempt database reconnection
func attemptReconnection(cfg *config.Config, state *db.ReconnectionState) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Send attempt message
		attemptMsg := ReconnectAttemptMsg{
			Attempt:     state.Attempt + 1,
			MaxAttempts: state.MaxAttempts,
			NextDelay:   state.CalculateNextDelay(),
		}

		// Attempt reconnection
		pool, err := db.AttemptReconnection(ctx, cfg, state)
		if err != nil {
			// Check if we've exhausted all attempts
			if !state.HasAttemptsRemaining() {
				return ReconnectFailedMsg{
					Err: fmt.Errorf("all reconnection attempts failed: %w", err),
				}
			}
			// Return attempt message to trigger next attempt
			return attemptMsg
		}

		// Get server version
		version, err := db.GetServerVersion(ctx, pool)
		if err != nil {
			version = "Unknown"
		}

		return ReconnectSuccessMsg{
			Pool:    pool,
			Version: version,
		}
	}
}

// fetchActivityData creates a command to fetch activity data
func fetchActivityData(monitor *monitors.ActivityMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// fetchStatsData creates a command to fetch stats data
func fetchStatsData(monitor *monitors.StatsMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// cancelQuery creates a command to cancel a running query
func cancelQuery(pool *pgxpool.Pool, pid int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.CancelQuery(ctx, pool, pid)
		return ui.CancelQueryResultMsg{
			PID:     pid,
			Success: success,
			Error:   err,
		}
	}
}

// terminateConnection creates a command to terminate a connection
func terminateConnection(pool *pgxpool.Pool, pid int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.TerminateConnection(ctx, pool, pid)
		return ui.TerminateConnectionResultMsg{
			PID:     pid,
			Success: success,
			Error:   err,
		}
	}
}
