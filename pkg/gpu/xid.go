package gpu

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// XID error pattern from NVIDIA kernel module
// Example: "NVRM: Xid (PCI:0000:3b:00.0): 79, pid=1234, GPU has fallen off the bus."
var xidPattern = regexp.MustCompile(`NVRM: Xid \(PCI:([0-9a-fA-F:\.]+)\): (\d+),(.*)`)

// XIDParser parses XID errors from system logs.
type XIDParser struct {
	lastCheck time.Time
}

// NewXIDParser creates a new XID parser.
func NewXIDParser() *XIDParser {
	return &XIDParser{
		lastCheck: time.Now(),
	}
}

// Parse reads dmesg output and extracts XID errors since the last check.
func (p *XIDParser) Parse() ([]*XIDError, error) {
	output, err := p.readDmesg()
	if err != nil {
		return nil, fmt.Errorf("failed to read dmesg: %w", err)
	}

	errors := p.parseOutput(output)
	p.lastCheck = time.Now()

	return errors, nil
}

// ParseSince reads dmesg output and extracts XID errors since a specific time.
func (p *XIDParser) ParseSince(since time.Time) ([]*XIDError, error) {
	output, err := p.readDmesg()
	if err != nil {
		return nil, fmt.Errorf("failed to read dmesg: %w", err)
	}

	return p.parseOutput(output), nil
}

// readDmesg executes dmesg and returns the output.
func (p *XIDParser) readDmesg() (string, error) {
	// Try dmesg first (requires root or appropriate permissions)
	cmd := exec.Command("dmesg", "--time-format=iso")
	output, err := cmd.Output()
	if err == nil {
		return string(output), nil
	}

	// Fall back to dmesg without timestamp format
	cmd = exec.Command("dmesg")
	output, err = cmd.Output()
	if err == nil {
		return string(output), nil
	}

	// Try journalctl as alternative
	cmd = exec.Command("journalctl", "-k", "--no-pager", "-o", "short-iso")
	output, err = cmd.Output()
	if err == nil {
		return string(output), nil
	}

	return "", fmt.Errorf("failed to read system logs: dmesg and journalctl both failed")
}

// parseOutput parses dmesg output and extracts XID errors.
func (p *XIDParser) parseOutput(output string) []*XIDError {
	var errors []*XIDError

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		// Look for XID pattern
		matches := xidPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		pciID := matches[1]
		xidCode, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}
		message := strings.TrimSpace(matches[3])

		// Extract timestamp from line if present (ISO format)
		timestamp := extractTimestamp(line)

		errors = append(errors, &XIDError{
			Timestamp: timestamp,
			DeviceID:  pciID,
			XIDCode:   xidCode,
			Message:   message,
		})
	}

	return errors
}

// extractTimestamp attempts to extract a timestamp from a dmesg line.
func extractTimestamp(line string) string {
	// ISO format: 2024-01-15T10:30:45,123456+00:00
	isoPattern := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`)
	if matches := isoPattern.FindStringSubmatch(line); matches != nil {
		return matches[1]
	}

	// Kernel timestamp format: [12345.678901]
	kernelPattern := regexp.MustCompile(`\[\s*(\d+\.\d+)\]`)
	if matches := kernelPattern.FindStringSubmatch(line); matches != nil {
		return matches[1]
	}

	return time.Now().Format(time.RFC3339)
}

// XIDSeverity returns the severity of an XID error code.
// Returns "fatal", "critical", "warning", or "info".
func XIDSeverity(code int) string {
	// Based on NVIDIA XID error documentation
	// https://docs.nvidia.com/deploy/xid-errors/index.html
	switch code {
	case 13, 31, 32, 43, 45, 64, 68, 69, 79, 92, 94, 95, 119:
		return "fatal" // Hardware failure, needs replacement
	case 48, 74:
		return "critical" // Double-bit ECC error
	case 63:
		return "warning" // Row remapping event
	default:
		return "info"
	}
}

// XIDDescription returns a human-readable description for an XID code.
func XIDDescription(code int) string {
	descriptions := map[int]string{
		13:  "Graphics Engine Exception",
		31:  "GPU memory page fault",
		32:  "Invalid or corrupted push buffer stream",
		43:  "GPU stopped processing",
		45:  "Preemptive cleanup, due to previous errors",
		48:  "Double Bit ECC Error",
		63:  "ECC page retirement or row remapping event",
		64:  "ECC page retirement or row remapping recording failure",
		68:  "Video processor exception",
		69:  "Graphics Engine class error",
		74:  "NVLINK Error",
		79:  "GPU has fallen off the bus",
		92:  "High single-bit ECC error rate",
		94:  "Contained ECC error",
		95:  "Uncontained ECC error",
		119: "GSP RPC timeout",
	}

	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return fmt.Sprintf("Unknown XID error (code %d)", code)
}

// IsFatalXID returns true if the XID code indicates a fatal hardware error
// that typically requires node replacement.
func IsFatalXID(code int) bool {
	return XIDSeverity(code) == "fatal"
}

