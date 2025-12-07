# User Story 7 Test Plan: Bidirectional Merge with Existing Data

**Feature Branch**: `015-node-init`
**User Story**: Initial Sync with Existing Data on Both Nodes (Priority: P2)
**Status**: Test Plan Draft

## Test Philosophy

- **No mocks, no fakes, no simulations** - All tests use real PostgreSQL via testcontainers
- **Deterministic fixtures** - Every test has explicit data states and expected outcomes
- **PostgreSQL 18 native features** - Leverage `track_commit_timestamp`, `origin=none`, conflict stats
- **Full observability** - Audit trail for every merge decision

## Test Infrastructure

### Container Setup

```go
// tests/integration/repl/merge_fixtures_test.go

type BidirectionalTestCluster struct {
    NodeA       *PostgresContainer  // PG18 with steep_repl extension
    NodeB       *PostgresContainer  // PG18 with steep_repl extension
    Network     testcontainers.Network
    CleanupFunc func()
}

func NewBidirectionalTestCluster(t *testing.T) *BidirectionalTestCluster {
    // Creates two PG18 containers on same Docker network
    // Enables: wal_level=logical, track_commit_timestamp=on
    // Installs steep_repl extension on both
    // Sets up postgres_fdw connections between nodes for hash-based comparison
    // Creates foreign server and user mappings on each node
}
```

### Performance Architecture

All overlap analysis uses **hash-based comparison via postgres_fdw**:

1. Extension computes row hashes (Rust/pgrx - fast)
2. postgres_fdw transfers only PKs + 8-byte hashes (minimal network)
3. PostgreSQL compares using indexes/hash joins (optimized)
4. Full row data fetched only for conflicts

```sql
-- Example: Only ~16 bytes per row cross the network (PK + hash)
-- vs ~500+ bytes for full row comparison
SELECT steep_repl.row_hash(users.*) FROM users;  -- 8-byte hash
```

### Audit Log Location

All merge decisions logged to `steep_repl.merge_audit_log` in the PostgreSQL extension (not SQLite). This keeps audit data with the source of truth and enables SQL-based compliance queries.

### Common Fixtures

All fixtures defined in `tests/integration/repl/testdata/merge/`:

```
testdata/merge/
├── schema.sql              # Common table definitions
├── simple_overlap.sql      # Basic 4-row test case
├── fk_relationships.sql    # Parent/child tables
├── large_dataset.sql       # 10K rows for performance
├── timestamp_conflicts.sql # last-modified resolution tests
├── multi_table.sql         # Cross-table consistency
└── edge_cases.sql          # NULL PKs, empty tables, etc.
```

---

## Test Categories

### Category 1: Overlap Analysis

Tests that verify correct identification of row categories.

#### T067-1: Basic Overlap Analysis - All Categories

```go
func TestOverlapAnalysis_AllCategories(t *testing.T) {
    // SETUP
    // Node A: users(1,'alice','v1'), users(2,'bob','v1'), users(3,'charlie','v1')
    // Node B: users(2,'bob','v1'), users(3,'charlie','v2'), users(4,'diana','v1')
    //
    // Row 1: A-only (exists only on A)
    // Row 2: Match (identical on both)
    // Row 3: Conflict (same PK, different data)
    // Row 4: B-only (exists only on B)

    // EXECUTE
    result := analyzer.AnalyzeOverlap(ctx, "public", "users", []string{"id"})

    // ASSERT
    assert.Equal(t, 1, result.Matches)
    assert.Equal(t, 1, result.Conflicts)
    assert.Equal(t, 1, result.AOnly)
    assert.Equal(t, 1, result.BOnly)
    assert.Equal(t, 4, result.Total)
}
```

**SQL Fixture** (`simple_overlap.sql`):
```sql
-- Node A data
INSERT INTO users (id, name, version) VALUES
    (1, 'alice', 'v1'),
    (2, 'bob', 'v1'),
    (3, 'charlie', 'v1');

-- Node B data
INSERT INTO users (id, name, version) VALUES
    (2, 'bob', 'v1'),      -- Match with A.row2
    (3, 'charlie', 'v2'),  -- Conflict with A.row3
    (4, 'diana', 'v1');    -- B-only
```

