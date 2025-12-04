#!/usr/bin/env bash
#
# repl-test-setup.sh - Create a PostgreSQL replication test environment
#
# Usage:
#   ./scripts/repl-test-setup.sh [OPTIONS]
#
# Options:
#   --replicas N       Number of streaming replicas (default: 1)
#   --logical          Also set up logical replication (adds subscriber container)
#   --cascade          Set up cascading replication (replica1 -> replica2)
#   --pgversion VER    PostgreSQL version (default: 16)
#   --primary-port P   Primary port (default: 15432)
#   --generate-lag     Insert data to create visible lag
#   --help             Show this help message
#
# Examples:
#   ./scripts/repl-test-setup.sh                      # Basic: primary + 1 replica
#   ./scripts/repl-test-setup.sh --replicas 2         # Primary + 2 replicas
#   ./scripts/repl-test-setup.sh --logical            # Primary + replica + logical subscriber
#   ./scripts/repl-test-setup.sh --cascade            # Primary -> replica1 -> replica2
#

set -euo pipefail

# Default configuration
REPLICAS=1
LOGICAL=false
CASCADE=false
PG_VERSION=18
PRIMARY_PORT=15432
GENERATE_LAG=false
NETWORK_NAME="steep-repl-test"
PRIMARY_NAME="steep-pg-primary"
REPL_USER="replicator"
REPL_PASS="repl_password"
POSTGRES_PASS="postgres"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Get PostgreSQL data directory path (PG18+ uses versioned path)
get_pgdata() {
    local version=$1
    local major_version="${version%%.*}"  # Extract major version (e.g., "18" from "18.1")
    if [[ "$major_version" -ge 18 ]]; then
        echo "/var/lib/postgresql/${major_version}/docker"
    else
        echo "/var/lib/postgresql/data"
    fi
}

show_help() {
    sed -n '2,/^$/p' "$0" | sed 's/^#//' | sed 's/^ //'
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --replicas)
            REPLICAS="$2"
            shift 2
            ;;
        --logical)
            LOGICAL=true
            shift
            ;;
        --cascade)
            CASCADE=true
            REPLICAS=2  # Cascade requires at least 2 replicas
            shift
            ;;
        --pgversion)
            PG_VERSION="$2"
            shift 2
            ;;
        --primary-port)
            PRIMARY_PORT="$2"
            shift 2
            ;;
        --generate-lag)
            GENERATE_LAG=true
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

# Set data directory path based on PostgreSQL version
PGDATA=$(get_pgdata "$PG_VERSION")

# Check Docker is available
if ! command -v docker &> /dev/null; then
    log_error "Docker is not installed or not in PATH"
    exit 1
fi

# Check if containers already exist
if docker ps -a --format '{{.Names}}' | grep -q "^${PRIMARY_NAME}$"; then
    log_warn "Containers already exist. Run ./scripts/repl-test-teardown.sh first."
    exit 1
fi

log_info "Setting up PostgreSQL replication test environment"
log_info "  PostgreSQL version: $PG_VERSION"
log_info "  Data directory: $PGDATA"
log_info "  Streaming replicas: $REPLICAS"
log_info "  Logical replication: $LOGICAL"
log_info "  Cascading: $CASCADE"
log_info "  Primary port: $PRIMARY_PORT"
echo

# Create network
log_info "Creating Docker network: $NETWORK_NAME"
docker network create "$NETWORK_NAME" 2>/dev/null || true
log_success "Network ready"

# Start primary
log_info "Starting primary server..."
docker run -d \
    --name "$PRIMARY_NAME" \
    --network "$NETWORK_NAME" \
    --hostname pg-primary \
    --shm-size=256m \
    -e POSTGRES_PASSWORD="$POSTGRES_PASS" \
    -e POSTGRES_HOST_AUTH_METHOD=scram-sha-256 \
    -e POSTGRES_INITDB_ARGS="--auth-host=scram-sha-256" \
    -p "${PRIMARY_PORT}:5432" \
    "postgres:${PG_VERSION}" \
    -c wal_level=logical \
    -c max_wal_senders=100 \
    -c max_replication_slots=100 \
    -c hot_standby=on \
    -c hot_standby_feedback=on \
    -c wal_keep_size=256MB \
    -c max_connections=100 \
    -c listen_addresses='*' \
    > /dev/null

