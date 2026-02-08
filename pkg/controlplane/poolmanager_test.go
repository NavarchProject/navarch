package controlplane

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/NavarchProject/navarch/pkg/clock"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
	pb "github.com/NavarchProject/navarch/proto"
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
	nodePools   map[string]string // nodeID -> poolName
}

func (m *mockMetrics) GetPoolMetrics(ctx context.Context, name string) (*PoolMetrics, error) {
	return &PoolMetrics{
		Utilization: m.utilization,
		QueueDepth:  m.queueDepth,
	}, nil
}

func (m *mockMetrics) GetPoolNodeCounts(ctx context.Context, name string) (PoolNodeCounts, error) {
	return PoolNodeCounts{}, nil
}

func (m *mockMetrics) GetNodePool(ctx context.Context, nodeID string) (string, error) {
	if m.nodePools != nil {
		return m.nodePools[nodeID], nil
	}
	return "", nil
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
	fakeClock := clock.NewFakeClock(time.Now())
	pm := NewPoolManager(PoolManagerConfig{
		EvaluationInterval: 100 * time.Millisecond,
		Clock:              fakeClock,
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

	// Advance time to trigger a few evaluation cycles
	fakeClock.Advance(50 * time.Millisecond)

	cancel()
	pm.Stop()
}

func TestPoolManager_AutoscalerLoop(t *testing.T) {
	fakeClock := clock.NewFakeClock(time.Now())
	metrics := &mockMetrics{utilization: 90}
	pm := NewPoolManager(PoolManagerConfig{
		EvaluationInterval: 50 * time.Millisecond,
		Clock:              fakeClock,
	}, metrics, nil, nil)

	prov := &mockProvider{}
	p, _ := pool.NewWithOptions(pool.NewPoolOptions{
		Config: pool.Config{
			Name:     "autoscale-test",
			MinNodes: 0,
			MaxNodes: 10,
		},
		Providers: []pool.ProviderConfig{
			{Name: "mock", Provider: prov, Priority: 1},
		},
		ProviderStrategy: "priority",
		Clock:            fakeClock,
	})

	autoscaler := pool.NewReactiveAutoscaler(80, 20)

	pm.AddPool(p, autoscaler)

	ctx, cancel := context.WithCancel(context.Background())
	pm.Start(ctx)

	// Advance time to trigger evaluation cycles
	for i := 0; i < 3; i++ {
		fakeClock.Advance(50 * time.Millisecond)
		// Brief yield to let goroutine process the tick
		time.Sleep(time.Millisecond)
	}

	cancel()
	pm.Stop()

	if prov.provisions.Load() == 0 {
		t.Error("expected autoscaler to trigger scale up")
	}
}

func TestPoolManager_OnNodeUnhealthy(t *testing.T) {
	t.Run("node_in_pool_with_auto_replace", func(t *testing.T) {
		// Create mock metrics that we can update after getting the node ID
		metrics := &mockMetrics{nodePools: make(map[string]string)}
		pm := NewPoolManager(PoolManagerConfig{}, metrics, nil, nil)

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

		// Register the node's pool in the mock metrics (simulating database lookup)
		metrics.nodePools[nodeID] = "test-pool"

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
		metrics := &mockMetrics{nodePools: make(map[string]string)}
		pm := NewPoolManager(PoolManagerConfig{}, metrics, nil, nil)

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

		// Register the node's pool in the mock metrics
		metrics.nodePools[nodeID] = "test-pool"

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

// TestPoolManager_IntegrationWithDBMetrics verifies the full flow:
// nodes register with pool labels → DBMetricsSource counts them → autoscaler sees correct counts.
func TestPoolManager_IntegrationWithDBMetrics(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)
	srv := NewServer(database, DefaultConfig(), nil, nil)
	fakeClock := clock.NewFakeClock(time.Now())
	prov := &mockProvider{}

	p, err := pool.NewWithOptions(pool.NewPoolOptions{
		Config: pool.Config{
			Name:           "integration-test",
			MinNodes:       3,
			MaxNodes:       5,
			CooldownPeriod: 0,
		},
		Providers: []pool.ProviderConfig{
			{Name: "mock", Provider: prov, Priority: 1},
		},
		ProviderStrategy: "priority",
		Clock:            fakeClock,
	})
	if err != nil {
		t.Fatal(err)
	}

	pm := NewPoolManager(PoolManagerConfig{
		EvaluationInterval: 100 * time.Millisecond,
		Clock:              fakeClock,
	}, metricsSource, nil, nil)

	autoscaler := pool.NewReactiveAutoscaler(80, 20)
	if err := pm.AddPool(p, autoscaler); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		req := &pb.RegisterNodeRequest{
			NodeId:       fmt.Sprintf("node-%d", i),
			Provider:     "mock",
			Region:       "us-east-1",
			InstanceType: "gpu-8x",
			Metadata: &pb.NodeMetadata{
				Labels: map[string]string{"pool": "integration-test"},
			},
		}
		resp, err := srv.RegisterNode(ctx, connect.NewRequest(req))
		if err != nil {
			t.Fatalf("failed to register node-%d: %v", i, err)
		}
		if !resp.Msg.Success {
			t.Fatalf("registration rejected for node-%d: %s", i, resp.Msg.Message)
		}
	}

	counts, err := metricsSource.GetPoolNodeCounts(ctx, "integration-test")
	if err != nil {
		t.Fatalf("GetPoolNodeCounts failed: %v", err)
	}
	if counts.Total != 3 {
		t.Errorf("expected 3 total nodes, got %d", counts.Total)
	}
	if counts.Healthy != 3 {
		t.Errorf("expected 3 healthy nodes, got %d", counts.Healthy)
	}

	evalCtx, cancel := context.WithCancel(ctx)
	pm.Start(evalCtx)

	fakeClock.Advance(100 * time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	// 3 nodes registered = min_nodes met, no scaling should occur
	if prov.provisions.Load() != 0 {
		t.Errorf("expected no provisions, got %d", prov.provisions.Load())
	}

	if err := database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_UNHEALTHY); err != nil {
		t.Fatalf("failed to mark node unhealthy: %v", err)
	}

	counts, err = metricsSource.GetPoolNodeCounts(ctx, "integration-test")
	if err != nil {
		t.Fatalf("GetPoolNodeCounts failed: %v", err)
	}
	if counts.Healthy != 2 {
		t.Errorf("expected 2 healthy nodes after marking one unhealthy, got %d", counts.Healthy)
	}
	if counts.Unhealthy != 1 {
		t.Errorf("expected 1 unhealthy node, got %d", counts.Unhealthy)
	}

	cancel()
	pm.Stop()
}
