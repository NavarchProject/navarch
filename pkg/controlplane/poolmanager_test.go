package controlplane

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
)

type mockProvider struct {
	provisions atomic.Int64
	terminates atomic.Int64
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	m.provisions.Add(1)
	return &provider.Node{
		ID:           "node-" + req.Name,
		Provider:     "mock",
		InstanceType: req.InstanceType,
		Status:       "running",
	}, nil
}
func (m *mockProvider) Terminate(ctx context.Context, id string) error {
	m.terminates.Add(1)
	return nil
}
func (m *mockProvider) List(ctx context.Context) ([]*provider.Node, error) {
	return nil, nil
}

type mockMetrics struct {
	utilization float64
	queueDepth  int
}

func (m *mockMetrics) GetPoolMetrics(ctx context.Context, name string) (*PoolMetrics, error) {
	return &PoolMetrics{
		Utilization: m.utilization,
		QueueDepth:  m.queueDepth,
	}, nil
}

func TestPoolManager_AddRemovePool(t *testing.T) {
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

	prov := &mockProvider{}
	p, err := pool.NewSimple(pool.Config{
		Name:     "test-pool",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")
	if err != nil {
		t.Fatal(err)
	}

	if err := pm.AddPool(p, nil); err != nil {
		t.Fatal(err)
	}

	pools := pm.ListPools()
	if len(pools) != 1 || pools[0] != "test-pool" {
		t.Errorf("expected [test-pool], got %v", pools)
	}

	if err := pm.AddPool(p, nil); err == nil {
		t.Error("expected error adding duplicate pool")
	}

	if err := pm.RemovePool("test-pool"); err != nil {
		t.Fatal(err)
	}

	pools = pm.ListPools()
	if len(pools) != 0 {
		t.Errorf("expected empty, got %v", pools)
	}

	if err := pm.RemovePool("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent pool")
	}
}

func TestPoolManager_GetPool(t *testing.T) {
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewSimple(pool.Config{
		Name:     "test",
		MinNodes: 0,
		MaxNodes: 5,
	}, prov, "mock")
	pm.AddPool(p, nil)

	got, ok := pm.GetPool("test")
	if !ok || got == nil {
		t.Error("expected to find pool")
	}

	_, ok = pm.GetPool("nonexistent")
	if ok {
		t.Error("expected not to find pool")
	}
}

func TestPoolManager_GetPoolStatus(t *testing.T) {
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewSimple(pool.Config{
		Name:     "status-test",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")
	pm.AddPool(p, nil)

	status, err := pm.GetPoolStatus("status-test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "status-test" {
		t.Errorf("expected name status-test, got %s", status.Name)
	}

	_, err = pm.GetPoolStatus("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

func TestPoolManager_ScalePool(t *testing.T) {
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewSimple(pool.Config{
		Name:     "scale-test",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")
	pm.AddPool(p, nil)

	ctx := context.Background()
	if err := pm.ScalePool(ctx, "scale-test", 3); err != nil {
		t.Fatal(err)
	}

	status, _ := pm.GetPoolStatus("scale-test")
	if status.TotalNodes != 3 {
		t.Errorf("expected 3 nodes, got %d", status.TotalNodes)
	}

	if err := pm.ScalePool(ctx, "scale-test", 3); err != nil {
		t.Error("scaling to same size should not error")
	}

	if err := pm.ScalePool(ctx, "nonexistent", 1); err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

func TestPoolManager_StartStop(t *testing.T) {
	pm := NewPoolManager(PoolManagerConfig{
		EvaluationInterval: 100 * time.Millisecond,
	}, nil, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewSimple(pool.Config{
		Name:     "loop-test",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")
	pm.AddPool(p, nil)

	ctx, cancel := context.WithCancel(context.Background())
	pm.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	cancel()
	pm.Stop()
}

func TestPoolManager_AutoscalerLoop(t *testing.T) {
	metrics := &mockMetrics{utilization: 90}
	pm := NewPoolManager(PoolManagerConfig{
		EvaluationInterval: 50 * time.Millisecond,
	}, metrics, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewSimple(pool.Config{
		Name:     "autoscale-test",
		MinNodes: 0,
		MaxNodes: 10,
	}, prov, "mock")

	autoscaler := pool.NewReactiveAutoscaler(80, 20)

	pm.AddPool(p, autoscaler)

	ctx, cancel := context.WithCancel(context.Background())
	pm.Start(ctx)

	time.Sleep(150 * time.Millisecond)

	cancel()
	pm.Stop()

	if prov.provisions.Load() == 0 {
		t.Error("expected autoscaler to trigger scale up")
	}
}

func TestPoolManager_OnNodeUnhealthy(t *testing.T) {
	t.Run("node_in_pool_with_auto_replace", func(t *testing.T) {
		pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

		prov := &mockProvider{}
		p, _ := pool.NewSimple(pool.Config{
			Name:               "test-pool",
			MinNodes:           0,
			MaxNodes:           10,
			AutoReplace:        true,
			UnhealthyThreshold: 1,
		}, prov, "mock")
		pm.AddPool(p, nil)

		// Add a node to the pool
		ctx := context.Background()
		nodes, _ := p.ScaleUp(ctx, 1)
		if len(nodes) == 0 {
			t.Fatal("Failed to add node to pool")
		}
		nodeID := nodes[0].ID

		// Trigger unhealthy notification
		pm.OnNodeUnhealthy(ctx, nodeID)

		// Should have terminated the old node and provisioned a new one
		if prov.terminates.Load() != 1 {
			t.Errorf("Expected 1 termination, got %d", prov.terminates.Load())
		}
		if prov.provisions.Load() != 2 { // 1 initial + 1 replacement
			t.Errorf("Expected 2 provisions, got %d", prov.provisions.Load())
		}
	})

	t.Run("node_in_pool_without_auto_replace", func(t *testing.T) {
		pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

		prov := &mockProvider{}
		p, _ := pool.NewSimple(pool.Config{
			Name:        "test-pool",
			MinNodes:    0,
			MaxNodes:    10,
			AutoReplace: false, // disabled
		}, prov, "mock")
		pm.AddPool(p, nil)

		// Add a node to the pool
		ctx := context.Background()
		nodes, _ := p.ScaleUp(ctx, 1)
		if len(nodes) == 0 {
			t.Fatal("Failed to add node to pool")
		}
		nodeID := nodes[0].ID

		// Trigger unhealthy notification
		pm.OnNodeUnhealthy(ctx, nodeID)

		// Should NOT terminate or provision
		if prov.terminates.Load() != 0 {
			t.Errorf("Expected 0 terminations, got %d", prov.terminates.Load())
		}
		if prov.provisions.Load() != 1 { // Only initial
			t.Errorf("Expected 1 provision (initial only), got %d", prov.provisions.Load())
		}
	})

	t.Run("node_not_in_any_pool", func(t *testing.T) {
		pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

		prov := &mockProvider{}
		p, _ := pool.NewSimple(pool.Config{
			Name:     "test-pool",
			MinNodes: 0,
			MaxNodes: 10,
		}, prov, "mock")
		pm.AddPool(p, nil)

		// Trigger unhealthy for unknown node (should not panic)
		ctx := context.Background()
		pm.OnNodeUnhealthy(ctx, "unknown-node-id")

		// Should not affect anything
		if prov.terminates.Load() != 0 {
			t.Errorf("Expected 0 terminations, got %d", prov.terminates.Load())
		}
	})

	t.Run("threshold_not_reached", func(t *testing.T) {
		pm := NewPoolManager(PoolManagerConfig{}, nil, nil, nil)

		prov := &mockProvider{}
		p, _ := pool.NewSimple(pool.Config{
			Name:               "test-pool",
			MinNodes:           0,
			MaxNodes:           10,
			AutoReplace:        true,
			UnhealthyThreshold: 3, // Requires 3 failures
		}, prov, "mock")
		pm.AddPool(p, nil)

		// Add a node to the pool
		ctx := context.Background()
		nodes, _ := p.ScaleUp(ctx, 1)
		if len(nodes) == 0 {
			t.Fatal("Failed to add node to pool")
		}
		nodeID := nodes[0].ID

		// First unhealthy notification - threshold not reached
		pm.OnNodeUnhealthy(ctx, nodeID)
		if prov.terminates.Load() != 0 {
			t.Errorf("Expected 0 terminations after 1st failure, got %d", prov.terminates.Load())
		}

		// Second unhealthy notification - threshold not reached
		pm.OnNodeUnhealthy(ctx, nodeID)
		if prov.terminates.Load() != 0 {
			t.Errorf("Expected 0 terminations after 2nd failure, got %d", prov.terminates.Load())
		}

		// Third unhealthy notification - threshold reached
		pm.OnNodeUnhealthy(ctx, nodeID)
		if prov.terminates.Load() != 1 {
			t.Errorf("Expected 1 termination after 3rd failure, got %d", prov.terminates.Load())
		}
	})
}

