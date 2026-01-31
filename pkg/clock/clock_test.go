package clock

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	c := Real()
	before := time.Now()
	got := c.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestRealClock_Since(t *testing.T) {
	c := Real()
	start := c.Now()
	time.Sleep(10 * time.Millisecond)
	elapsed := c.Since(start)

	if elapsed < 10*time.Millisecond {
		t.Errorf("Since() = %v, want >= 10ms", elapsed)
	}
}

func TestRealClock_Sleep(t *testing.T) {
	c := Real()
	start := time.Now()
	c.Sleep(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 50*time.Millisecond {
		t.Errorf("Sleep() took %v, want >= 50ms", elapsed)
	}
}

func TestRealClock_After(t *testing.T) {
	c := Real()
	start := time.Now()
	<-c.After(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 50*time.Millisecond {
		t.Errorf("After() took %v, want >= 50ms", elapsed)
	}
}

func TestRealClock_NewTicker(t *testing.T) {
	c := Real()
	ticker := c.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var count int
	timeout := time.After(100 * time.Millisecond)

loop:
	for {
		select {
		case <-ticker.C():
			count++
			if count >= 3 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if count < 3 {
		t.Errorf("ticker fired %d times, want >= 3", count)
	}
}

func TestFakeClock_Now(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	if got := c.Now(); !got.Equal(start) {
		t.Errorf("Now() = %v, want %v", got, start)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	c.Advance(5 * time.Minute)

	want := start.Add(5 * time.Minute)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("Now() = %v, want %v", got, want)
	}
}

func TestFakeClock_AdvanceTo(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	target := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	c.AdvanceTo(target)

	if got := c.Now(); !got.Equal(target) {
		t.Errorf("Now() = %v, want %v", got, target)
	}
}

func TestFakeClock_AdvanceTo_Backwards(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	// Try to go backwards
	earlier := time.Date(2024, 1, 1, 6, 0, 0, 0, time.UTC)
	c.AdvanceTo(earlier)

	// Time should not have changed
	if got := c.Now(); !got.Equal(start) {
		t.Errorf("Now() = %v, want %v (no backwards)", got, start)
	}
}

func TestFakeClock_Since(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	c.Advance(5 * time.Minute)

	if got := c.Since(start); got != 5*time.Minute {
		t.Errorf("Since() = %v, want 5m", got)
	}
}

func TestFakeClock_Until(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	future := start.Add(10 * time.Minute)
	if got := c.Until(future); got != 10*time.Minute {
		t.Errorf("Until() = %v, want 10m", got)
	}
}

func TestFakeClock_Sleep(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	done := make(chan struct{})
	go func() {
		c.Sleep(5 * time.Minute)
		close(done)
	}()

	// Wait for the goroutine to start waiting
	c.BlockUntilWaiters(1)

	// Advance time
	c.Advance(5 * time.Minute)

	// Should complete
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Sleep() did not return after Advance()")
	}
}

func TestFakeClock_Sleep_Zero(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	// Should return immediately
	done := make(chan struct{})
	go func() {
		c.Sleep(0)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Sleep(0) should return immediately")
	}
}

func TestFakeClock_After(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	ch := c.After(5 * time.Minute)

	// Advance partially
	c.Advance(3 * time.Minute)

	select {
	case <-ch:
		t.Error("After() fired too early")
	default:
		// Good, not fired yet
	}

	// Advance past the deadline
	c.Advance(3 * time.Minute)

	select {
	case got := <-ch:
		want := start.Add(5 * time.Minute)
		if !got.Equal(want) {
			t.Errorf("After() sent %v, want %v", got, want)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("After() did not fire")
	}
}

func TestFakeClock_After_Zero(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	ch := c.After(0)

	select {
	case got := <-ch:
		if !got.Equal(start) {
			t.Errorf("After(0) sent %v, want %v", got, start)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("After(0) should fire immediately")
	}
}

func TestFakeClock_After_Ordering(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	// Schedule in reverse order
	ch3 := c.After(3 * time.Minute)
	ch1 := c.After(1 * time.Minute)
	ch2 := c.After(2 * time.Minute)

	// Advance to each deadline and verify they fire in order.
	// This is deterministic since we check one at a time.
	c.Advance(1 * time.Minute)

	select {
	case <-ch1:
		// Good - ch1 should fire first
	case <-ch2:
		t.Error("ch2 fired before ch1")
	case <-ch3:
		t.Error("ch3 fired before ch1")
	default:
		t.Error("ch1 did not fire at 1 minute")
	}

	c.Advance(1 * time.Minute) // now at 2 minutes

	select {
	case <-ch2:
		// Good - ch2 should fire second
	case <-ch3:
		t.Error("ch3 fired before ch2")
	default:
		t.Error("ch2 did not fire at 2 minutes")
	}

	c.Advance(1 * time.Minute) // now at 3 minutes

	select {
	case <-ch3:
		// Good - ch3 should fire last
	default:
		t.Error("ch3 did not fire at 3 minutes")
	}
}

func TestFakeClock_AfterFunc(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	var called atomic.Bool
	c.AfterFunc(5*time.Minute, func() {
		called.Store(true)
	})

	c.Advance(3 * time.Minute)
	time.Sleep(10 * time.Millisecond)
	if called.Load() {
		t.Error("AfterFunc fired too early")
	}

	c.Advance(3 * time.Minute)
	time.Sleep(10 * time.Millisecond)
	if !called.Load() {
		t.Error("AfterFunc did not fire")
	}
}

func TestFakeClock_NewTimer(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	timer := c.NewTimer(5 * time.Minute)

	c.Advance(3 * time.Minute)
	select {
	case <-timer.C():
		t.Error("Timer fired too early")
	default:
	}

	c.Advance(3 * time.Minute)
	select {
	case got := <-timer.C():
		want := start.Add(5 * time.Minute)
		if !got.Equal(want) {
			t.Errorf("Timer sent %v, want %v", got, want)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timer did not fire")
	}
}

func TestFakeClock_NewTimer_Stop(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	timer := c.NewTimer(5 * time.Minute)

	if !timer.Stop() {
		t.Error("Stop() returned false on active timer")
	}

	c.Advance(10 * time.Minute)

	select {
	case <-timer.C():
		t.Error("Stopped timer fired")
	default:
		// Good
	}
}

func TestFakeClock_NewTimer_Reset(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	timer := c.NewTimer(5 * time.Minute)

	c.Advance(3 * time.Minute)

	// Reset to 10 more minutes from now
	timer.Reset(10 * time.Minute)

	c.Advance(5 * time.Minute)
	select {
	case <-timer.C():
		t.Error("Reset timer fired too early")
	default:
	}

	c.Advance(6 * time.Minute)
	select {
	case <-timer.C():
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Reset timer did not fire")
	}
}

func TestFakeClock_NewTicker(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	ticker := c.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var ticks []time.Time

	// Should tick at 1m, 2m, 3m
	for i := 0; i < 3; i++ {
		c.Advance(1 * time.Minute)
		select {
		case got := <-ticker.C():
			ticks = append(ticks, got)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("tick %d did not fire", i+1)
		}
	}

	if len(ticks) != 3 {
		t.Fatalf("got %d ticks, want 3", len(ticks))
	}

	for i, tick := range ticks {
		want := start.Add(time.Duration(i+1) * time.Minute)
		if !tick.Equal(want) {
			t.Errorf("tick %d = %v, want %v", i, tick, want)
		}
	}
}

func TestFakeClock_NewTicker_Stop(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	ticker := c.NewTicker(1 * time.Minute)

	c.Advance(1 * time.Minute)
	<-ticker.C()

	ticker.Stop()

	c.Advance(5 * time.Minute)

	select {
	case <-ticker.C():
		t.Error("Stopped ticker fired")
	default:
		// Good
	}
}

func TestFakeClock_NewTicker_Reset(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	ticker := c.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// First tick at 1m
	c.Advance(1 * time.Minute)
	<-ticker.C()

	// Reset to 5 minute interval
	ticker.Reset(5 * time.Minute)

	// Advance 3 minutes - should not tick
	c.Advance(3 * time.Minute)
	select {
	case <-ticker.C():
		t.Error("Reset ticker fired too early")
	default:
	}

	// Advance 3 more minutes - should tick
	c.Advance(3 * time.Minute)
	select {
	case <-ticker.C():
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Reset ticker did not fire")
	}
}

func TestFakeClock_FIFO_SameTime(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	// Schedule three events at the same time
	ch1 := c.After(1 * time.Minute)
	ch2 := c.After(1 * time.Minute)
	ch3 := c.After(1 * time.Minute)

	// Verify internal ordering by checking PendingTimers count
	// and that all fire at the same advancement
	if got := c.PendingTimers(); got != 3 {
		t.Errorf("PendingTimers() = %d, want 3", got)
	}

	c.Advance(1 * time.Minute)

	// All three should fire - verify by draining channels
	for i, ch := range []<-chan time.Time{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			want := start.Add(1 * time.Minute)
			if !got.Equal(want) {
				t.Errorf("ch%d sent %v, want %v", i+1, got, want)
			}
		default:
			t.Errorf("ch%d did not fire", i+1)
		}
	}

	if got := c.PendingTimers(); got != 0 {
		t.Errorf("after advance, PendingTimers() = %d, want 0", got)
	}
}

func TestFakeClock_AutoAdvance(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClockAuto(start)
	defer c.Stop()

	done := make(chan time.Time)

	c.RegisterGoroutine()
	go func() {
		defer c.UnregisterGoroutine()
		c.Sleep(5 * time.Minute)
		done <- c.Now()
	}()

	select {
	case got := <-done:
		want := start.Add(5 * time.Minute)
		if !got.Equal(want) {
			t.Errorf("completed at %v, want %v", got, want)
		}
	case <-time.After(1 * time.Second):
		t.Error("auto-advance did not complete")
	}
}

func TestFakeClock_AutoAdvance_MultipleGoroutines(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClockAuto(start)
	defer c.Stop()

	var results []time.Duration
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Start 3 goroutines with different sleep times
	for _, d := range []time.Duration{1 * time.Minute, 3 * time.Minute, 2 * time.Minute} {
		wg.Add(1)
		d := d
		c.RegisterGoroutine()
		go func() {
			defer wg.Done()
			defer c.UnregisterGoroutine()
			c.Sleep(d)
			mu.Lock()
			results = append(results, c.Since(start))
			mu.Unlock()
		}()
	}

	// Wait for all to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("auto-advance did not complete all goroutines")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Results should be 1m, 2m, 3m (sorted by completion time)
	if results[0] != 1*time.Minute || results[1] != 2*time.Minute || results[2] != 3*time.Minute {
		t.Errorf("results = %v, want [1m 2m 3m]", results)
	}
}

func TestFakeClock_BlockUntilWaiters(t *testing.T) {
	c := NewFakeClock(time.Now())

	go func() {
		c.Sleep(1 * time.Hour)
	}()
	go func() {
		c.Sleep(2 * time.Hour)
	}()

	// Should block until both goroutines are waiting
	c.BlockUntilWaiters(2)

	if got := c.WaiterCount(); got < 2 {
		t.Errorf("WaiterCount() = %d, want >= 2", got)
	}
}

func TestFakeClock_PendingTimers(t *testing.T) {
	c := NewFakeClock(time.Now())

	if got := c.PendingTimers(); got != 0 {
		t.Errorf("PendingTimers() = %d, want 0", got)
	}

	c.After(1 * time.Hour)
	c.After(2 * time.Hour)
	c.NewTimer(3 * time.Hour)

	if got := c.PendingTimers(); got != 3 {
		t.Errorf("PendingTimers() = %d, want 3", got)
	}

	c.Advance(1 * time.Hour)

	if got := c.PendingTimers(); got != 2 {
		t.Errorf("after advance, PendingTimers() = %d, want 2", got)
	}
}

func TestFakeClock_WaiterCount_NeverNegative(t *testing.T) {
	// This test verifies that concurrent timer creation and firing
	// doesn't cause the waiting counter to go negative due to race conditions.
	// The fix ensures waiting.Add(1) happens before mutex unlock.
	c := NewFakeClock(time.Now())

	const iterations = 1000
	const goroutines = 10

	var wg sync.WaitGroup
	var negativeCount atomic.Int32

	// Monitor goroutine that checks for negative waiter counts
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				if count := c.WaiterCount(); count < 0 {
					negativeCount.Add(1)
				}
				time.Sleep(time.Microsecond)
			}
		}
	}()

	// Create timers and advance concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Create a timer
				timer := c.NewTimer(time.Millisecond)
				// Immediately advance past it
				c.Advance(2 * time.Millisecond)
				// Drain the channel
				select {
				case <-timer.C():
				default:
				}
			}
		}()
	}

	// Also test After() concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ch := c.After(time.Millisecond)
				c.Advance(2 * time.Millisecond)
				select {
				case <-ch:
				default:
				}
			}
		}()
	}

	wg.Wait()
	close(done)

	if got := negativeCount.Load(); got > 0 {
		t.Errorf("WaiterCount() was negative %d times, want 0", got)
	}
}
