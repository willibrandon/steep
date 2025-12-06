#!/bin/bash
# test_deadlocks.sh - Generate actual deadlocks for testing deadlock history
#
# Creates 2-node deadlock cycles between two concurrent sessions

set -e

# Defaults
HOST="localhost"
PORT="5432"
DB=""
USER="${PGUSER:-$(whoami)}"
COUNT=3

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

usage() {
    echo "Usage: $0 [OPTIONS] -d DATABASE"
    echo ""
    echo "Generate 2-node deadlocks for testing deadlock detection and history"
    echo ""
    echo "Options:"
    echo "  -h, --host HOST       PostgreSQL host (default: localhost)"
    echo "  -p, --port PORT       PostgreSQL port (default: 5432)"
    echo "  -d, --database DB     Database name (required)"
    echo "  -U, --user USER       PostgreSQL user (default: \$PGUSER or current user)"
    echo "  -n, --count N         Number of deadlocks to generate (default: 3)"
    echo "      --help            Show this help message"
    echo ""
    echo "Environment:"
    echo "  PGPASSWORD            Password for PostgreSQL connection"
    echo ""
    echo "Examples:"
    echo "  $0 -d postgres                    # Generate 3 deadlocks on localhost"
    echo "  $0 -h db.example.com -p 5433 -d mydb -n 5"
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

TABLE="test_deadlock_$$"
PSQL="psql -h $HOST -p $PORT -U $USER -d $DB"

echo -e "${CYAN}=========================================="
echo -e "2-Node Deadlock Generator"
echo -e "==========================================${NC}"
echo ""
echo -e "Host:     ${HOST}:${PORT}"
echo -e "Database: ${DB}"
echo -e "User:     ${USER}"
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

# Create test table
echo -e "${CYAN}Creating test table...${NC}"
$PSQL -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (
        id INT PRIMARY KEY,
        data TEXT
    );
    INSERT INTO $TABLE VALUES (1, 'row1'), (2, 'row2');
"

echo ""

for i in $(seq 1 $COUNT); do
    echo -e "${YELLOW}Generating deadlock $i of $COUNT...${NC}"

    # Session 1: Lock row 1, sleep, then try row 2
    $PSQL <<EOF &
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
    $PSQL <<EOF &
BEGIN;
UPDATE $TABLE SET data = 'session2_$i' WHERE id = 2;
UPDATE $TABLE SET data = 'session2_$i' WHERE id = 1;
COMMIT;
EOF
    PID2=$!

    # Wait for both to complete (one will be killed by deadlock detection)
    wait $PID1 2>/dev/null || true
    wait $PID2 2>/dev/null || true

    echo -e "  ${GREEN}Deadlock $i generated${NC}"
    sleep 0.5
done

echo ""
echo -e "${CYAN}=========================================="
echo -e "  Generated $COUNT deadlocks"
echo -e "==========================================${NC}"
echo ""
echo -e "Check PostgreSQL logs for deadlock messages."
echo -e "In Steep, go to Locks view (${YELLOW}4${NC}) and switch to Deadlock History tab (${YELLOW}→${NC})."
echo ""
