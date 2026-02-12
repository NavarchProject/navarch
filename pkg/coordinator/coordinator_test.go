package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoop(t *testing.T) {
	ctx := context.Background()
	noop := NewNoop(nil)

	if noop.Name() != "noop" {
		t.Errorf("expected name 'noop', got %q", noop.Name())
	}

	// All operations should succeed
	if err := noop.Cordon(ctx, "node-1", "test"); err != nil {
		t.Errorf("Cordon failed: %v", err)
	}

	if err := noop.Uncordon(ctx, "node-1"); err != nil {
		t.Errorf("Uncordon failed: %v", err)
	}

	if err := noop.Drain(ctx, "node-1", "test"); err != nil {
		t.Errorf("Drain failed: %v", err)
	}

	drained, err := noop.IsDrained(ctx, "node-1")
	if err != nil {
		t.Errorf("IsDrained failed: %v", err)
	}
	if !drained {
		t.Error("expected IsDrained to return true")
	}
}

func TestWebhook(t *testing.T) {
	ctx := context.Background()

	t.Run("sends cordon webhook", func(t *testing.T) {
		var receivedEvent WebhookEvent
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedEvent); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			CordonURL: server.URL,
		}, nil)

		if err := webhook.Cordon(ctx, "node-1", "GPU failure"); err != nil {
			t.Errorf("Cordon failed: %v", err)
		}

		if receivedEvent.Event != "cordon" {
			t.Errorf("expected event 'cordon', got %q", receivedEvent.Event)
		}
		if receivedEvent.NodeID != "node-1" {
			t.Errorf("expected node_id 'node-1', got %q", receivedEvent.NodeID)
		}
		if receivedEvent.Reason != "GPU failure" {
			t.Errorf("expected reason 'GPU failure', got %q", receivedEvent.Reason)
		}
	})

	t.Run("sends drain webhook", func(t *testing.T) {
		var receivedEvent WebhookEvent
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedEvent)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			DrainURL: server.URL,
		}, nil)

		if err := webhook.Drain(ctx, "node-2", "maintenance"); err != nil {
			t.Errorf("Drain failed: %v", err)
		}

		if receivedEvent.Event != "drain" {
			t.Errorf("expected event 'drain', got %q", receivedEvent.Event)
		}
		if receivedEvent.NodeID != "node-2" {
			t.Errorf("expected node_id 'node-2', got %q", receivedEvent.NodeID)
		}
	})

	t.Run("checks drain status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			nodeID := r.URL.Query().Get("node_id")
			if nodeID != "node-1" {
				t.Errorf("expected node_id 'node-1', got %q", nodeID)
			}
			json.NewEncoder(w).Encode(WebhookDrainStatus{Drained: true})
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			DrainStatusURL: server.URL,
		}, nil)

		drained, err := webhook.IsDrained(ctx, "node-1")
		if err != nil {
			t.Errorf("IsDrained failed: %v", err)
		}
		if !drained {
			t.Error("expected drained to be true")
		}
	})

	t.Run("handles drain not complete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(WebhookDrainStatus{
				Drained: false,
				Message: "2 pods still running",
			})
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			DrainStatusURL: server.URL,
		}, nil)

		drained, err := webhook.IsDrained(ctx, "node-1")
		if err != nil {
			t.Errorf("IsDrained failed: %v", err)
		}
		if drained {
			t.Error("expected drained to be false")
		}
	})

	t.Run("includes custom headers", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			CordonURL: server.URL,
			Headers: map[string]string{
				"Authorization": "Bearer secret-token",
			},
		}, nil)

		webhook.Cordon(ctx, "node-1", "test")

		if authHeader != "Bearer secret-token" {
			t.Errorf("expected Authorization header 'Bearer secret-token', got %q", authHeader)
		}
	})

	t.Run("handles webhook error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		webhook := NewWebhook(WebhookConfig{
			CordonURL: server.URL,
		}, nil)

		err := webhook.Cordon(ctx, "node-1", "test")
		if err == nil {
			t.Error("expected error for 500 response")
		}
	})

	t.Run("skips when URL not configured", func(t *testing.T) {
		webhook := NewWebhook(WebhookConfig{}, nil)

		// Should succeed without making any requests
		if err := webhook.Cordon(ctx, "node-1", "test"); err != nil {
			t.Errorf("Cordon failed: %v", err)
		}
		if err := webhook.Drain(ctx, "node-1", "test"); err != nil {
			t.Errorf("Drain failed: %v", err)
		}

		// IsDrained should return true when no URL configured
		drained, err := webhook.IsDrained(ctx, "node-1")
		if err != nil {
			t.Errorf("IsDrained failed: %v", err)
		}
		if !drained {
			t.Error("expected drained to be true when no URL configured")
		}
	})
}
