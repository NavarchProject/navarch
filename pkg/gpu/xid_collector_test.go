//go:build linux && cgo

package gpu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

func TestParseXIDLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantPCI string
		wantXID int
		wantMsg string
		wantOK  bool
	}{
		{
			name:    "GPU fallen off bus",
			line:    "NVRM: Xid (PCI:0000:41:00): 79, pid=12345, GPU has fallen off the bus",
			wantPCI: "0000:41:00",
			wantXID: 79,
			wantMsg: "pid=12345, GPU has fallen off the bus",
			wantOK:  true,
		},
		{
			name:    "Graphics Engine Exception",
			line:    "NVRM: Xid (PCI:0000:3b:00): 13, pid=0, Graphics Engine Exception",
			wantPCI: "0000:3b:00",
			wantXID: 13,
			wantMsg: "pid=0, Graphics Engine Exception",
			wantOK:  true,
		},
		{
			name:    "ECC error",
			line:    "NVRM: Xid (PCI:0000:86:00): 48, pid=9876, DBE (Double Bit Error)",
			wantPCI: "0000:86:00",
			wantXID: 48,
			wantMsg: "pid=9876, DBE (Double Bit Error)",
			wantOK:  true,
		},
		{
			name:    "XID without message",
			line:    "NVRM: Xid (PCI:0000:41:00): 63",
			wantPCI: "0000:41:00",
			wantXID: 63,
			wantMsg: "",
			wantOK:  true,
		},
		{
			name:   "not an XID line",
			line:   "some other log message",
			wantOK: false,
		},
		{
			name:   "partial XID pattern",
			line:   "NVRM: Xid (PCI:incomplete",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pci, xid, msg, ok := ParseXIDLine(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ParseXIDLine() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if !ok {
				return
			}
			if pci != tt.wantPCI {
				t.Errorf("ParseXIDLine() pci = %q, want %q", pci, tt.wantPCI)
			}
			if xid != tt.wantXID {
				t.Errorf("ParseXIDLine() xid = %d, want %d", xid, tt.wantXID)
			}
			if msg != tt.wantMsg {
				t.Errorf("ParseXIDLine() msg = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestXIDCollector_DmesgFallback(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kern.log")

	logContent := `Jan 15 10:00:00 host kernel: some normal log
Jan 15 10:00:01 host kernel: NVRM: Xid (PCI:0000:41:00): 79, pid=123, GPU has fallen off the bus
Jan 15 10:00:02 host kernel: some other log
Jan 15 10:00:03 host kernel: NVRM: Xid (PCI:0000:3b:00): 13, pid=0, Graphics Engine Exception
`
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	collector := NewXIDCollector()
	collector.SetLogPath(logPath)
	collector.SetPCIMappings(
		map[string]int{"0000:41:00": 0, "0000:3b:00": 1},
		map[string]string{"0000:41:00": "GPU-uuid-0", "0000:3b:00": "GPU-uuid-1"},
	)

	events, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("Collect() returned %d events, want 2", len(events))
	}

	// First event: XID 79
	if events[0].GPUIndex != 0 {
		t.Errorf("events[0].GPUIndex = %d, want 0", events[0].GPUIndex)
	}
	if events[0].GPUUUID != "GPU-uuid-0" {
		t.Errorf("events[0].GPUUUID = %q, want %q", events[0].GPUUUID, "GPU-uuid-0")
	}
	if events[0].EventType != pb.HealthEventType_HEALTH_EVENT_TYPE_XID {
		t.Errorf("events[0].EventType = %v, want XID", events[0].EventType)
	}
	if xid, ok := events[0].Metrics["xid_code"].(int); !ok || xid != 79 {
		t.Errorf("events[0].Metrics[xid_code] = %v, want 79", events[0].Metrics["xid_code"])
	}

	// Second event: XID 13
	if events[1].GPUIndex != 1 {
		t.Errorf("events[1].GPUIndex = %d, want 1", events[1].GPUIndex)
	}
	if xid, ok := events[1].Metrics["xid_code"].(int); !ok || xid != 13 {
		t.Errorf("events[1].Metrics[xid_code] = %v, want 13", events[1].Metrics["xid_code"])
	}

	// Second collect should return no new events
	events2, err := collector.Collect()
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if len(events2) != 0 {
		t.Errorf("second Collect() returned %d events, want 0", len(events2))
	}
}

func TestXIDCollector_LogRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kern.log")

	initial := `Jan 15 10:00:00 host kernel: NVRM: Xid (PCI:0000:41:00): 79, pid=123, error
`
	if err := os.WriteFile(logPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	collector := NewXIDCollector()
	collector.SetLogPath(logPath)

	events, err := collector.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("first Collect() returned %d events, want 1", len(events))
	}

	// Simulate log rotation: new file is shorter
	rotated := `Jan 16 00:00:00 host kernel: NVRM: Xid (PCI:0000:3b:00): 13, pid=0, error
`
	if err := os.WriteFile(logPath, []byte(rotated), 0644); err != nil {
		t.Fatal(err)
	}

	events, err = collector.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("after rotation Collect() returned %d events, want 1", len(events))
	}
	if xid, ok := events[0].Metrics["xid_code"].(int); !ok || xid != 13 {
		t.Errorf("rotated event xid_code = %v, want 13", events[0].Metrics["xid_code"])
	}
}

