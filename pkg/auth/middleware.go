package auth

import (
	"net/http"
	"strings"
)

// TokenAuthMiddleware provides token-based authentication for HTTP handlers.
type TokenAuthMiddleware struct {
	token string
}

// NewTokenAuthMiddleware creates a new token authentication middleware.
// If token is empty, authentication is disabled.
func NewTokenAuthMiddleware(token string) *TokenAuthMiddleware {
	return &TokenAuthMiddleware{token: token}
}

// Wrap wraps an http.Handler with token authentication.
// Requests to /healthz and /readyz are exempted from authentication.
func (m *TokenAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health endpoints
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// If no token configured, skip authentication
		if m.token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		// Expect "Bearer <token>" format
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			// No "Bearer " prefix found
			http.Error(w, "invalid authorization format, expected 'Bearer <token>'", http.StatusUnauthorized)
			return
		}

		if token != m.token {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Enabled returns true if authentication is enabled (token is configured).
func (m *TokenAuthMiddleware) Enabled() bool {
	return m.token != ""
}
