# Quickstart: Replication Monitoring & Setup

**Feature**: 006-replication-monitoring
**Date**: 2025-11-24

## Prerequisites

- Go 1.21+ installed
- PostgreSQL 11+ databases (primary and standby for full testing)
- Steep development environment set up

## Development Setup

### 1. Branch Setup

```bash
# Already on feature branch
git branch
# * 006-replication-monitoring

# Install new dependencies
go get github.com/charmbracelet/huh
go get github.com/guptarohit/asciigraph
go get github.com/sethvargo/go-password/password

# Ensure dependencies are up to date
go mod tidy
```

### 2. Test Database Setup (Standalone Primary)

For initial development, a standalone PostgreSQL with replication-ready configuration:

```sql
-- Connect to PostgreSQL
psql -U postgres

-- Check current replication config
SHOW wal_level;           -- Should be 'replica' or 'logical'
SHOW max_wal_senders;     -- Should be > 0
SHOW max_replication_slots; -- Should be > 0

-- If not configured, set parameters (requires restart):
ALTER SYSTEM SET wal_level = 'replica';
ALTER SYSTEM SET max_wal_senders = 10;
ALTER SYSTEM SET max_replication_slots = 10;
-- Then restart PostgreSQL

-- Create a replication user for testing
CREATE USER repl_user REPLICATION LOGIN PASSWORD 'test_password';

-- Create a replication slot for testing
SELECT pg_create_physical_replication_slot('test_slot');

-- For logical replication testing:
ALTER SYSTEM SET wal_level = 'logical';
-- Restart required

-- Create test publication
CREATE DATABASE steep_repl_test;
\c steep_repl_test

CREATE TABLE test_table (
    id SERIAL PRIMARY KEY,
    data TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE PUBLICATION test_pub FOR TABLE test_table;
```

### 3. Test with Docker Replication Cluster (Recommended)

For full replication testing with primary + replica:

```bash
# Create network
docker network create pg-repl-test

# Start primary
docker run -d --name pg-primary --network pg-repl-test \
  -e POSTGRES_PASSWORD=test \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  -p 5432:5432 \
  postgres:16 \
  -c wal_level=replica \
  -c max_wal_senders=10 \
  -c max_replication_slots=10 \
  -c hot_standby=on

# Wait for primary to start
sleep 5

# Create replication user and slot on primary
docker exec pg-primary psql -U postgres -c \
  "CREATE USER replicator REPLICATION LOGIN PASSWORD 'repl_pass';"
docker exec pg-primary psql -U postgres -c \
  "SELECT pg_create_physical_replication_slot('replica1_slot');"

# Take base backup for replica
docker exec pg-primary pg_basebackup -U replicator -D /tmp/replica_data -Fp -Xs -P -R

# Start replica from backup
docker run -d --name pg-replica --network pg-repl-test \
  -e POSTGRES_PASSWORD=test \
  -p 5433:5432 \
  postgres:16 \
  -c primary_conninfo="host=pg-primary user=replicator password=repl_pass" \
  -c primary_slot_name=replica1_slot

# Verify replication is working
docker exec pg-primary psql -U postgres -c "SELECT * FROM pg_stat_replication;"
```

### 4. Configure Steep for Test Database

Steep uses configuration files, not command-line DSN. Configure via:

**Option A: Environment Variables**

```bash
# For primary (port 15432 from repl-test-setup.sh)
export STEEP_CONNECTION_HOST=localhost
export STEEP_CONNECTION_PORT=15432
export STEEP_CONNECTION_USER=postgres
export STEEP_CONNECTION_DATABASE=postgres
export PGPASSWORD=postgres

# Build and run
make build
./bin/steep

# For replica (port 15433), change port:
export STEEP_CONNECTION_PORT=15433
./bin/steep

# For readonly mode
./bin/steep --readonly
```

**Option B: Config File**

Create or edit `~/.config/steep/config.yaml` or `./config.yaml`:

```yaml
# For replication test environment (primary)
connection:
  host: localhost
  port: 15432  # Primary from repl-test-setup.sh
  database: postgres
  user: postgres
  sslmode: disable
  pool_max_conns: 10
  pool_min_conns: 2

ui:
  theme: dark
  refresh_interval: 2s

debug: true
```

To test with replica, change port to 15433.

### 5. Access Replication View

1. Launch Steep
2. Press `6` to open Replication view
3. Use `Tab` to switch between tabs (Overview, Slots, Logical, Setup)
4. Press `t` to toggle topology view
5. Press `h` for help

## File Structure

Files to create for this feature:

```
internal/
├── db/
│   ├── models/
│   │   └── replication.go        # NEW
│   └── queries/
│       └── replication.go        # NEW
├── monitors/
│   └── replication.go            # NEW
├── storage/sqlite/
│   ├── schema.go                 # MODIFY (add lag_history table)
│   └── replication_store.go      # NEW
└── ui/
    ├── components/
    │   └── sparkline.go          # NEW
    └── views/
        └── replication/
            ├── view.go           # NEW
            ├── tabs.go           # NEW
            ├── help.go           # NEW
            ├── repviz/
            │   ├── topology.go   # NEW
            │   └── pipeline.go   # NEW
            └── setup/
                ├── config_check.go     # NEW
                ├── physical_wizard.go  # NEW
                ├── logical_wizard.go   # NEW
                └── connstring.go       # NEW
```

## Implementation Order

