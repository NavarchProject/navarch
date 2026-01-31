package simulator

import (
	"context"
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

func TestNewSimulatedNode(t *testing.T) {
	spec := NodeSpec{
		ID:           "test-node",
		Provider:     "gcp",
		Region:       "us-central1",
		Zone:         "us-central1-a",
		InstanceType: "a3-highgpu-8g",
		GPUCount:     8,
		GPUType:      "NVIDIA H100",
	}

	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	if node.ID() != "test-node" {
		t.Errorf("ID() = %v, want test-node", node.ID())
	}
	if node.running {
		t.Error("new node should not be running")
	}
	if len(node.failures) != 0 {
		t.Error("new node should have no failures")
	}
}

func TestSimulatedNode_InjectFailure(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	failure := InjectedFailure{
		Type:     "xid_error",
		XIDCode:  79,
		GPUIndex: 3,
		Message:  "GPU has fallen off the bus",
	}

	node.InjectFailure(failure)

	failures := node.GetFailures()
	if len(failures) != 1 {
		t.Fatalf("GetFailures() returned %d failures, want 1", len(failures))
	}

	f := failures[0]
	if f.Type != "xid_error" {
		t.Errorf("Type = %v, want xid_error", f.Type)
	}
	if f.XIDCode != 79 {
		t.Errorf("XIDCode = %v, want 79", f.XIDCode)
	}
	if f.GPUIndex != 3 {
		t.Errorf("GPUIndex = %v, want 3", f.GPUIndex)
	}
	if f.Since.IsZero() {
		t.Error("Since should be set")
	}
}

func TestSimulatedNode_ClearFailures(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79})
	node.InjectFailure(InjectedFailure{Type: "nvml_failure", Message: "test"})
	node.InjectFailure(InjectedFailure{Type: "boot_failure", Message: "test"})

	if len(node.GetFailures()) != 3 {
		t.Fatal("expected 3 failures before clear")
	}

	node.ClearFailures()

	if len(node.GetFailures()) != 0 {
		t.Errorf("GetFailures() returned %d failures after clear, want 0", len(node.GetFailures()))
	}
}

func TestSimulatedNode_RecoverFailure(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79})
	node.InjectFailure(InjectedFailure{Type: "nvml_failure", Message: "test"})
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 48})

	// Recover xid_error failures
	node.RecoverFailure("xid_error")

	failures := node.GetFailures()
	if len(failures) != 1 {
		t.Fatalf("GetFailures() returned %d failures, want 1", len(failures))
	}
	if failures[0].Type != "nvml_failure" {
		t.Errorf("remaining failure type = %v, want nvml_failure", failures[0].Type)
	}
}

func TestSimulatedNode_MultipleFailures(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	// Inject multiple failures of different types
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79, GPUIndex: 0})
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 48, GPUIndex: 1})
	node.InjectFailure(InjectedFailure{Type: "nvml_failure", GPUIndex: 2, Message: "test"})
	node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: 3})
	node.InjectFailure(InjectedFailure{Type: "boot_failure", Message: "test"})

	failures := node.GetFailures()
	if len(failures) != 5 {
		t.Errorf("GetFailures() returned %d failures, want 5", len(failures))
	}

	// Recover just xid_error
	node.RecoverFailure("xid_error")
	failures = node.GetFailures()
	if len(failures) != 3 {
		t.Errorf("after xid_error recovery: %d failures, want 3", len(failures))
	}

	// Recover nvml_failure
	node.RecoverFailure("nvml_failure")
	failures = node.GetFailures()
	if len(failures) != 2 {
		t.Errorf("after nvml_failure recovery: %d failures, want 2", len(failures))
	}

	// Clear all
	node.ClearFailures()
	failures = node.GetFailures()
	if len(failures) != 0 {
		t.Errorf("after clear: %d failures, want 0", len(failures))
	}
}

func TestSimulatedNode_ClearXIDErrors(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)
	ctx := context.Background()

	// Initialize GPU so we can query its state
	if err := node.gpu.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU: %v", err)
	}

	// Inject multiple XID errors on different GPUs
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79, GPUIndex: 0})
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 48, GPUIndex: 1})
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 31, GPUIndex: 2})

	// Inject a thermal event as well to verify selective clearing
	node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: 0})

	// Verify 4 health events exist (3 XID + 1 thermal)
	// Note: don't call CollectHealthEvents here as it clears the events
	if !node.gpu.HasActiveFailures() {
		t.Fatal("expected active failures after injection")
	}

	// Clear XID errors by recovering the xid_error failure type
	// This should clear all XID health events but leave thermal
	node.RecoverFailure("xid_error")

	// Collect health events - should only have thermal left
	events, _ := node.gpu.CollectHealthEvents(ctx)
	if len(events) != 1 {
		t.Errorf("expected 1 event after clearing XID errors, got %d", len(events))
	}
	if len(events) > 0 && events[0].EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL {
		t.Errorf("expected thermal event, got %v", events[0].EventType)
	}
}

func TestSimulatedNode_GetFailures_IsCopy(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79})

	failures := node.GetFailures()
	failures[0].XIDCode = 999 // Modify the copy

	// Original should be unchanged
	original := node.GetFailures()
	if original[0].XIDCode != 79 {
		t.Errorf("modifying returned slice affected original: XIDCode = %d, want 79", original[0].XIDCode)
	}
}

func TestSimulatedNode_Stop_BeforeStart(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	// Should not panic when stopping a node that was never started
	node.Stop()

	if node.running {
		t.Error("node should not be running after stop")
	}
}

