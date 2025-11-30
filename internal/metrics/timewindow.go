// Package metrics provides time-series metrics collection and storage.
package metrics

import "time"

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
// Short windows (1m, 5m) use 1s granularity.
// Medium windows (15m, 1h) use 10s granularity.
// Long windows (24h) use 1m granularity.
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

// RequiresPersistence returns true if this window needs SQLite data.
func (tw TimeWindow) RequiresPersistence() bool {
	return tw >= TimeWindow15m
}

// AllTimeWindows returns all available time windows in order.
func AllTimeWindows() []TimeWindow {
	return []TimeWindow{
		TimeWindow1m,
		TimeWindow5m,
		TimeWindow15m,
		TimeWindow1h,
		TimeWindow24h,
	}
}
