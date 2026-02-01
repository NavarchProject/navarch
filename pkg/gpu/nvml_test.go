package gpu

import (
	"context"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

func TestNVML_NewNVML(t *testing.T) {
	m := NewNVML()
	if m == nil {
		t.Fatal("NewNVML() returned nil")
	}
	if m.initialized {
		t.Error("new NVML manager should not be initialized")
	}
}

func TestNVML_InitializeRequiresNVML(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if !m.initialized {
		t.Error("initialized = false after Initialize(), want true")
	}

	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestNVML_InitializeTwiceFails(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	if err := m.Initialize(ctx); err == nil {
		t.Error("second Initialize() should fail")
	}
}

func TestNVML_ShutdownWithoutInitializeFails(t *testing.T) {
	ctx := context.Background()
	m := NewNVML()

	if err := m.Shutdown(ctx); err == nil {
		t.Error("Shutdown() without Initialize() should fail")
	}
}

func TestNVML_OperationsRequireInitialization(t *testing.T) {
	ctx := context.Background()
	m := NewNVML()

	t.Run("GetDeviceCount", func(t *testing.T) {
		if _, err := m.GetDeviceCount(ctx); err == nil {
			t.Error("GetDeviceCount() should fail when not initialized")
		}
	})

	t.Run("GetDeviceInfo", func(t *testing.T) {
		if _, err := m.GetDeviceInfo(ctx, 0); err == nil {
			t.Error("GetDeviceInfo() should fail when not initialized")
		}
	})

	t.Run("GetDeviceHealth", func(t *testing.T) {
		if _, err := m.GetDeviceHealth(ctx, 0); err == nil {
			t.Error("GetDeviceHealth() should fail when not initialized")
		}
	})

	t.Run("CollectHealthEvents", func(t *testing.T) {
		if _, err := m.CollectHealthEvents(ctx); err == nil {
			t.Error("CollectHealthEvents() should fail when not initialized")
		}
	})
}

func TestNVML_GetDeviceCount(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	count, err := m.GetDeviceCount(ctx)
	if err != nil {
		t.Fatalf("GetDeviceCount() error = %v", err)
	}

	if count <= 0 {
		t.Errorf("GetDeviceCount() = %d, want > 0", count)
	}

	t.Logf("Detected %d GPU(s)", count)
}

func TestNVML_GetDeviceInfo(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	count, _ := m.GetDeviceCount(ctx)
	if count == 0 {
		t.Skip("No GPUs available")
	}

	t.Run("valid index", func(t *testing.T) {
		info, err := m.GetDeviceInfo(ctx, 0)
		if err != nil {
			t.Fatalf("GetDeviceInfo(0) error = %v", err)
		}

		if info.Index != 0 {
			t.Errorf("Index = %d, want 0", info.Index)
		}
		if info.UUID == "" {
			t.Error("UUID is empty")
		}
		if info.Name == "" {
			t.Error("Name is empty")
		}
		if info.PCIBusID == "" {
			t.Error("PCIBusID is empty")
		}
		if info.Memory == 0 {
			t.Error("Memory is 0")
		}

		t.Logf("GPU 0: %s (UUID: %s, Memory: %d MB)",
			info.Name, info.UUID, info.Memory/1024/1024)
	})

	t.Run("invalid index negative", func(t *testing.T) {
		if _, err := m.GetDeviceInfo(ctx, -1); err == nil {
			t.Error("GetDeviceInfo(-1) should fail")
		}
	})

	t.Run("invalid index too large", func(t *testing.T) {
		if _, err := m.GetDeviceInfo(ctx, 100); err == nil {
			t.Error("GetDeviceInfo(100) should fail")
		}
	})
}

func TestNVML_GetDeviceHealth(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	count, _ := m.GetDeviceCount(ctx)
	if count == 0 {
		t.Skip("No GPUs available")
	}

	t.Run("valid index", func(t *testing.T) {
		health, err := m.GetDeviceHealth(ctx, 0)
		if err != nil {
			t.Fatalf("GetDeviceHealth(0) error = %v", err)
		}

		// Temperature should be reasonable (0-150°C)
		if health.Temperature < 0 || health.Temperature > 150 {
			t.Errorf("Temperature = %d, want 0-150", health.Temperature)
		}

		// Power usage should be positive
		if health.PowerUsage < 0 {
			t.Errorf("PowerUsage = %f, want >= 0", health.PowerUsage)
		}

		// Memory should be consistent
		if health.MemoryUsed > health.MemoryTotal {
			t.Errorf("MemoryUsed (%d) > MemoryTotal (%d)", health.MemoryUsed, health.MemoryTotal)
		}

		// Utilization should be 0-100
		if health.GPUUtilization < 0 || health.GPUUtilization > 100 {
			t.Errorf("GPUUtilization = %d, want 0-100", health.GPUUtilization)
		}

		t.Logf("GPU 0 health: %d°C, %.1fW, %d%% util, %d/%d MB",
			health.Temperature, health.PowerUsage, health.GPUUtilization,
			health.MemoryUsed/1024/1024, health.MemoryTotal/1024/1024)
	})

	t.Run("invalid index", func(t *testing.T) {
		if _, err := m.GetDeviceHealth(ctx, -1); err == nil {
			t.Error("GetDeviceHealth(-1) should fail")
		}
		if _, err := m.GetDeviceHealth(ctx, 100); err == nil {
			t.Error("GetDeviceHealth(100) should fail")
		}
	})
}

func TestNVML_CollectHealthEvents(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	t.Run("returns empty slice for MVP", func(t *testing.T) {
		events, err := m.CollectHealthEvents(ctx)
		if err != nil {
			t.Fatalf("CollectHealthEvents() error = %v", err)
		}
		// MVP returns empty slice (XID collection is Task 2)
		if events == nil {
			t.Error("CollectHealthEvents() returned nil, want empty slice")
		}
	})

	t.Run("events cleared after collection", func(t *testing.T) {
		// Add a test event
		m.AddHealthEvent(NewXIDEvent(0, "test-uuid", 79, "test event"))

		events, _ := m.CollectHealthEvents(ctx)
		if len(events) != 1 {
			t.Errorf("len(events) = %d, want 1", len(events))
		}

		// Second collection should be empty
		events, _ = m.CollectHealthEvents(ctx)
		if len(events) != 0 {
			t.Errorf("len(events) after second collection = %d, want 0", len(events))
		}
	})
}

func TestNVML_AddHealthEvent(t *testing.T) {
	ctx := context.Background()
	m := NewNVML()

	// AddHealthEvent should work even without initialization (for external collectors)
	event := NewXIDEvent(0, "GPU-0", 79, "test")
	m.AddHealthEvent(event)

	// But CollectHealthEvents requires initialization
	if _, err := m.CollectHealthEvents(ctx); err == nil {
		t.Error("CollectHealthEvents() should fail when not initialized")
	}
}

func TestNVML_ImplementsManager(t *testing.T) {
	// Compile-time check that NVML implements Manager
	var _ Manager = (*NVML)(nil)
}

func TestIsNVMLAvailable(t *testing.T) {
	// Just verify it doesn't panic
	available := IsNVMLAvailable()
	t.Logf("NVML available: %v", available)
}

func TestIsNVMLAvailable_Idempotent(t *testing.T) {
	// Calling IsNVMLAvailable multiple times should be safe
	for i := 0; i < 3; i++ {
		IsNVMLAvailable()
	}
}

func TestNVML_InitShutdownCycle(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()

	// Test multiple init/shutdown cycles
	for i := 0; i < 3; i++ {
		m := NewNVML()

		if err := m.Initialize(ctx); err != nil {
			t.Fatalf("cycle %d: Initialize() error = %v", i, err)
		}

		count, err := m.GetDeviceCount(ctx)
		if err != nil {
			t.Fatalf("cycle %d: GetDeviceCount() error = %v", i, err)
		}
		if count == 0 {
			t.Fatalf("cycle %d: GetDeviceCount() = 0", i)
		}

		if err := m.Shutdown(ctx); err != nil {
			t.Fatalf("cycle %d: Shutdown() error = %v", i, err)
		}
	}
}

func TestNVML_AllDevices(t *testing.T) {
	if !IsNVMLAvailable() {
		t.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	count, _ := m.GetDeviceCount(ctx)
	t.Logf("Testing %d GPU(s)", count)

	for i := 0; i < count; i++ {
		info, err := m.GetDeviceInfo(ctx, i)
		if err != nil {
			t.Errorf("GetDeviceInfo(%d) error = %v", i, err)
			continue
		}

		health, err := m.GetDeviceHealth(ctx, i)
		if err != nil {
			t.Errorf("GetDeviceHealth(%d) error = %v", i, err)
			continue
		}

		t.Logf("GPU %d: %s, %d°C, %.1fW, %d%% util",
			i, info.Name, health.Temperature, health.PowerUsage, health.GPUUtilization)
	}
}

// Benchmark tests for NVML operations
func BenchmarkNVML_GetDeviceHealth(b *testing.B) {
	if !IsNVMLAvailable() {
		b.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		b.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.GetDeviceHealth(ctx, 0)
	}
}

func BenchmarkNVML_GetDeviceInfo(b *testing.B) {
	if !IsNVMLAvailable() {
		b.Skip("NVML not available")
	}

	ctx := context.Background()
	m := NewNVML()

	if err := m.Initialize(ctx); err != nil {
		b.Fatalf("Initialize() error = %v", err)
	}
	defer m.Shutdown(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.GetDeviceInfo(ctx, 0)
	}
}

// TestNVML_ErrorMessages verifies error messages are informative
func TestNVML_ErrorMessages(t *testing.T) {
	// Test that nvmlError returns a meaningful string
	ret := nvml.ERROR_NOT_FOUND
	errStr := nvmlError(ret)
	if errStr == "" {
		t.Error("nvmlError() returned empty string")
	}
	t.Logf("NVML error string for NOT_FOUND: %q", errStr)
}
