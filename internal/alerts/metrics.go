package alerts

import (
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// MetricValues provides current metric values for alert evaluation.
type MetricValues interface {
	// Get returns the value for a metric name.
	// Returns 0, false if metric is unavailable.
	Get(name string) (float64, bool)

	// Timestamp returns when metrics were collected.
	Timestamp() time.Time
}

// MetricsAdapter adapts models.Metrics to the MetricValues interface.
type MetricsAdapter struct {
	metrics   *models.Metrics
	timestamp time.Time

	// Additional metrics from other sources
	replicationLagBytes         float64
	replicationLagAvailable     bool
	longestTransactionSeconds   float64
	longestTransactionAvailable bool
	idleInTransactionSeconds    float64
	idleInTransactionAvailable  bool
	maxConnections              int
	maxConnectionsAvailable     bool
}

// NewMetricsAdapter creates a new adapter for models.Metrics.
func NewMetricsAdapter(m *models.Metrics) *MetricsAdapter {
	if m == nil {
		return &MetricsAdapter{
			timestamp: time.Now(),
		}
	}
	return &MetricsAdapter{
		metrics:   m,
		timestamp: m.Timestamp,
	}
}

// Get returns the value for a metric name.
func (a *MetricsAdapter) Get(name string) (float64, bool) {
	if a.metrics == nil {
		return 0, false
	}

	switch name {
	case "active_connections", "connection_count":
		return float64(a.metrics.ConnectionCount), true
	case "max_connections":
		if a.maxConnectionsAvailable {
			return float64(a.maxConnections), true
		}
		return 0, false
	case "cache_hit_ratio":
		// Convert from percentage (0-100) to ratio (0-1) for threshold comparison
		return a.metrics.CacheHitRatio / 100.0, true
	case "cache_hit_ratio_pct":
		// Return raw percentage value
		return a.metrics.CacheHitRatio, true
	case "tps", "transactions_per_second":
		return a.metrics.TPS, true
	case "database_size", "db_size":
		return float64(a.metrics.DatabaseSize), true
	case "replication_lag_bytes":
		if a.replicationLagAvailable {
			return a.replicationLagBytes, true
		}
		return 0, false
	case "longest_transaction_seconds":
		if a.longestTransactionAvailable {
			return a.longestTransactionSeconds, true
		}
		return 0, false
	case "idle_in_transaction_seconds":
		if a.idleInTransactionAvailable {
			return a.idleInTransactionSeconds, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// Timestamp returns when the metrics were collected.
func (a *MetricsAdapter) Timestamp() time.Time {
	return a.timestamp
}

// SetReplicationLag sets the replication lag metric.
func (a *MetricsAdapter) SetReplicationLag(bytes float64) {
	a.replicationLagBytes = bytes
	a.replicationLagAvailable = true
}

// SetLongestTransaction sets the longest running transaction duration.
func (a *MetricsAdapter) SetLongestTransaction(seconds float64) {
	a.longestTransactionSeconds = seconds
	a.longestTransactionAvailable = true
}

// SetIdleInTransaction sets the longest idle-in-transaction duration.
func (a *MetricsAdapter) SetIdleInTransaction(seconds float64) {
	a.idleInTransactionSeconds = seconds
	a.idleInTransactionAvailable = true
}

// SetMaxConnections sets the max_connections setting.
func (a *MetricsAdapter) SetMaxConnections(max int) {
	a.maxConnections = max
	a.maxConnectionsAvailable = true
}

// AvailableMetrics returns the list of metric names that can be queried.
func AvailableMetrics() []string {
	return []string{
		"active_connections",
		"connection_count",
		"max_connections",
		"cache_hit_ratio",
		"cache_hit_ratio_pct",
		"tps",
		"transactions_per_second",
		"database_size",
		"db_size",
		"replication_lag_bytes",
		"longest_transaction_seconds",
		"idle_in_transaction_seconds",
	}
}

// MetricDescription returns a human-readable description of a metric.
func MetricDescription(name string) string {
	descriptions := map[string]string{
		"active_connections":           "Number of active database connections",
		"connection_count":             "Number of active database connections (alias)",
		"max_connections":              "Maximum allowed connections (from pg_settings)",
		"cache_hit_ratio":              "Buffer cache hit ratio (0-1)",
		"cache_hit_ratio_pct":          "Buffer cache hit ratio (0-100%)",
		"tps":                          "Transactions per second",
		"transactions_per_second":      "Transactions per second (alias)",
		"database_size":                "Database size in bytes",
		"db_size":                      "Database size in bytes (alias)",
		"replication_lag_bytes":        "Replication lag in bytes",
		"longest_transaction_seconds":  "Duration of longest running transaction in seconds",
		"idle_in_transaction_seconds":  "Duration of longest idle-in-transaction in seconds",
	}
	if desc, ok := descriptions[name]; ok {
		return desc
	}
	return "Unknown metric"
}
