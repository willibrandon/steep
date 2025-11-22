# Quickstart: Query Performance Monitoring

**Feature**: 003-query-performance

## Prerequisites

1. Go 1.21+ installed
2. CGO enabled (for SQLite and pg_query_go)
3. PostgreSQL database accessible
4. GCC compiler (Linux: `build-essential`, macOS: Xcode CLI tools)

## Install Dependencies

```bash
go get github.com/pganalyze/pg_query_go/v5
go get github.com/honeycombio/honeytail
go get github.com/mattn/go-sqlite3
go get golang.design/x/clipboard
```

Note: First build takes ~3 minutes due to pg_query_go compilation.

## Verify Build

```bash
CGO_ENABLED=1 go build ./...
```

## PostgreSQL Configuration

### Enable Query Logging (Recommended)

Connect as superuser and run:

```sql
-- Check current setting
SHOW log_min_duration_statement;

-- Enable logging for all queries (0ms threshold)
ALTER SYSTEM SET log_min_duration_statement = '0';

-- Reload configuration
SELECT pg_reload_conf();

-- Verify log file location
SELECT pg_current_logfile();
```

### Alternative: Sampling Mode

If logging cannot be enabled (e.g., no superuser access), the feature automatically falls back to pg_stat_activity sampling. This provides limited data but works on any PostgreSQL instance.

## Quick Implementation Test

### 1. Test Query Fingerprinting

```go
package main

import (
    "fmt"
    pg_query "github.com/pganalyze/pg_query_go/v5"
)

func main() {
    query := "SELECT * FROM users WHERE id = 123"

    // Normalize
    normalized, err := pg_query.Normalize(query)
    if err != nil {
        panic(err)
    }
    fmt.Println("Normalized:", normalized)
    // Output: SELECT * FROM users WHERE id = $1

    // Fingerprint
    fp, err := pg_query.FingerprintToUInt64(query)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Fingerprint: %d\n", fp)
}
```

### 2. Test SQLite Storage

```go
package main

import (
    "database/sql"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    db, err := sql.Open("sqlite3", "file::memory:?_journal_mode=WAL")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Create table
    _, err = db.Exec(`
        CREATE TABLE query_stats (
            fingerprint INTEGER PRIMARY KEY,
            normalized_query TEXT NOT NULL,
            calls INTEGER DEFAULT 0
        )
    `)
    if err != nil {
        panic(err)
    }

    // Insert
    _, err = db.Exec(
        "INSERT INTO query_stats (fingerprint, normalized_query, calls) VALUES (?, ?, ?)",
        12345, "SELECT * FROM users WHERE id = $1", 10,
    )
    if err != nil {
        panic(err)
    }

    // Query
    var calls int
    err = db.QueryRow("SELECT calls FROM query_stats WHERE fingerprint = ?", 12345).Scan(&calls)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Calls: %d\n", calls)
}
```

### 3. Test Clipboard (if UI available)

```go
package main

import (
    "fmt"
    "golang.design/x/clipboard"
)

func main() {
    err := clipboard.Init()
    if err != nil {
        fmt.Println("Clipboard unavailable:", err)
        return
    }

    text := "SELECT * FROM users"
    clipboard.Write(clipboard.FmtText, []byte(text))
    fmt.Println("Query copied to clipboard")
}
```

## Development Workflow

1. **Start with data model**: Implement SQLite schema and QueryStatsStore interface
2. **Add fingerprinting**: Implement QueryFingerprinter using pg_query_go
3. **Build collector**: Implement log parsing with honeytail, then sampling fallback
4. **Create UI**: Build Queries view with tabbed interface
5. **Add features**: EXPLAIN, search, clipboard in priority order

## Running Tests

```bash
# Unit tests
go test ./internal/monitors/queries/... -v

# Integration tests (requires Docker for testcontainers)
go test ./tests/integration/queries/... -v
```

## Troubleshooting

### CGO Build Errors

```bash
# Ensure CGO is enabled
export CGO_ENABLED=1

# macOS: Install Xcode CLI tools
xcode-select --install

# Linux: Install build-essential
sudo apt-get install build-essential
```

### Log File Access Denied

```bash
# Check PostgreSQL log directory permissions
ls -la /var/lib/postgresql/*/main/log/

# Alternative: Configure log destination
ALTER SYSTEM SET log_directory = '/tmp/pg_logs';
ALTER SYSTEM SET log_filename = 'postgresql.log';
SELECT pg_reload_conf();
```

### Clipboard Not Working on Linux

```bash
# Install X11 development library
sudo apt-get install libx11-dev

# Ensure DISPLAY is set
echo $DISPLAY
```

## File Structure

After implementation, expected file structure:

```
internal/
├── monitors/
│   └── queries/
│       ├── monitor.go      # Main monitor goroutine
│       ├── collector.go    # Log/sampling collection
│       ├── fingerprint.go  # pg_query_go wrapper
│       └── stats.go        # Aggregation logic
├── storage/
│   └── sqlite/
│       ├── db.go           # Connection management
│       └── queries.go      # QueryStatsStore impl
└── ui/
    └── views/
        └── queries/
            ├── view.go     # Main view model
            ├── table.go    # Query table
            ├── tabs.go     # Tab navigation
            ├── explain.go  # EXPLAIN display
            └── search.go   # Search input
```
