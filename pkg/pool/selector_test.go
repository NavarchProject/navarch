package pool

import (
	"context"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

type mockProviderWithTypes struct {
	mockProvider
	instanceTypes []provider.InstanceType
}

func (m *mockProviderWithTypes) ListInstanceTypes(ctx context.Context) ([]provider.InstanceType, error) {
	return m.instanceTypes, nil
}

func TestPrioritySelector(t *testing.T) {
	selector := NewPrioritySelector()
	candidates := []ProviderCandidate{
		{Name: "high", Priority: 10},
		{Name: "low", Priority: 1},
		{Name: "medium", Priority: 5},
	}

	ctx := context.Background()

	// First selection should return lowest priority
	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "low" {
		t.Errorf("Select() = %s, want low", c.Name)
	}

	// After failure, should return next lowest
	selector.RecordFailure("low", nil)
	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "medium" {
		t.Errorf("Select() = %s, want medium", c.Name)
	}

	// After all fail, should return error
	selector.RecordFailure("medium", nil)
	selector.RecordFailure("high", nil)
	_, err = selector.Select(ctx, candidates)
	if err == nil {
		t.Error("Select() should fail when all providers exhausted")
	}

	// Success resets state
	selector.RecordSuccess("any")
	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() after reset error = %v", err)
	}
	if c.Name != "low" {
		t.Errorf("Select() = %s, want low (reset)", c.Name)
	}
}

func TestRoundRobinSelector(t *testing.T) {
	candidates := []ProviderCandidate{
		{Name: "a", Weight: 1},
		{Name: "b", Weight: 1},
	}
	selector := NewRoundRobinSelector(candidates)
	ctx := context.Background()

	counts := make(map[string]int)
	for i := 0; i < 10; i++ {
		c, err := selector.Select(ctx, candidates)
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		counts[c.Name]++
	}

	// Should be roughly evenly distributed
	if counts["a"] != 5 || counts["b"] != 5 {
		t.Errorf("distribution not even: a=%d, b=%d", counts["a"], counts["b"])
	}
}

func TestRoundRobinSelector_Weighted(t *testing.T) {
	candidates := []ProviderCandidate{
		{Name: "heavy", Weight: 3},
		{Name: "light", Weight: 1},
	}
	selector := NewRoundRobinSelector(candidates)
	ctx := context.Background()

	counts := make(map[string]int)
	for i := 0; i < 8; i++ {
		c, err := selector.Select(ctx, candidates)
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		counts[c.Name]++
	}

	// With weights 3:1, over 8 selections expect 6:2
	if counts["heavy"] != 6 {
		t.Errorf("heavy count = %d, want 6", counts["heavy"])
	}
	if counts["light"] != 2 {
		t.Errorf("light count = %d, want 2", counts["light"])
	}
}

func TestAvailabilitySelector(t *testing.T) {
	availableProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true},
		},
	}
	unavailableProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: false},
		},
	}

	candidates := []ProviderCandidate{
		{Name: "unavailable", Provider: unavailableProvider, InstanceType: "gpu_8x_h100", Priority: 1},
		{Name: "available", Provider: availableProvider, InstanceType: "gpu_8x_h100", Priority: 2},
	}

	selector := NewAvailabilitySelector()
	ctx := context.Background()

	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "available" {
		t.Errorf("Select() = %s, want available", c.Name)
	}
}

func TestCostSelector(t *testing.T) {
	cheapProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 10.0},
		},
	}
	expensiveProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 50.0},
		},
	}

	candidates := []ProviderCandidate{
		{Name: "expensive", Provider: expensiveProvider, InstanceType: "gpu_8x_h100"},
		{Name: "cheap", Provider: cheapProvider, InstanceType: "gpu_8x_h100"},
	}

	selector := NewCostSelector()
	ctx := context.Background()

	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "cheap" {
		t.Errorf("Select() = %s, want cheap", c.Name)
	}
}

func TestCostSelector_MaxPrice(t *testing.T) {
	cheapProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 10.0},
		},
	}
	expensiveProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 50.0},
		},
	}

	candidates := []ProviderCandidate{
		{Name: "expensive", Provider: expensiveProvider, InstanceType: "gpu_8x_h100"},
		{Name: "cheap", Provider: cheapProvider, InstanceType: "gpu_8x_h100"},
	}

	// Max price of $15/hr - should only select cheap provider
	selector := NewCostSelector(WithMaxPrice(15.0))
	ctx := context.Background()

	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "cheap" {
		t.Errorf("Select() = %s, want cheap", c.Name)
	}

	// After cheap fails, expensive exceeds max price
	selector.RecordFailure("cheap", nil)
	_, err = selector.Select(ctx, candidates)
	if err == nil {
		t.Error("Select() should fail when remaining providers exceed max price")
	}
}

