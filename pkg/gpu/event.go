package gpu

import (
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// HealthEvent represents a GPU health event from DCGM or other monitoring backends.
// Events are sent to the control plane where CEL policies evaluate them.
type HealthEvent struct {
	Timestamp time.Time
	GPUIndex  int
	GPUUUID   string
	System    pb.HealthWatchSystem
	EventType pb.HealthEventType
	Metrics   map[string]any
	Message   string
}

// NewXIDEvent creates a HealthEvent for an XID error.
func NewXIDEvent(gpuIndex int, gpuUUID string, xidCode int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER,
		EventType: pb.HealthEventType_HEALTH_EVENT_TYPE_XID,
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
		System:    pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL,
		EventType: pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL,
		Metrics: map[string]any{
			"temperature": temperature,
		},
		Message: message,
	}
}

// NewMemoryEvent creates a HealthEvent for a memory error.
func NewMemoryEvent(gpuIndex int, gpuUUID string, eventType pb.HealthEventType, sbeCount, dbeCount int, message string) HealthEvent {
	return HealthEvent{
		Timestamp: time.Now(),
		GPUIndex:  gpuIndex,
		GPUUUID:   gpuUUID,
		System:    pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM,
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
		System:    pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVLINK,
		EventType: pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK,
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
		System:    pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_POWER,
		EventType: pb.HealthEventType_HEALTH_EVENT_TYPE_POWER,
		Metrics: map[string]any{
			"power_watts": powerWatts,
		},
		Message: message,
	}
}

// EventTypeString returns a CEL-friendly string for the event type.
func EventTypeString(t pb.HealthEventType) string {
	switch t {
	case pb.HealthEventType_HEALTH_EVENT_TYPE_XID:
		return "xid"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL:
		return "thermal"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_POWER:
		return "power"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_MEMORY:
		return "memory"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK:
		return "nvlink"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_PCIE:
		return "pcie"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_DRIVER_ERROR:
		return "driver_error"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_SBE:
		return "ecc_sbe"
	case pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE:
		return "ecc_dbe"
	default:
		return "unknown"
	}
}

// SystemString returns a CEL-friendly string for the health watch system.
func SystemString(s pb.HealthWatchSystem) string {
	switch s {
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PCIE:
		return "DCGM_HEALTH_WATCH_PCIE"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVLINK:
		return "DCGM_HEALTH_WATCH_NVLINK"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PMU:
		return "DCGM_HEALTH_WATCH_PMU"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MCU:
		return "DCGM_HEALTH_WATCH_MCU"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM:
		return "DCGM_HEALTH_WATCH_MEM"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_SM:
		return "DCGM_HEALTH_WATCH_SM"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_INFOROM:
		return "DCGM_HEALTH_WATCH_INFOROM"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL:
		return "DCGM_HEALTH_WATCH_THERMAL"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_POWER:
		return "DCGM_HEALTH_WATCH_POWER"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER:
		return "DCGM_HEALTH_WATCH_DRIVER"
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVSWITCH:
		return "DCGM_HEALTH_WATCH_NVSWITCH"
	default:
		return "DCGM_HEALTH_WATCH_UNKNOWN"
	}
}
