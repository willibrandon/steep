#!/usr/bin/env bash
#
# repl-test-status.sh - Show PostgreSQL replication test environment status
#
# Usage:
#   ./scripts/repl-test-status.sh [OPTIONS]
#
# Options:
#   --watch           Continuously monitor (refresh every 2s)
#   --json            Output in JSON format
#   --help            Show this help message
#

set -euo pipefail

WATCH=false
JSON=false
PRIMARY_NAME="steep-pg-primary"
CONTAINER_PREFIX="steep-pg-"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

show_help() {
    sed -n '2,/^$/p' "$0" | sed 's/^#//' | sed 's/^ //'
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --watch|-w)
            WATCH=true
            shift
            ;;
        --json)
            JSON=true
            shift
            ;;
        --help|-h)
            show_help
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

show_status() {
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')

    if [[ "$JSON" == "true" ]]; then
        show_status_json
        return
    fi

    echo "=========================================="
    echo -e "${CYAN}Replication Test Environment Status${NC}"
    echo "Time: $timestamp"
    echo "=========================================="
    echo

    # Check if primary is running
    if ! docker ps --format "{{.Names}}" | grep -q "^${PRIMARY_NAME}$"; then
        log_error "Primary container is not running"
        echo
        echo "Run ./scripts/repl-test-setup.sh to create the environment"
        return 1
    fi

    # Container status
    echo -e "${BLUE}Containers:${NC}"
    docker ps -a --filter "name=${CONTAINER_PREFIX}" \
        --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null || true
    echo

    # Primary info
    echo -e "${BLUE}Primary Server:${NC}"
    docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
        SELECT 'Version: ' || version();
    " 2>/dev/null | head -1 || log_error "Cannot connect to primary"

    local is_primary
    is_primary=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "SELECT NOT pg_is_in_recovery();" 2>/dev/null | tr -d ' ')
    if [[ "$is_primary" == "t" ]]; then
        echo -e "  Role: ${GREEN}PRIMARY${NC}"
    else
        echo -e "  Role: ${YELLOW}STANDBY${NC}"
    fi
    echo

    # Replication status
    echo -e "${BLUE}Streaming Replication:${NC}"
    local repl_count
    repl_count=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "SELECT count(*) FROM pg_stat_replication;" 2>/dev/null | tr -d ' ')

    if [[ "$repl_count" -gt 0 ]]; then
        docker exec "$PRIMARY_NAME" psql -U postgres -c "
            SELECT
                application_name AS replica,
                client_addr AS address,
                state,
                sync_state AS sync,
                pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) AS lag,
                CASE
                    WHEN pg_wal_lsn_diff(sent_lsn, replay_lsn) < 1048576 THEN 'healthy'
                    WHEN pg_wal_lsn_diff(sent_lsn, replay_lsn) < 10485760 THEN 'warning'
                    ELSE 'critical'
                END AS status
            FROM pg_stat_replication
            ORDER BY application_name;
        " 2>/dev/null
    else
        echo "  No streaming replicas connected"
    fi
    echo

    # Replication slots
    echo -e "${BLUE}Replication Slots:${NC}"
    local slot_count
    slot_count=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "SELECT count(*) FROM pg_replication_slots;" 2>/dev/null | tr -d ' ')

    if [[ "$slot_count" -gt 0 ]]; then
        docker exec "$PRIMARY_NAME" psql -U postgres -c "
            SELECT
                slot_name,
                slot_type AS type,
                CASE WHEN active THEN 'yes' ELSE 'no' END AS active,
                pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained,
                COALESCE(wal_status, '-') AS wal_status
            FROM pg_replication_slots
            ORDER BY slot_name;
        " 2>/dev/null
    else
        echo "  No replication slots"
    fi
    echo

    # Check for logical replication
    local pub_count
    pub_count=$(docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -t -c "SELECT count(*) FROM pg_publication;" 2>/dev/null | tr -d ' ' || echo "0")

    if [[ "$pub_count" -gt 0 ]]; then
        echo -e "${BLUE}Publications (Primary):${NC}"
        docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -c "
            SELECT
                pubname AS name,
                CASE WHEN puballtables THEN 'all' ELSE 'partial' END AS tables,
                CONCAT_WS(' ',
                    CASE WHEN pubinsert THEN 'I' END,
                    CASE WHEN pubupdate THEN 'U' END,
                    CASE WHEN pubdelete THEN 'D' END,
                    CASE WHEN pubtruncate THEN 'T' END
                ) AS operations
            FROM pg_publication;
        " 2>/dev/null
        echo
    fi

    # Check subscriber if it exists
    if docker ps --format "{{.Names}}" | grep -q "steep-pg-subscriber"; then
        echo -e "${BLUE}Subscriptions (Subscriber):${NC}"
        docker exec steep-pg-subscriber psql -U postgres -d steep_test -c "
            SELECT
                subname AS name,
                CASE WHEN subenabled THEN 'yes' ELSE 'no' END AS enabled,
                substring(subconninfo from 'host=([^ ]+)') AS provider
            FROM pg_subscription;
        " 2>/dev/null
        echo
    fi

    # Configuration status
    echo -e "${BLUE}Configuration:${NC}"
    docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
        SELECT
            '  wal_level: ' || current_setting('wal_level') ||
            CASE WHEN current_setting('wal_level') IN ('replica', 'logical') THEN ' (OK)' ELSE ' (needs config)' END;
    " 2>/dev/null

    docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
        SELECT
            '  max_wal_senders: ' || current_setting('max_wal_senders') ||
            CASE WHEN current_setting('max_wal_senders')::int > 0 THEN ' (OK)' ELSE ' (needs config)' END;
    " 2>/dev/null

    docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
        SELECT
            '  max_replication_slots: ' || current_setting('max_replication_slots') ||
            CASE WHEN current_setting('max_replication_slots')::int > 0 THEN ' (OK)' ELSE ' (needs config)' END;
    " 2>/dev/null
    echo

    # Connection info
    echo -e "${BLUE}Connection Info:${NC}"
    while IFS= read -r line; do
        local name port
        name=$(echo "$line" | cut -d'|' -f1)
        port=$(echo "$line" | cut -d'|' -f2 | grep -oE '0\.0\.0\.0:[0-9]+' | cut -d: -f2 | head -1)
        if [[ -n "$port" ]]; then
            local role="replica"
            if [[ "$name" == "$PRIMARY_NAME" ]]; then
                role="primary"
            elif [[ "$name" == *"subscriber"* ]]; then
                role="subscriber"
            fi
            echo "  $name ($role): postgres://postgres:postgres@localhost:$port/postgres"
        fi
    done < <(docker ps --filter "name=${CONTAINER_PREFIX}" --format "{{.Names}}|{{.Ports}}" 2>/dev/null)
    echo
}

