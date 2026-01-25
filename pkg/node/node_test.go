package node

import (
	"context"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/gpu"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestNew(t *testing.T) {
	t.Run("valid_config", func(t *testing.T) {
		cfg := Config{
			ControlPlaneAddr: "http://localhost:50051",
			NodeID:           "test-node",
			Provider:         "gcp",
		}

		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.config.NodeID != "test-node" {
			t.Errorf("Expected NodeID test-node, got %s", n.config.NodeID)
		}

		if n.gpu == nil {
			t.Error("Expected GPU manager to be initialized")
		}
	})

	t.Run("missing_control_plane_addr", func(t *testing.T) {
		cfg := Config{
			NodeID: "test-node",
		}

		_, err := New(cfg, nil)
		if err == nil {
			t.Error("Expected error for missing control plane address")
		}
	})

	t.Run("missing_node_id", func(t *testing.T) {
		cfg := Config{
			ControlPlaneAddr: "http://localhost:50051",
		}

		_, err := New(cfg, nil)
		if err == nil {
			t.Error("Expected error for missing node ID")
		}
	})

	t.Run("custom_gpu_manager", func(t *testing.T) {
		fakeGPU := gpu.NewFake(4)
		cfg := Config{
			ControlPlaneAddr: "http://localhost:50051",
			NodeID:           "test-node",
			GPU:              fakeGPU,
		}

		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.gpu != fakeGPU {
			t.Error("Expected custom GPU manager to be used")
		}
	})
}

func TestDetectGPUs(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := fakeGPU.Initialize(ctx); err != nil {
		t.Fatalf("GPU Initialize failed: %v", err)
	}

	gpus, err := n.detectGPUs(ctx)
	if err != nil {
		t.Fatalf("detectGPUs failed: %v", err)
	}

	if len(gpus) != 2 {
		t.Errorf("Expected 2 GPUs, got %d", len(gpus))
	}

	if gpus[0].Index != 0 {
		t.Errorf("Expected GPU index 0, got %d", gpus[0].Index)
	}

	if gpus[0].Uuid == "" {
		t.Error("Expected non-empty UUID")
	}

	if gpus[0].Name == "" {
		t.Error("Expected non-empty Name")
	}
}

func TestHealthChecks(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := fakeGPU.Initialize(ctx); err != nil {
		t.Fatalf("GPU Initialize failed: %v", err)
	}

	t.Run("boot_check_healthy", func(t *testing.T) {
		result := n.runBootCheck(ctx)
		if result.CheckName != "boot" {
			t.Errorf("Expected check name 'boot', got %s", result.CheckName)
		}
		if result.Status != 1 { // HEALTH_STATUS_HEALTHY
			t.Errorf("Expected healthy status, got %d", result.Status)
		}
	})

	t.Run("nvml_check_healthy", func(t *testing.T) {
		result := n.runNVMLCheck(ctx)
		if result.CheckName != "nvml" {
			t.Errorf("Expected check name 'nvml', got %s", result.CheckName)
		}
		if result.Status != 1 { // HEALTH_STATUS_HEALTHY
			t.Errorf("Expected healthy status, got %d", result.Status)
		}
	})

	t.Run("xid_check_healthy", func(t *testing.T) {
		result := n.runXIDCheck(ctx)
		if result.CheckName != "xid" {
			t.Errorf("Expected check name 'xid', got %s", result.CheckName)
		}
		if result.Status != 1 { // HEALTH_STATUS_HEALTHY
			t.Errorf("Expected healthy status, got %d", result.Status)
		}
	})

	t.Run("xid_check_unhealthy", func(t *testing.T) {
		fakeGPU.InjectXIDError("GPU-0", 79, "Test XID error")

		result := n.runXIDCheck(ctx)
		if result.Status != 3 { // HEALTH_STATUS_UNHEALTHY
			t.Errorf("Expected unhealthy status, got %d", result.Status)
		}

		fakeGPU.ClearXIDErrors()
	})
}

