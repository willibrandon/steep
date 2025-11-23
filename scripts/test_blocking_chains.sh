#!/bin/bash
# test_blocking_chains.sh - Generate complex multi-level blocking chains for testing
#
# This script creates deep blocking chains where:
# - A blocks B, B blocks C, C blocks D (chain depth)
# - Multiple independent chains
# - Multiple blocked queries per blocker
#
# Usage: ./scripts/test_blocking_chains.sh [database] [duration_seconds]
#   database: Database name (default: steep_test)
#   duration_seconds: How long to hold locks (default: 120)
#
# Example:
#   ./scripts/test_blocking_chains.sh postgres 60

set -e

DB="${1:-steep_test}"
DURATION="${2:-120}"
TABLE="test_chains_$$"

echo "=== Multi-Level Blocking Chains Test ==="
echo "Database: $DB"
echo "Duration: ${DURATION}s"
echo "Table: $TABLE"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    jobs -p | xargs -r kill 2>/dev/null || true
    psql -d "$DB" -c "DROP TABLE IF EXISTS $TABLE;" 2>/dev/null || true
    echo "Done."
}

trap cleanup EXIT INT TERM

# Create test table with multiple rows
echo "Creating test table with 20 rows..."
psql -d "$DB" -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id SERIAL PRIMARY KEY,
        chain_id INT,
        data TEXT,
        updated_at TIMESTAMP DEFAULT NOW()
    );
    INSERT INTO $TABLE (chain_id, data)
    SELECT
        (i % 5) + 1,
        'row_' || i
    FROM generate_series(1, 20) AS i;
"

echo ""
echo "Creating blocking chains..."
echo ""

# ============================================
# Chain 1: Deep chain (4 levels)
# Root -> Level1 -> Level2 -> Level3
# ============================================
echo "=== Chain 1: Deep 4-level chain on row 1 ==="

# Root blocker (holds the lock)
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 1 Root: Holds lock on row 1
SELECT * FROM $TABLE WHERE id = 1 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Root] Started - holds FOR UPDATE on row 1"
sleep 0.3

# Level 1 (blocked by root, will block level 2)
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 1 Level 1: Blocked by root, will block level 2
UPDATE $TABLE SET data = 'level1' WHERE id = 1;
SELECT * FROM $TABLE WHERE id = 2 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Level 1] Started - blocked on row 1, holds row 2"
sleep 0.3

# Level 2 (blocked by level 1, will block level 3)
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 1 Level 2: Blocked by level 1
UPDATE $TABLE SET data = 'level2' WHERE id = 2;
SELECT * FROM $TABLE WHERE id = 3 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Level 2] Started - blocked on row 2, holds row 3"
sleep 0.3

# Level 3 (blocked by level 2 - leaf)
psql -d "$DB" <<EOF &
-- Chain 1 Level 3: Leaf - blocked by level 2
UPDATE $TABLE SET data = 'level3' WHERE id = 3;
EOF
echo "  [Level 3] Started - blocked on row 3 (leaf)"
sleep 0.3

# ============================================
# Chain 2: Wide chain (1 blocker, 4 blocked)
# ============================================
echo ""
echo "=== Chain 2: Wide chain - 1 blocker, 4 blocked on row 5 ==="

# Root blocker
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 2 Root: Blocks 4 queries
SELECT * FROM $TABLE WHERE id = 5 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Root] Started - holds FOR UPDATE on row 5"
sleep 0.3

# 4 blocked queries
for i in 1 2 3 4; do
    psql -d "$DB" <<EOF &
-- Chain 2 Blocked $i: Waiting for row 5
UPDATE $TABLE SET data = 'blocked_$i' WHERE id = 5;
EOF
    echo "  [Blocked $i] Started - waiting for row 5"
    sleep 0.1
done
sleep 0.3

# ============================================
# Chain 3: Diamond pattern
# Root blocks A and B, both A and B block C
# ============================================
echo ""
echo "=== Chain 3: Mixed chain on rows 10-12 ==="

# Root
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 3 Root: Holds rows 10 and 11
SELECT * FROM $TABLE WHERE id = 10 FOR UPDATE;
SELECT * FROM $TABLE WHERE id = 11 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Root] Started - holds rows 10 and 11"
sleep 0.3

# Branch A (blocked on 10)
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 3 Branch A: Blocked on row 10, holds row 12
UPDATE $TABLE SET data = 'branch_a' WHERE id = 10;
SELECT * FROM $TABLE WHERE id = 12 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Branch A] Started - blocked on row 10, holds row 12"
sleep 0.3

# Branch B (blocked on 11)
psql -d "$DB" <<EOF &
-- Chain 3 Branch B: Blocked on row 11
UPDATE $TABLE SET data = 'branch_b' WHERE id = 11;
EOF
echo "  [Branch B] Started - blocked on row 11 (leaf)"
sleep 0.3

# Leaf (blocked on 12 by Branch A)
psql -d "$DB" <<EOF &
-- Chain 3 Leaf: Blocked by Branch A on row 12
UPDATE $TABLE SET data = 'leaf' WHERE id = 12;
EOF
echo "  [Leaf] Started - blocked on row 12 (leaf)"
sleep 0.3

# ============================================
# Chain 4: Table lock chain
# ============================================
echo ""
echo "=== Chain 4: Table-level lock blocking ==="

# Table lock holder
psql -d "$DB" <<EOF &
BEGIN;
-- Chain 4: Holds EXCLUSIVE table lock
LOCK TABLE $TABLE IN EXCLUSIVE MODE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
echo "  [Table Lock] Started - holds EXCLUSIVE lock on table"
sleep 0.5

# Blocked by table lock
psql -d "$DB" <<EOF &
-- Chain 4: Blocked waiting for table lock
LOCK TABLE $TABLE IN ACCESS EXCLUSIVE MODE;
EOF
echo "  [Blocked] Started - waiting for ACCESS EXCLUSIVE"
sleep 0.3

echo ""
echo "=========================================="
echo "  Blocking Chains Active"
echo "=========================================="
echo ""
echo "Chain 1: Deep chain (4 levels)"
echo "  Root -> Level1 -> Level2 -> Level3"
echo ""
echo "Chain 2: Wide chain (1 blocker, 4 blocked)"
echo "  Root -> [Blocked1, Blocked2, Blocked3, Blocked4]"
echo ""
echo "Chain 3: Mixed pattern"
echo "  Root -> BranchA -> Leaf"
echo "       -> BranchB"
echo ""
echo "Chain 4: Table lock"
echo "  TableLock -> Blocked"
echo ""
echo "=========================================="
echo ""
echo "Open Steep and navigate to Locks view (press 4) to see:"
echo "  - Multiple blocking chains in the tree view"
echo "  - Blockers in YELLOW, blocked in RED"
echo "  - Deep chains showing hierarchy"
echo ""
echo "Locks will be held for ${DURATION} seconds."
echo "Press Ctrl+C to cancel early."
echo ""

wait

echo ""
echo "Blocking chains test completed."
