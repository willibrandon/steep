# Research: Query Performance Monitoring

**Feature**: 003-query-performance
**Date**: 2025-11-21

## Executive Summary

All key dependencies are production-ready and actively maintained. Primary complexity is CGO requirement for pg_query_go, go-sqlite3, and clipboard (macOS). The honeytail parser does not fully support quoted identifiers in SQL but handles standard PostgreSQL logs well.

## Dependency Research

### 1. pg_query_go/v5 - Query Fingerprinting

**Decision**: Use github.com/pganalyze/pg_query_go/v5 for query normalization and fingerprinting

**Rationale**: Industry-standard library used by pganalyze, provides consistent fingerprinting across same query patterns with different parameters

**Usage Pattern**:
```go
import "github.com/pganalyze/pg_query_go/v5"

// Normalize: Replace constants with placeholders
normalized, err := pg_query.Normalize("SELECT * FROM users WHERE id = 1")
// Result: "SELECT * FROM users WHERE id = $1"

// Fingerprint as 64-bit integer for efficient storage/comparison
fpInt, err := pg_query.FingerprintToUInt64("SELECT * FROM users WHERE id = ?")
```

**Alternatives Considered**:
- Manual regex normalization: Rejected - unreliable, doesn't handle complex SQL
- pg_stat_statements normalization: Requires extension we're avoiding

**Gotchas**:
- Requires CGO (binds to PostgreSQL source code)
- Initial builds take ~3 minutes (CI consideration)
- Unsanitized input to deparser can crash

---

### 2. honeytail PostgreSQL Parser - Log Parsing

**Decision**: Use github.com/honeycombio/honeytail/parsers/postgresql for log file parsing

**Rationale**: Purpose-built for PostgreSQL log format, handles multi-line queries, extracts duration/user/database metadata

**Usage Pattern**:
```go
import "github.com/honeycombio/honeytail/parsers/postgresql"

parser := &postgresql.Parser{}
opts := postgresql.Options{
    LogLinePrefix: "%t [%p]: [%l-1] user=%u,db=%d,app=%a,client=%h ",
}
parser.Init(opts)

// Process log lines via channels
go parser.ProcessLines(logLines, events, prefixRegex)
```

**Alternatives Considered**:
- Custom regex parser: Rejected - complex to maintain, error-prone
- pgBadger: Rejected - Perl tool, not embeddable in Go

**Gotchas**:
- Does NOT support quoted table/column names (e.g., `"Table"."Column"`)
- Requires matching log_line_prefix configuration
- Part of larger honeytail framework

---

### 3. go-sqlite3 - Statistics Persistence

**Decision**: Use github.com/mattn/go-sqlite3 for embedded SQLite storage

**Rationale**: Most mature SQLite driver for Go, good performance with WAL mode for concurrent access

**Usage Pattern**:
```go
import (
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

// Open with WAL mode for better concurrency
db, err := sql.Open("sqlite3",
    "file:~/.config/steep/query_stats.db?_journal_mode=WAL&_busy_timeout=5000")

// Use prepared statements for repeated queries
stmt, _ := db.Prepare("INSERT INTO query_stats (fingerprint, query, calls, total_time) VALUES (?, ?, ?, ?)")
```

**Alternatives Considered**:
- modernc.org/sqlite: Pure Go but slower, less battle-tested
- BoltDB/BadgerDB: Key-value stores don't support SQL queries for top-N sorting

**Gotchas**:
- Requires CGO - `CGO_ENABLED=1` and GCC compiler
- Platform-specific build requirements (gcc on Linux, Xcode on macOS)
- Use `_busy_timeout` to handle lock contention

---

### 4. golang.design/x/clipboard - Clipboard Access

**Decision**: Use golang.design/x/clipboard for copy-to-clipboard functionality

**Rationale**: Cross-platform support (macOS/Linux/Windows), simple API, actively maintained

**Usage Pattern**:
```go
import "golang.design/x/clipboard"

// Initialize once at startup
err := clipboard.Init()

// Copy query text
clipboard.Write(clipboard.FmtText, []byte(queryText))
```

