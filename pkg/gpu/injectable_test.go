package gpu

import (
	"context"
	"errors"
	"testing"
)

func TestInjectable_NewInjectable(t *testing.T) {
	t.Run("default GPU type", func(t *testing.T) {
		g := NewInjectable(4, "")
		if g.deviceCount != 4 {
			t.Errorf("deviceCount = %d, want 4", g.deviceCount)
		}
		if len(g.devices) != 4 {
			t.Errorf("len(devices) = %d, want 4", len(g.devices))
		}
		if g.devices[0].info.Name != "NVIDIA H100 80GB HBM3" {
			t.Errorf("default GPU name = %q, want NVIDIA H100 80GB HBM3", g.devices[0].info.Name)
		}
	})

	t.Run("custom GPU type", func(t *testing.T) {
		g := NewInjectable(2, "NVIDIA A100")
		if g.devices[0].info.Name != "NVIDIA A100" {
			t.Errorf("GPU name = %q, want NVIDIA A100", g.devices[0].info.Name)
		}
	})

	t.Run("device info populated", func(t *testing.T) {
		g := NewInjectable(8, "")
		for i := 0; i < 8; i++ {
			if g.devices[i].info.Index != i {
				t.Errorf("device %d index = %d, want %d", i, g.devices[i].info.Index, i)
			}
			if g.devices[i].info.UUID == "" {
				t.Errorf("device %d UUID is empty", i)
			}
			if g.devices[i].info.PCIBusID == "" {
				t.Errorf("device %d PCIBusID is empty", i)
			}
			if g.devices[i].info.Memory != 80*1024*1024*1024 {
				t.Errorf("device %d memory = %d, want 80GB", i, g.devices[i].info.Memory)
			}
		}
	})
}

func TestInjectable_Initialize(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(2, "")

	t.Run("initialize successfully", func(t *testing.T) {
		if err := g.Initialize(ctx); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		if !g.initialized {
			t.Error("initialized = false, want true")
		}
	})

	t.Run("initialize twice fails", func(t *testing.T) {
		if err := g.Initialize(ctx); err == nil {
			t.Error("second Initialize() should fail")
		}
	})
}

func TestInjectable_Initialize_WithBootError(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(2, "")

	bootErr := errors.New("boot failure: GPU not detected")
	g.InjectBootError(bootErr)

	if err := g.Initialize(ctx); err != bootErr {
		t.Errorf("Initialize() error = %v, want %v", err, bootErr)
	}

	g.ClearBootError()
	if err := g.Initialize(ctx); err != nil {
		t.Errorf("Initialize() after clear error = %v", err)
	}
}

func TestInjectable_Shutdown(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(2, "")

	t.Run("shutdown without initialize fails", func(t *testing.T) {
		if err := g.Shutdown(ctx); err == nil {
			t.Error("Shutdown() without Initialize() should fail")
		}
	})

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("shutdown successfully", func(t *testing.T) {
		if err := g.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		if g.initialized {
			t.Error("initialized = true after shutdown, want false")
		}
	})
}

func TestInjectable_GetDeviceCount(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(8, "")

	t.Run("not initialized", func(t *testing.T) {
		if _, err := g.GetDeviceCount(ctx); err == nil {
			t.Error("GetDeviceCount() should fail when not initialized")
		}
	})

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("returns correct count", func(t *testing.T) {
		count, err := g.GetDeviceCount(ctx)
		if err != nil {
			t.Fatalf("GetDeviceCount() error = %v", err)
		}
		if count != 8 {
			t.Errorf("GetDeviceCount() = %d, want 8", count)
		}
	})

	t.Run("with NVML error", func(t *testing.T) {
		nvmlErr := errors.New("NVML error")
		g.InjectNVMLError(nvmlErr)
		if _, err := g.GetDeviceCount(ctx); err != nvmlErr {
			t.Errorf("GetDeviceCount() error = %v, want %v", err, nvmlErr)
		}
		g.ClearNVMLError()
	})
}

