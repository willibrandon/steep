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

#### Active Locks Tab
1. [ ] Navigate to Locks view with `4` key
2. [ ] Verify table displays all columns (PID, Type, Mode, Dur, Grt, DB, Relation, Query)
3. [ ] Create blocking scenario and verify red/yellow coloring
4. [ ] Press `s` to cycle sort columns (PID → Type → Mode → Granted → Duration)
5. [ ] Press `S` to toggle sort direction (↓ ↔ ↑)
6. [ ] Press `d` or Enter to view full query detail with formatted SQL
7. [ ] In detail view: `j/k` scroll, `y` copy query, `Esc` back
8. [ ] Verify blocking chain tree renders in detail view
9. [ ] Press `x` on blocking query, confirm dialog appears
10. [ ] Test in `--readonly` mode, verify `x` shows error toast
11. [ ] Press `y` to copy query to clipboard
12. [ ] Verify auto-refresh every 2 seconds
13. [ ] Press `h` for help overlay

#### Deadlock History Tab
14. [ ] Press `→` to switch to Deadlock History tab
15. [ ] Verify deadlock events display with timestamp, database, processes, tables
16. [ ] Press `s` to cycle sort (Time → Database → Processes)
17. [ ] Press `S` to toggle sort direction
18. [ ] Press `d` or Enter to view deadlock detail with wait-for cycle visualization
19. [ ] Verify 2-node deadlocks show horizontal diagram
20. [ ] Verify 3+ node deadlocks show vertical cycle diagram
21. [ ] In detail view: `c` to copy all queries
22. [ ] Press `R` to reset deadlock history (confirm dialog)
23. [ ] Press `P` to reset log positions (re-parse logs)

#### General
24. [ ] Test at 80x24 terminal size
25. [ ] Verify footer hints match available actions for each tab

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