func TestXIDCollector_MissingLogFile(t *testing.T) {
	collector := NewXIDCollector()
	collector.SetLogPath("/nonexistent/path/kern.log")

	events, err := collector.Collect()
	if err != nil {
		t.Errorf("Collect() error = %v, want nil", err)
	}
	if events != nil {
		t.Errorf("Collect() events = %v, want nil", events)
	}
}

func TestXIDCollector_EmptyLogPath(t *testing.T) {
	collector := NewXIDCollector()
	collector.SetLogPath("")

	events, err := collector.Collect()
	if err != nil {
		t.Errorf("Collect() error = %v, want nil", err)
	}
	if events != nil {
		t.Errorf("Collect() events = %v, want nil", events)
	}
}

func TestXIDCollector_CollectFromDmesg(t *testing.T) {
	dmesgOutput := `[12345.678] some message
[12346.789] NVRM: Xid (PCI:0000:41:00): 79, pid=123, GPU has fallen off the bus
[12347.890] another message
[12348.901] NVRM: Xid (PCI:0000:3b:00): 48, pid=456, DBE error
`
	collector := NewXIDCollector()
	collector.SetPCIMappings(
		map[string]int{"0000:41:00": 0, "0000:3b:00": 1},
		nil,
	)

	events := collector.CollectFromDmesg(dmesgOutput)

	if len(events) != 2 {
		t.Fatalf("CollectFromDmesg() returned %d events, want 2", len(events))
	}

	if xid := events[0].Metrics["xid_code"]; xid != 79 {
		t.Errorf("events[0] xid_code = %v, want 79", xid)
	}
	if xid := events[1].Metrics["xid_code"]; xid != 48 {
		t.Errorf("events[1] xid_code = %v, want 48", xid)
	}
}

