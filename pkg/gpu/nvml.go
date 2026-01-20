package gpu

import (
	"context"
	"fmt"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// NVML is a GPU manager that uses NVIDIA's NVML library.
type NVML struct {
	mu          sync.RWMutex
	initialized bool
	xidParser   *XIDParser
}

// NewNVML creates a new NVML GPU manager.
func NewNVML() *NVML {
	return &NVML{
		xidParser: NewXIDParser(),
	}
}

// Initialize initializes the NVML library.
func (n *NVML) Initialize(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.initialized {
		return fmt.Errorf("already initialized")
	}

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("failed to initialize NVML: %v", nvml.ErrorString(ret))
	}

	n.initialized = true
	return nil
}

// Shutdown shuts down the NVML library.
func (n *NVML) Shutdown(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.initialized {
		return fmt.Errorf("not initialized")
	}

	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("failed to shutdown NVML: %v", nvml.ErrorString(ret))
	}

	n.initialized = false
	return nil
}

// GetDeviceCount returns the number of NVIDIA GPUs in the system.
func (n *NVML) GetDeviceCount(ctx context.Context) (int, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return 0, fmt.Errorf("not initialized")
	}

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("failed to get device count: %v", nvml.ErrorString(ret))
	}

	return count, nil
}

// GetDeviceInfo returns information about a specific GPU device.
func (n *NVML) GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	device, ret := nvml.DeviceGetHandleByIndex(index)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get device handle: %v", nvml.ErrorString(ret))
	}

	uuid, ret := device.GetUUID()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get device UUID: %v", nvml.ErrorString(ret))
	}

	name, ret := device.GetName()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get device name: %v", nvml.ErrorString(ret))
	}

	pciInfo, ret := device.GetPciInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get PCI info: %v", nvml.ErrorString(ret))
	}

	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get memory info: %v", nvml.ErrorString(ret))
	}

	// Convert PCI bus ID from byte array to string
	busID := ""
	for _, b := range pciInfo.BusId {
		if b == 0 {
			break
		}
		busID += string(b)
	}

	return &DeviceInfo{
		Index:    index,
		UUID:     uuid,
		Name:     name,
		PCIBusID: busID,
		Memory:   memory.Total,
	}, nil
}

// GetDeviceHealth returns current health metrics for a specific GPU device.
func (n *NVML) GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	device, ret := nvml.DeviceGetHandleByIndex(index)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get device handle: %v", nvml.ErrorString(ret))
	}

	temp, ret := device.GetTemperature(nvml.TEMPERATURE_GPU)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get temperature: %v", nvml.ErrorString(ret))
	}

	power, ret := device.GetPowerUsage()
	if ret != nvml.SUCCESS {
		// Power reading may not be supported on all GPUs
		power = 0
	}

	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get memory info: %v", nvml.ErrorString(ret))
	}

	utilization, ret := device.GetUtilizationRates()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to get utilization: %v", nvml.ErrorString(ret))
	}

	return &HealthInfo{
		Temperature:    int(temp),
		PowerUsage:     float64(power) / 1000.0, // Convert milliwatts to watts
		MemoryUsed:     memory.Used,
		MemoryTotal:    memory.Total,
		GPUUtilization: int(utilization.Gpu),
	}, nil
}

// GetXIDErrors returns any XID errors detected by parsing system logs.
func (n *NVML) GetXIDErrors(ctx context.Context) ([]*XIDError, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	return n.xidParser.Parse()
}

// IsNVMLAvailable checks if NVML is available on the system.
// This can be used to decide whether to use NVML or fall back to fake.
func IsNVMLAvailable() bool {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return false
	}
	nvml.Shutdown()
	return true
}
