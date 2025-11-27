# Quickstart: Configuration Viewer

**Feature**: 008-configuration-viewer
**Date**: 2025-11-27

## Prerequisites

- Go 1.21+ installed
- PostgreSQL 11+ running and accessible
- Steep repository cloned

## Build & Run

```bash
# From repository root
cd /Users/brandon/src/steep

# Build
make build

# Run with connection string
./bin/steep --dsn "postgres://user:pass@localhost:5432/dbname"

# Or with environment variables
export PGHOST=localhost
export PGPORT=5432
export PGDATABASE=mydb
export PGUSER=myuser
export PGPASSWORD=mypass
./bin/steep
```

## Access Configuration Viewer

1. Launch Steep and connect to a PostgreSQL server
2. Press `8` from any view to open the Configuration Viewer
3. Use `j/k` or arrow keys to navigate parameters
4. Press `/` to search by parameter name
5. Press `d` to view parameter details
6. Press `h` for help

## Quick Test Queries

Test the underlying pg_settings query:

```sql
-- Count parameters
SELECT COUNT(*) FROM pg_settings;
-- Expected: ~350

-- Check modified parameters
SELECT COUNT(*) FROM pg_settings WHERE setting != boot_val;

-- List categories
SELECT DISTINCT category FROM pg_settings ORDER BY category;

-- Find memory parameters
SELECT name, setting, unit
FROM pg_settings
WHERE category LIKE '%Memory%'
ORDER BY name;
```

## Development Workflow

### Run Tests

```bash
# Unit tests
go test ./internal/db/models/... -v

# Integration tests (requires Docker)
go test ./tests/integration/... -v -tags=integration
```

### Build and Test Incrementally

```bash
# Build only
make build

# Run with debug logging
./bin/steep --dsn "postgres://localhost/postgres" --log-level debug
```

## File Structure for Implementation

```
internal/
├── db/
│   ├── models/
│   │   └── config.go           # NEW: Parameter, ConfigData models
│   └── queries/
│       └── config.go           # NEW: GetAllParameters query
├── monitors/
│   └── config.go               # NEW: Config monitor (60s refresh)
└── ui/
    └── views/
        ├── types.go            # MODIFY: Add ViewConfig
        └── config/             # NEW: View package
            ├── view.go         # Main view implementation
            ├── help.go         # Help panel content
            └── export.go       # Export functionality
```

## Key Implementation Notes

1. **Write Commands**: Use `:set <param> <value>` to change parameters (ALTER SYSTEM), `:reset <param>` to restore defaults, and `:reload` to apply sighup-context changes. Note: postmaster-context changes require a server restart.

2. **Yellow Highlighting**: Parameters where `setting != boot_val` should be highlighted yellow to indicate customization.

3. **Auto-Refresh**: Refresh every 60 seconds (configuration rarely changes).

4. **Search**: Case-insensitive search on `name` and `short_desc` fields.

5. **Export Command**: `:export config <filename>` exports current view to file.

## Validation Checklist

- [x] View opens with `8` key
- [x] All ~350 parameters displayed
- [x] Modified parameters highlighted yellow
- [x] Pending restart parameters highlighted red with "!" prefix
- [x] Search (`/`) filters by name/description
- [x] Category filter works
- [x] Detail view (`d`) shows full info
- [x] Sort by name/category works
- [x] Export creates valid file (`:export config <file>`)
- [x] Help (`h`) shows keybindings
- [x] 60-second auto-refresh works
- [x] Manual refresh with `r` key
- [x] `:set <param> <value>` changes parameter
- [x] `:reset <param>` resets to default
- [x] `:reload` applies sighup-context changes
- [x] Clipboard copy with `y` (name) and `Y` (value)
- [x] Read-only mode blocks write operations
