package controlplane

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestPrometheusMetrics_NodesTotal(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()
	ctx := context.Background()

	// Register nodes with different statuses
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:   "node-1",
		Provider: "gcp",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		GPUs:     []*pb.GPUInfo{{Index: 0}, {Index: 1}},
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:   "node-2",
		Provider: "gcp",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		GPUs:     []*pb.GPUInfo{{Index: 0}},
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:   "node-3",
		Provider: "aws",
		Status:   pb.NodeStatus_NODE_STATUS_CORDONED,
		GPUs:     []*pb.GPUInfo{{Index: 0}, {Index: 1}, {Index: 2}},
	})

	pm := NewPrometheusMetrics(database)

	// Register collector for testing
	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	// Check nodes_total metric
	count, err := testutil.GatherAndCount(registry, "navarch_nodes_total")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_nodes_total metric")
	}

	// Check gpus_total metric
	count, err = testutil.GatherAndCount(registry, "navarch_gpus_total")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_gpus_total metric")
	}
}

func TestPrometheusMetrics_HealthStatus(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()
	ctx := context.Background()

	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:       "node-healthy",
		HealthStatus: pb.HealthStatus_HEALTH_STATUS_HEALTHY,
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:       "node-degraded",
		HealthStatus: pb.HealthStatus_HEALTH_STATUS_DEGRADED,
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:       "node-unhealthy",
		HealthStatus: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
	})

	pm := NewPrometheusMetrics(database)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	count, err := testutil.GatherAndCount(registry, "navarch_node_health_status")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_node_health_status metric")
	}
}

func TestPrometheusMetrics_PoolNodes(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()
	ctx := context.Background()

	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "gpu-pool-1"},
		},
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-2",
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "gpu-pool-1"},
		},
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-3",
		Metadata: &pb.NodeMetadata{
			Labels: map[string]string{"pool": "gpu-pool-2"},
		},
	})

	pm := NewPrometheusMetrics(database)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	count, err := testutil.GatherAndCount(registry, "navarch_pool_current_nodes")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_pool_current_nodes metric")
	}
}

func TestPrometheusMetrics_RecordHealthEvent(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	pm := NewPrometheusMetrics(database)

	pm.RecordHealthEvent("xid")
	pm.RecordHealthEvent("xid")
	pm.RecordHealthEvent("thermal")

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	count, err := testutil.GatherAndCount(registry, "navarch_health_events_total")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_health_events_total metric")
	}
}

func TestPrometheusMetrics_ScalingEvents(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	pm := NewPrometheusMetrics(database)

	pm.RecordScalingEvent("gpu-pool-1", "up")
	pm.RecordScalingEvent("gpu-pool-1", "up")
	pm.RecordScalingEvent("gpu-pool-1", "down")

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	count, err := testutil.GatherAndCount(registry, "navarch_scaling_events_total")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("Expected navarch_scaling_events_total metric")
	}
}

func TestPrometheusMetrics_SetPoolTargetNodes(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	pm := NewPrometheusMetrics(database)

	pm.SetPoolTargetNodes("gpu-pool-1", 5)
	pm.SetPoolTargetNodes("gpu-pool-2", 3)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	count, err := testutil.GatherAndCount(registry, "navarch_pool_target_nodes")
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 pool_target_nodes metrics, got %d", count)
	}
}

func TestPrometheusMetrics_EmptyDatabase(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	pm := NewPrometheusMetrics(database)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm)

	// Should not panic with empty database
	_, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	// No assertion on families count - empty database may return no metric families
	// The important thing is that it doesn't panic or error
}

func TestNodeStatusString(t *testing.T) {
	tests := []struct {
		status   pb.NodeStatus
		expected string
	}{
		{pb.NodeStatus_NODE_STATUS_ACTIVE, "active"},
		{pb.NodeStatus_NODE_STATUS_CORDONED, "cordoned"},
		{pb.NodeStatus_NODE_STATUS_DRAINING, "draining"},
		{pb.NodeStatus_NODE_STATUS_UNHEALTHY, "unhealthy"},
		{pb.NodeStatus_NODE_STATUS_TERMINATED, "terminated"},
		{pb.NodeStatus_NODE_STATUS_UNKNOWN, "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := nodeStatusString(tc.status)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestHealthStatusString(t *testing.T) {
	tests := []struct {
		status   pb.HealthStatus
		expected string
	}{
		{pb.HealthStatus_HEALTH_STATUS_HEALTHY, "healthy"},
		{pb.HealthStatus_HEALTH_STATUS_DEGRADED, "degraded"},
		{pb.HealthStatus_HEALTH_STATUS_UNHEALTHY, "unhealthy"},
		{pb.HealthStatus_HEALTH_STATUS_UNKNOWN, "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := healthStatusString(tc.status)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestHealthStatusValue(t *testing.T) {
	tests := []struct {
		status   pb.HealthStatus
		expected float64
	}{
		{pb.HealthStatus_HEALTH_STATUS_HEALTHY, 1.0},
		{pb.HealthStatus_HEALTH_STATUS_DEGRADED, 0.5},
		{pb.HealthStatus_HEALTH_STATUS_UNHEALTHY, 0.0},
		{pb.HealthStatus_HEALTH_STATUS_UNKNOWN, 0.0},
	}

	for _, tc := range tests {
		t.Run(healthStatusString(tc.status), func(t *testing.T) {
			result := healthStatusValue(tc.status)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}
