#!/bin/bash
# repl-test-lag.sh - Generate heavy load to create visible replication lag
# This script creates bursts of writes to intentionally cause lag for testing

set -e

PRIMARY_PORT=15432
DB_NAME="steep_test"
BURST_SIZE=10000    # Large batch per burst
BURST_COUNT=5       # Number of bursts
BURST_DELAY=0.1     # Short delay between bursts

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Generate heavy load to create visible replication lag for testing"
    echo ""
    echo "Options:"
    echo "  -p, --port PORT       Primary port (default: 15432)"
    echo "  -d, --database DB     Database name (default: steep_test)"
    echo "  -b, --burst SIZE      Rows per burst (default: 10000)"
    echo "  -n, --count N         Number of bursts (default: 5)"
    echo "  -s, --delay SECONDS   Delay between bursts (default: 0.1)"
    echo "  -h, --help            Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                    # Run with defaults (50k rows total)"
    echo "  $0 -b 50000 -n 3      # 3 bursts of 50k rows each"
    echo "  $0 -b 100000 -n 1     # Single burst of 100k rows"
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -p|--port) PRIMARY_PORT="$2"; shift 2 ;;
        -d|--database) DB_NAME="$2"; shift 2 ;;
        -b|--burst) BURST_SIZE="$2"; shift 2 ;;
        -n|--count) BURST_COUNT="$2"; shift 2 ;;
        -s|--delay) BURST_DELAY="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
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
echo -e "Replication Lag Generator"
echo -e "==========================================${NC}"
echo ""
echo -e "Primary: localhost:${PRIMARY_PORT}/${DB_NAME}"
echo -e "Burst size: ${BURST_SIZE} rows"
echo -e "Burst count: ${BURST_COUNT}"
echo -e "Total rows: $((BURST_SIZE * BURST_COUNT))"
echo ""

# Check connection
if ! psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}Error: Cannot connect to primary at localhost:${PRIMARY_PORT}${NC}"
    echo "Make sure the replication test environment is running:"
    echo "  ./scripts/repl-test-setup.sh"
    exit 1
fi

# Ensure lag_test table exists (created by repl-test-setup.sh)
echo -e "${CYAN}Verifying lag test table...${NC}"
if ! psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -tAc "SELECT 1 FROM pg_tables WHERE tablename='lag_test'" | grep -q 1; then
    echo -e "${RED}Error: lag_test table not found. Run ./scripts/repl-test-setup.sh first${NC}"
    exit 1
fi

# Show initial replication status
echo -e "${CYAN}Initial replication status:${NC}"
psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -c "
SELECT
    application_name,
    pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes,
    pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) as lag_pretty
FROM pg_stat_replication;
"

echo ""
echo -e "${YELLOW}Starting burst writes - watch Steep for lag!${NC}"
echo ""

TOTAL_ROWS=$((BURST_SIZE * BURST_COUNT))
echo -e "${GREEN}Inserting ${TOTAL_ROWS} rows in a single transaction...${NC}"
echo -e "${YELLOW}(This keeps the transaction open longer to create visible lag)${NC}"
echo ""

# Use a single large transaction to maximize WAL generation before replica catches up
# The key is generating lots of WAL in a short time
psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" <<EOF &
BEGIN;
-- Insert in batches within the same transaction
$(for i in $(seq 1 $BURST_COUNT); do
echo "INSERT INTO lag_test (data, padding) SELECT md5(random()::text) || md5(random()::text), repeat(md5(random()::text), 20) FROM generate_series(1, ${BURST_SIZE});"
done)
COMMIT;
EOF

INSERT_PID=$!

# Monitor lag while insert is running
echo -e "${CYAN}Monitoring lag during insert...${NC}"
for i in {1..20}; do
    if ! kill -0 $INSERT_PID 2>/dev/null; then
        echo -e "${GREEN}Insert completed${NC}"
        break
    fi

    lag_info=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -c "
    SELECT
        application_name,
        pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) as byte_lag,
        replay_lag::text as time_lag
    FROM pg_stat_replication
    LIMIT 1;
    " 2>/dev/null | tr -s ' ')

    echo -e "  ${YELLOW}${lag_info:-checking...}${NC}"
    sleep 0.3
done

# Wait for insert to finish
wait $INSERT_PID 2>/dev/null

echo ""
echo -e "${CYAN}Burst complete! Monitoring lag recovery...${NC}"
echo ""

# Monitor lag recovery
for i in {1..10}; do
    lag_bytes=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -c "
    SELECT COALESCE(pg_wal_lsn_diff(sent_lsn, replay_lsn), 0)
    FROM pg_stat_replication
    LIMIT 1;
    " 2>/dev/null | tr -d ' ')

    lag_pretty=$(psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -t -c "
    SELECT COALESCE(pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)), '0 bytes')
    FROM pg_stat_replication
    LIMIT 1;
    " 2>/dev/null | tr -d ' ')

    echo -e "  Lag: ${YELLOW}${lag_pretty:-no replicas}${NC}"

    if [ "${lag_bytes:-0}" -eq 0 ]; then
        echo -e "${GREEN}Replica caught up!${NC}"
        break
    fi

    sleep 0.5
done

echo ""
echo -e "${CYAN}Final table size:${NC}"
psql -h localhost -p "$PRIMARY_PORT" -U postgres -d "$DB_NAME" -c "
SELECT
    pg_size_pretty(pg_total_relation_size('lag_test')) as table_size,
    count(*) as row_count
FROM lag_test;
"

echo ""
echo -e "${CYAN}Cleanup: TRUNCATE lag_test; to remove test data${NC}"
