package queries

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// queryInfo stores information about an active query
type queryInfo struct {
	query      string
	user       string
	database   string
	queryStart time.Time
}

// SamplingCollector polls pg_stat_activity for active queries.
type SamplingCollector struct {
	pool     *pgxpool.Pool
	interval time.Duration
	events   chan QueryEvent
	// Track queries we've seen to emit event when they complete
	activeQueries map[int32]queryInfo // pid -> query info
}

// NewSamplingCollector creates a new SamplingCollector.
func NewSamplingCollector(pool *pgxpool.Pool, interval time.Duration) *SamplingCollector {
	return &SamplingCollector{
		pool:          pool,
		interval:      interval,
		events:        make(chan QueryEvent, 10000),
		activeQueries: make(map[int32]queryInfo),
	}
}

// Events returns the channel of query events.
func (c *SamplingCollector) Events() <-chan QueryEvent {
	return c.events
}

// Start begins polling pg_stat_activity.
func (c *SamplingCollector) Start(ctx context.Context) error {
	go c.run(ctx)
	return nil
}

// Stop stops the collector.
func (c *SamplingCollector) Stop() error {
	return nil
}

// run is the main polling loop.
func (c *SamplingCollector) run(ctx context.Context) {
	// Sample immediately on start
	c.sample(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(c.events)
			return
		case <-ticker.C:
			c.sample(ctx)
		}
	}
}

// sample polls pg_stat_activity and generates events for completed queries.
func (c *SamplingCollector) sample(ctx context.Context) {
	rows, err := c.pool.Query(ctx, `
		SELECT
			pid,
			COALESCE(usename, '') as usename,
			COALESCE(datname, '') as datname,
			COALESCE(query, '') as query,
			query_start
		FROM pg_stat_activity
		WHERE state = 'active'
		  AND query NOT LIKE '%pg_stat_activity%'
		  AND pid != pg_backend_pid()
		  AND query_start IS NOT NULL
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	currentQueries := make(map[int32]bool)

	for rows.Next() {
		var pid int32
		var user, database, query string
		var queryStart time.Time

		err := rows.Scan(&pid, &user, &database, &query, &queryStart)
		if err != nil {
			continue
		}

		currentQueries[pid] = true

		// Track new queries with their info
		if _, seen := c.activeQueries[pid]; !seen {
			c.activeQueries[pid] = queryInfo{
				query:      query,
				user:       user,
				database:   database,
				queryStart: queryStart,
			}
		}
	}

	// Generate events for queries that have completed (no longer in active list)
	for pid, info := range c.activeQueries {
		if !currentQueries[pid] {
			// Query completed - calculate final duration
			durationMs := time.Since(info.queryStart).Seconds() * 1000

			// Skip if duration is invalid
			if durationMs <= 0 {
				delete(c.activeQueries, pid)
				continue
			}

			event := QueryEvent{
				Query:      info.query,
				DurationMs: durationMs,
				Rows:       0, // Not available from pg_stat_activity
				Timestamp:  time.Now(),
				Database:   info.database,
				User:       info.user,
			}

			select {
			case c.events <- event:
			default:
				// Channel full, skip event
			}

			delete(c.activeQueries, pid)
		}
	}
}
