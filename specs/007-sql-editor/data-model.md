# Data Model: SQL Editor & Execution

**Feature**: 007-sql-editor | **Date**: 2025-11-25

## Entities

### 1. Query

Represents a SQL query entered by the user with execution metadata.

```go
// internal/ui/views/sqleditor/models.go

// Query represents a SQL query with execution metadata
type Query struct {
    SQL          string        // The SQL text
    ExecutedAt   time.Time     // When query was executed
    Duration     time.Duration // Execution time
    RowCount     int64         // Number of rows returned/affected
    Error        string        // Error message if failed
    Cancelled    bool          // Whether query was cancelled
}
```

**Validation Rules**:
- SQL must not be empty
- SQL must not exceed 100KB (CharLimit)
- ExecutedAt set automatically on execution

**State Transitions**:
```
[Empty] -> [Editing] -> [Executing] -> [Completed | Error | Cancelled]
                ^                              |
                |______________________________|
```

---

### 2. ResultSet

Collection of rows and columns returned by a query.

```go
// ResultSet holds query results for display
type ResultSet struct {
    Columns      []Column       // Column metadata
    Rows         [][]string     // Row data (pre-formatted for display)
    TotalRows    int            // Total row count (before pagination)
    CurrentPage  int            // Current page (1-indexed)
    PageSize     int            // Rows per page (default 100)
    ExecutionMs  int64          // Query execution time in milliseconds
}

// Column represents a result column
type Column struct {
    Name     string // Column name from query
    TypeOID  uint32 // PostgreSQL type OID
    TypeName string // Human-readable type name (e.g., "integer", "text")
    Width    int    // Display width for TUI table
}
```

**Derived Properties**:
- `TotalPages = ceil(TotalRows / PageSize)`
- `HasNextPage = CurrentPage < TotalPages`
- `HasPrevPage = CurrentPage > 1`
- `StartRow = (CurrentPage - 1) * PageSize + 1`
- `EndRow = min(CurrentPage * PageSize, TotalRows)`

---

### 3. TransactionState

Tracks the current database transaction state.

```go
// TransactionState represents transaction context
type TransactionState struct {
    Active         bool          // Whether in a transaction
    StartedAt      time.Time     // When transaction began
    SavepointStack []string      // Nested savepoint names
    IsolationLevel string        // READ COMMITTED, SERIALIZABLE, etc.
}

// TransactionStateType for status bar display
type TransactionStateType int

const (
    TxNone TransactionStateType = iota
    TxActive
    TxAborted
)
```

**State Transitions**:
```
[None] --BEGIN--> [Active] --COMMIT--> [None]
                     |
                     +--ROLLBACK--> [None]
                     |
                     +--ERROR--> [Aborted] --ROLLBACK--> [None]
                     |
                     +--SAVEPOINT--> [Active + Savepoint]
                           |
                           +--ROLLBACK TO--> [Active]
```

---

### 4. HistoryEntry

Executed query stored for recall.

```go
// HistoryEntry represents a query in history
type HistoryEntry struct {
    ID         int64         // SQLite row ID
    SQL        string        // Query text
    ExecutedAt time.Time     // Execution timestamp
    DurationMs int64         // Execution duration
    RowCount   int64         // Rows returned/affected
    Error      string        // Error if failed (empty if success)
}
```

**SQLite Schema**:
```sql
CREATE TABLE IF NOT EXISTS query_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query TEXT NOT NULL,
    executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER DEFAULT 0,
    row_count INTEGER DEFAULT 0,
    error TEXT DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_history_executed_at
    ON query_history(executed_at DESC);

-- Limit history to 1000 entries (cleanup on insert)
CREATE TRIGGER IF NOT EXISTS limit_history_size
AFTER INSERT ON query_history
BEGIN
    DELETE FROM query_history
    WHERE id NOT IN (
        SELECT id FROM query_history
        ORDER BY executed_at DESC
        LIMIT 1000
    );
END;
```

**Identity & Uniqueness**:
- ID is auto-incremented primary key
- Consecutive duplicate queries are not stored (deduplication on insert)

---

### 5. Snippet

Named query saved for reuse.

```go
// Snippet represents a saved query
type Snippet struct {
    Name        string    // Unique identifier
    Description string    // Optional description
    SQL         string    // Query text
    CreatedAt   time.Time // When snippet was created
    UpdatedAt   time.Time // When snippet was last modified
}
```

**YAML Storage** (`~/.config/steep/snippets.yaml`):
```yaml
snippets:
  - name: "active-queries"
    description: "Show currently running queries"
    sql: |
      SELECT pid, usename, query, now() - query_start as duration
      FROM pg_stat_activity
      WHERE state = 'active'
    created_at: "2025-11-25T10:00:00Z"
    updated_at: "2025-11-25T10:00:00Z"
```

**Identity & Uniqueness**:
- Name must be unique (case-insensitive)
- Name must match pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$`
- Name max length: 50 characters

---

### 6. EditorState

UI state for the SQL editor component.

```go
// EditorState represents the editor's UI state
type EditorState struct {
    Focus         FocusArea     // Which pane has focus
    SplitRatio    float64       // Editor/results split (0.0-1.0)
    HistoryIndex  int           // Current position in history (-1 = not browsing)
    ShowHelp      bool          // Help overlay visible
    CommandBuffer string        // For : commands
}

// FocusArea indicates which component has keyboard focus
type FocusArea int

const (
    FocusEditor FocusArea = iota
    FocusResults
    FocusCommandLine
    FocusSnippetBrowser
    FocusHistorySearch
)
```

---

## Relationships

```
┌─────────────┐
│   Query     │───executes───>┌─────────────┐
└─────────────┘               │  ResultSet  │
       │                      └─────────────┘
       │ stores in
       v
┌─────────────┐
│HistoryEntry │
└─────────────┘

┌─────────────┐
│  Snippet    │───loads into──>┌─────────────┐
└─────────────┘                │   Query     │
                               └─────────────┘

┌─────────────────┐
│TransactionState │───affects───>┌─────────────┐
└─────────────────┘              │   Query     │
                                 │ (execution) │
                                 └─────────────┘
```

---

## Data Volume Assumptions

| Entity | Expected Volume | Retention |
|--------|-----------------|-----------|
| Query (in-memory) | 1 active | Per session |
| ResultSet | 1 active, up to 10K rows | Per query |
| TransactionState | 1 active | Per session |
| HistoryEntry | Max 1000 | Persistent (SQLite) |
| Snippet | Max 100 | Persistent (YAML) |
| EditorState | 1 | Per session |

---

## Type OID Reference

Common PostgreSQL type OIDs for display formatting:

| OID | Type Name | Display Format |
|-----|-----------|----------------|
| 16 | bool | "true"/"false" |
| 20 | int8 | Integer |
| 21 | int2 | Integer |
| 23 | int4 | Integer |
| 25 | text | String |
| 700 | float4 | 2 decimal places |
| 701 | float8 | 2 decimal places |
| 1043 | varchar | String |
| 1082 | date | YYYY-MM-DD |
| 1114 | timestamp | YYYY-MM-DD HH:MM:SS |
| 1184 | timestamptz | YYYY-MM-DD HH:MM:SS TZ |
| 2950 | uuid | UUID string |
| 114 | json | JSON string |
| 3802 | jsonb | JSON string |
