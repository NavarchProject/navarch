package gpu

import (
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/NavarchProject/navarch/proto"
)

// EventTypeToProto converts a string event type to the proto enum.
func EventTypeToProto(eventType string) pb.HealthEventType {
	switch eventType {
	case EventTypeXID:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_XID
	case EventTypeThermal:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL
	case EventTypePower:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_POWER
	case EventTypeMemory:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_MEMORY
	case EventTypeNVLink:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK
	case EventTypePCIE:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_PCIE
	case EventTypeDriverError:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_DRIVER_ERROR
	case EventTypeECCSBE:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_SBE
	case EventTypeECCDBE:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE
	default:
		return pb.HealthEventType_HEALTH_EVENT_TYPE_UNKNOWN
	}
}

// EventTypeFromProto converts a proto enum to a string event type.
func EventTypeFromProto(eventType pb.HealthEventType) string {
	switch eventType {
	case pb.HealthEventType_HEALTH_EVENT_TYPE_XID:
		return EventTypeXID
	case pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL:
		return EventTypeThermal
	case pb.HealthEventType_HEALTH_EVENT_TYPE_POWER:
		return EventTypePower
	case pb.HealthEventType_HEALTH_EVENT_TYPE_MEMORY:
		return EventTypeMemory
	case pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK:
		return EventTypeNVLink
	case pb.HealthEventType_HEALTH_EVENT_TYPE_PCIE:
		return EventTypePCIE
	case pb.HealthEventType_HEALTH_EVENT_TYPE_DRIVER_ERROR:
		return EventTypeDriverError
	case pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_SBE:
		return EventTypeECCSBE
	case pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE:
		return EventTypeECCDBE
	default:
		return ""
	}
}

// SystemToProto converts a string health system to the proto enum.
func SystemToProto(system string) pb.HealthWatchSystem {
	switch system {
	case HealthSystemPCIE:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PCIE
	case HealthSystemNVLink:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVLINK
	case HealthSystemPMU:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PMU
	case HealthSystemMCU:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MCU
	case HealthSystemMem:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM
	case HealthSystemSM:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_SM
	case HealthSystemInforom:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_INFOROM
	case HealthSystemThermal:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL
	case HealthSystemPower:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_POWER
	case HealthSystemDriver:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER
	case HealthSystemNVSwitch:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVSWITCH
	default:
		return pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_UNKNOWN
	}
}

// SystemFromProto converts a proto enum to a string health system.
func SystemFromProto(system pb.HealthWatchSystem) string {
	switch system {
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PCIE:
		return HealthSystemPCIE
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVLINK:
		return HealthSystemNVLink
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_PMU:
		return HealthSystemPMU
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MCU:
		return HealthSystemMCU
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM:
		return HealthSystemMem
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_SM:
		return HealthSystemSM
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_INFOROM:
		return HealthSystemInforom
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL:
		return HealthSystemThermal
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_POWER:
		return HealthSystemPower
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER:
		return HealthSystemDriver
	case pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVSWITCH:
		return HealthSystemNVSwitch
	default:
		return ""
	}
}

// HealthEventToProto converts a HealthEvent to its proto representation.
func HealthEventToProto(event *HealthEvent) *pb.HealthEvent {
	if event == nil {
		return nil
	}

	// Convert metrics map to string values for proto
	metrics := make(map[string]string, len(event.Metrics))
	for k, v := range event.Metrics {
		metrics[k] = formatMetricValue(v)
	}

	return &pb.HealthEvent{
		Timestamp: timestamppb.New(event.Timestamp),
		GpuIndex:  int32(event.GPUIndex),
		GpuUuid:   event.GPUUUID,
		System:    SystemToProto(event.System),
		EventType: EventTypeToProto(event.EventType),
		Metrics:   metrics,
		Message:   event.Message,
	}
}

// HealthEventFromProto converts a proto HealthEvent to the Go type.
func HealthEventFromProto(event *pb.HealthEvent) *HealthEvent {
	if event == nil {
		return nil
	}

	// Convert metrics map from string values
	metrics := make(map[string]any, len(event.Metrics))
	for k, v := range event.Metrics {
		metrics[k] = parseMetricValue(v)
	}

	ts := time.Now()
	if event.Timestamp != nil {
		ts = event.Timestamp.AsTime()
	}

	return &HealthEvent{
		Timestamp: ts,
		GPUIndex:  int(event.GpuIndex),
		GPUUUID:   event.GpuUuid,
		System:    SystemFromProto(event.System),
		EventType: EventTypeFromProto(event.EventType),
		Metrics:   metrics,
		Message:   event.Message,
	}
}

// HealthEventsToProto converts a slice of HealthEvents to proto.
func HealthEventsToProto(events []HealthEvent) []*pb.HealthEvent {
	result := make([]*pb.HealthEvent, len(events))
	for i := range events {
		result[i] = HealthEventToProto(&events[i])
	}
	return result
}

// HealthEventsFromProto converts a slice of proto HealthEvents to Go types.
func HealthEventsFromProto(events []*pb.HealthEvent) []HealthEvent {
	result := make([]HealthEvent, len(events))
	for i, e := range events {
		if he := HealthEventFromProto(e); he != nil {
			result[i] = *he
		}
	}
	return result
}

// formatMetricValue converts a metric value to a string for proto.
func formatMetricValue(v any) string {
	switch val := v.(type) {
	case int:
		return fmt.Sprintf("%d", val)
	case int32:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case uint:
		return fmt.Sprintf("%d", val)
	case uint32:
		return fmt.Sprintf("%d", val)
	case uint64:
		return fmt.Sprintf("%d", val)
	case float32:
		return fmt.Sprintf("%f", val)
	case float64:
		return fmt.Sprintf("%f", val)
	case string:
		return val
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// parseMetricValue parses a string metric value back to its typed form.
// Returns the original string if parsing fails.
func parseMetricValue(s string) any {
	// Try parsing as int first (most common for xid_code, temperature, etc.)
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return int(i)
	}
	// Try parsing as float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Try parsing as bool
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	// Return as string
	return s
}
