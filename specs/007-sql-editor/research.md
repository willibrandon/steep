# Research: SQL Editor & Execution

**Feature**: 007-sql-editor | **Date**: 2025-11-25

## Summary

This document consolidates research findings for implementing the SQL Editor view in Steep. All critical technical decisions have been resolved.

---

## 1. Bubbles Textarea Component

**Decision**: Use `charmbracelet/bubbles/textarea` for multi-line SQL input

**Rationale**: The textarea component provides all required features out-of-box:
- Line numbers (`ShowLineNumbers = true`)
- Cursor line highlighting (`FocusedStyle.CursorLine`)
- Focus/blur styling (`FocusedStyle.Base`, `BlurredStyle.Base`)
- Position detection (`Line()`, `LineInfo()`) for history recall at boundaries

**Alternatives Considered**:
- Custom textarea implementation: Rejected (unnecessary complexity, bubbles/textarea is well-maintained)
- Third-party editor component: Rejected (no Go-native alternatives with required features)

### Implementation Pattern

```go
ta := textarea.New()
ta.ShowLineNumbers = true
ta.Placeholder = "Enter SQL query... (Ctrl+Enter to execute)"
ta.MaxHeight = 20
ta.CharLimit = 100000

// Focused state (with blue border)
ta.FocusedStyle = textarea.Style{
    Base: lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(styles.ColorAccent),
    CursorLine: lipgloss.NewStyle().
        Background(lipgloss.Color("236")),
}

// Blurred state (gray border)
ta.BlurredStyle = textarea.Style{
    Base: lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(styles.ColorBorder),
}
```

### History Recall at Boundaries

```go
// Detect cursor at top of editor for history navigation
func (e *Editor) isAtStart() bool {
    return e.textarea.Line() == 0 && e.textarea.LineInfo().ColumnOffset == 0
}

// In Update(), intercept up arrow at boundary
if key.Matches(msg, upKey) && e.isAtStart() {
    prev := e.history.Previous()
    if prev != "" {
        e.textarea.SetValue(prev)
        e.textarea.CursorEnd()
        return nil
    }
}
```

### Gotchas

1. Line numbers use 4 characters (3 digits + margin); call `SetWidth()` after enabling
2. `Line()` returns logical lines, not visual rows (soft-wrapping applies)
3. Cursor blinking requires calling `textarea.Blink()` in `Init()`

---

## 2. Transaction Management with pgx

**Decision**: Implement `SessionExecutor` that tracks transaction state across query executions

**Rationale**:
- Transactions require connection affinity (same connection for all queries)
- pgx.Tx must be used for all queries within a transaction
- State machine approach cleanly handles BEGIN/COMMIT/ROLLBACK/SAVEPOINT

**Alternatives Considered**:
- Let PostgreSQL track state (no Go-side tracking): Rejected (need UI indicator)
- Use database/sql instead of pgx: Rejected (pgx already used throughout Steep)

### Transaction State Tracking

```go
type TransactionState struct {
    IsInTransaction bool
    TxObject        pgx.Tx
    SavepointStack  []string
    StartedAt       time.Time
}

type SessionExecutor struct {
    pool     *pgxpool.Pool
    txState  *TransactionState
    readonly bool
}
```

### Transaction Statement Detection

```go
// Simple regex-based detection (lightweight, no pg_query_go dependency)
func DetectTransactionStatement(query string) StatementType {
    upper := strings.ToUpper(strings.TrimSpace(query))
    switch {
    case strings.HasPrefix(upper, "BEGIN"):
        return StatementTypeBegin
    case strings.HasPrefix(upper, "COMMIT"):
        return StatementTypeCommit
    case strings.HasPrefix(upper, "ROLLBACK TO"):
        return StatementTypeRollbackToSavepoint
    case strings.HasPrefix(upper, "ROLLBACK"):
        return StatementTypeRollback
    case strings.HasPrefix(upper, "SAVEPOINT"):
        return StatementTypeSavepoint
    default:
        return StatementTypeQuery
    }
}
```

### Query Timeout with Cancellation

```go
func (se *SessionExecutor) ExecuteQuery(ctx context.Context, query string,
    timeout time.Duration) (*Result, error) {

    queryCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    if se.txState.IsInTransaction {
        return se.executeOnTx(queryCtx, query, se.txState.TxObject)
    }
    return se.executeOnPool(queryCtx, query)
}
```

### Gotchas

1. Context cancellation does NOT auto-rollback transactions in pgx
2. All queries in a transaction must use the same `pgx.Tx` object
3. Nested `tx.Begin()` creates a SAVEPOINT automatically

---

## 3. Query Results Handling

**Decision**: Use pgx `rows.FieldDescriptions()` for column metadata, `rows.Values()` for dynamic type handling

**Rationale**:
- SQL Editor executes arbitrary user queries (unknown schema at compile time)
- Need to display any PostgreSQL type as string
- Separate COUNT query for pagination (avoid window functions)

