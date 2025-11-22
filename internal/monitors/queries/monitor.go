package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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
	// AutoEnableLogging prompts to enable query logging if disabled
	AutoEnableLogging bool
}

// LoggingStatus represents the current state of PostgreSQL query logging.
type LoggingStatus struct {
	Enabled       bool
	LogPath       string
	LogLinePrefix string
}

// DefaultMonitorConfig returns default configuration.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		RefreshInterval:   5 * time.Second,
		RetentionDays:     7,
		AutoEnableLogging: true,
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

	// Check logging status and auto-detect log path
	status, err := m.CheckLoggingStatus(ctx)
	if err == nil && status.Enabled && status.LogPath != "" {
		m.config.LogPath = status.LogPath
		m.config.LogLinePrefix = status.LogLinePrefix
	}

	// Determine data source
	if m.config.LogPath != "" {
		m.dataSource = DataSourceLogParsing
		return m.startLogCollector(ctx)
	}

	m.dataSource = DataSourceSampling
	return m.startSamplingCollector(ctx)
}

// CheckLoggingStatus checks if PostgreSQL query logging is enabled and returns the log path.
func (m *Monitor) CheckLoggingStatus(ctx context.Context) (*LoggingStatus, error) {
	var logMinDuration string
	var dataDir string
	var logLinePrefix string

	err := m.pool.QueryRow(ctx, "SHOW log_min_duration_statement").Scan(&logMinDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to check log_min_duration_statement: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW data_directory").Scan(&dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get data_directory: %w", err)
	}

	err = m.pool.QueryRow(ctx, "SHOW log_line_prefix").Scan(&logLinePrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to get log_line_prefix: %w", err)
	}

	// -1 means disabled, any other value means enabled
	enabled := logMinDuration != "-1"
	logPath := filepath.Join(dataDir, "postgresql.log")

	return &LoggingStatus{
		Enabled:       enabled,
		LogPath:       logPath,
		LogLinePrefix: logLinePrefix,
	}, nil
}

// EnableLogging enables query logging with parameter capture.
func (m *Monitor) EnableLogging(ctx context.Context) error {
	// Set log_min_duration_statement to log all completed queries with duration
	_, err := m.pool.Exec(ctx, "ALTER SYSTEM SET log_min_duration_statement = 0")
	if err != nil {
		return fmt.Errorf("failed to set log_min_duration_statement: %w", err)
	}

	// Set log_statement to capture bound parameters for accurate EXPLAIN estimates
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_statement = 'all'")
	if err != nil {
		return fmt.Errorf("failed to set log_statement: %w", err)
	}

	// Enable parameter logging in DETAIL lines
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_parameter_max_length = -1")
	if err != nil {
		return fmt.Errorf("failed to set log_parameter_max_length: %w", err)
	}

	// Ensure DETAIL lines are included
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_error_verbosity = 'default'")
	if err != nil {
		return fmt.Errorf("failed to set log_error_verbosity: %w", err)
	}

	// Disable executor stats that interfere with parameter DETAIL lines
	_, err = m.pool.Exec(ctx, "ALTER SYSTEM SET log_executor_stats = off")
	if err != nil {
		return fmt.Errorf("failed to set log_executor_stats: %w", err)
	}

	_, err = m.pool.Exec(ctx, "SELECT pg_reload_conf()")
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	return nil
}

// IsLoggingEnabled returns whether query logging is currently enabled.
func (m *Monitor) IsLoggingEnabled(ctx context.Context) bool {
	status, err := m.CheckLoggingStatus(ctx)
	if err != nil {
		return false
	}
	return status.Enabled
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

	// Get row estimate via EXPLAIN if not available
	rows := event.Rows
	if rows == 0 {
		rows = m.estimateRows(ctx, event.Query, event.Params)
	}

	// Convert params to JSON for storage
	var sampleParams string
	if len(event.Params) > 0 {
		if paramsJSON, err := json.Marshal(event.Params); err == nil {
			sampleParams = string(paramsJSON)
		}
	}

	// Store in database
	_ = m.store.Upsert(ctx, fingerprint, normalized, event.DurationMs, rows, sampleParams)
}

// estimateRows runs EXPLAIN to get estimated row count for a query.
func (m *Monitor) estimateRows(ctx context.Context, query string, params map[string]string) int64 {
	// Only estimate for SELECT queries
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(trimmed, "SELECT") {
		return 0
	}

	// Replace parameter placeholders with actual values if available, otherwise use 0
	queryForExplain := query
	if len(params) > 0 {
		// Use captured params for accurate estimates
		for param, value := range params {
			// Quote string values for SQL
			quotedValue := "'" + strings.ReplaceAll(value, "'", "''") + "'"
			queryForExplain = strings.ReplaceAll(queryForExplain, param, quotedValue)
		}
	} else {
		// Fall back to replacing with 0 for unknown params
		paramRe := regexp.MustCompile(`\$\d+`)
		queryForExplain = paramRe.ReplaceAllString(query, "0")
	}

	// Run EXPLAIN (FORMAT JSON) to get plan with row estimates
	explainQuery := fmt.Sprintf("EXPLAIN (FORMAT JSON) %s", queryForExplain)

	var planJSON string
	err := m.pool.QueryRow(ctx, explainQuery).Scan(&planJSON)
	if err != nil {
		return 0
	}

	// Parse JSON to extract Plan Rows
	// Format: [{"Plan": {"Plan Rows": 100, ...}}]
	var plans []struct {
		Plan struct {
			PlanRows float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}

	if err := json.Unmarshal([]byte(planJSON), &plans); err != nil {
		return 0
	}

	if len(plans) > 0 {
		return int64(plans[0].Plan.PlanRows)
	}

	return 0
}

// GetExplainPlan runs EXPLAIN (FORMAT JSON) and returns the formatted plan.
func (m *Monitor) GetExplainPlan(ctx context.Context, query string) (string, error) {
	// Replace parameter placeholders with NULL for EXPLAIN
	// (we don't have params here, so use NULL which is valid for any type)
	paramRe := regexp.MustCompile(`\$\d+`)
	queryForExplain := paramRe.ReplaceAllString(query, "NULL")

	// Run EXPLAIN (FORMAT JSON) to get full plan
	explainQuery := fmt.Sprintf("EXPLAIN (FORMAT JSON) %s", queryForExplain)

	var planJSON string
	err := m.pool.QueryRow(ctx, explainQuery).Scan(&planJSON)
	if err != nil {
		return "", fmt.Errorf("EXPLAIN failed: %w", err)
	}

	return planJSON, nil
}

// Pool returns the database connection pool.
func (m *Monitor) Pool() *pgxpool.Pool {
	return m.pool
}
