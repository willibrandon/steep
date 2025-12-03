package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/alerts"
)

// WebhookConfig holds configuration for webhook delivery.
type WebhookConfig struct {
	// URL is the webhook endpoint.
	URL string

	// MaxRetries is the maximum number of retry attempts (default: 3).
	MaxRetries int

	// InitialBackoff is the initial backoff duration (default: 1s).
	InitialBackoff time.Duration

	// MaxBackoff is the maximum backoff duration (default: 30s).
	MaxBackoff time.Duration

	// Timeout is the HTTP request timeout (default: 10s).
	Timeout time.Duration
}

// DefaultWebhookConfig returns sensible defaults.
func DefaultWebhookConfig() WebhookConfig {
	return WebhookConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Timeout:        10 * time.Second,
	}
}

// WebhookPayload is the JSON payload sent to the webhook endpoint.
type WebhookPayload struct {
	// Event is the type of event (alert_triggered, alert_resolved).
	Event string `json:"event"`

	// Alert contains alert details.
	Alert WebhookAlert `json:"alert"`

	// Timestamp is when the webhook was generated.
	Timestamp time.Time `json:"timestamp"`

	// Agent contains agent metadata.
	Agent WebhookAgent `json:"agent"`
}

// WebhookAlert contains alert information.
type WebhookAlert struct {
	// Name is the rule name.
	Name string `json:"name"`

	// Metric is the metric being evaluated.
	Metric string `json:"metric"`

	// State is the new alert state (normal, warning, critical).
	State string `json:"state"`

	// PreviousState is the previous state.
	PreviousState string `json:"previous_state"`

	// Value is the current metric value.
	Value float64 `json:"value"`

	// Threshold is the threshold that was crossed.
	Threshold float64 `json:"threshold"`

	// TriggeredAt is when the state changed.
	TriggeredAt time.Time `json:"triggered_at"`

	// Message is the formatted alert message.
	Message string `json:"message,omitempty"`
}

// WebhookAgent contains agent metadata.
type WebhookAgent struct {
	// Version is the agent version.
	Version string `json:"version"`

	// Hostname is the agent hostname.
	Hostname string `json:"hostname,omitempty"`

	// Instance is the PostgreSQL instance name.
	Instance string `json:"instance,omitempty"`
}