#### T067-2: Overlap Analysis - Composite Primary Keys

```go
func TestOverlapAnalysis_CompositePK(t *testing.T) {
    // SETUP
    // Table: order_items(order_id, item_id, quantity)
    // PK: (order_id, item_id)
    //
    // Node A: (1,1,5), (1,2,3), (2,1,1)
    // Node B: (1,1,5), (1,2,10), (2,2,7)
    //
    // (1,1): Match
    // (1,2): Conflict (quantity differs)
    // (2,1): A-only
    // (2,2): B-only

    // EXECUTE
    result := analyzer.AnalyzeOverlap(ctx, "public", "order_items", []string{"order_id", "item_id"})

    // ASSERT
    assert.Equal(t, 1, result.Matches)
    assert.Equal(t, 1, result.Conflicts)
    assert.Equal(t, 1, result.AOnly)
    assert.Equal(t, 1, result.BOnly)
}
```

#### T067-3: Overlap Analysis - Empty Tables

```go
func TestOverlapAnalysis_EmptyTables(t *testing.T) {
    // SETUP: Both tables empty
    result := analyzer.AnalyzeOverlap(ctx, "public", "empty_table", []string{"id"})

    // ASSERT
    assert.Equal(t, 0, result.Total)
    assert.True(t, result.IsEmpty())
}
```

#### T067-4: Overlap Analysis - One Node Empty

```go
func TestOverlapAnalysis_OneNodeEmpty(t *testing.T) {
    // SETUP
    // Node A: users(1,'alice'), users(2,'bob')
    // Node B: empty

    result := analyzer.AnalyzeOverlap(ctx, "public", "users", []string{"id"})

    // ASSERT
    assert.Equal(t, 0, result.Matches)
    assert.Equal(t, 0, result.Conflicts)
    assert.Equal(t, 2, result.AOnly)
    assert.Equal(t, 0, result.BOnly)
}
```

#### T067-5: Overlap Analysis - Large Dataset Performance

```go
func TestOverlapAnalysis_Performance_10K_Rows(t *testing.T) {
    // SETUP: 10,000 rows on each node
    // 8,000 matches, 1,000 conflicts, 500 A-only, 500 B-only

    start := time.Now()
    result := analyzer.AnalyzeOverlap(ctx, "public", "large_users", []string{"id"})
    elapsed := time.Since(start)

    // ASSERT: Must complete in under 5 seconds
    assert.Less(t, elapsed, 5*time.Second)
    assert.Equal(t, 8000, result.Matches)
    assert.Equal(t, 1000, result.Conflicts)
}
```

#### T067-6: Overlap Analysis - NULL Values in Non-PK Columns

```go
func TestOverlapAnalysis_NullValues(t *testing.T) {
    // SETUP
    // Node A: users(1,'alice',NULL), users(2,'bob','active')
    // Node B: users(1,'alice','pending'), users(2,'bob','active')
    //
    // Row 1: Conflict (NULL vs 'pending')
    // Row 2: Match

    result := analyzer.AnalyzeOverlap(ctx, "public", "users", []string{"id"})

    assert.Equal(t, 1, result.Matches)
    assert.Equal(t, 1, result.Conflicts)
}
```

#### T067-7: Overlap Analysis - Multi-Table Summary

```go
func TestOverlapAnalysis_MultiTable(t *testing.T) {
    // SETUP: Multiple tables with different overlap patterns
    tables := []string{"users", "orders", "products"}

    result := analyzer.AnalyzeAllTables(ctx, tables)

    // ASSERT: Summary statistics across all tables
    assert.Equal(t, 3, len(result.Tables))
    assert.Greater(t, result.TotalConflicts, 0)
}
```

---

### Category 2: Conflict Resolution Strategies

Tests that verify each resolution strategy produces correct outcomes.

#### T067-8: Resolution Strategy - prefer-node-a

