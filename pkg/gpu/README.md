# GPU package

The GPU package provides an abstraction layer for interacting with NVIDIA GPUs. It supports real hardware via NVML and simulated hardware for development and testing.

## Overview

The package provides:

- A `Manager` interface for GPU operations.
- NVML implementation for real GPU hardware.
- XID error collection via NVML events with dmesg fallback.
- Injectable implementation for testing and development.
- Health event collection for CEL policy evaluation.
- DCGM health watch system constants.

## Manager interface

The `Manager` interface defines operations for GPU management:

```go
type Manager interface {
    Initialize(ctx context.Context) error
    Shutdown(ctx context.Context) error
    GetDeviceCount(ctx context.Context) (int, error)
    GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error)
    GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error)
    CollectHealthEvents(ctx context.Context) ([]HealthEvent, error)
}
```

## NVML implementation

The NVML implementation provides real GPU monitoring using the NVIDIA Management Library:

```go
manager := gpu.NewNVML()
if err := manager.Initialize(ctx); err != nil {
    log.Fatal(err)
}
defer manager.Shutdown(ctx)

count, _ := manager.GetDeviceCount(ctx)
for i := 0; i < count; i++ {
    info, _ := manager.GetDeviceInfo(ctx, i)
    health, _ := manager.GetDeviceHealth(ctx, i)
    fmt.Printf("GPU %d: %s, Temp: %d°C, Power: %.0fW\n",
        i, info.Name, health.Temperature, health.PowerUsage)
}

// Collect XID errors
events, _ := manager.CollectHealthEvents(ctx)
```

### XID error collection

The NVML implementation includes automatic XID error collection:

1. **NVML events (primary)**: Uses `nvmlEventSetWait()` for real-time XID capture.
2. **Kernel log fallback**: Parses `/dev/kmsg` or `/var/log/kern.log` when NVML events are unavailable.

XID errors are automatically converted to `HealthEvent` objects and returned by `CollectHealthEvents()`.

### Automatic fallback

The node daemon automatically selects the GPU manager:

1. Attempts to initialize NVML on startup.
2. If successful, uses NVML manager with XID collection.
3. If NVML is unavailable (no driver), falls back to Injectable.
4. Set `NAVARCH_FAKE_GPU=true` to force fake mode.

## Injectable implementation

The Injectable implementation simulates GPU hardware for development and testing.

```go
manager := gpu.NewInjectable(8, "") // Simulate 8 H100 GPUs
if err := manager.Initialize(ctx); err != nil {
    log.Fatal(err)
}
defer manager.Shutdown(ctx)

count, _ := manager.GetDeviceCount(ctx)
for i := 0; i < count; i++ {
    info, _ := manager.GetDeviceInfo(ctx, i)
    fmt.Printf("GPU %d: %s (%s)\n", i, info.Name, info.UUID)
}
```

Features:

- Configurable number of simulated GPUs.
- Realistic device information (H100 80GB HBM3 by default).
- Configurable GPU type string.
- Health event injection for testing failure scenarios.

### Health event injection

The Injectable implementation supports timestamp control for deterministic tests using `*At` variants:

```go
injectable := gpu.NewInjectable(4, "")
injectable.Initialize(ctx)

// Inject with current time (uses time.Now())
injectable.InjectXIDHealthEvent(0, 79, "GPU has fallen off the bus")

// Inject with specific timestamp (for deterministic tests)
ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
injectable.InjectXIDHealthEventAt(0, 79, "GPU has fallen off the bus", ts)
injectable.InjectThermalHealthEventAt(0, 95, "High temperature", ts)
injectable.InjectMemoryHealthEventAt(0, gpu.EventTypeECCDBE, 0, 1, "ECC error", ts)
injectable.InjectNVLinkHealthEventAt(0, 0, "NVLink failure", ts)
```

