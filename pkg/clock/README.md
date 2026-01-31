# Clock package

This package provides time abstractions for deterministic simulation and testing.

## Overview

The clock package decouples code from the real `time` package, enabling:

- Deterministic tests that do not depend on wall-clock timing.
- Time-accelerated simulations that run faster than real time.
- Reproducible scenarios where time advances on demand.

## Clock interface

The `Clock` interface mirrors the most commonly used `time` package functions:

```go
type Clock interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    Until(t time.Time) time.Duration
    Sleep(d time.Duration)
    After(d time.Duration) <-chan time.Time
    AfterFunc(d time.Duration, f func()) Timer
    NewTicker(d time.Duration) Ticker
    NewTimer(d time.Duration) Timer
}
```

## Implementations

### RealClock

`Real()` returns a clock that delegates to the standard `time` package. Use this in production.

```go
clk := clock.Real()
now := clk.Now()              // time.Now()
ticker := clk.NewTicker(1*time.Second)  // time.NewTicker(...)
```

### FakeClock

`NewFakeClock(start)` returns a clock where time only advances when you call `Advance()`. Use this in tests for deterministic timing.

```go
start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
clk := clock.NewFakeClock(start)

// Time does not advance automatically
fmt.Println(clk.Now())  // 2024-01-01 00:00:00

// Advance time explicitly
clk.Advance(5 * time.Minute)
fmt.Println(clk.Now())  // 2024-01-01 00:05:00
```

Features:

- `Advance(d)` moves time forward, firing any pending timers or tickers.
- `AdvanceTo(t)` moves time to a specific point.
- `BlockUntilWaiters(n)` blocks until n goroutines are waiting on the clock.
- `WaiterCount()` returns how many goroutines are waiting.
- `PendingTimers()` returns how many timers are scheduled.

### FakeClock with auto-advance

`NewFakeClockAuto(start)` creates a clock that automatically advances when all registered goroutines are blocked waiting on the clock. This is useful for simulations.

```go
clk := clock.NewFakeClockAuto(time.Now())
defer clk.Stop()

clk.RegisterGoroutine()
go func() {
    defer clk.UnregisterGoroutine()
    clk.Sleep(1 * time.Hour)  // Clock auto-advances to fire this
    fmt.Println("woke up")
}()
```

## Usage pattern

Components that use time should accept a `Clock` in their configuration:

```go
type Config struct {
    Clock clock.Clock  // If nil, uses real time
}

func New(cfg Config) *MyComponent {
    clk := cfg.Clock
    if clk == nil {
        clk = clock.Real()
    }
    return &MyComponent{clock: clk}
}

func (c *MyComponent) Run(ctx context.Context) {
    ticker := c.clock.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C():
            c.doWork()
        }
    }
}
```

In tests, inject a FakeClock to control time:

```go
func TestMyComponent(t *testing.T) {
    clk := clock.NewFakeClock(time.Now())
    comp := New(Config{Clock: clk})

    go comp.Run(ctx)

    // Advance time to trigger ticker
    clk.Advance(30 * time.Second)

    // Verify work was done
    // ...
}
```

## Thread safety

FakeClock is safe for concurrent use. Multiple goroutines can call `Now()`, `Sleep()`, `After()`, etc. concurrently while another goroutine calls `Advance()`.

The clock releases its lock before firing callbacks to avoid deadlock when callbacks need to reschedule timers.

## Testing

Run tests with:

```bash
go test ./pkg/clock/... -v
```

The test suite covers:

- RealClock delegation to the time package.
- FakeClock manual advancement.
- Timer and ticker behavior.
- Concurrent access patterns.
- Auto-advance mode.
