package gpu

import (
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/NavarchProject/navarch/proto"
)

// HealthEventToProto converts a HealthEvent to its proto representation.
func HealthEventToProto(event *HealthEvent) *pb.HealthEvent {
	if event == nil {
		return nil
	}

	return &pb.HealthEvent{
		Timestamp: timestamppb.New(event.Timestamp),
		GpuIndex:  int32(event.GPUIndex),
		GpuUuid:   event.GPUUUID,
		System:    event.System,
		EventType: event.EventType,
		Metrics:   metricsToProto(event.Metrics),
		Message:   event.Message,
	}
}

// HealthEventFromProto converts a proto HealthEvent to the Go type.
func HealthEventFromProto(event *pb.HealthEvent) *HealthEvent {
	if event == nil {
		return nil
	}

	ts := time.Now()
	if event.Timestamp != nil {
		ts = event.Timestamp.AsTime()
	}

	return &HealthEvent{
		Timestamp: ts,
		GPUIndex:  int(event.GpuIndex),
		GPUUUID:   event.GpuUuid,
		System:    event.System,
		EventType: event.EventType,
		Metrics:   metricsFromProto(event.Metrics),
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

// metricsToProto converts a metrics map to string values for proto.
func metricsToProto(metrics map[string]any) map[string]string {
	if metrics == nil {
		return nil
	}

	result := make(map[string]string, len(metrics))
	for k, v := range metrics {
		result[k] = formatMetricValue(v)
	}
	return result
}

// metricsFromProto converts proto string metrics back to typed values.
func metricsFromProto(metrics map[string]string) map[string]any {
	if metrics == nil {
		return nil
	}

	result := make(map[string]any, len(metrics))
	for k, v := range metrics {
		result[k] = parseMetricValue(v)
	}
	return result
}

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

func parseMetricValue(s string) any {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return int(i)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	return s
}
