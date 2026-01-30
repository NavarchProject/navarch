package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

		_, err := srv.RegisterNode(ctx, req)
		if err == nil {
			t.Fatal("Expected error for missing node_id")
		}
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("Expected InvalidArgument code, got: %v", connectErr.Code())
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

		_, err := srv.SendHeartbeat(ctx, hbReq)
		if err == nil {
			t.Fatal("Expected error for unregistered node")
		}
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("Expected NotFound code, got: %v", connectErr.Code())
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
		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			Parameters:  map[string]string{"reason": "maintenance"},
		})
		issueResp, err := srv.IssueCommand(ctx, issueReq)
		if err != nil {
			t.Fatalf("IssueCommand failed: %v", err)
		}
		if issueResp.Msg.CommandId == "" {
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
		srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		}))
		srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
		}))

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
	srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
		NodeId:      "node-1",
		CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
	}))
	database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_CORDONED)

	node, _ = database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_CORDONED {
		t.Error("Expected status CORDONED")
	}

	// 5. Draining
	srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
		NodeId:      "node-1",
		CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
	}))
	database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_DRAINING)

	// 6. Termination
	srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
		NodeId:      "node-1",
		CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
	}))
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
		listResp, _ := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{}))
		if len(listResp.Msg.Nodes) != 3 {
			t.Errorf("Expected 3 nodes, got %d", len(listResp.Msg.Nodes))
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
		srv.IssueCommand(ctx, connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		}))

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

// TestListNodes tests the ListNodes admin API
func TestListNodes(t *testing.T) {
	t.Run("empty_list", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 0 {
			t.Errorf("Expected 0 nodes, got %d", len(resp.Msg.Nodes))
		}
	})

	t.Run("list_all_nodes", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register 3 nodes
		for i := 1; i <= 3; i++ {
			nodeID := fmt.Sprintf("node-%d", i)
			req := connect.NewRequest(&pb.RegisterNodeRequest{
				NodeId:   nodeID,
				Provider: "gcp",
				Region:   "us-central1",
			})
			srv.RegisterNode(ctx, req)
		}

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 3 {
			t.Errorf("Expected 3 nodes, got %d", len(resp.Msg.Nodes))
		}
	})

	t.Run("filter_by_provider", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register nodes from different providers
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-gcp-1",
			Provider: "gcp",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-aws-1",
			Provider: "aws",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-gcp-2",
			Provider: "gcp",
		}))

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{
			Provider: "gcp",
		}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 2 {
			t.Errorf("Expected 2 GCP nodes, got %d", len(resp.Msg.Nodes))
		}
		for _, node := range resp.Msg.Nodes {
			if node.Provider != "gcp" {
				t.Errorf("Expected only GCP nodes, got %s", node.Provider)
			}
		}
	})

	t.Run("filter_by_region", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register nodes in different regions
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-us-1",
			Provider: "gcp",
			Region:   "us-central1",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-eu-1",
			Provider: "gcp",
			Region:   "europe-west1",
		}))

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{
			Region: "us-central1",
		}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 1 {
			t.Errorf("Expected 1 node in us-central1, got %d", len(resp.Msg.Nodes))
		}
		if resp.Msg.Nodes[0].Region != "us-central1" {
			t.Errorf("Expected us-central1, got %s", resp.Msg.Nodes[0].Region)
		}
	})

	t.Run("filter_by_status", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register nodes
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-2",
		}))

		// Mark one as unhealthy
		database.UpdateNodeStatus(ctx, "node-2", pb.NodeStatus_NODE_STATUS_UNHEALTHY)

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{
			Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
		}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 1 {
			t.Errorf("Expected 1 active node, got %d", len(resp.Msg.Nodes))
		}
		if resp.Msg.Nodes[0].Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			t.Errorf("Expected ACTIVE status, got %v", resp.Msg.Nodes[0].Status)
		}
	})

	t.Run("combined_filters", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register nodes with various attributes
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-gcp-us-1",
			Provider: "gcp",
			Region:   "us-central1",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-gcp-eu-1",
			Provider: "gcp",
			Region:   "europe-west1",
		}))
		srv.RegisterNode(ctx, connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId:   "node-aws-us-1",
			Provider: "aws",
			Region:   "us-central1",
		}))

		resp, err := srv.ListNodes(ctx, connect.NewRequest(&pb.ListNodesRequest{
			Provider: "gcp",
			Region:   "us-central1",
		}))
		if err != nil {
			t.Fatalf("ListNodes failed: %v", err)
		}
		if len(resp.Msg.Nodes) != 1 {
			t.Errorf("Expected 1 node matching filters, got %d", len(resp.Msg.Nodes))
		}
		if resp.Msg.Nodes[0].NodeId != "node-gcp-us-1" {
			t.Errorf("Expected node-gcp-us-1, got %s", resp.Msg.Nodes[0].NodeId)
		}
	})
}

