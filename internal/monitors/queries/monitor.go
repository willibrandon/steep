package queries

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// DataSourceType indicates the source of query data.
type DataSourceType int

const (
	DataSourceSampling DataSourceType = iota
	DataSourceLogParsing
)

// MonitorStatus represents the current status of the monitor.
type MonitorStatus int

const (
	MonitorStatusStopped MonitorStatus = iota
	MonitorStatusRunning
	MonitorStatusError
)

// MonitorConfig holds configuration for the query monitor.
type MonitorConfig struct {
	// RefreshInterval is how often to poll for new queries
	RefreshInterval time.Duration
	// RetentionDays is how long to keep query statistics
	RetentionDays int
	// LogPath is the path to PostgreSQL log file (for log parsing mode)
	LogPath string
	// LogLinePrefix is the log_line_prefix setting (for log parsing mode)
	LogLinePrefix string
}

// DefaultMonitorConfig returns default configuration.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		RefreshInterval: 5 * time.Second,
		RetentionDays:   7,
	}
}

// Monitor orchestrates query data collection and storage.
type Monitor struct {
	pool        *pgxpool.Pool
	store       *sqlite.QueryStatsStore
	fingerprint *Fingerprinter
	config      MonitorConfig

	// State
	status     MonitorStatus
	dataSource DataSourceType
	cancel     context.CancelFunc
}

// NewMonitor creates a new query monitor.
func NewMonitor(pool *pgxpool.Pool, store *sqlite.QueryStatsStore, config MonitorConfig) *Monitor {
	return &Monitor{
		pool:        pool,
		store:       store,
		fingerprint: NewFingerprinter(),
		config:      config,
		status:      MonitorStatusStopped,
		dataSource:  DataSourceSampling,
	}
}

// Start begins monitoring queries.
func (m *Monitor) Start(ctx context.Context) error {
	ctx, m.cancel = context.WithCancel(ctx)
	m.status = MonitorStatusRunning

	// Determine data source
	if m.config.LogPath != "" {
		m.dataSource = DataSourceLogParsing
		return m.startLogCollector(ctx)
	}

	m.dataSource = DataSourceSampling
	return m.startSamplingCollector(ctx)
}

// Stop stops monitoring.
func (m *Monitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.status = MonitorStatusStopped
	return nil
}

// Status returns the current monitor status.
func (m *Monitor) Status() MonitorStatus {
	return m.status
}

// DataSource returns the current data source type.
func (m *Monitor) DataSource() DataSourceType {
	return m.dataSource
}

// startSamplingCollector starts collecting via pg_stat_activity polling.
func (m *Monitor) startSamplingCollector(ctx context.Context) error {
	collector := NewSamplingCollector(m.pool, m.config.RefreshInterval)
	if err := collector.Start(ctx); err != nil {
		m.status = MonitorStatusError
		return err
	}

	go m.processEvents(ctx, collector.Events())
	return nil
}

// startLogCollector starts collecting via log file parsing.
func (m *Monitor) startLogCollector(ctx context.Context) error {
	collector := NewLogCollector(m.config.LogPath, m.config.LogLinePrefix)
	if err := collector.Start(ctx); err != nil {
		m.status = MonitorStatusError
		return err
	}

	go m.processEvents(ctx, collector.Events())
	return nil
}

// processEvents processes query events from a collector.
func (m *Monitor) processEvents(ctx context.Context, events <-chan QueryEvent) {
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Drain remaining events before exiting
			for event := range events {
				m.processEvent(context.Background(), event)
			}
			return

		case event, ok := <-events:
			if !ok {
				return
			}
			m.processEvent(ctx, event)

		case <-cleanupTicker.C:
			// Cleanup old records
			retention := time.Duration(m.config.RetentionDays) * 24 * time.Hour
			_, _ = m.store.Cleanup(ctx, retention)
		}
	}
}

// processEvent processes a single query event.
func (m *Monitor) processEvent(ctx context.Context, event QueryEvent) {
	// Generate fingerprint
	fingerprint, normalized, err := m.fingerprint.Fingerprint(event.Query)
	if err != nil {
		// Use original query if fingerprinting fails
		normalized = event.Query
	}

	// Store in database
	_ = m.store.Upsert(ctx, fingerprint, normalized, event.DurationMs, event.Rows)
}
