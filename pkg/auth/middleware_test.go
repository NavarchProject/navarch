package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_AuthenticatedRequest(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", []string{"admins"})
	middleware := NewMiddleware(auth)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		if id == nil {
			t.Error("Expected identity in context")
			return
		}
		if id.Subject != "user:test" {
			t.Errorf("Expected subject 'user:test', got %q", id.Subject)
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestMiddleware_UnauthenticatedRequest_Required(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth, WithRequireAuth(true))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for unauthenticated request")
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("Expected WWW-Authenticate header")
	}
}

func TestMiddleware_UnauthenticatedRequest_NotRequired(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth, WithRequireAuth(false))

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		id := IdentityFromContext(r.Context())
		if id != nil {
			t.Errorf("Expected nil identity, got %+v", id)
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Handler should be called when auth not required")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestMiddleware_InvalidCredentials(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for invalid credentials")
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

func TestMiddleware_ExcludedPaths(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth,
		WithExcludedPaths("/healthz", "/readyz", "/metrics"),
		WithRequireAuth(true),
	)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	paths := []string{"/healthz", "/readyz", "/metrics"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handlerCalled = false
			req := httptest.NewRequest("GET", path, nil)
			// No Authorization header
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if !handlerCalled {
				t.Errorf("Handler should be called for excluded path %s", path)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestMiddleware_NonExcludedPathRequiresAuth(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth,
		WithExcludedPaths("/healthz"),
		WithRequireAuth(true),
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without auth")
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestMiddleware_CustomUnauthorizedHandler(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)

	customHandlerCalled := false
	var receivedErr error

	middleware := NewMiddleware(auth,
		WithUnauthorizedHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			customHandlerCalled = true
			receivedErr = err
			w.WriteHeader(http.StatusForbidden) // Custom status
		}),
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !customHandlerCalled {
		t.Error("Custom unauthorized handler should be called")
	}
	if !errors.Is(receivedErr, ErrInvalidToken) {
		t.Errorf("Expected ErrInvalidToken, got %v", receivedErr)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected custom status 403, got %d", rec.Code)
	}
}

func TestMiddleware_GenericErrorMessage(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "user:test", nil)
	middleware := NewMiddleware(auth)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	body := rec.Body.String()
	// Should not leak why auth failed
	if body != "unauthorized\n" {
		t.Errorf("Response should be generic 'unauthorized', got %q", body)
	}
}

// Legacy API tests (for backwards compatibility)

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
