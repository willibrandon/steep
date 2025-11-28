package monitors

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)


// SessionState holds cached session information from pg_stat_activity.
type SessionState struct {
	PID          int
	BackendStart time.Time
	XactStart    *time.Time
	CapturedAt   time.Time
}

// SessionCache maintains a cache of session state for PIDs with waiting locks.
// This allows capturing xact_start before deadlocks are resolved.
type SessionCache struct {
	pool    *pgxpool.Pool
	cache   map[int]*SessionState
	mu      sync.RWMutex
	ttl     time.Duration
	running bool
	cancel  context.CancelFunc
}

// NewSessionCache creates a new session cache.
func NewSessionCache(pool *pgxpool.Pool) *SessionCache {
	return &SessionCache{
		pool:  pool,
		cache: make(map[int]*SessionState),
		ttl:   60 * time.Second, // Keep state for 60 seconds after capture
	}
}

// Start begins polling for lock waits and caching session state.
// This method is idempotent - calling it multiple times has no effect if already running.
func (c *SessionCache) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return // Already running
	}
	c.running = true
	c.mu.Unlock()

	ctx, c.cancel = context.WithCancel(ctx)

	go c.pollLoop(ctx)
	go c.cleanupLoop(ctx)
}

// Stop stops the cache polling.
func (c *SessionCache) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return // Not running
	}
	c.running = false
	cancel := c.cancel
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// GetSessionState returns cached session state for a PID.
func (c *SessionCache) GetSessionState(pid int) *SessionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[pid]
}

// pollLoop polls for lock waits every second.
func (c *SessionCache) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkAndCapture(ctx)
		}
	}
}

// cleanupLoop removes stale entries from the cache.
func (c *SessionCache) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// checkAndCapture checks for waiting locks and captures session state.
func (c *SessionCache) checkAndCapture(ctx context.Context) {
	// Capture full lock graph - both blocked and blocking PIDs
	c.captureFullLockGraph(ctx)
}

// captureFullLockGraph captures both blocked and blocking PIDs in a single atomic query.
func (c *SessionCache) captureFullLockGraph(ctx context.Context) {
	rows, err := c.pool.Query(ctx, `
		SELECT DISTINCT
			blocked.pid,
			blocked_activity.backend_start,
			blocked_activity.xact_start,
			blocker.pid,
			blocker_activity.backend_start,
			blocker_activity.xact_start
		FROM pg_locks blocked
		JOIN pg_locks blocker ON (
			blocker.locktype = blocked.locktype
			AND blocker.database IS NOT DISTINCT FROM blocked.database
			AND blocker.relation IS NOT DISTINCT FROM blocked.relation
			AND blocker.granted = true
			AND blocked.granted = false
		)
		JOIN pg_stat_activity blocked_activity ON blocked_activity.pid = blocked.pid
		JOIN pg_stat_activity blocker_activity ON blocker_activity.pid = blocker.pid
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for rows.Next() {
		var blockedPID, blockerPID int
		var blockedBackend, blockerBackend time.Time
		var blockedXact, blockerXact *time.Time

		if err := rows.Scan(&blockedPID, &blockedBackend, &blockedXact,
			&blockerPID, &blockerBackend, &blockerXact); err != nil {
			continue
		}

		// Cache blocked (victim)
		c.cache[blockedPID] = &SessionState{
			PID:          blockedPID,
			BackendStart: blockedBackend,
			XactStart:    blockedXact,
			CapturedAt:   now,
		}

		// Cache blocker
		c.cache[blockerPID] = &SessionState{
			PID:          blockerPID,
			BackendStart: blockerBackend,
			XactStart:    blockerXact,
			CapturedAt:   now,
		}
	}
}

// cleanup removes entries older than TTL.
func (c *SessionCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-c.ttl)
	for pid, state := range c.cache {
		if state.CapturedAt.Before(cutoff) {
			delete(c.cache, pid)
		}
	}
}
