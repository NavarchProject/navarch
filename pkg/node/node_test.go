package node

import (
	"context"
	"strings"
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
		if !strings.Contains(results, "2 GPUs tested") {
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

func TestStateTransitions(t *testing.T) {
	t.Run("valid_transitions", func(t *testing.T) {
		// Test all valid transitions from the state graph
		tests := []struct {
			from NodeState
			to   NodeState
			want bool
		}{
			// From Active
			{StateActive, StateActive, true},
			{StateActive, StateCordoned, true},
			{StateActive, StateDraining, true},
			{StateActive, StateTerminating, true},
			// From Cordoned
			{StateCordoned, StateCordoned, true},
			{StateCordoned, StateActive, true}, // Can uncordon
			{StateCordoned, StateDraining, true},
			{StateCordoned, StateTerminating, true},
			// From Draining
			{StateDraining, StateDraining, true},
			{StateDraining, StateTerminating, true},
			{StateDraining, StateActive, false},    // Cannot go back
			{StateDraining, StateCordoned, false},  // Cannot go back
			// From Terminating (terminal state)
			{StateTerminating, StateTerminating, true},
			{StateTerminating, StateActive, false},
			{StateTerminating, StateCordoned, false},
			{StateTerminating, StateDraining, false},
		}

		for _, tt := range tests {
			name := tt.from.String() + "_to_" + tt.to.String()
			t.Run(name, func(t *testing.T) {
				got := canTransition(tt.from, tt.to)
				if got != tt.want {
					t.Errorf("canTransition(%s, %s) = %v, want %v",
						tt.from, tt.to, got, tt.want)
				}
			})
		}
	})
}

func TestInvalidStateTransitions(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	t.Run("cordon_from_draining", func(t *testing.T) {
		n, _ := New(cfg, nil)

		// First, put node in draining state
		drainCmd := &pb.NodeCommand{
			CommandId:  "cmd-drain",
			Type:       pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			Parameters: map[string]string{"timeout": "1s"},
		}
		if err := n.executeCommand(ctx, drainCmd); err != nil {
			t.Fatalf("drain command failed: %v", err)
		}

		if n.State() != StateDraining {
			t.Fatalf("expected state draining, got %s", n.State())
		}

		// Now try to cordon (should fail - can't go backwards)
		cordonCmd := &pb.NodeCommand{
			CommandId: "cmd-cordon",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		}
		err := n.executeCommand(ctx, cordonCmd)
		if err == nil {
			t.Error("expected error when cordoning a draining node")
		}

		var stateErr *ErrInvalidStateTransition
		if !isInvalidStateTransitionError(err, &stateErr) {
			t.Errorf("expected ErrInvalidStateTransition, got %T: %v", err, err)
		} else {
			if stateErr.From != StateDraining {
				t.Errorf("expected from=draining, got %s", stateErr.From)
			}
			if stateErr.To != StateCordoned {
				t.Errorf("expected to=cordoned, got %s", stateErr.To)
			}
		}
	})

	t.Run("cordon_from_terminating", func(t *testing.T) {
		n, _ := New(cfg, nil)

		// Put node in terminating state
		terminateCmd := &pb.NodeCommand{
			CommandId: "cmd-terminate",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}
		if err := n.executeCommand(ctx, terminateCmd); err != nil {
			t.Fatalf("terminate command failed: %v", err)
		}

		// Try to cordon (should fail)
		cordonCmd := &pb.NodeCommand{
			CommandId: "cmd-cordon",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		}
		err := n.executeCommand(ctx, cordonCmd)
		if err == nil {
			t.Error("expected error when cordoning a terminating node")
		}
	})

	t.Run("drain_from_terminating", func(t *testing.T) {
		n, _ := New(cfg, nil)

		// Put node in terminating state
		terminateCmd := &pb.NodeCommand{
			CommandId: "cmd-terminate",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}
		if err := n.executeCommand(ctx, terminateCmd); err != nil {
			t.Fatalf("terminate command failed: %v", err)
		}

		// Try to drain (should fail)
		drainCmd := &pb.NodeCommand{
			CommandId: "cmd-drain",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
		}
		err := n.executeCommand(ctx, drainCmd)
		if err == nil {
			t.Error("expected error when draining a terminating node")
		}
	})

	t.Run("diagnostic_from_terminating", func(t *testing.T) {
		n, _ := New(cfg, nil)
		if err := fakeGPU.Initialize(ctx); err != nil {
			t.Fatalf("GPU Initialize failed: %v", err)
		}
		n.gpu = fakeGPU

		// Put node in terminating state
		terminateCmd := &pb.NodeCommand{
			CommandId: "cmd-terminate",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}
		if err := n.executeCommand(ctx, terminateCmd); err != nil {
			t.Fatalf("terminate command failed: %v", err)
		}

		// Try to run diagnostic (should fail)
		diagCmd := &pb.NodeCommand{
			CommandId: "cmd-diag",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC,
		}
		err := n.executeCommand(ctx, diagCmd)
		if err == nil {
			t.Error("expected error when running diagnostic on terminating node")
		}
	})
}

func TestStateAccessors(t *testing.T) {
	ctx := context.Background()
	fakeGPU := gpu.NewFake(2)

	cfg := Config{
		ControlPlaneAddr: "http://localhost:50051",
		NodeID:           "test-node",
		GPU:              fakeGPU,
	}

	t.Run("initial_state", func(t *testing.T) {
		n, _ := New(cfg, nil)

		if n.State() != StateActive {
			t.Errorf("expected initial state active, got %s", n.State())
		}
		if n.IsCordoned() {
			t.Error("new node should not be cordoned")
		}
		if n.IsDraining() {
			t.Error("new node should not be draining")
		}
		if n.IsShuttingDown() {
			t.Error("new node should not be shutting down")
		}
	})

	t.Run("cordoned_state", func(t *testing.T) {
		n, _ := New(cfg, nil)

		cmd := &pb.NodeCommand{
			CommandId: "cmd-cordon",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
		}
		n.executeCommand(ctx, cmd)

		if n.State() != StateCordoned {
			t.Errorf("expected state cordoned, got %s", n.State())
		}
		if !n.IsCordoned() {
			t.Error("cordoned node should report IsCordoned=true")
		}
		if n.IsDraining() {
			t.Error("cordoned node should not be draining")
		}
		if n.IsShuttingDown() {
			t.Error("cordoned node should not be shutting down")
		}
	})

	t.Run("draining_state", func(t *testing.T) {
		n, _ := New(cfg, nil)

		cmd := &pb.NodeCommand{
			CommandId:  "cmd-drain",
			Type:       pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			Parameters: map[string]string{"timeout": "100ms"},
		}
		n.executeCommand(ctx, cmd)

		if n.State() != StateDraining {
			t.Errorf("expected state draining, got %s", n.State())
		}
		if !n.IsCordoned() {
			t.Error("draining node should report IsCordoned=true")
		}
		if !n.IsDraining() {
			t.Error("draining node should report IsDraining=true")
		}
		if n.IsShuttingDown() {
			t.Error("draining node should not be shutting down")
		}
	})

	t.Run("terminating_state", func(t *testing.T) {
		n, _ := New(cfg, nil)

		cmd := &pb.NodeCommand{
			CommandId: "cmd-terminate",
			Type:      pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE,
		}
		n.executeCommand(ctx, cmd)

		if n.State() != StateTerminating {
			t.Errorf("expected state terminating, got %s", n.State())
		}
		if !n.IsCordoned() {
			t.Error("terminating node should report IsCordoned=true")
		}
		if !n.IsDraining() {
			t.Error("terminating node should report IsDraining=true")
		}
		if !n.IsShuttingDown() {
			t.Error("terminating node should report IsShuttingDown=true")
		}
	})
}

func TestErrInvalidStateTransition(t *testing.T) {
	err := &ErrInvalidStateTransition{
		From:    StateDraining,
		To:      StateCordoned,
		Command: "cordon",
	}

	expected := "invalid state transition from draining to cordoned (command: cordon)"
	if err.Error() != expected {
		t.Errorf("error message = %q, want %q", err.Error(), expected)
	}
}

// isInvalidStateTransitionError checks if err is an ErrInvalidStateTransition
// and assigns it to target if so.
func isInvalidStateTransitionError(err error, target **ErrInvalidStateTransition) bool {
	if e, ok := err.(*ErrInvalidStateTransition); ok {
		*target = e
		return true
	}
	return false
}

