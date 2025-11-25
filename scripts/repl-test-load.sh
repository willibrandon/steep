#!/bin/bash
# repl-test-load.sh - Generate load on the replication test environment
# This script inserts, updates, and deletes data to create replication traffic

set -e

# Defaults
PRIMARY_PORT=15432
DB_NAME="steep_test"
BATCH_SIZE=100
ITERATIONS=0  # 0 = infinite
DELAY=1

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Generate load on the replication test environment"
    echo ""
    echo "Options:"
    echo "  -p, --port PORT       Primary port (default: 15432)"
    echo "  -d, --database DB     Database name (default: steep_test)"
    echo "  -b, --batch SIZE      Batch size per operation (default: 100)"
    echo "  -n, --iterations N    Number of iterations, 0=infinite (default: 0)"
    echo "  -s, --delay SECONDS   Delay between iterations (default: 1)"
    echo "  -h, --help            Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                           # Run with defaults"
    echo "  $0 -b 500                    # 500 rows per batch"
    echo "  $0 -n 10                     # Run 10 iterations then stop"
    echo "  $0 -b 200 -n 50 -s 0.5       # 200 rows, 50 iterations, 0.5s delay"
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -p|--port)
            PRIMARY_PORT="$2"
            shift 2
            ;;
        -d|--database)
            DB_NAME="$2"
            shift 2
            ;;
        -b|--batch)
            BATCH_SIZE="$2"
            shift 2
            ;;
        -n|--iterations)
            ITERATIONS="$2"
            shift 2
            ;;
        -s|--delay)
            DELAY="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

export PGPASSWORD="${PGPASSWORD:-postgres}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}=========================================="
echo -e "Replication Load Generator"
echo -e "==========================================${NC}"
echo ""
echo -e "Primary: localhost:${PRIMARY_PORT}/${DB_NAME}"
echo -e "Batch size: ${BATCH_SIZE} rows"
echo -e "Iterations: $([ "$ITERATIONS" -eq 0 ] && echo "infinite" || echo "$ITERATIONS")"
echo -e "Delay: ${DELAY}s"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop${NC}"
echo ""

# Check connection
if ! psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}Error: Cannot connect to primary at localhost:${PRIMARY_PORT}${NC}"
    echo "Make sure the replication test environment is running:"
    echo "  ./scripts/repl-test-setup.sh"
    exit 1
fi

# Create load test table if it doesn't exist
echo -e "${CYAN}Creating load test table...${NC}"
psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" <<'EOF'
CREATE TABLE IF NOT EXISTS load_test (
    id SERIAL PRIMARY KEY,
    data TEXT,
    counter INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_load_test_created ON load_test(created_at);
CREATE INDEX IF NOT EXISTS idx_load_test_counter ON load_test(counter);
EOF

iteration=0
total_inserts=0
total_updates=0
total_deletes=0

cleanup() {
    echo ""
    echo -e "${CYAN}=========================================="
    echo -e "Load Generation Summary"
    echo -e "==========================================${NC}"
    echo -e "Iterations completed: ${iteration}"
    echo -e "Total inserts: ${total_inserts}"
    echo -e "Total updates: ${total_updates}"
    echo -e "Total deletes: ${total_deletes}"
    echo ""
    exit 0
}

trap cleanup SIGINT SIGTERM

while true; do
    iteration=$((iteration + 1))

    echo -e "${GREEN}--- Iteration ${iteration} ---${NC}"

    # INSERT batch
    echo -n "  Inserting ${BATCH_SIZE} rows... "
    psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -q <<EOF
INSERT INTO load_test (data, counter)
SELECT
    md5(random()::text) || md5(random()::text),
    floor(random() * 1000)::int
FROM generate_series(1, ${BATCH_SIZE});
EOF
    total_inserts=$((total_inserts + BATCH_SIZE))
    echo -e "${GREEN}done${NC}"

    sleep 0.5

    # UPDATE random rows
    echo -n "  Updating random rows... "
    updated=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -q <<EOF
UPDATE load_test
SET counter = counter + 1,
    updated_at = NOW(),
    data = md5(random()::text)
WHERE id IN (
    SELECT id FROM load_test
    ORDER BY random()
    LIMIT ${BATCH_SIZE}
);
SELECT count(*) FROM load_test WHERE updated_at > NOW() - interval '2 seconds';
EOF
)
    updated=$(echo "$updated" | tr -d ' ')
    total_updates=$((total_updates + updated))
    echo -e "${GREEN}${updated} rows${NC}"

    sleep 0.5

    # DELETE oldest rows (keep table size manageable)
    echo -n "  Deleting old rows... "
    deleted=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -q <<EOF
WITH deleted AS (
    DELETE FROM load_test
    WHERE id IN (
        SELECT id FROM load_test
        ORDER BY created_at ASC
        LIMIT ${BATCH_SIZE}
    )
    RETURNING id
)
SELECT count(*) FROM deleted;
EOF
)
    deleted=$(echo "$deleted" | tr -d ' ')
    total_deletes=$((total_deletes + deleted))
    echo -e "${GREEN}${deleted} rows${NC}"

    # Show current table size
    row_count=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -q -c "SELECT count(*) FROM load_test")
    row_count=$(echo "$row_count" | tr -d ' ')
    echo -e "  Table size: ${YELLOW}${row_count} rows${NC}"

    # Check if we've reached iteration limit
    if [ "$ITERATIONS" -gt 0 ] && [ "$iteration" -ge "$ITERATIONS" ]; then
        echo ""
        echo -e "${CYAN}Reached iteration limit (${ITERATIONS})${NC}"
        cleanup
    fi

    # Delay between iterations
    sleep "$DELAY"
done
