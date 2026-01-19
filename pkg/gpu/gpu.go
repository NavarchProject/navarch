package gpu

import "context"

// DeviceInfo contains information about a GPU device.
type DeviceInfo struct {
	Index    int
	UUID     string
	Name     string
	PCIBusID string
	Memory   uint64
}

// HealthInfo contains health metrics for a GPU device.
type HealthInfo struct {
	Temperature    int
	PowerUsage     float64
	MemoryUsed     uint64
	MemoryTotal    uint64
	GPUUtilization int
}

// XIDError represents an XID error detected on a GPU.
type XIDError struct {
	Timestamp string
	DeviceID  string
	XIDCode   int
	Message   string
}

// Manager provides access to GPU information and health metrics.
type Manager interface {
	// Initialize prepares the GPU manager for use.
	Initialize(ctx context.Context) error

	// Shutdown cleans up resources.
	Shutdown(ctx context.Context) error

	// GetDeviceCount returns the number of GPU devices available.
	GetDeviceCount(ctx context.Context) (int, error)

	// GetDeviceInfo returns information about a specific GPU device.
	GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error)

	// GetDeviceHealth returns current health metrics for a specific GPU device.
	GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error)

	// GetXIDErrors returns any XID errors detected since last check.
	// This may read from dmesg or other system logs.
	GetXIDErrors(ctx context.Context) ([]*XIDError, error)
}
