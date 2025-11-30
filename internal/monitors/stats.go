package monitors

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui"
)

// MetricsRecorder is an interface for recording metrics for visualization.
type MetricsRecorder interface {
	Record(metricName string, value float64)
}

// StatsMonitor fetches database metrics at regular intervals.
type StatsMonitor struct {
	pool            *pgxpool.Pool
	interval        time.Duration
	lastSnapshot    *models.MetricsSnapshot
	metricsRecorder MetricsRecorder
}

// NewStatsMonitor creates a new StatsMonitor.
func NewStatsMonitor(pool *pgxpool.Pool, interval time.Duration) *StatsMonitor {
	return &StatsMonitor{
		pool:     pool,
		interval: interval,
	}
}

// SetMetricsRecorder sets the metrics recorder for visualization data.
func (m *StatsMonitor) SetMetricsRecorder(recorder MetricsRecorder) {
	m.metricsRecorder = recorder
}

// FetchOnce fetches metrics data once and returns the result.
func (m *StatsMonitor) FetchOnce(ctx context.Context) ui.MetricsDataMsg {
	// Get current snapshot
	snapshot, err := queries.GetDatabaseStats(ctx, m.pool)
	if err != nil {
		return ui.MetricsDataMsg{
			Error:     err,
			FetchedAt: time.Now(),
		}
	}
	snapshot.Timestamp = time.Now()

	// Get database size
	dbSize, err := queries.GetDatabaseSize(ctx, m.pool)
	if err != nil {
		return ui.MetricsDataMsg{
			Error:     err,
			FetchedAt: time.Now(),
		}
	}

	// Get connection counts
	connCount, err := queries.GetTotalConnectionCount(ctx, m.pool)
	if err != nil {
		return ui.MetricsDataMsg{
			Error:     err,
			FetchedAt: time.Now(),
		}
	}

	// Calculate metrics
	metrics := models.Metrics{
		DatabaseSize:    dbSize,
		ConnectionCount: connCount,
		Timestamp:       snapshot.Timestamp,
	}

	// Calculate TPS (transactions per second)
	if m.lastSnapshot != nil {
		elapsed := snapshot.Timestamp.Sub(m.lastSnapshot.Timestamp).Seconds()
		if elapsed > 0 {
			xactDelta := snapshot.TotalXacts - m.lastSnapshot.TotalXacts
			// Handle counter reset (e.g., stats reset or server restart)
			if xactDelta >= 0 {
				metrics.TPS = float64(xactDelta) / elapsed
			}
		}
	}

	// Store snapshot for next delta calculation
	m.lastSnapshot = &snapshot

	// Calculate cache hit ratio
	totalBlocks := snapshot.BlksHit + snapshot.BlksRead
	if totalBlocks > 0 {
		metrics.CacheHitRatio = float64(snapshot.BlksHit) / float64(totalBlocks) * 100
	} else {
		metrics.CacheHitRatio = 100 // No blocks read means perfect cache
	}

	// Record metrics for visualization if recorder is set
	if m.metricsRecorder != nil {
		m.metricsRecorder.Record("tps", metrics.TPS)
		m.metricsRecorder.Record("connections", float64(metrics.ConnectionCount))
		m.metricsRecorder.Record("cache_hit_ratio", metrics.CacheHitRatio)
	}

	return ui.MetricsDataMsg{
		Metrics:   metrics,
		FetchedAt: time.Now(),
	}
}

// Run starts the monitor goroutine that sends data to the provided channel.
func (m *StatsMonitor) Run(ctx context.Context, dataChan chan<- ui.MetricsDataMsg) {
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

// CreateStatsCmd returns a function that fetches stats data.
func (m *StatsMonitor) CreateStatsCmd(ctx context.Context) func() ui.MetricsDataMsg {
	return func() ui.MetricsDataMsg {
		return m.FetchOnce(ctx)
	}
}
