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

func TestInMemDB_HealthStatusTransitions(t *testing.T) {
	tests := []struct {
		name               string
		initialNodeStatus  pb.NodeStatus
		healthCheckStatus  pb.HealthStatus
		expectedNodeStatus pb.NodeStatus
		expectedHealth     pb.HealthStatus
	}{
		{
			name:               "active_stays_active_on_healthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_ACTIVE,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_ACTIVE,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:               "active_stays_active_on_degraded",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_ACTIVE,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_DEGRADED,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_ACTIVE,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name:               "active_becomes_unhealthy_on_unhealthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_ACTIVE,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:               "unhealthy_becomes_active_on_healthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_ACTIVE,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:               "unhealthy_stays_unhealthy_on_degraded",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_DEGRADED,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name:               "unhealthy_stays_unhealthy_on_unhealthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:               "cordoned_stays_cordoned_on_healthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_CORDONED,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_CORDONED,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:               "cordoned_becomes_unhealthy_on_unhealthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_CORDONED,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:               "draining_stays_draining_on_healthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_DRAINING,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_DRAINING,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:               "draining_becomes_unhealthy_on_unhealthy",
			initialNodeStatus:  pb.NodeStatus_NODE_STATUS_DRAINING,
			healthCheckStatus:  pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			expectedNodeStatus: pb.NodeStatus_NODE_STATUS_UNHEALTHY,
			expectedHealth:     pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewInMemDB()
			ctx := context.Background()

			// Register node with initial status
			record := &NodeRecord{
				NodeID: "node-1",
				Status: tt.initialNodeStatus,
			}
			db.RegisterNode(ctx, record)

			// Record health check
			healthRecord := &HealthCheckRecord{
				NodeID:    "node-1",
				Timestamp: time.Now(),
				Results: []*pb.HealthCheckResult{
					{
						CheckName: "nvml",
						Status:    tt.healthCheckStatus,
						Message:   "test",
					},
				},
			}
			db.RecordHealthCheck(ctx, healthRecord)

			// Verify results
			node, _ := db.GetNode(ctx, "node-1")
			if node.Status != tt.expectedNodeStatus {
				t.Errorf("Expected node status %v, got %v", tt.expectedNodeStatus, node.Status)
			}
			if node.HealthStatus != tt.expectedHealth {
				t.Errorf("Expected health status %v, got %v", tt.expectedHealth, node.HealthStatus)
			}
		})
	}
}

func TestInMemDB_HealthStatusTransitionSequence(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Register active node
	record := &NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	}
	db.RegisterNode(ctx, record)

	// Sequence: ACTIVE -> UNHEALTHY -> DEGRADED (stays UNHEALTHY) -> HEALTHY (becomes ACTIVE)

	// Step 1: Become unhealthy
	db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY},
		},
	})
	node, _ := db.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Fatalf("Step 1: Expected UNHEALTHY, got %v", node.Status)
	}

	// Step 2: Report degraded - should stay unhealthy
	db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_DEGRADED},
		},
	})
	node, _ = db.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Fatalf("Step 2: Expected UNHEALTHY (degraded doesn't recover), got %v", node.Status)
	}
	if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_DEGRADED {
		t.Fatalf("Step 2: Expected health DEGRADED, got %v", node.HealthStatus)
	}

	// Step 3: Report healthy - should become active
	db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		},
	})
	node, _ = db.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Fatalf("Step 3: Expected ACTIVE, got %v", node.Status)
	}
	if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_HEALTHY {
		t.Fatalf("Step 3: Expected health HEALTHY, got %v", node.HealthStatus)
	}
}

func TestInMemDB_MixedHealthCheckResults(t *testing.T) {
	tests := []struct {
		name           string
		results        []pb.HealthStatus
		expectedHealth pb.HealthStatus
	}{
		{
			name:           "all_healthy",
			results:        []pb.HealthStatus{pb.HealthStatus_HEALTH_STATUS_HEALTHY, pb.HealthStatus_HEALTH_STATUS_HEALTHY},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:           "one_degraded_makes_degraded",
			results:        []pb.HealthStatus{pb.HealthStatus_HEALTH_STATUS_HEALTHY, pb.HealthStatus_HEALTH_STATUS_DEGRADED},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name:           "one_unhealthy_makes_unhealthy",
			results:        []pb.HealthStatus{pb.HealthStatus_HEALTH_STATUS_HEALTHY, pb.HealthStatus_HEALTH_STATUS_UNHEALTHY},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:           "unhealthy_overrides_degraded",
			results:        []pb.HealthStatus{pb.HealthStatus_HEALTH_STATUS_DEGRADED, pb.HealthStatus_HEALTH_STATUS_UNHEALTHY},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:           "multiple_degraded",
			results:        []pb.HealthStatus{pb.HealthStatus_HEALTH_STATUS_DEGRADED, pb.HealthStatus_HEALTH_STATUS_DEGRADED},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name: "mixed_all_three",
			results: []pb.HealthStatus{
				pb.HealthStatus_HEALTH_STATUS_HEALTHY,
				pb.HealthStatus_HEALTH_STATUS_DEGRADED,
				pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			},
			expectedHealth: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewInMemDB()
			ctx := context.Background()

			record := &NodeRecord{
				NodeID: "node-1",
				Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
			}
			db.RegisterNode(ctx, record)

			var results []*pb.HealthCheckResult
			for i, status := range tt.results {
				results = append(results, &pb.HealthCheckResult{
					CheckName: "check-" + string(rune('0'+i)),
					Status:    status,
				})
			}

			db.RecordHealthCheck(ctx, &HealthCheckRecord{
				NodeID:    "node-1",
				Timestamp: time.Now(),
				Results:   results,
			})

			node, _ := db.GetNode(ctx, "node-1")
			if node.HealthStatus != tt.expectedHealth {
				t.Errorf("Expected health %v, got %v", tt.expectedHealth, node.HealthStatus)
			}
		})
	}
}

