// Package tables provides the Tables view for schema/table statistics monitoring.
package tables

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/ui"
)

// TablesMode represents the current interaction mode.
type TablesMode int

const (
	ModeNormal TablesMode = iota
	ModeDetails
	ModeCopyMenu
	ModeConfirmInstall
	ModeConfirmVacuum
	ModeConfirmAnalyze
	ModeConfirmReindex
	ModeConfirmReindexConcurrently
	ModeHelp
	// New modes for operations menu
	ModeOperationsMenu    // Show operation selection menu
	ModeOperationProgress // Show progress for running operation
	ModeConfirmCancel     // Confirm operation cancellation
	ModePermissions       // Show permissions dialog
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

// IndexSortColumn represents the column to sort indexes by.
type IndexSortColumn int

const (
	IndexSortByName IndexSortColumn = iota
	IndexSortBySize
	IndexSortByScans
	IndexSortByRowsRead
	IndexSortByCacheHit
)

// String returns the display name for the index sort column.
func (s IndexSortColumn) String() string {
	switch s {
	case IndexSortByName:
		return "Name"
	case IndexSortBySize:
		return "Size"
	case IndexSortByScans:
		return "Scans"
	case IndexSortByRowsRead:
		return "Rows"
	case IndexSortByCacheHit:
		return "Cache"
	default:
		return "Unknown"
	}
}

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

// Message types for the tables view
type (
	// TablesDataMsg contains refreshed table/index data.
	TablesDataMsg struct {
		Schemas              []models.Schema
		Tables               []models.Table
		Indexes              []models.Index
		Partitions           map[uint32][]uint32
		Bloat                map[uint32]float64 // OID -> accurate bloat % (nil if pgstattuple unavailable)
		PgstattupleAvailable bool
		Error                error
	}

	// RefreshTablesMsg triggers data refresh.
	RefreshTablesMsg struct{}

	// InstallExtensionMsg contains the result of extension installation.
	InstallExtensionMsg struct {
		Success bool
		Error   error
	}

	// TableDetailsMsg contains column and constraint data for a table.
	TableDetailsMsg struct {
		TableOID    uint32
		Columns     []models.TableColumn
		Constraints []models.Constraint
		Error       error
	}

	// MaintenanceResultMsg contains the result of a maintenance operation.
	MaintenanceResultMsg struct {
		Operation string        // "VACUUM", "ANALYZE", "REINDEX"
		TableName string        // schema.table
		Success   bool
		Error     error
		Elapsed   time.Duration // How long the operation took
	}

	// OperationProgressMsg contains progress update for a running operation.
	OperationProgressMsg struct {
		Progress *models.OperationProgress
	}

	// OperationStartedMsg signals that an operation has started.
	OperationStartedMsg struct {
		Operation *models.MaintenanceOperation
	}

	// ProgressTickMsg triggers a progress poll.
	ProgressTickMsg struct{}

	// CancelOperationMsg signals that the user wants to cancel an operation.
	CancelOperationMsg struct {
		PID int // PID of the backend to cancel
	}

	// OperationCancelledMsg contains the result of a cancellation attempt.
	OperationCancelledMsg struct {
		PID       int
		Cancelled bool  // True if pg_cancel_backend returned true
		Error     error // Non-nil if the cancel call itself failed
	}
)

// TablesView displays schema and table statistics.
type TablesView struct {
	width            int
	height           int
	viewHeaderHeight int // Calculated height of view header elements for mouse coordinate translation

	// Data
	schemas    []models.Schema
	tables     []models.Table
	indexes    []models.Index
	partitions map[uint32][]uint32 // parent OID -> child OIDs
	details    *models.TableDetails

	// Table lookup by OID for quick access
	tablesByOID map[uint32]*models.Table

	// Tree state
	treeItems    []TreeItem // Flattened tree for rendering
	selectedIdx  int
	scrollOffset int

	// UI state
	mode                 TablesMode
	focusPanel           FocusPanel
	selectedIndex        int // Selected index in index list when FocusIndexes
	indexScrollOffset    int
	detailsScrollOffset  int      // Vertical scroll offset for details panel
	detailsHScrollOffset int      // Horizontal scroll offset for details panel
	detailsLines         []string // Pre-computed details content lines
	detailsMaxLineWidth  int      // Max line width in details (for horizontal scroll)
	sortColumn           SortColumn
	sortAscending        bool
	indexSortColumn      IndexSortColumn
	indexSortAscending   bool
	showSystemSchemas    bool
	splitRatio           float64 // 0.0-1.0, portion for tables panel (vs index panel)

	// Extension state
	pgstattupleAvailable bool
	pgstattupleChecked   bool
	installPromptShown   bool // Session-scoped: don't prompt again if declined

	// Maintenance target
	maintenanceTarget *models.Table // Table selected for maintenance operation

	// Operations menu state
	operationsMenu       *OperationsMenu              // Active operations menu (nil if not showing)
	currentOperation     *models.MaintenanceOperation // Currently running operation (nil if none)
	operationHistory     *models.OperationHistory     // Session-scoped operation history
	pendingVacuumFull    bool                         // If true, execute VACUUM FULL instead of VACUUM
	pendingVacuumAnalyze bool                         // If true, execute VACUUM ANALYZE instead of VACUUM
	pollingInProgress    bool                         // If true, a progress poll is already running

	// Permissions dialog state
	permissionsDialog *PermissionsDialog // Active permissions dialog (nil if not showing)

	// App state
	readonlyMode   bool
	connected      bool
	connectionInfo string
	lastUpdate     time.Time
	refreshing     bool
	loading        bool
	err            error

	// Toast
	toastMessage string
	toastError   bool
	toastTime    time.Time

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
		mode:               ModeNormal,
		focusPanel:         FocusTables,
		sortColumn:         SortByName,
		indexSortColumn:    IndexSortByName,
		indexSortAscending: false,
		splitRatio:         0.67, // 67% tables, 33% indexes
		partitions:         make(map[uint32][]uint32),
		tablesByOID:        make(map[uint32]*models.Table),
		clipboard:          ui.NewClipboardWriter(),
		spinner:            s,
		showSystemSchemas:  false, // Hidden by default per spec
		loading:            true,  // Start in loading state
		operationHistory:   models.NewOperationHistory(100), // Session-scoped, max 100 entries
	}
}

