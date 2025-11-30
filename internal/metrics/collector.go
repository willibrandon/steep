package metrics

import (
	"context"
	"sync"
	"time"
)

// Common metric names used throughout the application.
const (
	MetricTPS           = "tps"
	MetricConnections   = "connections"
	MetricCacheHitRatio = "cache_hit_ratio"
)

// MetricsStore defines the interface for persistent metric storage.
type MetricsStore interface {
	SaveDataPoint(ctx context.Context, metricName string, dp DataPoint) error
	SaveBatch(ctx context.Context, metricName string, points []DataPoint) error
	GetHistory(ctx context.Context, metricName string, since time.Time, limit int) ([]DataPoint, error)
	GetAggregated(ctx context.Context, metricName string, since time.Time, intervalSeconds int) ([]DataPoint, error)
	Prune(ctx context.Context, retentionDays int) (int64, error)
}

// MetricSeries represents a named time-series of data points.
type MetricSeries struct {
	Name       string
	Buffer     *CircularBuffer
	LastUpdate time.Time
}

// Collector handles metrics collection with in-memory buffering and optional persistence.
type Collector struct {
	series   map[string]*MetricSeries
	store    MetricsStore
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	capacity int

	// Configuration
	persistInterval time.Duration
	pruneInterval   time.Duration
	retentionDays   int
}

// CollectorOption configures a Collector.
type CollectorOption func(*Collector)

// WithCapacity sets the buffer capacity for each metric series.
func WithCapacity(capacity int) CollectorOption {
	return func(c *Collector) {
		c.capacity = capacity
	}
}

// WithStore sets the persistent storage backend.
func WithStore(store MetricsStore) CollectorOption {
	return func(c *Collector) {
		c.store = store
	}
}

// WithPersistInterval sets how often to persist data to storage.
func WithPersistInterval(d time.Duration) CollectorOption {
	return func(c *Collector) {
		c.persistInterval = d
	}
}

// WithPruneInterval sets how often to prune old data.
func WithPruneInterval(d time.Duration) CollectorOption {
	return func(c *Collector) {
		c.pruneInterval = d
	}
}

// WithRetentionDays sets the data retention period.
func WithRetentionDays(days int) CollectorOption {
	return func(c *Collector) {
		c.retentionDays = days
	}
}

// NewCollector creates a new metrics collector.
func NewCollector(opts ...CollectorOption) *Collector {
	c := &Collector{
		series:          make(map[string]*MetricSeries),
		capacity:        DefaultBufferCapacity,
		persistInterval: 10 * time.Second,
		pruneInterval:   time.Hour,
		retentionDays:   7,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Record adds a new data point for a metric.
// Thread-safe; called from monitor goroutine.
func (c *Collector) Record(metricName string, value float64) {
	dp := NewDataPoint(value)
	if !dp.IsValid() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	series, ok := c.series[metricName]
	if !ok {
		series = &MetricSeries{
			Name:   metricName,
			Buffer: NewCircularBuffer(c.capacity),
		}
		c.series[metricName] = series
	}

	series.Buffer.Push(dp)
	series.LastUpdate = dp.Timestamp
}

// RecordAt adds a data point with a specific timestamp.
func (c *Collector) RecordAt(metricName string, timestamp time.Time, value float64) {
	dp := NewDataPointAt(timestamp, value)
	if !dp.IsValid() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	series, ok := c.series[metricName]
	if !ok {
		series = &MetricSeries{
			Name:   metricName,
			Buffer: NewCircularBuffer(c.capacity),
		}
		c.series[metricName] = series
	}

	series.Buffer.Push(dp)
	series.LastUpdate = dp.Timestamp
}

// GetValues returns metric values for the specified time window.
// Returns float64 slice suitable for asciigraph.Plot().
// Thread-safe; called from View() rendering.
func (c *Collector) GetValues(metricName string, window TimeWindow) []float64 {
	c.mu.RLock()
	series, ok := c.series[metricName]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	// For short windows, use in-memory buffer only
	if !window.RequiresPersistence() {
		return series.Buffer.GetValuesForWindow(window)
	}

	// For longer windows, merge with persistent storage
	return c.getMergedValues(metricName, window)
}

// GetDataPoints returns full DataPoint structs for the time window.
// Used when timestamps are needed (e.g., heatmap aggregation).
func (c *Collector) GetDataPoints(metricName string, window TimeWindow) []DataPoint {
	c.mu.RLock()
	series, ok := c.series[metricName]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	since := time.Now().Add(-window.Duration())

	// For short windows, use in-memory buffer only
	if !window.RequiresPersistence() {
		return series.Buffer.GetSince(since)
	}

	// For longer windows, merge with persistent storage
	return c.getMergedDataPoints(metricName, window)
}

// GetLatest returns the most recent value for a metric.
func (c *Collector) GetLatest(metricName string) (float64, time.Time, bool) {
	c.mu.RLock()
	series, ok := c.series[metricName]
	c.mu.RUnlock()

	if !ok {
		return 0, time.Time{}, false
	}

	dp, ok := series.Buffer.Latest()
	if !ok {
		return 0, time.Time{}, false
	}

	return dp.Value, dp.Timestamp, true
}

// Start begins background persistence and cleanup goroutine.
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if c.store != nil {
		c.wg.Add(1)
		go c.persistLoop()

		c.wg.Add(1)
		go c.pruneLoop()
	}

	return nil
}

// Stop gracefully shuts down the collector.
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}

