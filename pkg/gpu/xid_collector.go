//go:build linux && cgo

package gpu

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// XIDCollector collects NVIDIA XID errors using NVML events.
// Falls back to dmesg parsing when NVML events are unavailable.
type XIDCollector struct {
	mu sync.Mutex

	// NVML event-based collection
	eventSet    nvml.EventSet
	devices     []nvml.Device
	deviceUUIDs []string

	// Dmesg fallback
	logPath      string
	lastPosition int64
	pciToIndex   map[string]int
	pciToUUID    map[string]string

	// Collected events buffer
	events []HealthEvent

	// State
	running bool
	cancel  context.CancelFunc
}

// XID error pattern in kernel logs:
// NVRM: Xid (PCI:0000:41:00): 79, pid=12345, GPU has fallen off the bus
var xidPattern = regexp.MustCompile(`NVRM: Xid \(PCI:([^)]+)\): (\d+)(?:, (.*))?`)

// NewXIDCollector creates a new XID collector.
// It attempts to use NVML events and falls back to dmesg parsing.
func NewXIDCollector() *XIDCollector {
	return &XIDCollector{
		logPath: findKernelLogPath(),
	}
}

// Initialize sets up NVML event monitoring for XID errors.
// Call this after NVML is initialized and devices are enumerated.
func (c *XIDCollector) Initialize(ctx context.Context, devices []nvml.Device) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	c.devices = devices
	c.deviceUUIDs = make([]string, len(devices))

	// Get UUIDs and build PCI mappings for fallback
	c.pciToIndex = make(map[string]int)
	c.pciToUUID = make(map[string]string)

	for i, device := range devices {
		uuid, ret := device.GetUUID()
		if ret == nvml.SUCCESS {
			c.deviceUUIDs[i] = uuid
		}

		pciInfo, ret := device.GetPciInfo()
		if ret == nvml.SUCCESS {
			pciID := pciBusIDToString(pciInfo.BusId)
			c.pciToIndex[pciID] = i
			c.pciToUUID[pciID] = c.deviceUUIDs[i]
		}
	}

	// Try to set up NVML event monitoring
	eventSet, ret := nvml.EventSetCreate()
	if ret != nvml.SUCCESS {
		// Fall back to dmesg parsing
		return nil
	}

	registered := false
	for _, device := range devices {
		// Check if device supports XID events
		supportedEvents, ret := device.GetSupportedEventTypes()
		if ret != nvml.SUCCESS {
			continue
		}

		if supportedEvents&nvml.EventTypeXidCriticalError != 0 {
			ret = device.RegisterEvents(nvml.EventTypeXidCriticalError, eventSet)
			if ret == nvml.SUCCESS {
				registered = true
			}
		}
	}

	if !registered {
		eventSet.Free()
		return nil
	}

	c.eventSet = eventSet
	c.running = true

	// Start background event collection
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	go c.collectEvents(ctx)

	return nil
}

// collectEvents runs in the background collecting NVML XID events.
func (c *XIDCollector) collectEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Wait for events with 100ms timeout to allow checking context
		data, ret := c.eventSet.Wait(100)
		if ret == nvml.ERROR_TIMEOUT {
			continue
		}
		if ret != nvml.SUCCESS {
			continue
		}

		if data.EventType&nvml.EventTypeXidCriticalError != 0 {
			c.handleXIDEvent(data)
		}
	}
}

// handleXIDEvent processes an XID event from NVML.
func (c *XIDCollector) handleXIDEvent(data nvml.EventData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find GPU index by matching device handle
	gpuIndex := -1
	gpuUUID := ""
	for i, device := range c.devices {
		if device == data.Device {
			gpuIndex = i
			gpuUUID = c.deviceUUIDs[i]
			break
		}
	}

	xidCode := int(data.EventData)
	if xidCode == 999 {
		// 999 means unknown XID error
		xidCode = 0
	}

	event := NewXIDEvent(gpuIndex, gpuUUID, xidCode, XIDDescription(xidCode))
	c.events = append(c.events, event)
}

// Shutdown stops event collection and frees resources.
func (c *XIDCollector) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.eventSet != nil {
		c.eventSet.Free()
		c.eventSet = nil
	}

	c.running = false
}

// Collect returns XID events since the last collection.
// Uses NVML events if available, otherwise falls back to dmesg parsing.
func (c *XIDCollector) Collect() ([]HealthEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If NVML events are working, return buffered events
	if c.running && c.eventSet != nil {
		events := c.events
		c.events = nil
		return events, nil
	}

	// Fall back to dmesg parsing
	return c.collectFromDmesg()
}

// collectFromDmesg parses kernel logs for XID errors.
func (c *XIDCollector) collectFromDmesg() ([]HealthEvent, error) {
	if c.logPath == "" {
		return nil, nil
	}

	f, err := os.Open(c.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open kernel log: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat kernel log: %w", err)
	}

	// Handle log rotation
	if info.Size() < c.lastPosition {
		c.lastPosition = 0
	}

	if c.lastPosition > 0 {
		if _, err := f.Seek(c.lastPosition, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek kernel log: %w", err)
		}
	}

	events, newPos, err := c.parseLogEntries(f)
	if err != nil {
		return nil, err
	}

	c.lastPosition = newPos
	return events, nil
}

// parseLogEntries reads log lines and extracts XID events.
func (c *XIDCollector) parseLogEntries(r io.Reader) ([]HealthEvent, int64, error) {
	var events []HealthEvent
	var bytesRead int64

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += int64(len(line)) + 1

		event := c.parseLine(line)
		if event != nil {
			events = append(events, *event)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan kernel log: %w", err)
	}

	return events, c.lastPosition + bytesRead, nil
}