func TestInjectable_GetDeviceInfo(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "NVIDIA H100")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("valid index", func(t *testing.T) {
		info, err := g.GetDeviceInfo(ctx, 2)
		if err != nil {
			t.Fatalf("GetDeviceInfo(2) error = %v", err)
		}
		if info.Index != 2 {
			t.Errorf("Index = %d, want 2", info.Index)
		}
		if info.Name != "NVIDIA H100" {
			t.Errorf("Name = %q, want NVIDIA H100", info.Name)
		}
	})

	t.Run("invalid index negative", func(t *testing.T) {
		if _, err := g.GetDeviceInfo(ctx, -1); err == nil {
			t.Error("GetDeviceInfo(-1) should fail")
		}
	})

	t.Run("invalid index too large", func(t *testing.T) {
		if _, err := g.GetDeviceInfo(ctx, 10); err == nil {
			t.Error("GetDeviceInfo(10) should fail")
		}
	})

	t.Run("with device error", func(t *testing.T) {
		devErr := errors.New("device error")
		g.InjectDeviceError(2, devErr)
		if _, err := g.GetDeviceInfo(ctx, 2); err != devErr {
			t.Errorf("GetDeviceInfo(2) error = %v, want %v", err, devErr)
		}
		// Other devices should still work
		if _, err := g.GetDeviceInfo(ctx, 0); err != nil {
			t.Errorf("GetDeviceInfo(0) error = %v, want nil", err)
		}
		g.ClearDeviceError(2)
	})

	t.Run("with NVML error", func(t *testing.T) {
		nvmlErr := errors.New("NVML error")
		g.InjectNVMLError(nvmlErr)
		if _, err := g.GetDeviceInfo(ctx, 0); err != nvmlErr {
			t.Errorf("GetDeviceInfo() error = %v, want %v", err, nvmlErr)
		}
		g.ClearNVMLError()
	})
}

func TestInjectable_GetDeviceHealth(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("normal health", func(t *testing.T) {
		health, err := g.GetDeviceHealth(ctx, 0)
		if err != nil {
			t.Fatalf("GetDeviceHealth(0) error = %v", err)
		}
		if health.Temperature <= 0 {
			t.Errorf("Temperature = %d, want > 0", health.Temperature)
		}
		if health.MemoryTotal != 80*1024*1024*1024 {
			t.Errorf("MemoryTotal = %d, want 80GB", health.MemoryTotal)
		}
	})

	t.Run("with temperature spike", func(t *testing.T) {
		g.InjectTemperatureSpike(1, 95)
		health, err := g.GetDeviceHealth(ctx, 1)
		if err != nil {
			t.Fatalf("GetDeviceHealth(1) error = %v", err)
		}
		if health.Temperature != 95 {
			t.Errorf("Temperature = %d, want 95", health.Temperature)
		}

		// Other devices should have normal temp
		health0, _ := g.GetDeviceHealth(ctx, 0)
		if health0.Temperature == 95 {
			t.Error("device 0 should not have temperature spike")
		}

		g.ClearTemperatureSpike(1)
		health, _ = g.GetDeviceHealth(ctx, 1)
		if health.Temperature == 95 {
			t.Error("temperature should be back to normal after clear")
		}
	})

	t.Run("invalid index", func(t *testing.T) {
		if _, err := g.GetDeviceHealth(ctx, -1); err == nil {
			t.Error("GetDeviceHealth(-1) should fail")
		}
		if _, err := g.GetDeviceHealth(ctx, 100); err == nil {
			t.Error("GetDeviceHealth(100) should fail")
		}
	})
}

func TestInjectable_XIDErrors(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("no errors initially", func(t *testing.T) {
		errors, err := g.GetXIDErrors(ctx)
		if err != nil {
			t.Fatalf("GetXIDErrors() error = %v", err)
		}
		if len(errors) != 0 {
			t.Errorf("len(errors) = %d, want 0", len(errors))
		}
	})

	t.Run("inject XID error", func(t *testing.T) {
		g.InjectXIDError(0, 79, "GPU fell off the bus")

		errors, err := g.GetXIDErrors(ctx)
		if err != nil {
			t.Fatalf("GetXIDErrors() error = %v", err)
		}
		if len(errors) != 1 {
			t.Fatalf("len(errors) = %d, want 1", len(errors))
		}
		if errors[0].XIDCode != 79 {
			t.Errorf("XIDCode = %d, want 79", errors[0].XIDCode)
		}
		if errors[0].Message != "GPU fell off the bus" {
			t.Errorf("Message = %q, want 'GPU fell off the bus'", errors[0].Message)
		}
		if errors[0].DeviceID == "" {
			t.Error("DeviceID should be set")
		}
	})

	t.Run("multiple XID errors", func(t *testing.T) {
		g.InjectXIDError(1, 48, "Double bit ECC error")
		g.InjectXIDError(2, 31, "GPU memory page fault")

		errors, err := g.GetXIDErrors(ctx)
		if err != nil {
			t.Fatalf("GetXIDErrors() error = %v", err)
		}
		if len(errors) != 3 {
			t.Errorf("len(errors) = %d, want 3", len(errors))
		}
	})

	t.Run("clear XID errors", func(t *testing.T) {
		g.ClearXIDErrors()

		errors, err := g.GetXIDErrors(ctx)
		if err != nil {
			t.Fatalf("GetXIDErrors() error = %v", err)
		}
		if len(errors) != 0 {
			t.Errorf("len(errors) = %d after clear, want 0", len(errors))
		}
	})

	t.Run("clear specific XID error", func(t *testing.T) {
		g.InjectXIDError(0, 79, "GPU fell off bus")
		g.InjectXIDError(1, 48, "Double bit ECC")
		g.InjectXIDError(0, 31, "Memory page fault")

		gpu0UUID := g.devices[0].info.UUID

		g.ClearXIDError(0, 79)

		errors, err := g.GetXIDErrors(ctx)
		if err != nil {
			t.Fatalf("GetXIDErrors() error = %v", err)
		}
		if len(errors) != 2 {
			t.Errorf("len(errors) = %d, want 2", len(errors))
		}
		for _, e := range errors {
			if e.DeviceID == gpu0UUID && e.XIDCode == 79 {
				t.Error("XID 79 on GPU 0 should have been cleared")
			}
		}
		g.ClearXIDErrors()
	})

	t.Run("clear nonexistent XID error", func(t *testing.T) {
		g.InjectXIDError(0, 79, "test")
		g.ClearXIDError(1, 79) // Different GPU
		g.ClearXIDError(0, 48) // Different code

		errors, _ := g.GetXIDErrors(ctx)
		if len(errors) != 1 {
			t.Errorf("len(errors) = %d, want 1", len(errors))
		}
		g.ClearXIDErrors()
	})

	t.Run("XID error with invalid GPU index", func(t *testing.T) {
		g.InjectXIDError(100, 79, "test")
		errors, _ := g.GetXIDErrors(ctx)
		if len(errors) != 1 {
			t.Fatalf("len(errors) = %d, want 1", len(errors))
		}
		if errors[0].DeviceID != "" {
			t.Errorf("DeviceID for invalid index should be empty, got %q", errors[0].DeviceID)
		}
		g.ClearXIDErrors()
	})
}