show_status_json() {
    # Build JSON output for programmatic use
    local containers replication slots

    containers=$(docker ps -a --filter "name=${CONTAINER_PREFIX}" --format '{"name":"{{.Names}}","status":"{{.Status}}","ports":"{{.Ports}}"}' 2>/dev/null | jq -s '.')

    if docker ps --format "{{.Names}}" | grep -q "^${PRIMARY_NAME}$"; then
        replication=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
            SELECT json_agg(row_to_json(r))
            FROM (
                SELECT application_name, state, sync_state,
                       pg_wal_lsn_diff(sent_lsn, replay_lsn) AS lag_bytes
                FROM pg_stat_replication
            ) r;
        " 2>/dev/null | tr -d ' \n' || echo "null")

        slots=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
            SELECT json_agg(row_to_json(s))
            FROM (
                SELECT slot_name, slot_type, active,
                       pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS retained_bytes
                FROM pg_replication_slots
            ) s;
        " 2>/dev/null | tr -d ' \n' || echo "null")
    else
        replication="null"
        slots="null"
    fi

    cat << EOF
{
  "timestamp": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
  "containers": $containers,
  "replication": $replication,
  "slots": $slots
}
EOF
}

# Main execution
if [[ "$WATCH" == "true" ]]; then
    while true; do
        clear
        show_status
        echo -e "${CYAN}Refreshing every 2 seconds... Press Ctrl+C to stop${NC}"
        sleep 2
    done
else
    show_status
fi
