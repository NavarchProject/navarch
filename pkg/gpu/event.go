package gpu

import (
	"context"
	"time"
)

// DCGM Health Watch System constants.
// These correspond to NVIDIA DCGM health watch systems.
const (
	HealthSystemPCIE    = "DCGM_HEALTH_WATCH_PCIE"
	HealthSystemNVLink  = "DCGM_HEALTH_WATCH_NVLINK"
	HealthSystemPMU     = "DCGM_HEALTH_WATCH_PMU"
	HealthSystemMCU     = "DCGM_HEALTH_WATCH_MCU"
	HealthSystemMem     = "DCGM_HEALTH_WATCH_MEM"
	HealthSystemSM      = "DCGM_HEALTH_WATCH_SM"
	HealthSystemInforom = "DCGM_HEALTH_WATCH_INFOROM"
	HealthSystemThermal = "DCGM_HEALTH_WATCH_THERMAL"
	HealthSystemPower   = "DCGM_HEALTH_WATCH_POWER"
	HealthSystemDriver  = "DCGM_HEALTH_WATCH_DRIVER"
	HealthSystemNVSwitch = "DCGM_HEALTH_WATCH_NVSWITCH"
)

// Event types for health events.
const (
	EventTypeXID         = "xid"
	EventTypeThermal     = "thermal"
	EventTypePower       = "power"
	EventTypeMemory      = "memory"
	EventTypeNVLink      = "nvlink"
	EventTypePCIE        = "pcie"
	EventTypeDriverError = "driver_error"
	EventTypeECCSBE      = "ecc_sbe" // Single-bit ECC error
	EventTypeECCDBE      = "ecc_dbe" // Double-bit ECC error
)

// HealthEvent represents a GPU health event from DCGM or other monitoring backends.
// Events are sent to the control plane where CEL policies evaluate them.
type HealthEvent struct {
	// Timestamp when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// GPUIndex identifies which GPU reported the event (-1 for node-level events).
	GPUIndex int `json:"gpu_index"`

	// GPUUUID is the unique identifier for the GPU (empty for node-level events).
	GPUUUID string `json:"gpu_uuid,omitempty"`

	// System is the DCGM health watch system that generated the event.
	System string `json:"system"`

	// EventType categorizes the event (xid, thermal, memory, etc).
	EventType string `json:"event_type"`

	// Metrics contains event-specific data accessible in CEL policies.
	// Common keys:
	//   - xid_code: int (for XID events)
	//   - temperature: int (for thermal events)
	//   - power_watts: float64 (for power events)
	//   - memory_used_bytes: uint64 (for memory events)
	//   - ecc_sbe_count: int (single-bit ECC errors)
	//   - ecc_dbe_count: int (double-bit ECC errors)
	Metrics map[string]any `json:"metrics,omitempty"`

	// Message is a human-readable description of the event.
	Message string `json:"message,omitempty"`
}

// HealthEventCollector extends the base Manager interface with event collection.
// Implementations that support DCGM-style health events implement this interface.
type HealthEventCollector interface {
	Manager

	// CollectHealthEvents returns health events since the last collection.
	// Events are cleared after collection.
	CollectHealthEvents(ctx context.Context) ([]HealthEvent, error)
}

// NewXIDEvent creates a HealthEvent for an XID error.
func NewXIDEvent(gpuIndex int, gpuUUID string, xidCode int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    HealthSystemDriver,
		EventType: EventTypeXID,
		Metrics: map[string]any{
			"xid_code": xidCode,
		},
		Message: message,
	}
}

// NewThermalEvent creates a HealthEvent for a thermal warning.
func NewThermalEvent(gpuIndex int, gpuUUID string, temperature int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    HealthSystemThermal,
		EventType: EventTypeThermal,
		Metrics: map[string]any{
			"temperature": temperature,
		},
		Message: message,
	}
}

// NewMemoryEvent creates a HealthEvent for a memory error.
func NewMemoryEvent(gpuIndex int, gpuUUID string, eventType string, sbeCount, dbeCount int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    HealthSystemMem,
		EventType: eventType,
		Metrics: map[string]any{
			"ecc_sbe_count": sbeCount,
			"ecc_dbe_count": dbeCount,
		},
		Message: message,
	}
}

// NewNVLinkEvent creates a HealthEvent for an NVLink error.
func NewNVLinkEvent(gpuIndex int, gpuUUID string, linkID int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    HealthSystemNVLink,
		EventType: EventTypeNVLink,
		Metrics: map[string]any{
			"link_id": linkID,
		},
		Message: message,
	}
}

// NewPowerEvent creates a HealthEvent for a power issue.
func NewPowerEvent(gpuIndex int, gpuUUID string, powerWatts float64, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    HealthSystemPower,
		EventType: EventTypePower,
		Metrics: map[string]any{
			"power_watts": powerWatts,
		},
		Message: message,
	}
}
