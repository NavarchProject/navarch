package gpu

import (
	"testing"
	"time"
)

func TestNewXIDEvent(t *testing.T) {
	event := NewXIDEvent(0, "GPU-12345", 79, "GPU has fallen off the bus")

	if event.GPUIndex != 0 {
		t.Errorf("GPUIndex = %d, want 0", event.GPUIndex)
	}
	if event.GPUUUID != "GPU-12345" {
		t.Errorf("GPUUUID = %s, want GPU-12345", event.GPUUUID)
	}
	if event.System != HealthSystemDriver {
		t.Errorf("System = %s, want %s", event.System, HealthSystemDriver)
	}
	if event.EventType != EventTypeXID {
		t.Errorf("EventType = %s, want %s", event.EventType, EventTypeXID)
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
	if event.System != HealthSystemThermal {
		t.Errorf("System = %s, want %s", event.System, HealthSystemThermal)
	}
	if event.EventType != EventTypeThermal {
		t.Errorf("EventType = %s, want %s", event.EventType, EventTypeThermal)
	}
	if event.Metrics["temperature"] != 95 {
		t.Errorf("temperature = %v, want 95", event.Metrics["temperature"])
	}
}

func TestNewMemoryEvent(t *testing.T) {
	event := NewMemoryEvent(2, "GPU-11111", EventTypeECCDBE, 0, 1, "Double-bit ECC error")

	if event.GPUIndex != 2 {
		t.Errorf("GPUIndex = %d, want 2", event.GPUIndex)
	}
	if event.System != HealthSystemMem {
		t.Errorf("System = %s, want %s", event.System, HealthSystemMem)
	}
	if event.EventType != EventTypeECCDBE {
		t.Errorf("EventType = %s, want %s", event.EventType, EventTypeECCDBE)
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

	if event.System != HealthSystemNVLink {
		t.Errorf("System = %s, want %s", event.System, HealthSystemNVLink)
	}
	if event.EventType != EventTypeNVLink {
		t.Errorf("EventType = %s, want %s", event.EventType, EventTypeNVLink)
	}
	if event.Metrics["link_id"] != 3 {
		t.Errorf("link_id = %v, want 3", event.Metrics["link_id"])
	}
}

func TestNewPowerEvent(t *testing.T) {
	event := NewPowerEvent(0, "GPU-33333", 450.5, "Power limit exceeded")

	if event.System != HealthSystemPower {
		t.Errorf("System = %s, want %s", event.System, HealthSystemPower)
	}
	if event.EventType != EventTypePower {
		t.Errorf("EventType = %s, want %s", event.EventType, EventTypePower)
	}
	if event.Metrics["power_watts"] != 450.5 {
		t.Errorf("power_watts = %v, want 450.5", event.Metrics["power_watts"])
	}
}
