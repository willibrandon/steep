# UI Operations Contract

**Package**: `internal/ui/views/tables`

This contract defines the Bubbletea messages and UI components for maintenance operations.

## Message Types

```go
// OperationsMenuMsg signals that the operations menu should be shown.
type OperationsMenuMsg struct {
    Table *models.Table
}

// StartOperationMsg requests starting a maintenance operation.
type StartOperationMsg struct {
    Type        OperationType
    Schema      string
    Table       string
    Index       string // For REINDEX INDEX
    Options     VacuumOptions
}

// OperationStartedMsg indicates an operation has started.
type OperationStartedMsg struct {
    Operation *MaintenanceOperation
}

// OperationProgressMsg contains progress updates for running operations.
type OperationProgressMsg struct {
    OperationID string
    Progress    *OperationProgress
}

// OperationCompletedMsg indicates an operation has finished.
type OperationCompletedMsg struct {
    Operation *MaintenanceOperation
    Success   bool
    Error     error
}

// CancelOperationMsg requests cancellation of a running operation.
type CancelOperationMsg struct {
    OperationID string
    BackendPID  int
}

// OperationCancelledMsg confirms an operation was cancelled.
type OperationCancelledMsg struct {
    OperationID string
    Success     bool
    Error       error
}
```

## View Modes

```go
// TablesMode extended with operation modes
type TablesMode int

const (
    ModeNormal TablesMode = iota
    ModeDetails
    ModeCopyMenu
    ModeConfirmInstall
    ModeConfirmVacuum
    ModeConfirmAnalyze
    ModeConfirmReindex
    ModeHelp
    // New modes for operations menu
    ModeOperationsMenu    // Show operation selection menu
    ModeOperationProgress // Show progress for running operation
    ModeConfirmCancel     // Confirm operation cancellation
)
```

## Operations Menu Component

```go
// OperationsMenu represents the maintenance operations selection menu.
type OperationsMenu struct {
    Table          *models.Table
    SelectedIndex  int
    Items          []OperationMenuItem
    ReadOnlyMode   bool
}

// OperationMenuItem represents a menu item.
type OperationMenuItem struct {
    Label       string
    Operation   OperationType
    Description string
    Disabled    bool
    DisabledReason string
}

// DefaultOperationsMenu returns the standard operations menu.
func DefaultOperationsMenu(table *models.Table, readOnly bool) *OperationsMenu {
    items := []OperationMenuItem{
        {
            Label:       "VACUUM",
            Operation:   OpVacuum,
            Description: "Reclaim space from dead tuples",
            Disabled:    readOnly,
            DisabledReason: "Read-only mode",
        },
        {
            Label:       "VACUUM FULL",
            Operation:   OpVacuumFull,
            Description: "Rewrite table to reclaim all space (BLOCKS TABLE)",
            Disabled:    readOnly,
            DisabledReason: "Read-only mode",
        },
        {
            Label:       "VACUUM ANALYZE",
            Operation:   OpVacuumAnalyze,
            Description: "Vacuum and update statistics",
            Disabled:    readOnly,
            DisabledReason: "Read-only mode",
        },
        {
            Label:       "ANALYZE",
            Operation:   OpAnalyze,
            Description: "Update query planner statistics",
            Disabled:    readOnly,
            DisabledReason: "Read-only mode",
        },
        {
            Label:       "REINDEX TABLE",
            Operation:   OpReindexTable,
            Description: "Rebuild all indexes (LOCKS TABLE)",
            Disabled:    readOnly,
            DisabledReason: "Read-only mode",
        },
    }
    return &OperationsMenu{
        Table:        table,
        Items:        items,
        ReadOnlyMode: readOnly,
    }
}

// View renders the operations menu.
func (m *OperationsMenu) View() string {
    var b strings.Builder

    b.WriteString(fmt.Sprintf("Operations for %s.%s\n\n",
        m.Table.SchemaName, m.Table.Name))

    for i, item := range m.Items {
        cursor := "  "
        if i == m.SelectedIndex {
            cursor = "> "
        }

        style := lipgloss.NewStyle()
        if item.Disabled {
            style = style.Foreground(styles.ColorTextDim)
        }

        line := fmt.Sprintf("%s%s - %s", cursor, item.Label, item.Description)
        if item.Disabled {
            line += fmt.Sprintf(" [%s]", item.DisabledReason)
        }

        b.WriteString(style.Render(line) + "\n")
    }

    b.WriteString("\n[Enter] Execute  [Esc] Cancel")
    return b.String()
}
```

## Progress Indicator Component