// TestGetNode tests the GetNode admin API
func TestGetNode(t *testing.T) {
	t.Run("get_existing_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register a node with full details
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
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
					MemoryTotal: 85899345920,
				},
			},
			Metadata: &pb.NodeMetadata{
				Hostname:   "node-1.example.com",
				InternalIp: "10.0.0.1",
				Labels: map[string]string{
					"env": "production",
				},
			},
		})
		srv.RegisterNode(ctx, regReq)

		// Get the node
		getReq := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: "node-1",
		})
		resp, err := srv.GetNode(ctx, getReq)
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}

		// Verify all details
		node := resp.Msg.Node
		if node.NodeId != "node-1" {
			t.Errorf("Expected node-1, got %s", node.NodeId)
		}
		if node.Provider != "gcp" {
			t.Errorf("Expected gcp, got %s", node.Provider)
		}
		if node.Region != "us-central1" {
			t.Errorf("Expected us-central1, got %s", node.Region)
		}
		if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			t.Errorf("Expected ACTIVE status, got %v", node.Status)
		}
		if len(node.Gpus) != 1 {
			t.Errorf("Expected 1 GPU, got %d", len(node.Gpus))
		}
		if node.Metadata.Hostname != "node-1.example.com" {
			t.Errorf("Expected hostname node-1.example.com, got %s", node.Metadata.Hostname)
		}
	})

	t.Run("node_not_found", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		getReq := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: "nonexistent-node",
		})
		_, err := srv.GetNode(ctx, getReq)
		if err == nil {
			t.Fatal("Expected error for nonexistent node")
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("Expected NotFound code, got: %v", connectErr.Code())
		}
	})

	t.Run("missing_node_id", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		getReq := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: "",
		})
		_, err := srv.GetNode(ctx, getReq)
		if err == nil {
			t.Fatal("Expected error for missing node_id")
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("Expected InvalidArgument code, got: %v", connectErr.Code())
		}
	})

	t.Run("node_with_updated_status", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Update status to unhealthy
		database.UpdateNodeStatus(ctx, "node-1", pb.NodeStatus_NODE_STATUS_UNHEALTHY)

		// Get node and verify status
		getReq := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: "node-1",
		})
		resp, err := srv.GetNode(ctx, getReq)
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}
		if resp.Msg.Node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
			t.Errorf("Expected UNHEALTHY status, got %v", resp.Msg.Node.Status)
		}
	})
}

// TestIssueCommand tests the IssueCommand admin API
func TestIssueCommand(t *testing.T) {
	t.Run("issue_cordon_command", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Issue command
		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			Parameters: map[string]string{
				"reason": "maintenance",
			},
		})
		resp, err := srv.IssueCommand(ctx, issueReq)
		if err != nil {
			t.Fatalf("IssueCommand failed: %v", err)
		}

		// Verify response
		if resp.Msg.CommandId == "" {
			t.Error("Expected command_id to be returned")
		}
		if resp.Msg.IssuedAt == nil {
			t.Error("Expected issued_at timestamp")
		}

		// Verify command was stored
		commands, _ := database.GetPendingCommands(ctx, "node-1")
		if len(commands) != 1 {
			t.Fatalf("Expected 1 pending command, got %d", len(commands))
		}
		if commands[0].Type != pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON {
			t.Errorf("Expected CORDON command, got %v", commands[0].Type)
		}
		if commands[0].Parameters["reason"] != "maintenance" {
			t.Error("Expected reason parameter to be stored")
		}
	})

	t.Run("issue_to_nonexistent_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "nonexistent-node",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		})
		_, err := srv.IssueCommand(ctx, issueReq)
		if err == nil {
			t.Fatal("Expected error for nonexistent node")
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("Expected NotFound code, got: %v", connectErr.Code())
		}
	})

	t.Run("missing_node_id", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		})
		_, err := srv.IssueCommand(ctx, issueReq)
		if err == nil {
			t.Fatal("Expected error for missing node_id")
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("Expected InvalidArgument code, got: %v", connectErr.Code())
		}
	})

	t.Run("missing_command_type", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_UNKNOWN,
		})
		_, err := srv.IssueCommand(ctx, issueReq)
		if err == nil {
			t.Fatal("Expected error for missing command_type")
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) {
			t.Fatalf("Expected Connect error, got: %v", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("Expected InvalidArgument code, got: %v", connectErr.Code())
		}
	})

	t.Run("multiple_commands_to_same_node", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Issue multiple commands
		commandTypes := []pb.NodeCommandType{
			pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}

		for _, cmdType := range commandTypes {
			issueReq := connect.NewRequest(&pb.IssueCommandRequest{
				NodeId:      "node-1",
				CommandType: cmdType,
			})
			resp, err := srv.IssueCommand(ctx, issueReq)
			if err != nil {
				t.Fatalf("IssueCommand failed for %v: %v", cmdType, err)
			}
			if resp.Msg.CommandId == "" {
				t.Errorf("Expected command_id for %v", cmdType)
			}
		}

		// Verify all commands stored
		commands, _ := database.GetPendingCommands(ctx, "node-1")
		if len(commands) != 3 {
			t.Errorf("Expected 3 pending commands, got %d", len(commands))
		}
	})

	t.Run("command_with_no_parameters", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Issue command without parameters
		issueReq := connect.NewRequest(&pb.IssueCommandRequest{
			NodeId:      "node-1",
			CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC,
		})
		resp, err := srv.IssueCommand(ctx, issueReq)
		if err != nil {
			t.Fatalf("IssueCommand failed: %v", err)
		}
		if resp.Msg.CommandId == "" {
			t.Error("Expected command_id even without parameters")
		}
	})
}

