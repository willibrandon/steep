#!/bin/bash
# setup-two-node.sh - Interactive setup for a 2-node replication environment
# Uses gum for interactive prompts and Docker for PostgreSQL containers.
#
# This script ONLY handles environment setup/teardown:
# - Docker network for container communication
# - PostgreSQL containers (pg-source, pg-target)
# - steep_repl extension on both nodes
# - Publications on both nodes (for bidirectional replication)
# - Test data
#
# All replication workflow should be done via steep-repl CLI commands.
#
# Usage: ./setup-two-node.sh [--teardown]

set -e

# Check for required tools
check_deps() {
    local missing=()

    if ! command -v gum &>/dev/null; then
        missing+=("gum")
    fi
    if ! command -v docker &>/dev/null; then
        missing+=("docker")
    fi
    if ! command -v psql &>/dev/null; then
        missing+=("psql")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "Missing required tools: ${missing[*]}"
        echo "Install with:"
        echo "  brew install gum docker postgresql"
        exit 1
    fi
}

# Configuration
NETWORK_NAME="steep-repl-test"
SOURCE_CONTAINER="pg-source"
TARGET_CONTAINER="pg-target"
DOCKER_IMAGE="ghcr.io/willibrandon/pg18-steep-repl:latest"
PG_USER="test"
PG_PASSWORD="test"
PG_DATABASE="testdb"

# Colors for gum
HEADER_FG="#7C3AED"
SUCCESS_FG="#22C55E"
ERROR_FG="#EF4444"
INFO_FG="#3B82F6"

header() {
    gum style --foreground "$HEADER_FG" --bold "$1"
}

success() {
    gum style --foreground "$SUCCESS_FG" "✓ $1"
}

error() {
    gum style --foreground "$ERROR_FG" "✗ $1"
}

info() {
    gum style --foreground "$INFO_FG" "→ $1"
}

# Teardown existing environment
teardown() {
    header "Tearing down existing environment..."

    # Stop and remove containers
    for container in "$SOURCE_CONTAINER" "$TARGET_CONTAINER"; do
        if docker ps -a --format '{{.Names}}' | grep -q "^${container}$"; then
            info "Stopping $container..."
            if docker stop "$container" >/dev/null 2>&1; then
                if docker rm "$container" >/dev/null 2>&1; then
                    success "Removed $container"
                else
                    error "Failed to remove $container"
                fi
            else
                error "Failed to stop $container"
            fi
        else
            info "$container not found (already removed)"
        fi
    done

    # Remove network
    if docker network ls --format '{{.Name}}' | grep -q "^${NETWORK_NAME}$"; then
        info "Removing network $NETWORK_NAME..."
        if docker network rm "$NETWORK_NAME" >/dev/null 2>&1; then
            success "Removed network $NETWORK_NAME"
        else
            error "Failed to remove network $NETWORK_NAME"
        fi
    else
        info "Network $NETWORK_NAME not found (already removed)"
    fi
}

# Create Docker network
create_network() {
    header "Creating Docker network..."

    if docker network ls --format '{{.Name}}' | grep -q "^${NETWORK_NAME}$"; then
        info "Network $NETWORK_NAME already exists"
    else
        gum spin --spinner dot --title "Creating network $NETWORK_NAME..." -- \
            docker network create "$NETWORK_NAME"
        success "Created network $NETWORK_NAME"
    fi
}

# Start a PostgreSQL container
start_container() {
    local name=$1
    local alias=$2
    local host_port=$3

    header "Starting $name container..."

    if docker ps --format '{{.Names}}' | grep -q "^${name}$"; then
        info "Container $name already running"
        return
    fi

    if docker ps -a --format '{{.Names}}' | grep -q "^${name}$"; then
        info "Starting existing container $name..."
        docker start "$name"
    else
        gum spin --spinner dot --title "Starting $name on port $host_port..." -- \
            docker run -d \
                --name "$name" \
                --network "$NETWORK_NAME" \
                --network-alias "$alias" \
                -p "${host_port}:5432" \
                -e POSTGRES_USER="$PG_USER" \
                -e POSTGRES_PASSWORD="$PG_PASSWORD" \
                -e POSTGRES_DB="$PG_DATABASE" \
                "$DOCKER_IMAGE" \
                -c shared_preload_libraries=steep_repl \
                -c wal_level=logical \
                -c max_wal_senders=10 \
                -c max_replication_slots=10 \
                -c max_logical_replication_workers=10
    fi

    success "Started $name (localhost:$host_port)"
}

