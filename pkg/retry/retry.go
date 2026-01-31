// Package retry provides utilities for retrying operations with exponential backoff.
package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
)

// Config configures retry behavior.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including the initial attempt).
	// A value of 0 means retry indefinitely (until context is cancelled).
	MaxAttempts int

	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is the factor by which the delay increases after each retry.
	Multiplier float64

	// Jitter adds randomness to delays to prevent thundering herd.
	// 0.0 means no jitter, 0.1 means +/- 10% of the delay.
	Jitter float64

	// RetryableFunc determines if an error should trigger a retry.
	// If nil, all non-nil errors are considered retryable.
	RetryableFunc func(error) bool

	// Clock is the clock to use for delays. If nil, uses real time.
	Clock clock.Clock
}

// DefaultConfig returns a reasonable default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  4,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// NetworkConfig returns retry configuration optimized for network operations.
func NetworkConfig() Config {
	return Config{
		MaxAttempts:  4,
		InitialDelay: 2 * time.Second,
		MaxDelay:     16 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.2,
	}
}

// Do executes the given function with retry logic.
// It returns the last error if all attempts fail.
func Do(ctx context.Context, cfg Config, fn func(ctx context.Context) error) error {
	if cfg.InitialDelay == 0 {
		cfg.InitialDelay = time.Second
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.Multiplier == 0 {
		cfg.Multiplier = 2.0
	}

	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real()
	}

	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; cfg.MaxAttempts == 0 || attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return errors.Join(ctx.Err(), lastErr)
			}
			return ctx.Err()
		default:
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if cfg.RetryableFunc != nil && !cfg.RetryableFunc(err) {
			return err
		}

		// Don't wait after the last attempt
		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			break
		}

		// Calculate delay with jitter
		actualDelay := delay
		if cfg.Jitter > 0 {
			jitterRange := float64(delay) * cfg.Jitter
			actualDelay = delay + time.Duration(rand.Float64()*2*jitterRange-jitterRange)
		}

		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), lastErr)
		case <-clk.After(actualDelay):
		}

		// Increase delay for next iteration
		delay = time.Duration(math.Min(float64(delay)*cfg.Multiplier, float64(cfg.MaxDelay)))
	}

	return lastErr
}

// DoWithValue executes the given function with retry logic and returns a value.
func DoWithValue[T any](ctx context.Context, cfg Config, fn func(ctx context.Context) (T, error)) (T, error) {
	var result T
	err := Do(ctx, cfg, func(ctx context.Context) error {
		var err error
		result, err = fn(ctx)
		return err
	})
	return result, err
}

// IsTemporary returns true if the error is temporary/transient.
// This can be used as a RetryableFunc.
func IsTemporary(err error) bool {
	type temporary interface {
		Temporary() bool
	}
	var t temporary
	if errors.As(err, &t) {
		return t.Temporary()
	}
	return true // Default to retrying unknown errors
}

// IsTimeout returns true if the error is a timeout error.
func IsTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	var t timeout
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}

// Combine returns a RetryableFunc that returns true if any of the given functions return true.
func Combine(funcs ...func(error) bool) func(error) bool {
	return func(err error) bool {
		for _, f := range funcs {
			if f(err) {
				return true
			}
		}
		return false
	}
}
