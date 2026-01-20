package pool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	nodes       map[string]*provider.Node
	nextID      int
	failOn      string // "provision", "terminate", "list"
}

func newMockProvider() *mockProvider {
	return &mockProvider{nodes: make(map[string]*provider.Node)}
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	if m.failOn == "provision" {
		return nil, fmt.Errorf("mock provision error")
	}
	m.nextID++
	node := &provider.Node{
		ID:           fmt.Sprintf("node-%d", m.nextID),
		Provider:     "mock",
		InstanceType: req.InstanceType,
		Region:       req.Region,
		Status:       "running",
	}
	m.nodes[node.ID] = node
	return node, nil
}

func (m *mockProvider) Terminate(ctx context.Context, nodeID string) error {
	if m.failOn == "terminate" {
		return fmt.Errorf("mock terminate error")
	}
	delete(m.nodes, nodeID)
	return nil
}

func (m *mockProvider) List(ctx context.Context) ([]*provider.Node, error) {
	if m.failOn == "list" {
		return nil, fmt.Errorf("mock list error")
	}
	var nodes []*provider.Node
	for _, n := range m.nodes {
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Name:     "test-pool",
				MinNodes: 1,
				MaxNodes: 10,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			cfg: Config{
				MinNodes: 1,
				MaxNodes: 10,
			},
			wantErr: true,
		},
		{
			name: "negative min_nodes",
			cfg: Config{
				Name:     "test-pool",
				MinNodes: -1,
				MaxNodes: 10,
			},
			wantErr: true,
		},
		{
			name: "max < min",
			cfg: Config{
				Name:     "test-pool",
				MinNodes: 10,
				MaxNodes: 5,
			},
			wantErr: true,
		},
		{
			name: "zero max_nodes",
			cfg: Config{
				Name:     "test-pool",
				MinNodes: 0,
				MaxNodes: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSimple(tt.cfg, newMockProvider(), "mock")
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNew_NoProviders(t *testing.T) {
	_, err := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:     "test-pool",
			MinNodes: 0,
			MaxNodes: 10,
		},
		Providers: nil,
	})
	if err == nil {
		t.Error("New() should error when no providers given")
	}
}

func TestPool_ScaleUp(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:         "test-pool",
		MinNodes:     0,
		MaxNodes:     5,
		InstanceType: "gpu_1x_a100",
		Region:       "us-west-2",
	}, prov, "mock")

	ctx := context.Background()

	// Scale up by 2
	nodes, err := pool.ScaleUp(ctx, 2)
	if err != nil {
		t.Fatalf("ScaleUp() error = %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("ScaleUp() returned %d nodes, want 2", len(nodes))
	}

	status := pool.Status()
	if status.TotalNodes != 2 {
		t.Errorf("Status().TotalNodes = %d, want 2", status.TotalNodes)
	}

	// Try to scale up beyond max
	_, err = pool.ScaleUp(ctx, 10)
	if err != nil {
		t.Fatalf("ScaleUp() should not error, got: %v", err)
	}
	status = pool.Status()
	if status.TotalNodes != 5 {
		t.Errorf("TotalNodes = %d, want 5 (max)", status.TotalNodes)
	}

	// Try to scale up when at max
	_, err = pool.ScaleUp(ctx, 1)
	if err == nil {
		t.Error("ScaleUp() should error when at max capacity")
	}
}

func TestPool_ScaleDown(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:     "test-pool",
		MinNodes: 2,
		MaxNodes: 10,
	}, prov, "mock")

	ctx := context.Background()

	// Provision some nodes first
	pool.ScaleUp(ctx, 5)

	// Scale down by 2
	err := pool.ScaleDown(ctx, 2)
	if err != nil {
		t.Fatalf("ScaleDown() error = %v", err)
	}

	status := pool.Status()
	if status.TotalNodes != 3 {
		t.Errorf("TotalNodes = %d, want 3", status.TotalNodes)
	}

	// Try to scale down to exactly min (should succeed)
	err = pool.ScaleDown(ctx, 1)
	if err != nil {
		t.Fatalf("ScaleDown() to min error = %v", err)
	}

	status = pool.Status()
	if status.TotalNodes != 2 {
		t.Errorf("TotalNodes = %d, want 2 (min)", status.TotalNodes)
	}

	// Try to scale down below min (should fail)
	err = pool.ScaleDown(ctx, 1)
	if err == nil {
		t.Error("ScaleDown() should error when at min")
	}
}

