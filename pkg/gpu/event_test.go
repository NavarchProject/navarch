package gpu

import (
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

func TestNewXIDEvent(t *testing.T) {
	event := NewXIDEvent(0, "GPU-12345", 79, "GPU has fallen off the bus")

	if event.GPUIndex != 0 {
		t.Errorf("GPUIndex = %d, want 0", event.GPUIndex)
	}
	if event.GPUUUID != "GPU-12345" {
		t.Errorf("GPUUUID = %s, want GPU-12345", event.GPUUUID)
	}
	if event.System != pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER {
		t.Errorf("System = %v, want DRIVER", event.System)
	}
	if event.EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_XID {
		t.Errorf("EventType = %v, want XID", event.EventType)
	}
	if event.Metrics["xid_code"] != 79 {
		t.Errorf("xid_code = %v, want 79", event.Metrics["xid_code"])
	}
	if event.Message != "GPU has fallen off the bus" {
		t.Errorf("Message = %s, want 'GPU has fallen off the bus'", event.Message)
	}
	if time.Since(event.Timestamp) > time.Second {
		t.Errorf("Timestamp too old: %v", event.Timestamp)
	}
}

func TestNewThermalEvent(t *testing.T) {
	event := NewThermalEvent(1, "GPU-67890", 95, "Temperature critical")

	if event.GPUIndex != 1 {
		t.Errorf("GPUIndex = %d, want 1", event.GPUIndex)
	}
	if event.System != pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL {
		t.Errorf("System = %v, want THERMAL", event.System)
	}
	if event.EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL {
		t.Errorf("EventType = %v, want THERMAL", event.EventType)
	}
	if event.Metrics["temperature"] != 95 {
		t.Errorf("temperature = %v, want 95", event.Metrics["temperature"])
	}
}

func TestNewMemoryEvent(t *testing.T) {
	event := NewMemoryEvent(2, "GPU-11111", pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE, 0, 1, "Double-bit ECC error")

	if event.GPUIndex != 2 {
		t.Errorf("GPUIndex = %d, want 2", event.GPUIndex)
	}
	if event.System != pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM {
		t.Errorf("System = %v, want MEM", event.System)
	}
	if event.EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE {
		t.Errorf("EventType = %v, want ECC_DBE", event.EventType)
	}
	if event.Metrics["ecc_sbe_count"] != 0 {
		t.Errorf("ecc_sbe_count = %v, want 0", event.Metrics["ecc_sbe_count"])
	}
	if event.Metrics["ecc_dbe_count"] != 1 {
		t.Errorf("ecc_dbe_count = %v, want 1", event.Metrics["ecc_dbe_count"])
	}
}

func TestNewNVLinkEvent(t *testing.T) {
	event := NewNVLinkEvent(0, "GPU-22222", 3, "NVLink error on link 3")

	if event.System != pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_NVLINK {
		t.Errorf("System = %v, want NVLINK", event.System)
	}
	if event.EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK {
		t.Errorf("EventType = %v, want NVLINK", event.EventType)
	}
	if event.Metrics["link_id"] != 3 {
		t.Errorf("link_id = %v, want 3", event.Metrics["link_id"])
	}
}

func TestNewPowerEvent(t *testing.T) {
	event := NewPowerEvent(0, "GPU-33333", 450.5, "Power limit exceeded")

	if event.System != pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_POWER {
		t.Errorf("System = %v, want POWER", event.System)
	}
	if event.EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_POWER {
		t.Errorf("EventType = %v, want POWER", event.EventType)
	}
	if event.Metrics["power_watts"] != 450.5 {
		t.Errorf("power_watts = %v, want 450.5", event.Metrics["power_watts"])
	}
}

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		input pb.HealthEventType
		want  string
	}{
		{pb.HealthEventType_HEALTH_EVENT_TYPE_XID, "xid"},
		{pb.HealthEventType_HEALTH_EVENT_TYPE_THERMAL, "thermal"},
		{pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE, "ecc_dbe"},
		{pb.HealthEventType_HEALTH_EVENT_TYPE_NVLINK, "nvlink"},
		{pb.HealthEventType_HEALTH_EVENT_TYPE_UNKNOWN, "unknown"},
	}

	for _, tt := range tests {
		got := EventTypeString(tt.input)
		if got != tt.want {
			t.Errorf("EventTypeString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSystemString(t *testing.T) {
	tests := []struct {
		input pb.HealthWatchSystem
		want  string
	}{
		{pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_DRIVER, "DCGM_HEALTH_WATCH_DRIVER"},
		{pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_THERMAL, "DCGM_HEALTH_WATCH_THERMAL"},
		{pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_MEM, "DCGM_HEALTH_WATCH_MEM"},
		{pb.HealthWatchSystem_HEALTH_WATCH_SYSTEM_UNKNOWN, "DCGM_HEALTH_WATCH_UNKNOWN"},
	}

	for _, tt := range tests {
		got := SystemString(tt.input)
		if got != tt.want {
			t.Errorf("SystemString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
