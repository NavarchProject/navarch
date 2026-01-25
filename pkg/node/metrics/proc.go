package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcReader reads system metrics from the /proc filesystem (Linux).
type ProcReader struct {
	mu sync.Mutex

	// Previous CPU stats for calculating delta
	prevCPUStats *cpuStats
	prevCPUTime  time.Time

	// Paths to proc files (can be overridden for testing)
	procStatPath    string
	procMeminfoPath string
}

// cpuStats holds CPU time values from /proc/stat.
type cpuStats struct {
	user    uint64
	nice    uint64
	system  uint64
	idle    uint64
	iowait  uint64
	irq     uint64
	softirq uint64
	steal   uint64
}

// NewProcReader creates a new ProcReader with default paths.
func NewProcReader() *ProcReader {
	return &ProcReader{
		procStatPath:    "/proc/stat",
		procMeminfoPath: "/proc/meminfo",
	}
}

// NewProcReaderWithPaths creates a ProcReader with custom paths (for testing).
func NewProcReaderWithPaths(statPath, meminfoPath string) *ProcReader {
	return &ProcReader{
		procStatPath:    statPath,
		procMeminfoPath: meminfoPath,
	}
}

// ReadCPUUsage returns the CPU usage as a percentage (0-100).
// It calculates usage based on the delta between two readings.
func (p *ProcReader) ReadCPUUsage(ctx context.Context) (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentStats, err := p.readCPUStats()
	if err != nil {
		return 0, err
	}

	now := time.Now()

	// If this is the first reading, store it and return 0
	if p.prevCPUStats == nil {
		p.prevCPUStats = currentStats
		p.prevCPUTime = now
		return 0, nil
	}

	// Calculate CPU usage from the delta
	usage := calculateCPUUsage(p.prevCPUStats, currentStats)

	// Update previous stats
	p.prevCPUStats = currentStats
	p.prevCPUTime = now

	return usage, nil
}

// readCPUStats parses /proc/stat and returns CPU statistics.
func (p *ProcReader) readCPUStats() (*cpuStats, error) {
	file, err := os.Open(p.procStatPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", p.procStatPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			return parseCPULine(line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", p.procStatPath, err)
	}

	return nil, fmt.Errorf("cpu line not found in %s", p.procStatPath)
}

// parseCPULine parses a cpu line from /proc/stat.
// Format: cpu user nice system idle iowait irq softirq steal guest guest_nice
func parseCPULine(line string) (*cpuStats, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil, fmt.Errorf("invalid cpu line format: %s", line)
	}

	stats := &cpuStats{}
	var err error

	// Parse required fields
	if stats.user, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse user: %w", err)
	}
	if stats.nice, err = strconv.ParseUint(fields[2], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse nice: %w", err)
	}
	if stats.system, err = strconv.ParseUint(fields[3], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse system: %w", err)
	}
	if stats.idle, err = strconv.ParseUint(fields[4], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse idle: %w", err)
	}

	// Parse optional fields
	if len(fields) > 5 {
		stats.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	}
	if len(fields) > 6 {
		stats.irq, _ = strconv.ParseUint(fields[6], 10, 64)
	}
	if len(fields) > 7 {
		stats.softirq, _ = strconv.ParseUint(fields[7], 10, 64)
	}
	if len(fields) > 8 {
		stats.steal, _ = strconv.ParseUint(fields[8], 10, 64)
	}

	return stats, nil
}

// calculateCPUUsage calculates CPU usage percentage from two stat readings.
func calculateCPUUsage(prev, curr *cpuStats) float64 {
	prevTotal := prev.user + prev.nice + prev.system + prev.idle + prev.iowait + prev.irq + prev.softirq + prev.steal
	currTotal := curr.user + curr.nice + curr.system + curr.idle + curr.iowait + curr.irq + curr.softirq + curr.steal

	totalDelta := float64(currTotal - prevTotal)
	if totalDelta == 0 {
		return 0
	}

	// Idle time includes iowait
	prevIdle := prev.idle + prev.iowait
	currIdle := curr.idle + curr.iowait
	idleDelta := float64(currIdle - prevIdle)

	usage := (totalDelta - idleDelta) / totalDelta * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}

	return usage
}

// ReadMemoryUsage returns the memory usage as a percentage (0-100).
func (p *ProcReader) ReadMemoryUsage(ctx context.Context) (float64, error) {
	file, err := os.Open(p.procMeminfoPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open %s: %w", p.procMeminfoPath, err)
	}
	defer file.Close()

	var memTotal, memAvailable uint64
	var foundTotal, foundAvailable bool

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "MemTotal":
			memTotal = value
			foundTotal = true
		case "MemAvailable":
			memAvailable = value
			foundAvailable = true
		}

		if foundTotal && foundAvailable {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read %s: %w", p.procMeminfoPath, err)
	}

	if !foundTotal {
		return 0, fmt.Errorf("MemTotal not found in %s", p.procMeminfoPath)
	}

	if !foundAvailable {
		return 0, fmt.Errorf("MemAvailable not found in %s", p.procMeminfoPath)
	}

	if memTotal == 0 {
		return 0, nil
	}

	memUsed := memTotal - memAvailable
	usage := float64(memUsed) / float64(memTotal) * 100

	return usage, nil
}
