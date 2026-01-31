package clock

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"
)

// FakeClock is a deterministic clock for simulation and testing.
//
// Time only advances when Advance() is called manually, or when auto-advance
// is enabled and all registered goroutines are blocked waiting on the clock.
//
// For simulation use, create with NewFakeClockAuto() and register goroutines
// with RegisterGoroutine()/UnregisterGoroutine(). Time will automatically
// advance to the next scheduled event when all goroutines are waiting.
//
// For simple tests, create with NewFakeClock() and call Advance() manually.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters waitHeap
	nextID  uint64

	// Goroutine tracking for auto-advance mode.
	// active counts goroutines that are running (not blocked on clock).
	// waiting counts goroutines blocked on After/Sleep/Ticker.
	active  atomic.Int64
	waiting atomic.Int64

	// Auto-advance control
	autoAdvance bool
	advanceCh   chan struct{}
	stopCh      chan struct{}
	stopped     atomic.Bool
}

// NewFakeClock creates a FakeClock starting at the given time.
// Time only advances when Advance() is called explicitly.
// Use this for simple unit tests.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{
		now:       start,
		advanceCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}
}

// NewFakeClockAuto creates a FakeClock that automatically advances time
// when all registered goroutines are blocked waiting on the clock.
//
// You must call RegisterGoroutine() when starting goroutines that use
// the clock, and UnregisterGoroutine() when they exit. The clock will
// advance to the next scheduled event when:
//   - All registered goroutines are blocked waiting on clock operations
//   - There is at least one pending timer/ticker
//
// Call Stop() when done to clean up the auto-advance goroutine.
func NewFakeClockAuto(start time.Time) *FakeClock {
	c := &FakeClock{
		now:         start,
		autoAdvance: true,
		advanceCh:   make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
	go c.autoAdvanceLoop()
	return c
}

// RegisterGoroutine marks a goroutine as active.
// Call this when starting a goroutine that will use clock operations.
func (c *FakeClock) RegisterGoroutine() {
	c.active.Add(1)
}

// UnregisterGoroutine marks a goroutine as finished.
// Call this (usually via defer) when a goroutine exits.
func (c *FakeClock) UnregisterGoroutine() {
	c.active.Add(-1)
	c.signalAdvance()
}

// Stop stops the auto-advance loop. Call this when done with the clock.
func (c *FakeClock) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		close(c.stopCh)
	}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Since returns the duration since t.
func (c *FakeClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Until returns the duration until t.
func (c *FakeClock) Until(t time.Time) time.Duration {
	return t.Sub(c.Now())
}

// Sleep blocks until the clock advances past the wake time.
func (c *FakeClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	<-c.After(d)
}

// After returns a channel that receives when d has elapsed.
func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)

	c.mu.Lock()
	if d <= 0 {
		ch <- c.now
		c.mu.Unlock()
		return ch
	}

	c.addWaiter(c.now.Add(d), ch, nil)
	c.mu.Unlock()

	c.waiting.Add(1)
	c.signalAdvance()

	return ch
}

// AfterFunc waits for the duration to elapse and then calls f.
func (c *FakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	ft := &fakeTimer{
		clock:    c,
		deadline: c.now.Add(d),
		fn:       f,
		ch:       make(chan time.Time, 1),
	}

	if d <= 0 {
		go f()
		return ft
	}

	ft.id = c.addWaiter(ft.deadline, nil, ft.fire)
	return ft
}

// NewTicker returns a new Ticker that ticks every d.
func (c *FakeClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("non-positive interval for NewTicker")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	ft := &fakeTicker{
		clock:    c,
		interval: d,
		ch:       make(chan time.Time, 1),
		stopCh:   make(chan struct{}),
	}

	ft.nextTick = c.now.Add(d)
	ft.id = c.addWaiter(ft.nextTick, nil, ft.tick)

	return ft
}

