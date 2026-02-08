package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestDBMetricsSource_GetPoolMetrics(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)

	// Register two nodes with pool labels
	node1 := &db.NodeRecord{
		NodeID:       "node-1",
		Provider:     "fake",
		InstanceType: "fake-8xgpu",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}
	node2 := &db.NodeRecord{
		NodeID:       "node-2",
		Provider:     "fake",
		InstanceType: "fake-8xgpu",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}

	if err := database.RegisterNode(ctx, node1); err != nil {
		t.Fatalf("failed to register node1: %v", err)
	}
	if err := database.RegisterNode(ctx, node2); err != nil {
		t.Fatalf("failed to register node2: %v", err)
	}

	// Store metrics for node-1: 2 GPUs at 80% and 90%
	metrics1 := &pb.NodeMetrics{
		GpuMetrics: []*pb.GPUMetrics{
			{GpuIndex: 0, UtilizationPercent: 80.0},
			{GpuIndex: 1, UtilizationPercent: 90.0},
		},
	}
	if err := metricsSource.StoreMetrics(ctx, "node-1", metrics1); err != nil {
		t.Fatalf("failed to store metrics for node-1: %v", err)
	}

	// Store metrics for node-2: 2 GPUs at 60% and 70%
	metrics2 := &pb.NodeMetrics{
		GpuMetrics: []*pb.GPUMetrics{
			{GpuIndex: 0, UtilizationPercent: 60.0},
			{GpuIndex: 1, UtilizationPercent: 70.0},
		},
	}
	if err := metricsSource.StoreMetrics(ctx, "node-2", metrics2); err != nil {
		t.Fatalf("failed to store metrics for node-2: %v", err)
	}

	// Get pool metrics
	poolMetrics, err := metricsSource.GetPoolMetrics(ctx, "test-pool")
	if err != nil {
		t.Fatalf("GetPoolMetrics failed: %v", err)
	}

	// Expected average: (80 + 90 + 60 + 70) / 4 = 75%
	expectedAvg := 75.0
	if poolMetrics.Utilization < expectedAvg-1 || poolMetrics.Utilization > expectedAvg+1 {
		t.Errorf("Expected utilization ~%.1f%%, got %.1f%%", expectedAvg, poolMetrics.Utilization)
	}

	if len(poolMetrics.UtilizationHistory) == 0 {
		t.Error("Expected utilization history, got empty")
	}
}

func TestDBMetricsSource_NoNodesInPool(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)

	// Get metrics for non-existent pool
	poolMetrics, err := metricsSource.GetPoolMetrics(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPoolMetrics failed: %v", err)
	}

	if poolMetrics.Utilization != 0 {
		t.Errorf("Expected 0 utilization for empty pool, got %.1f%%", poolMetrics.Utilization)
	}
}

func TestDBMetricsSource_GetPoolNodeCounts(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)

	node1 := &db.NodeRecord{
		NodeID:   "node-1",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}
	node2 := &db.NodeRecord{
		NodeID:   "node-2",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}
	node3 := &db.NodeRecord{
		NodeID:   "node-3",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_UNHEALTHY,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}
	node4 := &db.NodeRecord{ // different pool
		NodeID:   "node-4",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "other-pool"},
		},
	}
	node5 := &db.NodeRecord{ // terminated, should not be counted
		NodeID:   "node-5",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_TERMINATED,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}

	for _, node := range []*db.NodeRecord{node1, node2, node3, node4, node5} {
		if err := database.RegisterNode(ctx, node); err != nil {
			t.Fatalf("failed to register %s: %v", node.NodeID, err)
		}
	}

	counts, err := metricsSource.GetPoolNodeCounts(ctx, "test-pool")
	if err != nil {
		t.Fatalf("GetPoolNodeCounts failed: %v", err)
	}

	if counts.Total != 3 {
		t.Errorf("Expected total=3, got %d", counts.Total)
	}
	if counts.Healthy != 2 {
		t.Errorf("Expected healthy=2, got %d", counts.Healthy)
	}
	if counts.Unhealthy != 1 {
		t.Errorf("Expected unhealthy=1, got %d", counts.Unhealthy)
	}

	counts, err = metricsSource.GetPoolNodeCounts(ctx, "other-pool")
	if err != nil {
		t.Fatalf("GetPoolNodeCounts failed: %v", err)
	}
	if counts.Total != 1 {
		t.Errorf("Expected total=1 for other-pool, got %d", counts.Total)
	}

	counts, err = metricsSource.GetPoolNodeCounts(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPoolNodeCounts failed: %v", err)
	}
	if counts.Total != 0 {
		t.Errorf("Expected total=0 for nonexistent pool, got %d", counts.Total)
	}
}

func TestDBMetricsSource_GetNodePool(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)

	node := &db.NodeRecord{
		NodeID:   "test-node",
		Provider: "fake",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "my-pool"},
		},
	}
	if err := database.RegisterNode(ctx, node); err != nil {
		t.Fatalf("failed to register node: %v", err)
	}

	poolName, err := metricsSource.GetNodePool(ctx, "test-node")
	if err != nil {
		t.Fatalf("GetNodePool failed: %v", err)
	}
	if poolName != "my-pool" {
		t.Errorf("Expected pool 'my-pool', got '%s'", poolName)
	}

	poolName, err = metricsSource.GetNodePool(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetNodePool failed for nonexistent node: %v", err)
	}
	if poolName != "" {
		t.Errorf("Expected empty pool for nonexistent node, got '%s'", poolName)
	}
}

func TestDBMetricsSource_MetricsRetention(t *testing.T) {
	ctx := context.Background()
	database := db.NewInMemDB()
	defer database.Close()

	metricsSource := NewDBMetricsSource(database, nil)

	// Register node
	node := &db.NodeRecord{
		NodeID:       "node-1",
		Provider:     "fake",
		InstanceType: "fake-8xgpu",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "test-pool"},
		},
	}
	if err := database.RegisterNode(ctx, node); err != nil {
		t.Fatalf("failed to register node: %v", err)
	}

	// Store 10 metrics samples
	for i := 0; i < 10; i++ {
		metrics := &pb.NodeMetrics{
			GpuMetrics: []*pb.GPUMetrics{
				{GpuIndex: 0, UtilizationPercent: float64(50 + i)},
			},
		}
		if err := metricsSource.StoreMetrics(ctx, "node-1", metrics); err != nil {
			t.Fatalf("failed to store metrics: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Get recent metrics (last 5 minutes)
	recent, err := database.GetRecentMetrics(ctx, "node-1", 5*time.Minute)
	if err != nil {
		t.Fatalf("GetRecentMetrics failed: %v", err)
	}

	if len(recent) != 10 {
		t.Errorf("Expected 10 recent metrics, got %d", len(recent))
	}

	// Get metrics from a narrow window (should be less)
	recent, err = database.GetRecentMetrics(ctx, "node-1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("GetRecentMetrics failed: %v", err)
	}

	if len(recent) >= 10 {
		t.Errorf("Expected fewer metrics in narrow window, got %d", len(recent))
	}
}

