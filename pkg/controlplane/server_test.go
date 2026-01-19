package controlplane

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// TestRegisterNode tests the complete node registration flow
func TestRegisterNode(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		// Setup
		database := db.NewInMemDB()
		defer database.Close()

		cfg := DefaultConfig()
		srv := NewServer(database, cfg, nil)
		ctx := context.Background()

		// Node sends registration request
		req := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:       "node-1",
			Provider:     "gcp",
			Region:       "us-central1",
			Zone:         "us-central1-a",
			InstanceType: "a3-highgpu-8g",
			Gpus: []*pb.GPUInfo{
				{
					Index:       0,
					Uuid:        "GPU-12345",
					Name:        "NVIDIA H100",
					MemoryTotal: 80 * 1024 * 1024 * 1024, // 80GB
				},
			},
			Metadata: &pb.NodeMetadata{
				Hostname:   "node-1.example.com",
				InternalIp: "10.0.0.1",
			},
		})

		// Execute
		resp, err := srv.RegisterNode(ctx, req)

		// Verify response
		if err != nil {
			t.Fatalf("RegisterNode failed: %v", err)
		}
		if !resp.Msg.Success {
			t.Errorf("Registration not successful: %s", resp.Msg.Message)
		}
		if resp.Msg.Config == nil {
			t.Fatal("Expected config in response")
		}
		if resp.Msg.Config.HealthCheckIntervalSeconds != cfg.HealthCheckIntervalSeconds {
			t.Errorf("Expected health check interval %d, got %d",
				cfg.HealthCheckIntervalSeconds, resp.Msg.Config.HealthCheckIntervalSeconds)
		}

		// Verify database state
		node, err := database.GetNode(ctx, "node-1")
		if err != nil {
			t.Fatalf("Failed to get node from database: %v", err)
		}
		if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			t.Errorf("Expected node status ACTIVE, got %v", node.Status)
		}
		if len(node.GPUs) != 1 {
			t.Errorf("Expected 1 GPU, got %d", len(node.GPUs))
		}
	})

	t.Run("missing_node_id", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		req := connect.NewRequest(&pb.RegisterNodeRequest{
			Provider: "gcp",
		})

		resp, err := srv.RegisterNode(ctx, req)
		if err != nil {
			t.Fatalf("Expected error in response, not request error: %v", err)
		}
		if resp.Msg.Success {
			t.Error("Expected registration to fail with missing node_id")
		}
		if resp.Msg.Message != "node_id is required" {
			t.Errorf("Expected specific error message, got: %s", resp.Msg.Message)
		}
	})

	t.Run("duplicate_registration", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		req := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})

		// First registration
		resp1, _ := srv.RegisterNode(ctx, req)
		if !resp1.Msg.Success {
			t.Fatal("First registration should succeed")
		}

		// Second registration (update)
		resp2, _ := srv.RegisterNode(ctx, req)
		if !resp2.Msg.Success {
			t.Error("Duplicate registration should succeed (update)")
		}

		// Verify only one node exists
		nodes, _ := database.ListNodes(ctx)
		if len(nodes) != 1 {
			t.Errorf("Expected 1 node after duplicate registration, got %d", len(nodes))
		}
	})
}

// TestSendHeartbeat tests the heartbeat flow
func TestSendHeartbeat(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node first
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Send heartbeat
		now := time.Now()
		hbReq := connect.NewRequest(&pb.HeartbeatRequest{
			NodeId:    "node-1",
			Timestamp: timestamppb.New(now),
			Metrics: &pb.NodeMetrics{
				CpuUsagePercent:    45.5,
				MemoryUsagePercent: 60.0,
			},
		})

		resp, err := srv.SendHeartbeat(ctx, hbReq)
		if err != nil {
			t.Fatalf("SendHeartbeat failed: %v", err)
		}
		if !resp.Msg.Acknowledged {
			t.Error("Expected heartbeat to be acknowledged")
		}

		// Verify database state
		node, _ := database.GetNode(ctx, "node-1")
		if node.LastHeartbeat.Before(now) {
			t.Error("LastHeartbeat timestamp not updated")
		}
	})

	t.Run("unregistered_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		hbReq := connect.NewRequest(&pb.HeartbeatRequest{
			NodeId: "unknown-node",
		})

		resp, err := srv.SendHeartbeat(ctx, hbReq)
		if err != nil {
			t.Fatalf("Expected error in response, not request error: %v", err)
		}
		if resp.Msg.Acknowledged {
			t.Error("Should not acknowledge heartbeat from unregistered node")
		}
	})
}

