package gpu

import (
	"context"
	"testing"
)

func TestIsNVMLAvailable(t *testing.T) {
	// This test always runs - it just checks that the function doesn't panic
	available := IsNVMLAvailable()
	t.Logf("NVML available: %v", available)
}

func TestNVML_InitializeWithoutHardware(t *testing.T) {
	if IsNVMLAvailable() {
		t.Skip("Skipping - NVML is available, use TestNVML_WithHardware instead")
	}

	ctx := context.Background()
	nvml := NewNVML()

	// Should fail gracefully when NVML is not available
	err := nvml.Initialize(ctx)
	if err == nil {
		t.Error("Expected error when NVML is not available")
		nvml.Shutdown(ctx)
	}
}

func TestNVML_OperationsWithoutInitialize(t *testing.T) {
	ctx := context.Background()
	nvml := NewNVML()

	// All operations should fail when not initialized
	_, err := nvml.GetDeviceCount(ctx)
	if err == nil {
		t.Error("Expected error for GetDeviceCount without Initialize")
	}

	_, err = nvml.GetDeviceInfo(ctx, 0)
	if err == nil {
		t.Error("Expected error for GetDeviceInfo without Initialize")
	}

	_, err = nvml.GetDeviceHealth(ctx, 0)
	if err == nil {
		t.Error("Expected error for GetDeviceHealth without Initialize")
	}

	_, err = nvml.GetXIDErrors(ctx)
	if err == nil {
		t.Error("Expected error for GetXIDErrors without Initialize")
	}

	err = nvml.Shutdown(ctx)
	if err == nil {
		t.Error("Expected error for Shutdown without Initialize")
	}
}

// TestNVML_WithHardware runs comprehensive tests when actual NVIDIA GPUs are present.
// This test is skipped on machines without NVIDIA GPUs.
func TestNVML_WithHardware(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("Skipping - NVML not available (no NVIDIA GPU or driver)")
	}

	ctx := context.Background()
	nvml := NewNVML()

	// Initialize
	if err := nvml.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer nvml.Shutdown(ctx)

	// Double initialize should fail
	if err := nvml.Initialize(ctx); err == nil {
		t.Error("Expected error on double Initialize")
	}

	// Get device count
	count, err := nvml.GetDeviceCount(ctx)
	if err != nil {
		t.Fatalf("GetDeviceCount failed: %v", err)
	}
	t.Logf("Found %d GPU(s)", count)

	if count == 0 {
		t.Fatal("Expected at least one GPU when NVML is available")
	}

	// Test each device
	for i := 0; i < count; i++ {
		t.Run("device_info", func(t *testing.T) {
			info, err := nvml.GetDeviceInfo(ctx, i)
			if err != nil {
				t.Fatalf("GetDeviceInfo(%d) failed: %v", i, err)
			}

			if info.Index != i {
				t.Errorf("Expected index %d, got %d", i, info.Index)
			}

			if info.UUID == "" {
				t.Error("Expected non-empty UUID")
			}
			t.Logf("GPU %d UUID: %s", i, info.UUID)

			if info.Name == "" {
				t.Error("Expected non-empty Name")
			}
			t.Logf("GPU %d Name: %s", i, info.Name)

			if info.Memory == 0 {
				t.Error("Expected non-zero Memory")
			}
			t.Logf("GPU %d Memory: %d bytes", i, info.Memory)
		})

		t.Run("device_health", func(t *testing.T) {
			health, err := nvml.GetDeviceHealth(ctx, i)
			if err != nil {
				t.Fatalf("GetDeviceHealth(%d) failed: %v", i, err)
			}

			// Temperature should be reasonable (0-100°C)
			if health.Temperature < 0 || health.Temperature > 100 {
				t.Errorf("Unexpected temperature: %d", health.Temperature)
			}
			t.Logf("GPU %d Temperature: %d°C", i, health.Temperature)

			// Utilization should be 0-100%
			if health.GPUUtilization < 0 || health.GPUUtilization > 100 {
				t.Errorf("Unexpected GPU utilization: %d", health.GPUUtilization)
			}
			t.Logf("GPU %d Utilization: %d%%", i, health.GPUUtilization)

			// Memory used should not exceed total
			if health.MemoryUsed > health.MemoryTotal {
				t.Errorf("Memory used (%d) exceeds total (%d)", health.MemoryUsed, health.MemoryTotal)
			}
			t.Logf("GPU %d Memory: %d / %d bytes", i, health.MemoryUsed, health.MemoryTotal)
		})
	}

	// Test invalid device index
	_, err = nvml.GetDeviceInfo(ctx, count+100)
	if err == nil {
		t.Error("Expected error for invalid device index")
	}

	_, err = nvml.GetDeviceHealth(ctx, count+100)
	if err == nil {
		t.Error("Expected error for invalid device index")
	}

	// Test XID errors (should return empty slice, not error)
	xidErrors, err := nvml.GetXIDErrors(ctx)
	if err != nil {
		t.Fatalf("GetXIDErrors failed: %v", err)
	}
	t.Logf("XID errors: %d", len(xidErrors))
}

func TestNVML_ImplementsManager(t *testing.T) {
	// Compile-time check that NVML implements Manager interface
	var _ Manager = (*NVML)(nil)
}