1. **Data Models** (`internal/db/models/replication.go`)
   - Define Replica, ReplicationSlot, Publication, Subscription structs
   - Define enums (SyncState, SlotType, LagSeverity)
   - Add helper functions (FormatByteLag, LagSeverity, etc.)

2. **Database Queries** (`internal/db/queries/replication.go`)
   - GetReplicas (pg_stat_replication)
   - GetSlots (pg_replication_slots)
   - GetPublications (pg_publication)
   - GetSubscriptions (pg_subscription)
   - GetWALReceiverStatus (pg_stat_wal_receiver)
   - GetReplicationConfig (pg_settings)
   - CreateReplicationUser, DropSlot, etc.

3. **SQLite Storage** (`internal/storage/sqlite/`)
   - Add replication_lag_history table to schema.go
   - Create replication_store.go for lag history persistence

4. **Monitor** (`internal/monitors/replication.go`)
   - ReplicationMonitor with 2-second refresh
   - Lag history ring buffer
   - SQLite persistence at intervals

5. **UI Components** (`internal/ui/components/sparkline.go`)
   - Sparkline component using asciigraph
   - Unicode block alternative for compact display

6. **View Implementation** (`internal/ui/views/replication/`)
   - view.go: Main ViewModel implementation
   - tabs.go: Overview, Slots, Logical, Setup tabs
   - help.go: Keyboard shortcuts reference

7. **Visualization** (`internal/ui/views/replication/repviz/`)
   - topology.go: ASCII tree using treeprint
   - pipeline.go: WAL pipeline stages visualization

8. **Setup Wizards** (`internal/ui/views/replication/setup/`)
   - config_check.go: Configuration readiness checker
   - physical_wizard.go: Physical replication wizard using huh
   - logical_wizard.go: Logical replication wizard
   - connstring.go: Connection string builder

9. **App Integration** (`internal/app/app.go`)
   - Register `6` key for Replication view
   - Add ReplicationView to view map

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run replication-specific tests
go test ./internal/db/queries/... -run Replication
go test ./internal/db/models/... -run Replica
go test ./internal/storage/sqlite/... -run LagHistory
go test ./internal/ui/views/replication/...
```

### Integration Tests

```bash
# Start test containers (primary + replica)
# Use the Docker setup from section 3 above

# Run integration tests
go test ./internal/db/queries/... -tags=integration -run Replication

# Clean up
docker stop pg-primary pg-replica
docker rm pg-primary pg-replica
docker network rm pg-repl-test
```

### Manual Testing Checklist

**Overview Tab:**
- [ ] Press `6` opens Replication view
- [ ] Replicas display with Name, State, Sync, Byte Lag, Time Lag
- [ ] Lag color-coded: green (<1MB), yellow (1-10MB), red (>10MB)
- [ ] `j`/`k` navigation works
- [ ] `t` toggles topology view
- [ ] Sparklines show lag history
- [ ] 2-second auto-refresh works

**Slots Tab:**
- [ ] `Tab` switches to Slots tab
- [ ] Slots display Name, Type, Active, Retained WAL
- [ ] Inactive slots highlighted
- [ ] `d` to drop slot (with confirmation, non-readonly)

**Logical Tab:**
- [ ] Publications show table counts
- [ ] Subscriptions show enabled status and lag
- [ ] "No logical replication" message when not configured

**Setup Tab:**
- [ ] Configuration checker shows param status
- [ ] Green checkmarks for correct config
- [ ] Red X with guidance for issues
- [ ] Physical wizard generates pg_basebackup commands
- [ ] Logical wizard generates CREATE PUBLICATION/SUBSCRIPTION
- [ ] Connection string builder with live preview
- [ ] Copy to clipboard works
- [ ] Setup blocked in readonly mode

**General:**
- [ ] `h` shows help overlay
- [ ] `Esc` closes overlays/wizards
- [ ] Works on standby (shows WAL receiver info)
- [ ] Graceful handling when replication not configured

## Common Issues

### "Replication not configured" Message

Ensure PostgreSQL has replication enabled:

```sql
SHOW wal_level; -- Must be 'replica' or 'logical'
ALTER SYSTEM SET wal_level = 'replica';
-- Restart PostgreSQL
```

### No Replicas Shown

Verify on primary:
1. Check `pg_stat_replication` has rows
2. Replica is connected and streaming
3. User has permission to view replication stats

### Permission Denied for Setup

Setup operations require superuser:

```sql
-- Check if superuser
SELECT current_user, usesuper FROM pg_user WHERE usename = current_user;

-- Or use pg_monitor role for read-only monitoring
GRANT pg_monitor TO your_user;
```

### Slow Queries (>500ms)

The replication queries are lightweight (system views with no joins to user tables). If slow:
1. Check network latency to database
2. Ensure no locks on system catalog tables
3. Verify database is not under extreme load

## Reference Implementation

Study existing views for patterns:

- `internal/ui/views/locks/view.go` - Modal overlays, confirmation dialogs, treeprint usage
- `internal/ui/views/tables/view.go` - Multi-tab layout, hierarchical tree display
- `internal/monitors/locks.go` - Monitor goroutine pattern
- `internal/storage/sqlite/schema.go` - SQLite table patterns

## New Library References

- **huh forms**: https://github.com/charmbracelet/huh
- **asciigraph**: https://github.com/guptarohit/asciigraph
- **go-password**: https://github.com/sethvargo/go-password
