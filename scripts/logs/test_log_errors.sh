#!/bin/bash
# Test script for generating PostgreSQL log entries (errors, warnings, notices)
# Used for testing the Steep Log Viewer

# Defaults for Postgres.app
HOST="${PGHOST:-localhost}"
PORT="${PGPORT:-5432}"
DB="${PGDATABASE:-brandon}"
USER="${PGUSER:-brandon}"
PASS="${PGPASSWORD:-}"

usage() {
    echo "Usage: $0 [command] [options]"
    echo ""
    echo "Commands:"
    echo "  errors      Generate ERROR level entries"
    echo "  warnings    Generate WARNING level entries"
    echo "  mixed       Generate mixed ERROR, WARNING, INFO entries (default)"
    echo "  spam        Generate continuous log entries (Ctrl+C to stop)"
    echo ""
    echo "Options:"
    echo "  -h, --host HOST     PostgreSQL host (default: localhost)"
    echo "  -p, --port PORT     PostgreSQL port (default: 5432)"
    echo "  -d, --database DB   Database name (default: brandon)"
    echo "  -U, --user USER     Username (default: brandon)"
    echo ""
    echo "Examples:"
    echo "  $0 errors"
    echo "  $0 warnings -p 5433"
    echo "  $0 mixed -d postgres -U postgres"
    echo "  $0 spam"
}

psql_cmd() {
    PGPASSWORD="$PASS" psql -h "$HOST" -p "$PORT" -d "$DB" -U "$USER" "$@"
}

generate_errors() {
    echo "Generating ERROR entries..."

    # Division by zero
    psql_cmd -c "SELECT 1/0;" 2>&1 || true

    # Missing table
    psql_cmd -c "SELECT * FROM nonexistent_table_$(date +%s);" 2>&1 || true

    # Invalid type cast
    psql_cmd -c "SELECT 'not_a_number'::integer;" 2>&1 || true

    # Syntax error
    psql_cmd -c "SELEC * FROM pg_stat_activity;" 2>&1 || true

    # Constraint violation (if table exists)
    psql_cmd -c "INSERT INTO pg_class VALUES (NULL);" 2>&1 || true

    echo "Generated 5 error entries"
}

generate_warnings() {
    echo "Generating WARNING entries..."

    psql_cmd -c "
DO \$\$
BEGIN
    RAISE WARNING 'Test warning: Connection pool approaching limit';
    RAISE WARNING 'Test warning: Slow query detected in module X';
    RAISE WARNING 'Test warning: Deprecated function called';
    RAISE WARNING 'Test warning: Index scan fell back to seq scan';
    RAISE WARNING 'Test warning: Large transaction detected';
END
\$\$;
"
    echo "Generated 5 warning entries"
}

generate_mixed() {
    echo "Generating mixed log entries..."

    # Warnings
    psql_cmd -c "
DO \$\$
BEGIN
    RAISE WARNING 'Warning: High memory usage detected';
    RAISE WARNING 'Warning: Connection from unusual IP';
    RAISE NOTICE 'Info: Routine maintenance starting';
    RAISE NOTICE 'Info: Cache statistics refreshed';
    RAISE LOG 'Log: Checkpoint completed';
END
\$\$;
"

    # Errors
    psql_cmd -c "SELECT 1/0;" 2>&1 || true
    psql_cmd -c "SELECT * FROM ghost_table_xyz;" 2>&1 || true

    # More notices
    psql_cmd -c "
DO \$\$
BEGIN
    RAISE NOTICE 'Info: Query plan cached';
    RAISE WARNING 'Warning: Approaching max_connections';
END
\$\$;
"

    echo "Generated mixed entries (2 errors, 4 warnings, 4 info)"
}

generate_spam() {
    echo "Generating continuous log entries (Ctrl+C to stop)..."

    local count=0
    while true; do
        count=$((count + 1))

        # Rotate through different levels
        case $((count % 5)) in
            0)
                psql_cmd -c "SELECT 1/0;" 2>&1 >/dev/null || true
                echo "[$count] ERROR: division by zero"
                ;;
            1)
                psql_cmd -c "DO \$\$ BEGIN RAISE WARNING 'Spam warning #$count'; END \$\$;" 2>&1 >/dev/null
                echo "[$count] WARNING: spam warning"
                ;;
            2)
                psql_cmd -c "DO \$\$ BEGIN RAISE NOTICE 'Spam notice #$count'; END \$\$;" 2>&1 >/dev/null
                echo "[$count] NOTICE: spam notice"
                ;;
            3)
                psql_cmd -c "SELECT * FROM fake_$count;" 2>&1 >/dev/null || true
                echo "[$count] ERROR: missing table"
                ;;
            4)
                psql_cmd -c "DO \$\$ BEGIN RAISE WARNING 'High load warning #$count'; END \$\$;" 2>&1 >/dev/null
                echo "[$count] WARNING: high load"
                ;;
        esac

        sleep 1
    done
}

# Parse options
COMMAND="mixed"
while [[ $# -gt 0 ]]; do
    case "$1" in
        errors|warnings|mixed|spam)
            COMMAND="$1"
            shift
            ;;
        -h|--host)
            HOST="$2"
            shift 2
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -d|--database)
            DB="$2"
            shift 2
            ;;
        -U|--user)
            USER="$2"
            shift 2
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Verify connection
if ! psql_cmd -c "SELECT 1;" >/dev/null 2>&1; then
    echo "Error: Cannot connect to PostgreSQL at $HOST:$PORT/$DB as $USER"
    exit 1
fi

echo "Connected to $HOST:$PORT/$DB as $USER"
echo ""

# Execute command
case "$COMMAND" in
    errors)
        generate_errors
        ;;
    warnings)
        generate_warnings
        ;;
    mixed)
        generate_mixed
        ;;
    spam)
        generate_spam
        ;;
esac
