package auth

import (
	"net/http"
	"testing"
)

func TestBearerTokenAuthenticator_ValidToken(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret-token", "service:test", []string{"system:services"})

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")

	id, ok, err := auth.AuthenticateRequest(req)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !ok {
		t.Error("Expected authentication to succeed")
	}
	if id == nil {
		t.Fatal("Expected identity, got nil")
	}
	if id.Subject != "service:test" {
		t.Errorf("Subject: expected %q, got %q", "service:test", id.Subject)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "system:services" {
		t.Errorf("Groups: expected [system:services], got %v", id.Groups)
	}
}

func TestBearerTokenAuthenticator_InvalidToken(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret-token", "service:test", nil)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	id, ok, err := auth.AuthenticateRequest(req)

	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken, got %v", err)
	}
	if ok {
		t.Error("Expected authentication to fail")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestBearerTokenAuthenticator_NoCredentials(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret-token", "service:test", nil)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	// No Authorization header

	id, ok, err := auth.AuthenticateRequest(req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if ok {
		t.Error("Expected ok=false when no credentials provided")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestBearerTokenAuthenticator_MalformedHeader_NoBearer(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret-token", "service:test", nil)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	id, ok, err := auth.AuthenticateRequest(req)

	if err != ErrMalformedAuthHeader {
		t.Errorf("Expected ErrMalformedAuthHeader, got %v", err)
	}
	if ok {
		t.Error("Expected authentication to fail")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestBearerTokenAuthenticator_MalformedHeader_EmptyToken(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret-token", "service:test", nil)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	id, ok, err := auth.AuthenticateRequest(req)

	if err != ErrMalformedAuthHeader {
		t.Errorf("Expected ErrMalformedAuthHeader, got %v", err)
	}
	if ok {
		t.Error("Expected authentication to fail")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestBearerTokenAuthenticator_NoTokenConfigured(t *testing.T) {
	// Empty token = authentication disabled
	auth := NewBearerTokenAuthenticator("", "service:test", nil)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer any-token")

	id, ok, err := auth.AuthenticateRequest(req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if ok {
		t.Error("Expected ok=false when no token configured")
	}
	if id != nil {
		t.Errorf("Expected nil identity, got %+v", id)
	}
}

func TestBearerTokenAuthenticator_IdentityIsolation(t *testing.T) {
	groups := []string{"group1", "group2"}
	auth := NewBearerTokenAuthenticator("secret", "service:test", groups)

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret")

	id1, _, _ := auth.AuthenticateRequest(req)
	id2, _, _ := auth.AuthenticateRequest(req)

	// Modifying one identity should not affect others
	id1.Subject = "modified"
	id1.Groups = append(id1.Groups, "hacked")

	if id2.Subject == "modified" {
		t.Error("Identity mutation leaked between requests")
	}
	if len(id2.Groups) != 2 {
		t.Error("Groups mutation leaked between requests")
	}

	// Original groups slice should be unaffected
	groups[0] = "modified"
	id3, _, _ := auth.AuthenticateRequest(req)
	if id3.Groups[0] == "modified" {
		t.Error("Original groups slice mutation affected authenticator")
	}
}

func TestBearerTokenAuthenticator_TimingAttackResistance(t *testing.T) {
	// This test verifies constant-time comparison is used by checking
	// that similar tokens don't cause different behavior
	// (We can't easily test timing, but we verify the code path works)
	auth := NewBearerTokenAuthenticator("secret-token-12345", "service:test", nil)

	testCases := []struct {
		name  string
		token string
	}{
		{"completely different", "xxxxxx"},
		{"same length different", "secret-token-99999"},
		{"partial match", "secret-token"},
		{"longer", "secret-token-12345-extra"},
		{"shorter", "secret"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)

			_, ok, err := auth.AuthenticateRequest(req)

			if ok {
				t.Error("Expected authentication to fail")
			}
			if err != ErrInvalidToken {
				t.Errorf("Expected ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestBearerTokenAuthenticator_ImplementsAuthenticatorDescriptor(t *testing.T) {
	auth := NewBearerTokenAuthenticator("secret", "service:test", nil)

	// Verify it implements AuthenticatorDescriptor
	desc, ok := interface{}(auth).(AuthenticatorDescriptor)
	if !ok {
		t.Fatal("BearerTokenAuthenticator should implement AuthenticatorDescriptor")
	}

	method := desc.Method()
	if method != "bearer-token" {
		t.Errorf("Method(): expected %q, got %q", "bearer-token", method)
	}
}