# Wait for database to be ready
wait_for_db() {
    local name=$1
    local port=$2

    info "Waiting for $name to be ready..."

    local max_attempts=30
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
        if PGPASSWORD="$PG_PASSWORD" psql -h localhost -p "$port" -U "$PG_USER" -d "$PG_DATABASE" -c "SELECT 1" &>/dev/null; then
            success "$name is ready"
            return 0
        fi
        gum spin --spinner dot --title "Waiting for $name ($attempt/$max_attempts)..." -- sleep 1
        ((attempt++))
    done

    error "$name did not become ready"
    return 1
}

# Setup extension and register database in central catalog
setup_extension() {
    local name=$1
    local port=$2

    header "Setting up steep_repl extension on $name..."

    # Step 1: Ensure extension is installed in postgres database (central catalog)
    PGPASSWORD="$PG_PASSWORD" psql -h localhost -p "$port" -U "$PG_USER" -d postgres <<EOF
-- Create extension in postgres (central catalog)
CREATE EXTENSION IF NOT EXISTS steep_repl;
EOF

    # Step 2: Register the target database in the central catalog
    PGPASSWORD="$PG_PASSWORD" psql -h localhost -p "$port" -U "$PG_USER" -d postgres <<EOF
-- Register $PG_DATABASE in the central catalog
-- This tells the background worker to spawn a worker for this database
INSERT INTO steep_repl.databases (datname)
VALUES ('$PG_DATABASE')
ON CONFLICT (datname) DO UPDATE SET enabled = true, registered_at = now();
EOF

    # Step 3: Create extension in the target database
    PGPASSWORD="$PG_PASSWORD" psql -h localhost -p "$port" -U "$PG_USER" -d "$PG_DATABASE" <<EOF
-- Create extension in target database
CREATE EXTENSION IF NOT EXISTS steep_repl;
EOF

    success "Extension setup complete on $name"
    success "Database $PG_DATABASE registered in central catalog"
}

# Create test data on source
create_test_data() {
    header "Creating test data on source..."

    local row_count
    row_count=$(gum input --placeholder "Number of rows (default: 100)" --value "100")
    row_count=${row_count:-100}

    PGPASSWORD="$PG_PASSWORD" psql -h localhost -p 15432 -U "$PG_USER" -d "$PG_DATABASE" <<EOF
-- Drop existing objects if they exist
DROP SUBSCRIPTION IF EXISTS steep_sub_source_node_from_target_node;
DROP SUBSCRIPTION IF EXISTS steep_sub_source_node;
DROP PUBLICATION IF EXISTS steep_pub_source_node;
DROP TABLE IF EXISTS test_data;

-- Create test table
CREATE TABLE test_data (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    value INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_by TEXT DEFAULT 'source'
);

-- Insert test data
INSERT INTO test_data (name, value)
SELECT 'item_' || i, i * 10
FROM generate_series(1, $row_count) AS i;

-- Create publication for this node's data
CREATE PUBLICATION steep_pub_source_node FOR TABLE test_data;
EOF

    success "Created $row_count rows in test_data table"
    success "Created publication steep_pub_source_node"
}

# Create matching table on target
create_target_table() {
    header "Creating matching table on target..."

    PGPASSWORD="$PG_PASSWORD" psql -h localhost -p 15433 -U "$PG_USER" -d "$PG_DATABASE" <<EOF
-- Drop existing objects if they exist
DROP SUBSCRIPTION IF EXISTS steep_sub_target_node_from_source_node;
DROP SUBSCRIPTION IF EXISTS steep_sub_source_node;
DROP PUBLICATION IF EXISTS steep_pub_target_node;
DROP TABLE IF EXISTS test_data;

-- Create matching table structure
CREATE TABLE test_data (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    value INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_by TEXT DEFAULT 'target'
);

-- Create publication for this node's data (for bidirectional)
CREATE PUBLICATION steep_pub_target_node FOR TABLE test_data;
EOF

    success "Created test_data table on target"
    success "Created publication steep_pub_target_node"
}


# Create config files for daemons
create_config_files() {
    header "Creating config files..."

    local config_dir="./configs/test"
    mkdir -p "$config_dir"

    # Source node config
    cat > "$config_dir/source.yaml" <<EOF
repl:
  enabled: true
  node_id: "source-node"
  node_name: "Source Node"

  postgresql:
    host: localhost
    port: 15432
    database: $PG_DATABASE
    user: $PG_USER
    sslmode: disable

  grpc:
    port: 15460

  ipc:
    enabled: true
    path: /tmp/steep-repl-source.sock

  http:
    enabled: false

debug: true
EOF

    success "Created $config_dir/source.yaml"

    # Target node config
    cat > "$config_dir/target.yaml" <<EOF
repl:
  enabled: true
  node_id: "target-node"
  node_name: "Target Node"

  postgresql:
    host: localhost
    port: 15433
    database: $PG_DATABASE
    user: $PG_USER
    sslmode: disable

  grpc:
    port: 15461

  ipc:
    enabled: true
    path: /tmp/steep-repl-target.sock

  http:
    enabled: false

debug: true
EOF

    success "Created $config_dir/target.yaml"
}

