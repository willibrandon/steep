// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// GetActivityConnections retrieves active connections from pg_stat_activity.
// Supports pagination with LIMIT/OFFSET and optional filtering.
func GetActivityConnections(ctx context.Context, pool *pgxpool.Pool, filter models.ActivityFilter, limit, offset int) ([]models.Connection, error) {
	query := `
		SELECT
			pid,
			COALESCE(usename, '') as usename,
			COALESCE(datname, '') as datname,
			COALESCE(state, '') as state,
			COALESCE(EXTRACT(EPOCH FROM (now() - query_start))::int, 0) as duration_seconds,
			COALESCE(TRIM(regexp_replace(query, '\s+', ' ', 'g')), '') as query,
			COALESCE(client_addr::text, '') as client_addr,
			COALESCE(application_name, '') as application_name,
			COALESCE(wait_event_type, '') as wait_event_type,
			COALESCE(wait_event, '') as wait_event,
			COALESCE(query_start, now()) as query_start,
			COALESCE(backend_start, now()) as backend_start
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
		  AND datname IS NOT NULL
	`

	args := []interface{}{}
	argNum := 1

	// Apply state filter
	if filter.StateFilter != "" {
		query += fmt.Sprintf(" AND state = $%d", argNum)
		args = append(args, filter.StateFilter)
		argNum++
	}

	// Apply database filter
	if filter.DatabaseFilter != "" {
		query += fmt.Sprintf(" AND datname = $%d", argNum)
		args = append(args, filter.DatabaseFilter)
		argNum++
	}

	// Apply query text filter
	if filter.QueryFilter != "" {
		query += fmt.Sprintf(" AND query ILIKE $%d", argNum)
		args = append(args, "%"+filter.QueryFilter+"%")
		argNum++
	}

	// Order by state priority (active first) then by duration
	query += ` ORDER BY
		CASE state
			WHEN 'active' THEN 1
			WHEN 'idle in transaction' THEN 2
			WHEN 'idle in transaction (aborted)' THEN 3
			ELSE 4
		END,
		duration_seconds DESC NULLS LAST`

	// Add pagination
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var connections []models.Connection
	for rows.Next() {
		var conn models.Connection
		var stateStr string
		err := rows.Scan(
			&conn.PID,
			&conn.User,
			&conn.Database,
			&stateStr,
			&conn.DurationSeconds,
			&conn.Query,
			&conn.ClientAddr,
			&conn.ApplicationName,
			&conn.WaitEventType,
			&conn.WaitEvent,
			&conn.QueryStart,
			&conn.BackendStart,
		)
		if err != nil {
			return nil, fmt.Errorf("scan connection row: %w", err)
		}
		conn.State = models.ConnectionState(stateStr)
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connections: %w", err)
	}

	return connections, nil
}

// GetConnectionCount returns the total count of connections matching the filter.
func GetConnectionCount(ctx context.Context, pool *pgxpool.Pool, filter models.ActivityFilter) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
		  AND datname IS NOT NULL
	`

	args := []interface{}{}
	argNum := 1

	// Apply state filter
	if filter.StateFilter != "" {
		query += fmt.Sprintf(" AND state = $%d", argNum)
		args = append(args, filter.StateFilter)
		argNum++
	}

	// Apply database filter
	if filter.DatabaseFilter != "" {
		query += fmt.Sprintf(" AND datname = $%d", argNum)
		args = append(args, filter.DatabaseFilter)
		argNum++
	}

	// Apply query text filter
	if filter.QueryFilter != "" {
		query += fmt.Sprintf(" AND query ILIKE $%d", argNum)
		args = append(args, "%"+filter.QueryFilter+"%")
	}

	var count int
	err := pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count connections: %w", err)
	}

	return count, nil
}

// CancelQuery cancels a running query using pg_cancel_backend.
func CancelQuery(ctx context.Context, pool *pgxpool.Pool, pid int) (bool, error) {
	var success bool
	err := pool.QueryRow(ctx, "SELECT pg_cancel_backend($1)", pid).Scan(&success)
	if err != nil {
		return false, fmt.Errorf("cancel query pid %d: %w", pid, err)
	}
	return success, nil
}

// TerminateConnection terminates a connection using pg_terminate_backend.
func TerminateConnection(ctx context.Context, pool *pgxpool.Pool, pid int) (bool, error) {
	var success bool
	err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&success)
	if err != nil {
		return false, fmt.Errorf("terminate connection pid %d: %w", pid, err)
	}
	return success, nil
}

// GetCurrentPID returns the PID of the current connection.
func GetCurrentPID(ctx context.Context, conn *pgx.Conn) (int, error) {
	var pid int
	err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid)
	if err != nil {
		return 0, fmt.Errorf("get backend pid: %w", err)
	}
	return pid, nil
}
