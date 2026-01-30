package gpu

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Injectable is a fake GPU manager that supports failure injection for testing.
// It implements the Manager interface with CollectHealthEvents for CEL policy evaluation.
type Injectable struct {
	mu          sync.RWMutex
	deviceCount int
	devices     []*injectableDevice
	initialized bool

	// Errors that affect all operations
	backendError error
	bootError    error
	deviceErrors map[int]error

	// Health events for CEL policy evaluation
	healthEvents []HealthEvent
}

type injectableDevice struct {
	info             DeviceInfo
	baseHealth       HealthInfo
	temperatureSpike int
}

// NewInjectable creates a new injectable GPU manager with the specified device count.
func NewInjectable(deviceCount int, gpuType string) *Injectable {
	if gpuType == "" {
		gpuType = "NVIDIA H100 80GB HBM3"
	}
	devices := make([]*injectableDevice, deviceCount)
	for i := 0; i < deviceCount; i++ {
		devices[i] = &injectableDevice{
			info: DeviceInfo{
				Index:    i,
				UUID:     fmt.Sprintf("GPU-%08d-%04d-%04d-%04d-%012d", i, i, i, i, i),
				Name:     gpuType,
				PCIBusID: fmt.Sprintf("0000:%02x:00.0", i),
				Memory:   80 * 1024 * 1024 * 1024,
			},
			baseHealth: HealthInfo{
				Temperature:    45,
				PowerUsage:     300.0,
				MemoryUsed:     40 * 1024 * 1024 * 1024,
				MemoryTotal:    80 * 1024 * 1024 * 1024,
				GPUUtilization: 75,
			},
		}
	}
	return &Injectable{
		deviceCount:  deviceCount,
		devices:      devices,
		deviceErrors: make(map[int]error),
	}
}

func (g *Injectable) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.bootError != nil {
		return g.bootError
	}

	if g.initialized {
		return errors.New("already initialized")
	}

	g.initialized = true
	return nil
}

func (g *Injectable) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.initialized {
		return errors.New("not initialized")
	}

	g.initialized = false
	return nil
}

func (g *Injectable) GetDeviceCount(ctx context.Context) (int, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.backendError != nil {
		return 0, g.backendError
	}

	if !g.initialized {
		return 0, errors.New("not initialized")
	}

	return g.deviceCount, nil
}

func (g *Injectable) GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.backendError != nil {
		return nil, g.backendError
	}

	if !g.initialized {
		return nil, errors.New("not initialized")
	}

	if err := g.deviceErrors[index]; err != nil {
		return nil, err
	}

	if index < 0 || index >= g.deviceCount {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	info := g.devices[index].info
	return &info, nil
}

func (g *Injectable) GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.backendError != nil {
		return nil, g.backendError
	}

	if !g.initialized {
		return nil, errors.New("not initialized")
	}

	if err := g.deviceErrors[index]; err != nil {
		return nil, err
	}

	if index < 0 || index >= g.deviceCount {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	health := g.devices[index].baseHealth
	if g.devices[index].temperatureSpike > 0 {
		health.Temperature = g.devices[index].temperatureSpike
	}

	return &health, nil
}

// CollectHealthEvents returns and clears all pending health events.
func (g *Injectable) CollectHealthEvents(ctx context.Context) ([]HealthEvent, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.backendError != nil {
		return nil, g.backendError
	}

	if !g.initialized {
		return nil, errors.New("not initialized")
	}

	events := make([]HealthEvent, len(g.healthEvents))
	copy(events, g.healthEvents)
	g.healthEvents = nil

	return events, nil
}

// InjectHealthEvent adds a health event for testing.
func (g *Injectable) InjectHealthEvent(event HealthEvent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.healthEvents = append(g.healthEvents, event)
}

// InjectXIDHealthEvent adds an XID error as a health event.
func (g *Injectable) InjectXIDHealthEvent(gpuIndex, xidCode int, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	gpuUUID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		gpuUUID = g.devices[gpuIndex].info.UUID
	}

	event := NewXIDEvent(gpuIndex, gpuUUID, xidCode, message)
	g.healthEvents = append(g.healthEvents, event)
}