```go
injectable := gpu.NewInjectable(4, "")
injectable.Initialize(ctx)

// Inject an XID error
injectable.InjectXIDHealthEvent(0, 79, "GPU has fallen off the bus")

// Inject a thermal event
injectable.InjectThermalHealthEvent(0, 95, "High temperature")

// Inject a memory error
injectable.InjectMemoryHealthEvent(0, gpu.EventTypeECCDBE, 0, 1, "ECC error")

// Inject an NVLink error
injectable.InjectNVLinkHealthEvent(0, 0, "NVLink failure")

// Health checks will now detect the events
events, _ := injectable.CollectHealthEvents(ctx)
// events contains the injected health events

// Clear events
injectable.ClearHealthEvents()

// Or clear all errors
injectable.ClearAllErrors()
```

## Health events

Health events are the primary mechanism for reporting GPU issues. They are collected by the node daemon and sent to the control plane for CEL policy evaluation.

### HealthEvent structure

```go
type HealthEvent struct {
    Timestamp time.Time      // When the event occurred
    GPUIndex  int            // Which GPU (-1 for node-level)
    GPUUUID   string         // GPU unique identifier
    System    string         // DCGM health watch system
    EventType string         // Event category
    Metrics   map[string]any // Event-specific data
    Message   string         // Human-readable description
}
```

### Event types

| Type | Description |
|------|-------------|
| `xid` | NVIDIA XID error |
| `thermal` | Temperature warning |
| `power` | Power issue |
| `memory` | Memory error |
| `nvlink` | NVLink error |
| `pcie` | PCIe error |
| `ecc_sbe` | Single-bit ECC error |
| `ecc_dbe` | Double-bit ECC error |

### DCGM health watch systems

| System | Description |
|--------|-------------|
| `DCGM_HEALTH_WATCH_PCIE` | PCIe health |
| `DCGM_HEALTH_WATCH_NVLINK` | NVLink health |
| `DCGM_HEALTH_WATCH_PMU` | PMU health |
| `DCGM_HEALTH_WATCH_MCU` | MCU health |
| `DCGM_HEALTH_WATCH_MEM` | Memory health |
| `DCGM_HEALTH_WATCH_SM` | SM health |
| `DCGM_HEALTH_WATCH_INFOROM` | InfoROM health |
| `DCGM_HEALTH_WATCH_THERMAL` | Thermal health |
| `DCGM_HEALTH_WATCH_POWER` | Power health |
| `DCGM_HEALTH_WATCH_DRIVER` | Driver health |
| `DCGM_HEALTH_WATCH_NVSWITCH` | NVSwitch health |

## Data types

### DeviceInfo

Contains static information about a GPU device:

```go
type DeviceInfo struct {
    Index    int    // Device index (0-based)
    UUID     string // Unique device identifier
    Name     string // Device name (e.g., "NVIDIA H100 80GB HBM3")
    PCIBusID string // PCI bus identifier
    Memory   uint64 // Total memory in bytes
}
```

### HealthInfo

Contains current health metrics for a GPU device:

```go
type HealthInfo struct {
    Temperature    int     // GPU temperature in Celsius
    PowerUsage     float64 // Power consumption in watts
    MemoryUsed     uint64  // Used memory in bytes
    MemoryTotal    uint64  // Total memory in bytes
    GPUUtilization int     // GPU utilization percentage (0-100)
}
```

## Common XID codes

| Code | Severity | Description |
|------|----------|-------------|
| 13 | Fatal | Graphics Engine Exception |
| 31 | Fatal | GPU memory page fault |
| 32 | Fatal | Invalid or corrupted push buffer stream |
| 43 | Fatal | GPU stopped processing |
| 48 | Critical | Double Bit ECC Error |
| 63 | Warning | ECC page retirement or row remapping |
| 74 | Critical | NVLINK Error |
| 79 | Fatal | GPU has fallen off the bus |
| 92 | Fatal | High single-bit ECC error rate |
| 94 | Fatal | Contained ECC error |
| 95 | Fatal | Uncontained ECC error |
| 119 | Fatal | GSP RPC timeout |

