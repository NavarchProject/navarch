// Package clock provides time abstractions for deterministic simulation.
//
// In production, use Real() which wraps the standard time package.
// In tests and simulations, use NewFakeClock() for deterministic time control.
package clock

import "time"

// Clock provides time operations that can be real or simulated.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// Since returns the time elapsed since t.
	Since(t time.Time) time.Duration

	// Until returns the duration until t.
	Until(t time.Time) time.Duration

	// Sleep pauses the current goroutine for at least duration d.
	Sleep(d time.Duration)

	// After waits for the duration to elapse and then sends the current time.
	After(d time.Duration) <-chan time.Time

	// AfterFunc waits for the duration to elapse and then calls f.
	// It returns a Timer that can be used to cancel the call.
	AfterFunc(d time.Duration, f func()) Timer

	// NewTicker returns a new Ticker containing a channel that will send
	// the current time after each tick.
	NewTicker(d time.Duration) Ticker

	// NewTimer creates a new Timer that will send the current time on its
	// channel after at least duration d.
	NewTimer(d time.Duration) Timer
}

// Ticker wraps time.Ticker functionality.
type Ticker interface {
	// C returns the channel on which ticks are delivered.
	C() <-chan time.Time

	// Stop turns off the ticker. After Stop, no more ticks will be sent.
	Stop()

	// Reset stops the ticker and resets it to tick with the new duration.
	Reset(d time.Duration)
}

// Timer wraps time.Timer functionality.
type Timer interface {
	// C returns the channel on which the time is delivered.
	C() <-chan time.Time

	// Stop prevents the Timer from firing. It returns true if the call
	// stops the timer, false if the timer has already expired or been stopped.
	Stop() bool

	// Reset changes the timer to expire after duration d.
	Reset(d time.Duration) bool
}