**Alternatives Considered**:
- atotto/clipboard: Less actively maintained
- OS-specific commands (pbcopy, xclip): Requires external tools

**Gotchas**:
- CGO required on macOS
- Linux requires X11 (`libx11-dev`), Wayland needs XWayland bridge
- Must handle Init() failure gracefully (headless servers)

---

### 5. PostgreSQL Log Configuration

**Decision**: Detect and optionally auto-enable logging via ALTER SYSTEM

**Rationale**: `log_min_duration_statement` is reloadable (no restart), provides complete query history

**Detection Pattern**:
```sql
SHOW log_min_duration_statement;
-- Returns '-1' if disabled

SELECT pg_current_logfile();
-- Returns path like 'log/postgresql-2025-11-21_120000.log'
```

**Auto-Enable Pattern**:
```sql
ALTER SYSTEM SET log_min_duration_statement = '0';
SELECT pg_reload_conf();
```

**Alternatives Considered**:
- `auto_explain`: Requires extension, more overhead
- `pg_stat_statements`: Explicitly avoided per requirements

**Gotchas**:
- Requires superuser for ALTER SYSTEM
- Log file path may be relative to data directory
- `log_statement` should be 'none' to avoid duplicate logging

---

### 6. pg_stat_activity Sampling Fallback

**Decision**: Poll pg_stat_activity every refresh cycle when logging unavailable

**Rationale**: Works on all PostgreSQL instances including remote, no configuration needed

**Sampling Pattern**:
```sql
SELECT
    pid,
    query,
    now() - query_start as duration,
    state
FROM pg_stat_activity
WHERE state != 'idle'
  AND query NOT LIKE 'FETCH%'  -- Exclude own queries
  AND pid != pg_backend_pid();
```

**Limitations**:
- Only captures active queries at poll time (misses fast queries)
- Duration is elapsed time, not total execution time
- No historical data - snapshot only

---

## Architecture Decisions

### Data Flow Pipeline

```
PostgreSQL Logs ─┬─→ honeytail parser ─→ Query Event
                 │
pg_stat_activity ┴─→ Sampling collector ─→ Query Event
                                               │
                                               ▼
                                    pg_query_go fingerprint
                                               │
                                               ▼
                                      SQLite aggregation
                                               │
                                               ▼
                                       UI view queries
```

### SQLite Schema Design

```sql
CREATE TABLE query_stats (
    fingerprint     INTEGER PRIMARY KEY,  -- pg_query FingerprintToUInt64
    normalized_query TEXT NOT NULL,       -- Normalized SQL text
    calls           INTEGER DEFAULT 0,
    total_time_ms   REAL DEFAULT 0,
    min_time_ms     REAL,
    max_time_ms     REAL,
    total_rows      INTEGER DEFAULT 0,
    first_seen      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_total_time ON query_stats(total_time_ms DESC);
CREATE INDEX idx_calls ON query_stats(calls DESC);
CREATE INDEX idx_total_rows ON query_stats(total_rows DESC);
CREATE INDEX idx_last_seen ON query_stats(last_seen);
```

### Retention Strategy

- Cleanup on each refresh cycle: `DELETE FROM query_stats WHERE last_seen < datetime('now', '-7 days')`
- Simple time-based retention avoids complex aggregation
- 7-day window sufficient for typical analysis patterns

## Build Considerations

### CGO Dependencies

All libraries with CGO are already part of Steep's dependency chain (pgx uses native protocol). New CGO requirements:
- go-sqlite3: Adds SQLite C library
- pg_query_go: Adds libpg_query

**CI Impact**: Initial builds ~3 minutes for pg_query_go compilation. Subsequent builds use cache.

**Platform Requirements**:
- macOS: Xcode command line tools (standard)
- Linux: gcc, build-essential
- Alpine: gcc, musl-dev

## Risk Assessment

| Component | Risk | Mitigation |
|-----------|------|------------|
| honeytail quoted identifiers | Medium | Log unparseable lines, don't fail silently |
| Clipboard on headless | Low | Graceful degradation with status message |
| Log file permissions | Medium | Clear error message with chmod guidance |
| CGO build complexity | Low | Document in contributing guide |
