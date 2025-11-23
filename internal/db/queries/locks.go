// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// GetLocks retrieves all active locks from pg_locks joined with pg_stat_activity.
func GetLocks(ctx context.Context, pool *pgxpool.Pool) ([]models.Lock, error) {
	query := `
		SELECT
			l.pid,
			COALESCE(a.usename, '') as usename,
			COALESCE(a.datname, '') as datname,
			l.locktype,
			l.mode,
			l.granted,
			COALESCE(c.relname, '') as relname,
			COALESCE(TRIM(regexp_replace(a.query, '\s+', ' ', 'g')), '') as query,
			COALESCE(a.state, '') as state,
			COALESCE(EXTRACT(EPOCH FROM (now() - a.query_start)), 0) as duration_seconds,
			COALESCE(a.wait_event_type, '') as wait_event_type,
			COALESCE(a.wait_event, '') as wait_event
		FROM pg_locks l
		LEFT JOIN pg_stat_activity a ON l.pid = a.pid
		LEFT JOIN pg_class c ON l.relation = c.oid
		WHERE l.pid != pg_backend_pid()
		ORDER BY
			CASE WHEN l.granted THEN 1 ELSE 0 END,
			a.query_start NULLS LAST
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_locks: %w", err)
	}
	defer rows.Close()

	var locks []models.Lock
	for rows.Next() {
		var lock models.Lock
		var durationSeconds float64
		err := rows.Scan(
			&lock.PID,
			&lock.User,
			&lock.Database,
			&lock.LockType,
			&lock.Mode,
			&lock.Granted,
			&lock.Relation,
			&lock.Query,
			&lock.State,
			&durationSeconds,
			&lock.WaitEventType,
			&lock.WaitEvent,
		)
		if err != nil {
			return nil, fmt.Errorf("scan lock row: %w", err)
		}
		lock.Duration = time.Duration(durationSeconds * float64(time.Second))
		locks = append(locks, lock)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locks: %w", err)
	}

	return locks, nil
}

// GetBlockingRelationships retrieves blocking relationships using pg_blocking_pids().
func GetBlockingRelationships(ctx context.Context, pool *pgxpool.Pool) ([]models.BlockingRelationship, error) {
	query := `
		SELECT
			blocked.pid AS blocked_pid,
			COALESCE(blocked.usename, '') AS blocked_user,
			COALESCE(TRIM(regexp_replace(blocked.query, '\s+', ' ', 'g')), '') AS blocked_query,
			COALESCE(EXTRACT(EPOCH FROM (now() - blocked.query_start)), 0) AS blocked_duration,
			blocking.pid AS blocking_pid,
			COALESCE(blocking.usename, '') AS blocking_user,
			COALESCE(TRIM(regexp_replace(blocking.query, '\s+', ' ', 'g')), '') AS blocking_query
		FROM pg_stat_activity blocked
		CROSS JOIN LATERAL unnest(pg_blocking_pids(blocked.pid)) AS blocking_pid
		JOIN pg_stat_activity blocking ON blocking.pid = blocking_pid
		WHERE blocked.pid != pg_backend_pid()
		  AND cardinality(pg_blocking_pids(blocked.pid)) > 0
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query blocking relationships: %w", err)
	}
	defer rows.Close()

	var relationships []models.BlockingRelationship
	for rows.Next() {
		var rel models.BlockingRelationship
		var blockedDurationSeconds float64
		err := rows.Scan(
			&rel.BlockedPID,
			&rel.BlockedUser,
			&rel.BlockedQuery,
			&blockedDurationSeconds,
			&rel.BlockingPID,
			&rel.BlockingUser,
			&rel.BlockingQuery,
		)
		if err != nil {
			return nil, fmt.Errorf("scan blocking relationship: %w", err)
		}
		rel.BlockedDuration = time.Duration(blockedDurationSeconds * float64(time.Second))
		relationships = append(relationships, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate blocking relationships: %w", err)
	}

	return relationships, nil
}

// TerminateBackend terminates a backend connection using pg_terminate_backend.
func TerminateBackend(ctx context.Context, pool *pgxpool.Pool, pid int) (bool, error) {
	var success bool
	err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&success)
	if err != nil {
		return false, fmt.Errorf("terminate backend pid %d: %w", pid, err)
	}
	return success, nil
}