// WebhookDelivery handles sending alert notifications to webhooks.
type WebhookDelivery struct {
	config WebhookConfig
	client *http.Client
	logger *log.Logger
	debug  bool

	// Queue for async delivery
	queue chan WebhookPayload
	wg    sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

// NewWebhookDelivery creates a new webhook delivery handler.
func NewWebhookDelivery(config WebhookConfig, logger *log.Logger, debug bool) *WebhookDelivery {
	if config.MaxRetries == 0 {
		config.MaxRetries = DefaultWebhookConfig().MaxRetries
	}
	if config.InitialBackoff == 0 {
		config.InitialBackoff = DefaultWebhookConfig().InitialBackoff
	}
	if config.MaxBackoff == 0 {
		config.MaxBackoff = DefaultWebhookConfig().MaxBackoff
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultWebhookConfig().Timeout
	}

	ctx, cancel := context.WithCancel(context.Background())

	wd := &WebhookDelivery{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		logger: logger,
		debug:  debug,
		queue:  make(chan WebhookPayload, 100), // Buffer up to 100 pending webhooks
		ctx:    ctx,
		cancel: cancel,
	}

	return wd
}

// Start begins the webhook delivery worker.
func (wd *WebhookDelivery) Start() {
	wd.wg.Add(1)
	go wd.deliveryWorker()
}

// Stop gracefully shuts down the webhook delivery.
func (wd *WebhookDelivery) Stop() {
	wd.cancel()
	close(wd.queue)
	wd.wg.Wait()
}

// SendAsync queues a webhook payload for async delivery.
func (wd *WebhookDelivery) SendAsync(payload WebhookPayload) {
	select {
	case wd.queue <- payload:
		if wd.debug {
			wd.logger.Printf("Webhook queued: %s for rule %s", payload.Event, payload.Alert.Name)
		}
	default:
		wd.logger.Printf("Webhook queue full, dropping: %s for rule %s", payload.Event, payload.Alert.Name)
	}
}

// deliveryWorker processes queued webhooks.
func (wd *WebhookDelivery) deliveryWorker() {
	defer wd.wg.Done()

	for {
		select {
		case <-wd.ctx.Done():
			// Drain remaining queue items
			for len(wd.queue) > 0 {
				select {
				case payload := <-wd.queue:
					_ = wd.deliverWithRetry(payload)
				default:
					return
				}
			}
			return
		case payload, ok := <-wd.queue:
			if !ok {
				return
			}
			if err := wd.deliverWithRetry(payload); err != nil {
				wd.logger.Printf("Webhook delivery failed after retries: %s for rule %s: %v",
					payload.Event, payload.Alert.Name, err)
			}
		}
	}
}

// deliverWithRetry attempts to deliver with exponential backoff.
func (wd *WebhookDelivery) deliverWithRetry(payload WebhookPayload) error {
	var lastErr error

	for attempt := 0; attempt <= wd.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate exponential backoff
			backoff := wd.calculateBackoff(attempt)
			if wd.debug {
				wd.logger.Printf("Webhook retry attempt %d/%d after %v", attempt, wd.config.MaxRetries, backoff)
			}

			select {
			case <-wd.ctx.Done():
				return wd.ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := wd.deliver(payload)
		if err == nil {
			if attempt > 0 {
				wd.logger.Printf("Webhook delivery succeeded on attempt %d for rule %s", attempt+1, payload.Alert.Name)
			} else if wd.debug {
				wd.logger.Printf("Webhook delivered: %s for rule %s", payload.Event, payload.Alert.Name)
			}
			return nil
		}

		lastErr = err
		if wd.debug {
			wd.logger.Printf("Webhook attempt %d failed: %v", attempt+1, err)
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// calculateBackoff returns the backoff duration for a retry attempt.
func (wd *WebhookDelivery) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: initialBackoff * 2^(attempt-1)
	multiplier := math.Pow(2, float64(attempt-1))
	backoff := time.Duration(float64(wd.config.InitialBackoff) * multiplier)

	if backoff > wd.config.MaxBackoff {
		backoff = wd.config.MaxBackoff
	}

	return backoff
}

// deliver sends a single webhook request.
func (wd *WebhookDelivery) deliver(payload WebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(wd.ctx, http.MethodPost, wd.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "steep-agent/"+Version)

	resp, err := wd.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body for error messages
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	// Consider 2xx as success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Check if we should retry based on status code
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return fmt.Errorf("HTTP %d: %s (retryable)", resp.StatusCode, string(respBody))
	}

	// Client errors (4xx except 429) are not retryable
	return fmt.Errorf("HTTP %d: %s (not retryable)", resp.StatusCode, string(respBody))
}

// CreateAlertPayload creates a webhook payload from a state change.
func CreateAlertPayload(change alerts.StateChange, rule *alerts.Rule, message string, instanceName string) WebhookPayload {
	event := "alert_triggered"
	if change.NewState == alerts.StateNormal {
		event = "alert_resolved"
	}

	metric := ""
	if rule != nil {
		metric = rule.Metric
	}

	return WebhookPayload{
		Event: event,
		Alert: WebhookAlert{
			Name:          change.RuleName,
			Metric:        metric,
			State:         string(change.NewState),
			PreviousState: string(change.PrevState),
			Value:         change.MetricValue,
			Threshold:     change.Threshold,
			TriggeredAt:   change.Timestamp,
			Message:       message,
		},
		Timestamp: time.Now(),
		Agent: WebhookAgent{
			Version:  Version,
			Instance: instanceName,
		},
	}
}
