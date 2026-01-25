package metrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProcReader_ReadCPUUsage(t *testing.T) {
	// Create temp directory for test files
	tmpDir := t.TempDir()

	// Create a mock /proc/stat file
	statPath := filepath.Join(tmpDir, "stat")
	meminfoPath := filepath.Join(tmpDir, "meminfo")

	// Initial stat content
	initialStat := `cpu  10000 500 2000 80000 1000 100 50 0 0 0
cpu0 5000 250 1000 40000 500 50 25 0 0 0
`
	if err := os.WriteFile(statPath, []byte(initialStat), 0644); err != nil {
		t.Fatalf("failed to write stat file: %v", err)
	}

	// Create mock meminfo for the reader
	meminfoContent := `MemTotal:       16000000 kB
MemAvailable:    8000000 kB
`
	if err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644); err != nil {
		t.Fatalf("failed to write meminfo file: %v", err)
	}

	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	// First read should return 0 (no previous data to compare)
	usage, err := reader.ReadCPUUsage(ctx)
	if err != nil {
		t.Fatalf("first ReadCPUUsage failed: %v", err)
	}
	if usage != 0 {
		t.Errorf("first read should return 0, got %f", usage)
	}

	// Update stat with more CPU usage
	updatedStat := `cpu  11000 600 2500 80500 1100 150 100 0 0 0
cpu0 5500 300 1250 40250 550 75 50 0 0 0
`
	if err := os.WriteFile(statPath, []byte(updatedStat), 0644); err != nil {
		t.Fatalf("failed to update stat file: %v", err)
	}

	// Second read should return actual CPU usage
	usage, err = reader.ReadCPUUsage(ctx)
	if err != nil {
		t.Fatalf("second ReadCPUUsage failed: %v", err)
	}

	// Calculate expected usage:
	// Delta total: (11000+600+2500+80500+1100+150+100) - (10000+500+2000+80000+1000+100+50) = 95950 - 93650 = 2300
	// Delta idle: (80500+1100) - (80000+1000) = 81600 - 81000 = 600
	// Usage: (2300-600)/2300 * 100 = 73.9%
	expectedMin := 70.0
	expectedMax := 80.0
	if usage < expectedMin || usage > expectedMax {
		t.Errorf("expected CPU usage between %.1f%% and %.1f%%, got %.2f%%", expectedMin, expectedMax, usage)
	}
}

func TestProcReader_ReadMemoryUsage(t *testing.T) {
	tmpDir := t.TempDir()
	meminfoPath := filepath.Join(tmpDir, "meminfo")
	statPath := filepath.Join(tmpDir, "stat")

	// Create mock /proc/meminfo
	meminfoContent := `MemTotal:       16000000 kB
MemFree:         2000000 kB
MemAvailable:    8000000 kB
Buffers:          500000 kB
Cached:          5500000 kB
`
	if err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644); err != nil {
		t.Fatalf("failed to write meminfo file: %v", err)
	}

	// Create empty stat file
	if err := os.WriteFile(statPath, []byte("cpu 0 0 0 0 0 0 0 0 0 0\n"), 0644); err != nil {
		t.Fatalf("failed to write stat file: %v", err)
	}

	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	usage, err := reader.ReadMemoryUsage(ctx)
	if err != nil {
		t.Fatalf("ReadMemoryUsage failed: %v", err)
	}

	// Expected: (16000000 - 8000000) / 16000000 * 100 = 50%
	expected := 50.0
	if usage != expected {
		t.Errorf("expected memory usage %.1f%%, got %.2f%%", expected, usage)
	}
}

func TestProcReader_ReadMemoryUsage_HighUsage(t *testing.T) {
	tmpDir := t.TempDir()
	meminfoPath := filepath.Join(tmpDir, "meminfo")
	statPath := filepath.Join(tmpDir, "stat")

	// Create mock /proc/meminfo with high memory usage
	meminfoContent := `MemTotal:       16000000 kB
MemFree:          500000 kB
MemAvailable:    1600000 kB
`
	if err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644); err != nil {
		t.Fatalf("failed to write meminfo file: %v", err)
	}

	if err := os.WriteFile(statPath, []byte("cpu 0 0 0 0 0 0 0 0 0 0\n"), 0644); err != nil {
		t.Fatalf("failed to write stat file: %v", err)
	}

	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	usage, err := reader.ReadMemoryUsage(ctx)
	if err != nil {
		t.Fatalf("ReadMemoryUsage failed: %v", err)
	}

	// Expected: (16000000 - 1600000) / 16000000 * 100 = 90%
	expected := 90.0
	if usage != expected {
		t.Errorf("expected memory usage %.1f%%, got %.2f%%", expected, usage)
	}
}

