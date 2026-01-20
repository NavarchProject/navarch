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

// PrioritySelector selects providers in priority order (lowest first).
// Falls back to next provider on failure.
type PrioritySelector struct {
	attempted map[string]bool
	mu        sync.Mutex
}

func NewPrioritySelector() *PrioritySelector {
	return &PrioritySelector{
		attempted: make(map[string]bool),
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
		if !s.attempted[sorted[i].Name] {
			return &sorted[i], nil
		}
	}

	return nil, errors.New("all providers exhausted")
}

func (s *PrioritySelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted = make(map[string]bool)
}

func (s *PrioritySelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted[providerName] = true
}

// RoundRobinSelector distributes provisioning evenly across providers.
// Uses weights to bias distribution.
type RoundRobinSelector struct {
	counter   uint64
	weights   map[string]int
	providers []string
	mu        sync.Mutex
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
		weights:   weights,
		providers: providers,
	}
}

func (s *RoundRobinSelector) Select(ctx context.Context, candidates []ProviderCandidate) (*ProviderCandidate, error) {
	if len(s.providers) == 0 {
		return nil, errors.New("no providers configured")
	}

	idx := atomic.AddUint64(&s.counter, 1) - 1
	name := s.providers[idx%uint64(len(s.providers))]

	for i := range candidates {
		if candidates[i].Name == name {
			return &candidates[i], nil
		}
	}

	if len(candidates) > 0 {
		return &candidates[0], nil
	}

	return nil, errors.New("no matching provider found")
}

func (s *RoundRobinSelector) RecordSuccess(providerName string) {}
func (s *RoundRobinSelector) RecordFailure(providerName string, err error) {}

// AvailabilitySelector queries providers for capacity and selects one with availability.
type AvailabilitySelector struct {
	attempted map[string]bool
	mu        sync.Mutex
}

func NewAvailabilitySelector() *AvailabilitySelector {
	return &AvailabilitySelector{
		attempted: make(map[string]bool),
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
		if s.attempted[sorted[i].Name] {
			continue
		}

		lister, ok := sorted[i].Provider.(provider.InstanceTypeLister)
		if !ok {
			return &sorted[i], nil
		}

		types, err := lister.ListInstanceTypes(ctx)
		if err != nil {
			s.attempted[sorted[i].Name] = true
			continue
		}

		for _, t := range types {
			if t.Name == sorted[i].InstanceType && t.Available {
				return &sorted[i], nil
			}
		}
		s.attempted[sorted[i].Name] = true
	}

	return nil, errors.New("no providers with available capacity")
}

func (s *AvailabilitySelector) RecordSuccess(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted = make(map[string]bool)
}

func (s *AvailabilitySelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted[providerName] = true
}

// CostSelector queries providers for pricing and selects the cheapest.
type CostSelector struct {
	attempted map[string]bool
	mu        sync.Mutex
}

func NewCostSelector() *CostSelector {
	return &CostSelector{
		attempted: make(map[string]bool),
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
		if s.attempted[candidates[i].Name] {
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
	s.attempted = make(map[string]bool)
}

func (s *CostSelector) RecordFailure(providerName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted[providerName] = true
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

