package pool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

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