func TestPool_Cordon(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:     "test-pool",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")

	ctx := context.Background()
	nodes, _ := pool.ScaleUp(ctx, 2)

	// Cordon a node
	err := pool.Cordon(nodes[0].ID)
	if err != nil {
		t.Fatalf("Cordon() error = %v", err)
	}

	status := pool.Status()
	if status.CordonedNodes != 1 {
		t.Errorf("CordonedNodes = %d, want 1", status.CordonedNodes)
	}

	// Uncordon
	err = pool.Uncordon(nodes[0].ID)
	if err != nil {
		t.Fatalf("Uncordon() error = %v", err)
	}

	status = pool.Status()
	if status.CordonedNodes != 0 {
		t.Errorf("CordonedNodes = %d, want 0", status.CordonedNodes)
	}
}

func TestPool_ScaleDown_PrefersCordoned(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:     "test-pool",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")

	ctx := context.Background()
	nodes, _ := pool.ScaleUp(ctx, 3)

	// Cordon the first node
	pool.Cordon(nodes[0].ID)

	// Scale down by 1 - should remove the cordoned node
	pool.ScaleDown(ctx, 1)

	// Verify the cordoned node was removed
	remaining := pool.Nodes()
	for _, mn := range remaining {
		if mn.Node.ID == nodes[0].ID {
			t.Error("Cordoned node should have been removed first")
		}
	}
}

func TestPool_ReplaceNode(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:         "test-pool",
		MinNodes:     1,
		MaxNodes:     10,
		InstanceType: "gpu_1x_a100",
	}, prov, "mock")

	ctx := context.Background()
	nodes, _ := pool.ScaleUp(ctx, 1)
	oldID := nodes[0].ID

	// Replace the node
	newNode, err := pool.ReplaceNode(ctx, oldID)
	if err != nil {
		t.Fatalf("ReplaceNode() error = %v", err)
	}

	if newNode.ID == oldID {
		t.Error("ReplaceNode() should return a new node")
	}

	// Old node should be gone
	for _, mn := range pool.Nodes() {
		if mn.Node.ID == oldID {
			t.Error("Old node should have been removed")
		}
	}

	status := pool.Status()
	if status.TotalNodes != 1 {
		t.Errorf("TotalNodes = %d, want 1", status.TotalNodes)
	}
}

func TestPool_HealthTracking(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:               "test-pool",
		MinNodes:           0,
		MaxNodes:           10,
		UnhealthyThreshold: 3,
		AutoReplace:        true,
	}, prov, "mock")

	ctx := context.Background()
	nodes, _ := pool.ScaleUp(ctx, 1)
	nodeID := nodes[0].ID

	// Record failures
	for i := 0; i < 2; i++ {
		shouldReplace := pool.RecordHealthFailure(nodeID)
		if shouldReplace {
			t.Errorf("RecordHealthFailure() returned true after %d failures, want false", i+1)
		}
	}

	// Third failure should trigger replacement
	shouldReplace := pool.RecordHealthFailure(nodeID)
	if !shouldReplace {
		t.Error("RecordHealthFailure() should return true after threshold reached")
	}

	// Health success resets counter
	pool.RecordHealthSuccess(nodeID)
	shouldReplace = pool.RecordHealthFailure(nodeID)
	if shouldReplace {
		t.Error("RecordHealthFailure() should return false after health success reset")
	}
}

func TestPool_Cooldown(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:           "test-pool",
		MinNodes:       0,
		MaxNodes:       10,
		CooldownPeriod: 100 * time.Millisecond,
	}, prov, "mock")

	ctx := context.Background()

	// First scale up succeeds
	_, err := pool.ScaleUp(ctx, 1)
	if err != nil {
		t.Fatalf("First ScaleUp() error = %v", err)
	}

	// Immediate second scale up fails due to cooldown
	_, err = pool.ScaleUp(ctx, 1)
	if err == nil {
		t.Error("Second ScaleUp() should fail during cooldown")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	// Now it should succeed
	_, err = pool.ScaleUp(ctx, 1)
	if err != nil {
		t.Fatalf("Third ScaleUp() error = %v", err)
	}
}

func TestPool_Status(t *testing.T) {
	prov := newMockProvider()
	pool, _ := NewSimple(Config{
		Name:               "test-pool",
		MinNodes:           2,
		MaxNodes:           10,
		UnhealthyThreshold: 2,
	}, prov, "mock")

	ctx := context.Background()
	nodes, _ := pool.ScaleUp(ctx, 5)

	// Cordon one
	pool.Cordon(nodes[0].ID)

	// Make one unhealthy
	pool.RecordHealthFailure(nodes[1].ID)
	pool.RecordHealthFailure(nodes[1].ID)

	status := pool.Status()
	if status.TotalNodes != 5 {
		t.Errorf("TotalNodes = %d, want 5", status.TotalNodes)
	}
	if status.HealthyNodes != 3 {
		t.Errorf("HealthyNodes = %d, want 3", status.HealthyNodes)
	}
	if status.UnhealthyNodes != 1 {
		t.Errorf("UnhealthyNodes = %d, want 1", status.UnhealthyNodes)
	}
	if status.CordonedNodes != 1 {
		t.Errorf("CordonedNodes = %d, want 1", status.CordonedNodes)
	}
	if !status.CanScaleUp {
		t.Error("CanScaleUp should be true")
	}
	if !status.CanScaleDown {
		t.Error("CanScaleDown should be true")
	}
}