# Show environment info
show_info() {
    header "Environment Ready!"
    echo ""

    gum style --border double --padding "1 2" --border-foreground "$SUCCESS_FG" \
        "Source: localhost:15432 (pg-source)" \
        "Target: localhost:15433 (pg-target)" \
        "" \
        "User: $PG_USER" \
        "Password: $PG_PASSWORD" \
        "Database: $PG_DATABASE"

    echo ""
    info "Connection strings:"
    echo "  Source: PGPASSWORD=$PG_PASSWORD psql -h localhost -p 15432 -U $PG_USER -d $PG_DATABASE"
    echo "  Target: PGPASSWORD=$PG_PASSWORD psql -h localhost -p 15433 -U $PG_USER -d $PG_DATABASE"
    echo ""

    info "Config files created:"
    echo "  Source: ./configs/test/source.yaml"
    echo "  Target: ./configs/test/target.yaml"
    echo ""

    info "Next steps:"
    echo "  1. Build steep-repl: make build-repl-daemon"
    echo ""
    echo "  2. Start source daemon (terminal 1):"
    echo "     PGPASSWORD=$PG_PASSWORD ./bin/steep-repl run --config ./configs/test/source.yaml"
    echo ""
    echo "  3. Start target daemon (terminal 2):"
    echo "     PGPASSWORD=$PG_PASSWORD ./bin/steep-repl run --config ./configs/test/target.yaml"
    echo ""
    echo "  4. Trigger init (terminal 3):"
    echo "     # Note: pg-source:5432 is the Docker internal address (container-to-container)"
    echo "     ./bin/steep-repl node start target-node --from source-node \\"
    echo "       --source-host pg-source --source-port 5432 --source-database $PG_DATABASE --source-user $PG_USER \\"
    echo "       --remote localhost:15461 --insecure"
    echo ""
    echo "  5. Monitor progress:"
    echo "     ./scripts/replication/monitor-init.sh"
    echo "     # Or manually:"
    echo "     watch -n1 \"PGPASSWORD=$PG_PASSWORD psql -h localhost -p 15433 -U $PG_USER -d $PG_DATABASE -c 'SELECT * FROM steep_repl.init_progress'\""
}

# Main menu
main_menu() {
    while true; do
        echo ""
        local choice
        choice=$(gum choose \
            "Setup fresh environment" \
            "Teardown environment" \
            "Create/Reset test data" \
            "Show connection info" \
            "Check container status" \
            "View source data" \
            "View target data" \
            "Exit")

        case "$choice" in
            "Setup fresh environment")
                teardown
                create_network
                start_container "$SOURCE_CONTAINER" "pg-source" 15432
                start_container "$TARGET_CONTAINER" "pg-target" 15433
                wait_for_db "source" 15432
                wait_for_db "target" 15433
                setup_extension "source" 15432
                setup_extension "target" 15433
                create_test_data
                create_target_table
                create_config_files
                show_info
                ;;
            "Teardown environment")
                if gum confirm "Are you sure you want to teardown?"; then
                    teardown
                    success "Environment torn down"
                fi
                ;;
            "Create/Reset test data")
                create_test_data
                create_target_table
                ;;
            "Show connection info")
                show_info
                ;;
            "Check container status")
                header "Container Status"
                docker ps -a --filter "name=pg-source" --filter "name=pg-target" \
                    --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
                ;;
            "View source data")
                header "Source Data (test_data)"
                PGPASSWORD="$PG_PASSWORD" psql -h localhost -p 15432 -U "$PG_USER" -d "$PG_DATABASE" \
                    -c "SELECT COUNT(*) as total_rows FROM test_data" \
                    -c "SELECT * FROM test_data ORDER BY id DESC LIMIT 10"
                ;;
            "View target data")
                header "Target Data (test_data)"
                PGPASSWORD="$PG_PASSWORD" psql -h localhost -p 15433 -U "$PG_USER" -d "$PG_DATABASE" \
                    -c "SELECT COUNT(*) as total_rows FROM test_data" \
                    -c "SELECT * FROM test_data ORDER BY id DESC LIMIT 10" \
                    -c "SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'"
                ;;
            "Exit")
                exit 0
                ;;
        esac
    done
}

# Entry point
check_deps

if [[ "${1:-}" == "--teardown" ]]; then
    teardown
    exit 0
fi

header "Steep Replication Test Environment"
echo "This script sets up a 2-node PostgreSQL environment for testing replication initialization."
echo ""

main_menu