```go
func TestConflictResolution_PreferNodeA(t *testing.T) {
    // SETUP: 5 conflicting rows
    // Node A: users(1,'alice','A'), (2,'bob','A'), (3,'charlie','A'), (4,'diana','A'), (5,'eve','A')
    // Node B: users(1,'alice','B'), (2,'bob','B'), (3,'charlie','B'), (4,'diana','B'), (5,'eve','B')

    // EXECUTE
    err := merger.ResolveConflicts(ctx, "public", "users", StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: All rows on both nodes now have Node A's values
    for _, nodePool := range []*pgxpool.Pool{cluster.NodeA.Pool, cluster.NodeB.Pool} {
        rows := queryAllUsers(ctx, nodePool)
        for _, row := range rows {
            assert.Equal(t, "A", row.Version, "Row %d should have A's value", row.ID)
        }
    }
}
```

#### T067-9: Resolution Strategy - prefer-node-b

```go
func TestConflictResolution_PreferNodeB(t *testing.T) {
    // Same setup as T067-8, opposite assertion
    err := merger.ResolveConflicts(ctx, "public", "users", StrategyPreferNodeB)
    require.NoError(t, err)

    // ASSERT: All rows have Node B's values
    for _, row := range queryAllUsers(ctx, cluster.NodeA.Pool) {
        assert.Equal(t, "B", row.Version)
    }
}
```

#### T067-10: Resolution Strategy - last-modified

```go
func TestConflictResolution_LastModified(t *testing.T) {
    // SETUP: Rows with different modification timestamps
    // Row 1: A modified at T1, B modified at T2 (T2 > T1) → B wins
    // Row 2: A modified at T3, B modified at T4 (T3 > T4) → A wins
    // Row 3: Same timestamp → deterministic tiebreaker (prefer A)

    // Insert with explicit timestamps using track_commit_timestamp
    // Node A: UPDATE users SET version='A' WHERE id=1; -- at T1
    // Node B: UPDATE users SET version='B' WHERE id=1; -- at T2 (later)

    err := merger.ResolveConflicts(ctx, "public", "users", StrategyLastModified)
    require.NoError(t, err)

    // ASSERT
    row1 := queryUser(ctx, cluster.NodeA.Pool, 1)
    assert.Equal(t, "B", row1.Version, "Row 1: B was modified later")

    row2 := queryUser(ctx, cluster.NodeA.Pool, 2)
    assert.Equal(t, "A", row2.Version, "Row 2: A was modified later")

    row3 := queryUser(ctx, cluster.NodeA.Pool, 3)
    assert.Equal(t, "A", row3.Version, "Row 3: Same timestamp, prefer A")
}
```

**Requirement**: `track_commit_timestamp = on` must be enabled.

#### T067-11: Resolution Strategy - manual (Generates Report)

```go
func TestConflictResolution_Manual_GeneratesReport(t *testing.T) {
    // SETUP: 3 conflicting rows

    report, err := merger.GenerateConflictReport(ctx, "public", "users")
    require.NoError(t, err)

    // ASSERT: Report contains all conflicts with actionable information
    assert.Equal(t, 3, len(report.Conflicts))
    for _, conflict := range report.Conflicts {
        assert.NotEmpty(t, conflict.PrimaryKey)
        assert.NotEmpty(t, conflict.NodeAValue)
        assert.NotEmpty(t, conflict.NodeBValue)
        assert.Contains(t, []string{"prefer-a", "prefer-b", "skip"}, conflict.SuggestedResolution)
    }
}
```

#### T067-12: Resolution Strategy - Mixed (Per-Table)

```go
func TestConflictResolution_MixedStrategies(t *testing.T) {
    // SETUP: Different strategies per table
    strategies := map[string]ConflictStrategy{
        "users":    StrategyPreferNodeA,
        "orders":   StrategyPreferNodeB,
        "products": StrategyLastModified,
    }

    err := merger.ResolveConflictsMultiTable(ctx, strategies)
    require.NoError(t, err)

    // ASSERT: Each table resolved according to its strategy
}
```

---

### Category 3: Foreign Key Ordering

Tests that verify parent tables are merged before children.

#### T067-13: FK Ordering - Simple Parent/Child

