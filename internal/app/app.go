package app

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/monitors"
	querymonitor "github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views"
	queriesview "github.com/willibrandon/steep/internal/ui/views/queries"
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
	queriesView *queriesview.QueriesView

	// Application state
	helpVisible  bool
	quitting     bool
	ready        bool
	tooSmall     bool

	// Reconnection state
	reconnectionState *db.ReconnectionState
	reconnecting      bool

	// Status bar data
	statusTimestamp   time.Time
	activeConnections int

	// Monitors
	activityMonitor *monitors.ActivityMonitor
	statsMonitor    *monitors.StatsMonitor

	// Query performance monitoring
	queryStatsDB    *sqlite.DB
	queryStatsStore *sqlite.QueryStatsStore
	queryMonitor    *querymonitor.Monitor
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

	// Initialize queries view
	queriesView := queriesview.NewQueriesView()

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
		queriesView:       queriesView,
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
		m.tooSmall = msg.Width < 80 || msg.Height < 24
		if !m.ready {
			m.ready = true
		}
		// Skip component sizing if terminal is too small
		if m.tooSmall {
			return m, nil
		}
		m.help.SetSize(msg.Width, msg.Height)
		m.statusBar.SetSize(msg.Width)
		m.dashboard.SetSize(msg.Width, msg.Height-5) // Reserve space for header and status bar
		m.queriesView.SetSize(msg.Width, msg.Height-5)
		return m, nil

	case DatabaseConnectedMsg:
		m.connected = true
		m.dbPool = msg.Pool
		m.serverVersion = msg.Version
		m.connectionErr = nil
		m.statusBar.SetConnected(true)
		m.dashboard.SetConnected(true)
		m.dashboard.SetServerVersion(msg.Version)
		connectionInfo := fmt.Sprintf("steep - %s@%s:%d/%s",
			m.config.Connection.User,
			m.config.Connection.Host,
			m.config.Connection.Port,
			m.config.Connection.Database)
		m.dashboard.SetConnectionInfo(connectionInfo)
		m.queriesView.SetConnected(true)
		m.queriesView.SetConnectionInfo(connectionInfo)

		// Initialize monitors
		refreshInterval := m.config.UI.RefreshInterval
		m.activityMonitor = monitors.NewActivityMonitor(msg.Pool, refreshInterval)
		m.statsMonitor = monitors.NewStatsMonitor(msg.Pool, refreshInterval)

		// Initialize query stats storage
		storagePath := m.config.Queries.StoragePath
		if storagePath == "" {
			// Use default cache directory
			cacheDir, _ := os.UserCacheDir()
			storagePath = fmt.Sprintf("%s/steep/query_stats.db", cacheDir)
		}
		queryDB, err := sqlite.Open(storagePath)
		if err == nil {
			m.queryStatsDB = queryDB
			m.queryStatsStore = sqlite.NewQueryStatsStore(queryDB)

			// Initialize query monitor
			monitorConfig := querymonitor.MonitorConfig{
				RefreshInterval: refreshInterval,
				RetentionDays:   m.config.Queries.RetentionDays,
				LogPath:         m.config.Queries.LogPath,
				LogLinePrefix:   m.config.Queries.LogLinePrefix,
			}
			m.queryMonitor = querymonitor.NewMonitor(msg.Pool, m.queryStatsStore, monitorConfig)
			_ = m.queryMonitor.Start(context.Background())

			// Check logging status and show dialog if disabled
			ctx := context.Background()
			status, err := m.queryMonitor.CheckLoggingStatus(ctx)
			if err == nil && !status.Enabled {
				m.queriesView.SetLoggingDisabled()
			}
		}

		// Get our own PIDs for self-kill warning
		go func() {
			ctx := context.Background()
			rows, err := msg.Pool.Query(ctx, "SELECT pid FROM pg_stat_activity WHERE application_name = 'steep'")
			if err != nil {
				return
			}
			defer rows.Close()
			var pids []int
			for rows.Next() {
				var pid int
				if rows.Scan(&pid) == nil {
					pids = append(pids, pid)
				}
			}
			if len(pids) > 0 {
				m.dashboard.SetOwnPIDs(pids)
			}
		}()

		// Start fetching data with unified tick
		return m, tea.Batch(
			fetchActivityData(m.activityMonitor),
			fetchStatsData(m.statsMonitor),
			tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
				return dataTickMsg{}
			}),
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
		return m, nil

	case ui.MetricsDataMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		// Update status bar active connections
		m.activeConnections = msg.Metrics.ConnectionCount
		m.statusBar.SetActiveConnections(msg.Metrics.ConnectionCount)
		return m, nil

	case dataTickMsg:
		// Fetch all data together for synchronized updates
		if m.connected && m.activityMonitor != nil && m.statsMonitor != nil {
			cmds := []tea.Cmd{
				fetchActivityData(m.activityMonitor),
				fetchStatsData(m.statsMonitor),
				tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
					return dataTickMsg{}
				}),
			}
			// Also fetch query stats if store is available
			if m.queryStatsStore != nil {
				cmds = append(cmds, fetchQueryStats(m.queryStatsStore, m.queriesView.GetSortColumn(), m.queriesView.GetFilter()))
			}
			return m, tea.Batch(cmds...)
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

	case ui.FilterChangedMsg:
		// Update monitor filter and fetch fresh data
		if m.activityMonitor != nil {
			m.activityMonitor.SetFilter(msg.Filter)
		}
		// Fetch fresh data with new filter
		if m.dbPool != nil {
			return m, func() tea.Msg {
				ctx := context.Background()
				return m.activityMonitor.FetchOnce(ctx)
			}
		}
		return m, nil

	case ui.RefreshRequestMsg:
		// Manual refresh requested - only refresh activity data
		// Stats continue on their regular interval to avoid TPS calculation issues
		if m.dbPool != nil && m.activityMonitor != nil {
			return m, fetchActivityData(m.activityMonitor)
		} else if !m.connected && !m.reconnecting {
			// Not connected, trigger reconnection
			m.reconnecting = true
			return m, attemptReconnection(m.config, m.reconnectionState)
		}
		return m, nil

	case queriesview.RefreshQueriesMsg:
		// Fetch query stats from SQLite store
		if m.queryStatsStore != nil {
			return m, fetchQueryStats(m.queryStatsStore, msg.SortColumn, msg.Filter)
		}
		return m, nil

	case queriesview.QueriesDataMsg:
		// Forward to queries view
		m.queriesView.Update(msg)
		return m, nil

	case queriesview.ResetQueryStatsMsg:
		// Reset query statistics
		if m.queryStatsStore != nil {
			return m, resetQueryStats(m.queryStatsStore)
		}
		return m, nil

	case queriesview.ResetQueryStatsResultMsg:
		// Forward to queries view
		m.queriesView.Update(msg)
		return m, nil

	case queriesview.CheckLoggingStatusMsg:
		// Check logging status
		if m.queryMonitor != nil {
			return m, checkLoggingStatus(m.queryMonitor)
		}
		return m, nil

	case queriesview.LoggingStatusMsg:
		// Forward to queries view
		m.queriesView.Update(msg)
		return m, nil

	case queriesview.EnableLoggingMsg:
		// Enable query logging
		if m.queryMonitor != nil {
			return m, enableLogging(m.queryMonitor)
		}
		return m, nil

	case queriesview.EnableLoggingResultMsg:
		// Forward to queries view and restart monitor
		m.queriesView.Update(msg)
		if msg.Success && m.queryMonitor != nil {
			// Restart monitor to use log collector
			m.queryMonitor.Stop()
			_ = m.queryMonitor.Start(context.Background())
		}
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
	// When terminal is too small, only allow quit
	if m.tooSmall {
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	// Check for quit (but not when view is in input mode)
	if msg.String() == "q" || msg.String() == "ctrl+c" {
		inInputMode := m.dashboard.IsInputMode() || m.queriesView.IsInputMode()
		if !inInputMode {
			m.quitting = true
			return m, tea.Quit
		}
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
	switch m.currentView {
	case views.ViewDashboard:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.dashboard.Update(msg)
			return m, cmd
		}
	case views.ViewQueries:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.queriesView.Update(msg)
			return m, cmd
		}
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

	// Check minimum terminal size
	if m.tooSmall {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styles.ErrorStyle.Render(fmt.Sprintf(
				"Terminal too small (%dx%d)\nMinimum size: 80x24\nPlease resize your terminal.",
				m.width, m.height,
			)),
		)
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
		return m.queriesView.View()
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
	if m.queryMonitor != nil {
		m.queryMonitor.Stop()
	}
	if m.queryStatsDB != nil {
		m.queryStatsDB.Close()
	}
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
		err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE state = 'active'").Scan(&activeConns)
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

// resetQueryStats creates a command to reset query statistics
func resetQueryStats(store *sqlite.QueryStatsStore) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := store.Reset(ctx)
		return queriesview.ResetQueryStatsResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// checkLoggingStatus creates a command to check PostgreSQL logging status
func checkLoggingStatus(monitor *querymonitor.Monitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		status, err := monitor.CheckLoggingStatus(ctx)
		if err != nil {
			return queriesview.LoggingStatusMsg{
				Error: err,
			}
		}
		return queriesview.LoggingStatusMsg{
			Enabled: status.Enabled,
			LogPath: status.LogPath,
		}
	}
}

// enableLogging creates a command to enable query logging
func enableLogging(monitor *querymonitor.Monitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := monitor.EnableLogging(ctx)
		return queriesview.EnableLoggingResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// fetchQueryStats creates a command to fetch query statistics
func fetchQueryStats(store *sqlite.QueryStatsStore, sortCol queriesview.SortColumn, filter string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Map view sort column to store sort field
		var storeSort sqlite.SortField
		switch sortCol {
		case queriesview.SortByTotalTime:
			storeSort = sqlite.SortByTotalTime
		case queriesview.SortByCalls:
			storeSort = sqlite.SortByCalls
		case queriesview.SortByMeanTime:
			storeSort = sqlite.SortByMeanTime
		case queriesview.SortByRows:
			storeSort = sqlite.SortByRows
		default:
			storeSort = sqlite.SortByTotalTime
		}

		var stats []sqlite.QueryStats
		var err error

		if filter != "" {
			stats, err = store.SearchQueries(ctx, filter, storeSort, 100)
		} else {
			stats, err = store.GetTopQueries(ctx, storeSort, 100)
		}

		return queriesview.QueriesDataMsg{
			Stats:     stats,
			FetchedAt: time.Now(),
			Error:     err,
		}
	}
}
