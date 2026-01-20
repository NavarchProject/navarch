package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
)

type mockProvider struct {
	provisions int
	terminates int
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	m.provisions++
	return &provider.Node{
		ID:           "node-" + req.Name,
		Provider:     "mock",
		InstanceType: req.InstanceType,
		Status:       "running",
	}, nil
}
func (m *mockProvider) Terminate(ctx context.Context, id string) error {
	m.terminates++
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
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil)

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
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil)

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
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil)

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
	pm := NewPoolManager(PoolManagerConfig{}, nil, nil)

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
	}, nil, nil)

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
	}, metrics, nil)

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

	if prov.provisions == 0 {
		t.Error("expected autoscaler to trigger scale up")
	}
}

