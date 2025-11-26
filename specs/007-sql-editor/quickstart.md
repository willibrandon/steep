# Quickstart: SQL Editor & Execution

**Feature**: 007-sql-editor | **Date**: 2025-11-25

## Prerequisites

1. Go 1.21+ installed
2. Steep built and running (`make build`)
3. PostgreSQL database connection configured

## File Structure

After implementation, the feature will add these files:

```
internal/ui/views/sqleditor/
├── view.go              # Main view (ViewModel interface)
├── editor.go            # Textarea wrapper
├── results.go           # Results table + pagination
├── statusbar.go         # Connection + TX state
├── history.go           # Query history (SQLite)
├── snippets.go          # Saved queries (YAML)
├── highlight.go         # SQL syntax highlighting
├── export.go            # CSV/JSON export
├── transaction.go       # Transaction state machine
├── help.go              # Help overlay
└── keys.go              # Key bindings

internal/ui/views/types.go         # Add ViewSQLEditor
internal/ui/styles/sqleditor.go    # Editor styles
internal/db/queries/sqleditor.go   # Query execution
```

## Key Patterns

### 1. View Implementation

```go
// internal/ui/views/sqleditor/view.go
package sqleditor

type SQLEditorView struct {
    // Bubbletea components
    editor   textarea.Model
    results  ResultsTable
    statusbar StatusBar
    help     help.Model

    // State
    focus    FocusArea
    executor *SessionExecutor
    history  *HistoryManager
    snippets *SnippetManager

    // Dimensions
    width, height int
}

func New(pool *pgxpool.Pool, readonly bool) *SQLEditorView {
    // Initialize components...
}

func (v *SQLEditorView) Init() tea.Cmd {
    return tea.Batch(
        v.editor.Blink(),
        v.history.Load(),
    )
}

func (v *SQLEditorView) Update(msg tea.Msg) (views.ViewModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return v.handleKey(msg)
    case QueryCompletedMsg:
        return v.handleQueryCompleted(msg)
    // ... other messages
    }
    return v, nil
}

func (v *SQLEditorView) View() string {
    // Render: statusbar + editor + results + footer
}
```

### 2. Query Execution

```go
// internal/db/queries/sqleditor.go
package queries

func ExecuteUserQuery(ctx context.Context, conn Queryable,
    sql string, timeout time.Duration) (*ExecutionResult, error) {

    queryCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    rows, err := conn.Query(queryCtx, sql)
    if err != nil {
        return nil, fmt.Errorf("query failed: %w", err)
    }
    defer rows.Close()

    // Extract column metadata
    fieldDescs := rows.FieldDescriptions()
    columns := make([]Column, len(fieldDescs))
    for i, fd := range fieldDescs {
        columns[i] = Column{
            Name:    fd.Name,
            TypeOID: fd.DataTypeOID,
        }
    }

    // Collect rows
    var resultRows [][]any
    for rows.Next() {
        values, _ := rows.Values()
        resultRows = append(resultRows, values)
    }

    return &ExecutionResult{
        Columns: columns,
        Rows:    resultRows,
    }, rows.Err()
}
```

### 3. Transaction State Machine

```go
// internal/ui/views/sqleditor/transaction.go
package sqleditor

type SessionExecutor struct {
    pool     *pgxpool.Pool
    tx       pgx.Tx
    inTx     bool
    readonly bool
}

func (se *SessionExecutor) Execute(ctx context.Context,
    sql string, timeout time.Duration) (*ExecutionResult, error) {

    stmtType := detectStatementType(sql)

    switch stmtType {
    case StmtBegin:
        return se.beginTransaction(ctx)
    case StmtCommit:
        return se.commitTransaction(ctx)
    case StmtRollback:
        return se.rollbackTransaction(ctx)
    default:
        return se.executeQuery(ctx, sql, timeout)
    }
}
```

### 4. History Persistence

```go
// internal/ui/views/sqleditor/history.go
package sqleditor

type HistoryManager struct {
    db       *sql.DB
    cache    []HistoryEntry // In-memory cache (last 100)
    position int            // Navigation position
}

func (h *HistoryManager) Add(entry HistoryEntry) error {
    // Skip if duplicate of last entry
    if len(h.cache) > 0 && h.cache[0].SQL == entry.SQL {
        return nil
    }

    _, err := h.db.Exec(`
        INSERT INTO query_history (query, duration_ms, row_count, error)
        VALUES (?, ?, ?, ?)
    `, entry.SQL, entry.DurationMs, entry.RowCount, entry.Error)

    // Update cache
    h.cache = append([]HistoryEntry{entry}, h.cache...)
    if len(h.cache) > 100 {
        h.cache = h.cache[:100]
    }

    return err
}
```

## Testing

### Unit Tests

```bash
# Run unit tests
go test ./internal/ui/views/sqleditor/... -v

# Run specific test
go test ./internal/ui/views/sqleditor/... -run TestHistoryNavigation
```

### Integration Tests

```bash
# Requires Docker for testcontainers
go test ./tests/integration/sqleditor_test.go -v -tags=integration
```

### Manual Testing Checklist

- [ ] Press '7' from Dashboard to open SQL Editor
- [ ] Type multi-line query, verify line numbers show
- [ ] Execute with Ctrl+Enter, verify results appear
- [ ] Navigate results with j/k, verify row highlighting
- [ ] Page through results with n/p
- [ ] Test Tab to switch focus between editor and results
- [ ] Test :begin/:commit/:rollback commands
- [ ] Test Up arrow at editor top recalls history
- [ ] Test :save and :load for snippets
- [ ] Test :export csv and :export json
- [ ] Test Esc to cancel running query
- [ ] Verify read-only mode blocks DDL/DML

## Key Bindings Reference

### Editor Focused
| Key | Action |
|-----|--------|
| Ctrl+Enter | Execute query |
| Tab | Focus results |
| Esc | Blur editor |
| Up (at line 1) | Previous history |
| Down (at last line) | Next history |
| Ctrl+L | Clear editor |

### Results Focused
| Key | Action |
|-----|--------|
| j/k | Navigate rows |
| n/p | Next/prev page |
| g/G | First/last row |
| y | Copy cell |
| Y | Copy row |
| Tab | Focus editor |

### Global
| Key | Action |
|-----|--------|
| :begin | Start transaction |
| :commit | Commit transaction |
| :rollback | Rollback |
| :save NAME | Save snippet |
| :load NAME | Load snippet |
| :export csv FILE | Export CSV |
| :export json FILE | Export JSON |
| h or ? | Show help |

## Configuration

Add to `~/.config/steep/config.yaml`:

```yaml
sql_editor:
  query_timeout: 30s      # Default query timeout
  page_size: 100          # Rows per page
  history_limit: 1000     # Max history entries
  auto_commit: false      # Auto-commit after each query
```

## Troubleshooting

### Query timeout immediately
- Check `query_timeout` config setting
- Verify database connection is stable
- Try simpler query first

### History not persisting
- Check `~/.config/steep/query_history.db` exists
- Check file permissions
- Run `sqlite3 ~/.config/steep/query_history.db ".tables"`

### Snippets not loading
- Check `~/.config/steep/snippets.yaml` syntax
- Validate YAML with online validator
- Check file permissions
