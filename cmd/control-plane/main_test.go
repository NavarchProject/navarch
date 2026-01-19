package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func TestHealthEndpoints(t *testing.T) {
	// Create test server
	database := db.NewInMemDB()
	defer database.Close()

	cfg := controlplane.DefaultConfig()
	srv := controlplane.NewServer(database, cfg, nil)

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)

	// Add health endpoints (using actual production handlers)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(database, nil))

	t.Run("healthz_returns_ok", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ok" {
			t.Errorf("Expected body 'ok', got '%s'", w.Body.String())
		}
	})

	t.Run("readyz_returns_ready", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/readyz", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ready" {
			t.Errorf("Expected body 'ready', got '%s'", w.Body.String())
		}
	})

	t.Run("healthz_supports_HEAD", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/healthz", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("readyz_supports_HEAD", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/readyz", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}
