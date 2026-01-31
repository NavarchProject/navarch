package clock

import "time"

// realClock implements Clock using the standard time package.
type realClock struct{}

// Real returns a Clock that uses the standard time package.
// This is the default for production use.
func Real() Clock {
	return realClock{}
}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (realClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

func (realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func (realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return &realTimer{t: time.AfterFunc(d, f)}
}

func (realClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

func (realClock) NewTimer(d time.Duration) Timer {
	return &realTimer{t: time.NewTimer(d)}
}

// realTicker wraps time.Ticker.
type realTicker struct {
	t *time.Ticker
}

func (r *realTicker) C() <-chan time.Time {
	return r.t.C
}

func (r *realTicker) Stop() {
	r.t.Stop()
}

func (r *realTicker) Reset(d time.Duration) {
	r.t.Reset(d)
}

// realTimer wraps time.Timer.
type realTimer struct {
	t *time.Timer
}

func (r *realTimer) C() <-chan time.Time {
	return r.t.C
}

func (r *realTimer) Stop() bool {
	return r.t.Stop()
}

func (r *realTimer) Reset(d time.Duration) bool {
	return r.t.Reset(d)
}
