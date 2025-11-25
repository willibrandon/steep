#!/usr/bin/env bash
#
# repl-test-teardown.sh - Remove PostgreSQL replication test environment
#
# Usage:
#   ./scripts/repl-test-teardown.sh [OPTIONS]
#
# Options:
#   --keep-volumes    Don't remove Docker volumes (preserve data)
#   --force           Don't prompt for confirmation
#   --help            Show this help message
#

set -euo pipefail

KEEP_VOLUMES=false
FORCE=false
NETWORK_NAME="steep-repl-test"
CONTAINER_PREFIX="steep-pg-"

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

show_help() {
    sed -n '2,/^$/p' "$0" | sed 's/^#//' | sed 's/^ //'
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --keep-volumes)
            KEEP_VOLUMES=true
            shift
            ;;
        --force|-f)
            FORCE=true
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

# Find all related containers
CONTAINERS=$(docker ps -a --filter "name=${CONTAINER_PREFIX}" --format "{{.Names}}" 2>/dev/null || true)

if [[ -z "$CONTAINERS" ]]; then
    log_info "No test containers found"

    # Still try to clean up network and volumes
    if docker network ls --format "{{.Name}}" | grep -q "^${NETWORK_NAME}$"; then
        log_info "Removing network: $NETWORK_NAME"
        docker network rm "$NETWORK_NAME" > /dev/null 2>&1 || true
        log_success "Network removed"
    fi

    exit 0
fi

# Show what will be removed
echo "The following containers will be removed:"
for container in $CONTAINERS; do
    echo "  - $container"
done

# Find related volumes
VOLUMES=$(docker volume ls --filter "name=steep-replica" --format "{{.Name}}" 2>/dev/null || true)
if [[ -n "$VOLUMES" && "$KEEP_VOLUMES" == "false" ]]; then
    echo
    echo "The following volumes will be removed:"
    for volume in $VOLUMES; do
        echo "  - $volume"
    done
fi

echo

# Confirm unless --force
if [[ "$FORCE" == "false" ]]; then
    read -p "Continue? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Aborted"
        exit 0
    fi
fi

# Stop containers
log_info "Stopping containers..."
for container in $CONTAINERS; do
    docker stop "$container" > /dev/null 2>&1 || true
done
log_success "Containers stopped"

# Remove containers
log_info "Removing containers..."
for container in $CONTAINERS; do
    docker rm "$container" > /dev/null 2>&1 || true
done
log_success "Containers removed"

# Remove volumes
if [[ "$KEEP_VOLUMES" == "false" && -n "$VOLUMES" ]]; then
    log_info "Removing volumes..."
    for volume in $VOLUMES; do
        docker volume rm "$volume" > /dev/null 2>&1 || true
    done
    log_success "Volumes removed"
elif [[ "$KEEP_VOLUMES" == "true" ]]; then
    log_info "Keeping volumes (use docker volume rm to remove manually)"
fi

# Remove network
if docker network ls --format "{{.Name}}" | grep -q "^${NETWORK_NAME}$"; then
    log_info "Removing network: $NETWORK_NAME"
    docker network rm "$NETWORK_NAME" > /dev/null 2>&1 || true
    log_success "Network removed"
fi

echo
log_success "Teardown complete!"