```go
func TestBidirectionalMerge_ForeignKeyOrdering(t *testing.T) {
    // SETUP
    // Tables: customers(id PK), orders(id PK, customer_id FK → customers)
    //
    // Node A: customer(100), orders for customer 100
    // Node B: customer(200), orders for customer 200

    err := merger.MergeBidirectional(ctx, []string{"customers", "orders"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: No FK violations occurred
    // Both nodes have customers 100, 200 and all orders
    customersA := queryAllCustomers(ctx, cluster.NodeA.Pool)
    assert.Equal(t, 2, len(customersA))

    ordersA := queryAllOrders(ctx, cluster.NodeA.Pool)
    // All orders reference valid customers
    for _, order := range ordersA {
        assert.Contains(t, []int{100, 200}, order.CustomerID)
    }
}
```

**SQL Fixture** (`fk_relationships.sql`):
```sql
-- Schema
CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    total DECIMAL(10,2)
);

-- Node A data
INSERT INTO customers (id, name) VALUES (100, 'Customer A');
INSERT INTO orders (id, customer_id, total) VALUES (1, 100, 99.99);

-- Node B data
INSERT INTO customers (id, name) VALUES (200, 'Customer B');
INSERT INTO orders (id, customer_id, total) VALUES (2, 200, 149.99);
```

#### T067-14: FK Ordering - Deep Hierarchy (3 Levels)

```go
func TestBidirectionalMerge_DeepFKHierarchy(t *testing.T) {
    // SETUP
    // departments → employees → timesheets
    //
    // Merge must happen in order: departments, employees, timesheets

    err := merger.MergeBidirectional(ctx, []string{"timesheets", "employees", "departments"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: Merge reordered tables correctly
    // Verify audit log shows correct order
    auditLog := queryMergeAuditLog(ctx)
    tables := extractTableOrder(auditLog)
    assert.Equal(t, []string{"departments", "employees", "timesheets"}, tables)
}
```

#### T067-15: FK Ordering - Circular Reference Detection

```go
func TestBidirectionalMerge_CircularFK_Fails(t *testing.T) {
    // SETUP: Circular FK (A → B → A)
    // This should fail with clear error

    err := merger.MergeBidirectional(ctx, []string{"table_a", "table_b"}, StrategyPreferNodeA)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "circular foreign key")
}
```

---

### Category 4: Data Movement

Tests that verify data actually moves correctly.

#### T067-16: Data Movement - A-Only Rows to B

```go
func TestDataMovement_AOnlyToB(t *testing.T) {
    // SETUP
    // Node A: users(1,'alice'), users(2,'bob'), users(3,'charlie')
    // Node B: users(1,'alice')
    //
    // A-only: rows 2, 3

    err := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: Node B now has rows 2 and 3
    usersB := queryAllUsers(ctx, cluster.NodeB.Pool)
    assert.Equal(t, 3, len(usersB))
    assert.Contains(t, getUserNames(usersB), "bob")
    assert.Contains(t, getUserNames(usersB), "charlie")
}
```

#### T067-17: Data Movement - B-Only Rows to A

```go
func TestDataMovement_BOnlyToA(t *testing.T) {
    // Mirror of T067-16

    // ASSERT: Node A now has B-only rows
}
```

#### T067-18: Data Movement - Bidirectional (Both Directions)

```go
func TestDataMovement_Bidirectional(t *testing.T) {
    // SETUP
    // Node A: users(1), users(2)
    // Node B: users(3), users(4)
    //
    // No conflicts, pure additive merge

    err := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: Both nodes have all 4 rows
    usersA := queryAllUsers(ctx, cluster.NodeA.Pool)
    usersB := queryAllUsers(ctx, cluster.NodeB.Pool)

    assert.Equal(t, 4, len(usersA))
    assert.Equal(t, 4, len(usersB))
    assert.ElementsMatch(t, getUserIDs(usersA), getUserIDs(usersB))
}
```

---

### Category 5: Atomicity and Failure Recovery

Tests that verify merge is atomic and recoverable.

#### T067-19: Atomicity - Rollback on Constraint Violation

