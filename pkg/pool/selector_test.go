package pool

import (
	"context"
	"testing"

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