```go
// ProgressIndicator displays operation progress.
type ProgressIndicator struct {
    Operation   *MaintenanceOperation
    Width       int
    ShowPhase   bool
    ShowPercent bool
}

// View renders the progress indicator.
func (p *ProgressIndicator) View() string {
    if p.Operation == nil {
        return ""
    }

    var b strings.Builder

    // Header
    b.WriteString(fmt.Sprintf("%s %s.%s\n",
        p.Operation.Type,
        p.Operation.TargetSchema,
        p.Operation.TargetTable))

    // Progress bar
    if p.Operation.Progress != nil && p.Operation.Progress.PercentComplete > 0 {
        bar := renderProgressBar(p.Operation.Progress.PercentComplete, p.Width-10)
        b.WriteString(fmt.Sprintf("%s %5.1f%%\n", bar, p.Operation.Progress.PercentComplete))

        // Phase
        if p.ShowPhase && p.Operation.Progress.Phase != "" {
            b.WriteString(fmt.Sprintf("Phase: %s\n", p.Operation.Progress.Phase))
        }
    } else {
        // No progress tracking (ANALYZE, REINDEX)
        b.WriteString(renderSpinner() + " In progress...\n")
    }

    // Duration
    elapsed := time.Since(p.Operation.StartedAt)
    b.WriteString(fmt.Sprintf("Elapsed: %s\n", formatDuration(elapsed)))

    // Cancel hint
    b.WriteString("\n[c] Cancel operation  [Esc] Continue in background")

    return b.String()
}

// renderProgressBar creates an ASCII progress bar.
func renderProgressBar(percent float64, width int) string {
    filled := int(float64(width) * percent / 100)
    empty := width - filled

    bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
    return "[" + bar + "]"
}
```

## Confirmation Dialog Component

```go
// ConfirmOperationDialog displays operation confirmation.
type ConfirmOperationDialog struct {
    Operation   OperationType
    Target      string // schema.table
    Description string
    Warning     string // Optional warning message
    Width       int
}

// View renders the confirmation dialog.
func (d *ConfirmOperationDialog) View() string {
    var b strings.Builder

    b.WriteString(fmt.Sprintf("%s %s?\n\n", d.Operation, d.Target))
    b.WriteString(d.Description + "\n")

    if d.Warning != "" {
        b.WriteString("\n")
        b.WriteString(styles.WarningStyle.Render("⚠ " + d.Warning))
        b.WriteString("\n")
    }

    b.WriteString("\n[y/Enter] Execute  [n/Esc] Cancel")

    return lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(styles.ColorAccent).
        Padding(1, 2).
        Width(d.Width).
        Render(b.String())
}

// GetWarning returns appropriate warning for operation type.
func GetOperationWarning(op OperationType) string {
    switch op {
    case OpVacuumFull:
        return "VACUUM FULL acquires an exclusive lock. The table will be unavailable during the operation."
    case OpReindexTable, OpReindexIndex:
        return "REINDEX locks the table for writes during the operation."
    default:
        return ""
    }
}
```

## Key Bindings

```go
// Operations menu key binding (Tables view)
case "x":
    if v.selectedIdx >= 0 && v.selectedIdx < len(v.treeItems) {
        item := v.treeItems[v.selectedIdx]
        if item.Table != nil {
            v.operationsMenu = DefaultOperationsMenu(item.Table, v.readonlyMode)
            v.mode = ModeOperationsMenu
        }
    }

// Cancel running operation
case "c":
    if v.mode == ModeOperationProgress && v.currentOperation != nil {
        v.mode = ModeConfirmCancel
    }

// Operations menu navigation
case "j", "down":
    if v.mode == ModeOperationsMenu {
        v.operationsMenu.MoveDown()
    }
case "k", "up":
    if v.mode == ModeOperationsMenu {
        v.operationsMenu.MoveUp()
    }
case "enter":
    if v.mode == ModeOperationsMenu {
        selected := v.operationsMenu.SelectedItem()
        if !selected.Disabled {
            v.pendingOperation = selected.Operation
            v.mode = ModeConfirmVacuum // or appropriate confirm mode
        }
    }
```

## Operation State Management

```go
// TablesView extended fields
type TablesView struct {
    // ... existing fields ...

    // Operations state
    operationsMenu    *OperationsMenu
    currentOperation  *MaintenanceOperation
    operationHistory  *OperationHistory
    progressTicker    *time.Ticker
}

// StartOperation begins a maintenance operation.
func (v *TablesView) StartOperation(op OperationType, table *models.Table, opts VacuumOptions) tea.Cmd {
    operation := &MaintenanceOperation{
        ID:           uuid.New().String(),
        Type:         op,
        TargetSchema: table.SchemaName,
        TargetTable:  table.Name,
        Status:       StatusPending,
        StartedAt:    time.Now(),
    }

    v.currentOperation = operation
    v.mode = ModeOperationProgress

    return tea.Batch(
        v.executeOperation(operation, opts),
        v.pollProgress(),
    )
}

// pollProgress returns a command that polls for operation progress.
func (v *TablesView) pollProgress() tea.Cmd {
    return tea.Tick(time.Second, func(t time.Time) tea.Msg {
        if v.currentOperation == nil {
            return nil
        }
        return pollProgressMsg{operationID: v.currentOperation.ID}
    })
}
```
