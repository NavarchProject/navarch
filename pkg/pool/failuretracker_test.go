package pool

import (
	"testing"
	"time"
)

func TestFailureTracker_BasicBackoff(t *testing.T) {
	ft := NewFailureTracker(
		WithBaseBackoff(1*time.Second),
		WithBackoffFactor(2.0),
		WithMaxBackoff(10*time.Second),
	)

	now := time.Now()

	// First failure: 1s backoff
	backoff := ft.RecordFailure("provider-a", now)
	if backoff != 1*time.Second {
		t.Errorf("first backoff = %v, want 1s", backoff)
	}
	if !ft.IsExcluded("provider-a", now) {
		t.Error("provider-a should be excluded after failure")
	}

	// Second failure: 2s backoff
	now = now.Add(2 * time.Second)
	backoff = ft.RecordFailure("provider-a", now)
	if backoff != 2*time.Second {
		t.Errorf("second backoff = %v, want 2s", backoff)
	}

	// Third failure: 4s backoff
	now = now.Add(3 * time.Second)
	backoff = ft.RecordFailure("provider-a", now)
	if backoff != 4*time.Second {
		t.Errorf("third backoff = %v, want 4s", backoff)
	}

	// Fourth failure: 8s backoff
	now = now.Add(5 * time.Second)
	backoff = ft.RecordFailure("provider-a", now)
	if backoff != 8*time.Second {
		t.Errorf("fourth backoff = %v, want 8s", backoff)
	}

	// Fifth failure: capped at 10s
	now = now.Add(10 * time.Second)
	backoff = ft.RecordFailure("provider-a", now)
	if backoff != 10*time.Second {
		t.Errorf("fifth backoff = %v, want 10s (capped)", backoff)
	}
}

func TestFailureTracker_ExclusionExpiry(t *testing.T) {
	ft := NewFailureTracker(
		WithBaseBackoff(1 * time.Second),
	)

	now := time.Now()
	ft.RecordFailure("provider-a", now)

	// Should be excluded immediately after failure
	if !ft.IsExcluded("provider-a", now) {
		t.Error("should be excluded at t=0")
	}

	// Should be excluded at t=0.5s
	if !ft.IsExcluded("provider-a", now.Add(500*time.Millisecond)) {
		t.Error("should be excluded at t=0.5s")
	}

	// Should NOT be excluded at t=1.1s (backoff expired)
	if ft.IsExcluded("provider-a", now.Add(1100*time.Millisecond)) {
		t.Error("should not be excluded at t=1.1s")
	}
}

func TestFailureTracker_SuccessReset(t *testing.T) {
	ft := NewFailureTracker(
		WithBaseBackoff(1*time.Second),
		WithResetAfter(5*time.Second),
	)

	now := time.Now()

	// Record some failures
	ft.RecordFailure("provider-a", now)
	ft.RecordFailure("provider-a", now.Add(2*time.Second))
	ft.RecordFailure("provider-a", now.Add(4*time.Second))

	if ft.GetFailureCount("provider-a") != 3 {
		t.Errorf("failure count = %d, want 3", ft.GetFailureCount("provider-a"))
	}

	// Success too soon - doesn't reset
	ft.RecordSuccess("provider-a", now.Add(6*time.Second)) // 2s after last failure
	if ft.GetFailureCount("provider-a") != 3 {
		t.Errorf("failure count after early success = %d, want 3", ft.GetFailureCount("provider-a"))
	}

	// Success after resetAfter - resets
	ft.RecordSuccess("provider-a", now.Add(10*time.Second)) // 6s after last failure
	if ft.GetFailureCount("provider-a") != 0 {
		t.Errorf("failure count after reset = %d, want 0", ft.GetFailureCount("provider-a"))
	}
}

func TestFailureTracker_MultipleKeys(t *testing.T) {
	ft := NewFailureTracker(
		WithBaseBackoff(1 * time.Second),
	)

	now := time.Now()

	ft.RecordFailure("provider-a", now)
	ft.RecordFailure("provider-b", now)
	ft.RecordFailure("provider-a:zone-1", now)

	if ft.GetFailureCount("provider-a") != 1 {
		t.Errorf("provider-a count = %d, want 1", ft.GetFailureCount("provider-a"))
	}
	if ft.GetFailureCount("provider-b") != 1 {
		t.Errorf("provider-b count = %d, want 1", ft.GetFailureCount("provider-b"))
	}
	if ft.GetFailureCount("provider-a:zone-1") != 1 {
		t.Errorf("provider-a:zone-1 count = %d, want 1", ft.GetFailureCount("provider-a:zone-1"))
	}
	if ft.GetFailureCount("provider-c") != 0 {
		t.Errorf("provider-c count = %d, want 0", ft.GetFailureCount("provider-c"))
	}
}

func TestFailureTracker_GetStats(t *testing.T) {
	ft := NewFailureTracker(
		WithBaseBackoff(10 * time.Second),
	)

	now := time.Now()
	ft.RecordFailure("provider-a", now)
	ft.RecordFailure("provider-a", now.Add(1*time.Second))
	ft.RecordFailure("provider-b", now.Add(2*time.Second))

	stats := ft.GetStats(now.Add(3 * time.Second))
	if len(stats) != 2 {
		t.Fatalf("stats length = %d, want 2", len(stats))
	}

	// Find provider-a stats
	var providerAStats *FailureStats
	for i := range stats {
		if stats[i].Key == "provider-a" {
			providerAStats = &stats[i]
			break
		}
	}

	if providerAStats == nil {
		t.Fatal("provider-a stats not found")
	}
	if providerAStats.FailureCount != 2 {
		t.Errorf("provider-a failure count = %d, want 2", providerAStats.FailureCount)
	}
	if providerAStats.ExcludedFor <= 0 {
		t.Error("provider-a should still be excluded")
	}
}

func TestFailureTracker_Reset(t *testing.T) {
	ft := NewFailureTracker()

	now := time.Now()
	ft.RecordFailure("provider-a", now)
	ft.RecordFailure("provider-b", now)

	ft.Reset()

	if ft.GetFailureCount("provider-a") != 0 {
		t.Error("provider-a should be reset")
	}
	if ft.GetFailureCount("provider-b") != 0 {
		t.Error("provider-b should be reset")
	}
}