```go
func TestBidirectionalMerge_AtomicRollback(t *testing.T) {
    // SETUP: Inject a constraint violation mid-merge
    // Table 1: will succeed
    // Table 2: will fail (CHECK constraint violation)
    // Table 3: should not be attempted

    err := merger.MergeBidirectional(ctx, []string{"table1", "table2", "table3"}, StrategyPreferNodeA)

    // ASSERT: Error occurred
    assert.Error(t, err)

    // ASSERT: No partial data - all tables unchanged
    table1A := queryTable1(ctx, cluster.NodeA.Pool)
    assert.Equal(t, originalTable1A, table1A, "Table 1 should be unchanged")

    table1B := queryTable1(ctx, cluster.NodeB.Pool)
    assert.Equal(t, originalTable1B, table1B, "Table 1 should be unchanged on B")
}
```

#### T067-20: Idempotency - Running Merge Twice

```go
func TestBidirectionalMerge_Idempotent(t *testing.T) {
    // EXECUTE: Run merge twice
    err1 := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err1)

    stateAfterFirst := captureState(ctx)

    err2 := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err2)

    stateAfterSecond := captureState(ctx)

    // ASSERT: State unchanged after second run
    assert.Equal(t, stateAfterFirst, stateAfterSecond)
}
```

#### T067-21: Checkpoint/Resume (Future Enhancement)

```go
func TestBidirectionalMerge_ResumeAfterFailure(t *testing.T) {
    // SETUP: Start merge, simulate crash after 50% completion

    // First attempt - will fail mid-way
    err1 := merger.MergeBidirectionalWithInjectedFailure(ctx, tables, failAfterTable2)
    assert.Error(t, err1)

    // Resume from checkpoint
    err2 := merger.ResumeMerge(ctx, mergeID)
    require.NoError(t, err2)

    // ASSERT: All data merged correctly
    verifyFullMerge(ctx, tables)
}
```

---

### Category 6: Audit Trail

Tests that verify complete audit logging.

#### T067-22: Audit Log - Records All Decisions (Extension Table)

```go
func TestAuditLog_RecordsAllDecisions(t *testing.T) {
    // SETUP: Mix of matches, conflicts, A-only, B-only

    err := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: Audit log in steep_repl.merge_audit_log has entry for every non-match row
    // (matches are not logged to save space - only actionable items)
    var auditEntries []MergeAuditEntry
    rows, _ := cluster.NodeA.Pool.Query(ctx, `
        SELECT merge_id, table_schema, table_name, pk_value, category,
               resolution, node_a_value, node_b_value, resolved_at, resolved_by
        FROM steep_repl.merge_audit_log
        ORDER BY id
    `)
    // ... scan rows

    // Count by category
    categories := countByCategory(auditEntries)
    assert.Equal(t, expectedConflicts, categories["conflict"])
    assert.Equal(t, expectedAOnly, categories["a_only"])
    assert.Equal(t, expectedBOnly, categories["b_only"])

    // Verify conflict entries have full row values for debugging
    for _, entry := range auditEntries {
        if entry.Category == "conflict" {
            assert.NotEmpty(t, entry.Resolution)
            assert.NotEmpty(t, entry.NodeAValue)  // JSONB of full row
            assert.NotEmpty(t, entry.NodeBValue)  // JSONB of full row
            assert.NotEmpty(t, entry.ResolvedBy)  // e.g., "strategy:prefer-node-a"
        }
    }
}
```

#### T067-23: Audit Log - Includes Timestamps and Merge ID (Extension Table)

```go
func TestAuditLog_HasMetadata(t *testing.T) {
    mergeID := merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)

    // Query the extension's merge_audit_log table
    var auditEntries []MergeAuditEntry
    rows, _ := cluster.NodeA.Pool.Query(ctx, `
        SELECT merge_id, table_schema, table_name, pk_value, category,
               resolution, resolved_at, resolved_by
        FROM steep_repl.merge_audit_log
        WHERE merge_id = $1
    `, mergeID)
    // ... scan rows

    assert.Greater(t, len(auditEntries), 0)
    for _, entry := range auditEntries {
        assert.Equal(t, mergeID, entry.MergeID)
        assert.False(t, entry.ResolvedAt.IsZero())
        assert.NotEmpty(t, entry.TableSchema)
        assert.NotEmpty(t, entry.TableName)
        assert.NotEmpty(t, entry.PKValue)  // JSONB of PK columns
    }
}
```

