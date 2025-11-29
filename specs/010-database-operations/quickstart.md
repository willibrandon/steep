# Quickstart: Database Management Operations

**Feature**: 010-database-operations | **Date**: 2025-11-28

## Overview

This guide provides step-by-step instructions for implementing the Database Management Operations feature in Steep. Follow the phases in order - each builds on the previous.

## Prerequisites

- Go 1.25.4+
- PostgreSQL 11+ test database
- Understanding of existing Steep architecture (Tables view, queries package)

## Phase 1: Extend Tables View with Vacuum Status (P2)

### Step 1.1: Update Table Model

Add vacuum status fields to `internal/db/models/table.go`:

```go
type Table struct {
    // ... existing fields ...

    // Vacuum status
    LastVacuum       *time.Time
    LastAutovacuum   *time.Time
    LastAnalyze      *time.Time
    LastAutoanalyze  *time.Time
    VacuumCount      int64
    AutovacuumCount  int64
    AutovacuumEnabled bool
}
```

### Step 1.2: Update Tables Query

Modify `internal/db/queries/tables.go` `GetTablesWithStats` to include vacuum columns:

```go
// Add to SELECT clause:
s.last_vacuum,
s.last_autovacuum,
s.last_analyze,
s.last_autoanalyze,
COALESCE(s.vacuum_count, 0) as vacuum_count,
COALESCE(s.autovacuum_count, 0) as autovacuum_count

// Add to Scan call:
&table.LastVacuum,
&table.LastAutovacuum,
// ...etc
```

### Step 1.3: Add Vacuum Status Columns to Tables View

Modify `internal/ui/views/tables/view.go` to display vacuum timestamps. Add columns to `renderHeader()` and `renderTreeRow()`.

**Test**: Run Steep, navigate to Tables view, verify vacuum timestamps display.

---

## Phase 2: Implement Operations Menu (P1)

### Step 2.1: Add Operations Menu Mode

Add to `internal/ui/views/tables/view.go`:

```go
const (
    // ... existing modes ...
    ModeOperationsMenu TablesMode = iota + 100
)
```

### Step 2.2: Create Operations Menu Component

Create `internal/ui/views/tables/operations.go`:

```go
type OperationsMenu struct {
    Table         *models.Table
    SelectedIndex int
    Items         []MenuItem
    ReadOnlyMode  bool
}

func NewOperationsMenu(table *models.Table, readOnly bool) *OperationsMenu
func (m *OperationsMenu) View() string
func (m *OperationsMenu) HandleKey(key string) (selected *MenuItem, close bool)
```

### Step 2.3: Wire Up `x` Key Binding

In `handleKeyPress()`:

```go
case "x":
    if v.focusPanel == FocusTables && item.Table != nil {
        v.operationsMenu = NewOperationsMenu(item.Table, v.readonlyMode)
        v.mode = ModeOperationsMenu
    }
```

**Test**: Press `x` on a table, verify menu appears with operation options.

---

## Phase 3: Implement VACUUM Operations (P1)

### Step 3.1: Add VACUUM Variants to Queries

Create or extend `internal/db/queries/maintenance.go`:

```go
type VacuumOptions struct {
    Full    bool
    Analyze bool
}

func ExecuteVacuumWithOptions(ctx context.Context, pool *pgxpool.Pool,
    schema, table string, opts VacuumOptions) error {

    var sql string
    switch {
    case opts.Full && opts.Analyze:
        sql = fmt.Sprintf("VACUUM (FULL, ANALYZE) %s.%s", quote(schema), quote(table))
    case opts.Full:
        sql = fmt.Sprintf("VACUUM FULL %s.%s", quote(schema), quote(table))
    case opts.Analyze:
        sql = fmt.Sprintf("VACUUM ANALYZE %s.%s", quote(schema), quote(table))
    default:
        sql = fmt.Sprintf("VACUUM %s.%s", quote(schema), quote(table))
    }

    _, err := pool.Exec(ctx, sql)
    return err
}
```

### Step 3.2: Add Confirmation Dialogs

Extend existing confirmation dialog pattern from Tables view for VACUUM variants.

### Step 3.3: Execute and Show Result

Follow existing `MaintenanceResultMsg` pattern to show completion toast.

**Test**: Execute VACUUM on a table, verify confirmation dialog, completion toast.

---

## Phase 4: Add Progress Tracking (P1)

### Step 4.1: Create Progress Query

Add to `internal/db/queries/maintenance.go`:

