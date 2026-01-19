package gpu

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Fake is a fake GPU implementation for testing and development.
type Fake struct {
	mu          sync.RWMutex
	deviceCount int
	devices     []*fakeDevice
	xidErrors   []*XIDError
	initialized bool
}

type fakeDevice struct {
	info   DeviceInfo
	health HealthInfo
}

// NewFake creates a new fake GPU interface with the specified number of devices.
func NewFake(deviceCount int) *Fake {
	return &Fake{
		deviceCount: deviceCount,
		devices:     make([]*fakeDevice, deviceCount),
	}
}

// Initialize prepares the fake GPU interface.
func (f *Fake) Initialize(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.initialized {
		return fmt.Errorf("already initialized")
	}

	for i := 0; i < f.deviceCount; i++ {
		f.devices[i] = &fakeDevice{
			info: DeviceInfo{
				Index:    i,
				UUID:     fmt.Sprintf("GPU-%08d-%04d-%04d-%04d-%012d", i, i, i, i, i),
				Name:     "NVIDIA H100 80GB HBM3",
				PCIBusID: fmt.Sprintf("0000:%02x:00.0", i),
				Memory:   80 * 1024 * 1024 * 1024, // 80GB
			},
			health: HealthInfo{
				Temperature:    30 + rand.Intn(20),
				PowerUsage:     100.0 + rand.Float64()*200.0,
				MemoryUsed:     10 * 1024 * 1024 * 1024,
				MemoryTotal:    80 * 1024 * 1024 * 1024,
				GPUUtilization: rand.Intn(100),
			},
		}
	}

	f.initialized = true
	return nil
}

// Shutdown cleans up the fake GPU interface.
func (f *Fake) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.initialized {
		return fmt.Errorf("not initialized")
	}

	f.devices = nil
	f.xidErrors = nil
	f.initialized = false
	return nil
}

// GetDeviceCount returns the number of fake GPU devices.
func (f *Fake) GetDeviceCount(ctx context.Context) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return 0, fmt.Errorf("not initialized")
	}

	return f.deviceCount, nil
}

// GetDeviceInfo returns information about a fake GPU device.
func (f *Fake) GetDeviceInfo(ctx context.Context, index int) (*DeviceInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	if index < 0 || index >= f.deviceCount {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	info := f.devices[index].info
	return &info, nil
}

// GetDeviceHealth returns current health metrics for a fake GPU device.
func (f *Fake) GetDeviceHealth(ctx context.Context, index int) (*HealthInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	if index < 0 || index >= f.deviceCount {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	health := f.devices[index].health
	health.Temperature = 30 + rand.Intn(20)
	health.PowerUsage = 100.0 + rand.Float64()*200.0
	health.GPUUtilization = rand.Intn(100)

	return &health, nil
}

// GetXIDErrors returns any fake XID errors.
func (f *Fake) GetXIDErrors(ctx context.Context) ([]*XIDError, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	errors := make([]*XIDError, len(f.xidErrors))
	copy(errors, f.xidErrors)
	return errors, nil
}

// InjectXIDError injects a fake XID error for testing.
func (f *Fake) InjectXIDError(deviceID string, xidCode int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.xidErrors = append(f.xidErrors, &XIDError{
		Timestamp: time.Now().Format(time.RFC3339),
		DeviceID:  deviceID,
		XIDCode:   xidCode,
		Message:   message,
	})
}

// ClearXIDErrors clears all fake XID errors.
func (f *Fake) ClearXIDErrors() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.xidErrors = nil
}

