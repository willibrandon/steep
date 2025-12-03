#!/bin/bash
# test_deadlocks_nnode.sh - Generate N-node deadlocks (3+ processes) for testing
#
# Creates N-node deadlock cycles where each session locks row i then waits for row i+1

set -e

# Defaults
HOST="localhost"
PORT="5432"
DB=""
USER="${PGUSER:-$(whoami)}"
NODES=3
COUNT=2

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

usage() {
    echo "Usage: $0 [OPTIONS] -d DATABASE"
    echo ""
    echo "Generate N-node deadlock cycles (3+ concurrent sessions) for testing"
    echo ""
    echo "Options:"
    echo "  -h, --host HOST       PostgreSQL host (default: localhost)"
    echo "  -p, --port PORT       PostgreSQL port (default: 5432)"
    echo "  -d, --database DB     Database name (required)"
    echo "  -U, --user USER       PostgreSQL user (default: \$PGUSER or current user)"
    echo "  -N, --nodes N         Number of nodes in deadlock cycle (default: 3, min: 3)"
    echo "  -n, --count N         Number of deadlocks to generate (default: 2)"
    echo "      --help            Show this help message"
    echo ""
    echo "Environment:"
    echo "  PGPASSWORD            Password for PostgreSQL connection"
    echo ""
    echo "Examples:"
    echo "  $0 -d postgres                    # 3-node deadlocks"
    echo "  $0 -d postgres -N 4 -n 3          # 3 deadlocks with 4-node cycles"
    echo "  $0 -h db.example.com -p 5433 -d mydb -N 5"
    echo ""
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--host) HOST="$2"; shift 2 ;;
        -p|--port) PORT="$2"; shift 2 ;;
        -d|--database) DB="$2"; shift 2 ;;
        -U|--user) USER="$2"; shift 2 ;;
        -N|--nodes) NODES="$2"; shift 2 ;;
        -n|--count) COUNT="$2"; shift 2 ;;
        --help) usage ;;
        *) echo -e "${RED}Unknown option: $1${NC}"; usage ;;
    esac
done

# Validate required args
if [ -z "$DB" ]; then
    echo -e "${RED}Error: Database name is required${NC}"
    echo ""
    usage
fi

# Ensure at least 3 nodes
if [ "$NODES" -lt 3 ]; then
    NODES=3
fi

TABLE="test_deadlock_nnode_$$"
PSQL="psql -h $HOST -p $PORT -U $USER -d $DB"

echo -e "${CYAN}=========================================="
echo -e "N-Node Deadlock Generator"
echo -e "==========================================${NC}"
echo ""
echo -e "Host:     ${HOST}:${PORT}"
echo -e "Database: ${DB}"
echo -e "User:     ${USER}"
echo -e "Nodes:    ${NODES} per cycle"
echo -e "Count:    ${COUNT} deadlocks"
echo ""

# Check connection
if ! $PSQL -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}Error: Cannot connect to PostgreSQL at ${HOST}:${PORT}/${DB}${NC}"
    echo "Check your connection parameters and PGPASSWORD environment variable."
    exit 1
fi

# Cleanup function
cleanup() {
    echo ""
    echo -e "${CYAN}Cleaning up...${NC}"
    jobs -p | xargs -r kill 2>/dev/null || true
    $PSQL -c "DROP TABLE IF EXISTS $TABLE;" 2>/dev/null || true
    echo -e "${GREEN}Done.${NC}"
}

trap cleanup EXIT INT TERM

# Create test table with N rows
echo -e "${CYAN}Creating test table with $NODES rows...${NC}"
$PSQL -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id INT PRIMARY KEY,
        data TEXT
    );
"

# Insert N rows
for i in $(seq 1 $NODES); do
    $PSQL -c "INSERT INTO $TABLE VALUES ($i, 'row$i');" 2>/dev/null
done

echo ""

for round in $(seq 1 $COUNT); do
    echo -e "${YELLOW}Generating $NODES-node deadlock $round of $COUNT...${NC}"

    PIDS=()

    # Create N sessions, each locking row i then trying to lock row i+1 (wrapping)
    for i in $(seq 1 $NODES); do
        # Calculate next row (wrap around)
        next=$((i % NODES + 1))

        # Each session locks its row, sleeps, then tries to lock the next row
        $PSQL <<EOF &
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

    echo -e "  ${GREEN}$NODES-node deadlock $round generated${NC}"
    sleep 0.5
done

echo ""
echo -e "${CYAN}=========================================="
echo -e "  Generated $COUNT deadlocks with $NODES nodes each"
echo -e "==========================================${NC}"
echo ""
echo -e "Check PostgreSQL logs for deadlock messages."
echo -e "In Steep, go to Locks view (${YELLOW}4${NC}) and switch to Deadlock History tab (${YELLOW}→${NC})."
echo ""
