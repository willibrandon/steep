package alerts

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"
	"time"

	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/logger"
)

// Store persists alert events.
type Store interface {
	SaveEvent(ctx context.Context, event *Event) error
}

// Engine evaluates alert rules against metrics and manages alert state.
type Engine struct {
	mu sync.RWMutex

	// rules holds the loaded alert rules keyed by name.
	rules map[string]*Rule

	// states holds the current state for each rule keyed by rule name.
	states map[string]*State

	// expressions holds parsed expressions for each rule keyed by rule name.
	expressions map[string]Expression

	// store is the optional persistence layer for alert events.
	store Store

	// enabled indicates if the engine is active.
	enabled bool
}

// NewEngine creates a new alert engine.
func NewEngine() *Engine {
	return &Engine{
		rules:       make(map[string]*Rule),
		states:      make(map[string]*State),
		expressions: make(map[string]Expression),
		enabled:     true,
	}
}

// SetStore sets the persistence store for alert events.
func (e *Engine) SetStore(store Store) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = store
}

// SetEnabled enables or disables the engine.
func (e *Engine) SetEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = enabled
}

// IsEnabled returns whether the engine is enabled.
func (e *Engine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// LoadRules loads alert rules from configuration.
// Invalid rules are logged as warnings and skipped.
func (e *Engine) LoadRules(ruleConfigs []config.AlertRuleConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear existing rules and states
	e.rules = make(map[string]*Rule)
	e.states = make(map[string]*State)
	e.expressions = make(map[string]Expression)

	parser := NewParser()

	for i, cfg := range ruleConfigs {
		// Skip disabled rules
		if !cfg.IsEnabled() {
			logger.Debug("alert rule disabled, skipping", "rule", cfg.Name)
			continue
		}

		// Convert config to domain rule
		rule := &Rule{
			Name:     cfg.Name,
			Metric:   cfg.Metric,
			Warning:  cfg.Warning,
			Critical: cfg.Critical,
			Enabled:  cfg.IsEnabled(),
			Message:  cfg.Message,
		}

		// Parse operator (default to ">")
		if cfg.Operator != "" {
			rule.Operator = Operator(cfg.Operator)
		} else {
			rule.Operator = OpGreaterThan
		}

		// Validate the rule
		if err := rule.Validate(); err != nil {
			logger.Warn("invalid alert rule, skipping",
				"index", i,
				"rule", cfg.Name,
				"error", err.Error())
			continue
		}

		// Parse the metric expression
		expr, err := parser.Parse(cfg.Metric)
		if err != nil {
			logger.Warn("invalid metric expression, skipping rule",
				"rule", cfg.Name,
				"metric", cfg.Metric,
				"error", err.Error())
			continue
		}

		// Store the rule, state, and expression
		e.rules[rule.Name] = rule
		e.states[rule.Name] = NewState(rule.Name)
		e.expressions[rule.Name] = expr

		logger.Debug("loaded alert rule",
			"rule", rule.Name,
			"metric", rule.Metric,
			"operator", rule.Operator,
			"warning", rule.Warning,
			"critical", rule.Critical)
	}

	logger.Info("alert engine loaded rules",
		"total", len(ruleConfigs),
		"loaded", len(e.rules))

	return nil
}

// Evaluate checks all rules against current metrics.
// Returns state changes that occurred (for notifications).
func (e *Engine) Evaluate(metrics MetricValues) []StateChange {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.enabled {
		return nil
	}

	var changes []StateChange

	for name, rule := range e.rules {
		if !rule.Enabled {
			continue
		}

		state := e.states[name]
		expr := e.expressions[name]

		// Evaluate the metric expression
		value, err := expr.Evaluate(metrics)
		if err != nil {
			// Metric not available - log at debug level and skip
			logger.Debug("metric evaluation failed",
				"rule", name,
				"metric", rule.Metric,
				"error", err.Error())
			continue
		}

		// Check for state transition
		if state.Transition(value, rule) {
			change := StateChange{
				RuleName:    name,
				PrevState:   state.PreviousState,
				NewState:    state.CurrentState,
				MetricValue: value,
				Threshold:   state.Threshold,
				Timestamp:   state.TriggeredAt,
			}
			changes = append(changes, change)

			logger.Info("alert state changed",
				"rule", name,
				"prev_state", state.PreviousState,
				"new_state", state.CurrentState,
				"value", value,
				"threshold", state.Threshold)

			// Persist the event if store is available
			if e.store != nil {
				event := &Event{
					RuleName:       name,
					PrevState:      state.PreviousState,
					NewState:       state.CurrentState,
					MetricValue:    value,
					ThresholdValue: state.Threshold,
					TriggeredAt:    state.TriggeredAt,
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := e.store.SaveEvent(ctx, event); err != nil {
					logger.Warn("failed to persist alert event",
						"rule", name,
						"error", err.Error())
				}
				cancel()
			}
		}
	}

	return changes
}

// GetActiveAlerts returns all currently firing alerts.
func (e *Engine) GetActiveAlerts() []ActiveAlert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var alerts []ActiveAlert

	for name, state := range e.states {
		if !state.IsActive() {
			continue
		}

		rule := e.rules[name]
		if rule == nil {
			continue
		}

		alert := ActiveAlert{
			RuleName:     name,
			State:        state.CurrentState,
			MetricValue:  state.MetricValue,
			Threshold:    state.Threshold,
			TriggeredAt:  state.TriggeredAt,
			Duration:     state.Duration(),
			Acknowledged: state.Acknowledged,
			Message:      e.formatMessage(rule, state),
		}
		alerts = append(alerts, alert)
	}

	return alerts
}

// GetState returns the current state for a specific rule.
func (e *Engine) GetState(ruleName string) (*State, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	state, ok := e.states[ruleName]
	if !ok {
		return nil, false
	}

	// Return a copy to avoid external modification
	stateCopy := *state
	return &stateCopy, true
}

// Acknowledge marks an alert as acknowledged.
func (e *Engine) Acknowledge(ruleName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.states[ruleName]
	if !ok {
		return &AlertNotFoundError{RuleName: ruleName}
	}

	if !state.IsActive() {
		return &AlertNotActiveError{RuleName: ruleName}
	}

	if state.Acknowledged {
		return &AlertAlreadyAcknowledgedError{RuleName: ruleName}
	}

	state.Acknowledge()

	logger.Info("alert acknowledged", "rule", ruleName)

	return nil
}

// Unacknowledge removes acknowledgment from an alert.
func (e *Engine) Unacknowledge(ruleName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.states[ruleName]
	if !ok {
		return &AlertNotFoundError{RuleName: ruleName}
	}

	if !state.Acknowledged {
		return nil // Already unacknowledged, no-op
	}

	state.Unacknowledge()

	logger.Info("alert unacknowledged", "rule", ruleName)

	return nil
}

// RuleCount returns the number of loaded rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}

// EnabledRuleCount returns the number of enabled rules.
func (e *Engine) EnabledRuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	count := 0
	for _, rule := range e.rules {
		if rule.Enabled {
			count++
		}
	}
	return count
}