// Init initializes the tables view (spinner only - data fetch triggered after pool is set).
func (v *TablesView) Init() tea.Cmd {
	return v.spinner.Tick
}

// FetchTablesData returns a command that loads all table data.
// This is exported for the app to trigger after setting the pool.
func (v *TablesView) FetchTablesData() tea.Cmd {
	return v.fetchTablesData()
}

// fetchTablesData returns a command that loads all table data.
func (v *TablesView) fetchTablesData() tea.Cmd {
	// Don't fetch during maintenance operations to avoid timeout errors
	if v.mode == ModeOperationProgress || v.currentOperation != nil {
		return nil
	}

	return func() tea.Msg {
		if v.pool == nil {
			return TablesDataMsg{Error: fmt.Errorf("database connection not available")}
		}

		// Use longer timeout - after maintenance operations the pool may be recovering
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Check pgstattuple extension availability
		pgstattupleAvailable, err := queries.CheckPgstattupleExtension(ctx, v.pool)
		if err != nil {
			// Non-fatal: continue without pgstattuple
			pgstattupleAvailable = false
		}

		schemas, err := queries.GetSchemas(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch schemas: %w", err)}
		}

		tables, err := queries.GetTablesWithStats(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch tables: %w", err)}
		}

		indexes, err := queries.GetIndexesWithStats(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch indexes: %w", err)}
		}

		partitions, err := queries.GetPartitionHierarchy(ctx, v.pool)
		if err != nil {
			return TablesDataMsg{Error: fmt.Errorf("fetch partitions: %w", err)}
		}

		// Get accurate bloat if pgstattuple is available
		var bloat map[uint32]float64
		if pgstattupleAvailable {
			bloat, err = queries.GetTableBloat(ctx, v.pool)
			if err != nil {
				// Non-fatal: fall back to estimates
				bloat = nil
			}
		}

		return TablesDataMsg{
			Schemas:              schemas,
			Tables:               tables,
			Indexes:              indexes,
			Partitions:           partitions,
			Bloat:                bloat,
			PgstattupleAvailable: pgstattupleAvailable,
		}
	}
}

// scheduleRefresh returns a command for 30-second auto-refresh.
func (v *TablesView) scheduleRefresh() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return RefreshTablesMsg{}
	})
}

// progressTick returns a command for polling operation progress.
func (v *TablesView) progressTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return ProgressTickMsg{}
	})
}

// pollProgress polls for the current operation's progress.
func (v *TablesView) pollProgress() tea.Cmd {
	if v.currentOperation == nil || v.maintenanceTarget == nil {
		return nil
	}

	opType := v.currentOperation.Type
	schema := v.maintenanceTarget.SchemaName
	table := v.maintenanceTarget.Name

	return func() tea.Msg {
		// Use short timeout - if pool is contended, fail fast and try again next tick
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var progress *models.OperationProgress
		var err error

		switch opType {
		case models.OpVacuum, models.OpVacuumAnalyze:
			// Regular VACUUM uses pg_stat_progress_vacuum
			progress, err = queries.GetVacuumProgress(ctx, v.pool, schema, table)
		case models.OpVacuumFull:
			// VACUUM FULL uses pg_stat_progress_cluster (rewrites table)
			progress, err = queries.GetVacuumFullProgress(ctx, v.pool, schema, table)
		default:
			// ANALYZE, REINDEX don't have progress tracking
			return OperationProgressMsg{Progress: nil}
		}

		if err != nil {
			// Log error but continue - non-fatal
			logger.Debug("progress poll error", "error", err, "schema", schema, "table", table, "opType", opType)
			return OperationProgressMsg{Progress: nil}
		}
		return OperationProgressMsg{Progress: progress}
	}
}

// installExtension returns a command that installs the pgstattuple extension.
func (v *TablesView) installExtension() tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return InstallExtensionMsg{Success: false, Error: fmt.Errorf("database connection not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := queries.InstallPgstattupleExtension(ctx, v.pool)
		if err != nil {
			return InstallExtensionMsg{Success: false, Error: err}
		}

		return InstallExtensionMsg{Success: true}
	}
}

// fetchTableDetails returns a command that fetches column and constraint data for a table.
func (v *TablesView) fetchTableDetails(tableOID uint32) tea.Cmd {
	return func() tea.Msg {
		if v.pool == nil {
			return TableDetailsMsg{Error: fmt.Errorf("database connection not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		columns, err := queries.GetTableColumns(ctx, v.pool, tableOID)
		if err != nil {
			return TableDetailsMsg{TableOID: tableOID, Error: fmt.Errorf("fetch columns: %w", err)}
		}

		constraints, err := queries.GetTableConstraints(ctx, v.pool, tableOID)
		if err != nil {
			return TableDetailsMsg{TableOID: tableOID, Error: fmt.Errorf("fetch constraints: %w", err)}
		}

		return TableDetailsMsg{
			TableOID:    tableOID,
			Columns:     columns,
			Constraints: constraints,
		}
	}
}