func TestNormalizePCIID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0000:41:00", "41:00"},
		{"41:00", "41:00"},
		{"0000:41:00.0", "41:00"},
		{"41:00.0", "41:00"},
		{"0000:3B:00", "3b:00"},
	}

	for _, tt := range tests {
		got := normalizePCIID(tt.input)
		if got != tt.want {
			t.Errorf("normalizePCIID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestXIDCollector_PCILookupNormalization(t *testing.T) {
	collector := NewXIDCollector()
	collector.SetPCIMappings(
		map[string]int{"0000:41:00.0": 0},
		map[string]string{"0000:41:00.0": "GPU-uuid-0"},
	)

	// Log uses different format than our mappings
	dmesg := "NVRM: Xid (PCI:0000:41:00): 79, pid=123, error"
	events := collector.CollectFromDmesg(dmesg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].GPUIndex != 0 {
		t.Errorf("GPUIndex = %d, want 0", events[0].GPUIndex)
	}
	if events[0].GPUUUID != "GPU-uuid-0" {
		t.Errorf("GPUUUID = %q, want GPU-uuid-0", events[0].GPUUUID)
	}
}

func TestXIDSeverity(t *testing.T) {
	tests := []struct {
		xid  int
		want string
	}{
		{79, "critical"},
		{13, "critical"},
		{48, "critical"},
		{8, "warning"},
		{32, "warning"},
		{1, "info"},
		{999, "info"},
	}

	for _, tt := range tests {
		got := XIDSeverity(tt.xid)
		if got != tt.want {
			t.Errorf("XIDSeverity(%d) = %q, want %q", tt.xid, got, tt.want)
		}
	}
}

func TestXIDDescription(t *testing.T) {
	tests := []struct {
		xid  int
		want string
	}{
		{79, "GPU has fallen off the bus"},
		{13, "Graphics Engine Exception"},
		{48, "Double Bit ECC Error"},
		{999, "XID error 999"},
	}

	for _, tt := range tests {
		got := XIDDescription(tt.xid)
		if got != tt.want {
			t.Errorf("XIDDescription(%d) = %q, want %q", tt.xid, got, tt.want)
		}
	}
}

func TestXIDCollector_SetPCIMappings(t *testing.T) {
	collector := NewXIDCollector()

	dmesg := "NVRM: Xid (PCI:0000:41:00): 79, pid=123, error"
	events := collector.CollectFromDmesg(dmesg)

	if events[0].GPUIndex != -1 {
		t.Errorf("before update, GPUIndex = %d, want -1", events[0].GPUIndex)
	}

	collector.SetPCIMappings(
		map[string]int{"0000:41:00": 5},
		map[string]string{"0000:41:00": "GPU-5"},
	)

	events = collector.CollectFromDmesg(dmesg)
	if events[0].GPUIndex != 5 {
		t.Errorf("after update, GPUIndex = %d, want 5", events[0].GPUIndex)
	}
	if events[0].GPUUUID != "GPU-5" {
		t.Errorf("after update, GPUUUID = %q, want GPU-5", events[0].GPUUUID)
	}
}

func TestNewXIDCollector(t *testing.T) {
	collector := NewXIDCollector()
	if collector == nil {
		t.Fatal("NewXIDCollector returned nil")
	}
	// Should have detected log path or be empty
	// Just verify it doesn't panic
}

func TestXIDCollector_Shutdown(t *testing.T) {
	collector := NewXIDCollector()
	// Shutdown should be safe to call even without Initialize
	collector.Shutdown()
	// Double shutdown should be safe
	collector.Shutdown()
}

func TestXIDCollector_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kern.log")

	logContent := `NVRM: Xid (PCI:0000:41:00): 79, pid=123, error
NVRM: Xid (PCI:0000:3b:00): 13, pid=456, error
`
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	collector := NewXIDCollector()
	collector.SetLogPath(logPath)
	collector.SetPCIMappings(
		map[string]int{"0000:41:00": 0, "0000:3b:00": 1},
		map[string]string{"0000:41:00": "GPU-0", "0000:3b:00": "GPU-1"},
	)

	// Collect first to position the reader
	_, err := collector.Collect()
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent access should be safe
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			collector.CollectFromDmesg("NVRM: Xid (PCI:0000:41:00): 79, pid=123, error")
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestXIDCollector_EventTimestamp(t *testing.T) {
	collector := NewXIDCollector()

	dmesg := "NVRM: Xid (PCI:0000:41:00): 79, pid=123, error"
	events := collector.CollectFromDmesg(dmesg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	// Timestamp should be set (non-zero)
	if events[0].Timestamp.IsZero() {
		t.Error("event timestamp is zero, expected non-zero")
	}
}

func TestXIDCollector_UnknownPCIDevice(t *testing.T) {
	collector := NewXIDCollector()
	collector.SetPCIMappings(
		map[string]int{"0000:41:00": 0},
		map[string]string{"0000:41:00": "GPU-0"},
	)

	// XID from unknown PCI device
	dmesg := "NVRM: Xid (PCI:0000:99:00): 79, pid=123, error"
	events := collector.CollectFromDmesg(dmesg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	// Unknown device should return -1 index and empty UUID
	if events[0].GPUIndex != -1 {
		t.Errorf("GPUIndex = %d, want -1", events[0].GPUIndex)
	}
	if events[0].GPUUUID != "" {
		t.Errorf("GPUUUID = %q, want empty", events[0].GPUUUID)
	}
}

func TestXIDCollector_LargeLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kern.log")

	// Create a log with many entries
	var logContent string
	for i := 0; i < 1000; i++ {
		logContent += "Jan 15 10:00:00 host kernel: some normal log\n"
		if i%100 == 0 {
			logContent += "Jan 15 10:00:01 host kernel: NVRM: Xid (PCI:0000:41:00): 79, pid=123, error\n"
		}
	}
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	collector := NewXIDCollector()
	collector.SetLogPath(logPath)

	events, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Should find 10 XID events (every 100th line)
	if len(events) != 10 {
		t.Errorf("Collect() returned %d events, want 10", len(events))
	}
}

func TestXIDCollector_NVMLModeBuffering(t *testing.T) {
	collector := NewXIDCollector()

	// Simulate being in NVML event mode by directly setting state
	// This tests that Collect() returns buffered events when in NVML mode
	collector.mu.Lock()
	collector.running = true
	collector.eventSet = nil // Force dmesg fallback even though running=true
	collector.mu.Unlock()

	collector.SetLogPath("")

	events, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	// Should return empty events, not error
	if len(events) != 0 {
		t.Errorf("Collect() returned %d events, want 0", len(events))
	}
}

func TestRecentXIDFilter(t *testing.T) {
	now := time.Now()
	events := []HealthEvent{
		{Timestamp: now.Add(-2 * time.Hour), Metrics: map[string]any{"xid_code": 1}},
		{Timestamp: now.Add(-30 * time.Minute), Metrics: map[string]any{"xid_code": 2}},
		{Timestamp: now.Add(-5 * time.Minute), Metrics: map[string]any{"xid_code": 3}},
	}

	filtered := RecentXIDFilter(events, now.Add(-1*time.Hour))

	if len(filtered) != 2 {
		t.Fatalf("RecentXIDFilter returned %d events, want 2", len(filtered))
	}
	if filtered[0].Metrics["xid_code"] != 2 {
		t.Errorf("first filtered event xid_code = %v, want 2", filtered[0].Metrics["xid_code"])
	}
	if filtered[1].Metrics["xid_code"] != 3 {
		t.Errorf("second filtered event xid_code = %v, want 3", filtered[1].Metrics["xid_code"])
	}
}