func TestCostSelector_PriceCache(t *testing.T) {
	callCount := 0
	provider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 10.0},
		},
	}

	// Wrap to count calls
	countingProvider := &countingProviderWrapper{
		mockProviderWithTypes: provider,
		callCount:             &callCount,
	}

	candidates := []ProviderCandidate{
		{Name: "test", Provider: countingProvider, InstanceType: "gpu_8x_h100"},
	}

	now := time.Now()
	clock := func() time.Time { return now }

	selector := NewCostSelector(
		WithPriceCacheTTL(1*time.Minute),
		WithCostClock(clock),
	)
	ctx := context.Background()

	// First call should query the provider
	_, _ = selector.Select(ctx, candidates)
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Reset for next selection
	selector.RecordSuccess("test")

	// Second call within TTL should use cache
	_, _ = selector.Select(ctx, candidates)
	if callCount != 1 {
		t.Errorf("expected 1 API call (cached), got %d", callCount)
	}

	// After cache expires, should query again
	now = now.Add(2 * time.Minute)
	selector.RecordSuccess("test")
	_, _ = selector.Select(ctx, candidates)
	if callCount != 2 {
		t.Errorf("expected 2 API calls (cache expired), got %d", callCount)
	}
}

func TestCostSelector_NoPriceInfo(t *testing.T) {
	providerWithPrice := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 50.0},
		},
	}
	// Provider without InstanceTypeLister interface
	providerNoPrice := &mockProvider{}

	candidates := []ProviderCandidate{
		{Name: "no-price", Provider: providerNoPrice, InstanceType: "gpu_8x_h100"},
		{Name: "has-price", Provider: providerWithPrice, InstanceType: "gpu_8x_h100"},
	}

	selector := NewCostSelector()
	ctx := context.Background()

	// Should prefer provider with known price
	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "has-price" {
		t.Errorf("Select() = %s, want has-price (known price preferred)", c.Name)
	}

	// After known price fails, falls back to unknown price
	selector.RecordFailure("has-price", nil)
	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "no-price" {
		t.Errorf("Select() = %s, want no-price (fallback)", c.Name)
	}
}

func TestCostSelector_Fallback(t *testing.T) {
	cheapProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 10.0},
		},
	}
	expensiveProvider := &mockProviderWithTypes{
		instanceTypes: []provider.InstanceType{
			{Name: "gpu_8x_h100", Available: true, PricePerHr: 50.0},
		},
	}

	candidates := []ProviderCandidate{
		{Name: "expensive", Provider: expensiveProvider, InstanceType: "gpu_8x_h100"},
		{Name: "cheap", Provider: cheapProvider, InstanceType: "gpu_8x_h100"},
	}

	selector := NewCostSelector()
	ctx := context.Background()

	// First: cheap
	c, _ := selector.Select(ctx, candidates)
	if c.Name != "cheap" {
		t.Errorf("Select() = %s, want cheap", c.Name)
	}

	// After cheap fails: expensive
	selector.RecordFailure("cheap", nil)
	c, _ = selector.Select(ctx, candidates)
	if c.Name != "expensive" {
		t.Errorf("Select() = %s, want expensive (fallback)", c.Name)
	}

	// After both fail: error
	selector.RecordFailure("expensive", nil)
	_, err := selector.Select(ctx, candidates)
	if err == nil {
		t.Error("Select() should fail when all providers exhausted")
	}
}

// countingProviderWrapper wraps a provider to count ListInstanceTypes calls
type countingProviderWrapper struct {
	*mockProviderWithTypes
	callCount *int
}

func (c *countingProviderWrapper) ListInstanceTypes(ctx context.Context) ([]provider.InstanceType, error) {
	*c.callCount++
	return c.mockProviderWithTypes.ListInstanceTypes(ctx)
}

func TestNewSelector(t *testing.T) {
	candidates := []ProviderCandidate{{Name: "test"}}

	tests := []struct {
		strategy string
		wantErr  bool
	}{
		{"", false},
		{"priority", false},
		{"round-robin", false},
		{"availability", false},
		{"cost", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.strategy, func(t *testing.T) {
			_, err := NewSelector(tt.strategy, candidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSelector(%q) error = %v, wantErr %v", tt.strategy, err, tt.wantErr)
			}
		})
	}
}

