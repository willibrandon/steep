# Contract: Tables View

**Feature**: 005-tables-statistics
**Package**: `internal/ui/views/tables`

## View Interface

The TablesView implements the standard ViewModel interface from `internal/ui/views/types.go`:

```go
// ViewModel interface (already defined in views package)
type ViewModel interface {
    Init() tea.Cmd
    Update(tea.Msg) (ViewModel, tea.Cmd)
    View() string
    SetSize(width, height int)
}
```

## Message Types

```go
// TablesDataMsg contains refreshed table/index data.
type TablesDataMsg struct {
    Schemas    []models.Schema
    Tables     []models.Table
    Indexes    []models.Index
    Partitions map[uint32][]uint32  // parent OID -> child OIDs
    Error      error
}

// TableDetailsMsg contains details for selected table.
type TableDetailsMsg struct {
    Details *models.TableDetails
    Error   error
}

// ExtensionCheckMsg indicates pgstattuple availability.
type ExtensionCheckMsg struct {
    Available bool
    Error     error
}

// ExtensionInstallMsg contains result of install attempt.
type ExtensionInstallMsg struct {
    Success bool
    Error   error
}

// TableBloatMsg contains accurate bloat from pgstattuple.
type TableBloatMsg struct {
    TableOID uint32
    BloatPct float64
    Error    error
}

// MaintenanceResultMsg contains result of VACUUM/ANALYZE/REINDEX.
type MaintenanceResultMsg struct {
    Operation string  // "VACUUM", "ANALYZE", "REINDEX"
    TableName string
    Success   bool
    Error     error
}

// RefreshTablesMsg triggers data refresh.
type RefreshTablesMsg struct{}
```

## View State

```go
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

// TablesView is the main view struct.
type TablesView struct {
    width  int
    height int

    // Data
    schemas    []models.Schema
    tables     []models.Table
    indexes    []models.Index
    partitions map[uint32][]uint32
    details    *models.TableDetails

    // UI state
    mode             TablesMode
    focusPanel       FocusPanel
    selectedSchema   int
    selectedTable    int
    selectedIndex    int
    sortColumn       SortColumn
    sortAscending    bool
    showSystemSchemas bool

    // Extension state
    pgstattupleAvailable bool
    pgstattupleChecked   bool
    installPromptShown   bool  // session-scoped: don't prompt again if declined

    // App state
    readonlyMode bool

    // Toast
    toast        string
    toastVisible bool
    toastTimer   *time.Timer

    // Spinner for loading states
    spinner spinner.Model

    // Database connection
    pool *pgxpool.Pool
}
```

## Keyboard Bindings

| Key | Mode | Action |
|-----|------|--------|
| `j` / `↓` | Normal | Move cursor down |
| `k` / `↑` | Normal | Move cursor up |
| `Enter` / `→` | Normal (schema) | Expand/collapse schema |
| `Enter` / `→` | Normal (table) | Expand partitions or open details |
| `←` | Normal | Collapse current item |
| `d` | Normal | Open details panel |
| `Tab` | Normal | Switch focus (tables ↔ indexes) |
| `s` | Normal | Cycle sort column |
| `S` | Normal | Toggle sort direction |
| `P` | Normal | Toggle system schemas visibility |
| `y` | Normal | Copy selected name to clipboard |
| `v` | Normal | VACUUM selected table (confirm) |
| `a` | Normal | ANALYZE selected table (confirm) |
| `r` | Normal | REINDEX selected table (confirm) |
| `h` / `?` | Any | Show help overlay |
| `Esc` / `q` | Details/Help | Close overlay |
| `y` / `Enter` | Confirm | Confirm action |
| `n` / `Esc` | Confirm | Cancel action |

## Commands

```go
// fetchTablesData returns a command that loads all table data.
func (v *TablesView) fetchTablesData() tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        schemas, err := queries.GetSchemas(ctx, v.pool)
        if err != nil {
            return TablesDataMsg{Error: err}
        }

        tables, err := queries.GetTablesWithStats(ctx, v.pool)
        // ... indexes, partitions ...

        return TablesDataMsg{
            Schemas:    schemas,
            Tables:     tables,
            Indexes:    indexes,
            Partitions: partitions,
        }
    }
}

// checkExtension returns a command that checks pgstattuple availability.
func (v *TablesView) checkExtension() tea.Cmd

// installExtension returns a command that installs pgstattuple.
func (v *TablesView) installExtension() tea.Cmd

// fetchTableDetails returns a command that loads details for selected table.
func (v *TablesView) fetchTableDetails(tableOID uint32) tea.Cmd

// fetchTableBloat returns a command that gets accurate bloat via pgstattuple.
func (v *TablesView) fetchTableBloat(tableOID uint32) tea.Cmd

// executeVacuum returns a command that runs VACUUM.
func (v *TablesView) executeVacuum(schema, table string) tea.Cmd

// executeAnalyze returns a command that runs ANALYZE.
func (v *TablesView) executeAnalyze(schema, table string) tea.Cmd

// executeReindex returns a command that runs REINDEX.
func (v *TablesView) executeReindex(schema, table string) tea.Cmd

// scheduleRefresh returns a command for 30-second auto-refresh.
func (v *TablesView) scheduleRefresh() tea.Cmd {
    return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
        return RefreshTablesMsg{}
    })
}
```

## Rendering

```go
// View renders the current state.
func (v *TablesView) View() string {
    switch v.mode {
    case ModeHelp:
        return v.renderWithOverlay(v.renderHelp())
    case ModeDetails:
        return v.renderWithOverlay(v.renderDetails())
    case ModeConfirmInstall:
        return v.renderWithOverlay(v.renderInstallConfirm())
    case ModeConfirmVacuum, ModeConfirmAnalyze, ModeConfirmReindex:
        return v.renderWithOverlay(v.renderMaintenanceConfirm())
    default:
        return v.renderMainView()
    }
}

// renderMainView renders the tree table view.
func (v *TablesView) renderMainView() string

// renderWithOverlay renders main view with modal overlay.
func (v *TablesView) renderWithOverlay(overlay string) string

// renderHelp renders the help overlay content.
func (v *TablesView) renderHelp() string

// renderDetails renders the table details panel.
func (v *TablesView) renderDetails() string

// renderInstallConfirm renders pgstattuple install confirmation.
func (v *TablesView) renderInstallConfirm() string

// renderMaintenanceConfirm renders VACUUM/ANALYZE/REINDEX confirmation.
func (v *TablesView) renderMaintenanceConfirm() string
```

## Integration Points

### App Registration

```go
// In internal/app/app.go
case "5":
    return m, m.switchView(views.ViewTables)
```

### View Factory

```go
// NewTablesView creates a new TablesView instance.
func NewTablesView(pool *pgxpool.Pool, readonlyMode bool) *TablesView
```

### Status Bar Integration

The view provides status information for the status bar:
- Connection string
- Current database
- Refresh timestamp
- pgstattuple availability indicator
