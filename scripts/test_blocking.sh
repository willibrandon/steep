#!/bin/bash
# test_blocking.sh - Generate blocking and blocked queries for testing the locks view
#
# This script creates a blocking scenario where one transaction holds a lock
# and another transaction waits for it.
#
# Usage: ./scripts/test_blocking.sh [database] [duration_seconds]
#   database: Database name (default: steep_test)
#   duration_seconds: How long to hold the lock (default: 60)
#
# Example:
#   ./scripts/test_blocking.sh postgres 30

set -e

DB="${1:-steep_test}"
DURATION="${2:-60}"
TABLE="test_blocking_$$"

echo "=== Lock Blocking Test Script ==="
echo "Database: $DB"
echo "Duration: ${DURATION}s"
echo "Table: $TABLE"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    # Kill background jobs
    jobs -p | xargs -r kill 2>/dev/null || true
    # Drop test table
    psql -d "$DB" -c "DROP TABLE IF EXISTS $TABLE;" 2>/dev/null || true
    echo "Done."
}

trap cleanup EXIT INT TERM

# Create test table
echo "Creating test table..."
psql -d "$DB" -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id SERIAL PRIMARY KEY,
        data TEXT,
        updated_at TIMESTAMP DEFAULT NOW()
    );
    INSERT INTO $TABLE (data) VALUES ('row1'), ('row2'), ('row3');
"

echo ""
echo "Starting blocking scenario..."
echo ""

# Connection 1: Hold an exclusive lock (BLOCKER - will appear YELLOW)
echo "[Blocker] Starting transaction with exclusive lock..."
psql -d "$DB" <<EOF &
BEGIN;
-- This is the BLOCKING query (will appear YELLOW in Steep)
SELECT * FROM $TABLE WHERE id = 1 FOR UPDATE;
SELECT pg_sleep($DURATION);
COMMIT;
EOF
BLOCKER_PID=$!

# Wait for blocker to acquire lock
sleep 1

# Connection 2: Try to update same row (BLOCKED - will appear RED)
echo "[Blocked 1] Starting blocked UPDATE query..."
psql -d "$DB" <<EOF &
-- This is a BLOCKED query (will appear RED in Steep)
UPDATE $TABLE SET data = 'blocked_update', updated_at = NOW() WHERE id = 1;
EOF
BLOCKED1_PID=$!

sleep 0.5

# Connection 3: Another blocked query
echo "[Blocked 2] Starting blocked DELETE query..."
psql -d "$DB" <<EOF &
-- Another BLOCKED query (will appear RED in Steep)
DELETE FROM $TABLE WHERE id = 1;
EOF
BLOCKED2_PID=$!

sleep 0.5

# Connection 4: Try to lock the table (BLOCKED - will appear RED)
echo "[Blocked 3] Starting blocked LOCK TABLE query..."
psql -d "$DB" <<EOF &
-- BLOCKED waiting for table lock (will appear RED in Steep)
LOCK TABLE $TABLE IN ACCESS EXCLUSIVE MODE;
EOF
BLOCKED3_PID=$!

echo ""
echo "=== Blocking Scenario Active ==="
echo ""
echo "Blocker PID: $BLOCKER_PID (holding FOR UPDATE lock - YELLOW)"
echo "Blocked PIDs: $BLOCKED1_PID, $BLOCKED2_PID, $BLOCKED3_PID (waiting - RED)"
echo ""
echo "Open Steep and navigate to Locks view (press 4) to see:"
echo "  - Blocking query in YELLOW"
echo "  - Blocked queries in RED"
echo ""
echo "Lock will be held for ${DURATION} seconds."
echo "Press Ctrl+C to cancel early."
echo ""

# Wait for all background jobs
wait

echo ""
echo "Blocking scenario completed."
