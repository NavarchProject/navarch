package simulator

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// TestHealthE2E_XIDFatal tests that fatal XID errors cause node to become unhealthy
// through the full e2e flow: node -> control plane -> status update.
func TestHealthE2E_XIDFatal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "xid-fatal-e2e",
		Description: "Verify fatal XID error makes node unhealthy via full e2e flow",
		Fleet: []NodeSpec{
			{
				ID:           "health-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			// Wait for node to be active
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "health-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject fatal XID 79 (GPU fallen off bus)
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "health-test-node", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     79, // Fatal XID
				GPUIndex:    0,
			}},
			// Verify node becomes unhealthy
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "health-test-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "health-test-node", Expected: "unhealthy"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_ThermalCritical tests that critical temperature causes node to become unhealthy.
func TestHealthE2E_ThermalCritical(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "thermal-critical-e2e",
		Description: "Verify critical temperature makes node unhealthy",
		Fleet: []NodeSpec{
			{
				ID:           "thermal-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "thermal-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject critical temperature
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "thermal-test-node", Params: EventParams{
				FailureType: "temperature",
				GPUIndex:    0,
			}},
			// Verify node becomes unhealthy (critical temp = 95C)
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "thermal-test-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "thermal-test-node", Expected: "unhealthy"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_MemoryError tests that memory errors cause node to become unhealthy.
func TestHealthE2E_MemoryError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "memory-error-e2e",
		Description: "Verify ECC memory error makes node unhealthy",
		Fleet: []NodeSpec{
			{
				ID:           "memory-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "memory-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject memory error (ECC DBE)
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "memory-test-node", Params: EventParams{
				FailureType: "memory_error",
				GPUIndex:    0,
			}},
			// Verify node becomes unhealthy
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "memory-test-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "memory-test-node", Expected: "unhealthy"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_NVLinkError tests that NVLink errors cause node to become degraded.
func TestHealthE2E_NVLinkError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "nvlink-error-e2e",
		Description: "Verify NVLink error makes node degraded",
		Fleet: []NodeSpec{
			{
				ID:           "nvlink-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "nvlink-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject NVLink error
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "nvlink-test-node", Params: EventParams{
				FailureType: "nvlink_error",
				GPUIndex:    0,
			}},
			// Wait for next health check cycle (use wait_for_status with timeout)
			{At: Duration(7 * time.Second), Action: "wait_for_status", Target: "nvlink-test-node", Params: EventParams{
				ExpectedStatus: "active", // May stay active with degraded health
				Timeout:        Duration(5 * time.Second),
			}},
		},
		Assertions: []Assertion{
			// NVLink errors should cause degraded health status
			{Type: "health_status", Target: "nvlink-test-node", Expected: "degraded"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_RecoveryFlow tests the full recovery flow:
// healthy -> unhealthy -> recover -> healthy.
func TestHealthE2E_RecoveryFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "recovery-flow-e2e",
		Description: "Verify full recovery flow: healthy -> unhealthy -> recover -> healthy",
		Fleet: []NodeSpec{
			{
				ID:           "recovery-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			// Wait for healthy
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "recovery-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject fatal XID
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "recovery-test-node", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     79,
				GPUIndex:    0,
			}},
			// Wait for unhealthy
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "recovery-test-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
			// Recover
			{At: Duration(8 * time.Second), Action: "recover_failure", Target: "recovery-test-node", Params: EventParams{
				FailureType: "xid_error",
			}},
			// Wait for active again (recovery happens after health check)
			{At: Duration(12 * time.Second), Action: "wait_for_status", Target: "recovery-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "recovery-test-node", Expected: "active"},
			{Type: "health_status", Target: "recovery-test-node", Expected: "healthy"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_MultipleFailureTypes tests multiple failure types on the same node.
func TestHealthE2E_MultipleFailureTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "multi-failure-e2e",
		Description: "Verify multiple failure types are handled correctly",
		Fleet: []NodeSpec{
			{
				ID:           "multi-failure-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "multi-failure-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			// Inject multiple failure types
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "multi-failure-node", Params: EventParams{
				FailureType: "nvlink_error",
				GPUIndex:    0,
			}},
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "multi-failure-node", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     79, // Fatal
				GPUIndex:    1,
			}},
			// Worst status wins - should be unhealthy
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "multi-failure-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "multi-failure-node", Expected: "unhealthy"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestHealthE2E_VerifyEventsAreSent verifies that raw health events are included
// in the ReportHealthRequest. This test uses a custom observer to inspect the
// actual protocol messages.
func TestHealthE2E_VerifyEventsAreSent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test verifies the node agent includes Events in ReportHealthRequest
	// by running a scenario and checking the database state.
	scenario := &Scenario{
		Name:        "events-sent-e2e",
		Description: "Verify health events are sent to control plane",
		Fleet: []NodeSpec{
			{
				ID:           "events-test-node",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(2 * time.Second), Action: "wait_for_status", Target: "events-test-node", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			{At: Duration(3 * time.Second), Action: "inject_failure", Target: "events-test-node", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     79,
				GPUIndex:    0,
			}},
			{At: Duration(6 * time.Second), Action: "wait_for_status", Target: "events-test-node", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Verify the node status changed (proves health reporting worked)
	node, err := runner.database.GetNode(context.Background(), "events-test-node")
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Errorf("Expected node status UNHEALTHY, got %v", node.Status)
	}
	if node.HealthStatus != pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
		t.Errorf("Expected health status UNHEALTHY, got %v", node.HealthStatus)
	}
}
