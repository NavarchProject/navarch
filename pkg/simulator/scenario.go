package simulator

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario defines a simulation scenario to run against the control plane.
type Scenario struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Fleet       []NodeSpec   `yaml:"fleet"`
	Events      []Event      `yaml:"events"`
	Assertions  []Assertion  `yaml:"assertions,omitempty"`
}

// NodeSpec defines a simulated node in the fleet.
type NodeSpec struct {
	ID           string            `yaml:"id"`
	Provider     string            `yaml:"provider"`
	Region       string            `yaml:"region"`
	Zone         string            `yaml:"zone"`
	InstanceType string            `yaml:"instance_type"`
	GPUCount     int               `yaml:"gpu_count"`
	GPUType      string            `yaml:"gpu_type"`
	Labels       map[string]string `yaml:"labels,omitempty"`
}

// Event represents something that happens during a scenario.
type Event struct {
	At     Duration    `yaml:"at"`
	Action string      `yaml:"action"`
	Target string      `yaml:"target,omitempty"`
	Params EventParams `yaml:"params,omitempty"`
}

// EventParams holds action-specific parameters.
type EventParams struct {
	// For inject_failure
	FailureType string `yaml:"failure_type,omitempty"`
	XIDCode     int    `yaml:"xid_code,omitempty"`
	GPUIndex    int    `yaml:"gpu_index,omitempty"`
	Message     string `yaml:"message,omitempty"`

	// For issue_command
	CommandType string            `yaml:"command_type,omitempty"`
	CommandArgs map[string]string `yaml:"command_args,omitempty"`

	// For wait_for_status
	ExpectedStatus string `yaml:"expected_status,omitempty"`
	Timeout        Duration `yaml:"timeout,omitempty"`

	// For log
	LogMessage string `yaml:"log_message,omitempty"`

	// For recover_failure
	// (uses Target to specify which node)
}

// Assertion defines a condition to verify at the end of the scenario.
type Assertion struct {
	Type     string `yaml:"type"`
	Target   string `yaml:"target"`
	Expected string `yaml:"expected"`
}

// Duration is a wrapper for time.Duration that supports YAML unmarshaling.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// LoadScenario loads a scenario from a YAML file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read scenario file: %w", err)
	}

	var scenario Scenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return nil, fmt.Errorf("failed to parse scenario: %w", err)
	}

	if err := scenario.Validate(); err != nil {
		return nil, fmt.Errorf("invalid scenario: %w", err)
	}

	return &scenario, nil
}

// Validate checks that the scenario is well-formed.
func (s *Scenario) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("scenario name is required")
	}
	if len(s.Fleet) == 0 {
		return fmt.Errorf("fleet must have at least one node")
	}

	nodeIDs := make(map[string]bool)
	for _, node := range s.Fleet {
		if node.ID == "" {
			return fmt.Errorf("node ID is required")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID: %s", node.ID)
		}
		nodeIDs[node.ID] = true
	}

	validActions := map[string]bool{
		"start_fleet":       true,
		"stop_fleet":        true,
		"inject_failure":    true,
		"recover_failure":   true,
		"issue_command":     true,
		"wait_for_status":   true,
		"wait":              true,
		"log":               true,
		"assert":            true,
	}

	for i, event := range s.Events {
		if !validActions[event.Action] {
			return fmt.Errorf("event %d: unknown action %q", i, event.Action)
		}
	}

	return nil
}

// Known XID error codes and their meanings.
var XIDCodes = map[int]XIDInfo{
	13:  {Code: 13, Name: "Graphics Engine Exception", Fatal: false, Description: "Usually a shader/compute kernel issue"},
	31:  {Code: 31, Name: "GPU memory page fault", Fatal: false, Description: "Memory access violation, often recoverable"},
	32:  {Code: 32, Name: "Invalid or corrupted push buffer stream", Fatal: false, Description: "Command buffer corruption"},
	43:  {Code: 43, Name: "GPU stopped processing", Fatal: true, Description: "GPU hang requiring reset"},
	45:  {Code: 45, Name: "Preemptive cleanup", Fatal: false, Description: "Driver preemptively cleaned up due to previous errors"},
	48:  {Code: 48, Name: "Double Bit ECC Error", Fatal: true, Description: "Uncorrectable memory error"},
	63:  {Code: 63, Name: "ECC page retirement/row remapping failure", Fatal: true, Description: "Memory subsystem degradation"},
	64:  {Code: 64, Name: "ECC page retirement/row remapping recording event", Fatal: false, Description: "ECC error being handled"},
	68:  {Code: 68, Name: "NVDEC0 Exception", Fatal: false, Description: "Video decoder error"},
	74:  {Code: 74, Name: "NVLink Error", Fatal: true, Description: "Multi-GPU interconnect failure"},
	79:  {Code: 79, Name: "GPU has fallen off the bus", Fatal: true, Description: "Complete GPU failure, hardware issue"},
	92:  {Code: 92, Name: "High single-bit ECC error rate", Fatal: false, Description: "Memory degradation warning"},
	94:  {Code: 94, Name: "Contained ECC error", Fatal: false, Description: "ECC error contained and corrected"},
	95:  {Code: 95, Name: "Uncontained ECC error", Fatal: true, Description: "ECC error that could not be contained"},
}

// XIDInfo describes an XID error code.
type XIDInfo struct {
	Code        int
	Name        string
	Fatal       bool
	Description string
}

