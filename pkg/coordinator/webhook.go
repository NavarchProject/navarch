package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// WebhookConfig configures the webhook coordinator.
type WebhookConfig struct {
	// CordonURL is called when a node is cordoned.
	CordonURL string `yaml:"cordon_url"`

	// UncordonURL is called when a node is uncordoned.
	UncordonURL string `yaml:"uncordon_url"`

	// DrainURL is called when a node should be drained.
	DrainURL string `yaml:"drain_url"`

	// DrainStatusURL is called to check if a node is drained.
	// Should return {"drained": true/false}.
	DrainStatusURL string `yaml:"drain_status_url"`

	// Timeout for webhook requests. Defaults to 30s.
	Timeout time.Duration `yaml:"timeout"`

	// Headers to include in webhook requests (e.g., for authentication).
	Headers map[string]string `yaml:"headers"`
}

// WebhookEvent is the payload sent to webhook endpoints.
type WebhookEvent struct {
	Event     string `json:"event"`
	NodeID    string `json:"node_id"`
	Reason    string `json:"reason,omitempty"`
	Timestamp string `json:"timestamp"`
}

// WebhookDrainStatus is the expected response from drain_status_url.
type WebhookDrainStatus struct {
	Drained bool   `json:"drained"`
	Message string `json:"message,omitempty"`
}

// Webhook implements coordination via HTTP webhooks.
// This allows users to integrate with any workload system by providing
// HTTP endpoints that handle node lifecycle events.
type Webhook struct {
	config WebhookConfig
	client *http.Client
	logger *slog.Logger
}

// NewWebhook creates a new webhook coordinator.
func NewWebhook(config WebhookConfig, logger *slog.Logger) *Webhook {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Webhook{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
		logger: logger,
	}
}

// Name returns the coordinator name.
func (w *Webhook) Name() string {
	return "webhook"
}

// Cordon calls the cordon webhook endpoint.
func (w *Webhook) Cordon(ctx context.Context, nodeID string, reason string) error {
	if w.config.CordonURL == "" {
		w.logger.Debug("no cordon webhook configured, skipping")
		return nil
	}

	event := WebhookEvent{
		Event:     "cordon",
		NodeID:    nodeID,
		Reason:    reason,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	return w.sendWebhook(ctx, w.config.CordonURL, event)
}

// Uncordon calls the uncordon webhook endpoint.
func (w *Webhook) Uncordon(ctx context.Context, nodeID string) error {
	if w.config.UncordonURL == "" {
		w.logger.Debug("no uncordon webhook configured, skipping")
		return nil
	}

	event := WebhookEvent{
		Event:     "uncordon",
		NodeID:    nodeID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	return w.sendWebhook(ctx, w.config.UncordonURL, event)
}

// Drain calls the drain webhook endpoint.
func (w *Webhook) Drain(ctx context.Context, nodeID string, reason string) error {
	if w.config.DrainURL == "" {
		w.logger.Debug("no drain webhook configured, skipping")
		return nil
	}

	event := WebhookEvent{
		Event:     "drain",
		NodeID:    nodeID,
		Reason:    reason,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	return w.sendWebhook(ctx, w.config.DrainURL, event)
}

// IsDrained checks the drain status webhook endpoint.
func (w *Webhook) IsDrained(ctx context.Context, nodeID string) (bool, error) {
	if w.config.DrainStatusURL == "" {
		// No status endpoint configured, assume drained
		w.logger.Debug("no drain status webhook configured, assuming drained")
		return true, nil
	}

	// Build URL with node ID
	url := fmt.Sprintf("%s?node_id=%s", w.config.DrainStatusURL, nodeID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	// Add configured headers
	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("drain status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("drain status returned %d: %s", resp.StatusCode, string(body))
	}

	var status WebhookDrainStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode drain status response: %w", err)
	}

	w.logger.Debug("drain status checked",
		slog.String("node_id", nodeID),
		slog.Bool("drained", status.Drained),
		slog.String("message", status.Message),
	)

	return status.Drained, nil
}

func (w *Webhook) sendWebhook(ctx context.Context, url string, event WebhookEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add configured headers
	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	w.logger.Debug("sending webhook",
		slog.String("url", url),
		slog.String("event", event.Event),
		slog.String("node_id", event.NodeID),
	)

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(body))
	}

	w.logger.Info("webhook sent successfully",
		slog.String("url", url),
		slog.String("event", event.Event),
		slog.String("node_id", event.NodeID),
	)

	return nil
}

// Ensure Webhook implements Coordinator.
var _ Coordinator = (*Webhook)(nil)