```go
type VacuumProgress struct {
    PID             int
    Phase           string
    HeapBlksTotal   int64
    HeapBlksScanned int64
    PercentComplete float64
}

func GetVacuumProgress(ctx context.Context, pool *pgxpool.Pool,
    schema, table string) (*VacuumProgress, error) {

    query := `
        SELECT pid, phase, heap_blks_total, heap_blks_scanned,
               ROUND(100.0 * heap_blks_scanned / NULLIF(heap_blks_total, 0), 2)
        FROM pg_stat_progress_vacuum
        WHERE relid = $1::regclass`

    var p VacuumProgress
    err := pool.QueryRow(ctx, query, schema+"."+table).Scan(
        &p.PID, &p.Phase, &p.HeapBlksTotal, &p.HeapBlksScanned, &p.PercentComplete)
    if err == pgx.ErrNoRows {
        return nil, nil
    }
    return &p, err
}
```

### Step 4.2: Create Progress Display Mode

Add `ModeOperationProgress` to show real-time progress with 1-second polling.

### Step 4.3: Implement Progress Polling

Use `tea.Tick` to poll progress every second while operation is running.

**Test**: Run VACUUM on a large table, verify progress percentage updates.

---

## Phase 5: Add Cancellation Support (P1)

### Step 5.1: Add Cancel Function

```go
func CancelOperation(ctx context.Context, pool *pgxpool.Pool, pid int) (bool, error) {
    var cancelled bool
    err := pool.QueryRow(ctx, "SELECT pg_cancel_backend($1)", pid).Scan(&cancelled)
    return cancelled, err
}
```

### Step 5.2: Add Cancel Confirmation

Add `ModeConfirmCancel` with confirmation dialog.

### Step 5.3: Wire Up `c` Key

In progress mode, `c` opens cancel confirmation.

**Test**: Start VACUUM, press `c`, confirm cancel, verify operation stops.

---

## Phase 6: Implement Roles View (P3)

### Step 6.1: Create Role Model

Create `internal/db/models/role.go`:

```go
type Role struct {
    OID             uint32
    Name            string
    IsSuperuser     bool
    CanLogin        bool
    CanCreateRole   bool
    CanCreateDB     bool
    ConnectionLimit int
    ValidUntil      *time.Time
    MemberOf        []string
}
```

### Step 6.2: Create Role Queries

Create `internal/db/queries/roles.go` with `GetRoles()`, `GetRoleMemberships()`.

### Step 6.3: Create Roles View

Create `internal/ui/views/roles/view.go` following Tables view pattern:
- List all roles with attributes
- Details panel on Enter
- `0` key binding in app.go

### Step 6.4: Add Help Overlay

Create `internal/ui/views/roles/help.go`.

**Test**: Press `0`, verify Roles view displays all roles with correct attributes.

---

## Phase 7: Implement GRANT/REVOKE (P3)

### Step 7.1: Add Permission Queries

Extend `internal/db/queries/roles.go`:

```go
func GetTablePermissions(ctx context.Context, pool *pgxpool.Pool, tableOID uint32) ([]Permission, error)
func GrantTablePrivilege(ctx context.Context, pool *pgxpool.Pool, params GrantParams) error
func RevokeTablePrivilege(ctx context.Context, pool *pgxpool.Pool, params RevokeParams) error
```

### Step 7.2: Create Permission Dialog

Add permission viewing and modification dialogs to Roles view or Tables view details.

**Test**: View permissions on a table, grant SELECT to a role, verify change.

---

## Validation Checklist

### Per-Phase Testing

- [ ] Phase 1: Vacuum status columns visible in Tables view
- [ ] Phase 2: Operations menu opens with `x` key
- [ ] Phase 3: VACUUM executes with confirmation
- [ ] Phase 4: Progress percentage updates in real-time
- [ ] Phase 5: Cancel stops running operation
- [ ] Phase 6: Roles view displays all roles
- [ ] Phase 7: GRANT/REVOKE works with confirmation

### Integration Testing

- [ ] Read-only mode blocks all destructive operations
- [ ] Only one operation runs at a time
- [ ] Connection loss shows appropriate error
- [ ] Keyboard navigation works throughout

### Performance Testing

- [ ] Progress polling < 100ms
- [ ] Role queries < 500ms
- [ ] UI remains responsive during operations

## File Reference

| File | Purpose |
|------|---------|
| `internal/db/models/table.go` | Extend with vacuum fields |
| `internal/db/models/role.go` | NEW: Role model |
| `internal/db/models/operation.go` | NEW: Operation models |
| `internal/db/queries/tables.go` | Extend with vacuum status |
| `internal/db/queries/maintenance.go` | NEW: VACUUM/progress queries |
| `internal/db/queries/roles.go` | NEW: Role/permission queries |
| `internal/ui/views/tables/view.go` | Extend with operations |
| `internal/ui/views/tables/operations.go` | NEW: Operations menu |
| `internal/ui/views/roles/view.go` | NEW: Roles view |
| `internal/ui/views/roles/help.go` | NEW: Help overlay |
