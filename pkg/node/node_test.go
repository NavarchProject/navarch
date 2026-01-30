package node

import (
	"context"
	"testing"

	"github.com/NavarchProject/navarch/pkg/gpu"
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
		injectableGPU := gpu.NewInjectable(4, "")
		cfg := Config{
			ControlPlaneAddr: "http://localhost:50051",
			NodeID:           "test-node",
			GPU:              injectableGPU,
		}

		n, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		if n.gpu != injectableGPU {
			t.Error("Expected custom GPU manager to be used")
		}
	})
}

func TestDetectGPUs(t *testing.T) {
	ctx := context.Background()
	injectableGPU := gpu.NewInjectable(2, "")

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              injectableGPU,
	}

	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := injectableGPU.Initialize(ctx); err != nil {
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
	injectableGPU := gpu.NewInjectable(2, "")

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              injectableGPU,
	}

	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := injectableGPU.Initialize(ctx); err != nil {
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

	t.Run("gpu_check_healthy", func(t *testing.T) {
		result := n.runGPUCheck(ctx)
		if result.CheckName != "gpu" {
			t.Errorf("Expected check name 'gpu', got %s", result.CheckName)
		}
		if result.Status != 1 { // HEALTH_STATUS_HEALTHY
			t.Errorf("Expected healthy status, got %d", result.Status)
		}
	})

	t.Run("health_event_check_healthy", func(t *testing.T) {
		result := n.runHealthEventCheck(ctx)
		if result.CheckName != "health_events" {
			t.Errorf("Expected check name 'health_events', got %s", result.CheckName)
		}
		if result.Status != 1 { // HEALTH_STATUS_HEALTHY
			t.Errorf("Expected healthy status, got %d", result.Status)
		}
	})

	t.Run("health_event_check_unhealthy_xid", func(t *testing.T) {
		injectableGPU.InjectXIDHealthEvent(0, 79, "Test XID error")

		result := n.runHealthEventCheck(ctx)
		if result.Status != 3 { // HEALTH_STATUS_UNHEALTHY
			t.Errorf("Expected unhealthy status, got %d", result.Status)
		}

		injectableGPU.ClearHealthEvents()
	})
}