// TestReportHealth tests health reporting and status updates
func TestReportHealth(t *testing.T) {
	t.Run("healthy_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Report healthy status
		healthReq := connect.NewRequest(&pb.ReportHealthRequest{
			NodeId: "node-1",
			Results: []*pb.HealthCheckResult{
				{
					CheckName: "boot",
					Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
					Message:   "Boot check passed",
					Timestamp: timestamppb.Now(),
				},
				{
					CheckName: "nvml",
					Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
					Message:   "All GPUs healthy",
					Timestamp: timestamppb.Now(),
				},
			},
		})

		resp, err := srv.ReportHealth(ctx, healthReq)
		if err != nil {
			t.Fatalf("ReportHealth failed: %v", err)
		}
		if !resp.Msg.Acknowledged {
			t.Error("Expected health report to be acknowledged")
		}
		if resp.Msg.NodeStatus != pb.NodeStatus_NODE_STATUS_ACTIVE {
			t.Errorf("Expected node status ACTIVE, got %v", resp.Msg.NodeStatus)
		}

		// Verify database state
		node, _ := database.GetNode(ctx, "node-1")
		if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_HEALTHY {
			t.Errorf("Expected health status HEALTHY, got %v", node.HealthStatus)
		}
	})

	t.Run("unhealthy_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Report unhealthy status
		healthReq := connect.NewRequest(&pb.ReportHealthRequest{
			NodeId: "node-1",
			Results: []*pb.HealthCheckResult{
				{
					CheckName: "nvml",
					Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
					Message:   "GPU XID error detected",
					Timestamp: timestamppb.Now(),
				},
			},
		})

		resp, err := srv.ReportHealth(ctx, healthReq)
		if err != nil {
			t.Fatalf("ReportHealth failed: %v", err)
		}
		if !resp.Msg.Acknowledged {
			t.Error("Expected health report to be acknowledged")
		}
		if resp.Msg.NodeStatus != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
			t.Errorf("Expected node status UNHEALTHY, got %v", resp.Msg.NodeStatus)
		}

		// Verify database state
		node, _ := database.GetNode(ctx, "node-1")
		if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
			t.Errorf("Expected node status UNHEALTHY, got %v", node.Status)
		}
	})

	t.Run("degraded_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Report degraded status
		healthReq := connect.NewRequest(&pb.ReportHealthRequest{
			NodeId: "node-1",
			Results: []*pb.HealthCheckResult{
				{
					CheckName: "nvml",
					Status:    pb.HealthStatus_HEALTH_STATUS_DEGRADED,
					Message:   "GPU temperature high",
					Timestamp: timestamppb.Now(),
				},
			},
		})

		resp, err := srv.ReportHealth(ctx, healthReq)
		if err != nil {
			t.Fatalf("ReportHealth failed: %v", err)
		}
		if !resp.Msg.Acknowledged {
			t.Error("Expected health report to be acknowledged")
		}

		// Verify health status is degraded
		node, _ := database.GetNode(ctx, "node-1")
		if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_DEGRADED {
			t.Errorf("Expected health status DEGRADED, got %v", node.HealthStatus)
		}
	})
}

// TestGetNodeCommands tests command issuance and execution
func TestGetNodeCommands(t *testing.T) {
	t.Run("cordon_command", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Issue cordon command
		cmdID, err := srv.IssueCommand(ctx, "node-1",
			pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			map[string]string{"reason": "maintenance"})
		if err != nil {
			t.Fatalf("IssueCommand failed: %v", err)
		}
		if cmdID == "" {
			t.Error("Expected command ID to be returned")
		}

		// Node polls for commands
		pollReq := connect.NewRequest(&pb.GetNodeCommandsRequest{
			NodeId: "node-1",
		})

		resp, err := srv.GetNodeCommands(ctx, pollReq)
		if err != nil {
			t.Fatalf("GetNodeCommands failed: %v", err)
		}
		if len(resp.Msg.Commands) != 1 {
			t.Fatalf("Expected 1 command, got %d", len(resp.Msg.Commands))
		}

		cmd := resp.Msg.Commands[0]
		if cmd.Type != pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON {
			t.Errorf("Expected CORDON command, got %v", cmd.Type)
		}
		if cmd.Parameters["reason"] != "maintenance" {
			t.Error("Expected reason parameter to be passed")
		}

		// Second poll should return no commands (already acknowledged)
		resp2, _ := srv.GetNodeCommands(ctx, pollReq)
		if len(resp2.Msg.Commands) != 0 {
			t.Error("Expected no commands on second poll (already acknowledged)")
		}
	})

	t.Run("multiple_commands", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-1",
			Provider: "gcp",
		})
		srv.RegisterNode(ctx, regReq)

		// Issue multiple commands
		srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON, nil)
		srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN, nil)

		// Poll should return all pending commands
		pollReq := connect.NewRequest(&pb.GetNodeCommandsRequest{
			NodeId: "node-1",
		})
		resp, _ := srv.GetNodeCommands(ctx, pollReq)

		if len(resp.Msg.Commands) != 2 {
			t.Errorf("Expected 2 commands, got %d", len(resp.Msg.Commands))
		}
	})
}

