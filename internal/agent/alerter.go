package agent

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/alerts"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db/models"
)

// AlerterConfig holds configuration for the alerter.
type AlerterConfig struct {
	// Enabled indicates whether alerting is active.
	Enabled bool

	// WebhookURL is the endpoint for alert notifications.
	WebhookURL string

	// EvaluationInterval is how often to evaluate alert rules (default: 5s).
	EvaluationInterval time.Duration

	// Rules are the alert rules from main config.
	Rules []config.AlertRuleConfig
}

// Alerter integrates the existing alert engine with the agent.
// It periodically evaluates metrics and sends webhook notifications on state changes.
type Alerter struct {
	config  AlerterConfig
	engine  *alerts.Engine
	webhook *WebhookDelivery
	pool    *pgxpool.Pool
	logger  *log.Logger
	debug   bool

	// Current instance name for tagging webhooks
	instanceName string

	// Rule lookup for message formatting
	rules map[string]*alerts.Rule

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAlerter creates a new alerter instance.
func NewAlerter(cfg AlerterConfig, pool *pgxpool.Pool, instanceName string, logger *log.Logger, debug bool) *Alerter {
	if cfg.EvaluationInterval == 0 {
		cfg.EvaluationInterval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	a := &Alerter{
		config:       cfg,
		engine:       alerts.NewEngine(),
		pool:         pool,
		instanceName: instanceName,
		logger:       logger,
		debug:        debug,
		rules:        make(map[string]*alerts.Rule),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Load alert rules into the engine
	if err := a.engine.LoadRules(cfg.Rules); err != nil {
		logger.Printf("Failed to load alert rules: %v", err)
	}

	// Build rule lookup for message formatting
	for _, ruleCfg := range cfg.Rules {
		if !ruleCfg.IsEnabled() {
			continue
		}
		rule := &alerts.Rule{
			Name:     ruleCfg.Name,
			Metric:   ruleCfg.Metric,
			Warning:  ruleCfg.Warning,
			Critical: ruleCfg.Critical,
			Enabled:  ruleCfg.IsEnabled(),
			Message:  ruleCfg.Message,
		}
		if ruleCfg.Operator != "" {
			rule.Operator = alerts.Operator(ruleCfg.Operator)
		} else {
			rule.Operator = alerts.OpGreaterThan
		}
		a.rules[ruleCfg.Name] = rule
	}

	// Initialize webhook delivery if URL is configured
	if cfg.WebhookURL != "" {
		webhookCfg := DefaultWebhookConfig()
		webhookCfg.URL = cfg.WebhookURL
		a.webhook = NewWebhookDelivery(webhookCfg, logger, debug)
	}

	return a
}

// Start begins the alerter evaluation loop.
func (a *Alerter) Start() {
	if !a.config.Enabled {
		if a.debug {
			a.logger.Println("Alerter disabled, not starting")
		}
		return
	}

	if a.webhook != nil {
		a.webhook.Start()
	}

	a.wg.Add(1)
	go a.evaluationLoop()

	a.logger.Printf("Alerter started (rules: %d, interval: %v)", a.engine.RuleCount(), a.config.EvaluationInterval)
}

// Stop gracefully shuts down the alerter.
func (a *Alerter) Stop() {
	a.cancel()

	if a.webhook != nil {
		a.webhook.Stop()
	}

	a.wg.Wait()
	a.logger.Println("Alerter stopped")
}

// evaluationLoop periodically evaluates alert rules.
func (a *Alerter) evaluationLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.EvaluationInterval)
	defer ticker.Stop()

	// Initial evaluation
	a.evaluate()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.evaluate()
		}
	}
}

// evaluate collects metrics and evaluates all alert rules.
func (a *Alerter) evaluate() {
	// Collect current metrics from PostgreSQL
	metrics, err := a.collectMetrics()
	if err != nil {
		if a.debug {
			a.logger.Printf("Failed to collect metrics for alerting: %v", err)
		}
		return
	}

	// Create adapter for the alert engine
	adapter := alerts.NewMetricsAdapter(metrics)

	// Also collect max_connections for connection ratio alerts
	maxConns, err := a.getMaxConnections()
	if err == nil {
		adapter.SetMaxConnections(maxConns)
	}

	// Collect replication lag if available
	lagBytes, err := a.getReplicationLag()
	if err == nil {
		adapter.SetReplicationLag(lagBytes)
	}

	// Evaluate all rules
	changes := a.engine.Evaluate(adapter)

	// Send webhook notifications for state changes
	for _, change := range changes {
		a.handleStateChange(change)
	}
}