---

### Category 7: Pre-flight Checks

Tests that verify merge refuses to start without proper conditions.

#### T067-24: Pre-flight - Schema Mismatch Fails

```go
func TestPreflight_SchemaMismatch_Fails(t *testing.T) {
    // SETUP: Different column on Node B
    _, err := cluster.NodeB.Pool.Exec(ctx, "ALTER TABLE users ADD COLUMN extra TEXT")

    err = merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "schema mismatch")
}
```

#### T067-25: Pre-flight - Missing Primary Key Fails

```go
func TestPreflight_MissingPK_Fails(t *testing.T) {
    // SETUP: Table without primary key
    _, err := cluster.NodeA.Pool.Exec(ctx, "CREATE TABLE no_pk (data TEXT)")
    _, err = cluster.NodeB.Pool.Exec(ctx, "CREATE TABLE no_pk (data TEXT)")

    err = merger.MergeBidirectional(ctx, []string{"no_pk"}, StrategyPreferNodeA)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "primary key required")
}
```

#### T067-26: Pre-flight - Active Transactions Warning

```go
func TestPreflight_ActiveTransactions_Warning(t *testing.T) {
    // SETUP: Start a transaction on Node A
    tx, _ := cluster.NodeA.Pool.Begin(ctx)
    defer tx.Rollback(ctx)

    _, err := tx.Exec(ctx, "SELECT * FROM users FOR UPDATE")

    // EXECUTE: Merge should warn but can proceed with --force
    err = merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "active transaction")
}
```

---

### Category 8: Dry-Run Mode

Tests that verify dry-run shows accurate preview without modifications.

#### T067-27: Dry-Run - Shows Accurate Preview

```go
func TestDryRun_AccuratePreview(t *testing.T) {
    // SETUP: Known overlap state

    preview, err := merger.DryRun(ctx, []string{"users"}, StrategyPreferNodeA)
    require.NoError(t, err)

    // ASSERT: Preview matches what merge would do
    assert.Equal(t, expectedConflicts, preview.ConflictsToResolve)
    assert.Equal(t, expectedAToB, preview.RowsAToB)
    assert.Equal(t, expectedBToA, preview.RowsBToA)

    // ASSERT: No data changed
    assert.Equal(t, originalStateA, captureState(ctx, cluster.NodeA.Pool))
    assert.Equal(t, originalStateB, captureState(ctx, cluster.NodeB.Pool))
}
```

#### T067-28: Dry-Run - Output Format

```go
func TestDryRun_OutputFormat(t *testing.T) {
    preview, _ := merger.DryRun(ctx, []string{"users", "orders"}, StrategyPreferNodeA)

    // ASSERT: Output is structured and human-readable
    output := preview.String()

    assert.Contains(t, output, "Overlap Analysis")
    assert.Contains(t, output, "Conflict Resolution")
    assert.Contains(t, output, "Data Movement")
    assert.Contains(t, output, "No changes made")
}
```

---

### Category 9: CLI Integration

Tests that verify CLI commands work end-to-end.

#### T067-29: CLI - analyze-overlap Command

```go
func TestCLI_AnalyzeOverlap(t *testing.T) {
    // EXECUTE
    cmd := exec.Command("steep-repl", "analyze-overlap",
        "--node-a", cluster.NodeA.ConnectionString(),
        "--node-b", cluster.NodeB.ConnectionString(),
        "--tables", "users,orders")

    output, err := cmd.CombinedOutput()
    require.NoError(t, err)

    // ASSERT: Output shows overlap analysis
    assert.Contains(t, string(output), "users:")
    assert.Contains(t, string(output), "matches:")
    assert.Contains(t, string(output), "conflicts:")
}
```

#### T067-30: CLI - init --mode=bidirectional-merge

