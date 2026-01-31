# Node metrics package

This package provides metrics collection for Navarch node daemons.

## Overview

The metrics package collects system and GPU metrics for heartbeat reporting:

- CPU usage percentage.
- Memory usage percentage.
- Per-GPU utilization, memory, temperature, and power.

## Collector interface

```go
type Collector interface {
    Collect(ctx context.Context) (*pb.NodeMetrics, error)
}
```

## Usage

```go
import (
    "github.com/NavarchProject/navarch/pkg/gpu"
    "github.com/NavarchProject/navarch/pkg/node/metrics"
)

gpuManager := gpu.NewInjectable(8, "")
gpuManager.Initialize(ctx)

collector := metrics.NewCollector(gpuManager, nil)

m, err := collector.Collect(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("CPU: %.1f%%\n", m.CpuUsagePercent)
fmt.Printf("Memory: %.1f%%\n", m.MemoryUsagePercent)
for i, g := range m.GpuMetrics {
    fmt.Printf("GPU %d: %.1f%% util, %d°C\n", i, g.UtilizationPercent, g.TemperatureCelsius)
}
```

## SystemMetricsReader

The `SystemMetricsReader` interface abstracts system metric collection:

```go
type SystemMetricsReader interface {
    ReadCPUUsage(ctx context.Context) (float64, error)
    ReadMemoryUsage(ctx context.Context) (float64, error)
}
```

### ProcReader

The default implementation reads from `/proc` on Linux:

```go
reader := metrics.NewProcReader()
cpu, _ := reader.ReadCPUUsage(ctx)
mem, _ := reader.ReadMemoryUsage(ctx)
```

### FakeReader

For testing, use a fake reader with configurable values:

```go
reader := metrics.NewFakeReader(50.0, 75.0) // 50% CPU, 75% memory
collector := metrics.NewCollector(gpuManager, reader)
```

## Integration with node daemon

The node daemon uses the collector for heartbeat reporting:

```go
// In the node daemon's heartbeat loop
metrics, _ := collector.Collect(ctx)
_, err := client.Heartbeat(ctx, connect.NewRequest(&pb.HeartbeatRequest{
    NodeId:  nodeID,
    Metrics: metrics,
}))
```

## Testing

```bash
go test ./pkg/node/metrics/... -v
```