# Wait for primary to be ready
log_info "Waiting for primary to be ready..."
for i in {1..30}; do
    if docker exec "$PRIMARY_NAME" pg_isready -U postgres -q 2>/dev/null; then
        break
    fi
    sleep 1
done

if ! docker exec "$PRIMARY_NAME" pg_isready -U postgres -q 2>/dev/null; then
    log_error "Primary failed to start"
    exit 1
fi
log_success "Primary is ready"

# Configure pg_hba.conf for replication
log_info "Configuring replication access..."
docker exec "$PRIMARY_NAME" bash -c "echo '# Replication connections' >> ${PGDATA}/pg_hba.conf"
docker exec "$PRIMARY_NAME" bash -c "echo 'host    replication     ${REPL_USER}      0.0.0.0/0       scram-sha-256' >> ${PGDATA}/pg_hba.conf"
docker exec "$PRIMARY_NAME" bash -c "echo 'host    all             all             0.0.0.0/0       scram-sha-256' >> ${PGDATA}/pg_hba.conf"

# Reload configuration (must run as postgres user, not root)
docker exec -u postgres "$PRIMARY_NAME" pg_ctl reload -D "${PGDATA}" > /dev/null 2>&1
log_success "Replication access configured"

# Create replication user
log_info "Creating replication user..."
docker exec "$PRIMARY_NAME" psql -U postgres -c \
    "CREATE USER $REPL_USER WITH REPLICATION LOGIN PASSWORD '$REPL_PASS';" > /dev/null 2>&1
log_success "Replication user created: $REPL_USER"

# Create test database and table for lag generation
log_info "Creating test database..."
docker exec "$PRIMARY_NAME" psql -U postgres -c "CREATE DATABASE steep_test;" > /dev/null 2>&1
docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -c "
    CREATE TABLE lag_test (
        id SERIAL PRIMARY KEY,
        data TEXT,
        padding TEXT,
        created_at TIMESTAMP DEFAULT NOW()
    );
    CREATE INDEX idx_lag_test_created ON lag_test(created_at);
" > /dev/null 2>&1
log_success "Test database created"

# Function to create a streaming replica
create_replica() {
    local replica_num=$1
    local upstream_name=$2
    local upstream_host=$3
    local replica_name="steep-pg-replica${replica_num}"
    local replica_port=$((PRIMARY_PORT + replica_num))
    local slot_name="replica${replica_num}_slot"

    log_info "Creating replication slot: $slot_name on $upstream_name..."
    docker exec "$upstream_name" psql -U postgres -c \
        "SELECT pg_create_physical_replication_slot('$slot_name');" > /dev/null 2>&1

    log_info "Taking base backup for replica${replica_num}..."

    # Create a volume for the replica data
    docker volume create "steep-replica${replica_num}-data" > /dev/null 2>&1

    # Run pg_basebackup in a temporary container
    docker run --rm \
        --network "$NETWORK_NAME" \
        -v "steep-replica${replica_num}-data:${PGDATA}" \
        "postgres:${PG_VERSION}" \
        bash -c "
            PGPASSWORD='$REPL_PASS' pg_basebackup \
                -h $upstream_host \
                -U $REPL_USER \
                -D ${PGDATA} \
                -Fp -Xs -P -R \
                -S $slot_name \
                2>/dev/null

            # Set primary_conninfo and slot name in postgresql.auto.conf
            cat >> ${PGDATA}/postgresql.auto.conf << EOF
primary_conninfo = 'host=$upstream_host port=5432 user=$REPL_USER password=$REPL_PASS application_name=replica${replica_num}'
primary_slot_name = '$slot_name'
EOF

            # Ensure proper permissions
            chmod 700 ${PGDATA}
        " > /dev/null 2>&1

    log_info "Starting replica${replica_num}..."
    docker run -d \
        --name "$replica_name" \
        --network "$NETWORK_NAME" \
        --hostname "pg-replica${replica_num}" \
        --shm-size=256m \
        -e POSTGRES_PASSWORD="$POSTGRES_PASS" \
        -v "steep-replica${replica_num}-data:${PGDATA}" \
        -p "${replica_port}:5432" \
        "postgres:${PG_VERSION}" \
        -c hot_standby=on \
        -c max_wal_senders=100 \
        -c max_replication_slots=100 \
        -c wal_level=logical \
        > /dev/null

    # Wait for replica to be ready
    log_info "Waiting for replica${replica_num} to be ready..."
    for i in {1..30}; do
        if docker exec "$replica_name" pg_isready -U postgres -q 2>/dev/null; then
            break
        fi
        sleep 1
    done

    if docker exec "$replica_name" pg_isready -U postgres -q 2>/dev/null; then
        log_success "Replica${replica_num} is ready (port: $replica_port)"
    else
        log_error "Replica${replica_num} failed to start"
        return 1
    fi
}

