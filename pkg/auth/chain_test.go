package auth

import (
	"errors"
	"net/http"
	"testing"
)

func TestChainAuthenticator_FirstWins(t *testing.T) {
	auth1 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return &Identity{Subject: "auth1"}, true, nil
	})
	auth2 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		t.Error("auth2 should not be called when auth1 succeeds")
		return &Identity{Subject: "auth2"}, true, nil
	})

	chain := NewChainAuthenticator(auth1, auth2)
	req, _ := http.NewRequest("GET", "/", nil)

	id, ok, err := chain.AuthenticateRequest(req)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !ok {
		t.Error("Expected authentication to succeed")
	}
	if id.Subject != "auth1" {
		t.Errorf("Expected subject 'auth1', got %q", id.Subject)
	}
}

func TestChainAuthenticator_FallsThrough(t *testing.T) {
	auth1 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		// No credentials found
		return nil, false, nil
	})
	auth2 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return &Identity{Subject: "auth2"}, true, nil
	})

	chain := NewChainAuthenticator(auth1, auth2)
	req, _ := http.NewRequest("GET", "/", nil)

	id, ok, err := chain.AuthenticateRequest(req)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !ok {
		t.Error("Expected authentication to succeed")
	}
	if id.Subject != "auth2" {
		t.Errorf("Expected subject 'auth2', got %q", id.Subject)
	}
}

func TestChainAuthenticator_ErrorStopsChain(t *testing.T) {
	expectedErr := errors.New("invalid token")

	auth1 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		// Found credentials but they're invalid
		return nil, false, expectedErr
	})
	auth2 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		t.Error("auth2 should not be called when auth1 returns error")
		return &Identity{Subject: "auth2"}, true, nil
	})

	chain := NewChainAuthenticator(auth1, auth2)
	req, _ := http.NewRequest("GET", "/", nil)

	id, ok, err := chain.AuthenticateRequest(req)

	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
	if ok {
		t.Error("Expected authentication to fail")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestChainAuthenticator_NoAuthenticatorsFound(t *testing.T) {
	auth1 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return nil, false, nil
	})
	auth2 := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return nil, false, nil
	})

	chain := NewChainAuthenticator(auth1, auth2)
	req, _ := http.NewRequest("GET", "/", nil)

	id, ok, err := chain.AuthenticateRequest(req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if ok {
		t.Error("Expected ok=false when no authenticator handled request")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestChainAuthenticator_EmptyChain(t *testing.T) {
	chain := NewChainAuthenticator()
	req, _ := http.NewRequest("GET", "/", nil)

	id, ok, err := chain.AuthenticateRequest(req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if ok {
		t.Error("Expected ok=false for empty chain")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestChainAuthenticator_ImmutableAfterCreation(t *testing.T) {
	auths := []Authenticator{
		AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
			return &Identity{Subject: "original"}, true, nil
		}),
	}

	chain := NewChainAuthenticator(auths...)

	// Modify original slice
	auths[0] = AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return &Identity{Subject: "modified"}, true, nil
	})

	req, _ := http.NewRequest("GET", "/", nil)
	id, _, _ := chain.AuthenticateRequest(req)

	if id.Subject != "original" {
		t.Error("Chain was affected by modification of original slice")
	}
}

func TestChainAuthenticator_Methods(t *testing.T) {
	chain := NewChainAuthenticator(
		NewBearerTokenAuthenticator("token1", "user1", nil),
		NewBearerTokenAuthenticator("token2", "user2", nil),
	)

	methods := chain.Methods()

	if len(methods) != 2 {
		t.Fatalf("Expected 2 methods, got %d", len(methods))
	}
	if methods[0] != "bearer-token" || methods[1] != "bearer-token" {
		t.Errorf("Expected [bearer-token, bearer-token], got %v", methods)
	}
}

func TestChainAuthenticator_Methods_SkipsNonDescriptors(t *testing.T) {
	chain := NewChainAuthenticator(
		NewBearerTokenAuthenticator("token", "user", nil),
		AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
			return nil, false, nil
		}),
	)

	methods := chain.Methods()

	if len(methods) != 1 {
		t.Fatalf("Expected 1 method, got %d", len(methods))
	}
	if methods[0] != "bearer-token" {
		t.Errorf("Expected [bearer-token], got %v", methods)
	}
}
