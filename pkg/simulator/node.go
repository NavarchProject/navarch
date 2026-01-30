package simulator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/node"
)

// SimulatedNode wraps a real node.Node with an injectable GPU for failure simulation.
type SimulatedNode struct {
	spec   NodeSpec
	node   *node.Node
	gpu    *gpu.Injectable
	logger *slog.Logger

	mu       sync.RWMutex
	failures []InjectedFailure
	running  bool
	cancel   context.CancelFunc
}

// InjectedFailure represents a failure condition injected into the node.
type InjectedFailure struct {
	Type     string
	XIDCode  int
	GPUIndex int
	Message  string
	Since    time.Time
}

// NewSimulatedNode creates a new simulated node using the real node package.
func NewSimulatedNode(spec NodeSpec, controlPlaneAddr string, logger *slog.Logger) *SimulatedNode {
	if logger == nil {
		logger = slog.Default()
	}

	spec.ControlPlaneAddr = controlPlaneAddr
	gpuManager := gpu.NewInjectable(spec.GPUCount, spec.GPUType)

	return &SimulatedNode{
		spec:     spec,
		gpu:      gpuManager,
		logger:   logger.With(slog.String("node_id", spec.ID)),
		failures: make([]InjectedFailure, 0),
	}
}

// Start begins the simulated node's operation using the real node implementation.
func (n *SimulatedNode) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return errors.New("node already running")
	}

	cfg := node.Config{
		ControlPlaneAddr: n.spec.ControlPlaneAddr,
		NodeID:           n.spec.ID,
		Provider:         n.spec.Provider,
		Region:           n.spec.Region,
		Zone:             n.spec.Zone,
		InstanceType:     n.spec.InstanceType,
		Labels:           n.spec.Labels,
		GPU:              n.gpu,
	}

	realNode, err := node.New(cfg, n.logger)
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("failed to create node: %w", err)
	}
	n.node = realNode

	ctx, n.cancel = context.WithCancel(ctx)
	n.running = true
	n.mu.Unlock()

	if err := n.node.Start(ctx); err != nil {
		n.mu.Lock()
		n.running = false
		n.mu.Unlock()
		return err
	}

	return nil
}

// Stop halts the simulated node.
func (n *SimulatedNode) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cancel != nil {
		n.cancel()
	}

	if n.node != nil {
		n.node.Stop()
	}

	n.running = false
}

// InjectFailure adds a failure condition to the node via the injectable GPU.
// This injects both legacy XID errors and new HealthEvents for compatibility.
func (n *SimulatedNode) InjectFailure(failure InjectedFailure) {
	n.mu.Lock()
	defer n.mu.Unlock()

	failure.Since = time.Now()
	n.failures = append(n.failures, failure)

	switch failure.Type {
	case "xid_error":
		// Use new HealthEvent-based injection (also adds legacy XID error)
		n.gpu.InjectXIDHealthEvent(failure.GPUIndex, failure.XIDCode, failure.Message)

	case "temperature":
		temp := 95
		if failure.GPUIndex >= 0 {
			n.gpu.InjectThermalHealthEvent(failure.GPUIndex, temp, failure.Message)
		} else {
			for i := 0; i < n.spec.GPUCount; i++ {
				n.gpu.InjectThermalHealthEvent(i, temp, failure.Message)
			}
		}

	case "nvml_failure", "backend_error":
		n.gpu.InjectBackendError(errors.New(failure.Message))

	case "boot_failure":
		n.gpu.InjectBootError(errors.New(failure.Message))

	case "device_error":
		n.gpu.InjectDeviceError(failure.GPUIndex, errors.New(failure.Message))

	case "memory_error":
		// New failure type for ECC errors
		n.gpu.InjectMemoryHealthEvent(failure.GPUIndex, gpu.EventTypeECCDBE, 0, 1, failure.Message)

	case "nvlink_error":
		// New failure type for NVLink errors
		n.gpu.InjectNVLinkHealthEvent(failure.GPUIndex, 0, failure.Message)

	case "network":
		// Network failures are simulated by stopping heartbeats/health checks.
		// The real node will naturally miss heartbeats when context is cancelled.
		// For now, we just record it - could be enhanced with network simulation.
	}

	n.logger.Info("injected failure",
		slog.String("type", failure.Type),
		slog.Int("xid_code", failure.XIDCode),
		slog.Int("gpu_index", failure.GPUIndex),
	)
}

// ClearFailures removes all injected failures.
func (n *SimulatedNode) ClearFailures() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.failures = make([]InjectedFailure, 0)
	n.gpu.ClearAllErrors()
	n.gpu.ClearHealthEvents()
	n.logger.Info("cleared all failures")
}

// RecoverFailure removes a specific failure type.
func (n *SimulatedNode) RecoverFailure(failureType string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	var remaining []InjectedFailure
	for _, f := range n.failures {
		if f.Type != failureType {
			remaining = append(remaining, f)
		} else {
			n.clearSpecificFailure(f)
		}
	}
	n.failures = remaining
	n.logger.Info("recovered from failure", slog.String("type", failureType))
}

func (n *SimulatedNode) clearSpecificFailure(f InjectedFailure) {
	switch f.Type {
	case "xid_error":
		n.gpu.ClearHealthEventsByType(gpu.EventTypeXID)
	case "temperature":
		if f.GPUIndex >= 0 {
			n.gpu.ClearTemperatureSpike(f.GPUIndex)
			n.gpu.ClearHealthEventsByGPU(f.GPUIndex)
		} else {
			for i := 0; i < n.spec.GPUCount; i++ {
				n.gpu.ClearTemperatureSpike(i)
			}
			n.gpu.ClearHealthEventsByType(gpu.EventTypeThermal)
		}
	case "nvml_failure", "backend_error":
		n.gpu.ClearBackendError()
	case "boot_failure":
		n.gpu.ClearBootError()
	case "device_error":
		n.gpu.ClearDeviceError(f.GPUIndex)
	case "memory_error":
		n.gpu.ClearHealthEventsByType(gpu.EventTypeECCDBE)
		n.gpu.ClearHealthEventsByType(gpu.EventTypeECCSBE)
	case "nvlink_error":
		n.gpu.ClearHealthEventsByType(gpu.EventTypeNVLink)
	}
}

// GetFailures returns current injected failures.
func (n *SimulatedNode) GetFailures() []InjectedFailure {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]InjectedFailure, len(n.failures))
	copy(result, n.failures)
	return result
}

// ID returns the node's ID.
func (n *SimulatedNode) ID() string {
	return n.spec.ID
}

// Spec returns the node's specification.
func (n *SimulatedNode) Spec() NodeSpec {
	return n.spec
}

// GPU returns the injectable GPU manager for direct manipulation.
func (n *SimulatedNode) GPU() *gpu.Injectable {
	return n.gpu
}
