// Package monitors provides background goroutines for fetching PostgreSQL data.
package monitors

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui"
)

// LocksMonitor fetches lock data at regular intervals.
type LocksMonitor struct {
	pool     *pgxpool.Pool
	interval time.Duration
}

// NewLocksMonitor creates a new LocksMonitor.
func NewLocksMonitor(pool *pgxpool.Pool, interval time.Duration) *LocksMonitor {
	return &LocksMonitor{
		pool:     pool,
		interval: interval,
	}
}

// FetchOnce fetches lock data once and returns the result.
func (m *LocksMonitor) FetchOnce(ctx context.Context) ui.LocksDataMsg {
	// Get all locks
	locks, err := queries.GetLocks(ctx, m.pool)
	if err != nil {
		return ui.LocksDataMsg{
			Data:      models.NewLocksData(),
			FetchedAt: time.Now(),
			Error:     err,
		}
	}

	// Get blocking relationships
	relationships, err := queries.GetBlockingRelationships(ctx, m.pool)
	if err != nil {
		// Return locks even if we can't get blocking info
		data := models.NewLocksData()
		data.Locks = locks
		return ui.LocksDataMsg{
			Data:      data,
			FetchedAt: time.Now(),
			Error:     err,
		}
	}

	// Build the full locks data structure
	data := models.NewLocksData()
	data.Locks = locks
	data.Blocking = relationships

	// Build blocking/blocked PID maps
	for _, rel := range relationships {
		data.BlockingPIDs[rel.BlockingPID] = true
		data.BlockedPIDs[rel.BlockedPID] = true
	}

	// Build blocking chains for tree visualization
	data.Chains = BuildBlockingChains(locks, relationships)

	return ui.LocksDataMsg{
		Data:      data,
		FetchedAt: time.Now(),
	}
}

// Run starts the monitor goroutine that sends data to the provided channel.
// It runs until the context is cancelled.
func (m *LocksMonitor) Run(ctx context.Context, dataChan chan<- ui.LocksDataMsg) {
	// Send initial data immediately
	dataChan <- m.FetchOnce(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dataChan <- m.FetchOnce(ctx)
		}
	}
}

// CreateLocksCmd returns a tea.Cmd that fetches lock data.
func (m *LocksMonitor) CreateLocksCmd(ctx context.Context) func() ui.LocksDataMsg {
	return func() ui.LocksDataMsg {
		return m.FetchOnce(ctx)
	}
}

// BuildBlockingChains constructs a hierarchical tree of blocking relationships.
func BuildBlockingChains(locks []models.Lock, relationships []models.BlockingRelationship) []models.BlockingChain {
	if len(relationships) == 0 {
		return nil
	}

	// Build maps for quick lookup
	lockByPID := make(map[int]models.Lock)
	for _, lock := range locks {
		lockByPID[lock.PID] = lock
	}

	// Build a map of blocker -> list of blocked PIDs
	blockerToBlocked := make(map[int][]int)
	blockedSet := make(map[int]bool)

	for _, rel := range relationships {
		blockerToBlocked[rel.BlockingPID] = append(blockerToBlocked[rel.BlockingPID], rel.BlockedPID)
		blockedSet[rel.BlockedPID] = true
	}

	// Find root blockers (blockers that are not blocked by anyone)
	var rootBlockers []int
	for blockerPID := range blockerToBlocked {
		if !blockedSet[blockerPID] {
			rootBlockers = append(rootBlockers, blockerPID)
		}
	}

	// Build chains recursively
	var chains []models.BlockingChain
	visited := make(map[int]bool)

	var buildChain func(pid int) models.BlockingChain
	buildChain = func(pid int) models.BlockingChain {
		if visited[pid] {
			// Prevent infinite loops
			return models.BlockingChain{BlockerPID: pid, Query: "(circular reference)"}
		}
		visited[pid] = true

		lock := lockByPID[pid]
		chain := models.BlockingChain{
			BlockerPID: pid,
			Query:      lock.Query,
			LockMode:   lock.Mode,
			User:       lock.User,
		}

		// Add blocked processes
		for _, blockedPID := range blockerToBlocked[pid] {
			chain.Blocked = append(chain.Blocked, buildChain(blockedPID))
		}

		return chain
	}

	for _, rootPID := range rootBlockers {
		chains = append(chains, buildChain(rootPID))
	}

	return chains
}
