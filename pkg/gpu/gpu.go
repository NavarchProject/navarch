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

// Manager provides access to GPU information and health monitoring.
// Implementations collect health events that are evaluated by CEL policies
// on the control plane to determine node health status.
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

	// CollectHealthEvents returns health events since the last collection.
	// Events are cleared after collection.
	// These events are sent to the control plane for CEL policy evaluation.
	CollectHealthEvents(ctx context.Context) ([]HealthEvent, error)
}
