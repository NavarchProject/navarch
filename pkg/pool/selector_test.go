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

func TestFailoverSelector_ExcludesFailingProviders(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	tracker := NewFailureTracker(
		WithBaseBackoff(10 * time.Second),
	)

	inner := NewPrioritySelector()
	selector := NewFailoverSelector(inner,
		WithFailureTracker(tracker),
		WithClock(clock),
	)

	candidates := []ProviderCandidate{
		{Name: "primary", Priority: 1},
		{Name: "secondary", Priority: 2},
	}

	ctx := context.Background()

	// First selection: primary (lowest priority)
	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "primary" {
		t.Errorf("Select() = %s, want primary", c.Name)
	}

	// Record failure for primary
	selector.RecordFailure("primary", nil)

	// Next selection: still tries primary first (inner selector handles attempts)
	// But after inner exhausts, we fall back
	inner.RecordFailure("primary", nil)
	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "secondary" {
		t.Errorf("Select() = %s, want secondary", c.Name)
	}

	// Reset inner selector for next round
	inner.RecordSuccess("any")

	// Primary should now be excluded due to backoff
	if !selector.IsProviderExcluded("primary") {
		t.Error("primary should be excluded")
	}

	// Selection should skip primary and return secondary
	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "secondary" {
		t.Errorf("Select() = %s, want secondary (primary excluded)", c.Name)
	}

	// After backoff expires, primary should be available again
	now = now.Add(11 * time.Second)
	inner.RecordSuccess("any") // Reset inner

	if selector.IsProviderExcluded("primary") {
		t.Error("primary should not be excluded after backoff")
	}

	c, err = selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if c.Name != "primary" {
		t.Errorf("Select() = %s, want primary (backoff expired)", c.Name)
	}
}

func TestFailoverSelector_ZoneFailures(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	tracker := NewFailureTracker(
		WithBaseBackoff(10 * time.Second),
	)

	inner := NewPrioritySelector()
	selector := NewFailoverSelector(inner,
		WithFailureTracker(tracker),
		WithClock(clock),
	)

	// Record zone-specific failure
	selector.RecordZoneFailure("provider-a", "us-east-1a", nil)

	if !selector.IsZoneExcluded("provider-a", "us-east-1a") {
		t.Error("zone us-east-1a should be excluded")
	}
	if !selector.IsProviderExcluded("provider-a") {
		t.Error("provider-a should be excluded (zone failure propagates)")
	}

	// Zone success should recover
	now = now.Add(15 * time.Second)
	selector.RecordZoneSuccess("provider-a", "us-east-1a")

	// After success + time, should be available
	now = now.Add(6 * time.Minute)
	if selector.IsZoneExcluded("provider-a", "us-east-1a") {
		t.Error("zone should not be excluded after success")
	}
}

func TestFailoverSelector_AllExhaustedReturnsError(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	tracker := NewFailureTracker(
		WithBaseBackoff(10 * time.Second),
	)

	inner := NewPrioritySelector()
	selector := NewFailoverSelector(inner,
		WithFailureTracker(tracker),
		WithClock(clock),
	)

	candidates := []ProviderCandidate{
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
	}

	ctx := context.Background()

	// Fail all providers (marks them as attempted and starts backoff)
	selector.RecordFailure("a", nil)
	selector.RecordFailure("b", nil)

	// All providers exhausted - should return error
	_, err := selector.Select(ctx, candidates)
	if err == nil {
		t.Error("Select() should fail when all providers exhausted")
	}

	// After backoff expires and inner is reset, should work again
	now = now.Add(15 * time.Second)
	selector.RecordSuccess("a") // Reset inner selector
	c, err := selector.Select(ctx, candidates)
	if err != nil {
		t.Fatalf("Select() after reset error = %v", err)
	}
	if c.Name != "a" {
		t.Errorf("Select() = %s, want a", c.Name)
	}
}

func TestZoneDistributor_EvenDistribution(t *testing.T) {
	zd := NewZoneDistributor([]string{"a", "b", "c"})

	// First three should go to different zones
	zones := make(map[string]int)
	for i := 0; i < 3; i++ {
		zone := zd.NextZone()
		zones[zone]++
		zd.RecordProvisioned(zone)
	}

	// Each zone should have exactly 1
	for _, z := range []string{"a", "b", "c"} {
		if zones[z] != 1 {
			t.Errorf("zone %s count = %d, want 1", z, zones[z])
		}
	}

	// Next three should also distribute evenly
	for i := 0; i < 3; i++ {
		zone := zd.NextZone()
		zones[zone]++
		zd.RecordProvisioned(zone)
	}

	// Each zone should now have 2
	for _, z := range []string{"a", "b", "c"} {
		if zones[z] != 2 {
			t.Errorf("zone %s count = %d, want 2", z, zones[z])
		}
	}
}

func TestZoneDistributor_HandleTermination(t *testing.T) {
	zd := NewZoneDistributor([]string{"a", "b"})

	// Provision 2 in zone a, 1 in zone b
	zd.RecordProvisioned("a")
	zd.RecordProvisioned("a")
	zd.RecordProvisioned("b")

	// Next should go to b (fewer nodes)
	if zone := zd.NextZone(); zone != "b" {
		t.Errorf("NextZone() = %s, want b", zone)
	}

	// Terminate one from a
	zd.RecordTerminated("a")

	// Now a and b are equal, should return one of them
	zone := zd.NextZone()
	if zone != "a" && zone != "b" {
		t.Errorf("NextZone() = %s, want a or b", zone)
	}
}

func TestZoneDistributor_EmptyZones(t *testing.T) {
	zd := NewZoneDistributor([]string{})

	if zone := zd.NextZone(); zone != "" {
		t.Errorf("NextZone() = %s, want empty", zone)
	}
}

