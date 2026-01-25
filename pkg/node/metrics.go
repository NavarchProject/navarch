package node

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SystemMetrics holds CPU and memory usage information.
type SystemMetrics struct {
	CPUUsagePercent    float64
	MemoryUsagePercent float64
	MemoryTotalBytes   uint64
	MemoryUsedBytes    uint64
}

// MetricsCollector collects system metrics.
type MetricsCollector struct {
	prevCPUStats cpuStats
	prevTime     time.Time
}

type cpuStats struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// Collect gathers current system metrics.
func (c *MetricsCollector) Collect() (SystemMetrics, error) {
	var metrics SystemMetrics

	// Collect CPU metrics
	cpuPercent, err := c.collectCPU()
	if err == nil {
		metrics.CPUUsagePercent = cpuPercent
	}

	// Collect memory metrics
	memInfo, err := c.collectMemory()
	if err == nil {
		metrics.MemoryTotalBytes = memInfo.total
		metrics.MemoryUsedBytes = memInfo.used
		if memInfo.total > 0 {
			metrics.MemoryUsagePercent = float64(memInfo.used) / float64(memInfo.total) * 100
		}
	}

	return metrics, nil
}

func (c *MetricsCollector) collectCPU() (float64, error) {
	stats, err := readCPUStats()
	if err != nil {
		return 0, err
	}

	// First call - just store stats and return 0
	if c.prevTime.IsZero() {
		c.prevCPUStats = stats
		c.prevTime = time.Now()
		return 0, nil
	}

	// Calculate CPU usage since last call
	prevTotal := c.prevCPUStats.user + c.prevCPUStats.nice + c.prevCPUStats.system +
		c.prevCPUStats.idle + c.prevCPUStats.iowait
	currTotal := stats.user + stats.nice + stats.system + stats.idle + stats.iowait

	totalDelta := float64(currTotal - prevTotal)
	idleDelta := float64((stats.idle + stats.iowait) - (c.prevCPUStats.idle + c.prevCPUStats.iowait))

	c.prevCPUStats = stats
	c.prevTime = time.Now()

	if totalDelta == 0 {
		return 0, nil
	}

	return (1 - (idleDelta / totalDelta)) * 100, nil
}

func readCPUStats() (cpuStats, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStats{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return cpuStats{}, fmt.Errorf("invalid /proc/stat format")
			}

			var stats cpuStats
			stats.user, _ = strconv.ParseUint(fields[1], 10, 64)
			stats.nice, _ = strconv.ParseUint(fields[2], 10, 64)
			stats.system, _ = strconv.ParseUint(fields[3], 10, 64)
			stats.idle, _ = strconv.ParseUint(fields[4], 10, 64)
			if len(fields) > 5 {
				stats.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
			}

			return stats, nil
		}
	}

	return cpuStats{}, fmt.Errorf("cpu line not found in /proc/stat")
}

type memInfo struct {
	total     uint64
	available uint64
	used      uint64
}

func (c *MetricsCollector) collectMemory() (memInfo, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	defer file.Close()

	var info memInfo
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Values are in kB
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		value *= 1024 // Convert to bytes

		switch fields[0] {
		case "MemTotal:":
			info.total = value
		case "MemAvailable:":
			info.available = value
		}
	}

	if info.total > 0 && info.available > 0 {
		info.used = info.total - info.available
	}

	return info, nil
}
