package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/NavarchProject/navarch/pkg/gpu"
)

// mockSystemReader is a mock implementation of SystemMetricsReader.
type mockSystemReader struct {
	cpuUsage    float64
	memoryUsage float64
	cpuErr      error
	memErr      error
}

func (m *mockSystemReader) ReadCPUUsage(ctx context.Context) (float64, error) {
	if m.cpuErr != nil {
		return 0, m.cpuErr
	}
	return m.cpuUsage, nil
}

func (m *mockSystemReader) ReadMemoryUsage(ctx context.Context) (float64, error) {
	if m.memErr != nil {
		return 0, m.memErr
	}
	return m.memoryUsage, nil
}

func TestCollector_Collect_WithAllMetrics(t *testing.T) {
	// Create a fake GPU manager
	gpuManager := gpu.NewFake(2)
	ctx := context.Background()
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(ctx)

	// Create a mock system reader
	systemReader := &mockSystemReader{
		cpuUsage:    45.5,
		memoryUsage: 72.3,
	}

	collector := NewCollector(gpuManager, systemReader)

	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify CPU usage
	if metrics.CpuUsagePercent != 45.5 {
		t.Errorf("expected CPU usage 45.5, got %f", metrics.CpuUsagePercent)
	}

	// Verify memory usage
	if metrics.MemoryUsagePercent != 72.3 {
		t.Errorf("expected memory usage 72.3, got %f", metrics.MemoryUsagePercent)
	}

	// Verify GPU metrics
	if len(metrics.GpuMetrics) != 2 {
		t.Errorf("expected 2 GPU metrics, got %d", len(metrics.GpuMetrics))
	}

	for i, gpuMetric := range metrics.GpuMetrics {
		if gpuMetric.GpuIndex != int32(i) {
			t.Errorf("GPU %d: expected index %d, got %d", i, i, gpuMetric.GpuIndex)
		}
		// Temperature should be between 30 and 50 for fake GPU
		if gpuMetric.Temperature < 30 || gpuMetric.Temperature > 50 {
			t.Errorf("GPU %d: unexpected temperature %d", i, gpuMetric.Temperature)
		}
		// Utilization should be between 0 and 100
		if gpuMetric.UtilizationPercent < 0 || gpuMetric.UtilizationPercent > 100 {
			t.Errorf("GPU %d: unexpected utilization %f", i, gpuMetric.UtilizationPercent)
		}
	}
}

func TestCollector_Collect_CPUError(t *testing.T) {
	gpuManager := gpu.NewFake(1)
	ctx := context.Background()
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(ctx)

	systemReader := &mockSystemReader{
		cpuErr:      errors.New("cpu read error"),
		memoryUsage: 50.0,
	}

	collector := NewCollector(gpuManager, systemReader)

	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should not fail on CPU error: %v", err)
	}

	// CPU should be 0 on error
	if metrics.CpuUsagePercent != 0 {
		t.Errorf("expected CPU usage 0 on error, got %f", metrics.CpuUsagePercent)
	}

	// Memory should still be collected
	if metrics.MemoryUsagePercent != 50.0 {
		t.Errorf("expected memory usage 50.0, got %f", metrics.MemoryUsagePercent)
	}
}

func TestCollector_Collect_MemoryError(t *testing.T) {
	gpuManager := gpu.NewFake(1)
	ctx := context.Background()
	if err := gpuManager.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize GPU manager: %v", err)
	}
	defer gpuManager.Shutdown(ctx)

	systemReader := &mockSystemReader{
		cpuUsage: 30.0,
		memErr:   errors.New("memory read error"),
	}

	collector := NewCollector(gpuManager, systemReader)

	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should not fail on memory error: %v", err)
	}

	// CPU should still be collected
	if metrics.CpuUsagePercent != 30.0 {
		t.Errorf("expected CPU usage 30.0, got %f", metrics.CpuUsagePercent)
	}

	// Memory should be 0 on error
	if metrics.MemoryUsagePercent != 0 {
		t.Errorf("expected memory usage 0 on error, got %f", metrics.MemoryUsagePercent)
	}
}

func TestCollector_Collect_NoGPUManager(t *testing.T) {
	ctx := context.Background()

	systemReader := &mockSystemReader{
		cpuUsage:    25.0,
		memoryUsage: 60.0,
	}

	collector := NewCollector(nil, systemReader)

	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// CPU and memory should be collected
	if metrics.CpuUsagePercent != 25.0 {
		t.Errorf("expected CPU usage 25.0, got %f", metrics.CpuUsagePercent)
	}
	if metrics.MemoryUsagePercent != 60.0 {
		t.Errorf("expected memory usage 60.0, got %f", metrics.MemoryUsagePercent)
	}

	// GPU metrics should be nil or empty
	if metrics.GpuMetrics != nil && len(metrics.GpuMetrics) > 0 {
		t.Errorf("expected no GPU metrics without GPU manager, got %d", len(metrics.GpuMetrics))
	}
}

func TestCollector_Collect_GPUNotInitialized(t *testing.T) {
	ctx := context.Background()

	// Create a fake GPU manager but don't initialize it
	gpuManager := gpu.NewFake(2)

	systemReader := &mockSystemReader{
		cpuUsage:    40.0,
		memoryUsage: 55.0,
	}

	collector := NewCollector(gpuManager, systemReader)

	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should not fail on GPU error: %v", err)
	}

	// CPU and memory should still be collected
	if metrics.CpuUsagePercent != 40.0 {
		t.Errorf("expected CPU usage 40.0, got %f", metrics.CpuUsagePercent)
	}
	if metrics.MemoryUsagePercent != 55.0 {
		t.Errorf("expected memory usage 55.0, got %f", metrics.MemoryUsagePercent)
	}

	// GPU metrics should be empty (not nil)
	if len(metrics.GpuMetrics) != 0 {
		t.Errorf("expected empty GPU metrics, got %d", len(metrics.GpuMetrics))
	}
}

func TestNewCollector_DefaultSystemReader(t *testing.T) {
	gpuManager := gpu.NewFake(1)
	collector := NewCollector(gpuManager, nil)

	// Verify that a default system reader was created
	if collector.systemReader == nil {
		t.Error("expected default system reader to be created")
	}
}