// WarningCount returns the number of alerts in Warning state.
func (e *Engine) WarningCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	count := 0
	for _, state := range e.states {
		if state.CurrentState == StateWarning {
			count++
		}
	}
	return count
}

// CriticalCount returns the number of alerts in Critical state.
func (e *Engine) CriticalCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	count := 0
	for _, state := range e.states {
		if state.CurrentState == StateCritical {
			count++
		}
	}
	return count
}

// messageTemplateData holds data available for message template substitution.
type messageTemplateData struct {
	// From Rule
	Name     string
	Metric   string
	Warning  float64
	Critical float64

	// From State
	State      string  // "normal", "warning", "critical"
	PrevState  string  // Previous state
	Value      float64 // Current metric value
	Threshold  float64 // Threshold that was crossed
	ValueFmt   string  // Value formatted with 2 decimal places
	ThreshFmt  string  // Threshold formatted with 2 decimal places
}

// formatMessage formats an alert message for display.
// Supports Go text/template syntax with fields: Name, Metric, Warning, Critical,
// State, PrevState, Value, Threshold, ValueFmt, ThreshFmt.
func (e *Engine) formatMessage(rule *Rule, state *State) string {
	if rule.Message == "" {
		return rule.Name
	}

	// Build template data
	data := messageTemplateData{
		Name:      rule.Name,
		Metric:    rule.Metric,
		Warning:   rule.Warning,
		Critical:  rule.Critical,
		State:     string(state.CurrentState),
		PrevState: string(state.PreviousState),
		Value:     state.MetricValue,
		Threshold: state.Threshold,
		ValueFmt:  fmt.Sprintf("%.2f", state.MetricValue),
		ThreshFmt: fmt.Sprintf("%.2f", state.Threshold),
	}

	// Parse and execute template
	tmpl, err := template.New("message").Parse(rule.Message)
	if err != nil {
		logger.Debug("failed to parse alert message template", "rule", rule.Name, "error", err)
		return rule.Message // Return raw message on parse error
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		logger.Debug("failed to execute alert message template", "rule", rule.Name, "error", err)
		return rule.Message // Return raw message on execute error
	}

	return buf.String()
}

// AlertNotFoundError indicates an alert rule was not found.
type AlertNotFoundError struct {
	RuleName string
}

func (e *AlertNotFoundError) Error() string {
	return "alert rule not found: " + e.RuleName
}

// AlertNotActiveError indicates an alert is not in an active state.
type AlertNotActiveError struct {
	RuleName string
}

func (e *AlertNotActiveError) Error() string {
	return "alert is not active: " + e.RuleName
}

// AlertAlreadyAcknowledgedError indicates an alert was already acknowledged.
type AlertAlreadyAcknowledgedError struct {
	RuleName string
}

func (e *AlertAlreadyAcknowledgedError) Error() string {
	return "alert already acknowledged: " + e.RuleName
}
