package auth

import (
	"context"
	"net/http"
	"testing"
)

func TestIdentityFromContext_NoIdentity(t *testing.T) {
	ctx := context.Background()
	id := IdentityFromContext(ctx)
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestIdentityFromContext_WithIdentity(t *testing.T) {
	ctx := context.Background()
	expected := &Identity{
		Subject: "user:test",
		Groups:  []string{"admins"},
		Extra:   map[string][]string{"claim": {"value"}},
	}

	ctx = ContextWithIdentity(ctx, expected)
	got := IdentityFromContext(ctx)

	if got == nil {
		t.Fatal("Expected identity, got nil")
	}
	if got.Subject != expected.Subject {
		t.Errorf("Subject: expected %q, got %q", expected.Subject, got.Subject)
	}
	if len(got.Groups) != len(expected.Groups) {
		t.Errorf("Groups: expected %v, got %v", expected.Groups, got.Groups)
	}
}

func TestAuthenticatorFunc(t *testing.T) {
	expectedIdentity := &Identity{Subject: "test"}

	fn := AuthenticatorFunc(func(r *http.Request) (*Identity, bool, error) {
		return expectedIdentity, true, nil
	})

	req, _ := http.NewRequest("GET", "/", nil)
	id, ok, err := fn.AuthenticateRequest(req)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !ok {
		t.Error("Expected authentication to succeed")
	}
	if id != expectedIdentity {
		t.Errorf("Expected %+v, got %+v", expectedIdentity, id)
	}
}
