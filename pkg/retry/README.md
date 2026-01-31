# Retry package

This package provides utilities for retrying operations with exponential backoff.

## Overview

Network calls and external service interactions can fail transiently. This package provides a simple retry mechanism with:

- Configurable maximum attempts.
- Exponential backoff with optional jitter.
- Maximum delay cap.
- Custom retryable error detection.
- Context cancellation support.
- Clock injection for deterministic tests.

## Basic usage

```go
import "github.com/NavarchProject/navarch/pkg/retry"

err := retry.Do(ctx, retry.DefaultConfig(), func(ctx context.Context) error {
    return callExternalService(ctx)
})
```

## Configuration

```go
type Config struct {
    MaxAttempts   int           // Maximum attempts (0 = infinite)
    InitialDelay  time.Duration // Delay before first retry
    MaxDelay      time.Duration // Maximum delay between retries
    Multiplier    float64       // Delay multiplier per attempt
    Jitter        float64       // Random jitter (0.1 = +/- 10%)
    RetryableFunc func(error) bool  // Custom retry predicate
    Clock         clock.Clock   // For testing (nil = real time)
}
```

## Preset configurations

```go
// DefaultConfig: 4 attempts, 1s initial, 30s max, 2x multiplier, 10% jitter
cfg := retry.DefaultConfig()

// NetworkConfig: Tuned for network operations
cfg := retry.NetworkConfig()
```

## Custom retry predicate

By default, all non-nil errors trigger a retry. To retry only specific errors:

```go
cfg := retry.Config{
    MaxAttempts:  5,
    InitialDelay: time.Second,
    RetryableFunc: func(err error) bool {
        // Only retry timeout errors
        return errors.Is(err, context.DeadlineExceeded)
    },
}
```

## Combining predicates

Use `Combine` to retry on multiple error types:

```go
cfg.RetryableFunc = retry.Combine(
    func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) },
    func(err error) bool { return errors.Is(err, syscall.ECONNRESET) },
)
```

## Returning values

Use `DoWithValue` when the operation returns a value:

```go
result, err := retry.DoWithValue(ctx, cfg, func(ctx context.Context) (int, error) {
    return fetchCount(ctx)
})
```

## Context cancellation

Retry stops immediately when the context is cancelled. The returned error includes both the context error and the last operation error:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

err := retry.Do(ctx, cfg, func(ctx context.Context) error {
    return slowOperation(ctx)
})

if errors.Is(err, context.DeadlineExceeded) {
    // Context timed out during retry
}
```

## Testing with FakeClock

Inject a FakeClock to test retry logic without real delays:

```go
func TestRetryBackoff(t *testing.T) {
    clk := clock.NewFakeClock(time.Now())
    cfg := retry.Config{
        MaxAttempts:  3,
        InitialDelay: time.Second,
        Clock:        clk,
    }

    attempts := 0
    done := make(chan error)
    go func() {
        done <- retry.Do(ctx, cfg, func(ctx context.Context) error {
            attempts++
            return errors.New("fail")
        })
    }()

    // First attempt runs immediately
    clk.BlockUntilWaiters(1)
    clk.Advance(time.Second)  // Trigger first retry

    clk.BlockUntilWaiters(1)
    clk.Advance(2 * time.Second)  // Trigger second retry (2x backoff)

    err := <-done
    // attempts == 3, err != nil
}
```

## Testing

```bash
go test ./pkg/retry/... -v
```
