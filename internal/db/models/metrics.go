package models

import (
	"fmt"
	"time"
)

// Metrics represents dashboard metrics from pg_stat_database.
type Metrics struct {
	TPS             float64   `json:"tps"`
	CacheHitRatio   float64   `json:"cache_hit_ratio"`
	ConnectionCount int       `json:"connection_count"`
	DatabaseSize    int64     `json:"database_size"`
	Timestamp       time.Time `json:"timestamp"`
}

// MetricsSnapshot is used internally for calculating deltas between samples.
type MetricsSnapshot struct {
	TotalXacts int64     `json:"total_xacts"`
	BlksHit    int64     `json:"blks_hit"`
	BlksRead   int64     `json:"blks_read"`
	Timestamp  time.Time `json:"timestamp"`
}

// FormatTPS returns TPS as a formatted string with units.
func (m *Metrics) FormatTPS() string {
	if m.TPS >= 1000 {
		return fmt.Sprintf("%.1fk/s", m.TPS/1000)
	}
	return fmt.Sprintf("%.0f/s", m.TPS)
}

// FormatCacheHitRatio returns cache hit ratio as percentage string.
func (m *Metrics) FormatCacheHitRatio() string {
	return fmt.Sprintf("%.1f%%", m.CacheHitRatio)
}

// FormatDatabaseSize returns database size with appropriate units.
func (m *Metrics) FormatDatabaseSize() string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case m.DatabaseSize >= TB:
		return fmt.Sprintf("%.1f TB", float64(m.DatabaseSize)/TB)
	case m.DatabaseSize >= GB:
		return fmt.Sprintf("%.1f GB", float64(m.DatabaseSize)/GB)
	case m.DatabaseSize >= MB:
		return fmt.Sprintf("%.1f MB", float64(m.DatabaseSize)/MB)
	case m.DatabaseSize >= KB:
		return fmt.Sprintf("%.1f KB", float64(m.DatabaseSize)/KB)
	default:
		return fmt.Sprintf("%d B", m.DatabaseSize)
	}
}

// IsCacheWarning returns true if cache hit ratio is below warning threshold.
func (m *Metrics) IsCacheWarning() bool {
	return m.CacheHitRatio < 90.0
}

// IsCacheCritical returns true if cache hit ratio is below critical threshold.
func (m *Metrics) IsCacheCritical() bool {
	return m.CacheHitRatio < 80.0
}