// NewTimer creates a new Timer that fires after d.
func (c *FakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()

	ft := &fakeTimer{
		clock:    c,
		deadline: c.now.Add(d),
		ch:       make(chan time.Time, 1),
	}

	if d <= 0 {
		ft.ch <- c.now
		c.mu.Unlock()
		return ft
	}

	ft.id = c.addWaiter(ft.deadline, ft.ch, nil)
	c.mu.Unlock()

	c.waiting.Add(1)
	c.signalAdvance()

	return ft
}

// Advance moves the clock forward by d, firing any timers that expire.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceTo(c.now.Add(d))
}

// AdvanceTo moves the clock to t, firing any timers that expire.
func (c *FakeClock) AdvanceTo(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceTo(t)
}

// BlockUntilWaiters blocks until at least n goroutines are waiting on the clock.
// Useful in tests to ensure goroutines have reached their wait points.
func (c *FakeClock) BlockUntilWaiters(n int) {
	for {
		if int(c.waiting.Load()) >= n {
			return
		}
		// Yield to let other goroutines run
		time.Sleep(time.Microsecond)
	}
}

// WaiterCount returns the number of goroutines waiting on the clock.
func (c *FakeClock) WaiterCount() int {
	return int(c.waiting.Load())
}

// PendingTimers returns the number of pending timers/tickers.
func (c *FakeClock) PendingTimers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waiters.Len()
}

// firedWaiter holds information about a waiter that needs to be fired.
type firedWaiter struct {
	ch       chan time.Time
	fn       func()
	deadline time.Time
}

// advanceTo moves time forward to t, waking waiters as needed.
//
// IMPORTANT: Caller must hold c.mu. This method temporarily releases c.mu
// while firing callbacks to avoid deadlocking when callbacks need to acquire
// the mutex (e.g., ticker rescheduling). The mutex is re-acquired before returning.
func (c *FakeClock) advanceTo(t time.Time) {
	if t.Before(c.now) {
		return
	}

	// Collect waiters to fire while holding the lock.
	var toFire []firedWaiter
	for c.waiters.Len() > 0 && !c.waiters[0].deadline.After(t) {
		w := heap.Pop(&c.waiters).(*waiter)
		c.now = w.deadline
		toFire = append(toFire, firedWaiter{
			ch:       w.ch,
			fn:       w.fn,
			deadline: w.deadline,
		})
	}
	c.now = t

	// Release lock before firing to avoid deadlock with callbacks
	// that need to acquire the lock (e.g., ticker rescheduling).
	c.mu.Unlock()

	// Fire all collected waiters.
	for _, w := range toFire {
		if w.ch != nil {
			select {
			case w.ch <- w.deadline:
				c.waiting.Add(-1)
			default:
			}
		}
		if w.fn != nil {
			w.fn()
		}
	}

	// Re-acquire lock for caller.
	c.mu.Lock()
}

// addWaiter adds a waiter to the heap. Caller must hold c.mu.
func (c *FakeClock) addWaiter(deadline time.Time, ch chan time.Time, fn func()) uint64 {
	c.nextID++
	heap.Push(&c.waiters, &waiter{
		deadline: deadline,
		ch:       ch,
		fn:       fn,
		id:       c.nextID,
	})
	return c.nextID
}

// removeWaiter removes a waiter by ID. Caller must hold c.mu.
func (c *FakeClock) removeWaiter(id uint64) bool {
	for i, w := range c.waiters {
		if w.id == id {
			heap.Remove(&c.waiters, i)
			return true
		}
	}
	return false
}

// signalAdvance signals the auto-advance loop to check if it can advance.
func (c *FakeClock) signalAdvance() {
	if c.autoAdvance {
		select {
		case c.advanceCh <- struct{}{}:
		default:
		}
	}
}

// autoAdvanceLoop runs in auto-advance mode, advancing time when possible.
func (c *FakeClock) autoAdvanceLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		case <-c.advanceCh:
			c.tryAutoAdvance()
		}
	}
}