func TestSimulatedNode_GPUFailureInjection(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	t.Run("xid_error injects to GPU", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79, GPUIndex: 3})
		if !node.GPU().HasActiveFailures() {
			t.Error("GPU should have active failures after XID injection")
		}
		node.ClearFailures()
	})

	t.Run("temperature injects to GPU", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: 2})
		if !node.GPU().HasActiveFailures() {
			t.Error("GPU should have active failures after temperature injection")
		}
		node.ClearFailures()
	})

	t.Run("nvml_failure injects to GPU", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "nvml_failure", Message: "NVML init failed"})
		if !node.GPU().HasActiveFailures() {
			t.Error("GPU should have active failures after NVML error injection")
		}
		node.ClearFailures()
	})

	t.Run("clear removes all GPU failures", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79})
		node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: 0})
		node.ClearFailures()
		if node.GPU().HasActiveFailures() {
			t.Error("GPU should have no active failures after clear")
		}
	})
}

func TestFailureTimestamp(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	before := time.Now()
	node.InjectFailure(InjectedFailure{Type: "xid_error", XIDCode: 79})
	after := time.Now()

	failures := node.GetFailures()
	if failures[0].Since.Before(before) || failures[0].Since.After(after) {
		t.Errorf("Since = %v, want between %v and %v", failures[0].Since, before, after)
	}
}

func TestSimulatedNode_Spec(t *testing.T) {
	spec := NodeSpec{
		ID:           "test-node",
		Provider:     "gcp",
		Region:       "us-central1",
		Zone:         "us-central1-a",
		InstanceType: "a3-highgpu-8g",
		GPUCount:     8,
		GPUType:      "NVIDIA H100",
		Labels:       map[string]string{"env": "test"},
	}

	node := NewSimulatedNode(spec, "http://localhost:8080", nil)
	returnedSpec := node.Spec()

	if returnedSpec.ID != spec.ID {
		t.Errorf("Spec().ID = %v, want %v", returnedSpec.ID, spec.ID)
	}
	if returnedSpec.Provider != spec.Provider {
		t.Errorf("Spec().Provider = %v, want %v", returnedSpec.Provider, spec.Provider)
	}
	if returnedSpec.GPUCount != spec.GPUCount {
		t.Errorf("Spec().GPUCount = %v, want %v", returnedSpec.GPUCount, spec.GPUCount)
	}
}

func TestSimulatedNode_AllFailureTypes(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	t.Run("boot_failure", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "boot_failure", Message: "GPU not detected"})
		if !node.GPU().HasActiveFailures() {
			t.Error("GPU should have active failures after boot_failure injection")
		}
		node.RecoverFailure("boot_failure")
		if node.GPU().HasActiveFailures() {
			t.Error("GPU should not have active failures after recovery")
		}
	})

	t.Run("device_error", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "device_error", GPUIndex: 2, Message: "device comm failure"})
		if !node.GPU().HasActiveFailures() {
			t.Error("GPU should have active failures after device_error injection")
		}
		node.RecoverFailure("device_error")
		if node.GPU().HasActiveFailures() {
			t.Error("GPU should not have active failures after recovery")
		}
	})

	t.Run("network failure (no GPU effect)", func(t *testing.T) {
		node.InjectFailure(InjectedFailure{Type: "network", Message: "connection lost"})
		failures := node.GetFailures()
		if len(failures) != 1 {
			t.Errorf("expected 1 failure recorded, got %d", len(failures))
		}
		if failures[0].Type != "network" {
			t.Errorf("failure type = %s, want network", failures[0].Type)
		}
		node.ClearFailures()
	})
}

func TestSimulatedNode_TemperatureAllGPUs(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 4}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	// Negative GPUIndex should affect all GPUs
	node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: -1})
	if !node.GPU().HasActiveFailures() {
		t.Error("GPU should have active failures after temperature injection on all GPUs")
	}

	node.RecoverFailure("temperature")
	if node.GPU().HasActiveFailures() {
		t.Error("GPU should not have active failures after recovery")
	}
}

func TestSimulatedNode_RecoverSpecificFailureTypes(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 4}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	testCases := []struct {
		name        string
		failureType string
		failure     InjectedFailure
	}{
		{"xid_error", "xid_error", InjectedFailure{Type: "xid_error", XIDCode: 79, GPUIndex: 0}},
		{"temperature", "temperature", InjectedFailure{Type: "temperature", GPUIndex: 1}},
		{"nvml_failure", "nvml_failure", InjectedFailure{Type: "nvml_failure", Message: "test"}},
		{"boot_failure", "boot_failure", InjectedFailure{Type: "boot_failure", Message: "test"}},
		{"device_error", "device_error", InjectedFailure{Type: "device_error", GPUIndex: 2, Message: "test"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node.InjectFailure(tc.failure)
			if len(node.GetFailures()) != 1 {
				t.Fatalf("expected 1 failure after injection, got %d", len(node.GetFailures()))
			}
			node.RecoverFailure(tc.failureType)
			if len(node.GetFailures()) != 0 {
				t.Errorf("expected 0 failures after recovery, got %d", len(node.GetFailures()))
			}
		})
	}
}

func TestSimulatedNode_ControlPlaneAddrSet(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 2}
	node := NewSimulatedNode(spec, "http://custom:9999", nil)

	returnedSpec := node.Spec()
	if returnedSpec.ControlPlaneAddr != "http://custom:9999" {
		t.Errorf("ControlPlaneAddr = %s, want http://custom:9999", returnedSpec.ControlPlaneAddr)
	}
}

func TestSimulatedNode_DefaultLogger(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 2}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	if node.logger == nil {
		t.Error("logger should not be nil when created with nil logger")
	}
}
