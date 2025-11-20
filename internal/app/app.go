package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/components"
	"github.com/willibrandon/steep/internal/ui/styles"
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
	help *components.HelpText

	// Application state
	helpVisible bool
	quitting    bool
	ready       bool

	// Status bar data
	statusTimestamp      time.Time
	activeConnections    int
}

// New creates a new application model
func New() (*Model, error) {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return &Model{
		config:    cfg,
		keys:      ui.DefaultKeyMap(),
		help:      components.NewHelp(),
		connected: false,
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
		if !m.ready {
			m.ready = true
		}
		return m, nil

	case DatabaseConnectedMsg:
		m.connected = true
		m.dbPool = msg.Pool
		m.serverVersion = msg.Version
		m.connectionErr = nil
		return m, nil

	case ConnectionFailedMsg:
		m.connected = false
		m.connectionErr = msg.Err
		return m, nil

	case StatusBarTickMsg:
		m.statusTimestamp = msg.Timestamp
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
		return m, nil

	case ErrorMsg:
		m.connectionErr = msg.Err
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
	if msg.String() == "h" || msg.String() == "?" {
		m.helpVisible = !m.helpVisible
		return m, nil
	}

	// Check for escape (close help)
	if msg.String() == "esc" {
		m.helpVisible = false
		return m, nil
	}

	return m, nil
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

	// Header
	view += m.renderHeader()

	// Main content area
	if m.connected {
		view += "\nConnected to PostgreSQL\n"
		view += fmt.Sprintf("Version: %s\n", m.serverVersion)
		view += fmt.Sprintf("Active connections: %d\n", m.activeConnections)
	} else {
		view += "\n"
		if m.connectionErr != nil {
			view += styles.ErrorStyle.Render(fmt.Sprintf("Connection Error: %s", m.connectionErr.Error()))
		} else {
			view += "Connecting to database..."
		}
		view += "\n"
	}

	// Help overlay
	if m.helpVisible {
		view += "\n" + m.renderHelp()
	}

	// Status bar
	view += "\n" + m.renderStatusBar()

	return view
}

// renderHeader renders the application header
func (m Model) renderHeader() string {
	return styles.HeaderStyle.Render("Steep - PostgreSQL Monitoring")
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	var status string
	if m.connected {
		status = styles.StatusConnectedStyle.Render("● Connected")
	} else {
		status = styles.StatusDisconnectedStyle.Render("● Disconnected")
	}

	dbName := m.config.Connection.Database
	timestamp := m.statusTimestamp.Format(m.config.UI.DateFormat)

	statusLine := fmt.Sprintf("%s | %s | %s", status, dbName, timestamp)
	return styles.StatusBarStyle.Render(statusLine)
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