// mockHealthObserver implements NodeHealthObserver for testing.
type mockHealthObserver struct {
	mu       sync.Mutex
	calls    []string
	callback func(nodeID string)
}

func (m *mockHealthObserver) OnNodeUnhealthy(ctx context.Context, nodeID string) {
	m.mu.Lock()
	m.calls = append(m.calls, nodeID)
	cb := m.callback
	m.mu.Unlock()
	if cb != nil {
		cb(nodeID)
	}
}

func (m *mockHealthObserver) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.calls))
	copy(result, m.calls)
	return result
}

func TestHealthObserver(t *testing.T) {
	t.Run("observer_called_on_transition_to_unhealthy", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		called := make(chan string, 1)
		observer := &mockHealthObserver{
			callback: func(nodeID string) {
				called <- nodeID
			},
		}
		srv.SetHealthObserver(observer)

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Report unhealthy status
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
		_, err := srv.ReportHealth(ctx, healthReq)
		if err != nil {
			t.Fatalf("ReportHealth failed: %v", err)
		}

		// Wait for observer to be called
		select {
		case nodeID := <-called:
			if nodeID != "node-1" {
				t.Errorf("Expected node-1, got %s", nodeID)
			}
		case <-time.After(time.Second):
			t.Error("Observer was not called within timeout")
		}
	})

	t.Run("observer_not_called_when_already_unhealthy", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		observer := &mockHealthObserver{}
		srv.SetHealthObserver(observer)

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// First unhealthy report
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

		// Wait a bit for async call
		time.Sleep(50 * time.Millisecond)

		// Should have one call
		if len(observer.getCalls()) != 1 {
			t.Fatalf("Expected 1 call after first unhealthy, got %d", len(observer.getCalls()))
		}

		// Second unhealthy report (already unhealthy)
		srv.ReportHealth(ctx, healthReq)

		// Wait a bit for potential async call
		time.Sleep(50 * time.Millisecond)

		// Should still have only one call
		if len(observer.getCalls()) != 1 {
			t.Errorf("Expected 1 call (no duplicate), got %d", len(observer.getCalls()))
		}
	})

	t.Run("observer_not_called_when_healthy", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()
		srv := NewServer(database, DefaultConfig(), nil)
		ctx := context.Background()

		observer := &mockHealthObserver{}
		srv.SetHealthObserver(observer)

		// Register node
		regReq := connect.NewRequest(&pb.RegisterNodeRequest{
			NodeId: "node-1",
		})
		srv.RegisterNode(ctx, regReq)

		// Report healthy status
		healthReq := connect.NewRequest(&pb.ReportHealthRequest{
			NodeId: "node-1",
			Results: []*pb.HealthCheckResult{
				{
					CheckName: "nvml",
					Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
					Message:   "All good",
				},
			},
		})
		srv.ReportHealth(ctx, healthReq)

		// Wait a bit for potential async call
		time.Sleep(50 * time.Millisecond)

		if len(observer.getCalls()) != 0 {
			t.Errorf("Expected no calls for healthy report, got %d", len(observer.getCalls()))
		}
	})
}