# Create streaming replicas
if [[ "$CASCADE" == "true" ]]; then
    # Cascading: primary -> replica1 -> replica2
    create_replica 1 "$PRIMARY_NAME" "pg-primary"

    # Wait a bit for replica1 to catch up
    sleep 2

    # Create replica2 from replica1
    log_info "Setting up cascading replication (replica1 -> replica2)..."
    create_replica 2 "steep-pg-replica1" "pg-replica1"
else
    # Standard: all replicas connect to primary
    for i in $(seq 1 "$REPLICAS"); do
        create_replica "$i" "$PRIMARY_NAME" "pg-primary"
    done
fi

# Set up logical replication if requested
if [[ "$LOGICAL" == "true" ]]; then
    log_info "Setting up logical replication..."

    # Create publication on primary
    docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -c "
        CREATE PUBLICATION test_publication FOR TABLE lag_test;
    " > /dev/null 2>&1
    log_success "Publication created: test_publication"

    # Create a separate subscriber container (not a streaming replica)
    SUBSCRIBER_NAME="steep-pg-subscriber"
    SUBSCRIBER_PORT=$((PRIMARY_PORT + REPLICAS + 1))

    log_info "Starting logical subscriber..."
    docker run -d \
        --name "$SUBSCRIBER_NAME" \
        --network "$NETWORK_NAME" \
        --hostname pg-subscriber \
        --shm-size=256m \
        -e POSTGRES_PASSWORD="$POSTGRES_PASS" \
        -p "${SUBSCRIBER_PORT}:5432" \
        "postgres:${PG_VERSION}" \
        -c wal_level=logical \
        > /dev/null

    # Wait for subscriber to be ready
    for i in {1..30}; do
        if docker exec "$SUBSCRIBER_NAME" pg_isready -U postgres -q 2>/dev/null; then
            break
        fi
        sleep 1
    done

    # Create matching database and table structure on subscriber
    docker exec "$SUBSCRIBER_NAME" psql -U postgres -c "CREATE DATABASE steep_test;" > /dev/null 2>&1
    docker exec "$SUBSCRIBER_NAME" psql -U postgres -d steep_test -c "
        CREATE TABLE lag_test (
            id SERIAL PRIMARY KEY,
            data TEXT,
            padding TEXT,
            created_at TIMESTAMP DEFAULT NOW()
        );
        CREATE INDEX idx_lag_test_created ON lag_test(created_at);
    " > /dev/null 2>&1

    # Create subscription
    docker exec "$SUBSCRIBER_NAME" psql -U postgres -d steep_test -c "
        CREATE SUBSCRIPTION test_subscription
        CONNECTION 'host=pg-primary port=5432 dbname=steep_test user=postgres password=$POSTGRES_PASS'
        PUBLICATION test_publication;
    " > /dev/null 2>&1

    log_success "Subscriber ready (port: $SUBSCRIBER_PORT)"
    log_success "Subscription created: test_subscription"