```go
func TestCLI_InitBidirectionalMerge(t *testing.T) {
    cmd := exec.Command("steep-repl", "init",
        "--mode", "bidirectional-merge",
        "--node-a", cluster.NodeA.ConnectionString(),
        "--node-b", cluster.NodeB.ConnectionString(),
        "--strategy", "prefer-node-a",
        "--tables", "users")

    output, err := cmd.CombinedOutput()
    require.NoError(t, err)

    // ASSERT: Merge completed
    assert.Contains(t, string(output), "Bidirectional merge complete")

    // ASSERT: Replication enabled
    subsA := querySubscriptions(ctx, cluster.NodeA.Pool)
    subsB := querySubscriptions(ctx, cluster.NodeB.Pool)
    assert.Equal(t, 1, len(subsA))
    assert.Equal(t, 1, len(subsB))
}
```

#### T067-31: CLI - --dry-run Flag

```go
func TestCLI_DryRunFlag(t *testing.T) {
    originalStateA := captureState(ctx, cluster.NodeA.Pool)

    cmd := exec.Command("steep-repl", "init",
        "--mode", "bidirectional-merge",
        "--dry-run",
        "--node-a", cluster.NodeA.ConnectionString(),
        "--node-b", cluster.NodeB.ConnectionString())

    output, err := cmd.CombinedOutput()
    require.NoError(t, err)

    // ASSERT: Shows preview
    assert.Contains(t, string(output), "Dry run")

    // ASSERT: No changes made
    assert.Equal(t, originalStateA, captureState(ctx, cluster.NodeA.Pool))
}
```

---

### Category 10: PostgreSQL 18 Integration

Tests that verify we leverage PG18 native features correctly.

#### T067-32: PG18 - origin=none Prevents Ping-Pong

```go
func TestPG18_OriginNone_PreventsPingPong(t *testing.T) {
    // SETUP: Complete bidirectional merge and enable replication
    merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)

    // EXECUTE: Insert on Node A
    _, err := cluster.NodeA.Pool.Exec(ctx, "INSERT INTO users (id, name) VALUES (999, 'new_user')")
    require.NoError(t, err)

    // Wait for replication
    waitForReplication(ctx, cluster.NodeB.Pool, "users", 999)

    // ASSERT: Row appears on Node B
    userB := queryUser(ctx, cluster.NodeB.Pool, 999)
    assert.Equal(t, "new_user", userB.Name)

    // ASSERT: Row does NOT ping-pong back to A (check WAL position stable)
    walPosA := getWALPosition(ctx, cluster.NodeA.Pool)
    time.Sleep(2 * time.Second)
    walPosA2 := getWALPosition(ctx, cluster.NodeA.Pool)

    // No new WAL generated from ping-pong
    assert.Equal(t, walPosA, walPosA2)
}
```

#### T067-33: PG18 - Conflict Statistics Populated

```go
func TestPG18_ConflictStats_Populated(t *testing.T) {
    // SETUP: Enable replication, then cause a conflict
    merger.MergeBidirectional(ctx, []string{"users"}, StrategyPreferNodeA)

    // Disable subscription temporarily, make conflicting change
    // Re-enable and verify conflict is detected

    stats := querySubscriptionStats(ctx, cluster.NodeA.Pool)

    // ASSERT: Conflict was recorded in pg_stat_subscription_stats
    assert.Greater(t, stats.ConflictInsertExists+stats.ConflictUpdateOriginDiffers, int64(0))
}
```

#### T067-34: PG18 - track_commit_timestamp Required

```go
func TestPG18_TrackCommitTimestamp_Required(t *testing.T) {
    // SETUP: Disable track_commit_timestamp
    // This should cause last-modified strategy to fail

    err := merger.ResolveConflicts(ctx, "users", StrategyLastModified)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "track_commit_timestamp")
}
```

---

## Performance Benchmarks

#### T067-35: Benchmark - Hash-Based Overlap Analysis Performance

