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

	// Stress test configuration (optional - enables stress testing mode)
	Stress *StressConfig `yaml:"stress,omitempty"`
}

// StressConfig defines stress test parameters for large-scale simulation.
type StressConfig struct {
	// Fleet generation (alternative to defining individual nodes)
	FleetGen *FleetGeneratorConfig `yaml:"fleet_gen,omitempty"`

	// Chaos engineering settings
	Chaos *ChaosConfig `yaml:"chaos,omitempty"`

	// Test duration (overrides event-based timing)
	Duration Duration `yaml:"duration,omitempty"`

	// Metrics collection interval
	MetricsInterval Duration `yaml:"metrics_interval,omitempty"`

	// Random seed for reproducibility (0 = random)
	Seed int64 `yaml:"seed,omitempty"`

	// Report output file (JSON format)
	ReportFile string `yaml:"report_file,omitempty"`

	// HTML report output file (visual report with charts)
	HTMLReportFile string `yaml:"html_report_file,omitempty"`

	// Log file for verbose output (useful for debugging/LLM context)
	LogFile string `yaml:"log_file,omitempty"`
}

// FleetGeneratorConfig defines how to generate a large fleet programmatically.
type FleetGeneratorConfig struct {
	// Total number of nodes to generate
	TotalNodes int `yaml:"total_nodes"`

	// Node templates with weighted distribution
	Templates []NodeTemplate `yaml:"templates"`

	// Provider distribution (provider -> percentage)
	Providers map[string]int `yaml:"providers,omitempty"`

	// Region distribution (region -> percentage)
	Regions map[string]int `yaml:"regions,omitempty"`

	// Zones per region (region -> []zones)
	Zones map[string][]string `yaml:"zones,omitempty"`

	// Startup configuration
	Startup StartupConfig `yaml:"startup,omitempty"`
}

// NodeTemplate defines a template for generating nodes.
type NodeTemplate struct {
	Name         string            `yaml:"name"`
	Weight       int               `yaml:"weight"` // Relative frequency
	GPUCount     int               `yaml:"gpu_count"`
	GPUType      string            `yaml:"gpu_type"`
	InstanceType string            `yaml:"instance_type"`
	Labels       map[string]string `yaml:"labels,omitempty"`
}

// StartupConfig controls how nodes join the cluster.
type StartupConfig struct {
	// Pattern: "instant", "linear", "exponential", "wave"
	Pattern string `yaml:"pattern,omitempty"`

	// Duration over which nodes start up
	Duration Duration `yaml:"duration,omitempty"`

	// Batch size for wave pattern
	BatchSize int `yaml:"batch_size,omitempty"`

	// Jitter percentage (0-100)
	JitterPercent int `yaml:"jitter_percent,omitempty"`
}

// ChaosConfig defines chaos engineering parameters.
type ChaosConfig struct {
	// Enable chaos engineering
	Enabled bool `yaml:"enabled"`

	// Failure injection rate (failures per minute per 1000 nodes)
	FailureRate float64 `yaml:"failure_rate"`

	// XID error distribution (code -> weight)
	XIDDistribution map[int]int `yaml:"xid_distribution,omitempty"`

	// Failure type distribution
	FailureTypes []FailureTypeWeight `yaml:"failure_types,omitempty"`

	// Cascading failure settings
	Cascading *CascadingConfig `yaml:"cascading,omitempty"`

	// Recovery settings
	Recovery *RecoveryConfig `yaml:"recovery,omitempty"`

	// Scheduled outages
	ScheduledOutages []ScheduledOutage `yaml:"scheduled_outages,omitempty"`

	// Correlated failures
	CorrelatedFailures []CorrelatedFailure `yaml:"correlated_failures,omitempty"`
}

// FailureTypeWeight defines a failure type and its probability weight.
type FailureTypeWeight struct {
	Type   string `yaml:"type"` // xid_error, temperature, nvml_failure, boot_failure, network
	Weight int    `yaml:"weight"`
}

