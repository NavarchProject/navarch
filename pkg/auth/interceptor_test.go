package auth

import (
	"context"
	"testing"

	"connectrpc.com/connect"
)

func TestTokenInterceptor_AddsHeader(t *testing.T) {
	interceptor := NewTokenInterceptor("test-token")

	called := false
	next := func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		auth := req.Header().Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected 'Bearer test-token', got %q", auth)
		}
		return nil, nil
	}

	wrapped := interceptor.WrapUnary(next)
	req := connect.NewRequest(&struct{}{})
	_, _ = wrapped(context.Background(), req)

	if !called {
		t.Error("Expected next handler to be called")
	}
}

func TestTokenInterceptor_EmptyToken(t *testing.T) {
	interceptor := NewTokenInterceptor("")

	called := false
	next := func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		auth := req.Header().Get("Authorization")
		if auth != "" {
			t.Errorf("Expected no Authorization header, got %q", auth)
		}
		return nil, nil
	}

	wrapped := interceptor.WrapUnary(next)
	req := connect.NewRequest(&struct{}{})
	_, _ = wrapped(context.Background(), req)

	if !called {
		t.Error("Expected next handler to be called")
	}
}

func TestTokenInterceptor_OverwritesExistingHeader(t *testing.T) {
	interceptor := NewTokenInterceptor("new-token")

	next := func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		auth := req.Header().Get("Authorization")
		if auth != "Bearer new-token" {
			t.Errorf("Expected 'Bearer new-token', got %q", auth)
		}
		return nil, nil
	}

	wrapped := interceptor.WrapUnary(next)
	req := connect.NewRequest(&struct{}{})
	req.Header().Set("Authorization", "Bearer old-token")
	_, _ = wrapped(context.Background(), req)
}
