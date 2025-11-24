// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Error types for table queries
var (
	// ErrPgstattupleNotInstalled indicates pgstattuple extension is not installed
	ErrPgstattupleNotInstalled = errors.New("pgstattuple extension not installed")
	// ErrInsufficientPrivileges indicates the user lacks required privileges
	ErrInsufficientPrivileges = errors.New("insufficient privileges for operation")
	// ErrTableNotFound indicates the requested table was not found
	ErrTableNotFound = errors.New("table not found")
)

// Tables query placeholder - actual implementations in subsequent tasks
// This file provides the package structure and error definitions.

// Placeholder to avoid unused import error - will be replaced
var _ = (*pgxpool.Pool)(nil)
var _ = context.Background