// parseLine parses a single log line for XID errors.
func (c *XIDCollector) parseLine(line string) *HealthEvent {
	matches := xidPattern.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	pciID := matches[1]
	xidCode, err := strconv.Atoi(matches[2])
	if err != nil {
		return nil
	}

	message := ""
	if len(matches) > 3 {
		message = matches[3]
	}

	gpuIndex := c.lookupGPUIndex(pciID)
	gpuUUID := c.lookupGPUUUID(pciID)

	event := NewXIDEvent(gpuIndex, gpuUUID, xidCode, message)
	return &event
}

// lookupGPUIndex returns the GPU index for a PCI bus ID.
func (c *XIDCollector) lookupGPUIndex(pciID string) int {
	if c.pciToIndex == nil {
		return -1
	}
	if idx, ok := c.pciToIndex[pciID]; ok {
		return idx
	}
	normalized := normalizePCIID(pciID)
	for k, v := range c.pciToIndex {
		if normalizePCIID(k) == normalized {
			return v
		}
	}
	return -1
}

// lookupGPUUUID returns the GPU UUID for a PCI bus ID.
func (c *XIDCollector) lookupGPUUUID(pciID string) string {
	if c.pciToUUID == nil {
		return ""
	}
	if uuid, ok := c.pciToUUID[pciID]; ok {
		return uuid
	}
	normalized := normalizePCIID(pciID)
	for k, v := range c.pciToUUID {
		if normalizePCIID(k) == normalized {
			return v
		}
	}
	return ""
}

// normalizePCIID normalizes a PCI bus ID.
func normalizePCIID(pciID string) string {
	if len(pciID) > 5 && pciID[4] == ':' {
		pciID = pciID[5:]
	}
	if idx := strings.LastIndex(pciID, "."); idx > 0 {
		pciID = pciID[:idx]
	}
	return strings.ToLower(pciID)
}

// findKernelLogPath returns the path to the kernel log file.
// Prefers /dev/kmsg for real-time kernel messages (like gpud does),
// falls back to log files.
func findKernelLogPath() string {
	paths := []string{
		"/dev/kmsg",         // Real-time kernel ring buffer (preferred)
		"/var/log/kern.log", // Debian/Ubuntu
		"/var/log/messages", // RHEL/CentOS
		"/var/log/syslog",   // Some systems
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// SetLogPath sets the log path for testing.
func (c *XIDCollector) SetLogPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logPath = path
	c.lastPosition = 0
}

// SetPCIMappings sets the PCI to GPU mappings for dmesg fallback.
func (c *XIDCollector) SetPCIMappings(pciToIndex map[string]int, pciToUUID map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pciToIndex = pciToIndex
	c.pciToUUID = pciToUUID
}

// CollectFromDmesg parses dmesg output for XID errors.
// This is useful for testing or when log files are not accessible.
func (c *XIDCollector) CollectFromDmesg(output string) []HealthEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	var events []HealthEvent
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		if event := c.parseLine(scanner.Text()); event != nil {
			events = append(events, *event)
		}
	}
	return events
}

// ParseXIDLine parses a single line for XID errors.
func ParseXIDLine(line string) (pciID string, xidCode int, message string, ok bool) {
	matches := xidPattern.FindStringSubmatch(line)
	if matches == nil {
		return "", 0, "", false
	}

	pciID = matches[1]
	code, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, "", false
	}

	if len(matches) > 3 {
		message = matches[3]
	}

	return pciID, code, message, true
}

// XIDSeverity returns the severity level for an XID code.
func XIDSeverity(xidCode int) string {
	critical := map[int]bool{
		13: true, 31: true, 43: true, 45: true, 48: true,
		61: true, 62: true, 63: true, 64: true, 74: true,
		79: true, 92: true, 94: true, 95: true,
	}
	warning := map[int]bool{
		8: true, 32: true, 38: true, 56: true,
		57: true, 68: true, 69: true, 119: true,
	}

	if critical[xidCode] {
		return "critical"
	}
	if warning[xidCode] {
		return "warning"
	}
	return "info"
}

// XIDDescription returns a human-readable description for an XID code.
func XIDDescription(xidCode int) string {
	descriptions := map[int]string{
		8:   "GPU memory access fault",
		13:  "Graphics Engine Exception",
		31:  "GPU memory page fault",
		32:  "Invalid or corrupted push buffer stream",
		38:  "Driver firmware error",
		43:  "GPU stopped processing",
		45:  "Preemptive cleanup, due to previous errors",
		48:  "Double Bit ECC Error",
		56:  "Display engine error",
		57:  "Unknown error in channel",
		61:  "Internal Micro-controller Breakpoint",
		62:  "Internal Micro-controller Halt",
		63:  "ECC page retirement or row remapping recording event",
		64:  "ECC page retirement or row remapping recording failure",
		68:  "Video processor exception",
		69:  "GSP firmware error",
		74:  "NVLink error",
		79:  "GPU has fallen off the bus",
		92:  "High single bit ECC error rate",
		94:  "Contained ECC error",
		95:  "Uncontained ECC error",
		119: "GSP RPC timeout",
	}

	if desc, ok := descriptions[xidCode]; ok {
		return desc
	}
	return fmt.Sprintf("XID error %d", xidCode)
}

// RecentXIDFilter filters events to only include those after a given timestamp.
func RecentXIDFilter(events []HealthEvent, since time.Time) []HealthEvent {
	var filtered []HealthEvent
	for _, e := range events {
		if e.Timestamp.After(since) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
