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
// Caches prices to avoid repeated API calls and supports max price thresholds.
type CostSelector struct {
	baseSelector
	priceCache map[string]cachedPrice
	cacheTTL   time.Duration
	maxPrice   float64 // Maximum acceptable price per hour (0 = no limit)
	clock      func() time.Time
}

type cachedPrice struct {
	price     float64
	fetchedAt time.Time
}

// CostSelectorOption configures the CostSelector.
type CostSelectorOption func(*CostSelector)

// WithPriceCacheTTL sets how long prices are cached (default: 5 minutes).
func WithPriceCacheTTL(ttl time.Duration) CostSelectorOption {
	return func(s *CostSelector) {
		s.cacheTTL = ttl
	}
}

// WithMaxPrice sets the maximum acceptable price per hour.
// Providers exceeding this price will be skipped.
func WithMaxPrice(maxPrice float64) CostSelectorOption {
	return func(s *CostSelector) {
		s.maxPrice = maxPrice
	}
}

// WithCostClock sets a custom clock for testing.
func WithCostClock(clock func() time.Time) CostSelectorOption {
	return func(s *CostSelector) {
		s.clock = clock
	}
}

func NewCostSelector(opts ...CostSelectorOption) *CostSelector {
	s := &CostSelector{
		baseSelector: newBaseSelector(),
		priceCache:   make(map[string]cachedPrice),
		cacheTTL:     5 * time.Minute,
		clock:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *CostSelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock()

	type pricedCandidate struct {
		candidate *ProviderCandidate
		price     float64
	}

	var priced []pricedCandidate

	for i := range candidates {
		if s.isAttempted(candidates[i].Name) {
			continue
		}

		price := s.getPrice(ctx, &candidates[i], now)

		// Skip if price exceeds max threshold
		if s.maxPrice > 0 && price > 0 && price > s.maxPrice {
			continue
		}

		priced = append(priced, pricedCandidate{
			candidate: &candidates[i],
			price:     price,
		})
	}

	if len(priced) == 0 {
		return nil, errors.New("all providers exhausted or exceed max price")
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

// getPrice returns the cached price or fetches it from the provider.
func (s *CostSelector) getPrice(ctx context.Context, c *ProviderCandidate, now time.Time) float64 {
	cacheKey := c.Name + ":" + c.InstanceType

	// Check cache
	if cached, ok := s.priceCache[cacheKey]; ok {
		if now.Sub(cached.fetchedAt) < s.cacheTTL {
			return cached.price
		}
	}

	// Fetch from provider
	price := float64(-1)
	lister, ok := c.Provider.(provider.InstanceTypeLister)
	if ok {
		types, err := lister.ListInstanceTypes(ctx)
		if err == nil {
			for _, t := range types {
				if t.Name == c.InstanceType && t.Available && t.PricePerHr > 0 {
					price = t.PricePerHr
					break
				}
			}
		}
	}

	// Cache the result
	s.priceCache[cacheKey] = cachedPrice{
		price:     price,
		fetchedAt: now,
	}

	return price
}

// GetCachedPrice returns the cached price for a provider/instance type, or -1 if not cached.
func (s *CostSelector) GetCachedPrice(providerName, instanceType string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheKey := providerName + ":" + instanceType
	if cached, ok := s.priceCache[cacheKey]; ok {
		return cached.price
	}
	return -1
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
func NewSelector(strategy string, candidates []ProviderCandidate) (ProviderSelector, error) {
	switch strategy {
	case "", "priority":
		return NewPrioritySelector(), nil
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

