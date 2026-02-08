# Authentication

Navarch supports pluggable authentication for the control plane API. This document covers the built-in bearer token authentication and how to implement custom authentication methods.

## Bearer token authentication

The control plane includes built-in support for bearer token authentication. When enabled, all API requests (except health endpoints) require a valid token in the `Authorization` header.

### Enabling authentication

To enable authentication on the control plane:

```bash
# Using environment variable
export NAVARCH_AUTH_TOKEN="your-secret-token"
control-plane --config config.yaml

# Using command-line flag
control-plane --auth-token "your-secret-token"
```

The environment variable takes precedence if both are set.

### Exempt endpoints

The following endpoints do not require authentication:

- `/healthz` — Liveness probe for orchestrators.
- `/readyz` — Readiness probe for load balancers.
- `/metrics` — Prometheus metrics endpoint.

### Client configuration

Clients must include the token in the `Authorization` header using the Bearer scheme.

For the CLI:

```bash
export NAVARCH_AUTH_TOKEN="your-secret-token"
navarch list
```

For curl:

```bash
curl -H "Authorization: Bearer your-secret-token" \
  http://localhost:50051/navarch.ControlPlaneService/ListNodes
```

For node agents:

```bash
export NAVARCH_AUTH_TOKEN="your-secret-token"
node-agent --server https://control-plane.example.com
```

### Token generation

Generate a secure token using a cryptographically secure random source:

```bash
# Using openssl
openssl rand -base64 32

# Using /dev/urandom
head -c 32 /dev/urandom | base64
```

Store tokens securely using your cloud provider's secret manager (AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault).

## Custom authentication

For authentication methods beyond bearer tokens (JWT, OIDC, mTLS), implement the `Authenticator` interface and rebuild the control plane.

### Authenticator interface

```go
type Authenticator interface {
    AuthenticateRequest(r *http.Request) (*Identity, bool, error)
}
```

Return values:

- `(*Identity, true, nil)` — Authentication succeeded.
- `(nil, false, nil)` — No credentials found; try the next authenticator.
- `(nil, false, error)` — Credentials found but invalid.

### Identity

The `Identity` struct represents an authenticated entity:

```go
type Identity struct {
    Subject string              // Primary identifier (e.g., "user:jane@example.com")
    Groups  []string            // Group memberships for authorization
    Extra   map[string][]string // Additional claims from the auth source
}
```

The `Subject` field aligns with the JWT `sub` claim and X.509 certificate subject.

### Chaining authenticators

Use `ChainAuthenticator` to try multiple authentication methods in sequence:

```go
chain := auth.NewChainAuthenticator(
    jwtAuthenticator,     // Try JWT first
    bearerAuthenticator,  // Fall back to static token
)

middleware := auth.NewMiddleware(chain,
    auth.WithExcludedPaths("/healthz", "/readyz", "/metrics"),
)
```

The first authenticator to return success wins. If an authenticator returns an error (invalid credentials), the chain stops and returns that error.

### Example: JWT authenticator

```go
type JWTAuthenticator struct {
    keyFunc jwt.Keyfunc
    issuer  string
}

func (a *JWTAuthenticator) AuthenticateRequest(r *http.Request) (*auth.Identity, bool, error) {
    header := r.Header.Get("Authorization")
    if !strings.HasPrefix(header, "Bearer ") {
        return nil, false, nil // No JWT, try next authenticator
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

### Security considerations

When implementing custom authenticators:

- Use `crypto/subtle.ConstantTimeCompare` for secret comparison to prevent timing attacks.
- Return generic error messages ("unauthorized") to avoid leaking why authentication failed.
- Copy slices and maps in the returned `Identity` to prevent mutation between requests.
- Validate token expiration, issuer, and audience claims for JWT/OIDC.

## Retrieving identity in handlers

After authentication, the `Identity` is available in the request context:

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    identity := auth.IdentityFromContext(r.Context())
    if identity == nil {
        // Unauthenticated request (only possible if WithRequireAuth(false))
        return
    }

    log.Printf("Request from %s", identity.Subject)
}
```

## What is next

- [Configuration reference](configuration.md) — Server and pool configuration.
- [Deployment guide](deployment.md) — Production deployment with TLS.