// InjectThermalHealthEvent adds a thermal event.
func (g *Injectable) InjectThermalHealthEvent(gpuIndex, temperature int, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	gpuUUID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		gpuUUID = g.devices[gpuIndex].info.UUID
		g.devices[gpuIndex].temperatureSpike = temperature
	}

	event := NewThermalEvent(gpuIndex, gpuUUID, temperature, message)
	g.healthEvents = append(g.healthEvents, event)
}

// InjectMemoryHealthEvent adds a memory/ECC event.
func (g *Injectable) InjectMemoryHealthEvent(gpuIndex int, eventType string, sbeCount, dbeCount int, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	gpuUUID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		gpuUUID = g.devices[gpuIndex].info.UUID
	}

	event := NewMemoryEvent(gpuIndex, gpuUUID, eventType, sbeCount, dbeCount, message)
	g.healthEvents = append(g.healthEvents, event)
}

// InjectNVLinkHealthEvent adds an NVLink error event.
func (g *Injectable) InjectNVLinkHealthEvent(gpuIndex, linkID int, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	gpuUUID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		gpuUUID = g.devices[gpuIndex].info.UUID
	}

	event := NewNVLinkEvent(gpuIndex, gpuUUID, linkID, message)
	g.healthEvents = append(g.healthEvents, event)
}

// InjectTemperatureSpike sets a high temperature on a specific GPU.
func (g *Injectable) InjectTemperatureSpike(gpuIndex, temperature int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		g.devices[gpuIndex].temperatureSpike = temperature
	}
}

// ClearTemperatureSpike resets temperature to normal.
func (g *Injectable) ClearTemperatureSpike(gpuIndex int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		g.devices[gpuIndex].temperatureSpike = 0
	}
}

// InjectBackendError makes all backend operations return an error.
// This simulates DCGM/driver failures.
func (g *Injectable) InjectBackendError(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.backendError = err
}

// ClearBackendError removes the global backend error.
func (g *Injectable) ClearBackendError() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.backendError = nil
}

// InjectBootError makes initialization fail.
func (g *Injectable) InjectBootError(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bootError = err
}

// ClearBootError removes the boot error.
func (g *Injectable) ClearBootError() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bootError = nil
}

// InjectDeviceError makes a specific device return errors.
func (g *Injectable) InjectDeviceError(gpuIndex int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deviceErrors[gpuIndex] = err
}

// ClearDeviceError removes an error from a specific device.
func (g *Injectable) ClearDeviceError(gpuIndex int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.deviceErrors, gpuIndex)
}

// ClearHealthEvents removes all pending health events.
func (g *Injectable) ClearHealthEvents() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.healthEvents = nil
}

// ClearHealthEventsByType removes health events of a specific type.
func (g *Injectable) ClearHealthEventsByType(eventType string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var remaining []HealthEvent
	for _, e := range g.healthEvents {
		if e.EventType != eventType {
			remaining = append(remaining, e)
		}
	}
	g.healthEvents = remaining
}

// ClearHealthEventsByGPU removes health events for a specific GPU.
func (g *Injectable) ClearHealthEventsByGPU(gpuIndex int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var remaining []HealthEvent
	for _, e := range g.healthEvents {
		if e.GPUIndex != gpuIndex {
			remaining = append(remaining, e)
		}
	}
	g.healthEvents = remaining
}

// ClearAllErrors resets all injected errors and health events.
func (g *Injectable) ClearAllErrors() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.healthEvents = nil
	g.backendError = nil
	g.bootError = nil
	g.deviceErrors = make(map[int]error)
	for i := 0; i < g.deviceCount; i++ {
		g.devices[i].temperatureSpike = 0
	}
}

// HasActiveFailures returns true if any failures are currently injected.
func (g *Injectable) HasActiveFailures() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.healthEvents) > 0 || g.backendError != nil || g.bootError != nil || len(g.deviceErrors) > 0 {
		return true
	}

	for _, d := range g.devices {
		if d.temperatureSpike > 0 {
			return true
		}
	}

	return false
}
