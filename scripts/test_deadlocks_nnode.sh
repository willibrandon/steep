#!/bin/bash
# test_deadlocks_nnode.sh - Generate N-node deadlocks (3+ processes) for testing
#
# Usage: ./scripts/test_deadlocks_nnode.sh <database> [nodes] [count]
#   database: Database name (required)
#   nodes: Number of nodes in deadlock cycle (default: 3, min: 3)
#   count: Number of deadlocks to generate (default: 2)

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <database> [nodes] [count]"
    echo ""
    echo "Arguments:"
    echo "  database  Database name (required)"
    echo "  nodes     Number of nodes in deadlock cycle (default: 3, min: 3)"
    echo "  count     Number of deadlocks to generate (default: 2)"
    echo ""
    echo "Example:"
    echo "  $0 postgres 4 3    # Generate 3 deadlocks with 4-node cycles"
    exit 1
fi

DB="$1"
NODES="${2:-3}"
COUNT="${3:-2}"
TABLE="test_deadlock_nnode_$$"

# Ensure at least 3 nodes
if [ "$NODES" -lt 3 ]; then
    NODES=3
fi

echo "=== N-Node Deadlock Generator ==="
echo "Database: $DB"
echo "Nodes per cycle: $NODES"
echo "Deadlocks to generate: $COUNT"
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

# Create test table with N rows
echo "Creating test table with $NODES rows..."
psql -d "$DB" -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id INT PRIMARY KEY,
        data TEXT
    );
"

# Insert N rows
for i in $(seq 1 $NODES); do
    psql -d "$DB" -c "INSERT INTO $TABLE VALUES ($i, 'row$i');" 2>/dev/null
done

echo ""

for round in $(seq 1 $COUNT); do
    echo "Generating $NODES-node deadlock $round of $COUNT..."

    PIDS=()

    # Create N sessions, each locking row i then trying to lock row i+1 (wrapping)
    for i in $(seq 1 $NODES); do
        # Calculate next row (wrap around)
        next=$((i % NODES + 1))

        # Each session locks its row, sleeps, then tries to lock the next row
        psql -d "$DB" <<EOF &
BEGIN;
UPDATE $TABLE SET data = 'session${i}_round${round}' WHERE id = $i;
SELECT pg_sleep(0.5);
UPDATE $TABLE SET data = 'session${i}_round${round}' WHERE id = $next;
COMMIT;
EOF
        PIDS+=($!)

        # Small stagger to ensure lock acquisition order
        sleep 0.1
    done

    # Wait for all sessions to complete (N-1 will succeed, 1 will be killed)
    for pid in "${PIDS[@]}"; do
        wait $pid 2>/dev/null || true
    done

    echo "  $NODES-node deadlock $round generated"
    sleep 0.5
done

echo ""
echo "==========================================="
echo "  Generated $COUNT deadlocks with $NODES nodes each"
echo "==========================================="
echo ""
echo "Check PostgreSQL logs for deadlock messages."
echo "In Steep, go to Locks view (4) and switch to Deadlock History tab (→)."
echo ""