// HasData returns true if there is any data for the metric.
func (c *Collector) HasData(metricName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	series, ok := c.series[metricName]
	if !ok {
		return false
	}
	return !series.Buffer.IsEmpty()
}

// MetricNames returns all registered metric names.
func (c *Collector) MetricNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.series))
	for name := range c.series {
		names = append(names, name)
	}
	return names
}

// persistLoop periodically persists buffered data to storage.
func (c *Collector) persistLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.persistInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Final persist before shutdown
			c.persistAll()
			return
		case <-ticker.C:
			c.persistAll()
		}
	}
}

// persistAll persists all buffered data to storage.
func (c *Collector) persistAll() {
	if c.store == nil {
		return
	}

	c.mu.RLock()
	names := make([]string, 0, len(c.series))
	for name := range c.series {
		names = append(names, name)
	}
	c.mu.RUnlock()

	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	for _, name := range names {
		c.mu.RLock()
		series := c.series[name]
		c.mu.RUnlock()

		if series == nil {
			continue
		}

		// Get recent points to persist (last persistInterval worth)
		since := time.Now().Add(-c.persistInterval - time.Second)
		points := series.Buffer.GetSince(since)

		if len(points) > 0 {
			_ = c.store.SaveBatch(ctx, name, points)
		}
	}
}

// pruneLoop periodically removes old data from storage.
func (c *Collector) pruneLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
			_, _ = c.store.Prune(ctx, c.retentionDays)
			cancel()
		}
	}
}

// getMergedValues returns values from both buffer and persistent storage.
func (c *Collector) getMergedValues(metricName string, window TimeWindow) []float64 {
	if c.store == nil {
		c.mu.RLock()
		series := c.series[metricName]
		c.mu.RUnlock()
		if series != nil {
			return series.Buffer.GetValuesForWindow(window)
		}
		return nil
	}

	since := time.Now().Add(-window.Duration())
	granularity := window.Granularity()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var points []DataPoint

	// For 24h window, use aggregated data
	if window == TimeWindow24h {
		points, _ = c.store.GetAggregated(ctx, metricName, since, int(granularity.Seconds()))
	} else {
		points, _ = c.store.GetHistory(ctx, metricName, since, 10000)
	}

	if len(points) == 0 {
		c.mu.RLock()
		series := c.series[metricName]
		c.mu.RUnlock()
		if series != nil {
			return series.Buffer.GetValuesForWindow(window)
		}
		return nil
	}

	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}
	return values
}

// getMergedDataPoints returns data points from both buffer and persistent storage.
func (c *Collector) getMergedDataPoints(metricName string, window TimeWindow) []DataPoint {
	if c.store == nil {
		c.mu.RLock()
		series := c.series[metricName]
		c.mu.RUnlock()
		if series != nil {
			since := time.Now().Add(-window.Duration())
			return series.Buffer.GetSince(since)
		}
		return nil
	}

	since := time.Now().Add(-window.Duration())
	granularity := window.Granularity()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// For 24h window, use aggregated data
	if window == TimeWindow24h {
		points, _ := c.store.GetAggregated(ctx, metricName, since, int(granularity.Seconds()))
		return points
	}

	points, _ := c.store.GetHistory(ctx, metricName, since, 10000)
	return points
}