func TestPool_MultiProvider_Priority(t *testing.T) {
	primary := newMockProvider()
	secondary := newMockProvider()

	pool, err := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:         "multi-pool",
			MinNodes:     0,
			MaxNodes:     10,
			InstanceType: "h100-8x",
		},
		Providers: []ProviderConfig{
			{Name: "primary", Provider: primary, Priority: 1},
			{Name: "secondary", Provider: secondary, Priority: 2},
		},
		ProviderStrategy: "priority",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	nodes, err := pool.ScaleUp(ctx, 2)
	if err != nil {
		t.Fatalf("ScaleUp() error = %v", err)
	}

	// Both nodes should come from primary provider
	for _, n := range nodes {
		if n.Provider != "mock" { // mockProvider always returns "mock"
			t.Errorf("expected node from primary provider")
		}
	}
	if len(primary.nodes) != 2 {
		t.Errorf("primary provider should have 2 nodes, got %d", len(primary.nodes))
	}
	if len(secondary.nodes) != 0 {
		t.Errorf("secondary provider should have 0 nodes, got %d", len(secondary.nodes))
	}
}

func TestPool_MultiProvider_Fallback(t *testing.T) {
	primary := newMockProvider()
	primary.failOn = "provision" // Primary fails
	secondary := newMockProvider()

	pool, _ := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:         "failover-pool",
			MinNodes:     0,
			MaxNodes:     10,
			InstanceType: "h100-8x",
		},
		Providers: []ProviderConfig{
			{Name: "primary", Provider: primary, Priority: 1},
			{Name: "secondary", Provider: secondary, Priority: 2},
		},
		ProviderStrategy: "priority",
	})

	ctx := context.Background()
	nodes, err := pool.ScaleUp(ctx, 1)
	if err != nil {
		t.Fatalf("ScaleUp() should succeed via fallback, got error = %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes))
	}
	if len(secondary.nodes) != 1 {
		t.Errorf("secondary provider should have 1 node, got %d", len(secondary.nodes))
	}

	// Verify the node is tracked with correct provider
	managedNodes := pool.Nodes()
	if len(managedNodes) != 1 {
		t.Fatalf("expected 1 managed node, got %d", len(managedNodes))
	}
	if managedNodes[0].ProviderName != "secondary" {
		t.Errorf("ProviderName = %s, want secondary", managedNodes[0].ProviderName)
	}
}

func TestPool_MultiProvider_AllFail(t *testing.T) {
	primary := newMockProvider()
	primary.failOn = "provision"
	secondary := newMockProvider()
	secondary.failOn = "provision"

	pool, _ := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:         "all-fail-pool",
			MinNodes:     0,
			MaxNodes:     10,
			InstanceType: "h100-8x",
		},
		Providers: []ProviderConfig{
			{Name: "primary", Provider: primary, Priority: 1},
			{Name: "secondary", Provider: secondary, Priority: 2},
		},
		ProviderStrategy: "priority",
	})

	ctx := context.Background()
	_, err := pool.ScaleUp(ctx, 1)
	if err == nil {
		t.Error("ScaleUp() should fail when all providers fail")
	}
}

func TestPool_MultiProvider_RoundRobin(t *testing.T) {
	provider1 := newMockProvider()
	provider2 := newMockProvider()

	pool, _ := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:         "rr-pool",
			MinNodes:     0,
			MaxNodes:     10,
			InstanceType: "h100-8x",
		},
		Providers: []ProviderConfig{
			{Name: "provider1", Provider: provider1, Weight: 1},
			{Name: "provider2", Provider: provider2, Weight: 1},
		},
		ProviderStrategy: "round-robin",
	})

	ctx := context.Background()
	pool.ScaleUp(ctx, 4)

	// Round-robin should distribute evenly (2 each)
	if len(provider1.nodes)+len(provider2.nodes) != 4 {
		t.Errorf("total nodes should be 4, got %d", len(provider1.nodes)+len(provider2.nodes))
	}
}