fi

# Generate some lag if requested
if [[ "$GENERATE_LAG" == "true" ]]; then
    log_info "Generating test data to create lag..."
    docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -c "
        INSERT INTO lag_test (data)
        SELECT md5(random()::text)
        FROM generate_series(1, 10000);
    " > /dev/null 2>&1
    log_success "Test data inserted"
fi

# Verify replication is working
echo
log_info "Verifying replication status..."
sleep 2

REPL_STATUS=$(docker exec "$PRIMARY_NAME" psql -U postgres -t -c "
    SELECT application_name, state, sync_state,
           pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes
    FROM pg_stat_replication;
" 2>/dev/null)

if [[ -n "$REPL_STATUS" ]]; then
    log_success "Streaming replication is active:"
    docker exec "$PRIMARY_NAME" psql -U postgres -c "
        SELECT application_name AS replica,
               state,
               sync_state AS sync,
               pg_size_pretty(pg_wal_lsn_diff(sent_lsn, replay_lsn)) AS lag
        FROM pg_stat_replication;
    " 2>/dev/null
else
    log_warn "No streaming replicas connected yet (may need a moment to sync)"
fi

# Show slot status
echo
log_info "Replication slots:"
docker exec "$PRIMARY_NAME" psql -U postgres -c "
    SELECT slot_name, slot_type, active,
           pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained
    FROM pg_replication_slots;
" 2>/dev/null

# Show logical replication status if enabled
if [[ "$LOGICAL" == "true" ]]; then
    echo
    log_info "Publications:"
    docker exec "$PRIMARY_NAME" psql -U postgres -d steep_test -c "
        SELECT pubname, puballtables, pubinsert, pubupdate, pubdelete
        FROM pg_publication;
    " 2>/dev/null

    echo
    log_info "Subscriptions:"
    docker exec "$SUBSCRIBER_NAME" psql -U postgres -d steep_test -c "
        SELECT subname, subenabled, subconninfo
        FROM pg_subscription;
    " 2>/dev/null
fi

# Print connection info
echo
echo "=========================================="
log_success "Replication test environment is ready!"
echo "=========================================="
echo
echo "PostgreSQL connection details:"
echo "  Primary:    localhost:${PRIMARY_PORT} (user: postgres, password: ${POSTGRES_PASS})"
for i in $(seq 1 "$REPLICAS"); do
    echo "  Replica${i}:   localhost:$((PRIMARY_PORT + i)) (user: postgres, password: ${POSTGRES_PASS})"
done
if [[ "$LOGICAL" == "true" ]]; then
    echo "  Subscriber: localhost:$((PRIMARY_PORT + REPLICAS + 1)) (database: steep_test)"
fi
echo
echo "Quick start with Steep (use environment variables):"
echo "  export STEEP_CONNECTION_HOST=localhost"
echo "  export STEEP_CONNECTION_PORT=${PRIMARY_PORT}"
echo "  export STEEP_CONNECTION_USER=postgres"
echo "  export STEEP_CONNECTION_DATABASE=postgres"
echo "  export PGPASSWORD=${POSTGRES_PASS}"
echo "  ./bin/steep"
echo
echo "Or edit config.yaml to set port: ${PRIMARY_PORT}"
echo
echo "To generate lag:"
echo "  docker exec $PRIMARY_NAME psql -U postgres -d steep_test -c \\"
echo "    \"INSERT INTO lag_test (data) SELECT md5(random()::text) FROM generate_series(1, 100000);\""
echo
echo "To tear down:"
echo "  ./scripts/repl-test-teardown.sh"
echo
