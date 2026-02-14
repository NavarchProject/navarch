package pool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// ProviderSelector chooses which provider to use for provisioning.
type ProviderSelector interface {
	// Select returns the next provider to try. Returns nil when no more providers available.
	Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error)

	// RecordSuccess records a successful provisioning from a provider.
	RecordSuccess(providerName string)

	// RecordFailure records a failed provisioning attempt from a provider.
	RecordFailure(providerName string, err error)
}

// ProviderCandidate represents a provider that could fulfill a provisioning request.
type ProviderCandidate struct {
	Provider     provider.Provider
	Name         string
	Priority     int
	Weight       int
	Regions      []string
	Zones        []string // Availability zones for zone-aware distribution
	InstanceType string
}

// baseSelector provides common functionality for all selectors.
type baseSelector struct {
	attempted map[string]bool
	mu        sync.Mutex
}

func newBaseSelector() baseSelector {
	return baseSelector{
		attempted: make(map[string]bool),
	}
}

func (b *baseSelector) isAttempted(name string) bool {
	return b.attempted[name]
}

func (b *baseSelector) markAttempted(name string) {
	b.attempted[name] = true
}

func (b *baseSelector) resetAttempted() {
	b.attempted = make(map[string]bool)
}

// PrioritySelector selects providers in priority order (lowest first).
// Falls back to next provider on failure.
type PrioritySelector struct {
	baseSelector
}

func NewPrioritySelector() *PrioritySelector {
	return &PrioritySelector{
		baseSelector: newBaseSelector(),
	}
}

func (s *PrioritySelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sorted := make([]ProviderCandidate, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for i := range sorted {
		if !s.isAttempted(sorted[i].Name) {
			return &sorted[i], nil
		}
	}

	return nil, errors.New("all providers exhausted")
}

func (s *PrioritySelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetAttempted()
}

func (s *PrioritySelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markAttempted(providerName)
}

// RoundRobinSelector distributes provisioning evenly across providers.
// Uses weights to bias distribution. Tracks failures to prevent infinite loops.
type RoundRobinSelector struct {
	baseSelector
	counter   uint64
	weights   map[string]int
	providers []string
}

func NewRoundRobinSelector(candidates []ProviderCandidate) *RoundRobinSelector {
	weights := make(map[string]int)
	var providers []string

	for _, c := range candidates {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		weights[c.Name] = w
		for i := 0; i < w; i++ {
			providers = append(providers, c.Name)
		}
	}

	return &RoundRobinSelector{
		baseSelector: newBaseSelector(),
		weights:      weights,
		providers:    providers,
	}
}

func (s *RoundRobinSelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.providers) == 0 {
		return nil, errors.New("no providers configured")
	}

	// Find next unattempted provider using round-robin
	startIdx := atomic.AddUint64(&s.counter, 1) - 1
	for i := 0; i < len(s.providers); i++ {
		idx := (startIdx + uint64(i)) % uint64(len(s.providers))
		name := s.providers[idx]

		if s.isAttempted(name) {
			continue
		}

		for j := range candidates {
			if candidates[j].Name == name {
				return &candidates[j], nil
			}
		}
	}

	return nil, errors.New("all providers exhausted")
}

func (s *RoundRobinSelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetAttempted()
}

func (s *RoundRobinSelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markAttempted(providerName)
}

// AvailabilitySelector queries providers for capacity and selects one with availability.
type AvailabilitySelector struct {
	baseSelector
}

func NewAvailabilitySelector() *AvailabilitySelector {
	return &AvailabilitySelector{
		baseSelector: newBaseSelector(),
	}
}

