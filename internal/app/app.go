package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/ui/components/vimtea"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors"
	querymonitor "github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views"
	activityview "github.com/willibrandon/steep/internal/ui/views/activity"
	configview "github.com/willibrandon/steep/internal/ui/views/config"
	locksview "github.com/willibrandon/steep/internal/ui/views/locks"
	queriesview "github.com/willibrandon/steep/internal/ui/views/queries"
	replicationview "github.com/willibrandon/steep/internal/ui/views/replication"
	sqleditorview "github.com/willibrandon/steep/internal/ui/views/sqleditor"
	tablesview "github.com/willibrandon/steep/internal/ui/views/tables"
)

// Model represents the main Bubbletea application model
type Model struct {
	// Configuration
	config *config.Config

	// Program reference for sending messages from goroutines
	program *tea.Program

	// Database connection
	dbPool        *pgxpool.Pool
	connected     bool
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
	currentView     views.ViewType
	viewList        []views.ViewType
	dashboard       *views.DashboardView
	activityView    *activityview.ActivityView
	queriesView     *queriesview.QueriesView
	locksView       *locksview.LocksView
	tablesView      *tablesview.TablesView
	replicationView *replicationview.ReplicationView
	sqlEditorView   *sqleditorview.SQLEditorView
	configView      *configview.ConfigView

	// Application state
	helpVisible bool
	quitting    bool
	ready       bool
	tooSmall    bool

	// Reconnection state
	reconnectionState *db.ReconnectionState
	reconnecting      bool

	// Status bar data
	statusTimestamp   time.Time
	activeConnections int

	// Monitors
	activityMonitor    *monitors.ActivityMonitor
	statsMonitor       *monitors.StatsMonitor
	locksMonitor       *monitors.LocksMonitor
	deadlockMonitor    *monitors.DeadlockMonitor
	deadlockStore      *sqlite.DeadlockStore
	replicationMonitor *monitors.ReplicationMonitor
	replicationStore   *sqlite.ReplicationStore
	configMonitor      *monitors.ConfigMonitor

	// Query performance monitoring
	queryStatsDB    *sqlite.DB
	queryStatsStore *sqlite.QueryStatsStore
	queryMonitor    *querymonitor.Monitor
}

// New creates a new application model
func New(readonly bool, configPath string) (*Model, error) {
	// Load configuration
	cfg, err := config.LoadConfigFromPath(configPath)
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

	// Initialize activity view
	activityView := activityview.New()
	activityView.SetReadOnly(readonly)

	// Initialize queries view
	queriesView := queriesview.NewQueriesView()

	// Initialize locks view
	locksView := locksview.NewLocksView()
	locksView.SetReadOnly(readonly)

	// Initialize tables view
	tablesView := tablesview.NewTablesView()
	tablesView.SetReadOnly(readonly)

	// Initialize replication view
	replicationView := replicationview.NewReplicationView()
	replicationView.SetReadOnly(readonly)
	replicationView.SetDebug(cfg.Debug)

	// Initialize SQL Editor view
	sqlEditorView := sqleditorview.NewSQLEditorView(cfg.UI.SyntaxTheme)
	sqlEditorView.SetReadOnly(readonly)
	if cfg.UI.QueryTimeout > 0 {
		sqlEditorView.SetQueryTimeout(cfg.UI.QueryTimeout)
	}

	// Initialize Configuration view
	configView := configview.NewConfigView()

	// Define available views
	viewList := []views.ViewType{
		views.ViewDashboard,
		views.ViewActivity,
		views.ViewQueries,
		views.ViewLocks,
		views.ViewTables,
		views.ViewReplication,
		views.ViewSQLEditor,
		views.ViewConfig,
	}

	return &Model{
		config:            cfg,
		keys:              ui.DefaultKeyMap(),
		help:              components.NewHelp(),
		statusBar:         statusBar,
		currentView:       views.ViewDashboard,
		viewList:          viewList,
		dashboard:         dashboard,
		activityView:      activityView,
		queriesView:       queriesView,
		locksView:         locksView,
		tablesView:        tablesView,
		replicationView:   replicationView,
		sqlEditorView:     sqlEditorView,
		configView:        configView,
		connected:         false,
		reconnectionState: db.NewReconnectionState(5), // Max 5 attempts
		reconnecting:      false,
	}, nil
}

