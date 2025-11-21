# Quickstart: Dashboard & Activity Monitoring

**Feature**: 002-dashboard-activity
**Date**: 2025-11-21

## Prerequisites

### Required
- Go 1.21+
- PostgreSQL 11+ (running instance for development)
- Terminal with 256-color support (80x24 minimum)

### Recommended
- Docker (for testcontainers integration tests)
- pg_stat_statements extension enabled (for future features)

## Development Setup

### 1. Clone and Install Dependencies

```bash
cd /Users/brandon/src/steep
go mod download
```

### 2. PostgreSQL Development Database

Option A: Local PostgreSQL
```bash
# Ensure PostgreSQL is running
psql -c "SELECT version();"

# Set connection environment variables
export PGHOST=localhost
export PGPORT=5432
export PGDATABASE=postgres
export PGUSER=postgres
export PGPASSWORD=your_password
```

Option B: Docker PostgreSQL
```bash
docker run -d \
  --name steep-dev-db \
  -e POSTGRES_PASSWORD=devpassword \
  -p 5432:5432 \
  postgres:16

export PGPASSWORD=devpassword
```

### 3. Generate Test Activity

Create some connections to monitor:
```bash
# Terminal 1: Long-running query
psql -c "SELECT pg_sleep(300);" &

# Terminal 2: Idle in transaction
psql -c "BEGIN; SELECT 1;" &

# Terminal 3: Active query
psql -c "SELECT * FROM pg_stat_activity WHERE state = 'active';"
```

## Build and Run

### Build
```bash
make build
# or
go build -o bin/steep ./cmd/steep
```

### Run
```bash
./bin/steep
# or with explicit connection
./bin/steep --host localhost --port 5432 --dbname postgres --user postgres
```

### Run with Read-Only Mode
```bash
./bin/steep --readonly
```

## Development Workflow

### Run Tests

```bash
# Unit tests
go test ./internal/... -short

# Integration tests (requires Docker)
go test ./tests/integration/... -v

# All tests
go test ./...
```

### Lint and Format

```bash
# Format
go fmt ./...

# Lint (if golangci-lint installed)
golangci-lint run
```

### Manual UI Testing

1. Start the application
2. Verify dashboard metrics display correctly
3. Test keyboard navigation:
   - `j/k` or arrows to navigate table
   - `g/G` for top/bottom
   - `/` to filter
   - `s` to sort
   - `d` for query details
   - `c` to cancel query
   - `x` to terminate connection
   - `q` to quit
4. Verify color-coding for connection states
5. Test at minimum terminal size (80x24)

## Key Files for This Feature

### Source Code
```
internal/db/queries/activity.go    # pg_stat_activity queries
internal/db/queries/stats.go       # pg_stat_database queries
internal/db/models/connection.go   # Connection struct
internal/db/models/metrics.go      # Metrics struct
internal/monitors/activity.go      # Activity monitor goroutine
internal/monitors/stats.go         # Metrics monitor goroutine
internal/ui/views/dashboard.go     # Combined Dashboard/Activity view
internal/ui/components/panel.go    # Metrics panel component
internal/ui/components/table.go    # Activity table component
internal/ui/components/dialog.go   # Confirmation dialog
internal/ui/styles/colors.go       # Connection state colors
```

### Tests
```
tests/unit/metrics_test.go         # TPS calculation, cache ratio
tests/integration/activity_test.go # Query execution, data mapping
```

### Documentation
```
specs/002-dashboard-activity/spec.md        # Feature specification
specs/002-dashboard-activity/plan.md        # This implementation plan
specs/002-dashboard-activity/research.md    # Technical decisions
specs/002-dashboard-activity/data-model.md  # Entity definitions
```

## Debugging

### Enable Debug Logging
```bash
export STEEP_DEBUG=1
./bin/steep
```

### View Raw Query Results
```sql
-- Activity data
SELECT pid, usename, datname, state, query_start, query
FROM pg_stat_activity
WHERE state != 'idle'
ORDER BY backend_start DESC;

-- Metrics data
SELECT
  sum(xact_commit + xact_rollback) as total_xacts,
  sum(blks_hit)::float / nullif(sum(blks_hit + blks_read), 0) * 100 as cache_ratio
FROM pg_stat_database;
```

### Common Issues

**"Connection refused"**: Ensure PostgreSQL is running and accepting connections

**"Permission denied for pg_stat_activity"**: User needs pg_read_all_stats role or superuser

**"Terminal too small"**: Resize to at least 80x24

**"Colors not showing"**: Ensure TERM environment variable supports 256 colors (e.g., `xterm-256color`)

## Performance Validation

Before merging, validate:

1. **Query performance**: < 500ms for both activity and metrics queries
   ```bash
   psql -c "EXPLAIN ANALYZE SELECT ... FROM pg_stat_activity LIMIT 500"
   ```

2. **Memory usage**: < 50MB during normal operation
   ```bash
   # While running steep
   ps aux | grep steep
   ```

3. **Refresh rate**: UI updates smoothly at 1-second intervals with no visible lag
