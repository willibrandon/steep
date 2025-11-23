#!/bin/bash
# test_deadlocks.sh - Generate actual deadlocks for testing deadlock history
#
# Usage: ./scripts/test_deadlocks.sh <database> [count]
#   database: Database name (required)
#   count: Number of deadlocks to generate (default: 3)

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <database> [count]"
    echo ""
    echo "Arguments:"
    echo "  database  Database name (required)"
    echo "  count     Number of deadlocks to generate (default: 3)"
    echo ""
    echo "Example:"
    echo "  $0 postgres 5"
    exit 1
fi

DB="$1"
COUNT="${2:-3}"
TABLE="test_deadlock_$$"

echo "=== Deadlock Generator ==="
echo "Database: $DB"
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

# Create test table
echo "Creating test table..."
psql -d "$DB" -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id INT PRIMARY KEY,
        data TEXT
    );
    INSERT INTO $TABLE VALUES (1, 'row1'), (2, 'row2');
"

echo ""

for i in $(seq 1 $COUNT); do
    echo "Generating deadlock $i of $COUNT..."

    # Session 1: Lock row 1, sleep, then try row 2
    psql -d "$DB" <<EOF &
BEGIN;
UPDATE $TABLE SET data = 'session1_$i' WHERE id = 1;
SELECT pg_sleep(0.5);
UPDATE $TABLE SET data = 'session1_$i' WHERE id = 2;
COMMIT;
EOF
    PID1=$!

    # Small delay to ensure session 1 gets row 1 first
    sleep 0.2

    # Session 2: Lock row 2, then try row 1 (creates deadlock)
    psql -d "$DB" <<EOF &
BEGIN;
UPDATE $TABLE SET data = 'session2_$i' WHERE id = 2;
UPDATE $TABLE SET data = 'session2_$i' WHERE id = 1;
COMMIT;
EOF
    PID2=$!

    # Wait for both to complete (one will be killed by deadlock detection)
    wait $PID1 2>/dev/null || true
    wait $PID2 2>/dev/null || true

    echo "  Deadlock $i generated"
    sleep 0.5
done

echo ""
echo "==========================================="
echo "  Generated $COUNT deadlocks"
echo "==========================================="
echo ""
echo "Check PostgreSQL logs for deadlock messages."
echo "In Steep, go to Locks view (4) and switch to Deadlock History tab (→)."
echo ""
