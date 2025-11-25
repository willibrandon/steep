# Research: Replication Monitoring & Setup

**Feature**: 006-replication-monitoring
**Date**: 2025-11-24

## Research Summary

This document consolidates research findings for the Replication Monitoring & Setup feature, covering new library integrations, PostgreSQL replication queries, and best practices.

---

## 1. Charmbracelet/Huh Forms Library

**Decision**: Use `github.com/charmbracelet/huh` for setup wizards
**Rationale**: Native Bubbletea integration, multi-step wizard support, built-in validation
**Alternatives Considered**: Custom form implementation (rejected - significant effort), tview (rejected - not Charm ecosystem)

### Key Integration Patterns

#### Multi-Step Wizard Structure

```go
form := huh.NewForm(
    // Step 1: Replication type
    huh.NewGroup(
        huh.NewSelect[string]().
            Key("repl_type").
            Title("Replication Type").
            Options(
                huh.NewOption("Streaming (Physical)", "streaming"),
                huh.NewOption("Logical", "logical"),
            ).
            Value(&config.Type),
    ),

    // Step 2: Configuration (conditional)
    huh.NewGroup(
        huh.NewInput().
            Key("repl_user").
            Title("Replication Username").
            Value(&config.Username).
            Validate(huh.ValidateNotEmpty()),
    ).WithHideFunc(func() bool {
        return config.Type == "" // Skip until type selected
    }),

    // Step 3: Review
    huh.NewGroup(
        huh.NewConfirm().
            Title("Apply Configuration?").
            Value(&config.Confirmed),
    ),
)
```

#### Bubbletea Integration

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    form, cmd := m.form.Update(msg)
    m.form = form.(*huh.Form)

    if m.form.State() == huh.StateCompleted {
        // Process results
        return m, nil
    }
    return m, cmd
}
```

### Validation Patterns

- Built-in: `ValidateNotEmpty()`, `ValidateMinLength(n)`, `ValidateLength(min, max)`
- Custom: `Validate(func(s string) error { ... })`
- Password strength validation with go-password library

### Styling Integration

- Use `huh.ThemeCharm()` as base theme
- Compatible with existing Lipgloss styles in `internal/ui/styles/`

### Limitations

- Terminal width < 5 chars can panic (guard in resize handler)
- Forms must be completely re-rendered on update (no partial updates)
- No dropdown search - use Select with filtered options

---

## 2. Asciigraph Library

**Decision**: Use `github.com/guptarohit/asciigraph` for lag sparklines
**Rationale**: Lightweight, real-time capable, color support, no external dependencies
**Alternatives Considered**: termui (rejected - heavier), custom Unicode blocks (rejected - more work)

### Key API

```go
// Basic sparkline
chart := asciigraph.Plot(lagHistory,
    asciigraph.Height(3),
    asciigraph.Width(40),
    asciigraph.LowerBound(0),
    asciigraph.UpperBound(maxLag),
    asciigraph.SeriesColors(asciigraph.Green),
)
```

### Real-Time Integration Pattern

```go
const dataPoints = 60 // 60 seconds of history
var lagHistory []float64

// In monitor goroutine
for range ticker.C {
    newLag := getLagBytes()
    lagHistory = append(lagHistory, float64(newLag))

    // Sliding window
    if len(lagHistory) > dataPoints {
        lagHistory = lagHistory[len(lagHistory)-dataPoints:]
    }

    // Send to UI
    updateChan <- ReplicationUpdate{LagHistory: lagHistory}
}
```

### Color Coding by Severity

```go
func getLagColor(lagBytes int64) asciigraph.AnsiColor {
    switch {
    case lagBytes < 1024*1024:    // < 1MB
        return asciigraph.Green
    case lagBytes < 10*1024*1024: // < 10MB
        return asciigraph.Yellow
    default:
        return asciigraph.Red
    }
}
```

### Limitations

- Produces multi-line output (minimum ~3 lines for useful display)
- For ultra-compact sparklines (single line), use Unicode blocks directly: `▁▂▃▄▅▆▇█`
- Bounds are hints, not hard limits (data exceeding bounds adjusts scale)

### Unicode Block Alternative for Compact Display

```go
// For single-line sparklines in replica list
func renderSparkline(data []float64, maxVal float64) string {
    blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
    var result strings.Builder
    for _, v := range data {
        idx := int((v / maxVal) * float64(len(blocks)-1))
        if idx < 0 { idx = 0 }
        if idx >= len(blocks) { idx = len(blocks) - 1 }
        result.WriteRune(blocks[idx])
    }
    return result.String()
}
```

---

## 3. Go-Password Library

**Decision**: Use `github.com/sethvargo/go-password` for secure password generation
**Rationale**: Well-maintained, cryptographically secure, configurable complexity
**Alternatives Considered**: crypto/rand DIY (rejected - error-prone), uuidgen (rejected - not memorable)

### Password Generation Pattern

```go
import "github.com/sethvargo/go-password/password"

