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

// ActivityMonitor fetches connection activity data at regular intervals.
type ActivityMonitor struct {
	pool     *pgxpool.Pool
	interval time.Duration
	filter   models.ActivityFilter
	limit    int
	offset   int
}

// NewActivityMonitor creates a new ActivityMonitor.
func NewActivityMonitor(pool *pgxpool.Pool, interval time.Duration) *ActivityMonitor {
	return &ActivityMonitor{
		pool:     pool,
		interval: interval,
		filter:   models.ActivityFilter{ShowAllDatabases: true},
		limit:    500,
		offset:   0,
	}
}

// SetFilter updates the activity filter.
func (m *ActivityMonitor) SetFilter(filter models.ActivityFilter) {
	m.filter = filter
}

// SetPagination updates the pagination parameters.
func (m *ActivityMonitor) SetPagination(limit, offset int) {
	m.limit = limit
	m.offset = offset
}

// FetchOnce fetches activity data once and returns the result.
func (m *ActivityMonitor) FetchOnce(ctx context.Context) ui.ActivityDataMsg {
	connections, err := queries.GetActivityConnections(ctx, m.pool, m.filter, m.limit, m.offset)
	if err != nil {
		return ui.ActivityDataMsg{
			Error:     err,
			FetchedAt: time.Now(),
		}
	}

	totalCount, err := queries.GetConnectionCount(ctx, m.pool, m.filter)
	if err != nil {
		return ui.ActivityDataMsg{
			Connections: connections,
			Error:       err,
			FetchedAt:   time.Now(),
		}
	}

	return ui.ActivityDataMsg{
		Connections: connections,
		TotalCount:  totalCount,
		FetchedAt:   time.Now(),
	}
}

// Run starts the monitor goroutine that sends data to the provided channel.
// It runs until the context is cancelled.
func (m *ActivityMonitor) Run(ctx context.Context, dataChan chan<- ui.ActivityDataMsg) {
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

// CreateActivityCmd returns a tea.Cmd that fetches activity data.
// This is useful for Bubbletea's command pattern.
func (m *ActivityMonitor) CreateActivityCmd(ctx context.Context) func() ui.ActivityDataMsg {
	return func() ui.ActivityDataMsg {
		return m.FetchOnce(ctx)
	}
}
