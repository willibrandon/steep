#!/bin/bash
# Test script for creating PostgreSQL connections with various states

DB="${PGDATABASE:-brandon}"
USER="${PGUSER:-brandon}"

case "$1" in
  start)
    echo "Creating test connections..."

    for i in {1..3}; do
      psql -d "$DB" -U "$USER" -c "SELECT pg_sleep(300)" &
      echo "Started pg_sleep connection $i ($DB)"
    done

    psql -d "$DB" -U "$USER" -c "BEGIN; SELECT pg_sleep(300);" &
    echo "Started idle-in-transaction connection ($DB)"

    # Also connect to postgres database for testing database filter
    psql -d postgres -U postgres -c "SELECT pg_sleep(300)" &
    echo "Started pg_sleep connection (postgres)"

    psql -d postgres -U postgres -c "SELECT pg_sleep(300)" &
    echo "Started pg_sleep connection (postgres)"

    echo "Done. Use '$0 stop' to terminate."
    ;;

  stop)
    echo "Stopping test connections..."
    pkill -f "pg_sleep"
    psql -d "$DB" -U "$USER" -tAc "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE '%pg_sleep%' AND pid != pg_backend_pid();" >/dev/null
    psql -d postgres -U postgres -tAc "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE '%pg_sleep%' AND pid != pg_backend_pid();" >/dev/null
    echo "Done."
    ;;

  status)
    echo "Running test processes:"
    pgrep -fl "pg_sleep" || echo "None."
    ;;

  *)
    echo "Usage: $0 {start|stop|status}"
    ;;
esac