// handleStateChange processes a state change and sends notifications.
func (a *Alerter) handleStateChange(change alerts.StateChange) {
	rule := a.rules[change.RuleName]

	// Format message
	message := change.RuleName
	if rule != nil && rule.Message != "" {
		// The engine already formats the message, but we can access it via GetState
		if state, ok := a.engine.GetState(change.RuleName); ok {
			message = a.formatMessage(rule, state)
		}
	}

	// Log the state change
	if change.NewState == alerts.StateNormal {
		a.logger.Printf("Alert RESOLVED: %s (was %s, value: %.2f)",
			change.RuleName, change.PrevState, change.MetricValue)
	} else {
		a.logger.Printf("Alert %s: %s (value: %.2f, threshold: %.2f)",
			change.NewState, change.RuleName, change.MetricValue, change.Threshold)
	}

	// Send webhook if configured
	if a.webhook != nil {
		payload := CreateAlertPayload(change, rule, message, a.instanceName)
		a.webhook.SendAsync(payload)
	}
}

// formatMessage formats an alert message using Go text/template.
func (a *Alerter) formatMessage(rule *alerts.Rule, state *alerts.State) string {
	if rule.Message == "" {
		return rule.Name
	}

	// Determine threshold - use rule threshold when state is normal (resolved)
	threshold := state.Threshold
	if state.CurrentState == alerts.StateNormal && threshold == 0 {
		// Use warning threshold for resolved messages
		threshold = rule.Warning
	}

	// Build template data
	data := struct {
		Name      string
		Metric    string
		Warning   float64
		Critical  float64
		State     string
		PrevState string
		Value     float64
		Threshold float64
		ValueFmt  string
		ThreshFmt string
	}{
		Name:      rule.Name,
		Metric:    rule.Metric,
		Warning:   rule.Warning,
		Critical:  rule.Critical,
		State:     string(state.CurrentState),
		PrevState: string(state.PreviousState),
		Value:     state.MetricValue,
		Threshold: threshold,
		ValueFmt:  fmt.Sprintf("%.2f", state.MetricValue*100), // Convert ratio to percentage
		ThreshFmt: fmt.Sprintf("%.2f", threshold*100),
	}

	// Parse and execute template
	tmpl, err := template.New("message").Parse(rule.Message)
	if err != nil {
		return rule.Message
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return rule.Message
	}

	return buf.String()
}

// collectMetrics fetches current metrics from PostgreSQL.
func (a *Alerter) collectMetrics() (*models.Metrics, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	var metrics models.Metrics
	metrics.Timestamp = time.Now()

	// Get connection count
	err := a.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_stat_activity
		WHERE backend_type = 'client backend'
	`).Scan(&metrics.ConnectionCount)
	if err != nil {
		return nil, err
	}

	// Get cache hit ratio as percentage (0-100) to match models.Metrics convention
	// Sum across ALL databases to match TUI behavior
	err = a.pool.QueryRow(ctx, `
		SELECT
			CASE
				WHEN (SUM(blks_hit) + SUM(blks_read)) = 0 THEN 100.0
				ELSE SUM(blks_hit)::float8 * 100.0 / (SUM(blks_hit) + SUM(blks_read))::float8
			END as cache_hit_ratio
		FROM pg_stat_database
	`).Scan(&metrics.CacheHitRatio)
	if err != nil {
		// Non-critical, continue
		metrics.CacheHitRatio = 100.0 // 100%
	}
	// Get TPS (approximate from xact_commit + xact_rollback delta)
	// For simplicity, we just get current commit count - actual TPS would need history
	var xactCommit, xactRollback int64
	err = a.pool.QueryRow(ctx, `
		SELECT xact_commit, xact_rollback
		FROM pg_stat_database
		WHERE datname = current_database()
	`).Scan(&xactCommit, &xactRollback)
	if err == nil {
		// TPS calculation would need delta from previous sample
		// For now, return 0 - the metrics collector handles actual TPS
		metrics.TPS = 0
	}

	// Get database size
	err = a.pool.QueryRow(ctx, `
		SELECT pg_database_size(current_database())
	`).Scan(&metrics.DatabaseSize)
	if err != nil {
		metrics.DatabaseSize = 0
	}

	return &metrics, nil
}

// getMaxConnections retrieves the max_connections setting.
func (a *Alerter) getMaxConnections() (int, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	defer cancel()

	var maxConns int
	err := a.pool.QueryRow(ctx, `
		SELECT setting::int FROM pg_settings WHERE name = 'max_connections'
	`).Scan(&maxConns)
	return maxConns, err
}

// getReplicationLag retrieves total replication lag bytes.
func (a *Alerter) getReplicationLag() (float64, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	defer cancel()

	var lagBytes float64
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(pg_wal_lsn_diff(sent_lsn, replay_lsn)), 0)::float
		FROM pg_stat_replication
	`).Scan(&lagBytes)
	return lagBytes, err
}

// GetEngine returns the underlying alert engine for inspection.
func (a *Alerter) GetEngine() *alerts.Engine {
	return a.engine
}

// GetActiveAlerts returns currently active alerts.
func (a *Alerter) GetActiveAlerts() []alerts.ActiveAlert {
	return a.engine.GetActiveAlerts()
}
