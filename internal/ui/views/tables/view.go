// Package tables provides the Tables view for schema/table statistics monitoring.
package tables

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// TablesMode represents the current interaction mode.
type TablesMode int

const (
	ModeNormal TablesMode = iota
	ModeDetails
	ModeConfirmInstall
	ModeConfirmVacuum
	ModeConfirmAnalyze
	ModeConfirmReindex
	ModeHelp
)

// FocusPanel indicates which panel has keyboard focus.
type FocusPanel int

const (
	FocusTables FocusPanel = iota
	FocusIndexes
)

// SortColumn represents available sort columns.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortBySize
	SortByRows
	SortByBloat
	SortByCacheHit
)

// String returns the display name for the sort column.
func (s SortColumn) String() string {
	switch s {
	case SortByName:
		return "Name"
	case SortBySize:
		return "Size"
	case SortByRows:
		return "Rows"
	case SortByBloat:
		return "Bloat"
	case SortByCacheHit:
		return "Cache"
	default:
		return "Unknown"
	}
}

// TreeItem represents an item in the tree view (schema, table, or partition).
type TreeItem struct {
	// Type info
	IsSchema    bool
	IsTable     bool
	IsPartition bool

	// Data references
	Schema *models.Schema
	Table  *models.Table

	// Display info
	Depth    int  // Indentation level
	IsLast   bool // Is this the last child?
	Expanded bool // Is this item expanded?

	// Parent info for partitions
	ParentOID uint32
}

// TablesView displays schema and table statistics.
type TablesView struct {
	width  int
	height int

	// Data
	schemas    []models.Schema
	tables     []models.Table
	indexes    []models.Index
	partitions map[uint32][]uint32 // parent OID -> child OIDs
	details    *models.TableDetails

	// Tree state
	treeItems    []TreeItem // Flattened tree for rendering
	selectedIdx  int
	scrollOffset int

	// UI state
	mode              TablesMode
	focusPanel        FocusPanel
	selectedIndex     int // Selected index in index list when FocusIndexes
	indexScrollOffset int
	sortColumn        SortColumn
	sortAscending     bool
	showSystemSchemas bool

	// Extension state
	pgstattupleAvailable bool
	pgstattupleChecked   bool
	installPromptShown   bool // Session-scoped: don't prompt again if declined

	// App state
	readonlyMode   bool
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	err            error

	// Toast
	toastMessage string
	toastError   bool
	toastTime    time.Time

	// Detail view state
	detailScrollOffset int
	detailLines        []string

	// Spinner for loading states
	spinner spinner.Model

	// Clipboard
	clipboard *ui.ClipboardWriter

	// Database connection
	pool *pgxpool.Pool
}

// NewTablesView creates a new TablesView instance.
func NewTablesView() *TablesView {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &TablesView{
		mode:              ModeNormal,
		focusPanel:        FocusTables,
		sortColumn:        SortByName,
		partitions:        make(map[uint32][]uint32),
		clipboard:         ui.NewClipboardWriter(),
		spinner:           s,
		showSystemSchemas: false, // Hidden by default per spec
	}
}

// Init initializes the tables view.
func (v *TablesView) Init() tea.Cmd {
	return v.spinner.Tick
}

// Update handles messages for the tables view.
func (v *TablesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Key handling will be implemented in subsequent tasks
		_ = msg

	case spinner.TickMsg:
		var cmd tea.Cmd
		v.spinner, cmd = v.spinner.Update(msg)
		return v, cmd

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return v, nil
}

// View renders the tables view.
func (v *TablesView) View() string {
	if !v.connected {
		return styles.InfoStyle.Render("Connecting to database...")
	}

	// Placeholder view - will be fully implemented in subsequent tasks
	return styles.InfoStyle.Render("Tables view - implementation in progress...")
}

// SetSize sets the dimensions of the view.
func (v *TablesView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *TablesView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *TablesView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *TablesView) SetReadOnly(readOnly bool) {
	v.readonlyMode = readOnly
}

// SetPool sets the database connection pool.
func (v *TablesView) SetPool(pool *pgxpool.Pool) {
	v.pool = pool
}

// IsInputMode returns true if in an input mode.
func (v *TablesView) IsInputMode() bool {
	return v.mode == ModeDetails
}

// showToast displays a toast message.
func (v *TablesView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}
