package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
	}

	attempts := 0
	err := Do(ctx, cfg, func(ctx context.Context) error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestDo_SuccessOnRetry(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		Multiplier:   1.5,
	}

	attempts := 0
	err := Do(ctx, cfg, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_MaxAttemptsExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
	}

	attempts := 0
	expectedErr := errors.New("persistent error")
	err := Do(ctx, cfg, func(ctx context.Context) error {
		attempts++
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := Config{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
	}

	attempts := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, cfg, func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got %v", err)
	}
}

func TestDo_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	nonRetryableErr := errors.New("permanent error")
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		RetryableFunc: func(err error) bool {
			return !errors.Is(err, nonRetryableErr)
		},
	}

	attempts := 0
	err := Do(ctx, cfg, func(ctx context.Context) error {
		attempts++
		return nonRetryableErr
	})

	if !errors.Is(err, nonRetryableErr) {
		t.Errorf("expected %v, got %v", nonRetryableErr, err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestDoWithValue_Success(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
	}

	attempts := 0
	result, err := DoWithValue(ctx, cfg, func(ctx context.Context) (int, error) {
		attempts++
		if attempts < 2 {
			return 0, errors.New("error")
		}
		return 42, nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxAttempts != 4 {
		t.Errorf("expected MaxAttempts 4, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != time.Second {
		t.Errorf("expected InitialDelay 1s, got %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay 30s, got %v", cfg.MaxDelay)
	}
}

func TestNetworkConfig(t *testing.T) {
	cfg := NetworkConfig()

	if cfg.MaxAttempts != 4 {
		t.Errorf("expected MaxAttempts 4, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 2*time.Second {
		t.Errorf("expected InitialDelay 2s, got %v", cfg.InitialDelay)
	}
}

func TestCombine(t *testing.T) {
	err1 := errors.New("error1")
	err2 := errors.New("error2")
	err3 := errors.New("error3")

	combined := Combine(
		func(err error) bool { return errors.Is(err, err1) },
		func(err error) bool { return errors.Is(err, err2) },
	)

	if !combined(err1) {
		t.Error("expected err1 to be retryable")
	}
	if !combined(err2) {
		t.Error("expected err2 to be retryable")
	}
	if combined(err3) {
		t.Error("expected err3 to not be retryable")
	}
}
