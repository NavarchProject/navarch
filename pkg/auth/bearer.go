package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

var (
	// ErrInvalidToken is returned when a token is present but invalid.
	ErrInvalidToken = errors.New("invalid bearer token")

	// ErrMalformedAuthHeader is returned when the Authorization header format is wrong.
	ErrMalformedAuthHeader = errors.New("malformed authorization header")
)

// BearerTokenAuthenticator authenticates requests using a static bearer token.
// This is suitable for service-to-service authentication with pre-shared tokens.
//
// For production use with user authentication, consider implementing a
// JWT or OIDC authenticator instead.
type BearerTokenAuthenticator struct {
	// token is the expected bearer token (stored as bytes for constant-time comparison)
	token []byte

	// identity is returned for successfully authenticated requests
	identity *Identity
}

// NewBearerTokenAuthenticator creates a new bearer token authenticator.
//
// Parameters:
//   - token: The expected bearer token. If empty, all requests will pass through
//     as unauthenticated (returning nil, false, nil).
//   - subject: The subject identifier for authenticated requests (e.g., "service:control-plane")
//   - groups: Optional groups to assign to authenticated requests
//
// The authenticator uses constant-time comparison to prevent timing attacks.
func NewBearerTokenAuthenticator(token string, subject string, groups []string) *BearerTokenAuthenticator {
	var tokenBytes []byte
	if token != "" {
		tokenBytes = []byte(token)
	}

	// Copy groups to prevent mutation
	var groupsCopy []string
	if len(groups) > 0 {
		groupsCopy = make([]string, len(groups))
		copy(groupsCopy, groups)
	}

	return &BearerTokenAuthenticator{
		token: tokenBytes,
		identity: &Identity{
			Subject: subject,
			Groups:  groupsCopy,
		},
	}
}

// AuthenticateRequest implements Authenticator.
func (a *BearerTokenAuthenticator) AuthenticateRequest(r *http.Request) (*Identity, bool, error) {
	// If no token is configured, don't attempt authentication
	if len(a.token) == 0 {
		return nil, false, nil
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// No credentials provided - not an error, just not authenticated
		return nil, false, nil
	}

	// Must be "Bearer <token>" format
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, false, ErrMalformedAuthHeader
	}

	providedToken := strings.TrimPrefix(authHeader, "Bearer ")
	if providedToken == "" {
		return nil, false, ErrMalformedAuthHeader
	}

	// Use constant-time comparison to prevent timing attacks
	providedBytes := []byte(providedToken)
	if subtle.ConstantTimeCompare(providedBytes, a.token) != 1 {
		return nil, false, ErrInvalidToken
	}

	// Return a copy of the identity to prevent mutation
	return &Identity{
		Subject: a.identity.Subject,
		Groups:  a.identity.Groups,
		Extra:   nil,
	}, true, nil
}

// Method implements AuthenticatorDescriptor.
func (a *BearerTokenAuthenticator) Method() string {
	return "bearer-token"
}
