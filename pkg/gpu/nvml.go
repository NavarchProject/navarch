package gpu

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// NVML implements Manager using the NVIDIA Management Library.
type NVML struct {
	mu          sync.RWMutex
	initialized bool
	devices     []nvml.Device

	// healthEvents stores events collected since the last call to CollectHealthEvents.
	// For MVP, this remains empty. Task 2 adds XID collection.
	healthEvents []HealthEvent
}

// NewNVML creates a new NVML-based GPU manager.
func NewNVML() *NVML {
	return &NVML{}
}

func (m *NVML) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return errors.New("already initialized")
	}

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvml.Init failed: %v", nvmlError(ret))
	}

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		nvml.Shutdown()
		return fmt.Errorf("nvml.DeviceGetCount failed: %v", nvmlError(ret))
	}

	m.devices = make([]nvml.Device, count)
	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			nvml.Shutdown()
			return fmt.Errorf("nvml.DeviceGetHandleByIndex(%d) failed: %v", i, nvmlError(ret))
		}
		m.devices[i] = device
	}

	m.initialized = true
	return nil
}

func (m *NVML) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.initialized {
		return errors.New("not initialized")
	}

	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvml.Shutdown failed: %v", nvmlError(ret))
	}

	m.devices = nil
	m.initialized = false
	return nil
}

func (m *NVML) GetDeviceCount(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.initialized {
		return 0, errors.New("not initialized")
	}

	return len(m.devices), nil
}

func (m *NVML) GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.initialized {
		return nil, errors.New("not initialized")
	}

	if index < 0 || index >= len(m.devices) {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	device := m.devices[index]

	uuid, ret := device.GetUUID()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetUUID failed: %v", nvmlError(ret))
	}

	name, ret := device.GetName()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetName failed: %v", nvmlError(ret))
	}

	pciInfo, ret := device.GetPciInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetPciInfo failed: %v", nvmlError(ret))
	}

	memInfo, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetMemoryInfo failed: %v", nvmlError(ret))
	}

	return &DeviceInfo{
		Index:    index,
		UUID:     uuid,
		Name:     name,
		PCIBusID: pciBusIDToString(pciInfo.BusId),
		Memory:   memInfo.Total,
	}, nil
}

func (m *NVML) GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.initialized {
		return nil, errors.New("not initialized")
	}

	if index < 0 || index >= len(m.devices) {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	device := m.devices[index]

	temp, ret := device.GetTemperature(nvml.TEMPERATURE_GPU)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetTemperature failed: %v", nvmlError(ret))
	}

	// GetPowerUsage returns milliwatts
	powerMw, ret := device.GetPowerUsage()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetPowerUsage failed: %v", nvmlError(ret))
	}

	memInfo, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetMemoryInfo failed: %v", nvmlError(ret))
	}

	util, ret := device.GetUtilizationRates()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GetUtilizationRates failed: %v", nvmlError(ret))
	}

	return &HealthInfo{
		Temperature:    int(temp),
		PowerUsage:     float64(powerMw) / 1000.0, // Convert mW to W
		MemoryUsed:     memInfo.Used,
		MemoryTotal:    memInfo.Total,
		GPUUtilization: int(util.Gpu),
	}, nil
}

// CollectHealthEvents returns health events since the last collection.
// For MVP, this returns an empty slice. XID collection is added in Task 2.
func (m *NVML) CollectHealthEvents(ctx context.Context) ([]HealthEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.initialized {
		return nil, errors.New("not initialized")
	}

	events := make([]HealthEvent, len(m.healthEvents))
	copy(events, m.healthEvents)
	m.healthEvents = nil

	return events, nil
}

// AddHealthEvent adds a health event to be returned by the next CollectHealthEvents call.
// This is used by external collectors (e.g., XID collector in Task 2).
func (m *NVML) AddHealthEvent(event HealthEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthEvents = append(m.healthEvents, event)
}

// nvmlError converts an NVML return code to an error string.
func nvmlError(ret nvml.Return) string {
	return ret.Error()
}

// pciBusIDToString converts a fixed-size byte array to a string, trimming null bytes.
func pciBusIDToString(busId [32]uint8) string {
	n := 0
	for i, b := range busId {
		if b == 0 {
			n = i
			break
		}
		n = i + 1
	}
	return string(busId[:n])
}

// IsNVMLAvailable checks if NVML can be initialized.
// This is useful for determining whether to use the NVML manager or fall back to fake.
func IsNVMLAvailable() bool {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return false
	}
	nvml.Shutdown()
	return true
}