// TestNodeLifecycle tests the complete node lifecycle
func TestNodeLifecycle(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()
	srv := NewServer(database, DefaultConfig(), nil)
	ctx := context.Background()

	// 1. Registration
	regReq := connect.NewRequest(&pb.RegisterNodeRequest{
		NodeId:   "node-1",
		Provider: "gcp",
	})
	regResp, _ := srv.RegisterNode(ctx, regReq)
	if !regResp.Msg.Success {
		t.Fatal("Registration failed")
	}

	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Error("Expected initial status ACTIVE")
	}

	// 2. Normal operation - heartbeat
	hbReq := connect.NewRequest(&pb.HeartbeatRequest{
		NodeId: "node-1",
	})
	srv.SendHeartbeat(ctx, hbReq)

	// 3. Health degradation
	healthReq := connect.NewRequest(&pb.ReportHealthRequest{
		NodeId: "node-1",
		Results: []*pb.HealthCheckResult{
			{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				Message:   "GPU failure",
			},
		},
	})
	srv.ReportHealth(ctx, healthReq)

	node, _ = database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Error("Expected status to change to UNHEALTHY")
	}

	// 4. Cordoning
	srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON, nil)
	database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_CORDONED)

	node, _ = database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_CORDONED {
		t.Error("Expected status CORDONED")
	}

	// 5. Draining
	srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN, nil)
	database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_DRAINING)

	// 6. Termination
	srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE, nil)
	database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_TERMINATED)

	node, _ = database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_TERMINATED {
		t.Error("Expected final status TERMINATED")
	}
}

// TestMultiNodeManagement tests multi-node fleet scenarios
func TestMultiNodeManagement(t *testing.T) {
	t.Run("concurrent_registration", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register multiple nodes concurrently
		done := make(chan bool, 3)
		for i := 1; i <= 3; i++ {
			nodeID := "node-" + string(rune('0'+i))
			go func(id string) {
				req := connect.NewRequest(&pb.RegisterNodeRequest{
					NodeId:   id,
					Provider: "gcp",
				})
				srv.RegisterNode(ctx, req)
				done <- true
			}(nodeID)
		}

		// Wait for all registrations
		for i := 0; i < 3; i++ {
			<-done
		}

		// Verify all nodes registered
		nodes, _ := srv.ListNodes(ctx)
		if len(nodes) != 3 {
			t.Errorf("Expected 3 nodes, got %d", len(nodes))
		}
	})

	t.Run("independent_node_states", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register two nodes
		for i := 1; i <= 2; i++ {
			nodeID := "node-" + string(rune('0'+i))
			req := connect.NewRequest(&pb.RegisterNodeRequest{
				NodeId:   nodeID,
				Provider: "gcp",
			})
			srv.RegisterNode(ctx, req)
		}

		// Mark node-1 as unhealthy
		healthReq := connect.NewRequest(&pb.ReportHealthRequest{
			NodeId: "node-1",
			Results: []*pb.HealthCheckResult{
				{
					CheckName: "nvml",
					Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				},
			},
		})
		srv.ReportHealth(ctx, healthReq)

		// Verify node-1 is unhealthy but node-2 is still active
		node1, _ := database.GetNode(ctx, "node-1")
		node2, _ := database.GetNode(ctx, "node-2")

		if node1.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
			t.Error("Expected node-1 to be UNHEALTHY")
		}
		if node2.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			t.Error("Expected node-2 to remain ACTIVE")
		}
	})

	t.Run("targeted_commands", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register two nodes
		for i := 1; i <= 2; i++ {
			nodeID := "node-" + string(rune('0'+i))
			req := connect.NewRequest(&pb.RegisterNodeRequest{
				NodeId:   nodeID,
				Provider: "gcp",
			})
			srv.RegisterNode(ctx, req)
		}

		// Issue command only to node-1
		srv.IssueCommand(ctx, "node-1", pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON, nil)

		// Node-1 should see command
		req1 := connect.NewRequest(&pb.GetNodeCommandsRequest{NodeId: "node-1"})
		resp1, _ := srv.GetNodeCommands(ctx, req1)
		if len(resp1.Msg.Commands) != 1 {
			t.Error("Expected node-1 to receive command")
		}

		// Node-2 should not see command
		req2 := connect.NewRequest(&pb.GetNodeCommandsRequest{NodeId: "node-2"})
		resp2, _ := srv.GetNodeCommands(ctx, req2)
		if len(resp2.Msg.Commands) != 0 {
			t.Error("Expected node-2 to receive no commands")
		}
	})
}