func TestInjectable_NVMLError(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(2, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	nvmlErr := errors.New("NVML driver not loaded")
	g.InjectNVMLError(nvmlErr)

	t.Run("affects GetDeviceCount", func(t *testing.T) {
		if _, err := g.GetDeviceCount(ctx); err != nvmlErr {
			t.Errorf("GetDeviceCount() error = %v, want %v", err, nvmlErr)
		}
	})

	t.Run("affects GetDeviceInfo", func(t *testing.T) {
		if _, err := g.GetDeviceInfo(ctx, 0); err != nvmlErr {
			t.Errorf("GetDeviceInfo() error = %v, want %v", err, nvmlErr)
		}
	})

	t.Run("affects GetDeviceHealth", func(t *testing.T) {
		if _, err := g.GetDeviceHealth(ctx, 0); err != nvmlErr {
			t.Errorf("GetDeviceHealth() error = %v, want %v", err, nvmlErr)
		}
	})

	t.Run("affects GetXIDErrors", func(t *testing.T) {
		if _, err := g.GetXIDErrors(ctx); err != nvmlErr {
			t.Errorf("GetXIDErrors() error = %v, want %v", err, nvmlErr)
		}
	})

	t.Run("clear restores functionality", func(t *testing.T) {
		g.ClearNVMLError()
		if _, err := g.GetDeviceCount(ctx); err != nil {
			t.Errorf("GetDeviceCount() after clear error = %v", err)
		}
	})
}

func TestInjectable_DeviceError(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	devErr := errors.New("device communication failure")
	g.InjectDeviceError(1, devErr)

	t.Run("affects specific device only", func(t *testing.T) {
		// Device 0 should work
		if _, err := g.GetDeviceInfo(ctx, 0); err != nil {
			t.Errorf("GetDeviceInfo(0) error = %v, want nil", err)
		}

		// Device 1 should fail
		if _, err := g.GetDeviceInfo(ctx, 1); err != devErr {
			t.Errorf("GetDeviceInfo(1) error = %v, want %v", err, devErr)
		}

		// Device 2 should work
		if _, err := g.GetDeviceInfo(ctx, 2); err != nil {
			t.Errorf("GetDeviceInfo(2) error = %v, want nil", err)
		}
	})

	t.Run("affects health check too", func(t *testing.T) {
		if _, err := g.GetDeviceHealth(ctx, 1); err != devErr {
			t.Errorf("GetDeviceHealth(1) error = %v, want %v", err, devErr)
		}
	})

	t.Run("clear restores device", func(t *testing.T) {
		g.ClearDeviceError(1)
		if _, err := g.GetDeviceInfo(ctx, 1); err != nil {
			t.Errorf("GetDeviceInfo(1) after clear error = %v", err)
		}
	})
}

func TestInjectable_ClearAllErrors(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Inject various errors
	g.InjectXIDError(0, 79, "XID error")
	g.InjectXIDError(1, 48, "Another XID")
	g.InjectNVMLError(errors.New("NVML error"))
	g.InjectDeviceError(2, errors.New("device error"))
	g.InjectTemperatureSpike(0, 95)
	g.InjectTemperatureSpike(3, 90)

	if !g.HasActiveFailures() {
		t.Error("HasActiveFailures() = false, want true")
	}

	g.ClearAllErrors()

	if g.HasActiveFailures() {
		t.Error("HasActiveFailures() after ClearAllErrors() = true, want false")
	}

	// Verify all operations work
	if _, err := g.GetDeviceCount(ctx); err != nil {
		t.Errorf("GetDeviceCount() after clear error = %v", err)
	}
	if _, err := g.GetDeviceInfo(ctx, 2); err != nil {
		t.Errorf("GetDeviceInfo(2) after clear error = %v", err)
	}

	errors, _ := g.GetXIDErrors(ctx)
	if len(errors) != 0 {
		t.Errorf("XID errors after clear = %d, want 0", len(errors))
	}

	health, _ := g.GetDeviceHealth(ctx, 0)
	if health.Temperature == 95 {
		t.Error("temperature spike should be cleared")
	}
}

func TestInjectable_HasActiveFailures(t *testing.T) {
	g := NewInjectable(4, "")

	t.Run("no failures initially", func(t *testing.T) {
		if g.HasActiveFailures() {
			t.Error("HasActiveFailures() = true, want false")
		}
	})

	t.Run("XID error", func(t *testing.T) {
		g.InjectXIDError(0, 79, "test")
		if !g.HasActiveFailures() {
			t.Error("HasActiveFailures() with XID error = false, want true")
		}
		g.ClearXIDErrors()
	})

	t.Run("NVML error", func(t *testing.T) {
		g.InjectNVMLError(errors.New("test"))
		if !g.HasActiveFailures() {
			t.Error("HasActiveFailures() with NVML error = false, want true")
		}
		g.ClearNVMLError()
	})

	t.Run("boot error", func(t *testing.T) {
		g.InjectBootError(errors.New("test"))
		if !g.HasActiveFailures() {
			t.Error("HasActiveFailures() with boot error = false, want true")
		}
		g.ClearBootError()
	})

	t.Run("device error", func(t *testing.T) {
		g.InjectDeviceError(0, errors.New("test"))
		if !g.HasActiveFailures() {
			t.Error("HasActiveFailures() with device error = false, want true")
		}
		g.ClearDeviceError(0)
	})

	t.Run("temperature spike", func(t *testing.T) {
		g.InjectTemperatureSpike(0, 95)
		if !g.HasActiveFailures() {
			t.Error("HasActiveFailures() with temperature spike = false, want true")
		}
		g.ClearTemperatureSpike(0)
	})

	t.Run("all cleared", func(t *testing.T) {
		if g.HasActiveFailures() {
			t.Error("HasActiveFailures() after all cleared = true, want false")
		}
	})
}

func TestInjectable_NotInitialized(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(2, "")

	// All operations should fail when not initialized
	t.Run("GetDeviceCount", func(t *testing.T) {
		if _, err := g.GetDeviceCount(ctx); err == nil {
			t.Error("GetDeviceCount() should fail when not initialized")
		}
	})

	t.Run("GetDeviceInfo", func(t *testing.T) {
		if _, err := g.GetDeviceInfo(ctx, 0); err == nil {
			t.Error("GetDeviceInfo() should fail when not initialized")
		}
	})

	t.Run("GetDeviceHealth", func(t *testing.T) {
		if _, err := g.GetDeviceHealth(ctx, 0); err == nil {
			t.Error("GetDeviceHealth() should fail when not initialized")
		}
	})

	t.Run("GetXIDErrors", func(t *testing.T) {
		if _, err := g.GetXIDErrors(ctx); err == nil {
			t.Error("GetXIDErrors() should fail when not initialized")
		}
	})
}

func TestInjectable_TemperatureSpikeBoundary(t *testing.T) {
	ctx := context.Background()
	g := NewInjectable(4, "")

	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Run("invalid GPU index negative", func(t *testing.T) {
		g.InjectTemperatureSpike(-1, 95)
		// Should not panic, just be ignored
		health, _ := g.GetDeviceHealth(ctx, 0)
		if health.Temperature == 95 {
			t.Error("negative index should not affect any device")
		}
	})

	t.Run("invalid GPU index too large", func(t *testing.T) {
		g.InjectTemperatureSpike(100, 95)
		// Should not panic, just be ignored
	})

	t.Run("clear invalid GPU index", func(t *testing.T) {
		g.ClearTemperatureSpike(-1)
		g.ClearTemperatureSpike(100)
		// Should not panic
	})
}

