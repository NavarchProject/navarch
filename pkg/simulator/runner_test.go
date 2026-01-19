package simulator

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	scenario := &Scenario{
		Name:  "test",
		Fleet: []NodeSpec{{ID: "node-1"}},
	}

	runner := NewRunner(scenario)

	if runner.scenario != scenario {
		t.Error("scenario not set correctly")
	}
	if runner.controlPlaneAddr != "http://localhost:8080" {
		t.Errorf("default address = %v, want http://localhost:8080", runner.controlPlaneAddr)
	}
	if runner.nodes == nil {
		t.Error("nodes map not initialized")
	}
	if runner.waitForCancel {
		t.Error("waitForCancel should be false by default")
	}
}

func TestNewRunner_WithOptions(t *testing.T) {
	scenario := &Scenario{
		Name:  "test",
		Fleet: []NodeSpec{{ID: "node-1"}},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runner := NewRunner(scenario,
		WithLogger(logger),
		WithControlPlaneAddr("http://custom:9090"),
		WithWaitForCancel(),
	)

	if runner.controlPlaneAddr != "http://custom:9090" {
		t.Errorf("address = %v, want http://custom:9090", runner.controlPlaneAddr)
	}
	if !runner.waitForCancel {
		t.Error("waitForCancel should be true")
	}
}

func TestParseNodeStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"active", "NODE_STATUS_ACTIVE"},
		{"cordoned", "NODE_STATUS_CORDONED"},
		{"draining", "NODE_STATUS_DRAINING"},
		{"unhealthy", "NODE_STATUS_UNHEALTHY"},
		{"terminated", "NODE_STATUS_TERMINATED"},
		{"unknown", "NODE_STATUS_UNKNOWN"},
		{"invalid", "NODE_STATUS_UNKNOWN"},
		{"", "NODE_STATUS_UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseNodeStatus(tt.input)
			if result.String() != tt.expected {
				t.Errorf("parseNodeStatus(%q) = %v, want %v", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestParseHealthStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"healthy", "HEALTH_STATUS_HEALTHY"},
		{"degraded", "HEALTH_STATUS_DEGRADED"},
		{"unhealthy", "HEALTH_STATUS_UNHEALTHY"},
		{"unknown", "HEALTH_STATUS_UNKNOWN"},
		{"invalid", "HEALTH_STATUS_UNKNOWN"},
		{"", "HEALTH_STATUS_UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseHealthStatus(tt.input)
			if result.String() != tt.expected {
				t.Errorf("parseHealthStatus(%q) = %v, want %v", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestRunner_Run_SimpleScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "simple-test",
		Description: "Test basic fleet startup",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
			{At: Duration(2 * time.Second), Action: "log", Params: EventParams{LogMessage: "Fleet started"}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "test-node-1", Expected: "active"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunner_Run_WithFailureInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "failure-test",
		Description: "Test failure injection",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
			{At: Duration(2 * time.Second), Action: "inject_failure", Target: "test-node-1", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     79,
				GPUIndex:    0,
			}},
			{At: Duration(4 * time.Second), Action: "wait_for_status", Target: "test-node-1", Params: EventParams{
				ExpectedStatus: "unhealthy",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "health_status", Target: "test-node-1", Expected: "unhealthy"},
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

func TestRunner_Run_WithRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "recovery-test",
		Description: "Test failure recovery",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
			{At: Duration(2 * time.Second), Action: "inject_failure", Target: "test-node-1", Params: EventParams{
				FailureType: "xid_error",
				XIDCode:     31, // Recoverable XID
				GPUIndex:    0,
			}},
			{At: Duration(5 * time.Second), Action: "recover_failure", Target: "test-node-1", Params: EventParams{
				FailureType: "xid_error",
			}},
			{At: Duration(8 * time.Second), Action: "wait_for_status", Target: "test-node-1", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "health_status", Target: "test-node-1", Expected: "healthy"},
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

func TestRunner_Run_WithCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "command-test",
		Description: "Test command issuance",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
			{At: Duration(2 * time.Second), Action: "issue_command", Target: "test-node-1", Params: EventParams{
				CommandType: "cordon",
			}},
			{At: Duration(4 * time.Second), Action: "issue_command", Target: "test-node-1", Params: EventParams{
				CommandType: "drain",
			}},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunner_Run_MultipleNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "multi-node-test",
		Description: "Test multiple nodes",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
			{
				ID:           "test-node-2",
				Provider:     "aws",
				Region:       "us-east-1",
				Zone:         "us-east-1a",
				InstanceType: "p5.48xlarge",
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
			},
			{
				ID:           "test-node-3",
				Provider:     "gcp",
				Region:       "us-west1",
				Zone:         "us-west1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     4,
				GPUType:      "NVIDIA A100",
			},
		},
		Events: []Event{
			{At: Duration(0), Action: "start_fleet"},
			{At: Duration(3 * time.Second), Action: "wait_for_status", Target: "test-node-1", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			{At: Duration(3 * time.Second), Action: "wait_for_status", Target: "test-node-2", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
			{At: Duration(3 * time.Second), Action: "wait_for_status", Target: "test-node-3", Params: EventParams{
				ExpectedStatus: "active",
				Timeout:        Duration(10 * time.Second),
			}},
		},
		Assertions: []Assertion{
			{Type: "node_status", Target: "test-node-1", Expected: "active"},
			{Type: "node_status", Target: "test-node-2", Expected: "active"},
			{Type: "node_status", Target: "test-node-3", Expected: "active"},
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

func TestRunner_Run_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name:        "cancel-test",
		Description: "Test context cancellation",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
			{At: Duration(30 * time.Second), Action: "log", Params: EventParams{LogMessage: "Should not reach"}},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.Run(ctx)
	if err == nil {
		t.Fatal("Run() should return error on context cancellation")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Run() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunner_Run_UnknownAction(t *testing.T) {
	scenario := &Scenario{
		Name:  "unknown-action-test",
		Fleet: []NodeSpec{{ID: "test-node-1"}},
		Events: []Event{
			{At: Duration(0), Action: "unknown_action"},
		},
	}

	// Validation should catch this
	err := scenario.Validate()
	if err == nil {
		t.Fatal("Validate() should reject unknown action")
	}
}

func TestRunner_Run_WaitForCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scenario := &Scenario{
		Name: "wait-cancel-test",
		Fleet: []NodeSpec{
			{
				ID:           "test-node-1",
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
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runner := NewRunner(scenario, WithLogger(logger), WithWaitForCancel())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	// Give scenario time to complete
	time.Sleep(2 * time.Second)

	// Cancel context
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Run() error = %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
