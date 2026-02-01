// Package auth provides authentication for the Navarch control plane.
//
// The package implements a pluggable authentication system inspired by
// Kubernetes' authenticator architecture. Custom authenticators can be
// implemented by satisfying the Authenticator interface.
package auth

import (
	"context"
	"net/http"
)

// Identity represents an authenticated entity (user, service, etc.).
// This is populated by authenticators after successful authentication.
type Identity struct {
	// Subject is the primary identifier for the authenticated entity.
	// This corresponds to the "sub" claim in JWT/OIDC tokens.
	// Examples: "user:jane@example.com", "service:node-daemon", "system:anonymous"
	Subject string

	// Groups contains the group memberships for authorization decisions.
	// Examples: ["system:nodes", "admins"]
	Groups []string

	// Extra contains additional claims from the authentication source.
	// This can include custom claims from JWT tokens, certificate attributes, etc.
	Extra map[string][]string
}

// Authenticator authenticates HTTP requests.
// Implementations should be safe for concurrent use.
type Authenticator interface {
	// AuthenticateRequest attempts to authenticate the given request.
	//
	// Returns:
	//   - (*Identity, true, nil): Authentication succeeded
	//   - (nil, false, nil): Authentication not attempted (no credentials present)
	//   - (nil, false, error): Authentication failed (invalid credentials)
	//
	// Implementations MUST:
	//   - Use constant-time comparison for secrets to prevent timing attacks
	//   - Not log or expose sensitive credential data
	//   - Be safe for concurrent use
	AuthenticateRequest(r *http.Request) (*Identity, bool, error)
}

// AuthenticatorFunc is an adapter to allow plain functions to be used as Authenticators.
type AuthenticatorFunc func(r *http.Request) (*Identity, bool, error)

// AuthenticateRequest implements Authenticator.
func (f AuthenticatorFunc) AuthenticateRequest(r *http.Request) (*Identity, bool, error) {
	return f(r)
}

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey int

const (
	identityKey contextKey = iota
)

// IdentityFromContext retrieves the authenticated Identity from the context.
// Returns nil if no identity is present (unauthenticated request).
func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}

// ContextWithIdentity returns a new context with the given Identity attached.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}
