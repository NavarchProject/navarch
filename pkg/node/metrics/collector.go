package metrics

import (
	"context"

	"github.com/NavarchProject/navarch/pkg/gpu"
	pb "github.com/NavarchProject/navarch/proto"
)

// Collector collects system and GPU metrics.
type Collector interface {
	// Collect gathers all metrics and returns them as NodeMetrics.
	Collect(ctx context.Context) (*pb.NodeMetrics, error)
}

// SystemMetricsReader reads system-level metrics (CPU, memory).
type SystemMetricsReader interface {
	// ReadCPUUsage returns the CPU usage as a percentage (0-100).
	ReadCPUUsage(ctx context.Context) (float64, error)

	// ReadMemoryUsage returns the memory usage as a percentage (0-100).
	ReadMemoryUsage(ctx context.Context) (float64, error)
}

// DefaultCollector collects metrics from the system and GPUs.
type DefaultCollector struct {
	gpuManager   gpu.Manager
	systemReader SystemMetricsReader
}

// NewCollector creates a new metrics collector.
func NewCollector(gpuManager gpu.Manager, systemReader SystemMetricsReader) *DefaultCollector {
	if systemReader == nil {
		systemReader = NewProcReader()
	}
	return &DefaultCollector{
		gpuManager:   gpuManager,
		systemReader: systemReader,
	}
}

// Collect gathers all metrics from the system and GPUs.
func (c *DefaultCollector) Collect(ctx context.Context) (*pb.NodeMetrics, error) {
	metrics := &pb.NodeMetrics{}

	// Collect CPU usage
	cpuUsage, err := c.systemReader.ReadCPUUsage(ctx)
	if err != nil {
		// Log warning but continue - we can still report other metrics
		cpuUsage = 0
	}
	metrics.CpuUsagePercent = cpuUsage

	// Collect memory usage
	memUsage, err := c.systemReader.ReadMemoryUsage(ctx)
	if err != nil {
		// Log warning but continue
		memUsage = 0
	}
	metrics.MemoryUsagePercent = memUsage

	// Collect GPU metrics if GPU manager is available
	if c.gpuManager != nil {
		gpuMetrics, err := c.collectGPUMetrics(ctx)
		if err != nil {
			// Log warning but continue with empty GPU metrics
			gpuMetrics = []*pb.GPUMetrics{}
		}
		metrics.GpuMetrics = gpuMetrics
	}

	return metrics, nil
}

// collectGPUMetrics collects metrics from all GPUs.
func (c *DefaultCollector) collectGPUMetrics(ctx context.Context) ([]*pb.GPUMetrics, error) {
	count, err := c.gpuManager.GetDeviceCount(ctx)
	if err != nil {
		return nil, err
	}

	gpuMetrics := make([]*pb.GPUMetrics, 0, count)
	for i := 0; i < count; i++ {
		health, err := c.gpuManager.GetDeviceHealth(ctx, i)
		if err != nil {
			// Skip this GPU but continue with others
			continue
		}

		gpuMetrics = append(gpuMetrics, &pb.GPUMetrics{
			GpuIndex:          int32(i),
			Temperature:       int32(health.Temperature),
			PowerUsage:        int32(health.PowerUsage),
			UtilizationPercent: float64(health.GPUUtilization),
			MemoryUsed:        int64(health.MemoryUsed),
		})
	}

	return gpuMetrics, nil
}