func TestCommandExecution(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	t.Run("cordon_command", func(t *testing.T) {
		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.IsCordoned() {
			t.Error("Node should not be cordoned initially")
		}

		cmd := &pb.NodeCommand{
			CommandId: "cmd-1",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			Parameters: map[string]string{
				"reason": "maintenance",
			},
		}

		err = n.executeCommand(ctx, cmd)
		if err != nil {
			t.Fatalf("executeCommand failed: %v", err)
		}

		if !n.IsCordoned() {
			t.Error("Node should be cordoned after cordon command")
		}

		// Executing cordon again should be idempotent
		err = n.executeCommand(ctx, cmd)
		if err != nil {
			t.Fatalf("second cordon command failed: %v", err)
		}
	})

	t.Run("drain_command", func(t *testing.T) {
		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.IsDraining() {
			t.Error("Node should not be draining initially")
		}

		cmd := &pb.NodeCommand{
			CommandId: "cmd-2",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			Parameters: map[string]string{
				"timeout": "1s",
			},
		}

		err = n.executeCommand(ctx, cmd)
		if err != nil {
			t.Fatalf("executeCommand failed: %v", err)
		}

		if !n.IsDraining() {
			t.Error("Node should be draining after drain command")
		}

		if !n.IsCordoned() {
			t.Error("Node should be cordoned when draining")
		}

		// Wait for drain to complete
		time.Sleep(2 * time.Second)
	})

	t.Run("diagnostic_command", func(t *testing.T) {
		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if err := fakeGPU.Initialize(ctx); err != nil {
			t.Fatalf("GPU Initialize failed: %v", err)
		}
		n.gpu = fakeGPU

		cmd := &pb.NodeCommand{
			CommandId: "cmd-3",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC,
			Parameters: map[string]string{
				"test_type": "quick",
			},
		}

		err = n.executeCommand(ctx, cmd)
		if err != nil {
			t.Fatalf("executeCommand failed: %v", err)
		}

		// Diagnostic runs async, just verify it doesn't error
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("terminate_command", func(t *testing.T) {
		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.IsShuttingDown() {
			t.Error("Node should not be shutting down initially")
		}

		cmd := &pb.NodeCommand{
			CommandId: "cmd-4",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}

		err = n.executeCommand(ctx, cmd)
		if err != nil {
			t.Fatalf("executeCommand failed: %v", err)
		}

		if !n.IsShuttingDown() {
			t.Error("Node should be shutting down after terminate command")
		}

		if !n.IsCordoned() {
			t.Error("Node should be cordoned when terminating")
		}

		if !n.IsDraining() {
			t.Error("Node should be draining when terminating")
		}

		// Verify shutdown channel is closed
		select {
		case <-n.ShutdownCh():
			// Expected
		default:
			t.Error("Shutdown channel should be closed")
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		cmd := &pb.NodeCommand{
			CommandId: "cmd-5",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_UNKNOWN,
		}

		err = n.executeCommand(ctx, cmd)
		if err == nil {
			t.Error("Expected error for unknown command type")
		}
	})
}

func TestRunDiagnostics(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := fakeGPU.Initialize(ctx); err != nil {
		t.Fatalf("GPU Initialize failed: %v", err)
	}
	n.gpu = fakeGPU

	t.Run("quick_diagnostic", func(t *testing.T) {
		results, err := n.runDiagnostics(ctx, "quick")
		if err != nil {
			t.Fatalf("runDiagnostics failed: %v", err)
		}

		if results == "" {
			t.Error("Expected non-empty diagnostic results")
		}

		// Should contain info about 2 GPUs
		if !contains(results, "2 GPUs tested") {
			t.Errorf("Expected results to mention 2 GPUs, got: %s", results)
		}
	})

	t.Run("diagnostic_with_context_cancel", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		_, err := n.runDiagnostics(cancelCtx, "quick")
		if err == nil {
			t.Error("Expected error for cancelled context")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

