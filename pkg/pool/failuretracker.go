package pool

import (
	"sync"
	"time"
)

// FailureTracker tracks provisioning failures per provider/zone with exponential backoff.
// Providers that fail repeatedly are temporarily excluded from selection.
type FailureTracker struct {
	mu       sync.RWMutex
	failures map[string]*failureRecord

	// Configuration
	baseBackoff    time.Duration // Initial backoff after first failure
	maxBackoff     time.Duration // Maximum backoff duration
	backoffFactor  float64       // Multiplier for each consecutive failure
	resetAfter     time.Duration // Reset failure count after this duration of success
}

type failureRecord struct {
	count        int       // Consecutive failure count
	lastFailure  time.Time // When the last failure occurred
	excludeUntil time.Time // Provider excluded until this time
}

// FailureTrackerOption configures the FailureTracker.
type FailureTrackerOption func(*FailureTracker)

// WithBaseBackoff sets the initial backoff duration (default: 30s).
func WithBaseBackoff(d time.Duration) FailureTrackerOption {
	return func(ft *FailureTracker) {
		ft.baseBackoff = d
	}
}

// WithMaxBackoff sets the maximum backoff duration (default: 10m).
func WithMaxBackoff(d time.Duration) FailureTrackerOption {
	return func(ft *FailureTracker) {
		ft.maxBackoff = d
	}
}

// WithBackoffFactor sets the backoff multiplier (default: 2.0).
func WithBackoffFactor(f float64) FailureTrackerOption {
	return func(ft *FailureTracker) {
		ft.backoffFactor = f
	}
}

// WithResetAfter sets how long success must be sustained to reset failures (default: 5m).
func WithResetAfter(d time.Duration) FailureTrackerOption {
	return func(ft *FailureTracker) {
		ft.resetAfter = d
	}
}

// NewFailureTracker creates a new failure tracker with the given options.
func NewFailureTracker(opts ...FailureTrackerOption) *FailureTracker {
	ft := &FailureTracker{
		failures:      make(map[string]*failureRecord),
		baseBackoff:   30 * time.Second,
		maxBackoff:    10 * time.Minute,
		backoffFactor: 2.0,
		resetAfter:    5 * time.Minute,
	}
	for _, opt := range opts {
		opt(ft)
	}
	return ft
}

// RecordFailure records a provisioning failure for the given key (provider or provider:zone).
// Returns the duration until the provider should be retried.
func (ft *FailureTracker) RecordFailure(key string, now time.Time) time.Duration {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	record, ok := ft.failures[key]
	if !ok {
		record = &failureRecord{}
		ft.failures[key] = record
	}

	record.count++
	record.lastFailure = now

	// Calculate backoff with exponential growth
	backoff := ft.baseBackoff
	for i := 1; i < record.count; i++ {
		backoff = time.Duration(float64(backoff) * ft.backoffFactor)
		if backoff > ft.maxBackoff {
			backoff = ft.maxBackoff
			break
		}
	}

	record.excludeUntil = now.Add(backoff)
	return backoff
}

// RecordSuccess records a successful provisioning for the given key.
// If enough time has passed since the last failure, resets the failure count.
func (ft *FailureTracker) RecordSuccess(key string, now time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	record, ok := ft.failures[key]
	if !ok {
		return
	}

	// Reset if enough time has passed since last failure
	if now.Sub(record.lastFailure) >= ft.resetAfter {
		delete(ft.failures, key)
	}
}

// IsExcluded returns true if the key is currently in a backoff period.
func (ft *FailureTracker) IsExcluded(key string, now time.Time) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	record, ok := ft.failures[key]
	if !ok {
		return false
	}

	return now.Before(record.excludeUntil)
}

// GetFailureCount returns the consecutive failure count for a key.
func (ft *FailureTracker) GetFailureCount(key string) int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if record, ok := ft.failures[key]; ok {
		return record.count
	}
	return 0
}

// GetExcludeUntil returns when a key's exclusion period ends.
// Returns zero time if the key is not excluded.
func (ft *FailureTracker) GetExcludeUntil(key string) time.Time {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if record, ok := ft.failures[key]; ok {
		return record.excludeUntil
	}
	return time.Time{}
}

// Reset clears all failure records.
func (ft *FailureTracker) Reset() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.failures = make(map[string]*failureRecord)
}

// Stats returns current failure statistics for monitoring.
type FailureStats struct {
	Key          string
	FailureCount int
	LastFailure  time.Time
	ExcludedFor  time.Duration // Remaining exclusion time (0 if not excluded)
}

// GetStats returns failure statistics for all tracked keys.
func (ft *FailureTracker) GetStats(now time.Time) []FailureStats {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	stats := make([]FailureStats, 0, len(ft.failures))
	for key, record := range ft.failures {
		s := FailureStats{
			Key:          key,
			FailureCount: record.count,
			LastFailure:  record.lastFailure,
		}
		if now.Before(record.excludeUntil) {
			s.ExcludedFor = record.excludeUntil.Sub(now)
		}
		stats = append(stats, s)
	}
	return stats
}