func (s *AvailabilitySelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sorted := make([]ProviderCandidate, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for i := range sorted {
		if s.isAttempted(sorted[i].Name) {
			continue
		}

		lister, ok := sorted[i].Provider.(provider.InstanceTypeLister)
		if !ok {
			return &sorted[i], nil
		}

		types, err := lister.ListInstanceTypes(ctx)
		if err != nil {
			s.markAttempted(sorted[i].Name)
			continue
		}

		for _, t := range types {
			if t.Name == sorted[i].InstanceType && t.Available {
				return &sorted[i], nil
			}
		}
		s.markAttempted(sorted[i].Name)
	}

	return nil, errors.New("no providers with available capacity")
}

func (s *AvailabilitySelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetAttempted()
}

func (s *AvailabilitySelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markAttempted(providerName)
}

// CostSelector queries providers for pricing and selects the cheapest.
type CostSelector struct {
	baseSelector
}

func NewCostSelector() *CostSelector {
	return &CostSelector{
		baseSelector: newBaseSelector(),
	}
}

func (s *CostSelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type pricedCandidate struct {
		candidate *ProviderCandidate
		price     float64
	}

	var priced []pricedCandidate

	for i := range candidates {
		if s.isAttempted(candidates[i].Name) {
			continue
		}

		price := float64(-1)
		lister, ok := candidates[i].Provider.(provider.InstanceTypeLister)
		if ok {
			types, err := lister.ListInstanceTypes(ctx)
			if err == nil {
				for _, t := range types {
					if t.Name == candidates[i].InstanceType && t.Available && t.PricePerHr > 0 {
						price = t.PricePerHr
						break
					}
				}
			}
		}

		priced = append(priced, pricedCandidate{
			candidate: &candidates[i],
			price:     price,
		})
	}

	if len(priced) == 0 {
		return nil, errors.New("all providers exhausted")
	}

	sort.Slice(priced, func(i, j int) bool {
		if priced[i].price < 0 {
			return false
		}
		if priced[j].price < 0 {
			return true
		}
		return priced[i].price < priced[j].price
	})

	return priced[0].candidate, nil
}

func (s *CostSelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetAttempted()
}

func (s *CostSelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markAttempted(providerName)
}

// NewSelector creates a ProviderSelector based on strategy name.
//
// Strategies:
//   - "priority" (default): Try providers in priority order. Automatically backs off
//     from providers that fail repeatedly to avoid hammering broken endpoints.
//   - "round-robin": Distribute provisioning evenly across providers using weights.
//   - "availability": Query providers for capacity and pick first with availability.
//   - "cost": Query providers for pricing and pick the cheapest option.
func NewSelector(strategy string, candidates []ProviderCandidate) (ProviderSelector, error) {
	switch strategy {
	case "", "priority":
		// Priority selector with smart backoff - providers that fail repeatedly
		// are temporarily skipped to avoid hammering broken endpoints
		return NewFailoverSelector(NewPrioritySelector()), nil
	case "round-robin":
		return NewRoundRobinSelector(candidates), nil
	case "availability":
		return NewAvailabilitySelector(), nil
	case "cost":
		return NewCostSelector(), nil
	default:
		return nil, fmt.Errorf("unknown provider strategy: %s", strategy)
	}
}

// FailoverSelector wraps another selector and adds failure tracking with exponential backoff.
// Providers that fail repeatedly are temporarily excluded from selection.
type FailoverSelector struct {
	inner   ProviderSelector
	tracker *FailureTracker
	clock   func() time.Time
	mu      sync.Mutex
}

// FailoverSelectorOption configures the FailoverSelector.
type FailoverSelectorOption func(*FailoverSelector)

// WithFailureTracker sets a custom failure tracker.
func WithFailureTracker(ft *FailureTracker) FailoverSelectorOption {
	return func(fs *FailoverSelector) {
		fs.tracker = ft
	}
}

// WithClock sets a custom clock for testing.
func WithClock(clock func() time.Time) FailoverSelectorOption {
	return func(fs *FailoverSelector) {
		fs.clock = clock
	}
}

// NewFailoverSelector creates a failover selector that wraps the given selector.
func NewFailoverSelector(inner ProviderSelector, opts ...FailoverSelectorOption) *FailoverSelector {
	fs := &FailoverSelector{
		inner:   inner,
		tracker: NewFailureTracker(),
		clock:   time.Now,
	}
	for _, opt := range opts {
		opt(fs)
	}
	return fs
}

func (fs *FailoverSelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	now := fs.clock()

	// Filter out excluded candidates (those in backoff)
	available := make([]ProviderCandidate, 0, len(candidates))
	for _, c := range candidates {
		key := fs.candidateKey(c)
		if !fs.tracker.IsExcluded(key, now) {
			available = append(available, c)
		}
	}

	// If all providers are in backoff, use original list but don't reset
	// inner tracking - let the inner selector's "all exhausted" error propagate
	if len(available) == 0 {
		available = candidates
	}

	return fs.inner.Select(ctx, available)
}

func (fs *FailoverSelector) RecordSuccess(providerName string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.tracker.RecordSuccess(providerName, fs.clock())
	fs.inner.RecordSuccess(providerName)
}

func (fs *FailoverSelector) RecordFailure(providerName string, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.tracker.RecordFailure(providerName, fs.clock())
	fs.inner.RecordFailure(providerName, err)
}

// RecordZoneFailure records a failure for a specific provider:zone combination.
func (fs *FailoverSelector) RecordZoneFailure(providerName, zone string, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	key := providerName + ":" + zone
	fs.tracker.RecordFailure(key, fs.clock())
	// Also record at provider level
	fs.tracker.RecordFailure(providerName, fs.clock())
	fs.inner.RecordFailure(providerName, err)
}

// RecordZoneSuccess records success for a specific provider:zone combination.
func (fs *FailoverSelector) RecordZoneSuccess(providerName, zone string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	key := providerName + ":" + zone
	fs.tracker.RecordSuccess(key, fs.clock())
	fs.tracker.RecordSuccess(providerName, fs.clock())
	fs.inner.RecordSuccess(providerName)
}

// GetFailureStats returns current failure statistics.
func (fs *FailoverSelector) GetFailureStats() []FailureStats {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.tracker.GetStats(fs.clock())
}

// IsProviderExcluded returns true if a provider is currently in backoff.
func (fs *FailoverSelector) IsProviderExcluded(providerName string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.tracker.IsExcluded(providerName, fs.clock())
}

// IsZoneExcluded returns true if a provider:zone is currently in backoff.
func (fs *FailoverSelector) IsZoneExcluded(providerName, zone string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	key := providerName + ":" + zone
	return fs.tracker.IsExcluded(key, fs.clock())
}

func (fs *FailoverSelector) candidateKey(c ProviderCandidate) string {
	return c.Name
}

// ZoneDistributor helps distribute nodes across zones for high availability.
type ZoneDistributor struct {
	zones     []string
	counts    map[string]int
	nextIndex int
	mu        sync.Mutex
}

// NewZoneDistributor creates a distributor for the given zones.
func NewZoneDistributor(zones []string) *ZoneDistributor {
	return &ZoneDistributor{
		zones:  zones,
		counts: make(map[string]int),
	}
}

// NextZone returns the next zone to use, preferring zones with fewer nodes.
func (zd *ZoneDistributor) NextZone() string {
	zd.mu.Lock()
	defer zd.mu.Unlock()

	if len(zd.zones) == 0 {
		return ""
	}

	// Find zone with minimum count
	minCount := -1
	var minZone string
	for _, z := range zd.zones {
		count := zd.counts[z]
		if minCount < 0 || count < minCount {
			minCount = count
			minZone = z
		}
	}

	return minZone
}

// RecordProvisioned records that a node was provisioned in the given zone.
func (zd *ZoneDistributor) RecordProvisioned(zone string) {
	zd.mu.Lock()
	defer zd.mu.Unlock()
	zd.counts[zone]++
}

// RecordTerminated records that a node was terminated in the given zone.
func (zd *ZoneDistributor) RecordTerminated(zone string) {
	zd.mu.Lock()
	defer zd.mu.Unlock()
	if zd.counts[zone] > 0 {
		zd.counts[zone]--
	}
}

// GetDistribution returns the current node count per zone.
func (zd *ZoneDistributor) GetDistribution() map[string]int {
	zd.mu.Lock()
	defer zd.mu.Unlock()
	result := make(map[string]int, len(zd.counts))
	for k, v := range zd.counts {
		result[k] = v
	}
	return result
}