**Alternatives Considered**:
- Fetch all rows then paginate in memory: Rejected (memory usage for large results)
- Use window functions for count: Rejected (performance overhead)

### Column Metadata Extraction

```go
rows, _ := pool.Query(ctx, userQuery)
defer rows.Close()

fieldDescs := rows.FieldDescriptions()
columns := make([]Column, len(fieldDescs))
for i, fd := range fieldDescs {
    columns[i] = Column{
        Name:    fd.Name,
        TypeOID: fd.DataTypeOID,
    }
}
```

### Dynamic Type Conversion

```go
for rows.Next() {
    values, _ := rows.Values() // Returns []any with decoded Go types

    row := make([]string, len(values))
    for i, val := range values {
        row[i] = formatValue(val)
    }
    results = append(results, row)
}

func formatValue(val any) string {
    if val == nil {
        return "NULL" // Distinct styling in UI
    }
    switch v := val.(type) {
    case time.Time:
        return v.Format("2006-01-02 15:04:05")
    case []byte:
        return string(v)
    default:
        return fmt.Sprintf("%v", v)
    }
}
```

### Pagination Strategy

```go
// Query 1: Get total count
var total int
pool.QueryRow(ctx, "SELECT COUNT(*) FROM (" + userQuery + ") AS t").Scan(&total)

// Query 2: Get paginated results
pagedQuery := fmt.Sprintf("%s LIMIT %d OFFSET %d", userQuery, pageSize, offset)
rows, _ := pool.Query(ctx, pagedQuery)
```

### NULL Handling for TUI

```go
// In formatValue(), check nil first
if val == nil {
    return "NULL" // Apply styles.DimStyle in UI rendering
}
```

---

## 4. Syntax Highlighting

**Decision**: Reuse existing Chroma pattern from `explain.go`

**Rationale**: Steep already uses Chroma for SQL highlighting in EXPLAIN views. Same pattern works for SQL Editor.

**Implementation**:

```go
import "github.com/alecthomas/chroma/v2/quick"

func HighlightSQL(sql string) string {
    var buf bytes.Buffer
    err := quick.Highlight(&buf, sql, "postgresql", "terminal256", "monokai")
    if err != nil {
        return sql // Fallback to plain text
    }
    return buf.String()
}
```

**Applied to**:
- Query history display
- Executed query header in results pane
- Copy/export operations

**NOT applied to** (deferred):
- Live editor input (requires custom textarea or fork)

---

## 5. History & Snippets Persistence

**Decision**: SQLite for history, YAML for snippets

**Rationale**:
- History: SQLite provides efficient search/pagination, already used in Steep for query stats
- Snippets: YAML is human-editable, simple structure, Viper already parses YAML

### History Schema

```sql
CREATE TABLE IF NOT EXISTS query_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query TEXT NOT NULL,
    executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER,
    row_count INTEGER,
    error TEXT
);

CREATE INDEX idx_history_executed_at ON query_history(executed_at DESC);
```

### Snippets Format

```yaml
# ~/.config/steep/snippets.yaml
snippets:
  - name: "active-queries"
    query: |
      SELECT pid, usename, query, now() - query_start as duration
      FROM pg_stat_activity
      WHERE state = 'active'
    created: 2025-11-25T10:00:00Z

  - name: "table-sizes"
    query: |
      SELECT relname, pg_size_pretty(pg_table_size(oid))
      FROM pg_class WHERE relkind = 'r'
      ORDER BY pg_table_size(oid) DESC LIMIT 20
    created: 2025-11-25T10:00:00Z
```

---

## 6. Export Formats

**Decision**: CSV with proper escaping, JSON as array of objects

**CSV Format**:
```csv
"id","name","created_at"
1,"Alice","2025-11-25 10:00:00"
2,"Bob","2025-11-25 11:00:00"
```

**JSON Format**:
```json
[
  {"id": 1, "name": "Alice", "created_at": "2025-11-25T10:00:00Z"},
  {"id": 2, "name": "Bob", "created_at": "2025-11-25T11:00:00Z"}
]
```

---

## Summary of Decisions

| Area | Decision | Key Rationale |
|------|----------|---------------|
| Editor Component | bubbles/textarea | Native line numbers, cursor highlighting, focus states |
| Transaction Management | SessionExecutor with state machine | Connection affinity, clear state tracking |
| Results Display | pgx FieldDescriptions + Values | Dynamic schema support for arbitrary queries |
| Pagination | Separate COUNT + LIMIT/OFFSET | Performance, avoid memory bloat |
| Syntax Highlighting | Chroma (monokai style) | Already used in Steep, consistent look |
| History Storage | SQLite | Efficient search, existing pattern |
| Snippets Storage | YAML | Human-editable, simple structure |
| NULL Display | Distinct "NULL" text with dim styling | Clear visual distinction |