// SetProgram sets the tea.Program reference for sending messages from goroutines.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		connectToDatabase(m.config),
		tickStatusBar(),
		m.locksView.Init(),
		m.queriesView.Init(),
		m.tablesView.Init(),
		m.sqlEditorView.Init(),
	)
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	// Forward SQL Editor messages to the SQL Editor view
	case sqleditorview.QueryCompletedMsg, sqleditorview.QueryCancelledMsg:
		_, cmd := m.sqlEditorView.Update(msg)
		return m, cmd

	// Forward vimtea messages to the SQL Editor view
	case vimtea.CommandMsg, vimtea.EditorModeMsg, vimtea.UndoRedoMsg:
		_, cmd := m.sqlEditorView.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		// Forward mouse events to the active view
		switch m.currentView {
		case views.ViewQueries:
			m.queriesView.Update(msg)
		case views.ViewDashboard:
			m.dashboard.Update(msg)
		case views.ViewActivity:
			m.activityView.Update(msg)
		case views.ViewLocks:
			m.locksView.Update(msg)
		case views.ViewTables:
			m.tablesView.Update(msg)
		case views.ViewReplication:
			m.replicationView.Update(msg)
		case views.ViewSQLEditor:
			m.sqlEditorView.Update(msg)
		case views.ViewConfig:
			m.configView.Update(msg)
		}
		return m, nil

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
		m.activityView.SetSize(msg.Width, msg.Height-5)
		m.queriesView.SetSize(msg.Width, msg.Height-5)
		m.locksView.SetSize(msg.Width, msg.Height-5)
		m.tablesView.SetSize(msg.Width, msg.Height-5)
		m.replicationView.SetSize(msg.Width, msg.Height-5)
		m.sqlEditorView.SetSize(msg.Width, msg.Height-5)
		m.configView.SetSize(msg.Width, msg.Height-5)
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
		m.activityView.SetConnected(true)
		m.activityView.SetConnectionInfo(connectionInfo)
		m.queriesView.SetConnected(true)
		m.queriesView.SetConnectionInfo(connectionInfo)
		m.locksView.SetConnected(true)
		m.locksView.SetConnectionInfo(connectionInfo)
		m.tablesView.SetConnected(true)
		m.tablesView.SetConnectionInfo(connectionInfo)
		m.tablesView.SetPool(msg.Pool)
		m.replicationView.SetConnected(true)
		m.replicationView.SetConnectionInfo(connectionInfo)
		m.sqlEditorView.SetConnected(true)
		m.sqlEditorView.SetConnectionInfo(connectionInfo)
		m.sqlEditorView.SetPool(msg.Pool)
		m.configView.SetConnected(true)
		m.configView.SetConnectionInfo(connectionInfo)

		// Initialize monitors
		refreshInterval := m.config.UI.RefreshInterval
		m.activityMonitor = monitors.NewActivityMonitor(msg.Pool, refreshInterval)
		m.statsMonitor = monitors.NewStatsMonitor(msg.Pool, refreshInterval)
		m.locksMonitor = monitors.NewLocksMonitor(msg.Pool, 2*time.Second) // 2s refresh for locks
		m.configMonitor = monitors.NewConfigMonitor(msg.Pool, 60*time.Second) // 60s refresh for config

		// Initialize query stats storage
		storagePath := m.config.Queries.StoragePath
		if storagePath == "" {
			// Use default cache directory
			cacheDir, _ := os.UserCacheDir()
			storagePath = fmt.Sprintf("%s/steep/steep.db", cacheDir)
		}
		queryDB, err := sqlite.Open(storagePath)
		if err == nil {
			m.queryStatsDB = queryDB
			m.queryStatsStore = sqlite.NewQueryStatsStore(queryDB)

			// Initialize deadlock store (shares same DB)
			m.deadlockStore = sqlite.NewDeadlockStore(queryDB)

			// Initialize SQL Editor history (shares same DB)
			m.sqlEditorView.SetDatabase(queryDB)

			// Initialize replication store and monitor (shares same DB)
			m.replicationStore = sqlite.NewReplicationStore(queryDB)
			m.replicationMonitor = monitors.NewReplicationMonitor(msg.Pool, 2*time.Second, m.replicationStore)

			// Set retention from config (convert duration to hours)
			retentionHours := int(m.config.Replication.LagHistoryRetention.Hours())
			if retentionHours > 0 {
				m.replicationMonitor.SetRetentionHours(retentionHours)
			}

			// Initialize deadlock monitor
			ctx := context.Background()
			m.deadlockMonitor, _ = monitors.NewDeadlockMonitor(ctx, msg.Pool, m.deadlockStore, 30*time.Second)

			// Initialize query monitor
			monitorConfig := querymonitor.MonitorConfig{
				RefreshInterval: refreshInterval,
				RetentionDays:   m.config.Queries.RetentionDays,
			}
			m.queryMonitor = querymonitor.NewMonitor(msg.Pool, m.queryStatsStore, monitorConfig)
			_ = m.queryMonitor.Start(context.Background())

			// Check logging status and show dialog if disabled
			status, err := m.queryMonitor.CheckLoggingStatus(ctx)
			if err == nil && !status.Enabled {
				m.queriesView.SetLoggingDisabled()
			}
		}

		// Fallback: create replication monitor without persistence if DB failed
		if m.replicationMonitor == nil {
			m.replicationMonitor = monitors.NewReplicationMonitor(msg.Pool, 2*time.Second, nil)
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
				m.activityView.SetOwnPIDs(pids)
			}
		}()

		// Start fetching data with unified tick
		cmds := []tea.Cmd{
			fetchActivityData(m.activityMonitor),
			fetchStatsData(m.statsMonitor),
			fetchLocksData(m.locksMonitor),
			fetchReplicationData(m.replicationMonitor),
			fetchDeadlockHistory(m.deadlockMonitor, m.program),
			fetchConfigData(m.configMonitor),
			m.tablesView.FetchTablesData(),
			tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
				return dataTickMsg{}
			}),
		}
		// Also fetch query stats if store is available
		if m.queryStatsStore != nil {
			cmds = append(cmds, fetchQueryStats(m.queryStatsStore, m.queryMonitor, m.queriesView.GetSortColumn(), m.queriesView.GetSortAsc(), m.queriesView.GetFilter()))
		}
		return m, tea.Batch(cmds...)

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
		return m, tickStatusBar()

	case MetricsUpdateMsg:
		// Note: Status bar activeConnections is updated by ui.MetricsDataMsg
		// This message is no longer used for status bar updates
		return m, nil

	case ui.ActivityDataMsg:
		// Forward to activity view
		m.activityView.Update(msg)
		return m, nil

	case ui.MetricsDataMsg:
		// Forward to dashboard
		m.dashboard.Update(msg)
		// Update status bar active connections
		m.activeConnections = msg.Metrics.ConnectionCount
		m.statusBar.SetActiveConnections(msg.Metrics.ConnectionCount)
		return m, nil

	case dataTickMsg:
		// Always schedule the next tick to keep the refresh loop alive
		nextTick := tea.Tick(m.config.UI.RefreshInterval, func(t time.Time) tea.Msg {
			return dataTickMsg{}
		})
		// Fetch all data together for synchronized updates
		if m.connected && m.activityMonitor != nil && m.statsMonitor != nil {
			cmds := []tea.Cmd{
				fetchActivityData(m.activityMonitor),
				fetchStatsData(m.statsMonitor),
				nextTick,
			}
			// Fetch locks data if monitor is available
			if m.locksMonitor != nil {
				cmds = append(cmds, fetchLocksData(m.locksMonitor))
			}
			// Fetch replication data if monitor is available
			if m.replicationMonitor != nil {
				cmds = append(cmds, fetchReplicationData(m.replicationMonitor))
			}
			// Fetch deadlock history if monitor is available
			if m.deadlockMonitor != nil {
				cmds = append(cmds, fetchDeadlockHistory(m.deadlockMonitor, m.program))
			}
			// Also fetch query stats if store is available
			if m.queryStatsStore != nil {
				cmds = append(cmds, fetchQueryStats(m.queryStatsStore, m.queryMonitor, m.queriesView.GetSortColumn(), m.queriesView.GetSortAsc(), m.queriesView.GetFilter()))
			}
			return m, tea.Batch(cmds...)
		}
		return m, nextTick

	case ErrorMsg:
		m.connectionErr = msg.Err
		return m, nil

	case ui.CancelQueryMsg:
		if m.dbPool != nil {
			return m, cancelQuery(m.dbPool, msg.PID)
		}
		return m, nil

	case ui.CancelQueryResultMsg:
		// Forward to activity view
		m.activityView.Update(msg)
		return m, nil

	case ui.TerminateConnectionMsg:
		if m.dbPool != nil {
			return m, terminateConnection(m.dbPool, msg.PID)
		}
		return m, nil

	case ui.TerminateConnectionResultMsg:
		// Forward to activity view
		m.activityView.Update(msg)
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
			return m, fetchQueryStats(m.queryStatsStore, m.queryMonitor, msg.SortColumn, msg.SortAsc, msg.Filter)
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

	case queriesview.ResetQueryLogPositionsMsg:
		// Reset query log positions
		if m.queryStatsStore != nil {
			return m, resetQueryLogPositions(m.queryStatsStore, m.queryMonitor, m.program)
		}
		return m, nil

	case queriesview.ResetQueryLogPositionsResultMsg:
		// Forward to queries view
		m.queriesView.Update(msg)
		return m, nil

	case queriesview.QueryScanProgressMsg:
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

	case queriesview.ExplainQueryMsg:
		// Execute EXPLAIN for the query
		if m.queryMonitor != nil {
			return m, executeExplain(m.queryMonitor, msg.Query, msg.Fingerprint, msg.Analyze)
		}
		return m, nil

	case queriesview.ExplainResultMsg:
		// Forward to queries view
		m.queriesView.Update(msg)
		return m, nil

	case ui.LocksDataMsg:
		// Forward to locks view
		m.locksView.Update(msg)
		return m, nil

	case configview.ConfigDataMsg:
		// Forward to config view
		m.configView.Update(msg)
		return m, nil

	case configview.ExportConfigResultMsg:
		// Forward to config view for toast display
		m.configView.Update(msg)
		return m, nil

	case ui.ReplicationDataMsg:
		// Forward to replication view and execute any returned cmd
		_, cmd := m.replicationView.Update(msg)
		return m, cmd

	case ui.DropSlotRequestMsg:
		// Execute the drop slot query
		return m, m.dropReplicationSlot(msg.SlotName)

	case ui.DropSlotResultMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.WizardExecRequestMsg:
		// Execute wizard SQL command and return result
		return m, m.executeWizardCommand(msg.Command, msg.Label)

	case ui.WizardExecResultMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.AlterSystemRequestMsg:
		// Execute ALTER SYSTEM commands
		return m, m.executeAlterSystemCommands(msg.Commands)

	case ui.AlterSystemResultMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.LagHistoryRequestMsg:
		// Fetch lag history from SQLite for the requested time window
		return m, m.fetchLagHistory(msg.Window)

	case ui.LagHistoryResponseMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.TablesRequestMsg:
		// Fetch tables for logical wizard
		return m, m.fetchTablesForWizard()

	case ui.TablesResponseMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.ConnTestRequestMsg:
		// Test connection for connection string builder
		return m, m.testConnection(msg.ConnString)

	case ui.ConnTestResponseMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.CreateReplicationUserMsg:
		// Create replication user
		return m, m.createReplicationUser(msg.Username, msg.Password)

	case ui.CreateReplicationUserResultMsg:
		// Forward to replication view
		m.replicationView.Update(msg)
		return m, nil

	case ui.DeadlockScanProgressMsg:
		// Forward progress to locks view
		m.locksView.Update(msg)
		return m, nil

	case ui.DeadlockHistoryMsg:
		// Forward to locks view
		m.locksView.Update(msg)
		return m, nil

	case locksview.RefreshLocksMsg:
		// Fetch locks data
		if m.locksMonitor != nil {
			return m, fetchLocksData(m.locksMonitor)
		}
		return m, nil

	case locksview.KillLockMsg:
		// Kill blocking process
		if m.dbPool != nil {
			return m, killLockingProcess(m.dbPool, msg.PID)
		}
		return m, nil

	case ui.KillQueryResultMsg:
		// Forward to locks view
		m.locksView.Update(msg)
		// Auto-refresh locks data after kill
		if m.locksMonitor != nil {
			return m, fetchLocksData(m.locksMonitor)
		}
		return m, nil

	case locksview.FetchDeadlockDetailMsg:
		// Fetch full deadlock event details
		return m, fetchDeadlockDetail(m.deadlockStore, msg.EventID)

	case ui.DeadlockDetailMsg:
		// Forward to locks view
		m.locksView.Update(msg)
		return m, nil

	case locksview.EnableLoggingCollectorMsg:
		// Enable logging_collector and restart PostgreSQL
		if m.dbPool != nil {
			return m, enableLoggingCollector(m.dbPool)
		}
		return m, nil

	case locksview.EnableLoggingCollectorResultMsg:
		// Forward to locks view
		m.locksView.Update(msg)
		// PostgreSQL was restarted - need to reconnect
		if msg.Success {
			m.connected = false
			m.reconnecting = true
			m.reconnectionState = db.NewReconnectionState(5)
			m.statusBar.SetConnected(false)
			// Give PostgreSQL a moment to start, then reconnect
			return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return attemptReconnection(m.config, m.reconnectionState)()
			})
		}
		return m, nil

	case ui.ResetDeadlocksMsg:
		// Reset deadlock history
		logger.Info("ResetDeadlocksMsg: received")
		if m.deadlockStore != nil {
			return m, resetDeadlockHistory(m.deadlockStore)
		}
		return m, nil

	case ui.ResetDeadlocksResultMsg:
		// Forward result to locks view
		logger.Info("ResetDeadlocksResultMsg: received", "success", msg.Success, "error", msg.Error)
		m.locksView.Update(msg)
		logger.Info("ResetDeadlocksResultMsg: forwarded to locks view")
		return m, nil

	case ui.ResetLogPositionsMsg:
		// Reset log positions
		logger.Info("ResetLogPositionsMsg: received")
		if m.deadlockStore != nil {
			return m, resetLogPositions(m.deadlockStore, m.deadlockMonitor)
		}
		return m, nil

	case ui.ResetLogPositionsResultMsg:
		// Forward result to locks view
		logger.Info("ResetLogPositionsResultMsg: received", "success", msg.Success, "error", msg.Error)
		m.locksView.Update(msg)
		// Trigger re-parse after successful reset
		if msg.Success && m.deadlockMonitor != nil {
			return m, fetchDeadlockHistory(m.deadlockMonitor, m.program)
		}
		return m, nil

	case tablesview.TablesDataMsg:
		// Forward to tables view and return its command (schedules next refresh)
		_, cmd := m.tablesView.Update(msg)
		return m, cmd

	case tablesview.RefreshTablesMsg:
		// Refresh tables data
		return m, m.tablesView.FetchTablesData()

	case tablesview.InstallExtensionMsg:
		// Forward to tables view
		_, cmd := m.tablesView.Update(msg)
		return m, cmd

	case tablesview.TableDetailsMsg:
		// Forward to tables view
		_, cmd := m.tablesView.Update(msg)
		return m, cmd

	case tablesview.MaintenanceResultMsg:
		// Forward to tables view
		_, cmd := m.tablesView.Update(msg)
		return m, cmd

	case spinner.TickMsg:
		// Forward spinner ticks to locks, queries, and tables views
		_, locksCmd := m.locksView.Update(msg)
		_, queriesCmd := m.queriesView.Update(msg)
		_, tablesCmd := m.tablesView.Update(msg)
		return m, tea.Batch(locksCmd, queriesCmd, tablesCmd)

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

	// Check if the CURRENT view is in input mode (only check active view)
	inInputMode := m.currentViewIsInputMode()

	// Check for quit (but not when view is in input mode)
	if msg.String() == "q" || msg.String() == "ctrl+c" {
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

	// Check for escape (close help) - but only if not in input mode
	if msg.String() == "esc" && m.helpVisible && !inInputMode {
		m.helpVisible = false
		return m, nil
	}

	// Check for view jumping (1-8) - but not when in input mode (editing fields)
	if !inInputMode {
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
		case "7":
			m.currentView = views.ViewSQLEditor
			return m, nil
		case "8":
			m.currentView = views.ViewConfig
			return m, nil
		case "tab":
			m.nextView()
			return m, nil
		case "shift+tab":
			m.prevView()
			return m, nil
		}
	}

	// Forward key events to current view
	switch m.currentView {
	case views.ViewDashboard:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.dashboard.Update(msg)
			return m, cmd
		}
	case views.ViewActivity:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.activityView.Update(msg)
			return m, cmd
		}
	case views.ViewQueries:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.queriesView.Update(msg)
			return m, cmd
		}
	case views.ViewLocks:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.locksView.Update(msg)
			return m, cmd
		}
	case views.ViewTables:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.tablesView.Update(msg)
			return m, cmd
		}
	case views.ViewReplication:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.replicationView.Update(msg)
			return m, cmd
		}
	case views.ViewSQLEditor:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.sqlEditorView.Update(msg)
			return m, cmd
		}
	case views.ViewConfig:
		if m.connected {
			var cmd tea.Cmd
			_, cmd = m.configView.Update(msg)
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

// currentViewIsInputMode returns true if the current view is in input mode.
// Only checks the active view, not all views.
func (m *Model) currentViewIsInputMode() bool {
	switch m.currentView {
	case views.ViewDashboard:
		return m.dashboard.IsInputMode()
	case views.ViewActivity:
		return m.activityView.IsInputMode()
	case views.ViewQueries:
		return m.queriesView.IsInputMode()
	case views.ViewLocks:
		return m.locksView.IsInputMode()
	case views.ViewTables:
		return m.tablesView.IsInputMode()
	case views.ViewReplication:
		return m.replicationView.IsInputMode()
	case views.ViewSQLEditor:
		return m.sqlEditorView.IsInputMode()
	case views.ViewConfig:
		return m.configView.IsInputMode()
	default:
		return false
	}
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

	// Status bar at bottom
	view += "\n" + m.renderStatusBar()

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
		return m.activityView.View()
	case views.ViewQueries:
		return m.queriesView.View()
	case views.ViewLocks:
		return m.locksView.View()
	case views.ViewTables:
		return m.tablesView.View()
	case views.ViewReplication:
		return m.replicationView.View()
	case views.ViewSQLEditor:
		return m.sqlEditorView.View()
	case views.ViewConfig:
		return m.configView.View()
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

// executeWizardCommand executes a SQL command from the wizard
func (m Model) executeWizardCommand(command, label string) tea.Cmd {
	return func() tea.Msg {
		if m.dbPool == nil {
			return ui.WizardExecResultMsg{
				Command: command,
				Label:   label,
				Success: false,
				Error:   fmt.Errorf("no database connection"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := m.dbPool.Exec(ctx, command)
		if err != nil {
			return ui.WizardExecResultMsg{
				Command: command,
				Label:   label,
				Success: false,
				Error:   err,
			}
		}

		return ui.WizardExecResultMsg{
			Command: command,
			Label:   label,
			Success: true,
			Error:   nil,
		}
	}
}

// executeAlterSystemCommands executes a series of ALTER SYSTEM commands
func (m Model) executeAlterSystemCommands(commands []string) tea.Cmd {
	return func() tea.Msg {
		if m.dbPool == nil {
			return ui.AlterSystemResultMsg{
				Commands: commands,
				Success:  false,
				Error:    fmt.Errorf("no database connection"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Execute each command in sequence
		for _, cmd := range commands {
			_, err := m.dbPool.Exec(ctx, cmd)
			if err != nil {
				return ui.AlterSystemResultMsg{
					Commands: commands,
					Success:  false,
					Error:    fmt.Errorf("failed on '%s': %w", cmd, err),
				}
			}
		}

		return ui.AlterSystemResultMsg{
			Commands: commands,
			Success:  true,
			Error:    nil,
		}
	}
}

// fetchLagHistory fetches lag history from SQLite for the given time window
func (m Model) fetchLagHistory(window time.Duration) tea.Cmd {
	return func() tea.Msg {
		if m.replicationStore == nil {
			return ui.LagHistoryResponseMsg{
				LagHistory: nil,
				Window:     window,
				Error:      fmt.Errorf("replication store not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		since := time.Now().Add(-window)
		entries, err := m.replicationStore.GetLagHistoryForAllReplicas(ctx, since)
		if err != nil {
			return ui.LagHistoryResponseMsg{
				LagHistory: nil,
				Window:     window,
				Error:      err,
			}
		}

		// Convert entries to float64 slices for sparklines
		lagHistory := make(map[string][]float64)
		for name, replicaEntries := range entries {
			values := make([]float64, len(replicaEntries))
			for i, e := range replicaEntries {
				values[i] = float64(e.ByteLag)
			}
			lagHistory[name] = values
		}

		return ui.LagHistoryResponseMsg{
			LagHistory: lagHistory,
			Window:     window,
			Error:      nil,
		}
	}
}

// fetchTablesForWizard fetches tables for the logical wizard
func (m Model) fetchTablesForWizard() tea.Cmd {
	return func() tea.Msg {
		if m.dbPool == nil {
			return ui.TablesResponseMsg{
				Tables: nil,
				Error:  fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		tables, err := queries.GetTablesWithStats(ctx, m.dbPool)
		if err != nil {
			return ui.TablesResponseMsg{
				Tables: nil,
				Error:  err,
			}
		}

		return ui.TablesResponseMsg{
			Tables: tables,
			Error:  nil,
		}
	}
}

// testConnection tests a connection string for the connection string builder
func (m Model) testConnection(connString string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Try to connect using the provided connection string
		conn, err := pgx.Connect(ctx, connString)
		if err != nil {
			return ui.ConnTestResponseMsg{
				Success: false,
				Message: "",
				Error:   err,
			}
		}
		defer conn.Close(ctx)

		// Test the connection with a simple query
		var version string
		err = conn.QueryRow(ctx, "SELECT version()").Scan(&version)
		if err != nil {
			return ui.ConnTestResponseMsg{
				Success: false,
				Message: "",
				Error:   fmt.Errorf("connection succeeded but query failed: %w", err),
			}
		}

		// Extract short version info
		shortVersion := version
		if idx := strings.Index(version, " on "); idx > 0 {
			shortVersion = version[:idx]
		}

		return ui.ConnTestResponseMsg{
			Success: true,
			Message: "Connected to " + shortVersion,
			Error:   nil,
		}
	}
}

// createReplicationUser creates a new replication user in the database
func (m Model) createReplicationUser(username, password string) tea.Cmd {
	return func() tea.Msg {
		if m.dbPool == nil {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Check if user is superuser
		isSuperuser, err := queries.IsSuperuser(ctx, m.dbPool)
		if err != nil {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    fmt.Errorf("failed to check privileges: %w", err),
			}
		}

		if !isSuperuser {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    fmt.Errorf("superuser privileges required to create users"),
			}
		}

		// Check if user already exists
		exists, err := queries.ReplicationUserExists(ctx, m.dbPool, username)
		if err != nil {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    fmt.Errorf("failed to check if user exists: %w", err),
			}
		}

		if exists {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    fmt.Errorf("user '%s' already exists", username),
			}
		}

		// Create the user
		err = queries.CreateReplicationUser(ctx, m.dbPool, username, password)
		if err != nil {
			return ui.CreateReplicationUserResultMsg{
				Success:  false,
				Username: username,
				Error:    err,
			}
		}

		return ui.CreateReplicationUserResultMsg{
			Success:  true,
			Username: username,
			Error:    nil,
		}
	}
}

// dropReplicationSlot drops a replication slot from the database
func (m Model) dropReplicationSlot(slotName string) tea.Cmd {
	return func() tea.Msg {
		if m.dbPool == nil {
			return ui.DropSlotResultMsg{
				SlotName: slotName,
				Success:  false,
				Error:    fmt.Errorf("database connection not available"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := queries.DropReplicationSlot(ctx, m.dbPool, slotName)
		if err != nil {
			return ui.DropSlotResultMsg{
				SlotName: slotName,
				Success:  false,
				Error:    err,
			}
		}

		return ui.DropSlotResultMsg{
			SlotName: slotName,
			Success:  true,
			Error:    nil,
		}
	}
}
