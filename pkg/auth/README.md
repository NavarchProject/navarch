# auth

Package auth provides pluggable authentication for the Navarch control plane.

For user-facing documentation, see [docs/authentication.md](../../docs/authentication.md).

## Overview

This package implements a pluggable authentication architecture where multiple authenticators can be chained together. The first authenticator to successfully authenticate a request wins.

## Key types

### Authenticator interface

```go
type Authenticator interface {
    AuthenticateRequest(r *http.Request) (*Identity, bool, error)
}
```

Return values:
- `(*Identity, true, nil)` — authentication succeeded.
- `(nil, false, nil)` — no credentials found, try next authenticator.
- `(nil, false, error)` — credentials found but invalid.

### Identity

```go
type Identity struct {
    Subject string              // Primary identifier (aligns with JWT "sub" claim)
    Groups  []string            // Group memberships for authorization
    Extra   map[string][]string // Additional claims
}
```

## Built-in authenticators

### BearerTokenAuthenticator

Validates static bearer tokens. Suitable for service-to-service authentication.

```go
auth := NewBearerTokenAuthenticator(
    "secret-token",           // Expected token
    "service:node-agent",     // Subject for authenticated requests
    []string{"system:nodes"}, // Groups
)
```

### ChainAuthenticator

Tries multiple authenticators in sequence.

```go
chain := NewChainAuthenticator(jwtAuth, bearerAuth, anonymousAuth)
```

## Middleware

The `Middleware` type wraps an `http.Handler` with authentication.

```go
middleware := NewMiddleware(authenticator,
    WithExcludedPaths("/healthz", "/readyz", "/metrics"),
    WithRequireAuth(true),
)
handler := middleware.Wrap(mux)
```

Options:
- `WithExcludedPaths(...)` — paths that bypass authentication.
- `WithRequireAuth(bool)` — whether to reject unauthenticated requests (default: true).
- `WithUnauthorizedHandler(fn)` — custom handler for 401 responses.

## Implementing custom authenticators

To add JWT, OIDC, mTLS, or other authentication methods, implement the `Authenticator` interface.

Example JWT authenticator skeleton:

```go
type JWTAuthenticator struct {
    keyFunc jwt.Keyfunc
    issuer  string
}

func (a *JWTAuthenticator) AuthenticateRequest(r *http.Request) (*auth.Identity, bool, error) {
    header := r.Header.Get("Authorization")
    if header == "" || !strings.HasPrefix(header, "Bearer ") {
        return nil, false, nil // No credentials, try next authenticator
    }

    tokenString := strings.TrimPrefix(header, "Bearer ")
    token, err := jwt.Parse(tokenString, a.keyFunc)
    if err != nil {
        return nil, false, fmt.Errorf("invalid token: %w", err)
    }

    claims := token.Claims.(jwt.MapClaims)
    return &auth.Identity{
        Subject: claims["sub"].(string),
        Groups:  extractGroups(claims),
    }, true, nil
}
```

## Security considerations

- Use `crypto/subtle.ConstantTimeCompare` for secret comparison to prevent timing attacks.
- Return generic error messages to avoid leaking credential details.
- Copy slices and maps to prevent mutation between requests.

## Client-side authentication

The `TokenInterceptor` adds authentication headers to outgoing Connect RPC requests.

```go
interceptor := NewTokenInterceptor("secret-token")
client := protoconnect.NewControlPlaneServiceClient(
    http.DefaultClient,
    serverURL,
    connect.WithInterceptors(interceptor),
)
```