func generateReplicationPassword() (string, error) {
    // Generate 24-char password with:
    // - At least 4 digits
    // - At least 4 symbols
    // - No ambiguous characters (0, O, l, 1)
    return password.Generate(24, 4, 4, false, false)
}
```

### Password Strength Validation

```go
func validatePasswordStrength(pw string) error {
    if len(pw) < 12 {
        return fmt.Errorf("password must be at least 12 characters")
    }
    hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(pw)
    hasLower := regexp.MustCompile(`[a-z]`).MatchString(pw)
    hasDigit := regexp.MustCompile(`[0-9]`).MatchString(pw)
    if !hasUpper || !hasLower || !hasDigit {
        return fmt.Errorf("password must contain uppercase, lowercase, and digit")
    }
    return nil
}
```

---

## 4. PostgreSQL Replication Queries

### 4.1 Primary Server - Streaming Replication Status

```sql
SELECT
    application_name,
    state,
    sync_state,
    client_addr,
    pg_wal_lsn_diff(sent_lsn, replay_lsn) AS lag_bytes,
    replay_lag,
    write_lsn,
    flush_lsn,
    replay_lsn,
    backend_start
FROM pg_stat_replication
ORDER BY application_name;
```

**Version**: PostgreSQL 10+ (LSN columns)
**Permissions**: Available to all users
**Performance**: < 5ms execution

### 4.2 Replication Slots

```sql
SELECT
    slot_name,
    slot_type,
    active,
    active_pid,
    pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS retained_bytes,
    wal_status,
    safe_wal_size
FROM pg_replication_slots
ORDER BY slot_name;
```

**Version**: PostgreSQL 9.4+ (base), 13+ for wal_status/safe_wal_size
**Permissions**: Available to all users

### 4.3 Standby Server - WAL Receiver Status

```sql
SELECT
    status,
    received_lsn,
    pg_wal_lsn_diff(pg_current_wal_lsn(), received_lsn) AS lag_bytes,
    sender_host,
    sender_port,
    slot_name,
    conninfo
FROM pg_stat_wal_receiver;
```

**Version**: PostgreSQL 10+ (received_lsn), 12+ (sender_host/port)
**Permissions**: Superuser only

### 4.4 Logical Replication - Publications

```sql
SELECT
    p.pubname,
    p.puballtables,
    p.pubinsert,
    p.pubupdate,
    p.pubdelete,
    COUNT(pt.tablename) AS table_count
FROM pg_publication p
LEFT JOIN pg_publication_tables pt ON p.pubname = pt.pubname
GROUP BY p.pubname, p.puballtables, p.pubinsert, p.pubupdate, p.pubdelete
ORDER BY p.pubname;
```

**Version**: PostgreSQL 10+
**Permissions**: Available to all users

### 4.5 Logical Replication - Subscriptions

```sql
SELECT
    s.subname,
    s.subenabled,
    s.subconninfo,
    s.subpublications,
    ss.received_lsn,
    ss.latest_end_lsn,
    pg_wal_lsn_diff(ss.latest_end_lsn, ss.received_lsn) AS lag_bytes
FROM pg_subscription s
LEFT JOIN pg_stat_subscription ss ON s.subname = ss.subname
ORDER BY s.subname;
```

**Version**: PostgreSQL 10+ (base), 12+ for pg_stat_subscription
**Permissions**: Available to all users

### 4.6 Configuration Parameters

```sql
SELECT name, setting, unit, context
FROM pg_settings
WHERE name IN (
    'wal_level',
    'max_wal_senders',
    'max_replication_slots',
    'wal_keep_size',
    'hot_standby',
    'archive_mode'
)
ORDER BY name;
```

**Version**: All versions
**Permissions**: Available to all users

### 4.7 pg_hba.conf Inspection (PostgreSQL 15+)

```sql
SELECT
    line_number,
    type,
    database,
    user_name,
    address,
    auth_method
FROM pg_hba_file_rules
WHERE database @> ARRAY['replication']
   OR database @> ARRAY['all']
