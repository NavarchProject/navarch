package auth

import (
	"context"

	"connectrpc.com/connect"
)

// TokenInterceptor adds an Authorization header to all outgoing requests.
type TokenInterceptor struct {
	token string
}

// NewTokenInterceptor creates a new token interceptor.
// If token is empty, no header is added.
func NewTokenInterceptor(token string) *TokenInterceptor {
	return &TokenInterceptor{token: token}
}

// WrapUnary implements connect.Interceptor.
func (i *TokenInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.token != "" {
			req.Header().Set("Authorization", "Bearer "+i.token)
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor.
func (i *TokenInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements connect.Interceptor.
func (i *TokenInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