```go
func BenchmarkOverlapAnalysis_HashBased(b *testing.B) {
    // Setup: 100K rows, 90% match, 5% conflict, 2.5% A-only, 2.5% B-only
    // Uses steep_repl.row_hash() + postgres_fdw

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        analyzer.AnalyzeOverlap(ctx, "public", "large_users", []string{"id"})
    }

    // Target: < 5 seconds for 100K rows
    // Hash-based should be 10x faster than full-row comparison
}
```

#### T067-36: Benchmark - Data Transfer Performance

```go
func BenchmarkDataTransfer(b *testing.B) {
    // Setup: Transfer 10K rows using COPY protocol

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        merger.TransferRows(ctx, sourcePool, targetPool, "users", rowIDs)
    }

    // Target: > 10K rows/second (COPY protocol)
}
```

#### T067-37: Benchmark - Extension Row Hash Performance

```go
func BenchmarkRowHash_Extension(b *testing.B) {
    // Setup: 100K rows to hash

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = pool.Exec(ctx, `
            SELECT steep_repl.row_hash(u.*) FROM large_users u
        `)
    }

    // Target: > 500K rows/second hashing throughput
    // Rust/pgrx implementation should be significantly faster than pure SQL
}
```

#### T067-38: Benchmark - Network Transfer Comparison

```go
func BenchmarkNetworkTransfer_HashVsFullRow(b *testing.B) {
    // Compare network bytes transferred for 100K rows
    // Hash-based: ~16 bytes/row (PK + hash)
    // Full-row: ~500 bytes/row (average)

    b.Run("hash_based", func(b *testing.B) {
        // postgres_fdw transferring only hashes
    })

    b.Run("full_row", func(b *testing.B) {
        // postgres_fdw transferring full rows
    })

    // Assert: Hash-based uses < 5% of network bandwidth
}

---

## Test Data Fixtures

### schema.sql
```sql
-- Common schema for all tests
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    version TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    total DECIMAL(10,2),
    status TEXT DEFAULT 'pending'
);

CREATE TABLE order_items (
    order_id INTEGER NOT NULL REFERENCES orders(id),
    item_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    price DECIMAL(10,2),
    PRIMARY KEY (order_id, item_id)
);

CREATE TABLE products (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    price DECIMAL(10,2)
);
```

### simple_overlap.sql
```sql
-- Node A
\c node_a
INSERT INTO users (id, name, version) VALUES
    (1, 'alice', 'v1'),    -- A-only
    (2, 'bob', 'v1'),      -- Match
    (3, 'charlie', 'v1');  -- Conflict (v1 vs v2)

-- Node B
\c node_b
INSERT INTO users (id, name, version) VALUES
    (2, 'bob', 'v1'),      -- Match
    (3, 'charlie', 'v2'),  -- Conflict
    (4, 'diana', 'v1');    -- B-only
```

---

## Summary

| Category | Test Count | Coverage |
|----------|------------|----------|
| Overlap Analysis | 7 | Detection of all row categories (hash-based via FDW) |
| Conflict Resolution | 5 | All strategies (prefer-a/b, last-mod, manual, mixed) |
| FK Ordering | 3 | Parent/child, deep hierarchy, circular detection |
| Data Movement | 3 | A→B, B→A, bidirectional |
| Atomicity | 3 | Rollback, idempotency, checkpoint |
| Audit Trail | 2 | Decision logging in steep_repl.merge_audit_log |
| Pre-flight Checks | 3 | Schema, PK, transactions |
| Dry-Run | 2 | Preview accuracy, format |
| CLI Integration | 3 | analyze-overlap, init, flags |
| PG18 Integration | 3 | origin=none, conflict stats, timestamp |
| Performance | 4 | Hash analysis, transfer, row_hash, network comparison |
| **Total** | **38** | |

### Key Architecture Decisions

1. **Hash-based comparison via postgres_fdw** - Minimal network transfer (~16 bytes/row vs ~500 bytes/row)
2. **Audit log in extension** - `steep_repl.merge_audit_log` keeps audit with source data
3. **Rust/pgrx row hashing** - Fast hashing in extension, not SQL
4. **PostgreSQL 18 native features** - `origin=none`, `track_commit_timestamp`, conflict stats

All tests use real PostgreSQL 18 via testcontainers with the steep_repl extension installed.