// tryAutoAdvance advances time if all goroutines are waiting.
func (c *FakeClock) tryAutoAdvance() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only advance if all registered goroutines are waiting and there are waiters
	active := c.active.Load()
	waiting := c.waiting.Load()

	if active > 0 && waiting >= active && c.waiters.Len() > 0 {
		// Advance to the next waiter's deadline
		next := c.waiters[0].deadline
		c.advanceTo(next)
		// Signal again in case there are more waiters to process
		c.signalAdvance()
	}
}

// waiter represents something waiting for a specific time.
type waiter struct {
	deadline time.Time
	ch       chan time.Time // Channel to send time on (may be nil)
	fn       func()         // Function to call (may be nil)
	id       uint64         // Unique ID for stable ordering and removal
	index    int            // Index in heap
}

// waitHeap is a min-heap of waiters ordered by deadline, then ID.
type waitHeap []*waiter

func (h waitHeap) Len() int { return len(h) }

func (h waitHeap) Less(i, j int) bool {
	if h[i].deadline.Equal(h[j].deadline) {
		return h[i].id < h[j].id // FIFO for same deadline
	}
	return h[i].deadline.Before(h[j].deadline)
}

func (h waitHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *waitHeap) Push(x any) {
	w := x.(*waiter)
	w.index = len(*h)
	*h = append(*h, w)
}

func (h *waitHeap) Pop() any {
	old := *h
	n := len(old)
	w := old[n-1]
	old[n-1] = nil
	w.index = -1
	*h = old[0 : n-1]
	return w
}

// fakeTimer implements Timer for FakeClock.
type fakeTimer struct {
	clock    *FakeClock
	deadline time.Time
	ch       chan time.Time
	fn       func()
	id       uint64
	mu       sync.Mutex
	stopped  bool
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return false
	}
	t.stopped = true

	t.clock.mu.Lock()
	removed := t.clock.removeWaiter(t.id)
	t.clock.mu.Unlock()

	// Decrement waiting counter if we removed a channel-based timer
	// (fn-based timers from AfterFunc don't increment waiting)
	if removed && t.fn == nil {
		t.clock.waiting.Add(-1)
	}

	return removed
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.clock.mu.Lock()
	wasActive := t.clock.removeWaiter(t.id)
	t.deadline = t.clock.now.Add(d)
	if t.fn != nil {
		t.id = t.clock.addWaiter(t.deadline, nil, t.fire)
	} else {
		t.id = t.clock.addWaiter(t.deadline, t.ch, nil)
	}
	t.stopped = false
	t.clock.mu.Unlock()

	return wasActive
}

func (t *fakeTimer) fire() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}

	if t.fn != nil {
		go t.fn()
	}
}

// fakeTicker implements Ticker for FakeClock.
type fakeTicker struct {
	clock    *FakeClock
	interval time.Duration
	nextTick time.Time
	ch       chan time.Time
	stopCh   chan struct{}
	id       uint64
	mu       sync.Mutex
	stopped  bool
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}
	t.stopped = true

	t.clock.mu.Lock()
	t.clock.removeWaiter(t.id)
	t.clock.mu.Unlock()

	close(t.stopCh)
}

func (t *fakeTicker) Reset(d time.Duration) {
	if d <= 0 {
		panic("non-positive interval for Reset")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.clock.mu.Lock()
	t.clock.removeWaiter(t.id)
	t.interval = d
	t.nextTick = t.clock.now.Add(d)
	t.id = t.clock.addWaiter(t.nextTick, nil, t.tick)
	t.stopped = false
	t.clock.mu.Unlock()
}

func (t *fakeTicker) tick() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}

	// Send tick (non-blocking to match time.Ticker behavior)
	select {
	case t.ch <- t.clock.Now():
	default:
	}

	// Schedule next tick
	t.clock.mu.Lock()
	t.nextTick = t.nextTick.Add(t.interval)
	t.id = t.clock.addWaiter(t.nextTick, nil, t.tick)
	t.clock.mu.Unlock()
}
