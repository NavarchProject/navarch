package auth

import (
	"net/http"
)

// ChainAuthenticator tries multiple authenticators in sequence.
// The first authenticator to successfully authenticate the request wins.
// If an authenticator returns an error (invalid credentials), the chain stops
// and returns that error.
type ChainAuthenticator struct {
	authenticators []Authenticator
}

// NewChainAuthenticator creates a new chain authenticator.
// Authenticators are tried in the order provided.
//
// The chain follows these rules:
//  1. If an authenticator returns (identity, true, nil), authentication succeeds
//  2. If an authenticator returns (nil, false, error), authentication fails with that error
//  3. If an authenticator returns (nil, false, nil), try the next authenticator
//  4. If all authenticators return (nil, false, nil), the request is unauthenticated
func NewChainAuthenticator(authenticators ...Authenticator) *ChainAuthenticator {
	// Copy to prevent mutation
	authsCopy := make([]Authenticator, len(authenticators))
	copy(authsCopy, authenticators)

	return &ChainAuthenticator{
		authenticators: authsCopy,
	}
}

// AuthenticateRequest implements Authenticator.
func (c *ChainAuthenticator) AuthenticateRequest(r *http.Request) (*Identity, bool, error) {
	for _, auth := range c.authenticators {
		identity, ok, err := auth.AuthenticateRequest(r)
		if err != nil {
			// Authenticator found credentials but they were invalid
			return nil, false, err
		}
		if ok {
			// Successfully authenticated
			return identity, true, nil
		}
		// This authenticator didn't find credentials, try next
	}

	// No authenticator handled the request
	return nil, false, nil
}

// Methods returns the method names of all authenticators in the chain.
func (c *ChainAuthenticator) Methods() []string {
	var methods []string
	for _, a := range c.authenticators {
		if desc, ok := a.(AuthenticatorDescriptor); ok {
			methods = append(methods, desc.Method())
		}
	}
	return methods
}