For a complete list, see the [NVIDIA XID Errors documentation](https://docs.nvidia.com/deploy/xid-errors/index.html).

## Testing

Run all GPU tests:

```bash
go test ./pkg/gpu/... -v
```

### Test coverage

The package includes tests for:

- Injectable GPU initialization and shutdown.
- Device enumeration and info retrieval.
- Health metric generation.
- Health event injection and collection.
- Event type filtering.

### Testing with health events

```go
func TestXIDHandling(t *testing.T) {
    injectable := gpu.NewInjectable(2, "")
    injectable.Initialize(context.Background())
    defer injectable.Shutdown(context.Background())

    // Inject XID error
    injectable.InjectXIDHealthEvent(0, 79, "Test error")

    // Verify detection
    events, _ := injectable.CollectHealthEvents(context.Background())
    if len(events) != 1 {
        t.Errorf("Expected 1 event, got %d", len(events))
    }
    if events[0].Metrics["xid_code"].(int) != 79 {
        t.Errorf("Expected XID 79")
    }
}
```

## Environment variables

The node daemon uses these environment variables for GPU configuration:

- `NAVARCH_GPU_COUNT=N`: Number of GPUs to simulate (default: 8).
- `NAVARCH_GPU_TYPE=TYPE`: GPU type string (default: "NVIDIA H100 80GB HBM3").

## Integration with node daemon

The GPU manager is used for:

- Detecting and reporting GPU devices during registration.
- Running health checks (boot, GPU metrics, health events).
- Monitoring GPU metrics for heartbeats.
- Collecting health events for CEL policy evaluation.

## CEL policy evaluation

Health events are sent to the control plane where CEL policies evaluate them to determine node health status. Example CEL expressions:

```cel
// Mark unhealthy on fatal XID errors
event.event_type == "xid" && event.metrics.xid_code in [79, 119, 94, 95]

// Mark degraded on high temperature
event.event_type == "thermal" && event.metrics.temperature > 85

// Mark unhealthy on double-bit ECC errors
event.event_type == "ecc_dbe"
```

See `pkg/health/defaults.go` for default policy rules.

## XID collector

The `XIDCollector` monitors for NVIDIA XID errors using a dual-strategy approach:

### NVML events (primary)

When available, the collector uses NVML's native event API:

```go
eventSet, _ := nvml.EventSetCreate()
device.RegisterEvents(nvml.EventTypeXidCriticalError, eventSet)

// Background goroutine waits for events
data, _ := eventSet.Wait(100) // 100ms timeout
// data.EventData contains XID code
```

### Kernel log fallback

When NVML events are unavailable, the collector parses kernel logs:

```go
// Matches: NVRM: Xid (PCI:0000:41:00): 79, pid=12345, GPU has fallen off the bus
var xidPattern = regexp.MustCompile(`NVRM: Xid \(PCI:([^)]+)\): (\d+)(?:, (.*))?`)
```

Log sources (in order of preference):
1. `/dev/kmsg` - Real-time kernel ring buffer
2. `/var/log/kern.log` - Debian/Ubuntu
3. `/var/log/messages` - RHEL/CentOS

### XID severity classification

```go
severity := gpu.XIDSeverity(79)     // "critical"
desc := gpu.XIDDescription(79)       // "GPU has fallen off the bus"
```

| Severity | XID Codes |
|----------|-----------|
| critical | 13, 31, 43, 45, 48, 61, 62, 63, 64, 74, 79, 92, 94, 95 |
| warning | 8, 32, 38, 56, 57, 68, 69, 119 |
| info | All others |

## Extending

To add a new GPU backend (e.g., DCGM for enhanced monitoring):

1. Create a new file (e.g., `dcgm.go`).
2. Implement the `Manager` interface.
3. Implement `CollectHealthEvents` to return DCGM health watch events.

The NVML implementation (`nvml.go`) serves as a reference.
