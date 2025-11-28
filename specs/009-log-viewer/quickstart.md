# Quickstart: Log Viewer

**Feature**: 009-log-viewer
**Date**: 2025-11-27

## Prerequisites

1. **PostgreSQL logging enabled** (or accept prompt to enable):
   ```sql
   -- Check current logging status
   SHOW logging_collector;     -- should be 'on'
   SHOW log_destination;       -- should include 'stderr', 'csvlog', or 'jsonlog'
   SHOW log_directory;         -- log file location
   ```

2. **Access to log files**:
   - File system read access to PostgreSQL log directory, OR
   - Database role with `pg_read_server_files` privilege for `pg_read_file()`

3. **Steep application running** with connection to PostgreSQL server

## Usage

### Opening Log Viewer

Press `9` from any view to open the Log Viewer.

### Basic Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Scroll down one line |
| `k` / `↑` | Scroll up one line |
| `g` | Jump to oldest log (top) |
| `G` | Jump to newest log (bottom) |
| `Ctrl+d` | Page down |
| `Ctrl+u` | Page up |

### Follow Mode

| Key | Action |
|-----|--------|
| `f` | Toggle follow mode on/off |

When follow mode is ON:
- New log entries appear automatically
- View auto-scrolls to show newest logs
- Indicator shows "FOLLOW" in status bar

When follow mode is OFF:
- View stays at current position
- User can scroll freely through history
- Manual scroll does not affect position

### Filtering by Severity

| Command | Effect |
|---------|--------|
| `:level error` | Show only ERROR, FATAL, PANIC entries |
| `:level warning` | Show only WARNING entries |
| `:level info` | Show only LOG, INFO, NOTICE entries |
| `:level debug` | Show only DEBUG entries |
| `:level all` | Clear severity filter (show all) |
| `:level clear` | Same as `:level all` |

### Text Search

| Key | Action |
|-----|--------|
| `/` | Start search (enter pattern) |
| `Enter` | Apply search pattern |
| `n` | Jump to next match |
| `N` | Jump to previous match |
| `Escape` | Clear search |

Search supports regex patterns:
- `error` - Find lines containing "error"
- `connection.*timeout` - Find connection timeout messages
- `pid=[0-9]+` - Find lines with PID numbers

### Timestamp Navigation

| Command | Effect |
|---------|--------|
| `:goto 2025-11-27 14:30` | Jump to logs around timestamp |
| `:goto 14:30` | Jump to today's logs at time |

### Other Commands

| Key | Action |
|-----|--------|
| `?` | Show help overlay |
| `Escape` | Close help / clear input |
| `q` | Return to previous view |

## Log Entry Format

Each log line displays:
```
[TIMESTAMP] [SEVERITY] [PID] MESSAGE
```

Example:
```
2025-11-27 14:30:15 ERROR  [12345] connection to database "mydb" failed
2025-11-27 14:30:14 WARN   [12346] parameter "work_mem" is deprecated
2025-11-27 14:30:13 INFO   [12347] database system is ready to accept connections
2025-11-27 14:30:12 DEBUG  [12348] checkpoint starting: time
```

## Color Coding

| Severity | Color | Meaning |
|----------|-------|---------|
| ERROR | Red | Errors requiring attention |
| WARNING | Yellow | Potential issues |
| INFO | White | Normal operations |
| DEBUG | Gray | Diagnostic information |

## Status Bar

The status bar shows:
- **Follow mode**: `FOLLOW` or `PAUSED`
- **Active filter**: e.g., `[level:error]` or `[search:/timeout/]`
- **Entry count**: `1,234 / 10,000` (filtered / total)
- **Last update**: `Updated: 14:30:15`

## Troubleshooting

### "Logging not enabled"

If you see a prompt to enable logging:
1. Press `y` to enable (recommended) - Steep will configure logging automatically
2. Press `n` to skip - You'll see manual configuration instructions

Manual configuration:
```sql
ALTER SYSTEM SET logging_collector = on;
ALTER SYSTEM SET log_destination = 'stderr,csvlog';
ALTER SYSTEM SET log_directory = 'log';
SELECT pg_reload_conf();  -- Apply changes
```

### "Cannot access log files"

Ensure Steep has file system access:
```bash
# Check log directory permissions
ls -la $(psql -c "SHOW data_directory" -t)/log/

# Or use pg_read_file (requires privilege)
GRANT pg_read_server_files TO your_user;
```

### "No log entries found"

1. Check if logging is enabled: `SHOW logging_collector;`
2. Check log directory exists: `SHOW log_directory;`
3. Generate some log output: `SELECT pg_sleep(0.1);` (if log_min_duration_statement is set)

## Examples

### Monitor for errors in real-time
1. Open Log Viewer: `9`
2. Filter to errors: `:level error`
3. Enable follow mode: `f`

### Investigate a past incident
1. Open Log Viewer: `9`
2. Disable follow mode: `f`
3. Jump to time: `:goto 2025-11-27 10:00`
4. Search for pattern: `/connection refused`
5. Navigate matches: `n` / `N`

### Find slow queries
1. Open Log Viewer: `9`
2. Search for duration: `/duration: [0-9]{4,}`
3. Navigate matches: `n` / `N`
