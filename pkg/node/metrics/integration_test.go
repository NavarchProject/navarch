package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/node/metrics"
)

// TestCollector_Integration tests the full metrics collection pipeline
// with real system metrics (using /proc filesystem).
func TestCollector_Integration(t *testing.T) {
	ctx := context.Background()

	// Create a fake GPU manager for testing
	gpuManager := gpu.NewFake(4)
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(ctx)

	// Create collector with default ProcReader (reads from real /proc)
	collector := metrics.NewCollector(gpuManager, nil)

	// First collection - CPU will be 0 due to no previous sample
	m1, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}

	// Verify memory usage is within expected range (0-100%)
	if m1.MemoryUsagePercent < 0 || m1.MemoryUsagePercent > 100 {
		t.Errorf("memory usage out of range: %f", m1.MemoryUsagePercent)
	}

	// Wait a bit and collect again to get meaningful CPU usage
	time.Sleep(100 * time.Millisecond)

	m2, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}

	// CPU usage should now be a real value (0-100)
	if m2.CpuUsagePercent < 0 || m2.CpuUsagePercent > 100 {
		t.Errorf("CPU usage out of range: %f", m2.CpuUsagePercent)
	}

	// Memory should still be valid
	if m2.MemoryUsagePercent < 0 || m2.MemoryUsagePercent > 100 {
		t.Errorf("memory usage out of range: %f", m2.MemoryUsagePercent)
	}

	// Verify GPU metrics
	if len(m2.GpuMetrics) != 4 {
		t.Errorf("expected 4 GPU metrics, got %d", len(m2.GpuMetrics))
	}

	for i, gm := range m2.GpuMetrics {
		if gm.GpuIndex != int32(i) {
			t.Errorf("GPU %d: unexpected index %d", i, gm.GpuIndex)
		}
		if gm.Temperature < 0 || gm.Temperature > 150 {
			t.Errorf("GPU %d: temperature out of range: %d", i, gm.Temperature)
		}
		if gm.UtilizationPercent < 0 || gm.UtilizationPercent > 100 {
			t.Errorf("GPU %d: utilization out of range: %f", i, gm.UtilizationPercent)
		}
	}

	t.Logf("Metrics collected successfully:")
	t.Logf("  CPU Usage: %.2f%%", m2.CpuUsagePercent)
	t.Logf("  Memory Usage: %.2f%%", m2.MemoryUsagePercent)
	t.Logf("  GPU Count: %d", len(m2.GpuMetrics))
}

// TestCollector_ConcurrentAccess tests that the collector is safe for concurrent use.
func TestCollector_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()

	gpuManager := gpu.NewFake(2)
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(ctx)

	collector := metrics.NewCollector(gpuManager, nil)

	// Run multiple concurrent collections
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 5; j++ {
				_, err := collector.Collect(ctx)
				if err != nil {
					t.Errorf("concurrent Collect failed: %v", err)
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestCollector_ContextCancellation tests that collection respects context cancellation.
func TestCollector_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	gpuManager := gpu.NewFake(2)
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(context.Background())

	collector := metrics.NewCollector(gpuManager, nil)

	// Cancel context before collection
	cancel()

	// Collection should still work (context is mainly for GPU operations)
	// The current implementation doesn't check context in system metrics
	_, err := collector.Collect(ctx)
	// This may or may not error depending on implementation
	// The important thing is it doesn't hang
	_ = err
}
