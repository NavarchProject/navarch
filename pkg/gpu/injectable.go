package gpu

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Injectable is a fake GPU manager that supports failure injection for testing.
type Injectable struct {
	mu          sync.RWMutex
	deviceCount int
	devices     []*injectableDevice
	xidErrors   []*XIDError
	initialized bool

	nvmlError    error
	bootError    error
	deviceErrors map[int]error
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

	if g.nvmlError != nil {
		return 0, g.nvmlError
	}

	if !g.initialized {
		return 0, errors.New("not initialized")
	}

	return g.deviceCount, nil
}

func (g *Injectable) GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.nvmlError != nil {
		return nil, g.nvmlError
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

	if g.nvmlError != nil {
		return nil, g.nvmlError
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

func (g *Injectable) GetXIDErrors(ctx context.Context) ([]*XIDError, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.nvmlError != nil {
		return nil, g.nvmlError
	}

	if !g.initialized {
		return nil, errors.New("not initialized")
	}

	result := make([]*XIDError, len(g.xidErrors))
	copy(result, g.xidErrors)
	return result, nil
}

// InjectXIDError adds an XID error for testing.
func (g *Injectable) InjectXIDError(gpuIndex, xidCode int, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	deviceID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		deviceID = g.devices[gpuIndex].info.UUID
	}

	g.xidErrors = append(g.xidErrors, &XIDError{
		Timestamp: time.Now().Format(time.RFC3339),
		DeviceID:  deviceID,
		XIDCode:   xidCode,
		Message:   message,
	})
}

// ClearXIDErrors removes all XID errors.
func (g *Injectable) ClearXIDErrors() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.xidErrors = nil
}

// ClearXIDError removes a specific XID error by GPU index and code.
func (g *Injectable) ClearXIDError(gpuIndex, xidCode int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	deviceID := ""
	if gpuIndex >= 0 && gpuIndex < g.deviceCount {
		deviceID = g.devices[gpuIndex].info.UUID
	}

	var remaining []*XIDError
	removed := false
	for _, err := range g.xidErrors {
		// Only remove the first matching error to handle duplicates correctly
		if !removed && err.DeviceID == deviceID && err.XIDCode == xidCode {
			removed = true
			continue
		}
		remaining = append(remaining, err)
	}
	g.xidErrors = remaining
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

// InjectNVMLError makes all NVML operations return an error.
func (g *Injectable) InjectNVMLError(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nvmlError = err
}

// ClearNVMLError removes the global NVML error.
func (g *Injectable) ClearNVMLError() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nvmlError = nil
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

// ClearAllErrors resets all injected errors.
func (g *Injectable) ClearAllErrors() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.xidErrors = nil
	g.nvmlError = nil
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

	if len(g.xidErrors) > 0 || g.nvmlError != nil || g.bootError != nil || len(g.deviceErrors) > 0 {
		return true
	}

	for _, d := range g.devices {
		if d.temperatureSpike > 0 {
			return true
		}
	}

	return false
}

