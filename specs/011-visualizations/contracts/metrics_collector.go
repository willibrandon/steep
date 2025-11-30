// Package contracts defines interfaces for the visualization feature.
// This file is a design artifact - copy to internal/metrics/ during implementation.
package contracts

import (
	"context"
	"time"
)

// TimeWindow represents a configurable time range for chart data.
type TimeWindow int

const (
	TimeWindow1m TimeWindow = iota
	TimeWindow5m
	TimeWindow15m
	TimeWindow1h
	TimeWindow24h
)

// Duration returns the time.Duration for the window.
func (tw TimeWindow) Duration() time.Duration {
	switch tw {
	case TimeWindow1m:
		return time.Minute
	case TimeWindow5m:
		return 5 * time.Minute
	case TimeWindow15m:
		return 15 * time.Minute
	case TimeWindow1h:
		return time.Hour
	case TimeWindow24h:
		return 24 * time.Hour
	default:
		return time.Minute
	}
}

// Granularity returns the data point interval for this window.
func (tw TimeWindow) Granularity() time.Duration {
	switch tw {
	case TimeWindow1m, TimeWindow5m:
		return time.Second
	case TimeWindow15m, TimeWindow1h:
		return 10 * time.Second
	case TimeWindow24h:
		return time.Minute
	default:
		return time.Second
	}
}

// String returns a display label.
func (tw TimeWindow) String() string {
	switch tw {
	case TimeWindow1m:
		return "1m"
	case TimeWindow5m:
		return "5m"
	case TimeWindow15m:
		return "15m"
	case TimeWindow1h:
		return "1h"
	case TimeWindow24h:
		return "24h"
	default:
		return "1m"
	}
}

// DataPoint represents a single metric measurement.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// MetricsCollector defines the interface for collecting and retrieving metrics.
type MetricsCollector interface {
	// Record adds a new data point for a metric.
	// Thread-safe; called from monitor goroutine.
	Record(metricName string, value float64)

	// GetValues returns metric values for the specified time window.
	// Returns float64 slice suitable for asciigraph.Plot().
	// Thread-safe; called from View() rendering.
	GetValues(metricName string, window TimeWindow) []float64

	// GetDataPoints returns full DataPoint structs for the time window.
	// Used when timestamps are needed (e.g., heatmap aggregation).
	GetDataPoints(metricName string, window TimeWindow) []DataPoint

	// GetLatest returns the most recent value for a metric.
	GetLatest(metricName string) (float64, time.Time, bool)

	// Start begins background persistence and cleanup goroutine.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the collector.
	Stop() error
}

// MetricsStore defines the interface for persistent metric storage.
type MetricsStore interface {
	// SaveDataPoint persists a single data point.
	SaveDataPoint(ctx context.Context, metricName string, dp DataPoint) error

	// SaveBatch persists multiple data points in a transaction.
	SaveBatch(ctx context.Context, metricName string, points []DataPoint) error

	// GetHistory retrieves historical data for a metric since the given time.
	// Limited to prevent unbounded result sets.
	GetHistory(ctx context.Context, metricName string, since time.Time, limit int) ([]DataPoint, error)

	// GetAggregated retrieves aggregated data (AVG) grouped by interval.
	// Used for long time windows (1h, 24h) to reduce data points.
	GetAggregated(ctx context.Context, metricName string, since time.Time, intervalSeconds int) ([]DataPoint, error)

	// Prune removes entries older than the retention period.
	// Returns number of rows deleted.
	Prune(ctx context.Context, retentionDays int) (int64, error)
}

// CircularBuffer defines the interface for in-memory metric buffering.
type CircularBuffer interface {
	// Push adds a new data point, evicting oldest if at capacity.
	Push(dp DataPoint)

	// GetRecent returns the n most recent data points.
	GetRecent(n int) []DataPoint

	// GetSince returns all data points since the given timestamp.
	GetSince(since time.Time) []DataPoint

	// GetValues returns all values as float64 slice (for asciigraph).
	GetValues() []float64

	// Len returns the current number of elements.
	Len() int

	// Clear resets the buffer.
	Clear()
}