// CascadingConfig controls cascading failure behavior.
type CascadingConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Probability        float64  `yaml:"probability"`         // 0.0-1.0
	MaxDepth           int      `yaml:"max_depth"`           // Maximum cascade depth
	MinDelay           Duration `yaml:"min_delay"`           // Minimum delay before cascade
	MaxDelay           Duration `yaml:"max_delay"`           // Maximum delay before cascade
	Scope              string   `yaml:"scope"`               // rack, zone, region, provider, random
	MaxAffectedPercent float64  `yaml:"max_affected_percent"` // Max % of scoped nodes affected
}

// RecoveryConfig controls automatic recovery behavior.
type RecoveryConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Probability        float64  `yaml:"probability"`           // Probability of recovery for non-fatal errors
	MeanTime           Duration `yaml:"mean_time"`             // Mean time to recovery
	StdDev             Duration `yaml:"std_dev"`               // Standard deviation of recovery time
}

// ScheduledOutage defines a planned outage event.
type ScheduledOutage struct {
	Name        string   `yaml:"name"`
	StartTime   Duration `yaml:"start_time"`
	Duration    Duration `yaml:"duration"`
	Scope       string   `yaml:"scope"`        // zone, region, provider, percentage
	Target      string   `yaml:"target"`       // Specific target or percentage
	FailureType string   `yaml:"failure_type"` // Type of failure to inject
}

// CorrelatedFailure defines failures that occur together.
type CorrelatedFailure struct {
	Name        string   `yaml:"name"`
	Trigger     string   `yaml:"trigger"`     // What triggers this (xid code or failure type)
	Response    string   `yaml:"response"`    // What failure to inject in response
	Probability float64  `yaml:"probability"` // 0.0-1.0
	Delay       Duration `yaml:"delay"`       // Delay before correlated failure
	Scope       string   `yaml:"scope"`       // same_node, same_rack, same_zone, random
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

	// Either fleet or stress.fleet_gen must be defined
	hasFleet := len(s.Fleet) > 0
	hasFleetGen := s.Stress != nil && s.Stress.FleetGen != nil && s.Stress.FleetGen.TotalNodes > 0

	if !hasFleet && !hasFleetGen {
		return fmt.Errorf("fleet or stress.fleet_gen must be defined")
	}

	if hasFleet {
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
	}

	if hasFleetGen {
		if err := s.Stress.FleetGen.Validate(); err != nil {
			return fmt.Errorf("invalid fleet_gen: %w", err)
		}
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

	if s.Stress != nil {
		if err := s.Stress.Validate(); err != nil {
			return fmt.Errorf("invalid stress config: %w", err)
		}
	}

	return nil
}

// Validate validates the fleet generator configuration.
func (f *FleetGeneratorConfig) Validate() error {
	if f.TotalNodes <= 0 {
		return fmt.Errorf("total_nodes must be positive")
	}
	if len(f.Templates) == 0 {
		return fmt.Errorf("at least one template is required")
	}
	for i, t := range f.Templates {
		if t.Name == "" {
			return fmt.Errorf("template %d: name is required", i)
		}
		if t.Weight <= 0 {
			return fmt.Errorf("template %s: weight must be positive", t.Name)
		}
		if t.GPUCount <= 0 {
			return fmt.Errorf("template %s: gpu_count must be positive", t.Name)
		}
	}
	return nil
}

// Validate validates the stress configuration.
func (s *StressConfig) Validate() error {
	if s.Chaos != nil && s.Chaos.Enabled {
		if s.Chaos.FailureRate < 0 {
			return fmt.Errorf("chaos.failure_rate must be non-negative")
		}
		if s.Chaos.Cascading != nil && s.Chaos.Cascading.Enabled {
			if s.Chaos.Cascading.Probability < 0 || s.Chaos.Cascading.Probability > 1 {
				return fmt.Errorf("cascading.probability must be between 0 and 1")
			}
			if s.Chaos.Cascading.MaxDepth <= 0 {
				s.Chaos.Cascading.MaxDepth = 3 // Default
			}
		}
	}
	return nil
}

// IsStressTest returns true if this scenario is configured for stress testing.
func (s *Scenario) IsStressTest() bool {
	return s.Stress != nil
}

// GetEffectiveDuration returns the duration for stress tests or 0 for regular scenarios.
func (s *Scenario) GetEffectiveDuration() time.Duration {
	if s.Stress != nil && s.Stress.Duration.Duration() > 0 {
		return s.Stress.Duration.Duration()
	}
	return 0
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