ORDER BY line_number;
```

**Version**: PostgreSQL 15+ only
**Fallback**: For PG < 15, use `SHOW hba_file` and read file externally

### 4.8 Replication User Validation

```sql
SELECT rolname, rolcanlogin, rolreplication
FROM pg_roles
WHERE rolreplication = true;
```

**Version**: All versions
**Permissions**: Available to all users

---

## 5. Version Compatibility Matrix

| Feature | PG11 | PG12 | PG13 | PG14 | PG15+ |
|---------|------|------|------|------|-------|
| pg_stat_replication (full) | ✓ | ✓ | ✓ | ✓ | ✓ |
| pg_replication_slots | ✓ | ✓ | ✓ | ✓ | ✓ |
| wal_status column | | | ✓ | ✓ | ✓ |
| pg_stat_wal_receiver (full) | ✓ | ✓ | ✓ | ✓ | ✓ |
| pg_stat_subscription | | ✓ | ✓ | ✓ | ✓ |
| pg_publication/subscription | ✓ | ✓ | ✓ | ✓ | ✓ |
| pg_wal_lsn_diff() | | ✓ | ✓ | ✓ | ✓ |
| pg_hba_file_rules | | | | | ✓ |

---

## 6. Existing Steep Patterns to Follow

### View Structure (from locks/view.go)

```go
type ReplicationView struct {
    width, height int
    mode          ReplicationMode
    activeTab     ViewTab

    // Data
    replicas      []models.Replica
    slots         []models.ReplicationSlot
    publications  []models.Publication
    subscriptions []models.Subscription
    lagHistory    map[string][]float64

    // UI components
    table    components.Table
    spinner  spinner.Model
    dialog   *components.Dialog
    helpOpen bool
}

func (v *ReplicationView) Init() tea.Cmd { ... }
func (v *ReplicationView) Update(tea.Msg) (views.ViewModel, tea.Cmd) { ... }
func (v *ReplicationView) View() string { ... }
func (v *ReplicationView) SetSize(width, height int) { ... }
```

### SQLite Schema Pattern (from schema.go)

```sql
CREATE TABLE IF NOT EXISTS replication_lag_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    replica_name TEXT NOT NULL,
    sent_lsn TEXT,
    write_lsn TEXT,
    flush_lsn TEXT,
    replay_lsn TEXT,
    byte_lag INTEGER,
    time_lag_ms INTEGER,
    sync_state TEXT,
    -- Future multi-master fields
    direction TEXT DEFAULT 'outbound',
    conflict_count INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_lag_history_time
    ON replication_lag_history(timestamp, replica_name);
```

### Monitor Goroutine Pattern (from monitors/locks.go)

```go
func (m *ReplicationMonitor) Start(ctx context.Context, updateChan chan<- ReplicationUpdate) {
    ticker := time.NewTicker(m.refreshInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            data, err := m.fetchReplicationData()
            if err != nil {
                logger.Error("replication fetch failed", "error", err)
                continue
            }
            updateChan <- ReplicationUpdate{Data: data}
        }
    }
}
```

---

## 7. Topology Visualization with Treeprint

The `xlab/treeprint` library is already in go.mod. Pattern for replication topology:

```go
import "github.com/xlab/treeprint"

func renderTopology(primary string, replicas []models.Replica) string {
    tree := treeprint.NewWithRoot(fmt.Sprintf("PRIMARY: %s", primary))

    // Group by upstream (for cascading)
    byUpstream := make(map[string][]models.Replica)
    for _, r := range replicas {
        upstream := r.Upstream
        if upstream == "" {
            upstream = primary
        }
        byUpstream[upstream] = append(byUpstream[upstream], r)
    }

    // Build tree recursively
    addReplicas(tree, primary, byUpstream)

    return tree.String()
}

func addReplicas(branch treeprint.Tree, upstream string, byUpstream map[string][]models.Replica) {
    for _, r := range byUpstream[upstream] {
        node := branch.AddMetaBranch(r.SyncState, fmt.Sprintf("%s (%s)", r.Name, formatLag(r.LagBytes)))
        addReplicas(node, r.Name, byUpstream)
    }
}
```

---

## 8. Security Considerations

### Password Handling

1. Generate passwords with `go-password` (cryptographically secure)
2. Display password once with copy option, then mask
3. Never log passwords
4. Use scram-sha-256 as default auth method in generated pg_hba.conf entries

### SSL/TLS for Connection Strings

```go
// Default to sslmode=prefer in generated connection strings
func buildConnInfo(host, port, user, appName string) string {
    return fmt.Sprintf(
        "host=%s port=%s user=%s sslmode=prefer application_name=%s",
        host, port, user, appName,
    )
}
```

### Read-Only Mode

All setup operations must check `config.ReadOnly` before execution:

```go
func (v *ReplicationView) handleSetupAction() tea.Cmd {
    if v.config.ReadOnly {
        return v.showError("Setup operations disabled in read-only mode")
    }
    // proceed with setup
}
```

---

## 9. Resolved Research Items

| Item | Resolution |
|------|------------|
| Forms library for wizards | charmbracelet/huh - native Bubbletea integration |
| Sparkline rendering | asciigraph + Unicode blocks for compact display |
| Password generation | sethvargo/go-password with strength validation |
| Topology visualization | xlab/treeprint (already in project) |
| PostgreSQL queries | Documented above, version-aware |
| SQLite schema | Follow existing pattern from deadlock_events |
