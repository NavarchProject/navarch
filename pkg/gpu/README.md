# GPU package

The GPU package provides an abstraction layer for interacting with NVIDIA GPUs. It supports both real hardware via NVML and simulated hardware for development and testing.

## Overview

The package provides:

- A `Manager` interface for GPU operations.
- NVML implementation for real NVIDIA GPUs.
- Fake implementation for development without hardware.
- XID error parsing from system logs.
- XID severity classification and descriptions.

## Manager interface

The `Manager` interface defines operations for GPU management:

```go
type Manager interface {
    Initialize(ctx context.Context) error
    Shutdown(ctx context.Context) error
    GetDeviceCount(ctx context.Context) (int, error)
    GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error)
    GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error)
    GetXIDErrors(ctx context.Context) ([]*XIDError, error)
}
```

## Implementations

### NVML (production)

The NVML implementation uses NVIDIA's Management Library to interact with real GPUs.

```go
manager := gpu.NewNVML()
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

- Device enumeration and information retrieval.
- Real-time health metrics (temperature, power, utilization).
- Memory usage monitoring.
- XID error detection via system log parsing.

Requirements:

- NVIDIA GPU with driver installed.
- NVML library available (included with NVIDIA drivers).

### Fake (development)

The Fake implementation simulates GPU hardware for development and testing.

```go
manager := gpu.NewFake(8) // Simulate 8 GPUs
if err := manager.Initialize(ctx); err != nil {
    log.Fatal(err)
}
defer manager.Shutdown(ctx)
```

Features:

- Configurable number of simulated GPUs.
- Realistic device information (H100 80GB HBM3).
- Randomized health metrics within normal ranges.
- XID error injection for testing failure scenarios.

XID injection example:

```go
fake := gpu.NewFake(4)
fake.Initialize(ctx)

// Inject an XID error
fake.InjectXIDError("GPU-0", 79, "GPU has fallen off the bus")

// Health checks will now detect the error
errors, _ := fake.GetXIDErrors(ctx)
// errors contains the injected XID

// Clear errors
fake.ClearXIDErrors()
```

## Auto-detection

Use `IsNVMLAvailable()` to check if real GPU hardware is available:

```go
var manager gpu.Manager

if gpu.IsNVMLAvailable() {
    manager = gpu.NewNVML()
} else {
    manager = gpu.NewFake(8)
}
```

The node daemon performs this detection automatically.

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

### XIDError

Represents an NVIDIA XID error:

```go
type XIDError struct {
    Timestamp string // When the error occurred
    DeviceID  string // PCI device ID
    XIDCode   int    // XID error code
    Message   string // Error message from logs
}
```

## XID error handling

XID errors are NVIDIA GPU errors reported in system logs. The package provides tools for parsing and classifying these errors.

### Parsing XID errors

The NVML implementation automatically parses XID errors from dmesg:

```go
errors, err := manager.GetXIDErrors(ctx)
for _, e := range errors {
    fmt.Printf("XID %d on %s: %s\n", e.XIDCode, e.DeviceID, e.Message)
}
```

### XID severity

Use `XIDSeverity()` to classify error severity:

```go
severity := gpu.XIDSeverity(79) // Returns "fatal"
```

Severity levels:

- **fatal**: Hardware failure requiring node replacement.
- **critical**: Serious error (e.g., double-bit ECC).
- **warning**: Recoverable condition (e.g., row remapping).
- **info**: Informational or unknown error.

### XID descriptions

Use `XIDDescription()` for human-readable descriptions:

```go
desc := gpu.XIDDescription(79) // Returns "GPU has fallen off the bus"
```

### Fatal XID detection

Use `IsFatalXID()` to check if an error requires node replacement:

```go
if gpu.IsFatalXID(error.XIDCode) {
    // Node should be cordoned and replaced
}
```

Fatal XID codes include: 13, 31, 32, 43, 45, 64, 68, 69, 79, 92, 94, 95, 119.

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

On machines without NVIDIA GPUs, hardware-specific tests are automatically skipped.

### Test coverage

The package includes tests for:

- Fake GPU initialization and shutdown.
- Device enumeration and info retrieval.
- Health metric generation.
- XID error injection and detection.
- XID parsing from various log formats.
- XID severity classification.
- NVML initialization errors (without hardware).
- Interface compliance verification.

### Testing with fake XID errors

```go
func TestXIDHandling(t *testing.T) {
    fake := gpu.NewFake(2)
    fake.Initialize(context.Background())
    defer fake.Shutdown(context.Background())

    // Inject error
    fake.InjectXIDError("GPU-0", 79, "Test error")

    // Verify detection
    errors, _ := fake.GetXIDErrors(context.Background())
    if len(errors) != 1 {
        t.Errorf("Expected 1 error, got %d", len(errors))
    }
    if errors[0].XIDCode != 79 {
        t.Errorf("Expected XID 79, got %d", errors[0].XIDCode)
    }
}
```

## Environment variables

The node daemon uses these environment variables for GPU configuration:

- `NAVARCH_FAKE_GPU=true`: Force fake GPU mode even when NVML is available.
- `NAVARCH_GPU_COUNT=N`: Number of fake GPUs to simulate (default: 8).

## Integration with node daemon

The node daemon automatically selects the appropriate GPU manager:

1. If `NAVARCH_FAKE_GPU=true`, use Fake.
2. If NVML is available, use NVML.
3. Otherwise, fall back to Fake.

The GPU manager is used for:

- Detecting and reporting GPU devices during registration.
- Running health checks (boot, NVML, XID).
- Monitoring GPU metrics for heartbeats.

## Extending

To add a new GPU backend (e.g., AMD ROCm):

1. Create a new file (e.g., `rocm.go`).
2. Implement the `Manager` interface.
3. Add an availability check function.
4. Update the node daemon's auto-detection logic.

Example skeleton:

```go
type ROCm struct {
    // ...
}

func NewROCm() *ROCm {
    return &ROCm{}
}

func (r *ROCm) Initialize(ctx context.Context) error {
    // Initialize ROCm/HIP
}

// Implement remaining Manager methods...

func IsROCmAvailable() bool {
    // Check if ROCm is available
}
```

