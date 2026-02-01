package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenAuthMiddleware_NoToken(t *testing.T) {
	middleware := NewTokenAuthMiddleware("")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_ValidToken(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_InvalidToken(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_MissingToken(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_InvalidFormat(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Basic secret-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_HealthzExempt(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	wrapped := middleware.Wrap(handler)

	// /healthz should be exempt
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected /healthz to return 200, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_ReadyzExempt(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected /readyz to return 200, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_MetricsExempt(t *testing.T) {
	middleware := NewTokenAuthMiddleware("secret-token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected /metrics to return 200, got %d", rec.Code)
	}
}

func TestTokenAuthMiddleware_Enabled(t *testing.T) {
	tests := []struct {
		token   string
		enabled bool
	}{
		{"", false},
		{"secret", true},
	}

	for _, tc := range tests {
		middleware := NewTokenAuthMiddleware(tc.token)
		if middleware.Enabled() != tc.enabled {
			t.Errorf("Token %q: expected Enabled()=%v, got %v", tc.token, tc.enabled, middleware.Enabled())
		}
	}
}
