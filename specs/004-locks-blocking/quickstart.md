# Quickstart: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## Prerequisites

1. **Go 1.21+** installed
2. **PostgreSQL 11+** running with access credentials
3. **Steep** built: `make build`

## New Dependencies

Add treeprint library:
```bash
go get github.com/xlab/treeprint
```

## Implementation Order

### P1 Stories (Core Functionality)

1. **View Active Locks** (User Story 1)
   - Create `internal/db/models/lock.go` with Lock struct
   - Implement `internal/db/queries/locks.go` GetLocks()
   - Add `internal/monitors/locks.go` LocksMonitor
   - Create `internal/ui/views/locks/view.go` implementing ViewModel
   - Wire `5` key to switch to locks view

2. **Identify Blocking Queries** (User Story 2)
   - Add BlockingRelationship struct to models
   - Implement GetBlockingRelationships() query
   - Build blocking/blocked PID maps in monitor
   - Add color coding to table rows (red=blocked, yellow=blocking)

### P2 Stories (Enhanced Functionality)

3. **Visualize Lock Dependency Tree** (User Story 3)
   - Add BlockingChain struct
   - Create `internal/ui/components/lock_tree.go` with treeprint
   - Build tree from blocking relationships
   - Display below lock table

4. **Kill Blocking Query** (User Story 4)
   - Implement TerminateBackend() query
   - Add confirmation dialog component
   - Wire `x` key with readonly mode check
   - Handle success/failure messages

### P3 Stories (Deferred)

5. **Deadlock History** (User Story 5) - May require additional storage

## Key Files to Create

```bash
# Models
touch internal/db/models/lock.go

# Queries
touch internal/db/queries/locks.go

# Monitor
touch internal/monitors/locks.go

# UI Components
mkdir -p internal/ui/views/locks
touch internal/ui/views/locks/view.go
touch internal/ui/views/locks/help.go
touch internal/ui/views/locks/detail.go
touch internal/ui/components/lock_tree.go

# Tests
mkdir -p tests/integration tests/unit
touch tests/integration/locks_test.go
touch tests/unit/lock_tree_test.go
```

## Testing

### Unit Tests
```bash
go test ./tests/unit/... -v
```

### Integration Tests (requires Docker)
```bash
go test ./tests/integration/... -v -tags=integration
```

### Manual Testing Checklist

1. [ ] Navigate to Locks view with `5` key
2. [ ] Verify table displays all columns (PID, Type, Mode, Granted, DB, Relation, Query)
3. [ ] Create blocking scenario and verify red/yellow coloring
4. [ ] Press `s` to sort by different columns
5. [ ] Press `d` to view full query detail
6. [ ] Verify lock tree renders below table
7. [ ] Press `x` on blocking query, confirm dialog appears
8. [ ] Test in `--readonly` mode, verify `x` is disabled
9. [ ] Verify auto-refresh every 2 seconds
10. [ ] Test at 80x24 terminal size

## Creating Test Blocking Scenarios

### Simple Blocking
```sql
-- Terminal 1: Hold lock
BEGIN;
UPDATE users SET name = 'test' WHERE id = 1;
-- Don't commit

-- Terminal 2: Get blocked
UPDATE users SET name = 'test2' WHERE id = 1;
-- This will wait
```

### Multi-Level Chain
```sql
-- Terminal 1: Root blocker
BEGIN;
LOCK TABLE users IN ACCESS EXCLUSIVE MODE;

-- Terminal 2: Blocked by 1, blocks 3
BEGIN;
SELECT * FROM users FOR UPDATE;

-- Terminal 3: Blocked by 2
BEGIN;
UPDATE users SET name = 'test';
```

## Performance Validation

Before merging, validate:
- [ ] GetLocks() < 500ms with 100+ locks
- [ ] GetBlockingRelationships() < 500ms
- [ ] Tree rendering < 100ms
- [ ] Memory < 50MB during operation
