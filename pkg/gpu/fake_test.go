package gpu

import (
	"context"
	"testing"
)

func TestFake_Initialize(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(8)

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !fake.initialized {
		t.Error("Expected initialized to be true")
	}

	if err := fake.Initialize(ctx); err == nil {
		t.Error("Expected error when initializing twice")
	}
}

func TestFake_GetDeviceCount(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(8)

	if _, err := fake.GetDeviceCount(ctx); err == nil {
		t.Error("Expected error when not initialized")
	}

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	count, err := fake.GetDeviceCount(ctx)
	if err != nil {
		t.Fatalf("GetDeviceCount failed: %v", err)
	}

	if count != 8 {
		t.Errorf("Expected 8 devices, got %d", count)
	}
}

func TestFake_GetDeviceInfo(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(4)

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	info, err := fake.GetDeviceInfo(ctx, 0)
	if err != nil {
		t.Fatalf("GetDeviceInfo failed: %v", err)
	}

	if info.Index != 0 {
		t.Errorf("Expected index 0, got %d", info.Index)
	}

	if info.UUID == "" {
		t.Error("Expected non-empty UUID")
	}

	if info.Name != "NVIDIA H100 80GB HBM3" {
		t.Errorf("Expected H100, got %s", info.Name)
	}

	if info.Memory != 80*1024*1024*1024 {
		t.Errorf("Expected 80GB memory, got %d", info.Memory)
	}

	if _, err := fake.GetDeviceInfo(ctx, 10); err == nil {
		t.Error("Expected error for invalid device index")
	}
}

func TestFake_GetDeviceHealth(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(2)

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	health, err := fake.GetDeviceHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetDeviceHealth failed: %v", err)
	}

	if health.Temperature < 0 {
		t.Error("Expected positive temperature")
	}

	if health.MemoryTotal != 80*1024*1024*1024 {
		t.Errorf("Expected 80GB total memory, got %d", health.MemoryTotal)
	}

	if health.GPUUtilization < 0 || health.GPUUtilization > 100 {
		t.Errorf("Expected GPU utilization 0-100, got %d", health.GPUUtilization)
	}
}

func TestFake_XIDErrors(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(2)

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	errors, err := fake.GetXIDErrors(ctx)
	if err != nil {
		t.Fatalf("GetXIDErrors failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("Expected 0 errors, got %d", len(errors))
	}

	fake.InjectXIDError("GPU-0", 79, "Test XID error")

	errors, err = fake.GetXIDErrors(ctx)
	if err != nil {
		t.Fatalf("GetXIDErrors failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errors))
	}

	if errors[0].XIDCode != 79 {
		t.Errorf("Expected XID code 79, got %d", errors[0].XIDCode)
	}

	fake.ClearXIDErrors()

	errors, err = fake.GetXIDErrors(ctx)
	if err != nil {
		t.Fatalf("GetXIDErrors failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("Expected 0 errors after clear, got %d", len(errors))
	}
}

func TestFake_Shutdown(t *testing.T) {
	ctx := context.Background()
	fake := NewFake(2)

	if err := fake.Shutdown(ctx); err == nil {
		t.Error("Expected error when shutting down without initialization")
	}

	if err := fake.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := fake.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if fake.initialized {
		t.Error("Expected initialized to be false after shutdown")
	}
}

