package db

import (
	"context"
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

func TestInMemDB_RegisterAndGetNode(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	record := &NodeRecord{
		NodeID:       "node-1",
		Provider:     "gcp",
		Region:       "us-central1",
		Zone:         "us-central1-a",
		InstanceType: "a3-highgpu-8g",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
	}

	// Register node
	if err := db.RegisterNode(ctx, record); err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Get node
	retrieved, err := db.GetNode(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	if retrieved.NodeID != "node-1" {
		t.Errorf("Expected NodeID 'node-1', got '%s'", retrieved.NodeID)
	}
	if retrieved.Provider != "gcp" {
		t.Errorf("Expected Provider 'gcp', got '%s'", retrieved.Provider)
	}
	if retrieved.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Errorf("Expected Status ACTIVE, got %v", retrieved.Status)
	}
}

func TestInMemDB_ListNodes(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Register multiple nodes
	for i := 1; i <= 3; i++ {
		record := &NodeRecord{
			NodeID:   "node-" + string(rune('0'+i)),
			Provider: "gcp",
			Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		}
		if err := db.RegisterNode(ctx, record); err != nil {
			t.Fatalf("RegisterNode failed: %v", err)
		}
	}

	// List nodes
	nodes, err := db.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodes))
	}
}

func TestInMemDB_UpdateNodeStatus(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	record := &NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	}
	db.RegisterNode(ctx, record)

	// Update status
	if err := db.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_CORDONED); err != nil {
		t.Fatalf("UpdateNodeStatus failed: %v", err)
	}

	// Verify update
	retrieved, _ := db.GetNode(ctx, "node-1")
	if retrieved.Status != pb.NodeStatus_NODE_STATUS_CORDONED {
		t.Errorf("Expected Status CORDONED, got %v", retrieved.Status)
	}
}

func TestInMemDB_HealthCheck(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Register node first
	record := &NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	}
	db.RegisterNode(ctx, record)

	// Record health check
	healthRecord := &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
				Message:   "All GPUs healthy",
			},
		},
	}

	if err := db.RecordHealthCheck(ctx, healthRecord); err != nil {
		t.Fatalf("RecordHealthCheck failed: %v", err)
	}

	// Get latest health check
	latest, err := db.GetLatestHealthCheck(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetLatestHealthCheck failed: %v", err)
	}

	if len(latest.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(latest.Results))
	}
	if latest.Results[0].CheckName != "nvml" {
		t.Errorf("Expected check name 'nvml', got '%s'", latest.Results[0].CheckName)
	}
}

func TestInMemDB_Commands(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Create command
	cmdRecord := &CommandRecord{
		CommandID:  "cmd-1",
		NodeID:     "node-1",
		Type:       pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		Parameters: map[string]string{"reason": "maintenance"},
		IssuedAt:   time.Now(),
	}

	if err := db.CreateCommand(ctx, cmdRecord); err != nil {
		t.Fatalf("CreateCommand failed: %v", err)
	}

	// Get pending commands
	pending, err := db.GetPendingCommands(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetPendingCommands failed: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("Expected 1 pending command, got %d", len(pending))
	}
	if pending[0].CommandID != "cmd-1" {
		t.Errorf("Expected CommandID 'cmd-1', got '%s'", pending[0].CommandID)
	}

	// Update command status
	if err := db.UpdateCommandStatus(ctx, "cmd-1", "completed"); err != nil {
		t.Fatalf("UpdateCommandStatus failed: %v", err)
	}

	// Verify no pending commands now
	pending, _ = db.GetPendingCommands(ctx, "node-1")
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending commands, got %d", len(pending))
	}
}

func TestInMemDB_UnhealthyNodeStatus(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Register node
	record := &NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	}
	db.RegisterNode(ctx, record)

	// Record unhealthy check
	healthRecord := &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				Message:   "GPU failure detected",
			},
		},
	}

	db.RecordHealthCheck(ctx, healthRecord)

	// Verify node status was updated to unhealthy
	node, _ := db.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Errorf("Expected Status UNHEALTHY, got %v", node.Status)
	}
	if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
		t.Errorf("Expected HealthStatus UNHEALTHY, got %v", node.HealthStatus)
	}
}

