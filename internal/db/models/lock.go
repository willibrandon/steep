// Package models contains data structures for database entities.
package models

import "time"

// LockStatus indicates if a lock is involved in blocking relationships
type LockStatus int

const (
	// LockStatusNormal indicates the lock is not involved in blocking
	LockStatusNormal LockStatus = iota
	// LockStatusBlocking indicates the lock is blocking other queries
	LockStatusBlocking
	// LockStatusBlocked indicates the lock is waiting (blocked by another)
	LockStatusBlocked
)

// Lock represents an active database lock from pg_locks
type Lock struct {
	// PID is the process ID holding or waiting for the lock
	PID int
	// User is the database username
	User string
	// Database is the database name
	Database string
	// LockType is the type of lock (relation, transactionid, tuple, etc.)
	LockType string
	// Mode is the lock mode (AccessShareLock, RowExclusiveLock, etc.)
	Mode string
	// Granted is true if the lock is held, false if waiting
	Granted bool
	// Relation is the name of the locked table/index (may be empty)
	Relation string
	// Query is the associated query text
	Query string
	// State is the connection state (active, idle, etc.)
	State string
	// Duration is the time since query started
	Duration time.Duration
	// WaitEventType is the type of wait event if waiting
	WaitEventType string
	// WaitEvent is the specific wait event
	WaitEvent string
}

// BlockingRelationship represents a blocker-blocked relationship between two processes
type BlockingRelationship struct {
	// BlockedPID is the process waiting for a lock
	BlockedPID int
	// BlockedUser is the username of the blocked process
	BlockedUser string
	// BlockedQuery is the query text of the blocked process
	BlockedQuery string
	// BlockedDuration is how long the process has been blocked
	BlockedDuration time.Duration
	// BlockingPID is the process holding the lock
	BlockingPID int
	// BlockingUser is the username of the blocking process
	BlockingUser string
	// BlockingQuery is the query text of the blocking process
	BlockingQuery string
}

// BlockingChain represents a hierarchical structure for lock dependency tree visualization
type BlockingChain struct {
	// BlockerPID is the PID of the blocking process
	BlockerPID int
	// Query is the query text (may be truncated for display)
	Query string
	// LockMode is the lock mode held by this process
	LockMode string
	// User is the username
	User string
	// Blocked is the list of processes blocked by this one (recursive)
	Blocked []BlockingChain
}

// LocksData contains all lock information for a single refresh
type LocksData struct {
	// Locks is the list of all active locks
	Locks []Lock
	// Blocking is the list of blocking relationships
	Blocking []BlockingRelationship
	// BlockingPIDs is a set of PIDs that are blocking others
	BlockingPIDs map[int]bool
	// BlockedPIDs is a set of PIDs that are blocked
	BlockedPIDs map[int]bool
	// Chains is the hierarchical blocking chain structure
	Chains []BlockingChain
}

// NewLocksData creates an initialized LocksData structure
func NewLocksData() *LocksData {
	return &LocksData{
		Locks:        make([]Lock, 0),
		Blocking:     make([]BlockingRelationship, 0),
		BlockingPIDs: make(map[int]bool),
		BlockedPIDs:  make(map[int]bool),
		Chains:       make([]BlockingChain, 0),
	}
}

// GetStatus returns the lock status for a given PID
func (d *LocksData) GetStatus(pid int) LockStatus {
	if d.BlockedPIDs[pid] {
		return LockStatusBlocked
	}
	if d.BlockingPIDs[pid] {
		return LockStatusBlocking
	}
	return LockStatusNormal
}
