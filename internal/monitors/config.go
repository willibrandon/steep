// Package monitors provides background goroutines for fetching PostgreSQL data.
package monitors

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui/views/config"
)

// ConfigMonitor fetches configuration data at regular intervals.
type ConfigMonitor struct {
	pool     *pgxpool.Pool
	interval time.Duration
}

// NewConfigMonitor creates a new ConfigMonitor.
func NewConfigMonitor(pool *pgxpool.Pool, interval time.Duration) *ConfigMonitor {
	return &ConfigMonitor{
		pool:     pool,
		interval: interval,
	}
}

// FetchOnce fetches configuration data once and returns the result.
func (m *ConfigMonitor) FetchOnce(ctx context.Context) config.ConfigDataMsg {
	data, err := queries.GetAllParameters(ctx, m.pool)
	if err != nil {
		return config.ConfigDataMsg{
			Data:  nil,
			Error: err,
		}
	}

	return config.ConfigDataMsg{
		Data:  data,
		Error: nil,
	}
}

// Interval returns the refresh interval.
func (m *ConfigMonitor) Interval() time.Duration {
	return m.interval
}
