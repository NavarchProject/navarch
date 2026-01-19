package simulator

import (
	"testing"
	"time"
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
	node.InjectFailure(InjectedFailure{Type: "nvml_failure"})
	node.InjectFailure(InjectedFailure{Type: "boot_failure"})

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
	node.InjectFailure(InjectedFailure{Type: "nvml_failure"})
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
	node.InjectFailure(InjectedFailure{Type: "nvml_failure", GPUIndex: 2})
	node.InjectFailure(InjectedFailure{Type: "temperature", GPUIndex: 3})
	node.InjectFailure(InjectedFailure{Type: "boot_failure"})

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

func TestHash(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"node-1"},
		{"node-2"},
		{"test-node"},
		{""},
		{"a"},
	}

	// hash should be deterministic
	for _, tt := range tests {
		h1 := hash(tt.input)
		h2 := hash(tt.input)
		if h1 != h2 {
			t.Errorf("hash(%q) not deterministic: %d != %d", tt.input, h1, h2)
		}
		if h1 < 0 {
			t.Errorf("hash(%q) returned negative value: %d", tt.input, h1)
		}
	}

	// Different inputs should (usually) produce different hashes
	h1 := hash("node-1")
	h2 := hash("node-2")
	if h1 == h2 {
		t.Log("warning: hash collision between node-1 and node-2")
	}
}

func TestGenerateBootCheck(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	t.Run("healthy", func(t *testing.T) {
		result := node.generateBootCheck(nil)
		if result.CheckName != "boot" {
			t.Errorf("CheckName = %v, want boot", result.CheckName)
		}
		if result.Status.String() != "HEALTH_STATUS_HEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_HEALTHY", result.Status)
		}
	})

	t.Run("with boot failure", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "boot_failure", Message: "Failed to boot"},
		}
		result := node.generateBootCheck(failures)
		if result.Status.String() != "HEALTH_STATUS_UNHEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_UNHEALTHY", result.Status)
		}
		if result.Message != "Failed to boot" {
			t.Errorf("Message = %v, want 'Failed to boot'", result.Message)
		}
	})
}

func TestGenerateNvmlCheck(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	t.Run("healthy", func(t *testing.T) {
		result := node.generateNvmlCheck(nil)
		if result.CheckName != "nvml" {
			t.Errorf("CheckName = %v, want nvml", result.CheckName)
		}
		if result.Status.String() != "HEALTH_STATUS_HEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_HEALTHY", result.Status)
		}
	})

	t.Run("with nvml failure", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "nvml_failure", GPUIndex: 2, Message: "NVML init failed"},
		}
		result := node.generateNvmlCheck(failures)
		if result.Status.String() != "HEALTH_STATUS_UNHEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_UNHEALTHY", result.Status)
		}
		if result.Details["gpu_index"] != "2" {
			t.Errorf("Details[gpu_index] = %v, want 2", result.Details["gpu_index"])
		}
	})

	t.Run("with temperature failure", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "temperature", GPUIndex: 5, Message: "GPU overheating"},
		}
		result := node.generateNvmlCheck(failures)
		if result.Status.String() != "HEALTH_STATUS_UNHEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_UNHEALTHY", result.Status)
		}
	})
}

func TestGenerateXidCheck(t *testing.T) {
	spec := NodeSpec{ID: "test-node", GPUCount: 8}
	node := NewSimulatedNode(spec, "http://localhost:8080", nil)

	t.Run("healthy", func(t *testing.T) {
		result := node.generateXidCheck(nil)
		if result.CheckName != "xid" {
			t.Errorf("CheckName = %v, want xid", result.CheckName)
		}
		if result.Status.String() != "HEALTH_STATUS_HEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_HEALTHY", result.Status)
		}
	})

	t.Run("with fatal xid error", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "xid_error", XIDCode: 79, GPUIndex: 3},
		}
		result := node.generateXidCheck(failures)
		if result.Status.String() != "HEALTH_STATUS_UNHEALTHY" {
			t.Errorf("Status = %v, want HEALTH_STATUS_UNHEALTHY for fatal XID", result.Status)
		}
		if result.Details["xid_code"] != "79" {
			t.Errorf("Details[xid_code] = %v, want 79", result.Details["xid_code"])
		}
		if result.Details["fatal"] != "true" {
			t.Errorf("Details[fatal] = %v, want true", result.Details["fatal"])
		}
	})

	t.Run("with recoverable xid error", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "xid_error", XIDCode: 31, GPUIndex: 1},
		}
		result := node.generateXidCheck(failures)
		if result.Status.String() != "HEALTH_STATUS_DEGRADED" {
			t.Errorf("Status = %v, want HEALTH_STATUS_DEGRADED for recoverable XID", result.Status)
		}
		if result.Details["fatal"] != "false" {
			t.Errorf("Details[fatal] = %v, want false", result.Details["fatal"])
		}
	})

	t.Run("with unknown xid error", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "xid_error", XIDCode: 9999, GPUIndex: 0, Message: "Unknown error"},
		}
		result := node.generateXidCheck(failures)
		// Unknown XID codes should be treated as degraded (not fatal)
		if result.Status.String() != "HEALTH_STATUS_DEGRADED" {
			t.Errorf("Status = %v, want HEALTH_STATUS_DEGRADED for unknown XID", result.Status)
		}
	})

	t.Run("with custom message", func(t *testing.T) {
		failures := []InjectedFailure{
			{Type: "xid_error", XIDCode: 79, Message: "Custom message"},
		}
		result := node.generateXidCheck(failures)
		if result.Message != "Custom message" {
			t.Errorf("Message = %v, want 'Custom message'", result.Message)
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