func TestInMemDB_HealthCheckUpdatesTimestamp(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	record := &NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	}
	db.RegisterNode(ctx, record)

	ts1 := time.Now()
	db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: ts1,
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		},
	})

	node, _ := db.GetNode(ctx, "node-1")
	if !node.LastHealthCheck.Equal(ts1) {
		t.Errorf("Expected LastHealthCheck %v, got %v", ts1, node.LastHealthCheck)
	}

	ts2 := ts1.Add(time.Minute)
	db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "node-1",
		Timestamp: ts2,
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		},
	})

	node, _ = db.GetNode(ctx, "node-1")
	if !node.LastHealthCheck.Equal(ts2) {
		t.Errorf("Expected LastHealthCheck %v, got %v", ts2, node.LastHealthCheck)
	}
}

func TestInMemDB_HealthCheckUnknownNode(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Record health check for non-existent node - should not error
	err := db.RecordHealthCheck(ctx, &HealthCheckRecord{
		NodeID:    "unknown-node",
		Timestamp: time.Now(),
		Results: []*pb.HealthCheckResult{
			{CheckName: "nvml", Status: pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		},
	})

	if err != nil {
		t.Errorf("Expected no error for unknown node, got %v", err)
	}

	// Health check should still be stored
	latest, err := db.GetLatestHealthCheck(ctx, "unknown-node")
	if err != nil {
		t.Errorf("Expected health check to be stored, got error: %v", err)
	}
	if latest == nil {
		t.Error("Expected health check record, got nil")
	}
}

func TestInMemDB_BootstrapLogs(t *testing.T) {
	db := NewInMemDB()
	ctx := context.Background()

	// Record a bootstrap log
	log1 := &BootstrapLogRecord{
		ID:          "log-1",
		NodeID:      "node-1",
		InstanceID:  "instance-1",
		Pool:        "gpu-pool",
		StartedAt:   time.Now(),
		Duration:    5 * time.Second,
		SSHWaitTime: 2 * time.Second,
		Success:     true,
		Commands: []BootstrapCommandLog{
			{Command: "echo hello", Stdout: "hello", ExitCode: 0, Duration: time.Second},
		},
	}

	if err := db.RecordBootstrapLog(ctx, log1); err != nil {
		t.Fatalf("RecordBootstrapLog failed: %v", err)
	}

	// Record a failed bootstrap for same node
	log2 := &BootstrapLogRecord{
		ID:         "log-2",
		NodeID:     "node-1",
		InstanceID: "instance-1",
		Pool:       "gpu-pool",
		StartedAt:  time.Now(),
		Duration:   10 * time.Second,
		Success:    false,
		Error:      "connection refused",
	}

	if err := db.RecordBootstrapLog(ctx, log2); err != nil {
		t.Fatalf("RecordBootstrapLog failed: %v", err)
	}

	// Get logs for node
	logs, err := db.GetBootstrapLogs(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetBootstrapLogs failed: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("Expected 2 logs, got %d", len(logs))
	}
	if logs[0].ID != "log-1" {
		t.Errorf("Expected first log ID 'log-1', got %q", logs[0].ID)
	}
	if logs[1].Success {
		t.Error("Expected second log to be failure")
	}

	// List by pool
	poolLogs, err := db.ListBootstrapLogsByPool(ctx, "gpu-pool", 10)
	if err != nil {
		t.Fatalf("ListBootstrapLogsByPool failed: %v", err)
	}
	if len(poolLogs) != 2 {
		t.Errorf("Expected 2 pool logs, got %d", len(poolLogs))
	}

	// Unknown node returns empty
	unknown, err := db.GetBootstrapLogs(ctx, "unknown")
	if err != nil {
		t.Fatalf("GetBootstrapLogs for unknown failed: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("Expected 0 logs for unknown node, got %d", len(unknown))
	}
}

