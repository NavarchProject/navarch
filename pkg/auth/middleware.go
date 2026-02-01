package auth

import (
	"net/http"
)

// Middleware provides HTTP middleware for authentication.
type Middleware struct {
	authenticator  Authenticator
	excludedPaths  map[string]bool
	requireAuth    bool
	onUnauthorized func(w http.ResponseWriter, r *http.Request, err error)
}

// MiddlewareOption configures the authentication middleware.
type MiddlewareOption func(*Middleware)

// WithExcludedPaths sets paths that bypass authentication.
// These paths will be accessible without authentication.
// Common examples: /healthz, /readyz, /metrics
func WithExcludedPaths(paths ...string) MiddlewareOption {
	return func(m *Middleware) {
		for _, p := range paths {
			m.excludedPaths[p] = true
		}
	}
}

// WithRequireAuth sets whether authentication is required for non-excluded paths.
// If true (default), requests without valid credentials receive 401 Unauthorized.
// If false, unauthenticated requests proceed without an identity in context.
func WithRequireAuth(require bool) MiddlewareOption {
	return func(m *Middleware) {
		m.requireAuth = require
	}
}

// WithUnauthorizedHandler sets a custom handler for unauthorized requests.
// This is called when authentication fails or is required but missing.
func WithUnauthorizedHandler(handler func(w http.ResponseWriter, r *http.Request, err error)) MiddlewareOption {
	return func(m *Middleware) {
		m.onUnauthorized = handler
	}
}

// NewMiddleware creates authentication middleware with the given authenticator.
//
// By default:
//   - Authentication is required (unauthenticated requests get 401)
//   - No paths are excluded
//   - Generic error messages are returned (no credential details leaked)
//
// Use options to customize behavior:
//
//	middleware := NewMiddleware(authenticator,
//	    WithExcludedPaths("/healthz", "/readyz", "/metrics"),
//	    WithRequireAuth(true),
//	)
func NewMiddleware(authenticator Authenticator, opts ...MiddlewareOption) *Middleware {
	m := &Middleware{
		authenticator:  authenticator,
		excludedPaths:  make(map[string]bool),
		requireAuth:    true,
		onUnauthorized: defaultUnauthorizedHandler,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Wrap wraps an http.Handler with authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is excluded from authentication
		if m.excludedPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Attempt authentication
		identity, authenticated, err := m.authenticator.AuthenticateRequest(r)

		if err != nil {
			// Authentication was attempted but failed (invalid credentials)
			m.onUnauthorized(w, r, err)
			return
		}

		if !authenticated && m.requireAuth {
			// No credentials provided and authentication is required
			m.onUnauthorized(w, r, nil)
			return
		}

		// Attach identity to context (may be nil if auth not required and not provided)
		ctx := r.Context()
		if identity != nil {
			ctx = ContextWithIdentity(ctx, identity)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// defaultUnauthorizedHandler writes a generic 401 response.
// It deliberately does not include details about why authentication failed
// to avoid leaking information to potential attackers.
func defaultUnauthorizedHandler(w http.ResponseWriter, r *http.Request, err error) {
	// Set WWW-Authenticate header per RFC 7235
	w.Header().Set("WWW-Authenticate", `Bearer realm="navarch"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