func TestProcReader_InvalidStatFile(t *testing.T) {
	tmpDir := t.TempDir()
	statPath := filepath.Join(tmpDir, "stat")
	meminfoPath := filepath.Join(tmpDir, "meminfo")

	// Non-existent file
	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	_, err := reader.ReadCPUUsage(ctx)
	if err == nil {
		t.Error("expected error for non-existent stat file")
	}
}

func TestProcReader_InvalidMeminfoFile(t *testing.T) {
	tmpDir := t.TempDir()
	statPath := filepath.Join(tmpDir, "stat")
	meminfoPath := filepath.Join(tmpDir, "meminfo")

	// Create valid stat but no meminfo
	if err := os.WriteFile(statPath, []byte("cpu 0 0 0 0 0 0 0 0 0 0\n"), 0644); err != nil {
		t.Fatalf("failed to write stat file: %v", err)
	}

	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	_, err := reader.ReadMemoryUsage(ctx)
	if err == nil {
		t.Error("expected error for non-existent meminfo file")
	}
}

func TestProcReader_MissingMemTotal(t *testing.T) {
	tmpDir := t.TempDir()
	meminfoPath := filepath.Join(tmpDir, "meminfo")
	statPath := filepath.Join(tmpDir, "stat")

	// Meminfo without MemTotal
	meminfoContent := `MemFree:         2000000 kB
MemAvailable:    8000000 kB
`
	if err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644); err != nil {
		t.Fatalf("failed to write meminfo file: %v", err)
	}

	if err := os.WriteFile(statPath, []byte("cpu 0 0 0 0 0 0 0 0 0 0\n"), 0644); err != nil {
		t.Fatalf("failed to write stat file: %v", err)
	}

	reader := NewProcReaderWithPaths(statPath, meminfoPath)
	ctx := context.Background()

	_, err := reader.ReadMemoryUsage(ctx)
	if err == nil {
		t.Error("expected error for missing MemTotal")
	}
}

func TestParseCPULine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr bool
	}{
		{
			name:    "valid full line",
			line:    "cpu  10000 500 2000 80000 1000 100 50 10 0 0",
			wantErr: false,
		},
		{
			name:    "valid minimal line",
			line:    "cpu  10000 500 2000 80000",
			wantErr: false,
		},
		{
			name:    "invalid - too few fields",
			line:    "cpu  10000 500",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric",
			line:    "cpu  abc 500 2000 80000",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCPULine(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCPULine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateCPUUsage(t *testing.T) {
	tests := []struct {
		name     string
		prev     *cpuStats
		curr     *cpuStats
		expected float64
	}{
		{
			name: "50% usage",
			prev: &cpuStats{user: 0, nice: 0, system: 0, idle: 100, iowait: 0},
			curr: &cpuStats{user: 50, nice: 0, system: 0, idle: 150, iowait: 0},
			expected: 50.0,
		},
		{
			name: "100% usage",
			prev: &cpuStats{user: 0, nice: 0, system: 0, idle: 100, iowait: 0},
			curr: &cpuStats{user: 100, nice: 0, system: 0, idle: 100, iowait: 0},
			expected: 100.0,
		},
		{
			name: "0% usage",
			prev: &cpuStats{user: 0, nice: 0, system: 0, idle: 100, iowait: 0},
			curr: &cpuStats{user: 0, nice: 0, system: 0, idle: 200, iowait: 0},
			expected: 0.0,
		},
		{
			name: "no change",
			prev: &cpuStats{user: 100, nice: 0, system: 0, idle: 100, iowait: 0},
			curr: &cpuStats{user: 100, nice: 0, system: 0, idle: 100, iowait: 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUUsage(tt.prev, tt.curr)
			if got != tt.expected {
				t.Errorf("calculateCPUUsage() = %v, want %v", got, tt.expected)
			}
		})
	}
}
